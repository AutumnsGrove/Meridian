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

## Last session: S7 (2026-10-25)

Finished: T18, ticked. `/control` lists every service with a control block: state pill, description,
one refused button per action with the reason on `aria-describedby`, a "Control plane not installed
on this box" note, a disabled reboot card, and `#<id>` marking and scrolling to a row once data
arrives. 154 web tests. T19 did **not** run: it starts the real binary with `meridian run` (T20), so
the owner moved it to S9 (decision #74).

Partial: none.

## Next

Session **S8**, first task **T20** (`meridian run`, signals, SIGHUP reload, healthcheck, version),
then T21 (`doctor`). Read `docs/agent/05-cli-and-deploy.md` from T20 and decision #41. `internal/cli`
is still a hand-rolled two-command switch, and cobra and `charmbracelet/log` are allowlisted but not
dependencies yet, so T20 adds the first two lines to `go.mod`.

## State of the tree

Clean and green under the full `scripts/check`. 15 commits unpushed; `origin/main` is still the S3 tip.

## Gotchas

- Both pages take their data from `createLiveStatus()` (`web/src/lib/state/live.svelte.ts`): the last
  good `PollState`, the clock its ages use, and a `start` for `onMount`. It is `.svelte.ts` because
  it needs `$state`.
- The control page's buttons are refused by `rowActions` (`web/src/lib/state/control.ts`): a button
  with a non-empty `reason` or `detail` is disabled and nothing else decides. T30 enables an action
  by clearing the reason, not by adding a flag.
- The contract has no criticality, uptime or reboot-capability field, so a row shows name, pill and
  description, and the reboot card takes its reason from `control.reason`. T25/T26 add those types.
- Hash targeting is an `$effect` keyed on the loaded status plus a `fragment` state updated by
  `hashchange`. jsdom has no `scrollIntoView`, so the page test stubs `Element.prototype`.
- Still true from S6: SvelteKit 3 config lives in `web/vite.config.ts`; `#lib` is declared twice
  (`web/package.json` and `web/tsconfig.json`); `ssr.noExternal: ['cookie']` is load-bearing for
  `pnpm build`; the build script re-creates the tracked `build/.gitkeep`; TypeScript stays on 6.x;
  Vitest needs `conditions: ['browser','node']`; goldens are read via `web/test/golden.ts`.
- `scripts/check e2e` still skips itself: there is no `web/playwright.config.ts` until S9, and the
  Playwright browsers have never been installed.

## For the owner

- 15 commits unpushed since S3, so CI has only seen the S3 tip. Push when it suits you.
- Dependabot's npm job now has a `web/` to update; its TypeScript 7 bump will break the
  `svelte-check` peer range and needs a decision, not a merge.
- Nothing has been looked at in a real browser yet. The Playwright suite in S9 is the first time.
