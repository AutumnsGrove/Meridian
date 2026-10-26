package cli_test

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/autumnsgrove/meridian/internal/cli"
)

// listenConfig writes the smallest config that names an address. It still needs a section and a
// service, because validation refuses a config with nothing in it.
func writeListenConfig(t *testing.T, listen string) string {
	t.Helper()
	body := "listen: " + listen + "\n" +
		"sections:\n  - {id: test, title: Test}\n" +
		"services:\n" +
		"  - id: nothing\n    name: Nothing\n    section: test\n    icon: activity\n" +
		"    probes:\n      health: {kind: http_status, url: \"http://127.0.0.1:1/health\", expect: [200]}\n"
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

func healthServer(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	return server
}

func TestHealthcheck_IsOKOnlyForA200ThatSaysOK(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		body    string
		wantErr bool
	}{
		{name: "alive", status: http.StatusOK, body: `{"status":"ok","version":"dev"}`},
		{name: "alive but unhappy", status: http.StatusOK, body: `{"status":"down"}`, wantErr: true},
		{name: "an error status", status: http.StatusServiceUnavailable, body: `{"status":"ok"}`, wantErr: true},
		{name: "not JSON at all", status: http.StatusOK, body: `ok`, wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := healthServer(t, test.status, test.body)
			path := writeListenConfig(t, server.Listener.Addr().String())
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()

			err := cli.Healthcheck(ctx, path)

			if test.wantErr && err == nil {
				t.Fatal("Healthcheck() = nil, want an error")
			}
			if !test.wantErr && err != nil {
				t.Fatalf("Healthcheck() = %v, want nil", err)
			}
		})
	}
}

func TestHealthcheck_RefusesToAskNothing(t *testing.T) {
	var listenConfig net.ListenConfig
	listener, err := listenConfig.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	address := listener.Addr().String()
	_ = listener.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := cli.Healthcheck(ctx, writeListenConfig(t, address)); err == nil {
		t.Fatal("Healthcheck() to a closed port = nil, want an error")
	}
}

func TestHealthcheck_NamesTheProblemWithTheConfig(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := cli.Healthcheck(ctx, filepath.Join(t.TempDir(), "absent.yaml"))

	if err == nil {
		t.Fatal("Healthcheck() with no config = nil, want an error")
	}
}
