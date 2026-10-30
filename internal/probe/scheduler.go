package probe

import (
	"context"
	"io"
	"log"
	"reflect"
	"sync"
	"time"

	"github.com/autumnsgrove/meridian/internal/config"
)

// minimumPollInterval is the shortest wait the scheduler will take. Config validation requires at
// least five seconds, so this floor only stops a broken config from turning the box into a spin loop.
const minimumPollInterval = time.Second

// cycleTimeout bounds one service's whole cycle: its health probe and every vital together. A service
// slower than this is reported as a timeout rather than waited for (ARCHITECTURE.md "Polling model").
const cycleTimeout = config.MaxPollTimeout

// The stagger that keeps the start-up burst off one second: each service is delayed a little more
// than the last, up to a cap.
const (
	staggerStep = 150 * time.Millisecond
	staggerMax  = 2 * time.Second
)

// NextInterval is how long to wait before polling again: the active cadence while a viewer has been
// seen within poll.idle_after, the idle cadence after that (decision #35). A clock that went
// backwards counts as a viewer rather than as idleness, and the result is never zero or negative.
func NextInterval(lastTouch, now time.Time, poll config.Poll) time.Duration {
	wait := time.Duration(poll.Interval)
	if now.After(lastTouch) && now.Sub(lastTouch) > time.Duration(poll.IdleAfter) {
		wait = time.Duration(poll.IdleInterval)
	}
	if wait < minimumPollInterval {
		return minimumPollInterval
	}
	return wait
}

// Stagger is the delay before a service's first poll, by its position in the config.
func Stagger(index int) time.Duration {
	if index <= 0 {
		return 0
	}
	return min(time.Duration(index)*staggerStep, staggerMax)
}

// Clock is the scheduler's time seam: the tests replace it with a clock they advance themselves, so
// no test has to wait on the wall clock to prove that a backoff happened.
type Clock struct {
	Now   func() time.Time
	After func(time.Duration) <-chan time.Time
}

// Scheduler keeps every service's last result in a Store, one goroutine per service. It is safe for
// concurrent use: the gateway touches it on every page load while the pollers run.
type Scheduler struct {
	env   Env
	store *Store
	clock Clock
	log   *log.Logger

	mu          sync.Mutex
	parent      context.Context
	poll        config.Poll
	hostMaxAge  time.Duration
	runs        map[string]*poller
	lastTouch   time.Time
	lastRefresh time.Time
}

// poller is one service's goroutine and the timers the scheduler uses to nudge it.
type poller struct {
	id       string
	service  config.Service
	index    int
	nudge    chan struct{}
	want     bool
	cancel   context.CancelFunc
	finished chan struct{}
}

// NewScheduler builds a scheduler. A nil clock field falls back to the real clock, and a nil logger
// to a silent one, so a test can replace only the seam it cares about.
func NewScheduler(env Env, store *Store, clock Clock, logger *log.Logger) *Scheduler {
	if clock.Now == nil {
		clock.Now = time.Now
	}
	if clock.After == nil {
		clock.After = time.After
	}
	if logger == nil {
		logger = log.New(io.Discard, "", 0)
	}
	return &Scheduler{
		env:   env,
		store: store,
		clock: clock,
		log:   logger,
		runs:  make(map[string]*poller),
	}
}

// Start begins polling every enabled service in cfg, one goroutine each, and stops them all when ctx
// is cancelled.
func (scheduler *Scheduler) Start(ctx context.Context, cfg *config.Config) {
	scheduler.mu.Lock()
	defer scheduler.mu.Unlock()
	scheduler.parent = ctx
	scheduler.setConfigLocked(cfg)
	for index := range cfg.Services {
		scheduler.startLocked(ctx, cfg.Services[index], index)
	}
}

// Reload swaps the probe set for a new config. A service whose probes are unchanged keeps its
// goroutine and its last result; a changed or new one restarts; a removed one stops and its state is
// dropped. A disabled service is not polled, but its last result is kept for when it comes back.
func (scheduler *Scheduler) Reload(cfg *config.Config) {
	scheduler.mu.Lock()
	defer scheduler.mu.Unlock()

	scheduler.setConfigLocked(cfg)
	if scheduler.parent == nil {
		// Start has not run yet: remember the settings and let Start spawn everything.
		return
	}

	wanted := make(map[string]config.Service, len(cfg.Services))
	for index := range cfg.Services {
		wanted[cfg.Services[index].ID] = cfg.Services[index]
	}

	for id, poller := range scheduler.runs {
		service, kept := wanted[id]
		if kept && !service.Disabled && samePolling(poller.service, service) {
			poller.service = service
			continue
		}
		poller.cancel()
		delete(scheduler.runs, id)
	}

	for index := range cfg.Services {
		service := cfg.Services[index]
		if _, running := scheduler.runs[service.ID]; running {
			continue
		}
		scheduler.startLocked(scheduler.parent, service, index)
	}

	ids := make([]string, 0, len(cfg.Services))
	for index := range cfg.Services {
		ids = append(ids, cfg.Services[index].ID)
	}
	scheduler.store.Retain(ids)
}

