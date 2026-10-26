package gateway_test

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"
	"unicode/utf8"

	"github.com/autumnsgrove/meridian/internal/config"
	"github.com/autumnsgrove/meridian/internal/gateway"
)

const testConfig = `
listen: 127.0.0.1:8891
server:
  allowed_hosts: [hub.example.org, withport.example.org:8891]
sections:
  - {id: services, title: Services}
poll:
  interval: 15s
  idle_interval: 2m
  idle_after: 10m
  timeout: 4s
  refresh_min: 5s
search:
  buttons:
    - label: Search
      url_template: https://search.example.org/?q={q}
services:
  - id: one
    name: One
    section: services
    icon: activity
    url: https://one.example.org
    probes:
      health: {kind: self}
`

// syncBuffer collects log lines written while a request is being served.
type syncBuffer struct {
	mu      sync.Mutex
	builder strings.Builder
}

func (buffer *syncBuffer) Write(chunk []byte) (int, error) {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	return buffer.builder.Write(chunk)
}

func (buffer *syncBuffer) String() string {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	return buffer.builder.String()
}

// testNow is the clock every gateway test works at, so no response depends on the wall clock.
var testNow = time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)

// testGateway is a gateway served over a fstest app tree, with a log the test can read and a config
// the test can replace, the way a reload would.
type testGateway struct {
	deps gateway.Deps
	logs *syncBuffer
	site fstest.MapFS
	cfg  *config.Config
	load gateway.LoadStatus
}

// Current is the config seam: it reports whatever the test last stored, which is how a reload is
// simulated without a running server.
func (hub *testGateway) Current() (*config.Config, gateway.LoadStatus) {
	return hub.cfg, hub.load
}

func newTestGateway(t *testing.T) *testGateway {
	t.Helper()
	parsed, warnings, err := config.Parse([]byte(testConfig))
	if err != nil {
		t.Fatalf("parse the test config: %v", err)
	}
	if len(warnings) > 0 {
		t.Fatalf("the test config should not warn: %v", warnings)
	}

	logs := &syncBuffer{}
	site := fstest.MapFS{
		"index.html":            {Data: []byte("<!doctype html><title>launcher</title>")},
		"_app/immutable/app.js": {Data: []byte("console.log('app')")},
	}
	hub := &testGateway{
		logs: logs,
		site: site,
		cfg:  parsed,
		load: gateway.LoadStatus{LoadedAt: testNow},
	}
	hub.deps = gateway.Deps{
		Current: hub.Current,
		Logger:  log.New(logs, "", 0),
		Version: "test-version",
		Static:  site,
	}
	return hub
}

// request drives the assembled handler, which is what a browser sees.
func (hub *testGateway) request(t *testing.T, method, target string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	body := io.Reader(nil)
	if method != http.MethodGet && method != http.MethodHead {
		body = io.Reader(bytes.NewReader([]byte(`{}`)))
	}
	request := httptest.NewRequestWithContext(t.Context(), method, target, body)
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	recorder := httptest.NewRecorder()
	hub.deps.Handler().ServeHTTP(recorder, request)
	return recorder
}

// hostRequest sends a GET with an explicit Host header, which is how the allowed-host check is reached.
func (hub *testGateway) hostRequest(t *testing.T, host, target string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, target, nil)
	request.Host = host
	recorder := httptest.NewRecorder()
	hub.deps.Handler().ServeHTTP(recorder, request)
	return recorder
}

