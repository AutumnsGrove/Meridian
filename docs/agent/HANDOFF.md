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

## Last session: S8 (2026-10-25)

Finished: T20 and T21, both ticked. `internal/cli` is cobra: `run` serves with SIGHUP and `--watch`
reloads, `healthcheck` asks the address the config names, `version` names the platform, and `doctor`
explains a config, its probes and the allowlist with one code and one hint per check. The probe logs
a line per status change, and the tree is green including the new read-only invariant on `doctor`.

Partial: none.

## Next

Session **S9**, first task **T19** (Playwright against the real binary), now unblocked because
`meridian run` exists (decision #74). Read `docs/agent/04-gateway-and-web.md` from T19 and
`docs/DESIGN.md`. T19 needs `testdata/stub/main.go`, a `web/playwright.config.ts` with two viewports,
and a globalSetup that starts the stub and `meridian run` on free ports.

## State of the tree

Clean and green under the full `scripts/check`. 25 commits unpushed; `origin/main` is the S3 tip.

## Gotchas

- `meridian run --config PATH` binds the `listen` in that config with no override, so the e2e setup
  must pick free ports and write them into a copied config (the portable fixture listens on 8891).
- The portable fixture's `backup` and `metrics` services probe `host_status.file`, so a stub run also
  needs a `host.json` in a temp directory or those tiles stay `unknown`.
- `cli.Main(args, stdin, stdout, stderr)` now takes a stdin reader: `doctor --allowlist -` reads it.
- `internal/doctor` is read only by rule (no writer call, no `control` or `atomicfile` import). A
  check's `Code` is the closed vocabulary the page and log share, every Warn/Fail carries a `Hint`,
  and `Render` is pinned by `testdata/doctor/report.golden.txt` (`go test ./internal/doctor -update`).
  Doctor exits 1 on any Fail and prints no second sentence.
- `probe.TransitionLine` writes `id: ok -> down (code)` once per change; `scheduler_log_test.go` pins it.
- CI's e2e job already installs Playwright browsers and skips until `web/playwright.config.ts`
  exists, so S9 only has to add the config to turn it on.
- Still true from S6/S7: SvelteKit 3 keeps its config in `web/vite.config.ts`; `#lib` is declared in
  `web/package.json` and `web/tsconfig.json`; `ssr.noExternal: ['cookie']` is load-bearing; the build
  re-creates `build/.gitkeep`; TypeScript stays 6.x; Vitest needs `conditions: ['browser','node']`;
  goldens are read via `web/test/golden.ts`.

## For the owner

- 25 commits unpushed since S3, so CI has only seen the S3 tip.
- Dependabot's npm job will offer TypeScript 7, which breaks the `svelte-check` peer range: it needs a
  decision, not a merge.
- Nothing has been seen in a real browser yet; S9 is the first time.
