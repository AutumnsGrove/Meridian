# Handoff

Read this first in every session. It is **rewritten, not appended**, at the end of each session; the
history is in git. Keep it under about 40 lines: it is for a cold start with an empty context.

## Template

```
## Last session: S<n> (<date>)
Finished: T## ... (ticked in TASKS.md)
Partial: none | T## and a checklist of remaining behaviours, with the files holding the finished part

## Next
Session S<n+1>, first task T##. One line on what it needs first.

## State of the tree
Clean and green under `scripts/check` | what is red and why

## Gotchas
Things the next session would otherwise rediscover: a surprising behaviour, a tool quirk, an
assumption a test depends on, a decision made in the middle of a task.

## For the owner
Anything that needs your hands (a secret, a GitHub setting, a [human] gate) or your call
(a pending item in OPEN-QUESTIONS.md).
```

---

## Last session: S10 (2026-10-26)

Finished: T22, T23, both ticked.

- **T22** `meridian-host probe` exists end to end. `internal/hostfile` reads the files the host trusts
  (regular file, expected uid, not group/other-writable, size cap, `O_NOFOLLOW`) and is the package
  T27's `loadTrusted` should reuse. `wire.IsUnitName` is the one unit-name grammar. `internal/hostprobe`
  collects unit states from `systemctl show`, the tailnet from `tailscale status --json`, uptime and
  load from `/proc`, merges `observe.json` + the allowlist's `unit` fields, and writes the snapshot
  through `atomicfile`; a source that fails is omitted, never guessed. `cmd/meridian-host` dispatches
  `probe`; `deploy/systemd/meridian-probe.{service,timer}` are the timer (oneshot, deploy user,
  sandboxed, `@DEPLOY_USER@`/`@STATUS_DIR@` for the installer).
- **T23** `Dockerfile` (Node build, Go cross-compile from `$BUILDPLATFORM`, distroless static nonroot,
  every base pinned by digest), `.dockerignore`, `deploy/docker-compose.yml` + `deploy/.env.example`,
  a compose invariants rule in `internal/invariants` with three failing fixtures, a `ci.yml` `image`
  job (buildx both arches, Trivy, smoke) and `scripts/smoke-image`.

Partial: none.

## Next

Session **S11**, first task **T25** (control wire types and validators). Read `docs/agent/06-control.md`
from T25 and `docs/CONTROL.md` (the implementation notes at the top matter: `inbox/{tmp,queue}`,
`outbox/results`, decision #62-#67).

## State of the tree

Clean and green under the full `scripts/check` (28 e2e tests, fuzz pass, security). 53 commits
unpushed; `origin/main` is still the S3 tip, so CI has never run any of it.

## Gotchas

- **The image job has never run.** Locally `docker build` succeeded and
  `docker buildx build --platform linux/amd64,linux/arm64` produced both; the loaded image answered
  `/healthz` through `docker exec … /meridian healthcheck` and served `/api/status` plus the SPA (200,
  `text/html`) when queried from a curl container sharing its netns. `scripts/smoke-image` itself
  could **not** run on macOS: Docker Desktop gives the container the VM's network namespace, so the
  app's loopback is not the caller's. It is Linux-only (CI, the box); its first real run is CI.
- The compose rule rejects these substrings anywhere in the file, comments included: `docker.sock`,
  `/etc/meridian`, `privileged:`, `ports:`, `pid: host`, `network_mode: bridge`. Watch wording.
- `wire.IsUnitName` now accepts `.path` (decision #79), because T31 puts `meridian-control.path` in
  `observe.json`. Anything else is dropped from the probe's watch list with a log line.
- The probe omits a unit when `systemctl show` exits non-zero, and leaves `since` zero when the
  timestamp will not parse; the timestamp is parsed in the local zone because systemd prints it so.
  macOS has no `/proc`: never call `hostprobe.RealDeps()` from a test, inject the readings.
- `hostfile.Read` takes the expected owner, so tests can own their fixtures. `probe` passes uid 0;
  the executor must too.
- Base images are pinned by digest; take a new one from the registry's **index** manifest (the build
  needs both arches), not from a tag.
- Still true: the e2e fixture copy is made by `replaceAll` on four exact strings, so editing
  `testdata/portable/config.yaml` means editing `global-setup.ts`; a tile's accessible name is its
  content (no `aria-label`); skip links and the flip test's ~31s wait are deliberate; `workers: 1`.
- Still true: SvelteKit 3 configures itself from `vite.config.ts`; `#lib` is a package subpath;
  `ssr.noExternal: ['cookie']` is load-bearing; the build re-creates `build/.gitkeep`; TypeScript
  stays 6.x; goldens are read via `web/test/golden.ts`; `cli.Main` takes a stdin reader.
- A fuzz job that goes red is answered by committing the crasher, never by re-running.

## For the owner

- 53 commits unpushed. The next push is the first CI run of everything since S3, now including the
  `image` job (multi-arch build, Trivy, the Linux-only smoke test). Worth watching.
- Dependabot's npm job will offer TypeScript 7, which breaks the `svelte-check` peer range: it needs
  a decision, not a merge. The Docker ecosystem will also start offering base-image digest bumps.
- T24 (Phase 1 on the potato) is now unblocked in code terms: T21, T22 and T23 are ticked. The
  `[human]` gates (T24, T32, T33, T37) are still open.
