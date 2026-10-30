package hostexec

import (
	"errors"
	"time"

	"github.com/autumnsgrove/meridian/internal/wire"
)

// StaleAfter is how old a request may be before it stops meaning anything. A queue that filled while
// the watcher was down must not fire hours later (docs/CONTROL.md).
const StaleAfter = 120 * time.Second

// CoalesceWindow is how recently the same action on the same target may have started for a duplicate
// to be answered instead of run (decision #37).
const CoalesceWindow = 60 * time.Second

// Decision is the pure core's verdict on one request: run the allowlisted action, refuse it with a
// code, or answer a duplicate without doing anything.
type Decision struct {
	// Run is true when the request may execute. Target and Action then say what to execute.
	Run bool
	// OK is what the result's ok field says: a coalesced duplicate is ok, because nothing failed.
	OK bool
	// Code is the closed-set code. Empty when the request runs, coalesced for a duplicate, and the
	// refusal otherwise.
	Code wire.Code
	// Note is the short text the result carries; a duplicate names the request it merged with.
	Note string
	// Target is the allowlist entry, zero when the request never reached the allowlist.
	Target Target
	// Action is the action to run, for the command table.
	Action wire.Action
}

// Decide applies everything the executor knows without touching the filesystem: the request's own
// structure, whether it is a replay or stale, whether the allowlist offers it, whether the
// acknowledgement matches, and whether the same action just ran. It is table-driven and fuzzed
// because it is the only thing standing between a request file and root.
//
// The caller checks that the request's id matches its file name before calling: that is I/O, and this
// function takes data only.
func Decide(request wire.Request, targets Targets, history []wire.Result, now time.Time) Decision {
	// Structure first: a request that is not well formed gets no further.
	if err := wire.ValidateRequest(request, now); err != nil {
		code := wire.CodeBadRequest
		var refusal *wire.Refusal
		if errors.As(err, &refusal) {
			code = refusal.Code
		}
		return Decision{Code: code}
	}

	for index := range history {
		if history[index].ID == request.ID {
			return Decision{Code: wire.CodeReplay}
		}
	}

	if now.Sub(request.RequestedAt) > StaleAfter {
		return Decision{Code: wire.CodeStale}
	}

	target, known := targets[request.Target]
	if !known {
		return Decision{Code: wire.CodeUnknownTarget}
	}
	if !target.Offers(request.Action) {
		return Decision{Code: wire.CodeActionNotAllowed}
	}

	if want := wire.RequiredAck(request.Action, target.DNS); request.Ack != want {
		if request.Ack == "" {
			return Decision{Code: wire.CodeMissingAck, Target: target}
		}
		return Decision{Code: wire.CodeBadAck, Target: target}
	}

	// A cancel is only meaningful while the restore it names is still armed, and the ref is the whole
	// of what it names: refuse it before coalescing, which would otherwise answer it as a duplicate.
	if request.Action == wire.ActionCancelAutorestore && !cancellationIsArmed(request, history, now) {
		return Decision{Code: wire.CodeBadRef, Target: target, Action: request.Action}
	}

	if prior, coalesced := coalescedWith(request, history, now); coalesced {
		return Decision{
			OK: true, Code: wire.CodeCoalesced, Note: "coalesced with " + prior,
			Target: target, Action: request.Action,
		}
	}

	return Decision{Run: true, OK: true, Target: target, Action: request.Action}
}

// cancellationIsArmed reports whether a cancel-autorestore still has something to cancel: the ref
// must name a stop for this target that scheduled a restore, that restore must not have fired yet, and
// no earlier cancel may have succeeded. A failed cancel counts for nothing, so it can be retried.
func cancellationIsArmed(request wire.Request, history []wire.Result, now time.Time) bool {
	var stop *wire.Result
	for index := range history {
		if history[index].ID == request.Ref {
			stop = &history[index]
			break
		}
	}
	if stop == nil || !stop.OK || stop.RestoreAt == nil || stop.Target != request.Target {
		return false
	}
	if !now.Before(*stop.RestoreAt) {
		return false
	}
	for index := range history {
		cancelled := &history[index]
		if cancelled.OK && cancelled.Action == wire.ActionCancelAutorestore && cancelled.Ref == request.Ref {
			return false
		}
	}
	return true
}

// coalescedWith finds a result for the same action on the same target that started inside the window.
// A failed action does not count: pressing restart again after a failed restart is the whole point of
// the button. Neither does a coalesced answer, so repeated taps cannot extend the window forever.
//
// A duplicate is the same tap twice, so two things end the match. A cancel only matches a cancel that
// names the same stop. And any command that ran on the target after the match, successful or not,
// has moved the target on: a restart, a stop and a restart again is three actions, and answering the
// last one as a duplicate would report success while the service stays stopped.
func coalescedWith(request wire.Request, history []wire.Result, now time.Time) (string, bool) {
	for index := range history {
		result := &history[index]
		if result.ID == request.ID || !result.OK || result.Code == wire.CodeCoalesced {
			continue
		}
		if result.Action != request.Action || result.Target != request.Target || result.Ref != request.Ref {
			continue
		}
		if now.Sub(result.StartedAt) > CoalesceWindow {
			continue
		}
		if !movedOnSince(request, result, history) {
			return result.ID, true
		}
	}
	return "", false
}

// movedOnSince reports whether a different action ran on the request's target after prior started.
func movedOnSince(request wire.Request, prior *wire.Result, history []wire.Result) bool {
	for index := range history {
		later := &history[index]
		if later.ID == request.ID || later.Target != request.Target || later.Action == request.Action {
			continue
		}
		if later.StartedAt.After(prior.StartedAt) && ranACommand(later) {
			return true
		}
	}
	return false
}

// ranACommand is true for a result whose command was started: it succeeded, or it failed after the
// executor had begun. A refusal, a replay and a coalesced answer never touched the target.
func ranACommand(result *wire.Result) bool {
	if result.OK {
		return result.Code == ""
	}
	switch result.Code {
	case wire.CodeExecFailed, wire.CodeTimeout, wire.CodeNotActiveAfter, wire.CodeRestoreFailed:
		return true
	default:
		return false
	}
}
