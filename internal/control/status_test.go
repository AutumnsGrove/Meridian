package control_test

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/autumnsgrove/meridian/internal/config"
	"github.com/autumnsgrove/meridian/internal/control"
	"github.com/autumnsgrove/meridian/internal/wire"
)

func (f *fixture) writeResult(t *testing.T, result wire.Result) {
	t.Helper()
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	if err := os.WriteFile(filepath.Join(f.resultsDir(), result.ID+".json"), data, 0o600); err != nil {
		t.Fatalf("write result: %v", err)
	}
}

func (f *fixture) writeAuditLine(t *testing.T, raw string) {
	t.Helper()
	file, err := os.OpenFile(f.auditPath(), os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatalf("open the audit log: %v", err)
	}
	defer func() { _ = file.Close() }()
	if _, err := file.WriteString(raw + "\n"); err != nil {
		t.Fatalf("append an audit line: %v", err)
	}
}

func auditLineJSON(t *testing.T, line wire.AuditLine) string {
	t.Helper()
	data, err := json.Marshal(line)
	if err != nil {
		t.Fatalf("marshal audit line: %v", err)
	}
	return string(data)
}

// resultAt builds a finished, successful result whose id and timestamps are coherent.
func resultAt(t *testing.T, startedAt time.Time) wire.Result {
	t.Helper()
	id, err := wire.NewID(startedAt, rand.Reader)
	if err != nil {
		t.Fatalf("NewID: %v", err)
	}
	return wire.Result{
		ID:         id,
		Action:     wire.ActionRestart,
		Target:     "bwmon",
		OK:         true,
		StartedAt:  startedAt,
		FinishedAt: startedAt.Add(2 * time.Second),
		DurationMS: 2000,
		Output:     "active (running)",
	}
}

func TestStatus_ListsTheNewestTwentyResultsFirst(t *testing.T) {
	f := newFixture(t)
	var ids []string
	for i := 25; i >= 1; i-- {
		result := resultAt(t, testNow.Add(-time.Duration(i)*time.Minute))
		ids = append(ids, result.ID)
		f.writeResult(t, result)
	}

	view := f.plane.Status(context.Background())
	if len(view.Results) != 20 {
		t.Fatalf("len(Results) = %d, want 20", len(view.Results))
	}
	if view.Results[0].ID != ids[len(ids)-1] {
		t.Errorf("Results[0].ID = %q, want the newest result %q", view.Results[0].ID, ids[len(ids)-1])
	}
	if view.Results[19].ID != ids[5] {
		t.Errorf("Results[19].ID = %q, want the 20th newest %q", view.Results[19].ID, ids[5])
	}
	for i := 1; i < len(view.Results); i++ {
		if !view.Results[i-1].StartedAt.After(view.Results[i].StartedAt) {
			t.Fatalf("Results are not newest first at %d: %v then %v",
				i, view.Results[i-1].StartedAt, view.Results[i].StartedAt)
		}
	}
	if view.Results[0].Sentence != "" {
		t.Errorf("Sentence for a successful result = %q, want empty", view.Results[0].Sentence)
	}
	if view.GeneratedAt != testNow {
		t.Errorf("GeneratedAt = %v, want %v", view.GeneratedAt, testNow)
	}
}

func TestStatus_SkipsWhatItCannotRead(t *testing.T) {
	f := newFixture(t)
	good := resultAt(t, testNow.Add(-time.Minute))
	f.writeResult(t, good)

	corrupt := resultAt(t, testNow.Add(-2*time.Minute))
	if err := os.WriteFile(filepath.Join(f.resultsDir(), corrupt.ID+".json"), []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.resultsDir(), "notes.json"), []byte("ignored"), 0o600); err != nil {
		t.Fatal(err)
	}

	f.writeAuditLine(t, auditLineJSON(t, wire.AuditLine{
		At: testNow.Add(-time.Minute), ID: good.ID, Action: wire.ActionRestart, Target: "bwmon",
		OK: true, DurationMS: 2000, By: wire.AuditBy, Output: "active (running)",
	}))
	f.writeAuditLine(t, `{"at":"half a line"`)
	f.writeAuditLine(t, auditLineJSON(t, wire.AuditLine{
		At: testNow.Add(-2 * time.Minute), ID: "not-a-uuid", Action: wire.ActionRestart, Target: "bwmon",
		OK: true, By: wire.AuditBy,
	}))

	view := f.plane.Status(context.Background())
	if len(view.Results) != 1 || view.Results[0].ID != good.ID {
		t.Errorf("Results = %+v, want only the readable result %s", view.Results, good.ID)
	}
	if len(view.Audit) != 1 || view.Audit[0].ID != good.ID {
		t.Errorf("Audit = %+v, want only the readable line %s", view.Audit, good.ID)
	}
}

