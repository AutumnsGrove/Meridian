package control_test

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/autumnsgrove/meridian/internal/config"
	"github.com/autumnsgrove/meridian/internal/control"
	"github.com/autumnsgrove/meridian/internal/wire"
)

// The fixture is real directories and a real config: the container side has no other dependencies,
// so there is nothing to fake (STANDARDS section 2).

var testNow = time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)

type fixture struct {
	cfg    *config.Config
	inbox  string
	outbox string
	plane  control.Plane
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	root := t.TempDir()
	inbox := filepath.Join(root, "inbox")
	outbox := filepath.Join(root, "outbox")
	for _, dir := range []string{
		filepath.Join(inbox, "tmp"),
		filepath.Join(inbox, "queue"),
		filepath.Join(outbox, "results"),
	} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("create %s: %v", dir, err)
		}
	}

	cfg := &config.Config{
		Control: config.ControlSettings{
			Reboot:         false,
			QueueDepth:     4,
			ActionTimeout:  config.Duration(90 * time.Second),
			AuditLines:     3,
			AuditRetention: config.Duration(365 * 24 * time.Hour),
			InboxDir:       inbox,
			OutboxDir:      outbox,
		},
		HostStatus: &config.HostStatus{File: filepath.Join(root, "host.json"), MaxAge: config.Duration(2 * time.Minute)},
		Services: []config.Service{
			{ID: "bwmon", Control: &config.ServiceControl{
				Target: "bwmon", Actions: []string{"start", "stop", "restart", "update"},
			}},
			{ID: "resolver", Control: &config.ServiceControl{
				Target: "resolver", Actions: []string{"start", "stop", "restart"}, DNS: true,
			}},
			{ID: "readonly", Control: nil},
		},
	}

	return &fixture{
		cfg:    cfg,
		inbox:  inbox,
		outbox: outbox,
		plane: control.Plane{
			Config: func() *config.Config { return cfg },
			Now:    func() time.Time { return testNow },
			Random: rand.Reader,
		},
	}
}

func (f *fixture) queueDir() string   { return filepath.Join(f.inbox, "queue") }
func (f *fixture) tmpDir() string     { return filepath.Join(f.inbox, "tmp") }
func (f *fixture) resultsDir() string { return filepath.Join(f.outbox, "results") }
func (f *fixture) auditPath() string  { return filepath.Join(f.outbox, "audit.jsonl") }
func (f *fixture) enqueue(t *testing.T, in control.Input) (string, error) {
	t.Helper()
	return f.plane.Enqueue(context.Background(), in)
}

// pendingRequest is a request already sitting in the queue, as if another tap wrote it.
func (f *fixture) pendingRequest(t *testing.T, action wire.Action, target string) string {
	t.Helper()
	return f.pendingRequestAt(t, action, target, testNow.Add(-time.Second))
}

// pendingRequestAt is pendingRequest with a chosen age, for the unclaimed-request rules.
func (f *fixture) pendingRequestAt(t *testing.T, action wire.Action, target string, requestedAt time.Time) string {
	t.Helper()
	id, err := wire.NewID(requestedAt, rand.Reader)
	if err != nil {
		t.Fatalf("NewID: %v", err)
	}
	f.writeRequest(t, wire.Request{ID: id, Action: action, Target: target, RequestedAt: requestedAt})
	return id
}

func (f *fixture) writeRequest(t *testing.T, request wire.Request) {
	t.Helper()
	data, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	if err := os.WriteFile(filepath.Join(f.queueDir(), request.ID+".json"), data, 0o600); err != nil {
		t.Fatalf("write request: %v", err)
	}
}

func names(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir(%s): %v", dir, err)
	}
	found := make([]string, 0, len(entries))
	for _, entry := range entries {
		found = append(found, entry.Name())
	}
	return found
}

