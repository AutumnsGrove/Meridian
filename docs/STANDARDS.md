# Standards

How code in this repo is written, tested, secured and committed. These are rules, not suggestions;
where a rule has a reason, the reason is written next to it so a judgement call can be made when a
case is not covered. Decisions behind them are in [DECISIONS.md](DECISIONS.md) #44-#71.

There are deliberately **no line-count limits and no coverage percentage gates**. A number invites
gaming it. The standard is the reading test: a stranger can open any file, understand what it does,
and see where its tests are.

## 1. Principles

### Data primacy

Start from the data, then write the smallest functions that move it.

- **Model first.** Before writing behaviour, write the types that hold the facts: config, a probe
  result, a request, a host snapshot. Plain structs with exported fields, no behaviour hidden in
  setters, no embedded mutexes in value types.
- **Behaviour is `func(Data) Data`.** Core logic is pure: input in, value out, no clock reads, no
  network, no globals. Merging a fresh probe result into a stale one is a function. Picking the next
  poll interval is a function. Deciding whether a request is stale is a function. These are the
  things that get table tests and fuzz targets.
- **I/O lives at the edges**, in a few thin functions that fetch, read a file, write a file or
  `exec`, then hand plain data to the pure core.
- **Tables over branches.** When behaviour varies by a closed set of names (probe kinds, vital
  kinds, actions, statuses), put the set in one `map` or slice of structs and look it up. Do not
  write a `switch` on the name in three places. One table feeds validation, execution, `doctor` and
  documentation, so they cannot disagree. This is also how decision #38 stays true: the vocabulary
  is data, never `if service == ...`.
- **The wire is data too.** Anything crossing a process or language boundary (JSON API, request
  files, `host.json`, audit lines) has exactly one Go type definition, one validation function, and
  one golden file shared with the TypeScript tests.

### Reusable functions over single-caller layers

- A helper is named for **what it does**, never for who calls it (`jsonpath.Get`, not
  `getAdguardField`), and works for any caller with the right input type.
- Before writing a helper, grep for one that already does it. Before writing a second, make the
  first one general.
- Do not create an interface, wrapper, "manager", "service", "factory" or package that has exactly
  one caller and exists only to be called from there. Write the function in place; extract it the
  second time it is needed, not before. **The one exception** is a seam at an I/O boundary that
  tests need to replace (a clock, an HTTP fetcher, a command runner). Those are tiny, defined by the
  consumer, and usually a single function type rather than an interface.
- Prefer composition of small functions over inheritance-shaped designs. Go has no inheritance;
  do not simulate it with embedding to share behaviour.

### Clean over clever

- Obvious beats short. If a reader has to stop and decode it, rewrite it.
- A function does one thing and its name says what. If you need "and" in the name, split it.
- No speculative generality: no option struct for a parameter with one value, no config for a thing
  that has never needed to vary.
- No dead code, no commented-out code, no TODOs without an `OPEN-QUESTIONS.md` entry. The git
  history remembers.
- Comments explain **why** (a constraint, a surprise, a link to a decision). The code says what.
- Names: full words, no abbreviations except universal ones (`ctx`, `err`, `id`, `url`, `ok`).
  Booleans read as questions (`isStale`, `hasAck`).

## 2. Test-driven development

The order is fixed:

1. **Write the test.** Name it for the behaviour (`TestMerge_KeepsLastGoodValueWhenProbeFails`).
2. **Run it and watch it fail for the right reason.** A test that was never red proves nothing. If
   it fails to compile, add only enough stub to make it compile and fail on the assertion.
3. **Write the least code that passes.**
4. **Refactor with the tests green.** This is where duplication gets folded into reusable functions.
5. **Run `scripts/check`, then commit** (see section 7).

Rules:

- **Integration over mocks.** Prefer the real thing at small scale: `httptest.Server` for services,
  real files under `t.TempDir()`, real JSON fixtures in `testdata/`, a real gateway handler driven
  by `httptest`. No mocking framework, ever. A hand-written fake is allowed only for the I/O seams
  named in section 1 (clock, command runner, DNS), and the fake must be a few lines.
- **Table-driven** for any function with more than two interesting inputs. Each row has a `name`.
- **Test behaviour, not structure.** Tests call exported functions. If you need to test an
  unexported function to feel safe, the thing is probably a missing package or an untestable seam.
- **Error cases are first-class.** Every function that returns an error has a test that makes it
  return one, and asserts on the **kind** of error (sentinel or type), not its text.
- **Determinism.** No `time.Sleep` in tests, no real wall clock, no network beyond `httptest`, no
  ordering dependence. Inject the clock. Run with `-race` and `-shuffle=on`.