func TestStatus_TrimsTheAuditToTheConfiguredLinesAndRetention(t *testing.T) {
	f := newFixture(t)
	f.cfg.Control.AuditLines = 3
	f.cfg.Control.AuditRetention = config.Duration(24 * time.Hour)

	var ids []string
	// Old enough to be outside the retention window.
	for i := 0; i < 2; i++ {
		result := resultAt(t, testNow.Add(-48*time.Hour+time.Duration(i)*time.Minute))
		ids = append(ids, result.ID)
		f.writeAuditLine(t, auditLineJSON(t, wire.AuditLine{
			At: result.StartedAt, ID: result.ID, Action: wire.ActionRestart, Target: "bwmon",
			OK: true, By: wire.AuditBy,
		}))
	}
	// Inside the window.
	for i := 0; i < 4; i++ {
		result := resultAt(t, testNow.Add(-time.Duration(i+1)*time.Minute))
		ids = append(ids, result.ID)
		f.writeAuditLine(t, auditLineJSON(t, wire.AuditLine{
			At: result.StartedAt, ID: result.ID, Action: wire.ActionRestart, Target: "bwmon",
			OK: true, By: wire.AuditBy,
		}))
	}

	view := f.plane.Status(context.Background())
	if len(view.Audit) != 3 {
		t.Fatalf("len(Audit) = %d, want 3", len(view.Audit))
	}
	// Newest first, and only from inside the retention window.
	want := []string{ids[2], ids[3], ids[4]}
	for i, id := range want {
		if view.Audit[i].ID != id {
			t.Errorf("Audit[%d].ID = %q, want %q", i, view.Audit[i].ID, id)
		}
	}
}

