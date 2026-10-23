# Open questions

Questions the docs do not answer, raised by an implementing agent that could not (or did not need to)
interrupt the owner. The preferred route for a question that blocks work is the question tool (see
`AGENTS.md`, "When you need the owner"); this file is the fallback for when that tool is unavailable
or unanswered, and for lower-priority questions that do not block the current task. The owner (or a
stronger model on request) settles them in batches. A settled question is moved into
[DECISIONS.md](DECISIONS.md) as a new numbered entry and removed from here with a link.

Rules for the agent:

- Raise a question only if you cannot resolve it from the docs, the code, or a sensible default
  that is obviously reversible.
- Always include your recommended answer. The owner should be able to reply "yes".
- After raising it, mark the blocked task as blocked here and continue with the next unblocked
  task. Do not build on a guess.

## Format

```
### Q<n>: short title
- **Blocks:** T## (and any task that depends on it)
- **Asked:** YYYY-MM-DD
- **Context:** what you were doing and what the docs say or do not say
- **Options:** A, B (one line each)
- **Recommendation:** which, and why
```

## Open

_None yet._
