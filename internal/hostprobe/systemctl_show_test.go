package hostprobe_test

import (
	"strings"
	"testing"
	"time"

	"github.com/autumnsgrove/meridian/internal/hostprobe"
	"github.com/autumnsgrove/meridian/internal/wire"
)

var (
	// began is what systemctl prints for ActiveEnterTimestamp in the fixtures, in UTC so the test
	// does not depend on the machine's timezone.
	began = time.Date(2026, 10, 22, 18, 4, 11, 0, time.UTC)

	activeShow   = "ActiveState=active\nActiveEnterTimestamp=Thu 2026-10-22 18:04:11 UTC\n"
	inactiveShow = "ActiveState=inactive\nActiveEnterTimestamp=\n"
)

func TestParseSystemctlShow_ReadsTheStateAndWhenItBegan(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   wire.UnitState
	}{
		{
			name:   "active",
			output: activeShow,
			want:   wire.UnitState{Active: true, Since: began},
		},
		{
			name:   "inactive, which has never had an active entry",
			output: inactiveShow,
			want:   wire.UnitState{Active: false},
		},
		{
			name:   "failed",
			output: "ActiveState=failed\nActiveEnterTimestamp=n/a\n",
			want:   wire.UnitState{Active: false},
		},
		{
			name:   "inactive with the timestamp still set",
			output: "ActiveState=inactive\nActiveEnterTimestamp=Thu 2026-10-22 18:04:11 UTC\n",
			want:   wire.UnitState{Active: false, Since: began},
		},
		{
			name: "the other properties systemctl prints alongside",
			output: "LoadState=loaded\nActiveState=active\nActiveEnterTimestamp=Thu 2026-10-22 18:04:11 UTC\n" +
				"FragmentPath=/usr/lib/systemd/system/a.service\nSubState=running\n",
			want: wire.UnitState{Active: true, Since: began},
		},
		{
			name:   "CRLF line endings",
			output: "ActiveState=active\r\nActiveEnterTimestamp=Thu 2026-10-22 18:04:11 UTC\r\n",
			want:   wire.UnitState{Active: true, Since: began},
		},
		{
			name:   "fractional seconds",
			output: "ActiveState=active\nActiveEnterTimestamp=Thu 2026-10-22 18:04:11.500000 UTC\n",
			want:   wire.UnitState{Active: true, Since: began.Add(500 * time.Millisecond)},
		},
		{
			name:   "a timestamp this build cannot parse keeps the state",
			output: "ActiveState=active\nActiveEnterTimestamp=whenever\n",
			want:   wire.UnitState{Active: true},
		},
		{
			name:   "no timestamp property at all",
			output: "ActiveState=active\n",
			want:   wire.UnitState{Active: true},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			state, ok := hostprobe.ParseSystemctlShow([]byte(test.output))

			if !ok {
				t.Fatalf("ParseSystemctlShow() = not ok, want the unit state")
			}
			if state.Active != test.want.Active {
				t.Errorf("active = %v, want %v", state.Active, test.want.Active)
			}
			if !state.Since.Equal(test.want.Since) {
				t.Errorf("since = %v, want %v", state.Since, test.want.Since)
			}
		})
	}
}

func TestParseSystemctlShow_RefusesOutputWithoutAState(t *testing.T) {
	for _, output := range []string{
		"",
		"\n",
		"ActiveState=\n",
		"ActiveState\n",
		"nonsense\n",
		"ActiveEnterTimestamp=Thu 2026-10-22 18:04:11 UTC\n",
	} {
		t.Run(strings.TrimSpace(output), func(t *testing.T) {
			if state, ok := hostprobe.ParseSystemctlShow([]byte(output)); ok {
				t.Fatalf("ParseSystemctlShow() = %+v, ok, want not ok", state)
			}
		})
	}
}

// FuzzParseSystemctlShow checks the parser against a deliberately dumb reference: the last
// ActiveState line wins, an absent or empty one means no state, and Active means exactly "active".
func FuzzParseSystemctlShow(f *testing.F) {
	f.Add([]byte(activeShow))
	f.Add([]byte(inactiveShow))
	f.Add([]byte("ActiveState=failed\nActiveEnterTimestamp=n/a\n"))
	f.Add([]byte("ActiveState=active\r\nActiveEnterTimestamp=Thu 2026-10-22 18:04:11 UTC\r\n"))
	f.Add([]byte("ActiveState=\x00\n"))
	f.Add([]byte("ActiveEnterTimestamp=\n"))
	f.Add([]byte(""))
	f.Add([]byte(strings.Repeat("ActiveState=active\n", 1000)))

	f.Fuzz(func(t *testing.T, output []byte) {
		state, ok := hostprobe.ParseSystemctlShow(output)

		// An independent, deliberately dumb reference: the last ActiveState line wins, and a
		// missing or empty one means there is nothing to report.
		value, present := "", false
		for _, line := range strings.Split(string(output), "\n") {
			if key, rest, found := strings.Cut(strings.TrimSuffix(line, "\r"), "="); found && key == "ActiveState" {
				value, present = rest, true
			}
		}

		if !present || value == "" {
			if ok {
				t.Fatalf("ParseSystemctlShow(%q) = %+v, ok, want not ok", output, state)
			}
			return
		}
		if !ok {
			t.Fatalf("ParseSystemctlShow(%q) = not ok, want a state", output)
		}
		if state.Active != (value == "active") {
			t.Fatalf("ParseSystemctlShow(%q).Active = %v, want %v", output, state.Active, value == "active")
		}
	})
}
