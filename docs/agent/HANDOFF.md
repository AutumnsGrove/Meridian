# Handoff

Read this first in every session. It is **rewritten, not appended**, at the end of each session; the
history is in git. Keep it under about 40 lines: it is for a cold start with an empty context.

## Last session: S16, T36 (2026-10-28)

**T36 is done and ticked.** Full `scripts/check` is green (34 e2e, fuzz, security). Six commits are
unpushed (including this one); the T35 batch is already on `origin/main`.

- `deploy/install.sh` creates the layout (including `update-signal/`), copies compose/config only when
  absent, installs a checksummed `meridian-host`, resolves the first image digest from GHCR unless
  `--image-digest` is given, writes `MERIDIAN_UID`/`GID` and a digest-pinned `MERIDIAN_IMAGE`, renders
  and validates the units and sudoers through `meridian-host render-units`, enables the probe timer
  and control path, and prints a pass/fail table. `--upgrade-host` replaces only the binary;
  `--dry-run` is a non-root plan and a Go test runs the real script against a fake binary.
- `meridian-host render-units` fills the closed placeholder list (`strings.NewReplacer`), refuses an
  unknown `@…@` or an injecting value, and has a test that renders the real templates and checks the
  T31 directives.
- `.github/workflows/release.yml`: the image job follows a successful CI on `main` (`:latest`) or a
  `v*` tag (`:vX.Y.Z`), pushes both architectures with provenance, an SBOM and an OIDC attestation,
  and prints the digest. The tag job builds `meridian-host` for arm64 and amd64, writes `SHA256SUMS`,
  and creates or updates the release with `gh`.
- `deploy/targets.example.json` now names the dashboard `meridian`, matching `config.yaml.example`'s
  `update.target`; the old `hub` name is gone.

## Next

**S17: T38**, the review brief. Read `docs/agent/08-closeout.md` T38. Write `docs/REVIEW-BRIEF.md`:
the hard constraints with the mechanical test for each, the six trust boundaries and their fuzz
targets, the generated fuzz-target list, the decisions most likely to erode (#10, #14, #15, #38, #52,
#63), the sharp edges, suggested review lanes, and the `scripts/check all` output and fuzz timings.
It is docs only and must contain no build-out task ids.

**T37 is the owner's Phase 4 gate**, not a code session. Steps:

- Install a fresh box layout in a scratch directory with `sudo deploy/install.sh --version vX.Y.Z`
  (or `--host-binary dist/meridian-host --image-digest sha256:…`). The installer leaves existing
  `.env`, `targets.json` and compose files alone, so a scratch directory is easier than the live one.
- The generic `targets.json` self target uses `/srv/meridian`; edit its `dir`/`signal_file` to the
  install directory (or start from `examples/targets.homelab.json`), and set `config.yaml`'s
  `update.target` and `update.image` to match.
- Push a trivial change, wait for CI and then the release workflow, tap update, and watch the version
  change. Push a deliberately broken image and confirm the previous digest is back in `.env` and the
  result says `failed`.

## Gotchas

- The box is still on the old layout: its compose has no `update-signal` mount and its `targets.json`
  still points `meridian` at `meridian-update.service`. T37 is what replaces those; do not assume the
  installer has run there.
- `install.sh --version` uses the same `vX.Y.Z` tag for the host asset and the image. A dev install
  with `--host-binary` defaults to `latest`; pass `--image-digest` if that tag is not published.
- The installer's `IMAGE_REPO` is `ghcr.io/autumnsgrove/meridian`; a fork edits the constant.
- `observe.json` is seeded from the example, not generated from `targets.json`; adding a target's unit
  is the CONTROL.md checklist step.
- The full check is roughly four minutes of fuzzing (34 targets at 5s plus startup) and ~1.5 minutes
  of Playwright. Use `--fast` while working; `FUZZ_TIME=1s scripts/check` is a quicker full pass.
- Still true: the control loop starts only once `/api/status` says a control plane exists; e2e needs
  both `pnpm build` and `go build`; 12px `--color-text-faint` fails axe; `ProtectHome=read-only` in
  the probe unit; executor 600s / unit 660s; `hostfile.Read` takes the owner; `hostexec`/`wire`/
  `hostprobe` are stdlib-only; a fuzz crasher is committed, never re-run away.

## For the owner

- **T37** is the Phase 4 proof. **Tick T24 and T32** after a look on your phone, and **T33** needs
  your hands: the rest of the target matrix, `control.reboot: true`, and the AdGuard auto-restore.
- Dependabot's TypeScript 7 offer still needs a decision, not a merge.
