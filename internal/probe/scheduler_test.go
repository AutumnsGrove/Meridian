package probe_test

import (
	"testing"
	"time"

	"github.com/autumnsgrove/meridian/internal/config"
	"github.com/autumnsgrove/meridian/internal/probe"
)

// testPoll is the cadence every scheduler test runs with: fast enough to express in a fake clock,
// shaped like the real one.
var testPoll = config.Poll{
	Interval:     config.Duration(15 * time.Second),
	IdleInterval: config.Duration(2 * time.Minute),
	IdleAfter:    config.Duration(10 * time.Minute),
	Timeout:      config.Duration(4 * time.Second),
	RefreshMin:   config.Duration(5 * time.Second),
}

func TestNextInterval_ChoosesTheCadenceForTheLastViewer(t *testing.T) {
	now := polledAt
	tests := []struct {
		name      string
		lastTouch time.Time
		poll      config.Poll
		want      time.Duration
	}{
		{
			name:      "a viewer is present",
			lastTouch: now.Add(-time.Minute),
			poll:      testPoll,
			want:      15 * time.Second,
		},
		{
			name:      "the far edge of idle_after still counts as present",
			lastTouch: now.Add(-10 * time.Minute),
			poll:      testPoll,
			want:      15 * time.Second,
		},
		{
			name:      "one second past idle_after backs off",
			lastTouch: now.Add(-10*time.Minute - time.Second),
			poll:      testPoll,
			want:      2 * time.Minute,
		},
		{
			name:      "a clock that went backwards is not idleness",
			lastTouch: now.Add(time.Minute),
			poll:      testPoll,
			want:      15 * time.Second,
		},
		{
			name:      "no viewer has ever been recorded",
			lastTouch: time.Time{},
			poll:      testPoll,
			want:      2 * time.Minute,
		},
		{
			name:      "a zero interval cannot spin",
			lastTouch: now,
			poll:      config.Poll{IdleAfter: config.Duration(time.Minute)},
			want:      time.Second,
		},
		{
			name:      "a negative interval cannot spin",
			lastTouch: now.Add(-time.Hour),
			poll: config.Poll{
				Interval:     config.Duration(-time.Second),
				IdleInterval: config.Duration(-time.Second),
				IdleAfter:    config.Duration(time.Minute),
			},
			want: time.Second,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := probe.NextInterval(test.lastTouch, now, test.poll); got != test.want {
				t.Fatalf("NextInterval() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestStagger_SpreadsTheFirstPolls(t *testing.T) {
	tests := []struct {
		index int
		want  time.Duration
	}{
		{index: 0, want: 0},
		{index: 1, want: 150 * time.Millisecond},
		{index: 2, want: 300 * time.Millisecond},
		{index: 3, want: 450 * time.Millisecond},
		{index: 13, want: 1950 * time.Millisecond},
		{index: 14, want: 2 * time.Second},
		{index: 500, want: 2 * time.Second},
		{index: -1, want: 0},
	}

	for _, test := range tests {
		if got := probe.Stagger(test.index); got != test.want {
			t.Fatalf("Stagger(%d) = %v, want %v", test.index, got, test.want)
		}
	}
}

// FuzzNextInterval asserts the property the loop depends on: whatever the config and whatever the
// clock does, the scheduler never waits for zero or a negative time, and never invents a third
// cadence beyond the two it was given (or the floor that stops an invalid config from spinning).
func FuzzNextInterval(f *testing.F) {
	f.Add(int64(0), int64(15*time.Second), int64(2*time.Minute), int64(10*time.Minute))
	f.Add(int64(time.Minute), int64(0), int64(0), int64(0))
	f.Add(int64(-time.Hour), int64(15*time.Second), int64(15*time.Second), int64(time.Hour))

	f.Fuzz(func(t *testing.T, offset, interval, idleInterval, idleAfter int64) {
		poll := config.Poll{
			Interval:     config.Duration(clampDuration(interval)),
			IdleInterval: config.Duration(clampDuration(idleInterval)),
			IdleAfter:    config.Duration(clampDuration(idleAfter)),
		}
		now := polledAt
		got := probe.NextInterval(now.Add(-clampDuration(offset)), now, poll)

		if got <= 0 {
			t.Fatalf("NextInterval() = %v, which would spin", got)
		}
		if floor := time.Second; got == floor {
			return
		}
		if got != time.Duration(poll.Interval) && got != time.Duration(poll.IdleInterval) {
			t.Fatalf("NextInterval() = %v, which is neither cadence", got)
		}
	})
}

// clampDuration keeps a fuzzed number inside a day either way, so the durations stay meaningful
// rather than saturating.
func clampDuration(n int64) time.Duration {
	const limit = int64(24 * time.Hour)
	switch {
	case n > limit:
		return time.Duration(limit)
	case n < -limit:
		return time.Duration(-limit)
	}
	return time.Duration(n)
}
