# TUI Keymap Convention

A shared interaction convention across Noam's Go/Bubble Tea TUIs: **prdash**,
**wtc**, **lazytmux**, **tmux-remux**, **aeye**. Sit down at any of these apps
and the escape hatches — quit, help, filter, navigate — behave the same way.

Rationale and full design history live in
[`docs/superpowers/specs/2026-07-20-tui-interaction-convention-design.md`](docs/superpowers/specs/2026-07-20-tui-interaction-convention-design.md).
This file is the distilled, user-facing reference — what the keys do, not why.

## The rule

> **Letters do the surface's primary verb. `/` summons the filter only where
> letters are verbs.**

- A surface whose job is *choose one thing* has no competing use for letters,
  so typing filters directly — no `/` needed.
- A surface whose job is *browse, then act* (merge, delete, rerun…) spends its
  letters on those actions, so the filter hides behind `/`.

This one rule sorts every app into one of three archetypes below.

## App → archetype

| App | Archetype |
|---|---|
| prdash | Board |
| wtc | Board |
| lazytmux | Picker |
| tmux-remux | Picker |
| aeye | Viewer |

## Archetype: Picker — *choose one thing*

Apps: lazytmux, tmux-remux.

- The filter box is **always visible and focused**. Typing filters immediately
  — fuzzy, live, no `/`.
- Move selection: `ctrl+j`/`ctrl+k` + arrows (plain `j`/`k` are filter text
  while the box holds focus; tmux-remux has no filter yet, so it currently
  keeps bare `j`/`k` nav until it gains one).
- `enter` — select and act.
- `esc` — two-stage: clear the query if one is present, otherwise quit.
- Help: a non-printable key (`?` is filter text here, not help).
- Secondary actions are `ctrl`-modified, since bare letters are spoken for.

## Archetype: Board — *browse, then do many things*

Apps: prdash, wtc.

- The filter box is **always visible** but starts **blurred**. `/` focuses it
  for a live incremental fuzzy filter; `esc` blurs it back.
- Bare letter keys are actions while the box is blurred (e.g. prdash's `m`
  merge / `r` rerun / `o` open / `a` actions palette; wtc's `d` delete / `D`
  force-delete / `space` multi-select / `a` select-stale / `e` expand /
  `enter` switch).
- `tab` — switch board (e.g. prdash's PR ↔ issue).
- Plain `j`/`k` + arrows move selection (free to use since the filter is
  gated behind `/`).
- `?` — keymap overlay.

## Archetype: Viewer — *spatial navigation, no filter*

Apps: aeye.

- `h`/`j`/`k`/`l` + arrows pan/move; `g`/`G` jump to ends; `1`-`9` jump;
  `z`/`Z` zoom; `tab` drills into a region.
- No filter box.
- `esc` keeps its viewer meaning (reset zoom / exit region drill-down) — it
  does **not** become the spine's two-stage quit.
- `ctrl+h`/`ctrl+j`/`ctrl+k`/`ctrl+l` cross to the neighbouring
  window/pane, not list selection (a viewer has no list).
- From the shared spine, a viewer only adopts: `q`/`ctrl+c` quit, `enter`
  primary (open), and the non-printable help key (+ `?` alias).

## The shared spine

The escape hatches below are what actually make these apps feel like one
family. Two rows have **honest exceptions**: a printable key can't be
reserved for quit/help on a surface where every printable key is filter
text, so pickers exempt `q` and `?`.

| Key | Meaning | Universal? |
|---|---|---|
| `enter` | primary action (select / context action / open) | yes |
| `ctrl+c` | hard quit — immediate, from any mode | yes |
| help key (non-printable, e.g. `F1`) | full keymap overlay | yes |
| `ctrl+j`/`ctrl+k` + arrows | move list selection | list surfaces only (picker + board) — viewer has no list and uses `ctrl+hjkl` for window nav instead |
| `esc` | two-stage: clear filter/selection, then back/quit | list surfaces only — viewer's `esc` resets the view instead |
| `alt+j`/`alt+k`, `alt+h`/`alt+l` | preview scroll (vertical / horizontal) | wherever a preview pane exists |
| `?` | keymap overlay (alias for the help key) | board + viewer only — in a picker, `?` is filter text |
| `q` | quit | board + viewer only — in a picker, `q` is filter text; use `esc`/`ctrl+c` instead |

## Filter presentation (always visible)

The filter/search box is a **persistent, always-rendered** element on every
surface that has one — it never pops in and out. Archetypes differ only in
whether it starts **focused**:

| Archetype | Box shown? | Focused by default? | Activate |
|---|---|---|---|
| Picker | yes | yes — typing filters immediately | already focused |
| Board | yes | no — blurred; letters are actions | `/` focuses it; `esc` blurs it back |
| Viewer | no | — | — |

When blurred, the board's box shows a placeholder hint (e.g. "type to
filter…") rather than disappearing, so the `/` affordance is always visible
and discoverable.
