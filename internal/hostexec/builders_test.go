package hostexec_test

import (
	"strings"
	"testing"
	"time"

	"github.com/autumnsgrove/meridian/internal/hostexec"
	"github.com/autumnsgrove/meridian/internal/wire"
)

// The ids in this file are canonical UUIDs; a request never reaches a builder with anything else
// (wire.ValidateRequest refuses it first).
const (
	builderID  = "0192f0c4-6c1a-7f3b-9a10-2b6f7c1d8e44"
	builderRef = "0192f0c4-6c1a-7f3b-9a10-2b6f7c1d8e55"
)

// fixedTokens is the vocabulary a builder may hardcode: the verbs and flags of the command table. A
// builder may add nothing else that is not on the allowlist, the id or the ref (decision #31).
var fixedTokens = map[string]bool{
	"systemctl": true, "start": true, "stop": true, "restart": true, "is-active": true,
	"active": true, "inactive": true,
	"docker": true, "compose": true, "--project-directory": true, "up": true, "-d": true,
	"--pull": true, "always": true, "container": true,
	"systemd-run": true, "--on-active=10": true, "--on-active=10min": true, "-p": true,
	"reboot": true,
}

func systemdTarget() hostexec.Target {
	return hostexec.Target{
		Kind: hostexec.KindSystemd, Unit: "worker.service",
		Actions:     []string{"start", "stop", "restart", "update"},
		Criticality: hostexec.CriticalityMedium,
		Update:      &hostexec.Update{Kind: hostexec.UpdateUnit, Unit: "worker-update.service", RunAs: deployUser},
	}
}

// volatileTarget is a target whose actions can kill the dashboard, so its result must exist first.
func volatileTarget() hostexec.Target {
	return hostexec.Target{
		Kind: hostexec.KindSystemd, Unit: "resolver.service", DNS: true, ResultFirst: true,
		Actions:     []string{"start", "stop", "restart"},
		Criticality: hostexec.CriticalityHigh,
	}
}

func composeTarget() hostexec.Target {
	return hostexec.Target{
		Kind: hostexec.KindCompose, Dir: "/srv/stack", Service: "web",
		Actions:     []string{"start", "stop", "restart", "update"},
		Criticality: hostexec.CriticalityLow,
		Update:      &hostexec.Update{Kind: hostexec.UpdateDocker},
	}
}

func containerTarget() hostexec.Target {
	return hostexec.Target{
		Kind: hostexec.KindContainer, Container: "worker",
		Actions:     []string{"start", "stop", "restart", "update"},
		Criticality: hostexec.CriticalityLow,
		Update: &hostexec.Update{
			Kind: hostexec.UpdateCommand, Cmd: []string{"/home/deploy/app/app", "update"}, RunAs: deployUser,
		},
	}
}

func hostTarget() hostexec.Target {
	return hostexec.Target{
		Kind: hostexec.KindHost, Actions: []string{"reboot"}, Criticality: hostexec.CriticalityHigh,
	}
}

type buildCase struct {
	name string
	in   hostexec.BuildInput
	want hostexec.Command
}

