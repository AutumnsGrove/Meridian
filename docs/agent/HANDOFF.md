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

## Last session: S1 (2026-10-24)

Finished: T01, T02, T03, T04 (ticked in TASKS.md). Stage 1 is done: a fresh clone with
`git config core.hooksPath .githooks` passes `scripts/check`, and the hook rejects an unformatted
Go file. Nothing is pushed, so CI has not run for real yet.

Partial: none.

## Next

Session **S2**, first task **T08** (`jsonpath`, `format`: pure packages, table and fuzz tests), then
T05, T06, T07. Read `docs/agent/02-config-and-wire.md` and only the T08 block of `03-probe.md`.

## State of the tree

Clean and green under the full `scripts/check` (govulncheck, gitleaks, actionlint, shellcheck
included).

## Gotchas

- `scripts/check` self-checks its own help: a new group must be added to the usage text too.
- The hook runs the whole `go` group over the working tree, so an untracked red test blocks every
  commit, even a docs tick. Finish or park the files; never `--no-verify`.
- `internal/invariants` is test-only on purpose: a rules file that is not a test would contain the
  service words its own first rule forbids. Fixtures in `internal/invariants/testdata` are walked
  as fake repo roots, and every rule needs a fixture it catches.
- The no-service-names rule exempts `cmd/meridian-host`, `internal/hostexec`, `internal/hostprobe`,
  loopback hosts and `meridian` itself (the module path).
- `cmd/meridian-host` must never import `internal/cli`: the host binary is stdlib only. Its own
  dispatch arrives in T22. `web/build/.gitkeep` must stay committed or the embed stops compiling.
- Fuzz seeds run inside plain `go test`, so every target runs on every check; `scripts/fuzz 20s` is
  the short CI pass, `10m` the weekly one.
- Full check needs golangci-lint, govulncheck, gitleaks, actionlint, shellcheck, pnpm; the hook
  (`--fast`) needs only Go and golangci-lint.
- Action pins are SHAs verified with `git ls-remote`; Dependabot bumps them weekly.

## For the owner

- Run `git config core.hooksPath .githooks` once per clone (done on this machine).
- The first push to `main` is CI's first real run. The `web` and `e2e` jobs print a skip notice
  until T16 and T19; Dependabot's npm and docker entries stay quiet until those manifests land.
