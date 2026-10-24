package cli_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/autumnsgrove/meridian/internal/cli"
)

func run(args ...string) (code int, stdout, stderr string) {
	var out, errOut bytes.Buffer
	code = cli.Main(args, &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestMain_VersionPrintsBuildLine(t *testing.T) {
	code, stdout, stderr := run("version")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %q)", code, stderr)
	}
	if !strings.Contains(stdout, "meridian dev") {
		t.Errorf("stdout = %q, want a line naming meridian and its version", stdout)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want empty", stderr)
	}
}

func TestMain_UnknownCommandPrintsUsageToStderr(t *testing.T) {
	code, stdout, stderr := run("frobnicate")

	if code == 0 {
		t.Fatalf("exit code = 0, want non-zero")
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}
	for _, want := range []string{"unknown command", "usage:"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr = %q, want it to contain %q", stderr, want)
		}
	}
}

func TestMain_NoArgumentsPrintsUsageToStderr(t *testing.T) {
	code, stdout, stderr := run()

	if code == 0 {
		t.Fatalf("exit code = 0, want non-zero")
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}
	if !strings.Contains(stderr, "usage:") {
		t.Errorf("stderr = %q, want usage", stderr)
	}
}

func TestMain_HelpPrintsUsageToStdout(t *testing.T) {
	for _, arg := range []string{"help", "-h", "--help"} {
		t.Run(arg, func(t *testing.T) {
			code, stdout, stderr := run(arg)

			if code != 0 {
				t.Fatalf("exit code = %d, want 0 (stderr: %q)", code, stderr)
			}
			if !strings.Contains(stdout, "usage:") {
				t.Errorf("stdout = %q, want usage", stdout)
			}
			if stderr != "" {
				t.Errorf("stderr = %q, want empty", stderr)
			}
		})
	}
}

func TestVersion_UnstampedBuildIsDev(t *testing.T) {
	want := "meridian dev (commit dev, built dev)"
	if got := cli.Version(); got != want {
		t.Errorf("Version() = %q, want %q", got, want)
	}
}
