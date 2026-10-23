# Architecture

Two processes and one boundary. A container that reads and renders, and a host-side watcher that
acts. Everything below follows from the decision that the container never gets the Docker socket and
never gets to run a command it was not explicitly allowlisted for.

```
┌─ Mac / phone (tailnet) ─────────────────────────────────────┐
│  browser → https://lepotato.taild95077.ts.net:8891          │
└──────────────────────────┬──────────────────────────────────┘
                           │ tailscale serve (TLS, tailnet only)
┌──────────────────────────▼──────────────────────────────────┐
│ meridian container  (network_mode: host, 127.0.0.1:8891)    │
│   gateway   /healthz, /api/status, /api/control, /api/…     │
│   probe     15s cached, server-side, parallel               │
│   control   writes inbox/queue/<id>.json, reads outbox/     │
│   web       SvelteKit, embedded via go:embed                │
└───────┬──────────────────────────────┬──────────────────────┘
        │ HTTP/DNS over host loopback  │ reads + writes files only
        ▼                             ▼
  AdGuard, Ivy, bwmon,          ~/Meridian/control/{inbox,outbox}
  Polaris, SearXNG,             ~/Meridian/status/host.json  (read)
  Vaultwarden, unbound                │
                                      │ inotify
                                      ▼
┌─────────────────────────────────────────────────────────────┐
│ host: meridian-control.path → meridian-control.service      │
│   compose/watcher/control.sh   (as deploy user, flock)      │
│     └── sudo -n /etc/meridian/control-run <request.json>    │
│           validates /etc/meridian/targets.json (root-owned) │
│           runs systemctl / docker / systemd-run only        │
│           writes results/<id>.json + audit.jsonl            │
│                                                             │
│ host: meridian-probe.timer (30s) → probe.sh                 │
│   systemctl is-active + tailscale status --json             │
│     → status/host.json   (the only way unit/tailnet state   │
│                            reaches the container)           │
└─────────────────────────────────────────────────────────────┘
```

