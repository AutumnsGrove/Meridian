package probe_test

import (
	"fmt"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/autumnsgrove/meridian/internal/probe"
)

// polledAt is the fixed clock every merge test runs at, so no test reads the wall clock.
var polledAt = time.Date(2026, 10, 24, 18, 0, 0, 0, time.UTC)

// assertState compares two states field by field: slices.Equal treats an empty and a nil vital list
// as the same, which is what a caller means.
func assertState(t *testing.T, got, want probe.State) {
	t.Helper()
	if got.Health != want.Health || !got.CheckedAt.Equal(want.CheckedAt) || !slices.Equal(got.Vitals, want.Vitals) {
		t.Fatalf("state = %+v, want %+v", got, want)
	}
}

func TestMerge_RecordsTheHealthAndTheCheckTime(t *testing.T) {
	got := probe.Merge(probe.State{}, probe.Health{Status: probe.StatusOK}, nil, polledAt)
	want := probe.State{Health: probe.Health{Status: probe.StatusOK}, CheckedAt: polledAt}
	assertState(t, got, want)
}

func TestMerge_KeepsTheLastGoodValueWhenAVitalFails(t *testing.T) {
	earlier := polledAt.Add(-30 * time.Second)
	prev := probe.State{
		Health:    probe.Health{Status: probe.StatusOK},
		CheckedAt: earlier,
		Vitals:    []probe.VitalState{{Label: "needs you", Value: "4", At: earlier}},
	}

	got := probe.Merge(prev, probe.Health{Status: probe.StatusOK}, []probe.VitalFresh{
		{Label: "needs you", Err: fmt.Errorf("%w: items", probe.ErrMissingField)},
	}, polledAt)

	want := probe.State{
		Health:    probe.Health{Status: probe.StatusOK},
		CheckedAt: polledAt,
		Vitals: []probe.VitalState{
			{Label: "needs you", Value: "4", At: earlier, Failing: true, Code: probe.CodeMissingField},
		},
	}
	assertState(t, got, want)
}

func TestMerge_ReportsAFailedVitalThatNeverHadAValue(t *testing.T) {
	got := probe.Merge(probe.State{}, probe.Health{Status: probe.StatusOK}, []probe.VitalFresh{
		{Label: "blocked", Err: probe.ErrUnexpectedType},
	}, polledAt)

	want := probe.State{
		Health:    probe.Health{Status: probe.StatusOK},
		CheckedAt: polledAt,
		Vitals: []probe.VitalState{
			{Label: "blocked", At: polledAt, Failing: true, Code: probe.CodeUnexpectedType},
		},
	}
	assertState(t, got, want)
}

func TestMerge_ReplacesTheValueWhenAVitalRecovers(t *testing.T) {
	earlier := polledAt.Add(-30 * time.Second)
	prev := probe.State{
		Health:    probe.Health{Status: probe.StatusOK},
		CheckedAt: earlier,
		Vitals: []probe.VitalState{
			{Label: "needs you", Value: "4", At: earlier, Failing: true, Code: probe.CodeTimeout},
		},
	}

	got := probe.Merge(prev, probe.Health{Status: probe.StatusOK}, []probe.VitalFresh{
		{Label: "needs you", Value: "7"},
	}, polledAt)

	want := probe.State{
		Health:    probe.Health{Status: probe.StatusOK},
		CheckedAt: polledAt,
		Vitals:    []probe.VitalState{{Label: "needs you", Value: "7", At: polledAt}},
	}
	assertState(t, got, want)
}

func TestMerge_LeavesVitalsAloneWhenHealthIsNotOK(t *testing.T) {
	earlier := polledAt.Add(-30 * time.Second)
	prev := probe.State{
		Health:    probe.Health{Status: probe.StatusOK},
		CheckedAt: earlier,
		Vitals: []probe.VitalState{
			{Label: "needs you", Value: "4", At: earlier},
			{Label: "blocked", At: earlier, Failing: true, Code: probe.CodeTimeout},
		},
	}

	got := probe.Merge(prev, probe.Health{Status: probe.StatusDown, Code: probe.CodeTimeout}, nil, polledAt)

	want := probe.State{
		Health:    probe.Health{Status: probe.StatusDown, Code: probe.CodeTimeout},
		CheckedAt: polledAt,
		Vitals:    prev.Vitals,
	}
	assertState(t, got, want)
}

