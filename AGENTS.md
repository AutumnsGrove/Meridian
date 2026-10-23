# Agents

Entry point for any coding agent working in this repo. `CLAUDE.md` carries the same hard
constraints; this file adds the build-out workflow. If the two ever disagree, `CLAUDE.md` wins and
the disagreement is a bug to log in `docs/OPEN-QUESTIONS.md`.

Meridian is a personal homelab dashboard: one page linking to every self-hosted service, showing
whether each is up, and letting its owner restart, update or stop them from a phone. Go backend,
SvelteKit frontend embedded in one binary, a Docker container, and a small root-owned host binary for
privileged actions.

## Start here, every session

1. `git log --oneline -20` and `git status`. Know where the last session stopped.
2. Read [docs/agent/README.md](docs/agent/README.md): the loop you follow.
3. Open [docs/agent/TASKS.md](docs/agent/TASKS.md): take the **first unticked task** whose
   dependencies are ticked and which is not blocked in
   [docs/OPEN-QUESTIONS.md](docs/OPEN-QUESTIONS.md). Read its stage document.
4. Read [docs/STANDARDS.md](docs/STANDARDS.md) if you have not this session. It is short on purpose.

You do not need to read the whole design set to start a task. Each task names the documents and
decision numbers it depends on; read those, and read more only when the task touches them.

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

## When you are stuck on a design question

Do not guess and do not stop the whole build. Append the question to
[docs/OPEN-QUESTIONS.md](docs/OPEN-QUESTIONS.md) in the format shown there (with your recommended
answer), commit it, and move to the next unblocked task. The owner settles the batch later.

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
