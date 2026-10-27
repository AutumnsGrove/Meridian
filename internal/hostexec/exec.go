package hostexec

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/autumnsgrove/meridian/internal/atomicfile"
	"github.com/autumnsgrove/meridian/internal/hostfile"
	"github.com/autumnsgrove/meridian/internal/wire"
)

// The paths the installer writes, and the limits the run works inside. Everything under the control
// directory is treated as hostile: the container can write the queue, so the queue is never trusted.
const (
	DefaultHostFile    = "/etc/meridian/host.json"
	DefaultTargetsFile = "/etc/meridian/targets.json"
	DefaultLockFile    = "/run/lock/meridian-control.lock"

	// maxRequestsPerRun bounds one invocation. The path unit fires again for whatever is left, which
	// keeps a single oneshot well inside its TimeoutStartSec (T31).
	maxRequestsPerRun = 64
	// historyDepth is how many results Decide sees: enough for a replay check and a coalescing
	// window several actions deep.
	historyDepth = 200
	// verifyInterval and verifyAttempts give a unit 10s to answer is-active after the action, which
	// is the window docs/CONTROL.md promises.
	verifyInterval = 500 * time.Millisecond
	verifyAttempts = 20
	// resultRetention is how long a result file stays; the page only lists the last few.
	resultRetention = 7 * 24 * time.Hour
	// auditRetention is how long an audit line stays (decision #32).
	auditRetention = 365 * 24 * time.Hour
	// maxOutputSize is the tail of a command's output that reaches the result.
	maxOutputSize = 4 << 10
	// maxAuditSize bounds the audit file the prune pass will rewrite. A year of actions is tiny.
	maxAuditSize = 16 << 20
)

// Commander runs one command and reports its exit code and output. T28 supplies the real one, which is
// the only place the executor starts a process.
type Commander func(ctx context.Context, command Command) (exitCode int, output string, err error)

// Executor is one run of `meridian-host exec`.
type Executor struct {
	HostFile    string
	TargetsFile string
	LockFile    string
	// Owner is the uid the trusted files must belong to: root in production, the test user in tests.
	Owner uint32
	// Build and Run are the command seams: Builders.Build and NewRunner in production, a recorded
	// fake in tests. Neither has a default, because a missing seam must be a compile error at the
	// call site rather than a silent no-op.
	Build Builder
	Run   Commander
	// Now is the clock. Nil means the wall clock.
	Now func() time.Time
	// Wait pauses between post-check attempts. Nil waits for real; tests pass a no-op so a check that
	// never answers costs no wall time.
	Wait func(ctx context.Context, delay time.Duration) error
	// Random supplies ids for the audit lines of files that are not requests. Nil means crypto/rand.
	Random io.Reader
	// Log receives the journal lines. Nil is silent.
	Log *log.Logger
}

// ErrUnsafeConfig means the root-owned files this run depends on are missing or not trustworthy, so
// nothing may execute at all.
var ErrUnsafeConfig = errors.New("hostexec: refusing to run")

// Execute is the whole run: trusted config, the lock, prune, then the queue. It returns an error only
// when the run itself must not happen; a refused or failed request is a result, not an error.
func (exec *Executor) Execute(ctx context.Context) error {
	host, targets, err := exec.loadTrusted()
	if err != nil {
		return err
	}

	release, held, err := acquireLock(exec.lockFile())
	if err != nil {
		return fmt.Errorf("%w: %s: %w", ErrUnsafeConfig, exec.lockFile(), err)
	}
	if held {
		// Another invocation is working through the same queue. It will pick these up; the path
		// unit fires again for anything it leaves behind.
		return nil
	}
	defer release()

	dirs := controlDirs(host.ControlDir)
	exec.prune(dirs)

	root, err := os.OpenRoot(dirs.inbox)
	if err != nil {
		return fmt.Errorf("%w: the control inbox: %w", ErrUnsafeConfig, err)
	}
	defer func() { _ = root.Close() }()

	history := loadHistory(dirs.results)

	processed := 0
	for processed < maxRequestsPerRun {
		requests, junk, err := scanQueue(root)
		if err != nil {
			return fmt.Errorf("hostexec: list the queue: %w", err)
		}
		for _, name := range junk {
			if processed >= maxRequestsPerRun {
				return nil
			}
			exec.discard(dirs, root, name)
			processed++
		}
		if len(requests) == 0 {
			return nil
		}
		for _, name := range requests {
			if processed >= maxRequestsPerRun {
				return nil
			}
			if ctx.Err() != nil {
				return nil
			}
			history = exec.handle(ctx, dirs, root, name, targets, host.DeployUser, history)
			processed++
		}
	}
	return nil
}

