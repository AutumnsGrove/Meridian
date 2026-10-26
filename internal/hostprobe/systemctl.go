package hostprobe

import (
	"strings"
	"time"

	"github.com/autumnsgrove/meridian/internal/wire"
)

// systemctlTimestampLayouts are the shapes systemd prints ActiveEnterTimestamp in: with microseconds,
// and without. systemd prints it in the host's local zone, and the probe runs on that host, so the
// value is parsed in the local zone to recover the real instant.
var systemctlTimestampLayouts = []string{
	"Mon 2006-01-02 15:04:05 MST",
	"Mon 2006-01-02 15:04:05.000000 MST",
}

// ParseSystemctlShow reads the two properties the probe asks `systemctl show` for. It reports nothing
// when there is no ActiveState, which is what an unknown unit looks like: the caller then omits the
// unit rather than claiming it is down.
func ParseSystemctlShow(output []byte) (wire.UnitState, bool) {
	var (
		activeState string
		hasState    bool
		entered     string
	)

	for _, line := range strings.Split(string(output), "\n") {
		key, value, found := strings.Cut(strings.TrimSuffix(line, "\r"), "=")
		if !found {
			continue
		}
		switch key {
		case "ActiveState":
			activeState, hasState = value, true
		case "ActiveEnterTimestamp":
			entered = value
		}
	}

	if !hasState || activeState == "" {
		return wire.UnitState{}, false
	}
	return wire.UnitState{
		Active: activeState == "active",
		Since:  parseSystemctlTimestamp(entered),
	}, true
}

// parseSystemctlTimestamp turns an ActiveEnterTimestamp into an instant. A unit that has never been
// active prints an empty value or "n/a", and an unreadable value is treated the same way: the state
// is what the container needs, and a missing "since" only costs the age label.
func parseSystemctlTimestamp(value string) time.Time {
	for _, layout := range systemctlTimestampLayouts {
		parsed, err := time.ParseInLocation(layout, value, time.Local)
		if err == nil {
			return parsed
		}
	}
	return time.Time{}
}
