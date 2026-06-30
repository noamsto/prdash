# prdash visual polish v2 — Phase C (rounded panes + preview sectioning) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax.

**Goal:** Wrap the list and the side preview in titled rounded borders, and split the preview's wall-of-info into labeled subsections (identity · blocker · checks · review · latest).

**Architecture:** A `titledBox(content, w, h, title)` helper draws a rounded border with the title set into the top edge — built by rendering the body with left/right/bottom borders and prepending a manually-composed top line (lipgloss v2 has no native border title). The list viewport and the preview pane are sized to the box *interior* and wrapped by `renderMain`. Preview subsections are dim labeled rules between the existing content blocks.

**Tech Stack:** Go, `charm.land/lipgloss/v2` v2.0.4, table-driven `testing`.

## Global Constraints

- **lipgloss v2 `Width`/`Height` are OUTER (total) dimensions** including the border (verified: `Width(20)` yields 18 cells of content between borders). Interior content area for a `titledBox(w, h)` is therefore `(w-2) × (h-2)`.
- Borders use **`theme.Rule`** (surface2 `#585b70`); pane titles use **`accentStyle`** (mauve).
- The list keeps its single-line rows; bordering must not change row content, only shrink the usable width to `ListWidth-2`. The cursor scroll math (`m.cursorLine`, Phase B) stays correct because the viewport still holds rows+headers — only its width/height shrink.
- Long preview content must **clip inside the border**, never overflow it (today's `MaxWidth/MaxHeight` guard becomes per-box).
- Two-pane split keeps `computeLayout`'s `Gap` (2 cols) between the boxes.
- Match existing test style. Commit with `PRE_COMMIT_ALLOW_NO_CONFIG=1 git commit …`. gopls `undefined`/`lipgloss.Color is not a type` warnings are workspace artifacts — trust `go build`/`go test`. Run `go test ./...`, `go vet ./...`, `nix build`; commit after each task.

---

### Task 1: `titledBox` + `clipLines` helpers

**Files:**
- Create: `internal/ui/box.go`
- Test: `internal/ui/box_test.go`

**Interfaces:**
- Produces: `func titledBox(content string, w, h int, title string) string` — a rounded box of outer size `w × h` with `title` in the top border; `func clipLines(s string, n int) string` — keep at most the first `n` lines.

- [ ] **Step 1: Write the failing test**

Create `internal/ui/box_test.go`:

```go
package ui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

func TestTitledBoxDimensionsAndTitle(t *testing.T) {
	const w, h = 30, 6
	box := titledBox("alpha\nbeta", w, h, "PRs · 12")
	lines := strings.Split(box, "\n")
	if len(lines) != h {
		t.Fatalf("box height = %d lines, want %d", len(lines), h)
	}
	for i, ln := range lines {
		if got := lipgloss.Width(ln); got != w {
			t.Fatalf("line %d width = %d, want %d (%q)", i, got, w, ln)
		}
	}
	if !strings.Contains(box, "PRs · 12") {
		t.Fatalf("box should carry its title: %q", box)
	}
	if !strings.Contains(lines[0], "╭") || !strings.Contains(lines[0], "╮") {
		t.Fatalf("top line should have rounded corners: %q", lines[0])
	}
	if !strings.Contains(lines[h-1], "╰") || !strings.Contains(lines[h-1], "╯") {
		t.Fatalf("bottom line should have rounded corners: %q", lines[h-1])
	}
}

func TestTitledBoxClipsOverflow(t *testing.T) {
	tall := strings.Repeat("x\n", 20)
	box := titledBox(tall, 12, 5, "t")
	if got := len(strings.Split(box, "\n")); got != 5 {
		t.Fatalf("overflowing content must clip to the box height; got %d lines, want 5", got)
	}
}

func TestClipLines(t *testing.T) {
	if got := clipLines("a\nb\nc\nd", 2); got != "a\nb" {
		t.Fatalf("clipLines = %q, want %q", got, "a\nb")
	}
	if got := clipLines("a\nb", 5); got != "a\nb" { // fewer lines than the cap is untouched
		t.Fatalf("clipLines = %q, want %q", got, "a\nb")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/ -run 'TestTitledBox|TestClipLines' -v`
Expected: FAIL — `undefined: titledBox` / `clipLines`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/ui/box.go`:

```go
package ui

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// clipLines keeps at most the first n lines of s.
func clipLines(s string, n int) string {
	if n < 0 {
		n = 0
	}
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[:n]
	}
	return strings.Join(lines, "\n")
}

