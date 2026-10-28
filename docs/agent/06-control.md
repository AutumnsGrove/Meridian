# Stage 6: Control plane

Goal: Phases 2 and 3 of [PLAN.md](../PLAN.md). This is the one stage where a bug can mean "someone
on the tailnet gets root on the box", so the rules in [CONTROL.md](../CONTROL.md) and
STANDARDS section 4 "Privilege" are not optional, and every refusal path is tested before its happy
path. Read CONTROL.md in full first. Decisions #14-#18, #31-#33, #36, #37, #52, #54, #62-#66.

**What changed from CONTROL.md** (all recorded in DECISIONS.md; CONTROL.md carries pointers):
- `/etc/meridian/control-run`, `control.sh` and `probe.sh` are one Go binary, `meridian-host`
  (#52). `meridian-host exec` is the executor; the sudoers rule names exactly
  `/etc/meridian/meridian-host exec` with no arguments.
- The container's writable area is one mount, `control/inbox/` (containing `tmp/` and `queue/`), so the
  atomic rename never crosses a mount point; results and the audit log are in `control/outbox/`,
  mounted **read-only** (#63).

## Wire types (T25)

`internal/wire`, stdlib only. Data first; the executor and the container both import these and
nothing else defines them.

```go
type Action string // start stop restart update reboot cancel-autorestore
type Request struct {
    ID          string    `json:"id"`           // canonical UUID, equals the file name
    Action      Action    `json:"action"`
    Target      string    `json:"target"`       // slug; "host" for reboot
    Ack         string    `json:"ack"`
    Ref         string    `json:"ref,omitempty"` // only for cancel-autorestore: the stop it cancels
    RequestedAt time.Time `json:"requested_at"`
}
type Result struct {
    ID, Action, Target string
    OK         bool
    Code       string     // closed set, see below; "" when ok
    ExitCode   int
    StartedAt, FinishedAt time.Time
    DurationMS int64
    Output     string     // tail, max 4 KiB, control characters stripped
    Note       string
    RestoreAt  *time.Time `json:"restore_at,omitempty"` // DNS stop: when auto-restore fires
    Ref        string     `json:"ref,omitempty"`
    InProgress bool       `json:"in_progress,omitempty"` // provisional result (see "result first")
}
type AuditLine struct { At time.Time; ID, Action, Target string; OK bool; Code string;
    DurationMS int64; By string; Output string } // By is always "tailnet"
```

- Refusal/result **codes** (closed table with the user sentence for each): `bad_request`, `bad_id`,
  `bad_action`, `bad_target`, `unknown_target`, `action_not_allowed`, `missing_ack`, `bad_ack`, `stale`,
  `replay`, `coalesced`, `queue_full`, `timeout`, `exec_failed`, `not_active_after`, `restore_failed`,
  `bad_ref`, `internal`, and `unavailable` for the container's own "no control plane here" answer
  (decision #82).
- `ValidateRequest(Request, now time.Time) error`: **structure only** (field grammar, id format, ref
  rules, `requested_at` not more than 5s in the future). Returns `*Refusal{Code}`; policy against the
  allowlist is `hostexec`'s job.
- The ack table is data: `RequiredAck(action Action, dns bool) string`: `stop` gives
  `stop-confirmed`, `stop` on a `dns` target `dns-stop-confirmed`, `reboot` `reboot-confirmed`, all
  others the empty string.
- Strict decoders (`DecodeRequest(r io.Reader)` caps at 4 KiB, disallows unknown fields, rejects
  trailing data) and golden files in `testdata/wire/` shared with the TypeScript tests.

**Tests first**: table of malformed requests, one per code; ack table; round-trip of the goldens.

**Fuzz**: `FuzzDecodeRequest` (never panics; if it returns a request, `ValidateRequest` either accepts
it or returns a `*Refusal`; anything accepted re-marshals to bytes that decode to an equal value),
`FuzzResultJSON`.

## T26 Container side

Package `internal/control`. It **never constructs a command**; it can only produce a validated
`wire.Request` and put it in the inbox.

- `Enqueue(ctx, in EnqueueInput) (id string, err error)`: `EnqueueInput{Action, Target, Ack, Ref}` is
  all the browser may supply. Checks, in order: the target and action exist in **config** for that
  service (`control:` block; `reboot` is allowed only when the top-level `control.reboot` setting is `true`), queue
  depth (count of files in `inbox/queue`) is under `control.queue_depth` (`ErrQueueFull`), a pending
  request for the same `(target, action)` is not already queued, the ack equals
  `wire.RequiredAck`. Then it builds the request (server-generated id and timestamp) and writes it
  with `atomicfile.WriteVia(inbox/tmp, inbox/queue, id+".json")` through an `os.Root` on the inbox.
  The ack is checked here as a courtesy; the executor checks it again as the rule.
- `Status(ctx) StatusView` (pure function over a directory snapshot plus the host snapshot):
  `executor` (`ok`, `missing`, `unknown`), `pending` requests (with age; older than 15s means
  "unclaimed"), recent results (last 20, newest first), `audit` (last `audit_lines`, only within
  `audit_retention`, tolerant of a corrupt line, reads at most the last 256 KiB), `autorestore`
  entries (DNS stop results with `restore_at` in the future and no later cancel/start result for the
  same `ref`).
- **Executor health** is derived from data we already have: the host snapshot reports
  `meridian-control.path` active or not (the installer adds it to `observe.json`), and an unclaimed
  request older than 15s forces `missing`. No spinner can outlive that: after 90s a pending request
  renders as "still running, see the audit log" (`control.action_timeout`) and keeps polling.
- `Available(cfg) (bool, reason string)`: false with a reason when the inbox/outbox directories are
  not mounted or not writable, true otherwise. `/api/status.control` is built from it, so Phase 1
  deployments (no mounts) correctly say "control plane not installed".
- Routes (added to the gateway table): `POST /api/control` body `{action,target,ack,ref?}` returns
  `202 {"id": …}`, or `400/409/429/503` with `{"code": …, "sentence": …}` (decision #86);
  `GET /api/control/status` returns the view.
  The POST goes through `browserGuard` (T14). It is the **only** mutating route.
- No result waiting inside the request: POST returns at once. The browser polls status.

**Tests first**: with real temp directories (no mocks): enqueue produces exactly one file in
`queue/` and nothing left in `tmp/`; failure between write and rename leaves nothing in `queue/`;
queue-full; duplicate pending request; unknown target/action from config; wrong ack; status with
hand-written results/audit files including a corrupt line, a 300 KiB audit file, a result in
progress, an unclaimed old request, a stale host snapshot; `Available` false when a directory is
missing.

**Fuzz**: `FuzzEnqueueInput` (random strings never produce a file whose contents fail
`wire.ValidateRequest`, and never write outside the inbox).

## T27 `meridian-host exec`: core

Package `internal/hostexec`, **stdlib only**. Runs as root via sudo. Treat everything under the
control directory as hostile.

**Trusted configuration** (`/etc/meridian/host.json` and `targets.json`), loaded by `loadTrusted`:
`Lstat` + open with `O_NOFOLLOW`, regular file, owned by uid 0, `mode & 0o022 == 0`, size at most 256
KiB, strict decode, full validation. **Any failure refuses to run at all** (exit non-zero, message to
the journal, nothing executed). `host.json` holds `{control_dir, deploy_user}`; `targets.json` is the
shape in CONTROL.md. Validation: target ids are slugs; `kind` in `systemd|compose|container|host`;
`actions` a subset of the known verbs; `unit` names match the unit-name grammar and `service` and
`container` names match Docker's (no leading dash, `[A-Za-z0-9][A-Za-z0-9_.-]*` — a real container
name has no unit suffix, so one grammar cannot cover both); `dir` absolute and clean; every `cmd` is a non-empty argv whose first element is an absolute path,
no element contains NUL; `run_as` is empty or equals `deploy_user`; `criticality` known;
`update_timeout_s` bounded 1..1800.

**Algorithm** for one invocation:
1. Load trusted config (above).
2. Take `flock(LOCK_EX|LOCK_NB)` on `/run/lock/meridian-control.lock`. If held, exit 0 quietly; the
   requests stay queued and the next path trigger processes them.
3. Prune results older than 7 days and audit lines older than 365 days (audit rewrite is
   `atomicfile` within the read-only-mounted outbox directory, so the container's view updates).
4. List `inbox/queue` through an `os.Root`; only names matching `^[0-9a-f-]{36}\.json$` are requests.
   Anything else is deleted and audited as `bad_request` under a fresh id, never the planted name
   (never opened). Sort by name (UUIDv7 sorts by time). Process one at a time, **re-listing** until
   empty, at most 64 per invocation.
5. For each file: open with `O_NOFOLLOW`, `Fstat` must be a regular file of at most 4 KiB, read, strict
   decode. **Never include file contents in an error, result or log.** Then in order:
   `wire.ValidateRequest`; id equals the filename stem; replay (a result for this id already exists:
   `replay`); stale (`requested_at` more than 120s old: `stale`); `unknown_target`;
   `action_not_allowed` (action not in the target's list; `cancel-autorestore` is implied by `dns: true`
   plus `stop`, not listed); ack equals `wire.RequiredAck(action, target.dns)`; for a cancel, the ref
   names a successful stop for this target whose restore has neither fired nor been cancelled
   (`bad_ref`, T29); **coalescing**: a prior request for the same `(target, action)` started within 60s
   gives `ok: true, code: coalesced, note: "coalesced with <id>"` and runs nothing (decision #37).
6. Build the command (T28), run it, build the `Result`.
7. **Every exit path writes a result** (`defer` + `recover`; a panic is `internal`), appends one audit
   line, and only then deletes the request file. Result written with `atomicfile` to
   `outbox/results/<id>.json` (0644, root-owned). Audit lines are appended with `O_APPEND` and one
   `write` call each, then `fsync`.

**Pure core:** `Decide(req wire.Request, cfg Targets, history []wire.Result, now time.Time) Decision`
(accept with the target and action, or refuse with a code) holds steps 5's checks and is the main
table-driven and fuzzed surface. Everything else is I/O around it.

**Tests first**, refusals before happy paths (these are the CONTROL.md "adding a target" step 5
checks, automated): unknown target; unknown action; action not in target's list; stale; replay;
missing and wrong ack; coalesced; a request file that is a symlink (to a secret file), a FIFO, a
directory, oversized, not JSON, JSON with unknown fields, trailing garbage, NUL bytes, a file named
`../x.json`; `targets.json` that is group-writable, not root-owned (use an injectable `Owner`
function for tests), a symlink, or invalid; flock already held; 100 queued files (cap of 64, rest left
for the next run); results always written; request file always deleted; contents of a secret file
never appear in any output.

**Fuzz:** `FuzzDecide` (arbitrary requests against arbitrary valid configs: **accepts only if every
documented condition holds**, checked against a deliberately dumb reference implementation in the
test), `FuzzRequestFile` (arbitrary bytes through the whole pipeline with a fake `Commander`: the
commander is called only for requests that decode, validate and are allowlisted; a result is always
written; never panics), `FuzzLoadTargets`.

## T28 Runners

The command builders are **pure functions from allowlist data to argv**. This is the most important
property in the project, so it is a table, not code branches:

```go
type Command struct {
    Argv        []string
    RunAs       string        // "" = root, otherwise the deploy user
    Dir         string
    Timeout     time.Duration
    ResultFirst bool          // write a provisional result before running (self-restart, reboot)
    Verify      *Verify       // optional post-check, e.g. is-active must say "active"
}
type Verify struct { Argv []string; Want string }   // a word, not an exit code
type BuildInput struct { Target Target; Action wire.Action; ID, Ref, DeployUser string }
type Builder func(in BuildInput) (Command, error)
var Builders = map[BuilderKey]Builder{ {Kind: "systemd", Action: "restart"}: …, … }
var updateBuilders = map[string]Builder{ UpdateUnit: …, UpdateCommand: …, UpdateDocker: … }
```

The update row looks in `updateBuilders` for its second dimension: the target's own `update.kind`.
`Target.ResultFirst` (`"result_first": true`) is how the allowlist says "this target is the dashboard,
its actions can kill the container".

| Kind / action | Argv (all pieces come from the allowlist or are the validated id) | Runs as |
|---|---|---|
| systemd start/stop/restart | `systemctl <verb> <unit>`; verify `systemctl is-active <unit>` (start/restart must report active within 10s, stop must report inactive) | root |
| compose start/stop/restart | `docker compose --project-directory <dir> <verb> <service>` | deploy user |
| container start/stop/restart | `docker <verb> <name>` | deploy user |
| update `unit` | `systemctl start <unit>` (blocking: those units are `Type=oneshot`) | root |
| update `command` | the `cmd` argv exactly as written | deploy user |
| update `docker` | `docker compose --project-directory <dir> up -d --pull always <service>` (one command: pull and recreate together) | deploy user |
| host reboot | `systemd-run --on-active=10 --unit=meridian-reboot-<id> systemctl reboot`, `ResultFirst` | root |
| meridian restart/stop/update | as systemd / unit, `ResultFirst` | root |

Rules:
- No builder reads the request except the validated `id`. A property test (below) enforces that every
  argv element is from the allowlist, a fixed verb/flag in the table, or the id.
- **Real execution** (`Commander`, the only `os/exec` use in the repo besides `hostprobe`): `exec.CommandContext`
  with `SysProcAttr{Setpgid: true, Credential: …}` for deploy-user commands (uid, gid and
  supplementary groups from `os/user` at startup, groups including `docker`), `Cancel` killing the
  whole process group, `WaitDelay` set, explicit environment `PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin`,
  `HOME`, `LANG=C.UTF-8`, nothing inherited. Stdout and stderr are captured with a size cap
  (the tail 4 KiB goes in the result, the rest to the journal).
- Default timeout 120s; `update` actions use `update_timeout_s` or 600s. A timeout is `timeout`, not a
  hang. The per-command `Timeout` and the unit's `TimeoutStartSec` (T31) must agree: the unit's is the
  larger.
- `ResultFirst`: write a provisional `Result{InProgress: true, OK: true, Note: "in progress"}` before
  running (so a container that dies in the middle, or a reboot, still leaves a story), then overwrite
  atomically with the final result. If finalising fails after a self-restart the provisional
  result stays and the audit line is still appended.

**Tests first**: for every (kind, action) in the allowlist grammar, assert the exact argv, run-as and
timeout (golden table); verify-step failure gives `not_active_after`; a fake `Commander` records calls
and returns scripted output/exit codes/timeouts; one real execution test using `/bin/sh`-free binaries
that exist everywhere (`/bin/true`, `/bin/false`, `sleep` via argv) to prove timeouts kill the process
group (a child that ignores TERM) and that the environment is minimal (`env` output equals the
expected set). Credential switching is tested only when running as root in CI (`t.Skip` otherwise,
with a reason).

**Fuzz:** `FuzzBuildArgv` (random valid allowlists and ids: **every argv element is accounted for** by
the allowlist, the verb table or the id; no element starts with `-` unless it is a fixed flag from the
table).

## T29 DNS guard, reboot, self-restart

- **DNS stop** (`dns: true` target): requires ack `dns-stop-confirmed`. After the stop succeeds, schedule
  `systemd-run --on-active=10min --unit=meridian-autorestore-<id> <the start argv for the target>`.
  If **scheduling fails, immediately start the target again** and report `restore_failed` with
  `ok: false`: the one button that can cause a second outage must fail safe. The result carries
  `restore_at`.
- **Cancel**: `action: cancel-autorestore`, `ref` = the stop request's id. `Decide` checks that `ref` is
  a UUID, a result with that id exists for the same target with `restore_at` set, and the restore has
  not already fired or been cancelled (a prior cancel result for that ref). The builder produces
  `systemctl stop meridian-autorestore-<ref>.timer`.
- **Reboot**: target `host`, action `reboot`, ack `reboot-confirmed`, builder as in T28 with
  `ResultFirst`. If the final result write fails after scheduling, cancel the reboot timer.
- **Self-restart / stop / update of Meridian**: `ResultFirst`, so the result exists before the
  container dies. The UI reconnects (T30) and finds the result by id.
- Audit lines for all of these, including `restore_at` in the note text: the note leads the audit
  line's `output`, because a result file is pruned after seven days and the audit line is what a year
  of actions is read from.

**Tests first**: stop AdGuard-like target schedules exactly one restore with the right delay and
argv; scheduling failure starts the target and reports `restore_failed`; cancel for a valid ref, an
unknown ref, a ref from another target, a ref already cancelled, a ref that is not a UUID; reboot
result exists before the (fake) timer fires; no `shutdown`/`poweroff` verb exists in any builder
(invariants test: the strings do not occur in `hostexec`).

**Fuzz:** `FuzzDecideCancel` (arbitrary refs and result histories: accepted only when every condition
holds).

## T30 Control UI

Make the T18 page live. Pure state machines first.

- `src/lib/state/armed.ts`: reducer for the two-step button. States `idle -> armed(expiresAt) ->
  sending -> pending(id) -> done(result)`. Events `tap`, `expire`, `sent`, `result`, `cancel`. Arming
  lasts 5s (injected clock). **Property (fast-check): a state that emits a `send` effect for stop or
  reboot is always reachable only through `armed` within the window.** Single-tap actions (`start`,
  `restart`, `update`) skip `armed`.
- Destructive buttons are outlined, never filled; once armed, **Cancel is the wider, primary button**
  (DESIGN "Control panel"). Armed copy states the consequence ("Stop AdGuard Home? It has been running
  12d. DNS stops for the whole tailnet.") and is announced via `aria-live`.
- One action at a time globally: while one is pending every other action button is disabled with the
  reason. Update buttons with `update_disabled_reason` render disabled and show it.
- Banners driven by `GET /api/control/status` polled every 1s while something is pending, otherwise
  every 10s: **No executor** (`executor.state == "missing"`, queued requests show `pending (unclaimed)`);
  DNS **auto-restore countdown** with a Cancel button (a normal request with `action:
  cancel-autorestore`); "Rebooting, back in about 40s" with reconnect that polls `/healthz` with
  backoff 2s, 4s, 8s up to 30s and then re-renders the launcher state; same reconnect for a Meridian
  restart, finding the finished result by id.
- Action history = the audit lines, newest first, from the status response.
- Every string follows the copy rules (no em dashes, no exclamation marks).

**Tests first**: reducer table and properties; component tests for each mockup state
(`control-normal`, `control-armed-stop`, `control-no-executor`, `control-reboot`); countdown rendering
from `restore_at` with a fake clock; Playwright additions against the stub upstream plus a **stub
executor** (a small Go helper in `testdata/stub` that consumes the inbox like the real one would, so the
full round trip is exercised without root).

## T31 Units, sudoers and installer pieces

Under `deploy/`, templated with `@…@` placeholders filled by the installer (T36). The unit, sudoers,
allowlist and observe examples are tested here; the installer only fills in the placeholders and
appends `meridian-control.path` to `observe.json`.
- `systemd/meridian-control.path`: `DirectoryNotEmpty=@CONTROL_DIR@/inbox/queue`, `Unit=meridian-control.service`.
- `systemd/meridian-control.service`: `Type=oneshot`, `User=@DEPLOY_USER@`,
  `ExecStart=/usr/bin/sudo -n /etc/meridian/meridian-host exec`, `TimeoutStartSec=660`,
  `StartLimitIntervalSec=0` (the `.path` unit can fire faster than a oneshot finishes; the default
  start limit would silently disable it, the exact failure that cost Polaris a day), `NoNewPrivileges`
  is **not** set here because sudo needs to elevate.
- `sudoers/meridian`: exactly one line, `@DEPLOY_USER@ ALL=(root) NOPASSWD: /etc/meridian/meridian-host exec`
  (no wildcard, no extra arguments), validated with `visudo -cf` in the installer.
- `targets.example.json` (generic) and `examples/targets.homelab.json` (the CONTROL.md matrix for this
  box). Both load with `hostexec.LoadTargets` in a test.
- Compose gains the inbox (rw) and outbox (ro) mounts only: `control/inbox:/control/inbox`,
  `control/outbox:/control/outbox:ro`. The compose invariants test (T23) is extended to allow exactly
  these and nothing wider.
- `observe.json` generation adds `meridian-control.path` automatically so executor health is visible.
  For now `deploy/observe.example.json` is the list the probe must be given; T36's installer appends
  it, and `TestDeploy_TheObserveListWatchesTheExecutor` checks the shape the probe reads.

**Tests**: a Go test that parses the unit files and asserts the directives above (a data-driven list of
`(file, key, expected)`); sudoers has no `*` and a single command; both JSON examples load clean.

## T32 `[human]` Phase 2 on the potato

Owner steps (PLAN Phase 2 "done when"): install `/etc/meridian/*` and the units with the installer,
start with **bwmon restart only**; measure and record restart latency in `docs/CONTROL.md`; stop
`meridian-control.path` and confirm the "No executor" banner replaces any spinner; hand-write request
files for an unknown target, an unknown action and a stale timestamp and confirm refusal in the audit
log; `docker inspect` confirms the container sees only `control/inbox`, `control/outbox` and the
read-only status and config mounts.

## T33 `[human]` Phase 3 on the potato

Owner steps (PLAN Phase 3): enable the full matrix; capture Vaultwarden's `docker run` spec as compose
in the homelab repo, migrate once, confirm the database untouched, then enable its update; perform
every action once and read the audit log; confirm updaters leave no root-owned files
(`find … -user root`); prove the AdGuard auto-restore by stopping it and doing nothing; reboot from a
phone and confirm the page returns without a manual reload; confirm audit pruning removes only lines
older than 365 days.

**Stage done:** every non-`[human]` task ticked; `scripts/check all` green; the fuzz-smoke job covers
`FuzzDecide`, `FuzzRequestFile`, `FuzzBuildArgv` and the wire fuzzers; an end-to-end Playwright run
through the stub executor completes a restart, an armed stop with countdown and cancel, and a reboot
reconnect.
