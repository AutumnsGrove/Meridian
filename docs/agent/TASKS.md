# Tasks

Progress lives here and nowhere else. Work is split into **sessions**: one session is one sitting with
a fresh context, covering a small fixed group of tasks. Do your session's tasks in order, run the
end-of-session ritual ([README.md](README.md)), and stop. A task is done per
[STANDARDS.md](../STANDARDS.md) section 10. `[human]` tasks are never ticked by an agent.

## Sessions

The next session is the first row with an unticked task. Task numbers are the ones below; stage
documents hold the detail. Each session is sized to stay in the early, precise part of a context
window; if one still runs long, split it with the partial-task rule in the README rather than pushing on.

| Session | Tasks | Stage doc | What exists afterwards |
|---|---|---|---|
| **S1** | T01, T02, T03, T04 | [01](01-foundation.md) | Empty project that rejects bad commits and bad pushes |
| **S2** | T08, T05, T06, T07 | [02](02-config-and-wire.md), [03](03-probe.md) (T08 only) | `jsonpath`, `format`, `atomicfile`, `wire` host snapshot, config load and validation, kinds table |
| **S3** | T09, T10, T11 | [03](03-probe.md) | Fetch client, every health and vital kind |
| **S4** | T12, T13 | [03](03-probe.md) | Merge rules, store, scheduler |
| **S5** | T14, T15 | [04](04-gateway-and-web.md) | Gateway with guards and `/api/status` golden contract |
| **S6** | T16, T17 | [04](04-gateway-and-web.md) | Web scaffold and the launcher UI |
| **S7** | T18 | [04](04-gateway-and-web.md) | Control placeholder page |
| **S8** | T20, T21 | [05](05-cli-and-deploy.md) | `meridian run`, reload, healthcheck, `doctor` |
| **S9** | T19 | [04](04-gateway-and-web.md) | Playwright suite against the real binary |
| **S10** | T22, T23 | [05](05-cli-and-deploy.md) | `meridian-host probe`, Dockerfile, compose, image CI |
| **gate** | T24 `[human]` | [05](05-cli-and-deploy.md) | Phase 1 verified on the potato (owner) |
| **S11** | T25, T26 | [06](06-control.md) | Control wire types and the container side |
| **S12** | T27 | [06](06-control.md) | `meridian-host exec` core: allowlist, validation, results, audit |
| **S13** | T28, T29 | [06](06-control.md) | Command builders, real execution, DNS guard, reboot |
| **S14** | T30, T31 | [06](06-control.md) | Control UI, units, sudoers |
| **gate** | T32, T33 `[human]` | [06](06-control.md) | Phases 2 and 3 verified on the potato (owner) |
| **S15** | T34, T35 | [07](07-update.md) | Digest resolution, `meridian-host update` with rollback |
| **S16** | T36 | [07](07-update.md) | `install.sh`, release workflow |
| **gate** | T37 `[human]` | [07](07-update.md) | Phase 4 verified on the potato (owner) |
| **S17** | T38 | [08](08-closeout.md) | Review brief |
| **S18** | T39 | [08](08-closeout.md) | Docs switched to maintenance mode; scaffolding archived |
| **S19** | T40 (optional) | [08](08-closeout.md) | PR review workflow |

`[human]` gates do not block later code sessions unless a task's `deps` say so (they do not); the
owner can run them whenever convenient. S17 is always last because it deletes this file's context.

Phase labels map to [PLAN.md](../PLAN.md): P1 = read-only launcher, P2 = control plane for one
service, P3 = all targets, P4 = self-update and install.

## Stage 1: Foundation ([01-foundation.md](01-foundation.md))

- [x] **T01** Repo scaffold: module, directories, thin mains, `web/embed.go`, lint config, pre-commit hook
- [x] **T02** `scripts/check` and `scripts/fuzz` (deps: T01)
- [x] **T03** Invariants test: the hard constraints as code (deps: T01)
- [x] **T04** CI workflows and Dependabot (deps: T02, T03)

## Stage 2: Config and wire types ([02-config-and-wire.md](02-config-and-wire.md))

