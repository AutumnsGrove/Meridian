# Handoff

Read this first in every session. It is **rewritten, not appended**, at the end of each session; the
history is in git. Keep it under about 40 lines: it is for a cold start with an empty context.

## Last session: S15, T34 and T35 (2026-10-28)

**T34 and T35 are done and ticked.** Full `scripts/check` is green (34 e2e, fuzz, security); 35
commits are unpushed (including this one). Stage 7's code is complete apart from T36's installer and release workflow.

- Container side (T34): `wire.Digest`/`ImageRepo`; `updater.Resolve` waits out an in-flight build or
  an unmoved tag for 4 minutes; `GET /api/update` reports read-only; a self-update tap resolves and
  writes `update-signal/requested` before it queues (decision #87).
- Host side (T35): the allowlist's `self` entry carries `image`, `dir`, `service`, `port` and
  `signal_file`; exactly one self target is allowed and it must set `result_first`. `meridian-host
  update` reads the hint with `O_NOFOLLOW`, re-validates the repo plus digest, rewrites `.env` with
  `SetVar`, pulls, recreates, polls `/healthz` for 120s, and on any failure restores the whole
  `.env` and recreates the old container.
- `SetVar` and `ImageRef` are fuzzed; the update command is tested with a fake `Commander`. Its
  docker children stay in the updater's process group so the executor's timeout kill reaches them
  (decision #89). Budgets: 180s pull, 90s recreate, 120s health, 90s rollback.
- The e2e stub executor now actually holds the 2.5s it reports; the round-trip test's in-flight
  assertion had been a race in the fixture, not the page.

## Next

**S16: T36** (`deploy/install.sh` and `.github/workflows/release.yml`), then the `[human]` gate T37.
Read `docs/agent/07-update.md` T36 and `docs/CONTROL.md` "Update validation". T36 adds the
`meridian-host render-units` helper (`strings.NewReplacer`, not `text/template`) and tests every
template leaves no `@` placeholder; the installer creates `update-signal/`, installs the host binary
with a checksum, never overwrites an existing `targets.json`, validates units and sudoers before
moving them, and verifies the timer and path units.

## Gotchas

- The box is unchanged: no `~/Meridian/update-signal/`, no compose mount, old `targets.json` with
  `meridian` on the retired `meridian-update.service`. T36 creates the directory and the self fields;
  T37 proves it. Do not copy the new `update:` block there before that.
- `meridian-host update` takes no target argument: it finds the one `self` target by kind. It refuses
  to run as root; the executor starts it as the deploy user.
- `.env` must be a regular file (no symlink) because the updater opens it with `O_NOFOLLOW`.
- The full check is roughly four minutes of fuzzing (34 targets at 5s plus startup) and ~1.5 minutes
  of Playwright. Use `scripts/check --fast` while working and `FUZZ_TIME=1s scripts/check` for a
  quicker full pass; the default full check is the push gate.
- Still true: the control loop starts only once `/api/status` says a control plane exists; e2e needs
  both `pnpm build` and `go build`; 12px `--color-text-faint` fails axe; `ProtectHome=read-only` in
  the probe unit; executor 600s / unit 660s; `hostfile.Read` takes the owner; `hostexec`/`wire`/
  `hostprobe` are stdlib-only; a fuzz crasher is committed, never re-run away.

## For the owner

- **Tick T24 and T32** after a look on your phone. **T33** needs your hands: the rest of the target
  matrix, `control.reboot: true`, and the AdGuard auto-restore proof.
- Dependabot's TypeScript 7 offer still needs a decision, not a merge.
