package hostexec_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/autumnsgrove/meridian/internal/hostexec"
	"github.com/autumnsgrove/meridian/internal/wire"
)

// currentUID is the uid the tests own their own files with; a unix uid always fits in 32 bits.
func currentUID() uint32 {
	return uint32(os.Getuid()) //nolint:gosec // a unix uid fits in uint32
}

// The harness is a whole control plane on a temporary filesystem: a root-owned-looking allowlist, a
// queue, an outbox and an executor whose command seams are recorded instead of run.
type harness struct {
	t           *testing.T
	root        string
	controlDir  string
	queueDir    string
	resultsDir  string
	outboxDir   string
	auditPath   string
	hostFile    string
	targetsFile string
	lockFile    string
	exec        *hostexec.Executor
	now         time.Time
	logs        *bytes.Buffer

	built    []buildCall
	ran      []hostexec.Command
	exitCode int
	output   string
	runErr   error
	buildErr error
	panicky  bool
}

type buildCall struct {
	target hostexec.Target
	action wire.Action
	id     string
	ref    string
}

const testTargets = `{"targets":{
  "app":{"kind":"systemd","unit":"app.service","actions":["start","stop","restart","update"],"criticality":"medium",
         "update":{"kind":"unit","unit":"app-update.service"}},
  "resolver":{"kind":"systemd","unit":"resolver.service","actions":["start","stop","restart"],"dns":true,"criticality":"high"},
  "host":{"kind":"host","actions":["reboot"],"criticality":"high"}
}}`

func newHarness(t *testing.T) *harness {
	t.Helper()
	root := t.TempDir()
	controlDir := filepath.Join(root, "control")
	h := &harness{
		t:           t,
		root:        root,
		controlDir:  controlDir,
		queueDir:    filepath.Join(controlDir, "inbox", "queue"),
		resultsDir:  filepath.Join(controlDir, "outbox", "results"),
		outboxDir:   filepath.Join(controlDir, "outbox"),
		auditPath:   filepath.Join(controlDir, "outbox", "audit.jsonl"),
		hostFile:    filepath.Join(root, "etc", "host.json"),
		targetsFile: filepath.Join(root, "etc", "targets.json"),
		lockFile:    filepath.Join(root, "meridian-control.lock"),
		now:         decideNow,
		logs:        &bytes.Buffer{},
	}
	for _, dir := range []string{h.queueDir, filepath.Join(controlDir, "inbox", "tmp"), h.resultsDir} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("create %s: %v", dir, err)
		}
	}
	h.writeTrusted(t, h.hostFile, `{"control_dir":"`+controlDir+`","deploy_user":"deploy"}`)
	h.writeTrusted(t, h.targetsFile, testTargets)

	h.exec = &hostexec.Executor{
		HostFile:    h.hostFile,
		TargetsFile: h.targetsFile,
		LockFile:    h.lockFile,
		Owner:       currentUID(),
		Build: func(in hostexec.BuildInput) (hostexec.Command, error) {
			if h.panicky {
				panic("the builder exploded")
			}
			h.built = append(h.built, buildCall{target: in.Target, action: in.Action, id: in.ID, ref: in.Ref})
			if h.buildErr != nil {
				return hostexec.Command{}, h.buildErr
			}
			return hostexec.Command{Argv: []string{"/bin/true"}, Timeout: time.Minute}, nil
		},
		Run: func(_ context.Context, command hostexec.Command) (int, string, error) {
			h.ran = append(h.ran, command)
			return h.exitCode, h.output, h.runErr
		},
		Now:    func() time.Time { return h.now },
		Random: rand.Reader,
		Log:    log.New(h.logs, "", 0),
	}
	return h
}

func (h *harness) writeTrusted(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("create %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil { //nolint:gosec // 0644 is the installer's mode, which is what the trust check is about
		t.Fatalf("write %s: %v", path, err)
	}
	// WriteFile applies the umask, so set the mode again to make the test mean what it says.
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatalf("chmod %s: %v", path, err)
	}
}

func (h *harness) execute() error {
	return h.exec.Execute(h.t.Context())
}

func (h *harness) queueRequest(t *testing.T, request wire.Request) {
	t.Helper()
	data, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	h.queueRaw(t, request.ID+".json", data)
}

