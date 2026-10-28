# Review brief

A whole-codebase review is easier to do well when someone has already marked what is load-bearing.
This document lists the constraints that define the design, the trust boundaries where untrusted
bytes cross into the process, the decisions most likely to erode, the known sharp edges and the lanes
a review can be split into. It describes the tree at the commit that added it, and every claim here
either links to the doc line that states it or names the test that enforces it.

Two rules for the reviewer:

- **The code wins over this document.** If a claim here and the code disagree, the code is what
  ships; report the doc as a defect.
- **Run the machine copy first.** `go test ./internal/invariants -v` prints every hard constraint with
  the fixture that deliberately breaks it. A rule that cannot fail is not enforcing anything, and
  that test proves each one can.

The short version of what this box is: a read-only launcher for one homelab, a `/control` page whose
buttons become request files, and a stdlib-only root-owned host binary that turns an allowlisted
request into an argv array. The container is the untrusted side; the allowlist is the boundary
([DECISIONS.md](DECISIONS.md) #14, #15, #52, #62, #63).

## 1. Hard constraints and what enforces each

The constraint list is [CLAUDE.md](../CLAUDE.md) "Hard constraints". The invariants test
([internal/invariants/invariants_test.go](../internal/invariants/invariants_test.go),
[compose_test.go](../internal/invariants/compose_test.go),
[deploy_test.go](../internal/invariants/deploy_test.go)) is its mechanical copy. The table below pairs
each constraint with a hand check and the rule or test that fails the build.

| Constraint | Check by hand | Mechanical enforcement |
| --- | --- | --- |
| **Zero service-specific knowledge** (#38) | `git grep -niwE 'polaris\|ivy\|bwmon\|adguard\|searxng\|vaultwarden\|unbound\|her-go' -- internal web/src`: hits are expected only in `_test.go` files, `internal/invariants/testdata/`, the host-side packages, and CSS comments. Then add a service to `examples/homelab.yaml` and confirm nothing in Go or Svelte changes | Invariants rule **"no service names in code"** (`checkNoServiceNames`), which derives its wordlist from the shipped example config, not from a constant. The vocabulary tests in [internal/config/kinds_test.go](../internal/config/kinds_test.go) and [internal/probe/registry_test.go](../internal/probe/registry_test.go) hold the tables, the runners and the docs together, and [internal/gateway/status_test.go](../internal/gateway/status_test.go) builds the contract from the portable fixture |
| **The container never gets the Docker socket, root, or arbitrary command execution** (#14, #15) | Read `deploy/docker-compose.yml` "volumes" and `deploy/sudoers/meridian`: one sudoers line, one exact command, no wildcard, no `/etc/meridian` mount, no `docker.sock` | Invariants rule **"no shell execution"** (`checkNoShell`: no `sh`/`bash`/`-c`), **"os/exec is confined to the host side"**, **"the compose file keeps the container's privilege boundary"** (`checkComposeInvariants`, with fixtures for a Docker socket, a published port, a bridge network and a writable outbox), and `TestDeploy_SudoersIsOneExactCommand` |
| **The container never runs `systemctl` or reads the tailscaled socket** (#27) | `git grep -n 'systemctl\|tailscaled.sock' -- internal cmd`: every hit outside `cmd/meridian-host`, `internal/hostexec` and `internal/hostprobe` must be a comment, never a string or an identifier. Read `deploy/systemd/meridian-probe.service` and confirm it runs as the deploy user | Invariants rule **"systemctl and Docker literals stay on the host side"** (`checkHostLiterals`) and **"os/exec is confined to the host side"** (`checkExecConfinement`); `TestDeploy_TheControlUnitsSayExactlyWhatTheDesignSays` pins the unit contents |
| **No authentication, ever** (#10) | Read [internal/gateway/middleware.go](../internal/gateway/middleware.go): the guards are `Host`, `Sec-Fetch-Site`/`Origin` and content type, not identity. There is no cookie, token, session or user store anywhere | Invariants rule **"no authentication surface"** (`checkNoAuthSurface`: no `cookie`, `csrf`, `login`, `password` or `session` identifier or string in `internal/control` or `internal/gateway`) |
| **No state-changing buttons on the launcher** (#34) | Open the launcher in devtools: the only request it makes is `GET /api/status` (and `/api/control/status` on `/control`). A down tile links to `/control#<id>` | Invariants rule **"only POST /api/control mutates"** (`checkOneMutatingRoute`: the only non-GET/HEAD route in `internal/gateway` is `POST /api/control`); the Playwright test `a service that goes down offers a way into its control row` asserts the link, not an action |
| **Probe URLs are not click-through URLs** | Compare `probe.url` with `url` in [config.yaml.example](../config.yaml.example); read the Ivy section of [INTEGRATIONS.md](INTEGRATIONS.md). The trap: Ivy 403s on the tailnet IP as `Host`, so the probe must use loopback while the tile links elsewhere | **Not mechanised.** Config validation only requires each URL to be http(s), and the probe URL to carry no credentials (`internal/config/validate.go`). This is a review item, not a build gate |
| **Never write a handoff JSON in place** (#36) | `git grep -n 'os.WriteFile' -- internal/control internal/hostexec internal/hostprobe` returns nothing; every write goes through [internal/atomicfile](../internal/atomicfile/atomicfile.go) (`WriteVia`/`WriteRoot`) | Invariants rule **"no in-place JSON writes"** (`checkNoInPlaceWrites`) |
| **No Node, no Python at runtime** (#8, #52) | Read the final stage of [`Dockerfile`](../Dockerfile): distroless, one static binary, no shell, no package manager, no interpreter; the frontend is embedded by `web/embed.go`. `meridian-host` is a separate checksummed release asset, never inside the image (#67) | Invariants rule **"the host side is standard library only"** (`checkHostStdlib`) proves `meridian-host` and the packages it shares with the container import nothing outside the standard library. The distroless image itself is a review item |
| **No dependencies off the allowlist** ([STANDARDS.md](STANDARDS.md) section 9) | `git diff go.mod web/package.json` in any change; a new module needs a new [DECISIONS.md](DECISIONS.md) entry | Invariants rule **"dependencies are on the allowlist"** (`checkDependencyAllowlist`) checks `go.mod`'s direct requirements. **`web/package.json` is not mechanically checked**: it is a review item |
| **Tests first, small commits, every commit green** | `git log --oneline` shows small Conventional Commits with tests beside the code | Not mechanisable. The pre-commit hook runs `scripts/check --fast`; the pre-push hook runs the full check |
| **Decisions are appended, never edited away** | `git log -p docs/DECISIONS.md`: additions only, and a reversal is a new numbered entry linking back | Not mechanisable |
| **A remote shutdown is unrecoverable, so the executor may only reboot** (#18) | Read the `(kind, action)` table in [internal/hostexec/builders.go](../internal/hostexec/builders.go) | Invariants rule **"the executor can schedule a reboot but never a shutdown"** (`checkNoShutdownVerb`) |
| **`doctor` explains, it never changes anything** | [internal/doctor](../internal/doctor/) imports neither `internal/control` nor `internal/atomicfile` and calls no writer | Invariants rule **"doctor is read only"** (`checkDoctorReadOnly`) |
| **Hygiene that keeps the rest reviewable** | `grep` for `os.Exit`/`log.Fatal` outside `cmd/` | Invariants rule **"os.Exit and log.Fatal live only in cmd/"** (`checkExitOnlyInCmd`) |

## 2. Trust boundaries

Six boundaries carry untrusted bytes into the privileged or semi-privileged code. For each: what is
untrusted, where it is validated, and the fuzz targets that hold the validator.

### Browser to gateway

**Untrusted:** every header the browser can set (`Host`, `Origin`, `Sec-Fetch-Site`,
`Content-Type`), the body of `POST /api/control`, every query parameter, and the ids and action names
inside the JSON. **Where it is validated:** `HostAllowed`/`HostCheck` in
[internal/gateway/middleware.go](../internal/gateway/middleware.go) decide whether the request is for
this app at all; the body is capped at 8 KiB (`MaxRequestBody`); `internal/control` decodes strictly
and checks the request against config policy before a file exists; `wire.ValidateRequest` judges
structure and `wire.DecodeRequest` refuses unknown fields. This is a browser guard, **not
authentication** (#54). **Fuzz targets:** `FuzzHostCheck`, `FuzzDecodeRequest`, `FuzzEnqueueInput`,
`FuzzDecide`. **A regression looks like:** a route that accepts a body without the guard, a `Host`
comparison that does `strings.Contains`, or an allowlist entry with a port accepted for a request
without one.

### Probe to upstream

**Untrusted:** HTTP status lines, headers, bodies and JSON from each service, DNS packets from the
resolver, and the shape of whatever a service returns at a URL the config named. **Where it is
validated:** [internal/probe/fetch.go](../internal/probe/fetch.go) builds a request for http(s) only,
refuses credentials on a URL that has them, follows no redirects, has no cookie jar or environment
proxy, caps the body at 1 MiB and puts a deadline on everything (#55). Health and vital evaluators
then work on decoded JSON through [internal/jsonpath](../internal/jsonpath/jsonpath.go), which is
typed and bounded. **Fuzz targets:** `FuzzBuildRequest`, `FuzzHTTPJSON`, `FuzzVitals`,
`FuzzDNSResponse`, `FuzzGet`, `FuzzEqual`, `FuzzNumber`. **A regression looks like:** a probe that
follows redirects "to be helpful", a `http.Client` constructed somewhere else, or a vital read that
casts decoded JSON instead of asking `jsonpath`.

### Container to inbox

**Untrusted:** everything in `control/inbox`, because the container can write there and may be
compromised. Names may be symlinks, FIFOs, directories, oversized or undecodable; there may be junk
the container never wrote. **Where it is validated:** [internal/hostexec/exec.go](../internal/hostexec/exec.go)
opens the inbox as an `os.Root`, reads entries with `O_NOFOLLOW|O_NONBLOCK`, requires a regular file,
caps it at `wire.MaxRequestSize` and never puts file contents in an error or an audit line (#62).
Junk is deleted under a fresh id and audited. **Fuzz targets:** `FuzzRequestFile`,
`FuzzDecodeRequest`, `FuzzParseID` (ids become file names). **A regression looks like:** an
`os.ReadFile(path)` built from a queue entry, a missing `O_NOFOLLOW`, or a result written under a name
taken from the request.

### Inbox to executor

**Untrusted:** the request *sequence*: replays, duplicates, stale timestamps, a missing or wrong
`ack`, a `cancel-autorestore` whose `ref` points at nothing, requests for targets or actions the
allowlist does not contain. **Where it is validated:** [internal/hostexec/decide.go](../internal/hostexec/decide.go)
is the pure policy core: it is checked against a deliberately dumb reference in the fuzz test, and
its refusals are a `*wire.Refusal` from the closed code table (#81, #85). `loadTrusted` re-reads
`targets.json` and refuses to run unless it is root-owned, regular and not group/world-writable
(#62). **Fuzz targets:** `FuzzDecide`, `FuzzDecideCancel`, `FuzzLoadTargets`, and
`FuzzParseAllowlist` (the doctor parser that renders the same file). **A regression looks like:** a
coalescing rule that swallows a failed prior action (so a retry does nothing), a `ref` check that
compares only the target, or policy moving from `Decide` into a command builder.

### Executor to subprocess

**Untrusted:** allowlist fields that end up in argv (`unit`, `dir`, `service`, `container`, a command
update's argv), the id and ref from the request, and the self-update signal. **Where it is validated:**
every command is a row of the `(kind, action)` table in
[internal/hostexec/builders.go](../internal/hostexec/builders.go), so the request only *selects* a
row (#84). Unit and container names use `wire.UnitName` / the Docker name grammar; a docker image
reference must match the allowlisted repository plus a `sha256:` digest (#88); a `.env` rewrite is
`SetVar`, which refuses newlines (#88). Subprocesses get an explicit minimal environment, an explicit
credential for the deploy-user cases, a context deadline and a process-group kill (#31, #89). **Fuzz
targets:** `FuzzBuildArgv`, `FuzzUnitName`, `FuzzImageRef`, `FuzzSetVar`, `FuzzManifestHeader`,
`FuzzParseDigest`, `FuzzParseImageRepo`. **A regression looks like:** an argv element interpolated from
request text, a table row that takes a flag from the config, or a command with no timeout.

### Host probe to snapshot

**Untrusted:** `systemctl show` output, `tailscale status --json`, `/proc` values, and the unit names
in `observe.json`/`targets.json`. **Where it is validated:**
[internal/hostprobe](../internal/hostprobe/) parses with a deliberately dumb reference in the fuzz
tests; names go through `wire.UnitName` (#79);
[internal/wire/wire.go](../internal/wire/wire.go) validates `host.json`, and the container side
refuses to show a dependant tile as green when the snapshot is stale (#27). **Fuzz targets:**
`FuzzParseSystemctlShow`, `FuzzParseTailscaleStatus`, `FuzzParseHostSnapshot`, `FuzzUnitName`. **A
regression looks like:** a `host.json` field read without a validator, a parser that trusts the first
line instead of the last, or a fresh-looking tile built from a stale file.

Two contract boundaries are not security boundaries but are still shared with the browser:
`/api/status` (`FuzzBuildStatus`, golden files under `testdata/api`) and `/api/control/status`
(`testdata/control`). Icon names (`FuzzIconName`) are the third: an accepted name is a file name in
one directory and can never name a path elsewhere.

## 3. Fuzz targets

This section is generated from the `Fuzz*` functions and the doc comment above each one. Regenerate it
with `go test ./internal/invariants -run TestReviewBrief -update`; a target added, renamed or
uncommented fails `scripts/check --fast` until you do. Edit the code, never the table.

<!-- fuzz-targets:begin -->
34 fuzz targets in 12 packages.

| Package | Target | Property asserted |
| --- | --- | --- |
| `internal/cli` | `FuzzMain` | FuzzMain holds the dispatch contract for any argument: it never panics, the exit code is one of the documented three (0 ok, 1 failed while doing the work, 2 the request was not understood), and the output lands on the side that matches. |
| `internal/config` | `FuzzParse` | FuzzParse holds the loader contract for arbitrary bytes: never panic, never return a config with an error, never return neither, and never return a config whose own validation fails again. |
| `internal/control` | `FuzzEnqueueInput` | FuzzEnqueueInput holds the two properties the container's side of the boundary needs: whatever the browser sends, an accepted request is one the executor will validate, and nothing is ever written outside the inbox. |
| `internal/doctor` | `FuzzParseAllowlist` | FuzzParseAllowlist holds the property the command line depends on: arbitrary bytes produce either an allowlist or the typed error, never a panic and never a half-parsed value. |
| `internal/format` | `FuzzBytes` | FuzzBytes holds the formatter to a round trip: whatever value, unit and precision arrive, an output it produces parses back to that value within the rounding its own rules allow. |
| `internal/gateway` | `FuzzBuildStatus` | FuzzBuildStatus feeds arbitrary probe results through the contract: it must never panic, always marshal, and always answer with the closed status set in config order. |
| `internal/gateway` | `FuzzHostCheck` | FuzzHostCheck is the browser guard's rule, checked against a deliberately dumb reference: a loopback Host is always accepted, and anything else only when an allowlist entry justifies it. |
| `internal/gateway` | `FuzzIconName` | FuzzIconName asserts the property the icon route relies on: an accepted name is a file name in one directory, so it can never name a path elsewhere. |
| `internal/hostexec` | `FuzzBuildArgv` | FuzzBuildArgv is the executor's argv-accounting property: every element of a built command comes from the allowlist, the request id or the ref, no element is a flag the table does not define, and every command carries a positive timeout. |
| `internal/hostexec` | `FuzzDecide` | FuzzDecide checks the pure core against a deliberately dumb reference: accepted only when every documented condition holds, and the refusal code is one of the closed set. |
| `internal/hostexec` | `FuzzDecideCancel` | FuzzDecideCancel holds the ref checks to a deliberately dumb reference: a cancel is accepted only when the ref names an armed restore, for this target, that nothing has cancelled yet. |
| `internal/hostexec` | `FuzzImageRef` | FuzzImageRef treats the update signal as untrusted: what it accepts is always the allowlisted repository pinned to a digest its own validator accepts, and anything else is the typed error. |
| `internal/hostexec` | `FuzzLoadTargets` | FuzzLoadTargets feeds arbitrary bytes at the allowlist parser: it must never panic, and anything it accepts must pass the same validation again. |
| `internal/hostexec` | `FuzzRequestFile` | FuzzRequestFile pushes arbitrary bytes through the whole pipeline with a fake commander: a result is written for every request file, and a command runs only for what decodes, validates and is allowlisted. |
| `internal/hostexec` | `FuzzSetVar` | FuzzSetVar treats the .env file and the new value as untrusted: an accepted rewrite holds exactly one assignment of the key and cannot smuggle a newline into the key or the value. |
| `internal/hostprobe` | `FuzzParseSystemctlShow` | FuzzParseSystemctlShow checks the parser against a deliberately dumb reference: the last ActiveState line wins, an absent or empty one means no state, and Active means exactly "active". |
| `internal/hostprobe` | `FuzzParseTailscaleStatus` | FuzzParseTailscaleStatus holds the parser to the shape of its own type: never a panic, and a state it accepts has no negative counts and never reports more online peers than it counted. |
| `internal/jsonpath` | `FuzzEqual` | FuzzEqual holds the comparison to the properties a probe rule relies on: symmetry for any pair of decoded documents, and reflexivity for the scalars it is documented to compare. |
| `internal/jsonpath` | `FuzzGet` | FuzzGet holds the lookup to JSON's own value set: whatever document decodes, a found value is one of the types JSON can produce, and a miss says so instead of inventing a zero value. |
| `internal/jsonpath` | `FuzzNumber` | FuzzNumber holds the numeric coercion to a finite result: whatever decodes, a number it reports is never NaN or an infinity. |
| `internal/probe` | `FuzzBuildRequest` | FuzzBuildRequest treats the URL and one auth header as untrusted: the result is either an error in the closed set or a request that can only reach the host the config named. |
| `internal/probe` | `FuzzDNSResponse` | FuzzDNSResponse feeds arbitrary bytes through the resolver as the server's reply, with the query id patched in so the parser is reached: no panic, and the result is an error or a response code. |
| `internal/probe` | `FuzzHTTPJSON` | FuzzHTTPJSON feeds arbitrary bytes through the http_json evaluator: never a panic, and the answer is always one of the three statuses a probe may return with a known reason code. |
| `internal/probe` | `FuzzMerge` | FuzzMerge runs arbitrary sequences of health and vital outcomes through the merge, then asserts the rules that hold for every sequence: the closed status set, no ok from a down health, vitals kept when they were not fetched, and a failing vital only ever showing a value it really had. |
| `internal/probe` | `FuzzNextInterval` | FuzzNextInterval asserts the property the loop depends on: whatever the config and whatever the clock does, the scheduler never waits for zero or a negative time, and never invents a third cadence beyond the two it was given (or the floor that stops an invalid config from spinning). |
| `internal/probe` | `FuzzVitals` | FuzzVitals feeds arbitrary JSON through every vital kind: never a panic, a bounded value, and an error in the closed set with a known reason code. |
| `internal/updater` | `FuzzManifestHeader` | FuzzManifestHeader treats the registry's response header as untrusted: the value is either the typed digest error or a digest that passes the same validator the update path uses. |
| `internal/wire` | `FuzzDecodeRequest` | FuzzDecodeRequest holds the request reader to three properties: a decode failure wraps the one sentinel, a decoded request always gets a verdict (a refusal from the closed table, never a panic), and re-encoding round-trips to the same value with the same verdict. |
| `internal/wire` | `FuzzDecodeResult` | FuzzDecodeResult holds the result reader to two properties: a decode failure wraps the one sentinel, and whatever decodes round-trips through its own encoding field for field. |
| `internal/wire` | `FuzzParseDigest` | FuzzParseDigest is the digest grammar the update path pins: an accepted value is `sha256:` plus 64 lowercase hex characters, and it renders back byte for byte. |
| `internal/wire` | `FuzzParseHostSnapshot` | FuzzParseHostSnapshot holds the snapshot parser to its contract: never a panic, and an accepted snapshot has a timestamp and survives a marshal of its own fields. |
| `internal/wire` | `FuzzParseID` | FuzzParseID is the id grammar the executor joins into file names: an accepted id comes back as the same 36-character lowercase string, with a hyphen in the four canonical places and nothing else. |
| `internal/wire` | `FuzzParseImageRepo` | FuzzParseImageRepo is the repository grammar the update path trusts: an accepted value is exactly `ghcr.io/<owner>/<name>` with no tag, digest or extra slash, and it renders back byte for byte. |
| `internal/wire` | `FuzzUnitName` | FuzzUnitName is the security property behind decision #38's host names: a name that passes the grammar is an argv element the probe will hand to systemctl, so it can never look like a flag, contain whitespace, or escape the unit namespace. |
<!-- fuzz-targets:end -->

Timings and where the runs happen:

- **Local full check:** `scripts/check all` runs each target for 5s (the default `FUZZ_TIME`), so the
  fuzz step dominates a four-to-six-minute run.
- **CI on a push:** the `fuzz-smoke` job sets `FUZZ_TIME: 20s`
  ([.github/workflows/ci.yml](../.github/workflows/ci.yml)).
- **Weekly:** `fuzz-long.yml` sets `FUZZ_TIME: 10m` and uploads any crashers it finds
  ([.github/workflows/fuzz-long.yml](../.github/workflows/fuzz-long.yml)).
- Crashers land in `testdata/fuzz/<Target>/` and are committed; the seed corpus runs inside plain
  `go test`, so every commit re-checks every crash ever found. A red fuzz job is answered by
  committing the crasher, never by re-running until it goes green ([STANDARDS.md](STANDARDS.md)
  section 3).

## 4. Decisions most likely to be quietly eroded

These six are the ones a well-meaning change can bend without noticing, because each one costs a few
lines to break and none of them announce themselves in a diff.

**#10 — no authentication, ever.** The tailnet is the boundary; the browser guard is not an identity
layer (#54). Erosion looks like an `Authorization` header check, a token in a query string, a
`users.yaml`, a middleware named `auth`, or a "temporary" allowlist of people. The invariants rule
catches the obvious vocabulary in `internal/gateway` and `internal/control`, but it cannot see a
helper package outside those directories, so read the diff for a *concept* of identity, not just a
word.

**#14 — request file + host watcher + root-owned executor.** The cheap alternatives are mounting
`docker.sock`, granting `NOPASSWD` sudo for `systemctl`, or letting the container run a command. Erosion
looks like a new volume in `docker-compose.yml`, a second sudoers line, an `os/exec` import on the
container side, a request field that carries a command or a unit name, or a "status only" Docker
socket mount. The compose and exec-confinement rules catch each of those; review the *reason* in the
commit message if one appears.

**#15 — the allowlist is root-owned and never mounted.** This is the invariant the design rests on: if
the container can write the allowlist, the allowlist is decoration. Erosion looks like moving
`targets.json` next to `config.yaml` "for convenience", mounting `/etc/meridian` read-only so `doctor`
can read it (the supported path is piping the file: `meridian-host targets | docker exec -i meridian
meridian doctor --allowlist -`, #68), or the installer writing an allowlist into the install
directory. Any diff that names `/etc/meridian` outside `deploy/` and `cmd/meridian-host` deserves a
second look.

**#38 — zero service-specific knowledge in the code.** Names, hosts, ports, icons, sections and
quirks live in `config.yaml`; probe and vital behaviour is a closed vocabulary of kinds (#39–42).
Erosion looks like `if cfg.Name == "…"`, a `switch` on a tile id in Svelte, a per-service field in the
config schema (`adguard_user`, `ivy_host`), a unit name in `config.yaml` instead of `targets.json`, or
a new kind whose name is a product. The invariants rule catches the names from the shipped examples;
it cannot catch a *shape* that exists for one service, so review new config fields and new kinds for
generality.

**#52 — one stdlib-only host binary.** `exec`, `probe`, `update`, `targets`, `render-units`; no Bash
logic, no third-party import, no shell. Erosion looks like `go.yaml.in/yaml/v3` creeping into
`internal/hostexec`, a `#!/bin/bash` script that validates a request, or validation moving into a
systemd `ExecStartPre`. The stdlib rule and the Go allowlist rule catch the imports; the review item
is logic leaking back into shell.

**#63 — the container's only writable mount is `control/inbox/` (plus the self-update signal).**
Results and the audit log are in a read-only `outbox/`; the atomic-rename rule of #36 is unchanged,
which is why `tmp/` and `queue/` live inside the one writable directory. Erosion looks like a second
writable bind mount, `control/outbox` losing `:ro`, `update-signal` becoming a directory the host
does not re-validate, or the container pruning its own results. The compose invariants test holds the
exact volume list and access modes, so this one fails loudly; the subtle version is a new writable
path that the test's allowlist was widened to admit, which is a decision, not an edit.

## 5. Known sharp edges

**Ivy's `Host` header.** Probes use loopback because Ivy's allowlist rejects the tailnet IP
(`100.81.103.51:8418` returns 403), while the browser must be given a URL it can open. Getting it
backwards produces a permanently red tile that is actually healthy and a log full of 403s
([INTEGRATIONS.md](INTEGRATIONS.md) "The Ivy `Host` detail").

**`network_mode: host` widens the container's reach.** On a bridge every probe URL would mean the
container itself (#26), so the container shares the box's network namespace: it can dial every
loopback service and the tailnet. Everything else is narrowed to compensate (read-only root
filesystem, all capabilities dropped, no published ports, one writable mount, no shell, no socket).
That trade is deliberate (#57) and the compose invariants hold the shape; a review should confirm the
narrowing still matches the widening.

**Anyone on the tailnet can press the buttons.** `by: "tailnet"` is the only audit identity there is
(#10, and the caveats at the end of [DECISIONS.md](DECISIONS.md)). That includes reboot. It is
consistent with every other service here, but it is the one place where "anyone" means "can reboot
the box", and it should be revisited before the tailnet gains users who are not the owner.

**`NOPASSWD: ALL` already exists for the deploy user.** The single exact sudoers line is documentation
as much as enforcement: it does not contain a compromised deploy account, because that account can
already become root by other means. The executor's real value is bounding what a *request file* may
ask for, in one root-owned, reviewable place. So review the allowlist (`targets.json`, `observe.json`)
as carefully as the binary, and do not treat the sudoers line as the security boundary it resembles.

**The inbox is hostile by construction.** The container's uid can create symlinks, FIFOs, directories
and junk there; the executor must treat every entry as an attack (#62). The matching sharp edge on the
host probe is the reverse: `host.json` is written by the deploy user and read by the container, so
staleness must never read as health.

**`ProtectHome=read-only`, not `yes`.** The probe's status directory lives under the deploy user's
home on the target box, so `ProtectHome=yes` makes the write path unreachable and `ReadWritePaths`
cannot undo it. The unit comment records that this failed on the first real install.

**Two bounds, and the larger must be systemd's.** The executor's per-command ceiling is 600s and the
oneshot's `TimeoutStartSec` is 660s; inverting them would kill a healthy update mid-action and leave
the request unanswered. The self-update's own steps (240s pull, 120s each for recreate, health and
rollback) fit inside both (#88).

## 6. Suggested review lanes

Four lanes, in the order that a finding is most expensive if missed. Each names where to start and
the questions that pay for the read.

**Lane A: the privileged host side (security).**
`cmd/meridian-host/`, `internal/hostexec/`, `internal/hostfile/`, `deploy/`.
Can any byte that reaches the process influence argv beyond the allowlisted slots? Does every refusal
path write both a result and an audit line? Can a symlink, FIFO, directory or oversized file make the
executor open something it should not? Is the allowlist re-validated (owner, mode, regularity) before
every action, and again before an update? Does the update path pin a digest, keep its children in its
own process group, restore the whole `.env` and poll `/healthz` before declaring success? Read
`exec.go`, `decide.go`, `builders.go`, `update.go`, `update_apply.go`, and the two fuzz targets that
carry the properties (`FuzzRequestFile`, `FuzzBuildArgv`).

**Lane B: config and probe correctness.**
`internal/config/`, `internal/probe/`, `internal/jsonpath/`, `internal/format/`, `examples/`,
`testdata/portable/`.
Does validation reject everything the runners later assume, with a reason a human can act on? Do the
kind tables remain the single source for validation, execution and `doctor`? Are the caps, deadlines,
redirect refusal and credential rules still the ones #55 describes? Do merge and scheduler rules hold
under a dead upstream and a reload? Read `kinds.go`, `validate.go`, `fetch.go`, `state.go`,
`scheduler.go`, plus the registry-sync and portable-fixture tests.

**Lane C: frontend, accessibility and the contract.**
`web/src/`, `web/test/`, `testdata/api/`, `testdata/control/`, `internal/gateway/status.go`.
Does the page render only what `/api/status` says, with no service knowledge and no second copy of a
vocabulary? Is the armed state machine's cancel the wider, primary button, and does every destructive
action pass through it? Do the Playwright assertions still cover 44px targets, visible focus rings,
`axe`, the phone viewport, same-origin-only requests, and ages computed from `at` rather than
`age_s`? Read the client validators, the two state modules, the route tests and both e2e specs.

**Lane D: deploy and CI.**
`deploy/`, `.github/workflows/`, `scripts/`, `internal/invariants/`.
Can the installer put an attacker-chosen or stale value into a unit, a sudoers file or `.env`? Does
the release workflow build the exact commit CI passed, pin actions by SHA, keep top-level
`permissions: contents: read`, and attach provenance and an SBOM? Does the image gate fail on fixable
HIGH/CRITICAL findings only (#80)? Do the compose invariants and the deploy tests still describe
every mount, unit and sudoers line the installer writes? Read `install.sh`, `release.yml`,
`ci.yml`, `compose_test.go`, `deploy_test.go`, `install_test.go`.

## 7. Baseline: what green looked like

The brief was written against the tree that added it; the full check and its timing are recorded
here so a later reviewer can tell when coverage dropped rather than merely changed.

<!-- check-baseline:begin -->
<!-- check-baseline:end -->
