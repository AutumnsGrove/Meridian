# Handoff

Read this first in every session. It is **rewritten, not appended**, at the end of each session; the
history is in git. Keep it under about 40 lines: it is for a cold start with an empty context.

## Last session: S15, T34 (2026-10-28)

**T34 is done and ticked. T35 has not started.** The tree is clean and green under the full
`scripts/check` (34 e2e, fuzz, security); 24 commits are unpushed (including this one).

- `internal/wire`: `Digest`/`ParseDigest` and `ImageRepo`/`ParseImageRepo`, both strict and fuzzed.
- `internal/probe`: the one fetch seam now does HEAD and literal headers, and returns response headers.
- `internal/updater`: `Resolve` (GHCR token + manifest, waits out an in-flight build or an unmoved tag
  for up to 4 minutes, reports `Available`), `Request` (atomic hint), `ManifestDigest`.
- Config: optional `update: {image, publish_url, signal_dir, target}`; `target` must name a service
  control target that offers `update`. Both shipped configs load under the normal validation.
- `GET /api/update` is the read-only report; a failed check is data. `POST /api/control` for that
  target resolves and writes `update-signal/requested` **before** it queues, with an extended write
  deadline (decision #87). New wire code `update_failed`.
- `deploy/docker-compose.yml` mounts `./update-signal:/update-signal`; the compose invariants know
  both writable mounts and nothing wider.
- Two pre-existing scheduler-test flakes fixed: an abandoned fake-clock timer, and an exact poll count.

## Next

**S15 continues with T35**, the host half in `internal/hostexec` (stdlib only). Add `Image` to
`hostexec.Update`, a `self` row in `updateBuilders`, read `update-signal/requested` with `O_NOFOLLOW`
and a 128-byte cap, rebuild `image@digest` from the allowlist, `SetVar` `.env` atomically,
`docker compose pull` + `up -d`, poll `/healthz`, roll back on failure, and write `ResultFirst`. Read
`docs/agent/07-update.md` T35 and `docs/CONTROL.md` "Update validation". `examples/targets.homelab.json`
still updates `meridian` with `kind: unit`; T35 changes it to `self` with
`image: ghcr.io/autumnsgrove/meridian`.

## Gotchas

- The box is unchanged: `~/Meridian` has no `update-signal/` and its compose has no mount, so the tap
  path is code-complete but not live until T36/T37. Its old config has no `update` block, so
  `GET /api/update` says `configured: false` there.
- The host allowlist needs its own repository string; it must never read the container's config.
- Both example publish URLs point at `.github/workflows/release.yml`, which T36 creates. Until then a
  check sees no workflow run, skips the wait, and reports whatever `:latest` is.
- Still true: the control loop starts only once `/api/status` says a control plane exists; e2e needs
  both `pnpm build` and `go build`; 12px `--color-text-faint` fails axe; `ProtectHome=read-only` in the
  probe unit; executor 600s / unit 660s; `hostfile.Read` takes the owner; `hostexec`/`wire`/`hostprobe`
  are stdlib-only; a fuzz crasher is committed, never re-run away.

## For the owner

- **Tick T24 and T32** after a look on your phone. **T33** needs your hands: the rest of the target
  matrix, `control.reboot: true`, and the AdGuard auto-restore proof.
- Do not copy the new `update:` block to the box until T36/T37 creates `update-signal/` and the mount;
  until then the update tap would fail with `update_failed`.
- Dependabot's TypeScript 7 offer still needs a decision, not a merge.
