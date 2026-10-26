# Design

Dark only, warm-neutral surfaces borrowed from Polaris, and one cold accent — Meridian blue — for
selection, focus and primary actions. The point of the borrowed neutrals is that the hub and Polaris
feel like the same hand made them; the point of the different accent is that you can tell at a
glance which page you're on.

Phone first, because that's where it will be used. Every decision below is tested against a 390×844
viewport before a 1440px one.

## Tokens

Raw values live in one file (`web/src/lib/styles/tokens.css`), and only there.

```css
:root {
  color-scheme: dark;              /* Polaris's reason: tells forced-dark extensions to stay out */

  /* surfaces: warm, low chroma, hue ~65 — a quiet page, not a cockpit */
  --color-bg:            oklch(15% 0.014 65);
  --color-surface:       oklch(19% 0.014 65);
  --color-surface-2:     oklch(23% 0.016 65);
  --color-border:        oklch(30% 0.016 65);
  --color-border-strong: oklch(40% 0.020 65);

  /* text */
  --color-text:          oklch(93% 0.010 75);
  --color-text-dim:      oklch(64% 0.014 70);
  --color-text-faint:    oklch(52% 0.012 70);

  /* the one accent: a cold ice-blue, the line drawn on a star chart.
     Unclaimed by Polaris (starlight gold) and Ivy (lilac/green/amber). */
  --color-accent:        oklch(78% 0.10 205);
  --color-accent-strong: oklch(68% 0.11 205);
  --color-accent-soft:   color-mix(in srgb, var(--color-accent) 16%, transparent);

  /* status: semantic, never used for decoration */
  --status-ok:       oklch(76% 0.12 160);
  --status-degraded: oklch(80% 0.14 85);
  --status-down:     oklch(68% 0.17 25);
  --status-unknown:  oklch(58% 0.010 65);

  --font-ui: 'Lexend Variable', system-ui, sans-serif;
  --fs-vital: 22px;    /* the one number on a tile */
  --fs-name: 15px;
  --fs-meta: 12px;
  --radius: 10px;
  --touch: 44px;       /* minimum tap target, no exceptions */
}
```

Type: Lexend, same as Polaris and Ivy, self-hosted via `@fontsource-variable/lexend` — no runtime
font fetch, since this box may be offline-ish and every other project here already avoids it.
Vital numbers use `font-variant-numeric: tabular-nums` so tiles don't jitter as values change.

## Launcher

Phone (single column, 16px gutters):

```
┌─────────────────────────────────┐
│ Meridian            6/7 up      │  ← wordmark + one-line summary
│                                 │
│ ┌─────────────────────────────┐ │
│ │ ⌕ Search anything…          │ │  ← one input
│ └─────────────────────────────┘ │
│ ┌───────────────┬─────────────┐ │
│ │ ⌕ Search      │ ✦ Ask Polaris│ │  ← two real buttons, not a keybind
│ └───────────────┴─────────────┘ │
│                                 │
│ EVERYDAY                        │
│ ┌─────────────────────────────┐ │
│ │ Polaris              ●      │ │
│ │ your most-used thing        │ │
│ │ r935                        │ │
│ └─────────────────────────────┘ │
│ ┌─────────────────────────────┐ │
│ │ Ivy                  ●      │ │
│ │ 4 need you                  │ │
│ └─────────────────────────────┘ │
│ ┌─────────────────────────────┐ │
│ │ bwmon                ◐      │ │
│ │ 0.5 GB today · 2m ago       │ │
│ └─────────────────────────────┘ │
│                                 │
│ BACKBONE                        │
│ ┌─────────────────────────────┐ │
│ │ AdGuard Home         ●      │ │
│ │ 12% blocked                 │ │
│ └─────────────────────────────┘ │
│ …                               │
│                                 │
│ ┌─────────────────────────────┐ │
│ │ Control panel             → │ │  ← the second view lives here
│ └─────────────────────────────┘ │
└─────────────────────────────────┘
```

≥768px: two columns inside each section, search block capped at 560px. ≥1200px: three columns.
Sections are `everyday` then `backbone`; an empty section renders nothing at all rather than a
heading over blank space.

