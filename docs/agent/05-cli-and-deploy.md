# Stage 5: CLI and Phase 1 deploy

Goal: everything from [PLAN.md](../PLAN.md) Phase 1 that can be built and tested off the box, plus
the exact steps for the part that cannot. Decisions #9, #26, #27, #41, #57, #58.

## T20 `meridian run`, `healthcheck`, `version`

Package `internal/cli` (cobra). `cmd/meridian/main.go` stays thin.

- `meridian run [--config PATH] [--watch]`. Config path order: flag, `MERIDIAN_CONFIG`, then
  `/config/config.yaml`. Steps: load and validate (fatal on failure at startup, with every problem
  printed), build the real `probe.Env`, store and scheduler, start the gateway, block until
  SIGINT/SIGTERM, then drain gracefully.
- **Reload** (decision #41): `SIGHUP`, or with `--watch` a 2s mtime poll, re-runs `config.Load`. On
  success: `scheduler.Reload`, swap the gateway's config pointer atomically, set `config.ok = true`.
  On failure: **keep the previous config serving**, log every problem, set `config.ok = false` and a
  short error for the banner. A later good reload clears it. Signals reach the code through an injected
  `<-chan os.Signal`, so tests drive reloads without real signals.
- **Host snapshot reader**: `Env.Host` re-reads `host_status.file` when its mtime changes (size cap 64
  KiB, via `wire.ParseHostSnapshot`) and returns the last good parse; a missing or corrupt file means
  `(zero, false)`. Pure parse, thin I/O.
- `meridian healthcheck [--config PATH]`: reads `listen` from the config (same path order as `run`),
  then `GET http://<listen>/healthz` with a 2s timeout and exits 0 only on `200` and `"status":"ok"`.
  An unspecified address (`0.0.0.0`, `[::]`) is asked on loopback instead. The distroless image has no
  `curl`, so this is what Docker's healthcheck runs.
- `meridian version`: version, commit, build date, `GOOS/GOARCH`.
- Logging: `charmbracelet/log`, level from `MERIDIAN_LOG` (default `info`, and a value it cannot parse
  also means `info`), handed to the gateway and the scheduler as a stdlib logger so everything has one
  destination and one level. One line per reload, one per state transition of a service
  (`ivy: ok -> down (timeout)`, from `probe.TransitionLine`), nothing per request beyond the gateway's
  access log, never secrets.

**Tests first** (integration, using the portable fixture and `httptest` upstreams): start `Main` in a
goroutine, wait for `/healthz`, assert `/api/status`, edit the config file and send a reload (adding a
service shows up without a restart); a broken reload leaves the old config serving and sets the
banner; shutdown returns promptly with no goroutine leak; `healthcheck` exit codes against a live and a
dead port.

**Fuzz**: none new (parsers already covered); the reload path is exercised by the config fuzzer.

## T21 `meridian doctor`

Package `internal/doctor`. Answers "why is my config wrong". **Read only**: it must not import
`control` and must not write files (invariants test).

```go
type Check struct { Name, Code, Detail, Hint string; Level Level } // Level: OK, Warn, Fail
type ServiceReport struct { ID, Name string; Checks []Check }
type Report struct { Config []Check; Services []ServiceReport }
func (r Report) Failed() bool                                   // decides the exit code
func Run(ctx context.Context, cfg *config.Config, env probe.Env, allow *Allowlist) Report
func InvalidConfig(err error) Report                            // a config that did not load, explained
func InvalidAllowlist(err error) Report
func ParseAllowlist([]byte) (*Allowlist, error)                 // names and actions; the rest is the executor's
func Render(r Report) string                                    // pure and stable: one line per check
```

`Hint` is not optional in spirit: every `Warn` and `Fail` carries one sentence of what to do next, from
a table keyed by reason code, so a failure is never a dead end. `Render` prints the level, the name,
the detail and the code in brackets, with the hint under it, and a last line with the counts. A probe
failure keeps the probe's own code, so the page, the log and `doctor` name the same cause.

- Checks per service: referenced env vars present (**names only**, never values); each probe run **once
  with the real runners** from `probe`, so doctor and the page can never disagree; the click-through
  URL is syntactically valid; vitals each run and reported.
- Table-driven hints keyed by reason code: `bad_status` with 403 gives "the service may check the
  `Host` header; probe URLs normally use `localhost`" (generic, no service named); `refused` gives "is
  anything listening on that port from inside the container (is `network_mode: host` set)"; `timeout`,
  `unit_not_reported`, `host_stale` each get one sentence.
- Allowlist check (decision #15 keeps `/etc/meridian` out of the container): `--allowlist FILE|-` reads
  JSON shaped like `targets.json` (target names and actions only matter). On the host:
  `meridian-host targets | docker exec -i meridian meridian doctor --allowlist -`. For each `control:`
  block, report a missing target, an action the allowlist does not permit, and the reverse (allowlist
  target no config mentions) as a warning. Without the flag, the control section prints
  "allowlist not checked" as a warning, not a failure.
- Exit code 1 if any `Fail`, else 0. Output groups by service, one line per check, a `Fail` always
  carries a reason and a hint.

**Tests first**: the three deliberately broken configs from PLAN (an unknown probe kind, a control
target missing from the allowlist, a URL that does not answer) each produce exactly the expected
`Check` codes; `Render` golden file; no secret value ever appears in output (set a distinctive fake
secret in the env and grep the rendered report).

**Fuzz**: `FuzzParseAllowlist` (arbitrary JSON: no panic, errors typed).

## T22 `meridian-host probe` and its units

Package `internal/hostprobe`, stdlib only, called from `cmd/meridian-host`. Runs as the **deploy
user**, not root: `systemctl show` and `tailscale status --json` do not need it.

```go
type Deps struct {
    Run      func(ctx context.Context, argv []string) ([]byte, error) // seam: command output
    ReadFile func(string) ([]byte, error)
    Now      func() time.Time
    Uptime   func() (time.Duration, float64, error)                  // reads /proc
}
func Collect(ctx context.Context, d Deps, units []string) wire.HostSnapshot
```

- Unit list = union of `unit` fields from `/etc/meridian/targets.json` (when present) and
  `/etc/meridian/observe.json` (`{"units": ["…"]}`, how Phase 1 watches units before any control
  plane exists; decision #61). Both are read with the strict, size-capped, root-ownership-checked
  loader shared with `hostexec` (`internal/hostfile`). Each name must pass `wire.IsUnitName`:
  `^[A-Za-z0-9:_.@-]+\.(service|timer|socket|target|path)$`, at most 255 characters, and never a
  leading `-`; anything else is dropped with a log line (decision #79).
- Per unit: `systemctl show <unit> -p ActiveState -p ActiveEnterTimestamp`, parsed as `key=value`
  lines. `active` is `ActiveState == "active"`. A unit that fails to show is **omitted**, so the
  container reports `unit_not_reported` rather than guessing.
- Tailscale: `tailscale status --json` parsed leniently: `Self.Online`, `Self.ExitNodeOption`,
  peers online and total from `Peer`. Failure leaves `tailscale` absent from the snapshot.
- Uptime and load from `/proc/uptime` and `/proc/loadavg`.
- `meridian-host probe --out PATH` writes the snapshot with `atomicfile.Write` (mode 0644). One
  failed source never prevents writing the rest. Exit non-zero only if the file could not be written.
- `deploy/systemd/meridian-probe.service` (`Type=oneshot`, `User=@DEPLOY_USER@`, `NoNewPrivileges`,
  `PrivateTmp`, `ProtectSystem=strict` with `ReadWritePaths=@STATUS_DIR@`, `ProtectHome` as the box
  allows) and `meridian-probe.timer` (`OnBootSec=15s`, `OnUnitActiveSec=30s`). The installer
  (T31/T36) fills the `@…@` placeholders.

**Tests first**: parsers against fixture outputs of `systemctl show` (active, inactive, failed,
not-found, extra fields, CRLF) and `tailscale status --json` (online, offline, no peers, an unknown
newer schema with extra keys); `Collect` with a fake `Run` where some commands fail; unit-name
filter table including `--help`, `../x`, empty and very long names; output file appears atomically.

**Fuzz**: `FuzzParseSystemctlShow`, `FuzzParseTailscaleStatus`, `FuzzUnitName` (accepted names pass
the grammar, never start with `-`, contain no whitespace).

## T23 Dockerfile, compose and image CI

- `Dockerfile` (multi-stage): Node stage builds the frontend with `pnpm install --frozen-lockfile`;
  Go stage (`--platform=$BUILDPLATFORM`, cross-compiling with `GOARCH=$TARGETARCH`, `CGO_ENABLED=0`)
  embeds `web/build` and builds `-trimpath -ldflags "-s -w -X …version"`; final stage is the current
  distroless `static` `nonroot` image. Base images are **pinned by digest** (look the digests up when
  you write it; do not invent them); Dependabot keeps them fresh. `ENTRYPOINT ["/meridian","run"]`.
  The image contains the binary and nothing else.
- `deploy/docker-compose.yml`: `network_mode: host`; `user: "${MERIDIAN_UID}:${MERIDIAN_GID}"`;
  `read_only: true`; `tmpfs: /tmp`; `cap_drop: [ALL]`; `security_opt: [no-new-privileges:true]`;
  `restart: unless-stopped`; `pids_limit` and `mem_limit` set; `env_file: .env`; healthcheck
  `["CMD","/meridian","healthcheck"]`; volumes exactly `config.yaml:/config/config.yaml:ro`,
  `icons:/config/icons:ro`, `status:/status:ro`. **No `ports`** (host mode), no `docker.sock`, nothing
  from `/etc/meridian`, no repo mount. The control mounts arrive in T31, not here.
  Image reference comes from `${MERIDIAN_IMAGE}` in `.env` so Phase 4 can pin by digest.
- `deploy/.env.example` with the variable names only.
- **Compose invariants** are added to the invariants test: parse `deploy/docker-compose.yml` with the
  YAML dependency and assert every property above, plus that the set of volumes is a subset of an
  allowlist in the test. The most expensive mistakes in this design are one-line compose edits, so
  they get a test.
- `ci.yml` `image` job: `docker buildx build` for `linux/amd64` and `linux/arm64` (no push), Trivy scan
  failing on HIGH and CRITICAL, then run the amd64 image against `testdata/portable/config.yaml`
  with the stub upstream and assert `/healthz` and `/api/status` through `meridian healthcheck`
  (`scripts/smoke-image`, so the step is shellcheck'd, readable and can be run by hand).

**Done when**: `docker build` works locally and in CI for both architectures, the smoke test passes,
and the compose invariants test is green.

## T24 `[human]` Phase 1 on the potato

The agent writes nothing here except to confirm the earlier tasks are ticked. The owner does the
following, mirroring the Phase 1 "done when" in PLAN.md. Do not tick this task on the owner's behalf.

1. Build and publish the image (T23 CI), pull on the box, install `meridian-host` to
   `/etc/meridian/` root-owned, write `/etc/meridian/observe.json` for the units to watch, install and
   enable `meridian-probe.timer`, and start the container with the compose file.
2. `docker exec meridian curl` is **not** available (distroless); verify probe URLs from inside the
   container with `docker exec meridian /meridian doctor`, which uses the same code and the same
   network namespace. This replaces the PLAN's `docker exec … curl` check.
3. Every service shows a correct dot, including the down case (stop one on purpose).
4. Idle backoff: with no page open for 10 minutes the service log shows probes every 2 minutes; a fresh
   load shows current values.
5. `meridian doctor` explains the three deliberately broken configs (see T21).
6. Adding a service is a config edit plus `kill -HUP`; a broken edit leaves the old config serving.
7. Ivy's vital matches the app's own needs-attention count; no probe shows as an error in a service's
   own logs.
8. Bound to loopback, reachable at `https://lepotato.taild95077.ts.net:8891` through `tailscale serve`
   with that name in `server.allowed_hosts`.

**Stage done**: `scripts/check all` green; the image builds for both architectures; `meridian run`
against the portable fixture behaves exactly as the Playwright suite expects.
