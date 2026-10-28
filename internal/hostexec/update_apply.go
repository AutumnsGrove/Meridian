package hostexec

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/autumnsgrove/meridian/internal/atomicfile"
	"github.com/autumnsgrove/meridian/internal/wire"
)

// One self update's budgets. The executor's default action timeout is 600s and the control unit's
// TimeoutStartSec is 660s; 180 + 90 + 120 + 90 is 480, which leaves the rollback room inside the
// executor's deadline even when every earlier step uses its whole budget.
const (
	pullTimeout          = 180 * time.Second
	upTimeout            = 90 * time.Second
	healthWindow         = 120 * time.Second
	healthInterval       = 2 * time.Second
	healthRequestTimeout = 5 * time.Second
	rollbackTimeout      = 90 * time.Second
	maxEnvSize           = 64 << 10
)

// Updater is one `meridian-host update` run. It is invoked by the executor as the self target's
// update command, runs as the deploy user, and is the only code that pulls or recreates Meridian.
type Updater struct {
	// Target is the allowlist entry whose update.kind is self.
	Target Target
	// DeployUser is the account the compose commands run as (decision #31).
	DeployUser string
	// Run is the command seam. Main supplies Runner; tests supply a fake.
	Run Commander
	// Now and Sleep are the health window's clock. Nil uses the wall clock.
	Now   func() time.Time
	Sleep func(ctx context.Context, delay time.Duration) error
	// HealthURL overrides the loopback health endpoint; empty builds one from the target's port.
	HealthURL string
	// HTTP is the health client; nil gets the bounded, no-redirect default.
	HTTP *http.Client
	// Log receives progress lines. Nil is silent.
	Log *log.Logger
}

// Apply pins the digest the container resolved, pulls and recreates the container, and waits for its
// health endpoint. A failure at any step restores the previous .env byte for byte and brings the old
// container back, because a half-applied update is worse than a failed one.
func (updater *Updater) Apply(ctx context.Context) error {
	settings := updater.Target.Update
	if settings == nil || settings.Kind != UpdateSelf {
		return errors.New("hostexec: update: the target is not a self update")
	}
	if updater.Run == nil {
		return errors.New("hostexec: update: no command runner")
	}

	signal, err := ReadUpdateSignal(settings.SignalFile)
	if err != nil {
		return err
	}
	ref, err := ImageRef(wire.ImageRepo(settings.Image), signal)
	if err != nil {
		return err
	}

	envPath := filepath.Join(settings.Dir, ".env")
	previous, mode, err := readEnvFile(envPath)
	if err != nil {
		return err
	}
	updated, err := SetVar(string(previous), "MERIDIAN_IMAGE", ref)
	if err != nil {
		return err
	}
	if err := atomicfile.Write(settings.Dir, ".env", []byte(updated), mode); err != nil {
		return fmt.Errorf("hostexec: update: pin %s: %w", ref, err)
	}
	updater.logf("pinned %s", ref)

	if err := updater.compose(ctx, pullTimeout, "pull"); err != nil {
		return updater.rollback(ctx, previous, mode, "pull", err)
	}
	if err := updater.compose(ctx, upTimeout, "up", "-d"); err != nil {
		return updater.rollback(ctx, previous, mode, "recreate", err)
	}
	if err := updater.awaitHealthy(ctx); err != nil {
		return updater.rollback(ctx, previous, mode, "health", err)
	}
	updater.logf("running %s", ref)
	return nil
}

// compose runs one docker compose verb for the allowlisted project and service. The argv is built
// only from the allowlist; the request contributes nothing.
func (updater *Updater) compose(ctx context.Context, timeout time.Duration, verb string, extra ...string) error {
	settings := updater.Target.Update
	argv := []string{"docker", "compose", "--project-directory", settings.Dir, verb}
	argv = append(argv, extra...)
	argv = append(argv, settings.Service)

	actionCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	code, output, err := updater.Run(actionCtx, Command{
		Argv: argv, RunAs: updater.DeployUser, Dir: settings.Dir, Timeout: timeout, NoNewGroup: true,
	})
	switch {
	case err != nil:
		return fmt.Errorf("%s: %w", verb, err)
	case code != 0:
		return fmt.Errorf("%s exited %d: %s", verb, code, strings.TrimSpace(output))
	default:
		return nil
	}
}