// wantRefusal asserts the error is a refusal carrying code.
func wantRefusal(t *testing.T, err error, code wire.Code) {
	t.Helper()
	if err == nil {
		t.Fatalf("error = nil, want refusal %q", code)
	}
	var refusal *wire.Refusal
	if !errors.As(err, &refusal) {
		t.Fatalf("error = %v, want a *wire.Refusal", err)
	}
	if refusal.Code != code {
		t.Errorf("code = %q, want %q", refusal.Code, code)
	}
}

func TestEnqueue_WritesExactlyOneRequestAndLeavesNoTemporary(t *testing.T) {
	f := newFixture(t)

	id, err := f.enqueue(t, control.Input{Action: wire.ActionRestart, Target: "bwmon"})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if _, err := wire.ParseID(id); err != nil {
		t.Fatalf("Enqueue returned id %q, which is not canonical: %v", id, err)
	}

	queue := names(t, f.queueDir())
	if len(queue) != 1 || queue[0] != id+".json" {
		t.Fatalf("queue contents = %v, want only %s.json", queue, id)
	}
	if temporary := names(t, f.tmpDir()); len(temporary) != 0 {
		t.Errorf("temp contents = %v, want empty", temporary)
	}

	file, err := os.Open(filepath.Join(f.queueDir(), id+".json"))
	if err != nil {
		t.Fatalf("open the queued request: %v", err)
	}
	defer func() { _ = file.Close() }()
	request, err := wire.DecodeRequest(file)
	if err != nil {
		t.Fatalf("the queued file is not a request: %v", err)
	}
	if request.ID != id {
		t.Errorf("request id = %q, want the file name %q", request.ID, id)
	}
	if request.Action != wire.ActionRestart || request.Target != "bwmon" {
		t.Errorf("request = %+v, want a restart of bwmon", request)
	}
	if !request.RequestedAt.Equal(testNow) {
		t.Errorf("RequestedAt = %v, want the injected clock %v", request.RequestedAt, testNow)
	}
	if err := wire.ValidateRequest(request, testNow); err != nil {
		t.Errorf("the queued request does not validate: %v", err)
	}

	info, err := os.Stat(filepath.Join(f.queueDir(), id+".json"))
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if got := info.Mode().Perm(); got&0o077 != 0 {
		t.Errorf("mode = %v, want nothing for group or other", got)
	}
}

func TestEnqueue_StopNeedsTheAcknowledgementForThatTarget(t *testing.T) {
	tests := []struct {
		name    string
		action  wire.Action
		target  string
		ack     string
		wantErr wire.Code
	}{
		{"stop without an ack", wire.ActionStop, "bwmon", "", wire.CodeMissingAck},
		{"stop with the wrong ack", wire.ActionStop, "bwmon", "stop-confirmed!", wire.CodeBadAck},
		{"stop with the ack", wire.ActionStop, "bwmon", "stop-confirmed", ""},
		{"dns stop with the plain ack", wire.ActionStop, "resolver", "stop-confirmed", wire.CodeBadAck},
		{"dns stop without an ack", wire.ActionStop, "resolver", "", wire.CodeMissingAck},
		{"dns stop with the dns ack", wire.ActionStop, "resolver", "dns-stop-confirmed", ""},
		{"restart takes no ack", wire.ActionRestart, "bwmon", "", ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f := newFixture(t)
			_, err := f.enqueue(t, control.Input{
				Action: test.action, Target: test.target, Ack: test.ack,
			})
			if test.wantErr == "" {
				if err != nil {
					t.Fatalf("Enqueue = %v, want nil", err)
				}
				return
			}
			wantRefusal(t, err, test.wantErr)
		})
	}
}

