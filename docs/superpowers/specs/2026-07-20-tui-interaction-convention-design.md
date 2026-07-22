# TUI Interaction Convention — Design

**Date:** 2026-07-20
**Status:** Approved direction; revised after adversarial review (spec-critic,
2026-07-20); under user review
**Scope:** A coherent navigation / filter / keybinding convention shared across
Noam's Go/Bubble Tea TUIs. First migrations: `prdash`, `wtc`, `lazytmux`.
`tmux-remux` and `aeye` adopt the shared spine only.

## Problem

Five Bubble Tea TUIs have drifted into five different interaction models:

| App | Filter trigger | List nav | Actions | Keybind mechanism |
|---|---|---|---|---|
| lazytmux | type-to-filter, always on (fuzzy) | `ctrl+j/k` + arrows | all `ctrl`-modified / `enter` | raw `switch`, hardcoded footer |
| prdash | `/` then type (omni-filter) | `j/k` + arrows | ~20 bare single-letter | raw `switch`, `?` legend modal |
| tmux-remux | none (boolean toggles `s/d/a`) | `j/k/h/l`, `tab` focus | `enter`/`q`/`?` | `key.Binding` + `help.Model` |
| wtc | `/` then type (substring) | `j/k` + arrows | `space a e d D` | raw `switch`, hardcoded footer |
| aeye | none | `h/l/j/k`, `n/p`, `g/G`, `1-9` | `o O y d r`, `z/Z`, `tab` | raw `switch`, static legend |

Four filter models, two nav conventions, five hand-rolled help renderings, and
only one app on the idiomatic `key.Binding`/`help.Model` machinery. Muscle memory
does not transfer between them.

## The trade-off against the original ask (read this first)

The request that started this was: *"allow filtering by typing by default, not by
pressing `/`."* This spec **does not** grant that everywhere. The two boards
(prdash, wtc) keep `/`, because on an action-rich surface the letters are spent
on verbs (prdash's ~20 actions; wtc's `d`/`D` delete) and typing-to-filter would
cost you those single-key actions. Always-on type-to-filter is delivered only on
the pickers, where letters have no other job.

This is a deliberate, eyes-open reversal of the literal ask on the two boards.
Both were confirmed with the user on 2026-07-20: **wtc** as a delete-heavy
manager, and **prdash** explicitly ("keep `/` on prdash" chosen over prototyping
always-on type-to-filter). The trade-off is settled; no re-open pending.

## The generating rule

One principle produces the whole convention:

> **Letters do the surface's primary verb. `/` summons the filter only where
> letters are verbs.**

- Where the surface's job is *choose one thing*, letters have no other job, so
  typing filters directly.
- Where the surface has *many verbs* (merge, rerun, delete…), letters are spent
  on those verbs, so the filter hides behind `/`.

