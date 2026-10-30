# Control plane

The privileged half of Meridian. Read this before changing anything under `internal/control`,
`internal/hostexec`, `internal/hostprobe` or `deploy/`.

> **Implementation (decisions #52, #62-#67).** The executor is `meridian-host exec`, a stdlib-only Go
> binary installed root-owned at `/etc/meridian/meridian-host`. The sudoers rule is the single command
> `/etc/meridian/meridian-host exec` with no arguments. The container's writable control area is
> `control/inbox/{tmp,queue}`; results and the audit log are in `control/outbox/`, mounted read-only,
> and the executor prunes both. Self-update is the `meridian-host update` subcommand. The container
> serves two routes: `POST /api/control` writes one request, and `GET /api/control/status` reads the
> queue, the recent results, the audit tail and any running auto-restore. Nothing polls an individual
> result file; the page polls that status route.

## Threat model, stated plainly

| Actor | Can they press a button? | Notes |
|---|---|---|
| Anyone on the tailnet | Yes | There is no login. The tailnet *is* the auth boundary, and every service here already behaves this way |
| A bug in Meridian's own Go code | Up to the allowlist | Worst case it writes a malformed request, which the executor rejects |
| A compromised Meridian container | Up to the allowlist | It can request a restart of an allowlisted target. It cannot add a target, cannot run a command, cannot reach the Docker socket, cannot edit the watcher script or the allowlist |
| Someone with `autumn`'s shell | Everything | Not in scope — and note that `autumn` already holds `NOPASSWD: ALL` on this box, so containing *host* root was never this design's job |

Two honest consequences of the last row:

1. The sudoers rule for the executor is **documentation as much as enforcement**. On a box where
   the deploy user already has passwordless sudo, the wrapper's value is that it constrains what a
   *request file* may ask for, and keeps the decision in one reviewable, root-owned file. It does
   not protect against a compromised deploy account.
2. Therefore the real invariant is: **the allowlist and the executor live in `/etc/meridian`,
   root-owned, outside the repo and never mounted into the container.** If that ever changes — e.g.
   someone "helpfully" bind-mounts `/etc/meridian` read-write, or moves `targets.json` into
   `~/Meridian` — the design collapses to "a container with a root button", and the request-file
   indirection becomes decorative.

Also: no CSRF token, no auth header, no rate limiting beyond one-action-at-a-time. That is
consistent with everything else on this tailnet, but it means the hub inherits Tailscale ACLs as its
only access control. If the ACLs are ever loosened, this is the page to think about first.

## The protocol

The container writes one file; the host writes one back. Both directions are JSON in a directory,
which is the same pattern Polaris and Ivy already use for their update handoff.

```
container                                        host
   │ POST /api/control                            │
   │ → control/inbox/queue/<id>.json              │
   │                                              │ meridian-control.path (DirectoryNotEmpty)
   │                                              │ → meridian-control.service (oneshot)
   │                                              │ → sudo -n /etc/meridian/meridian-host exec
   │                                              │     lock /run/lock/meridian-control.lock
   │                                              │     validate, decide, run one command
   │ → control/outbox/results/<id>.json           │
   │ GET /api/control/status ────────────────────►│
   │ (1s while pending, otherwise every 10s)      │
```

**Request** (written by the container, never trusted):

```json
{
  "id": "0192f0c4-6c1a-7f3b-9a10-2b6f7c1d8e44",
  "action": "restart",
  "target": "bwmon",
  "ack": "",
  "requested_at": "2026-10-23T18:04:11Z"
}
```

**Result** (written by the executor, always, on every exit path):

```json
{
  "id": "0192f0c4-…",
  "action": "restart",
  "target": "bwmon",
  "ok": true,
  "exit_code": 0,
  "started_at": "2026-10-23T18:04:11Z",
  "finished_at": "2026-10-23T18:04:16Z",
  "duration_ms": 4820,
  "output": "bwmon.service restarted; active (running) after 2.1s",
  "note": ""
}
```

Rules for both files:

- **Every JSON file is written to a temp path and `rename(2)`d into place**, never written in situ.
  This matters most for the request: `meridian-control.path` uses `DirectoryNotEmpty=`, so it fires the
  moment a filename appears in `control/inbox/queue/` — possibly while the container is still writing
  bytes. The container therefore writes `control/inbox/tmp/<id>.json` and renames it into
  `control/inbox/queue/<id>.json` (same mount, so the rename is atomic), and the executor treats an
  unparseable file as an error rather than retrying blindly. The same rule applies to
  `control/outbox/results/<id>.json` and `status/host.json`, for the readers rather than the `.path`
  units.
- File name is `<id>.json`; the id inside must match the name, must parse as a UUID, and must not
  have been seen before (results are kept, so a replay is detectable).
- Requests older than 120s are refused as stale with `ok:false, note:"stale request"` — a queue that
  filled up while the watcher was down must not fire hours later.
- **Duplicate `(target, action)` requests within 60s of each other are coalesced** — the second gets
  its own result file saying `note:"coalesced with <id>"` rather than acting twice. A double-tapped
  restart button, or two browsers at once, should not be able to restart something twice; the queue
  is a convenience, not a command buffer.
- `output` is capped at 4KiB from the tail; the executor's full output goes to the journal
  (`journalctl -u meridian-control.service`).
- The request file is deleted by `meridian-host exec` once the command has exited, success or
  failure. The result file stays until the executor prunes results older than 7 days.

## The allowlist

`/etc/meridian/targets.json`, root-owned, `0644`, installed by `install.sh`, never by the container.
The container's `config.yaml` and this file must agree; the executor is authoritative and a mismatch
shows up as a refused action, which is the correct direction for the failure to point.

```json
{
  "targets": {
    "bwmon": {
      "kind": "systemd",
      "unit": "bwmon.service",
      "actions": ["start", "stop", "restart", "update"],
      "update": {"kind": "command", "cmd": ["/home/autumn/homelab/bandwidth-monitor/bwmon", "update"], "run_as": "autumn"},
      "criticality": "low"
    },
    "adguard": {
      "kind": "systemd",
      "unit": "AdGuardHome.service",
      "actions": ["start", "stop", "restart"],
      "dns": true,
      "criticality": "high",
      "no_update_reason": "AdGuard has no scripted updater"
    },
    "polaris": {
      "kind": "compose",
      "dir": "/home/autumn/Polaris",
      "service": "polaris",
      "actions": ["start", "stop", "restart", "update"],
      "update": {"kind": "unit", "unit": "polaris-update.service"},
      "criticality": "medium"
    },
    "her-go": {
      "kind": "systemd",
      "unit": "her-go.service",
      "actions": ["start", "stop", "restart", "update"],
      "update": {"kind": "command", "cmd": ["/home/autumn/her-go/her", "update"], "run_as": "autumn"},
      "criticality": "medium",
      "update_timeout_s": 420
    }
  }
}
```

`kind` is one of `systemd`, `compose`, `container`, `host`. `update.kind` is one of `unit`
(`systemctl start <unit>`, blocking because those units are `Type=oneshot`), `command` (an argv
array — never a shell string), `docker` (one `docker compose up -d --pull always <service>`, which
pulls and recreates together), or `self`
(Meridian's own digest-pinned update, run by `meridian-host update`; carries the image repository,
the compose directory, the service, the loopback health port and the signal file). Exactly one
target may be `self`, because the command finds it by kind rather than taking a target argument.
A `self` update's `update_timeout_s` is at least 480 seconds, the sum of its pull, recreate, health and
rollback budgets, or it is left out for the 600 second default (decision #97).

### Who runs what (this is not cosmetic)

The executor is root, but it must not run *everything* as root. App updaters do `git pull` +
`go build` in their own checkout, and `docker compose` writes into the project directory — run those
as root and you leave root-owned `.git` objects, a root-owned binary and a root-owned build cache
behind. The next update, invoked by the service's own CLI as `autumn`, then fails on permissions.

| Action kind | Runs as | Why |
|---|---|---|
| `systemctl start/stop/restart` | root | system units |
| `systemd-run` (auto-restore, reboot) | root | creates permanent transient units |
| `update.kind: command` | deploy user (`autumn`) | the app's own updater writes to its own checkout |
| `update.kind: docker`, `kind: compose` | deploy user | `docker` group, and the compose project directory is user-owned |
| `update.kind: unit` | root (via systemctl) | the oneshot unit already declares its own `User=` |
| `update.kind: self` | deploy user | `docker` group; the command re-executes `meridian-host update` as the same user and must never leave root-owned files in `~/Meridian` |

Implementation: the command runs with a `syscall.Credential` (uid, gid and the account's groups) for
the deploy-user cases, and an explicit minimal environment (`PATH` including `/usr/local/bin`, `HOME`,
`LANG`) — Go lives in `/usr/local/bin` on this box, and the deploy user's interactive `PATH` is not
what a systemd oneshot inherits.

## The action matrix

What the control plane allows. This table is the design; `targets.json` is the implementation.

| Target | Kind | start | stop | restart | update | Notes |
|---|---|---|---|---|---|---|
| `adguard` | systemd `AdGuardHome.service` | ✓ | ⚠ dns | ✓ | — no updater | Stopping it breaks DNS for the **entire tailnet**. Button disabled with a reason, not hidden |
| `unbound` | systemd `unbound.service` | ✓ | ⚠ dns | ✓ | — no updater | AdGuard's upstream; also a tailnet-DNS hazard. Button disabled with a reason |
| `polaris` | compose `~/Polaris` svc `polaris` | ✓ | ✓ | ✓ | `systemctl start polaris-update.service` | |
| `searxng` | compose `~/Polaris` svc `searxng` | ✓ | ✓ | ✓ | via the Polaris update unit (same pull) | Polaris degrades to no search without it |
| `ivy` | compose `~/ivy` svc `ivy` | ✓ | ✓ | ✓ | `systemctl start ivy-update.service` | |
| `warbler` | compose `~/Warbler` svc `warbler` | ✓ | ✓ | ✓ | `systemctl start warbler-update.service` | A bare start of the unit is a complete update: the watcher resolves the newest image digest itself, so no signal file is needed. It pins the digest, takes a database snapshot, recreates, polls health for 120s and rolls back if the new container is not healthy. Design: Warbler's `docs/update.md` |
| `vaultwarden` | compose `~/vaultwarden` svc `vaultwarden` (after the capture below) | ✓ | ✓ | ✓ | `systemctl start` the compose pull/recreate, or the unit if one is added | Its `docker run` spec must be captured as compose first — see below |
| `bwmon` | systemd `bwmon.service` (+`Requires=bwmon-nft.service`) | ✓ | ✓ | ✓ | `bwmon update` (git pull + rebuild + restart) | First control target, least critical |
| `her-go` | systemd `her-go.service` | ✓ | ✓ | ✓ | `{"kind":"command","cmd":["/home/autumn/her-go/her","update"]}` as `autumn` | Same path as Telegram's `/update`: git pull → `go build` → `procmgr` restart (which sudo-restarts itself). Must run as `autumn` — see "Who runs what". `Restart=always` means a stop is a real stop |
| `meridian` | systemd `meridian.service` | ✓ | ✓ | ✓ | `meridian-host update`: pin, pull, recreate, health-check, roll back | Result written by the host *before* the container dies; the digest is the one the tap resolved |
| `host` | host | — | — | — | `reboot` only | No shutdown, no arbitrary reboot flags |

Two entries that must **not** appear: `polaris.service` (the stale bare-metal unit left behind by
the Compose migration — `disabled` since 2026-08-16) and anything under `polaris-codeexec`, which is
Polaris's own privilege boundary and not the hub's business.

**Vaultwarden's update is disabled until its run spec is captured as a compose file.** It was
started with `docker run`, so `docker restart` after a pull would keep running the *old* image. Its
`start`, `stop` and `restart` are ordinary compose actions; enabling `update` needs the current run
spec written out as a compose file in the homelab repo — `docker inspect` gives the env
(`WEBSOCKET_ENABLED`, `DOMAIN`, `SIGNUPS_ALLOWED`), the `8081→80` mapping, the `unless-stopped` policy
and the `/home/autumn/vaultwarden/data` bind. The allowlist carries the reason in
`no_update_reason`, so the button renders disabled and says why.

## Guards

### Two-step stop

`start`, `restart` and `update` are single-tap (with the button showing its own pending state).
`stop` requires a second, differently-worded confirmation: the first tap arms the button for 5s
("Stop AdGuard Home? It's running 12d"), the second tap sends the request. The `ack` field carries
`"stop-confirmed"` so the executor can refuse a stop that arrived without one — the UI guard is a
courtesy, the executor guard is the actual rule.

### DNS auto-restore

For targets marked `"dns": true`:

- the request must carry `ack: "dns-stop-confirmed"` (a stronger phrase than the ordinary stop)
- the executor, *after* the stop succeeds, schedules an independent restore:
  `systemd-run --on-active=10min --unit=meridian-autorestore-<id> systemctl start <unit>`
- the UI shows a live countdown and a **Cancel** button, which sends
  `action:"cancel-autorestore"`, which the executor implements as
  `systemctl stop meridian-autorestore-<id>` (stopping a transient timer unit cancels it)
- the result JSON for a DNS stop carries `note: "auto-restore scheduled at <time>"` so the audit log
  explains why AdGuard came back up on its own

The point: the one button capable of causing a second outage must not be able to last longer than
ten minutes unattended. If the emergency was real, cancel or re-stop; if it was a mis-tap, DNS comes
back by itself and the audit log says so.

### Emergency reboot

Guarded with the same two-step arm as stop (first tap explains the ~40s outage and that the boot
health check will email; second tap commits; the armed state expires in 5s), because it is the
highest-blast-radius action on the page and deserves at least the friction of a stop.

Order matters, and it is short:

1. write the result JSON (`ok:true, note:"reboot in 5s"`) — *before* triggering anything
2. `systemd-run --on-active=5 --unit=meridian-reboot-<id> systemctl reboot`
3. the container is killed by the reboot, which is expected; the UI switches to a "rebooting" state
4. the page polls `/healthz` with backoff (2s, 4s, 8s… to 30s) and renders the launcher when it
   answers, showing the box's new uptime
5. the existing `service-healthcheck.sh` will also email a pass/fail table on boot — that is a
   feature, not a surprise, and the control page should say so before the button is pressed

No shutdown button. A shutdown is unrecoverable without physical access, which is the exact opposite
of what an "emergency remote control" should offer.

### Meridian restarting itself

The executor writes the result, then `systemctl restart meridian.service`. The container is gone
before it can read its own result — which is correct. The UI (loaded in a browser, not in the
container) reconnects, re-fetches `/api/control/status`, finds the finished result by id and shows
it. Same reconnect path as the reboot, so it gets tested for free.

### Update validation

Self-update follows Ivy's hardening, not a loose version of it:

- the update tap resolves the latest GHCR digest and waits out an in-flight CI build before writing
  `update-signal/requested` (Polaris's behaviour, so a tap right after `git push` cannot grab a stale
  build). The target is `update.target` in `config.yaml`, and the hint is written before the control
  request is queued (decision #87)
- `meridian-host update` refuses any image reference that is not exactly the allowlisted repository
  plus `@sha256:` and 64 lowercase hex characters. The container's file is a hint; the host's
  allowlist is the rule
- it pins the digest in `.env`, pulls, recreates, then polls `/healthz` for up to 120s
- if the new container is not healthy, the previous digest goes back and the result says `failed`

## Audit log

`~/Meridian/control/outbox/audit.jsonl`, appended only by the executor (one JSON object per line,
flush on write). Mounted read-only to the container, which renders the last 50 in the control view.

```json
{"at":"2026-10-23T18:04:16Z","id":"0192f0c4-…","action":"restart","target":"bwmon","ok":true,"duration_ms":4820,"by":"tailnet","output":"active (running) after 2.1s"}
```

`by` is `tailnet` for every request, because there is nothing to distinguish. Recording it anyway
means the field exists if authentication ever does, and it makes the absence deliberate rather than
accidental. The log is append-only and never rewritten by the container.

**Retention: 365 days, then purged.** The executor prunes expired lines at the top of each run (one
pass over the file; a year of personal-scale actions is a few hundred KB, so the rewrite is cheap and
never in the hot path of the action itself). The container's read-only view filters to the same
window, so the page cannot show a row the file is about to lose.

## Measured on the box

2026-10-28, `lepotato` (aarch64, systemd 257, Docker 29.6):

- `restart bwmon` from the container's `POST /api/control` to the result file being readable by the
  page: **831 ms**, of which the executor spent **165 ms** in `systemctl restart bwmon.service` and
  its `is-active` post-check. The rest is the `.path` unit starting the oneshot and the page's own
  1s poll.
- With `meridian-control.path` stopped, `/api/control/status` reports `executor.state: missing` within
  one probe tick (30s), which is what renders the "No executor" banner.
- The container sees `control/inbox` writable and `control/outbox`, `config.yaml`, `icons` and
  `status` read-only, with host networking, a read-only root filesystem and uid 1000.

## Concurrency, limits, timeouts

- One action at a time, globally. `meridian-host exec` takes an exclusive lock on
  `/run/lock/meridian-control.lock` and exits immediately if another run holds it — the request stays
  queued and the next `.path` trigger picks it up, exactly as Polaris's `codeexec.sh` does.
- Maximum queue depth 8. Beyond that the UI refuses to enqueue rather than piling up stale requests.
- The UI's wait for a result is 90s (updates can legitimately take longer — those show as "still
  running, see audit log" and keep polling in the background).
- `meridian-control.service` gets `TimeoutStartSec=660s`, larger than the executor's own 600s per-command
  ceiling, so systemd outlives the slowest legitimate action (a registry pull) instead of killing a
  healthy run mid-update; `StartLimitIntervalSec=0` because the `.path` unit can fire faster than a
  oneshot finishes and systemd's default start limit would otherwise disable the unit silently — this
  exact failure cost Polaris a `code_exec` call for ~23 hours on 2026-09-17.

## Adding a target (checklist)

1. Add it to `/etc/meridian/targets.json` with an explicit `actions` list and a `criticality`.
2. Add the target's unit to `/etc/meridian/observe.json` so `meridian-host probe` reports on it. The
   installer seeds that file with `meridian-control.path`; it does not overwrite an existing one.
3. Add a matching `control:` block to `config.yaml`, and a probe/vital entry per
   [INTEGRATIONS.md](INTEGRATIONS.md).
4. Decide `dns: true` if stopping it breaks name resolution for the tailnet.
5. Test the refusal paths first: unknown target, unknown action, stale request, missing `ack`.
6. Perform the real action once, from a phone, and read the audit line afterwards.
7. Update the matrix table above and [INTEGRATIONS.md](INTEGRATIONS.md).

## What Meridian will never do

- Run a shell string, ever. Every command is a fixed argv array in a root-owned file.
- Take a target, unit, container or command name from the request. Those come from the allowlist;
  the request only *selects* among them.
- Add, remove or modify targets at runtime — including through any UI.
- Shut the box down, or reboot with arbitrary flags.
- Hold the Docker socket, or any capability, or run as root.
- Mount the repo, `/etc/meridian`, or anything else the watcher depends on.