// samePolling reports whether a reload left alone the parts of a service that decide what is asked
// and how often. A title or icon change is not a reason to restart a poller.
func samePolling(previous, next config.Service) bool {
	return previous.Disabled == next.Disabled && reflect.DeepEqual(previous.Probes, next.Probes)
}

// Touch records that a viewer exists right now. When the scheduler had backed off, it also asks for
// an immediate pass, so opening the page never shows a tile from minutes ago (decision #35).
func (scheduler *Scheduler) Touch() {
	scheduler.mu.Lock()
	now := scheduler.clock.Now()
	wasIdle := now.Sub(scheduler.lastTouch) > time.Duration(scheduler.poll.IdleAfter)
	scheduler.lastTouch = now
	scheduler.mu.Unlock()

	if wasIdle {
		scheduler.nudgeAll()
	}
}

// EnsureFresh asks for a pass on every service whose last result is older than maxAge, and waits
// until those passes finish or ctx expires. It is what the gateway calls on a page load.
func (scheduler *Scheduler) EnsureFresh(ctx context.Context, maxAge time.Duration) {
	now := scheduler.clock.Now()
	states := scheduler.store.Snapshot()

	scheduler.mu.Lock()
	waiting := make([]<-chan struct{}, 0, len(scheduler.runs))
	for id, poller := range scheduler.runs {
		if state, polled := states[id]; polled && now.Sub(state.CheckedAt) <= maxAge {
			continue
		}
		nudge(poller)
		waiting = append(waiting, poller.finished)
	}
	scheduler.mu.Unlock()

	for _, finished := range waiting {
		select {
		case <-finished:
		case <-ctx.Done():
			return
		}
	}
}

// Refresh asks every service for a pass now, unless one was asked for less than poll.refresh_min
// ago. It starts the passes and returns; use EnsureFresh to wait for them.
func (scheduler *Scheduler) Refresh(ctx context.Context) (started bool) {
	if ctx.Err() != nil {
		return false
	}

	scheduler.mu.Lock()
	now := scheduler.clock.Now()
	if !scheduler.lastRefresh.IsZero() && now.Sub(scheduler.lastRefresh) < time.Duration(scheduler.poll.RefreshMin) {
		scheduler.mu.Unlock()
		return false
	}
	scheduler.lastRefresh = now
	scheduler.mu.Unlock()

	scheduler.nudgeAll()
	return true
}

// startLocked spawns one service's goroutine. A disabled service is never polled: its status comes
// from the config, and asking a service the owner turned off buys nothing.
func (scheduler *Scheduler) startLocked(ctx context.Context, service config.Service, index int) {
	if service.Disabled {
		return
	}
	if _, running := scheduler.runs[service.ID]; running {
		return
	}

	//nolint:gosec // the cancel is kept by the poller so a reload can stop it
	runCtx, cancel := context.WithCancel(ctx)
	poller := &poller{
		id:       service.ID,
		service:  service,
		index:    index,
		nudge:    make(chan struct{}, 1),
		cancel:   cancel,
		finished: make(chan struct{}),
	}
	scheduler.runs[service.ID] = poller
	go scheduler.runLoop(runCtx, poller)
}

// runLoop polls once, then waits for the interval the last touch asks for, a nudge, or the run
// context ending.
func (scheduler *Scheduler) runLoop(ctx context.Context, poller *poller) {
	wait := Stagger(poller.index)
	for {
		if wait > 0 {
			select {
			case <-ctx.Done():
				scheduler.finishCycle(poller)
				return
			case <-poller.nudge:
			case <-scheduler.clock.After(wait):
			}
		}
		scheduler.beginCycle(poller)
		scheduler.pollOnce(ctx, poller)
		if scheduler.finishCycle(poller) {
			// A pass was asked for while this one ran, so it is not the answer to that request.
			wait = 0
			continue
		}
		wait = NextInterval(scheduler.lastTouchTime(), scheduler.clock.Now(), scheduler.pollSettings())
	}
}

// beginCycle marks the pass as started, which answers every pass wanted until now.
func (scheduler *Scheduler) beginCycle(poller *poller) {
	scheduler.mu.Lock()
	defer scheduler.mu.Unlock()
	poller.want = false
}

