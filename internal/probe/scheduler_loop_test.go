package probe_test

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/autumnsgrove/meridian/internal/config"
	"github.com/autumnsgrove/meridian/internal/probe"
)

// The two cadences the loop tests run under. "slow" parks its pollers two hours out, so a test can
// move the clock and make a state stale without waking the goroutines that would refresh it.
const (
	defaultPoll = `  interval: 15s
  idle_interval: 2m
  idle_after: 10m
  timeout: 2s
  refresh_min: 5s`

	slowPoll = `  interval: 15s
  idle_interval: 2h
  idle_after: 10m
  timeout: 2s
  refresh_min: 5s`
)

// fakeClock is the scheduler's clock seam: time moves only when the test advances it, and the tests
// read the timers the loop has parked on to see the cadence it chose.
type fakeClock struct {
	mu      sync.Mutex
	now     time.Time
	waiters []fakeWaiter
}

type fakeWaiter struct {
	at    time.Time
	timer chan time.Time
}

func newFakeClock(now time.Time) *fakeClock {
	return &fakeClock{now: now}
}

func (clock *fakeClock) seams() probe.Clock {
	return probe.Clock{Now: clock.Now, After: clock.After}
}

func (clock *fakeClock) Now() time.Time {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	return clock.now
}

func (clock *fakeClock) After(delay time.Duration) <-chan time.Time {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	timer := make(chan time.Time, 1)
	clock.waiters = append(clock.waiters, fakeWaiter{at: clock.now.Add(delay), timer: timer})
	return timer
}

// Advance moves time forward and fires every timer that is now due.
func (clock *fakeClock) Advance(delay time.Duration) {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	clock.now = clock.now.Add(delay)
	pending := clock.waiters[:0]
	for _, waiter := range clock.waiters {
		if waiter.at.After(clock.now) {
			pending = append(pending, waiter)
			continue
		}
		waiter.timer <- clock.now
	}
	clock.waiters = pending
}

// waiting is how many goroutines are parked on a timer, which is how a test knows the loop has
// settled before it advances the clock.
func (clock *fakeClock) waiting() int {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	return len(clock.waiters)
}

// latestWake is the timer registered most recently, which is the one the loop is actually parked
// on: a nudge makes a poller abandon an earlier timer, and that abandoned one stays registered here.
func (clock *fakeClock) latestWake() time.Time {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	if len(clock.waiters) == 0 {
		return time.Time{}
	}
	return clock.waiters[len(clock.waiters)-1].at
}

// eventually waits for something the scheduler's own goroutines do. It is driven by a real deadline
// and yields rather than sleeping, so it costs nothing when the condition is already true.
func eventually(t *testing.T, what string, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		runtime.Gosched()
	}
	t.Fatalf("timed out waiting for %s", what)
}

// testService is one httptest service and the number of requests that reached it.
type testService struct {
	url    string
	hits   atomic.Int64
	status atomic.Int64
	hang   bool
}

func newTestService(t *testing.T, status int) *testService {
	t.Helper()
	service := &testService{hang: status < 0}
	service.status.Store(int64(status))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		service.hits.Add(1)
		if service.hang {
			// Block until the probe's deadline cancels the request, which is what a slow service
			// does to a fetch client.
			<-request.Context().Done()
			return
		}
		w.WriteHeader(int(service.status.Load()))
		_, _ = w.Write([]byte("ok"))
	}))
	t.Cleanup(server.Close)
	service.url = server.URL
	return service
}

// serviceSpec renders one service whose health is an http_status probe against url.
func serviceSpec(id, url string) string {
	return fmt.Sprintf(`  - id: %s
    name: %s
    section: services
    icon: activity
    url: %s
    probes:
      health: {kind: http_status, url: "%s/health"}
`, id, id, url, url)
}

func schedulerConfig(t *testing.T, poll, services string) *config.Config {
	t.Helper()
	parsed, warnings, err := config.Parse([]byte(fmt.Sprintf(`
listen: 127.0.0.1:8891
server:
  allowed_hosts: [hub.example.org]
sections:
  - {id: services, title: Services}
poll:
%s
search:
  buttons:
    - label: Search
      url_template: https://search.example.org/?q={q}
services:
%s`, poll, services)))
	if err != nil {
		t.Fatalf("parse the scheduler config: %v", err)
	}
	if len(warnings) > 0 {
		t.Fatalf("the scheduler config should not warn: %v", warnings)
	}
	return parsed
}

func quietLogger() *log.Logger {
	return log.New(io.Discard, "", 0)
}

// syncBuffer collects log lines written from a poller goroutine.
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