> **Implementation notes (decisions #51, #52, #63).** Go packages live under `internal/`. The host
> side (`control-run`, `control.sh`, `probe.sh` in the diagram above) is one stdlib-only Go binary,
> `meridian-host`, with subcommands `exec`, `probe`, `update` and `targets`. The container's
> writable control area is a single `control/inbox/` mount; results and the audit log are in a
> read-only `control/outbox/`. Where this document and a decision disagree, the decision wins.

## Networking

The container runs with `network_mode: host`.

That is not laziness, it is the only arrangement that makes the probe URLs honest. On a bridge
network, `localhost:8899` inside the container is *the container*, so every probe would report red
while the services are fine — the single most expensive bug this design could ship. The two
alternatives are worse: `host.docker.internal` requires an `extra_hosts` entry and then every probe
URL carries a fake hostname, and the tailnet IP works but sends traffic out through Tailscale and
back, and re-triggers Ivy's `Host` check (403).

Host networking is acceptable here specifically because the container is the only tenant and publishes
exactly one port, bound to `127.0.0.1:8891`, on a box that is already tailnet-only. Be precise about
what it does grant, though: the container can now reach loopback-only services it could not reach
over the tailnet IP — `unbound` on `127.0.0.1:5335` most importantly. That is the point (a DNS probe
is unbound's only honest health signal), but it is a real widening, and it is why the container must
stay unprivileged: an attacker who lands in it gets the host's loopback network, not the host's root.

## Two probe paths, one page

Not everything worth showing is reachable over HTTP from inside a container, and this is where the
design would otherwise get dishonest:

| Source | Covers | Owner |
|---|---|---|
| **In-container HTTP/DNS probes** | AdGuard, Ivy, bwmon, Polaris, SearXNG, Vaultwarden, unbound (a real DNS query to `127.0.0.1:5335`) | the container, every 15s |
| **Host-written status file** | systemd unit states, `tailscale status --json`, box uptime | `meridian-probe.timer` on the host, every 30s → `status/host.json` |

The container never runs `systemctl` or talks to the tailscaled socket. A small timer on the host
writes one JSON snapshot; the container reads it through a read-only mount. If that file is older
than 90s the affected tiles go stale rather than green — a dead probe must never render as health.

```json
{
  "at": "2026-10-23T18:04:11Z",
  "host": {"uptime_s": 1412033, "load1": 0.31},
  "units": {
    "AdGuardHome.service": {"active": true, "since": "2026-10-11T03:02:00Z"},
    "unbound.service":      {"active": true, "since": "2026-10-11T03:02:01Z"},
    "her-go.service":       {"active": true, "since": "2026-10-20T09:14:02Z"},
    "bwmon.service":        {"active": true, "since": "2026-10-22T20:00:03Z"}
  },
  "tailscale": {"online": true, "exit_node": true, "peers_online": 2, "peers_total": 3}
}
```

The unit list in that script is generated by `install.sh` from `/etc/meridian/targets.json`, so a
target cannot be controllable without also being observable.

## Components

| Package / dir | Responsibility | Constraint |
|---|---|---|
| `cmd/` | cobra CLI: `run`, `version`, `doctor`, `update`, `restart` | no privileged calls |
| `config/` | load `config.yaml`, env overrides, validate against the schema, hot reload | unknown probe/vital kinds and undeclared sections are startup errors, not silent skips |
| `doctor/` | resolve config, check control targets, probe once, explain what's wrong | reads only, never mutates |
| `probe/` | health + vitals, per-service goroutines, TTL cache, reads `status/host.json` | a failing probe must never block the page (see below) |
| `control/` | request/result file protocol, audit read, busy state | never constructs a command; only `{action, target, ack}` |
| `updater/` | resolve latest GHCR digest, hand off to the watcher | never pulls or recreates anything itself |
| `gateway/` | HTTP routes, static embed, JSON | `/api/control` is the only mutating route |
| `web/` | SvelteKit app, adapter-static, `go:embed` | no Node at runtime |

## Polling model

- One goroutine per service, all started together, staggered by ~150ms so seven probes don't all hit
  the same second.
- Each service has a `health` probe (required) and zero or more `vitals` (optional). A service whose
  health is fine but whose vital fails is **degraded**, not down — the vital is a nice-to-have, and
  its failure must not make the launcher lie about availability.
- Results are cached with a 15s TTL. A page load reads the cache; it never triggers seven upstream
  requests itself. A manual refresh is allowed but rate-limited to once per 5s.
- **Polling is lazy when nobody is looking.** 15s while a page has been open in the last 10 minutes;
  after that, back off to once every 2 minutes, and always do a fresh pass on load (a first load
  never shows a tile older than the load itself). The potato spends most of its life unwatched, and
  seven loopback requests every 15s forever to keep a page nobody has open current is waste.
- Timeouts: 4s per probe hard ceiling, 6s for the whole refresh cycle. Anything slower is reported
  as a timeout, not awaited.
- **Stale-while-error:** the cache keeps the last good value and its age. A tile shows
  `0.5 GB · 2m ago` greyed out rather than `—`, because "the number I saw two minutes ago" is more
  useful than an absence, as long as the age is visible.
- **Two sources, one model.** HTTP/DNS probes come from the container; systemd and tailnet state
  come from `status/host.json`, written every 30s by `meridian-probe.timer`. `host.json` older than
  90s makes its dependants stale, never green.
- Status vocabulary, and nothing else: `ok`, `degraded`, `down`, `unknown` (never polled yet),
  `disabled` (configured off).

## Config

`config.yaml` on the volume; `config.yaml.example` is a generic template in the repo, and
`examples/homelab.yaml` is this box's worked config. The loader is `internal/config`; a test keeps
the template's vocabulary in sync with the code.

**The config is the whole product for anyone but us.** Meridian's code contains no service names, no
hostnames, no ports, no service-specific quirks. Every URL, icon, section, probe and action is data.
Adding a service means editing YAML; it never means editing Go.

Four rules the schema is built around:

1. **Probe URLs are not click-through URLs.** Probes use `localhost` (or whatever hostname the
   service's own host check accepts); tiles link to whatever a browser should get. Ivy makes this
   explicit: `Host: 100.81.103.51:8418` gets a 403, `Host: localhost:8418` gets a 200.
2. **Every `control:` target must also exist in `/etc/meridian/targets.json`.** The config names
   what the UI offers; the host allowlist decides what is possible. Both must agree, the executor is
   authoritative, and the mismatch surfaces in the UI rather than silently.
3. **Sections are declared.** The `sections:` list is the display order; a service pointing at an
   undeclared section is a startup error, not a tile that quietly never renders.
4. **Vitals are a small closed vocabulary, not per-service code.** `json_field`, `json_count`,
   `json_sum`, `json_ratio_pct`, `host_field` cover every tile here, including AdGuard's percentage
   (a ratio of two JSON numbers) and bwmon's byte total (a sum over an array). No `adguard_blocked`
   special case exists, because a stranger's DNS filter needs to express the same thing.

```yaml
sections:
  - {id: everyday, title: Everyday}
  - {id: backbone, title: Backbone}
search:
  buttons:
    - {label: Search, url_template: "https://search.example.com/search?q={q}"}
services:
  - id: ivy
    name: Ivy
    section: everyday
    icon: mail
    url: http://lepotato:8418            # browser
    probes:
      health: {kind: http_json, url: "http://localhost:8418/api/v1/health", path: status, equals: ok}
      vitals:
        - {kind: json_count, label: needs you, url: "http://localhost:8418/api/v1/needs-attention",
           where: items, field: needs, equals: true}
    control: {target: ivy, actions: [restart, stop, start, update]}
```

`meridian doctor` is the answer to "why is my config wrong": it loads the file, resolves every URL
and env var, checks every control target against the host allowlist, probes each service once and
prints what it found, per service, with the reason for anything red. The full vocabulary is
documented at the bottom of `config.yaml.example`.

**Config reload:** SIGHUP (or `meridian run --watch`) re-reads the file and swaps the probe set
without dropping the listener or restarting the container — adding a service shouldn't need a
deploy. A config that fails validation on reload is rejected as a whole and the previous one stays
live, with a banner on the page saying so.

### Portability check (a test, not a promise)

One test loads `examples/` plus a fixture config describing services this homelab has never heard of
(`gitea`, `jellyfin`, a raw TCP port, a service with no web UI at all) and asserts the page renders
them. If any service-specific assumption leaks into Go or Svelte, that test fails.

## Files on disk (potato)

```
~/Meridian/
├── config.yaml                 # probe definitions; no secrets beyond env references
├── .env                        # credentials (referenced by env var name), image digest pin
├── docker-compose.yml          # network_mode: host, one loopback publish
├── control/
│   ├── inbox/                  # the container's ONLY writable control mount (decision #63)
│   │   ├── tmp/                #   write here first, then rename into queue/ (same mount: atomic)
│   │   └── queue/              #   the only way the container asks for anything
│   └── outbox/                 # mounted READ-ONLY; written by the executor only
│       ├── results/            #   one JSON per finished request, kept 7 days
│       └── audit.jsonl         #   append-only, kept 365 days
├── status/
│   └── host.json               # mounted ro, written by meridian-probe.timer
├── update-signal/              # mounted rw — self-update handoff (Polaris/Ivy pattern)
└── data/                       # hub's own state if it ever needs any (none planned: no DB)

/etc/meridian/                  # root-owned, outside the repo, never mounted
├── targets.json                # the allowlist
└── control-run                 # the executor
```

Nothing else from the host is mounted. Not the repo (so a compromised container cannot rewrite the
watcher script), not `/etc/meridian` (so it cannot widen its own allowlist), not the Docker socket.

**Ownership:** the container runs as `${MERIDIAN_UID}:${MERIDIAN_GID}` (the deploy user, written by
`install.sh`), the same convention Ivy uses, so the queue/results directories are writable without
being world-writable and the `control.sh` watcher can delete request files as itself.

## Why not just mount docker.sock

It would delete about 300 lines and one moving part. It would also make the hub's HTTP surface
root-equivalent on the host, and this hub is the one service whose whole purpose is to accept an
instruction and perform it. Polaris's own docs already ruled this out for a much narrower feature
(code execution); repeating the mistake here, for stopping and rebooting units, would be worse. The
request-file indirection is the price of the button existing at all.

## Failure modes, deliberately

| Failure | Visible as | Never |
|---|---|---|
| `meridian-control.path` disabled/stopped | "No executor" banner on the control page; queued requests show `pending (unclaimed)` | a spinner that never resolves |
| `meridian-probe.timer` stopped | unit/tailnet tiles show stale with the age of `host.json`, not green | silently claiming health it hasn't checked |
| Executor exits non-zero | Result JSON carries `ok:false` + output tail; red row in the audit log | silence |
| A service is down | Tile dot red, link still clickable, vital shows last good value with its age | hiding the service |
| Meridian itself is down | Nothing else breaks; URLs typed by hand work | cascading failure |
| Reboot from the UI | "Rebooting — back in ~40s", then auto-reconnect | a dead page after the box returns |
| Tailscale is down on the potato | The hub is unreachable *and* the health check's Tailscale check emails a failure | being silently inaccessible |

## Ports and addressing

| Service | Probe URL (host-side) | Click-through (browser) |
|---|---|---|
| Meridian | `localhost:8891/healthz` | `https://lepotato.taild95077.ts.net:8891` |
| Polaris | `localhost:8899/healthz` | `https://lepotato.taild95077.ts.net:8899` |
| Ivy | `localhost:8418/api/v1/health` | `http://100.81.103.51:8418` (see note below) |
| bwmon | `localhost:8890/api/summary?range=today` | `http://100.81.103.51:8890` |
| AdGuard | `localhost/control/stats` (basic auth) | `http://100.81.103.51` |
| SearXNG | `localhost:8888/` | `http://100.81.103.51:8888` |
| Vaultwarden | `localhost:8081/` | `https://lepotato.taild95077.ts.net` |
| Unbound | DNS query to `127.0.0.1:5335` | none |

"Localhost" above means the potato, because of `network_mode: host`.

Ivy has no `tailscale serve` entry today, so its click-through is the raw tailnet IP. Worth adding
`--https=8418` at some point for consistency; out of scope here, but the config makes it a one-line
change when it happens.
