package hostexec_test

import (
	"testing"
	"time"

	"github.com/autumnsgrove/meridian/internal/hostexec"
	"github.com/autumnsgrove/meridian/internal/wire"
)

var decideNow = time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)

const (
	runID    = "0192f0c4-6c1a-7f3b-9a10-2b6f7c1d8e44"
	otherID  = "0192f0c4-6c1a-7f3b-9a10-2b6f7c1d8e45"
	refID    = "0192f0c4-6c1a-7f3b-9a10-2b6f7c1d8e46"
	absentID = "0192f0c4-6c1a-7f3b-9a10-2b6f7c1d8e99"
)

func decideTargets() hostexec.Targets {
	return hostexec.Targets{
		"app": {
			Kind: hostexec.KindSystemd, Unit: "app.service", Criticality: "medium",
			Actions: []string{"start", "stop", "restart", "update"},
			Update:  &hostexec.Update{Kind: "unit", Unit: "app-update.service"},
		},
		"resolver": {
			Kind: hostexec.KindSystemd, Unit: "resolver.service", Criticality: "high",
			Actions: []string{"start", "stop", "restart"}, DNS: true,
		},
		"host": {Kind: hostexec.KindHost, Criticality: "high", Actions: []string{"reboot"}},
	}
}

func decideRequest(action wire.Action, target string) wire.Request {
	request := wire.Request{
		ID: runID, Action: action, Target: target, RequestedAt: decideNow.Add(-time.Second),
	}
	request.Ack = wire.RequiredAck(action, target == "resolver")
	return request
}

func TestDecide_AcceptsWhatTheAllowlistOffers(t *testing.T) {
	tests := []struct {
		name    string
		request wire.Request
	}{
		{"restart", decideRequest(wire.ActionRestart, "app")},
		{"start", decideRequest(wire.ActionStart, "app")},
		{"update", decideRequest(wire.ActionUpdate, "app")},
		{"stop", decideRequest(wire.ActionStop, "app")},
		{"dns stop", decideRequest(wire.ActionStop, "resolver")},
		{"reboot", decideRequest(wire.ActionReboot, "host")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			decision := hostexec.Decide(test.request, decideTargets(), nil, decideNow)
			if !decision.Run || !decision.OK || decision.Code != "" {
				t.Fatalf("Decide = %+v, want it to run", decision)
			}
			if decision.Target.Kind == "" {
				t.Error("an accepted decision carries no target")
			}
			if decision.Action != test.request.Action {
				t.Errorf("Action = %q, want %q", decision.Action, test.request.Action)
			}
		})
	}
}

func TestDecide_RefusesByCode(t *testing.T) {
	history := func(results ...wire.Result) []wire.Result { return results }

	tests := []struct {
		name    string
		request wire.Request
		history []wire.Result
		want    wire.Code
	}{
		{
			name:    "unknown target",
			request: decideRequest(wire.ActionRestart, "mail"),
			want:    wire.CodeUnknownTarget,
		},
		{
			name:    "action the target does not list",
			request: decideRequest(wire.ActionUpdate, "resolver"),
			want:    wire.CodeActionNotAllowed,
		},
		{
			name:    "reboot on a service",
			request: decideRequest(wire.ActionReboot, "app"),
			want:    wire.CodeActionNotAllowed,
		},
		{
			name: "cancel on a target that serves no DNS",
			request: func() wire.Request {
				request := decideRequest(wire.ActionCancelAutorestore, "app")
				request.Ref = refID
				return request
			}(),
			want: wire.CodeActionNotAllowed,
		},
		{
			name: "bad id",
			request: func() wire.Request {
				request := decideRequest(wire.ActionRestart, "app")
				request.ID = "nope"
				return request
			}(),
			want: wire.CodeBadID,
		},
		{
			name: "bad action",
			request: func() wire.Request {
				request := decideRequest(wire.ActionRestart, "app")
				request.Action = "destroy"
				return request
			}(),
			want: wire.CodeBadAction,
		},
		{
			name: "bad target",
			request: func() wire.Request {
				request := decideRequest(wire.ActionRestart, "app")
				request.Target = "App"
				return request
			}(),
			want: wire.CodeBadTarget,
		},
		{
			name: "missing ack",
			request: func() wire.Request {
				request := decideRequest(wire.ActionStop, "app")
				request.Ack = ""
				return request
			}(),
			want: wire.CodeMissingAck,
		},
		{
			name: "wrong ack",
			request: func() wire.Request {
				request := decideRequest(wire.ActionStop, "app")
				request.Ack = "yes"
				return request
			}(),
			want: wire.CodeBadAck,
		},
		{
			name: "plain ack where the DNS one is needed",
			request: func() wire.Request {
				request := decideRequest(wire.ActionStop, "resolver")
				request.Ack = "stop-confirmed"
				return request
			}(),
			want: wire.CodeBadAck,
		},
		{
			name: "ref on an action that takes none",
			request: func() wire.Request {
				request := decideRequest(wire.ActionRestart, "app")
				request.Ref = refID
				return request
			}(),
			want: wire.CodeBadRef,
		},
		{
			name: "requested in the future",
			request: func() wire.Request {
				request := decideRequest(wire.ActionRestart, "app")
				request.RequestedAt = decideNow.Add(time.Minute)
				return request
			}(),
			want: wire.CodeBadRequest,
		},
		{
			name: "stale",
			request: func() wire.Request {
				request := decideRequest(wire.ActionRestart, "app")
				request.RequestedAt = decideNow.Add(-121 * time.Second)
				return request
			}(),
			want: wire.CodeStale,
		},
		{
			name:    "replay",
			request: decideRequest(wire.ActionRestart, "app"),
			history: history(wire.Result{ID: runID, Action: wire.ActionRestart, Target: "app", OK: true, StartedAt: decideNow.Add(-time.Hour)}),
			want:    wire.CodeReplay,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			decision := hostexec.Decide(test.request, decideTargets(), test.history, decideNow)
			if decision.Run {
				t.Fatalf("Decide = %+v, want a refusal", decision)
			}
			if decision.Code != test.want {
				t.Errorf("Code = %q, want %q", decision.Code, test.want)
			}
		})
	}
}

