# Build-out guide

This directory is the work order for building Meridian from nothing to the end of
[PLAN.md](../PLAN.md) Phase 4. The documents here are **temporary scaffolding**: the last two tasks
in [TASKS.md](TASKS.md) replace all of it with maintenance-oriented documentation and archive what
is left. Do not link to these files from anything that will outlive the build.

## How it is organised

| File | What it is |
|---|---|
| [HANDOFF.md](HANDOFF.md) | Rewritten at the end of every session: where to resume. Read first |
| [TASKS.md](TASKS.md) | The session table and ordered task list with checkboxes. The only place progress is recorded |
| [01-foundation.md](01-foundation.md) | Repo scaffold, `scripts/check`, CI, invariants test |
| [02-config-and-wire.md](02-config-and-wire.md) | Config schema, validation, the registry, shared wire types |
| [03-probe.md](03-probe.md) | Fetching, health and vital kinds, state merging, scheduler |
| [04-gateway-and-web.md](04-gateway-and-web.md) | HTTP server, `/api/status`, the SvelteKit app, Playwright |
| [05-cli-and-deploy.md](05-cli-and-deploy.md) | `run`, `doctor`, host probe, Docker image and compose (Phase 1 ends here) |
| [06-control.md](06-control.md) | Control plane, container side and `meridian-host exec` (Phases 2 and 3) |
| [07-update.md](07-update.md) | Self-update, install script, release workflow (Phase 4) |
| [08-closeout.md](08-closeout.md) | Review brief and the switch from build-forward to maintenance-forward docs |

Each stage document has, per task: what to build, the design that is already decided, behaviours
worth testing first, fuzz targets, and what "done" means. They state **decisions**, not
implementations. Writing the code is your job.

## The loop

For each task, in order:

1. **Orient.** Read the task and the documents and decision numbers it cites. Grep the code for
   anything you are about to write; reuse before you build.
2. **Red.** Write the tests (and fuzz targets) for the task's behaviours. Run them. They must fail,
   and fail on the assertion, not on a typo. Add edge cases the doc did not think of.
3. **Green.** Write the least code that passes.
4. **Refactor.** Fold duplication into reusable functions. Tests stay green.
5. **Check.** `scripts/check --fast` after every commit; full `scripts/check` at the end of a task.
6. **Commit small.** Conventional Commits, one logical change each. See STANDARDS.md section 7.
   A normal task is three to eight commits.
7. **Reconcile docs.** If the code made a sentence in a design doc false, fix the doc in the same
   series. If you made a design choice the docs did not, append it to
   [DECISIONS.md](../DECISIONS.md).
8. **Tick.** Mark the task in TASKS.md, as its own small commit
   (`docs(tasks): tick T09`).
9. **Next.** Pick the next unticked task whose dependencies are done.

Push when your harness or the owner expects it. Run the full `scripts/check` first. CI runs only on
the tip of a push, so every commit in a batch has to be green on its own.

## Task flags

- `[human]` needs the real box, a secret, a GitHub setting or an owner decision. Write the exact
  steps into the stage document's "Owner steps" section, leave the box unticked, and continue.
- `deps: T##` the task cannot start until those are ticked.
- A task listed under a question in [OPEN-QUESTIONS.md](../OPEN-QUESTIONS.md) is blocked; skip it,
  and if the whole session is blocked say so in the handoff and stop.

## Sessions, and how to stop cleanly

The owner works in **short dedicated sessions** and clears context between them. The unit is the
session table in [TASKS.md](TASKS.md). Your job in a session:

1. Start per `AGENTS.md` "Start here" (HANDOFF first, then your session's block). Keep the reading small.
2. Do the session's tasks in order, using the loop above.
3. **Never stop in the middle of a task** unless the partial-task rule below applies. A session ends
   between tasks, with the tree green and committed.
4. Run the end-of-session ritual, then stop and say so. Do not start the next session's work.

### End-of-session ritual

Do these in order, as small commits where they change files:

1. `scripts/check` (full, not `--fast`) is green. If something is red and you cannot fix it, say so in
   the handoff; do not hide it.
2. Docs match the code: any sentence the work made false is fixed; any new design choice is appended to
   `DECISIONS.md`. Questions you did not need the owner for are in `OPEN-QUESTIONS.md`.
3. Tasks you finished are ticked in `TASKS.md` (`docs(tasks): tick T09-T11`).
4. **Rewrite** [HANDOFF.md](HANDOFF.md) completely (do not append; git history keeps the past) using the
   template in that file: what exists now, the exact next session and first task, gotchas and
   surprises, anything half-done, anything the owner should know or do.
5. `git status` clean, everything committed (`docs(handoff): end of S3`). Do not push unless the owner
   told you to; they push in batches and CI runs on the tip. Say in your final message that the
   session is complete and name the next session.

### The partial-task rule

If a single task is turning out too big for the context you have left, or you notice the quality of
your own work slipping (repeating yourself, losing track of earlier decisions), stop at the last green
commit instead of pushing on:

- Commit what is green.
- In HANDOFF.md list the **remaining behaviours as a checklist** (copy them from the task doc) and
  which files hold the finished part. Leave the task unticked.
- End the session with the ritual. The next session's first job is that checklist.

Splitting a task this way is normal and costs nothing; a half-understood commit costs a lot.

### Gates

When every non-`[human]` task in a stage is done, its "stage done" list in the stage document should
hold; check it as part of that session's ritual and mention the result in the handoff.
`[human]` gates are for the owner and never block a code session.

## When the docs and reality disagree

Reality wins, then fix the docs. If it is a design disagreement (the doc says X, X turns out to be
impossible or unsafe), do not paper over it: ask the owner with the question tool (see `AGENTS.md`
"When you need the owner") or, failing that, log it in OPEN-QUESTIONS.md with your recommendation
and skip ahead.

## Layout you are building toward

```
cmd/meridian/            thin main for the container binary
cmd/meridian-host/       thin main for the host binary (stdlib only)
internal/
  cli/                   run, doctor, version, healthcheck, update, restart (cobra)
  config/                schema, strict load, validation, kinds table, reload
  wire/                  shared types: host snapshot, control request/result/audit; ids; validators
  atomicfile/            write temp + fsync + rename (used by both binaries)
  jsonpath/              dotted-path lookup and type-aware equality over decoded JSON
  format/                number, byte, percent and age formatting (pure)
  probe/                 fetch client, health kinds, vital kinds, merge, store, scheduler
  gateway/               routes, middleware (security headers, browser guard), static, icons
  control/               container side: enqueue, poll results, queue depth, status
  doctor/                explain a config against the world, read only
  updater/               resolve latest GHCR digest, write the handoff
  hostexec/              meridian-host exec: allowlist, validation, runners, audit, prune
  hostprobe/             meridian-host probe: unit states, tailscale, uptime
  invariants/            go/parser based architecture tests (the hard constraints, mechanised)
web/                     SvelteKit app; web/embed.go exposes build/ via go:embed
deploy/                  compose file, systemd units, sudoers, install.sh
scripts/                 check, fuzz, and small helpers
testdata/                shared fixtures and golden files (JSON the Go and TS tests both read)
```

A package that does not exist yet is created by the task that first needs it, not before.