// buildCases is the command table as the executor must produce it: one row per (kind, action), with
// the exact argv, run-as user, working directory, timeout and post-check.
func buildCases() []buildCase {
	return []buildCase{
		{
			name: "systemd start",
			in:   hostexec.BuildInput{Target: systemdTarget(), Action: wire.ActionStart, ID: builderID, DeployUser: deployUser},
			want: hostexec.Command{
				Argv:    []string{"systemctl", "start", "worker.service"},
				Timeout: 120 * time.Second,
				Verify:  &hostexec.Verify{Argv: []string{"systemctl", "is-active", "worker.service"}, Want: "active"},
			},
		},
		{
			name: "systemd stop",
			in:   hostexec.BuildInput{Target: systemdTarget(), Action: wire.ActionStop, ID: builderID, DeployUser: deployUser},
			want: hostexec.Command{
				Argv:    []string{"systemctl", "stop", "worker.service"},
				Timeout: 120 * time.Second,
				Verify:  &hostexec.Verify{Argv: []string{"systemctl", "is-active", "worker.service"}, Want: "inactive"},
			},
		},
		{
			name: "systemd restart of a volatile target writes its result first",
			in:   hostexec.BuildInput{Target: volatileTarget(), Action: wire.ActionRestart, ID: builderID, DeployUser: deployUser},
			want: hostexec.Command{
				Argv:        []string{"systemctl", "restart", "resolver.service"},
				Timeout:     120 * time.Second,
				ResultFirst: true,
				Verify:      &hostexec.Verify{Argv: []string{"systemctl", "is-active", "resolver.service"}, Want: "active"},
			},
		},
		{
			name: "compose start",
			in:   hostexec.BuildInput{Target: composeTarget(), Action: wire.ActionStart, ID: builderID, DeployUser: deployUser},
			want: hostexec.Command{
				Argv:    []string{"docker", "compose", "--project-directory", "/srv/stack", "start", "web"},
				RunAs:   deployUser,
				Dir:     "/srv/stack",
				Timeout: 120 * time.Second,
			},
		},
		{
			name: "compose stop",
			in:   hostexec.BuildInput{Target: composeTarget(), Action: wire.ActionStop, ID: builderID, DeployUser: deployUser},
			want: hostexec.Command{
				Argv:    []string{"docker", "compose", "--project-directory", "/srv/stack", "stop", "web"},
				RunAs:   deployUser,
				Dir:     "/srv/stack",
				Timeout: 120 * time.Second,
			},
		},
		{
			name: "compose restart",
			in:   hostexec.BuildInput{Target: composeTarget(), Action: wire.ActionRestart, ID: builderID, DeployUser: deployUser},
			want: hostexec.Command{
				Argv:    []string{"docker", "compose", "--project-directory", "/srv/stack", "restart", "web"},
				RunAs:   deployUser,
				Dir:     "/srv/stack",
				Timeout: 120 * time.Second,
			},
		},
		{
			name: "container start",
			in:   hostexec.BuildInput{Target: containerTarget(), Action: wire.ActionStart, ID: builderID, DeployUser: deployUser},
			want: hostexec.Command{
				Argv:    []string{"docker", "start", "worker"},
				RunAs:   deployUser,
				Timeout: 120 * time.Second,
			},
		},
		{
			name: "container stop",
			in:   hostexec.BuildInput{Target: containerTarget(), Action: wire.ActionStop, ID: builderID, DeployUser: deployUser},
			want: hostexec.Command{
				Argv:    []string{"docker", "stop", "worker"},
				RunAs:   deployUser,
				Timeout: 120 * time.Second,
			},
		},
		{
			name: "container restart",
			in:   hostexec.BuildInput{Target: containerTarget(), Action: wire.ActionRestart, ID: builderID, DeployUser: deployUser},
			want: hostexec.Command{
				Argv:    []string{"docker", "restart", "worker"},
				RunAs:   deployUser,
				Timeout: 120 * time.Second,
			},
		},
		{
			name: "update by starting a oneshot unit",
			in:   hostexec.BuildInput{Target: systemdTarget(), Action: wire.ActionUpdate, ID: builderID, DeployUser: deployUser},
			want: hostexec.Command{
				Argv:    []string{"systemctl", "start", "worker-update.service"},
				Timeout: 600 * time.Second,
			},
		},
		{
			name: "update by the allowlist's own argv",
			in:   hostexec.BuildInput{Target: containerTarget(), Action: wire.ActionUpdate, ID: builderID, DeployUser: deployUser},
			want: hostexec.Command{
				Argv:    []string{"/home/deploy/app/app", "update"},
				RunAs:   deployUser,
				Timeout: 600 * time.Second,
			},
		},
		{
			name: "update by pull and recreate",
			in:   hostexec.BuildInput{Target: composeTarget(), Action: wire.ActionUpdate, ID: builderID, DeployUser: deployUser},
			want: hostexec.Command{
				Argv:    []string{"docker", "compose", "--project-directory", "/srv/stack", "up", "-d", "--pull", "always", "web"},
				RunAs:   deployUser,
				Dir:     "/srv/stack",
				Timeout: 600 * time.Second,
			},
		},
		{
			name: "update timeout comes from the allowlist",
			in: hostexec.BuildInput{
				Target: func() hostexec.Target {
					target := containerTarget()
					target.UpdateTimeoutS = 420
					return target
				}(),
				Action: wire.ActionUpdate, ID: builderID, DeployUser: deployUser,
			},
			want: hostexec.Command{
				Argv:    []string{"/home/deploy/app/app", "update"},
				RunAs:   deployUser,
				Timeout: 420 * time.Second,
			},
		},
		{
			name: "reboot is scheduled with a delay and writes its result first",
			in:   hostexec.BuildInput{Target: hostTarget(), Action: wire.ActionReboot, ID: builderID, DeployUser: deployUser},
			want: hostexec.Command{
				Argv:        []string{"systemd-run", "--on-active=10", "--unit=meridian-reboot-" + builderID, "systemctl", "reboot"},
				Timeout:     120 * time.Second,
				ResultFirst: true,
			},
		},
	}
}