// discard deletes a queue entry that is not a request, without ever opening it, and records that it
// happened. The name itself is not copied into the audit log: it is attacker-chosen text, and the
// journal is where it belongs.
func (exec *Executor) discard(dirs dirs, root *os.Root, name string) {
	exec.logf("discarding a queue entry that is not a request: %q", name)
	if err := root.Remove(filepath.Join("queue", name)); err != nil {
		exec.logf("could not discard %q: %v", name, err)
		return
	}
	now := exec.now()
	id, err := wire.NewID(now, exec.randomness())
	if err != nil {
		return
	}
	exec.appendAudit(dirs, wire.Result{ID: id, Code: wire.CodeBadRequest, StartedAt: now, FinishedAt: now})
}

// dirs are the paths under the control directory. One writable mount holds the inbox; the outbox is
// read-only to the container and written only here (decision #63).
type dirs struct {
	inbox   string
	queue   string
	outbox  string
	results string
	audit   string
}

func controlDirs(controlDir string) dirs {
	inbox := filepath.Join(controlDir, "inbox")
	outbox := filepath.Join(controlDir, "outbox")
	return dirs{
		inbox:   inbox,
		queue:   filepath.Join(inbox, "queue"),
		outbox:  outbox,
		results: filepath.Join(outbox, "results"),
		audit:   filepath.Join(outbox, "audit.jsonl"),
	}
}

// loadTrusted reads both root-owned files and refuses anything it cannot trust completely.
func (exec *Executor) loadTrusted() (HostConfig, Targets, error) {
	trust := hostfile.Trust{Owner: exec.Owner, MaxSize: MaxTrustedSize}

	hostData, err := hostfile.Read(exec.hostFile(), trust)
	if err != nil {
		return HostConfig{}, nil, fmt.Errorf("%w: %w", ErrUnsafeConfig, err)
	}
	host, err := LoadHost(hostData)
	if err != nil {
		return HostConfig{}, nil, fmt.Errorf("%w: %w", ErrUnsafeConfig, err)
	}

	targetsData, err := hostfile.Read(exec.targetsFile(), trust)
	if err != nil {
		return HostConfig{}, nil, fmt.Errorf("%w: %w", ErrUnsafeConfig, err)
	}
	targets, err := LoadTargets(targetsData, host.DeployUser)
	if err != nil {
		return HostConfig{}, nil, fmt.Errorf("%w: %w", ErrUnsafeConfig, err)
	}
	return host, targets, nil
}

