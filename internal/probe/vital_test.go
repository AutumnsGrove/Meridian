package probe_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/autumnsgrove/meridian/internal/config"
	"github.com/autumnsgrove/meridian/internal/probe"
	"github.com/autumnsgrove/meridian/internal/wire"
)

// vital runs one kind against a body served by a real server, so the fetch, the limit and the decode
// are the real ones.
func vital(t *testing.T, kind, body string, spec config.Probe) (string, error) {
	t.Helper()
	spec.Kind = kind
	spec.URL = serveBody(t, http.StatusOK, body)
	return probe.VitalRunners[kind](context.Background(), httpEnv(), spec)
}

func TestJSONField_FormatsTheValue(t *testing.T) {
	tests := []struct {
		name  string
		body  string
		probe config.Probe
		want  string
	}{
		{
			name:  "a string as it is",
			body:  `{"version":"r935.138ed16"}`,
			probe: config.Probe{Path: "version"},
			want:  "r935.138ed16",
		},
		{
			name:  "a number with decimals",
			body:  `{"load":0.4567}`,
			probe: config.Probe{Path: "load", Decimals: 2},
			want:  "0.46",
		},
		{
			name:  "a number with no decimals",
			body:  `{"load":0.4567}`,
			probe: config.Probe{Path: "load"},
			want:  "0",
		},
		{
			name:  "a huge number",
			body:  `{"sent":1e21}`,
			probe: config.Probe{Path: "sent"},
			want:  "1000000000000000000000",
		},
		{
			name:  "a negative number",
			body:  `{"delta":-12.5}`,
			probe: config.Probe{Path: "delta", Decimals: 1},
			want:  "-12.5",
		},
		{
			name:  "true is yes",
			body:  `{"online":true}`,
			probe: config.Probe{Path: "online"},
			want:  "yes",
		},
		{
			name:  "false is no",
			body:  `{"online":false}`,
			probe: config.Probe{Path: "online"},
			want:  "no",
		},
		{
			name:  "a numeric string stays a string",
			body:  `{"code":"007"}`,
			probe: config.Probe{Path: "code"},
			want:  "007",
		},
		{
			name:  "prefix and suffix",
			body:  `{"version":"r935"}`,
			probe: config.Probe{Path: "version", Prefix: "build", Suffix: "(stable)"},
			want:  "build r935 (stable)",
		},
		{
			name:  "a nested path",
			body:  `{"databases":{"mirror":"ok"}}`,
			probe: config.Probe{Path: "databases.mirror"},
			want:  "ok",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := vital(t, "json_field", test.body, test.probe)
			if err != nil {
				t.Fatalf("vital: %v", err)
			}
			if got != test.want {
				t.Errorf("value = %q, want %q", got, test.want)
			}
		})
	}
}

func TestJSONField_RefusesValuesItDoesNotFormat(t *testing.T) {
	tests := []struct {
		name  string
		body  string
		probe config.Probe
		want  error
	}{
		{name: "an object", body: `{"value":{"a":1}}`, probe: config.Probe{Path: "value"}, want: probe.ErrUnexpectedType},
		{name: "an array", body: `{"value":[1,2]}`, probe: config.Probe{Path: "value"}, want: probe.ErrUnexpectedType},
		{name: "null", body: `{"value":null}`, probe: config.Probe{Path: "value"}, want: probe.ErrUnexpectedType},
		{name: "a missing path", body: `{"value":1}`, probe: config.Probe{Path: "other"}, want: probe.ErrMissingField},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := vital(t, "json_field", test.body, test.probe)
			if !errors.Is(err, test.want) {
				t.Fatalf("err = %v, want %v", err, test.want)
			}
			if got != "" {
				t.Errorf("value = %q, want an empty value on error", got)
			}
		})
	}
}

// TestJSONCount_CountsNeedsAttention is the Ivy shape from INTEGRATIONS.md.
func TestJSONCount_CountsNeedsAttention(t *testing.T) {
	body := `{"items":[
		{"id":"1","from":"a","needs":true},
		{"id":"2","from":"b","needs":false},
		{"id":"3","from":"c","needs":true},
		{"id":"4","from":"d"}
	]}`
	got, err := vital(t, "json_count", body, config.Probe{
		Where:  "items",
		Field:  "needs",
		Equals: true,
		Set:    keys("field", "equals"),
	})
	if err != nil {
		t.Fatalf("vital: %v", err)
	}
	if got != "2" {
		t.Errorf("value = %q, want 2", got)
	}
}

