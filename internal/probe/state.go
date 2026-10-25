package probe

import (
	"slices"
	"time"
)

// VitalState is one vital as the page shows it: the last good reading, when it was taken, and
// whether the most recent attempt to refresh it failed. The age of At is what makes a stale reading
// visible, so a value is never shown without it.
type VitalState struct {
	Label   string
	Value   string
	At      time.Time
	Failing bool
	Code    string
}

// State is one service's last poll: its health, when that poll happened, and its vitals.
type State struct {
	Health    Health
	CheckedAt time.Time
	Vitals    []VitalState
}

// VitalFresh is one vital outcome from the current cycle: the formatted value, or the error that
// replaced it. Merge reads the error's reason code, so Value is ignored when Err is set.
type VitalFresh struct {
	Label string
	Value string
	Err   error
}

// Merge folds a fresh poll into the previous state. It never mutates its arguments and never turns a
// health that is not ok into one that is.
func Merge(prev State, health Health, vitals []VitalFresh, now time.Time) State {
	state := State{Health: health, CheckedAt: now}
	if health.Status != StatusOK {
		// A vital is only fetched from a healthy service, so nothing was asked this cycle: the last
		// readings stay as they were and their growing age is what marks them stale.
		state.Vitals = slices.Clone(prev.Vitals)
		return state
	}

	state.Vitals = make([]VitalState, 0, len(vitals))
	for position, fresh := range vitals {
		prior, matched := priorVital(prev.Vitals, position, fresh.Label)
		if fresh.Err == nil {
			state.Vitals = append(state.Vitals, VitalState{Label: fresh.Label, Value: fresh.Value, At: now})
			continue
		}
		vital := VitalState{Label: fresh.Label, At: now, Failing: true, Code: CodeOf(fresh.Err)}
		// Stale-while-error: a failure keeps the last good reading and the time it was taken. A prior
		// vital that is itself failing is worth keeping only if it still holds a value.
		if matched && (prior.Value != "" || !prior.Failing) {
			vital.Value = prior.Value
			vital.At = prior.At
		}
		state.Vitals = append(state.Vitals, vital)
	}
	return state
}

// priorVital finds the previous reading for a vital: the same label at the same position. A reload
// that renames or reorders a vital therefore starts it fresh instead of pairing it with the wrong
// neighbour's value.
func priorVital(previous []VitalState, position int, label string) (VitalState, bool) {
	if position >= len(previous) || previous[position].Label != label {
		return VitalState{}, false
	}
	return previous[position], true
}

// Derive decides the tile status, and is the only place degraded is decided: a healthy service with
// a failing vital is degraded, because a missing number must not make the page lie about
// availability (decision #40). A zero CheckedAt means the service has never been polled.
func Derive(state State, enabled bool) Status {
	switch {
	case !enabled:
		return StatusDisabled
	case state.CheckedAt.IsZero():
		return StatusUnknown
	}
	switch state.Health.Status {
	case StatusOK:
		if anyVitalFailing(state.Vitals) {
			return StatusDegraded
		}
		return StatusOK
	case StatusDown, StatusUnknown:
		return state.Health.Status
	default:
		// No runner returns anything else; an unrecognised status is not a claim we can make.
		return StatusUnknown
	}
}

func anyVitalFailing(vitals []VitalState) bool {
	for _, vital := range vitals {
		if vital.Failing {
			return true
		}
	}
	return false
}
