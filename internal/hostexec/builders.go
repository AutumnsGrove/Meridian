package hostexec

import (
	"errors"
	"fmt"
	"time"

	"github.com/autumnsgrove/meridian/internal/wire"
)

// The command table (docs/agent/06-control.md "T28"). Every command the executor may run is a pure
// function from allowlist data to argv: this file is the only place that names a program or a flag,
// and nothing a browser sends can add one (decision #31). A row that is missing is a refusal, never a
// fallback, so a new action cannot run by accident.

const (
	// DefaultTimeout bounds one action's command. The control unit's TimeoutStartSec must be larger
	// (T31), or systemd kills the run before the executor can answer.
	DefaultTimeout = 120 * time.Second
	// UpdateDefaultTimeout bounds an update, which pulls and builds. A target may lower it with
	// update_timeout_s.
	UpdateDefaultTimeout = 600 * time.Second
	// quickTimeout bounds the short systemd commands: scheduling a timer, cancelling one.
	quickTimeout = 30 * time.Second

	// AutoRestoreWindow is how long a DNS stop is allowed to stand before the target comes back on its
	// own (decision #17). autoRestoreOnActive is the same window as systemd-run reads it: "10min", not
	// Go's "10m0s".
	AutoRestoreWindow   = 10 * time.Minute
	autoRestoreOnActive = "10min"

	// autorestoreUnitPrefix and rebootUnitPrefix name the transient units the executor schedules.
	autorestoreUnitPrefix = "meridian-autorestore-"
	rebootUnitPrefix      = "meridian-reboot-"

	// SelfUpdateBinary is where the installer puts the host binary (decision #67). The self update
	// re-executes it, as the deploy user, so the compose commands run in the docker group.
	SelfUpdateBinary = "/etc/meridian/meridian-host"
)

// ErrNoCommand means the allowlist offers something the table cannot build. That is an implementation
// gap rather than the request's fault, so the executor answers `internal` and the audit log shows it.
var ErrNoCommand = errors.New("hostexec: no command for that target and action")

// Command is one allowlisted action, ready to run. Every part of it comes from the allowlist, the id
// or the ref; the rest of the request contributes nothing (decision #31).
type Command struct {
	Argv []string
	// RunAs is the user to run as; "" runs as root.
	RunAs string
	Dir   string
	// Timeout bounds the command. Every builder sets one: a command without a deadline is a command
	// that can hang the oneshot.
	Timeout time.Duration
	// ResultFirst writes a provisional result before the command runs, because the command is going
	// to kill the container that would otherwise report it (decision #64).
	ResultFirst bool
	// Verify is the post-check for an action whose exit code does not prove it worked.
	Verify *Verify
	// NoNewGroup keeps the command in the caller's process group instead of starting its own. The
	// self updater sets it so the executor's group kill reaches the docker commands it started.
	NoNewGroup bool
}

// Verify is a post-check: run Argv until its output says Want. A unit that was told to start is only
// started once it says so, and a stop is only done once it says inactive (wire.CodeNotActiveAfter).
type Verify struct {
	Argv []string
	Want string
}

// BuildInput is everything a builder may look at: the allowlist entry, the deploy user that docker
// and app updaters run as, and the two validated identifiers. Nothing else from the request is here.
type BuildInput struct {
	Target     Target
	Action     wire.Action
	ID         string
	Ref        string
	DeployUser string
}

// Builder turns one allowlisted action into the command to run.
type Builder func(in BuildInput) (Command, error)

// BuilderKey is one row of the table: what kind of target, and what it is being asked.
type BuilderKey struct {
	Kind   string
	Action wire.Action
}

