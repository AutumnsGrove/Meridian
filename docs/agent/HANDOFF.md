# Handoff

Read this first in every session. It is **rewritten, not appended**, at the end of each session; the
history is in git. Keep it under about 40 lines: it is for a cold start with an empty context.

## Last session: S13 and the first half of S14 (2026-10-27)

Finished: **T27's follow-ups, T28 and T29** (all ticked). Partial: **T30**, pure layer only.

- T28/T29 are in `internal/hostexec`: the builder table (`builders.go`), the real Commander
  (`runner.go`), `ResultFirst` + post-check + deadlines + the DNS guard in `exec.go`, and the
  `meridian-host exec` command (`command.go`). Decisions #84-#85.
- T30 so far, all green and committed:
  - `web/src/lib/state/armed.ts` + `.test.ts`: the two-step reducer with the two fast-check
    properties. Done.
  - `web/src/lib/api/fields.ts` (shared strict readers), `api/control.ts` (`parseControlStatus`,
    `testdata/control/status.golden.json` is the shared golden) + tests. Done.
  - `api/client.ts`: `postControl` and `fetchControlStatus`; the POST's refusal carries `sentence`
    now, so the page keeps no copy of the code table (decision #86, gateway change included). Done.
  - `state/poller.ts`: `startLoop` owns the loop mechanics for both pages; `startPolling` keeps its
    API and 15s/60s behaviour. `state/control.ts`: `controlDelay` (1s/10s), `pendingFor`,
    `resultFor`, `resultByID`, `busyReason`, `remainingSeconds`, `countdownLabel`, `historyRows`.
    Done, with tests.

## Next

Session S15 (a fresh context, T30 is still the task): the **components and the page**, then the
Playwright round trip. Remaining behaviours, in order:

- [ ] `ActionButton.svelte`: `busy` label, `destructive` (rose outline, never filled, per DESIGN),
      `onclick`. Destructive actions arm; Cancel is the wider primary button once armed.
- [ ] `ServiceRow.svelte`: the armed block with the consequence copy and `aria-live`, one global
      action at a time via `busyReason`, `update_disabled_reason` still on its own button.
- [ ] `RebootCard.svelte`: same global arm machine, host target, reconnect when it lands.
- [ ] New: `RestoreBanner.svelte` (countdown from `restore_at` + Cancel that posts
      `cancel-autorestore` with the ref), `ActionHistory.svelte` (from `historyRows`), and the
      no-executor banner (`executor.state === 'missing'`, queued rows say `pending (unclaimed)`).
- [ ] The page wiring: one `ArmState`, one `activeTarget`, `postControl` → `sent`/`refused`,
      `result` from the control view by id, `controlLive.svelte.ts` using `startLoop` +
      `controlDelay(pending.length)`, and `/healthz` reconnect with backoff 2s/4s/8s up to 30s.
- [ ] Component tests for the four mockup states (`control-normal`, `control-armed-stop`,
      `control-no-executor`, `control-reboot`), a fake clock for the countdown, then the Playwright
      additions and the stub executor in `testdata/stub` (a small Go helper that drains
      `control/inbox/queue` like the real one, so a restart and an armed stop complete without root).
- [ ] Copy rules on every new string: no em dashes, no exclamation marks, second person, present.

## State of the tree

Clean and green under the full `scripts/check` (fuzz, e2e, security included). Nothing is pushed;
S13 and S14's commits are waiting for the owner's next batch.

## Gotchas

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

- Dependabot's npm job will offer TypeScript 7, which breaks the `svelte-check` peer range: it needs
  a decision, not a merge.
- `[human]` gates T24, T32, T33, T37 are still open. T24 (Phase 1 on the potato) is code-unblocked.
