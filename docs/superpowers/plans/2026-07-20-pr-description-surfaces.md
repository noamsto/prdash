# PR Description Surfaces Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Display the PR body (`gh.PR.Body`) — which is fetched but never shown for PRs — in the preview pane (a compact, authorship-sized `description` section) and in a new full-text **Description** tab in the expanded view.

**Architecture:** Both surfaces render from list data (`PR.Body`) via the existing memoized `preview.Render`, so neither waits on the per-PR detail fetch. A supporting refactor replaces the expanded view's hardcoded tab indices with named constants (`tabDescription`…`tabDiff`) so inserting a tab at index 0 can't silently break Checks navigation.

**Tech Stack:** Go, charm.land/bubbletea v2, charm.land/lipgloss v2, glamour (via `internal/preview`).

## Global Constraints

- Both surfaces render from list data (`ps.prAt(m.cursor)`), never gated behind `m.detail[...]` being cached.
- Reuse `preview.Render(body, width)` for markdown → ANSI (do not add rendering machinery).
- Follow existing table-driven test style in `internal/ui/*_test.go`.
- `sectionRule` uppercases its label, so the preview section renders as `DESCRIPTION ─`.
- The design doc for this work is `docs/superpowers/specs/2026-07-20-pr-description-surfaces-design.md`.

---

## File Structure

- `internal/ui/expanded.go` — expanded (focused) view: tab list, tab constants, `expandedBody`, `jumpTabIndex`, key handling, footer, anchoring. Modified in Tasks 1 & 2; gains `renderDescription`.
- `internal/ui/expanded_test.go` — tab-index-dependent tests. Modified in Task 2.
- `internal/ui/preview.go` — side preview pane: `previewPane`. Modified in Task 3; gains `previewDescriptionBody`.
- `internal/ui/preview_test.go` — preview pane tests. Modified in Task 3.

---

## Task 1: Named tab constants (pure refactor, no behavior change)

Introduce constants for the current four tabs and replace every magic index in
`expanded.go`. The tab order and all index *values* are unchanged, so existing
tests stay green — this de-risks the reindex in Task 2.

**Files:**
- Modify: `internal/ui/expanded.go` (`expandedTabs` at line 16; `jumpTabIndex` 19-28; `expandedBody` 133-149; `enterExpanded` 111; `renderExpanded` 202; `updateExpanded` 237, 257-301; `expandedFooter` 414)

**Interfaces:**
- Produces: constants `tabConversation = 0`, `tabReviews = 1`, `tabChecks = 2`, `tabDiff = 3` (via `iota`), consumed by Task 2.

- [ ] **Step 1: Add the tab constants above `expandedTabs`**

In `internal/ui/expanded.go`, immediately above `var expandedTabs = ...` (line 16), add:

```go
const (
	tabConversation = iota
	tabReviews
	tabChecks
	tabDiff
)
```

- [ ] **Step 2: Replace magic indices with the constants**

In `jumpTabIndex`, change the numeric returns:

```go
func jumpTabIndex(jump string) int {
	switch jump {
	case "reviews":
		return tabReviews
	case "checks":
		return tabChecks
	case "diff":
		return tabDiff
	default:
		return tabConversation
	}
}
```

In `expandedBody`, change the switch cases `case 1:` → `case tabReviews:`, `case 2:` → `case tabChecks:`, `case 3:` → `case tabDiff:` (leave `default:` — it is Conversation).

In `enterExpanded`, change `m.expandedTab = 0` → `m.expandedTab = tabConversation`.

In `renderExpanded`, change:

```go
	if m.expandedTab == tabConversation || m.expandedTab == tabReviews {
		m.vp.GotoBottom()
	} else {
		m.vp.SetYOffset(0)
	}
```

In `updateExpanded`: change `if m.expandedTab == 0 {` (the `left`/`h` exit branch) → `if m.expandedTab == tabConversation {`. Change every `if m.expandedTab == 2 {` (the `r`, `R`, `o`, `Y`, `j`/`down`, `k`/`up`, and `enter` branches) → `if m.expandedTab == tabChecks {`.

In `expandedFooter`, change `if m.expandedTab == 2 {` → `if m.expandedTab == tabChecks {`.

- [ ] **Step 3: Build and run the ui package tests to verify no behavior change**

Run: `cd internal/ui && go test ./...`
Expected: PASS (all existing tests green; values are identical to before).

- [ ] **Step 4: Commit**

```bash
git add internal/ui/expanded.go
git commit -m "refactor(ui): name expanded tab indices (#38)"
```

---

## Task 2: Description tab (insert at index 0, default landing, render from list data)

