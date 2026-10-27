package hostexec_test

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/autumnsgrove/meridian/internal/hostexec"
)

// mainArgs points the command at files the test owns rather than at /etc/meridian, so nothing here
// needs root and nothing can touch the real install.
func mainArgs(t *testing.T, dir string) []string {
	t.Helper()
	return []string{
		"--host", filepath.Join(dir, "host.json"),
		"--targets", filepath.Join(dir, "targets.json"),
		"--lock", filepath.Join(dir, "lock"),
	}
}

func TestMain_RefusesToRunWithoutTrustedConfiguration(t *testing.T) {
	var stderr bytes.Buffer

	// The files do not exist, and if they did they would belong to the test user rather than root.
	// Either way nothing may run, and the run has to say so.
	code := hostexec.Main(mainArgs(t, t.TempDir()), &stderr)

	if code != 1 {
		t.Fatalf("exit code = %d, want 1 so the unit shows a failed run\n%s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "refusing to run") || !strings.Contains(stderr.String(), "host.json") {
		t.Errorf("stderr = %q, want a refusal that names the file", stderr.String())
	}
	if strings.Contains(stderr.String(), "targets.json") && !strings.Contains(stderr.String(), "host.json") {
		t.Error("the refusal should start with host.json")
	}
}

func TestMain_TakesNoArgumentsAndPrintsItsUsage(t *testing.T) {
	var stderr bytes.Buffer

	if code := hostexec.Main([]string{"extra"}, &stderr); code != 2 {
		t.Errorf("exit code = %d, want 2 for a positional argument", code)
	}
	if !strings.Contains(stderr.String(), "usage: meridian-host exec") {
		t.Errorf("stderr = %q, want the usage", stderr.String())
	}

	stderr.Reset()
	if code := hostexec.Main([]string{"--help"}, &stderr); code != 0 {
		t.Errorf("exit code = %d, want 0 for --help", code)
	}
}
