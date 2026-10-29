> Historical: the plan this project was built from. Not maintained.

# Stage 3: Probing

Goal: turn config into live, honest state. This stage is almost entirely pure functions over data
with a handful of named I/O seams, which makes it the best place to practise test-first and fuzzing.
Read [ARCHITECTURE.md](../../ARCHITECTURE.md) "Polling model" and [INTEGRATIONS.md](../../INTEGRATIONS.md)
(especially the Ivy `Host` section) first. Decisions #11, #26, #27, #35, #40.

## Shared vocabulary

```go
package probe

type Status string // exactly: ok, degraded, down, unknown, disabled

// Seams: plain function values, passed in a struct, faked in tests in a few lines.
type Env struct {
    Fetch   Fetcher                 // HTTP GET with limits (T09)
    Resolve Resolver                // one DNS question (T10)
    Host    func() (wire.HostSnapshot, bool) // latest snapshot, false if none or unreadable
    Getenv  func(string) string
    Now     func() time.Time
}

type HealthRunner func(ctx context.Context, env Env, p config.Probe) Health
type VitalRunner  func(ctx context.Context, env Env, p config.Probe) (string, error)

type Health struct { Status Status; Code string; Stale bool }
```

`Code` comes from one closed table in `probe/reasons.go` mapping a code to the short sentence shown
to the user: `timeout`, `refused`, `unreachable`, `bad_status`, `bad_json`, `missing_field`,
`unexpected_value`, `unexpected_type`, `body_too_large`, `env_missing`, `host_unavailable`,
`host_stale`, `unit_inactive`, `unit_not_reported`, `dns_rcode`, `off`, `bad_value`. The UI shows the sentence; logs show
the code. Errors never carry response bodies or URLs with secrets (STANDARDS section 4).

## T08 `jsonpath` and `format`

**`internal/jsonpath`** (stdlib only)
- `Decode(r io.Reader, limit int64) (any, error)`: decode with `UseNumber`, reject trailing data,
  honour the byte limit.
- `Get(doc any, path string) (any, bool)`: dotted path; each segment is an object key or, on arrays,
  a non-negative index (`items.0.name`). Empty path returns `doc`. Missing, wrong-type or out-of-range
  gives `(nil, false)`, never a panic. Cap path length (256) and segment count (16).
- `Number(v any) (float64, bool)`: accepts `json.Number` and the Go numeric types the YAML decoder
  produces; rejects NaN and Inf.
- `Equal(got, want any) bool`: type-aware. Numbers compare numerically, strings to strings, bools
  to bools. **No coercion** between types (`"true"` is not `true`, `"1"` is not `1`).
- `Elements(doc any, path string) ([]any, bool)`: the array at `path`.

**`internal/format`** (stdlib only, all pure)
- `Number(f float64, decimals int) string`: fixed decimals, thousands separator off, `-0` printed as
  `0`; decimals clamped 0..6.
- `Bytes(n float64, unit string, decimals int) string`: decimal units (KB=1000). Empty `unit` picks
  the largest unit giving a value of at least 1; a named unit forces it. Negative or non-finite
  input is the caller's error (return `("", false)` through a second result, not a panic).
- `Age(d time.Duration) string`: `12s`, `2m`, `3h`, `4d`; rounds down; negative durations print `0s`.
- `Compose(prefix, value, suffix string) string`.

**Tests first**: table tests including numbers at the edges (0, -0, 1e21, 0.5, 999.5 with 0
decimals), arrays and indices, keys containing digits, deeply nested paths, huge arrays.

**Fuzz**: `FuzzGet` (random JSON + random path: no panic, result type is one JSON can produce),
`FuzzEqual` (symmetric, reflexive for non-NaN), `FuzzNumber`, `FuzzBytes` (output parses back to a
value within rounding of the input).

## T09 Fetch client

`Fetcher` is `func(ctx context.Context, r Request) (Response, error)`; `Request{URL string; Auth
config.Auth}`; `Response{Status int; Body []byte}`. The real implementation is `NewHTTPFetcher(getenv
func(string) string, userAgent string, limit int64) Fetcher`.

- One shared `http.Client` per fetcher: **no redirect following** (`CheckRedirect` returns
  `http.ErrUseLastResponse`; a 302 to a login page is a valid healthy answer for `http_status`), no
  cookie jar, `DisableKeepAlives` false but idle conns bounded, no proxy from the environment
  (`Proxy: nil`: the container is on the host network and must not tunnel probes), the deadline is the
  context's.
- `Host` header is the URL's host (the Ivy `localhost` rule: the config URL is used verbatim, never
  rewritten).
- Body read through `io.LimitReader(limit+1)`; more than `limit` bytes is `ErrBodyTooLarge`.
- Auth is applied from `Getenv` at request time. Missing variable gives `ErrEnvMissing{Names []string}`
  (names, never values) and **no request is sent**. Credentials only ride on the request for the
  configured URL.
