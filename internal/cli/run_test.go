package cli_test

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	clog "github.com/charmbracelet/log"

	"github.com/autumnsgrove/meridian/internal/cli"
)

// statusBody is the slice of /api/status these tests read. The full contract is pinned by the
// gateway's golden files; here we only need to see that a reload reached the page.
type statusBody struct {
	Config struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	} `json:"config"`
	Sections []struct {
		Services []struct {
			Name   string `json:"name"`
			Status string `json:"status"`
		} `json:"services"`
	} `json:"sections"`
}

func quietLogger() *clog.Logger {
	return clog.New(io.Discard)
}

// freeAddress is a loopback address nothing is listening on, so the server under test can bind it.
func freeAddress(t *testing.T) string {
	t.Helper()
	var listenConfig net.ListenConfig
	listener, err := listenConfig.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("find a free port: %v", err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("release the port: %v", err)
	}
	return address
}

func configBody(listen, upstream string, names ...string) string {
	body := "listen: " + listen + "\n" +
		"sections:\n  - {id: test, title: Test}\n" +
		"poll:\n  interval: 10s\n  idle_interval: 1m\n  idle_after: 5m\n  timeout: 1s\n  refresh_min: 1s\n" +
		"services:\n"
	for _, name := range names {
		body += "  - id: " + name + "\n" +
			"    name: " + strings.ToUpper(name[:1]) + name[1:] + "\n" +
			"    section: test\n" +
			"    icon: activity\n" +
			"    description: a service under test\n" +
			"    probes:\n" +
			"      health: {kind: http_status, url: \"" + upstream + "/health\", expect: [200]}\n"
	}
	return body
}

func writeConfig(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
}

// until polls a condition until it holds or the deadline passes. It is the only waiting these tests
// do: the processes under test poll on their own schedule, so a fixed sleep would be either flaky or
// slow.
func until(t *testing.T, what string, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func healthy(ctx context.Context, address string) bool {
	client := &http.Client{Timeout: time.Second}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+address+"/healthz", http.NoBody)
	if err != nil {
		return false
	}
	response, err := client.Do(request)
	if err != nil {
		return false
	}
	defer func() { _ = response.Body.Close() }()
	return response.StatusCode == http.StatusOK
}

func fetchStatus(ctx context.Context, address string) (statusBody, bool) {
	client := &http.Client{Timeout: time.Second}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+address+"/api/status", http.NoBody)
	if err != nil {
		return statusBody{}, false
	}
	response, err := client.Do(request)
	if err != nil {
		return statusBody{}, false
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return statusBody{}, false
	}
	var body statusBody
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&body); err != nil {
		return statusBody{}, false
	}
	return body, true
}

// allHealthy reports whether every service on the page has been probed and found healthy.
func allHealthy(body statusBody) bool {
	count := 0
	for _, section := range body.Sections {
		for _, service := range section.Services {
			count++
			if service.Status != "ok" {
				return false
			}
		}
	}
	return count > 0
}

func serviceNames(body statusBody) []string {
	names := make([]string, 0)
	for _, section := range body.Sections {
		for _, service := range section.Services {
			names = append(names, service.Name)
		}
	}
	return names
}

