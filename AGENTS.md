# Agents

Entry point for any coding agent working in this repo. [CLAUDE.md](CLAUDE.md) carries the hard
constraints and they are not negotiable; this file adds the maintenance workflow. If the two ever
disagree, `CLAUDE.md` wins and the disagreement is a bug to log in
[docs/OPEN-QUESTIONS.md](docs/OPEN-QUESTIONS.md).

Meridian is a personal homelab dashboard: one page linking to every self-hosted service, showing
whether each is up, and letting its owner restart, update or stop them from a phone. Go backend,
SvelteKit frontend embedded in one binary, a distroless container, and a small root-owned host binary
for privileged actions. The design is settled; [docs/DECISIONS.md](docs/DECISIONS.md) says why.

## The five rules that matter most

1. **Test first, watch it fail, then implement.** Small commits, each green under
   `scripts/check --fast`, the full `scripts/check all` before a push.
2. **Data first.** Types and tables, then small pure functions, with I/O at the edges. Reuse before
   writing; do not build a layer with one caller.
3. **Zero service-specific knowledge in code.** No service name, host, port, icon or quirk in Go or
   Svelte. It is all `config.yaml`. The invariants test fails the build if you break this.
4. **The container never gets root, the Docker socket, `systemctl`, or a shell.** Privileged work goes
   through request files to `meridian-host` (stdlib only, argv only, allowlist only).
5. **No authentication, ever, and no state-changing buttons on the launcher.**

## Making a change

1. Read the doc for the area (table at the bottom) and grep the code before writing a helper.
2. Red: write the test, and the fuzz target if bytes cross a boundary. Watch it fail on the assertion.
3. Green: the least code that passes. Refactor with the tests green.
4. `scripts/check --fast`; commit small, Conventional Commits, tests with the code they test.
5. If the change made a doc sentence false, fix it in the same series. If it made a design choice,
   append it to [docs/DECISIONS.md](docs/DECISIONS.md) — never edit an existing entry.
6. `scripts/check all` before pushing. The pre-push hook runs it too.

## How do I...

### Add a service

Config only, no code. Add a `services:` entry to `config.yaml` with a `section`, a `probe`, an
optional `vital`, the click-through `url`, and an `icon` (a lucide name or a local image). Then
reload: `docker kill -s HUP meridian`, or run the binary with `--watch`. Check it with
`cat /etc/meridian/targets.json | docker exec -i meridian /meridian doctor --allowlist -`, which
probes once and explains anything red. If it should also have buttons, see "add a control target";
the launcher never gains one on its own.

To add a *whole* homelab rather than one service, copy `config.yaml.example`, and note that
[`examples/homelab.yaml`](examples/homelab.yaml) shows every shape in use.

### Add a probe or vital kind

The vocabulary is one table per kind, and everything else reads it:

1. Add a row to `config.HealthKinds` or `config.VitalKinds` in
   [`internal/config/kinds.go`](internal/config/kinds.go): the `Doc` line, `Required`, `Optional` and
   any cross-field `Needs`.
2. Add the runner in [`internal/probe`](internal/probe/) and register it in the registry next to the
   others.
3. Add table tests for the runner, and a fuzz target if the kind parses untrusted bytes (see
   [docs/STANDARDS.md](docs/STANDARDS.md) section 3).
4. Regenerate the vocabulary block at the bottom of [`config.yaml.example`](config.yaml.example)
   with `meridian docs kinds`: the block is compared with that output by a test and fails until they
   agree.
5. Run `scripts/check`. `TestHealthRunners_MatchTheConfigKinds` and its vital twin fail until the
   table, the runner and the example all agree.

### Add a control target

Follow the "Adding a target" checklist in [docs/CONTROL.md](docs/CONTROL.md). In short: an entry in
`/etc/meridian/targets.json` with an explicit `actions` list and `criticality`, the target's unit in
`/etc/meridian/observe.json`, a matching `control:` block in `config.yaml`, `dns: true` if stopping it
breaks name resolution, tests for the refusal paths, one real action from a phone, then update the
matrix in `CONTROL.md` and [docs/INTEGRATIONS.md](docs/INTEGRATIONS.md). The allowlist is root-owned
and never mounted; the container cannot add a target at runtime.

### Cut a release

