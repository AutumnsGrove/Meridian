package hostexec_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"os/user"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/autumnsgrove/meridian/internal/hostexec"
)

// The runner is the only place the executor starts a process, so these tests run real processes. The
// helper below is this test binary re-executed: the runner gives its child a minimal environment, so
// the marker that says "you are the child" travels in argv rather than in an environment variable.
const helperMarker = "meridian-runner-helper"

func TestRunnerHelperProcess(t *testing.T) {
	if !slices.Contains(os.Args, helperMarker) {
		t.Skip("only runs as the child of a runner test")
	}
	mode := ""
	if index := slices.Index(os.Args, helperMarker); index+1 < len(os.Args) {
		mode = os.Args[index+1]
	}

	switch mode {
	case "tree":
		// A child that refuses to die politely, with a grandchild of its own: only a process-group
		// kill takes both down.
		signals := make(chan os.Signal, 1)
		signal.Notify(signals, syscall.SIGTERM)
		child := exec.CommandContext(context.Background(), lookPath(t, "sleep"), "30") //nolint:gosec // a fixed path, and the point is a real grandchild
		if err := child.Start(); err != nil {
			t.Fatalf("start the grandchild: %v", err)
		}
		fmt.Printf("grandchild=%d\n", child.Process.Pid)
		<-signals
		_ = child.Wait()
	case "flood":
		for index := 0; index < 4000; index++ {
			fmt.Printf("%05d a line of output from a noisy command\n", index)
		}
		fmt.Print("TAIL-MARKER\n")
	case "where":
		where, err := os.Getwd()
		if err != nil {
			t.Fatalf("getwd: %v", err)
		}
		fmt.Println(where)
	default:
		t.Fatalf("the runner helper was called with no mode: %q", os.Args)
	}
}

// lookPath resolves one of the programs these tests run, so they do not assume /bin or /usr/bin.
func lookPath(t *testing.T, program string) string {
	t.Helper()
	path, err := exec.LookPath(program)
	if err != nil {
		t.Fatalf("%s is not installed: %v", program, err)
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		t.Fatalf("abs %s: %v", path, err)
	}
	return absolute
}

// firstLine is the helper's own answer. The test binary prints "PASS" after a helper run, which is
// the framework, not the command.
func firstLine(output string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(output), "\n")
	return strings.TrimSpace(line)
}

func helperArgv(mode string) []string {
	return []string{os.Args[0], "-test.run=TestRunnerHelperProcess", "--", helperMarker, mode}
}

func runFor(t *testing.T, command hostexec.Command, timeout time.Duration) (exitCode int, output string, err error) {
	t.Helper()
	exitCode, output, _, err = runLogged(t, command, timeout)
	return exitCode, output, err
}

func runLogged(t *testing.T, command hostexec.Command, timeout time.Duration) (exitCode int, output string, logged []string, err error) {
	t.Helper()
	run := hostexec.Runner(func(format string, values ...any) {
		logged = append(logged, fmt.Sprintf(format, values...))
	})
	ctx, cancel := context.WithTimeout(t.Context(), timeout)
	defer cancel()
	exitCode, output, err = run(ctx, command)
	return exitCode, output, logged, err
}

func TestRunner_GivesTheCommandTheMinimalEnvironment(t *testing.T) {
	root, err := user.Lookup("root")
	if err != nil {
		t.Fatalf("look up root: %v", err)
	}

	exitCode, output, err := runFor(t, hostexec.Command{Argv: []string{lookPath(t, "env")}}, 10*time.Second)
	if err != nil {
		t.Fatalf("run env: %v", err)
	}
	if exitCode != 0 {
		t.Fatalf("env exited %d, want 0", exitCode)
	}

	got := strings.Split(strings.TrimSpace(output), "\n")
	sort.Strings(got)
	want := []string{
		"HOME=" + root.HomeDir,
		"LANG=C.UTF-8",
		"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
	}
	sort.Strings(want)
	if !slices.Equal(got, want) {
		t.Errorf("environment = %q\nwant exactly %q, because nothing is inherited", got, want)
	}
}

func TestRunner_ReportsTheExitCodeAndBothStreams(t *testing.T) {
	tests := []struct {
		name string
		argv []string
		want int
	}{
		{name: "success", argv: []string{lookPath(t, "true")}},
		{name: "failure", argv: []string{lookPath(t, "false")}, want: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			exitCode, _, err := runFor(t, hostexec.Command{Argv: test.argv}, 10*time.Second)
			if err != nil {
				t.Fatalf("run %q: %v, want a non-zero exit to be data, not an error", test.argv, err)
			}
			if exitCode != test.want {
				t.Errorf("exit code = %d, want %d", exitCode, test.want)
			}
		})
	}

	_, output, err := runFor(t, hostexec.Command{Argv: helperArgv("where")}, 30*time.Second)
	if err != nil {
		t.Fatalf("run the helper: %v", err)
	}
	where, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	assertSamePath(t, firstLine(output), where)
}