func TestJSONCount_CountsTheArrayWithoutMatching(t *testing.T) {
	got, err := vital(t, "json_count", `{"items":[{},{},{}]}`, config.Probe{Where: "items", Suffix: "items"})
	if err != nil {
		t.Fatalf("vital: %v", err)
	}
	if got != "3 items" {
		t.Errorf("value = %q, want %q", got, "3 items")
	}
}

func TestJSONCount_DoesNotCoerce(t *testing.T) {
	got, err := vital(t, "json_count", `{"items":[{"needs":"true"},{"needs":true}]}`, config.Probe{
		Where:  "items",
		Field:  "needs",
		Equals: "true",
		Set:    keys("field", "equals"),
	})
	if err != nil {
		t.Fatalf("vital: %v", err)
	}
	if got != "1" {
		t.Errorf("value = %q, want 1: a string is not a boolean", got)
	}
}

func TestJSONCount_EmptyArrayIsZero(t *testing.T) {
	got, err := vital(t, "json_count", `{"items":[]}`, config.Probe{Where: "items"})
	if err != nil {
		t.Fatalf("vital: %v", err)
	}
	if got != "0" {
		t.Errorf("value = %q, want 0", got)
	}
}

func TestJSONCount_RefusesAShapeItCannotCount(t *testing.T) {
	tests := []struct {
		name  string
		body  string
		where string
		want  error
	}{
		{name: "missing", body: `{"other":[]}`, where: "items", want: probe.ErrMissingField},
		{name: "not an array", body: `{"items":{"a":1}}`, where: "items", want: probe.ErrUnexpectedType},
		{name: "a number", body: `{"items":4}`, where: "items", want: probe.ErrUnexpectedType},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := vital(t, "json_count", test.body, config.Probe{Where: test.where}); !errors.Is(err, test.want) {
				t.Fatalf("err = %v, want %v", err, test.want)
			}
		})
	}
}

// TestJSONSum_SumsDailyBytes is the bwmon shape: up and down counters per device, formatted as GB.
func TestJSONSum_SumsDailyBytes(t *testing.T) {
	body := `{"range":"today","devices":[
		{"device":"a","up_bytes":1500000000,"down_bytes":2500000000},
		{"device":"b","up_bytes":0,"down_bytes":0}
	]}`
	got, err := vital(t, "json_sum", body, config.Probe{
		Where:    "devices",
		Fields:   []string{"up_bytes", "down_bytes"},
		Scale:    "bytes",
		Unit:     "GB",
		Decimals: 1,
		Suffix:   "GB today",
	})
	if err != nil {
		t.Fatalf("vital: %v", err)
	}
	if got != "4.0 GB today" {
		t.Errorf("value = %q, want %q", got, "4.0 GB today")
	}
}

func TestJSONSum_FormatsAndSkips(t *testing.T) {
	tests := []struct {
		name  string
		body  string
		probe config.Probe
		want  string
	}{
		{
			name:  "scale none",
			body:  `{"items":[{"n":1},{"n":2}]}`,
			probe: config.Probe{Where: "items", Fields: []string{"n"}, Scale: "none", Decimals: 1},
			want:  "3.0",
		},
		{
			name:  "non-numeric fields are skipped",
			body:  `{"items":[{"n":1},{"n":"two"},{"n":4}]}`,
			probe: config.Probe{Where: "items", Fields: []string{"n"}, Scale: "none"},
			want:  "5",
		},
		{
			name:  "a missing field in one element",
			body:  `{"items":[{"n":1},{"m":2},{"n":3}]}`,
			probe: config.Probe{Where: "items", Fields: []string{"n"}, Scale: "none"},
			want:  "4",
		},
		{
			name:  "several fields per element",
			body:  `{"items":[{"a":1,"b":2},{"a":3}]}`,
			probe: config.Probe{Where: "items", Fields: []string{"a", "b"}, Scale: "none"},
			want:  "6",
		},
		{
			name:  "negative numbers",
			body:  `{"items":[{"n":-4},{"n":1.5}]}`,
			probe: config.Probe{Where: "items", Fields: []string{"n"}, Scale: "none", Decimals: 1},
			want:  "-2.5",
		},
		{
			name:  "huge numbers",
			body:  `{"items":[{"n":1e15},{"n":1e15}]}`,
			probe: config.Probe{Where: "items", Fields: []string{"n"}, Scale: "none"},
			want:  "2000000000000000",
		},
		{
			name:  "a byte sum that picks its own unit",
			body:  `{"items":[{"n":2500}]}`,
			probe: config.Probe{Where: "items", Fields: []string{"n"}, Scale: "bytes", Decimals: 1},
			want:  "2.5",
		},
		{
			name:  "an empty array",
			body:  `{"items":[]}`,
			probe: config.Probe{Where: "items", Fields: []string{"n"}, Scale: "none"},
			want:  "",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := vital(t, "json_sum", test.body, test.probe)
			if test.want == "" {
				if !errors.Is(err, probe.ErrMissingField) {
					t.Fatalf("err = %v, want %v: a sum with nothing numeric must not claim zero", err, probe.ErrMissingField)
				}
				return
			}
			if err != nil {
				t.Fatalf("vital: %v", err)
			}
			if got != test.want {
				t.Errorf("value = %q, want %q", got, test.want)
			}
		})
	}
}

