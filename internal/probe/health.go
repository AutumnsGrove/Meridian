package probe

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/autumnsgrove/meridian/internal/config"
	"github.com/autumnsgrove/meridian/internal/jsonpath"
	"github.com/autumnsgrove/meridian/internal/wire"
)

// MaxBodySize caps every probe response body. A health payload is a few hundred bytes, not a data
// store, and the cap is what stops a misdirected probe from reading a large file into memory.
const MaxBodySize = 1 << 20

// Status is the closed set of tile states. A probe returns only ok, down or unknown; degraded and
// disabled are decided over the whole service by the merge step.
type Status string

// The tile states.
const (
	StatusOK       Status = "ok"
	StatusDegraded Status = "degraded"
	StatusDown     Status = "down"
	StatusUnknown  Status = "unknown"
	StatusDisabled Status = "disabled"
)

// Health is one health outcome. Code is the reason code the logs carry; Stale marks an answer that
// came from an out-of-date host snapshot, which is unknown rather than green.
type Health struct {
	Status Status
	Code   string
	Stale  bool
}

// Env is everything a runner may reach besides its arguments. Each field is a seam tests replace
// with a function literal; the scheduler fills them from config at startup and on reload.
type Env struct {
	// Fetch is the HTTP seam. A nil Fetch makes http probes report unknown.
	Fetch Fetcher
	// Resolve asks one DNS question. A nil Resolve makes dns probes report unknown.
	Resolve Resolver
	// Host returns the latest host snapshot, false when there is none or it is unreadable.
	Host func() (wire.HostSnapshot, bool)
	// HostMaxAge is how old a host snapshot may be before it is stale. Zero with a snapshot
	// configured is a wiring error, and host probes then report unknown instead of green.
	HostMaxAge time.Duration
	// Now is the clock. A host probe with no clock cannot judge freshness, so it reports unknown.
	Now func() time.Time
}

// HealthRunner runs one health probe. It returns only ok, down or unknown.
type HealthRunner func(ctx context.Context, env Env, probe config.Probe) Health

// HealthRunners is every health kind. The registry test keeps it equal to config.HealthKinds, so a
// kind cannot exist in the config schema without a runner, or the other way round.
var HealthRunners = map[string]HealthRunner{
	"http_status": healthHTTPStatus,
	"http_json":   healthHTTPJSON,
	"dns":         healthDNS,
	"host_unit":   healthHostUnit,
	"host_field":  healthHostField,
	"self":        healthSelf,
}

// Errors the host snapshot seam can produce. They are part of the closed set because the page says
// the same thing for both: we do not know, and we will not guess.
var (
	// ErrHostUnavailable means there is no host snapshot to read.
	ErrHostUnavailable = errors.New("probe: no host snapshot")
	// ErrHostStale means the snapshot is older than config's host_status.max_age.
	ErrHostStale = errors.New("probe: host snapshot is out of date")
)

// errBadJSON means the body was not JSON this build can read.
var errBadJSON = errors.New("probe: body is not JSON")

// healthHTTPStatus reports ok when the status is in expect, or in expect_class (default 2xx).
func healthHTTPStatus(ctx context.Context, env Env, probe config.Probe) Health {
	if env.Fetch == nil {
		return unknownHealth(CodeBadValue)
	}
	response, err := env.Fetch(ctx, requestFor(probe))
	if err != nil {
		return healthFor(err)
	}
	if statusMatches(response.Status, probe) {
		return okHealth()
	}
	return downHealth(CodeBadStatus)
}

// healthHTTPJSON decodes the body and applies exactly one of equals, one_of or exists.
func healthHTTPJSON(ctx context.Context, env Env, probe config.Probe) Health {
	document, err := fetchJSON(ctx, env, probe)
	if err != nil {
		return healthFor(err)
	}

	value, found := jsonpath.Get(document, probe.Path)
	if !found {
		if probe.Exists != nil && !*probe.Exists {
			return okHealth()
		}
		return downHealth(CodeMissingField)
	}
	switch {
	case probe.Exists != nil:
		if *probe.Exists {
			return okHealth()
		}
		return downHealth(CodeUnexpectedValue)
	case probe.Has("equals"):
		if jsonpath.Equal(value, probe.Equals) {
			return okHealth()
		}
		return downHealth(CodeUnexpectedValue)
	case probe.Has("one_of"):
		if slices.ContainsFunc(probe.OneOf, func(candidate any) bool { return jsonpath.Equal(value, candidate) }) {
			return okHealth()
		}
		return downHealth(CodeUnexpectedValue)
	default:
		// Validation requires one of the three, so this is a probe built by something else.
		return unknownHealth(CodeBadValue)
	}
}

// healthDNS asks one question and compares the response code name with expect_rcode (default NOERROR).
func healthDNS(ctx context.Context, env Env, probe config.Probe) Health {
	if env.Resolve == nil {
		return unknownHealth(CodeBadValue)
	}
	rcode, err := env.Resolve(ctx, probe.Addr, probe.Qname)
	if err != nil {
		return healthFor(err)
	}
	expected := probe.ExpectRcode
	if expected == "" {
		expected = "NOERROR"
	}
	if strings.EqualFold(rcode, expected) {
		return okHealth()
	}
	return downHealth(CodeDNSRcode)
}