// startScheduler runs a scheduler for the test's lifetime and returns it with its clock.
func startScheduler(t *testing.T, env probe.Env, cfg *config.Config, logger *log.Logger) (*probe.Scheduler, *probe.Store, *fakeClock) {
	t.Helper()
	store := &probe.Store{}
	clock := newFakeClock(polledAt)
	scheduler := probe.NewScheduler(env, store, clock.seams(), logger)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	scheduler.Start(ctx, cfg)
	return scheduler, store, clock
}

func TestScheduler_PollsEveryServiceAndStoresTheResult(t *testing.T) {
	up := newTestService(t, http.StatusOK)
	down := newTestService(t, http.StatusInternalServerError)
	cfg := schedulerConfig(t, defaultPoll, serviceSpec("up", up.url)+serviceSpec("down", down.url))
	_, store, clock := startScheduler(t, httpEnv(), cfg, quietLogger())

	// The second service waits out the stagger, so it parks on its timer before it polls.
	eventually(t, "both pollers to park", func() bool { return clock.waiting() >= 2 })
	if got := up.hits.Load(); got != 1 {
		t.Fatalf("the first service was polled %d times before the stagger, want 1", got)
	}

	clock.Advance(probe.Stagger(1))
	eventually(t, "both services to be polled", func() bool { return len(store.Snapshot()) == 2 })

	snapshot := store.Snapshot()
	if got := probe.Derive(snapshot["up"], true); got != probe.StatusOK {
		t.Fatalf("up derived %q, want %q", got, probe.StatusOK)
	}
	if got := probe.Derive(snapshot["down"], true); got != probe.StatusDown {
		t.Fatalf("down derived %q, want %q", got, probe.StatusDown)
	}
	if up.hits.Load() != 1 || down.hits.Load() != 1 {
		t.Fatalf("hits = %d and %d, want one each", up.hits.Load(), down.hits.Load())
	}
}

func TestScheduler_StaggersTheFirstPolls(t *testing.T) {
	first := newTestService(t, http.StatusOK)
	second := newTestService(t, http.StatusOK)
	third := newTestService(t, http.StatusOK)
	cfg := schedulerConfig(t, defaultPoll, serviceSpec("first", first.url)+serviceSpec("second", second.url)+serviceSpec("third", third.url))
	_, _, clock := startScheduler(t, httpEnv(), cfg, quietLogger())

	eventually(t, "every poller to park", func() bool { return clock.waiting() >= 3 })
	if got := []int64{first.hits.Load(), second.hits.Load(), third.hits.Load()}; got[0] != 1 || got[1] != 0 || got[2] != 0 {
		t.Fatalf("hits = %v at the start, want [1 0 0]", got)
	}

	clock.Advance(probe.Stagger(1))
	eventually(t, "the second service to be polled", func() bool { return second.hits.Load() == 1 })
	if got := third.hits.Load(); got != 0 {
		t.Fatalf("the third service was polled %d times before its stagger, want 0", got)
	}

	clock.Advance(probe.Stagger(2) - probe.Stagger(1))
	eventually(t, "the third service to be polled", func() bool { return third.hits.Load() == 1 })
}

func TestScheduler_BacksOffWhileNobodyWatches(t *testing.T) {
	service := newTestService(t, http.StatusOK)
	cfg := schedulerConfig(t, defaultPoll, serviceSpec("only", service.url))
	scheduler, _, clock := startScheduler(t, httpEnv(), cfg, quietLogger())

	eventually(t, "the first poll to park", func() bool { return clock.waiting() == 1 })
	if got, want := clock.latestWake(), polledAt.Add(2*time.Minute); !got.Equal(want) {
		t.Fatalf("next poll at %v, want the idle interval at %v", got, want)
	}

	scheduler.Touch()
	eventually(t, "the fresh pass after a touch", func() bool { return service.hits.Load() == 2 })
	eventually(t, "the poller to park on the active interval", func() bool {
		return clock.latestWake().Equal(polledAt.Add(15 * time.Second))
	})

	// A second touch while a viewer is already present does not ask for another pass.
	scheduler.Touch()
	if got, want := clock.latestWake(), polledAt.Add(15*time.Second); !got.Equal(want) {
		t.Fatalf("a second touch moved the next poll to %v, want %v", got, want)
	}
	if got := service.hits.Load(); got != 2 {
		t.Fatalf("a second touch polled %d times, want 2", got)
	}
}

func TestScheduler_EnsureFresh_ReturnsAtOnceWhenEverythingIsFresh(t *testing.T) {
	service := newTestService(t, http.StatusOK)
	cfg := schedulerConfig(t, slowPoll, serviceSpec("only", service.url))
	scheduler, _, clock := startScheduler(t, httpEnv(), cfg, quietLogger())
	eventually(t, "the first poll to park", func() bool { return clock.waiting() == 1 })

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	scheduler.EnsureFresh(ctx, time.Hour)

	if ctx.Err() != nil {
		t.Fatal("EnsureFresh waited although the only service was fresh")
	}
	if got := service.hits.Load(); got != 1 {
		t.Fatalf("the service was polled %d times, want 1", got)
	}
}

