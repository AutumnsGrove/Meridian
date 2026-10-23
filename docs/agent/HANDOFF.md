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

## Last session: none yet

Nothing has been built. No Go, no frontend.

## Next

Session **S1**, first task **T01** (repo scaffold). After S1, the owner should run
`git config core.hooksPath .githooks` once on their own machine too.

## State of the tree

Documentation only.

## Gotchas

- The owner works in short sessions with a cleared context between them; see `AGENTS.md`.
- `main` is the working branch. Commits are small and local; CI runs on the tip of a push.

## For the owner

Nothing pending.
