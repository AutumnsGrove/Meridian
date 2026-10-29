> Historical: the plan this project was built from. Not maintained.

# Stage 7: Self-update and install

Goal: PLAN Phase 4. A `git push` followed by one tap lands a new version, and a broken image rolls
itself back. Read CONTROL.md "Update validation". Decisions #20, #52, #67.

## T34 `updater`: resolve the newest digest

Package `internal/updater` (container side, **no `os/exec`**, never pulls or recreates anything).

- `Resolve(ctx, f Fetcher, repo string, wantSHA string) (Digest, error)`: asks GHCR (anonymous token
  endpoint, then the manifest `HEAD` for `:latest`, reading `Docker-Content-Digest`). `repo` comes from
  config (`update.image`, validated as `ghcr.io/<owner>/<name>`), never from the request. Reuse the
  probe `Fetcher` and its limits; do not write a second HTTP client.
- **Wait out an in-flight CI build** (Polaris's behaviour): the caller passes the commit SHA the
  running build corresponds to; if the newest GitHub Actions run for `main` is still in progress or the
  `latest` digest equals the running one while a newer commit exists, retry with backoff for up to 4
  minutes before answering "already current" or "newest available". The source of "newest commit" is
  a config URL (the repo's public API), so no GitHub knowledge is in the code beyond the
  digest/manifest protocol.
- `Digest` is a validated type: `sha256:` plus 64 lowercase hex characters. `ParseDigest` is strict
  and shared with the host side (put it in `internal/wire`).
- `Request(dir, digest)`: writes `update-signal/requested` (atomically, via `atomicfile`) containing
  only the digest. The host side re-validates; the container's file is just a hint. Actually applying
  it goes through the control plane as the `meridian` target's `update` action (T35), so nothing here
  needs privileges. `update.target` names that control target in config; the enqueue handler resolves
  and writes the hint before it queues the request, extending that one response's write deadline to
  the resolve cap (decision #87).
- `GET /api/update` (read only) reports running version, newest digest, and whether an update exists.
  The tap itself is the ordinary `POST /api/control` with `target: meridian, action: update`.

**Tests first**: `httptest` GHCR stand-in (token, manifest, redirect-free); in-flight build waits then
resolves (fake clock); malformed digest in a registry response is rejected; unknown repo format
rejected at config load; the handoff file is atomic.

**Fuzz**: `FuzzParseDigest` (accepted values match the strict grammar, nothing else), `FuzzManifestHeader`.

## T35 `meridian-host update`

`internal/hostexec` (stdlib only), invoked as the `meridian` target's `update` command, replacing the
bash `update.sh` (decision #52). It is the only code that pulls or recreates the Meridian container.

- The allowlist entry carries the repository (`image`), the compose `dir`, the `service`, the loopback
  `port` and the `signal_file`. Exactly one target may be `self`; the command finds it by kind, so no
  target name reaches its command line.
- Input: nothing from the request except the signal file written by T34 (on the update tap, before
  the request is queued), which is untrusted. Read it
  with `O_NOFOLLOW`, size cap 128 bytes, and **re-validate**: the final image reference is built from
  the allowlisted repo in `targets.json` (`update.image`, e.g. `ghcr.io/autumnsgrove/meridian`) plus
  `@` plus a digest matching `^sha256:[0-9a-f]{64}$`. Anything else is refused. Never a tag.
- Steps, each a small function with a fake `Commander` in tests: record the current `.env`; write the
  new `MERIDIAN_IMAGE=<repo>@<digest>` into `.env` atomically (preserving every other line byte for
  byte); `docker compose pull`; `docker compose up -d`; poll `http://127.0.0.1:<port>/healthz` for up
  to 120s; on any failure restore the previous **whole `.env`** and `up -d` again, then report `failed`
  with the reason. The result is written first (`ResultFirst`) because the container dies during
  recreate. Budgets are 180s pull, 90s recreate, 120s health and 90s rollback, so a rollback still fits
  inside the executor's 600s; the docker commands stay in the updater's process group so the executor's
  timeout kill reaches them too (decision #89).
- It runs as the deploy user (docker group), launched by the executor, never directly by the container.
- `.env` rewriting is a pure function `SetVar(contents, key, value) (string, error)` with its own tests
  and a fuzz target: no other line changes, no duplicate keys, no newline injection through the value
  (the digest grammar already forbids it; the function must refuse regardless).

**Tests first**: happy path; unhealthy new container triggers rollback and the old digest is back in
`.env`; reference with a tag, a different registry, uppercase hex, trailing whitespace or a `\n`
refused; health poll timeout; `.env` with comments, blank lines, quoted values and CRLF preserved.

**Fuzz**: `FuzzSetVar`, `FuzzImageRef` (the only accepted references have the exact allowlisted prefix
and a valid digest).

## T36 `install.sh` and `release.yml`

**Release workflow** (`.github/workflows/release.yml`): on push to `main` after CI success and on tags
`v*`, with per-job permissions only where needed (`packages: write`, `id-token: write`,
`attestations: write`, `contents: write` for the release job):
- Build and push the multi-arch image to `ghcr.io/autumnsgrove/meridian` (`:latest` on `main`, `:vX.Y.Z` on
  tags) by digest, with provenance and an SBOM; the digest is printed in the job summary.
- On tags: build `meridian-host` for `linux/arm64` and `linux/amd64` (`CGO_ENABLED=0`, `-trimpath`),
  attach both plus `SHA256SUMS` to the GitHub release.
- Never builds on the potato; the box only ever pulls.

**Decision #67**: `meridian-host` is shipped as a release asset and installed with a checksum
verification; it is **not** extracted from the container image and **not** updated by the container's
self-update. The container must never be able to cause a root-owned file under `/etc/meridian` to
change. A host-binary upgrade is a deliberate `install.sh --upgrade-host` run by the owner.

**`deploy/install.sh`** (bash, `set -euo pipefail`, `shellcheck`-clean, must refuse to run as a
non-root user other than via sudo; idempotent):
1. Parse flags: `--deploy-user`, `--dir` (default `~/Meridian`), `--host-binary PATH|--version vX.Y.Z`,
   `--image-digest` / `--image-tag`, `--upgrade-host`, `--dry-run`.
2. Create `~/Meridian/{config,icons,status,control/inbox/{tmp,queue},control/outbox/results,update-signal}` with
   correct owners and modes (inbox owner the deploy user, outbox root-owned 0755). Copy
   `docker-compose.yml` and `config.yaml` from the repo only when the target does not exist.
3. Install `meridian-host` (download + `sha256sum -c` against `SHA256SUMS`, or a local path) to
   `/etc/meridian/meridian-host`, mode 0755, root-owned; `host.json` and (if missing) `targets.json` and
   `observe.json` from the examples, root-owned 0644. **Never overwrites an existing `targets.json`.**
4. Render and install the systemd units and the sudoers file from templates (`@…@` placeholders),
   validate with `visudo -cf` and `systemd-analyze verify` before moving into place.
5. Write `.env` (`MERIDIAN_UID`, `MERIDIAN_GID`, `MERIDIAN_IMAGE` pinned by digest) if missing. The pin
   is `--image-digest` when given, otherwise resolved from GHCR for `--image-tag` (default `latest`,
   or the `--version` tag); a failed resolution stops the install rather than writing a tag.
6. Enable the timer and `.path` units, then **verify** them (active, enabled, `is-failed` empty, probe
   file appears within 45s), in the spirit of Polaris/Ivy's `watcher-sync-verify.sh`. Print a clear
   pass/fail table.
7. Print the `tailscale serve --https=8891 …` command; do not run it.

**Tests**: `shellcheck`; the `@…@` rendering is done by a tiny `meridian-host render-units` helper
(stdlib `text/template` is **not** used; plain `strings.NewReplacer` over a closed list of
placeholders) so it is unit-tested instead of being sed in bash; a test renders every template and
asserts no `@` placeholder remains and the output passes the unit-directive tests from T31. The
installer's shell glue stays thin enough that `shellcheck` plus a dry-run mode (`--dry-run` prints the
actions) is the test; a Go test runs the real script with `--dry-run`, a fake host binary and a
temporary `--dir`, and asserts the plan is printed and nothing is created.

The generic `deploy/targets.example.json` names the dashboard `meridian`, matching the config
template's `update.target`, so a fresh install's pair points at the same target.

## T37 `[human]` Phase 4 on the potato

Owner steps (PLAN Phase 4 "done when"): install a fresh box layout with `install.sh` in a scratch
directory first; push a trivial change, wait for the release workflow, tap update in the UI, watch the
version change; then push a deliberately broken image (health endpoint returns 500) and confirm the
previous version is back and the result says `failed`; confirm the `.env` pin matches the running image.

**Stage done:** non-`[human]` tasks ticked; `scripts/check all` green; release workflow lints clean
with `actionlint`; the rollback path has a passing test with a fake `Commander`.