1. Wait for `main` to be green; the image job follows CI and publishes `:latest` from the commit CI
   tested.
2. Tag it and push the tag: `git tag vX.Y.Z && git push origin vX.Y.Z`. The tag publishes
   `:vX.Y.Z`, builds both `meridian-host` binaries, writes `SHA256SUMS` and creates the release.
3. On the box: `sudo deploy/install.sh --upgrade-host --version vX.Y.Z` for the host binary, then
   update the dashboard from its control row. The host binary is never updated by the container.

### Handle a Dependabot update

Dependabot opens grouped weekly updates for `gomod`, `npm`, `github-actions` and `docker`. Let
`scripts/check all` decide: merge when green. Read the changelog for a major version bump, and remember
that a new Go module or npm package needs a new decision (the allowlist is in
[docs/STANDARDS.md](docs/STANDARDS.md) section 9), while a base-image digest bump only needs the checks.
An offer that is not on an allowlist is a decision for the owner, not a merge.

### Triage a fuzz crasher

A failing fuzz run writes the input under `testdata/fuzz/<Target>/` next to the target. **Commit the
crasher file** and fix the bug with a normal red-green cycle; the seed corpus runs in plain `go test`,
so the crash is then a permanent regression test. Never delete a crasher or re-run until it goes green.
`scripts/check fuzz` (and the weekly `fuzz-long` workflow) runs the engine for real; `FUZZ_TIME=20s`
matches CI.

### Review a change

Read [docs/REVIEW-BRIEF.md](docs/REVIEW-BRIEF.md) first: it lists the hard constraints with the test
that enforces each, the six trust boundaries, the fuzz targets, the decisions most likely to erode and
four review lanes.

There is also an optional automated reviewer, [`.github/workflows/review.yml`](.github/workflows/review.yml),
for changes that arrive as pull requests. It asks Pi, through OpenRouter, to review the diff against
the brief and [docs/STANDARDS.md](docs/STANDARDS.md) and posts one comment, or none. It is **off until
the owner enables it**: add the repository secret `OPENROUTER_API_KEY` and set the repository
variable `REVIEW_MODEL` to a provider-qualified model id such as
`openrouter/anthropic/claude-sonnet-4.6`. Drafts, documentation-only pull requests and forks are
skipped, and the agent sees only the base checkout with read-only tools.

## When you need the owner

Ask with the question tool (batched, recommended option first) only when the answer is genuinely the
owner's call: reversing a decision, adding a dependency, changing the security model, spending money,
or choosing between two designs the docs do not settle. Do not ask what the docs answer, for a
conventional default, or for permission to do your task. If the tool is unavailable, log the question
in [docs/OPEN-QUESTIONS.md](docs/OPEN-QUESTIONS.md) with a recommendation and do not build on a guess.

## Things you must never do

- Edit or delete an existing line of `docs/DECISIONS.md`. Append only.
- Add a dependency that is not on the allowlist in `docs/STANDARDS.md` section 9.
- Skip, weaken or delete a failing test to get green. Use `--no-verify`. Commit a secret.
- Run anything that needs the real homelab (`systemctl`, Docker, the tailnet). Tests use fakes at the
  named seams and `httptest`.
- Give the container a new mount, capability, socket or shell. That shape is a decision, not an edit.

## Reference documents

| Doc | Read when |
|---|---|
| [docs/STANDARDS.md](docs/STANDARDS.md) | always; how to write, test, fuzz, secure, commit |
| [docs/DECISIONS.md](docs/DECISIONS.md) | before changing anything a decision covers |
| [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) | components, networking, polling model, config shape |
| [docs/CONTROL.md](docs/CONTROL.md) | anything under `control`, `hostexec`, `hostprobe`, or `deploy/` |
| [docs/DESIGN.md](docs/DESIGN.md) + `mockups/` | anything under `web/` |
| [docs/INTEGRATIONS.md](docs/INTEGRATIONS.md) | adding a service, a probe kind or a vital kind |
| [docs/REVIEW-BRIEF.md](docs/REVIEW-BRIEF.md) | reviewing a change, or checking what is load-bearing |
| [config.yaml.example](config.yaml.example) | the config vocabulary; a test keeps it in sync with the code |
| [docs/history/](docs/history/PLAN.md) | how it was built; historical, not maintained |
