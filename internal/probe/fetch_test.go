package probe_test

import (
	"context"
	"encoding/base64"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/autumnsgrove/meridian/internal/config"
	"github.com/autumnsgrove/meridian/internal/probe"
)

func newFetcher() probe.Fetcher {
	return probe.NewHTTPFetcher(nil, "meridian-test", 1<<20)
}

// env returns a getenv function over a map, standing in for the process environment.
func env(values map[string]string) func(string) string {
	return func(name string) string { return values[name] }
}

func TestFetcher_ReturnsRedirectInsteadOfFollowingIt(t *testing.T) {
	var targetHits atomic.Int64
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		targetHits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/login", http.StatusFound)
	}))
	defer origin.Close()

	response, err := newFetcher()(context.Background(), probe.Request{URL: origin.URL + "/"})
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if response.Status != http.StatusFound {
		t.Errorf("Status = %d, want %d", response.Status, http.StatusFound)
	}
	if hits := targetHits.Load(); hits != 0 {
		t.Errorf("the redirect was followed: the target saw %d requests", hits)
	}
}

func TestFetcher_DoesNotSendCredentialsToARedirectTarget(t *testing.T) {
	var targetAuth atomic.Value
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetAuth.Store(r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/login", http.StatusTemporaryRedirect)
	}))
	defer origin.Close()

	getenv := env(map[string]string{"TOKEN": "s3cret"})
	fetch := probe.NewHTTPFetcher(getenv, "meridian-test", 1<<20)
	if _, err := fetch(context.Background(), probe.Request{
		URL:  origin.URL + "/",
		Auth: config.Auth{BearerEnv: "TOKEN"},
	}); err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if sent, _ := targetAuth.Load().(string); sent != "" {
		t.Errorf("the redirect target received Authorization %q", sent)
	}
}

func TestFetcher_RefusesBodyOverLimit(t *testing.T) {
	tests := []struct {
		name     string
		limit    int64
		bodySize int
		wantErr  error
	}{
		{name: "at the limit is fine", limit: 16, bodySize: 16},
		{name: "one over the limit is not", limit: 16, bodySize: 17, wantErr: probe.ErrBodyTooLarge},
		{name: "far over the limit", limit: 16, bodySize: 4096, wantErr: probe.ErrBodyTooLarge},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			body := strings.Repeat("x", test.bodySize)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(body))
			}))
			defer server.Close()

			response, err := probe.NewHTTPFetcher(nil, "meridian-test", test.limit)(
				context.Background(), probe.Request{URL: server.URL})
			if test.wantErr != nil {
				if !errors.Is(err, test.wantErr) {
					t.Fatalf("err = %v, want %v", err, test.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("fetch: %v", err)
			}
			if int64(len(response.Body)) != test.limit {
				t.Errorf("body is %d bytes, want %d", len(response.Body), test.limit)
			}
		})
	}
}

func TestFetcher_ReportsATimeoutFromTheContext(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		// The client giving up cancels the request context, so the handler leaves with it.
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	if _, err := newFetcher()(ctx, probe.Request{URL: server.URL}); !errors.Is(err, probe.ErrTimeout) {
		t.Fatalf("err = %v, want %v", err, probe.ErrTimeout)
	}
}

func TestFetcher_ReportsAReusedPortAsRefused(t *testing.T) {
	listener, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("close listener: %v", err)
	}

	if _, err := newFetcher()(context.Background(), probe.Request{URL: "http://" + addr + "/"}); !errors.Is(err, probe.ErrRefused) {
		t.Fatalf("err = %v, want %v", err, probe.ErrRefused)
	}
}

func TestFetcher_SendsAuthFromTheEnvironment(t *testing.T) {
	tests := []struct {
		name   string
		auth   config.Auth
		env    map[string]string
		header string
		value  string
	}{
		{
			name:   "basic",
			auth:   config.Auth{BasicEnv: []string{"USER", "PASS"}},
			env:    map[string]string{"USER": "autumn", "PASS": "hunter2"},
			header: "Authorization",
			value:  "Basic " + base64.StdEncoding.EncodeToString([]byte("autumn:hunter2")),
		},
		{
			name:   "bearer",
			auth:   config.Auth{BearerEnv: "TOKEN"},
			env:    map[string]string{"TOKEN": "abc123"},
			header: "Authorization",
			value:  "Bearer abc123",
		},
		{
			name:   "literal header",
			auth:   config.Auth{Header: map[string]string{"X-Api-Key": "k-1"}},
			header: "X-Api-Key",
			value:  "k-1",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var got atomic.Value
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				got.Store(r.Header.Get(test.header))
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()

			fetch := probe.NewHTTPFetcher(env(test.env), "meridian-test", 1<<20)
			if _, err := fetch(context.Background(), probe.Request{URL: server.URL, Auth: test.auth}); err != nil {
				t.Fatalf("fetch: %v", err)
			}
			if sent, _ := got.Load().(string); sent != test.value {
				t.Errorf("%s = %q, want %q", test.header, sent, test.value)
			}
		})
	}
}

