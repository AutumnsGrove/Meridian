package probe_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/autumnsgrove/meridian/internal/config"
	"github.com/autumnsgrove/meridian/internal/probe"
	"github.com/autumnsgrove/meridian/internal/wire"
)

// keys turns field names into the Set a probe built in code needs, the way config fills it from YAML.
func keys(names ...string) map[string]struct{} {
	set := make(map[string]struct{}, len(names))
	for _, name := range names {
		set[name] = struct{}{}
	}
	return set
}

func fixedClock(instant time.Time) func() time.Time {
	return func() time.Time { return instant }
}

// serveBody starts a server that answers every request with body and status.
func serveBody(t *testing.T, status int, body string) string {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	return server.URL
}

func httpEnv() probe.Env {
	return probe.Env{Fetch: newFetcher()}
}

func TestHTTPStatus_MatchesTheExpectedAnswer(t *testing.T) {
	tests := []struct {
		name   string
		status int
		probe  config.Probe
		want   probe.Health
	}{
		{
			name:   "2xx by default",
			status: http.StatusOK,
			want:   probe.Health{Status: probe.StatusOK},
		},
		{
			name:   "5xx by default",
			status: http.StatusInternalServerError,
			want:   probe.Health{Status: probe.StatusDown, Code: probe.CodeBadStatus},
		},
		{
			name:   "any code in expect",
			status: http.StatusTeapot,
			probe:  config.Probe{Expect: []int{200, 418}},
			want:   probe.Health{Status: probe.StatusOK},
		},
		{
			name:   "a code left out of expect",
			status: http.StatusNotFound,
			probe:  config.Probe{Expect: []int{200, 418}},
			want:   probe.Health{Status: probe.StatusDown, Code: probe.CodeBadStatus},
		},
		{
			name:   "a redirect class",
			status: http.StatusFound,
			probe:  config.Probe{ExpectClass: "3xx"},
			want:   probe.Health{Status: probe.StatusOK},
		},
		{
			name:   "another class",
			status: http.StatusOK,
			probe:  config.Probe{ExpectClass: "3xx"},
			want:   probe.Health{Status: probe.StatusDown, Code: probe.CodeBadStatus},
		},
		{
			name:   "a class written in capitals",
			status: http.StatusNotFound,
			probe:  config.Probe{ExpectClass: "4XX"},
			want:   probe.Health{Status: probe.StatusOK},
		},
		{
			name:   "a class that is not a class",
			status: http.StatusOK,
			probe:  config.Probe{ExpectClass: "banana"},
			want:   probe.Health{Status: probe.StatusDown, Code: probe.CodeBadStatus},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.probe.Kind = "http_status"
			test.probe.URL = serveBody(t, test.status, "")
			got := probe.HealthRunners["http_status"](context.Background(), httpEnv(), test.probe)
			if got != test.want {
				t.Errorf("health = %+v, want %+v", got, test.want)
			}
		})
	}
}

// TestHTTPStatus_RedirectToLoginIsHealthy is the AdGuard case in INTEGRATIONS.md: a 302 to a login
// page is a working service, not a broken one.
func TestHTTPStatus_RedirectToLoginIsHealthy(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/login.html", http.StatusFound)
	}))
	defer server.Close()

	got := probe.HealthRunners["http_status"](context.Background(), httpEnv(), config.Probe{
		Kind:   "http_status",
		URL:    server.URL,
		Expect: []int{200, 302},
	})
	if got.Status != probe.StatusOK {
		t.Errorf("health = %+v, want ok: a login redirect is a healthy answer", got)
	}
}

// TestHTTPStatus_HostCheckAndAuthTogether is the Ivy scenario: the configured URL says localhost, the
// service refuses any other Host header, and the probe still succeeds.
func TestHTTPStatus_HostCheckAndAuthTogether(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Host, "localhost:") {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	port := server.URL[strings.LastIndex(server.URL, ":")+1:]
	got := probe.HealthRunners["http_status"](context.Background(), httpEnv(), config.Probe{
		Kind: "http_status",
		URL:  "http://localhost:" + port + "/api/v1/health",
	})
	if got.Status != probe.StatusOK {
		t.Errorf("health = %+v, want ok: the probe must send the configured Host", got)
	}
}