func TestStatus_ReadsOnlyTheTailOfALargeAuditFile(t *testing.T) {
	f := newFixture(t)
	f.cfg.Control.AuditLines = 100_000 // deliberately larger than the file, to see the 256 KiB cap

	const lines = 3000
	padding := strings.Repeat("x", 64)
	for i := 0; i < lines; i++ {
		at := testNow.Add(-time.Duration(lines-i) * time.Second)
		id, err := wire.NewID(at, rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		f.writeAuditLine(t, auditLineJSON(t, wire.AuditLine{
			At: at, ID: id, Action: wire.ActionRestart, Target: "bwmon",
			OK: true, By: wire.AuditBy, Output: padding,
		}))
	}
	info, err := os.Stat(f.auditPath())
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() < 300<<10 {
		t.Fatalf("the audit fixture is only %d bytes; the tail cap is not exercised", info.Size())
	}

	view := f.plane.Status(context.Background())
	if len(view.Audit) == lines {
		t.Fatalf("len(Audit) = %d; the whole file was read, not its tail", len(view.Audit))
	}
	if len(view.Audit) < 100 {
		t.Fatalf("len(Audit) = %d, want the last few hundred lines from the tail", len(view.Audit))
	}
	if !view.Audit[0].At.After(view.Audit[len(view.Audit)-1].At) {
		t.Errorf("Audit is not newest first: %v then %v", view.Audit[0].At, view.Audit[len(view.Audit)-1].At)
	}
}

func TestStatus_MarksAQueuedRequestUnclaimedAfterFifteenSeconds(t *testing.T) {
	f := newFixture(t)
	old := f.pendingRequestAt(t, wire.ActionRestart, "bwmon", testNow.Add(-16*time.Second))
	freshID, err := wire.NewID(testNow.Add(-5*time.Second), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	f.writeRequest(t, wire.Request{
		ID: freshID, Action: wire.ActionStop, Target: "bwmon", Ack: "stop-confirmed",
		RequestedAt: testNow.Add(-5 * time.Second),
	})

	view := f.plane.Status(context.Background())
	if len(view.Pending) != 2 {
		t.Fatalf("len(Pending) = %d, want 2", len(view.Pending))
	}
	byID := map[string]control.PendingView{}
	for _, pending := range view.Pending {
		byID[pending.ID] = pending
	}
	if pending := byID[old]; !pending.Unclaimed || pending.AgeS != 16 {
		t.Errorf("the 16 second old request = %+v, want unclaimed with age 16", pending)
	}
	if pending := byID[freshID]; pending.Unclaimed || pending.AgeS != 5 {
		t.Errorf("the 5 second old request = %+v, want claimed with age 5", pending)
	}
}

func TestStatus_ReadsTheExecutorFromTheHostSnapshot(t *testing.T) {
	active := wire.HostSnapshot{
		At: testNow.Add(-10 * time.Second),
		Units: map[string]wire.UnitState{
			control.ExecutorUnit: {Active: true, Since: testNow.Add(-time.Hour)},
		},
	}
	inactive := wire.HostSnapshot{
		At: testNow.Add(-10 * time.Second),
		Units: map[string]wire.UnitState{
			control.ExecutorUnit: {Active: false, Since: testNow.Add(-time.Hour)},
		},
	}
	stale := active
	stale.At = testNow.Add(-10 * time.Minute)
	withoutUnit := wire.HostSnapshot{At: testNow.Add(-10 * time.Second), Units: map[string]wire.UnitState{}}

	tests := []struct {
		name        string
		host        *wire.HostSnapshot
		queueOld    bool
		wantState   string
		wantSinceAt time.Time
	}{
		{"no snapshot yet", nil, false, control.ExecutorUnknown, time.Time{}},
		{"snapshot too old", &stale, false, control.ExecutorUnknown, time.Time{}},
		{"snapshot without the unit", &withoutUnit, false, control.ExecutorUnknown, time.Time{}},
		{"unit active", &active, false, control.ExecutorOK, testNow.Add(-time.Hour)},
		{"unit inactive", &inactive, false, control.ExecutorMissing, testNow.Add(-time.Hour)},
		{"an unclaimed request overrides an active unit", &active, true, control.ExecutorMissing, testNow.Add(-time.Hour)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f := newFixture(t)
			if test.host != nil {
				snapshot := *test.host
				f.plane.Host = func() (wire.HostSnapshot, bool) { return snapshot, true }
			} else {
				f.plane.Host = func() (wire.HostSnapshot, bool) { return wire.HostSnapshot{}, false }
			}
			if test.queueOld {
				f.pendingRequestAt(t, wire.ActionRestart, "bwmon", testNow.Add(-20*time.Second))
			}

			view := f.plane.Status(context.Background())
			if view.Executor.State != test.wantState {
				t.Errorf("Executor.State = %q, want %q", view.Executor.State, test.wantState)
			}
			if !view.Executor.Since.Equal(test.wantSinceAt) {
				t.Errorf("Executor.Since = %v, want %v", view.Executor.Since, test.wantSinceAt)
			}
		})
	}
}

func TestStatus_ListsAutoRestoresThatAreStillPending(t *testing.T) {
	stopAt := testNow.Add(-time.Minute)
	restoreAt := testNow.Add(9 * time.Minute)

	stopResult := func(t *testing.T) wire.Result {
		stop := resultAt(t, stopAt)
		stop.Action = wire.ActionStop
		stop.Target = "resolver"
		stop.RestoreAt = &restoreAt
		return stop
	}
	cancel := func(t *testing.T, ref string, ok bool) wire.Result {
		result := resultAt(t, testNow.Add(-30*time.Second))
		result.Action = wire.ActionCancelAutorestore
		result.Target = "resolver"
		result.Ref = ref
		result.OK = ok
		return result
	}
	start := func(t *testing.T) wire.Result {
		result := resultAt(t, testNow.Add(-30*time.Second))
		result.Action = wire.ActionStart
		result.Target = "resolver"
		return result
	}

	tests := []struct {
		name    string
		prepare func(t *testing.T, f *fixture, stop wire.Result)
		want    int
	}{
		{"still pending", func(*testing.T, *fixture, wire.Result) {}, 1},
		{
			name: "cancelled",
			prepare: func(t *testing.T, f *fixture, stop wire.Result) {
				f.writeResult(t, cancel(t, stop.ID, true))
			},
			want: 0,
		},
		{
			name: "a failed cancel does not count",
			prepare: func(t *testing.T, f *fixture, stop wire.Result) {
				f.writeResult(t, cancel(t, stop.ID, false))
			},
			want: 1,
		},
		{
			name: "started again by hand",
			prepare: func(t *testing.T, f *fixture, _ wire.Result) {
				f.writeResult(t, start(t))
			},
			want: 0,
		},
		{
			name: "the stop failed",
			prepare: func(t *testing.T, f *fixture, stop wire.Result) {
				// Overwrite the stop with a failed one: nothing was stopped, nothing restores.
				stop.OK = false
				f.writeResult(t, stop)
			},
			want: 0,
		},
		{
			name: "the restore already fired",
			prepare: func(t *testing.T, f *fixture, stop wire.Result) {
				past := testNow.Add(-time.Second)
				stop.RestoreAt = &past
				f.writeResult(t, stop)
			},
			want: 0,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f := newFixture(t)
			stop := stopResult(t)
			f.writeResult(t, stop)
			test.prepare(t, f, stop)

			view := f.plane.Status(context.Background())
			if len(view.Autorestore) != test.want {
				t.Fatalf("len(Autorestore) = %d, want %d (%+v)", len(view.Autorestore), test.want, view.Autorestore)
			}
			if test.want == 0 {
				return
			}
			restore := view.Autorestore[0]
			if restore.Ref != stop.ID {
				t.Errorf("Ref = %q, want the stop request %q", restore.Ref, stop.ID)
			}
			if restore.Target != "resolver" {
				t.Errorf("Target = %q, want resolver", restore.Target)
			}
			if !restore.RestoreAt.Equal(restoreAt) {
				t.Errorf("RestoreAt = %v, want %v", restore.RestoreAt, restoreAt)
			}
			if restore.RemainingS != 540 {
				t.Errorf("RemainingS = %d, want 540", restore.RemainingS)
			}
		})
	}
}