// titledBox wraps content in a rounded border of OUTER size w × h, with title
// set into the top edge. lipgloss has no native border title, so the body is
// rendered with left/right/bottom borders only and a hand-built top line is
// prepended. Content is clipped to the interior so it never overflows the box.
func titledBox(content string, w, h int, title string) string {
	if w < 4 {
		w = 4
	}
	if h < 2 {
		h = 2
	}
	rb := lipgloss.RoundedBorder()
	rule := lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Rule))
	body := lipgloss.NewStyle().
		Border(rb, false, true, true, true).
		BorderForeground(lipgloss.Color(theme.Rule)).
		Width(w).Height(h - 1).MaxWidth(w).MaxHeight(h - 1).
		Render(clipLines(content, h-2))
	label := " " + truncate(title, w-4) + " "
	rest := w - 3 - lipgloss.Width(label)
	if rest < 0 {
		rest = 0
	}
	top := rule.Render(rb.TopLeft+rb.Top) +
		accentStyle.Render(label) +
		rule.Render(strings.Repeat(rb.Top, rest)+rb.TopRight)
	return top + "\n" + body
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/ui/ -run 'TestTitledBox|TestClipLines' -v`
Expected: PASS (all three).

- [ ] **Step 5: Commit**

```bash
git add internal/ui/box.go internal/ui/box_test.go
git commit -m "feat(ui): titledBox + clipLines pane helpers"
```

---

### Task 2: Border the list pane

**Files:**
- Modify: `internal/ui/prlist.go` (`renderList` sizes the viewport to the box interior; add `listTitle`; `renderMain` wraps the list — but `renderMain` lives in `preview.go`, edited in Task 3, so this task only changes `renderList` + adds `listTitle`)
- Test: `internal/ui/prlist_test.go`

**Interfaces:**
- Consumes: `computeLayout`, `titledBox`.
- Produces: `func (m Model) listTitle() string` (`"PRs · N"` / `"Issues · N"`); `renderList` now renders rows and sizes `m.vp` to `(ListWidth-2) × (ContentHeight-2)`.

- [ ] **Step 1: Write the failing test**

Add to `internal/ui/prlist_test.go`:

```go
func TestListTitleReflectsSection(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.SetRepo("r")
	m.setPRs([]gh.PR{{Number: 1}, {Number: 2}})
	if got := m.listTitle(); got != "PRs · 2" {
		t.Fatalf("listTitle = %q, want %q", got, "PRs · 2")
	}
}

