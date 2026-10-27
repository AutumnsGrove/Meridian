# Handoff

Read this first in every session. It is **rewritten, not appended**, at the end of each session; the
history is in git. Keep it under about 40 lines: it is for a cold start with an empty context.

## Last session: S13 (2026-10-27)

Finished: **T28** and **T29**, both ticked. The executor can now act.

- `builders.go`: `Builders` keyed by `BuilderKey{Kind, Action}` plus `updateBuilders` keyed by the
  target's `update.kind`. `Build(BuildInput{Target, Action, ID, Ref, DeployUser})` is the only place
  that names a program; `ScheduleRestore(start, id)` wraps the *start* argv in `systemd-run`.
  `Target.ResultFirst` is the new `result_first` field, and `LoadTargets` fills `Update.RunAs` with
  the deploy user. New rule: `dns: true` requires `stop`.
- `runner.go`: `Runner(logf)`, the real `Commander`. Minimal env (PATH/HOME/LANG), `Credential` with
  groups for deploy-user commands, process-group `SIGKILL` on the deadline, `WaitDelay`, 64 KiB tail
  captured and the rest to the journal.
- `exec.go`: `Executor.Wait` seam, provisional result for `ResultFirst`, `is-active` post-check
  (`not_active_after`), one deadline per command, the DNS guard (`restore_at`, `restore_failed` fail
  safe), reboot-timer cancel when the final write fails, `result.Ref`, and audit lines that lead with
  the result's note.
- `command.go`: `meridian-host exec` (`--host/--targets/--lock`, exit 1 when the config cannot be
  trusted), dispatched from `cmd/meridian-host`. Fuzzers: `FuzzBuildArgv`, `FuzzDecideCancel`.
  Invariants gained "reboot but never shut down" with a fixture.

## Next

Session **S14**, first task **T30** (control UI). Read `docs/agent/06-control.md` T30, `DESIGN.md`
"Control panel" and the `mockups/control-*` files. T30 wants a stub executor under `testdata/stub` as
well as the reducers, so expect the session to be long; the partial-task rule applies freely.

## State of the tree

Clean and green under the full `scripts/check` (fuzz, e2e and security included). Seven commits from
S13 are **unpushed**; the owner batches pushes and the pre-push hook re-runs the full check.

## Gotchas

- The verify step compares the **word** `systemctl is-active` prints, not its exit code: `inactive`
  exits 3, so an exit-code check would call every good stop a failure.
- `ScheduleRestore` takes the `Command` that `Build` produced for `start`, so a new kind needs no
  restore row; systemd wants `--on-active=10min` (`10m0s` is not a span it accepts).
- A docker update is one `docker compose up -d --pull always <service>`, not pull-then-up, so there is
  one exit code and one deadline. T31's example allowlist must use `update: {kind: docker}`.
- T31's allowlist also needs `"result_first": true` on the Meridian target, or a self-restart loses its
  provisional result, and `dns: true` on any resolver it wants the guard for.
- `Decide` can only cancel a restore whose stop is still in the newest 200 results; older than that and
  the cancel is `bad_ref`.
- The test harness fakes `Build` but not `ScheduleRestore`, so the restore argv there is the real one
  wrapping `/bin/true`. Replies are matched by a substring of the argv (`h.replies`).
- `internal/probe`'s scheduler test ("timed out waiting for the poller to park again") flaked once
  under a fully loaded `check go` and passed on rerun and under `-race`. Not S13's doing; watch it.
- A new invariants rule ships with a fixture under `internal/invariants/testdata/` or
  `TestHardConstraints_CanFail` fails.
- Still true: `testdata/api` is globbed as `/api/status` documents; `hostfile.Read` takes the expected
  owner; the host binary and `hostexec` are stdlib-only; `scripts/check` is the only gate; a fuzz
  crasher is committed, never re-run away.

## For the owner

- Worth reading the new `docs/DECISIONS.md` #84-#85 before T31: they settle `result_first`, the single
  `up --pull always` command, and why the audit line carries the note.
- Dependabot's npm job will offer TypeScript 7, which breaks the `svelte-check` peer range: it needs a
  decision, not a merge.
- `[human]` gates T24, T32, T33, T37 are still open. T24 (Phase 1 on the potato) is code-unblocked.