func TestStatus_WithNothingOnDisk(t *testing.T) {
	f := newFixture(t)
	view := f.plane.Status(context.Background())
	if view.Pending == nil || view.Results == nil || view.Audit == nil || view.Autorestore == nil {
		t.Fatalf("empty lists must marshal as [], not null: %+v", view)
	}
	if len(view.Pending) != 0 || len(view.Results) != 0 || len(view.Audit) != 0 {
		t.Errorf("a fresh plane has content: %+v", view)
	}

	data, err := json.Marshal(view)
	if err != nil {
		t.Fatalf("marshal the view: %v", err)
	}
	for _, want := range []string{`"pending":[]`, `"results":[]`, `"audit":[]`, `"autorestore":[]`} {
		if !strings.Contains(string(data), want) {
			t.Errorf("view JSON is missing %s: %s", want, data)
		}
	}
	if !strings.Contains(string(data), `"state":"unknown"`) {
		t.Errorf("view JSON does not say the executor is unknown: %s", data)
	}
}

func TestStatus_IsReadableWhenTheResultsDirectoryIsMissing(t *testing.T) {
	f := newFixture(t)
	mustRemoveAll(t, f.resultsDir())
	f.writeAuditLine(t, auditLineJSON(t, wire.AuditLine{
		At: testNow, ID: "0192f0c4-6c1a-7f3b-9a10-2b6f7c1d8e44", Action: wire.ActionRestart,
		Target: "bwmon", OK: true, By: wire.AuditBy,
	}))

	view := f.plane.Status(context.Background())
	if len(view.Results) != 0 {
		t.Errorf("Results = %+v, want none", view.Results)
	}
	if len(view.Audit) != 1 {
		t.Errorf("Audit = %+v, want the one line", view.Audit)
	}
}