- Errors map to the closed set: `ErrTimeout`, `ErrRefused`, `ErrBodyTooLarge`, `ErrEnvMissing`, other
  network failures `ErrUnreachable`. Use `errors.Is`-friendly sentinels and `%w`.
- `Memo(f Fetcher) Fetcher`: a per-cycle wrapper that fetches the same `(URL, auth)` once, so a
  health probe and two vitals on the same endpoint cost one request. A new `Memo` per refresh cycle;
  never shared across cycles.

**Tests first** (all with `httptest.Server`): redirect is returned not followed; body over limit;
timeout via context; basic and bearer auth headers arrive; a `Host` check server (copy Ivy: 403
unless `Host: localhost:<port>`) is happy when the configured URL says `localhost`; a server that
redirects to a second server never receives the credentials; `Memo` hits the server once for
duplicate requests and twice for different auth.

**Fuzz**: `FuzzBuildRequest` (random URL strings and header values: either a clean error or a request
whose URL equals the input; never panics, never emits a header containing CR/LF).

## T10 Health kinds

Implement `HealthRunners = map[string]HealthRunner` in `probe/health.go`, one small function per
kind, plus `probe/registry_test.go` asserting its keys equal `config.HealthKinds` exactly.

| Kind | Result |
|---|---|
| `http_status` | `ok` if the status is in `expect` or in `expect_class` (default class `2xx`); else `down` / `bad_status` |
| `http_json` | decode body (limit), `jsonpath.Get`, then exactly one of `equals` (type-aware), `one_of`, `exists`; `down` with `bad_json` / `missing_field` / `unexpected_value` |
| `dns` | ask `addr` for an A record of `qname`, compare the response code name to `expect_rcode` (default `NOERROR`); `down` / `dns_rcode`, `timeout`, `refused` |
| `host_unit` | from the snapshot: `active` gives `ok`, inactive `down` / `unit_inactive`, unit absent `unknown` / `unit_not_reported`; **snapshot missing or older than `max_age` gives `unknown` with `Stale: true`**, never `ok` |
| `host_field` | value at `path` from the snapshot; boolean `true` is `ok`, `false` is `down`; with `equals`, `Equal` decides; stale or missing snapshot as above |
| `self` | always `ok`; it is the process answering |

Health runners return only `ok`, `down` or `unknown`. `degraded` is derived (T12), never returned
by a probe. A network failure, a timeout or a 5xx is `down`; a missing environment variable for auth
is `unknown` / `env_missing` (we could not ask, so we do not claim the service is down).

**DNS resolver** (`probe/dns.go`): `Resolver func(ctx, addr, qname string) (rcode string, err error)`.
Build the query with `golang.org/x/net/dns/dnsmessage`, UDP, random 16-bit id from `crypto/rand`,
verify the response id and question match, deadline from the context, ignore the truncation bit
(an answer is an answer for a health check). Tests run a tiny in-process UDP DNS server on
`127.0.0.1:0`. A mismatched id, a short packet and garbage are `ErrUnreachable`, never a panic.

**Tests first**: each kind has an ok case, each of its down codes, and its unknown case; the Ivy
`Host` scenario; stale `host.json` never produces `ok`; the 302-is-healthy AdGuard scenario.

**Fuzz**: `FuzzHTTPJSON` (random body bytes through the `http_json` evaluation with a fixed probe:
no panic, status in the closed set), `FuzzDNSResponse` (random bytes as the server reply).

## T11 Vital kinds

`VitalRunners = map[string]VitalRunner` in `probe/vital.go`, registry-tested the same way. A vital
returns the already-formatted value (the UI never formats), or an error from the closed set.

| Kind | Value |
|---|---|
| `json_field` | value at `path`: numbers via `format.Number(..., decimals)`, strings verbatim, booleans `yes`/`no`; objects and arrays are `unexpected_type`; then `prefix` + value + `suffix` |
| `json_count` | number of elements of `where` whose `field` `Equal`s `equals`; with neither `field` nor `equals`, the length of the array; `suffix` appended |
| `json_sum` | sum of the numeric `fields` over the elements of `where` (non-numeric element fields are skipped, but if **nothing** numeric was found it is `missing_field`); `scale: bytes` formats with `format.Bytes(sum, unit, decimals)`, `none` with `Number` |
| `json_ratio_pct` | `numerator / denominator * 100`; denominator `0` with numerator `0` gives `0`, otherwise `bad_value`; then `suffix` |
| `host_field` | value at `path` from the snapshot, formatted like `json_field`; stale or missing snapshot is `host_stale` / `host_unavailable` |

All five reuse `jsonpath` and `format`; if you find yourself writing a path walker or a rounding
rule here, stop and extend those packages instead.

**Tests first**: build the expected results from the shapes in INTEGRATIONS.md (an array with a
`needs` flag, an array of daily byte counters, a DNS stats object). Include: `0` of `0`, huge
numbers, negative numbers, strings where numbers are expected, missing `where`, `where` not an
array.