func TestEnqueue_RefusesWhatTheConfigDoesNotOffer(t *testing.T) {
	tests := []struct {
		name    string
		input   control.Input
		reboot  bool
		wantErr wire.Code
	}{
		{
			name:    "unknown target",
			input:   control.Input{Action: wire.ActionRestart, Target: "mailserver"},
			wantErr: wire.CodeUnknownTarget,
		},
		{
			name:    "a service without a control block",
			input:   control.Input{Action: wire.ActionRestart, Target: "readonly"},
			wantErr: wire.CodeUnknownTarget,
		},
		{
			name:    "an action the target does not list",
			input:   control.Input{Action: wire.ActionUpdate, Target: "resolver"},
			wantErr: wire.CodeActionNotAllowed,
		},
		{
			name:    "an action nobody knows",
			input:   control.Input{Action: wire.Action("destroy"), Target: "bwmon"},
			wantErr: wire.CodeBadAction,
		},
		{
			name:    "a target that is not a slug",
			input:   control.Input{Action: wire.ActionRestart, Target: "Bwmon"},
			wantErr: wire.CodeBadTarget,
		},
		{
			name:    "a ref on an action that takes none",
			input:   control.Input{Action: wire.ActionRestart, Target: "bwmon", Ref: "0192f0c4-6c1a-7f3b-9a10-2b6f7c1d8e44"},
			wantErr: wire.CodeBadRef,
		},
		{
			name:    "cancel without a ref",
			input:   control.Input{Action: wire.ActionCancelAutorestore, Target: "resolver"},
			wantErr: wire.CodeBadRef,
		},
		{
			name:    "reboot while the box says no",
			input:   control.Input{Action: wire.ActionReboot, Target: "host", Ack: "reboot-confirmed"},
			wantErr: wire.CodeActionNotAllowed,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f := newFixture(t)
			f.cfg.Control.Reboot = test.reboot
			_, err := f.enqueue(t, test.input)
			wantRefusal(t, err, test.wantErr)
			if queued := names(t, f.queueDir()); len(queued) != 0 {
				t.Errorf("a refused request was written: %v", queued)
			}
		})
	}
}

func TestEnqueue_AcceptsRebootWhenTheBoxAllowsIt(t *testing.T) {
	f := newFixture(t)
	f.cfg.Control.Reboot = true

	id, err := f.enqueue(t, control.Input{Action: wire.ActionReboot, Target: "host", Ack: "reboot-confirmed"})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if queued := names(t, f.queueDir()); len(queued) != 1 || queued[0] != id+".json" {
		t.Errorf("queue contents = %v, want only %s.json", queued, id)
	}
}

func TestEnqueue_AcceptsACancelForAKnownTarget(t *testing.T) {
	f := newFixture(t)

	_, err := f.enqueue(t, control.Input{
		Action: wire.ActionCancelAutorestore,
		Target: "resolver",
		Ref:    "0192f0c4-6c1a-7f3b-9a10-2b6f7c1d8e44",
	})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
}

func TestEnqueue_RefusesAnIdenticalRequestAlreadyQueued(t *testing.T) {
	f := newFixture(t)
	first := f.pendingRequest(t, wire.ActionRestart, "bwmon")

	_, err := f.enqueue(t, control.Input{Action: wire.ActionRestart, Target: "bwmon"})
	wantRefusal(t, err, wire.CodeCoalesced)

	// A different action on the same target is a different request.
	if _, err := f.enqueue(t, control.Input{Action: wire.ActionStop, Target: "bwmon", Ack: "stop-confirmed"}); err != nil {
		t.Fatalf("Enqueue stop while a restart is queued: %v", err)
	}
	if queued := names(t, f.queueDir()); len(queued) != 2 {
		t.Errorf("queue contents = %v, want the restart %s and the stop", queued, first)
	}
}

func TestEnqueue_RefusesBeyondTheQueueDepth(t *testing.T) {
	f := newFixture(t)
	f.cfg.Control.QueueDepth = 2
	f.pendingRequest(t, wire.ActionRestart, "bwmon")
	f.pendingRequest(t, wire.ActionRestart, "resolver")

	_, err := f.enqueue(t, control.Input{Action: wire.ActionStart, Target: "bwmon"})
	wantRefusal(t, err, wire.CodeQueueFull)
}