// healthHostUnit reports the unit's state from the snapshot, and never ok from a stale one.
func healthHostUnit(_ context.Context, env Env, probe config.Probe) Health {
	snapshot, err := readHost(env)
	if err != nil {
		return hostHealth(err)
	}
	unit, reported := snapshot.Units[probe.Unit]
	switch {
	case !reported:
		return unknownHealth(CodeUnitNotReported)
	case unit.Active:
		return okHealth()
	default:
		return downHealth(CodeUnitInactive)
	}
}

// healthHostField reads a dotted path from the snapshot: true is ok, false is down, and equals decides
// when the config gives one.
func healthHostField(_ context.Context, env Env, probe config.Probe) Health {
	snapshot, err := readHost(env)
	if err != nil {
		return hostHealth(err)
	}
	value, found := snapshot.Field(probe.Path)
	if !found {
		return unknownHealth(CodeMissingField)
	}
	if probe.Has("equals") {
		if jsonpath.Equal(value, probe.Equals) {
			return okHealth()
		}
		return downHealth(CodeUnexpectedValue)
	}
	state, isBoolean := value.(bool)
	switch {
	case !isBoolean:
		return unknownHealth(CodeUnexpectedType)
	case state:
		return okHealth()
	default:
		return downHealth(CodeOff)
	}
}

// healthSelf is always ok: this process is the one answering.
func healthSelf(context.Context, Env, config.Probe) Health {
	return okHealth()
}

// requestFor builds the fetch request for a probe: the configured URL and its auth block by value,
// so a runner cannot mutate the config.
func requestFor(probe config.Probe) Request {
	request := Request{URL: probe.URL}
	if probe.Auth != nil {
		request.Auth = *probe.Auth
	}
	return request
}

// fetchJSON fetches a probe URL and decodes the body, so health and vital runners share one limit and
// one set of errors.
func fetchJSON(ctx context.Context, env Env, probe config.Probe) (any, error) {
	if env.Fetch == nil {
		return nil, ErrBadRequest
	}
	response, err := env.Fetch(ctx, requestFor(probe))
	if err != nil {
		return nil, err
	}
	document, err := jsonpath.Decode(bytes.NewReader(response.Body), MaxBodySize)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errBadJSON, err)
	}
	return document, nil
}

// readHost returns the latest snapshot, or ErrHostUnavailable / ErrHostStale when it cannot be
// trusted. A clock or a max age that is not wired up reports unknown rather than green: a dead
// probe must never render as health.
func readHost(env Env) (wire.HostSnapshot, error) {
	if env.Host == nil || env.Now == nil || env.HostMaxAge <= 0 {
		return wire.HostSnapshot{}, ErrHostUnavailable
	}
	snapshot, ok := env.Host()
	if !ok {
		return wire.HostSnapshot{}, ErrHostUnavailable
	}
	if snapshot.Age(env.Now()) > env.HostMaxAge {
		return wire.HostSnapshot{}, ErrHostStale
	}
	return snapshot, nil
}

// statusMatches decides whether code is an answer the config expects: an entry of expect, or a
// status in expect_class, which defaults to 2xx. A class it cannot parse matches nothing.
func statusMatches(code int, probe config.Probe) bool {
	if len(probe.Expect) > 0 {
		return slices.Contains(probe.Expect, code)
	}
	class := strings.ToLower(probe.ExpectClass)
	if class == "" {
		class = "2xx"
	}
	if len(class) != 3 || class[1] != 'x' || class[2] != 'x' {
		return false
	}
	hundreds := int(class[0] - '0')
	return hundreds >= 1 && hundreds <= 5 && code/100 == hundreds
}

// CodeOf names the reason code for an error from a seam. An error it does not recognise is
// bad_value, never a leaked message.
func CodeOf(err error) string {
	var missing *ErrEnvMissing
	switch {
	case err == nil:
		return ""
	case errors.Is(err, ErrTimeout):
		return CodeTimeout
	case errors.Is(err, ErrRefused):
		return CodeRefused
	case errors.Is(err, ErrUnreachable):
		return CodeUnreachable
	case errors.Is(err, ErrBodyTooLarge):
		return CodeBodyTooLarge
	case errors.Is(err, errBadJSON):
		return CodeBadJSON
	case errors.Is(err, ErrHostStale):
		return CodeHostStale
	case errors.Is(err, ErrHostUnavailable):
		return CodeHostUnavailable
	case errors.As(err, &missing):
		return CodeEnvMissing
	default:
		return CodeBadValue
	}
}

// healthFor maps a seam error onto a health. A credential we could not read or a request we could
// not build is unknown: we did not ask, so we do not claim the service is down.
func healthFor(err error) Health {
	code := CodeOf(err)
	if code == CodeEnvMissing || code == CodeBadValue {
		return unknownHealth(code)
	}
	return downHealth(code)
}

// hostHealth maps a host snapshot error onto a health. A stale snapshot is unknown and marked stale
// so the page can show how old the reading is.
func hostHealth(err error) Health {
	switch {
	case errors.Is(err, ErrHostStale):
		return Health{Status: StatusUnknown, Code: CodeHostStale, Stale: true}
	case errors.Is(err, ErrHostUnavailable):
		return unknownHealth(CodeHostUnavailable)
	default:
		return unknownHealth(CodeBadValue)
	}
}

func okHealth() Health {
	return Health{Status: StatusOK}
}

func downHealth(code string) Health {
	return Health{Status: StatusDown, Code: code}
}

func unknownHealth(code string) Health {
	return Health{Status: StatusUnknown, Code: code}
}
