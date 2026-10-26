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

// Deps is everything the gateway serves from. Config is the only field a working gateway needs; the
// rest have a sensible zero value so a test can replace just the part it is exercising.
type Deps struct {
	// Config is the configuration currently being served.
	Config *config.Config
	// Logger receives the access log and recovered panics. A nil logger stays silent.
	Logger *log.Logger
	// Version is the build version, reported by /healthz.
	Version string
	// ConfigDir is the directory holding config.yaml; its icons/ subdirectory is served. An empty
	// directory means no icon resolves.
	ConfigDir string
	// Static is the built app. A nil Static serves web.FS(), the app embedded in this binary.
	Static fs.FS
}

// Route is one entry of the route table: a Go 1.22+ pattern and the handler that answers it.
type Route struct {
	Pattern string
	Handler http.Handler
}

// Routes is the whole route table as data, so the invariants test can read it and prove the launcher
// offers nothing but GET. T15 adds the status endpoint here.
func Routes(deps Deps) []Route {
	return []Route{
		{Pattern: "GET /healthz", Handler: healthz(deps)},
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
	allowed := deps.allowedHosts()
	return RecoverPanic(deps.Logger,
		SecurityHeaders(
			HostCheck(allowed,
				AccessLog(deps.Logger,
					BrowserGuard(allowed, mux)))))
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

func (deps Deps) allowedHosts() []string {
	if deps.Config == nil {
		return nil
	}
	return deps.Config.Server.AllowedHosts
}

func (deps Deps) listen() string {
	if deps.Config == nil {
		return ""
	}
	return deps.Config.Listen
}

// discardLogger is what a nil logger means: nobody asked to see the log.
func discardLogger() *log.Logger {
	return log.New(io.Discard, "", 0)
}
