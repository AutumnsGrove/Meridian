# Stage 1: Foundation

Goal: an empty but fully gated project. After this stage a commit that violates a standard cannot be
made, and a push that violates one fails CI. Read [STANDARDS.md](../STANDARDS.md) sections 5, 7, 8.

## T01 Repo scaffold

**Build**
- `go.mod`: module `github.com/autumnsgrove/meridian`, `go 1.26`. Dependencies are added by the task
  that first imports them, never ahead of time.
- Directory skeleton from [README.md](README.md) "Layout", created lazily. At this point only:
  `cmd/meridian/main.go`, `cmd/meridian-host/main.go`, `internal/cli/` with a `Version()` function
  and test, `web/embed.go`, `scripts/`, `.githooks/`, `testdata/`.
- `cmd/meridian/main.go` is a few lines: build args, call `cli.Main(os.Args[1:], os.Stdout,
  os.Stderr)`, `os.Exit` with the returned code. `cli.Main` takes writers so it is testable.
  `cmd/meridian-host/main.go` is a stub here and must not import `internal/cli`: the host binary is
  standard library only (decision #56), so it gets its own dispatch in T22.
- `web/embed.go` declares `//go:embed all:build` over an `embed.FS`. Commit `web/build/.gitkeep`
  (and adjust `.gitignore` to `web/build/*` plus `!web/build/.gitkeep`) so `go build` works before
  the frontend exists. The embed package exposes `FS() fs.FS` rooted at `build/`.
- Version info: `internal/cli` holds `var version, commit, date string` set by `-ldflags -X`;
  `meridian version` prints them; defaults are `dev`.
- `.golangci.yml` enabling the linters in STANDARDS section 5; `.editorconfig`; `.gitattributes`
  already exists.
- `.githooks/pre-commit` runs `scripts/check --fast` and exits non-zero on failure. Document
  `git config core.hooksPath .githooks` in the top of `scripts/check`'s help and run it once as the
  last step of this task.
- `Makefile`-free: `scripts/` is the interface. No Make.

**Tests first**: `cli.Main` returns exit code 0 and prints a version line for `version`, non-zero
and usage to stderr for an unknown command. That is all the behaviour that exists yet.

**Done when**: `go build ./...`, `go vet ./...` and `go test ./...` pass; `go build` of both
binaries works with an empty `web/build/`.

## T02 `scripts/check` and `scripts/fuzz`

**Build**: POSIX `sh` or bash with `set -eu`, `shellcheck`-clean.

`scripts/check [go|web|e2e|security|all] [--fast]`, default `all`:

| Group | Commands |
|---|---|
| `go` | `gofmt -l` must print nothing; `go vet ./...`; `golangci-lint run`; `go test -race -shuffle=on ./...` (`--fast` drops `-race`); `go build ./cmd/...` |
| `web` | skipped with a notice if `web/package.json` does not exist yet. Else `pnpm install --frozen-lockfile`, `pnpm check` (svelte-check), `pnpm lint`, `pnpm format:check`, `pnpm test`, `pnpm build` (`--fast` skips build) |
| `e2e` | build web and binary, run Playwright (skipped under `--fast`) |
| `security` | `govulncheck ./...`, `gitleaks detect`, `pnpm audit --prod`, `actionlint`, `shellcheck scripts/* deploy/**/*.sh` (skipped under `--fast`) |

Missing tools are an error with an install hint. The security group is skipped under `--fast`, so
`gitleaks` is only required for the full run. Each group prints a one-line header so CI logs are navigable.

`scripts/fuzz [duration]` discovers every `func FuzzXxx(` with `go test -list` and runs each with
`-fuzz=^FuzzXxx$ -fuzztime=<duration>` (default 20s) in its own package. It exits non-zero on the
first crasher and prints the path of the saved corpus entry.

**Tests**: shell scripts are tested by running them. Add a CI-less self-check: `scripts/check help`
exits 0 and lists the groups.

## T03 Invariants test

The hard constraints in CLAUDE.md, mechanised, so a weaker model cannot drift without a red build.
`internal/invariants/invariants_test.go`, a table of rules, each rule a name plus a function over
the parsed repo. Use `go/parser` and `go/build`-free directory walking; no dependencies.

Rules to include (add rows, do not add branches):

| Rule | Check |
|---|---|
| no service names in code | Collect every `id`, `name` and service URL host from `examples/homelab.yaml` (when it exists; loopback hosts and the project's own name are not service knowledge), plus the hard-coded words `polaris ivy bwmon adguard searxng vaultwarden unbound her-go`, and fail if any appears (case-insensitive, word-boundary) in a non-test `.go` file or in `web/src/**` outside test files. Tests, fixtures and the host-side packages (`cmd/meridian-host`, `internal/hostexec`, `internal/hostprobe`, `internal/wire`) are exempt |
| shell-free | no call to `exec.Command` / `CommandContext` whose first argument is `sh`, `bash`, `-c`-style shell, anywhere |
| `os/exec` confinement | `os/exec` is imported only by `internal/hostexec` and `internal/hostprobe` (and `cmd/meridian-host`); tests exempt. Nothing in the container-side packages may import it |
| no `systemctl`/`docker` strings in the container side | the literals `systemctl`, `docker.sock`, `tailscaled.sock` appear only in `internal/hostexec`, `internal/hostprobe`, `cmd/meridian-host` and docs |
| host binary is stdlib-only | every import in `internal/hostexec`, `internal/hostprobe`, `internal/wire`, `internal/atomicfile`, `cmd/meridian-host` is in the standard library |
| dependency allowlist | `go.mod` requires only modules listed in STANDARDS section 9 (parse the `require` lines of `go.mod` by hand; no extra dependency) |
| no auth surface | no identifier or string literal named `login`, `session`, `cookie`, `csrf`, `password` in non-test Go code under `internal/gateway` and `internal/control` (the `basic_env` config names are exempt because they live in `config`/`probe`) |
| no in-place JSON writes | `os.WriteFile` is not called in `internal/control`, `internal/hostexec`, `internal/hostprobe`; those must use `atomicfile` |
| one mutating route | the gateway's route table is data; the test asserts the only non-`GET` pattern is `POST /api/control` |
| `os.Exit`/`log.Fatal` only in `cmd/` | |

Each rule has a deliberately failing fixture under `internal/invariants/testdata/` proving the rule
can fail (a rule that cannot fail is decoration). Rules that depend on files not yet present pass
vacuously and say so in their name (`..._when_present`).

**Done when**: `go test ./internal/invariants` passes on the scaffold and every rule has a failing
fixture test.

## T04 CI workflows and Dependabot

**Build** `.github/workflows/ci.yml`, `fuzz-long.yml`, `.github/dependabot.yml`, per STANDARDS
section 8. Jobs call `scripts/check <group>`; they add only setup (Go via `go.mod`, pnpm, Node LTS,
Playwright browser cache, tool installs). Rules:
- Pin every third-party action to a full SHA with a trailing `# vX.Y.Z` comment. Verify the SHA
  exists for that tag; do not invent one.
- `permissions: contents: read` at the top. `ci.yml` has no secrets and no write permissions.
- Trigger `push` on `main` and `workflow_dispatch`. `concurrency: group: ci-${{ github.ref }}`,
  `cancel-in-progress: true`.
- `fuzz-smoke` runs `scripts/fuzz 20s`; `fuzz-long.yml` runs `scripts/fuzz 10m` weekly and uploads
  `**/testdata/fuzz/**` changes as an artifact on failure.
- `image` job is a placeholder that is enabled by T23; leave it out until then rather than stubbing.
- Web jobs are conditional on `web/package.json` existing, so CI is green from the first push.
- Lint the workflows with `actionlint` in the `security` group.

**Tests**: `actionlint` clean. There is nothing else to unit test; the first real push is the test,
which is why this task also checks that every command the workflow runs works locally.

**Stage done**: fresh clone, `git config core.hooksPath .githooks`, `scripts/check` green, a
deliberately unformatted Go file is rejected by the hook.
