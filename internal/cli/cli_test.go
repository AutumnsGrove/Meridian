package cli_test

import (
	"bytes"
	"path/filepath"
	"runtime"
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

func TestVersion_UnstampedBuildIsDevAndNamesThePlatform(t *testing.T) {
	want := "meridian dev (commit dev, built dev, " + runtime.GOOS + "/" + runtime.GOARCH + ")"
	if got := cli.Version(); got != want {
		t.Errorf("Version() = %q, want %q", got, want)
	}
}

func TestMain_UnknownFlagIsAUsageError(t *testing.T) {
	code, stdout, stderr := run("run", "--nope")

	if code != 2 {
		t.Fatalf("exit code = %d, want 2 (stderr: %q)", code, stderr)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}
	for _, want := range []string{"unknown flag", "usage:"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr = %q, want it to contain %q", stderr, want)
		}
	}
}

func TestMain_RunWithoutAConfigFails(t *testing.T) {
	t.Setenv("MERIDIAN_CONFIG", filepath.Join(t.TempDir(), "missing.yaml"))

	code, stdout, stderr := run("run")

	if code != 1 {
		t.Fatalf("exit code = %d, want 1 (stderr: %q)", code, stderr)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty: a failed run prints nothing on the success stream", stdout)
	}
	if !strings.Contains(stderr, "missing.yaml") {
		t.Errorf("stderr = %q, want it to name the config file", stderr)
	}
}

// FuzzMain holds the dispatch contract for any argument: it never panics, the exit code is one of the
// documented three (0 ok, 1 failed while doing the work, 2 the request was not understood), and the
// output lands on the side that matches.
func FuzzMain(f *testing.F) {
	for _, seed := range []string{"version", "help", "-h", "--fast", "", "ünïcode", "x"} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, arg string) {
		// These two would start a server or dial one; the run and healthcheck tests cover them.
		if arg == "run" || arg == "healthcheck" {
			t.Skip()
		}
		t.Setenv("MERIDIAN_CONFIG", filepath.Join(t.TempDir(), "absent.yaml"))

		code, stdout, stderr := run(arg)

		switch code {
		case 0:
			if stdout == "" {
				t.Errorf("args %q: exit 0 wrote nothing to stdout (stderr %q)", arg, stderr)
			}
		case 1, 2:
			if stderr == "" {
				t.Errorf("args %q: exit %d wrote nothing to stderr", arg, code)
			}
		default:
			t.Fatalf("args %q: exit code = %d, want 0, 1 or 2", arg, code)
		}
	})
}

func TestNewLogger_DefaultsToInfoAndReadsTheLevelFromTheEnvironment(t *testing.T) {
	tests := []struct {
		name      string
		level     string
		wantDebug bool
	}{
		{name: "unset means info", level: "", wantDebug: false},
		{name: "a typo means info too, never silence", level: "loud", wantDebug: false},
		{name: "debug when asked", level: "debug", wantDebug: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var lines bytes.Buffer
			logger := cli.NewLogger(&lines, func(key string) string {
				if key == "MERIDIAN_LOG" {
					return test.level
				}
				return ""
			})

			logger.Debug("a debug line")
			logger.Info("an info line")

			text := lines.String()
			if !strings.Contains(text, "an info line") {
				t.Errorf("the info line was dropped: %q", text)
			}
			if got := strings.Contains(text, "a debug line"); got != test.wantDebug {
				t.Errorf("debug line present = %v, want %v (log %q)", got, test.wantDebug, text)
			}
		})
	}
}
