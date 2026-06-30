# prdash visual & interaction polish v2 — Catppuccin palette, borders, actionability sort, floating overlays (design)

**Date:** 2026-06-29
**Status:** Draft (brainstormed)
**Author:** Noam
**Builds on:** `2026-06-29-prdash-ux-layout-sweep-design.md` (the dense board, the focus-driven side card, the context-aware action-bar intent, the reliable-vs-detail-derived column rule).

## Purpose

The layout sweep landed the dense board and the side card, but the result still
reads as a flat, blue-tinted wall of glyphs. This sweep is a **visual and
interaction polish pass** on that surface: a deliberate Catppuccin palette,
real columns and borders, an actionability sort, clearer signage, and
first-class floating overlays.

It is layout/information-design only — no new product surface, still
worktree-first. Ten observations + two open questions from the author drive it,
consolidated into the sections below.

## 1. Palette — owned Catppuccin Mocha, mauve-led

Today `theme.go` uses 256-color **indices** (`accent`=39 blue, header/focus=81
cyan, mauve=141 for selection only) that **inherit the terminal's theme**. Two
problems: the indices only approximate Catppuccin (141 ≈ `#af87ff`, bluer than
true mauve), and the palette reads blue/cyan-led rather than mauve-led.

**Decision: stop inheriting the terminal theme. Own the palette as hardcoded
Catppuccin Mocha hex constants** (lipgloss v2 renders truecolor), structured for
future extension.

### Theme structure

A `Theme` struct with named role fields, constructed per flavor:

```go
type Theme struct {
    Accent, Header, Focus, Select        lipgloss.Color // mauve, mauve, sky, pink
    Text, Meta, Rule, RowBg              lipgloss.Color // text, subtext0, surface2, surface0
    Pass, Fail, Pending                  lipgloss.Color // green, red, yellow
    Author []lipgloss.Color                             // distinct author hues
}

func Mocha() Theme { /* the table below */ }
```

The package builds the role `lipgloss.Style` values from the active `Theme`.
Adding Latte/Frappé/Macchiato or a **dark↔light toggle is a later one-block
swap** (a second constructor + a key) — structured for now, **not built now**
(YAGNI: the toggle is explicitly deferred).

### Mocha role map

| Catppuccin name | Hex | Role |
|---|---|---|
| Mauve | `#cba6f7` | **accent** — `#`, action keys, links, card headline; **header / active tab** |
| Sky | `#89dceb` | focus bar (cursor row) — cool, stays distinct from mauve |
| Pink | `#f5c2e7` | select mark `●` — warm, distinct from both accent and focus |
| Green | `#a6e3a1` | CI pass / approved |
| Red | `#f38ba8` | CI fail / changes requested / conflict |
| Yellow | `#f9e2af` | pending / behind-base |
| Text | `#cdd6f4` | row titles, body |
| Subtext0 | `#a6adc8` | meta — age, labels, dim hints |
| Surface2 | `#585b70` | divider rules, pane borders |
| Surface0 | `#313244` | cursor-row background |

The per-author palette keeps its current intent (stable hue per login, bots
dim) but is sourced from Catppuccin hues (e.g. lavender, teal, peach, sapphire,
maroon, flamingo) chosen to stay clear of the red/green/yellow state colors.

## 2. Column alignment

Rows are ragged because `#num` (3–5 digits) and the `author+age` right block are
both variable width. Fix with **fixed columns**, widths computed per frame:

- number — right-aligned to max digit count across the shown set (min 4)
- age — right-aligned to 3 (`12m`/`5h`/`3d`)
- author — when shown, padded/truncated to a fixed width

With grouping (§4) the author usually moves into the group header, so most rows
end `… title<pad>age` — a clean right rule.

## 3. Sort — actionability ladder, drafts last

There is **no sort today** — rows render in raw `gh pr list` order, and the
`isDraft` field (already fetched) is unused. Introduce a stable comparator:

**Rank (lower = higher on the board):**

1. ready (approved + passing + clean)
2. changes requested
3. checks failing
4. checks running
5. waiting on review
6. **draft — always last**

Ties broken by `updatedAt` descending.