func TestDecide_CoalescesADuplicateWithinAMinute(t *testing.T) {
	prior := wire.Result{
		ID: otherID, Action: wire.ActionRestart, Target: "app", OK: true,
		StartedAt: decideNow.Add(-10 * time.Second),
	}

	decision := hostexec.Decide(decideRequest(wire.ActionRestart, "app"), decideTargets(), []wire.Result{prior}, decideNow)
	if decision.Run {
		t.Fatalf("Decide = %+v, want a coalesced answer", decision)
	}
	if !decision.OK || decision.Code != wire.CodeCoalesced {
		t.Errorf("decision = %+v, want ok true and code coalesced", decision)
	}
	if decision.Note != "coalesced with "+otherID {
		t.Errorf("Note = %q, want the prior request named", decision.Note)
	}
}

func TestDecide_DoesNotCoalesceWhatShouldRunAgain(t *testing.T) {
	tests := []struct {
		name  string
		prior wire.Result
	}{
		{
			name: "a different action",
			prior: wire.Result{
				ID: otherID, Action: wire.ActionStop, Target: "app", OK: true,
				StartedAt: decideNow.Add(-10 * time.Second),
			},
		},
		{
			name: "a different target",
			prior: wire.Result{
				ID: otherID, Action: wire.ActionRestart, Target: "resolver", OK: true,
				StartedAt: decideNow.Add(-10 * time.Second),
			},
		},
		{
			name: "older than a minute",
			prior: wire.Result{
				ID: otherID, Action: wire.ActionRestart, Target: "app", OK: true,
				StartedAt: decideNow.Add(-61 * time.Second),
			},
		},
		{
			name: "a failure worth retrying",
			prior: wire.Result{
				ID: otherID, Action: wire.ActionRestart, Target: "app", OK: false,
				Code: wire.CodeExecFailed, StartedAt: decideNow.Add(-10 * time.Second),
			},
		},
		{
			name: "a coalesced answer is not an action",
			prior: wire.Result{
				ID: otherID, Action: wire.ActionRestart, Target: "app", OK: true,
				Code: wire.CodeCoalesced, StartedAt: decideNow.Add(-10 * time.Second),
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			decision := hostexec.Decide(decideRequest(wire.ActionRestart, "app"), decideTargets(), []wire.Result{test.prior}, decideNow)
			if !decision.Run {
				t.Fatalf("Decide = %+v, want it to run", decision)
			}
		})
	}
}