// Builders is the command table. One row per (kind, action); an update row looks in updateBuilders
// for its second dimension.
var Builders = map[BuilderKey]Builder{
	{Kind: KindSystemd, Action: wire.ActionStart}:   systemdVerb("start", "active"),
	{Kind: KindSystemd, Action: wire.ActionStop}:    systemdVerb("stop", "inactive"),
	{Kind: KindSystemd, Action: wire.ActionRestart}: systemdVerb("restart", "active"),
	{Kind: KindSystemd, Action: wire.ActionUpdate}:  updateByKind,

	{Kind: KindCompose, Action: wire.ActionStart}:   composeVerb("start"),
	{Kind: KindCompose, Action: wire.ActionStop}:    composeVerb("stop"),
	{Kind: KindCompose, Action: wire.ActionRestart}: composeVerb("restart"),
	{Kind: KindCompose, Action: wire.ActionUpdate}:  updateByKind,

	{Kind: KindContainer, Action: wire.ActionStart}:   containerVerb("start"),
	{Kind: KindContainer, Action: wire.ActionStop}:    containerVerb("stop"),
	{Kind: KindContainer, Action: wire.ActionRestart}: containerVerb("restart"),
	{Kind: KindContainer, Action: wire.ActionUpdate}:  updateByKind,

	{Kind: KindHost, Action: wire.ActionReboot}: reboot,
}

// updateBuilders is how a target says it updates itself, the second half of the update row.
var updateBuilders = map[string]Builder{
	UpdateUnit:    updateByUnit,
	UpdateCommand: updateByCommand,
	UpdateDocker:  updateByDocker,
	UpdateSelf:    updateBySelf,
}

// Build returns the command for one allowlisted action, or ErrNoCommand.
func Build(in BuildInput) (Command, error) {
	// Cancelling an auto-restore is the one verb whose command does not depend on the target: the
	// timer a DNS stop scheduled is always a systemd unit.
	if in.Action == wire.ActionCancelAutorestore {
		return cancelAutorestore(in)
	}
	builder, known := Builders[BuilderKey{Kind: in.Target.Kind, Action: in.Action}]
	if !known {
		return Command{}, fmt.Errorf("%w: %q on a %q target", ErrNoCommand, in.Action, in.Target.Kind)
	}
	return builder(in)
}

// cancelAutorestore stops the transient timer that a DNS stop armed. Stopping a timer unit is how
// systemd cancels it, so this is the whole implementation (decision #17).
func cancelAutorestore(in BuildInput) (Command, error) {
	if _, err := wire.ParseID(in.Ref); err != nil {
		return Command{}, fmt.Errorf("%w: a cancel needs the id of the stop it cancels", ErrNoCommand)
	}
	return Command{
		Argv:    []string{"systemctl", "stop", autorestoreUnitPrefix + in.Ref + ".timer"},
		Timeout: quickTimeout,
	}, nil
}

// ScheduleRestore wraps a target's own start command in a transient systemd timer, which is the
// safety net a DNS stop buys: the one button that can cause a second outage cannot outlive ten
// minutes unattended (decision #17). start must come from Build, so its argv is already allowlisted.
func ScheduleRestore(start Command, id string) Command {
	argv := []string{"systemd-run", "--on-active=" + autoRestoreOnActive, "--unit=" + autorestoreUnitPrefix + id}
	if start.RunAs != "" {
		argv = append(argv, "-p", "User="+start.RunAs)
	}
	argv = append(argv, start.Argv...)
	return Command{Argv: argv, Timeout: quickTimeout}
}

// systemdVerb is the systemctl row: the unit comes from the allowlist and the post-check is
// is-active, which is what makes "start succeeded" mean the unit is actually running.
func systemdVerb(verb, want string) Builder {
	return func(in BuildInput) (Command, error) {
		return Command{
			Argv:        []string{"systemctl", verb, in.Target.Unit},
			Timeout:     DefaultTimeout,
			ResultFirst: in.Target.ResultFirst,
			Verify:      &Verify{Argv: []string{"systemctl", "is-active", in.Target.Unit}, Want: want},
		}, nil
	}
}

