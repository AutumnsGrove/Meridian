# Handoff

Read this first in every session. It is **rewritten, not appended**, at the end of each session; the
history is in git. Keep it under about 40 lines: it is for a cold start with an empty context.

## Template

```
## Last session: S<n> (<date>)
Finished: T## ... (ticked in TASKS.md)
Partial: none | T## and a checklist of remaining behaviours, with the files holding the finished part

## Next
Session S<n+1>, first task T##. One line on what it needs first.

## State of the tree
Clean and green under `scripts/check` | what is red and why

## Gotchas
Things the next session would otherwise rediscover: a surprising behaviour, a tool quirk, an
assumption a test depends on, a decision made in the middle of a task.

## For the owner
Anything that needs your hands (a secret, a GitHub setting, a [human] gate) or your call
(a pending item in OPEN-QUESTIONS.md).
```

---

## Last session: S12 (2026-10-26)

Finished: **T27**, ticked. The executor's core exists; nothing calls it yet.

- `internal/hostexec/targets.go`: `Target`/`Targets`/`HostConfig`, `LoadTargets`, `LoadHost`,
  `ValidateTargets`, and the grammars (unit name via `wire.IsUnitName`, Docker's container name, an
  absolute clean program path, `run_as` == the deploy user). One bad target refuses the whole file.
  `wire.DecodeStrict` is now exported and used by both readers.
- `decide.go`: `Decide(request, targets, history, now) Decision` with `StaleAfter` (120s) and
  `CoalesceWindow` (60s); pure, table-driven, fuzzed against a dumb reference.
- `exec.go`: `Executor{HostFile, TargetsFile, LockFile, Owner, Build, Run, Now, Random, Log}` and
  `Execute(ctx)`: trusted config → non-blocking flock → prune results (7d) and audit lines (365d) →
  drain the queue (64 per run, re-listed, junk deleted) → result + audit + delete per request.
  `Command`, `Builder` and `Commander` are the seams T28 fills in.
- Goldens in `testdata/hostexec/`. Fuzzers: `FuzzLoadTargets`, `FuzzDecide`, `FuzzRequestFile`.

Partial: none. **No `exec` subcommand exists yet** and there is no real `Builder`/`Commander`.

## Next

Session **S13**, first task **T28**. It needs the builders table (`Builders[BuilderKey]`), the real
`Commander` (`os/exec`, `Credential`, process-group kill, minimal env, output caps), the verify step
(`not_active_after`), `ResultFirst`, and the `meridian-host exec` dispatch in
`cmd/meridian-host`. Read `docs/agent/06-control.md` T28 and CONTROL.md "Who runs what".

## State of the tree

Clean and green under the full `scripts/check`. S11's 66 commits were pushed at the start of this
session (CI ran everything since S3 for the first time, `image` job included); 6 commits are
unpushed now.

## Gotchas

- `Command` has **no** `ResultFirst` and no `Verify` yet, and the loop does not handle them: the T28
  doc puts both in the command type, so T28 adds the fields and the steps in `handle`.
- `Decide` refuses `cancel-autorestore` because it is not in any target's `actions`. The container's
  `Enqueue` accepts it, so a cancel queued today gets `action_not_allowed`. **T29** gives it the ref
  checks; that is expected, not a bug to "fix" early.
- Coalescing counts only a prior result that is `OK` and not itself `coalesced` (decision #83), so a
  retry after a failure runs.
- A queue entry that is not a request is deleted and audited under a **fresh** UUID; the planted name
  only reaches the journal (decision #83). A result that will not decode still counts for replay by
  its file name.
- `readRequest` opens with `O_NOFOLLOW|O_NONBLOCK`. Without `O_NONBLOCK` a planted FIFO blocks root
  forever; the test asserts progress with a 10s deadline rather than sleeping.
- Decoder errors are **dropped**, never logged: `encoding/json` errors quote field names from the
  file, and file contents must not reach a log, a result or the page.
- The audit prune keeps corrupt lines on purpose: deleting evidence is not pruning.
- `FuzzRequestFile` is disk-bound (~10 execs/s: a result fsync, a directory fsync and an audit fsync
  per iteration). Its seeds run in every `go test`; the full check's 5s pass covers a few dozen more.
  Do not weaken the fsyncs to speed it up.
- The container kind's JSON field is `container` (`"container": "worker"`); compose uses `dir` +
  `service`. T31's example allowlists must match.
- Still true: `testdata/api` is globbed as `/api/status` documents; `hostfile.Read` takes the
  expected owner; the host binary and `hostexec` are stdlib-only (invariants test); `scripts/check`
  is the only gate; a fuzz crasher is committed, never re-run away.

## For the owner

- 6 commits unpushed. The first CI run after S11's push covers everything since S3; worth reading the
  `image` job's first real result.
- Dependabot's npm job will offer TypeScript 7, which breaks the `svelte-check` peer range: it needs
  a decision, not a merge.
- `[human]` gates T24, T32, T33, T37 are still open. T24 (Phase 1 on the potato) is code-unblocked.
