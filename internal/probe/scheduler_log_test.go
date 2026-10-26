package probe_test

import (
	"context"
	"log"
	"strings"
	"testing"
	"time"
)

// TestScheduler_LogsEachStatusChangeOnce is what makes the log useful after the fact: one line when a
// service changes state, and nothing while it stays the same.
func TestScheduler_LogsEachStatusChangeOnce(t *testing.T) {
	down := newTestService(t, 500)
	cfg := schedulerConfig(t, slowPoll, serviceSpec("svc", down.url))
	var logs syncBuffer
	scheduler, _, _ := startScheduler(t, httpEnv(), cfg, log.New(&logs, "", 0))

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	eventually(t, "the transition to be logged", func() bool {
		return strings.Contains(logs.String(), "svc: unknown -> down")
	})

	changes := strings.Count(logs.String(), "svc: unknown -> down")
	scheduler.Refresh(ctx)
	scheduler.EnsureFresh(ctx, 0)

	if got := strings.Count(logs.String(), "svc: unknown -> down"); got != changes {
		t.Fatalf("the same status change was logged %d times, want %d: %q", got, changes, logs.String())
	}
	if !strings.Contains(logs.String(), "svc: unknown -> down") {
		t.Fatalf("the log does not name the service and its states: %q", logs.String())
	}
}

// TestScheduler_DoesNotLogWhileNothingChanges keeps the log quiet for a service that stays healthy.
func TestScheduler_DoesNotLogWhileNothingChanges(t *testing.T) {
	up := newTestService(t, 200)
	cfg := schedulerConfig(t, slowPoll, serviceSpec("steady", up.url))
	var logs syncBuffer
	scheduler, _, _ := startScheduler(t, httpEnv(), cfg, log.New(&logs, "", 0))

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	scheduler.EnsureFresh(ctx, time.Hour)

	// One line for unknown -> ok, then nothing for a second identical poll.
	first := strings.Count(logs.String(), "steady:")
	scheduler.Refresh(ctx)
	scheduler.EnsureFresh(ctx, 0)
	if got := strings.Count(logs.String(), "steady:"); got != first {
		t.Fatalf("a steady service logged again: %q", logs.String())
	}
}