func TestEnqueue_LeavesNoTemporaryWhenTheQueueRefusesTheRename(t *testing.T) {
	f := newFixture(t)
	if err := os.Chmod(f.queueDir(), 0o500); err != nil {
		t.Fatalf("chmod the queue: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(f.queueDir(), 0o700) })

	if _, err := f.enqueue(t, control.Input{Action: wire.ActionRestart, Target: "bwmon"}); err == nil {
		t.Fatal("Enqueue into a read-only queue = nil, want an error")
	}
	if temporary := names(t, f.tmpDir()); len(temporary) != 0 {
		t.Errorf("temp contents = %v, want empty after a failed rename", temporary)
	}
	if err := os.Chmod(f.queueDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	if queued := names(t, f.queueDir()); len(queued) != 0 {
		t.Errorf("queue contents = %v, want empty", queued)
	}
}

func TestEnqueue_RefusesWhenThereIsNoPlane(t *testing.T) {
	t.Run("no config", func(t *testing.T) {
		plane := control.Plane{Now: func() time.Time { return testNow }}
		_, err := plane.Enqueue(context.Background(), control.Input{Action: wire.ActionRestart, Target: "bwmon"})
		if !errors.Is(err, control.ErrUnavailable) {
			t.Fatalf("error = %v, want ErrUnavailable", err)
		}
	})

	t.Run("no inbox", func(t *testing.T) {
		f := newFixture(t)
		if err := os.RemoveAll(f.inbox); err != nil {
			t.Fatal(err)
		}
		_, err := f.enqueue(t, control.Input{Action: wire.ActionRestart, Target: "bwmon"})
		if !errors.Is(err, control.ErrUnavailable) {
			t.Fatalf("error = %v, want ErrUnavailable", err)
		}
	})
}

func TestAvailable(t *testing.T) {
	tests := []struct {
		name    string
		prepare func(t *testing.T, f *fixture)
		want    bool
		reason  string
	}{
		{"mounted and writable", func(*testing.T, *fixture) {}, true, ""},
		{
			name:    "no inbox",
			prepare: func(t *testing.T, f *fixture) { mustRemoveAll(t, f.inbox) },
			want:    false, reason: "control plane not installed",
		},
		{
			name:    "no outbox",
			prepare: func(t *testing.T, f *fixture) { mustRemoveAll(t, f.outbox) },
			want:    false, reason: "control plane not installed",
		},
		{
			name:    "no temp directory",
			prepare: func(t *testing.T, f *fixture) { mustRemoveAll(t, f.tmpDir()) },
			want:    false, reason: "control plane not installed",
		},
		{
			name:    "no queue directory",
			prepare: func(t *testing.T, f *fixture) { mustRemoveAll(t, f.queueDir()) },
			want:    false, reason: "control plane not installed",
		},
		{
			name: "inbox is read-only",
			prepare: func(t *testing.T, f *fixture) {
				if err := os.Chmod(f.inbox, 0o500); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = os.Chmod(f.inbox, 0o700) })
			},
			want: false, reason: "the control plane is not writable from the container",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f := newFixture(t)
			test.prepare(t, f)
			available, reason := f.plane.Available()
			if available != test.want {
				t.Errorf("Available() = %v, want %v", available, test.want)
			}
			if reason != test.reason {
				t.Errorf("reason = %q, want %q", reason, test.reason)
			}
		})
	}

	t.Run("no config", func(t *testing.T) {
		plane := control.Plane{}
		if available, reason := plane.Available(); available || reason == "" {
			t.Errorf("Available() = %v, %q, want false with a reason", available, reason)
		}
	})
}

func mustRemoveAll(t *testing.T, path string) {
	t.Helper()
	if err := os.RemoveAll(path); err != nil {
		t.Fatalf("remove %s: %v", path, err)
	}
}