**Tile anatomy** — the whole tile is the link (no nested targets), name on the left, status dot on
the right, vital number or subtitle underneath in `--color-text-dim`. One tile is one target, so the
`restart →` case below replaces the link rather than adding a second one (decision #73). Minimum height 76px, which
comfortably clears the 44px touch target even with the padding.

**Down tiles offer a way in, not a way to act.** A tile whose service is down grows a single
`restart →` affordance that navigates to `/control#<id>` and highlights that row — it does not
perform anything itself. The reasoning: the launcher is tapped twenty times a day and the control
page twice a month, so the launcher must not hold a button that changes state. But a dead tile with
no path forward would send you hunting, and "restart →" is the exact thought already in your head.

**Status dots are doubled up deliberately**, because colour alone is not a signal:

| State | Mark | Meaning |
|---|---|---|
| `ok` | `●` bright green | answered its health probe |
| `degraded` | `◐` amber | healthy, but something it reports isn't right (vital failed) |
| `down` | `✕` rose | probe failed or timed out |
| `unknown` | `○` grey | no probe configured, or the host status file has never been written |
| `disabled` | `—` faint | configured off |

Each also carries a visually hidden label ("Ivy, healthy") for screen readers. The tile's accessible
name is its **content**, not an `aria-label`: the status word and each vital's label are present as
visually hidden text, so what a screen reader hears is what the tile shows, plus the words the shapes
stand for. There is no second sentence to keep in sync.

**Stale values are shown, not hidden.** A vital keeps its last good reading with its age
(`0.5 GB today · 2m ago`, dimmed) rather than becoming `—`. If the age exceeds 5 minutes the value
drops to `--color-text-faint` and the dot goes `◐`.

## Search

One input, two buttons, always visible side by side. Enter submits **Search**; there is no modifier
or chord that changes which backend you get, because the primary device has no keyboard.

- **Search** → `http://100.81.103.51:8888/search?q=<q>` (SearXNG, JSON output already enabled)
- **Ask Polaris** → `https://lepotato.taild95077.ts.net:8899/search?q=<q>` (verified: `/search`
  reads `?q=` from the URL)

Both open in the same tab. Empty input disables both buttons rather than doing something surprising.
Once Polaris grows a URL parameter that starts an assistant turn rather than a search, "Ask Polaris"
should point at that instead — noted in [PLAN.md](PLAN.md).

## Control panel

`/control`, linked from the bottom of the launcher. Same tokens, stricter layout: it is a list, not
a grid, because rows are compared down their left edge.

```
┌─────────────────────────────────┐
│ ← Meridian / Control            │
│                                 │
│  ⚠ No executor                  │  ← only when meridian-control.path is down
│  Actions are queued but not     │
│  being picked up.               │
│                                 │
│ AdGuard Home                    │
│ ● running 12d · high            │
│ DNS for the whole tailnet       │
│ [restart] [update] [stop]       │
│                                 │
│ Ivy                             │
│ ● running 3d · medium           │
│ [restart] [update] [stop]       │
└─────────────────────────────────┘
```

Each row: name, state pill (`● running 12d`), criticality word, one line of consequence text where
there is one worth stating, then buttons. Buttons are 44px tall, wrap to a second line on narrow
phones, and each shows its own state (`restart` → `restarting…` → `restarted in 6s`).

**Armed stop** — the second state of a two-step, and the only place the page raises its voice:

```
┌─────────────────────────────────┐
│ AdGuard Home                    │
│ ● running 12d                   │
│                                 │
│ ⚠ Stopping this breaks DNS for  │
│   the whole tailnet, including  │
│   this page. Auto-restores in   │
│   10 min unless you cancel.     │
│                                 │
│ [ cancel ]   [ stop anyway ]    │  ← cancel is the wide, first button
└─────────────────────────────────┘
```

The armed state expires after 5 seconds. Cancel is visually primary; the destructive button is rose
outlined, never filled.

**Danger zone** at the bottom, separated by a rule, containing the reboot, guarded with the same
two-step arm as stop:

```
│ HOST                            │
│ Reboot lepotato                 │
│ ~40s offline. The boot health   │
│ check will email you a report.  │
│ [ reboot… ]                     │
│        ↓ first tap arms ↓       │
│ ⚠ The launcher will be offline  │
│   for about 40s. Everything     │
│   comes back on its own.        │
│ [ cancel ]   [ reboot anyway ]  │
```

**Action history** below that: the last 50 audit lines, newest first, one line each
(`18:04 restart bwmon · 4.8s · ok`). Failed rows get a rose left border and can be tapped to reveal
the executor's output tail. This is the feature that makes the buttons trustworthy: every press has
a receipt.

**Disabled buttons stay visible and say why.** AdGuard's and Unbound's `update` render at 40%
opacity, `disabled`, with the reason on the button's second line (`no scripted updater`) and in its
`aria-describedby` text. Hiding an unsupported action makes the page look like it lost a feature;
a greyed one teaches what the box can actually do. The same treatment applies to any action the
allowlist refuses — the UI never invents capability the executor will reject.

## Wording

- Second person, present tense, no exclamation marks.
- Buttons name the verb, not "OK"/"Go": `restart`, `update`, `stop`, `reboot…`.
- Consequence text states what breaks, not that something might: "breaks DNS for the whole tailnet",
  not "this action may have unintended effects".
- The Hub is "Meridian"; the box is "lepotato" (lowercase, as `hostname` reports it); "tailnet" is
  lowercase in prose.

## Accessibility and other constraints

- AA contrast throughout (the tokens above are checked against their own backgrounds, not assumed);
  `--color-text-dim` is only ever used on `--color-surface` or darker.
- Visible focus ring on everything interactive: `outline: 2px solid var(--color-accent); offset: 2px`.
- All touch targets ≥44px, including the small action buttons and the search buttons.
- `prefers-reduced-motion: reduce` removes the only two animations (status dot pulse while a request
  is pending, and the armed-stop shake — the latter becomes a static border).
- No spinners without text. A pending action says what it's doing and how long it has been doing it.
- The page must be readable at 200% zoom on a 390px viewport, which is the practical test for
  whether the single-column layout actually works.
- `color-scheme: dark` declared, with Polaris's reasoning preserved: forced-dark extensions otherwise
  mangle `oklch()` values into washed-out cards.