// TestFetcher_SendsTheConfiguredHost covers the Ivy rule in INTEGRATIONS.md: the URL from the config
// is used verbatim, so a service that checks Host sees exactly what the config said.
func TestFetcher_SendsTheConfiguredHost(t *testing.T) {
	var configuredHost string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != configuredHost {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse test URL: %v", err)
	}
	configuredHost = "localhost:" + parsed.Port()

	response, err := newFetcher()(context.Background(),
		probe.Request{URL: "http://" + configuredHost + "/api/v1/health"})
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if response.Status != http.StatusOK {
		t.Errorf("Status = %d, want 200: the Host header was not the configured one", response.Status)
	}
}

func TestFetcher_MissingCredentialSendsNothing(t *testing.T) {
	var hits atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	fetch := probe.NewHTTPFetcher(env(map[string]string{"USER": "autumn"}), "meridian-test", 1<<20)
	_, err := fetch(context.Background(), probe.Request{
		URL:  server.URL,
		Auth: config.Auth{BasicEnv: []string{"USER", "PASS"}},
	})

	var missing *probe.ErrEnvMissing
	if !errors.As(err, &missing) {
		t.Fatalf("err = %v, want *probe.ErrEnvMissing", err)
	}
	if !slices.Equal(missing.Names, []string{"PASS"}) {
		t.Errorf("Names = %q, want [PASS]", missing.Names)
	}
	if strings.Contains(err.Error(), "autumn") {
		t.Errorf("the error contains a credential value: %v", err)
	}
	if got := hits.Load(); got != 0 {
		t.Errorf("the server saw %d requests, want 0", got)
	}
}

func TestFetcher_RefusesAnUnbuildableRequest(t *testing.T) {
	tests := []struct {
		name string
		url  string
		auth config.Auth
		env  map[string]string
	}{
		{name: "not a URL", url: "not a url"},
		{name: "no scheme", url: "//example.org/path"},
		{name: "another scheme", url: "file:///etc/passwd"},
		{name: "credentials in the URL", url: "http://user:pass@127.0.0.1:1/"}, //nolint:gosec // a fake credential, and the point is that it is refused
		{name: "no host", url: "http:///path"},
		{name: "header value with a newline", url: "http://127.0.0.1:1/",
			auth: config.Auth{Header: map[string]string{"X-Token": "a\r\nX-Injected: 1"}}}, //nolint:gosec // a header injection the request must refuse
		{name: "header name with a space", url: "http://127.0.0.1:1/",
			auth: config.Auth{Header: map[string]string{"X Token": "a"}}},
		{name: "basic env of the wrong length", url: "http://127.0.0.1:1/",
			auth: config.Auth{BasicEnv: []string{"USER"}}, env: map[string]string{"USER": "autumn"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fetch := probe.NewHTTPFetcher(env(test.env), "meridian-test", 1<<20)
			_, err := fetch(context.Background(), probe.Request{URL: test.url, Auth: test.auth})
			if !errors.Is(err, probe.ErrBadRequest) {
				t.Fatalf("err = %v, want %v", err, probe.ErrBadRequest)
			}
		})
	}
}

func TestMemo_FetchesEachEndpointAndCredentialOnce(t *testing.T) {
	var calls atomic.Int64
	fetch := probe.Memo(func(_ context.Context, request probe.Request) (probe.Response, error) {
		calls.Add(1)
		return probe.Response{Status: http.StatusOK, Body: []byte(request.URL)}, nil
	})
	ctx := context.Background()

	duplicate := probe.Request{URL: "http://one.test/health"}
	for range 2 {
		response, err := fetch(ctx, duplicate)
		if err != nil {
			t.Fatalf("fetch: %v", err)
		}
		if string(response.Body) != duplicate.URL {
			t.Errorf("Body = %q, want %q", response.Body, duplicate.URL)
		}
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("identical requests caused %d fetches, want 1", got)
	}

	if _, err := fetch(ctx, probe.Request{URL: duplicate.URL, Auth: config.Auth{BearerEnv: "TOKEN"}}); err != nil {
		t.Fatalf("fetch with other auth: %v", err)
	}
	if _, err := fetch(ctx, probe.Request{URL: "http://two.test/health"}); err != nil {
		t.Fatalf("fetch of another URL: %v", err)
	}
	if got := calls.Load(); got != 3 {
		t.Errorf("calls = %d, want 3: different auth or URL must not share a response", got)
	}
}