func TestRunner_RunsInTheCommandsDirectory(t *testing.T) {
	dir := t.TempDir()

	_, output, err := runFor(t, hostexec.Command{Argv: helperArgv("where"), Dir: dir}, 30*time.Second)
	if err != nil {
		t.Fatalf("run the helper: %v", err)
	}
	assertSamePath(t, firstLine(output), dir)
}

func TestRunner_FailsForAProgramThatIsNotThere(t *testing.T) {
	_, _, err := runFor(t, hostexec.Command{Argv: []string{"/nonexistent/meridian-test"}}, 10*time.Second)
	if err == nil {
		t.Fatal("running a missing program returned no error")
	}
	if !strings.Contains(err.Error(), "meridian-test") {
		t.Errorf("error = %v, want it to name the program so the journal explains itself", err)
	}
}

func TestRunner_FailsForAUserWhoIsNotThere(t *testing.T) {
	command := hostexec.Command{Argv: []string{lookPath(t, "true")}, RunAs: "meridian-no-such-user"}
	_, _, err := runFor(t, command, 10*time.Second)
	if err == nil {
		t.Fatal("running as a user who does not exist returned no error")
	}
}

func TestRunner_KeepsTheTailAndLogsTheRest(t *testing.T) {
	exitCode, output, logged, err := runLogged(t, hostexec.Command{Argv: helperArgv("flood")}, 30*time.Second)
	if err != nil || exitCode != 0 {
		t.Fatalf("run the flood: exit %d, %v", exitCode, err)
	}
	if len(output) > 64<<10 {
		t.Errorf("captured %d bytes, want at most the 64 KiB tail", len(output))
	}
	if !strings.Contains(output, "TAIL-MARKER") || strings.Contains(output, "00000 a line") {
		t.Errorf("captured %d bytes that did not keep the tail and drop the head: %q",
			len(output), output[max(0, len(output)-80):])
	}
	if len(logged) == 0 {
		t.Error("nothing reached the journal, so a command's full output would be lost")
	}
}

func TestRunner_KillsTheProcessGroupOnTimeout(t *testing.T) {
	started := time.Now()
	exitCode, output, err := runFor(t, hostexec.Command{Argv: helperArgv("tree")}, 2*time.Second)
	elapsed := time.Since(started)

	if err == nil {
		t.Fatalf("a command that outlived its context returned no error (exit %d, output %q)", exitCode, output)
	}
	if !strings.Contains(err.Error(), context.DeadlineExceeded.Error()) {
		t.Fatalf("error = %v, want context.DeadlineExceeded", err)
	}
	if elapsed > 15*time.Second {
		t.Errorf("the timeout took %v to take effect; the child ignores SIGTERM on purpose", elapsed)
	}

	grandchild := strings.TrimPrefix(firstLine(output), "grandchild=")
	pid, convErr := strconv.Atoi(grandchild)
	if convErr != nil {
		t.Fatalf("output = %q, want the grandchild's pid; without it the test proves nothing", output)
	}
	// A kill is asynchronous: the grandchild has to be reaped by init before it disappears.
	deadline := time.Now().Add(5 * time.Second)
	for syscall.Kill(pid, 0) == nil {
		if time.Now().After(deadline) {
			t.Fatalf("the grandchild (pid %d) outlived the timeout, so the kill missed the process group", pid)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// assertSamePath compares two paths after resolving symlinks: macOS hands out /private/var for
// /var, and the runner only has to respect what it was given.
func assertSamePath(t *testing.T, got, want string) {
	t.Helper()
	resolvedGot, gotErr := filepath.EvalSymlinks(got)
	resolvedWant, wantErr := filepath.EvalSymlinks(want)
	if gotErr != nil || wantErr != nil {
		t.Fatalf("resolve %q and %q: %v, %v", got, want, gotErr, wantErr)
	}
	if resolvedGot != resolvedWant {
		t.Errorf("path = %q, want %q", got, want)
	}
}

func TestRunner_SwitchesUserWhenItIsRoot(t *testing.T) {
	if os.Getuid() != 0 {
		t.Skip("switching user needs root; CI runs this in the container image job only when it can")
	}
	account, err := user.Lookup("nobody")
	if err != nil {
		t.Skipf("no nobody user on this box: %v", err)
	}

	exitCode, output, err := runFor(t, hostexec.Command{Argv: []string{lookPath(t, "id"), "-u"}, RunAs: "nobody"}, 10*time.Second)
	if err != nil || exitCode != 0 {
		t.Fatalf("run as nobody: exit %d, %v", exitCode, err)
	}
	if strings.TrimSpace(output) != account.Uid {
		t.Errorf("uid = %q, want %q: a deploy-user command must not run as root", strings.TrimSpace(output), account.Uid)
	}
	if home, err := user.Lookup("nobody"); err == nil && !filepath.IsAbs(home.HomeDir) {
		t.Errorf("HOME for nobody is %q, want an absolute path", home.HomeDir)
	}
}
