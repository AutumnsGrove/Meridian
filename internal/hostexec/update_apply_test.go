package hostexec_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/autumnsgrove/meridian/internal/hostexec"
)

// scriptedRunner is the fake Commander: it records every command, fails the verbs a test scripts, and
// succeeds otherwise. It is a few lines because the executor's seam is a function, not an interface.
type scriptedRunner struct {
	calls      []hostexec.Command
	pullFails  bool
	upFailures int
}

func (runner *scriptedRunner) run(_ context.Context, command hostexec.Command) (code int, output string, err error) {
	runner.calls = append(runner.calls, command)
	verb := ""
	if len(command.Argv) > 4 {
		verb = command.Argv[4]
	}
	switch {
	case verb == "pull" && runner.pullFails:
		return 1, "pull failed", nil
	case verb == "up" && runner.upFailures > 0:
		runner.upFailures--
		return 1, "up failed", nil
	default:
		return 0, "", nil
	}
}

func (runner *scriptedRunner) verbs(verb string) int {
	count := 0
	for _, call := range runner.calls {
		if len(call.Argv) > 4 && call.Argv[4] == verb {
			count++
		}
	}
	return count
}

// applyClock is the health window's clock: sleeps move time instead of waiting.
type applyClock struct {
	now    time.Time
	sleeps int
}

func (clock *applyClock) Now() time.Time {
	return clock.now
}

func (clock *applyClock) Sleep(ctx context.Context, delay time.Duration) error {
	clock.sleeps++
	clock.now = clock.now.Add(delay)
	return ctx.Err()
}

func newApplyClock() *applyClock {
	return &applyClock{now: time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)}
}

func healthServer(t *testing.T, statuses ...int) (*httptest.Server, *atomic.Int64) {
	t.Helper()
	hits := &atomic.Int64{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		index := int(hits.Add(1)) - 1
		status := statuses[len(statuses)-1]
		if index < len(statuses) {
			status = statuses[index]
		}
		w.WriteHeader(status)
	}))
	t.Cleanup(server.Close)
	return server, hits
}

// applyFixture is one updater over a temporary compose directory, a real signal file and a real .env.
type applyFixture struct {
	dir      string
	signal   string
	previous string
	runner   *scriptedRunner
	clock    *applyClock
	hits     *atomic.Int64
	updater  *hostexec.Updater
}

func newApplyFixture(t *testing.T, healthStatuses ...int) *applyFixture {
	t.Helper()
	dir := t.TempDir()
	signalDir := filepath.Join(dir, "update-signal")
	if err := os.Mkdir(signalDir, 0o700); err != nil {
		t.Fatalf("create the signal directory: %v", err)
	}
	signal := filepath.Join(signalDir, "requested")
	if err := os.WriteFile(signal, []byte(digestA), 0o600); err != nil {
		t.Fatalf("write the signal: %v", err)
	}
	previous := "# keep this comment\nOTHER=\"quoted\"\nMERIDIAN_IMAGE=" + image + "@" + digestB + "\nTAIL=2\n"
	//nolint:gosec // the test asserts the updater preserves this mode rather than forcing 0600
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(previous), 0o640); err != nil {
		t.Fatalf("write .env: %v", err)
	}

	server, hits := healthServer(t, healthStatuses...)
	runner := &scriptedRunner{}
	clock := newApplyClock()
	return &applyFixture{
		dir:      dir,
		signal:   signal,
		previous: previous,
		runner:   runner,
		clock:    clock,
		hits:     hits,
		updater: &hostexec.Updater{
			Target: hostexec.Target{
				Kind: hostexec.KindSystemd, Unit: "meridian.service",
				Actions: []string{"restart", "update"}, Criticality: hostexec.CriticalityHigh,
				ResultFirst: true,
				Update: &hostexec.Update{
					Kind: hostexec.UpdateSelf, Image: image, Dir: dir, Service: "meridian",
					Port: 8891, SignalFile: signal,
				},
			},
			DeployUser: deployUser,
			Run:        runner.run,
			Now:        clock.Now,
			Sleep:      clock.Sleep,
			HealthURL:  server.URL,
		},
	}
}

func (fixture *applyFixture) env(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(fixture.dir, ".env"))
	if err != nil {
		t.Fatalf("read .env: %v", err)
	}
	return string(data)
}

