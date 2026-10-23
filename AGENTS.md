# Agents

Entry point for any coding agent working in this repo. `CLAUDE.md` carries the same hard
constraints; this file adds the build-out workflow. If the two ever disagree, `CLAUDE.md` wins and
the disagreement is a bug to log in `docs/OPEN-QUESTIONS.md`.

Meridian is a personal homelab dashboard: one page linking to every self-hosted service, showing
whether each is up, and letting its owner restart, update or stop them from a phone. Go backend,
SvelteKit frontend embedded in one binary, a Docker container, and a small root-owned host binary for
privileged actions.

## How work is split: one session, one chunk

The owner runs **one dedicated session per chunk of work** and clears context between them, because
precision drops as context grows. So you do a bounded chunk, leave everything in a state where a
brand-new session can pick up cold, and **stop**. The chunks are the numbered sessions (S1, S2, ...)
in [docs/agent/TASKS.md](docs/agent/TASKS.md).

## Start here, every session (keep this cheap)

Read only these, in order. Do not read the other stage documents or the whole design set.

1. [docs/agent/HANDOFF.md](docs/agent/HANDOFF.md): where the last session stopped, what is next, any
   gotchas. Short by design.
2. `git status` and `git log --oneline -10`. The tree should be clean; if it is not, the handoff says why.
3. The block for your session in [docs/agent/TASKS.md](docs/agent/TASKS.md) (the session table at the
   top), then the **stage document sections for those tasks only**.
4. [docs/STANDARDS.md](docs/STANDARDS.md) (once per session; it is short on purpose).
5. Anything a task explicitly cites (a decision number, a design doc section). Read just that part.

Then work the session's tasks in order, following [docs/agent/README.md](docs/agent/README.md) "The
loop". When the session's last task is done, run the **end-of-session ritual** in that file and stop.
Do not roll into the next session because you feel fine: a fresh context is the point.

## The five rules that matter most

1. **Test first, watch it fail, then implement.** Small commits, each one green under
   `scripts/check --fast`.
2. **Data first.** Types and tables, then small pure functions, with I/O at the edges. Reuse before
   writing; do not build a layer with one caller.
3. **Zero service-specific knowledge in code.** No service name, host, port, icon or quirk in Go or
   Svelte. It is all `config.yaml`. The invariants test will fail the build if you break this.
4. **The container never gets root, the Docker socket, `systemctl`, or a shell.** Privileged work goes
   through request files to `meridian-host` (stdlib only, argv only, allowlist only).
5. **No authentication, ever, and no state-changing buttons on the launcher.**

The full list of hard constraints is in [CLAUDE.md](CLAUDE.md). They are not negotiable and not
yours to relax, even when a task seems easier that way.

## When you need the owner

Pi has a question tool (the same style as Claude Code's `AskUserQuestion`: a few questions, 2-4
options each, an "other" free-text escape). Using it plays a sound that pulls the owner back to the
window, so **ask rarely and make each ask count**.

Ask when the answer is genuinely the owner's call and you cannot resolve it from the docs, the code
or an obviously reversible default. Do not ask what the docs answer, do not ask for conventional
defaults, and do not ask for permission to do your task.

How to ask well:
- Batch related questions into one call (up to four). One interruption beats four.
- Put your recommended option first and mark it "(Recommended)".
- Give enough context in each question to answer without scrolling back, with consequences in the
  option descriptions.
- When answered, record the decision as a new numbered entry in
  [docs/DECISIONS.md](docs/DECISIONS.md) in the same commit series as the work it unblocks.

If the question tool is unavailable, unanswered, or the question is not needed to keep working, fall
back to [docs/OPEN-QUESTIONS.md](docs/OPEN-QUESTIONS.md): log it with your recommendation, skip to
the next unblocked task, and never build on a guess.

## Things you must never do

- Edit or delete an existing line of `docs/DECISIONS.md`. Append only.
- Add a dependency that is not on the allowlist in `docs/STANDARDS.md` section 9.
- Skip, weaken or delete a failing test to get green. Use `--no-verify`. Commit a secret.
- Tick a `[human]` task. Those need the real box; write the steps and leave them.
- Run anything that needs the real homelab (`systemctl`, Docker, the tailnet). Tests use fakes at the
  named seams and `httptest`.

## Reference documents

| Doc | Read when |
|---|---|
| [docs/STANDARDS.md](docs/STANDARDS.md) | always; how to write, test, fuzz, secure, commit |
| [docs/agent/](docs/agent/README.md) | always; the loop and the task list |
| [docs/DECISIONS.md](docs/DECISIONS.md) | before changing anything a decision covers |
| [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) | components, networking, polling model, config shape |
| [docs/CONTROL.md](docs/CONTROL.md) | anything under `control`, `hostexec`, `hostprobe`, or `deploy/` |
| [docs/DESIGN.md](docs/DESIGN.md) + `mockups/` | anything under `web/` |
| [docs/INTEGRATIONS.md](docs/INTEGRATIONS.md) | writing `examples/homelab.yaml`, probe kinds, the Ivy `Host` quirk |
| [config.yaml.example](config.yaml.example) | the config vocabulary; it is kept in sync with code by a test |
