package hostexec

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/autumnsgrove/meridian/internal/wire"
)

// The allowlist and the host configuration are the executor's whole world: root-owned files that say
// what may be asked for and how it runs (decision #15). Nothing in a request can add to them.

// MaxTrustedSize is the largest allowlist or host file the executor reads.
const MaxTrustedSize = 256 << 10

// Kinds of allowlisted target.
const (
	KindSystemd   = "systemd"
	KindCompose   = "compose"
	KindContainer = "container"
	KindHost      = "host"
)

// Kinds of update a target may declare.
const (
	UpdateUnit    = "unit"
	UpdateCommand = "command"
	UpdateDocker  = "docker"
	UpdateSelf    = "self"
)

// Criticality is what the page shows next to a target. It changes no behaviour.
const (
	CriticalityLow    = "low"
	CriticalityMedium = "medium"
	CriticalityHigh   = "high"
)

var knownKinds = map[string]bool{
	KindSystemd: true, KindCompose: true, KindContainer: true, KindHost: true,
}

var knownUpdateKinds = map[string]bool{
	UpdateUnit: true, UpdateCommand: true, UpdateDocker: true, UpdateSelf: true,
}

var knownCriticalities = map[string]bool{
	CriticalityLow: true, CriticalityMedium: true, CriticalityHigh: true,
}

