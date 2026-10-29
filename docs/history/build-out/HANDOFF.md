# Handoff

This file is history. The build-out ended when T39 archived this directory: there is no session to
resume, and nothing in `docs/history/` is maintained. The live documents are in `docs/`.

## Last session: S18, T39 (2026-10-28)

**T39 is done and ticked; the build is over.** In order:

- `docs/PLAN.md` and this directory moved to `docs/history/` with a banner on every file; every link
  that pointed at them was fixed; `OPEN-QUESTIONS.md` returned to its empty template.
- `README.md` says what exists and how to install, update and point it at other services.
- `CLAUDE.md` and `AGENTS.md` are maintenance documents: the hard constraints, the loop for one
  change, and recipes for a service, a kind, a control target, a release, Dependabot and a crasher.
- The reference docs were read against the code. ARCHITECTURE, CONTROL and STANDARDS now describe the
  binaries, paths and commands that exist; the code comments lost their task ids; DECISIONS has the
  closing note (nothing reversed; #52, #79 and #93 are the amendments).
- `meridian docs kinds` prints the vocabulary from `config.HealthKinds`/`config.VitalKinds`, and the
  block at the bottom of `config.yaml.example` is compared with its output by a test.
- `docs/homelab-followups.md` carries the other repository's checklist (Meridian entry,
  `docs/meridian.md`, Ivy finally documented, two health checks).

Verification: `scripts/check all` green, every relative markdown link resolves, and no build task ids
or Phase language remain outside `docs/history/` and the append-only decision log.

## What is left

Only **T40**, the optional PR review workflow with Pi and an OpenRouter key. It is deliberately not
part of the build: it needs the owner's secret and model choice, and the workflow file stays out of
the repo until then. Its specification is in `docs/history/build-out/08-closeout.md`.

## For the owner

- **T37** is still the Phase 4 proof on the potato, and **T24**, **T32** and **T33** are the other
  owner steps. Their instructions are in this archive and in the previous handoff's git history.
- The homelab repo checklist is [docs/homelab-followups.md](../../homelab-followups.md).