// handle processes one request file and returns the history with its result appended. Every path
// writes a result, appends an audit line and only then removes the request (decision #62).
func (exec *Executor) handle(ctx context.Context, dirs dirs, root *os.Root, name string, targets Targets,
	deployUser string, history []wire.Result) []wire.Result {
	id := strings.TrimSuffix(name, ".json")
	now := exec.now()
	result := wire.Result{ID: id, StartedAt: now, FinishedAt: now}

	defer func() {
		if recovered := recover(); recovered != nil {
			// A panic is a bug, but the request still gets an answer and the queue still moves.
			exec.logf("panic while handling a request: %v", recovered)
			result = wire.Result{ID: id, Code: wire.CodeInternal, StartedAt: now, FinishedAt: now}
		}
		result.Output = clipOutput(result.Output)
		if err := exec.writeResult(dirs, result); err != nil {
			exec.logf("could not write the result for %s: %v", id, err)
		}
		exec.appendAudit(dirs, result)
		if err := root.Remove(filepath.Join("queue", name)); err != nil {
			exec.logf("could not remove request %s: %v", id, err)
		}
	}()

	request, code := readRequest(root, name)
	if code != "" {
		result.Code = code
		return append(history, result)
	}
	if request.ID != id {
		result.Action, result.Target = request.Action, request.Target
		result.Code = wire.CodeBadID
		return append(history, result)
	}

	result.Action, result.Target = request.Action, request.Target
	decision := Decide(request, targets, history, now)
	if !decision.Run {
		result.OK, result.Code, result.Note = decision.OK, decision.Code, decision.Note
		return append(history, result)
	}

	started := exec.now()
	result.StartedAt = started
	command, err := exec.Build(BuildInput{
		Target: decision.Target, Action: decision.Action, ID: id, Ref: request.Ref, DeployUser: deployUser,
	})
	if err != nil {
		// The builder refused something the allowlist allowed: that is an implementation gap, not
		// the request's fault, and it must still be visible.
		exec.logf("no command for %s on %s: %v", decision.Action, request.Target, err)
		result.Code = wire.CodeInternal
		return append(history, result)
	}
	if command.ResultFirst {
		exec.markInProgress(dirs, result)
	}

	// One deadline for the action and its post-check: a command that never returns must not hold the
	// oneshot, and a timeout is `timeout`, not a hang.
	actionCtx, cancel := context.WithTimeout(ctx, timeoutOf(command))
	defer cancel()

	result.ExitCode, result.Output, err = exec.Run(actionCtx, command)
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		result.Code = wire.CodeTimeout
	case err != nil:
		exec.logf("%s on %s could not run: %v", request.Action, request.Target, err)
		result.Code = wire.CodeExecFailed
	case result.ExitCode != 0:
		result.Code = wire.CodeExecFailed
	case !exec.verify(actionCtx, command):
		result.Code = wire.CodeNotActiveAfter
	default:
		result.OK = true
	}
	result.FinishedAt = exec.now()
	result.DurationMS = result.FinishedAt.Sub(result.StartedAt).Milliseconds()
	return append(history, result)
}

// markInProgress writes the provisional result a self-restart or a reboot needs: the container is gone
// before it could report, so the story has to exist first (decision #64). A failed write is logged and
// the action still runs, because refusing to act is the worse failure.
func (exec *Executor) markInProgress(dirs dirs, result wire.Result) {
	result.InProgress = true
	result.OK = true
	result.Note = "in progress"
	if err := exec.writeResult(dirs, result); err != nil {
		exec.logf("could not write the provisional result for %s: %v", result.ID, err)
	}
}

// verify runs the post-check inside the action's deadline. A check that cannot run keeps trying: a
// unit that is still activating is not a failure yet.
func (exec *Executor) verify(ctx context.Context, command Command) bool {
	check := command.Verify
	if check == nil {
		return true
	}
	for attempt := 0; attempt < verifyAttempts; attempt++ {
		if attempt > 0 {
			if err := exec.wait(ctx, verifyInterval); err != nil {
				return false
			}
		}
		_, output, err := exec.Run(ctx, Command{
			Argv: check.Argv, RunAs: command.RunAs, Dir: command.Dir, Timeout: command.Timeout,
		})
		if err == nil && reportsWord(output, check.Want) {
			return true
		}
	}
	return false
}

// reportsWord looks for the word a post-check must print. systemctl is-active exits non-zero for a
// stopped unit, so the answer is the word, not the exit code.
func reportsWord(output, want string) bool {
	for _, line := range strings.Split(output, "\n") {
		if strings.TrimSpace(line) == want {
			return true
		}
	}
	return false
}

// timeoutOf is the builder's bound on the command, or the default when a builder forgot one.
func timeoutOf(command Command) time.Duration {
	if command.Timeout > 0 {
		return command.Timeout
	}
	return DefaultTimeout
}