**Fuzz**: `FuzzVitals` (random JSON through each vital kind with fixed valid probes: no panic, output
length bounded, never contains the secret env value used in a fixture).

## T12 Result model, merge, store

All pure except the store's mutex.

```go
type VitalState struct { Label, Value string; At time.Time; Failing bool; Code string }
type State struct {
    Health    Health
    CheckedAt time.Time
    Vitals    []VitalState
}
func Merge(prev State, health Health, vitals []VitalFresh, now time.Time) State
func Derive(s State, enabled bool) Status   // the one place degraded is decided
```

Rules (each is a test):
1. Status is `disabled` if the service is configured off (`Service.Disabled` from config; it is the only way
   `disabled` arises), `unknown` before the first poll.
2. If health is `ok` and **any** vital is failing, status is `degraded` (decision #40's rationale:
   a missing vital must not make the page lie about availability).
3. A failing vital keeps its **last good value and timestamp** and sets `Failing`; the UI renders it
   dimmed with its age (stale-while-error). With no prior value, `Value` is empty, `Failing` is true
   and `At` is the time of the failed check, so the wire never carries a zero timestamp.
4. When health is not `ok`, vitals are **not fetched** that cycle; their last values stay exactly
   as they were, and the growing age of `at` is what marks them stale. `failing` is left alone: we
   did not ask, so we do not claim the vital failed, and the server adds no per-vital stale flag to
   the contract because the age already says it.
5. `Merge` never mutates its arguments and never produces `ok` from a `down` health.
6. Vitals are matched to previous values by `Label` **and** position so a reload that renames a label
   starts fresh.

`Store`: `Update(id string, s State)`, `Snapshot() map[string]State` (a copy, safe to hold),
`Retain(ids []string)` drops removed services on reload. RWMutex inside, no exported fields.

**Fuzz**: `FuzzMerge` over arbitrary sequences of (health, vital outcomes): invariants above hold;
a `down` health never derives `ok` or `degraded`.

## T13 Scheduler

Pure parts first (tested exhaustively, fuzzed):
- `NextInterval(lastTouch, now time.Time, p config.Poll) time.Duration`: `p.Interval` if
  `now - lastTouch <= p.IdleAfter`, else `p.IdleInterval`. Never zero, never negative, clock going
  backwards handled.
- `Stagger(index int) time.Duration`: `index * 150ms`, capped at 2s.

The loop:
```go
type Clock struct { Now func() time.Time; After func(time.Duration) <-chan time.Time }
func NewScheduler(env Env, store *Store, clock Clock, log *log.Logger) *Scheduler
func (s *Scheduler) Start(ctx context.Context, cfg *config.Config)
func (s *Scheduler) Reload(cfg *config.Config)   // swap the probe set, keep unchanged states
func (s *Scheduler) Touch()                       // a viewer exists right now
func (s *Scheduler) EnsureFresh(ctx context.Context, maxAge time.Duration) // see below
func (s *Scheduler) Refresh(ctx context.Context) (started bool) // manual; false inside refresh_min
```
- One goroutine per service, started with `Stagger(i)`, each: run health (with `Memo`), run vitals if
  health ok, `Merge`, `store.Update`, then wait `NextInterval` or a nudge. Per-probe timeout
  `poll.timeout`; the whole cycle is bounded at 6 seconds (decision: slow is reported as a
  timeout, not awaited).
- `Touch` records the viewer and, when the scheduler had been idling, nudges every goroutine: coming
  back to the page ends the idle cadence at once. `EnsureFresh` is what the gateway calls on a page
  load: it nudges the services whose result is older than `maxAge` and waits for them to finish or
  `ctx` to expire (6s), so **a first load after an idle period never shows a tile older than the load
  itself** (decision #35). A pass asked for while another one runs is taken next, so a nudge is never
  lost.
- `Reload` diffs by service id and probe config: unchanged services keep their goroutine and state;
  changed or new ones restart; removed ones stop and `Retain` drops them. No goroutine leaks:
  every goroutine ends when its context does.
- Goroutine-per-service panics are recovered and logged as `down` / `bad_value`; one broken probe must
  never take the page down.

**Tests first** with a fake `Clock` (a channel the test advances): staggering order; backoff after
`idle_after`; `Touch` returns to `interval`; `EnsureFresh` returns promptly when everything is fresh
and waits when it is not; `Refresh` rate limit; reload keeps state for unchanged services and drops
removed; no goroutines remain after cancel (`goleak`-free: count with `runtime.NumGoroutine` after a
settle loop driven by the fake clock, not by sleeping).

**Stage done**: against `httptest` servers built from the portability fixture, a scheduler run yields
the expected status for an up, a down, a slow and a flapping service, and a `host_unit` service with
a stale snapshot reads `unknown`.