func TestListViewportSizedForBorder(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.SetRepo("r")
	m.width, m.height = 100, 30 // narrow (<120): single list pane, width 100
	m.setPRs([]gh.PR{{Number: 1, Title: "x"}})
	m.renderList()
	l := computeLayout(100, 30)
	if got := m.vp.Width(); got != l.ListWidth-2 {
		t.Fatalf("viewport width = %d, want ListWidth-2 = %d", got, l.ListWidth-2)
	}
	if got := m.vp.Height(); got != l.ContentHeight-2 {
		t.Fatalf("viewport height = %d, want ContentHeight-2 = %d", got, l.ContentHeight-2)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/ -run 'TestListTitle|TestListViewportSizedForBorder' -v`
Expected: FAIL — `listTitle` undefined; viewport sized to full `ListWidth`/`ContentHeight`.

- [ ] **Step 3: Write minimal implementation**

In `renderList` (`prlist.go`), compute the interior size and use it for rows, the group header, and the viewport. Replace the body from `listW := l.ListWidth` through the `m.vp.SetHeight(...)` line:

```go
	innerW := l.ListWidth - 2  // inside the pane's left/right border
	innerH := l.ContentHeight - 2
	if innerW < 1 {
		innerW = 1
	}
	if innerH < 1 {
		innerH = 1
	}
	numW := columnWidths(m.section)
	ps, isPR := m.section.(*PRSection)
	grouped := isPR && ps.grouped
	var b strings.Builder
	line, prevAuthor := 0, ""
	for i := 0; i < m.section.Len(); i++ {
		if grouped {
			if a := ps.prAt(i).Author.Login; a != prevAuthor {
				b.WriteString(groupHeader(a, innerW) + "\n")
				line++
				prevAuthor = a
			}
		}
		if i == m.cursor {
			m.cursorLine = line
		}
		flag := ""
		if isPR {
			d, cached := m.detail[ps.prAt(i).Number]
			flag = flagGlyph(d, cached)
		}
		b.WriteString(m.section.RenderRow(i, RowOpts{
			Width: innerW, NumWidth: numW, Focused: i == m.cursor, Selected: m.sel.has(i), Flag: flag,
		}))
		b.WriteString("\n")
		line++
	}
	if m.section.Len() == 0 {
		m.cursorLine = 0
	}
	m.vp.SetWidth(innerW)
	m.vp.SetHeight(innerH)
	m.vp.SetContent(b.String())
	m.scrollToCursor()
}
```

Add `listTitle` near `header`:

```go
// listTitle is the list pane's border title: the section kind + shown count.
func (m Model) listTitle() string {
	if m.section.Kind() == "issue" {
		return fmt.Sprintf("Issues · %d", m.section.Len())
	}
	return fmt.Sprintf("PRs · %d", m.section.Len())
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/ui/ -run 'TestListTitle|TestListViewportSizedForBorder|TestGroupedRender|TestMineViewRendersFlat' -v`
Expected: PASS. (Grouped/flat render tests still pass — they assert on `m.vp.View()` content, which is unaffected by the width shrink beyond wrapping.)

- [ ] **Step 5: Commit**

```bash
git add internal/ui/prlist.go internal/ui/prlist_test.go
git commit -m "feat(ui): size list viewport for its border; add listTitle"
```

---

### Task 3: Wrap both panes in `renderMain`

**Files:**
- Modify: `internal/ui/preview.go` (`renderMain` wraps list + side in `titledBox`; `previewWidth` subtracts the border; add `previewTitle`)
- Test: `internal/ui/preview_test.go`

**Interfaces:**
- Consumes: `titledBox`, `m.listTitle`, `computeLayout`, `m.vp.View`, `m.previewPane`.
- Produces: `func (m Model) previewTitle() string` (`"#NNN"` of the focused PR, else `"Preview"`); `renderMain` returns bordered panes; `previewWidth` returns the box interior width.

- [ ] **Step 1: Write the failing test**

Add to `internal/ui/preview_test.go`:

```go
func TestRenderMainBordersListPane(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.SetRepo("r")
	m.width, m.height = 100, 30 // narrow: single bordered list pane
	m.setPRs([]gh.PR{{Number: 1, Title: "x"}})
	m.renderList()
	out := m.renderMain()
	if !strings.Contains(out, "╭") || !strings.Contains(out, "╯") {
		t.Fatalf("renderMain should wrap the list in a rounded border: %q", out)
	}
	if !strings.Contains(out, "PRs · 1") {
		t.Fatalf("list pane should be titled: %q", out)
	}
}

func TestPreviewWidthSubtractsBorder(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.width, m.height = 150, 40 // wide: side pane shows
	l := computeLayout(150, 40)
	if got := m.previewWidth(); got != l.SideWidth-2 {
		t.Fatalf("previewWidth = %d, want SideWidth-2 = %d", got, l.SideWidth-2)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/ -run 'TestRenderMainBordersListPane|TestPreviewWidthSubtractsBorder' -v`
Expected: FAIL — no border in output; `previewWidth` returns `SideWidth`.

- [ ] **Step 3: Write minimal implementation**

Rewrite `renderMain` (`preview.go`):

```go
// renderMain lays the bordered list and (when wide) the bordered side preview.
func (m Model) renderMain() string {
	l := computeLayout(m.width, m.height)
	if m.previewMax && l.ShowSide {
		return titledBox(m.previewPane(), m.width, l.ContentHeight, m.previewTitle())
	}
	list := titledBox(m.vp.View(), l.ListWidth, l.ContentHeight, m.listTitle())
	if !l.ShowSide {
		return list
	}
	side := titledBox(m.previewPane(), l.SideWidth, l.ContentHeight, m.previewTitle())
	side = lipgloss.NewStyle().MarginLeft(l.Gap).Render(side)
	return lipgloss.JoinHorizontal(lipgloss.Top, list, side)
}

// previewTitle is the side pane's border title.
func (m Model) previewTitle() string {
	if v, ok := m.cursorVars(); ok && v.Number > 0 {
		return fmt.Sprintf("#%d", v.Number)
	}
	return "Preview"
}
```

Adjust `previewWidth` so the preview content fits the box interior:

```go
func (m Model) previewWidth() int {
	l := computeLayout(m.width, m.height)
	if !l.ShowSide {
		return 40
	}
	if m.previewMax {
		return m.width - 2 // interior of the full-width box
	}
	return l.SideWidth - 2
}
```

(`fmt` is already imported in `preview.go`.)

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/ui/ -v`
Expected: PASS (full package). Watch `TestEmptyResultShowsEmptyStateNotLoading`, `TestViewShowsHeaderAndStatus`, and the layout tests — the border wrap must not break the empty/loading or header/status paths.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/preview.go internal/ui/preview_test.go
git commit -m "feat(ui): wrap list + preview panes in titled rounded borders"
```

---

### Task 4: Preview subsection rules

**Files:**
- Modify: `internal/ui/preview.go` (`previewPane` interleaves dim labeled rules; add `sectionRule`)
- Test: `internal/ui/preview_test.go`

**Interfaces:**
- Produces: `func sectionRule(label string, w int) string` — `label` + a dim rule filling width; `previewPane` groups its blocks under `blocker` / `checks` / `review` / `latest` rules.

- [ ] **Step 1: Write the failing test**

Add to `internal/ui/preview_test.go`:

```go
func TestSectionRule(t *testing.T) {
	r := sectionRule("blocker", 30)
	if !strings.Contains(r, "blocker") || !strings.Contains(r, "─") {
		t.Fatalf("section rule should show the label and a rule: %q", r)
	}
	if strings.Contains(r, "\n") {
		t.Fatalf("section rule is one line: %q", r)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/ -run TestSectionRule -v`
Expected: FAIL — `undefined: sectionRule`.

- [ ] **Step 3: Write minimal implementation**

Add `sectionRule` to `preview.go`:

```go
// sectionRule is a dim labeled divider inside the preview: "label ─────".
func sectionRule(label string, w int) string {
	rest := w - lipgloss.Width(label) - 1
	if rest < 0 {
		rest = 0
	}
	return dimStyle.Render(label) + " " + sepStyle.Render(strings.Repeat("─", rest))
}
```

Rewrite the body of `previewPane` (from `var parts []string` to the `return`) to interleave the rules:

```go
	w := m.previewWidth()
	var parts []string
	if ps, ok := m.section.(*PRSection); ok {
		pr := ps.prAt(m.cursor)
		parts = append(parts, identityHeader(pr))
		if card := renderCard(triage.Compute(pr, d), w); card != "" {
			parts = append(parts, sectionRule("blocker", w), strings.TrimRight(card, "\n"))
		}
		if ci := ciLine(pr); ci != "" {
			parts = append(parts, sectionRule("checks", w), ci)
		}
	}
	parts = append(parts, sectionRule("review", w), reviewersLine(d.ReviewRequests))
	timeline := renderTimeline(preview.Timeline(d), m.previewN, w, m.previewExpanded)
	return strings.Join(parts, "\n") + "\n" + sectionRule("latest", w) + "\n\n" + timeline
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/ui/ -v`
Expected: PASS (full package).

- [ ] **Step 5: Commit**

```bash
git add internal/ui/preview.go internal/ui/preview_test.go
git commit -m "feat(ui): section the preview with labeled rules"
```

---

### Task 5: Full verification + visual smoke

**Files:** none (verification task)

- [ ] **Step 1:** `go test ./... && go vet ./...` — all pass, no warnings.
- [ ] **Step 2:** `nix build` — clean (no `go.mod`/`go.sum` change).
- [ ] **Step 3: Manual smoke.** Run `./result/bin/prdash` on a **wide** terminal (≥120). Confirm: the list sits in a rounded `╭─ PRs · N ─╮` box and the preview in a `╭─ #NNN ─╮` box with a 2-col gap; `j/k` scrolls rows *inside* the list border (nothing leaks past it); the focused-row background still fills correctly within the border; the preview shows `blocker`/`checks`/`review`/`latest` dim rules and a long timeline clips at the bottom border instead of overflowing. Resize narrow (<120): single bordered list pane, no preview. Press `z`: preview maximizes in its own full-width box.
- [ ] **Step 4: No commit** (verification only). Phase C complete.

## Deferred to Phase D (signage + overlays)

§6's **column-header row** and the **`?` floating legend** move to Phase D, alongside the floating action menu (they share the centered-overlay rendering). Phase D also adds the bottom bar's rounded border and the fuller state-specific key set.
