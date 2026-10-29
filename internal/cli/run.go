package cli

import (
	"context"
	"errors"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	clog "github.com/charmbracelet/log"
	"github.com/spf13/cobra"

	"github.com/autumnsgrove/meridian/internal/config"
	"github.com/autumnsgrove/meridian/internal/control"
	"github.com/autumnsgrove/meridian/internal/gateway"
	"github.com/autumnsgrove/meridian/internal/probe"
	"github.com/autumnsgrove/meridian/internal/updater"
	"github.com/autumnsgrove/meridian/internal/wire"
)

const (
	// watchInterval is how often --watch looks at the config file's mtime. A reload is cheap, but a
	// two second poll is cheap enough that it does not need to be cleverer.
	watchInterval = 2 * time.Second
	// probeUserAgent is what probe requests say; services see it in their access log.
	probeUserAgent = "meridian"
	// maxProbeBody caps how much of a probe response is read (docs/STANDARDS.md section 4).
	maxProbeBody = 1 << 20
	// bannerProblems is how many rejected-config problems reach the page banner; the log has them all.
	bannerProblems = 3
)

// RunOptions are what `meridian run` was asked for.
type RunOptions struct {
	// ConfigPath is the --config flag; empty falls back to MERIDIAN_CONFIG and then the image default.
	ConfigPath string
	// Watch reloads when the file changes, in addition to SIGHUP.
	Watch bool
}

// Run serves the dashboard until ctx is cancelled or a shutdown signal arrives. A SIGHUP (or, with
// --watch, a changed file) reloads without dropping a request: an accepted config is swapped in
// whole, a rejected one leaves the last good config serving and sets the banner (decision #41).
func Run(ctx context.Context, opts RunOptions, signals <-chan os.Signal, logger *clog.Logger) error {
	if logger == nil {
		logger = clog.New(io.Discard)
	}

	path := ConfigPath(opts.ConfigPath, os.Getenv)
	cfg, warnings, err := config.Load(path)
	if err != nil {
		return err
	}
	logWarnings(logger, warnings)

	host := newHostReader()
	host.SetPath(hostPath(cfg))

	// One HTTP client for probes and the self-update check: no redirects, no cookie jar, every body
	// capped (STANDARDS section 4).
	fetch := probe.NewHTTPFetcher(os.Getenv, probeUserAgent, maxProbeBody)

	store := &probe.Store{}
	scheduler := probe.NewScheduler(
		probe.Env{
			Fetch:   fetch,
			Resolve: probe.NewDNSResolver(),
			Host:    host.Read,
			Now:     time.Now,
		},
		store,
		probe.Clock{Now: time.Now, After: time.After},
		logger.StandardLog(),
	)

	live := &liveConfig{}
	live.accept(cfg, time.Now())

	runCtx, stop := context.WithCancel(ctx)
	defer stop()

	scheduler.Start(runCtx, cfg)

	deps := gateway.Deps{
		Current:   live.Current,
		Logger:    logger.StandardLog(),
		Version:   version,
		ConfigDir: filepath.Dir(path),
		Store:     store,
		Scheduler: scheduler,
		Host:      host.Read,
		Now:       time.Now,
		// The container side of the control plane. It answers "not installed" on a box with no
		// control mounts (decisions #10 and #57).
		Control: &control.Plane{
			Config: func() *config.Config {
				current, _ := live.Current()
				return current
			},
			Host: host.Read,
			Now:  time.Now,
		},
		// The read-only self-update check. The running digest comes from the image reference the
		// deployment started, which compose passes through .env; a build without one still reports
		// the newest digest and compares commits instead.
		Update: func(ctx context.Context, settings *config.Update) (updater.Resolution, error) {
			running := updater.Running{Commit: commit}
			if image := os.Getenv("MERIDIAN_IMAGE"); image != "" {
				if at := strings.LastIndex(image, "@"); at >= 0 {
					if digest, err := wire.ParseDigest(image[at+1:]); err == nil {
						running.Digest = digest
					}
				}
			}
			return updater.Resolve(ctx, fetch, updater.Source{
				Repo:       wire.ImageRepo(settings.Image),
				PublishURL: settings.PublishURL,
			}, running, updater.Clock{Now: time.Now})
		},
	}

	logger.Info("serving", "config", path, "listen", cfg.Listen, "services", len(cfg.Services))

	served := make(chan error, 1)
	go func() { served <- gateway.Serve(runCtx, deps) }()

	configMod := fileModTime(path)
	reload := func() {
		fresh, freshWarnings, err := config.Load(path)
		if err != nil {
			live.reject(bannerError(err))
			for _, line := range problemLines(err) {
				logger.Error("config rejected, still serving the last good one", "problem", line)
			}
			return
		}
		logWarnings(logger, freshWarnings)
		scheduler.Reload(fresh)
		host.SetPath(hostPath(fresh))
		live.accept(fresh, time.Now())
		configMod = fileModTime(path)
		logger.Info("config reloaded", "services", len(fresh.Services), "listen", fresh.Listen)
	}

	var watch <-chan time.Time
	if opts.Watch {
		ticker := time.NewTicker(watchInterval)
		defer ticker.Stop()
		watch = ticker.C
	}

	for {
		select {
		case <-ctx.Done():
			stop()
			return <-served
		case err := <-served:
			return err
		case received := <-signals:
			if received == syscall.SIGHUP {
				logger.Debug("reload requested", "signal", received.String())
				reload()
				continue
			}
			logger.Info("shutting down", "signal", received.String())
			stop()
			return <-served
		case <-watch:
			if changed := fileModTime(path); changed.After(configMod) {
				reload()
			}
		}
	}
}