func (h *harness) queueRaw(t *testing.T, name string, data []byte) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(h.queueDir, name), data, 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func (h *harness) queued(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(h.queueDir)
	if err != nil {
		t.Fatalf("read the queue: %v", err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}

func (h *harness) result(t *testing.T, id string) wire.Result {
	t.Helper()
	file, err := os.Open(filepath.Join(h.resultsDir, id+".json"))
	if err != nil {
		t.Fatalf("open the result for %s: %v", id, err)
	}
	defer func() { _ = file.Close() }()
	result, err := wire.DecodeResult(file)
	if err != nil {
		t.Fatalf("the result for %s does not decode: %v", id, err)
	}
	if result.ID != id {
		t.Fatalf("result id = %q, want %q", result.ID, id)
	}
	return result
}

func (h *harness) resultFiles(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(h.resultsDir)
	if err != nil {
		t.Fatalf("read results: %v", err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}

func (h *harness) audit(t *testing.T) []wire.AuditLine {
	t.Helper()
	data, err := os.ReadFile(h.auditPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("read the audit log: %v", err)
	}
	var lines []wire.AuditLine
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		audit, err := wire.DecodeAuditLine([]byte(line))
		if err != nil {
			t.Fatalf("audit line %q does not decode: %v", line, err)
		}
		lines = append(lines, audit)
	}
	return lines
}

func (h *harness) writeResult(t *testing.T, result wire.Result) {
	t.Helper()
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	if err := os.WriteFile(filepath.Join(h.resultsDir, result.ID+".json"), data, 0o600); err != nil {
		t.Fatalf("write result: %v", err)
	}
}

// restartOf is a well-formed request for the app target, one second old.
func restartOf(t *testing.T, id string, age time.Duration) wire.Request {
	t.Helper()
	return wire.Request{
		ID: id, Action: wire.ActionRestart, Target: "app",
		RequestedAt: decideNow.Add(-age),
	}
}

func newIDAt(t *testing.T, at time.Time) string {
	t.Helper()
	id, err := wire.NewID(at, rand.Reader)
	if err != nil {
		t.Fatalf("NewID: %v", err)
	}
	return id
}

func TestExecute_RunsAnAllowlistedActionAndWritesTheStory(t *testing.T) {
	h := newHarness(t)
	h.output = "active (running) since now"
	id := newIDAt(t, decideNow)
	h.queueRequest(t, restartOf(t, id, time.Second))

	if err := h.execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if len(h.built) != 1 {
		t.Fatalf("built %d commands, want 1", len(h.built))
	}
	if call := h.built[0]; call.target.Unit != "app.service" || call.action != wire.ActionRestart || call.id != id {
		t.Errorf("built %+v, want the app restart for %s", call, id)
	}
	if len(h.ran) != 1 || h.ran[0].Argv[0] != "/bin/true" {
		t.Fatalf("ran %+v, want the built command", h.ran)
	}

	result := h.result(t, id)
	if !result.OK || result.Code != "" || result.ExitCode != 0 {
		t.Errorf("result = %+v, want a success", result)
	}
	if result.Action != wire.ActionRestart || result.Target != "app" {
		t.Errorf("result names %q on %q, want restart on app", result.Action, result.Target)
	}
	if result.Output != "active (running) since now" {
		t.Errorf("output = %q, want the command's output", result.Output)
	}

	if queued := h.queued(t); len(queued) != 0 {
		t.Errorf("queue = %v, want empty", queued)
	}
	lines := h.audit(t)
	if len(lines) != 1 {
		t.Fatalf("audit has %d lines, want 1", len(lines))
	}
	if lines[0].ID != id || !lines[0].OK || lines[0].By != wire.AuditBy {
		t.Errorf("audit line = %+v, want the successful restart by the tailnet", lines[0])
	}

	info, err := os.Stat(filepath.Join(h.resultsDir, id+".json"))
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o644 {
		t.Errorf("result mode = %v, want 0644 so the container can read it", mode)
	}
}

func TestExecute_RefusesWithACodeAndStillAnswers(t *testing.T) {
	tests := []struct {
		name    string
		prepare func(t *testing.T, h *harness) (id string)
		want    wire.Code
	}{
		{
			name: "unknown target",
			prepare: func(t *testing.T, h *harness) string {
				request := restartOf(t, newIDAt(t, decideNow), time.Second)
				request.Target = "mail"
				h.queueRequest(t, request)
				return request.ID
			},
			want: wire.CodeUnknownTarget,
		},
		{
			name: "action not allowed",
			prepare: func(t *testing.T, h *harness) string {
				request := restartOf(t, newIDAt(t, decideNow), time.Second)
				request.Action = wire.ActionUpdate
				request.Target = "resolver"
				h.queueRequest(t, request)
				return request.ID
			},
			want: wire.CodeActionNotAllowed,
		},
		{
			name: "missing ack",
			prepare: func(t *testing.T, h *harness) string {
				request := restartOf(t, newIDAt(t, decideNow), time.Second)
				request.Action = wire.ActionStop
				h.queueRequest(t, request)
				return request.ID
			},
			want: wire.CodeMissingAck,
		},
		{
			name: "stale",
			prepare: func(t *testing.T, h *harness) string {
				request := restartOf(t, newIDAt(t, decideNow.Add(-5*time.Minute)), 5*time.Minute)
				h.queueRequest(t, request)
				return request.ID
			},
			want: wire.CodeStale,
		},
		{
			name: "replay",
			prepare: func(t *testing.T, h *harness) string {
				id := newIDAt(t, decideNow)
				h.writeResult(t, wire.Result{ID: id, Action: wire.ActionRestart, Target: "app", OK: true,
					StartedAt: decideNow.Add(-time.Minute), FinishedAt: decideNow.Add(-time.Minute)})
				h.queueRequest(t, restartOf(t, id, time.Second))
				return id
			},
			want: wire.CodeReplay,
		},
		{
			name: "id does not match the file name",
			prepare: func(t *testing.T, h *harness) string {
				filenameID := newIDAt(t, decideNow)
				request := restartOf(t, newIDAt(t, decideNow), time.Second)
				h.queueRequest(t, request)
				data, err := json.Marshal(request)
				if err != nil {
					t.Fatal(err)
				}
				h.queueRaw(t, filenameID+".json", data)
				_ = os.Remove(filepath.Join(h.queueDir, request.ID+".json"))
				return filenameID
			},
			want: wire.CodeBadID,
		},
		{
			name: "not JSON",
			prepare: func(t *testing.T, h *harness) string {
				id := newIDAt(t, decideNow)
				h.queueRaw(t, id+".json", []byte("not a request"))
				return id
			},
			want: wire.CodeBadRequest,
		},
		{
			name: "unknown fields",
			prepare: func(t *testing.T, h *harness) string {
				id := newIDAt(t, decideNow)
				h.queueRaw(t, id+".json", []byte(
					`{"id":"`+id+`","action":"restart","target":"app","requested_at":"`+
						decideNow.Add(-time.Second).Format(time.RFC3339)+`","command":"rm -rf /"}`))
				return id
			},
			want: wire.CodeBadRequest,
		},
		{
			name: "trailing garbage",
			prepare: func(t *testing.T, h *harness) string {
				id := newIDAt(t, decideNow)
				data, err := json.Marshal(restartOf(t, id, time.Second))
				if err != nil {
					t.Fatal(err)
				}
				h.queueRaw(t, id+".json", append(data, []byte(" {}")...))
				return id
			},
			want: wire.CodeBadRequest,
		},
		{
			name: "oversized",
			prepare: func(t *testing.T, h *harness) string {
				id := newIDAt(t, decideNow)
				request := restartOf(t, id, time.Second)
				request.Ack = strings.Repeat("a", wire.MaxRequestSize)
				data, err := json.Marshal(request)
				if err != nil {
					t.Fatal(err)
				}
				h.queueRaw(t, id+".json", data)
				return id
			},
			want: wire.CodeBadRequest,
		},
		{
			name: "NUL bytes",
			prepare: func(t *testing.T, h *harness) string {
				id := newIDAt(t, decideNow)
				h.queueRaw(t, id+".json", []byte("{\x00}"))
				return id
			},
			want: wire.CodeBadRequest,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			h := newHarness(t)
			id := test.prepare(t, h)

			if err := h.execute(); err != nil {
				t.Fatalf("Execute: %v", err)
			}
			if len(h.ran) != 0 {
				t.Fatalf("a refused request ran %+v", h.ran)
			}

			result := h.result(t, id)
			if result.OK {
				t.Errorf("result is ok, want the refusal %q", test.want)
			}
			if result.Code != test.want {
				t.Errorf("code = %q, want %q", result.Code, test.want)
			}
			if queued := h.queued(t); len(queued) != 0 {
				t.Errorf("queue = %v, want the request removed", queued)
			}
			lines := h.audit(t)
			if len(lines) != 1 || lines[0].ID != id || lines[0].Code != test.want {
				t.Errorf("audit = %+v, want one line for %s with code %q", lines, id, test.want)
			}
		})
	}
}

func TestExecute_NeverOpensWhatItCannotTrust(t *testing.T) {
	secretContent := "ROOT-ONLY-SECRET-MARKER"

	t.Run("a symlink to a secret file", func(t *testing.T) {
		h := newHarness(t)
		secret := filepath.Join(h.root, "secret.txt")
		if err := os.WriteFile(secret, []byte(secretContent), 0o600); err != nil {
			t.Fatal(err)
		}
		id := newIDAt(t, decideNow)
		if err := os.Symlink(secret, filepath.Join(h.queueDir, id+".json")); err != nil {
			t.Fatal(err)
		}

		if err := h.execute(); err != nil {
			t.Fatalf("Execute: %v", err)
		}
		if len(h.ran) != 0 {
			t.Fatalf("a symlink was executed: %+v", h.ran)
		}
		if result := h.result(t, id); result.Code != wire.CodeBadRequest {
			t.Errorf("code = %q, want bad_request", result.Code)
		}
		assertNoLeak(t, h, secretContent)
		if content, err := os.ReadFile(secret); err != nil || string(content) != secretContent {
			t.Errorf("the secret file changed (content %q, err %v)", content, err)
		}
	})

	t.Run("a FIFO", func(t *testing.T) {
		h := newHarness(t)
		id := newIDAt(t, decideNow)
		if err := syscall.Mkfifo(filepath.Join(h.queueDir, id+".json"), 0o600); err != nil {
			t.Fatalf("mkfifo: %v", err)
		}

		// Without O_NONBLOCK the open would block forever; give the run a deadline so a regression
		// fails this test instead of hanging the suite.
		done := make(chan error, 1)
		go func() { done <- h.execute() }()
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("Execute: %v", err)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("Execute blocked on a FIFO")
		}
		if result := h.result(t, id); result.Code != wire.CodeBadRequest {
			t.Errorf("code = %q, want bad_request", result.Code)
		}
	})

	t.Run("a directory", func(t *testing.T) {
		h := newHarness(t)
		id := newIDAt(t, decideNow)
		if err := os.Mkdir(filepath.Join(h.queueDir, id+".json"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := h.execute(); err != nil {
			t.Fatalf("Execute: %v", err)
		}
		if result := h.result(t, id); result.Code != wire.CodeBadRequest {
			t.Errorf("code = %q, want bad_request", result.Code)
		}
	})
}

// assertNoLeak checks the one rule that makes refusing worth anything: a file's contents never reach
// a result, the audit log or the journal.
func assertNoLeak(t *testing.T, h *harness, marker string) {
	t.Helper()
	for _, name := range h.resultFiles(t) {
		data, err := os.ReadFile(filepath.Join(h.resultsDir, name)) //nolint:gosec // a fixture path under t.TempDir
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), marker) {
			t.Errorf("the result %s contains file contents", name)
		}
	}
	if data, err := os.ReadFile(h.auditPath); err == nil && strings.Contains(string(data), marker) {
		t.Error("the audit log contains file contents")
	}
	if strings.Contains(h.logs.String(), marker) {
		t.Error("the journal contains file contents")
	}
}

func TestExecute_RemovesAnEntryThatIsNotARequest(t *testing.T) {
	h := newHarness(t)
	h.queueRaw(t, "notes.txt", []byte("not a request"))
	h.queueRaw(t, "..hidden", []byte("not a request"))
	id := newIDAt(t, decideNow)
	h.queueRequest(t, restartOf(t, id, time.Second))

	if err := h.execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if queued := h.queued(t); len(queued) != 0 {
		t.Errorf("queue = %v, want every entry gone", queued)
	}
	if !h.result(t, id).OK {
		t.Error("the real request did not run")
	}

	lines := h.audit(t)
	var discarded int
	for _, line := range lines {
		if line.Code == wire.CodeBadRequest && line.ID != id {
			discarded++
			if strings.Contains(line.Output, "notes") {
				t.Errorf("the audit line carries the hostile name: %+v", line)
			}
		}
	}
	if discarded != 2 {
		t.Errorf("audited %d discarded entries, want 2 (%+v)", discarded, lines)
	}
}

func TestExecute_ProcessesAtMostSixtyFourRequestsPerRun(t *testing.T) {
	h := newHarness(t)
	for i := 0; i < 100; i++ {
		id := newIDAt(t, decideNow.Add(time.Duration(i)*time.Second))
		h.queueRequest(t, restartOf(t, id, time.Duration(i+1)*time.Second))
	}

	if err := h.execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if results := h.resultFiles(t); len(results) != 64 {
		t.Errorf("%d results, want 64", len(results))
	}
	if queued := h.queued(t); len(queued) != 36 {
		t.Errorf("%d requests left, want 36", len(queued))
	}
}

func TestExecute_DoesNotRunWhileAnotherInvocationHoldsTheLock(t *testing.T) {
	h := newHarness(t)
	id := newIDAt(t, decideNow)
	h.queueRequest(t, restartOf(t, id, time.Second))

	lock, err := os.OpenFile(h.lockFile, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		t.Fatalf("open the lock: %v", err)
	}
	defer func() { _ = lock.Close() }()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatalf("take the lock: %v", err)
	}

	if err := h.execute(); err != nil {
		t.Fatalf("Execute while locked = %v, want nil (the next trigger takes the queue)", err)
	}
	if len(h.ran) != 0 {
		t.Errorf("work ran while locked: %+v", h.ran)
	}
	if queued := h.queued(t); len(queued) != 1 {
		t.Errorf("queue = %v, want the request untouched", queued)
	}

	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_UN); err != nil {
		t.Fatal(err)
	}
	if err := h.execute(); err != nil {
		t.Fatalf("Execute after unlocking: %v", err)
	}
	if !h.result(t, id).OK {
		t.Error("the request did not run after the lock was released")
	}
}

