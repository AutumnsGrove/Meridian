# Meridian

The front door to the homelab: one page that links to every self-hosted service, shows whether each
is up, and — when something needs a kick — restarts, updates or stops it from a phone.

A single Go binary with a SvelteKit frontend embedded, run as a distroless container. A second,
root-owned binary on the host (`meridian-host`) does the privileged work. The container never gets
root, the Docker socket, `systemctl` or a shell. There is no authentication: the tailnet is the
access control.

Nothing about the homelab is in the code. Every service, hostname, port, icon, section and probe is
`config.yaml`.

## Install

You need a Debian-family box with systemd and Docker, a checkout of this repo, and `sudo`.

```bash
git clone https://github.com/AutumnsGrove/Meridian.git
cd Meridian
sudo deploy/install.sh --version vX.Y.Z      # latest release tag
```

The installer is idempotent and never overwrites an existing `config.yaml`, `targets.json`,
`observe.json`, compose file or `.env`. `--dry-run` prints the plan and changes nothing; `--help`
lists the rest.

Then:

```bash
$EDITOR ~/Meridian/config.yaml          # your services, sections and probes
sudoedit /etc/meridian/targets.json     # optional: what the control page may act on
cd ~/Meridian && docker compose up -d
tailscale serve --bg --https=8891 http://127.0.0.1:8891
```

Without `targets.json` the page is a launcher. With it, the control page can act — and only on what
that file allows. The checklist is in [docs/CONTROL.md](docs/CONTROL.md).

## Configure

- Start from [`config.yaml.example`](config.yaml.example), which documents every probe and vital
  shape. [`examples/homelab.yaml`](examples/homelab.yaml) is a real config with every shape in use.
- Secrets are environment variable **names** in `config.yaml` and values in `~/Meridian/.env` only.
  They never reach the browser.
- Edits are live within a second: the deployed container runs `meridian --watch`. Do not reload with
  `docker kill -s HUP` — Docker records any kill-API signal as a manual stop (decision #100).
- `cat /etc/meridian/targets.json | docker exec -i meridian /meridian doctor --allowlist -` checks
  the config against the world, read-only, and explains anything red.

## Update

- **Dashboard:** tap update on Meridian's own row in `/control`. It pins the newest digest, pulls,
  recreates, polls `/healthz` and rolls back if anything fails.
- **Host binary:** `sudo deploy/install.sh --upgrade-host --version vX.Y.Z`. The container never
  changes it; the privileged half is upgraded deliberately, by hand.

A push to `main` that passes CI publishes `:latest`; a `vX.Y.Z` tag publishes the versioned image and
the two `meridian-host` binaries with `SHA256SUMS`.

## What it is not

Not SSO (the tailnet is the access control), not a monitoring system (no alerting, no history), not
a way in from outside Tailscale. If Meridian is down nothing else breaks: it is a single point of
*discovery*, not of *service*.

## Documents

| Doc | What's in it |
|---|---|
| [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) | Components, networking, polling, files on disk |
| [docs/CONTROL.md](docs/CONTROL.md) | The privileged control plane: protocol, allowlist, guards, threat model |
| [docs/DESIGN.md](docs/DESIGN.md) | Visual language, layouts, phone-first behaviour |
| [docs/INTEGRATIONS.md](docs/INTEGRATIONS.md) | Probe/vital/action matrix, verified endpoints, adding a service |
| [docs/DECISIONS.md](docs/DECISIONS.md) | The decision log, with the rejected alternatives |
| [docs/STANDARDS.md](docs/STANDARDS.md) | How code is written, tested, fuzzed and committed |
| [AGENTS.md](AGENTS.md), [CLAUDE.md](CLAUDE.md) | The hard constraints and the "how do I" recipes for changing this repo |
| [docs/REVIEW-BRIEF.md](docs/REVIEW-BRIEF.md) | What is load-bearing, the trust boundaries, the review lanes |
| [config.yaml.example](config.yaml.example) | Every probe shape and the generated vocabulary |

## Licence

MIT — see [LICENSE](LICENSE).
