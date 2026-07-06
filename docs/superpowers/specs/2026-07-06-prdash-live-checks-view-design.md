# prdash — Live combined checks view

**Date:** 2026-07-06
**Status:** Design approved, ready for planning

## Problem

The preview pane's triage card is the summary surface for a focused PR. Two gaps
around CI checks:

1. **Failing and in-progress checks are never shown together.** When a PR has
   failing checks, the `KindChecksFailing` card lists only the failing names —
   you can't see which checks are still running. When nothing has failed yet,
   `KindChecksRunning` lists the pending names, but `renderCard` paints *all*
   card `Lines` in red (`failStyle`), so running checks read as failures.
2. **The list is static.** Check state only updates on launch, a filter switch,
   or after an inline action. While CI runs, you must trigger a refresh yourself
   to see checks flip from running → pass/fail.

## Goal

- Show **failed and still-running checks together**, styled apart, in the triage
  card.
- Make the whole list **live**: auto-poll while any shown PR has running checks,
  so rows and cards update on their own; stop polling once everything settles.

Passed checks stay out of the card — the full pass/fail/pending list already
lives in the expanded **Checks** tab, which is unchanged.

## Part 1 — Combined checks display

### Data model (`internal/triage`)

Replace `Card.Lines []string` with two typed fields:

```go
type Card struct {
    Kind        Kind
    Headline    string
    Failing     []string // failing check labels
    Running     []string // in-progress check labels
    ActionKey   string
    ActionLabel string
    JumpTab     string
}
```

`Lines` is currently set only by the two check cards, so this repurposing has no
other callers.

### `triage.Compute`

- `KindChecksFailing`: populate **both** `Failing` (existing) and `Running`
  (compute `pending` even when failures exist). Headline:
  - both present → `"2 failing · 3 running"`
  - failing only → `"2 checks failing"` (unchanged)
- `KindChecksRunning`: `Running` only, headline `"Checks running…"` (unchanged).

The `pending` slice is already computed in `Compute`; the only new work is
threading it into the failing card.

### `renderCard` (`internal/ui/card.go`)

Render the two line groups with distinct glyph + style:

- `Failing` → red `✗` glyph (`failStyle`)
- `Running` → yellow `●` glyph (`pendStyle`)

Example card body:

```
✗ 2 failing · 3 running
  ✗ lint
  ✗ test-integration
  ● build
  ● test-unit
  ● e2e
```

Both groups truncate to card width as `Lines` does today.

## Part 2 — Live whole-list auto-poll

Mirror the existing spinner-loop idiom (`m.spinning` + self-rescheduling
`spinnerTick`). No new architecture — reuses the existing fetch → `prsFetchedMsg`
path.

### New pieces (`internal/ui`)

- `const pollInterval = 30 * time.Second`
- `m.polling bool` — poll loop running (one loop only, like `m.spinning`).
- `checksPollMsg` (in `messages.go`).
- `checksPollTick()` — `tea.Tick(pollInterval, …)`.
- `anyChecksRunning()` — scans **all shown rows** (both halves in mine-view) for
  `CIState() == "pending"`.

### Loop lifecycle

- After a list fetch lands (`prsFetchedMsg` / `mineFetchedMsg`, current filter):
  if `anyChecksRunning()` && `!m.polling`, start the loop (`m.polling = true`,
  batch in `checksPollTick()`).
- On `checksPollMsg`:
  - `!anyChecksRunning()` → stop: `m.polling = false`, do **not** reschedule.
  - busy — `m.pending != nil` (confirm) || `m.filtering` || `m.showPicker` ||
    `m.actionRunning()` || `m.refreshing` → skip this beat, just reschedule
    (never reorder rows under the user mid-action).
  - otherwise → fire a silent background reconcile (`fetchCmd(m.filter)` or
    `mineFetchCmd()` for mine-view; set `m.refreshing = true`, start spinner; no
    row-clearing) **and** reschedule the tick.

The reconcile funnels through the same `prsFetchedMsg`/`mineFetchedMsg` handlers,
which update rows in place. When the fetch shows nothing running anymore, the
next tick stops the loop.

### Cost

One `gh pr list` per 30 s (two in mine-view) **only while something is in
flight**; self-stopping once all checks settle. `pollInterval` is a `const` — no
config knob (YAGNI).

## Testing

- `triage_test`: `Compute` populates `Running` on a `KindChecksFailing` card when
  pending checks coexist; combined headline `"N failing · M running"`; failing-only
  headline unchanged.
- `card_test`: `renderCard` renders `Failing` with the red `✗` glyph and `Running`
  with the yellow `●` glyph; empty groups render nothing.
- `prlist_test`: `anyChecksRunning` detection (single view + mine-view both
  halves); poll starts when a fetch shows running checks; poll stops when none
  run; poll skips the fetch while a confirm prompt / picker / filter / inline
  action / in-flight refresh is active.

## Out of scope

- A dedicated manual-refresh keybinding (none exists today; not needed here).
- Configurable poll interval.
- Changes to the expanded **Checks** tab (already shows the full list).
