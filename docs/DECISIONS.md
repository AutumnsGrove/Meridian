# Decisions

One line per decision, with the alternatives that were rejected and why. Append new decisions rather
than editing old ones; if a decision is reversed, add a new entry that says so and link back.

## Identity

| # | Decision | Rejected alternatives | Why |
|---|---|---|---|
| 1 | Named **Meridian** | Trellis, Sextant, Asterism, Zenith, Ephemeris, Firmament, Nocturlabe, Caelum, Proxima | Meridian keeps the astronomical register of Polaris, and "the line you measure everything against" is exactly the role. It also avoids the vocabulary Polaris already uses internally (Comet, Constellation, Pulsar, Atlas) |
| 2 | Warm-neutral surfaces (Polaris's) + **Meridian blue** `oklch(78% 0.10 205)` accent | Starlight gold (Polaris's), lilac (Ivy's), Grove Tokyo Night palette, indigo, green flash, ember | Sibling to Polaris, distinguishable from it. The ice-blue is unclaimed by any existing project, and it reads as a line drawn on a star chart |
| 3 | Dark only, no light theme | Light + dark with a toggle | Every surface it links to is dark; a light hub in front of four dark apps is a flashbang. Revisit only if a service here ever gets a light mode |

## Shape

| # | Decision | Rejected alternatives | Why |
|---|---|---|---|
| 4 | Two views: launcher and `/control` | One page with actions inline, separate pages per service | Launcher is what gets used 20× a day and should not be one mis-tap from a stop button. The destructive stuff lives behind one deliberate tap and a different visual register |
| 5 | Grouped sections (`everyday`, `backbone`), not one flat grid | Flat grid, alphabetical list, usage-frequency ranking, "daily/infrastructure", "tools/plumbing" | Seven tiles is already enough that a flat grid needs scanning. "Everyday / Backbone" names what the two groups *are* to you rather than what kind of software they are, and gives "adding a service" an answer that isn't "make the page longer" |
| 6 | Search box with two **visible buttons** (Search / Ask Polaris) | `⌘K` command palette, modifier-key toggle, single button with a dropdown | Primary device is a phone. A keybinding-first design is a design for the laptop you're not holding |
| 7 | Ask Polaris links to Polaris's `/search?q=` | Deep-linking into a new assistant thread; embedding Polaris in an iframe | `/search?q=` is verified to work today with no changes to Polaris. A new-thread parameter would need Polaris work first (noted as a future upgrade) |
| 8 | Go single binary + SvelteKit embedded via `go:embed`, **separate public repo** | Plain embedded HTML with no build step (cheaper, ~400 lines), everything inside the homelab repo | Consistency with Polaris/Ivy was chosen deliberately over the simpler thing: same deploy story, same CI, same update mechanism, one less bespoke pattern to remember |

## Deployment

| # | Decision | Rejected alternatives | Why |
|---|---|---|---|
| 9 | Container on the potato, loopback-bound, `https://lepotato.taild95077.ts.net:8891` via `tailscale serve` | Root of `:443` (would require moving Vaultwarden to `:8443` and re-pointing every Bitwarden client), plain HTTP on the tailnet IP | Nothing else moves, and it matches Polaris's pattern exactly |
| 10 | No authentication of any kind | Shared secret, Basic auth, a real identity provider | The tailnet is the boundary; every service behind it is already unauthenticated. Adding a login to the hub alone would be theatre. The control page's privilege is bounded by the allowlist instead |
| 11 | Probes run **server-side**, 15s TTL cache, results stale-while-error | Browser-side `fetch` to each service with CORS headers | Three apps would need CORS changes (Ivy has a host allowlist, Polaris pins its origin), and AdGuard's credentials would end up in the browser. Server-side keeps every credential on the box and every probe off the wire |
| 26 | Container runs with `network_mode: host` | Bridge network + `host.docker.internal` (`extra_hosts`), bridge + tailnet IP probe URLs | On a bridge, `localhost` inside the container is the container, so all seven probes would report red while every service is fine. Host networking keeps the probe URLs literal and stops traffic from leaving through Tailscale and coming back — which would also re-trigger Ivy's `Host` check |
| 27 | systemd and tailnet state arrive via a host-written `status/host.json` from `meridian-probe.timer` | Giving the container `systemctl` access, mounting the tailscaled socket, or dropping unit/tailnet tiles entirely | The container has no legitimate way to see systemd. One small timer writing one JSON snapshot keeps a single privilege boundary and still makes "is AdGuard's unit actually running" answerable. Dependant tiles go stale (never green) when the file is old |
| 12 | No database; state is files | SQLite (as Polaris and bwmon use) | The hub has no user data. Config is a file, action state is request/result JSON, history is a JSONL audit log. A DB would be ceremony |
| 13 | Alerting stays in `service-healthcheck.sh` (Resend email on boot) | Building notification into Meridian, adding Uptime Kuma | A health check that depends on the thing it's checking is not a health check. The existing script runs on boot and emails; Meridian only ever *displays* |

## Control plane

| # | Decision | Rejected alternatives | Why |
|---|---|---|---|
| 14 | Request-file + host watcher + root-owned executor | Mount `docker.sock` into the container (≈300 fewer lines), `NOPASSWD` sudoers for `systemctl`/`docker` directly | The hub's entire purpose is to accept an instruction and act on it, so it is the last place to hand root to. Polaris already ruled out `docker.sock` for a much narrower feature; repeating it here would be worse |
| 15 | Allowlist and executor in `/etc/meridian`, root-owned, never mounted | Keeping `targets.json` in the repo next to `config.yaml` (simpler, one place to edit) | If the container can write the allowlist, the allowlist is decoration. This is the single invariant the whole design rests on |
| 16 | Requests carry an `ack` field the executor validates | Trusting the UI's confirmation dialog | The UI guard is a courtesy. The executor must refuse a stop that never passed through it |
| 17 | Two-step stop for everything; DNS targets additionally get a 10-minute auto-restore timer | Single `confirm()` on every destructive action, or no stop/start at all | Stopping AdGuard breaks DNS for the entire tailnet — a button that can cause a second, worse outage must not be able to last unattended. The timer makes the worst case recoverable |
| 18 | Reboot yes, shutdown no | Both, or neither | A remote shutdown is unrecoverable without physical access, which is the opposite of what an emergency control panel is for |
| 19 | v1 actions: start/stop/restart, per-service update via each app's own updater, reboot, Meridian restart, Meridian self-update | "Update all" button (not selected) | Divided dependencies stay with the app that owns them, so Meridian cannot silently diverge from `polaris update`/`ivy update`/`her update`. Update-all is easy to add later and easy to regret first |
| 28 | AdGuard vital reuses the existing admin login | Creating a second, less-privileged AdGuard user | One fewer credential to keep in sync on a single-user box, and the API surface used is read-only (`/control/stats`). Still `.env`-only, never git, never the browser |
| 29 | AdGuard and Unbound have **no** update action; the button renders disabled with a reason | Shipping a half-working `apt`/installer path, or hiding the button entirely | Nobody has a scripted updater for either, so inventing one is how you break the resolver at 2am. A disabled button that says "no scripted updater" teaches the page; a missing button looks like a bug |
| 30 | her-go's update is wired to `her update` (its own CLI, the `/update` Telegram equivalent) | Leaving her-go control-only until the update path was "better wired" | The mechanism already exists and works; it just needed an allowlist entry. Not wiring it would have meant SSHing for something the box can already do |
| 31 | App updaters and Docker actions run as the deploy user; only `systemctl`/`systemd-run` run as root | Running the executor as root throughout | `her update` and `bwmon update` write into their own checkouts; as root they leave root-owned `.git` objects, binaries and build caches, and the next **telegram-triggered** update fails on permissions. Root would silently poison the exact path it was trying to run |
| 32 | Audit log kept 365 days, pruned in `control.sh` | Rotating at a line count, keeping forever, or pruning from the container | Time is the useful unit here ("what did I do last spring"), and a year of actions is a few hundred KB. Pruning on the host keeps the container's mount read-only |
| 20 | Self-update built in from the start: digest resolution, Ivy-style image-reference validation, health-checked rollback | Git-pull-and-rebuild on the host, `:latest` pull without a digest pin | Polaris's update path was retrofitted in stages; the hardening Ivy ended up with (refuse anything that isn't the expected digest, roll back on unhealthy) is cheap to build in on day one and expensive to bolt on later |
| 21 | First control target is `bwmon` restart, alone | Starting with Polaris (most used) or AdGuard (most dangerous) | Least critical, no dependants, and its `Requires=bwmon-nft.service` dependency immediately tests whether the executor handles unit graphs sensibly |
| 22 | Vaultwarden's run spec gets captured as a compose file (Phase 3), then it becomes an ordinary compose target | Reconstructing the `docker run` arguments from `docker inspect` at action time; shipping pull-only permanently | Guessing at a run spec at the moment you need it is how you lose a password database. Until the capture lands, Vaultwarden has no update action at all |
| 33 | Reboot is two-step, same arm pattern as stop | Single confirm | Highest blast radius on the page; consistency means you learn the armed pattern once |
| 34 | A down tile gains a `restart →` shortcut into the control row, never an inline action | Inline restart on the tile, or no affordance at all | The launcher is tapped daily and the control page monthly, so it must not carry state-changing buttons — but a dead tile with no path forward sends you hunting |
| 35 | Polling backs off to 2 minutes after 10 minutes with no page open, with a fresh pass on load | Always 15s; polling only while a page is open | Always-15s is wasted work on a 5W box; page-open-only means the first load after a pause shows nothing. Backoff gives both |
| 36 | Every JSON handoff file is written to a temp path and `rename`d into place; queue writes land in `queue-tmp/` first | Writing files in place and hoping readers are slow | `meridian-control.path` fires on `DirectoryNotEmpty=`, which can catch a half-written file. Atomic rename removes the whole race class instead of adding a retry loop |
| 37 | Duplicate `(target, action)` requests within 60s are coalesced, with the duplicate answered `note:"coalesced with <id>"` | Relying on the UI disabling its own buttons | Two taps, two browsers, or one impatient thumb should not restart something twice. The UI guard is a courtesy; the executor is the rule |
| 38 | **Zero service-specific knowledge in the code.** Names, hostnames, ports, icons, sections and quirks are all config | Hardcoding this homelab's seven services and treating the config as a convenience | Someone else should be able to clone Meridian, write one YAML file, and have it work for their services. A hub that only knows one homelab is a personal script with a README, and this repo is public |
| 39 | Sections are declared in config (`sections:`) with ids and titles | Fixed `everyday`/`backbone` in code, inferred order | Someone else's grouping is not ours. Declared sections also make the display order explicit and an undeclared section a startup error |
| 40 | Vitals use a closed generic vocabulary (`json_field`, `json_count`, `json_sum`, `json_ratio_pct`, `host_field`) | A named extractor per service (`adguard_blocked`, `bwmon_today`, `ivy_needs`) | Per-service extractors would have baked this homelab into `probe/`. A ratio of two JSON numbers expresses AdGuard without being AdGuard, and it's what any DNS filter tile needs |
| 41 | `meridian doctor` + validation on load, with hot reload (SIGHUP) | Trusting the YAML and debugging from symptoms; restarting the container to add a service | The common failure is a valid YAML file that's wrong about a URL, and the page can't say why. Doctor says why. Reload means adding a service is an edit, not a deploy |
| 42 | A CI test renders a fixture config of services this homelab has never heard of | Trusting the portability claim | Decision 38 is the kind of promise that rots quietly. The test is the only thing that keeps it true |

