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

## Last session: S6 (2026-10-25)

Finished: T16 (web scaffold) and T17 (launcher UI), both ticked. `web/` is a SvelteKit 3 SPA with
pnpm, tokens and self-hosted Lexend, ESLint/Prettier/svelte-check/Vitest, and a launcher that renders
`/api/status`: validator, poller, tiles, status marks, search, banners. 116 web tests. `scripts/check`
(full: go, fuzz, web, security; e2e still skips) is green.

Partial: none.

## Next

Session **S7**, first task **T18** (`/control` placeholder: rows, state pills, disabled actions with
their reasons, `#<id>` targeting, emergency reboot section). Read `docs/agent/04-gateway-and-web.md`
from T18, then T19. Decision #73 (a tile is one target) already shapes the `restart →` link T18's
hash targeting serves.

## State of the tree

Clean and green under the full `scripts/check`. 10 commits unpushed; `origin/main` is still the S3 tip.

## Gotchas

- **It is SvelteKit 3, not 5.** Config lives in `web/vite.config.ts` via `sveltekit({ adapter, csp })`;
  there is no `svelte.config.js`, and `$lib` is gone in favour of `#lib`. `#lib` is declared twice and
  the two must stay in sync: `web/package.json` `imports` (Vite/rolldown) and `web/tsconfig.json`
  `paths` (the type checker, which does not add extensions to subpath imports).
- `vite.config.ts` sets `ssr.noExternal: ['cookie']`: Kit externalises its own dependencies and
  adapter-static imports the SSR output once to analyse routes, while pnpm does not link Kit's deps
  into the app. Removing it breaks `pnpm build` (locally a stray `/Users/autumn/node_modules/cookie`
  made it fail loudly instead).
- `pnpm build` empties `web/build`, so the build script re-creates the tracked `.gitkeep`
  (`vite build && touch build/.gitkeep`). Keep that, or every build leaves a deleted file behind.
- TypeScript is pinned to 6.x because `svelte-check` peers `^5 || ^6` and the registry's latest is 7.
  A Dependabot bump to 7 will break the peer range; it needs a decision, not a merge.
- Vitest needs `resolve.conditions: ['browser', 'node']` in `web/vitest.config.ts`: without `browser`
  Svelte resolves its server build and `mount()` throws, without `node` Vite cannot resolve
  `node:module`. Component tests pass props directly: `render(Tile, { service, now })`.
- Golden files are read through `web/test/golden.ts` from `process.cwd()/../testdata/api`
  (`import.meta.url` is not a file URL under jsdom). Its `serviceNamed` helper is how every component
  test gets the portable fixture, so no component test names a real service.
- `svelte/no-navigation-without-resolve` is off in `web/eslint.config.js`: the gateway serves the SPA
  at `/` with no base path. The same block declares `window`/`document`, because eslint-plugin-svelte's
  config no longer brings browser globals.
- Nothing has been looked at in a real browser yet: T19's Playwright suite is the first time. The
  launcher's only buttons are the search engines; keep it that way (T18 adds disabled ones).

## For the owner

- Still nothing pushed since S3: 10 commits, CI green on the S3 tip only.
- Dependabot's npm job now has a `web/` to update; its first run will try the TypeScript 7 bump above.
- `pnpm e2e` does not work yet (no Playwright config until T19); browsers will need
  `pnpm exec playwright install` once. `scripts/check e2e` skips itself until then.