// readRequest opens one queue entry and decodes it, returning the request or the code to refuse it
// with. The error from the decoder is deliberately dropped: a JSON error names fields from the file,
// and file contents never reach a log, a result or the browser (decision #62).
func readRequest(root *os.Root, name string) (wire.Request, wire.Code) {
	// O_NONBLOCK matters: without it, opening a FIFO someone planted blocks the executor forever.
	file, err := root.OpenFile(filepath.Join("queue", name),
		os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return wire.Request{}, wire.CodeBadRequest
	}
	defer func() { _ = file.Close() }()

	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > wire.MaxRequestSize {
		return wire.Request{}, wire.CodeBadRequest
	}
	request, err := wire.DecodeRequest(file)
	if err != nil {
		return wire.Request{}, wire.CodeBadRequest
	}
	return request, ""
}

// writeResult writes the one file the container reads back, atomically and root-owned (decision #36).
func (exec *Executor) writeResult(dirs dirs, result wire.Result) error {
	data, err := json.Marshal(result)
	if err != nil {
		return err
	}
	return atomicfile.Write(dirs.results, result.ID+".json", data, 0o644)
}

// appendAudit adds one line to the audit log: one O_APPEND write and an fsync, so a crash cannot leave
// half a line in front of the next one.
func (exec *Executor) appendAudit(dirs dirs, result wire.Result) {
	line := wire.AuditLine{
		At:         result.FinishedAt,
		ID:         result.ID,
		Action:     result.Action,
		Target:     result.Target,
		OK:         result.OK,
		Code:       result.Code,
		DurationMS: result.DurationMS,
		By:         wire.AuditBy,
		Output:     result.Output,
	}
	data, err := json.Marshal(line)
	if err != nil {
		exec.logf("could not marshal an audit line: %v", err)
		return
	}
	file, err := os.OpenFile(dirs.audit, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644) //nolint:gosec // a fixed path under the control directory
	if err != nil {
		exec.logf("could not open the audit log: %v", err)
		return
	}
	defer func() { _ = file.Close() }()
	if _, err := file.Write(append(data, '\n')); err != nil {
		exec.logf("could not append to the audit log: %v", err)
		return
	}
	if err := file.Sync(); err != nil {
		exec.logf("could not sync the audit log: %v", err)
	}
}

// prune drops old results and old audit lines. It is best effort: a prune that cannot run must not
// stop an action the owner is waiting for.
func (exec *Executor) prune(dirs dirs) {
	exec.pruneResults(dirs)
	exec.pruneAudit(dirs)
}

func (exec *Executor) pruneResults(dirs dirs) {
	entries, err := os.ReadDir(dirs.results)
	if err != nil {
		if !os.IsNotExist(err) {
			exec.logf("could not list results to prune: %v", err)
		}
		return
	}
	cutoff := exec.now().Add(-resultRetention)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil || info.ModTime().After(cutoff) {
			continue
		}
		if err := os.Remove(filepath.Join(dirs.results, entry.Name())); err != nil {
			exec.logf("could not prune %s: %v", entry.Name(), err)
		}
	}
}

func (exec *Executor) pruneAudit(dirs dirs) {
	file, err := os.Open(dirs.audit) //nolint:gosec // a fixed path under the control directory
	if err != nil {
		return
	}
	defer func() { _ = file.Close() }()

	info, err := file.Stat()
	if err != nil || info.Size() > maxAuditSize {
		if err == nil {
			exec.logf("the audit log is %d bytes; leaving it alone", info.Size())
		}
		return
	}
	data, err := io.ReadAll(io.LimitReader(file, maxAuditSize+1))
	if err != nil {
		exec.logf("could not read the audit log: %v", err)
		return
	}

	cutoff := exec.now().Add(-auditRetention)
	kept := make([]byte, 0, len(data))
	dropped := 0
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		audit, err := wire.DecodeAuditLine([]byte(line))
		if err != nil {
			// A corrupt line is kept as it is: deleting evidence is not pruning.
			kept = append(kept, line...)
			kept = append(kept, '\n')
			continue
		}
		if audit.At.Before(cutoff) {
			dropped++
			continue
		}
		kept = append(kept, line...)
		kept = append(kept, '\n')
	}
	if dropped == 0 {
		return
	}
	if err := atomicfile.Write(dirs.outbox, "audit.jsonl", kept, 0o644); err != nil {
		exec.logf("could not rewrite the audit log: %v", err)
	}
}