// liveConfig is the config the gateway serves, swapped whole under a lock so a request never sees
// half a reload. The gateway reads it once per request.
type liveConfig struct {
	mu     sync.RWMutex
	cfg    *config.Config
	status gateway.LoadStatus
}

func (live *liveConfig) Current() (*config.Config, gateway.LoadStatus) {
	live.mu.RLock()
	defer live.mu.RUnlock()
	return live.cfg, live.status
}

// accept makes cfg the one being served and clears any earlier rejection.
func (live *liveConfig) accept(cfg *config.Config, at time.Time) {
	live.mu.Lock()
	defer live.mu.Unlock()
	live.cfg = cfg
	live.status = gateway.LoadStatus{LoadedAt: at}
}

// reject keeps the config that is serving and explains why the new one was not taken.
func (live *liveConfig) reject(problem string) {
	live.mu.Lock()
	defer live.mu.Unlock()
	live.status.Error = problem
}

// hostPath is the snapshot file the config names, or "" when host_status is not configured.
func hostPath(cfg *config.Config) string {
	if cfg.HostStatus == nil {
		return ""
	}
	return cfg.HostStatus.File
}

// fileModTime is the file's modification time, or the zero time when it cannot be read.
func fileModTime(path string) time.Time {
	info, err := os.Stat(path)
	if err != nil {
		return time.Time{}
	}
	return info.ModTime()
}

// problemLines is every config problem as one line, for the log. A problem that is not a parsed
// config (an unreadable file, say) is one line of its own.
func problemLines(err error) []string {
	var problems config.Problems
	if !errors.As(err, &problems) {
		return []string{err.Error()}
	}
	lines := make([]string, len(problems))
	for index, problem := range problems {
		lines[index] = problem.String()
	}
	return lines
}

// bannerError is the short form the page shows: the first few problems as "path: code", because a
// banner is not the place for a paragraph.
func bannerError(err error) string {
	var problems config.Problems
	if !errors.As(err, &problems) {
		return err.Error()
	}
	short := make([]string, 0, bannerProblems)
	for _, problem := range problems {
		if len(short) == bannerProblems {
			break
		}
		if problem.Path == "" {
			short = append(short, string(problem.Code))
			continue
		}
		short = append(short, problem.Path+": "+string(problem.Code))
	}
	return strings.Join(short, "; ")
}

func logWarnings(logger *clog.Logger, warnings []config.Warning) {
	for _, warning := range warnings {
		logger.Warn("config warning", "path", warning.Path, "code", string(warning.Code), "message", warning.Message)
	}
}

func newRunCommand() *cobra.Command {
	var opts RunOptions
	command := &cobra.Command{
		Use:   "run",
		Short: "serve the dashboard",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			logger := NewLogger(command.ErrOrStderr(), os.Getenv)
			signals := make(chan os.Signal, 2)
			signal.Notify(signals, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM)
			defer signal.Stop(signals)
			return Run(command.Context(), opts, signals, logger)
		},
	}
	command.Flags().StringVar(&opts.ConfigPath, "config", "", "configuration file")
	command.Flags().BoolVar(&opts.Watch, "watch", false, "also reload when the file changes")
	return command
}