func TestBuild_TableOfCommands(t *testing.T) {
	for _, test := range buildCases() {
		t.Run(test.name, func(t *testing.T) {
			got, err := hostexec.Build(test.in)
			if err != nil {
				t.Fatalf("Build: %v", err)
			}
			assertCommand(t, got, test.want)
		})
	}
}

func assertCommand(t *testing.T, got, want hostexec.Command) {
	t.Helper()
	if strings.Join(got.Argv, "\x00") != strings.Join(want.Argv, "\x00") {
		t.Errorf("argv = %q, want %q", got.Argv, want.Argv)
	}
	if got.RunAs != want.RunAs {
		t.Errorf("run as = %q, want %q", got.RunAs, want.RunAs)
	}
	if got.Dir != want.Dir {
		t.Errorf("dir = %q, want %q", got.Dir, want.Dir)
	}
	if got.Timeout != want.Timeout {
		t.Errorf("timeout = %v, want %v", got.Timeout, want.Timeout)
	}
	if got.ResultFirst != want.ResultFirst {
		t.Errorf("result first = %v, want %v", got.ResultFirst, want.ResultFirst)
	}
	switch {
	case got.Verify == nil && want.Verify == nil:
	case got.Verify == nil || want.Verify == nil:
		t.Errorf("verify = %+v, want %+v", got.Verify, want.Verify)
	default:
		if strings.Join(got.Verify.Argv, "\x00") != strings.Join(want.Verify.Argv, "\x00") || got.Verify.Want != want.Verify.Want {
			t.Errorf("verify = %+v, want %+v", got.Verify, want.Verify)
		}
	}
}

// TestBuild_ArgvIsTheAllowlistPlusFixedWords is the property the whole design rests on: change
// everything the allowlist and the request contribute, and every argv element is either a fixed word
// from the table or one of those values. Nothing else can reach a command line (decision #31).
func TestBuild_ArgvIsTheAllowlistPlusFixedWords(t *testing.T) {
	for _, test := range buildCases() {
		t.Run(test.name, func(t *testing.T) {
			assertArgvTemplate(t, test.in)
		})
	}
}

func assertArgvTemplate(t *testing.T, in hostexec.BuildInput) {
	t.Helper()
	other := twinInput(in)
	first, err := hostexec.Build(in)
	if err != nil {
		return
	}
	second, err := hostexec.Build(other)
	if err != nil {
		t.Fatalf("the same row refused its twin: %v", err)
	}
	if len(first.Argv) != len(second.Argv) {
		t.Fatalf("argv grew from %q to %q when the allowlist changed", first.Argv, second.Argv)
	}
	for index, element := range first.Argv {
		if element == second.Argv[index] {
			if !fixedTokens[element] && !accountedFor(element, in) {
				t.Errorf("argv[%d] = %q is hardcoded and not in the verb table", index, element)
			}
			continue
		}
		if !accountedFor(element, in) {
			t.Errorf("argv[%d] = %q does not come from the allowlist, the id or the ref", index, element)
		}
	}
	if first.Verify != nil {
		for index, element := range first.Verify.Argv {
			if !fixedTokens[element] && !accountedFor(element, in) {
				t.Errorf("verify.argv[%d] = %q is neither fixed nor allowed", index, element)
			}
		}
		if first.Verify.Want != "active" && first.Verify.Want != "inactive" {
			t.Errorf("verify.want = %q, want a word from the table", first.Verify.Want)
		}
	}
}

