# Handoff

Read this first in every session. It is **rewritten, not appended**, at the end of each session; the
history is in git. Keep it under about 40 lines: it is for a cold start with an empty context.

## Template

```
## Last session: S<n> (<date>)
Finished: T## ... (ticked in TASKS.md)
Partial: none | T## and a checklist of remaining behaviours, with the files holding the finished part

## Next
Session S<n+1>, first task T##. One line on what it needs first.

## State of the tree
Clean and green under `scripts/check` | what is red and why

## Gotchas
Things the next session would otherwise rediscover: a surprising behaviour, a tool quirk, an
assumption a test depends on, a decision made in the middle of a task.

## For the owner
Anything that needs your hands (a secret, a GitHub setting, a [human] gate) or your call
(a pending item in OPEN-QUESTIONS.md).
```

---

## Last session: S4 (2026-10-25)

Finished: T12, T13 (ticked). `internal/probe` now has the result model (`state.go`: `VitalState`,
`State`, `VitalFresh`, `Merge`, `Derive`), the store (`store.go`), the scheduler (`scheduler.go`:
`Clock`, `NextInterval`, `Stagger`, `NewScheduler`, `Start`, `Reload`, `Touch`, `EnsureFresh`,
`Refresh`), the fuzz target `FuzzNextInterval`, and the stage acceptance test
`TestScheduler_StageDone`. **Stage 3's "stage done" list holds.**

Partial: none.

## Next

Session **S5**, first task **T14** (gateway skeleton), then T15. Read
`docs/agent/04-gateway-and-web.md` from T14 and decisions #54, #57, #59 first. T15 consumes what S4
built: `probe.Derive`, `probe.Store.Snapshot`, `Scheduler.Touch` then `EnsureFresh(ctx, maxAge)`.

## State of the tree

Clean and green under the full `scripts/check` (go, security; web and e2e still skip). 10 commits
unpushed; `origin/main` is the S3 tip, whose CI run passed in 5m.

## Gotchas

- `Merge`: a failing vital keeps the prior value **and** `At`; with no prior good reading it gets
  `At = now` (never a zero time, so `/api/status` has no year-0001 age). When health is not `ok`,
  vitals are untouched, including `Failing`: the growing age is the staleness marker.
- `Derive` keys "never polled" off `CheckedAt.IsZero()`; any health status other than
  ok/down/unknown derives `unknown`. It is the only place `degraded` and `disabled` arise.
- `Store`'s zero value is usable (no constructor). `Update` and `Snapshot` both deep-copy the vital
  slice; `Snapshot` is safe to hold. `state(id)` is the unexported single-entry read the poller uses.
- The scheduler fills `Env.Now` and `Env.HostMaxAge` from the clock and `host_status.max_age`, but
  **`Env.Host` (reading `host.json`) is the caller's job**, wired in T20. Leaving it nil makes every
  host probe `unknown`/`host_unavailable`, which is fail-safe but looks like a bug.
- Each cycle wraps `env.Fetch` in `Memo` **before** the health probe, so a vital on the same URL
  reuses the health response. A new memo per cycle; never across.
- Nudges carry a `poller.want` flag as well as a token: a pass asked for while another runs is taken
  next, but a nudge that the cycle about to run already answers is not repeated. Tests that count
  polls must therefore not use `EnsureFresh` to start things; `scheduler_loop_test.go` waits for the
  pollers to park and advances the stagger instead.
- The fake clock keeps abandoned timers registered, so count with `latestWake()` (the newest timer),
  not the earliest, and wait on goroutines with `eventually()` (2s real deadline, yields, no sleep).
  `waiting()` alone can be satisfied by an abandoned timer.
- `Reload` compares `Disabled` and `Probes` with `reflect.DeepEqual`, and `Probes` carries the `Set`
  map from the YAML unmarshaller: probes built in code without `Set` always look changed.
- Stagger is `index*150ms` capped at 2s; `NextInterval` floors at 1s; a whole cycle is capped at 6s
  (`cycleTimeout`) and each probe at `poll.timeout`.

## For the owner

- Nothing from S4 is pushed. `origin/main` is the S3 tip and its CI is green.
- One open Dependabot PR (#3, the actions group) passed its checks. The npm and docker update runs
  fail on purpose until `web/` (T16) and the Dockerfile (T23) exist; the go_modules one passed.
- Cosmetic, still open: `suffix: "% blocked"` in the worked example renders as `30 % blocked`,
  because `format.Compose` always joins with a space.
