// Package invariants_test mechanises the hard constraints in CLAUDE.md, so a change that drifts
// from them fails the build instead of relying on a reviewer's memory.
//
// Every rule is a function from a repository root to the first violation, and the table below binds
// each rule to a directory under testdata/ that deliberately breaks it. A rule without a failing
// fixture would be decoration, so the fixture test fails if one stops being caught.
package invariants_test

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// repoRoot is two directories above internal/invariants.
const repoRoot = "../.."

// hardConstraints is the whole rulebook. Add a row for a new constraint; do not add branches here.
var hardConstraints = []struct {
	name     string
	fixtures []string
	check    func(root string) error
}{
	{
		name:     "no service names in code",
		fixtures: []string{"no-service-names-go", "no-service-names-web"},
		check:    checkNoServiceNames,
	},
	{
		name:     "no shell execution",
		fixtures: []string{"shell"},
		check:    checkNoShell,
	},
	{
		name:     "os/exec is confined to the host side (when present)",
		fixtures: []string{"exec-confinement"},
		check:    checkExecConfinement,
	},
	{
		name:     "systemctl and Docker literals stay on the host side (when present)",
		fixtures: []string{"host-literals"},
		check:    checkHostLiterals,
	},
	{
		name:     "the host side is standard library only (when present)",
		fixtures: []string{"host-stdlib"},
		check:    checkHostStdlib,
	},
	{
		name:     "the compose file keeps the container's privilege boundary",
		fixtures: []string{"compose-bridge-network", "compose-docker-socket", "compose-published-port"},
		check:    checkComposeInvariants,
	},
	{
		name:     "dependencies are on the allowlist",
		fixtures: []string{"dependencies"},
		check:    checkDependencyAllowlist,
	},
	{
		name:     "doctor is read only (when present)",
		fixtures: []string{"doctor-read-only"},
		check:    checkDoctorReadOnly,
	},
	{
		name:     "no authentication surface (when present)",
		fixtures: []string{"auth-surface"},
		check:    checkNoAuthSurface,
	},
	{
		name:     "no in-place JSON writes (when present)",
		fixtures: []string{"in-place-writes"},
		check:    checkNoInPlaceWrites,
	},
	{
		name:     "only POST /api/control mutates (when present)",
		fixtures: []string{"mutating-route"},
		check:    checkOneMutatingRoute,
	},
	{
		name:     "os.Exit and log.Fatal live only in cmd/",
		fixtures: []string{"exit-outside-cmd"},
		check:    checkExitOnlyInCmd,
	},
}

// TestHardConstraints_Hold is the gate: today's tree obeys every rule. Rules whose packages do not
// exist yet pass vacuously; their names say "when present" until the package lands.
func TestHardConstraints_Hold(t *testing.T) {
	for _, rule := range hardConstraints {
		t.Run(rule.name, func(t *testing.T) {
			if err := rule.check(repoRoot); err != nil {
				t.Error(err)
			}
		})
	}
}

// TestHardConstraints_CanFail proves each rule against a fixture that breaks it. This is what keeps
// the rulebook honest: a rule that cannot fail is not enforcing anything.
func TestHardConstraints_CanFail(t *testing.T) {
	for _, rule := range hardConstraints {
		t.Run(rule.name, func(t *testing.T) {
			for _, name := range rule.fixtures {
				fixture := filepath.Join("testdata", name)
				if _, err := os.Stat(fixture); err != nil {
					t.Fatalf("fixture %s is missing: %v", fixture, err)
				}
				err := rule.check(fixture)
				if err == nil {
					t.Fatalf("rule passed on its own failing fixture in %s, so the rule cannot fail", fixture)
				}
				t.Logf("%s caught as expected: %v", name, err)
			}
		})
	}
}

// TestHardConstraints_PassOnEmptyTree guards against rules that invent a violation out of nothing,
// which is what a false positive in CI would look like.
func TestHardConstraints_PassOnEmptyTree(t *testing.T) {
	empty := t.TempDir()
	for _, rule := range hardConstraints {
		t.Run(rule.name, func(t *testing.T) {
			if err := rule.check(empty); err != nil {
				t.Errorf("rule failed on an empty tree: %v", err)
			}
		})
	}
}

// TestServiceWords_ReadIDsNamesAndHosts proves the no-service-names rule is not just the hard-coded
// list: ids, names and URL hosts come from the example config, while loopback hosts and the
// project's own name stay allowed.
func TestServiceWords_ReadIDsNamesAndHosts(t *testing.T) {
	words, err := serviceWords(filepath.Join(repoRoot, "examples", "homelab.yaml"))
	if err != nil {
		t.Fatalf("serviceWords: %v", err)
	}

	for _, want := range []string{
		"polaris",
		"adguard home",
		"her-go",
		"tailscale",
		"100.81.103.51",
		"lepotato.taild95077.ts.net",
		"everyday",
		"backbone",
	} {
		if !slices.Contains(words, want) {
			t.Errorf("serviceWords() = %q, want it to include %q", words, want)
		}
	}
	for _, unwanted := range []string{"", "localhost", "127.0.0.1", "meridian"} {
		if slices.Contains(words, unwanted) {
			t.Errorf("serviceWords() = %q, want it to leave out %q", words, unwanted)
		}
	}
}

// TestServiceWords_MissingConfigIsVacuous covers the window before examples/homelab.yaml exists.
func TestServiceWords_MissingConfigIsVacuous(t *testing.T) {
	words, err := serviceWords(filepath.Join(t.TempDir(), "homelab.yaml"))
	if err != nil {
		t.Fatalf("serviceWords on a missing file: %v", err)
	}
	if !slices.Contains(words, "polaris") {
		t.Errorf("serviceWords() = %q, want the hard-coded names even without a config", words)
	}
}