// FuzzEnqueueInput holds the two properties the container's side of the boundary needs: whatever
// the browser sends, an accepted request is one the executor will validate, and nothing is ever
// written outside the inbox.
func FuzzEnqueueInput(f *testing.F) {
	f.Add("restart", "bwmon", "", "")
	f.Add("stop", "resolver", "dns-stop-confirmed", "")
	f.Add("stop", "resolver", "stop-confirmed", "")
	f.Add("cancel-autorestore", "resolver", "", "0192f0c4-6c1a-7f3b-9a10-2b6f7c1d8e44")
	f.Add("reboot", "host", "reboot-confirmed", "")
	f.Add("", "", "", "")
	f.Add("restart\x00", "../../etc/passwd", "a\nb", "../")
	f.Add(strings.Repeat("a", 200), strings.Repeat("b", 200), strings.Repeat("c", 200), strings.Repeat("d", 200))

	f.Fuzz(func(t *testing.T, action, target, ack, ref string) {
		root := t.TempDir()
		inbox := filepath.Join(root, "inbox")
		outbox := filepath.Join(root, "outbox")
		for _, dir := range []string{filepath.Join(inbox, "tmp"), filepath.Join(inbox, "queue"), outbox} {
			if err := os.MkdirAll(dir, 0o700); err != nil {
				t.Fatalf("create %s: %v", dir, err)
			}
		}
		cfg := &config.Config{
			Control: config.ControlSettings{
				Reboot: true, QueueDepth: 4,
				ActionTimeout:  config.Duration(time.Minute),
				AuditLines:     10,
				AuditRetention: config.Duration(24 * time.Hour),
				InboxDir:       inbox,
				OutboxDir:      outbox,
			},
			Services: []config.Service{
				{ID: "bwmon", Control: &config.ServiceControl{Target: "bwmon", Actions: []string{"start", "stop", "restart", "update"}}},
				{ID: "resolver", Control: &config.ServiceControl{Target: "resolver", Actions: []string{"start", "stop", "restart"}, DNS: true}},
			},
		}
		plane := control.Plane{
			Config: func() *config.Config { return cfg },
			Now:    func() time.Time { return testNow },
			Random: strings.NewReader(strings.Repeat("0123456789abcdef", 8)),
		}

		id, err := plane.Enqueue(context.Background(), control.Input{
			Action: wire.Action(action), Target: target, Ack: ack, Ref: ref,
		})
		if err != nil {
			var refusal *wire.Refusal
			if errors.As(err, &refusal) {
				if !wire.IsCode(refusal.Code) {
					t.Fatalf("refusal code %q is not in the closed table", refusal.Code)
				}
				return
			}
			if errors.Is(err, control.ErrUnavailable) {
				return
			}
			t.Fatalf("unexpected error: %v", err)
		}

		// An accepted input produced exactly one file, named after the returned id, and its
		// contents are a request the executor's own validator accepts.
		queued := names(t, filepath.Join(inbox, "queue"))
		if len(queued) != 1 || queued[0] != id+".json" {
			t.Fatalf("queue contents = %v, want only %s.json", queued, id)
		}
		data, err := os.ReadFile(filepath.Join(inbox, "queue", id+".json")) //nolint:gosec // a fixture path under t.TempDir
		if err != nil {
			t.Fatalf("read the queued request: %v", err)
		}
		request, err := wire.DecodeRequest(strings.NewReader(string(data)))
		if err != nil {
			t.Fatalf("the queued file does not decode: %v", err)
		}
		if request.ID != id {
			t.Fatalf("request id %q != file name %q", request.ID, id)
		}
		if err := wire.ValidateRequest(request, testNow); err != nil {
			t.Fatalf("the queued request does not validate: %v", err)
		}

		// Nothing escaped the inbox: every file under the fixture root lives under inbox/.
		err = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				return nil
			}
			relative, err := filepath.Rel(inbox, path)
			if err != nil || strings.HasPrefix(relative, "..") {
				t.Fatalf("Enqueue wrote %s outside the inbox", path)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk the fixture: %v", err)
		}
	})
}
