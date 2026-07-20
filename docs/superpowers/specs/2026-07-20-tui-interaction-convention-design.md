# TUI Interaction Convention — Design

**Date:** 2026-07-20
**Status:** Approved direction; spec under review
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
Apps: **lazytmux**, **wtc**, **tmux-remux**.

- **Type → filter**, always on, fuzzy, live.
- Move selection: `ctrl+j/k` + arrows (plain `j/k` are filter text).
- `enter` → select and act.
- `esc` → two-stage: clear query if present, else quit.
- Secondary actions (few) are `ctrl`-modified; they must fit the ctrl keyspace.

### Board — *browse, then do many things*
Apps: **prdash**.

- `/` → live incremental fuzzy filter (one keystroke of ceremony, then it
  behaves like a picker's filter). prdash's server+local omni-filter is a
  superset and stays.
- Bare single-letter keys → actions (`m` merge, `r` rerun, `o` open, `a`
  actions palette, …). Unchanged.
- `tab` → switch board (PR ↔ issue). Plain `j/k` + arrows move selection
  (letters are free here because filter is gated).
- `?` → which-key / keymap overlay (see Discoverability).

### Viewer — *spatial navigation, no filter*
Apps: **aeye**.

- `h/j/k/l` + arrows → pan / move; `g/G` ends; `1-9` jump; `z/Z` zoom; `tab`
  region drill. Unchanged.
- No filter. Participates in the convention **only through the shared spine.**

## The shared spine (identical in every app)

The felt coherence lives here — the escape hatches behave the same everywhere,
which matters more than the filter-entry key.

| Key | Meaning | Notes |
|---|---|---|
| `enter` | primary action | select (picker) / context action (board) / open (viewer) |
| `esc` | two-stage: clear filter/selection → back/quit | uniform exit grammar |
| `ctrl+c` | hard quit | always, immediate, from any mode |
| `?` | full keymap overlay | every app; board adds in-overlay search |
| arrows | move selection | filter-safe everywhere |
| `ctrl+j` / `ctrl+k` | move selection down / up | filter-safe everywhere; the one nav pair that works in all archetypes |
| `alt+j/k` / `alt+h/l` | preview scroll (vert / horiz) | where a preview pane exists |

`q` is quit **only** where letters are verbs (board, viewer). In a picker `q` is
filter text; quit-from-empty is `esc` or `ctrl+c`. This is the generating rule
applied honestly to `q` itself.

## Concrete key decisions (resolving current conflicts)

1. **`ctrl+j/k` is reserved for selection movement, spine-wide.** prdash
   currently uses `ctrl+j/k` for *preview scroll* — that moves to `alt+j/k`
   (matching lazytmux, which already scrolls its preview with `alt+j/k`).
2. **Preview scroll = `alt+hjkl`** everywhere a preview exists (lazytmux's
   existing choice; keeps `alt` = "the other pane").
3. **Two-stage `esc`** replaces lazytmux's current `q`-clears-then-quits, so the
   exit grammar is identical across archetypes. lazytmux gains `q` as filter
   text as a side effect (a fix, not a regression).
4. **`?` overlay** is added to lazytmux, wtc, and aeye (prdash and tmux-remux
   already have help surfaces).

## Discoverability (`?`)

- **All apps:** `?` opens a full keymap overlay, grouped and scaled to the
  current mode. Prefer `bubbles/help` `FullHelp()` so it is generated from the
  `key.Binding` set, not hand-maintained.
- **Board (prdash):** the overlay is *searchable* — type to filter the keymap
  list (the open lazygit request, applied here). This is the discoverability
  analogue of Helix's which-key menu.

## Per-app migration impact

| App | Archetype | Changes |
|---|---|---|
| **lazytmux** | Picker (reference) | Migrate raw `switch` → `key.Binding` + `help.Model`; add `?` overlay; `esc` two-stage replaces `q`-clear-quit. Behavior otherwise unchanged. |
| **wtc** | Picker *(judgment call — see below)* | Drop the `/` gate → always-on type-to-filter; relocate `space/a/e/d/D` to `ctrl`-modified actions; `?` overlay; migrate to `key.Binding`. |
| **prdash** | Board | Keep `/` + all actions. Filter → live incremental fuzzy. `esc` two-stage. Move preview-scroll `ctrl+j/k` → `alt+j/k`. Add searchable `?` overlay. Migrate to `key.Binding` over time. |
| **tmux-remux** | Picker | Already on `key.Binding`/`help.Model`. Reconcile nav (`ctrl+j/k` + arrows) and quit (`esc` two-stage, `ctrl+c`) to spine. |
| **aeye** | Viewer | Adopt spine keys only (`enter`, `esc`, `ctrl+c`, `?`). No filter, spatial nav unchanged. |

## Open judgment call: wtc picker vs board

wtc's explorer has real verbs (`space` multi-select, `a` select-stale, `e`
expand, `d/D` delete) — that reads board-ish. It is classified as a **picker**
because:

- Its *primary* verb is "switch to one worktree" (`enter`); management is
  secondary.
- Its action set is small (~4) and **fits** the ctrl keyspace (unlike prdash's
  20), so `ctrl`-modifiers are viable: `ctrl+space`/`ctrl+d`/`ctrl+e` etc.

If you use wtc mostly to *manage* worktrees rather than *switch*, it should
instead be a board (keep `/`, keep bare-letter actions). **This is the one
classification to confirm during review.**

## Implementation strategy — spec now, library at second use

1. **Now:** write `KEYMAP.md` (this convention: rule + spine + three archetype
   tables). Canonical home recommended: **lazytmux** (the reference picker) or a
   dedicated docs location — to confirm during review. Each repo links to it.
2. **As each app is touched:** migrate its keys onto `bubbles/key.Binding` +
   `help.Model`. tmux-remux already proves the idiom in this codebase family.
3. **Extract a shared `tuikit` package only at the second duplication** — most
   likely a spine `key.Binding` set + a help renderer. The filterable-list
   widget's natural birth is the wtc conversion: port lazytmux's fuzzy filter;
   if it transplants cleanly, that becomes the library. No framework is built
   upfront for five small apps.

## Non-goals

- No single identical keymap across all apps (the rule deliberately produces
  three archetypes).
- No relocation of prdash's action vocabulary into a modifier palette (rejected:
  `ctrl` keyspace can't hold 20 actions; departs from every board-TUI norm).
- No upfront shared framework / premature abstraction.

## Success criteria

- The escape hatches (`enter`, `esc`, `ctrl+c`, `?`) behave identically in all
  five apps.
- Pickers filter as you type with zero ceremony; the board filters after one
  `/`, then behaves identically.
- `KEYMAP.md` exists and each migrated app conforms to its archetype table.
- prdash, wtc, and lazytmux are migrated; tmux-remux and aeye satisfy the spine.
