# Meridian

The front door to the homelab: one page that links to every self-hosted service, shows whether each
one is actually up, and — when something needs a kick — lets you restart, update or stop it without
finding the right SSH command at the wrong moment.

**Status: built, deployed, maintained.** Two binaries, one container and a root-owned host side, all
released from this repo. The design is in [docs/](docs/ARCHITECTURE.md); the rules for changing it are
in [docs/STANDARDS.md](docs/STANDARDS.md).

Named after the line that passes overhead: the reference you measure everything else against. The
sibling projects are Polaris (a fixed star you navigate by) and Ivy (the mail client); Meridian is
where you stand to look at all of them.

## Quick start

### Install

You need a Debian-family box with systemd and Docker, a checkout of this repo, and `sudo`.

```bash
git clone https://github.com/AutumnsGrove/Meridian.git
cd Meridian
sudo deploy/install.sh --version vX.Y.Z      # the tag of the latest release
```

The installer creates `~/Meridian` (the data directory) and `/etc/meridian` (root-owned), installs a
checksummed `meridian-host` from that release, pins the first image digest from GHCR into `.env`,
renders the units and the sudoers rule and validates them with `systemd-analyze` and `visudo`, then
enables the probe timer and the control watcher and prints a pass/fail table. It never overwrites an
existing `config.yaml`, `targets.json`, `observe.json`, compose file or `.env`.

`sudo deploy/install.sh --dry-run` prints the whole plan without changing anything, and
`--host-binary dist/meridian-host` installs a binary you built yourself. Then:

```bash
$EDITOR ~/Meridian/config.yaml               # your services, sections and probes
sudo $EDITOR /etc/meridian/targets.json      # what the control page may act on
cd ~/Meridian && docker compose up -d
tailscale serve --bg --https=8891 http://127.0.0.1:8891
```

Without `targets.json` the page is a launcher. With it, the control page can act, and only on what
that file allows.

### Update

- **The dashboard:** tap update on Meridian's own row in `/control`. The container resolves the newest
  digest of `update.image` from GHCR, pins it, pulls, recreates, polls `/healthz` for 120 seconds and
  restores the previous image and `.env` if anything fails.
- **The host binary:** `sudo deploy/install.sh --upgrade-host --version vX.Y.Z` from a checkout. The
  container never changes it: the privileged half is upgraded deliberately, by hand.
- **Releases:** a push to `main` that passes CI publishes `:latest`; a `vX.Y.Z` tag publishes
  `:vX.Y.Z` and attaches the two `meridian-host` binaries with `SHA256SUMS`.

### Point it at your services

Nothing about this homelab is in the code. Service names, hostnames, ports, icons, section headings,
search buttons and every probe are configuration.

- Start from [`config.yaml.example`](config.yaml.example), which documents every probe and vital
  shape and the full vocabulary. The vocabulary block at the bottom is generated: `docker exec
  meridian /meridian docs kinds` prints it from the code's own table, and a test compares the two.
- Secrets are environment variable **names** in `config.yaml`, and values in `~/Meridian/.env` only.
  They never reach the browser.
- Reload without a restart: `docker kill -s HUP meridian` (or run the binary with `--watch`).
- Ask it what is wrong: `cat /etc/meridian/targets.json | docker exec -i meridian /meridian doctor
  --allowlist -`. The command is read-only and explains the config against the world.
- Control targets are the privileged half and live outside the container: add them to
  `/etc/meridian/targets.json` and `observe.json` following the checklist in
  [docs/CONTROL.md](docs/CONTROL.md).
- [`examples/homelab.yaml`](examples/homelab.yaml) is this box's real config, quirks and all, if you
  want a worked example of every shape in use.

A CI test renders a fixture config of services this project has never heard of, so the portability
claim cannot quietly rot.

## What it is not

Not SSO, and deliberately so. The tailnet is the access control; every service here is already
reachable by anyone on the tailnet with no login of its own. Meridian adds no identity layer, because
there is nothing to layer it onto. It also is not a monitoring system (no alerting, no metrics
history), not a port forward, and not a way to reach the homelab from outside Tailscale.

If Meridian is down, nothing else breaks: the URLs still work when typed by hand. It is a single point
of *discovery*, not of *service*, and that asymmetry is why the design stays small.

## How it is put together

A single Go binary with a SvelteKit frontend embedded via `go:embed`, run as a distroless container
with host networking and only the control inbox and the update signal writable. A second, stdlib-only
Go binary on the host, `meridian-host`, is the only thing that ever acts on a service: it reads the
request files the container writes, validates them against the root-owned allowlist, and runs the
command that file names.

The container never gets the Docker socket, root, `systemctl` or a shell. Read
[docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) for the diagram and the two probe paths, and
[docs/CONTROL.md](docs/CONTROL.md) before changing anything about that boundary.

## Documents

| Doc | What's in it |
|---|---|
| [CLAUDE.md](CLAUDE.md) | The hard constraints, and the rules for changing this repo |
| [AGENTS.md](AGENTS.md) | The same, for a coding agent, with the "how do I" recipes |
| [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) | Components, networking, polling, config shape, files on disk |
| [docs/CONTROL.md](docs/CONTROL.md) | The privileged control plane: protocol, allowlist, guards, audit, threat model |
| [docs/DESIGN.md](docs/DESIGN.md) | Visual language, tokens, layouts, phone-first behaviour, accessibility |
| [docs/INTEGRATIONS.md](docs/INTEGRATIONS.md) | Per-service probe/vital/action matrix, verified endpoints, adding a service |
| [docs/STANDARDS.md](docs/STANDARDS.md) | How code is written, tested, fuzzed, secured and committed |
| [docs/DECISIONS.md](docs/DECISIONS.md) | The decision log, with the alternatives that were rejected and why |
| [docs/REVIEW-BRIEF.md](docs/REVIEW-BRIEF.md) | What is load-bearing, the trust boundaries and the review lanes |
| [docs/OPEN-QUESTIONS.md](docs/OPEN-QUESTIONS.md) | Questions awaiting the owner's answer |
| [docs/history/](docs/history/PLAN.md) | The plan and the work order that built it, archived and not maintained |
| [config.yaml.example](config.yaml.example) | Generic config template: every probe shape and the full vocabulary |
| [examples/homelab.yaml](examples/homelab.yaml) | This homelab's worked config |

## Licence

MIT — see [LICENSE](LICENSE). Polaris is MIT and Ivy is AGPL-3.0; Meridian follows Polaris, and
contains no third-party-derived code.
