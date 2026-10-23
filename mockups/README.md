# Mockups

Design mockups for Meridian, before any component is built. One self-contained HTML file per screen
or state: no build step, no dependencies, tokens inlined. Open the file in a browser at 390px wide
first — that's the viewport that decides the layout (see [../docs/DESIGN.md](../docs/DESIGN.md)).

Polaris keeps its mockups this way (`~/Projects/Polaris/mockups/`) and it works: a mockup you can
open without a toolchain is a mockup that gets looked at.

## Rules

- Dark only. Warm-neutral surfaces, one cold accent: `--color-accent: oklch(78% 0.10 205)`.
- Phone first (390×844), then the same screen at 1440px.
- No `⌘K`, no modifier chords, no hover-only affordances. Two visible search buttons.
- Every interactive element ≥44px tall with a visible focus ring.
- Destructive actions are two-step, outlined (never filled), with the cancel button wider.
- Sections: **Everyday**, **Backbone**.
- Status marks are shape + colour: `●` ok, `◐` degraded, `✕` down, `○` unknown, `—` disabled.

## Screens, in priority order

1. `launcher-phone.html` — default state
2. `launcher-desktop.html` — same content, 1440px
3. `launcher-down.html` — a service down, with its `restart →` shortcut into control
4. `control-normal.html` — rows, state pills, actions, action history
5. `control-armed-stop.html` — armed stop on the DNS service: warning + auto-restore countdown
6. `control-no-executor.html` — the watcher isn't running; queued but unclaimed
7. `control-reboot.html` — armed reboot, and the "rebooting, back in ~40s" state
8. `launcher-stale.html` — stale vitals (value + age, dimmed) and an `unknown` dot

Name files for the screen and state, not the iteration: `control-armed-stop.html`, not
`control-v3-final-2.html`.
