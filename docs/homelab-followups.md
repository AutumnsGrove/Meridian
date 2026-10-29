# Homelab followups

The homelab repo documents every service on the box. Meridian lives here, so the entries that point
at it have to be written there, and this repo does not edit that one. This is the checklist for that
change, kept next to the code so it survives the build that produced it.

The last item is the same test the rest of that repo uses: what the README says matches what is
actually running on the potato.

## 1. Meridian in the homelab README

Add an entry in the same shape as the Polaris one (service list, Quick Access row, port, update
command, backup line, changelog):

| Fact | Value |
|---|---|
| URL | `https://lepotato.taild95077.ts.net:8891` (tailscale serve to `127.0.0.1:8891`) |
| Port | `8891`, loopback only |
| What runs it | the `meridian` container from `~/Meridian/docker-compose.yml`, plus `meridian-probe.timer` and `meridian-control.path` on the host |
| Where its data lives | `~/Meridian/` (config, compose, `.env`, status, control) and `/etc/meridian/` (allowlist, host binary, units) |
| How to update it | the update button on `/control` for the image; `sudo deploy/install.sh --upgrade-host --version vX.Y.Z` for the host binary |
| Logs | `docker logs meridian`; `journalctl -u meridian-probe.service`; `journalctl -u meridian-control.service` |
| Backup | `~/Meridian/config.yaml`, `~/Meridian/.env`, and `/etc/meridian/{targets.json,observe.json,host.json}` are not in any repo; copy them with the rest of the box's configuration |
| Changelog | one line, with the release tag Meridian was installed from |

## 2. `docs/meridian.md`

Alongside `docs/polaris.md`, and covering the parts that only exist on the box:

- What it is: the launcher plus the control plane, and the rule that the container never gets the
  Docker socket or root.
- Install: `sudo deploy/install.sh --version vX.Y.Z` from a checkout; the first run creates
  `~/Meridian` and `/etc/meridian`.
- Config: `~/Meridian/config.yaml` for services, sections and probes; `/etc/meridian/targets.json`
  for what the buttons may act on; `/etc/meridian/observe.json` for extra units to watch. Reload with
  `docker kill -s HUP meridian`.
- Self-update and its rollback: update from `/control`, digest pinned in `.env`, the previous image
  restored if `/healthz` does not answer within 120 seconds.
- The host binary is upgraded separately, by hand: `sudo deploy/install.sh --upgrade-host`.
- Reading a failure: `meridian doctor` for config, `journalctl -u meridian-control.service` for
  refused actions, `~/Meridian/control/outbox/audit.jsonl` for what was done and when (365 days).
- Where each file lives: [ARCHITECTURE.md](ARCHITECTURE.md) "Files on disk".

## 3. Ivy into the homelab README and `docs/ivy.md`

Ivy runs on the box at `:8418` and appears in zero lines of the homelab repo. That is the gap worth
closing while the context is loaded:

- README: service entry, URL, port, the `ivy` compose project, `ivy update` (or the
  `ivy-update.service` unit), and the data directory.
- `docs/ivy.md`: what it is, how mail and the Telegram front end are configured, and the `Host`
  quirk from [INTEGRATIONS.md](INTEGRATIONS.md) "The Ivy `Host` detail" so nobody re-learns that
  `Host: 100.81.103.51:8418` returns 403 while `localhost:8418` returns 200.
- Meridian's probe for Ivy is `http://localhost:8418/api/v1/health`; the tile's click-through is the
  URL a browser accepts. Do not copy one into the other.

## 4. `maintenance-setup/service-healthcheck.sh`

Add the two services that notify nobody today, so a dead mail client or a dead hub produces an email
instead of silence:

- **Ivy**: `http://localhost:8418/api/v1/health`
- **Meridian**: `http://localhost:8891/healthz`

Follow the file's existing check shape (name, URL, expected answer, timeout) rather than adding a new
mechanism, and keep the boot-time run: the check that fires on boot is the one that catches a failed
reboot.

## 5. Verify, then close it

- `docker ps` and `systemctl list-units --type=service --state=running` on the potato list Meridian
  (container), `meridian-probe.timer`, `meridian-control.path`, and Ivy.
- The README's service list matches those two commands, with nothing missing and nothing stale.
- The healthcheck script, run by hand, reports both new services green.
- The changelog has the release Meridian was installed from, and `meridian version` on the box prints
  the same tag.

**Done when** the README matches `docker ps` and `systemctl list-units`, and the two health checks
email on failure.
