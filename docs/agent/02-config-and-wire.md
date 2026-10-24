# Stage 2: Config and wire types

Goal: the two pieces of data everything else is built on. Config is the whole product for anyone
but us (decisions #38-42); wire types are the contracts between the container, the host and the
browser. Read [ARCHITECTURE.md](../ARCHITECTURE.md) "Config" and `config.yaml.example` first.

## T05 `atomicfile` and the `wire` host snapshot

**`internal/atomicfile`**
- `Write(dir, name string, data []byte, mode fs.FileMode) error`: create a temp file **in the same
  directory** (so rename is atomic), write, `fsync` the file, `rename` into place, `fsync` the
  directory. Never leaves a partial `name`. Cleans up the temp file on any error.
- Takes `dir` and a bare `name` and rejects a `name` containing a separator or equal to `.`/`..`.
- Decision #36 requires this for every handoff file. The control queue needs the two-directory
  variant (write in `queue-tmp/`, rename into `queue/`): `WriteVia(tmpDir, dir, name, ...)`.

**`internal/wire`** (stdlib only, shared by both binaries)
- `HostSnapshot` matching the JSON in ARCHITECTURE.md: `at`, `host{uptime_s, load1}`,
  `units{name -> {active, since}}`, `tailscale{online, exit_node, peers_online, peers_total}`.
  Pointer fields where "absent" differs from zero (`tailscale` may be missing).
- `ParseHostSnapshot([]byte) (HostSnapshot, error)`: strict, size-capped (64 KiB), `at` required.
- `HostSnapshot.Age(now time.Time) time.Duration` and `IsFresh(now, maxAge) bool` (pure).
- `Field(path string) (any, bool)` on the snapshot for `host_field` probes: a dotted path into the
  JSON form (`tailscale.online`). Marshal once to a generic tree and call `jsonpath.Get`. This task
  therefore depends on T08; do T08 first. (Reuse over rewriting: no second path walker.)
- `ID`: `NewID(now time.Time, rand io.Reader) (string, error)` produces a UUIDv7, returning the
  random source's error rather than inventing an id; `ParseID(string) (string, error)` accepts only
  canonical lowercase UUID text (36 chars, hyphens at the right places, version 4 or 7 nibble). No
  dependency.

**Tests first**: atomic write leaves either the old or the complete new content when the writer is
interrupted (use a failing `io.Writer`/injected error between write and rename); snapshot parser
accepts the doc example and rejects unknown fields, missing `at`, oversized input; `IsFresh` at the
boundary; `ParseID` table of good and malicious strings.

**Fuzz**: `FuzzParseHostSnapshot`, `FuzzParseID` (property: accepted IDs round-trip through
`NewID`-shaped validation and contain only `[0-9a-f-]`).

## T06 Config types, strict load, validation

Package `internal/config`. Read the whole of `config.yaml.example` and `docs/INTEGRATIONS.md`.

**Types** are plain structs with `yaml:"..."` tags, mirroring the template:

```go
type Config struct {
    Listen     string
    Server     Server       // allowed_hosts (decision #54); public_url optional
    Sections   []Section
    Poll       Poll
    HostStatus *HostStatus  // nil: host probes report unknown
    Search     Search
    Control    ControlSettings
    Services   []Service
}
type Service struct {
    ID, Name, Section, Description, URL string
    Icon    Icon              // exactly one of Lucide / Image
    Disabled bool             // configured off: shown as `disabled`, never probed
    Probes  Probes
    Control *ServiceControl   // nil: read-only tile
}
type Probes struct { Health Probe; Vitals []Probe }
```

`Probe` is a **flat union**: every field any kind uses (`Kind, URL, Path, Equals, OneOf, Exists,
Expect, ExpectClass, Addr, Qname, ExpectRcode, Unit, Label, Where, Field, Fields, Scale,
Numerator, Denominator, Prefix, Suffix, Decimals, Auth`; `Unit` serves both `host_unit` and
`json_sum`). Strict decoding (`KnownFields(true)`)
rejects typos. `Equals` is `any` (string, bool or number from YAML). A custom `UnmarshalYAML`
also records which keys were present (`Set map[string]struct{}`) so validation can say "kind
`dns` does not take `url`" instead of silently ignoring it.

`Icon` unmarshals from a string (lucide) or `{image: path}` (decision #43).

**Load**: `Load(path string) (*Config, []Warning, error)` reads with a 256 KiB cap, decodes strictly,
applies defaults (`poll.interval 15s`, `idle_interval 2m`, `idle_after 10m`, `timeout 4s`,
`refresh_min 5s`, `control.queue_depth 8`, etc., from the template), validates, returns **all**
validation problems at once as one error wrapping `[]Problem` (`Problem{Path, Code, Message}`, where
Path is like `services[2].probes.health.url` and Code is a stable string like `service_url_invalid`).
A user fixing a config should not need five round trips. `Parse([]byte)` is the pure half of Load so
the loader can be fuzzed without touching the filesystem.

**Validation rules** (each is a table row with a test):
- ids: slug regexp, unique across services; section ids unique; every service's `section` declared
  (decision #39); at least one section and one service.
- `listen` parses as `host:port`; `server.allowed_hosts` entries are `host` or `host:port`, no
  scheme, no path. Empty means loopback names only.
- `poll` durations parse (Go syntax) and sit in sane bounds (`timeout` 500ms to 30s, `interval`
  at least 5s, `timeout` less than `interval`, `idle_interval` at least `interval`).
- every probe URL is `http` or `https` with a host; no userinfo in URLs.
- `search.buttons`: each `url_template` is http(s) and contains `{q}` exactly once; labels
  non-empty, at most 4 buttons.
- icons: lucide name matches `^[a-z0-9]+(-[a-z0-9]+)*$`; image path must match
  `^icons/[A-Za-z0-9._-]+\.(png|webp|jpg|jpeg|gif)$` (a file in the `icons/` directory next to the
  config, no SVG because it can carry script, no `..`, no subdirectories).
- `auth`: exactly one of `basic_env` (two names), `bearer_env` (one name), `header` (map). Env var
  names match `^[A-Z_][A-Z0-9_]*$`. Header names and values are checked for control characters.
  Never read the environment at load; that is `doctor`'s and the probe's job.
- top-level `control` settings: `reboot` (bool, default false) enables the emergency reboot section;
  `inbox_dir`, `outbox_dir` replace the old `queue_dir`/`results_dir`/`audit_file` keys (decision #63).
- per-service `control`: `target` is a slug; `actions` are a subset of `start stop restart update`, no
  duplicates; `dns: true` requires `stop` in actions to mean anything (warn otherwise);
  `no_update_reason` with `update` in actions is a conflict.
- kind-specific rules come from the kinds table (T07). In this task, validate only that `kind` is
  non-empty; T07 completes it.

**Tests first**: a table of broken configs, one rule each, asserting the `Problem.Path` and a
stable error `Code` (add a `Code string` to `Problem`, e.g. `section_undeclared`; tests assert the
code, not the English). A config with three mistakes reports three problems. Defaults applied. A
valid copy of `config.yaml.example` loads clean.

**Fuzz**: `FuzzLoad` over arbitrary bytes: never panics, never returns `(non-nil config, error)`,
and if it returns a config, re-validating it reports no problems (validation is idempotent).

## T07 Kinds table, registry sync, example configs, portability fixture

**The kinds table** (decision #40; this is the single place the vocabulary exists):

```go
type KindRules struct {
    Doc      string   // one line; used by doctor and the generated docs
    Required []string // yaml keys that must be present
    Optional []string // yaml keys that may be present
    Needs    []Need   // cross-field rules, e.g. exactly one of equals/one_of/exists
}
var HealthKinds = map[string]KindRules{ "http_status": {...}, "http_json": {...}, "dns": {...},
    "host_unit": {...}, "host_field": {...}, "self": {...} }
var VitalKinds  = map[string]KindRules{ "json_field": {...}, "json_count": {...}, "json_sum": {...},
    "json_ratio_pct": {...}, "host_field": {...} }
```

Derive the rules from the vocabulary at the bottom of `config.yaml.example`. Validation of a probe
is then one generic function: look up the kind, check `Set` against Required/Optional/Needs. No
per-kind `switch`. `auth` is allowed on every http-using kind.

**Registry sync test**: later, `probe` exposes `HealthRunners` and `VitalRunners` maps keyed by kind.
Add `internal/probe/registry_test.go` in T10/T11 asserting the key sets equal `config.HealthKinds`
and `config.VitalKinds`. Add the config-side half now: a test that every kind named in the
vocabulary comment of `config.yaml.example` is in the tables and vice versa (parse the comment
block; it is the documented contract).

**Example configs**
- Update `config.yaml.example` with the `server:` block (`allowed_hosts`) and keep it loading clean.
- Update the existing `examples/homelab.yaml` to the same schema (add `server.allowed_hosts` with the
  `.ts.net` name) and make it a loading-clean fixture in tests. Also set `control.reboot: true`
  there: the default is `false`, and this deployment is supposed to have the emergency reboot
  (decision #19). The template keeps `false` so a stranger doesn't inherit a reboot button.
- **Portability fixture** `testdata/portable/config.yaml`: services this homelab has never heard of
  (`gitea`, `jellyfin`, a raw TCP-ish `host_unit` service, a service with no web UI), different
  section ids (`media`, `dev`), different search buttons. Loads clean. Used again by the gateway
  and Playwright tests in later tasks (decision #42).

**Tests**: every config in the repo loads (`config.yaml.example`, `examples/homelab.yaml`,
`testdata/portable/config.yaml`); a probe with a field its kind does not take yields the right
problem code; the `Needs` rules ("exactly one of equals/one_of/exists") each have a passing and
failing case.

**Stage done**: all three example configs load; `go test ./internal/config` has a broken-config
table with one row per rule; fuzz targets have seed corpora.