func TestHTTPStatus_NetworkFailuresAreDown(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := server.URL
	server.Close()

	got := probe.HealthRunners["http_status"](context.Background(), httpEnv(),
		config.Probe{Kind: "http_status", URL: url})
	if got.Status != probe.StatusDown || got.Code != probe.CodeRefused {
		t.Errorf("health = %+v, want down/refused", got)
	}
}

func TestHTTPStatus_MissingCredentialIsUnknown(t *testing.T) {
	got := probe.HealthRunners["http_status"](context.Background(), probe.Env{Fetch: newFetcher()}, config.Probe{
		Kind: "http_status",
		URL:  "http://127.0.0.1:1/",
		Auth: &config.Auth{BearerEnv: "MERIDIAN_TEST_TOKEN"},
	})
	if got.Status != probe.StatusUnknown || got.Code != probe.CodeEnvMissing {
		t.Errorf("health = %+v, want unknown/env_missing: we could not ask, so we do not claim down", got)
	}
}

func TestHTTPStatus_WithoutAFetcherIsUnknown(t *testing.T) {
	got := probe.HealthRunners["http_status"](context.Background(), probe.Env{}, config.Probe{
		Kind: "http_status",
		URL:  "http://127.0.0.1:1/",
	})
	if got.Status != probe.StatusUnknown || got.Code != probe.CodeBadValue {
		t.Errorf("health = %+v, want unknown/bad_value", got)
	}
}

func TestHTTPJSON_MatchesAValueTypeAware(t *testing.T) {
	tests := []struct {
		name  string
		body  string
		probe config.Probe
		want  probe.Health
	}{
		{
			name:  "equals a string",
			body:  `{"status":"ok"}`,
			probe: config.Probe{Path: "status", Equals: "ok", Set: keys("equals")},
			want:  probe.Health{Status: probe.StatusOK},
		},
		{
			name:  "equals does not coerce",
			body:  `{"status":true}`,
			probe: config.Probe{Path: "status", Equals: "true", Set: keys("equals")},
			want:  probe.Health{Status: probe.StatusDown, Code: probe.CodeUnexpectedValue},
		},
		{
			name:  "equals a number through a YAML int",
			body:  `{"unread":2}`,
			probe: config.Probe{Path: "unread", Equals: 2, Set: keys("equals")},
			want:  probe.Health{Status: probe.StatusOK},
		},
		{
			name:  "equals a boolean",
			body:  `{"online":false}`,
			probe: config.Probe{Path: "online", Equals: false, Set: keys("equals")},
			want:  probe.Health{Status: probe.StatusOK},
		},
		{
			name:  "a nested path with an index",
			body:  `{"items":[{"id":"a"},{"id":"b"}]}`,
			probe: config.Probe{Path: "items.1.id", Equals: "b", Set: keys("equals")},
			want:  probe.Health{Status: probe.StatusOK},
		},
		{
			name:  "one_of the first",
			body:  `{"state":"ok"}`,
			probe: config.Probe{Path: "state", OneOf: []any{"ok", "degraded"}, Set: keys("one_of")},
			want:  probe.Health{Status: probe.StatusOK},
		},
		{
			name:  "one_of the second",
			body:  `{"state":"degraded"}`,
			probe: config.Probe{Path: "state", OneOf: []any{"ok", "degraded"}, Set: keys("one_of")},
			want:  probe.Health{Status: probe.StatusOK},
		},
		{
			name:  "one_of none",
			body:  `{"state":"down"}`,
			probe: config.Probe{Path: "state", OneOf: []any{"ok", "degraded"}, Set: keys("one_of")},
			want:  probe.Health{Status: probe.StatusDown, Code: probe.CodeUnexpectedValue},
		},
		{
			name:  "exists and is there",
			body:  `{"version":"r1"}`,
			probe: config.Probe{Path: "version", Exists: boolPointer(true), Set: keys("exists")},
			want:  probe.Health{Status: probe.StatusOK},
		},
		{
			name:  "exists and is missing",
			body:  `{"version":"r1"}`,
			probe: config.Probe{Path: "build", Exists: boolPointer(true), Set: keys("exists")},
			want:  probe.Health{Status: probe.StatusDown, Code: probe.CodeMissingField},
		},
		{
			name:  "must not exist and does not",
			body:  `{"status":"ok"}`,
			probe: config.Probe{Path: "error", Exists: boolPointer(false), Set: keys("exists")},
			want:  probe.Health{Status: probe.StatusOK},
		},
		{
			name:  "must not exist and does",
			body:  `{"error":"boom"}`,
			probe: config.Probe{Path: "error", Exists: boolPointer(false), Set: keys("exists")},
			want:  probe.Health{Status: probe.StatusDown, Code: probe.CodeUnexpectedValue},
		},
		{
			name:  "a missing nested field",
			body:  `{"databases":{"mirror":"ok"}}`,
			probe: config.Probe{Path: "databases.state", Equals: "ok", Set: keys("equals")},
			want:  probe.Health{Status: probe.StatusDown, Code: probe.CodeMissingField},
		},
		{
			name:  "an index past the end",
			body:  `{"items":[]}`,
			probe: config.Probe{Path: "items.0.id", Equals: "a", Set: keys("equals")},
			want:  probe.Health{Status: probe.StatusDown, Code: probe.CodeMissingField},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.probe.Kind = "http_json"
			test.probe.URL = serveBody(t, http.StatusOK, test.body)
			got := probe.HealthRunners["http_json"](context.Background(), httpEnv(), test.probe)
			if got != test.want {
				t.Errorf("health = %+v, want %+v", got, test.want)
			}
		})
	}
}