func TestUpdater_AppliesTheDigestAndRecreates(t *testing.T) {
	fixture := newApplyFixture(t, http.StatusOK)

	if err := fixture.updater.Apply(context.Background()); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	want := strings.Replace(fixture.previous,
		image+"@"+digestB, image+"@"+digestA, 1)
	if got := fixture.env(t); got != want {
		t.Fatalf(".env =\n%q\nwant\n%q", got, want)
	}
	if got := fixture.runner.verbs("pull"); got != 1 {
		t.Errorf("pull commands = %d, want 1", got)
	}
	if got := fixture.runner.verbs("up"); got != 1 {
		t.Errorf("up commands = %d, want 1", got)
	}
	if got := fixture.hits.Load(); got < 1 {
		t.Errorf("health checks = %d, want at least 1", got)
	}
	for index, call := range fixture.runner.calls {
		if call.RunAs != deployUser {
			t.Errorf("call %d runs as %q, want the deploy user", index, call.RunAs)
		}
		if call.Dir != fixture.dir {
			t.Errorf("call %d dir = %q, want %q", index, call.Dir, fixture.dir)
		}
		if call.Timeout <= 0 {
			t.Errorf("call %d has no timeout", index)
		}
	}
	info, err := os.Stat(filepath.Join(fixture.dir, ".env"))
	if err != nil {
		t.Fatalf("stat .env: %v", err)
	}
	if got, want := info.Mode().Perm(), os.FileMode(0o640); got != want {
		t.Errorf(".env mode = %o, want %o", got, want)
	}
}

func TestUpdater_RollsBackWhenTheNewContainerIsUnhealthy(t *testing.T) {
	fixture := newApplyFixture(t, http.StatusInternalServerError)

	err := fixture.updater.Apply(context.Background())
	if err == nil {
		t.Fatal("Apply accepted a container that never became healthy")
	}
	if !strings.Contains(err.Error(), "not healthy") {
		t.Errorf("error = %v, want it to name the health check", err)
	}
	if got := fixture.env(t); got != fixture.previous {
		t.Fatalf(".env =\n%q\nwant the previous contents\n%q", got, fixture.previous)
	}
	if got := fixture.runner.verbs("up"); got != 2 {
		t.Errorf("up commands = %d, want the recreate and the rollback", got)
	}
	if fixture.clock.sleeps == 0 {
		t.Error("the health poll gave up without waiting")
	}
}

func TestUpdater_RollsBackWhenThePullFails(t *testing.T) {
	fixture := newApplyFixture(t, http.StatusOK)
	fixture.runner.pullFails = true

	err := fixture.updater.Apply(context.Background())
	if err == nil {
		t.Fatal("Apply accepted a failed pull")
	}
	if !strings.Contains(err.Error(), "pull") {
		t.Errorf("error = %v, want it to name the pull", err)
	}
	if got := fixture.env(t); got != fixture.previous {
		t.Errorf(".env was not restored after a failed pull")
	}
	if got := fixture.runner.verbs("up"); got != 1 {
		t.Errorf("up commands = %d, want one rollback up", got)
	}
}

func TestUpdater_RollsBackWhenTheRecreateFails(t *testing.T) {
	fixture := newApplyFixture(t, http.StatusOK)
	fixture.runner.upFailures = 1

	err := fixture.updater.Apply(context.Background())
	if err == nil {
		t.Fatal("Apply accepted a failed recreate")
	}
	if got := fixture.env(t); got != fixture.previous {
		t.Errorf(".env was not restored after a failed recreate")
	}
	if got := fixture.runner.verbs("up"); got != 2 {
		t.Errorf("up commands = %d, want the failed recreate and the rollback", got)
	}
}

func TestUpdater_ReportsWhenTheRollbackItselfFails(t *testing.T) {
	fixture := newApplyFixture(t, http.StatusOK)
	fixture.runner.upFailures = 2

	err := fixture.updater.Apply(context.Background())
	if err == nil {
		t.Fatal("Apply accepted a failed recreate and a failed rollback")
	}
	if !strings.Contains(err.Error(), "did not come back") {
		t.Errorf("error = %v, want it to say the rollback failed too", err)
	}
	if got := fixture.env(t); got != fixture.previous {
		t.Errorf(".env was not restored before the failed rollback")
	}
}

