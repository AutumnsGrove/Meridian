# Plan

Milestones in the order they should be built. Each one is independently deployable — the launcher
is useful the day it exists, before any privileged code is written. Do not start a phase before the
previous one's "done when" is true.

The task-level work order that implements these phases is [agent/TASKS.md](agent/TASKS.md): Phase 1 is
its stages 1-5, Phases 2 and 3 are stage 6, Phase 4 is stage 7. This file stays the definition of what
"done" means for each phase. Two details here are superseded by decisions: the host-side scripts
(`control.sh`, `probe.sh`, `control-run`) are the Go binary `meridian-host` (#52), and the Phase 1
"verify from inside the container" check uses `meridian doctor` because the distroless image has no
`curl` (#68).

Phase 0 — Plans ✅ done
Phase 1 — Read-only launcher, deployed (current; work order stages 1-5)
Phase 2 — Control plane, one service, behind a guard
Phase 3 — Control plane, all targets
Phase 4 — Self-update and install path
Phase 5 — Homelab documentation

---

## Phase 0 — Plans ✅

Write the documents in this repo; settle the open questions at the bottom of this file.

**Done when:** the docs agree with each other, the potato facts they cite are still true (unit
names, ports, container names), and the control-plane design has been read end to end once with
fresh eyes.

**Done** on 2026-10-23. The design landed at 71 recorded decisions, a standards document and a
session-by-session work order ([agent/TASKS.md](agent/TASKS.md)), with mockups for every state.
Two things the original criterion was written before: the repo is public, and the build is split
across short sessions (decisions #44, #70), so "done" was verified by the docs being testable
rather than by one long read. The remaining design questions live in
[OPEN-QUESTIONS.md](OPEN-QUESTIONS.md), not at the bottom of this file.

---

## Phase 1 — Read-only launcher

Goal: one URL worth bookmarking, with no privileged code in the tree at all.

- Go module, `cmd/` CLI (`run`, `doctor`, `version`), `gateway/` HTTP server, `probe/` poller,
  `config/` schema + validation, `web/` SvelteKit app embedded with `go:embed`
- Config loading (sections, services, probes, vitals, search buttons), strict validation with useful
  errors, SIGHUP hot reload, and `meridian doctor`
- **Portability fixture test**: a config of services this homelab has never heard of renders
  correctly, so no service-specific assumption can leak into the code (see
  [DECISIONS.md](DECISIONS.md) #38–42)
- `network_mode: host` in the compose file from day one — see [ARCHITECTURE.md](ARCHITECTURE.md)
  "Networking"; getting this wrong later means every probe URL has to change at once
- `meridian-probe.timer` + `probe.sh` writing `status/host.json` (unit states, tailscale, uptime),
  generated from the target list so nothing is controllable that isn't observable
- Tiles: status dot, one vital number, click through to the service
- Search box: **Search** → `https://…:8888/search?q=`, **Ask Polaris** →
  `https://…:8899/search?q=` (verified: Polaris's `/search` route reads `?q=` from the URL)
- `GET /healthz` for the hub itself
- `/control` route exists but every action button is disabled with a "control plane not installed"
  note — the layout and its failure modes get exercised before they can do anything

**Explicitly not in this phase:** no `control` package, no request files, no sudoers, no watcher
units, no update path. If the phase-1 container were fully compromised, the attacker would gain a
page that reads some health endpoints on the local network.

**Done when:**
- `go build` and `pnpm build` both pass on the Mac, and the binary serves the embedded frontend
- Every service in the config shows a correct dot on the potato's tailnet, including the "down"
  case (test by stopping one on purpose)
- Probe URLs are verified from *inside the container* (`docker exec … curl`), not from the host —
  this is the check that catches a bridge-network regression before it turns seven tiles red
- Idle backoff behaves: with no page open for 10 minutes, the poller drops to 2 minutes and a fresh
  page load still shows current values (verify by watching the service logs' request timestamps)
- `meridian doctor` correctly explains three deliberately broken configs: an unknown probe kind, a
  control target missing from the allowlist, and a URL that doesn't answer
- Adding a service is a config edit and a SIGHUP — no restart, no rebuild — and a broken reload
  leaves the previous config serving
- Ivy's vital number is correct (needs-attention count cross-checked against the app's own UI)
- No probe is visible in the service's own logs as an error (the Ivy `Host` header detail in
  [INTEGRATIONS.md](INTEGRATIONS.md) matters here)
- Runs under Docker Compose on the potato, `restart: unless-stopped`, bound to loopback, reachable
  at `https://lepotato.taild95077.ts.net:8891` through `tailscale serve`

---

## Phase 2 — Control plane, one service, behind a guard

Goal: prove the privileged path end to end against the least dangerous target.

- `/etc/meridian/targets.json` (root-owned allowlist) + `/etc/meridian/control-run` (root-owned
  executor), installed by an installer step, never by the container
- `control/` package: write `control/queue/<id>.json`, poll `control/results/<id>.json`, render
  pending/succeeded/failed with the executor's output tail
- `meridian-control.path` → `meridian-control.service` → `compose/watcher/control.sh` (runs as the
  deploy user, `flock`-guarded, one request at a time)
- Audit log append in the executor (`control/audit.jsonl`, mounted read-only to the container)
- First target: **bwmon restart only.** Reasons: it is the least critical service, it is a plain
  `Type=simple` unit with no dependants, and it has a `Requires=bwmon-nft.service` dependency that
  will immediately prove whether the executor handles unit dependencies sanely
- Two-step stop UI is built here, not later, but only bwmon has it enabled

**Done when:**
- Restart latency is measured (button → service back) and recorded in the docs; a stuck action
  times out visibly instead of hanging forever
- Killing `meridian-control.path` produces a clear "no executor" state in the UI rather than a
  spinner that never resolves (this is Ivy's documented failure mode and it should not be repeated)
- A hand-written request file with an unknown target, an unknown action, or a stale timestamp is
  refused by the executor, and the refusal is visible in the audit log
- The container's filesystem view contains `control/queue`, `control/results`, `control/audit.jsonl`
  and nothing else from the host — no repo mount, no `/etc/meridian`

---

## Phase 3 — Control plane, all targets

- The full matrix in [CONTROL.md](CONTROL.md): AdGuard, Unbound, Polaris (+ SearXNG), Ivy,
  Vaultwarden, bwmon, her-go, Meridian itself, the host
- Per-target update via each app's own updater (`systemctl start polaris-update.service`,
  `ivy-update.service`, `her update` as the deploy user, `bwmon update` as the deploy user,
  `docker pull` + recreate for Vaultwarden)
- Update buttons that do not exist for AdGuard and Unbound: rendered disabled, carrying the reason,
  rather than omitted
- **Vaultwarden: capture the `docker run` spec as a compose file** in the homelab repo first (env,
  `8081→80`, `unless-stopped`, `/home/autumn/vaultwarden/data` bind), run the migration once,
  confirm the database is untouched, and only then enable its update action. Until that lands its
  update button is absent rather than half-working
- DNS guard: stop on AdGuard/Unbound requires the second confirmation *and* schedules a 10-minute
  auto-restore, with a visible countdown and a cancel button
- Emergency reboot: result written before the reboot is triggered, page reconnects on its own
- Meridian restarting itself (result written by the host before the container dies)

**Done when:**
- Every action in the matrix has been performed once, on purpose, and the audit log tells the story
  afterwards
- The two app updaters run as `autumn` and leave no root-owned files in `~/her-go` or
  `~/homelab/bandwidth-monitor` (check with `find … -user root` after each)
- The auto-restore has been proven by stopping AdGuard and doing nothing: DNS comes back on its own
- A reboot initiated from a phone returns to a working launcher with no manual reload
- Audit lines older than 365 days are pruned and nothing else is

---

## Phase 4 — Self-update and install path

- `meridian update`: resolve the newest GHCR digest (waiting out an in-flight CI build, as Polaris
  does), write `update/requested`, hand off to `meridian-update.service`
- `update.sh` validates the image reference (Ivy's hardening: refuse anything that is not a
  `ghcr.io/autumnsgrove/meridian@sha256:…` digest), pins it in `.env`, pulls, recreates, polls
  `/healthz`, and puts the previous digest back if the new container is not healthy
- `install.sh` for a fresh box: creates `~/Meridian/{data,control,update-signal}`, writes the
  compose file and units, installs `/etc/meridian/*` and the one narrow sudoers rule, enables the
  `.path` units, then verifies them (the way Polaris/Ivy's `watcher-sync-verify.sh` does)
- GitHub Actions: multi-arch image to GHCR on tag/push to `main`, matching Ivy's CI

**Done when:** a `git push` followed by one tap in the UI lands a new version, and a deliberately
broken image rolls itself back with the previous version still running.

---

## Phase 5 — Homelab documentation

Belongs in the homelab repo, not this one:

- Meridian section in `README.md` (service entry, Quick Access row, port, backup line, changelog)
- `docs/meridian.md` alongside `docs/polaris.md`
- **Ivy into the homelab README and a new `docs/ivy.md`** — it is deployed and running right now and
  appears in zero lines of that repo
- `maintenance-setup/service-healthcheck.sh`: add Ivy (`http://localhost:8418/api/v1/health`) and
  Meridian (`http://localhost:8891/healthz`), so a dead mail client or a dead hub produces an email

**Done when:** the homelab README's service list matches `docker ps` and `systemctl list-units` on
the potato, with nothing missing and nothing stale.

---

## Open questions

Settle these before the phase that needs them.

| Question | Needed by | Notes |
|---|---|---|
| AdGuard credentials | **answered** | Reuse the admin login; password in `~/Meridian/.env` |
| Unbound and AdGuard update path | **answered** | Neither has a scripted updater, and neither gets one. The update button renders disabled with the reason, rather than absent — a greyed button explains itself, a missing one looks like a bug |
| her-go update path | **answered** | `her update` exists; wire it as an `update.kind: command` target that runs as `autumn` (see CONTROL.md "Who runs what") |
| Action retention | **answered** | Keep 365 days, prune older lines in `control.sh` on each run |
| Does the hub need its own login? | never, by design | Tailnet is the boundary. Revisit only if the box is ever exposed beyond Tailscale — the same trigger as the bwmon `CAP_NET_RAW` note |
| Vaultwarden compose definition | **answered** | Capture the run spec as compose in Phase 3, then enable its update |
| `polaris.service` (bare metal, now `disabled`) | Phase 3 | Stale, left behind by the move to Compose. Do **not** add it as a control target; consider deleting it from the box |
| Update-all | after Phase 4 | Not in v1 |
