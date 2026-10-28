package hostexec

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"os/user"
	"strconv"
	"syscall"
	"time"
)

// The Commander that actually starts processes (docs/agent/06-control.md "T28"). It is the only
// os/exec use in the executor, and it inherits nothing: the environment is the three variables below,
// the working directory and the user come from the allowlist, and the whole process group dies on a
// timeout (STANDARDS section 4 "Privilege").

const (
	// commandPath is the PATH a command gets. The deploy user's interactive PATH is not what a
	// systemd oneshot inherits, and Go lives in /usr/local/bin on the homelab box (CONTROL.md).
	commandPath = "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
	// commandLang keeps a program's output parseable regardless of the caller's locale.
	commandLang = "C.UTF-8"
	// maxCaptured is how much of a command's output the result may carry. The executor clips what
	// reaches the page; the journal gets the rest.
	maxCaptured = 64 << 10
	// waitDelay bounds the wait for the output pipes after the process group has been killed.
	waitDelay = 5 * time.Second
)

// Runner returns the Commander that runs a command for real. logf receives the output that does not
// fit in the captured tail, so a long failure is still explainable from the journal.
func Runner(logf func(string, ...any)) Commander {
	return func(ctx context.Context, command Command) (int, string, error) {
		if len(command.Argv) == 0 {
			return 0, "", errors.New("hostexec: a command with no argv")
		}
		home, credential, err := identity(command.RunAs)
		if err != nil {
			return 0, "", err
		}

		process := exec.CommandContext(ctx, command.Argv[0], command.Argv[1:]...) //nolint:gosec // argv comes from the root-owned allowlist, never from a request
		process.Dir = command.Dir
		process.Env = []string{"PATH=" + commandPath, "HOME=" + home, "LANG=" + commandLang}
		process.SysProcAttr = &syscall.SysProcAttr{Credential: credential}
		if !command.NoNewGroup {
			process.SysProcAttr.Setpgid = true
		}
		process.WaitDelay = waitDelay
		// Cancel is the whole point of the process group: an updater that spawned a build must not
		// leave it running after the timeout took its parent. A NoNewGroup command shares the caller's
		// group, so killing that group here would kill the caller too; kill just this child.
		if command.NoNewGroup {
			process.Cancel = func() error {
				return process.Process.Kill()
			}
		} else {
			process.Cancel = func() error {
				return syscall.Kill(-process.Process.Pid, syscall.SIGKILL)
			}
		}
		captured := &tailBuffer{limit: maxCaptured, spill: logf}
		process.Stdout = captured
		process.Stderr = captured

		runErr := process.Run()
		output := captured.String()
		switch {
		case runErr == nil:
			return 0, output, nil
		case ctx.Err() != nil:
			// A timeout is a timeout even if the process exited on its own a moment later.
			return exitCodeOf(process), output, ctx.Err()
		}
		var exitError *exec.ExitError
		if errors.As(runErr, &exitError) {
			// A non-zero exit is data: the executor turns it into `exec_failed` and keeps the output.
			return exitError.ExitCode(), output, nil
		}
		return 0, output, fmt.Errorf("hostexec: run %s: %w", command.Argv[0], runErr)
	}
}

// identity resolves the user a command runs as and the environment it needs. An empty RunAs is root,
// which needs no Credential: the executor is already root, and asking syscall to setuid to 0 would
// only make the tests depend on being root.
func identity(runAs string) (home string, credential *syscall.Credential, err error) {
	name := runAs
	if name == "" {
		name = "root"
	}
	account, err := user.Lookup(name)
	if err != nil {
		return "", nil, fmt.Errorf("hostexec: run as %q: %w", name, err)
	}
	if runAs == "" {
		return account.HomeDir, nil, nil
	}

	uid, err := strconv.ParseUint(account.Uid, 10, 32)
	if err != nil {
		return "", nil, fmt.Errorf("hostexec: run as %q: uid %q: %w", name, account.Uid, err)
	}
	gid, err := strconv.ParseUint(account.Gid, 10, 32)
	if err != nil {
		return "", nil, fmt.Errorf("hostexec: run as %q: gid %q: %w", name, account.Gid, err)
	}
	// The supplementary groups are what make `docker` usable: the deploy user's membership of the
	// docker group is the only reason a compose command can talk to the daemon at all.
	groupIDs, err := account.GroupIds()
	if err != nil {
		return "", nil, fmt.Errorf("hostexec: run as %q: groups: %w", name, err)
	}
	groups := make([]uint32, 0, len(groupIDs))
	for _, groupID := range groupIDs {
		parsed, err := strconv.ParseUint(groupID, 10, 32)
		if err != nil {
			return "", nil, fmt.Errorf("hostexec: run as %q: group %q: %w", name, groupID, err)
		}
		groups = append(groups, uint32(parsed))
	}
	return account.HomeDir, &syscall.Credential{Uid: uint32(uid), Gid: uint32(gid), Groups: groups}, nil
}

// exitCodeOf is a process's exit code, or 0 when it never got one (it was killed by the timeout).
func exitCodeOf(process *exec.Cmd) int {
	if process.ProcessState == nil {
		return 0
	}
	return process.ProcessState.ExitCode()
}

// tailBuffer keeps the last limit bytes written to it and hands the rest to the log, so the result
// carries the end of a failure and the journal carries everything.
type tailBuffer struct {
	limit int
	spill func(string, ...any)
	data  []byte
}

// Write never fails: a command's output is information, not a reason to stop reading its pipe.
func (buffer *tailBuffer) Write(p []byte) (int, error) {
	buffer.data = append(buffer.data, p...)
	if excess := len(buffer.data) - buffer.limit; excess > 0 {
		if buffer.spill != nil {
			buffer.spill("%s", string(buffer.data[:excess]))
		}
		buffer.data = append(buffer.data[:0], buffer.data[excess:]...)
	}
	return len(p), nil
}

func (buffer *tailBuffer) String() string {
	return string(buffer.data)
}