// awaitHealthy polls the loopback health endpoint until it answers 200 or the window closes.
func (updater *Updater) awaitHealthy(ctx context.Context) error {
	client := updater.HTTP
	if client == nil {
		client = newHealthClient()
	}
	deadline := updater.now().Add(healthWindow)
	for {
		last := updater.checkHealth(ctx, client)
		if last == nil {
			return nil
		}
		if !updater.now().Before(deadline) {
			return fmt.Errorf("not healthy after %s: %w", healthWindow, last)
		}
		delay := healthInterval
		if remaining := deadline.Sub(updater.now()); delay > remaining {
			delay = remaining
		}
		if err := updater.sleep(ctx, delay); err != nil {
			return err
		}
	}
}

// checkHealth is one GET against /healthz. Anything but 200 is the reason the poll keeps going.
func (updater *Updater) checkHealth(ctx context.Context, client *http.Client) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, updater.healthURL(), http.NoBody)
	if err != nil {
		return err
	}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4<<10))
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("health answered %d", response.StatusCode)
	}
	return nil
}

// rollback restores the previous .env and recreates the container from it. It deliberately detaches
// from the update's cancellation: the reason to roll back is usually that the update ran out of time.
func (updater *Updater) rollback(ctx context.Context, previous []byte, mode fs.FileMode, step string, cause error) error {
	rollbackCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), rollbackTimeout)
	defer cancel()

	updater.logf("%s failed (%v); restoring the previous .env", step, cause)
	if err := atomicfile.Write(updater.Target.Update.Dir, ".env", previous, mode); err != nil {
		return fmt.Errorf("%s failed (%w); the previous .env could not be restored: %w", step, cause, err)
	}
	if err := updater.compose(rollbackCtx, rollbackTimeout, "up", "-d"); err != nil {
		return fmt.Errorf("%s failed (%w); the previous .env is back but the container did not come back: %w", step, cause, err)
	}
	return fmt.Errorf("%s failed: %w", step, cause)
}

// healthURL is the loopback endpoint the allowlist's port names.
func (updater *Updater) healthURL() string {
	if updater.HealthURL != "" {
		return updater.HealthURL
	}
	return fmt.Sprintf("http://127.0.0.1:%d/healthz", updater.Target.Update.Port)
}

// newHealthClient is the one client the health poll uses: no environment proxy (the endpoint is
// loopback), no redirects, and a timeout short enough to fit several attempts in the window.
func newHealthClient() *http.Client {
	return &http.Client{
		Timeout:   healthRequestTimeout,
		Transport: &http.Transport{Proxy: nil},
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// readEnvFile reads .env without following a symlink or buffering an arbitrary document, preserving
// the mode the rollback restores.
func readEnvFile(path string) ([]byte, fs.FileMode, error) {
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0) //nolint:gosec // path is built from the root-owned allowlist
	if err != nil {
		return nil, 0, fmt.Errorf("hostexec: update: read .env: %w", err)
	}
	defer func() { _ = file.Close() }()

	info, err := file.Stat()
	if err != nil {
		return nil, 0, fmt.Errorf("hostexec: update: stat .env: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, 0, errors.New("hostexec: update: .env is not a regular file")
	}
	if info.Size() > maxEnvSize {
		return nil, 0, fmt.Errorf("hostexec: update: .env is %d bytes, larger than %d", info.Size(), maxEnvSize)
	}
	data, err := io.ReadAll(io.LimitReader(file, maxEnvSize+1))
	if err != nil {
		return nil, 0, fmt.Errorf("hostexec: update: read .env: %w", err)
	}
	if len(data) > maxEnvSize {
		return nil, 0, fmt.Errorf("hostexec: update: .env grew past %d bytes", maxEnvSize)
	}
	return data, info.Mode().Perm(), nil
}

// now is the health window's clock.
func (updater *Updater) now() time.Time {
	if updater.Now == nil {
		return time.Now()
	}
	return updater.Now()
}

// sleep pauses between health attempts, or returns the context's error.
func (updater *Updater) sleep(ctx context.Context, delay time.Duration) error {
	if updater.Sleep != nil {
		return updater.Sleep(ctx, delay)
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (updater *Updater) logf(format string, arguments ...any) {
	if updater.Log != nil {
		updater.Log.Printf(format, arguments...)
	}
}