func TestHTTPJSON_BadBodyIsDown(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "not JSON", body: "<html>login</html>"},
		{name: "empty", body: ""},
		{name: "truncated", body: `{"status":`},
		{name: "trailing data", body: `{"status":"ok"}{"status":"ok"}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := probe.HealthRunners["http_json"](context.Background(), httpEnv(), config.Probe{
				Kind:   "http_json",
				URL:    serveBody(t, http.StatusOK, test.body),
				Path:   "status",
				Equals: "ok",
				Set:    keys("equals"),
			})
			if got.Status != probe.StatusDown || got.Code != probe.CodeBadJSON {
				t.Errorf("health = %+v, want down/bad_json", got)
			}
		})
	}
}

func TestHTTPJSON_ABodyOverTheLimitIsDown(t *testing.T) {
	got := probe.HealthRunners["http_json"](context.Background(),
		probe.Env{Fetch: probe.NewHTTPFetcher(nil, "meridian-test", 8)},
		config.Probe{
			Kind:   "http_json",
			URL:    serveBody(t, http.StatusOK, `{"status":"ok","padding":"xxxxxxxxxxxxxxxx"}`),
			Path:   "status",
			Equals: "ok",
			Set:    keys("equals"),
		})
	if got.Status != probe.StatusDown || got.Code != probe.CodeBodyTooLarge {
		t.Errorf("health = %+v, want down/body_too_large", got)
	}
}

func TestDNS_ComparesTheResponseCode(t *testing.T) {
	tests := []struct {
		name  string
		rcode string
		err   error
		probe config.Probe
		want  probe.Health
	}{
		{
			name:  "the default is NOERROR",
			rcode: "NOERROR",
			want:  probe.Health{Status: probe.StatusOK},
		},
		{
			name:  "another expected code",
			rcode: "NXDOMAIN",
			probe: config.Probe{ExpectRcode: "NXDOMAIN"},
			want:  probe.Health{Status: probe.StatusOK},
		},
		{
			name:  "an unexpected code",
			rcode: "SERVFAIL",
			want:  probe.Health{Status: probe.StatusDown, Code: probe.CodeDNSRcode},
		},
		{
			name: "a timeout",
			err:  probe.ErrTimeout,
			want: probe.Health{Status: probe.StatusDown, Code: probe.CodeTimeout},
		},
		{
			name: "an unreachable server",
			err:  probe.ErrUnreachable,
			want: probe.Health{Status: probe.StatusDown, Code: probe.CodeUnreachable},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.probe.Kind = "dns"
			test.probe.Addr = "127.0.0.1:5335"
			test.probe.Qname = "example.com."
			env := probe.Env{Resolve: func(context.Context, string, string) (string, error) {
				return test.rcode, test.err
			}}
			got := probe.HealthRunners["dns"](context.Background(), env, test.probe)
			if got != test.want {
				t.Errorf("health = %+v, want %+v", got, test.want)
			}
		})
	}
}

func TestDNS_WithoutAResolverIsUnknown(t *testing.T) {
	got := probe.HealthRunners["dns"](context.Background(), probe.Env{}, config.Probe{
		Kind:  "dns",
		Addr:  "127.0.0.1:5335",
		Qname: "example.com.",
	})
	if got.Status != probe.StatusUnknown || got.Code != probe.CodeBadValue {
		t.Errorf("health = %+v, want unknown/bad_value", got)
	}
}

// hostEnv builds an Env with a snapshot taken at fixedNow.
func hostEnv(snapshot wire.HostSnapshot, now time.Time, maxAge time.Duration) probe.Env {
	return probe.Env{
		Host:       func() (wire.HostSnapshot, bool) { return snapshot, true },
		HostMaxAge: maxAge,
		Now:        fixedClock(now),
	}
}

func sampleSnapshot(at time.Time) wire.HostSnapshot {
	return wire.HostSnapshot{
		At: at,
		Host: wire.HostInfo{
			UptimeS: 3600,
			Load1:   0.5,
		},
		Units: map[string]wire.UnitState{
			"mail.service": {Active: true, Since: at.Add(-time.Hour)},
			"dns.service":  {Active: false, Since: at.Add(-time.Hour)},
		},
		Tailscale: &wire.TailscaleState{Online: true, ExitNode: false, PeersOnline: 3, PeersTotal: 4},
	}
}

func TestHostUnit_ReadsTheUnitState(t *testing.T) {
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	env := hostEnv(sampleSnapshot(now), now, time.Minute)

	tests := []struct {
		name string
		unit string
		want probe.Health
	}{
		{name: "active", unit: "mail.service", want: probe.Health{Status: probe.StatusOK}},
		{name: "inactive", unit: "dns.service", want: probe.Health{Status: probe.StatusDown, Code: probe.CodeUnitInactive}},
		{name: "not reported", unit: "other.service", want: probe.Health{Status: probe.StatusUnknown, Code: probe.CodeUnitNotReported}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := probe.HealthRunners["host_unit"](context.Background(), env,
				config.Probe{Kind: "host_unit", Unit: test.unit})
			if got != test.want {
				t.Errorf("health = %+v, want %+v", got, test.want)
			}
		})
	}
}

func TestHostUnit_StaleSnapshotIsNeverGreen(t *testing.T) {
	at := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	now := at.Add(2 * time.Minute)
	env := hostEnv(sampleSnapshot(at), now, time.Minute)

	got := probe.HealthRunners["host_unit"](context.Background(), env,
		config.Probe{Kind: "host_unit", Unit: "mail.service"})
	want := probe.Health{Status: probe.StatusUnknown, Code: probe.CodeHostStale, Stale: true}
	if got != want {
		t.Errorf("health = %+v, want %+v: a stale snapshot must not render as health", got, want)
	}
}

func TestHostUnit_MissingSnapshotIsUnknown(t *testing.T) {
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	env := probe.Env{
		Host:       func() (wire.HostSnapshot, bool) { return wire.HostSnapshot{}, false },
		HostMaxAge: time.Minute,
		Now:        fixedClock(now),
	}
	got := probe.HealthRunners["host_unit"](context.Background(), env,
		config.Probe{Kind: "host_unit", Unit: "mail.service"})
	if got.Status != probe.StatusUnknown || got.Code != probe.CodeHostUnavailable {
		t.Errorf("health = %+v, want unknown/host_unavailable", got)
	}
}

// TestHostProbes_WithoutAClockAreUnknown pins the fail-safe: a runner with no clock cannot judge
// freshness, so it reports unknown instead of claiming the unit is healthy.
func TestHostProbes_WithoutAClockAreUnknown(t *testing.T) {
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	env := probe.Env{
		Host:       func() (wire.HostSnapshot, bool) { return sampleSnapshot(now), true },
		HostMaxAge: time.Minute,
	}
	got := probe.HealthRunners["host_unit"](context.Background(), env,
		config.Probe{Kind: "host_unit", Unit: "mail.service"})
	if got.Status != probe.StatusUnknown || got.Code != probe.CodeHostUnavailable {
		t.Errorf("health = %+v, want unknown/host_unavailable", got)
	}
}

func TestHostField_ReadsABooleanOrAValue(t *testing.T) {
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	env := hostEnv(sampleSnapshot(now), now, time.Minute)

	tests := []struct {
		name  string
		probe config.Probe
		want  probe.Health
	}{
		{
			name:  "true is ok",
			probe: config.Probe{Path: "tailscale.online"},
			want:  probe.Health{Status: probe.StatusOK},
		},
		{
			name:  "false is down",
			probe: config.Probe{Path: "tailscale.exit_node"},
			want:  probe.Health{Status: probe.StatusDown, Code: probe.CodeOff},
		},
		{
			name:  "equals decides",
			probe: config.Probe{Path: "tailscale.peers_online", Equals: 3, Set: keys("equals")},
			want:  probe.Health{Status: probe.StatusOK},
		},
		{
			name:  "equals decides the other way",
			probe: config.Probe{Path: "tailscale.peers_online", Equals: 2, Set: keys("equals")},
			want:  probe.Health{Status: probe.StatusDown, Code: probe.CodeUnexpectedValue},
		},
		{
			name:  "a field that is not there",
			probe: config.Probe{Path: "tailscale.self_hostname"},
			want:  probe.Health{Status: probe.StatusUnknown, Code: probe.CodeMissingField},
		},
		{
			name:  "a number with no equals",
			probe: config.Probe{Path: "host.load1"},
			want:  probe.Health{Status: probe.StatusUnknown, Code: probe.CodeUnexpectedType},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.probe.Kind = "host_field"
			got := probe.HealthRunners["host_field"](context.Background(), env, test.probe)
			if got != test.want {
				t.Errorf("health = %+v, want %+v", got, test.want)
			}
		})
	}
}

func TestHostField_StaleSnapshotIsNeverGreen(t *testing.T) {
	at := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	env := hostEnv(sampleSnapshot(at), at.Add(time.Hour), time.Minute)

	got := probe.HealthRunners["host_field"](context.Background(), env,
		config.Probe{Kind: "host_field", Path: "tailscale.online"})
	want := probe.Health{Status: probe.StatusUnknown, Code: probe.CodeHostStale, Stale: true}
	if got != want {
		t.Errorf("health = %+v, want %+v", got, want)
	}
}

func TestSelf_IsAlwaysOK(t *testing.T) {
	got := probe.HealthRunners["self"](context.Background(), probe.Env{}, config.Probe{Kind: "self"})
	if got != (probe.Health{Status: probe.StatusOK}) {
		t.Errorf("health = %+v, want ok with no seams at all", got)
	}
}

func boolPointer(value bool) *bool {
	return &value
}

// FuzzHTTPJSON feeds arbitrary bytes through the http_json evaluator: never a panic, and the answer
// is always one of the three statuses a probe may return with a known reason code.
func FuzzHTTPJSON(f *testing.F) {
	for _, seed := range []string{
		`{"status":"ok"}`,
		`{"status":true}`,
		`{"status":null}`,
		`{"items":[{"needs":true}]}`,
		`[]`, `null`, `{}`, ``, `{`, `{"a":{"b":{"c":[1,2,3]}}}`,
		`{"status":"ok"}{"status":"ok"}`,
		"\x00\xff\xfe",
	} {
		f.Add([]byte(seed))
	}
	spec := config.Probe{
		Kind:   "http_json",
		URL:    "http://hub.test/health",
		Path:   "status",
		Equals: "ok",
		Set:    keys("equals"),
	}
	f.Fuzz(func(t *testing.T, body []byte) {
		env := probe.Env{Fetch: func(context.Context, probe.Request) (probe.Response, error) {
			return probe.Response{Status: http.StatusOK, Body: body}, nil
		}}
		health := probe.HealthRunners["http_json"](context.Background(), env, spec)
		switch health.Status {
		case probe.StatusOK, probe.StatusDown, probe.StatusUnknown:
		default:
			t.Fatalf("status %q is not one a probe may return", health.Status)
		}
		if health.Code != "" && probe.Reason(health.Code) == "" {
			t.Fatalf("code %q has no sentence", health.Code)
		}
		if health.Status == probe.StatusOK && health.Code != "" {
			t.Fatalf("ok with reason code %q", health.Code)
		}
	})
}
