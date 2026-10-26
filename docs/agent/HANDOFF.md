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

## Last session: S4 and S5 (2026-10-25)

Finished: T12, T13, T14, T15 (all ticked). `internal/probe` gained the result model, the store and
the scheduler (stage 3's "stage done" test holds). `internal/gateway` now serves the launcher: the
route table, the middleware chain, `/healthz`, static files, icons, and `/api/status` with eight
golden files in `testdata/api/`.

Partial: none. The web app itself does not exist yet, by design (T16).

## Next

Session **S6**, first task **T16** (SvelteKit scaffold, tokens, fonts, test tooling, embed wiring),
then T17. Read `docs/agent/04-gateway-and-web.md` from T16 and decisions #58, #59. `pnpm` is required
and `web/` currently holds only `embed.go` and a placeholder `build/.gitkeep`.

## State of the tree

Clean and green under the full `scripts/check` (go, security; web, e2e and image still skip because
`web/package.json` does not exist). 18 commits unpushed; `origin/main` is the S3 tip.

## Gotchas

- `gateway.Deps.Current` is a **func**, not a pointer: it returns the config being served and a
  `LoadStatus`. It is called once per request, so T20's SIGHUP reload only has to swap what it
  returns. The host allowlist is read per request through the same seam; `NewServer`'s `Addr` is not,
  so a `listen` change still needs a restart.
- `Deps` also carries `Store`, `Scheduler`, `Host func() (wire.HostSnapshot, bool)`, `Now`,
  `ControlAvailable` and `ControlReason`. T20 must wire `Host` from the same reader it gives
  `probe.Env.Host`; leaving it nil just means the `host` block is null.
- The contract's small print, now in `04-gateway-and-web.md`: disabled services are outside
  `summary.total`, degraded counts as up, `checked_at` is the zero time before a first poll, `host`
  is null when nothing has written a snapshot, and a degraded tile's `reason` is its failing vital's
  sentence.
- Golden files live in `testdata/api/status.*.golden.json` (repo root, not the package) and are only
  regenerated with `go test ./internal/gateway -update`. T17's TypeScript client tests must read the
  same files, so a contract change is a golden diff plus both sides' tests.
- `BrowserGuard` wraps every non-GET request but no POST route exists yet, so it has no live route
  until T26. It refuses a declared body over 8 KiB with 413 before the handler runs.
- `RecoverPanic` logs the panic value **and** the stack, and answers 500 with no detail. Only the
  owner can read the log, and a panic without its value is undebuggable.
- Icons: the config value is `icons/<file>` and the contract emits `/icons/<file>`. The route opens
  `<config dir>/icons` as an `os.Root`, so a symlink out of it is a 404.
- `web/build` holds only `.gitkeep`, so the SPA fallback currently ends in a 404 from
  `http.ServeFileFS`; every gateway test passes its own `fstest.MapFS` instead.
- `internal/gateway/testdata/fuzz/FuzzHostCheck` has five committed crashers. They pin the accepted
  host grammar: invalid UTF-8 is refused (lower casing would otherwise fold two different malformed
  names together), IPv6 must be bracketed, and a stray bracket is not a host.

## For the owner

- Nothing from S4 or S5 is pushed. `origin/main` is the S3 tip and its CI is green.
- Dependabot PR #3 (the actions group) sits open and passing. The npm and docker update runs keep
  failing until `web/` (T16) and the Dockerfile (T23) exist.
- Cosmetic, still open: `suffix: "% blocked"` in the worked example renders as `30 % blocked`,
  because `format.Compose` always joins with a space.