func TestUpdater_WaitsForTheFirstHealthyAnswer(t *testing.T) {
	fixture := newApplyFixture(t,
		http.StatusInternalServerError, http.StatusServiceUnavailable, http.StatusOK)

	if err := fixture.updater.Apply(context.Background()); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if got := fixture.hits.Load(); got != 3 {
		t.Errorf("health checks = %d, want 3", got)
	}
	if got := fixture.clock.sleeps; got != 2 {
		t.Errorf("sleeps = %d, want 2", got)
	}
	if got := fixture.env(t); !strings.Contains(got, image+"@"+digestA) {
		t.Errorf(".env does not carry the new digest: %q", got)
	}
}

func TestUpdater_RefusesBadInputWithoutTouchingTheEnvironment(t *testing.T) {
	tests := []struct {
		name   string
		change func(t *testing.T, fixture *applyFixture)
	}{
		{"a tag in the signal", func(t *testing.T, fixture *applyFixture) {
			if err := os.WriteFile(fixture.signal, []byte("latest"), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{"a newline in the signal", func(t *testing.T, fixture *applyFixture) {
			if err := os.WriteFile(fixture.signal, []byte(digestA+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{"a missing signal", func(t *testing.T, fixture *applyFixture) {
			if err := os.Remove(fixture.signal); err != nil {
				t.Fatal(err)
			}
		}},
		{"a missing .env", func(t *testing.T, fixture *applyFixture) {
			if err := os.Remove(filepath.Join(fixture.dir, ".env")); err != nil {
				t.Fatal(err)
			}
		}},
		{"a duplicate key in .env", func(t *testing.T, fixture *applyFixture) {
			duplicate := "MERIDIAN_IMAGE=one\nMERIDIAN_IMAGE=two\n"
			if err := os.WriteFile(filepath.Join(fixture.dir, ".env"), []byte(duplicate), 0o600); err != nil {
				t.Fatal(err)
			}
			fixture.previous = duplicate
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newApplyFixture(t, http.StatusOK)
			test.change(t, fixture)

			if err := fixture.updater.Apply(context.Background()); err == nil {
				t.Fatal("Apply accepted bad input")
			}
			if len(fixture.runner.calls) != 0 {
				t.Errorf("commands ran after bad input: %v", fixture.runner.calls)
			}
			if _, statErr := os.Stat(filepath.Join(fixture.dir, ".env")); errors.Is(statErr, os.ErrNotExist) {
				return
			}
			if got := fixture.env(t); got != fixture.previous {
				t.Errorf(".env changed: %q, want %q", got, fixture.previous)
			}
		})
	}
}

func TestUpdater_RefusesATargetThatIsNotASelfUpdate(t *testing.T) {
	fixture := newApplyFixture(t, http.StatusOK)
	fixture.updater.Target.Update.Kind = hostexec.UpdateDocker

	if err := fixture.updater.Apply(context.Background()); err == nil {
		t.Fatal("Apply accepted a non-self target")
	}
	if len(fixture.runner.calls) != 0 {
		t.Errorf("commands ran for a non-self target: %v", fixture.runner.calls)
	}
}

func TestSelfUpdateTarget_FindsExactlyOne(t *testing.T) {
	targets := hostexec.Targets{
		"app":      {Update: &hostexec.Update{Kind: hostexec.UpdateDocker}},
		"meridian": {Update: &hostexec.Update{Kind: hostexec.UpdateSelf, Image: image}},
	}
	target, err := hostexec.SelfUpdateTarget(targets)
	if err != nil {
		t.Fatalf("SelfUpdateTarget: %v", err)
	}
	if target.Update == nil || target.Update.Kind != hostexec.UpdateSelf || target.Update.Image != image {
		t.Errorf("target = %+v, want the self update", target)
	}
}

func TestSelfUpdateTarget_RefusesNoneOrSeveral(t *testing.T) {
	none := hostexec.Targets{"app": {Update: &hostexec.Update{Kind: hostexec.UpdateDocker}}}
	if _, err := hostexec.SelfUpdateTarget(none); err == nil {
		t.Error("SelfUpdateTarget accepted an allowlist with no self update")
	}
	several := hostexec.Targets{
		"one": {Update: &hostexec.Update{Kind: hostexec.UpdateSelf}},
		"two": {Update: &hostexec.Update{Kind: hostexec.UpdateSelf}},
	}
	if _, err := hostexec.SelfUpdateTarget(several); err == nil {
		t.Error("SelfUpdateTarget accepted two self updates")
	}
}