func TestMemo_RemembersAFailure(t *testing.T) {
	var calls atomic.Int64
	fetch := probe.Memo(func(_ context.Context, _ probe.Request) (probe.Response, error) {
		calls.Add(1)
		return probe.Response{}, probe.ErrRefused
	})
	ctx := context.Background()

	for range 2 {
		if _, err := fetch(ctx, probe.Request{URL: "http://one.test/"}); !errors.Is(err, probe.ErrRefused) {
			t.Fatalf("err = %v, want %v", err, probe.ErrRefused)
		}
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("calls = %d, want 1: a failure is remembered for the cycle too", got)
	}
}

func TestMemo_CollapsesConcurrentDuplicates(t *testing.T) {
	const callers = 8
	var calls atomic.Int64
	release := make(chan struct{})
	fetch := probe.Memo(func(_ context.Context, _ probe.Request) (probe.Response, error) {
		calls.Add(1)
		<-release
		return probe.Response{Status: http.StatusOK}, nil
	})

	start := make(chan struct{})
	var wait sync.WaitGroup
	wait.Add(callers)
	for range callers {
		go func() {
			defer wait.Done()
			<-start
			if _, err := fetch(context.Background(), probe.Request{URL: "http://one.test/"}); err != nil {
				t.Errorf("fetch: %v", err)
			}
		}()
	}
	close(start)
	close(release)
	wait.Wait()

	if got := calls.Load(); got != 1 {
		t.Errorf("calls = %d, want 1: concurrent duplicates share one fetch", got)
	}
}

// FuzzBuildRequest treats the URL and one auth header as untrusted: the result is either an error
// in the closed set or a request that can only reach the host the config named.
func FuzzBuildRequest(f *testing.F) {
	for _, seed := range []string{
		"http://localhost:8899/healthz",
		"https://hub.example.org/api/v1/health?x=1",
		"http://127.0.0.1:5335/",
		"http://[::1]:8080/",
		"", "  ", "not a url", "//hub.example.org/x", "file:///etc/passwd",
		"http://user:pass@127.0.0.1:1/", "http://host/%zz", "http://host:abc/",
	} {
		f.Add(seed, "X-Token", "value")
	}
	f.Fuzz(func(t *testing.T, rawURL, headerName, headerValue string) {
		auth := config.Auth{Header: map[string]string{headerName: headerValue}}
		request, err := probe.BuildRequest(context.Background(),
			probe.Request{URL: rawURL, Auth: auth},
			func(string) string { return "" })
		if err != nil {
			if !errors.Is(err, probe.ErrBadRequest) {
				t.Fatalf("error %v is not ErrBadRequest", err)
			}
			return
		}
		if request.URL.Scheme != "http" && request.URL.Scheme != "https" {
			t.Fatalf("scheme %q is not http or https", request.URL.Scheme)
		}
		if request.URL.Host == "" {
			t.Fatal("request has no host")
		}
		if request.URL.User != nil {
			t.Fatal("request carries credentials in its URL")
		}
		for name, values := range request.Header {
			if strings.ContainsAny(name, "\r\n\x00") {
				t.Fatalf("header name %q contains a control character", name)
			}
			for _, value := range values {
				if strings.ContainsAny(value, "\r\n\x00") {
					t.Fatalf("header %q has a value with a control character", name)
				}
			}
		}
		// The config's URL survives untouched: same scheme, host, path and query. The one exception
		// is Go dropping an empty port ("http://hub:" becomes "http://hub"), which names the same
		// endpoint.
		parsed, parseErr := url.Parse(rawURL)
		if parseErr != nil {
			t.Fatalf("a request was built from an unparsable URL %q", rawURL)
		}
		if request.URL.Scheme != parsed.Scheme ||
			request.URL.Host != strings.TrimSuffix(parsed.Host, ":") ||
			request.URL.Path != parsed.Path ||
			request.URL.RawQuery != parsed.RawQuery {
			t.Fatalf("URL changed from %q to %q", rawURL, request.URL.String())
		}
	})
}