var (
	// containerNameGrammar is Docker's name grammar: no leading dash, so a name from a root-owned
	// file can never become a flag.
	containerNameGrammar = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)
	// userNameGrammar is a Linux user name, which bounds what run_as can be.
	userNameGrammar = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.-]{0,31}$`)
)

// Target is one entry of targets.json: a thing the page may act on, and everything the executor needs
// to act on it without reading the request.
type Target struct {
	Kind        string   `json:"kind"`
	Unit        string   `json:"unit,omitempty"`
	Dir         string   `json:"dir,omitempty"`
	Service     string   `json:"service,omitempty"`
	Container   string   `json:"container,omitempty"`
	Actions     []string `json:"actions"`
	DNS         bool     `json:"dns,omitempty"`
	Criticality string   `json:"criticality"`
	// Update is how the target updates itself, when it can. A target without one refuses update.
	Update *Update `json:"update,omitempty"`
	// UpdateTimeoutS overrides the update timeout, in seconds, bounded at 30 minutes.
	UpdateTimeoutS int `json:"update_timeout_s,omitempty"`
	// NoUpdateReason renders a disabled update button in the page.
	NoUpdateReason string `json:"no_update_reason,omitempty"`
}

// Update is one way to update a target. Every field comes from the allowlist, never from a request.
type Update struct {
	Kind  string   `json:"kind"`
	Unit  string   `json:"unit,omitempty"`
	Cmd   []string `json:"cmd,omitempty"`
	RunAs string   `json:"run_as,omitempty"`
}

// Targets is the allowlist, keyed by the slug the page sends.
type Targets map[string]Target

// HostConfig is /etc/meridian/host.json: where the control directory lives and which user the
// container and the app updaters run as.
type HostConfig struct {
	ControlDir string `json:"control_dir"`
	DeployUser string `json:"deploy_user"`
}

// LoadTargets decodes and validates targets.json. It refuses the whole document on any problem: a
// half-valid allowlist is not something the executor can reason about.
func LoadTargets(data []byte, deployUser string) (Targets, error) {
	var document struct {
		Targets Targets `json:"targets"`
	}
	if err := wire.DecodeStrict(data, MaxTrustedSize, &document); err != nil {
		return nil, fmt.Errorf("hostexec: targets.json: %w", err)
	}
	if len(document.Targets) == 0 {
		return nil, errors.New("hostexec: targets.json: no targets")
	}
	if err := ValidateTargets(document.Targets, deployUser); err != nil {
		return nil, err
	}
	return document.Targets, nil
}

// ValidateTargets reports everything wrong with an allowlist at once, so one run of the installer
// fixes every typo rather than one per attempt.
func ValidateTargets(targets Targets, deployUser string) error {
	var problems []string
	fail := func(format string, arguments ...any) {
		problems = append(problems, fmt.Sprintf(format, arguments...))
	}
	if deployUser == "" {
		fail("host.json declares no deploy user, so no update can be checked")
	}

	for id := range targets {
		target := targets[id]
		where := "target " + id
		if !wire.IsSlug(id) {
			fail("%s: the id is not a slug like my-app", where)
		}
		if !knownKinds[target.Kind] {
			fail("%s: kind %q is not one of systemd, compose, container, host", where, target.Kind)
		}
		if !knownCriticalities[target.Criticality] {
			fail("%s: criticality %q is not one of low, medium, high", where, target.Criticality)
		}
		validateActions(where, target, fail)
		validateShape(where, target, fail)
		validateUpdate(where, target, deployUser, fail)
		if target.UpdateTimeoutS < 0 || target.UpdateTimeoutS > 1800 {
			fail("%s: update_timeout_s %d is outside 1..1800", where, target.UpdateTimeoutS)
		}
	}

	if len(problems) > 0 {
		return fmt.Errorf("hostexec: targets.json: %s", strings.Join(problems, "; "))
	}
	return nil
}

// validateActions checks the action list against the closed vocabulary, allowing reboot only for the
// box itself.
func validateActions(where string, target Target, fail func(string, ...any)) {
	if len(target.Actions) == 0 {
		fail("%s: no actions", where)
		return
	}
	seen := make(map[string]bool, len(target.Actions))
	for _, action := range target.Actions {
		if seen[action] {
			fail("%s: action %q is listed twice", where, action)
		}
		seen[action] = true

		switch {
		case action == string(wire.ActionReboot):
			if target.Kind != KindHost {
				fail("%s: reboot is only allowed on the host target", where)
			}
		case !wire.IsTargetAction(wire.Action(action)):
			fail("%s: action %q is not one of start, stop, restart, update", where, action)
		}
	}
}

// validateShape checks that the fields belong to the kind and that every identifier that will reach a
// command is in its grammar.
func validateShape(where string, target Target, fail func(string, ...any)) {
	unused := func(field, value string) {
		if value != "" {
			fail("%s: %s is set on a %s target", where, field, target.Kind)
		}
	}

	switch target.Kind {
	case KindSystemd:
		unused("dir", target.Dir)
		unused("service", target.Service)
		unused("container", target.Container)
		if !wire.IsUnitName(target.Unit) {
			fail("%s: unit %q is not a systemd unit name", where, target.Unit)
		}
	case KindCompose:
		unused("unit", target.Unit)
		unused("container", target.Container)
		if !isCleanAbs(target.Dir) {
			fail("%s: dir %q must be an absolute, clean path", where, target.Dir)
		}
		if !containerNameGrammar.MatchString(target.Service) {
			fail("%s: service %q is not a container name", where, target.Service)
		}
	case KindContainer:
		unused("unit", target.Unit)
		unused("dir", target.Dir)
		unused("service", target.Service)
		if !containerNameGrammar.MatchString(target.Container) {
			fail("%s: container %q is not a container name", where, target.Container)
		}
	case KindHost:
		unused("unit", target.Unit)
		unused("dir", target.Dir)
		unused("service", target.Service)
		unused("container", target.Container)
		if len(target.Actions) != 1 || target.Actions[0] != string(wire.ActionReboot) {
			fail("%s: the host target offers reboot and nothing else", where)
		}
	}
}

// validateUpdate checks the update block, which is itself a small allowlist of its own.
func validateUpdate(where string, target Target, deployUser string, fail func(string, ...any)) {
	offersUpdate := false
	for _, action := range target.Actions {
		if action == string(wire.ActionUpdate) {
			offersUpdate = true
		}
	}

	if target.Update == nil {
		if offersUpdate {
			fail("%s: offers update but declares no update", where)
		}
		if target.UpdateTimeoutS != 0 {
			fail("%s: update_timeout_s is set but there is no update", where)
		}
		return
	}
	if !offersUpdate {
		fail("%s: declares an update but does not offer it in actions", where)
	}
	if target.NoUpdateReason != "" {
		fail("%s: no_update_reason is set but an update is declared", where)
	}
	if !knownUpdateKinds[target.Update.Kind] {
		fail("%s: update kind %q is not one of unit, command, docker, self", where, target.Update.Kind)
	}

	switch target.Update.Kind {
	case UpdateUnit:
		if !wire.IsUnitName(target.Update.Unit) {
			fail("%s: update unit %q is not a systemd unit name", where, target.Update.Unit)
		}
		if len(target.Update.Cmd) != 0 {
			fail("%s: a unit update takes no cmd", where)
		}
	case UpdateCommand:
		if target.Update.Unit != "" {
			fail("%s: a command update takes no unit", where)
		}
		validateArgv(where, target.Update.Cmd, fail)
		if target.Update.RunAs != "" && target.Update.RunAs != deployUser {
			fail("%s: run_as %q is not the deploy user %q", where, target.Update.RunAs, deployUser)
		}
	case UpdateDocker:
		if target.Kind != KindCompose {
			fail("%s: a docker update needs a compose target", where)
		}
		if target.Update.Unit != "" || len(target.Update.Cmd) != 0 {
			fail("%s: a docker update takes no unit or cmd", where)
		}
	case UpdateSelf:
		// Meridian's own update is run by `meridian-host update`, not by this command path; the
		// fields it needs arrive with that task (T35).
		if target.Update.Unit != "" || len(target.Update.Cmd) != 0 {
			fail("%s: a self update takes no unit or cmd", where)
		}
	}
}

// validateArgv checks a command from the allowlist: a program with an absolute, clean path and
// arguments that cannot carry a NUL.
func validateArgv(where string, argv []string, fail func(string, ...any)) {
	if len(argv) == 0 {
		fail("%s: the command has no argv", where)
		return
	}
	if len(argv) > 64 {
		fail("%s: the command has %d arguments, more than 64", where, len(argv))
	}
	for index, element := range argv {
		if strings.ContainsRune(element, 0) {
			fail("%s: argument %d contains a NUL", where, index)
		}
		if len(element) > 4096 {
			fail("%s: argument %d is longer than 4096 bytes", where, index)
		}
		if index == 0 && !isCleanAbs(element) {
			fail("%s: program %q must be an absolute, clean path", where, element)
		}
	}
}

// LoadHost decodes and validates host.json.
func LoadHost(data []byte) (HostConfig, error) {
	var host HostConfig
	if err := wire.DecodeStrict(data, MaxTrustedSize, &host); err != nil {
		return HostConfig{}, fmt.Errorf("hostexec: host.json: %w", err)
	}
	if !isCleanAbs(host.ControlDir) {
		return HostConfig{}, fmt.Errorf("hostexec: host.json: control_dir %q must be an absolute, clean path", host.ControlDir)
	}
	if !userNameGrammar.MatchString(host.DeployUser) {
		return HostConfig{}, fmt.Errorf("hostexec: host.json: deploy_user %q is not a user name", host.DeployUser)
	}
	return host, nil
}

// isCleanAbs is the one rule for a path the executor will pass to a program: absolute, and already in
// its own canonical form, so nothing can be smuggled in with a ".." or a doubled separator.
func isCleanAbs(path string) bool {
	return path != "" && !strings.ContainsRune(path, 0) &&
		filepath.IsAbs(path) && filepath.Clean(path) == path
}

// Offers reports whether the target's allowlist entry includes the action.
func (target Target) Offers(action wire.Action) bool {
	for _, allowed := range target.Actions {
		if allowed == string(action) {
			return true
		}
	}
	return false
}