- **Golden files** (`testdata/*.golden.json`) for wire output, regenerated only with an explicit
  `-update` flag and reviewed in the diff.
- **Do not delete or weaken a failing test to make the build pass.** A red test is information. If
  the test is wrong, say why in the commit message.
- A bug fix starts with a test that reproduces the bug.
- This is not a test matrix. The task docs list behaviours and edge cases worth covering as a
  starting point; thinking of more is part of the job.

Frontend: same order. Pure logic in `.ts` modules gets Vitest tests first; components get
Testing Library tests for behaviour a user can see (text, roles, disabled state); one Playwright
suite runs against the real built binary.

## 3. Fuzzing

Fuzzing is for code that takes bytes or strings from outside a trust boundary, and for pure
functions with a property that should hold for all inputs.

**Go**: native `testing.F` (no library needed), one `FuzzXxx` per target, next to the tests.

| Target kind | Property to assert |
|---|---|
| Parsers (config YAML, request JSON, `host.json`, audit line, UUID) | never panics; returns a typed error or a value that passes its own validator |
| Round trips (marshal/unmarshal, encode/decode) | `decode(encode(x)) == x` |
| Pure transforms (`jsonpath.Get`, number formatting, search-URL building, interval selection) | no panic; output within documented bounds; idempotence where it applies |
| Validators on the security boundary (request validation, allowlist lookup, path joins, image-reference check) | **never accepts** an input outside the grammar; checked against an independent, deliberately dumb reference when one exists |

- Seed with real examples (the fixtures) and with the known nasty ones: empty, huge, unicode, NUL,
  deeply nested JSON, `../`, trailing newline, duplicate keys.
- Fuzz targets must be fast (no network, no sleeping) and deterministic for a given input.
- Crashers go in `testdata/fuzz/<Target>/` automatically; **commit them**, they are the regression
  suite. A crasher is fixed with a normal red-green cycle.
- `go test` runs every seed corpus as a plain test, so the corpus is checked by every commit for
  free. The full `scripts/check` also runs every target for real, for `FUZZ_TIME` (5s locally, 20s
  in CI, 10 minutes weekly). Fuzzing is probabilistic: a clean local run makes a red CI run less
  likely, not impossible. A red `fuzz-smoke` job is answered by committing the crasher it found, never
  by re-running until it goes green.