// composeVerb runs docker compose in the project directory the allowlist names, as the deploy user:
// compose writes into that directory, and root-owned files there break the next update (decision #31).
func composeVerb(verb string) Builder {
	return func(in BuildInput) (Command, error) {
		return Command{
			Argv:        []string{"docker", "compose", "--project-directory", in.Target.Dir, verb, in.Target.Service},
			RunAs:       in.DeployUser,
			Dir:         in.Target.Dir,
			Timeout:     DefaultTimeout,
			ResultFirst: in.Target.ResultFirst,
		}, nil
	}
}

// containerVerb is a bare docker command on one container, as the deploy user.
func containerVerb(verb string) Builder {
	return func(in BuildInput) (Command, error) {
		return Command{
			Argv:        []string{"docker", verb, in.Target.Container},
			RunAs:       in.DeployUser,
			Timeout:     DefaultTimeout,
			ResultFirst: in.Target.ResultFirst,
		}, nil
	}
}

// updateByKind is the update row: the target's update block selects the second half of the table.
func updateByKind(in BuildInput) (Command, error) {
	if in.Target.Update == nil {
		return Command{}, fmt.Errorf("%w: update on a target with no update block", ErrNoCommand)
	}
	builder, known := updateBuilders[in.Target.Update.Kind]
	if !known {
		return Command{}, fmt.Errorf("%w: update kind %q", ErrNoCommand, in.Target.Update.Kind)
	}
	return builder(in)
}

// updateByUnit starts the oneshot unit that owns the update. Those units are Type=oneshot, so
// systemctl blocks until the update is done; the unit declares its own User=.
func updateByUnit(in BuildInput) (Command, error) {
	return Command{
		Argv:        []string{"systemctl", "start", in.Target.Update.Unit},
		Timeout:     updateTimeout(in.Target),
		ResultFirst: in.Target.ResultFirst,
	}, nil
}

// updateByCommand runs the app's own updater exactly as the allowlist writes it, as the deploy user.
func updateByCommand(in BuildInput) (Command, error) {
	if len(in.Target.Update.Cmd) == 0 {
		return Command{}, fmt.Errorf("%w: a command update with no argv", ErrNoCommand)
	}
	return Command{
		Argv:        append([]string(nil), in.Target.Update.Cmd...),
		RunAs:       in.Target.Update.RunAs,
		Timeout:     updateTimeout(in.Target),
		ResultFirst: in.Target.ResultFirst,
	}, nil
}

// updateByDocker pulls and recreates one compose service in a single command: `up --pull always`
// does both, so an update has one exit code and one deadline rather than two half-steps.
func updateByDocker(in BuildInput) (Command, error) {
	return Command{
		Argv: []string{
			"docker", "compose", "--project-directory", in.Target.Dir,
			"up", "-d", "--pull", "always", in.Target.Service,
		},
		RunAs:       in.DeployUser,
		Dir:         in.Target.Dir,
		Timeout:     updateTimeout(in.Target),
		ResultFirst: in.Target.ResultFirst,
	}, nil
}

// updateBySelf re-executes the host binary as the deploy user. That command owns the pin, pull,
// recreate, health poll and rollback (T35); the executor owns the result, so this row is one command
// with one deadline like every other.
func updateBySelf(in BuildInput) (Command, error) {
	return Command{
		Argv:        []string{SelfUpdateBinary, "update"},
		RunAs:       in.DeployUser,
		Timeout:     updateTimeout(in.Target),
		ResultFirst: in.Target.ResultFirst,
	}, nil
}

// reboot schedules the reboot and returns. It always writes its result first: the container is gone
// before it could report anything, and a scheduled reboot can be cancelled if the report fails (#64).
func reboot(in BuildInput) (Command, error) {
	return Command{
		Argv: []string{
			"systemd-run", "--on-active=10", "--unit=" + rebootUnitPrefix + in.ID,
			"systemctl", "reboot",
		},
		Timeout:     DefaultTimeout,
		ResultFirst: true,
	}, nil
}

// updateTimeout is the allowlist's bound on an update, or the default.
func updateTimeout(target Target) time.Duration {
	if target.UpdateTimeoutS > 0 {
		return time.Duration(target.UpdateTimeoutS) * time.Second
	}
	return UpdateDefaultTimeout
}
