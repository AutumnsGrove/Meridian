package gateway

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/autumnsgrove/meridian/internal/config"
)

// TestServe_ADrainThatExpiresIsNotAnError covers the shutdown path the rest of the suite only ever
// exercises when nothing is in flight. http.Server.Shutdown refuses to close a StateNew connection
// younger than five seconds (golang/go#22682), which is exactly the whole drain budget, so any peer
// that dials and stays quiet — a healthcheck, a browser preconnect, a half-open socket over the
// tailnet — makes the drain expire. The process is going down either way, so that is a slow
// shutdown, not a failed one; returning the deadline turned a clean restart into a non-zero exit and
// an error line that reads like a crash (decision #102).
func TestServe_ADrainThatExpiresIsNotAnError(t *testing.T) {
	previous := shutdownDrain
	shutdownDrain = 50 * time.Millisecond
	t.Cleanup(func() { shutdownDrain = previous })

	address := freeAddress(t)
	deps := Deps{
		Current: func() (*config.Config, LoadStatus) {
			return &config.Config{Listen: address}, LoadStatus{}
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	served := make(chan error, 1)
	go func() { served <- Serve(ctx, deps) }()

	// Dial until it answers, then hold the last connection open without writing a byte: accepted and
	// still StateNew is the state the drain cannot close.
	var dialer net.Dialer
	var silent net.Conn
	for deadline := time.Now().Add(2 * time.Second); silent == nil && time.Now().Before(deadline); {
		conn, err := dialer.DialContext(ctx, "tcp", address)
		if err != nil {
			time.Sleep(10 * time.Millisecond)
			continue
		}
		silent = conn
	}
	if silent == nil {
		t.Fatal("the server never started listening")
	}
	t.Cleanup(func() { _ = silent.Close() })

	cancel()

	select {
	case err := <-served:
		if err != nil {
			t.Fatalf("Serve() = %v after the drain expired, want nil: a slow shutdown is not a failed one", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Serve() did not return after its context was cancelled")
	}
}

// freeAddress picks a loopback port that is free right now.
func freeAddress(t *testing.T) string {
	t.Helper()
	var listenConfig net.ListenConfig
	listener, err := listenConfig.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve an address: %v", err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("release the reserved address: %v", err)
	}
	return address
}