- [x] **T05** `atomicfile` and `wire` host snapshot, ids (deps: T01, T08)
- [x] **T06** Config types, strict load, validation (deps: T01)
- [x] **T07** Kinds table, registry sync test, example configs, portability fixture (deps: T06)

## Stage 3: Probing ([03-probe.md](03-probe.md))

- [x] **T08** `jsonpath` and `format` pure packages (deps: T01)
- [x] **T09** Fetch client: limits, no redirects, auth, redaction (deps: T06)
- [x] **T10** Health kinds: `http_status`, `http_json`, `dns`, `host_unit`, `host_field`, `self` (deps: T07, T08, T09, T05)
- [x] **T11** Vital kinds (deps: T10)
- [x] **T12** Result model, merge rules, store (deps: T10, T11)
- [x] **T13** Scheduler: stagger, idle backoff, touch, reload swap (deps: T12)

## Stage 4: Gateway and web ([04-gateway-and-web.md](04-gateway-and-web.md))

- [x] **T14** Gateway skeleton: server, headers, browser guard, `/healthz`, icons (deps: T06)
- [x] **T15** `/api/status` contract, handler and golden file (deps: T13, T14)
- [x] **T16** Web scaffold: SvelteKit, tokens, fonts, test tooling, embed wiring (deps: T01)
- [x] **T17** Launcher UI: tiles, states, search (deps: T15, T16)
- [x] **T18** `/control` placeholder page: layout and failure modes, no actions (deps: T17)
- [x] **T19** Playwright suite against the real binary, with `axe` (deps: T17, T18)

## Stage 5: CLI and Phase 1 deploy ([05-cli-and-deploy.md](05-cli-and-deploy.md))

- [x] **T20** `meridian run`, signals, SIGHUP reload, `healthcheck`, `version` (deps: T15)
- [x] **T21** `meridian doctor` (deps: T20)
- [x] **T22** `meridian-host probe` and the `host.json` timer units (deps: T05)
- [x] **T23** Dockerfile, compose, image build in CI (deps: T20, T04, T19)
- [ ] **T24** `[human]` Phase 1 on the potato: verification checklist (deps: T21, T22, T23)

## Stage 6: Control plane ([06-control.md](06-control.md))

- [x] **T25** Control wire types and validators (deps: T05)
- [ ] **T26** Container side: enqueue, results, status, `/api/control` (deps: T25, T14)
- [ ] **T27** `meridian-host exec` core: allowlist, validation, results, audit, prune (deps: T25)
- [ ] **T28** Runners: systemd, compose, container, host, run-as, timeouts (deps: T27)
- [ ] **T29** DNS auto-restore and cancel, reboot, Meridian self-restart (deps: T28)
- [ ] **T30** Control UI: armed buttons, countdown, banners, reconnect (deps: T26, T18)
- [ ] **T31** Control units, sudoers, installer pieces (deps: T27)
- [ ] **T32** `[human]` Phase 2 on the potato: bwmon restart end to end (deps: T26, T28, T30, T31)
- [ ] **T33** `[human]` Phase 3 on the potato: full target matrix, Vaultwarden capture (deps: T29, T32)

## Stage 7: Update and install ([07-update.md](07-update.md))

- [ ] **T34** `updater`: resolve newest digest, handoff file (deps: T26)
- [ ] **T35** `meridian-host update`: validation, pin, pull, recreate, health poll, rollback (deps: T27)
- [ ] **T36** `install.sh` and `release.yml` (deps: T31, T35, T04)
- [ ] **T37** `[human]` Phase 4 on the potato: push, tap, new version; broken image rolls back (deps: T34, T36)

## Stage 8: Close-out ([08-closeout.md](08-closeout.md))

- [ ] **T38** Review brief for the end-of-build whole-codebase review (deps: all code tasks)
- [ ] **T39** Maintenance rewrite of the docs, archive the build-out scaffolding (deps: T38; last)
- [ ] **T40** Optional: PR review workflow with Pi and OpenRouter (deps: T39)

Phase 5 (homelab repo documentation) is out of this repo; T39 leaves a checklist for it.
