# Stage 4: Gateway and web

Goal: a launcher in a browser, served by the real binary, that renders whatever the config says and
nothing the code knows. Read [ARCHITECTURE.md](../ARCHITECTURE.md), [DESIGN.md](../DESIGN.md) and
open the files in `mockups/` at 390px wide; the mockups are the visual specification. Decisions #4,
#6, #10, #34, #43, #54, #59.

## The contract between Go and the browser

`GET /api/status` returns this shape (names are fixed here; types in Go and TypeScript mirror it):

```json
{
  "generated_at": "2026-10-23T18:04:11Z",
  "config": {"ok": true, "loaded_at": "2026-10-23T17:00:00Z", "error": ""},
  "summary": {"up": 6, "total": 7},
  "search": {"buttons": [{"label": "Search", "url_template": "https://…/search?q={q}"}]},
  "control": {"available": false, "reason": "control plane not installed"},
  "host": {"at": "2026-10-23T18:03:50Z", "age_s": 21, "stale": false},
  "sections": [{
    "id": "everyday", "title": "Everyday",
    "services": [{
      "id": "ivy", "name": "Ivy", "description": "mail",
      "icon": {"lucide": "mail"},
      "url": "http://…",
      "status": "ok", "reason": "", "checked_at": "2026-10-23T18:04:09Z",
      "vitals": [{"label": "needs you", "value": "4", "at": "2026-10-23T18:04:09Z",
                  "age_s": 2, "failing": false}],
      "control": {"target": "ivy", "actions": ["start","stop","restart","update"],
                  "update_disabled_reason": "", "dns": false}
    }]
  }]
}
```

- `icon` is `{"lucide": name}` or `{"image": "/icons/<file>"}`.
- `status` is exactly the five-value vocabulary. `reason` is the user sentence for the reason code,
  empty when `ok`.
- Everything the UI shows is already formatted by Go. The browser computes only ages from `at` and
  the local clock, and substitutes `{q}` into search templates.
- `host` is `null` when `host_status` is not configured, and also when it is configured but no
  snapshot has been written yet: both mean "there is no host reading to show".
- `checked_at` is the zero time before a service's first poll. The page shows nothing for an
  `unknown` tile, so it never renders that as an age.
- A `degraded` tile's `reason` is the sentence for the vital that failed; a `down` or `unknown` one
  uses its health code; `ok` and `disabled` carry none.
- `control` is `null` on a read-only tile. The top-level `control.available` says whether the
  control plane exists on this box at all (false throughout Phase 1).
- Empty sections are **omitted** by the server (DESIGN: no heading over blank space).
- The shape is pinned by golden files: `testdata/api/status.*.golden.json`, read by both Go tests and
  the TypeScript client tests. A change to the contract is a change to the golden file in the same
  commit.

## T14 Gateway skeleton

Package `internal/gateway`.

- **Route table as data.** `func Routes(d Deps) []Route` where `Route{Pattern string; Handler
  http.Handler}`. `Serve` builds the mux from it. The invariants test (T03) reads the table; the only
  non-`GET` pattern permitted is `POST /api/control` (added in T26). Patterns use Go 1.22+ syntax.
- **Middleware chain** (small composable functions, each with its own test, order fixed):
  `recoverPanic` (500 without detail), `securityHeaders`, `hostCheck`, `accessLog` (method, path,
  status, duration; never query strings or bodies), then for non-`GET` only `browserGuard`.