func TestJSONSum_RefusesWhatItCannotSum(t *testing.T) {
	tests := []struct {
		name  string
		body  string
		probe config.Probe
		want  error
	}{
		{
			name:  "nothing numeric",
			body:  `{"items":[{"n":"one"},{"n":null}]}`,
			probe: config.Probe{Where: "items", Fields: []string{"n"}, Scale: "none"},
			want:  probe.ErrMissingField,
		},
		{
			name:  "where is not an array",
			body:  `{"items":{"n":1}}`,
			probe: config.Probe{Where: "items", Fields: []string{"n"}, Scale: "none"},
			want:  probe.ErrUnexpectedType,
		},
		{
			name:  "negative bytes",
			body:  `{"items":[{"n":-1}]}`,
			probe: config.Probe{Where: "items", Fields: []string{"n"}, Scale: "bytes"},
			want:  probe.ErrBadValue,
		},
		{
			name:  "a scale that is neither",
			body:  `{"items":[{"n":1}]}`,
			probe: config.Probe{Where: "items", Fields: []string{"n"}, Scale: "furlongs"},
			want:  probe.ErrBadValue,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := vital(t, "json_sum", test.body, test.probe); !errors.Is(err, test.want) {
				t.Fatalf("err = %v, want %v", err, test.want)
			}
		})
	}
}

// TestJSONRatioPct_BlockedQueries is the AdGuard shape: blocked filtering over all DNS queries.
func TestJSONRatioPct_BlockedQueries(t *testing.T) {
	body := `{"num_dns_queries":4000,"num_blocked_filtering":1200}`
	got, err := vital(t, "json_ratio_pct", body, config.Probe{
		Numerator:   "num_blocked_filtering",
		Denominator: "num_dns_queries",
		Suffix:      "blocked",
	})
	if err != nil {
		t.Fatalf("vital: %v", err)
	}
	if got != "30 blocked" {
		t.Errorf("value = %q, want %q", got, "30 blocked")
	}
}

func TestJSONRatioPct_RoundsAndHandlesZero(t *testing.T) {
	tests := []struct {
		name  string
		body  string
		probe config.Probe
		want  string
	}{
		{
			name:  "decimals",
			body:  `{"a":1,"b":8}`,
			probe: config.Probe{Numerator: "a", Denominator: "b", Decimals: 1},
			want:  "12.5",
		},
		{
			name:  "zero over zero is zero",
			body:  `{"a":0,"b":0}`,
			probe: config.Probe{Numerator: "a", Denominator: "b"},
			want:  "0",
		},
		{
			name:  "nothing over anything is zero",
			body:  `{"a":0,"b":7}`,
			probe: config.Probe{Numerator: "a", Denominator: "b", Decimals: 2},
			want:  "0.00",
		},
		{
			name:  "more than all of it",
			body:  `{"a":3,"b":2}`,
			probe: config.Probe{Numerator: "a", Denominator: "b"},
			want:  "150",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := vital(t, "json_ratio_pct", test.body, test.probe)
			if err != nil {
				t.Fatalf("vital: %v", err)
			}
			if got != test.want {
				t.Errorf("value = %q, want %q", got, test.want)
			}
		})
	}
}

