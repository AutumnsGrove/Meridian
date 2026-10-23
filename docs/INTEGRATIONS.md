# Integrations

Every probe below was called against the live box on 2026-10-23 and returned what is described.
Re-verify before implementing a phase — these are the facts the config depends on, and they drift.

## Verified endpoints

| Service | Endpoint | Response | Use |
|---|---|---|---|
| Meridian | `GET /healthz` | `{"status":"ok"}` | self |
| Polaris | `GET http://localhost:8899/healthz` | `{"status":"ok","version":"r935.138ed16"}` | health + version vital |
| Polaris | `/search?q=…` (web route) | — | "Ask Polaris" hand-off; the route reads `?q=` from the URL |
| Ivy | `GET http://localhost:8418/api/v1/health` | `{"status":"ok","version":"r563.85fb96c","databases":{"mirror":"ok","state":"ok"}}` | health + version vital |
| Ivy | `GET /api/v1/needs-attention` | `{"items":[{"id","from","needs":true,"needsReason","preview",…}]}` | vital: count items where `needs == true` |
| Ivy | `GET /api/v1/accounts` | `[{"id","address","unread":2,"sync":"ok","syncedAt",…}]` | vital (alt): sum `unread` |
| bwmon | `GET http://localhost:8890/api/summary?range=today` | `{"range":"today","devices":[{"device","display_name","up_bytes","down_bytes"}]}` | vital: sum bytes → GB today |
| SearXNG | `GET http://localhost:8888/` | `200` | health |
| SearXNG | `GET /search?q=…&format=json` | `200` | the **Search** button (JSON output is enabled in Polaris's compose settings) |
| Vaultwarden | `GET http://localhost:8081/` | `200` | health |
| AdGuard | `GET http://localhost/` | `302` → login | link only |
| AdGuard | `GET /control/stats` (basic auth) | the admin user/pass (reused deliberately) | vital: `num_blocked_filtering / num_dns_queries` |
| Unbound | UDP DNS query to `127.0.0.1:5335` | `NOERROR` + an answer | health (unit state comes from `status/host.json`) |
| tailscaled | `tailscale status --json` on the host, copied into `status/host.json` | `Self.Online`, `Self.ExitNode`, `Peer[]` | Tailscale tile: exit-node state + peers online |

## The Ivy `Host` detail (this will bite you)

Ivy validates the `Host` header against `allowed_hosts` in `data/ivy.yaml`, which currently lists
`lepotato` and `lepotato.taild95077.ts.net`. Measured with curl:

| `Host:` sent | `/api/v1/health` |
|---|---|
| `127.0.0.1:8418` | 200 |
| `localhost:8418` | 200 |
| `lepotato:8418` | 200 |
| `lepotato.taild95077.ts.net:8418` | 200 |
| `100.81.103.51:8418` | **403** |

So Meridian's probes must use `http://localhost:8418/…` (or `lepotato:8418`), while the tile's
click-through should be a URL a *browser* will accept. Getting this backwards produces a permanently
red Ivy tile that is actually fine, and an Ivy log full of 403s. If Ivy later gains a `tailscale
serve` entry on `:8418`, the click-through becomes `https://lepotato.taild95077.ts.net:8418` and
nothing about the probe changes.

## Vitals, chosen

| Tile | Vital | Why that number | Failure mode |
|---|---|---|---|
| Ivy | needs-attention count | the one thing about email that changes what you do next | falls back to `—`, tile stays green |
| bwmon | GB today (up+down) | answers "is something eating the cap" without opening it | falls back to last good value + age |
| AdGuard | % of queries blocked today | the "is it still working" number; a sudden 0% means filtering broke | degraded, not down |
| Unbound | none | a DNS answer is the health signal; no separate number worth showing | down if the query fails |
| Polaris | version/build stamp | confirms the last deploy actually landed | degraded |
| SearXNG | none (health only) | it either answers or it doesn't | down |
| Vaultwarden | none (health only) | no unauthenticated API worth the exposure | down |
| her-go | none | no web surface; unit state from `status/host.json`, so the dot is real | down if the unit is inactive |
| Tailscale | exit-node on/off + peers online | the 2026-07-27 false-offline incident, in one glance | degraded |

AdGuard is the only vital needing credentials. **Decision: reuse the existing admin login**
(`autumn`, bcrypt in `/opt/AdGuardHome/AdGuardHome.yaml`) rather than creating a second user — the
password lives in `~/Meridian/.env` as `MERIDIAN_ADGUARD_USER`/`MERIDIAN_ADGUARD_PASS`, referenced by
name from `config.yaml`, never in git and never sent to the browser (all probing is server-side, so
CORS never enters the picture). Only `/control/stats` and `/control/status` are used, both read-only.

## Adding a service

1. **Probe** — find a cheap, unauthenticated endpoint that returns 200 when healthy. Prefer an
   explicit health endpoint over a homepage fetch; a homepage that 200s on a broken backend is worse
   than nothing.
2. **Vital** — only if the number changes what you'd do. One per tile, never a row of them.
3. **Click-through** — the URL a *browser* should open, which is often not the probe URL (`Host`
   checks, TLS, ports).
4. **Control** — only if `/etc/meridian/targets.json` also lists it, and only with an explicit
   `actions` list. Update the matrix in [CONTROL.md](CONTROL.md).
5. **Section** — `everyday` or `backbone`. If a tile needs a third section, ask whether the page is
   getting too long rather than adding one.

## Things that are deliberately not integrations

- **her-go** — Telegram bot with no web UI, but it *does* have an updater: `her update`
  (`cmd/update.go`), the CLI equivalent of its `/update` Telegram command — git pull, `go build`,
  then a `procmgr` restart. Meridian can call it directly, provided the executor runs it as `autumn`
  (running it as root leaves root-owned `.git`/binary/build-cache files and breaks the next
  Telegram-triggered update). See [CONTROL.md](CONTROL.md) "Who runs what".
- **Polaris's `code_exec` / `polaris-codeexec.service`** — its own sandbox privilege boundary.
  Meridian does not touch it.
- **`polaris.service`** — stale bare-metal unit, `disabled` since the Compose migration. Should
  probably be deleted from the box; must never be a control target.
- **Unbound** — no web surface at all. Its tile uses a real DNS query to `127.0.0.1:5335` (which is
  the actual health signal anyway), plus the unit state from `status/host.json`. The tailnet-wide
  resolution check in `service-healthcheck.sh` (`dig @127.0.0.1 -p 53`) remains the authority on the
  full AdGuard→Unbound chain.