**Stability rule (deliberate):** the rank uses only signals **reliable from the
bulk list** — CI rollup, `reviewDecision`, `isDraft`. Conflict / behind-base is
detail-derived (known only for the ~5 prefetched rows; see the base spec), so it
**does not feed the sort** — otherwise the board would reshuffle under the cursor
as background prefetch lands. Conflict still stands out via the red `!` column
and a dim "blocked" row treatment; it just never reorders rows. A stable board
beats a perfectly-ranked one.

Draft rows render **dimmed** (title + glyphs in the meta/subtext color) in
addition to sinking to the bottom. Because pure dimming makes them too easy to
miss, a draft also carries a `[draft]` tag painted in a dedicated peach role
(`Theme.Draft`, kept out of the author rotation so it can't clash) — so on an
otherwise-receded row the one thing that stands out is what it is. A text tag
(not a glyph) keeps it legible without depending on the §6 legend.

**Focus highlight.** The cursor row gets a full-width `Theme.RowBg` (surface0)
background — the prominence cue the base spec called for, now implemented. Since
lipgloss resets the background after each styled segment, the row renderer
re-applies the background's opening sequence after every reset to fill the whole
line. Focus also **overrides draft dimming for the title**: the hovered row is
always bright/bold so the cursor never gets lost on a dim draft (the `[draft]`
tag still marks it).

**Hide-drafts toggle.** `D` toggles excluding draft PRs from the board entirely
(a `PRSection.hideDrafts` filter applied as the shown set is built, so it
composes with the author filter and grouping). The status bar shows
`drafts hidden` while active.

## 4. Grouping — driven by author cardinality

The author asked to group by assignee and to hide their own handle on "my" PRs.
Rather than couple to which preset is active, **drive grouping off the data**:

- **All shown PRs share one author → flat list, no author column.** Covers the
  "mine" filter and any single-author view; the redundant handle disappears.
- **≥2 distinct authors → group under dim author-rule headers** (`alice ─────`),
  with the handle in the header, not repeated per row. Within each group, the §3
  actionability ladder. Groups ordered by their most-actionable member, ties by
  recency.

Self-adjusting, no preset plumbing. The existing PRs/Issues split is preserved;
grouping is *within* the PR section. Issues keep their current flat rendering.

## 5. Borders — rounded panes + preview sectioning

Rounded (`lipgloss.RoundedBorder`) borders, drawn in the Surface2 rule color:

- **List pane** — titled with the section label (`PRs · 12`).
- **Preview pane** — titled with the focused identifier (`#309`).
- **Action bar** — the bottom key bar (§7), bordered.

Inside the preview, the current wall-of-info splits into **labeled dim
subsections** separated by thin rules: **identity · blocker · checks · review ·
latest**. `computeLayout` and `previewWidth` subtract the border + padding so
content never clips against the frame (the existing `MaxWidth/MaxHeight` clip
stays as the backstop).

## 6. Legend / clearer signage

The single-glyph CI / RV / `!` columns are cryptic. Two additions:

- A thin **dim column-header row** above the list, labeling the glyph columns:
  `CI RV !  #    title                      age`. Self-documenting at a glance.
- A **`?` floating help overlay** (same chrome as the action menu, §10) with the
  full glyph legend **and** the keybinding reference.

## 7. Keybindings — context-aware bar, bordered, audited

> **Landed early (feedback):** the bottom bar is now **pure keybindings** — the
> status text (`N selected`, `drafts hidden`) moved up to the header, and the bar
> **leads with the focused PR's recommended action** (`r rerun failed`,
> `m merge`, …) as a real, pressable key that changes with the cursor. The
> remaining Phase D work is the rounded border (§5) and the full state-specific
> key set.

- The bottom bar gets the rounded border (§5) and is restored to
  **context-aware** (it drifted to a static string that omits live verbs like
  `y`/`o`/`↵`/`m`): always-present core verbs + the one state-specific key for
  the focused PR, exactly as the base spec's §"Context-aware action bar"
  specified. This directly addresses "keys hard to discover / some seem to do
  nothing" — every applicable verb is shown for the focused PR's state.
- **Audit, not a known bug.** The draft card already routes Mark-ready through
  `a` (`triage.go` sets `ActionKey:"a"`), so there is *no* confirmed phantom
  keystroke today; the multi-character `ready` action is reachable via the `a`
  menu and works. The task is therefore a **guard + audit**: a test asserting no
  card/bar `ActionKey` is a multi-character string (so a future regression can't
  advertise an unpressable key), plus an audit that every key the bar shows maps
  to a live handler. *(If Mark-ready should become first-class rather than
  menu-only, bind a free single key — e.g. `p` "publish". Default: menu-only,
  unchanged.)*