func TestJSONRatioPct_RefusesWhatItCannotDivide(t *testing.T) {
	tests := []struct {
		name  string
		body  string
		probe config.Probe
		want  error
	}{
		{
			name:  "anything over zero",
			body:  `{"a":5,"b":0}`,
			probe: config.Probe{Numerator: "a", Denominator: "b"},
			want:  probe.ErrBadValue,
		},
		{
			name:  "a missing numerator",
			body:  `{"b":5}`,
			probe: config.Probe{Numerator: "a", Denominator: "b"},
			want:  probe.ErrMissingField,
		},
		{
			name:  "a missing denominator",
			body:  `{"a":5}`,
			probe: config.Probe{Numerator: "a", Denominator: "b"},
			want:  probe.ErrMissingField,
		},
		{
			name:  "a string numerator",
			body:  `{"a":"5","b":5}`,
			probe: config.Probe{Numerator: "a", Denominator: "b"},
			want:  probe.ErrUnexpectedType,
		},
		{
			name:  "an object denominator",
			body:  `{"a":5,"b":{"c":1}}`,
			probe: config.Probe{Numerator: "a", Denominator: "b"},
			want:  probe.ErrUnexpectedType,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := vital(t, "json_ratio_pct", test.body, test.probe); !errors.Is(err, test.want) {
				t.Fatalf("err = %v, want %v", err, test.want)
			}
		})
	}
}

func TestVitalHostField_FormatsTheSnapshot(t *testing.T) {
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	env := hostEnv(sampleSnapshot(now), now, time.Minute)

	tests := []struct {
		name  string
		probe config.Probe
		want  string
	}{
		{name: "a number", probe: config.Probe{Path: "tailscale.peers_online"}, want: "3"},
		{name: "a boolean", probe: config.Probe{Path: "tailscale.exit_node"}, want: "no"},
		{name: "with a suffix", probe: config.Probe{Path: "host.uptime_s", Suffix: "seconds"}, want: "3600 seconds"},
		{name: "with decimals", probe: config.Probe{Path: "host.load1", Decimals: 2}, want: "0.50"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.probe.Kind = "host_field"
			got, err := probe.VitalRunners["host_field"](context.Background(), env, test.probe)
			if err != nil {
				t.Fatalf("vital: %v", err)
			}
			if got != test.want {
				t.Errorf("value = %q, want %q", got, test.want)
			}
		})
	}
}

func TestVitalHostField_ReportsTheSnapshotFailure(t *testing.T) {
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	spec := config.Probe{Kind: "host_field", Path: "tailscale.peers_online"}

	tests := []struct {
		name string
		env  probe.Env
		want error
	}{
		{
			name: "stale",
			env:  hostEnv(sampleSnapshot(now.Add(-time.Hour)), now, time.Minute),
			want: probe.ErrHostStale,
		},
		{
			name: "no snapshot",
			env: probe.Env{
				Host:       func() (wire.HostSnapshot, bool) { return wire.HostSnapshot{}, false },
				HostMaxAge: time.Minute,
				Now:        fixedClock(now),
			},
			want: probe.ErrHostUnavailable,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := probe.VitalRunners["host_field"](context.Background(), test.env, spec)
			if !errors.Is(err, test.want) {
				t.Fatalf("err = %v, want %v", err, test.want)
			}
			if got != "" {
				t.Errorf("value = %q, want an empty value on error", got)
			}
			if code := probe.CodeOf(err); code != probe.CodeHostStale && code != probe.CodeHostUnavailable {
				t.Errorf("code = %q, want a host reason code", code)
			}
		})
	}
}

func TestVitalHostField_MissingPath(t *testing.T) {
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	env := hostEnv(sampleSnapshot(now), now, time.Minute)
	if _, err := probe.VitalRunners["host_field"](context.Background(), env,
		config.Probe{Kind: "host_field", Path: "tailscale.self_hostname"}); !errors.Is(err, probe.ErrMissingField) {
		t.Fatalf("err = %v, want %v", err, probe.ErrMissingField)
	}
}

// TestVitals_NeverShowTheCredential is the secret rule at the last seam: the service reflects the
// Authorization header in its answer, the vital reads another field, and the credential must not
// appear in the value or in an error.
func TestVitals_NeverShowTheCredential(t *testing.T) {
	const secret = "s3cret-token-6f1c"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		//nolint:gosec // the reflected header is the point: the vital must not print it
		_, _ = w.Write([]byte(`{"authorization":"` + r.Header.Get("Authorization") + `","ok":1}`))
	}))
	defer server.Close()

	getenv := env(map[string]string{"TOKEN": secret})
	fetch := probe.Memo(probe.NewHTTPFetcher(getenv, "meridian-test", probe.MaxBodySize))
	env := probe.Env{Fetch: fetch}

	specs := map[string]config.Probe{
		"json_field":         {Kind: "json_field", URL: server.URL, Path: "ok", Auth: &config.Auth{BearerEnv: "TOKEN"}},
		"json_count":         {Kind: "json_count", URL: server.URL, Where: "missing", Auth: &config.Auth{BearerEnv: "TOKEN"}},
		"json_sum":           {Kind: "json_sum", URL: server.URL, Where: "missing", Fields: []string{"ok"}, Scale: "none", Auth: &config.Auth{BearerEnv: "TOKEN"}},
		"json_ratio_pct":     {Kind: "json_ratio_pct", URL: server.URL, Numerator: "ok", Denominator: "zero", Auth: &config.Auth{BearerEnv: "TOKEN"}},
		"json_field_no_auth": {Kind: "json_field", URL: server.URL, Path: "ok"},
	}
	for kind, spec := range specs {
		runner, known := probe.VitalRunners[spec.Kind]
		if !known {
			t.Fatalf("no runner for %s", kind)
		}
		value, err := runner(context.Background(), env, spec)
		if strings.Contains(value, secret) {
			t.Errorf("%s returned the credential: %q", kind, value)
		}
		if err != nil && strings.Contains(err.Error(), secret) {
			t.Errorf("%s error contains the credential: %v", kind, err)
		}
	}
}