func TestMerge_MatchesVitalsByPositionAndLabel(t *testing.T) {
	earlier := polledAt.Add(-30 * time.Second)
	prev := probe.State{
		Health:    probe.Health{Status: probe.StatusOK},
		CheckedAt: earlier,
		Vitals: []probe.VitalState{
			{Label: "needs you", Value: "4", At: earlier},
			{Label: "blocked", Value: "30", At: earlier},
		},
	}

	tests := []struct {
		name   string
		fresh  []probe.VitalFresh
		vitals []probe.VitalState
	}{
		{
			name:  "a vital that moved to another position starts fresh",
			fresh: []probe.VitalFresh{{Label: "blocked", Err: probe.ErrMissingField}},
			vitals: []probe.VitalState{
				{Label: "blocked", At: polledAt, Failing: true, Code: probe.CodeMissingField},
			},
		},
		{
			name:  "a renamed label starts fresh",
			fresh: []probe.VitalFresh{{Label: "waiting", Value: "4"}},
			vitals: []probe.VitalState{
				{Label: "waiting", Value: "4", At: polledAt},
			},
		},
		{
			name:  "the same label in the same position keeps its value",
			fresh: []probe.VitalFresh{{Label: "needs you", Err: probe.ErrMissingField}},
			vitals: []probe.VitalState{
				{Label: "needs you", Value: "4", At: earlier, Failing: true, Code: probe.CodeMissingField},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := probe.Merge(prev, probe.Health{Status: probe.StatusOK}, test.fresh, polledAt)
			want := probe.State{
				Health:    probe.Health{Status: probe.StatusOK},
				CheckedAt: polledAt,
				Vitals:    test.vitals,
			}
			assertState(t, got, want)
		})
	}
}

func TestMerge_DoesNotMutateItsArguments(t *testing.T) {
	earlier := polledAt.Add(-30 * time.Second)
	prev := probe.State{
		Health:    probe.Health{Status: probe.StatusOK},
		CheckedAt: earlier,
		Vitals:    []probe.VitalState{{Label: "needs you", Value: "4", At: earlier}},
	}
	fresh := []probe.VitalFresh{{Label: "needs you", Err: probe.ErrBadValue}}
	beforePrev := probe.State{
		Health:    prev.Health,
		CheckedAt: prev.CheckedAt,
		Vitals:    slices.Clone(prev.Vitals),
	}
	beforeFresh := slices.Clone(fresh)

	probe.Merge(prev, probe.Health{Status: probe.StatusOK}, fresh, polledAt)

	assertState(t, prev, beforePrev)
	if !reflect.DeepEqual(fresh, beforeFresh) {
		t.Fatalf("fresh = %+v, want %+v", fresh, beforeFresh)
	}
}

func TestDerive_DecidesTheTileStatus(t *testing.T) {
	healthy := probe.Health{Status: probe.StatusOK}
	tests := []struct {
		name    string
		state   probe.State
		enabled bool
		want    probe.Status
	}{
		{
			name:    "configured off is disabled whatever the state says",
			state:   probe.State{Health: healthy, CheckedAt: polledAt},
			enabled: false,
			want:    probe.StatusDisabled,
		},
		{
			name:    "configured off before the first poll is still disabled",
			enabled: false,
			want:    probe.StatusDisabled,
		},
		{
			name:    "never polled is unknown",
			enabled: true,
			want:    probe.StatusUnknown,
		},
		{
			name:    "healthy with no vitals is ok",
			state:   probe.State{Health: healthy, CheckedAt: polledAt},
			enabled: true,
			want:    probe.StatusOK,
		},
		{
			name: "healthy with healthy vitals is ok",
			state: probe.State{
				Health:    healthy,
				CheckedAt: polledAt,
				Vitals:    []probe.VitalState{{Label: "needs you", Value: "4", At: polledAt}},
			},
			enabled: true,
			want:    probe.StatusOK,
		},
		{
			name: "healthy with a failing vital is degraded",
			state: probe.State{
				Health:    healthy,
				CheckedAt: polledAt,
				Vitals: []probe.VitalState{
					{Label: "needs you", Value: "4", At: polledAt},
					{Label: "blocked", At: polledAt, Failing: true, Code: probe.CodeTimeout},
				},
			},
			enabled: true,
			want:    probe.StatusDegraded,
		},
		{
			name:    "down is down",
			state:   probe.State{Health: probe.Health{Status: probe.StatusDown, Code: probe.CodeTimeout}, CheckedAt: polledAt},
			enabled: true,
			want:    probe.StatusDown,
		},
		{
			name:    "unknown is unknown",
			state:   probe.State{Health: probe.Health{Status: probe.StatusUnknown, Code: probe.CodeEnvMissing}, CheckedAt: polledAt},
			enabled: true,
			want:    probe.StatusUnknown,
		},
		{
			name: "a down health with a failing vital is not degraded",
			state: probe.State{
				Health:    probe.Health{Status: probe.StatusDown, Code: probe.CodeTimeout},
				CheckedAt: polledAt,
				Vitals:    []probe.VitalState{{Label: "blocked", Failing: true, Code: probe.CodeTimeout}},
			},
			enabled: true,
			want:    probe.StatusDown,
		},
		{
			name:    "a health status no runner returns is unknown",
			state:   probe.State{Health: probe.Health{Status: probe.StatusDegraded}, CheckedAt: polledAt},
			enabled: true,
			want:    probe.StatusUnknown,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := probe.Derive(test.state, test.enabled); got != test.want {
				t.Fatalf("Derive() = %q, want %q", got, test.want)
			}
		})
	}
}