## 8. Expanded view — `h` on the first tab zooms out

In `updateExpanded`, `h` / `←` currently wraps from the leftmost tab
(Conversation, index 0) around to Diff. Change: on the **first tab**, `h` / `←`
**exits the expanded view** back to the list. The same gesture un-maximizes when
in `z` preview-max. Number keys and `tab` still cycle through tabs as today.

## 9. Preview navigation

The side preview is static clipped text today — it cannot scroll. Give it **its
own `viewport`**. **`Ctrl+j` scrolls it down, `Ctrl+k` up**; the list keeps
plain `j/k`. No mode switch.

**Caveat to verify in impl:** in many terminals `Ctrl+J` is byte-identical to
Enter (LF). If bubbletea v2 cannot distinguish `ctrl+j` from `enter`, fall back
to **`Ctrl+d` / `Ctrl+u`** (half-page). Verify against the real terminal before
committing the binding.

## 10. Floating action menu (and `?` overlay)

The `a` menu is a plain inline list today. Make it a **centered floating
panel**: rounded mauve border, `Actions` title, the filter input inside,
selected row highlighted (mauve background / accented key column), rendered over
a dimmed list. The `?` legend (§6) reuses the same floating chrome.

**Caveat:** true z-layer overlays in lipgloss v2 may require manual composition.
If layering fights, render the centered panel on a cleared frame — visually
equivalent for a modal.

## Testing

Follow the existing table-driven style:

- **Theme** — `Mocha()` returns the expected role→hex map; role styles resolve
  to truecolor (no 256-index fallback).
- **Row renderer** — fixed-width number/age/author columns align across varied
  inputs; draft rows render dimmed; column-header row labels match the columns.
- **Sort** — actionability rank order; drafts always last; ties by `updatedAt`;
  conflict/behind does **not** change order (stability rule); sort is stable as
  detail arrives (re-render with new detail does not reorder).
- **Grouping** — single distinct author → flat, no author column; ≥2 → grouped
  headers with handle hoisted; within-group ladder; group order by most
  actionable.
- **Action bar** — context-aware visible-key set per focused-PR state; core
  verbs always present; no `ActionKey` is a multi-char string (no phantom keys).
- **Expanded** — `h`/`←` on tab 0 exits; on tabs 1–3 moves left; un-maximizes
  from `z`.
- **Preview scroll** — Ctrl+j/k (or the fallback) scroll the preview viewport;
  list `j/k` unaffected.
- **Overlays** — action menu + `?` legend render bordered/centered; filter +
  selection behavior unchanged.
- **Layout** — `computeLayout`/`previewWidth` account for the new border +
  padding; nothing clips into the frame.

## Phasing

- **Phase A — palette + rows.** `Theme` struct + `Mocha()`; rebuild role styles;
  fixed-width columns; actionability sort; draft dimming. Pure render/data
  changes, no new state.
- **Phase B — grouping.** Author-cardinality grouping, hoisted handle, group
  headers, within-group ladder.
- **Phase C — borders + signage.** Rounded list/preview/bar panes; preview
  subsection rules; column-header row; `?` legend overlay.
- **Phase D — overlays + interaction.** Floating action menu; preview viewport
  scroll (Ctrl+j/k, verified); expanded `h`-to-exit; restored context-aware bar;
  dead-key fix.

## Open decisions (resolved defaults)

- **Palette** — hardcoded Catppuccin Mocha, no terminal inheritance; `Theme`
  structured for future flavors + a deferred dark/light toggle.
- **Sort stability** — list-reliable signals only; conflict/behind never
  reorders rows (revisit if the reshuffle is preferred).
- **Mark-ready** — stays menu-only via `a` (already how the card behaves); a
  regression guard forbids multi-char `ActionKey`s (revisit: promote to single
  key `p`).
- **Legend** — both a persistent column-header row and a `?` overlay.
- **Preview scroll** — `Ctrl+j`/`Ctrl+k`, with `Ctrl+d`/`Ctrl+u` fallback if the
  terminal conflates `ctrl+j` with Enter.
