# Claude

Guidance for working in this repo. Meridian is a personal homelab dashboard: one page that links to
every self-hosted service, shows whether each is up, and lets its owner restart/update/stop them
from a phone.

**Status: build-out about to start.** There is no Go code and no frontend yet; the design is settled
and the work order is written. Implementation is done by a coding agent following `AGENTS.md` and
`docs/agent/TASKS.md`. When the build ends, the last tasks rewrite this file for maintenance.

## Read these first, in this order

0. `AGENTS.md` and `docs/STANDARDS.md` — the build workflow and the engineering standards (TDD, data
   primacy, fuzzing, security rules, commit style). They apply to every change
1. `README.md` — what this is, what it deliberately is not
2. `docs/PLAN.md` — the phases, and what "done" means for each
3. `docs/DECISIONS.md` — 71 decisions already made; do not silently reverse one
4. `docs/ARCHITECTURE.md` — components, networking, the two probe paths
5. `docs/CONTROL.md` — the privileged control plane, if you're touching anything action-related
6. `docs/DESIGN.md` — tokens, layouts, phone-first rules
7. `docs/INTEGRATIONS.md` — the verified endpoints and per-service quirks

## Hard constraints — do not violate these

- **Zero service-specific knowledge in the code.** No service name, hostname, port, icon, section
  title or vendor quirk may appear in Go or Svelte. It all comes from `config.yaml`. If you're
  tempted to add `if service == "ivy"`, add a probe/vital kind instead — see
  `docs/DECISIONS.md` #38–42 and the vocabulary at the bottom of `config.yaml.example`.
- **The container never gets the Docker socket, root, or arbitrary command execution.** Actions go
  through `control/inbox/queue/<id>.json` → host `meridian-control.path` →
  `sudo /etc/meridian/meridian-host exec`, which validates against a root-owned allowlist. `docs/CONTROL.md` explains why; changing this
  invalidates the entire design.
- **The container never runs `systemctl` or reads the tailscaled socket.** systemd/tailnet state
  arrives via `status/host.json`, written by a host timer.
- **No authentication, ever.** The tailnet is the access boundary. Do not add a login, a token, or
  a session to "fix" the lack of one.
- **No inline state-changing buttons on the launcher.** Actions live on `/control` only. A down tile
  may link to its control row; it may not act.
- **Probe URLs are not click-through URLs.** Ivy returns 403 for `Host: 100.81.103.51:8418`; probes
  use `localhost`. See `docs/INTEGRATIONS.md`.
- **Never write a handoff JSON in place** — write to the temp path then `rename` into position.
- **No Node, no Python at runtime.** One Go binary with the frontend embedded via `go:embed`, plus the
  small stdlib-only `meridian-host` binary on the host side (decision #52).
- **Tests first, small commits, every commit green.** `scripts/check --fast` passes before each
  commit; see `docs/STANDARDS.md`. Never skip or weaken a test to get green.
- **No dependencies off the allowlist** in `docs/STANDARDS.md` section 9 without a new decision.
- Decisions are appended to `docs/DECISIONS.md`, never edited away. If one must reverse, add a new
  entry saying so and link back.

## Design work (mockups)

The visual language is fixed by `docs/DESIGN.md`; it is not up for renegotiation in a mockup pass.
In particular:

- Warm-neutral dark surfaces borrowed from Polaris, **one** cold accent: Meridian blue
  `oklch(78% 0.10 205)`. Not gold (Polaris), not lilac (Ivy), not the Grove Tokyo Night palette.
- Dark only. No light theme, no theme toggle.
- Phone first: 390×844 is the viewport that decides the layout. Desktop is the adaptation.
- Two visible search buttons (**Search**, **Ask Polaris**). No `⌘K`-only paths, no modifier chords —
  the primary device has no keyboard.
- Sections are **Everyday** and **Backbone**. Five status states, each with a non-colour mark:
  `● ok`, `◐ degraded`, `✕ down`, `○ unknown`, `— disabled`.
- Everything interactive has a ≥44px touch target and a visible focus ring.
- Destructive actions are two-step (armed) and never filled: cancel is the wider, primary button.

### Screens worth mocking, in priority order

1. Launcher — default state, phone width (390) and desktop width (1440)
2. Launcher — a service down, showing the `restart →` shortcut into control
3. Control — normal: rows with state pills, action buttons, action history beneath
4. Control — armed stop on AdGuard, with the DNS warning and auto-restore countdown
5. Control — "No executor" banner, actions queued but not picked up
6. Control — armed reboot, and the "rebooting, back in ~40s" state
7. Launcher — empty search + non-empty search (both buttons' enabled/disabled states)
8. Launcher — stale vitals (a value with its age, dimmed) and an `unknown` dot

Mockups live in `mockups/`, one self-contained HTML file per screen or state, tokens inlined from
`docs/DESIGN.md` (Polaris keeps its mockups this way — see `~/Projects/Polaris/mockups/`).

## House conventions (from the sibling projects)

- Go with `net/http` route patterns (`GET /api/status`), `charmbracelet/log` for logging
- SvelteKit 5 + Svelte 5 runes, `adapter-static`, plain CSS custom properties (no Tailwind, no CSS
  framework), `@fontsource-variable/lexend` self-hosted, `@lucide/svelte` for icons
- Config is YAML; secrets are `.env`-only and referenced by environment variable name
- Single binary, embedded frontend, container that only ever pulls a CI-built image
- Tests are written first and watched failing; integration tests over mocks; fuzz targets for every
  parser and security-boundary validator (`docs/STANDARDS.md`)
- No em dashes in user-facing copy; no exclamation marks; second person, present tense

## Local development (once Phase 1 exists)

The frontend builds on the Mac (`pnpm build` in `web/`); the binary embeds it. Never compile on the
Le Potato by preference — it is a weak ARM board and the whole point of the CI image is that it only
pulls.
