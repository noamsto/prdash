# PR description surfaces — design

**Issue:** [#38](https://github.com/noamsto/prdash/issues/38)
**Date:** 2026-07-20

## Problem

The PR body (`gh.PR.Body`) is fetched at list time (it is in `prFields`) but is
never displayed anywhere for PRs. Only *issues* render a body today, via
`issuePreviewPane`. When reviewing someone else's PR you have to leave the app to
read what the change even does.

## Goal

Surface the PR description in two places:

1. **Preview pane** — a compact `description` section, sized by authorship.
2. **Expanded view** — a dedicated, full-text **Description** tab.

Both render from list data (`PR.Body`), so neither waits on the per-PR detail
fetch — the same property the Checks tab already relies on.

## Non-goals

- No change to what gh fetches (`Body` is already requested).
- No comment/timeline changes; the Conversation tab keeps rendering the timeline.
- No issue-body changes; `issuePreviewPane` already shows an issue body.

## Design

### Rendering

Both surfaces reuse `preview.Render(body, width)` (glamour markdown → ANSI,
memoized by body + width). No new rendering machinery.

### 1. Preview-pane description section

- **Order:** inserted under the identity header, before the blocker card:
  `identity → description → blocker → checks → review → latest`.
- **Source:** `ps.prAt(m.cursor)` — renders instantly from list data, before
  detail loads.
- **Authorship sizing** (`pr.Author.Login == m.viewerLogin`):
  - Own PR: cap the rendered body to **2 lines**.
  - Others' PR: cap to **6 lines**.
  - When the body is longer than the cap, append a dim hint:
    `· full text in Description tab`.
  - Caps are named constants so they are trivially tunable.
- **Empty body:** omit the section entirely (no `sectionRule`), so PRs with no
  description add no noise to the pane.
- Built with the existing `section(label, body)` closure and `sectionRule`
  helper already used in `previewPane`.

### 2. Expanded Description tab

- `expandedTabs = ["Description", "Conversation", "Reviews", "Checks", "Diff"]`.
- **Default landing tab on focus = Description** (index 0). `focusExpanded`
  already sets `m.expandedTab = 0` then optionally overrides via a triage card's
  jump; with Description at 0, a card with no specific jump now lands on
  Description, while reviews/checks/diff jumps still land on their tabs.
- **Content:** full body via `preview.Render`, laid out with
  `renderDiscussionColumn` (comfortable reading column, matches the prose tabs),
  **top-anchored**.
- **Source:** list data (`ps.prAt(m.cursor)`), like the Checks tab — no detail
  wait.
- **Empty body:** dim `No description provided.`

### 3. Supporting refactor — named tab constants

Inserting a tab at index 0 shifts every existing tab:

| Tab          | Old index | New index |
|--------------|-----------|-----------|
| Description  | —         | 0         |
| Conversation | 0         | 1         |
| Reviews      | 1         | 2         |
| Checks       | 2         | 3         |
| Diff         | 3         | 4         |

The expanded view hardcodes these indices in ~10 places (the Checks tab's
`m.expandedTab == 2` for cursor/log-drill navigation, the
`m.expandedTab == 0 || m.expandedTab == 1` bottom-anchor for Conversation/
Reviews, the `1`-based number-key handler, and `jumpTabIndex`). To keep the
reindex from silently breaking Checks navigation:

- Introduce constants: `tabDescription = 0`, `tabConversation = 1`,
  `tabReviews = 2`, `tabChecks = 3`, `tabDiff = 4`.
- Replace every magic index with the named constant.
- `jumpTabIndex` remaps to the new indices; its default (empty jump) returns
  `tabDescription`.

### 4. Anchoring

`renderExpanded` bottom-anchors Conversation and Reviews (most-recent-first) and
top-anchors the rest. Description joins the top-anchored set. The condition
becomes `tabConversation`/`tabReviews` at their new indices.

## Testing

Table-driven, matching `expanded_test.go` / `preview_test.go`:

- Description tab renders the body; empty body → `No description provided.`
- Description is the default tab on focus; triage jumps still reach
  reviews/checks/diff.
- Preview pane shows the description section under identity when non-empty;
  omitted when empty.
- Collapse sizing: own PR caps at 2 lines with the hint; other's PR caps at 6.
- Reindex regression: Checks cursor navigation, log drill-in, and number-key
  tab selection still target the Checks tab after the shift.