This is one rule applied to different surfaces, not two competing conventions.
It also matches the entire action-rich-TUI ecosystem: lazygit, k9s, gh-dash
(prdash's direct ancestor), yazi, and Charm's own `bubbles/list` all gate filter
behind `/`; fzf / telescope / lazytmux (pure pickers) all filter as you type.

## The three archetypes

Applying the rule classifies every surface into one of three archetypes.

### Picker — *choose one thing*
Apps: **lazytmux** (has always-on filter today), **tmux-remux** (target
classification; no text filter today — see phasing below).

- The filter box is **always visible and focused** — type filters immediately,
  fuzzy, live (no `/`). See "Filter presentation".
- Move selection: `ctrl+j/k` + arrows. **Where a text filter is active, plain
  `j/k` are filter text** (so lazytmux navigates with `ctrl+j/k`/arrows). An app
  with no text filter yet (tmux-remux) may keep bare `j/k` *in addition to* the
  spine's `ctrl+j/k`+arrows until it gains a filter.
- `enter` → select and act.
- `esc` → two-stage: clear query if present, else quit.
- Help: a **non-printable** key (see Discoverability) — `?` is filter text here.
- Secondary actions (few) are `ctrl`-modified; they must fit the ctrl keyspace.

### Board — *browse, then do many things*
Apps: **prdash**, **wtc**.

- The filter box is **always visible** (see "Filter presentation") but starts
  **blurred**. `/` focuses it → live incremental fuzzy filter; `esc` blurs it
  back. prdash's server+local omni-filter is a superset and stays.
- Bare single-letter keys → actions (`m` merge, `r` rerun, `o` open, `a`
  actions palette, …) while the box is blurred. Unchanged.
- `tab` → switch board (PR ↔ issue). Plain `j/k` + arrows move selection
  (letters are free here because filter is gated).
- `?` → which-key / keymap overlay (see Discoverability).

**wtc** is a board too — its job is managing and deleting worktrees. `/`
filters; `d`/`D` delete / force-delete, `space` multi-selects, `a` selects all
stale, `e` expands, `enter` switches to the worktree — all bare letters,
unchanged. Deletion stays a single keystroke.

### Viewer — *spatial navigation, no filter*
Apps: **aeye**.

- `h/j/k/l` + arrows → pan / move; `g/G` ends; `1-9` jump; `z/Z` zoom; `tab`
  region drill. Unchanged.
- No filter. Participates in the spine **partially** — a viewer has no list
  selection, so the spine's `ctrl+j/k` selection nav does not apply, and aeye
  already owns `ctrl+h/j/k/l` for crossing to the neighbouring kitty/tmux window
  (`gallery.go:296`). Those stay.
- **`esc` exemption:** aeye's `esc` resets zoom / exits region drill-down
  (`gallery.go:339`). It keeps that meaning; quit is `q` / `ctrl+c` only. The
  spine's two-stage `esc`→quit does **not** override viewer `esc`.
- Spine keys aeye *does* adopt: `q`/`ctrl+c` quit, `enter` primary (open), and a
  non-printable help key.

## The shared spine

The felt coherence lives here — the escape hatches behave the same everywhere,
which matters more than the filter-entry key. Two keys have **honest exceptions**
that follow directly from the rule (a printable key cannot be reserved on a
surface where every printable key is filter input): they are marked below.

| Key | Meaning | Universal? |
|---|---|---|
| `enter` | primary action (select / context action / open) | yes |
| `ctrl+c` | hard quit — immediate, from any mode | yes |
| help key (non-printable, e.g. `F1`) | full keymap overlay | yes — see Discoverability |
| `ctrl+j` / `ctrl+k` + arrows | move list selection | **list surfaces only** (picker + board); viewer has no list and owns `ctrl+hjkl` for window nav |
| `esc` | two-stage: clear filter/selection → back/quit | **list surfaces only**; viewer `esc` = reset view |
| `alt+j/k` / `alt+h/l` | preview scroll (vert / horiz) | where a preview pane exists |
| `?` | keymap overlay (alias) | **board + viewer only** — in a picker `?` is filter text |
| `q` | quit | **board + viewer only** — in a picker `q` is filter text; use `esc`/`ctrl+c` |

`q` and `?` are quit / help **only** where letters are verbs (board, viewer). In
a picker both are filter text; the generating rule is applied honestly to them.
The truly universal keys are `enter`, `ctrl+c`, the non-printable help key, and
(on list surfaces) `ctrl+j/k`+arrows and two-stage `esc`.

## Filter presentation (always visible)

The filter/search input is a **persistent, always-rendered UI element** on every
surface that filters — it never pops in and out. This is the strongest single
source of visual coherence, and it makes prdash's `/` discoverable (you can see
the box you're activating). The archetypes differ only in **focus**, not
presence:

| Archetype | Box shown? | Focused by default? | Activate |
|---|---|---|---|
| Picker | yes | **yes** — typing filters immediately | (already focused) |
| Board | yes | no — blurred; letters are actions | `/` focuses; `esc` blurs |
| Viewer | no (aeye has no filter) | — | — |

Concretely: prdash and wtc render their filter bar at all times (like gh-dash's
persistent search bar) rather than only while filtering. The bar shows a
placeholder ("type to filter…" / the omni-filter hint) when blurred, and the
live query when focused. This is a real change for prdash and wtc, which today
show the input only after `/`.

## Concrete key decisions (resolving current conflicts)

1. **`ctrl+j/k` is reserved for list-selection movement on list surfaces.**
   prdash currently uses `ctrl+j/k` in two places: main-list *preview scroll*
   (`prlist.go:1395,1398`) and actions-palette *selection nav*
   (`prlist.go:1346,1351`). Only the **preview-scroll** usage moves to `alt+j/k`;
   the palette usage already matches the spine and stays.
2. **Preview scroll = `alt+hjkl`** everywhere a preview exists (lazytmux's
   existing choice; keeps `alt` = "the other pane"). Verified collision-free in
   lazytmux, tmux-remux, wtc.
3. **Two-stage `esc`** (list surfaces) replaces lazytmux's current
   `q`-clears-then-quits, so the exit grammar is identical across picker+board.
   lazytmux gains `q` as filter text as a side effect (a fix, not a regression).
4. **Help overlay** is added to lazytmux, wtc, and aeye (prdash and tmux-remux
   already have help surfaces), bound to the non-printable help key below.

## Discoverability (help)

- **Help key:** a **non-printable** key, canonically **`F1`** (`ctrl+g` is the
  fallback if `F1` proves unreliable in a target terminal — settle in the plan).
  It works in every archetype including pickers, where `?` is filter text.
- **`?` as alias:** on board + viewer surfaces (letters free), `?` also opens the
  overlay — the conventional muscle-memory key stays where it can.
- **All apps:** the overlay is generated from the `key.Binding` set via
  `bubbles/help` `FullHelp()`, not hand-maintained.
- **Board (prdash):** the overlay is *searchable* — type to filter the keymap
  list (the open lazygit request, applied here). The discoverability analogue of
  Helix's which-key menu.

## Per-app migration impact

| App | Archetype | Changes |
|---|---|---|
| **lazytmux** | Picker (reference) | Migrate raw `switch` → `key.Binding` + `help.Model`; add `?` overlay; `esc` two-stage replaces `q`-clear-quit. Behavior otherwise unchanged. |
| **wtc** | Board | Keep `/` filter and bare-letter actions (`d/D` delete, `space` multi-select, `a` select-stale, `e` expand). **Render the filter bar always-visible** (blurred until `/`). Align to spine (`esc` two-stage, `ctrl+c`, `?` overlay, `ctrl+j/k` nav, `alt+hjkl` preview scroll); migrate to `key.Binding`. |
| **prdash** | Board | Keep `/` + all actions (confirmed 2026-07-20). Filter → live incremental fuzzy, **filter bar always-visible** (blurred until `/`). `esc` two-stage. Move main-list preview-scroll `ctrl+j/k` → `alt+j/k` (palette `ctrl+j/k` stays). Add searchable help overlay + `?` alias. Migrate to `key.Binding` over time. |
| **tmux-remux** | Picker *(target)* | Already on `key.Binding`/`help.Model`. **This pass:** add `ctrl+j/k`+arrows spine nav *alongside* existing bare `j/k` (no removal), `ctrl+c` hard quit, non-printable help key + `?` alias, two-stage `esc`. **Deferred:** always-on type-to-filter and the resulting `s/d/a` relocation — until then it keeps bare `j/k` nav and boolean toggles. |
| **aeye** | Viewer | Adopt `enter`, `ctrl+c`, non-printable help + `?` alias only. Keep `ctrl+hjkl` (window nav) and `esc` (reset zoom). No filter; spatial nav unchanged. |

## Resolved: wtc is a board

wtc's primary use is *managing and deleting* worktrees, not switching to one.
Its verbs (`d/D` delete, `space` multi-select, `a` select-stale, `e` expand)
are the point, so by the rule it keeps `/`-gated filter and single-key actions —
deletion stays one keystroke. (Confirmed with the user, 2026-07-20.)

This leaves the always-on-type-to-filter conversions to the pickers (lazytmux,
tmux-remux); the two boards (prdash, wtc) keep `/`.

## Implementation strategy — spec now, library at second use

**Decomposition.** These are five separate Go modules in five repos
(`github.com/noamsto/prdash`, `github.com/noamsto/wt`, lazytmux, tmux-remux,
aeye) — no single branch or PR can span them. The work decomposes into
independent units, each its own branch/PR: (0) the `KEYMAP.md` convention doc,
then one migration per repo. The implementation plan produced from this spec
covers unit 0 plus the **prdash / wtc / lazytmux** migrations; tmux-remux and
aeye get lighter spine-only units.

1. **Now:** write `KEYMAP.md` (this convention: rule + spine + three archetype
   tables). Canonical home recommended: **lazytmux** (the reference picker) or a
   dedicated docs location — to confirm during review. Each repo links to it.
2. **As each app is touched:** migrate its keys onto `bubbles/key.Binding` +
   `help.Model`. tmux-remux already proves the idiom in this codebase family.
3. **Extract a shared `tuikit` package only at the second duplication** — most
   likely a spine `key.Binding` set + a help renderer. The filterable-list
   widget's natural birth is the wtc conversion: port lazytmux's fuzzy filter;
   if it transplants cleanly, that becomes the library. No framework is built
   upfront for five small apps. **Cross-module distribution is unsolved and
   deferred:** sharing one package across five separate private modules needs a
   published module or a `replace`/monorepo strategy (note: prdash's private-repo
   nix refs already fail — see project memory), to be designed at extraction time.

## Non-goals

- No single identical keymap across all apps (the rule deliberately produces
  three archetypes).
- No relocation of prdash's action vocabulary into a modifier palette (rejected:
  `ctrl` keyspace can't hold 20 actions; departs from every board-TUI norm).
- No upfront shared framework / premature abstraction.

## Success criteria

- The universal escape hatches (`enter`, `ctrl+c`, the non-printable help key)
  behave identically in all five apps; `esc` and `ctrl+j/k` behave identically on
  the list surfaces (picker + board), with the viewer's documented exemptions.
- The filter bar is always visible on every filtering surface; pickers filter as
  you type with zero ceremony; the boards focus the bar with one `/`, then behave
  identically.
- `KEYMAP.md` exists and each migrated app conforms to its archetype table.
- prdash, wtc, and lazytmux are migrated; tmux-remux and aeye satisfy the spine.
