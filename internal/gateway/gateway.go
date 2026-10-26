// Package gateway serves the launcher: the embedded app, the JSON API and the icons. Every route is
// read-only, every request is checked against the host allowlist, and nothing here knows a service
// name (decisions #10, #38, #54).
package gateway

import (
	"context"
	"encoding/json"
	"io"
	"io/fs"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/autumnsgrove/meridian/internal/config"
	"github.com/autumnsgrove/meridian/internal/control"
	"github.com/autumnsgrove/meridian/internal/probe"
	"github.com/autumnsgrove/meridian/internal/wire"
)

// The documented server limits. WriteTimeout has to clear the six seconds a page load may wait for
// fresh probe results (T15); the rest are the conservative values from STANDARDS section 4.
const (
	readHeaderTimeout = 5 * time.Second
	readTimeout       = 10 * time.Second
	writeTimeout      = 15 * time.Second
	idleTimeout       = 60 * time.Second
	maxHeaderBytes    = 16 << 10
	shutdownDrain     = 5 * time.Second
)

// LoadStatus is how the last configuration load went. A rejected reload keeps the previous config
// serving and sets Error, which the page shows as a banner (decision #41).
type LoadStatus struct {
	// LoadedAt is when the configuration being served was accepted.
	LoadedAt time.Time
	// Error is a short "path: code" summary of a rejected reload, empty after a good load.
	Error string
}

// Deps is everything the gateway serves from. Current is the only field a working gateway needs; the
// rest have a sensible zero value so a test can replace just the part it is exercising.
type Deps struct {
	// Current returns the configuration being served and how its last load went. It is called once
	// per request, which is what makes a SIGHUP reload take effect without a restart. A nil func, or
	// a nil config, means no configuration has ever loaded.
	Current func() (*config.Config, LoadStatus)
	// Logger receives the access log and recovered panics. A nil logger stays silent.
	Logger *log.Logger
	// Version is the build version, reported by /healthz.
	Version string
	// ConfigDir is the directory holding config.yaml; its icons/ subdirectory is served. An empty
	// directory means no icon resolves.
	ConfigDir string
	// Static is the built app. A nil Static serves web.FS(), the app embedded in this binary.
	Static fs.FS
	// Store is the probe results the page is built from. A nil store means nothing has been probed.
	Store *probe.Store
	// Scheduler keeps those results current. A page load touches it and waits for a stale service.
	Scheduler *probe.Scheduler
	// Host reads the latest host snapshot. A nil Host, or no snapshot, leaves the host block null.
	Host func() (wire.HostSnapshot, bool)
	// Control is the container side of the control plane. A nil Control means this box has none, which
	// is every Phase 1 deployment: /api/control answers 503 and /api/status says so.
	Control *control.Plane
	// Now is the clock that stamps the response. Nil means the wall clock.
	Now func() time.Time
}

// Route is one entry of the route table: a Go 1.22+ pattern and the handler that answers it.
type Route struct {
	Pattern string
	Handler http.Handler
}

// Routes is the whole route table as data, so the invariants test can read it and prove the launcher
// offers nothing but GET.
func Routes(deps Deps) []Route {
	return []Route{
		{Pattern: "GET /healthz", Handler: healthz(deps)},
		{Pattern: "GET /api/status", Handler: status(deps)},
		{Pattern: "POST /api/control", Handler: enqueue(deps)},
		{Pattern: "GET /api/control/status", Handler: controlStatus(deps)},
		{Pattern: "GET /icons/{file}", Handler: icons(deps)},
		{Pattern: "/", Handler: static(deps)},
	}
}

// Handler assembles the route table and the middleware, in the documented order: a panic is
// recovered before anything else sees it, the security headers are set on the way in, the Host is
// checked, the request is logged, and only a mutating request meets the browser guard.
func (deps Deps) Handler() http.Handler {
	mux := http.NewServeMux()
	for _, route := range Routes(deps) {
		mux.Handle(route.Pattern, route.Handler)
	}
	// The allowlist is read per request, so a reload that changes it is enforced at once.
	hosts := func() []string {
		cfg, _ := deps.current()
		if cfg == nil {
			return nil
		}
		return cfg.Server.AllowedHosts
	}
	return RecoverPanic(deps.Logger,
		SecurityHeaders(
			HostCheck(hosts,
				AccessLog(deps.Logger,
					BrowserGuard(hosts, mux)))))
}

// NewServer builds the HTTP server the binary runs, with the documented timeouts.
func NewServer(deps Deps) *http.Server {
	return &http.Server{
		Addr:              deps.listen(),
		Handler:           deps.Handler(),
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
		MaxHeaderBytes:    maxHeaderBytes,
	}
}

// Serve listens until ctx is cancelled, then drains for up to five seconds. A failure to bind is
// returned as it happens, so a bad listen address is reported instead of retried.
func Serve(ctx context.Context, deps Deps) error {
	server := NewServer(deps)
	failed := make(chan error, 1)
	go func() {
		failed <- server.ListenAndServe()
	}()

	select {
	case err := <-failed:
		return err
	case <-ctx.Done():
	}

	// A drain cannot inherit a context that was just cancelled, so it gets its own deadline.
	drain, cancel := context.WithTimeout(context.Background(), shutdownDrain)
	defer cancel()
	//nolint:contextcheck // see above: the drain deliberately starts from a fresh context
	return server.Shutdown(drain)
}

// healthz answers the container's healthcheck. It says only that the process can serve: no probing,
// no reload state, cheap enough to run every few seconds.
func healthz(deps Deps) http.Handler {
	body := struct {
		Status  string `json:"status"`
		Version string `json:"version"`
	}{Status: "ok", Version: deps.Version}

	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, http.StatusOK, body)
	})
}

// writeJSON writes value as the response, so every JSON answer in the gateway has one content type
// and one encoder.
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	// A write error here means the client is gone; there is nobody left to report it to.
	_ = json.NewEncoder(w).Encode(value)
}

// writeError writes the JSON error shape every client parses.
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, struct {
		Error string `json:"error"`
	}{Error: message})
}

// isAPIPath reports whether a path belongs to the API, which is answered with JSON and never with the
// app shell.
func isAPIPath(path string) bool {
	return path == "/api" || strings.HasPrefix(path, "/api/")
}

// current reads the configuration being served, tolerating a gateway that has never loaded one.
func (deps Deps) current() (*config.Config, LoadStatus) {
	if deps.Current == nil {
		return nil, LoadStatus{}
	}
	return deps.Current()
}

// now is the clock the response is stamped with.
func (deps Deps) now() time.Time {
	if deps.Now == nil {
		return time.Now()
	}
	return deps.Now()
}

// control reports whether this box has a control plane, and why not when it does not. It is asked
// per request, like everything else, so mounting the directories and reloading is enough.
func (deps Deps) control() (available bool, reason string) {
	if deps.Control == nil {
		return false, "control plane not installed"
	}
	return deps.Control.Available()
}

func (deps Deps) listen() string {
	cfg, _ := deps.current()
	if cfg == nil {
		return ""
	}
	return cfg.Listen
}

// discardLogger is what a nil logger means: nobody asked to see the log.
func discardLogger() *log.Logger {
	return log.New(io.Discard, "", 0)
}
