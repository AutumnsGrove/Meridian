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

## Last session: S9 (2026-10-26)

Finished: T19, ticked. `web/playwright.config.ts` runs the suite against `dist/meridian`: `globalSetup`
picks two free ports, writes a copy of the portable fixture that names them, writes and refreshes the
`host.json` the host-side probe would write, builds and starts `testdata/stub` (one listener, a flip
endpoint per service), starts the binary, and warms the first poll. Two projects (390x844, 1440x900),
one worker. 28 tests: both sections, 44px targets, sideways scroll at 390 and at 200% zoom, search
buttons and Enter, tab order with focus rings, axe on `/` and `/control`, a stub service going down
into a `restart →` link and a highlighted row, a stale vital dimmed by moving the browser clock, and
the page's global checks (no console noise, no off-origin request, security headers, CSP).

Partial: none.

## Next

Session **S10**, first task **T22** (`meridian-host probe` and the `host.json` timer units). Read
`docs/agent/05-cli-and-deploy.md` from T22 and `docs/CONTROL.md`. The `wire` snapshot type, its strict
parse and the `host.json` shape are already done; what is missing is the stdlib-only command that
writes that file and the systemd units around it.

## State of the tree

Clean and green under the full `scripts/check`. 41 commits unpushed; `origin/main` is the S3 tip, so
CI has still never run the e2e job.

## Gotchas

- The first real browser run paid for itself: it found a CSP violation on every load (SvelteKit's
  route announcer has a hardcoded `style` attribute, so `style-src` now allows inline and
  `script-src` stays hashed, #77), no `h1`, and two axe rules. The calls-shape changes are decisions
  #75-#78; read them before touching tiles, CSP or the search fixture.
- A tile's accessible name is its **content** now (status word and vital labels are visually hidden
  text). There is no `tileLabel` and no `aria-label`; do not add one back without re-running axe.
- The e2e's fixture copy is made by `replaceAll` on four exact strings in
  `testdata/portable/config.yaml`. Editing that fixture means editing `global-setup.ts`; the setup
  throws if a string it needs is gone. The metrics tile's health is `tailscale.online` because a
  `host_field` health probe without `equals` reads a boolean (S9 fixed a fixture that could never be
  healthy).
- Playwright state lives at `$TMPDIR/meridian-e2e/state.json`; globalSetup builds the stub with `go
  build` and expects `dist/meridian` (which `scripts/check e2e` builds). A local machine needs
  `cd web && pnpm exec playwright install chromium` once.
- The flip test waits a real poll cycle (~20s, 31s wall): do not shorten it into a reload. `workers:
  1` is deliberate, because one binary and one stub are shared.
- `@types/node` is declared now; `svelte-check` had been passing off a stray copy above the repo.
- Still true: SvelteKit 3 configures itself from `vite.config.ts`; `#lib` is declared in
  `web/package.json` and `web/tsconfig.json`; `ssr.noExternal: ['cookie']` is load-bearing; the build
  re-creates `build/.gitkeep`; TypeScript stays 6.x; Vitest needs `conditions: ['browser','node']`;
  goldens are read via `web/test/golden.ts`; `cli.Main` takes a stdin reader for `doctor --allowlist -`.
- The full fuzz pass found a real bug in T14's host guard while S9 closed (`[hub.example.org]`
  matched the allowlist entry). If a fuzz job goes red, commit the crasher under
  `internal/gateway/testdata/fuzz/` and fix it; do not re-run until green.

## For the owner

- 41 commits unpushed since S3. The next push is the first CI run that builds the web app, runs the
  Playwright job and runs the e2e setup in the image-less runner; worth watching rather than walking
  away.
- Dependabot's npm job will offer TypeScript 7, which breaks the `svelte-check` peer range: it needs a
  decision, not a merge.
- Nothing has been opened on the potato yet; the `[human]` gates (T24, T32, T33, T37) are still open
  and unblocked.