Insert **Description** as the first tab, renumber the constants, render the full
body, and update the index-dependent tests.

**Files:**
- Modify: `internal/ui/expanded.go` (tab constants; `expandedTabs`; `jumpTabIndex`; `expandedBody`; number-key handler in `updateExpanded`); add `renderDescription`.
- Modify: `internal/ui/expanded_test.go` (`TestJumpTabIndex`, `TestEnterExpandedDeepLinks`, `TestChecksTabCursorNavigates`, `TestTabSegmentMarksActive`, `TestExpandedBoxWidthCapsEveryTab`, `TestExpandedFooterNeverOffersPan`, `TestConversationOpensAtMostRecent`); add new Description-tab tests.

**Interfaces:**
- Consumes: `tabConversation`/`tabReviews`/`tabChecks`/`tabDiff` from Task 1; `preview.Render`, `renderDiscussionColumn`, `dimStyle`, `gh.PR`.
- Produces: constant `tabDescription = 0` (all others shift +1); `func renderDescription(pr gh.PR, w int) string`.

- [ ] **Step 1: Write the failing tests for the Description tab**

Add to `internal/ui/expanded_test.go`:

```go
func TestRenderDescriptionShowsBody(t *testing.T) {
	pr := gh.PR{Number: 7, Body: "## Summary\n\nRefactors the fetch path."}
	out := ansi.Strip(renderDescription(pr, 80))
	if !strings.Contains(out, "Summary") || !strings.Contains(out, "Refactors the fetch path") {
		t.Fatalf("Description tab should render the body:\n%s", out)
	}
}

func TestRenderDescriptionEmptyBody(t *testing.T) {
	out := ansi.Strip(renderDescription(gh.PR{Number: 7}, 80))
	if !strings.Contains(out, "No description provided") {
		t.Fatalf("empty body should show a placeholder:\n%s", out)
	}
}

func TestDescriptionIsDefaultLandingTab(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.width, m.height = 120, 30
	m.setPRs([]gh.PR{{Number: 7, Title: "hi", Body: "hello world"}})
	// No m.detail[7]: detail uncached, so no triage jump overrides the default.
	m.enterExpanded()
	if m.expandedTab != tabDescription {
		t.Fatalf("focus should land on the Description tab, got %d", m.expandedTab)
	}
	if !strings.Contains(ansi.Strip(m.expandedView()), "hello world") {
		t.Fatalf("Description tab should render the body from list data before detail loads")
	}
}
```

Note: `ansi.Strip` is already imported in `expanded_test.go` (used by `TestTabSegmentMarksActive`).

- [ ] **Step 2: Run the new tests to verify they fail**

Run: `cd internal/ui && go test ./... -run 'RenderDescription|DescriptionIsDefaultLandingTab'`
Expected: FAIL — `renderDescription` undefined, `tabDescription` undefined.

- [ ] **Step 3: Renumber the tab constants and add Description to the tab list**

In `internal/ui/expanded.go`, change the constant block to:

```go
const (
	tabDescription = iota
	tabConversation
	tabReviews
	tabChecks
	tabDiff
)
```

Change the tab list:

```go
var expandedTabs = []string{"Description", "Conversation", "Reviews", "Checks", "Diff"}
```

In `enterExpanded`, change the base landing tab from `tabConversation` (set in
Task 1) to `tabDescription`, so an uncached focus lands on Description:

```go
	m.expandedTab = tabDescription
```

(The triage `jumpTabIndex` override below it still deep-links reviews/checks/
conversation when detail is cached — see Step 4.)

- [ ] **Step 4: Handle the Description tab in `jumpTabIndex`**

Add the explicit `"conversation"` case (triage emits it) and make the default land on Description:

```go
func jumpTabIndex(jump string) int {
	switch jump {
	case "conversation":
		return tabConversation
	case "reviews":
		return tabReviews
	case "checks":
		return tabChecks
	case "diff":
		return tabDiff
	default:
		return tabDescription
	}
}
```

- [ ] **Step 5: Render the Description tab from list data, before the detail gate**

Add the `renderDescription` helper (place it near `renderReviews`):

```go
// renderDescription renders the PR body as the Description tab: the full markdown
// in the reading column. Empty bodies get a dim placeholder.
func renderDescription(pr gh.PR, w int) string {
	if strings.TrimSpace(pr.Body) == "" {
		return dimStyle.Render("  No description provided.")
	}
	return renderDiscussionColumn(w, func(cw int) string {
		body, err := preview.Render(pr.Body, cw)
		if err != nil {
			body = pr.Body
		}
		return strings.TrimRight(body, "\n")
	})
}
```