// accountedFor reports whether a value is built from the allowlist entry, the deploy user, the id or
// the ref. A builder may wrap them (--unit=meridian-reboot-<id>) but never invent them.
func accountedFor(value string, in hostexec.BuildInput) bool {
	pieces := []string{
		in.ID, in.Ref, in.DeployUser,
		in.Target.Unit, in.Target.Dir, in.Target.Service, in.Target.Container,
	}
	if in.Target.Update != nil {
		pieces = append(pieces, in.Target.Update.Unit, in.Target.Update.RunAs)
		pieces = append(pieces, in.Target.Update.Cmd...)
	}
	for _, piece := range pieces {
		if piece != "" && strings.Contains(value, piece) {
			return true
		}
	}
	return false
}

// twinInput is the same row with every input value changed, so a comparison of the two argv slices
// shows which slots are data and which are the fixed table.
func twinInput(in hostexec.BuildInput) hostexec.BuildInput {
	other := in
	other.ID = builderRef
	other.Ref = builderID
	other.DeployUser = "twin"
	target := in.Target
	if target.Unit != "" {
		target.Unit = "twin-" + target.Unit
	}
	if target.Dir != "" {
		target.Dir = "/srv/twin"
	}
	if target.Service != "" {
		target.Service = "twin-svc"
	}
	if target.Container != "" {
		target.Container = "twin-container"
	}
	if target.Update != nil {
		update := *target.Update
		if update.Unit != "" {
			update.Unit = "twin-" + update.Unit
		}
		if update.RunAs != "" {
			update.RunAs = "twin"
		}
		if len(update.Cmd) > 0 {
			command := append([]string(nil), update.Cmd...)
			command[len(command)-1] += "-twin"
			update.Cmd = command
		}
		target.Update = &update
	}
	other.Target = target
	return other
}

func FuzzBuildArgv(f *testing.F) {
	f.Add("systemd", "restart", "worker.service", "", "", "", "unit", "worker-update.service", "", builderID, "")
	f.Add("compose", "update", "", "/srv/stack", "web", "", "docker", "", "", builderID, "")
	f.Add("container", "update", "", "", "", "worker", "command", "", "/home/deploy/app/app\x00update", builderID, "")
	f.Add("host", "reboot", "", "", "", "", "", "", "", builderID, "")
	f.Add("systemd", "start", "relative/../unit", "/", "", "", "command", "", "../../bin/thing", "", builderID)
	f.Add("nonsense", "start", "", "", "", "", "", "", "", "", "not-a-uuid")
	f.Add("systemd", "cancel-autorestore", "worker.service", "", "", "", "", "", "", builderID, builderRef)

	f.Fuzz(func(t *testing.T, kind, action, unit, dir, service, container, updateKind, updateUnit, command, id, ref string) {
		target := hostexec.Target{
			Kind: kind, Unit: unit, Dir: dir, Service: service, Container: container,
			Actions: []string{action}, Criticality: hostexec.CriticalityLow,
		}
		if updateKind != "" {
			target.Update = &hostexec.Update{
				Kind: updateKind, Unit: updateUnit, Cmd: strings.Split(command, "\x00"), RunAs: deployUser,
			}
		}
		in := hostexec.BuildInput{
			Target: target, Action: wire.Action(action), ID: id, Ref: ref, DeployUser: deployUser,
		}

		built, err := hostexec.Build(in)
		if err != nil {
			return
		}
		if len(built.Argv) == 0 {
			t.Fatal("a command with no argv")
		}
		if built.Timeout <= 0 {
			t.Fatalf("argv %q has no timeout, so it could hang the oneshot", built.Argv)
		}
		// Two rows that differ in every input must differ only in the slots those inputs fill.
		other := hostexec.BuildInput{
			Target: hostexec.Target{Kind: kind, Unit: unit, Dir: dir, Service: service, Container: container,
				Update: target.Update},
			Action: wire.Action(action), ID: id, Ref: ref, DeployUser: deployUser,
		}
		other.Target.Unit = unit
		twin, err := hostexec.Build(twinInput(other))
		if err != nil {
			t.Fatalf("the twin was refused while the original built: %v", err)
		}
		if len(twin.Argv) != len(built.Argv) {
			t.Fatalf("argv grew from %q to %q", built.Argv, twin.Argv)
		}
		for index, element := range built.Argv {
			if element != twin.Argv[index] && !accountedFor(element, in) {
				t.Fatalf("argv[%d] = %q does not come from the allowlist, the id or the ref", index, element)
			}
			if strings.HasPrefix(element, "-") && !fixedTokens[element] && !accountedFor(element, in) {
				t.Fatalf("argv[%d] = %q looks like a flag the table does not define", index, element)
			}
		}
	})
}

