// Package hostprobe is the host side of the dashboard: it asks systemd and Tailscale what is true
// and writes the one snapshot the container can read (decision #27). It runs as the deploy user,
// never root, and is standard library only.
package hostprobe

import (
	"context"
	"os"
	"os/exec"
	"time"

	"github.com/autumnsgrove/meridian/internal/wire"
)

// Deps are the I/O seams Collect needs: running a command, reading a file, the clock, and the box's
// own uptime and load. Everything else in this package is pure.
type Deps struct {
	Run      func(ctx context.Context, argv []string) ([]byte, error)
	ReadFile func(string) ([]byte, error)
	Now      func() time.Time
	Uptime   func() (time.Duration, float64, error)
}

// Collect builds a snapshot from the units the host watches and the host's own readings. A source
// that fails is left out rather than guessed at: a unit that does not answer is absent, so the
// container reports "not reported" instead of a made-up state, and a failed tailscale call leaves
// the whole block out (which is different from "reported offline").
func Collect(ctx context.Context, d Deps, units []string) wire.HostSnapshot {
	snapshot := wire.HostSnapshot{
		At:    d.Now(),
		Units: make(map[string]wire.UnitState, len(units)),
	}

	for _, unit := range units {
		output, err := d.Run(ctx, []string{
			"systemctl", "show", unit, "-p", "ActiveState", "-p", "ActiveEnterTimestamp",
		})
		if err != nil {
			continue
		}
		if state, ok := ParseSystemctlShow(output); ok {
			snapshot.Units[unit] = state
		}
	}

	if output, err := d.Run(ctx, []string{"tailscale", "status", "--json"}); err == nil {
		if state, ok := ParseTailscaleStatus(output); ok {
			snapshot.Tailscale = &state
		}
	}

	if uptime, load, err := d.Uptime(); err == nil {
		snapshot.Host = wire.HostInfo{UptimeS: int64(uptime / time.Second), Load1: load}
	}

	return snapshot
}

// RealDeps is the probe's real I/O: the two commands, the clock in UTC, and /proc through ReadFile.
func RealDeps() Deps {
	return Deps{
		Run:      runCommand,
		ReadFile: os.ReadFile,
		Now:      func() time.Time { return time.Now().UTC() },
		Uptime:   func() (time.Duration, float64, error) { return ReadUptime(os.ReadFile) },
	}
}

// runCommand runs argv and returns its standard output. There is no shell anywhere in this path:
// every element is a separate argument, so a unit name from the host's own files cannot become a
// second command. Standard error is discarded; the command's failure is the signal.
func runCommand(ctx context.Context, argv []string) ([]byte, error) {
	return exec.CommandContext(ctx, argv[0], argv[1:]...).Output() //nolint:gosec // argv is a fixed program plus names that passed wire.IsUnitName
}
