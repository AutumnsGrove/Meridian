package hostprobe_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/autumnsgrove/meridian/internal/hostprobe"
	"github.com/autumnsgrove/meridian/internal/wire"
)

var fixedNow = time.Date(2026, 10, 26, 21, 0, 0, 0, time.UTC)

func TestCollect_AsksSystemctlForEachUnitAndTailscaleOnce(t *testing.T) {
	var asked []string
	deps := hostprobe.Deps{
		Run: func(_ context.Context, argv []string) ([]byte, error) {
			asked = append(asked, argv[0]+" "+argv[1])
			switch argv[0] {
			case "systemctl":
				return []byte(activeShow), nil
			case "tailscale":
				return []byte(`{"Self":{"Online":true},"Peer":{"a":{"Online":true}}}`), nil
			}
			t.Fatalf("unexpected command %v", argv)
			return nil, nil
		},
		ReadFile: func(path string) ([]byte, error) {
			t.Fatalf("Collect read %s; it reads units only", path)
			return nil, nil
		},
		Now:    func() time.Time { return fixedNow },
		Uptime: func() (time.Duration, float64, error) { return 90 * time.Minute, 0.25, nil },
	}

	snapshot := hostprobe.Collect(context.Background(), deps, []string{"alpha.service", "beta.timer"})

	wantAsked := []string{"systemctl show", "systemctl show", "tailscale status"}
	if len(asked) != len(wantAsked) {
		t.Fatalf("asked = %v, want %v", asked, wantAsked)
	}
	for i, want := range wantAsked {
		if asked[i] != want {
			t.Errorf("command %d = %q, want %q", i, asked[i], want)
		}
	}

	if !snapshot.At.Equal(fixedNow) {
		t.Errorf("at = %v, want %v", snapshot.At, fixedNow)
	}
	if got := snapshot.Host; got.UptimeS != 5400 || got.Load1 != 0.25 {
		t.Errorf("host = %+v, want 5400s uptime and 0.25 load", got)
	}
	if len(snapshot.Units) != 2 {
		t.Fatalf("units = %+v, want both", snapshot.Units)
	}
	for _, unit := range []string{"alpha.service", "beta.timer"} {
		state, found := snapshot.Units[unit]
		if !found || !state.Active || !state.Since.Equal(began) {
			t.Errorf("unit %q = %+v, found %v; want active since %v", unit, state, found, began)
		}
	}
	if snapshot.Tailscale == nil || !snapshot.Tailscale.Online || snapshot.Tailscale.PeersOnline != 1 {
		t.Errorf("tailscale = %+v, want online with one peer", snapshot.Tailscale)
	}
}

func TestCollect_OmitsAUnitThatFailsToShow(t *testing.T) {
	deps := hostprobe.Deps{
		Run: func(_ context.Context, argv []string) ([]byte, error) {
			if argv[0] == "systemctl" && argv[2] == "gone.service" {
				return nil, errors.New("Unit gone.service could not be found")
			}
			return []byte(activeShow), nil
		},
		Now:    func() time.Time { return fixedNow },
		Uptime: func() (time.Duration, float64, error) { return 0, 0, nil },
	}

	snapshot := hostprobe.Collect(context.Background(), deps, []string{"here.service", "gone.service"})

	if _, found := snapshot.Units["gone.service"]; found {
		t.Errorf("units = %+v, want the unit that failed to show omitted", snapshot.Units)
	}
	if _, found := snapshot.Units["here.service"]; !found {
		t.Errorf("units = %+v, want the unit that showed to stay", snapshot.Units)
	}
}

func TestCollect_LeavesOutSourcesThatFailAndStillReportsTheRest(t *testing.T) {
	deps := hostprobe.Deps{
		Run: func(_ context.Context, argv []string) ([]byte, error) {
			if argv[0] == "tailscale" {
				return nil, errors.New("tailscale is not running")
			}
			return []byte("ActiveState=\n"), nil
		},
		Now:    func() time.Time { return fixedNow },
		Uptime: func() (time.Duration, float64, error) { return 0, 0, errors.New("/proc is missing") },
	}

	snapshot := hostprobe.Collect(context.Background(), deps, []string{"alpha.service"})

	if snapshot.Tailscale != nil {
		t.Errorf("tailscale = %+v, want it absent when the command failed", snapshot.Tailscale)
	}
	if len(snapshot.Units) != 0 {
		t.Errorf("units = %+v, want the unit without a state omitted", snapshot.Units)
	}
	if snapshot.Host != (wire.HostInfo{}) {
		t.Errorf("host = %+v, want the zero reading when /proc failed", snapshot.Host)
	}
	if !snapshot.At.Equal(fixedNow) {
		t.Errorf("at = %v, want the timestamp even when every source failed", snapshot.At)
	}
}

func TestParseMachineReadings_ReadsUptimeAndTheOneMinuteLoad(t *testing.T) {
	tests := []struct {
		name    string
		uptime  string
		loadavg string
		want    time.Duration
		want1   float64
	}{
		{
			name:    "the real shapes",
			uptime:  "1412033.42 2356789.10\n",
			loadavg: "0.31 0.22 0.19 1/234 5678\n",
			want:    1412033 * time.Second,
			want1:   0.31,
		},
		{
			name:    "a trailing newline is optional",
			uptime:  "12.9 99.0",
			loadavg: "3.5 1.0 0.5 2/100 9999",
			want:    12 * time.Second,
			want1:   3.5,
		},
		{
			name:    "a fractional uptime is truncated to whole seconds",
			uptime:  "60.99 120.00\n",
			loadavg: "0.00 0.00 0.00 1/1 1\n",
			want:    60 * time.Second,
			want1:   0,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			uptime, load, err := hostprobe.ParseMachineReadings([]byte(test.uptime), []byte(test.loadavg))

			if err != nil {
				t.Fatalf("ParseMachineReadings() = %v, want a reading", err)
			}
			if uptime != test.want || load != test.want1 {
				t.Errorf("readings = %v, %v, want %v, %v", uptime, load, test.want, test.want1)
			}
		})
	}
}

func TestParseMachineReadings_RefusesWhatProcNeverWrites(t *testing.T) {
	tests := []struct{ name, uptime, loadavg string }{
		{name: "empty", uptime: "", loadavg: ""},
		{name: "words", uptime: "up 3 days", loadavg: "0.1 0.1 0.1 1/1 1"},
		{name: "no load average", uptime: "12.0 99.0", loadavg: ""},
		{name: "load average words", uptime: "12.0 99.0", loadavg: "busy"},
		{name: "a negative uptime", uptime: "-1.0 99.0", loadavg: "0.1 0.1 0.1 1/1 1"},
		{name: "a negative load", uptime: "12.0 99.0", loadavg: "-0.1 0.1 0.1 1/1 1"},
		{name: "not a number", uptime: "NaN 99.0", loadavg: "0.1 0.1 0.1 1/1 1"},
		{name: "infinite", uptime: "12.0 99.0", loadavg: "Inf 0.1 0.1 1/1 1"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, _, err := hostprobe.ParseMachineReadings([]byte(test.uptime), []byte(test.loadavg))

			if !errors.Is(err, hostprobe.ErrInvalidReading) {
				t.Fatalf("ParseMachineReadings() = %v, want ErrInvalidReading", err)
			}
		})
	}
}
