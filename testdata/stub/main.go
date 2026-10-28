// Command stub is the upstream the Playwright suite probes.
//
// The portable fixture probes two services over HTTP, and nothing on a build machine answers those
// URLs. This fixture does: it serves the paths the config names and has one control endpoint a test
// can use to take a service down, which is how the suite watches a tile change without touching
// anything real.
//
// It lives under testdata/ because it deliberately names the fixture's services; the invariants test
// skips that directory, and every name here is already in the committed config.
//
//	go run ./testdata/stub -addr 127.0.0.1:3000
//
// The config's probe URLs point at it with a path prefix, so one listener stands in for both
// services: /gitea/api/healthz and /jellyfin/health.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"net/http"
	"slices"
	"sync"
	"time"
)

// services are the upstreams this fixture can take up and down, in the order the fixture defines.
var services = []string{"gitea", "jellyfin"}

func main() {
	addr := flag.String("addr", "127.0.0.1:3000", "address to listen on")
	control := flag.String("control", "", "run the stub executor over this control directory")
	flag.Parse()

	if *control != "" {
		// The e2e run has no root and no systemd, so this stands in for meridian-host exec: it drains
		// the same inbox and writes the same results and audit lines.
		newExecutor(*control).start(context.Background())
	}

	server := &http.Server{Addr: *addr, Handler: newHandler(), ReadHeaderTimeout: 5 * time.Second}
	// A fixture, not the shipped binary: STANDARDS section 5's exit rules are about product code,
	// and a stub that cannot bind its port has to say so and stop.
	log.Fatal(server.ListenAndServe())
}

// stub is the upstream state. The zero value is every service up.
type stub struct {
	mu   sync.Mutex
	down map[string]bool
}

// newHandler is the whole route table: readiness, the two fixtures' probe endpoints, and the flip.
func newHandler() http.Handler {
	s := &stub{down: map[string]bool{}}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]bool{"ready": true})
	})
	mux.HandleFunc("GET /gitea/api/healthz", s.giteaHealth)
	mux.HandleFunc("GET /gitea/api/v1/version", s.giteaVersion)
	mux.HandleFunc("GET /jellyfin/health", s.jellyfinHealth)
	mux.HandleFunc("POST /control/{service}/{state}", s.flip)
	return mux
}

// giteaHealth answers the fixture's http_status probe: any 200 is healthy.
func (s *stub) giteaHealth(w http.ResponseWriter, _ *http.Request) {
	if s.isDown("gitea") {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// giteaVersion answers the fixture's json_field vital. It stays up when the health endpoint is
// down, because a real service can keep its API answering while health says no.
func (s *stub) giteaVersion(w http.ResponseWriter, _ *http.Request) {
	if s.isDown("gitea") {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"version": "1.22.0"})
}

// jellyfinHealth answers the fixture's http_json probe: 200 with a status that is not ok, so the
// tile goes down for a reason the page can name rather than for a refused connection.
func (s *stub) jellyfinHealth(w http.ResponseWriter, _ *http.Request) {
	status := "ok"
	if s.isDown("jellyfin") {
		status = "down"
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": status})
}

// flip takes a service up or down and answers with the state of both, so a caller can confirm the
// change landed instead of assuming it.
func (s *stub) flip(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("service")
	state := r.PathValue("state")
	if !slices.Contains(services, name) {
		http.Error(w, "no such service", http.StatusNotFound)
		return
	}
	if state != "up" && state != "down" {
		http.Error(w, "state is up or down", http.StatusBadRequest)
		return
	}

	s.mu.Lock()
	s.down[name] = state == "down"
	states := make(map[string]string, len(services))
	for _, service := range services {
		states[service] = "up"
		if s.down[service] {
			states[service] = "down"
		}
	}
	s.mu.Unlock()

	writeJSON(w, http.StatusOK, states)
}

func (s *stub) isDown(name string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.down[name]
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	// A write error means the client is gone, and a fixture has nobody to report it to.
	_ = json.NewEncoder(w).Encode(value)
}
