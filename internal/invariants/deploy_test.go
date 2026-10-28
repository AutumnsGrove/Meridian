package invariants_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/autumnsgrove/meridian/internal/hostexec"
	"github.com/autumnsgrove/meridian/internal/hostprobe"
)

// The deployment files are the other half of the privilege story: the executor is only as narrow as
// the sudoers rule that starts it and the unit that fires it (decisions #52, #63, #66). A typo here
// is a control plane that looks installed and does nothing, or one that is wider than the design.

// unitFile is one parsed systemd unit: "Section.Key" to value.
func unitFile(t *testing.T, name string) map[string]string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRoot, "deploy", "systemd", name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}

	fields := make(map[string]string)
	section := ""
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, ";") {
			continue
		}
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			section = strings.Trim(trimmed, "[]")
			continue
		}
		key, value, hasValue := strings.Cut(trimmed, "=")
		if !hasValue {
			continue
		}
		fields[section+"."+strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	return fields
}

func TestDeploy_TheControlUnitsSayExactlyWhatTheDesignSays(t *testing.T) {
	tests := []struct {
		file  string
		key   string
		value string
	}{
		{"meridian-control.path", "Path.DirectoryNotEmpty", "@CONTROL_DIR@/inbox/queue"},
		{"meridian-control.path", "Path.Unit", "meridian-control.service"},
		{"meridian-control.path", "Install.WantedBy", "paths.target"},
		{"meridian-control.service", "Unit.StartLimitIntervalSec", "0"},
		{"meridian-control.service", "Service.Type", "oneshot"},
		{"meridian-control.service", "Service.User", "@DEPLOY_USER@"},
		{"meridian-control.service", "Service.ExecStart", "/usr/bin/sudo -n /etc/meridian/meridian-host exec"},
		// Larger than the executor's own 600s ceiling: systemd must be the last to give up, or a
		// healthy run dies mid-action and the request is never answered.
		{"meridian-control.service", "Service.TimeoutStartSec", "660"},
		// ProtectHome=yes would make the status directory under /home unreachable and the probe would
		// fail on its very first write, which is what the first real install did.
		{"meridian-probe.service", "Service.ProtectHome", "read-only"},
		{"meridian-probe.service", "Service.ReadWritePaths", "@STATUS_DIR@"},
		{"meridian-probe.service", "Service.User", "@DEPLOY_USER@"},
	}
	for _, test := range tests {
		t.Run(test.file+" "+test.key, func(t *testing.T) {
			fields := unitFile(t, test.file)
			if got := fields[test.key]; got != test.value {
				t.Errorf("%s = %q, want %q", test.key, got, test.value)
			}
		})
	}
}

func TestDeploy_TheControlServiceKeepsSudoPossible(t *testing.T) {
	fields := unitFile(t, "meridian-control.service")

	// NoNewPrivileges would stop sudo from elevating, and the unit would fail every run with the
	// allowlist untouched: the failure that looks like "the button does nothing".
	if value, set := fields["Service.NoNewPrivileges"]; set {
		t.Errorf("NoNewPrivileges = %q, but the unit runs the executor through sudo (decision #52)", value)
	}
	for key, value := range fields {
		for _, forbidden := range []string{"systemctl", "docker", "@TARGET", "targets.json"} {
			if strings.Contains(value, forbidden) {
				t.Errorf("%s = %q names %q; the unit starts the executor and knows no targets (decision #38)", key, value, forbidden)
			}
		}
	}
}

func TestDeploy_SudoersIsOneExactCommand(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repoRoot, "deploy", "sudoers", "meridian"))
	if err != nil {
		t.Fatalf("read the sudoers rule: %v", err)
	}

	var rules []string
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		rules = append(rules, trimmed)
	}
	if len(rules) != 1 {
		t.Fatalf("the sudoers file has %d rules, want exactly one: %q", len(rules), rules)
	}
	const want = "@DEPLOY_USER@ ALL=(root) NOPASSWD: /etc/meridian/meridian-host exec"
	if rules[0] != want {
		t.Errorf("rule = %q, want %q", rules[0], want)
	}
	if strings.Contains(rules[0], "*") {
		t.Error("the rule contains a wildcard, so it is no longer one exact command (decision #66)")
	}
}

func TestDeploy_TheExampleAllowlistsLoad(t *testing.T) {
	tests := []struct {
		path       string
		deployUser string
	}{
		{"deploy/targets.example.json", "deploy"},
		{"examples/targets.homelab.json", "autumn"},
	}
	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(repoRoot, filepath.FromSlash(test.path)))
			if err != nil {
				t.Fatalf("read %s: %v", test.path, err)
			}
			targets, err := hostexec.LoadTargets(data, test.deployUser)
			if err != nil {
				t.Fatalf("%s does not load: %v", test.path, err)
			}
			if len(targets) == 0 {
				t.Fatalf("%s has no targets", test.path)
			}
		})
	}
}

// TestDeploy_TheHomelabAllowlistKeepsTheHazards is the CONTROL.md action matrix, checked where it
// cannot drift: the DNS targets keep the guard, the dashboard keeps its provisional result, and the
// one service whose update path is not captured yet does not pretend to have one (decision #22).
func TestDeploy_TheHomelabAllowlistKeepsTheHazards(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repoRoot, "examples", "targets.homelab.json"))
	if err != nil {
		t.Fatalf("read the homelab allowlist: %v", err)
	}
	targets, err := hostexec.LoadTargets(data, "autumn")
	if err != nil {
		t.Fatalf("the homelab allowlist does not load: %v", err)
	}

	for _, id := range []string{"adguard", "unbound"} {
		target, known := targets[id]
		if !known {
			t.Fatalf("%s is not in the allowlist", id)
		}
		if !target.DNS || !target.Offers("stop") {
			t.Errorf("%s must serve DNS and be stoppable, or the auto-restore has no subject", id)
		}
		if target.Offers("update") {
			t.Errorf("%s offers update without a scripted updater (decision #29)", id)
		}
	}
	if target := targets["meridian"]; !target.ResultFirst {
		t.Error("meridian must set result_first, or a self-restart loses its provisional result (decision #64)")
	}
	if target := targets["vaultwarden"]; target.Offers("update") {
		t.Error("vaultwarden offers update before its run spec is captured as compose (decision #22)")
	}
	host, known := targets["host"]
	if !known || !host.Offers("reboot") || len(host.Actions) != 1 {
		t.Error("host must offer reboot and nothing else (decision #18)")
	}
}

// TestDeploy_TheObserveListWatchesTheExecutor is what makes "no executor" visible from the container:
// the page only learns the watcher is dead because the host snapshot reports its unit, and the probe
// only reports units it was told about (decisions #61, #79). The installer appends this line (T36).
func TestDeploy_TheObserveListWatchesTheExecutor(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repoRoot, "deploy", "observe.example.json"))
	if err != nil {
		t.Fatalf("read the observe example: %v", err)
	}

	list := hostprobe.Units(data, nil)

	if len(list.Problems) != 0 {
		t.Fatalf("the observe example has problems: %v", list.Problems)
	}
	if len(list.Valid) != 1 || list.Valid[0] != "meridian-control.path" {
		t.Errorf("watched units = %v, want the control trigger so a dead executor is visible", list.Valid)
	}
}
