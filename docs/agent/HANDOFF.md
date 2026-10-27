# Handoff

Read this first in every session. It is **rewritten, not appended**, at the end of each session; the
history is in git. Keep it under about 40 lines: it is for a cold start with an empty context.

## Last session: S13, S14 and S15 (2026-10-27)

Finished: **T28 and T29** (ticked). Partial: **T30**, everything except the e2e round trip.

- T28/T29 are in `internal/hostexec`: the builder table (`builders.go`), the real Commander
  (`runner.go`), `ResultFirst` + post-check + deadlines + the DNS guard in `exec.go`, and the
  `meridian-host exec` command (`command.go`). Decisions #84-#85.
- T30, green and committed (only the Playwright round trip is missing):
  - `web/src/lib/state/armed.ts` + `.test.ts`: the two-step reducer with the two fast-check
    properties. Done.
  - `web/src/lib/api/fields.ts` (shared strict readers), `api/control.ts` (`parseControlStatus`,
    `testdata/control/status.golden.json` is the shared golden) + tests. Done.
  - `api/client.ts`: `postControl` and `fetchControlStatus`; the POST's refusal carries `sentence`
    now, so the page keeps no copy of the code table (decision #86, gateway change included). Done.
  - `state/poller.ts`: `startLoop` owns the loop mechanics for both pages; `startPolling` keeps its
    API and 15s/60s behaviour. `state/control.ts`: `controlDelay` (1s/10s), `pendingFor`,
    `resultFor`, `resultByID`, `busyReason`, `remainingSeconds`, `countdownLabel`, `historyRows`,
    `ackFor`, `armedWarning`, `resultLine`, `progressLabel`. Done, with tests.
  - `state/reconnect.ts`: the 2s/4s/8s/16s/30s ladder and `watchForRecovery`. Done, with tests.
  - Components and the page: `ActionButton` (busy, destructive), `ServiceRow` (armed block, per-row
    pending and result), `RebootCard`, `RestoreBanner`, `ActionHistory`, `Banner` (with a detail
    line), and `/control` wired to one `ArmState` for the whole page. Component tests cover the four
    mockup states. The e2e suite (28 tests) passes against the real binary.

## Next

Session S16 (T30 is still the task): **the last behaviour only**, the Playwright round trip against a
stub executor.

- [ ] `testdata/stub` gains an executor mode: `-control <dir>` drains `<dir>/inbox/queue` every
      100ms like `meridian-host exec` would (import `internal/wire` for the shapes; testdata is exempt
      from the invariants), writes `outbox/results/<id>.json` plus one audit line, removes the
      request. A DNS stop should carry `restore_at = now+10m` and a `cancel-autorestore` should carry
      the ref, so the countdown banner and its cancel can be exercised.
- [ ] `test/e2e/global-setup.ts`: create `<runDir>/control/{inbox/queue,inbox/tmp,outbox}`, point the
      fixture's `inbox_dir`/`outbox_dir` at them, set `control.reboot: true`, start the stub executor,
      and stop it in teardown. Give one fixture service `dns: true` so the countdown has a subject.
- [ ] `test/e2e/control.spec.ts` is currently the **Phase 1** spec (all buttons disabled, "Control
      plane not installed"). With a working plane it must become the live spec: a restart round trip
      (queued, then its result, then the history row), an armed DNS stop with cancel, and the
      no-executor case by stopping the stub executor mid-run. Keep the tap-target and axe tests.
- [ ] Then tick T30, and T31 follows (units, sudoers, installer pieces).

## State of the tree

Clean and green under the full `scripts/check` (fuzz, e2e, security included). Nothing is pushed;
S13, S14 and S15's commits are waiting for the owner's next batch.

## Gotchas

- The control loop starts only once `/api/status` says a control plane exists. Without that guard the
  page polls a 503 route on a Phase 1 box and the browser logs a console error, which the e2e fixture
  treats as a failure. Do not "simplify" it back.
- `web/build` is embedded in the binary, so an e2e run needs **both** `pnpm build` and
  `go build -o dist/meridian ./cmd/meridian`; `scripts/check e2e` does both. Running Playwright alone
  serves the previous build and will lie to you.
- 12px text at `--color-text-faint` fails axe's contrast floor. Use `--color-text-dim` for small text.

- `startPolling` now delegates to `startLoop`; the launcher's shape is unchanged, so a new page needs
  only `request`, `nextDelay` and `onChange`. `refresh()` on a `Loop` re-polls now or after the
  request in flight.
- The control view's `results` are the newest 20 and its `audit` the tail the container configured:
  a row finds its own answer by id with `resultByID`, never by target.
- `pendingFor`/`busyReason` match on the *target slug*, not the service id. The page must map
  service → `service.control.target` before asking.
- The verify step in the executor compares the **word** `systemctl is-active` prints; `inactive`
  exits 3.
- A docker update is one `docker compose up -d --pull always <service>`. T31's example allowlist
  needs `update: {kind: docker}`, `"result_first": true` on the Meridian target, and `dns: true` on
  any resolver.
- `exec.Command` in a Go test is fine; `scripts/fuzz` picks up any new `Fuzz*` target automatically.
- `internal/probe`'s scheduler test flaked once under a loaded `check go` and passed on rerun.
- Still true: `hostfile.Read` takes the expected owner; `hostexec`/`wire`/`hostprobe` are stdlib-only;
  `scripts/check` is the only gate; a fuzz crasher is committed, never re-run away.

## For the owner

- T30's UI is usable on the box already: the page acts, follows a queued action and counts down an
  auto-restore. What is missing is the automated round trip, not behaviour.
- Dependabot's npm job will offer TypeScript 7, which breaks the `svelte-check` peer range: it needs
  a decision, not a merge.
- `[human]` gates T24, T32, T33, T37 are still open. T24 (Phase 1 on the potato) is code-unblocked.