In `expandedBody`, handle Description before the `!cached` detail gate (so it renders instantly), and update the switch:

```go
func (m Model) expandedBody(w int) string {
	v, ok := m.cursorVars()
	if !ok {
		return ""
	}
	if m.expandedTab == tabDescription {
		if ps, ok := m.section.(*PRSection); ok {
			return renderDescription(ps.prAt(m.cursor), w)
		}
		return ""
	}
	d, cached := m.detail[v.Number]
	if !cached {
		return dimStyle.Render("  Loading…")
	}
	switch m.expandedTab {
	case tabReviews:
		return renderDiscussionColumn(w, func(contentWidth int) string {
			return renderReviews(d, contentWidth)
		})
	case tabChecks:
		if ps, ok := m.section.(*PRSection); ok {
			return renderChecks(ps.prAt(m.cursor), w, m.checkCursor)
		}
		return ""
	case tabDiff:
		return renderDiffstat(d, w)
	default:
		items := preview.Timeline(d)
		return renderDiscussionColumn(w, func(contentWidth int) string {
			return renderTimeline(items, len(items), contentWidth, true)
		})
	}
}
```

- [ ] **Step 6: Extend the number-key handler to five tabs**

In `updateExpanded`, change the number-key case so `5` selects the Diff tab:

```go
	case "1", "2", "3", "4", "5":
		m.expandedTab = int(msg.String()[0] - '1')
		m.checkCursor = 0
		m.renderExpanded()
		return m, nil
```

- [ ] **Step 7: Update the index-dependent existing tests**

In `internal/ui/expanded_test.go`:

`TestJumpTabIndex` — replace the cases map:

```go
	cases := map[string]int{
		"conversation": tabConversation,
		"reviews":      tabReviews,
		"checks":       tabChecks,
		"diff":         tabDiff,
		"":             tabDescription,
	}
```

`TestEnterExpandedDeepLinks` — the checks-failing card deep-links to the Checks tab:

```go
	if m.expandedTab != tabChecks {
		t.Fatalf("deep-link to Checks tab expected (%d), got %d", tabChecks, m.expandedTab)
	}
```

`TestChecksTabCursorNavigates` — same precondition:

```go
	m.enterExpanded() // deep-links to the Checks tab
	if m.expandedTab != tabChecks {
		t.Fatalf("precondition: expected Checks tab, got %d", m.expandedTab)
	}
```

`TestTabSegmentMarksActive` — mark the Checks tab by constant:

```go
	out := tabSegment(expandedTabs, tabChecks)
```

`TestExpandedBoxWidthCapsEveryTab` — cover five tabs:

```go
	for _, tab := range []int{0, 1, 2, 3, 4} { // Description, Conversation, Reviews, Checks, Diff
```

`TestExpandedFooterNeverOffersPan` — cover five tabs:

```go
	for _, tab := range []int{0, 1, 2, 3, 4} {
```

`TestConversationOpensAtMostRecent` — the fallback PR deep-links to Conversation (triage emits `"conversation"`), which is no longer index 0:

```go
	m.enterExpanded()
	if m.expandedTab != tabConversation {
		t.Fatalf("precondition: Conversation tab expected, got %d", m.expandedTab)
	}
```

- [ ] **Step 8: Run the ui package tests**

Run: `cd internal/ui && go test ./...`
Expected: PASS — new Description tests green, all reindexed tests green.

- [ ] **Step 9: Commit**

```bash
git add internal/ui/expanded.go internal/ui/expanded_test.go
git commit -m "feat(ui): add Description tab to the expanded PR view (#38)"
```

---

## Task 3: Preview-pane description section (authorship-sized)

Add a `description` section to the side preview pane, under the identity header
and before the blocker card. Collapsed for the viewer's own PRs, fuller for
others', omitted when empty.

**Files:**
- Modify: `internal/ui/preview.go` (`previewPane`, insert after `blocks = append(blocks, identityHeader(pr))` at line 236); add `previewDescriptionBody` + line-cap constants.
- Modify: `internal/ui/preview_test.go` (new tests).

**Interfaces:**
- Consumes: `preview.Render`, `dimStyle`, `m.viewerLogin`, the `section(label, body)` closure inside `previewPane`.
- Produces: `func previewDescriptionBody(pr gh.PR, viewer string, w int) string`; constants `descLinesOwn = 2`, `descLinesOthers = 6`.

- [ ] **Step 1: Write the failing tests for the preview description**

Add to `internal/ui/preview_test.go` (the `ansi` regexp helper pattern is already used by neighboring tests):

