package hostprobe

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// ErrInvalidReading means a source answered with something that is not a reading the snapshot can
// carry. A caller distinguishes "the box has no /proc" from "the number is nonsense" only by looking.
var ErrInvalidReading = errors.New("hostprobe: invalid reading")

// maxUptimeSeconds is where a float uptime stops fitting in a time.Duration.
const maxUptimeSeconds = float64(math.MaxInt64) / float64(time.Second)

// ParseMachineReadings turns the contents of /proc/uptime ("seconds idle") and /proc/loadavg ("load
// runnable/total lastpid") into the box's uptime, truncated to whole seconds, and its one-minute load
// average. Both files are read with the same numeric rules, so a value that is negative, infinite or
// not a number is refused rather than written into the snapshot.
func ParseMachineReadings(uptime, loadavg []byte) (time.Duration, float64, error) {
	seconds, err := firstNumber(uptime)
	if err != nil {
		return 0, 0, fmt.Errorf("%w: /proc/uptime: %w", ErrInvalidReading, err)
	}
	if seconds < 0 || seconds > maxUptimeSeconds {
		return 0, 0, fmt.Errorf("%w: /proc/uptime: %v is out of range", ErrInvalidReading, seconds)
	}

	load, err := firstNumber(loadavg)
	if err != nil {
		return 0, 0, fmt.Errorf("%w: /proc/loadavg: %w", ErrInvalidReading, err)
	}
	if load < 0 {
		return 0, 0, fmt.Errorf("%w: /proc/loadavg: %v is negative", ErrInvalidReading, load)
	}

	return time.Duration(seconds) * time.Second, load, nil
}

// ReadUptime reads /proc/uptime and /proc/loadavg through read, which is os.ReadFile in production
// and a map in a test.
func ReadUptime(read func(string) ([]byte, error)) (time.Duration, float64, error) {
	uptime, err := read("/proc/uptime")
	if err != nil {
		return 0, 0, fmt.Errorf("%w: /proc/uptime: %w", ErrInvalidReading, err)
	}
	loadavg, err := read("/proc/loadavg")
	if err != nil {
		return 0, 0, fmt.Errorf("%w: /proc/loadavg: %w", ErrInvalidReading, err)
	}
	return ParseMachineReadings(uptime, loadavg)
}

// firstNumber returns the first whitespace-separated field of a /proc file as a finite number.
func firstNumber(data []byte) (float64, error) {
	fields := strings.Fields(string(data))
	if len(fields) == 0 {
		return 0, errors.New("no number")
	}
	number, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return 0, err
	}
	if math.IsNaN(number) || math.IsInf(number, 0) {
		return 0, fmt.Errorf("%q is not a finite number", fields[0])
	}
	return number, nil
}