// FuzzDecide checks the pure core against a deliberately dumb reference: accepted only when every
// documented condition holds, and the refusal code is one of the closed set.
func FuzzDecide(f *testing.F) {
	f.Add(runID, "restart", "app", "", "", int64(1), otherID, "restart", "app", int64(10), true)
	f.Add(runID, "stop", "app", "stop-confirmed", "", int64(1), "", "", "", int64(0), false)
	f.Add(runID, "stop", "resolver", "dns-stop-confirmed", "", int64(1), otherID, "stop", "resolver", int64(5), true)
	f.Add(runID, "reboot", "host", "reboot-confirmed", "", int64(1), "", "", "", int64(0), false)
	f.Add(runID, "restart", "app", "", "", int64(200), otherID, "restart", "app", int64(10), true)
	f.Add(runID, "cancel-autorestore", "app", "", refID, int64(1), "", "", "", int64(0), false)
	f.Add("", "", "", "", "", int64(0), "", "", "", int64(0), false)
	f.Add(runID+"\x00", "restart\x00", "app\x00", "\x00", "\x00", int64(-5), otherID, "start\x00", "app", int64(-1), true)

	f.Fuzz(func(t *testing.T, id, action, target, ack, ref string, agoSeconds int64,
		priorID, priorAction, priorTarget string, priorAgoSeconds int64, priorOK bool) {
		request := wire.Request{
			ID: id, Action: wire.Action(action), Target: target, Ack: ack, Ref: ref,
			RequestedAt: decideNow.Add(-time.Duration(agoSeconds) * time.Second),
		}
		var history []wire.Result
		if priorID != "" {
			history = append(history, wire.Result{
				ID: priorID, Action: wire.Action(priorAction), Target: priorTarget, OK: priorOK,
				StartedAt: decideNow.Add(-time.Duration(priorAgoSeconds) * time.Second),
			})
		}

		decision := hostexec.Decide(request, decideTargets(), history, decideNow)

		if want := referenceRuns(request, decideTargets(), history, decideNow); decision.Run != want {
			t.Fatalf("Decide(%+v) Run = %v, reference says %v (decision %+v)", request, decision.Run, want, decision)
		}
		if decision.Run {
			if decision.Code != "" || !decision.OK {
				t.Fatalf("a runnable decision = %+v, want no code and ok", decision)
			}
			return
		}
		if !wire.IsCode(decision.Code) {
			t.Fatalf("refusal code %q is not in the closed table", decision.Code)
		}
	})
}

// referenceRuns is the deliberately dumb restatement of Decide's conditions. It shares no code with
// the implementation, which is the point: a rule that drifts shows up as a disagreement.
func referenceRuns(request wire.Request, targets hostexec.Targets, history []wire.Result, now time.Time) bool {
	if wire.ValidateRequest(request, now) != nil {
		return false
	}
	for index := range history {
		if history[index].ID == request.ID {
			return false
		}
	}
	if now.Sub(request.RequestedAt) > 120*time.Second {
		return false
	}
	target, known := targets[request.Target]
	if !known {
		return false
	}
	offered := false
	for _, action := range target.Actions {
		if action == string(request.Action) {
			offered = true
		}
	}
	if !offered {
		return false
	}
	wantAck := ""
	if request.Action == wire.ActionStop {
		wantAck = "stop-confirmed"
		if target.DNS {
			wantAck = "dns-stop-confirmed"
		}
	}
	if request.Action == wire.ActionReboot {
		wantAck = "reboot-confirmed"
	}
	if request.Ack != wantAck {
		return false
	}
	for index := range history {
		result := &history[index]
		if result.ID == request.ID {
			continue
		}
		if result.Action == request.Action && result.Target == request.Target &&
			result.OK && result.Code != wire.CodeCoalesced &&
			now.Sub(result.StartedAt) <= 60*time.Second {
			return false
		}
	}
	return true
}

// cancelOf is a well-formed cancel-autorestore for the resolver, naming ref.
func cancelOf(ref string) wire.Request {
	request := decideRequest(wire.ActionCancelAutorestore, "resolver")
	request.Ref = ref
	return request
}

// armedStop is a DNS stop result that scheduled its auto-restore.
func armedStop(id, target string, restoreAt time.Time) wire.Result {
	return wire.Result{
		ID: id, Action: wire.ActionStop, Target: target, OK: true,
		StartedAt: decideNow.Add(-time.Minute), FinishedAt: decideNow.Add(-time.Minute),
		RestoreAt: &restoreAt,
	}
}

// TestDecide_RefusesACancelThatWouldNotCancelAnything is the second half of the DNS guard: a cancel
// only means something while the restore it names is still armed (decision #17).
func TestDecide_RefusesACancelThatWouldNotCancelAnything(t *testing.T) {
	armed := decideNow.Add(9 * time.Minute)
	fired := decideNow.Add(-time.Minute)

	tests := []struct {
		name    string
		ref     string
		history []wire.Result
		want    wire.Code
	}{
		{
			name: "a ref no result answers to",
			ref:  refID,
			want: wire.CodeBadRef,
		},
		{
			name: "a ref that is not a UUID",
			ref:  "not-a-uuid",
			want: wire.CodeBadRef,
		},
		{
			name:    "a stop for another target",
			ref:     refID,
			history: []wire.Result{armedStop(refID, "app", armed)},
			want:    wire.CodeBadRef,
		},
		{
			name:    "a result that armed nothing",
			ref:     refID,
			history: []wire.Result{{ID: refID, Action: wire.ActionRestart, Target: "resolver", OK: true, StartedAt: decideNow.Add(-time.Minute)}},
			want:    wire.CodeBadRef,
		},
		{
			name:    "a restore that already fired",
			ref:     refID,
			history: []wire.Result{armedStop(refID, "resolver", fired)},
			want:    wire.CodeBadRef,
		},
		{
			name: "the restore was already cancelled",
			ref:  refID,
			history: []wire.Result{
				armedStop(refID, "resolver", armed),
				{
					ID: otherID, Action: wire.ActionCancelAutorestore, Target: "resolver", Ref: refID, OK: true,
					StartedAt: decideNow.Add(-time.Minute),
				},
			},
			want: wire.CodeBadRef,
		},
		{
			name: "a stop that failed armed nothing",
			ref:  refID,
			history: []wire.Result{func() wire.Result {
				stop := armedStop(refID, "resolver", armed)
				stop.OK = false
				stop.Code = wire.CodeExecFailed
				return stop
			}()},
			want: wire.CodeBadRef,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			decision := hostexec.Decide(cancelOf(test.ref), decideTargets(), test.history, decideNow)
			if decision.Run {
				t.Fatalf("Decide = %+v, want a refusal", decision)
			}
			if decision.Code != test.want {
				t.Errorf("Code = %q, want %q", decision.Code, test.want)
			}
		})
	}
}