- `hostCheck`: request `Host` must be a loopback name (`localhost`, `127.0.0.1`, `[::1]`, any port) or
  match `server.allowed_hosts` (an entry without a port matches any port; with a port, exactly).
  Otherwise `421 Misdirected Request`. This is the DNS-rebinding defence (decision #54). Note that
  `/healthz` called by the container healthcheck uses a loopback host, so it passes.
- `browserGuard` (mutating routes only): `Content-Type` must be exactly `application/json`;
  `Sec-Fetch-Site` if present must be `same-origin` or `none`; `Origin` if present must have a host
  accepted by `hostCheck`; body capped at 8 KiB (`http.MaxBytesReader`, and a declared body over
  the cap is refused with `413` up front). Anything else is `403` with a JSON error. This is **not
  authentication**: no token, no cookie, no session, and it must not
  grow into one (decision #10).
- `GET /healthz`: `{"status":"ok","version":"…"}` 200 whenever the process can serve. No probing, no
  config reload state; cheap enough for a Docker healthcheck every few seconds.
- Static files: serve `web.FS()`. Unknown paths that do not start with `/api/` or `/icons/` return
  `index.html` with 200 (SPA routes like `/control`); unknown `/api/...` is a JSON 404.
  `_app/immutable/*` gets `Cache-Control: public, max-age=31536000, immutable`; `index.html`
  `no-cache`.
- Icons: `GET /icons/{file}` serves from `<config dir>/icons/` via `os.Root`. Name must match
  `^[A-Za-z0-9._-]+\.(png|webp|jpg|jpeg|gif)$`; anything else is 404. `nosniff`, `max-age=3600`. No
  directory listing, no symlink escape.
- Server construction: `ReadHeaderTimeout 5s`, `ReadTimeout 10s`, `WriteTimeout 15s` (the status
  endpoint may wait up to 6s), `IdleTimeout 60s`, `MaxHeaderBytes 16 KiB`. Graceful shutdown on
  context cancel with a 5s drain.

**Tests first** (all through `httptest` and the real handler chain): wrong `Host` is 421; each
browser-guard rule has a rejecting and an accepting case; headers present on every response including
errors and 404s; SPA fallback versus API 404; icon traversal attempts (`..%2f`, encoded slashes, a
symlink pointing outside) are all 404; panic in a handler gives 500 and the server keeps serving.

**Fuzz**: `FuzzHostCheck` (random Host headers against random allowlists: loopback always allowed,
nothing outside the allowlist ever allowed), `FuzzIconName` (accepted names contain no separator and
no `..`).

## T15 `/api/status`

- `BuildStatus(cfg *config.Config, states map[string]probe.State, host *wire.HostSnapshot, now
  time.Time, in StatusInputs) StatusResponse` in `internal/gateway/status.go`: **pure**. Walks
  `cfg.Sections` in order, services in config order within each section, uses `probe.Derive` and the
  reason table, computes `age_s` and the `summary` (`up` counts `ok` and `degraded`; `total`
  excludes `disabled`).
- Handler: `Touch()` the scheduler, call `EnsureFresh` bounded by 6s, then respond from the store.
  `GET /api/status?fresh=1` triggers `Refresh` (rate limited to `refresh_min`; inside the window it
  just serves the cache, `generated_at` shows how old). It stays a `GET` so the one-mutating-route
  rule holds.
- If a reload was rejected the response carries `config.ok = false` and a short `error` (the first
  three problems as `path: code`). The page shows a banner; the old config keeps serving.
- JSON is encoded from the response structs only; fields are never built from raw probe data.

**Tests first**: golden files for the portable fixture config in states: all ok; one down; one
degraded with a stale vital; unknown-before-first-poll; a disabled service; an empty section omitted;
host snapshot stale; config rejected. Handler test: first request after idle waits for a fresh pass;
`?fresh=1` twice in a row refreshes once.

**Fuzz**: `FuzzBuildStatus` over generated states (random statuses, nil/absent entries, zero times):
never panics, output always marshals, every `status` value is in the closed set, section order equals
config order.

## T16 Web scaffold

`web/` with SvelteKit 3 (Svelte 5 runes), TypeScript strict, pnpm. SvelteKit 3 takes its whole
config from the Vite plugin: there is no `svelte.config.js`, `tsconfig.json` extends `$app/tsconfig`,
and `$lib` is replaced by the `#lib` subpath import declared in `package.json`.

- `vite.config.ts`: `sveltekit({ adapter: adapter({ fallback: 'index.html' }), csp })` with
  `adapter-static`, the CSP in hash mode, output to `web/build`, and the dev proxy for `/api`,
  `/icons`, `/healthz` to `127.0.0.1:8891`. `src/routes/+layout.ts`: `ssr = false; prerender = false`.
  It is a SPA served by Go.
- `src/lib/styles/tokens.css`: the tokens from DESIGN.md "Tokens", copied verbatim, plus reset, focus
  ring, `color-scheme: dark`, reduced-motion rules, the 44px touch-target utility. Self-hosted Lexend
  via `@fontsource-variable/lexend`. No external requests of any kind.
- `package.json` scripts: `dev build check lint format format:check test test:watch e2e`.
- ESLint + Prettier configs with the Svelte plugins. `pnpm-lock.yaml` committed.
- `web/embed.go` already exists (T01); confirm `go build` embeds a real `web/build`.

**Tests**: one Vitest smoke test, and `pnpm build` produces `build/index.html`.

## T17 Launcher UI

Files (pure `.ts` first, components thin):

```
src/lib/api/types.ts            types mirroring the contract
src/lib/api/parse.ts            parseStatus(unknown): Status | Error  (hand-written validator)
src/lib/api/client.ts           fetchStatus(), via fetch, same-origin only
src/lib/format/age.ts           ageLabel(at, now) -> "2m"
src/lib/format/search.ts        buildSearchURL(template, q)
src/lib/state/poller.ts         poll loop as a reducer + effect: visible-only, backoff on error
src/lib/state/status.ts         selectors: summary text, sections, stale-vital rules
src/lib/components/             StatusMark, Tile, Section, SearchBox, Banner
src/routes/+page.svelte         the launcher
```

Behaviour decided here (see DESIGN "Launcher" and "Search"):
- The whole tile is one link to `service.url` (no nested interactive targets), same tab, with
  `rel="noopener noreferrer"`. A service with no `url` renders as a non-link tile.
- `StatusMark` renders the shape and a visually hidden label for each of the five statuses; the tile
  `aria-label` says it all ("Ivy, healthy, 4 need you").
- A vital older than 5 minutes renders `--color-text-faint` with its age; `failing` vitals dim and show
  `· 2m ago`. The server already makes the dot `degraded`; the UI does not recompute status.
- A `down` service whose `control.actions` includes `restart` grows a single `restart →` link to
  `/control#<id>`. It is a link, never a button, and never triggers anything.
- Search: one input and two (or N) real buttons from `search.buttons`, always visible. Empty input
  disables all of them. Enter submits the first. `buildSearchURL` URL-encodes the query and replaces
  the single `{q}`; it opens the result in the same tab.
- Poller: fetch on mount, then every `15s` while the page is visible; on `visibilitychange` to visible
  fetch immediately; on failure keep the last good data, show a banner ("Can't reach Meridian,
  showing data from 2m ago") and back off to 60s; recover automatically. Never more than one request
  in flight.
- Summary line (`6/7 up`) comes from `summary`; "Control panel" link at the bottom goes to `/control`.
- The UI has no idea what a "kind" is and no service names. Component tests use the portable fixture.

**Tests first**: `parseStatus` accepts every golden file and rejects mutations of them (missing
fields, unknown status string, wrong types); `ageLabel` table; `buildSearchURL` property tests with
`fast-check` (result always starts with the template prefix, never contains a raw space, `#`, `&` or
`"` from the query, round-trips through `URLSearchParams`); poller reducer transitions; component
tests with Testing Library asserting roles and text for each of the five statuses, the stale vital,
the `restart →` link appearing only for `down` + restart, empty-query disabled buttons, empty section
absent.

**Fuzz/property**: `fast-check` on `parseStatus` (arbitrary JSON never throws, only returns a result
or an error) and on `buildSearchURL`.

## T18 `/control` placeholder

`src/routes/control/+page.svelte` renders the control layout from `mockups/control-normal.html` and
`control-no-executor.html`, driven by `status.control`. In Phase 1 `available` is false, so:
- a clear note ("Control plane not installed on this box") at the top;
- every service row with its state pill and its action buttons, **all disabled**, each carrying the
  reason as accessible text;
- `#<id>` in the URL highlights and scrolls to that row (used by the `restart →` link);
- the emergency reboot section visible and disabled.

The page is built from the same data and components that T30 will make live, so the layout and its
failure modes are exercised before they can do anything (PLAN Phase 1).

**Tests first**: with `available: false` no button is enabled; hash targeting; an update button with
`update_disabled_reason` shows the reason; no button in the DOM is a `<form>` submit.

## T19 Playwright suite

Runs against the real binary with the portable fixture; this is what proves the embedded app works.

- `testdata/stub/main.go` (run with `go run ./testdata/stub`): a tiny upstream that serves the
  fixture's probe endpoints and has a control endpoint to flip a service up or down. `globalSetup`
  starts it and `meridian run --config testdata/portable/config.yaml`, on free ports, waits for
  `/healthz`.
- Project 1 viewport 390x844 (the layout-deciding one), project 2 viewport 1440x900.
- Cases: launcher renders both sections; tiles meet the 44px minimum; no horizontal scroll at 390 and
  at 200% zoom; flipping a stub service down turns its tile `✕` with a `restart →` link within a poll
  cycle; stale vital dims; search buttons disabled then enabled; Enter submits the first button and
  lands on the right URL; keyboard tab order reaches every control and focus ring is visible (assert
  computed outline); `@axe-core/playwright` reports zero violations on `/` and `/control`; the
  `/control` buttons are all disabled in Phase 1.
- Global assertions: **every network request is same-origin**, no console errors or warnings, no
  response lacks the security headers, CSP present on the document.

**Stage done**: `scripts/check all` is green with the e2e group; opening the built app shows the
portable fixture correctly at both widths.
