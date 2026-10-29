# Claude

Guidance for working in this repo. Meridian is a personal homelab dashboard: one page that links to
every self-hosted service, shows whether each is up, and lets its owner restart, update or stop them
from a phone. Go backend, SvelteKit frontend embedded in one binary, a distroless container, and a
small root-owned host binary for privileged actions.

**Status: maintained.** The design is settled and the docs below are the source of truth;
[docs/DECISIONS.md](docs/DECISIONS.md) records why each choice was made. The plan and its build-out
work order are archived under [docs/history/](docs/history/PLAN.md) and are not maintained.

## Hard constraints — do not violate these

- **Zero service-specific knowledge in the code.** No service name, hostname, port, icon, section
  title or vendor quirk may appear in Go or Svelte. It all comes from `config.yaml`. If you are tempted
  to add `if service == "something"`, add a probe or vital kind instead — see
  [docs/DECISIONS.md](docs/DECISIONS.md) #38–42 and the vocabulary at the bottom of
  `config.yaml.example`.
- **The container never gets the Docker socket, root, or arbitrary command execution.** Actions go
  through `control/inbox/queue/<id>.json` → the host's `meridian-control.path` unit →
  `sudo /etc/meridian/meridian-host exec`, which validates against a root-owned allowlist.
  [docs/CONTROL.md](docs/CONTROL.md) explains why; changing this invalidates the entire design.
- **The container never runs `systemctl` or reads the tailscaled socket.** systemd and tailnet state
  arrive via `status/host.json`, written by a host timer.
- **No authentication, ever.** The tailnet is the access boundary. Do not add a login, token or
  session to "fix" the lack of one.
- **No inline state-changing buttons on the launcher.** Actions live on `/control` only. A down tile
  may link to its control row; it may not act.
- **Probe URLs are not click-through URLs.** Ivy returns 403 for the tailnet IP as `Host`, so probes
  use loopback while the tile links elsewhere. See [docs/INTEGRATIONS.md](docs/INTEGRATIONS.md).
- **Never write a handoff JSON in place** — write to the temp path then `rename` into position
  (`internal/atomicfile`).
- **No Node, no Python at runtime.** One Go binary with the frontend embedded via `go:embed`, plus the
  stdlib-only `meridian-host` on the host side. The container never updates that host binary
  (decision #67).
- **The host binary is standard library only**, takes argv arrays from the root-owned allowlist, and
  never invokes a shell (decisions #52, #62).
- **Tests first, small commits, every commit green.** `scripts/check --fast` passes before each
  commit. Never skip, weaken or delete a failing test to get green.
- **No dependencies off the allowlist** in [docs/STANDARDS.md](docs/STANDARDS.md) section 9 without a
  new decision.
- **Decisions are appended to [docs/DECISIONS.md](docs/DECISIONS.md), never edited away.** If one
  must reverse, append a new entry that says so and links back.

## The loop for a change

1. **Find the doc.** [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md), [docs/CONTROL.md](docs/CONTROL.md),
   [docs/DESIGN.md](docs/DESIGN.md), [docs/INTEGRATIONS.md](docs/INTEGRATIONS.md) or
   [docs/STANDARDS.md](docs/STANDARDS.md) covers most changes; [docs/DECISIONS.md](docs/DECISIONS.md)
   says why the current shape won. Grep the code before writing any helper.
2. **Red.** Write the test (and fuzz target, if bytes cross a boundary). Run it and watch it fail for
   the right reason.
3. **Green.** Write the least code that passes. I/O at the edges, pure functions in the middle.
4. **Refactor** with the tests green.
5. **Check.** `scripts/check --fast` before every commit; `scripts/check all` before pushing or
   finishing.
6. **Commit small**, Conventional Commits, one logical change each, tests beside the code they test.
7. **Reconcile.** A sentence this change made false is fixed in the same series; a new design choice
   is appended to [docs/DECISIONS.md](docs/DECISIONS.md). A question only the owner can answer goes in
   [docs/OPEN-QUESTIONS.md](docs/OPEN-QUESTIONS.md) with a recommendation, or to them directly with the
   question tool when it is blocking.

There is no build to resume, so there is no session ritual: a change lands when it is green,
documented and committed.

## Where things are

| Doc | Read when |
|---|---|
| [docs/STANDARDS.md](docs/STANDARDS.md) | always; how to write, test, fuzz, secure, commit |
| [docs/DECISIONS.md](docs/DECISIONS.md) | before changing anything a decision covers |
| [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) | components, networking, polling model, config shape |
| [docs/CONTROL.md](docs/CONTROL.md) | anything under `control`, `hostexec`, `hostprobe`, or `deploy/` |
| [docs/DESIGN.md](docs/DESIGN.md) + `mockups/` | anything under `web/` |
| [docs/INTEGRATIONS.md](docs/INTEGRATIONS.md) | adding a service, a probe kind, or a vital kind |
| [docs/REVIEW-BRIEF.md](docs/REVIEW-BRIEF.md) | what is load-bearing, and where the trust boundaries are |
| [config.yaml.example](config.yaml.example) | the config vocabulary; a test keeps it in sync with the code |
| [docs/history/](docs/history/PLAN.md) | how it was built; historical, not maintained |

## Conventions

- Go with `net/http` route patterns (`GET /api/status`), `charmbracelet/log`, packages under
  `internal/`, thin `cmd/*/main.go`.
- SvelteKit 5 / Svelte 5 runes, `adapter-static`, plain CSS custom properties from
  [docs/DESIGN.md](docs/DESIGN.md) tokens. No Tailwind, no component library, no service knowledge.
- Config is YAML; secrets are `.env`-only and referenced by environment variable name. A secret never
  reaches a struct that gets logged or marshalled, an error, or the browser.
- Tests are written first and watched failing; integration tests over mocks; fuzz targets for every
  parser and security-boundary validator.
- Copy rules for every user-facing string: no em dashes, no exclamation marks, second person, present
  tense.

## Local development

The frontend builds on a laptop and the binary embeds `web/build`. Never compile on the Le Potato by
preference: it is a weak ARM board, and the point of the CI image is that it only pulls.

```bash
cd web && pnpm install --frozen-lockfile && pnpm build   # the embedded app
go build ./cmd/...                                       # both binaries
scripts/check --fast                                     # what the pre-commit hook runs
scripts/check all                                        # adds fuzz, e2e, security (~7 min)
FUZZ_TIME=1s scripts/check all                           # the quick full pass
go test ./internal/invariants -v                         # the hard constraints, one test each
```

## Design changes

The visual language is fixed by [docs/DESIGN.md](docs/DESIGN.md); it is not up for renegotiation in a
casual change. One cold accent (Meridian blue), dark only, phone first (390×844 decides the layout),
two visible search buttons, status states each with a non-colour mark, 44px targets, visible focus
rings. Destructive actions are two-step and never filled; cancel is the wider, primary button.

Mockups live in `mockups/`, one self-contained HTML file per screen or state, tokens inlined from
`docs/DESIGN.md`.