// pollOnce runs one service's whole cycle: health, then vitals only when it is healthy, then the
// merge into the store. A panic from a probe is contained here, so one broken service cannot take
// the page down.
func (scheduler *Scheduler) pollOnce(ctx context.Context, poller *poller) {
	env, poll := scheduler.envFor()
	defer func() {
		if recovered := recover(); recovered != nil {
			scheduler.log.Printf("probe panic: service %s: %v", poller.id, recovered)
			previous, _ := scheduler.store.state(poller.id)
			health := Health{Status: StatusDown, Code: CodeBadValue}
			merged := Merge(previous, health, nil, scheduler.clock.Now())
			scheduler.store.Update(poller.id, merged)
			scheduler.logTransition(poller, previous, merged)
		}
	}()

	cycleCtx, cancel := context.WithTimeout(ctx, cycleTimeout)
	defer cancel()

	// One memo per cycle, and it wraps the health probe too: a health probe and two vitals on the
	// same endpoint cost one request, and the next cycle starts empty.
	env.Fetch = memoFetcher(env.Fetch)

	health := runHealth(cycleCtx, env, poll, poller.service.Probes.Health)
	var vitals []VitalFresh
	if health.Status == StatusOK {
		vitals = runVitals(cycleCtx, env, poll, poller.service.Probes.Vitals)
	}
	if ctx.Err() != nil {
		// A cancelled cycle must not write a result that a newer config no longer asked for.
		return
	}
	previous, _ := scheduler.store.state(poller.id)
	merged := Merge(previous, health, vitals, scheduler.clock.Now())
	scheduler.store.Update(poller.id, merged)
	scheduler.logTransition(poller, previous, merged)
}

// logTransition writes the one log line a service's status change earns, and nothing when it did not
// change. Called after the store is updated so a slow logger cannot delay the next reader.
func (scheduler *Scheduler) logTransition(poller *poller, before, after State) {
	enabled := !poller.service.Disabled
	if line := TransitionLine(poller.id, Derive(before, enabled), Derive(after, enabled), after.Health.Code); line != "" {
		scheduler.log.Print(line)
	}
}

// finishCycle releases anyone waiting on this service's next result, and reports whether another
// pass was asked for while this one ran.
func (scheduler *Scheduler) finishCycle(poller *poller) bool {
	scheduler.mu.Lock()
	defer scheduler.mu.Unlock()
	close(poller.finished)
	poller.finished = make(chan struct{})
	return poller.want
}

func (scheduler *Scheduler) nudgeAll() {
	scheduler.mu.Lock()
	defer scheduler.mu.Unlock()
	for _, poller := range scheduler.runs {
		nudge(poller)
	}
}

// nudge asks a poller for a pass, and must be called with the scheduler's lock held. A pass that is
// already wanted is not asked for twice, so a burst of page loads costs one pass, not one per load.
func nudge(poller *poller) {
	poller.want = true
	select {
	case poller.nudge <- struct{}{}:
	default:
	}
}

func (scheduler *Scheduler) setConfigLocked(cfg *config.Config) {
	scheduler.poll = cfg.Poll
	scheduler.hostMaxAge = 0
	if cfg.HostStatus != nil {
		scheduler.hostMaxAge = time.Duration(cfg.HostStatus.MaxAge)
	}
}

func (scheduler *Scheduler) pollSettings() config.Poll {
	scheduler.mu.Lock()
	defer scheduler.mu.Unlock()
	return scheduler.poll
}

func (scheduler *Scheduler) lastTouchTime() time.Time {
	scheduler.mu.Lock()
	defer scheduler.mu.Unlock()
	return scheduler.lastTouch
}

// envFor fills the seams that come from config rather than from the caller: how old a host snapshot
// may be and which clock to judge it by.
func (scheduler *Scheduler) envFor() (Env, config.Poll) {
	scheduler.mu.Lock()
	defer scheduler.mu.Unlock()
	env := scheduler.env
	env.Now = scheduler.clock.Now
	env.HostMaxAge = scheduler.hostMaxAge
	return env, scheduler.poll
}

// runHealth runs one health probe inside the cycle's deadline.
func runHealth(ctx context.Context, env Env, poll config.Poll, spec config.Probe) Health {
	runner, found := HealthRunners[spec.Kind]
	if !found {
		return unknownHealth(CodeBadValue)
	}
	probeCtx, cancel := context.WithTimeout(ctx, time.Duration(poll.Timeout))
	defer cancel()
	return runner(probeCtx, env, spec)
}

// runVitals runs every vital of a healthy service, each inside its own deadline. A vital that fails
// is reported to the merge step as an error, never as a value.
func runVitals(ctx context.Context, env Env, poll config.Poll, specs []config.Probe) []VitalFresh {
	vitals := make([]VitalFresh, 0, len(specs))
	for index := range specs {
		spec := specs[index]
		runner, found := VitalRunners[spec.Kind]
		if !found {
			vitals = append(vitals, VitalFresh{Label: spec.Label, Err: ErrBadValue})
			continue
		}
		probeCtx, cancel := context.WithTimeout(ctx, time.Duration(poll.Timeout))
		value, err := runner(probeCtx, env, spec)
		cancel()
		vitals = append(vitals, VitalFresh{Label: spec.Label, Value: value, Err: err})
	}
	return vitals
}

// memoFetcher wraps the fetch seam for one cycle, tolerating a nil one so a self-only config needs
// no network at all.
func memoFetcher(fetch Fetcher) Fetcher {
	if fetch == nil {
		return nil
	}
	return Memo(fetch)
}