// scanQueue splits the queue into request files, oldest first, and everything else. A name that is not
// a request is never opened; it is listed so the caller can delete it.
func scanQueue(root *os.Root) (requests, junk []string, err error) {
	entries, err := fs.ReadDir(root.FS(), "queue")
	if err != nil {
		return nil, nil, err
	}
	for _, entry := range entries {
		name := entry.Name()
		stem, isJSON := strings.CutSuffix(name, ".json")
		if isJSON {
			if _, err := wire.ParseID(stem); err == nil {
				requests = append(requests, name)
				continue
			}
		}
		junk = append(junk, name)
	}
	sort.Strings(requests) // UUIDv7 names sort by request time
	sort.Strings(junk)
	return requests, junk, nil
}

// acquireLock takes the executor's lock without waiting. A run that finds it held does nothing at all:
// the requests stay queued and the next path trigger picks them up.
func acquireLock(path string) (release func(), held bool, err error) {
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, false, err
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = file.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, true, nil
		}
		return nil, false, err
	}
	return func() {
		_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
		_ = file.Close()
	}, false, nil
}

// loadHistory reads the newest results, newest first. A result that does not decode still counts by
// its file name: its existence is what makes a replay a replay.
func loadHistory(resultsDir string) []wire.Result {
	entries, err := os.ReadDir(resultsDir)
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		stem, isJSON := strings.CutSuffix(entry.Name(), ".json")
		if !isJSON {
			continue
		}
		if _, err := wire.ParseID(stem); err != nil {
			continue
		}
		names = append(names, entry.Name())
	}
	sort.Sort(sort.Reverse(sort.StringSlice(names)))
	if len(names) > historyDepth {
		names = names[:historyDepth]
	}

	history := make([]wire.Result, 0, len(names))
	for _, name := range names {
		stem := strings.TrimSuffix(name, ".json")
		file, err := os.Open(filepath.Join(resultsDir, name))
		if err != nil {
			history = append(history, wire.Result{ID: stem})
			continue
		}
		result, err := wire.DecodeResult(file)
		_ = file.Close()
		if err != nil {
			history = append(history, wire.Result{ID: stem})
			continue
		}
		history = append(history, result)
	}
	return history
}

// clipOutput keeps the tail of a command's output and strips the control characters that would let a
// program rewrite the page it is displayed on.
func clipOutput(output string) string {
	if len(output) > maxOutputSize {
		output = output[len(output)-maxOutputSize:]
	}
	var builder strings.Builder
	builder.Grow(len(output))
	for _, character := range output {
		switch {
		case character == '\n' || character == '\t':
			builder.WriteRune(character)
		case character < 0x20 || character == 0x7f:
			// Drop it: carriage returns and escapes are how a log line becomes a fake page.
		default:
			builder.WriteRune(character)
		}
	}
	return builder.String()
}

func (exec *Executor) hostFile() string {
	if exec.HostFile == "" {
		return DefaultHostFile
	}
	return exec.HostFile
}

func (exec *Executor) targetsFile() string {
	if exec.TargetsFile == "" {
		return DefaultTargetsFile
	}
	return exec.TargetsFile
}

func (exec *Executor) lockFile() string {
	if exec.LockFile == "" {
		return DefaultLockFile
	}
	return exec.LockFile
}

func (exec *Executor) now() time.Time {
	if exec.Now == nil {
		return time.Now().UTC()
	}
	return exec.Now()
}

// wait is the pause between post-check attempts.
func (exec *Executor) wait(ctx context.Context, delay time.Duration) error {
	if exec.Wait != nil {
		return exec.Wait(ctx, delay)
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (exec *Executor) randomness() io.Reader {
	if exec.Random == nil {
		return rand.Reader
	}
	return exec.Random
}

func (exec *Executor) logger() *log.Logger {
	if exec.Log == nil {
		return log.New(io.Discard, "", 0)
	}
	return exec.Log
}

func (exec *Executor) logf(format string, arguments ...any) {
	exec.logger().Printf(format, arguments...)
}
