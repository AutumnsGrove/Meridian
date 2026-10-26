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

## Last session: S11 (2026-10-26)

Finished: **T25, T26**, both ticked. The control plane's container half exists end to end and the
host half has its contract.

- **T25** `internal/wire/control.go`: `Action` (six, with the acknowledgement each needs and the DNS
  variant), `Request`/`Result`/`AuditLine`, the closed `Code` table with one user sentence each,
  `*Refusal`, `ValidateRequest` (structure only), strict `DecodeRequest`/`DecodeResult`/
  `DecodeAuditLine` (4/8/8 KiB caps, no unknown fields, no trailing data), `IsSlug`. Goldens in
  `testdata/wire/`, exact bytes, fuzzers `FuzzDecodeRequest`, `FuzzDecodeResult`. `config` now takes
  the slug grammar and action vocabulary from `wire` instead of its own copies.
- **T26** `internal/control`: `Plane{Config, Host, Now, Random}`, `Available`, `Enqueue` (validate →
  config policy → queue depth → duplicate → ack → atomic write through `os.Root`), `Status`
  (executor health from `meridian-control.path` in the snapshot and the 15s unclaimed rule, pending,
  last 20 results, audit tail capped at 256 KiB, derived auto-restore). `atomicfile.WriteRoot` is new
  for the rooted write. Gateway: `POST /api/control` (202 + id, codes 400/409/429/503) and
  `GET /api/control/status`; availability is read from the directories per request. Golden:
  `testdata/control/status.golden.json` (its own directory, see gotchas).

Partial: none.

## Next

Session **S12**, first task **T27** (`meridian-host exec` core), which is the big one: allowlist
loading through `hostfile`, the `Decide` pure core, result/audit writing, prune, flock. Read
`docs/agent/06-control.md` from T27 and `docs/CONTROL.md` "The allowlist" and "Guards".

## State of the tree

Clean and green under the full `scripts/check` (28 e2e tests, fuzz pass, security). 66 commits
unpushed; `origin/main` is still the S3 tip, so CI has never run any of it.

## Gotchas

- **`testdata/api` is globbed by the web client as `/api/status` documents.** Any other golden there
  fails `vitest` (`parseStatus`). The control view's golden lives in `testdata/control/` for that
  reason; T30 will need its own loader next to `web/test/golden.ts`.
- `Status` never fails: a corrupt result, a half-written audit line, a bad id or a missing `results/`
  directory costs that entry and nothing else. `Available` is the only thing that distinguishes
  "not installed" from "empty".
- Executor health: an unclaimed request (older than 15s) forces `missing` even when the snapshot says
  the path unit is active, and `Since` still comes from the unit. `unknown` covers no snapshot, a
  stale snapshot (`host_status.max_age`) and a snapshot without the unit.
- `Enqueue` refuses locally with the executor's own codes. `cancel-autorestore` is accepted for any
  target that exists in config, not only ones listing it in `actions` (config's action vocabulary
  cannot hold it). `reboot` needs `control.reboot: true` and target `host`.
- 409 is a duplicate (`coalesced`), 429 is a full queue, 503 is `unavailable`. The code is the
  contract; the HTTP status is derived from it in `refusalStatus`.
- `atomicfile.WriteRoot` is the only writer that goes through an `os.Root`; `os.Root` has no
  `CreateTemp`, so it generates a random suffix itself and calls `Root.Rename`.
- Still true: the e2e fixture copy is made by `replaceAll` on four exact strings in
  `web/test/e2e/global-setup.ts`; a tile's accessible name is its content; `workers: 1`; the flip
  test's ~31s wait is deliberate; `scripts/smoke-image` is Linux-only; base images pinned by digest;
  SvelteKit configures itself from `vite.config.ts`; goldens are read via `web/test/golden.ts`;
  `cli.Main` takes a stdin reader; `hostfile.Read` takes the expected owner (pass uid 0 from root).
- A fuzz job that goes red is answered by committing the crasher, never by re-running.

## For the owner

- 66 commits unpushed. The next push is the first CI run of everything since S3, including the
  `image` job (multi-arch build, Trivy, the Linux-only smoke test). Worth watching.
- Dependabot's npm job will offer TypeScript 7, which breaks the `svelte-check` peer range: it needs
  a decision, not a merge. The Docker ecosystem will also start offering base-image digest bumps.
- `[human]` gates T24, T32, T33, T37 are still open. T24 (Phase 1 on the potato) is code-unblocked.
