# prdash UX polish — design

Tracking: [#3](https://github.com/noamsto/prdash/issues/3) · branch `feat/3-ux-polish`

A polish pass on the prdash TUI covering six items from a review session. Each
is independent enough to land and test on its own; they share the same `ui`/`gh`
files so they ship together.

## Decisions (locked with the user)

- **Checks rerun**: both a prettier static list in the quick view *and* a
  navigable per-check rerun in the expanded Checks tab.
- **Expanded layout**: responsive to terminal width (reuse the `computeLayout`
  pattern already used for the list side-preview).
- **Preview perf**: pre-fill the card from list data, prefetch neighbor details,
  and persist detail to the on-disk cache.
- **Label chips**: real GitHub label colors (add `color` to the fetch).
- **Rerun keys**: plain `r` (hovered) / `R` (all failed) — terminals cannot
  reliably distinguish `ctrl+r` from `ctrl+R`, but lower/upper letters are
  distinct everywhere.

## 1. Failing checks — visualization + rerun

**Data.** Extend `gh.Check` with `DetailsUrl string \`json:"detailsUrl"\``. For
GitHub Actions checks this is `…/actions/runs/<runID>/job/<jobID>`, so the job
ID is parseable with **no extra fetch**. Add `Check.JobID() string` (returns ""
for external StatusContext checks, which carry `targetUrl` and have no job).
`statusCheckRollup` already returns these sub-fields from `gh pr list`; only the
struct tags are missing.

**Quick view.** `ciLine` (preview.go) currently comma-joins failing names into
one line. Replace with a vertical list: a `✗ N checks failing` header followed
by one `  ✗ name` per failing check. Pending stays a single `● checks running`
line. The triage card's `Lines` already lists failing names — consolidate so the
card and `ciLine` don't duplicate (card keeps the headline+action; `ciLine` owns
the per-check list).

**Expanded Checks tab.** Becomes navigable:
- New `Model.checkCursor int`. While the active tab is Checks, `j/k` move the
  check cursor (instead of scrolling the viewport); the hovered row is
  highlighted. On other tabs `j/k` keep scrolling.
- `r` reruns the hovered check via `action.RerunCheck(r, dir, jobID)` →
  `gh run rerun --job <jobID>`. If the hovered check has no job ID (external CI),
  show a transient hint instead of acting.
- `R` reruns all failed via the existing `action.RerunFailed`.
- Footer reflects the new keys when on the Checks tab.
- `checkCursor` clamps to the rollup length and resets when the PR or tab
  changes.

**New action.** `action.RerunCheck(r gh.Runner, dir, jobID string) error` runs
`run rerun --job <jobID>`. Mirrors `RerunFailed`'s shape.

## 2. Label chips

- Add `Color string \`json:"color"\`` to `gh.Label` (GitHub returns a 6-hex
  string, no `#`).
- New `chipStyle(hex string) lipgloss.Style`: background = label color,
  foreground = black or white chosen by relative luminance of the hex. Render as
  `padded " name "`.
- Use chips in: the row meta line (`renderItemRow`, replacing plain
  `labelNames`) capped to the available width with a `+N` overflow marker, and
  the expanded metadata rail. The dense 2-line row keeps labels last so chips
  elide first when space is tight.

## 3. Keybindings

- **Expanded backward nav**: `h` / `left` / `shift+tab` exit to the list when
  `expandedTab == 0` instead of wrapping to the last tab. On other tabs they
  decrement as today.
- **List `enter` expands**: add an explicit `case "enter"` in the list key
  handler that calls `enterExpanded()`, so `enter` matches `l`/`→`. Opening a
  worktree remains `enter` *inside* the expanded view (already wired in
  `updateExpanded`). The `enter` entry stays in the action map for the expanded
  view; only the list's default-case lookup of it is removed.

## 4. Expanded responsive layout

Add an `ExpandedLayout(w, h)` geometry helper (layout.go). Width decides the
shape:

- **Wide** (`w >= sideThreshold`, 120): two columns.
  - Left **metadata rail** (~32–40 cols): PR `#num` + title, author, branch →
    base, label chips, requested reviewers, diffstat, CI summary line.
  - Right pane: tab strip + scrollable viewport. Viewport width = right-pane
    width; markdown/timeline render at that width (not full screen).
- **Narrow** (`w < sideThreshold`): single column with a compact multi-line
  header block (author · branch · CI · reviewers) above the tab strip, then the
  viewport at full width. Richer than today's flush-left content.

`renderExpanded` sets the viewport width from the computed content-pane width in
both modes; `expandedView` composes rail + pane (wide) or header + pane
(narrow).

## 5. UX polish pass

After 1–4 and 6 land: `go build`, run the real TUI in a tmux pane (capture-pane
via the tmux-interactive flow), and audit alignment, color contrast,
truncation, footer-hint accuracy, and empty states. Fix nits found. The binary
is run locally (the `github:` nix ref fails for this private repo).

## 6. Preview perf + cache

- **Pre-fill card**: `triage.Preliminary(pr gh.PR) Card` builds a best-effort
  card from list-only fields (draft, CI failing/running, changes-requested) with
  no merge-state. `previewPane` renders it (instead of "Loading preview…") when
  the full detail isn't cached yet, then swaps to `triage.Compute` once detail
  lands.
- **Prefetch neighbors**: on cursor move, batch detail fetches for `cursor ± 1`
  (skipping cached) via `tea.Batch`, so `j/k` navigation feels instant.
- **Persist detail to cache**: detail keyed `cache.Key("detail", repo+"#"+num,
  0, schemaVer)`. Written on `prDetailMsg`; hydrated into `m.detail` on startup.
  Stale-while-revalidate: cached detail shows instantly; the cursor visit still
  triggers a background refetch that reconciles (overwrites) the entry. The
  7-day prune already bounds staleness.
- **"PR bleeds on `f`" bug**: reproduce against the running TUI first, confirm
  root cause (suspected: the previous filter's list/preview renders during the
  in-flight refetch because `setPRs` hasn't run yet), then fix — likely clearing
  the shown set / showing the loading state on filter change.
- Bump `schemaVer` `v2` → `v3` (new `color` + `detailsUrl` fields invalidate the
  old list cache cleanly).

## Testing

Pure-function renderers and helpers, tested like the existing `*_test.go`:

- `gh.Check.JobID()` parsing (Actions URL, external URL, empty).
- `gh.Label` color JSON parse.
- `chipStyle` luminance pick (dark text on light label, light on dark).
- `triage.Preliminary` for each list-derivable kind.
- Keybindings: expanded `h` on tab 0 exits; list `enter` expands.
- `ExpandedLayout` selection at wide vs narrow widths.

## Files touched

- `internal/gh/prs.go` — `Check.DetailsUrl`, `Label.Color`, `Check.JobID()`.
- `internal/action/builtin.go` + `defaults.go` — `RerunCheck`.
- `internal/ui/theme.go` — `chipStyle`.
- `internal/ui/section.go` — chips in the row meta line.
- `internal/ui/card.go` / `preview.go` — pre-fill card, `ciLine` list.
- `internal/ui/expanded.go` — responsive layout, navigable checks, keybindings,
  metadata rail.
- `internal/ui/prlist.go` — `enter` expands, `checkCursor`, neighbor prefetch,
  detail cache hydrate/persist, `f` bleed fix, `schemaVer` bump.
- `internal/ui/layout.go` — `ExpandedLayout`.
- `internal/triage/triage.go` — `Preliminary`.
