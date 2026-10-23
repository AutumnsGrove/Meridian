# Meridian

The front door to the homelab: one page that links to every self-hosted service, shows whether
each one is actually up, and — when something needs a kick — lets you restart, update or stop it
without finding the right SSH command at the wrong moment.

**Status: planning / scaffolding.** No application code yet — this repo currently holds the design
and plan documents, a generic config template and one worked example. Nothing is deployed.

Named after the line that passes overhead: the reference you measure everything else against. The
sibling projects are Polaris (a fixed star you navigate by) and Ivy (the mail client); Meridian is
where you stand to look at all of them.

## Why it exists

There are now seven things worth opening — Polaris, Ivy, bwmon, AdGuard, SearxNG, Vaultwarden and
this page — and each one has its own URL, port and scheme, so "bookmark everything" stopped being
useful around the third one. A launcher could just be a page of links. Meridian is a page of links
that also knows their state, which is the part a bookmarks folder cannot do:

- every service already exposes an unauthenticated health endpoint or a cheap count that is worth
  one glance (Ivy's needs-attention queue, bwmon's bytes today, AdGuard's blocked percentage,
  Polaris's build stamp) — see [docs/INTEGRATIONS.md](docs/INTEGRATIONS.md)
- the homelab's existing health check already knows how to tell whether each one is alive, and it
  runs unattended, so nobody is watching when it fails
- the recovery procedure for a wedged service is currently "SSH in and remember whether Polaris is
  a `docker compose` project or a systemd unit", which is exactly the thing you do not want to be
  recalling at the moment something is broken

## What it is not

Not SSO, and deliberately so. The tailnet is the access control; every service here is already
reachable by anyone on the tailnet with no login of its own. Meridian adds no identity layer,
because there is nothing to layer it onto. It also is not a monitoring system (no alerting, no
history beyond the action log), not a port forward, and not a way to reach the homelab from outside
Tailscale.

If Meridian is down, nothing else breaks — the URLs still work when typed by hand. It is a single
point of *discovery*, not of *service*, and that asymmetry is the reason the design can stay small.

## Bring your own services

Nothing about this homelab is in the code. Service names, hostnames, ports, icons, section headings,
search buttons and every probe are configuration, so pointing Meridian at a different set of services
means copying `config.yaml.example`, editing it, and sending `SIGHUP` — no rebuild, no code changes.

- [`config.yaml.example`](config.yaml.example) — a generic template with a placeholder service for
each supported probe shape, and the full vocabulary documented at the bottom
- [`examples/homelab.yaml`](examples/homelab.yaml) — this box's real config, quirks and all
- `meridian doctor` — resolves the config, checks every control target against the host allowlist,
  probes each service once and explains anything red
- A CI test renders a fixture config of services this project has never heard of, so the portability
  claim can't quietly rot

The privileged half is the one thing that stays machine-specific: `/etc/meridian/targets.json` is a
root-owned allowlist of what may be started, stopped or restarted (see
docs/CONTROL.md). Without it the page is a launcher; with it, it can act.

## Documents

| Doc | What's in it |
|---|---|
| [docs/PLAN.md](docs/PLAN.md) | Phased milestones, what "done" means for each, open questions |
| [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) | Components, polling model, config shape, host/container boundary, files on disk |
| [docs/CONTROL.md](docs/CONTROL.md) | The privileged control plane: request protocol, allowlist, guards, audit, threat model |
| [docs/DESIGN.md](docs/DESIGN.md) | Visual language, tokens, layouts, phone-first behaviour, accessibility |
| [docs/INTEGRATIONS.md](docs/INTEGRATIONS.md) | Per-service probe/vital/action matrix, verified endpoints, adding a service |
| [docs/DECISIONS.md](docs/DECISIONS.md) | Decision log, including the alternatives that were rejected and why |
| [config.yaml.example](config.yaml.example) | Generic config template: every probe shape, and the full vocabulary |
| [examples/homelab.yaml](examples/homelab.yaml) | This homelab's worked config |
| [CLAUDE.md](CLAUDE.md) | Rules for AI assistants working here, including the mockup brief |

## Shape of the thing (planned)

A single Go binary with a SvelteKit frontend embedded via `go:embed`, the same shape as Polaris and
Ivy, deployed as a container on the Le Potato that only ever pulls a CI-built image. Two views:

1. **Launcher** — grouped tiles, a status dot and one vital number each, plus a search box with two
   buttons: *Search* (SearxNG) and *Ask Polaris*.
2. **Control** — per-service health detail and action buttons, including restart, update, stop
   (two-step, with automatic DNS recovery) and an emergency reboot of the box.

The container never gets the Docker socket. Actions are performed by a host-side watcher reading a
request file, validated against a root-owned allowlist. Read
[docs/CONTROL.md](docs/CONTROL.md) before changing anything about that boundary.

## Licence

MIT — see [LICENSE](LICENSE). Polaris is MIT and Ivy is AGPL-3.0; Meridian follows Polaris, and
contains no third-party-derived code.