func TestRun_ServesProbesAndReloadsWithoutRestarting(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(upstream.Close)

	address := freeAddress(t)
	path := filepath.Join(t.TempDir(), "config.yaml")
	writeConfig(t, path, configBody(address, upstream.URL, "alpha"))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	signals := make(chan os.Signal, 8)
	served := make(chan error, 1)
	go func() { served <- cli.Run(ctx, cli.RunOptions{ConfigPath: path}, signals, quietLogger()) }()

	until(t, "the server to answer /healthz", func() bool { return healthy(ctx, address) })

	body, ok := fetchStatus(ctx, address)
	if !ok {
		t.Fatal("GET /api/status failed")
	}
	if got := serviceNames(body); len(got) != 1 || got[0] != "Alpha" {
		t.Fatalf("services = %v, want just Alpha", got)
	}

	// The scheduler polls on its own; the first result makes the service healthy.
	until(t, "Alpha to be probed", func() bool {
		status, ok := fetchStatus(ctx, address)
		if !ok || len(status.Sections) == 0 || len(status.Sections[0].Services) == 0 {
			return false
		}
		return status.Sections[0].Services[0].Status == "ok"
	})

	// A reload that adds a service must show it without a restart.
	writeConfig(t, path, configBody(address, upstream.URL, "alpha", "beta"))
	signals <- syscall.SIGHUP
	until(t, "Beta to appear after a reload", func() bool {
		status, ok := fetchStatus(ctx, address)
		return ok && len(serviceNames(status)) == 2
	})
	// The page lists whatever the config says; this proves the scheduler took the reload too and is
	// actually polling the new service.
	until(t, "Beta to be probed", func() bool {
		status, ok := fetchStatus(ctx, address)
		return ok && allHealthy(status)
	})

	// A reload that does not validate keeps the old config serving and raises the banner.
	writeConfig(t, path, strings.Replace(configBody(address, upstream.URL, "alpha", "beta"),
		"kind: http_status", "kind: nonsense", 1))
	signals <- syscall.SIGHUP
	until(t, "the rejection to reach the page", func() bool {
		status, ok := fetchStatus(ctx, address)
		return ok && !status.Config.OK
	})
	body, ok = fetchStatus(ctx, address)
	if !ok {
		t.Fatal("GET /api/status failed after a rejected reload")
	}
	if !strings.Contains(body.Config.Error, "probe_kind_unknown") {
		t.Fatalf("banner error = %q, want the reason code", body.Config.Error)
	}
	if got := serviceNames(body); len(got) != 2 {
		t.Fatalf("a rejected reload served %v, want the last good config", got)
	}

	// A good reload clears the banner again.
	writeConfig(t, path, configBody(address, upstream.URL, "alpha", "beta"))
	signals <- syscall.SIGHUP
	until(t, "the banner to clear", func() bool {
		status, ok := fetchStatus(ctx, address)
		return ok && status.Config.OK
	})

	// Shutting down returns promptly and leaves the address free.
	signals <- syscall.SIGTERM
	select {
	case err := <-served:
		if err != nil {
			t.Fatalf("Run() = %v, want nil after a shutdown signal", err)
		}
	case <-time.After(6 * time.Second):
		t.Fatal("Run() did not return within the drain window")
	}

	var listenConfig net.ListenConfig
	listener, err := listenConfig.Listen(ctx, "tcp", address)
	if err != nil {
		t.Fatalf("the address is still bound after shutdown: %v", err)
	}
	_ = listener.Close()
}

func TestRun_CancelledContextStopsServing(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(upstream.Close)

	address := freeAddress(t)
	path := filepath.Join(t.TempDir(), "config.yaml")
	writeConfig(t, path, configBody(address, upstream.URL, "alpha"))

	ctx, cancel := context.WithCancel(context.Background())
	served := make(chan error, 1)
	go func() { served <- cli.Run(ctx, cli.RunOptions{ConfigPath: path}, nil, quietLogger()) }()
	until(t, "the server to answer /healthz", func() bool { return healthy(ctx, address) })

	cancel()

	select {
	case err := <-served:
		if err != nil {
			t.Fatalf("Run() = %v, want nil", err)
		}
	case <-time.After(6 * time.Second):
		t.Fatal("Run() ignored the cancelled context")
	}
}

func TestRun_RefusesToStartOnABrokenConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	writeConfig(t, path, "listen: 127.0.0.1:0\nservices: []\n")

	err := cli.Run(context.Background(), cli.RunOptions{ConfigPath: path}, nil, quietLogger())

	if err == nil {
		t.Fatal("Run() = nil, want the validation problems")
	}
	if !strings.Contains(err.Error(), "sections") {
		t.Fatalf("Run() error = %q, want it to name the missing sections", err)
	}
}

func TestRun_WatchReloadsWhenTheFileChanges(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(upstream.Close)

	address := freeAddress(t)
	path := filepath.Join(t.TempDir(), "config.yaml")
	writeConfig(t, path, configBody(address, upstream.URL, "alpha"))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	served := make(chan error, 1)
	go func() {
		served <- cli.Run(ctx, cli.RunOptions{ConfigPath: path, Watch: true}, nil, quietLogger())
	}()
	until(t, "the server to answer /healthz", func() bool { return healthy(ctx, address) })

	// Touching the file is not enough: the content has to differ, and so does the mtime.
	body := configBody(address, upstream.URL, "alpha", "beta")
	writeConfig(t, path, body)
	future := time.Now().Add(time.Minute)
	if err := os.Chtimes(path, future, future); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	until(t, "the watched file to be reloaded", func() bool {
		status, ok := fetchStatus(ctx, address)
		return ok && len(serviceNames(status)) == 2
	})

	cancel()
	<-served
}
