package probe

import (
	"time"

	"github.com/autumnsgrove/meridian/internal/config"
)

// minimumPollInterval is the shortest wait the scheduler will take. Config validation requires at
// least five seconds, so this floor only stops a broken config from turning the box into a spin loop.
const minimumPollInterval = time.Second

// The stagger that keeps a start-up burst off one second: each service is delayed a little more than
// the last, up to a cap (ARCHITECTURE.md "Polling model").
const (
	staggerStep = 150 * time.Millisecond
	staggerMax  = 2 * time.Second
)

// NextInterval is how long to wait before polling again: the active cadence while a viewer has been
// seen within poll.idle_after, the idle cadence after that (decision #35). A clock that went
// backwards counts as a viewer rather than as idleness, and the result is never zero or negative.
func NextInterval(lastTouch, now time.Time, poll config.Poll) time.Duration {
	wait := time.Duration(poll.Interval)
	if now.After(lastTouch) && now.Sub(lastTouch) > time.Duration(poll.IdleAfter) {
		wait = time.Duration(poll.IdleInterval)
	}
	if wait < minimumPollInterval {
		return minimumPollInterval
	}
	return wait
}

// Stagger is the delay before a service's first poll, by its position in the config.
func Stagger(index int) time.Duration {
	if index <= 0 {
		return 0
	}
	return min(time.Duration(index)*staggerStep, staggerMax)
}