**Frontend**: [`fast-check`](https://fast-check.dev) property tests for the pure `.ts` modules
(search URL builder, age formatting, armed-button state machine, status parser).

## 4. Security standards

The project's hard constraints live in [CLAUDE.md](../CLAUDE.md) and are mechanically enforced by the
invariants test (task T03). The rules below are what to do inside them.

**Inputs and parsing**
- All external input (HTTP bodies, query strings, request files, probe responses, config, `host.json`)
  is untrusted until a validator has accepted it. Decode strictly (`DisallowUnknownFields`,
  `yaml.KnownFields(true)`), validate, then convert to an internal type. Never pass raw decoded data
  onward.
- Cap everything: request bodies (`http.MaxBytesReader`, 8 KiB), probe response bodies
  (`io.LimitReader`, 1 MiB), request files (4 KiB), JSON nesting depth, config file size, string
  lengths in validated fields.
- Identifiers that become paths or unit names (`id`, `target`, file names) must match a strict regexp
  (`^[a-z0-9][a-z0-9-]{0,31}$` for slugs, canonical UUID for request ids). Check **before** joining
  into any path. Use `os.Root` (or `openat` with `O_NOFOLLOW`) for any file access inside a
  directory another party can write to.

**Network**
- Every outbound call has a context deadline and a body cap. The shared HTTP client does **not**
  follow redirects (a login redirect is a valid healthy answer) and has no cookie jar.
- Probe URLs must be `http` or `https`. A probe never sends credentials to a host other than the one
  in its configured URL.
- The server sets `ReadHeaderTimeout`, `ReadTimeout`, `WriteTimeout`, `IdleTimeout` and
  `MaxHeaderBytes`. No handler spawns an unbounded goroutine per request.
- Mutating routes pass the browser guard (decision #54): JSON content type, same-origin
  `Sec-Fetch-Site`/`Origin`, and a `Host` on the allowlist. This is not authentication and must not
  grow into it; it stops a malicious web page, or DNS rebinding, from driving the tailnet-only API.
- Responses carry `X-Content-Type-Options: nosniff`, `Referrer-Policy: no-referrer`,
  `X-Frame-Options: DENY`; `/api/*` is `Cache-Control: no-store`. Outbound links use
  `rel="noopener noreferrer"`.

**Secrets**
- Secrets are read from the environment by the variable name config gives, at the moment of use.
  They are never in config, never in a struct that gets logged or marshalled, never in an error
  string, never in a JSON response, never in the browser. Anything that prints config (`doctor`,
  logs) prints variable **names**, not values.
- `gitleaks` runs in CI; a leaked secret is rotated, not just removed.

**Privilege (the host binary and anything near it)**
- `meridian-host` is stdlib-only. It never invokes a shell. Commands are argv arrays read from the
  root-owned allowlist; the request only selects among them (CONTROL.md "What Meridian will never
  do").
- It checks that its own allowlist is root-owned and not group/world-writable before trusting it.
- It opens request files with `O_NOFOLLOW`, requires a regular file, enforces the size cap, and
  never echoes file contents into an error or result.
- Subprocesses get an explicit minimal environment and an explicit `Credential` (uid/gid/groups) for
  the run-as-deploy-user cases, plus a context timeout and a process-group kill.
- Anything ambiguous is a refusal. A refusal is always written to the result and the audit log.

**Errors and logging**
- Errors wrap with `%w` and add context at the boundary that has it (`fmt.Errorf("probe %s: %w", id, err)`).
  Log **or** return an error, never both. Lowercase messages, no trailing punctuation.
- User-facing failure text (the `reason` on a tile, a refused action) is a short sentence built
  from a known set of causes. It never contains a stack trace, file path outside config, or secret.
- Logging is `charmbracelet/log`, structured key/value, no secrets, no request bodies.

**Container**: see [ARCHITECTURE.md](ARCHITECTURE.md) and decision #57 (distroless, non-root,
read-only root filesystem, all capabilities dropped, `no-new-privileges`, pinned base digests).

## 5. Go conventions

- Go version is the one in `go.mod`; do not use features newer than it.
- Module `github.com/autumnsgrove/meridian`. Packages under `internal/`; `cmd/*/main.go` are thin
  (parse flags, call into `internal/cli`, exit code). Nothing outside `cmd/` calls `os.Exit` or
  `log.Fatal`.
- `net/http` route patterns (`mux.HandleFunc("GET /api/status", ...)`). Handlers are small: decode,
  call a pure function, encode. No business logic in handlers.
- `context.Context` is the first parameter of anything that does I/O or blocks. Every goroutine has
  a clear owner and stops when its context is cancelled; `go test -race` must pass.
- No package-level mutable state, no `init()` side effects. Pass dependencies as arguments.
- Return concrete types, accept the narrowest input (a `func`, or a one-method interface defined in
  the consumer's package).
- Exported identifiers have a doc comment that says what they guarantee, not how they work.
- Format with `gofmt`/`goimports`; lint with `golangci-lint` (config in `.golangci.yml`, enabling at
  least `errcheck govet staticcheck gosec errorlint gocritic revive unused ineffassign bodyclose
  noctx contextcheck`). A `//nolint` needs a reason on the same line.

## 6. Frontend conventions

- SvelteKit 5, Svelte 5 runes, TypeScript strict, `adapter-static` as an SPA (`ssr = false`), plain CSS
  custom properties from [DESIGN.md](DESIGN.md) tokens. No Tailwind, no component library.
- **No service knowledge**, same as Go: the frontend renders whatever `/api/status` contains. It
  does not know what kinds exist, what a service is called, or which URL to hit.
- Logic lives in `src/lib/**/*.ts` as pure functions with tests. Components stay thin: props in,
  markup out, events up. State machines (the armed button, the poll loop) are reducers with tests.
- The API client parses responses with a hand-written validator and the shared golden files; it
  never casts `as Status`.
- Accessibility is tested, not hoped for: every interactive element is at least 44x44 CSS px and has
  a visible focus ring (checked in Playwright at 390x844 with `axe`), every status carries a
  non-colour mark, and the page works with the keyboard.
- Content security policy comes from the `csp` option of SvelteKit's Vite plugin (hash mode); no
  inline event handlers,
  no `{@html}` of anything from the API.
- Copy rules from CLAUDE.md apply to every user-facing string: no em dashes, no exclamation marks,
  second person, present tense.
- Package manager is `pnpm`, lockfile committed, installs use `--frozen-lockfile`.

## 7. Git

- **Small commits.** One logical change each. A reader of `git log -p` should be able to review a
  commit in a couple of minutes. Tests ship with the code they test. A refactor is its own commit.
  A documentation change that became true because of a code change ships in that commit.
- **Conventional Commits**: `type(scope): imperative subject` (`feat(probe): add dns kind`,
  `test(config): reject undeclared section`, `fix(gateway): cap request body`). Types: `feat fix test
  refactor docs build ci chore perf`. Body explains **why** when it is not obvious.
- **Every commit must pass `scripts/check --fast` before it is made**, and the pre-commit hook
  enforces it. CI only sees the tip of whatever batch is pushed, so a broken middle commit would
  otherwise never be caught, and `git bisect` would be useless.
- A push runs the full `scripts/check`, fuzz pass included, and the pre-push hook enforces it. Pushes
  are batched, so this is once per batch, which is exactly when it matters.
- Never commit generated output (`web/build/` aside from `.gitkeep`, binaries, coverage), secrets,
  or the real `config.yaml`.
- Never rewrite published history. Never `--no-verify` to get past a hook; fix the cause.
- The decision log is append-only (see CLAUDE.md).

## 8. CI

One entry point, shared by hands and machines: **`scripts/check [go|web|e2e|security|all] [--fast]`**.
CI jobs call the same script, so green locally means green in CI. `--fast` is what the pre-commit
hook runs (format, vet, lint, `go test` including fuzz seeds, web unit tests); it excludes the
slow jobs.

Workflows (`.github/workflows/`):

| Workflow | Trigger | Does |
|---|---|---|
| `ci.yml` | push to `main`, manual | `go` (gofmt, vet, golangci-lint, `go test -race -shuffle=on`, build both binaries), `web` (frozen install, `svelte-check`, eslint, prettier, vitest, build), `fuzz-smoke` (`scripts/check fuzz` with `FUZZ_TIME=20s`), `e2e` (Playwright against the real binary), `security`, `image` (build, no push, scan) |
| `fuzz-long.yml` | weekly cron, manual | `scripts/check fuzz` with `FUZZ_TIME=10m`, uploads crashers as an artifact |
| `release.yml` | added in Phase 4 | multi-arch image to GHCR by digest, host binary + `SHA256SUMS` as release assets |

`security` job: `govulncheck`, `gitleaks`, `pnpm audit --prod`, `actionlint`, `shellcheck` (for the
few shell files), and Trivy on the built image.

Workflow hygiene: top-level `permissions: contents: read`, granting more per job only where needed;
third-party actions pinned to a full commit SHA with the version in a comment; no
`pull_request_target`; no secrets in `ci.yml`; `concurrency` cancels superseded runs of the same ref;
Dependabot (weekly, grouped) for `gomod`, `npm`, `github-actions` and `docker`.

There is no review workflow in this phase of the project (decision #50).

## 9. Dependencies

Fewer is better; each one is code we did not write and cannot fully audit.

**Go allowlist** (direct requirements; the indirect dependencies an allowed module brings are
accepted): `go.yaml.in/yaml/v3`, `github.com/spf13/cobra`, `github.com/charmbracelet/log`,
`golang.org/x/net/dns/dnsmessage`. `internal/hostexec`, `internal/hostprobe`, `internal/hostfile`,
`internal/wire`, `internal/atomicfile` and `cmd/meridian-host` import **only the standard library**
(enforced by the invariants test). Test-only helpers: none; use the standard library.

**Web allowlist**: `svelte`, `@sveltejs/kit`, `@sveltejs/adapter-static`, `vite`,
`@fontsource-variable/lexend`, `@lucide/svelte`; dev only: `typescript`, `svelte-check`, `eslint`
(+ svelte plugin), `prettier` (+ svelte plugin), `vitest`, `@testing-library/svelte`, `jsdom`,
`@playwright/test`, `@axe-core/playwright`, `fast-check`. The toolchain's required companions are
allowed with the package that needs them (decision #72): `@sveltejs/vite-plugin-svelte`,
`typescript-eslint`, `@eslint/js`, `@testing-library/dom`, and `@types/node` for the Node-side
scripts Vite and Playwright run (decision #75).

Adding anything else needs a new decision entry saying what it replaces and why the standard
library or existing dependency is not enough. Versions are pinned by the lockfiles; upgrades arrive
through Dependabot and must pass `scripts/check all`.

## 10. Definition of done for a task

A task in [docs/history/build-out/TASKS.md](history/build-out/TASKS.md) is done when:

1. The behaviours it lists have tests that were seen failing first.
2. Any fuzz targets it lists exist, with seeds.
3. `scripts/check` is green.
4. No service-specific string or privilege-boundary rule was violated (the invariants test says so).
5. Docs that the change made false are corrected in the same commit series, and any new design choice
   is appended to DECISIONS.md.
6. The task is ticked in TASKS.md in its own small commit.

Work happens in short sessions with a cleared context between them (decision #70); the rules for
stopping cleanly are in [history/build-out/README.md](history/build-out/README.md).

If a task needs the real homelab box (marked `[human]`), the agent writes the exact steps and leaves
the box unticked for the owner.
