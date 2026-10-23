# Tasks

Progress lives here and nowhere else. Take the first unticked task whose `deps` are ticked. A task
is done per [STANDARDS.md](../STANDARDS.md) section 10. `[human]` tasks are never ticked by an agent.

Phase labels map to [PLAN.md](../PLAN.md): P1 = read-only launcher, P2 = control plane for one
service, P3 = all targets, P4 = self-update and install.

## Stage 1: Foundation ([01-foundation.md](01-foundation.md))

- [ ] **T01** Repo scaffold: module, directories, thin mains, `web/embed.go`, lint config, pre-commit hook
- [ ] **T02** `scripts/check` and `scripts/fuzz` (deps: T01)
- [ ] **T03** Invariants test: the hard constraints as code (deps: T01)
- [ ] **T04** CI workflows and Dependabot (deps: T02, T03)

## Stage 2: Config and wire types ([02-config-and-wire.md](02-config-and-wire.md))

- [ ] **T05** `atomicfile` and `wire` host snapshot, ids (deps: T01, T08)
- [ ] **T06** Config types, strict load, validation (deps: T01)
- [ ] **T07** Kinds table, registry sync test, example configs, portability fixture (deps: T06)

## Stage 3: Probing ([03-probe.md](03-probe.md))

- [ ] **T08** `jsonpath` and `format` pure packages (deps: T01)
- [ ] **T09** Fetch client: limits, no redirects, auth, redaction (deps: T06)
- [ ] **T10** Health kinds: `http_status`, `http_json`, `dns`, `host_unit`, `host_field`, `self` (deps: T07, T08, T09, T05)
- [ ] **T11** Vital kinds (deps: T10)
- [ ] **T12** Result model, merge rules, store (deps: T10, T11)
- [ ] **T13** Scheduler: stagger, idle backoff, touch, reload swap (deps: T12)

## Stage 4: Gateway and web ([04-gateway-and-web.md](04-gateway-and-web.md))

- [ ] **T14** Gateway skeleton: server, headers, browser guard, `/healthz`, icons (deps: T06)
- [ ] **T15** `/api/status` contract, handler and golden file (deps: T13, T14)
- [ ] **T16** Web scaffold: SvelteKit, tokens, fonts, test tooling, embed wiring (deps: T01)
- [ ] **T17** Launcher UI: tiles, states, search (deps: T15, T16)
- [ ] **T18** `/control` placeholder page: layout and failure modes, no actions (deps: T17)
- [ ] **T19** Playwright suite against the real binary, with `axe` (deps: T17, T18)

## Stage 5: CLI and Phase 1 deploy ([05-cli-and-deploy.md](05-cli-and-deploy.md))

- [ ] **T20** `meridian run`, signals, SIGHUP reload, `healthcheck`, `version` (deps: T15)
- [ ] **T21** `meridian doctor` (deps: T20)
- [ ] **T22** `meridian-host probe` and the `host.json` timer units (deps: T05)
- [ ] **T23** Dockerfile, compose, image build in CI (deps: T20, T04, T19)
- [ ] **T24** `[human]` Phase 1 on the potato: verification checklist (deps: T21, T22, T23)

## Stage 6: Control plane ([06-control.md](06-control.md))

- [ ] **T25** Control wire types and validators (deps: T05)
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

---

## Stage notes

_Agents append a short paragraph here at the end of each stage._
