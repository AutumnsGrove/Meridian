# Handoff

Read this first in every session. It is **rewritten, not appended**, at the end of each session; the
history is in git. Keep it under about 40 lines: it is for a cold start with an empty context.

## Last session: S13-S16 and the first install on the box (2026-10-27/28)

**T28, T29, T30, T31 are done and ticked.** Stage 6 (control plane) is code-complete; only the two
`[human]` gates remain. Everything below is committed and green.

- `internal/hostexec`: the command table (`builders.go`), the real Commander (`runner.go`),
  `ResultFirst` + `is-active` post-check + deadlines + the DNS guard (`exec.go`), and the
  `meridian-host exec` command (`command.go`). Decisions #84-#86.
- Web: `state/armed.ts`, `api/control.ts`, `state/poller.ts` (`startLoop` for both pages),
  `state/control.ts`, `state/reconnect.ts`, and the live `/control` page (`ActionButton`,
  `ServiceRow`, `RebootCard`, `RestoreBanner`, `ActionHistory`, `Banner`). Component tests cover the
  four mockup states.
- `deploy/`: `systemd/meridian-control.{path,service}`, `sudoers/meridian`, `targets.example.json`,
  `observe.example.json`, compose's one writable mount; `examples/targets.homelab.json` is the
  CONTROL.md matrix. `internal/invariants/deploy_test.go` holds them to it.
- e2e: `testdata/stub` is now the stub upstream **and** a stub executor (`-control DIR`), so
  `control.spec.ts` runs the real round trip without root.

## On the potato

Done at the owner's request, but **T24 and T32 stay unticked**: `[human]` tasks are the owner's to
tick. All of it is reversible.

- `~/Meridian/`: `config.yaml` (from `examples/homelab.yaml`, `reboot: false` for now),
  `docker-compose.yml`, `.env` (`MERIDIAN_IMAGE=meridian:0.1.0-dev`), `icons/`, `status/`,
  `control/inbox/{tmp,queue}`, `control/outbox/results`. The image was built for arm64 on the Mac and
  `docker load`ed: **not from GHCR**, which is what T34/T35 fixes.
- Live at `https://lepotato.taild95077.ts.net:8891` (`tailscale serve --https=8891`; `--https=8891 off`
  undoes it), 10 of 10 tiles up. Ivy's tile links to `http://lepotato.taild95077.ts.net:8418`: 8418 is
  Ivy's own plain-HTTP port, not a `tailscale serve` port, so HTTPS there fails the handshake.
- `/etc/meridian/{meridian-host,host.json,targets.json,observe.json}` (bwmon only) and
  `/etc/sudoers.d/meridian` (validated). `meridian-probe.timer` and `meridian-control.path` enabled.
  One bwmon restart measured **831ms** wall / **165ms** in systemctl; refusals and the no-executor
  state verified. Numbers in `docs/CONTROL.md` "Measured on the box".
- AdGuard's vital is degraded: `MERIDIAN_ADGUARD_USER`/`_PASS` are missing from `.env`. Owner only.

## Next

Session **S17**: **T34** (`internal/updater`: `Resolve` against GHCR, wait out an in-flight CI build,
`wire.ParseDigest`, `Request(dir, digest)`, `GET /api/update`) then **T35** (`meridian-host update`:
validate the reference, pin, pull, recreate, poll `/healthz`, roll back). Read
`docs/agent/07-update.md` and `docs/CONTROL.md` "Self-update". T34 is one session's work on its own.

## State of the tree

Clean and green under the full `scripts/check` (fuzz, 34 e2e, security). Nine commits are unpushed.

## Gotchas

- The control loop starts only once `/api/status` says a control plane exists: a Phase 1 box polling
  the 503 route logs a console error, and the e2e fixture fails on that.
- `web/build` is embedded in the binary, so e2e needs **both** `pnpm build` and
  `go build -o dist/meridian ./cmd/meridian`; `scripts/check e2e` does both. Playwright alone lies.
- The e2e control directory is shared across a run: assert history rows by **count**, not presence,
  and the stub executor holds an action 2.5s so the in-flight state is assertable.
- 12px text at `--color-text-faint` fails axe's contrast floor; use `--color-text-dim`.
- `ProtectHome=read-only`, never `yes`, in the probe unit: its status directory lives under `/home`.
- The executor's ceiling is 600s and the unit's `TimeoutStartSec` is 660 on purpose; the control
  service must keep `StartLimitIntervalSec=0` and must not set `NoNewPrivileges` (sudo needs it).
- Still true: `hostfile.Read` takes the expected owner; `hostexec`/`wire`/`hostprobe` are stdlib-only;
  `scripts/check` is the only gate; a fuzz crasher is committed, never re-run away.

## For the owner

- **Tick T24 and T32** after a look on your phone: press restart on bwmon once, and confirm the reboot
  button is disabled with a reason (the `host` target is not in the allowlist yet).
- **T33** needs your hands: the rest of the matrix in `/etc/meridian/targets.json` (start from
  `examples/targets.homelab.json`), `control.reboot: true`, and the AdGuard auto-restore proof.
- Dependabot's npm job will offer TypeScript 7, which breaks the `svelte-check` peer range: it needs a
  decision, not a merge.
