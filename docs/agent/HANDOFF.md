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

## Last session: S2 (2026-10-24)

Finished: T08, T05, T06, T07 (ticked in TASKS.md). Stage 2 is done: `jsonpath` and `format` with
fuzz targets, `atomicfile` rename-into-place writes, the strict `wire` host snapshot and UUIDs,
config load with defaults and all-problems-at-once validation, and the kinds table synced to the
vocabulary comment. The template, the worked example and the portability fixture all load clean.

Partial: none.

## Next

Session **S3**, first task **T09** (fetch client with limits, no redirects, auth, redaction), then
T10 and T11. Read `docs/agent/03-probe.md` (T09 onward) and the Ivy `Host` section in
`docs/INTEGRATIONS.md` first.

## State of the tree

Clean and green under the full `scripts/check`. Nothing is pushed.

## Gotchas

- `config.Probe` is a flat union whose `Set` records present keys; per-kind rules live in
  `internal/config/kinds.go`. Adding a kind is a table row plus a line in the vocabulary comment of
  `config.yaml.example`, because a test compares the two.
- Defaults treat a zero duration or int as "not written" (every defaulted field has a positive lower
  bound); `config.Duration` converts with `time.Duration(x)`. `config.Parse([]byte)` is the pure half
  of `Load`, so fuzz targets never touch files.
- Validation messages are scanned by the service-name invariant: examples in them must be invented
  (`my-app`), never a real section or service word.
- The host stdlib-only invariant now includes `internal/jsonpath` because `wire.Field` imports it.
  Any new package the wire side imports must be stdlib-only and added to that rule's set.

## For the owner

- Nothing is pushed. The first push to `main` is CI's first real run; web and e2e print a skip
  notice until T16 and T19.
- Hooks are already installed on this machine (`core.hooksPath`).