func TestExecute_RefusesUntrustworthyConfiguration(t *testing.T) {
	tests := []struct {
		name    string
		prepare func(t *testing.T, h *harness)
	}{
		{
			name: "targets.json is group-writable",
			prepare: func(t *testing.T, h *harness) {
				if err := os.Chmod(h.targetsFile, 0o664); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "host.json is world-writable",
			prepare: func(t *testing.T, h *harness) {
				if err := os.Chmod(h.hostFile, 0o666); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "targets.json is a symlink",
			prepare: func(t *testing.T, h *harness) {
				moved := h.targetsFile + ".real"
				if err := os.Rename(h.targetsFile, moved); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(moved, h.targetsFile); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "targets.json is missing",
			prepare: func(t *testing.T, h *harness) {
				if err := os.Remove(h.targetsFile); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "targets.json does not validate",
			prepare: func(t *testing.T, h *harness) {
				h.writeTrusted(t, h.targetsFile, `{"targets":{"app":{"kind":"daemon","actions":["start"]}}}`)
			},
		},
		{
			name: "a file owned by somebody else",
			prepare: func(_ *testing.T, h *harness) {
				h.exec.Owner = currentUID() + 1
			},
		},
		{
			name: "a trusted file that is far too large",
			prepare: func(t *testing.T, h *harness) {
				big := `{"targets":{"app":{"kind":"systemd","unit":"app.service","actions":["restart"],"criticality":"low","padding":"` +
					strings.Repeat("a", hostexec.MaxTrustedSize) + `"}}}`
				h.writeTrusted(t, h.targetsFile, big)
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			h := newHarness(t)
			id := newIDAt(t, decideNow)
			h.queueRequest(t, restartOf(t, id, time.Second))
			test.prepare(t, h)

			err := h.execute()
			if !errors.Is(err, hostexec.ErrUnsafeConfig) {
				t.Fatalf("Execute = %v, want ErrUnsafeConfig", err)
			}
			if len(h.ran) != 0 {
				t.Errorf("something ran with untrusted config: %+v", h.ran)
			}
			if queued := h.queued(t); len(queued) != 1 {
				t.Errorf("queue = %v, want the request untouched", queued)
			}
			if results := h.resultFiles(t); len(results) != 0 {
				t.Errorf("results = %v, want none", results)
			}
		})
	}
}

func TestExecute_PrunesOldResultsAndAuditLines(t *testing.T) {
	h := newHarness(t)
	old := newIDAt(t, decideNow.Add(-8*24*time.Hour))
	recent := newIDAt(t, decideNow.Add(-time.Hour))
	for _, id := range []string{old, recent} {
		h.writeResult(t, wire.Result{ID: id, Action: wire.ActionRestart, Target: "app", OK: true,
			StartedAt: decideNow.Add(-time.Hour), FinishedAt: decideNow.Add(-time.Hour)})
	}
	if err := os.Chtimes(filepath.Join(h.resultsDir, old+".json"), decideNow.Add(-8*24*time.Hour), decideNow.Add(-8*24*time.Hour)); err != nil {
		t.Fatal(err)
	}

	oldLine := wire.AuditLine{At: decideNow.Add(-400 * 24 * time.Hour), ID: old,
		Action: wire.ActionRestart, Target: "app", OK: true, By: wire.AuditBy}
	recentLine := wire.AuditLine{At: decideNow.Add(-time.Hour), ID: recent,
		Action: wire.ActionRestart, Target: "app", OK: true, By: wire.AuditBy}
	corrupt := `{"at":"half a line"`
	appendRaw(t, h.auditPath, mustJSON(t, oldLine), mustJSON(t, recentLine), corrupt)

	if err := h.execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if _, err := os.Stat(filepath.Join(h.resultsDir, old+".json")); !os.IsNotExist(err) {
		t.Error("the 8 day old result was not pruned")
	}
	if _, err := os.Stat(filepath.Join(h.resultsDir, recent+".json")); err != nil {
		t.Errorf("the recent result was pruned: %v", err)
	}

	data, err := os.ReadFile(h.auditPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), old) {
		t.Error("the 400 day old audit line was not pruned")
	}
	if !strings.Contains(string(data), recent) || !strings.Contains(string(data), corrupt) {
		t.Errorf("the recent or corrupt audit line disappeared:\n%s", data)
	}
}

func TestExecute_CoalescesADuplicate(t *testing.T) {
	h := newHarness(t)
	prior := wire.Result{
		ID: newIDAt(t, decideNow.Add(-10*time.Second)), Action: wire.ActionRestart, Target: "app",
		OK: true, StartedAt: decideNow.Add(-10 * time.Second), FinishedAt: decideNow.Add(-8 * time.Second),
	}
	h.writeResult(t, prior)

	id := newIDAt(t, decideNow)
	h.queueRequest(t, restartOf(t, id, time.Second))

	if err := h.execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(h.ran) != 0 {
		t.Fatalf("a duplicate ran: %+v", h.ran)
	}
	result := h.result(t, id)
	if !result.OK || result.Code != wire.CodeCoalesced {
		t.Errorf("result = %+v, want ok and coalesced", result)
	}
	if result.Note != "coalesced with "+prior.ID {
		t.Errorf("Note = %q, want the prior request named", result.Note)
	}
}

func TestExecute_ReportsHowACommandFailed(t *testing.T) {
	tests := []struct {
		name     string
		exitCode int
		runErr   error
		output   string
		want     wire.Code
		ok       bool
	}{
		{"a non-zero exit", 1, nil, "job failed", wire.CodeExecFailed, false},
		{"a timeout", -1, context.DeadlineExceeded, "", wire.CodeTimeout, false},
		{"a command that never started", -1, errors.New("no such file"), "", wire.CodeExecFailed, false},
		{"a clean exit", 0, nil, "done", "", true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			h := newHarness(t)
			h.exitCode, h.runErr, h.output = test.exitCode, test.runErr, test.output
			id := newIDAt(t, decideNow)
			h.queueRequest(t, restartOf(t, id, time.Second))

			if err := h.execute(); err != nil {
				t.Fatalf("Execute: %v", err)
			}
			result := h.result(t, id)
			if result.OK != test.ok || result.Code != test.want {
				t.Errorf("result = %+v, want ok=%v code=%q", result, test.ok, test.want)
			}
			if result.Output != test.output {
				t.Errorf("output = %q, want %q", result.Output, test.output)
			}
			if queued := h.queued(t); len(queued) != 0 {
				t.Errorf("queue = %v, want the request removed", queued)
			}
		})
	}
}

func TestExecute_AnswersWhenTheBuilderPanics(t *testing.T) {
	h := newHarness(t)
	h.panicky = true
	id := newIDAt(t, decideNow)
	h.queueRequest(t, restartOf(t, id, time.Second))

	if err := h.execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result := h.result(t, id); result.Code != wire.CodeInternal || result.OK {
		t.Errorf("result = %+v, want an internal failure", result)
	}
	if lines := h.audit(t); len(lines) != 1 || lines[0].Code != wire.CodeInternal {
		t.Errorf("audit = %+v, want one internal line", lines)
	}
	if !strings.Contains(h.logs.String(), "panic") {
		t.Errorf("the journal does not mention the panic: %s", h.logs.String())
	}
	if queued := h.queued(t); len(queued) != 0 {
		t.Errorf("queue = %v, want the request removed", queued)
	}
}

func TestExecute_ClipsAndSanitizesOutput(t *testing.T) {
	h := newHarness(t)
	h.output = "\x1b[31mred\x00" + strings.Repeat("x", 3*maxOutputForTest) + "\n"
	id := newIDAt(t, decideNow)
	h.queueRequest(t, restartOf(t, id, time.Second))

	if err := h.execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	output := h.result(t, id).Output
	if len(output) > maxOutputForTest {
		t.Errorf("output is %d bytes, want at most %d", len(output), maxOutputForTest)
	}
	if strings.ContainsAny(output, "\x1b\x00") {
		t.Errorf("output keeps control characters: %q", output)
	}
	if !strings.HasSuffix(output, "\n") {
		t.Errorf("output lost its tail: %q", output[len(output)-10:])
	}
}

// maxOutputForTest mirrors the executor's output cap; it is a constant in the package, and a result
// the container cannot decode would be worse than no output at all.
const maxOutputForTest = 4 << 10

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(data)
}

func appendRaw(t *testing.T, path string, lines ...string) {
	t.Helper()
	content := ""
	for _, line := range lines {
		content += line + "\n"
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// FuzzRequestFile pushes arbitrary bytes through the whole pipeline with a fake commander: a result
// is written for every request file, and a command runs only for what decodes, validates and is
// allowlisted.
func FuzzRequestFile(f *testing.F) {
	f.Add([]byte(`{"id":"0192f0c4-6c1a-7f3b-9a10-2b6f7c1d8e44","action":"restart","target":"app","ack":"","requested_at":"2026-10-26T11:59:59Z"}`))
	f.Add([]byte(`{"id":"0192f0c4-6c1a-7f3b-9a10-2b6f7c1d8e44","action":"stop","target":"app","ack":"stop-confirmed","requested_at":"2026-10-26T11:59:59Z"}`))
	f.Add([]byte(`{"id":"../x","action":"restart","target":"app"}`))
	f.Add([]byte(`{"id":"0192f0c4-6c1a-7f3b-9a10-2b6f7c1d8e44","action":"restart","target":"app","requested_at":"2026-10-26T11:59:59Z","extra":1}`))
	f.Add([]byte("\x00\x01\x02"))
	f.Add([]byte(`{"id":"0192f0c4-6c1a-7f3b-9a10-2b6f7c1d8e44","action":"reboot","target":"host","ack":"reboot-confirmed","requested_at":"2026-10-26T11:59:59Z"}`))

	f.Fuzz(func(t *testing.T, data []byte) {
		h := newHarness(t)
		id := newIDAt(t, decideNow)
		h.queueRaw(t, id+".json", data)

		if err := h.execute(); err != nil {
			t.Fatalf("Execute: %v", err)
		}

		// A result always exists: every path answers, so the page never waits on a dead spinner.
		result := h.result(t, id)
		if queued := h.queued(t); len(queued) != 0 {
			t.Fatalf("the request file is still queued: %v", queued)
		}
		if len(h.ran) > 0 {
			// A command ran, so the request passed every gate and the answer must be a success.
			if !result.OK || result.Code != "" {
				t.Fatalf("a command ran for a request answered with %+v", result)
			}
		}
		if result.Code != "" && !wire.IsCode(result.Code) {
			t.Fatalf("code %q is not in the closed table", result.Code)
		}
	})
}
