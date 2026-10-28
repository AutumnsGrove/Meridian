package invariants_test

import (
	"errors"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"testing"
)

// TestInstall_DryRunPrintsThePlanAndTouchesNothing is the installer's test: shellcheck proves the
// syntax, and this proves the dry run is a real plan (and not a real install) on the machine running
// the suite. It deliberately passes --dir and --image-digest so it never reads /etc, systemd or GHCR.
func TestInstall_DryRunPrintsThePlanAndTouchesNothing(t *testing.T) {
	tmp := t.TempDir()
	fakeHost := filepath.Join(tmp, "meridian-host")
	if err := os.WriteFile(fakeHost, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil { //nolint:gosec // a fake executable, not a secret
		t.Fatalf("write the fake host binary: %v", err)
	}
	target := filepath.Join(tmp, "Meridian")
	digest := "sha256:" + strings.Repeat("a", 64)

	account := os.Getenv("USER")
	if account == "" {
		current, err := user.Current()
		if err != nil {
			t.Fatalf("work out the current user: %v", err)
		}
		account = current.Username
	}

	script, err := filepath.Abs(filepath.Join(repoRoot, "deploy", "install.sh"))
	if err != nil {
		t.Fatalf("resolve install.sh: %v", err)
	}
	command := exec.CommandContext(t.Context(), "bash", script, //nolint:gosec // the script is the repo's own install.sh; running it dry is the test
		"--dry-run",
		"--deploy-user", account,
		"--dir", target,
		"--host-binary", fakeHost,
		"--image-digest", digest,
	)
	command.Dir = repoRoot
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("install.sh --dry-run: %v\n%s", err, output)
	}
	text := string(output)
	for _, want := range []string{
		"would: install " + fakeHost + " -> /etc/meridian/meridian-host",
		"would: create " + target,
		"would: write " + filepath.Join(target, ".env"),
		"tailscale serve --bg --https=8891",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("dry run does not mention %q:\n%s", want, text)
		}
	}
	if _, err := os.Stat(target); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the dry run created %s", target)
	}
}