func TestVitalErrors_NameAReasonCode(t *testing.T) {
	tests := []struct {
		err  error
		code string
	}{
		{err: probe.ErrMissingField, code: probe.CodeMissingField},
		{err: probe.ErrUnexpectedType, code: probe.CodeUnexpectedType},
		{err: probe.ErrUnexpectedValue, code: probe.CodeUnexpectedValue},
		{err: probe.ErrBadValue, code: probe.CodeBadValue},
		{err: probe.ErrHostStale, code: probe.CodeHostStale},
		{err: probe.ErrHostUnavailable, code: probe.CodeHostUnavailable},
		{err: probe.ErrBodyTooLarge, code: probe.CodeBodyTooLarge},
		{err: probe.ErrTimeout, code: probe.CodeTimeout},
	}
	for _, test := range tests {
		if code := probe.CodeOf(test.err); code != test.code {
			t.Errorf("CodeOf(%v) = %q, want %q", test.err, code, test.code)
		}
		if code := probe.CodeOf(test.err); probe.Reason(code) == "" {
			t.Errorf("code %q has no sentence", code)
		}
	}
}

// FuzzVitals feeds arbitrary JSON through every vital kind: never a panic, a bounded value, and an
// error in the closed set with a known reason code.
func FuzzVitals(f *testing.F) {
	for _, seed := range []string{
		`{"version":"r1","items":[{"n":1,"ok":true},{"n":2,"ok":false}],"a":1,"b":4}`,
		`{"items":[]}`, `{"items":null}`, `{}`, `null`, `[]`, `{"items":[{"n":"x"}]}`,
		`{"items":[{"n":1e308},{"n":1e308}]}`, `{"a":0,"b":0}`, `{"a":1e308,"b":1e-308}`,
	} {
		f.Add([]byte(seed), uint8(0))
	}

	specs := []config.Probe{
		{Kind: "json_field", Path: "version", Prefix: "v", Suffix: "x", Decimals: 2},
		{Kind: "json_field", Path: "items.0.n", Decimals: 3},
		{Kind: "json_count", Where: "items", Field: "ok", Equals: true, Set: keys("field", "equals")},
		{Kind: "json_count", Where: "items"},
		{Kind: "json_sum", Where: "items", Fields: []string{"n", "price"}, Scale: "none", Decimals: 1},
		{Kind: "json_sum", Where: "items", Fields: []string{"n"}, Scale: "bytes", Unit: "KB"},
		{Kind: "json_ratio_pct", Numerator: "a", Denominator: "b", Suffix: "%", Decimals: 2},
		{Kind: "host_field", Path: "host.load1", Decimals: 1},
	}
	for index := range specs {
		f.Add([]byte(`{"version":"r1","items":[{"n":1}],"a":1,"b":2}`), uint8(index))
	}
	f.Fuzz(func(t *testing.T, body []byte, which uint8) {
		spec := specs[int(which)%len(specs)]
		env := probe.Env{Fetch: func(context.Context, probe.Request) (probe.Response, error) {
			return probe.Response{Status: http.StatusOK, Body: body}, nil
		}}
		value, err := probe.VitalRunners[spec.Kind](context.Background(), env, spec)
		if len(value) > 4096 {
			t.Fatalf("%s formatted %d bytes", spec.Kind, len(value))
		}
		if err != nil {
			if code := probe.CodeOf(err); probe.Reason(code) == "" {
				t.Fatalf("%s error %v has no reason code", spec.Kind, err)
			}
			if value != "" {
				t.Fatalf("%s returned %q with an error", spec.Kind, value)
			}
		}
	})
}