Use a bullet list so glamour renders one line per item (single-newline prose
would reflow into one paragraph). Assert relative sizing + the hint, not exact
line counts — glamour's surrounding whitespace makes exact counts brittle.

```go
func TestPreviewDescriptionCollapsesForOwnPR(t *testing.T) {
	body := "- L1\n- L2\n- L3\n- L4\n- L5\n- L6\n- L7\n- L8\n- L9\n- L10"
	mk := func(login string) gh.PR {
		p := gh.PR{Body: body}
		p.Author.Login = login
		return p
	}
	own := previewDescriptionBody(mk("me"), "me", 60)
	others := previewDescriptionBody(mk("them"), "me", 60)
	if !strings.Contains(own, "full text in Description tab") {
		t.Fatalf("truncated own PR should hint at the Description tab:\n%s", own)
	}
	ownLines := strings.Count(own, "\n")
	otherLines := strings.Count(others, "\n")
	if ownLines >= otherLines {
		t.Fatalf("own PR (%d lines) should collapse smaller than others (%d):\nown=%q\nothers=%q",
			ownLines, otherLines, own, others)
	}
}

func TestPreviewDescriptionEmptyOmitted(t *testing.T) {
	if got := previewDescriptionBody(gh.PR{Body: ""}, "me", 60); got != "" {
		t.Fatalf("empty body should yield no section, got %q", got)
	}
}

func TestPreviewPaneShowsDescriptionSection(t *testing.T) {
	ansiRe := regexp.MustCompile("\x1b\\[[0-9;]*m")
	m := NewModel("/repo", "is:open", nil)
	m.width, m.height = 150, 40
	m.viewerLogin = "me"
	p := gh.PR{Number: 1, Title: "x", Body: "does a cool thing"}
	p.Author.Login = "them"
	m.setPRs([]gh.PR{p})
	m.renderList()
	out := ansiRe.ReplaceAllString(m.previewPane(), "")
	if !strings.Contains(out, "DESCRIPTION") || !strings.Contains(out, "does a cool thing") {
		t.Fatalf("preview should show the description section:\n%s", out)
	}
}
```

- [ ] **Step 2: Run the new tests to verify they fail**

Run: `cd internal/ui && go test ./... -run 'PreviewDescription|PreviewPaneShowsDescription'`
Expected: FAIL — `previewDescriptionBody`, `descLinesOwn`, `descLinesOthers` undefined.

- [ ] **Step 3: Add the `previewDescriptionBody` helper**

In `internal/ui/preview.go`, near `previewPane`, add:

```go
const (
	descLinesOwn    = 2 // your own PRs collapse tight — you wrote them
	descLinesOthers = 6 // others' PRs show enough to start reviewing
)

// previewDescriptionBody renders the PR body for the preview pane, capped by
// authorship. Empty bodies return "" so the caller omits the section entirely.
func previewDescriptionBody(pr gh.PR, viewer string, w int) string {
	if strings.TrimSpace(pr.Body) == "" {
		return ""
	}
	rendered, err := preview.Render(pr.Body, w)
	if err != nil {
		rendered = pr.Body
	}
	limit := descLinesOthers
	if viewer != "" && pr.Author.Login == viewer {
		limit = descLinesOwn
	}
	lines := strings.Split(strings.Trim(rendered, "\n"), "\n")
	if len(lines) <= limit {
		return strings.Join(lines, "\n")
	}
	return strings.Join(lines[:limit], "\n") + "\n" +
		dimStyle.Render("· full text in Description tab")
}
```

- [ ] **Step 4: Insert the section into `previewPane`**

In `previewPane`, immediately after `blocks = append(blocks, identityHeader(pr))` (line 236), add:

```go
		if body := previewDescriptionBody(pr, m.viewerLogin, bw); body != "" {
			blocks = append(blocks, section("description", body))
		}
```

- [ ] **Step 5: Run the ui package tests**

Run: `cd internal/ui && go test ./...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/ui/preview.go internal/ui/preview_test.go
git commit -m "feat(ui): show PR description in the preview pane (#38)"
```

---

## Final verification

- [ ] **Run the full test suite and vet**

Run: `go test ./... && go vet ./...`
Expected: PASS across all packages.

- [ ] **Manual smoke check (optional)**

Run: `go run . ` in a repo with open PRs; confirm: the preview pane shows a
`DESCRIPTION` section (short for your own PRs, longer for others'); focusing a
PR lands on the Description tab when detail is still loading; the tab strip
reads `Description · Conversation · Reviews · Checks · Diff`; number keys `1`–`5`
select tabs; Checks-tab rerun/log/cursor keys still work.