func TestHandler_AnswersTheHealthCheck(t *testing.T) {
	gw := newTestGateway(t)
	response := gw.request(t, http.MethodGet, "http://hub.example.org/healthz", nil)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	var body struct {
		Status  string `json:"status"`
		Version string `json:"version"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("healthz body %q is not JSON: %v", response.Body.String(), err)
	}
	if body.Status != "ok" || body.Version != "test-version" {
		t.Fatalf("healthz = %+v, want ok and the build version", body)
	}
}

func TestHandler_RejectsAHostItDoesNotAnswerTo(t *testing.T) {
	gw := newTestGateway(t)
	tests := []struct {
		name string
		host string
		want int
	}{
		{name: "a loopback name", host: "localhost:8891", want: http.StatusOK},
		{name: "loopback without a port", host: "localhost", want: http.StatusOK},
		{name: "IPv4 loopback", host: "127.0.0.1:8891", want: http.StatusOK},
		{name: "IPv6 loopback", host: "[::1]:8891", want: http.StatusOK},
		{name: "an allowed name", host: "hub.example.org", want: http.StatusOK},
		{name: "an allowed name on any port", host: "hub.example.org:9999", want: http.StatusOK},
		{name: "an allowed name and port", host: "withport.example.org:8891", want: http.StatusOK},
		{name: "an allowed name on another port", host: "withport.example.org:9000", want: http.StatusMisdirectedRequest},
		{name: "a name that is not allowed", host: "evil.example.org", want: http.StatusMisdirectedRequest},
		{name: "a suffix that only looks allowed", host: "nothub.example.org", want: http.StatusMisdirectedRequest},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := gw.hostRequest(t, test.host, "/healthz")
			if response.Code != test.want {
				t.Fatalf("Host %q = %d, want %d", test.host, response.Code, test.want)
			}
		})
	}
}

func TestHandler_SetsTheSecurityHeadersOnEveryResponse(t *testing.T) {
	gw := newTestGateway(t)
	tests := []struct {
		name   string
		target string
		host   string
	}{
		{name: "the health check", target: "/healthz", host: "hub.example.org"},
		{name: "an API 404", target: "/api/nope", host: "hub.example.org"},
		{name: "a page", target: "/control", host: "hub.example.org"},
		{name: "a rejected host", target: "/healthz", host: "evil.example.org"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := gw.hostRequest(t, test.host, test.target)
			header := response.Header()
			if got := header.Get("X-Content-Type-Options"); got != "nosniff" {
				t.Errorf("X-Content-Type-Options = %q, want nosniff", got)
			}
			if got := header.Get("Referrer-Policy"); got != "no-referrer" {
				t.Errorf("Referrer-Policy = %q, want no-referrer", got)
			}
			if got := header.Get("X-Frame-Options"); got != "DENY" {
				t.Errorf("X-Frame-Options = %q, want DENY", got)
			}
			if strings.HasPrefix(test.target, "/api/") {
				if got := header.Get("Cache-Control"); got != "no-store" {
					t.Errorf("Cache-Control = %q for an API response, want no-store", got)
				}
			}
		})
	}
}

func TestHandler_ServesTheAppForUnknownPagesAnd404sTheApi(t *testing.T) {
	gw := newTestGateway(t)

	page := gw.hostRequest(t, "hub.example.org", "/control")
	if page.Code != http.StatusOK || page.Body.String() != "<!doctype html><title>launcher</title>" {
		t.Fatalf("an app route = %d %q, want the app shell", page.Code, page.Body.String())
	}
	if got := page.Header().Get("Cache-Control"); got != "no-cache" {
		t.Fatalf("the app shell Cache-Control = %q, want no-cache", got)
	}

	missing := gw.hostRequest(t, "hub.example.org", "/api/nope")
	if missing.Code != http.StatusNotFound {
		t.Fatalf("an unknown API path = %d, want 404", missing.Code)
	}
	if contentType := missing.Header().Get("Content-Type"); contentType != "application/json" {
		t.Fatalf("an unknown API path Content-Type = %q, want application/json", contentType)
	}
}

func TestHandler_ServesImmutableAssetsWithALongCache(t *testing.T) {
	gw := newTestGateway(t)
	response := gw.hostRequest(t, "hub.example.org", "/_app/immutable/app.js")

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}
	if got := response.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Fatalf("Cache-Control = %q, want the immutable value", got)
	}
}

func TestBrowserGuard_RefusesWhatABrowserPageShouldNotSend(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		_, _ = io.Copy(io.Discard, request.Body)
		w.WriteHeader(http.StatusOK)
	})
	guard := gateway.BrowserGuard(func() []string { return []string{"hub.example.org"} }, next)

	tests := []struct {
		name    string
		method  string
		headers map[string]string
		want    int
	}{
		{
			name:    "a same-origin JSON request",
			method:  http.MethodPost,
			headers: map[string]string{"Content-Type": "application/json", "Sec-Fetch-Site": "same-origin"},
			want:    http.StatusOK,
		},
		{
			name:    "a request with no fetch metadata at all",
			method:  http.MethodPost,
			headers: map[string]string{"Content-Type": "application/json"},
			want:    http.StatusOK,
		},
		{
			name:    "a request the browser marks as none",
			method:  http.MethodPost,
			headers: map[string]string{"Content-Type": "application/json", "Sec-Fetch-Site": "none"},
			want:    http.StatusOK,
		},
		{
			name:    "a cross-site request",
			method:  http.MethodPost,
			headers: map[string]string{"Content-Type": "application/json", "Sec-Fetch-Site": "cross-site"},
			want:    http.StatusForbidden,
		},
		{
			name:    "a same-site request from another origin",
			method:  http.MethodPost,
			headers: map[string]string{"Content-Type": "application/json", "Sec-Fetch-Site": "same-site"},
			want:    http.StatusForbidden,
		},
		{
			name:    "a form submission",
			method:  http.MethodPost,
			headers: map[string]string{"Content-Type": "application/x-www-form-urlencoded"},
			want:    http.StatusForbidden,
		},
		{
			name:    "no content type",
			method:  http.MethodPost,
			headers: nil,
			want:    http.StatusForbidden,
		},
		{
			name:    "a content type with extra parameters",
			method:  http.MethodPost,
			headers: map[string]string{"Content-Type": "application/json; charset=utf-8"},
			want:    http.StatusForbidden,
		},
		{
			name:    "an allowed origin",
			method:  http.MethodPost,
			headers: map[string]string{"Content-Type": "application/json", "Origin": "https://hub.example.org"},
			want:    http.StatusOK,
		},
		{
			name:    "an origin on another port than the one configured",
			method:  http.MethodPost,
			headers: map[string]string{"Content-Type": "application/json", "Origin": "https://withport.example.org:9000"},
			want:    http.StatusForbidden,
		},
		{
			name:    "an origin that is not allowed",
			method:  http.MethodPost,
			headers: map[string]string{"Content-Type": "application/json", "Origin": "https://evil.example.org"},
			want:    http.StatusForbidden,
		},
		{
			name:    "an origin that is not a URL",
			method:  http.MethodPost,
			headers: map[string]string{"Content-Type": "application/json", "Origin": "not a url"},
			want:    http.StatusForbidden,
		},
		{
			name:    "a GET with no headers at all passes through",
			method:  http.MethodGet,
			headers: nil,
			want:    http.StatusOK,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequestWithContext(t.Context(), test.method, "http://hub.example.org/api/control", strings.NewReader("{}"))
			request.Header.Del("Content-Type")
			for name, value := range test.headers {
				request.Header.Set(name, value)
			}
			recorder := httptest.NewRecorder()
			guard.ServeHTTP(recorder, request)
			if recorder.Code != test.want {
				t.Fatalf("status = %d, want %d (body %q)", recorder.Code, test.want, recorder.Body.String())
			}
		})
	}
}

func TestBrowserGuard_RefusesAnOversizedBody(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if _, err := io.Copy(io.Discard, request.Body); err != nil {
			w.WriteHeader(http.StatusRequestEntityTooLarge)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	guard := gateway.BrowserGuard(func() []string { return nil }, next)

	request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "http://localhost/api/control", strings.NewReader(strings.Repeat("x", 8<<10+1)))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	guard.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413 for a body over 8 KiB", recorder.Code)
	}
}

func TestRecoverPanic_AnswersWithoutLeakingThePanic(t *testing.T) {
	var logs syncBuffer
	guard := gateway.RecoverPanic(log.New(&logs, "", 0), http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("the database password is hunter2")
	}))

	recorder := httptest.NewRecorder()
	guard.ServeHTTP(recorder, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "http://localhost/", nil))

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", recorder.Code)
	}
	if body := recorder.Body.String(); strings.Contains(body, "hunter2") {
		t.Fatalf("the response leaked the panic: %q", body)
	}
	// The log is the only place a panic becomes debuggable: it keeps the value and the stack, since
	// only the container's owner can read it. The response above is what must stay silent.
	if !strings.Contains(logs.String(), "panic") || !strings.Contains(logs.String(), "goroutine") {
		t.Fatalf("the panic log %q does not record the panic and its stack", logs.String())
	}
}

func TestAccessLog_RecordsTheRequestWithoutItsQuery(t *testing.T) {
	gw := newTestGateway(t)
	if response := gw.hostRequest(t, "hub.example.org", "/api/nope?secret=1"); response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", response.Code)
	}

	line := gw.logs.String()
	for _, want := range []string{"GET", "/api/nope", "404"} {
		if !strings.Contains(line, want) {
			t.Fatalf("the access log %q does not contain %q", line, want)
		}
	}
	if strings.Contains(line, "secret=1") {
		t.Fatalf("the access log leaked the query string: %q", line)
	}
	if lines := bufio.NewScanner(strings.NewReader(line)); lines.Scan() && strings.Count(line, "\n") > 1 {
		t.Fatalf("one request wrote more than one log line: %q", line)
	}
}

func TestNewServer_UsesTheDocumentedTimeouts(t *testing.T) {
	gw := newTestGateway(t)
	server := gateway.NewServer(gw.deps)

	if server.Addr != "127.0.0.1:8891" {
		t.Errorf("Addr = %q, want the configured listen address", server.Addr)
	}
	if server.ReadHeaderTimeout != 5*time.Second || server.ReadTimeout != 10*time.Second ||
		server.WriteTimeout != 15*time.Second || server.IdleTimeout != 60*time.Second {
		t.Errorf("timeouts = %v/%v/%v/%v, want 5s/10s/15s/60s",
			server.ReadHeaderTimeout, server.ReadTimeout, server.WriteTimeout, server.IdleTimeout)
	}
	if server.MaxHeaderBytes != 16<<10 {
		t.Errorf("MaxHeaderBytes = %d, want 16 KiB", server.MaxHeaderBytes)
	}
	if server.Handler == nil {
		t.Error("the server has no handler")
	}
}

func TestRoutes_AreAllReadOnlyExceptControl(t *testing.T) {
	gw := newTestGateway(t)
	routes := gateway.Routes(gw.deps)
	if len(routes) == 0 {
		t.Fatal("the gateway has no routes")
	}
	// Decision #34: the launcher serves nothing mutating, and the control plane has exactly one
	// route that is (the invariants test enforces the same rule on the literals).
	const mutatingRoute = "POST /api/control"
	for _, route := range routes {
		if route.Handler == nil {
			t.Errorf("route %q has no handler", route.Pattern)
		}
		if !strings.HasPrefix(route.Pattern, "GET ") && route.Pattern != "/" && route.Pattern != mutatingRoute {
			t.Errorf("route %q is not a GET route; %q is the only mutating route", route.Pattern, mutatingRoute)
		}
	}
}

func TestServe_ReportsAStartedServerAsAnError(t *testing.T) {
	gw := newTestGateway(t)
	// Something else already holds the port, so Serve must return the listen error instead of blocking.
	taken := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer taken.Close()
	gw.cfg.Listen = strings.TrimPrefix(taken.URL, "http://")

	err := gateway.Serve(t.Context(), gw.deps)
	if err == nil {
		t.Fatal("Serve returned no error although the address was taken")
	}
	if !strings.Contains(err.Error(), "address already in use") {
		t.Fatalf("Serve returned %v, want the listen error", err)
	}
}

func TestHandlersRespondWithinTheWriteTimeout(t *testing.T) {
	// Guard against a handler that blocks without a deadline: every route must answer at once.
	gw := newTestGateway(t)
	paths := []string{"/healthz", "/", "/control", "/_app/immutable/app.js", "/api/nope", "/icons/none.png"}
	for _, path := range paths {
		started := time.Now()
		gw.hostRequest(t, "hub.example.org", path)
		if elapsed := time.Since(started); elapsed > time.Second {
			t.Errorf("%s took %v", path, elapsed)
		}
	}
}

func TestRawJSONErrorShape(t *testing.T) {
	// The 404 for an unknown API path is the shape every JSON error uses, so a client can parse one.
	gw := newTestGateway(t)
	response := gw.hostRequest(t, "hub.example.org", "/api/nope")
	var body struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("error body %q is not JSON: %v", response.Body.String(), err)
	}
	if body.Error == "" {
		t.Fatal("the error body has no message")
	}
}

func TestHostAllowed(t *testing.T) {
	allowed := []string{"hub.example.org", "withport.example.org:8891"}
	tests := []struct {
		name string
		host string
		want bool
	}{
		{name: "the backup name in the design", host: "localhost:8891", want: true},
		{name: "a loopback name with no port", host: "localhost", want: true},
		{name: "IPv4 loopback", host: "127.0.0.1:8891", want: true},
		{name: "IPv6 loopback", host: "[::1]:8891", want: true},
		{name: "an allowed name", host: "hub.example.org", want: true},
		{name: "an allowed name on any port", host: "hub.example.org:1", want: true},
		{name: "an allowed name and port", host: "withport.example.org:8891", want: true},
		{name: "an allowed name on the wrong port", host: "withport.example.org:8892", want: false},
		{name: "another name", host: "evil.example.org", want: false},
		{name: "a suffix of an allowed name", host: "xhub.example.org", want: false},
		{name: "an empty host", host: "", want: false},
		{name: "a host with a trailing dot", host: "hub.example.org.", want: false},
		{name: "an uppercase loopback name", host: "LOCALHOST:8891", want: true},
		{name: "an IPv6 loopback with a bare port", host: "[::1]:]", want: true},
		{name: "a bracket around something that is not an address", host: "[0]:]", want: false},
		{name: "a bracketed name", host: "[hub.example.org]", want: false},
		{name: "a bracket that never closes", host: "[::1", want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := gateway.HostAllowed(test.host, allowed); got != test.want {
				t.Fatalf("HostAllowed(%q) = %v, want %v", test.host, got, test.want)
			}
		})
	}
}

func TestDepsHandler_ToleratesAMissingLogAndVersion(t *testing.T) {
	// A zero Deps is not a working gateway, but the logger and version must never be the reason for a
	// panic: the config is the only required field.
	gw := newTestGateway(t)
	gw.deps.Logger = nil
	response := gw.request(t, http.MethodGet, "http://hub.example.org/healthz", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}
}

// splitEntries turns a fuzz corpus string into an allowlist, deterministically.
func splitEntries(entries string) []string {
	fields := strings.FieldsFunc(entries, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\n' || r == ';' || r == '\t'
	})
	if len(fields) > 8 {
		return fields[:8]
	}
	return fields
}

func FuzzHostCheck(f *testing.F) {
	f.Add("localhost:8891", "hub.example.org")
	f.Add("evil.example.org", "hub.example.org,withport.example.org:8891")
	f.Add("[::1]:80", "")
	f.Add("hub.example.org:21", "hub.example.org:8891")

	f.Fuzz(func(t *testing.T, host, entries string) {
		allowed := splitEntries(entries)
		accepted := gateway.HostAllowed(host, allowed)

		if referenceLoopback(host) && !accepted {
			t.Fatalf("HostAllowed(%q, %q) refused a loopback name", host, allowed)
		}
		if !accepted {
			return
		}
		if referenceLoopback(host) {
			return
		}
		// Nothing outside the allowlist may be accepted: the host must be an entry exactly, or an
		// entry without a port with the request's name.
		name, hasPort := referenceSplit(host)
		for _, entry := range allowed {
			if host == entry {
				return
			}
			entryName, entryHasPort := referenceSplit(entry)
			if !entryHasPort && !hasPort && name == entryName {
				return
			}
			if !entryHasPort && hasPort && name == entryName {
				return
			}
		}
		t.Fatalf("HostAllowed(%q, %q) accepted a host no entry justifies", host, allowed)
	})
}

// referenceLoopback is a deliberately dumb second opinion on the three spellings a loopback Host can
// use, each with an optional port. A malformed header is not a host at all, so it is not loopback:
// validity is the one thing both sides agree on by construction.
func referenceLoopback(host string) bool {
	if !utf8.ValidString(host) {
		return false
	}
	lowered := strings.ToLower(host)
	for _, name := range []string{"localhost", "127.0.0.1"} {
		if lowered == name {
			return true
		}
		// A bare name takes one colon and then a port; a second colon is not a host at all.
		if port, ok := strings.CutPrefix(lowered, name+":"); ok && !strings.Contains(port, ":") {
			return true
		}
	}
	if lowered == "[::1]" || strings.HasPrefix(lowered, "[::1]:") {
		return strings.Index(lowered, "]") == len("[::1]")-1
	}
	return false
}

// referenceSplit separates a Host header into its name and whether it named a port.
func referenceSplit(host string) (name string, hasPort bool) {
	lowered := strings.ToLower(host)
	if strings.HasPrefix(lowered, "[") {
		end := strings.LastIndex(lowered, "]")
		if end < 0 {
			return lowered, false
		}
		rest := lowered[end+1:]
		if rest != "" && !strings.HasPrefix(rest, ":") {
			// Not a host header at all, so it is its own name and matches nothing.
			return lowered, false
		}
		return lowered[1:end], strings.HasPrefix(rest, ":")
	}
	if strings.Count(lowered, ":") > 1 {
		return lowered, false
	}
	if index := strings.LastIndex(lowered, ":"); index >= 0 {
		return lowered[:index], true
	}
	return lowered, false
}

func TestHandler_ReadsTheConfigOnEveryRequest(t *testing.T) {
	hub := newTestGateway(t)
	handler := hub.deps.Handler()

	serve := func(host string) int {
		request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/healthz", nil)
		request.Host = host
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		return recorder.Code
	}

	if got := serve("hub.example.org"); got != http.StatusOK {
		t.Fatalf("the configured host = %d before the reload, want 200", got)
	}

	reloaded, _, err := config.Parse([]byte(strings.Replace(testConfig, "allowed_hosts: [hub.example.org, withport.example.org:8891]",
		"allowed_hosts: [new.example.org]", 1)))
	if err != nil {
		t.Fatalf("parse the reloaded config: %v", err)
	}
	hub.cfg = reloaded

	// The same handler, a new config: a reload does not need a restart or a rebuilt mux.
	if got := serve("hub.example.org"); got != http.StatusMisdirectedRequest {
		t.Fatalf("the old host = %d after the reload, want 421", got)
	}
	if got := serve("new.example.org"); got != http.StatusOK {
		t.Fatalf("the new host = %d after the reload, want 200", got)
	}
}