## Scope boundaries

| # | Decision | Rejected alternatives | Why |
|---|---|---|---|
| 23 | Not a monitoring system: no metrics history, no alerting, no graphs | Embedding Grafana, adding Uptime Kuma, storing probe history | The question the page answers is "is it up right now, and what do I do about it", not "what was the CPU in March". Homelab has enough moving parts |
| 24 | Not SSO, and never pretends to be | Bolting on GroveAuth or similar for the hub alone | Nothing behind the hub has an identity system to unify. Naming it SSO would imply a guarantee that does not exist |
| 25 | Ivy documentation is part of this project's Phase 5 | Fixing it separately, later | Ivy is deployed at `:8418` right now and appears in zero lines of the homelab repo — found while planning this. Fixing the gap while the context is loaded is cheaper than rediscovering it |

## Honest caveats worth re-reading

- **The sudoers rule is documentation as much as enforcement** — `autumn` already holds
  `NOPASSWD: ALL` on the potato, so the executor's real value is constraining what a *request file*
  may ask for, in one root-owned, reviewable place. It does not contain a compromised deploy account.
- **Anyone on the tailnet can press these buttons.** That is consistent with every other service
  here, but this is the first one where "anyone" means "can reboot the box". Worth revisiting if the
  tailnet ever gains users who aren't the owner.
- **Meridian being down breaks nothing.** It is a single point of discovery, not of service. That
  asymmetry is what keeps this design small, and it should be preserved rather than quietly eroded.