func TestBuild_CancelsAnAutoRestoreByStoppingItsTimer(t *testing.T) {
	in := hostexec.BuildInput{
		Target: volatileTarget(), Action: wire.ActionCancelAutorestore, ID: builderID, Ref: builderRef, DeployUser: deployUser,
	}

	got, err := hostexec.Build(in)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	want := []string{"systemctl", "stop", "meridian-autorestore-" + builderRef + ".timer"}
	if strings.Join(got.Argv, " ") != strings.Join(want, " ") {
		t.Errorf("argv = %q, want %q", got.Argv, want)
	}
	if got.Timeout <= 0 || got.Timeout > time.Minute {
		t.Errorf("timeout = %v, want a short one: cancelling a timer is one systemctl call", got.Timeout)
	}
	if got.ResultFirst || got.Verify != nil {
		t.Errorf("command = %+v, want no provisional result and no post-check", got)
	}
}

func TestBuild_RefusesACancelWithoutAUsableRef(t *testing.T) {
	for _, ref := range []string{"", "nope", "../../etc/passwd", builderRef + "x"} {
		t.Run(ref, func(t *testing.T) {
			in := hostexec.BuildInput{
				Target: volatileTarget(), Action: wire.ActionCancelAutorestore, ID: builderID, Ref: ref, DeployUser: deployUser,
			}
			if _, err := hostexec.Build(in); err == nil {
				t.Fatalf("Build accepted ref %q, want a refusal", ref)
			}
		})
	}
}

// TestScheduleRestore_WrapsTheTargetsOwnStartCommand is the DNS safety net: whatever starting the
// target means, the timer runs exactly that, ten minutes from now.
func TestScheduleRestore_WrapsTheTargetsOwnStartCommand(t *testing.T) {
	tests := []struct {
		name  string
		input hostexec.BuildInput
		want  []string
	}{
		{
			name:  "a system unit",
			input: hostexec.BuildInput{Target: volatileTarget(), Action: wire.ActionStart, ID: builderID, DeployUser: deployUser},
			want: []string{
				"systemd-run", "--on-active=10min", "--unit=meridian-autorestore-" + builderID,
				"systemctl", "start", "resolver.service",
			},
		},
		{
			name:  "a deploy-user target keeps its user",
			input: hostexec.BuildInput{Target: composeTarget(), Action: wire.ActionStart, ID: builderID, DeployUser: deployUser},
			want: []string{
				"systemd-run", "--on-active=10min", "--unit=meridian-autorestore-" + builderID, "-p", "User=" + deployUser,
				"docker", "compose", "--project-directory", "/srv/stack", "start", "web",
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			start, err := hostexec.Build(test.input)
			if err != nil {
				t.Fatalf("Build: %v", err)
			}
			scheduled := hostexec.ScheduleRestore(start, builderID)
			if strings.Join(scheduled.Argv, " ") != strings.Join(test.want, " ") {
				t.Errorf("argv = %q\nwant %q", scheduled.Argv, test.want)
			}
			if scheduled.Timeout <= 0 || scheduled.Timeout > time.Minute {
				t.Errorf("timeout = %v, want a short one: scheduling a timer is one systemd-run call", scheduled.Timeout)
			}
		})
	}
}

func TestScheduleRestore_UsesTheRestoreWindowTheDocsPromise(t *testing.T) {
	if hostexec.AutoRestoreWindow != 10*time.Minute {
		t.Errorf("AutoRestoreWindow = %v, want 10 minutes (decision #17)", hostexec.AutoRestoreWindow)
	}
	if !strings.Contains(strings.Join(hostexec.ScheduleRestore(hostexec.Command{Argv: []string{"systemctl", "start", "resolver.service"}}, builderID).Argv, " "), "--on-active=10min") {
		t.Error("the timer does not say 10min, so systemd-run would refuse it")
	}
}