func TestScheduler_EnsureFresh_WaitsForAStaleService(t *testing.T) {
	service := newTestService(t, http.StatusOK)
	cfg := schedulerConfig(t, slowPoll, serviceSpec("only", service.url))
	scheduler, store, clock := startScheduler(t, httpEnv(), cfg, quietLogger())
	eventually(t, "the first poll to park", func() bool { return clock.waiting() == 1 })

	// Thirty minutes pass with the pollers parked two hours out, so the state is stale but the
	// loop has not refreshed it by itself.
	clock.Advance(30 * time.Minute)
	if got := store.Snapshot()["only"].CheckedAt; !got.Equal(polledAt) {
		t.Fatalf("checked at %v, want the first poll at %v", got, polledAt)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	scheduler.EnsureFresh(ctx, time.Minute)

	if ctx.Err() != nil {
		t.Fatal("EnsureFresh gave up waiting for the stale service")
	}
	if got := service.hits.Load(); got != 2 {
		t.Fatalf("the service was polled %d times, want 2", got)
	}
	if got, want := store.Snapshot()["only"].CheckedAt, polledAt.Add(30*time.Minute); !got.Equal(want) {
		t.Fatalf("checked at %v after EnsureFresh, want %v", got, want)
	}
}

func TestScheduler_RefreshIsRateLimited(t *testing.T) {
	service := newTestService(t, http.StatusOK)
	cfg := schedulerConfig(t, defaultPoll, serviceSpec("only", service.url))
	scheduler, _, clock := startScheduler(t, httpEnv(), cfg, quietLogger())
	eventually(t, "the first poll to park", func() bool { return clock.waiting() == 1 })

	ctx := context.Background()
	if !scheduler.Refresh(ctx) {
		t.Fatal("the first refresh was refused")
	}
	eventually(t, "the refreshed poll", func() bool { return service.hits.Load() == 2 })
	eventually(t, "the poller to park again", func() bool { return clock.waiting() == 1 })

	if scheduler.Refresh(ctx) {
		t.Fatal("a refresh inside refresh_min started another pass")
	}

	clock.Advance(5 * time.Second)
	if !scheduler.Refresh(ctx) {
		t.Fatal("a refresh after refresh_min was refused")
	}
	eventually(t, "the second refreshed poll", func() bool { return service.hits.Load() == 3 })

	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	clock.Advance(time.Minute)
	if scheduler.Refresh(cancelled) {
		t.Fatal("a refresh for a cancelled request started a pass")
	}
}

func TestScheduler_RecoversAPanickingProbe(t *testing.T) {
	good := newTestService(t, http.StatusOK)
	boom := newTestService(t, http.StatusOK)
	realFetcher := newFetcher()
	env := probe.Env{Fetch: func(ctx context.Context, request probe.Request) (probe.Response, error) {
		if request.URL == boom.url+"/health" {
			panic("the fetch seam exploded")
		}
		return realFetcher(ctx, request)
	}}
	cfg := schedulerConfig(t, slowPoll, serviceSpec("good", good.url)+serviceSpec("boom", boom.url))
	var logs syncBuffer
	scheduler, store, clock := startScheduler(t, env, cfg, log.New(&logs, "", 0))

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	scheduler.EnsureFresh(ctx, time.Hour)

	snapshot := store.Snapshot()
	if got := probe.Derive(snapshot["good"], true); got != probe.StatusOK {
		t.Fatalf("the healthy service derived %q, want %q", got, probe.StatusOK)
	}
	if state := snapshot["boom"]; state.Health.Status != probe.StatusDown || state.Health.Code != probe.CodeBadValue {
		t.Fatalf("the panicking service is %+v, want down with %q", state.Health, probe.CodeBadValue)
	}
	if !strings.Contains(logs.String(), "boom") {
		t.Fatalf("the panic was not logged with the service id: %q", logs.String())
	}

	// The goroutine survived: it polls again on the next pass.
	clock.Advance(10 * time.Second)
	if !scheduler.Refresh(ctx) {
		t.Fatal("a refresh after the panic was refused")
	}
	scheduler.EnsureFresh(ctx, 0)
	if got := good.hits.Load(); got != 2 {
		t.Fatalf("the healthy service was polled %d times after the panic, want 2", got)
	}
}

func TestScheduler_StopsEveryGoroutineWhenTheContextEnds(t *testing.T) {
	var specs strings.Builder
	for index := range 5 {
		specs.WriteString(serviceSpec(fmt.Sprintf("service-%d", index), fmt.Sprintf("http://127.0.0.1:1/%d", index)))
	}
	cfg := schedulerConfig(t, slowPoll, specs.String())
	// No sockets here: the fetch seam answers for every service, so the only goroutines in play are
	// the ones the scheduler starts.
	env := probe.Env{Fetch: func(context.Context, probe.Request) (probe.Response, error) {
		return probe.Response{Status: http.StatusOK, Body: []byte("ok")}, nil
	}}

	baseline := runtime.NumGoroutine()
	store := &probe.Store{}
	clock := newFakeClock(polledAt)
	scheduler := probe.NewScheduler(env, store, clock.seams(), quietLogger())
	ctx, cancel := context.WithCancel(context.Background())
	scheduler.Start(ctx, cfg)

	ensureCtx, ensureCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer ensureCancel()
	scheduler.EnsureFresh(ensureCtx, 0)
	if got := len(store.Snapshot()); got != 5 {
		t.Fatalf("%d services polled, want 5", got)
	}
	cancel()

	eventually(t, "the pollers to stop", func() bool { return runtime.NumGoroutine() <= baseline })
}

func TestScheduler_DoesNotPollADisabledService(t *testing.T) {
	service := newTestService(t, http.StatusOK)
	cfg := schedulerConfig(t, slowPoll, fmt.Sprintf(`  - id: off
    name: Off
    section: services
    icon: activity
    url: %s
    disabled: true
    probes:
      health: {kind: http_status, url: "%s/health"}
`, service.url, service.url))
	scheduler, store, _ := startScheduler(t, httpEnv(), cfg, quietLogger())

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	scheduler.EnsureFresh(ctx, 0)

	if got := service.hits.Load(); got != 0 {
		t.Fatalf("a disabled service was polled %d times, want 0", got)
	}
	if state, polled := store.Snapshot()["off"]; polled {
		t.Fatalf("a disabled service has state %+v, want none", state)
	}
}

func TestScheduler_ReloadKeepsUnchangedServicesAndDropsRemoved(t *testing.T) {
	stable := newTestService(t, http.StatusOK)
	removed := newTestService(t, http.StatusOK)
	added := newTestService(t, http.StatusOK)
	cfg := schedulerConfig(t, slowPoll, serviceSpec("stable", stable.url)+serviceSpec("removed", removed.url))
	scheduler, store, clock := startScheduler(t, httpEnv(), cfg, quietLogger())
	eventually(t, "the pollers to park", func() bool { return clock.waiting() >= 2 })
	clock.Advance(probe.Stagger(1))
	eventually(t, "both services to be polled", func() bool { return len(store.Snapshot()) == 2 })
	spot := store.Snapshot()["stable"].CheckedAt
	before := stable.hits.Load()

	scheduler.Reload(schedulerConfig(t, slowPoll, serviceSpec("stable", stable.url)+serviceSpec("added", added.url)))

	if _, found := store.Snapshot()["removed"]; found {
		t.Fatal("the removed service kept its state")
	}
	// The new service parks on its stagger, then polls. That is the moment the unchanged one would
	// also have been polled again if the reload had restarted it.
	addedWake := clock.Now().Add(probe.Stagger(1))
	eventually(t, "the added poller to park on its stagger", func() bool {
		return clock.latestWake().Equal(addedWake)
	})
	clock.Advance(probe.Stagger(1))
	eventually(t, "the added service to be polled", func() bool { return added.hits.Load() == 1 })
	if got := stable.hits.Load(); got != before {
		t.Fatalf("the unchanged service was polled %d times by the reload, want %d", got, before)
	}
	if got := store.Snapshot()["stable"].CheckedAt; !got.Equal(spot) {
		t.Fatalf("the unchanged service was checked at %v, want its first poll at %v", got, spot)
	}
}

func TestScheduler_ReloadRestartsAChangedProbe(t *testing.T) {
	first := newTestService(t, http.StatusOK)
	second := newTestService(t, http.StatusOK)
	cfg := schedulerConfig(t, slowPoll, serviceSpec("moved", first.url))
	scheduler, store, clock := startScheduler(t, httpEnv(), cfg, quietLogger())
	eventually(t, "the poller to park", func() bool { return clock.waiting() == 1 })
	if _, polled := store.Snapshot()["moved"]; !polled {
		t.Fatal("the service was not polled before the reload")
	}
	before := first.hits.Load()

	scheduler.Reload(schedulerConfig(t, slowPoll, serviceSpec("moved", second.url)))

	eventually(t, "the new probe target to be polled", func() bool { return second.hits.Load() == 1 })
	if got := first.hits.Load(); got != before {
		t.Fatalf("the old target was polled %d times, want %d", got, before)
	}
}