// FuzzMerge runs arbitrary sequences of health and vital outcomes through the merge, then asserts the
// rules that hold for every sequence: the closed status set, no ok from a down health, vitals kept
// when they were not fetched, and a failing vital only ever showing a value it really had.
func FuzzMerge(f *testing.F) {
	f.Add([]byte{0x00})
	f.Add([]byte{0x11, 0x22, 0x33, 0x44})
	f.Add([]byte{0x7f, 0x80, 0xff})

	statuses := []probe.Status{"", probe.StatusOK, probe.StatusOK, probe.StatusDown, probe.StatusUnknown, "bogus"}
	outcomes := []error{
		nil, nil, probe.ErrMissingField, probe.ErrUnexpectedType, probe.ErrBadValue,
		fmt.Errorf("wrapped: %w", probe.ErrHostStale),
	}
	closedSet := []probe.Status{
		probe.StatusOK, probe.StatusDegraded, probe.StatusDown, probe.StatusUnknown, probe.StatusDisabled,
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		now := polledAt
		var prev probe.State
		for index, octet := range data {
			health := probe.Health{Status: statuses[int(octet)%len(statuses)], Code: probe.CodeBadValue}
			count := int(octet % 4)
			fresh := make([]probe.VitalFresh, 0, count)
			for vital := range count {
				outcome := outcomes[(int(octet)+vital+index)%len(outcomes)]
				fresh = append(fresh, probe.VitalFresh{
					Label: string(rune('a' + vital)),
					Value: "1",
					Err:   outcome,
				})
			}
			prior := prev
			next := probe.Merge(prev, health, fresh, now)
			if !reflect.DeepEqual(prior, prev) {
				t.Fatalf("Merge mutated prev: %+v, want %+v", prev, prior)
			}

			derived := probe.Derive(next, true)
			if !slices.Contains(closedSet, derived) {
				t.Fatalf("Derive() = %q, which is outside the closed set", derived)
			}
			if health.Status == probe.StatusDown && (derived == probe.StatusOK || derived == probe.StatusDegraded) {
				t.Fatalf("a down health derived %q", derived)
			}

			if health.Status == probe.StatusOK {
				if len(next.Vitals) != len(fresh) {
					t.Fatalf("kept %d vitals, want %d", len(next.Vitals), len(fresh))
				}
				for position, vital := range next.Vitals {
					if vital.Failing && vital.Value != "" {
						if prior.Vitals[position].Value != vital.Value || !prior.Vitals[position].At.Equal(vital.At) {
							t.Fatalf("vital %d invented a value: %+v", position, vital)
						}
					}
				}
			} else if !slices.Equal(next.Vitals, prior.Vitals) {
				t.Fatalf("vitals changed without being fetched: %+v, want %+v", next.Vitals, prior.Vitals)
			}

			prev = next
			now = now.Add(time.Second)
		}
	})
}

func TestTransitionLine(t *testing.T) {
	tests := []struct {
		name          string
		before, after probe.Status
		code          string
		want          string
	}{
		{name: "no change says nothing", before: probe.StatusOK, after: probe.StatusOK},
		{name: "a service goes down", before: probe.StatusOK, after: probe.StatusDown, code: probe.CodeTimeout, want: "ivy: ok -> down (timeout)"},
		{name: "a service comes back", before: probe.StatusDown, after: probe.StatusOK, code: "", want: "ivy: down -> ok"},
		{name: "degraded carries no health code", before: probe.StatusOK, after: probe.StatusDegraded, code: "", want: "ivy: ok -> degraded"},
		{name: "unknown is explained too", before: probe.StatusOK, after: probe.StatusUnknown, code: probe.CodeTimeout, want: "ivy: ok -> unknown (timeout)"},
		{name: "a disabled service has no code to give", before: probe.StatusOK, after: probe.StatusDisabled, code: "", want: "ivy: ok -> disabled"},
		{name: "a missing code leaves the parentheses off", before: probe.StatusOK, after: probe.StatusDown, code: "", want: "ivy: ok -> down"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := probe.TransitionLine("ivy", test.before, test.after, test.code); got != test.want {
				t.Errorf("TransitionLine() = %q, want %q", got, test.want)
			}
		})
	}
}
