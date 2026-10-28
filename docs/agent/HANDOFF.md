# Handoff

Read this first in every session. It is **rewritten, not appended**, at the end of each session; the
history is in git. Keep it under about 40 lines: it is for a cold start with an empty context.

## Last session: S13-S16, then the box (2026-10-27/28)

Finished: **T28, T29, T30, T31**, all ticked. **Stage 6 (control plane) is code-complete**: the only
work left in it is the two `[human]` gates, T32 and T33.

- `internal/hostexec`: the command table (`builders.go`), the real Commander (`runner.go`),
  `ResultFirst` + post-check + deadlines + the DNS guard in `exec.go`, `meridian-host exec`
  (`command.go`). Decisions #84-#85.
- Web: `state/armed.ts` (two-step reducer, fast-check properties), `api/control.ts` (the view parser,
  shared golden), `api/client.ts` (`postControl`/`fetchControlStatus`), `state/poller.ts`
  (`startLoop` for both pages), `state/control.ts` (delay, busy reason, countdown, history, acks,
  copy), `state/reconnect.ts` (2s-to-30s ladder), and the live page: `ActionButton`, `ServiceRow`,
  `RebootCard`, `RestoreBanner`, `ActionHistory`, `Banner`.
- `deploy/`: `systemd/meridian-control.{path,service}`, `sudoers/meridian`,
  `targets.example.json`, `observe.example.json`, compose's one writable mount. `examples/
  targets.homelab.json` is the CONTROL.md matrix.
- e2e: `testdata/stub` is now both the stub upstream and a **stub executor** (`-control DIR`), so
  `control.spec.ts` runs the real round trip: restart to receipt, armed DNS stop with countdown and
  cancel, and a reboot that writes its provisional result and clears when the hub answers.

## On the potato (Phase 1, and Phase 2 for bwmon only)

The owner asked for this and it is done, but **T24 and T32 stay unticked**: `[human]` tasks are the
owner's to tick. What is on the box now, all of it reversible:

- `~/Meridian/`: `config.yaml` (from `examples/homelab.yaml`, `control.reboot: false` for now),
  `docker-compose.yml`, `.env` (`MERIDIAN_IMAGE=meridian:0.1.0-dev`, uid/gid 1000), and the four
  directories (`icons`, `status`, `control/inbox/{tmp,queue}`, `control/outbox/results`).
- The image was built for arm64 on the Mac and `docker load`ed on the box. It is not from GHCR yet;
  T34/T35 is what pins it properly.
- `https://lepotato.taild95077.ts.net:8891` via `tailscale serve --https=8891`
  (`sudo tailscale serve --https=8891 off` undoes it). 10 of 10 tiles up.
- Ivy's tile links to `http://lepotato.taild95077.ts.net:8418`, not its IP. Port 8418 is Ivy's own
  plain-HTTP listener, **not** a `tailscale serve` port: HTTPS there fails the TLS handshake, so the
  tile has to say `http`. Its probes stay on localhost, which is the one Host header Ivy accepts.
- `/etc/meridian/{meridian-host,host.json,targets.json,observe.json}` (bwmon only,
  `observe.json` = her-go.service + meridian-control.path) and `/etc/sudoers.d/meridian`, validated.
- `meridian-probe.timer` and `meridian-control.path` are enabled. One bwmon restart was measured at
  **831ms** wall / **165ms** in systemctl; refusals and the no-executor state were checked. Numbers
  are in `docs/CONTROL.md` "Measured on the box".
- AdGuard's vital is degraded: `MERIDIAN_ADGUARD_USER` and `MERIDIAN_ADGUARD_PASS` are not in `.env`.
  Only the owner can add those.

## Next

Session **S17**: **T34** (`updater`: newest GHCR digest, the handoff file), then T35
(`meridian-host update` with pin, pull, recreate, health poll, rollback). Read
`docs/agent/07-update.md` and `docs/CONTROL.md` "Self-update". Then T36's installer is what turns
this hand-install into a repeatable one.

## State of the tree

Clean and green under the full `scripts/check` (fuzz, e2e 34 tests, security). Nothing is pushed:
S13-S16's commits are waiting for the owner's next batch.

## Gotchas

- The control loop starts only once `/api/status` says a control plane exists: without that guard a
  Phase 1 box polls a 503 route and the browser logs a console error, which the e2e fixture fails on.
- `web/build` is embedded in the binary, so an e2e run needs **both** `pnpm build` and
  `go build -o dist/meridian ./cmd/meridian`; `scripts/check e2e` does both. Running Playwright alone
  serves the previous build and lies to you.
- The e2e control directory is shared by every test in the run, so a test that looks for a history
  row must count rows before and after, not just find one (the reboot test does).
- 12px text at `--color-text-faint` fails axe's contrast floor; use `--color-text-dim`.
- `deploy/observe.example.json` is the line T36's installer must append to `/etc/meridian/observe.json`
  (`meridian-control.path`), or a dead executor looks like a healthy one.
- The executor's own ceiling is 600s and the unit's `TimeoutStartSec` is 660 on purpose: systemd must
  never be the one that gives up. `StartLimitIntervalSec=0` is what keeps the watcher alive.
- The verify step compares the **word** `systemctl is-active` prints, not its exit code.
- Still true: `hostfile.Read` takes the expected owner; `hostexec`/`wire`/`hostprobe` are stdlib-only;
  `scripts/check` is the only gate; a fuzz crasher is committed, never re-run away.

## For the owner

- **T32 is done on the box but needs your eyes**: open the page on your phone, press restart on
  bwmon, and confirm the 40s reboot button is disabled with a reason (the `host` target is not in the
  allowlist yet). Then tick T24 and T32.
- **T33** is the next gate and needs your hands: enable the rest of the matrix in
  `/etc/meridian/targets.json` (start from `examples/targets.homelab.json`), set `control.reboot: true`
  in `~/Meridian/config.yaml`, and prove the AdGuard auto-restore.
- Dependabot's npm job will offer TypeScript 7, which breaks the `svelte-check` peer range: it needs a
  decision, not a merge.
- `[human]` gates T24, T32, T33, T37 are open. T24 (Phase 1) and T32 (Phase 2) are both unblocked.