func TestDecide_AcceptsACancelForAnArmedRestore(t *testing.T) {
	armed := decideNow.Add(9 * time.Minute)

	tests := []struct {
		name    string
		history []wire.Result
	}{
		{
			name:    "an armed stop",
			history: []wire.Result{armedStop(refID, "resolver", armed)},
		},
		{
			name: "a cancel that failed did not cancel anything",
			history: []wire.Result{
				armedStop(refID, "resolver", armed),
				{
					ID: otherID, Action: wire.ActionCancelAutorestore, Target: "resolver", Ref: refID, OK: false,
					Code: wire.CodeExecFailed, StartedAt: decideNow.Add(-time.Minute),
				},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			decision := hostexec.Decide(cancelOf(refID), decideTargets(), test.history, decideNow)
			if !decision.Run || !decision.OK {
				t.Fatalf("Decide = %+v, want it to run", decision)
			}
			if decision.Action != wire.ActionCancelAutorestore {
				t.Errorf("Action = %q, want the cancel", decision.Action)
			}
		})
	}
}

// FuzzDecideCancel holds the ref checks to a deliberately dumb reference: a cancel is accepted only
// when the ref names an armed restore, for this target, that nothing has cancelled yet.
func FuzzDecideCancel(f *testing.F) {
	f.Add(refID, refID, true, 540, true)
	f.Add(refID, runID, true, 540, false)
	f.Add(refID, refID, false, 540, false)
	f.Add(refID, refID, true, -60, false)
	f.Add(refID, refID, true, 540, false)
	f.Add("not-a-uuid", "not-a-uuid", true, 540, false)
	f.Add("", "", false, 0, false)

	f.Fuzz(func(t *testing.T, ref, stopID string, stopOK bool, restoreIn int, cancelled bool) {
		restoreAt := decideNow.Add(time.Duration(restoreIn) * time.Second)
		history := []wire.Result{{
			ID: stopID, Action: wire.ActionStop, Target: "resolver", OK: stopOK,
			StartedAt: decideNow.Add(-time.Minute), FinishedAt: decideNow.Add(-time.Minute),
			RestoreAt: &restoreAt,
		}}
		if cancelled {
			history = append(history, wire.Result{
				ID: otherID, Action: wire.ActionCancelAutorestore, Target: "resolver", Ref: stopID, OK: true,
				StartedAt: decideNow.Add(-time.Minute),
			})
		}

		decision := hostexec.Decide(cancelOf(ref), decideTargets(), history, decideNow)
		want := referenceCancelRuns(ref, stopID, stopOK, cancelled, restoreIn)
		if decision.Run != want {
			t.Fatalf("Decide(ref=%q, stop=%q, ok=%v, restoreIn=%d, cancelled=%v) = %+v, want run=%v",
				ref, stopID, stopOK, restoreIn, cancelled, decision, want)
		}
		if !decision.Run && decision.Code != wire.CodeBadRef && decision.Code != wire.CodeBadRequest &&
			decision.Code != wire.CodeReplay && decision.Code != wire.CodeBadID {
			t.Fatalf("refusal code %q is not one the ref rules produce", decision.Code)
		}
	})
}

// referenceCancelRuns is the dumb version: no tables, no history scan, just the conditions written
// out in the order the docs list them.
func referenceCancelRuns(ref, stopID string, stopOK, cancelled bool, restoreIn int) bool {
	if _, err := wire.ParseID(ref); err != nil {
		return false
	}
	if stopID == runID {
		return false // the request's own id is already in the results: a replay
	}
	if ref != stopID || !stopOK || cancelled {
		return false
	}
	return restoreIn > 0
}
