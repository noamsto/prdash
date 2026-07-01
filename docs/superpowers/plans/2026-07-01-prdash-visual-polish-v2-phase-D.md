# prdash visual polish v2 — Phase D (overlays + interaction) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax.

**Goal:** Finish the sweep's interaction layer: a floating action menu, a `?` legend overlay, side-preview scrolling, `h`-to-exit in the expanded view, and a bordered bottom bar.

**Architecture:** Overlays are centered modals on a cleared frame via `lipgloss.Place(width, height, Center, Center, panel)` (verified: exact w×h, panel centered — the reliable path vs low-level Canvas/Layer whose `Draw` ignores x/y). The panel is a `titledBox` (Phase C). Preview scrolling is a line `previewOffset` applied before the box clips, reset when the focused PR changes.

**Tech Stack:** Go, `charm.land/lipgloss/v2` v2.0.4, `charm.land/bubbletea/v2`, table-driven `testing`.

## Global Constraints

- Overlays render as **centered modals** via `lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, panel)`; the panel is a mauve-bordered `titledBox`.
- Preview scroll keys are **`ctrl+j` / `ctrl+k`** (down/up). Safe: if the terminal delivers `ctrl+j` as Enter, the `"ctrl+j"` case simply won't match and Enter keeps working.
- `previewOffset` **resets to 0 whenever the focused PR changes** (cursor move, refetch).
- Scope note: the §6 **persistent column-header row is dropped** — the `?` legend decodes the glyphs; a 2-char header over 1-char glyph columns reads cluttered.
- Match existing test style. Commit with `PRE_COMMIT_ALLOW_NO_CONFIG=1 git commit …`. gopls `undefined`/`lipgloss.Color is not a type`/`use of internal package` warnings are workspace artifacts — trust `go build`/`go test`. Run `go test ./...`, `go vet ./...`, `nix build`; commit after each task.

---

### Task 1: `h`/`←` exits the expanded view on the first tab

**Files:**
- Modify: `internal/ui/expanded.go` (`updateExpanded`)
- Test: `internal/ui/expanded_test.go`

**Interfaces:**
- Consumes: `updateExpanded(msg tea.KeyMsg)`, `m.expanded`, `m.expandedTab`.
- Produces: on the leftmost tab (index 0), `h`/`left` exits expanded instead of wrapping to the last tab.

- [ ] **Step 1: Write the failing test**

Add to `internal/ui/expanded_test.go`:

```go
func TestExpandedLeftOnFirstTabExits(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.setPRs([]gh.PR{{Number: 1, Title: "x"}})
	m.width, m.height = 120, 40
	m.expanded = true
	m.expandedTab = 0

	u, _ := m.Update(tea.KeyPressMsg{Code: 'h', Text: "h"})
	m = u.(Model)
	if m.expanded {
		t.Fatal("h on the first tab should exit the expanded view")
	}
}

func TestExpandedLeftOnLaterTabMovesLeft(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.setPRs([]gh.PR{{Number: 1, Title: "x"}})
	m.width, m.height = 120, 40
	m.expanded = true
	m.expandedTab = 2

	u, _ := m.Update(tea.KeyPressMsg{Code: 'h', Text: "h"})
	m = u.(Model)
	if !m.expanded || m.expandedTab != 1 {
		t.Fatalf("h on a later tab should move left; expanded=%v tab=%d", m.expanded, m.expandedTab)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/ -run 'TestExpandedLeftOn' -v`
Expected: `TestExpandedLeftOnFirstTabExits` FAILs (h wraps to last tab, stays expanded).

- [ ] **Step 3: Write minimal implementation**

In `updateExpanded` (`expanded.go`), split the left/back handling out of the shared tab-cycle case. Replace the `case "shift+tab", "left", "h":` block with:

```go
	case "left", "h":
		if m.expandedTab == 0 {
			m.expanded = false
			m.renderList()
			return m, nil
		}
		m.expandedTab--
		m.renderExpanded()
		return m, nil
	case "shift+tab":
		m.expandedTab = (m.expandedTab + len(expandedTabs) - 1) % len(expandedTabs)
		m.renderExpanded()
		return m, nil
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/ui/ -run 'TestExpanded' -v`
Expected: PASS (new tests + existing expanded tests).

- [ ] **Step 5: Commit**

```bash
git add internal/ui/expanded.go internal/ui/expanded_test.go
git commit -m "feat(ui): h/left exits expanded view from the first tab"
```

---

### Task 2: Side-preview scrolling (`ctrl+j` / `ctrl+k`)

**Files:**
- Modify: `internal/ui/prlist.go` (`Model.previewOffset`; reset in `moveCursor`; `ctrl+j`/`ctrl+k` keys)
- Modify: `internal/ui/preview.go` (`renderMain` drops `previewOffset` lines before the box) + `dropLines` in `box.go`
- Test: `internal/ui/preview_test.go`

**Interfaces:**
- Produces: `Model.previewOffset int`; `func dropLines(s string, n int) string`; `func (m *Model) previewScrollBy(delta int)`.

- [ ] **Step 1: Write the failing test**

Add to `internal/ui/box_test.go`:

```go
func TestDropLines(t *testing.T) {
	if got := dropLines("a\nb\nc\nd", 2); got != "c\nd" {
		t.Fatalf("dropLines = %q, want %q", got, "c\nd")
	}
	if got := dropLines("a\nb", 5); got != "" {
		t.Fatalf("dropping more than present should empty: %q", got)
	}
}
```

Add to `internal/ui/preview_test.go`:

```go
func TestPreviewScrollClampsAndResets(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.SetRepo("r")
	m.width, m.height = 150, 40
	p := gh.PR{Number: 1, Title: "x"}
	p.Author.Login = "a"
	m.setPRs([]gh.PR{p})
	m.detail[1] = gh.PRDetail{MergeStateStatus: "CLEAN"}
	m.renderList()

	m.previewScrollBy(-5) // can't scroll above the top
	if m.previewOffset != 0 {
		t.Fatalf("scroll up at top should clamp to 0, got %d", m.previewOffset)
	}
	m.previewScrollBy(3)
	if m.previewOffset != 3 {
		t.Fatalf("scroll down should advance the offset, got %d", m.previewOffset)
	}
	m.moveCursor(0) // focus change resets the preview scroll
	if m.previewOffset != 0 {
		t.Fatalf("moving the cursor should reset preview scroll, got %d", m.previewOffset)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/ -run 'TestDropLines|TestPreviewScrollClampsAndResets' -v`
Expected: FAIL — `dropLines` / `previewScrollBy` / `previewOffset` undefined.

- [ ] **Step 3: Write minimal implementation**

Add `dropLines` to `box.go` (next to `clipLines`):

```go
// dropLines removes the first n lines of s (for scrolling).
func dropLines(s string, n int) string {
	if n <= 0 {
		return s
	}
	lines := strings.Split(s, "\n")
	if n >= len(lines) {
		return ""
	}
	return strings.Join(lines[n:], "\n")
}
```

Add `previewOffset int` to the `Model` struct (near `cursorLine`):

```go
	cursorLine      int  // display-line offset of the cursor row (headers shift it)
	previewOffset   int  // ctrl+j/k scroll position within the side preview
```

Add the scroll method (near `scrollToCursor`):

```go
// previewScrollBy scrolls the side preview by delta lines, clamped so the last
// line can't scroll above the top of the pane.
func (m *Model) previewScrollBy(delta int) {
	l := computeLayout(m.width, m.height)
	visible := l.ContentHeight - 2 // inside the pane border
	over := lipgloss.Height(m.previewPane()) - visible
	m.previewOffset += delta
	if m.previewOffset > over {
		m.previewOffset = over
	}
	if m.previewOffset < 0 {
		m.previewOffset = 0
	}
}
```

Reset it on focus change — in `moveCursor`, after clamping the cursor (before `m.renderList()`):

```go
	m.previewOffset = 0
	m.renderList()
```

Wire the keys in the list-view key switch (near the `"z"` / `"D"` cases):

```go
		case "ctrl+j":
			m.previewScrollBy(1)
			return m, nil
		case "ctrl+k":
			m.previewScrollBy(-1)
			return m, nil
```

Apply the offset in `renderMain` (`preview.go`) — scroll before the box clips. Replace each `m.previewPane()` argument to `titledBox` with `dropLines(m.previewPane(), m.previewOffset)`:

```go
	if m.previewMax && l.ShowSide {
		return titledBox(dropLines(m.previewPane(), m.previewOffset), m.width, l.ContentHeight, m.previewTitle())
	}
	list := titledBox(m.vp.View(), l.ListWidth, l.ContentHeight, m.listTitle())
	if !l.ShowSide {
		return list
	}
	side := titledBox(dropLines(m.previewPane(), m.previewOffset), l.SideWidth, l.ContentHeight, m.previewTitle())
	side = lipgloss.NewStyle().MarginLeft(l.Gap).Render(side)
	return lipgloss.JoinHorizontal(lipgloss.Top, list, side)
```

`lipgloss` is already imported in both files.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/ui/ -v`
Expected: PASS (full package). `previewScrollBy` clamps; `moveCursor` reset covered.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/box.go internal/ui/prlist.go internal/ui/preview.go internal/ui/box_test.go internal/ui/preview_test.go
git commit -m "feat(ui): scroll the side preview with ctrl+j/ctrl+k"
```

---

### Task 3: `modal()` helper + floating action menu

**Files:**
- Modify: `internal/ui/box.go` (add `modal`)
- Modify: `internal/ui/prlist.go` (`render()` action-menu branch renders a centered modal)
- Test: `internal/ui/box_test.go`, `internal/ui/actionview_test.go` (or `prlist_test.go`)

**Interfaces:**
- Consumes: `titledBox`, `filterActions`, `m.actions`, `m.actionFilter`, `m.actionCursor`.
- Produces: `func modal(panel string, w, h int) string` — centers `panel` on a cleared `w×h` frame; the action menu renders as a bordered floating panel.

- [ ] **Step 1: Write the failing test**

Add to `internal/ui/box_test.go`:

```go
func TestModalCentersPanel(t *testing.T) {
	panel := titledBox("body", 12, 3, "Actions")
	out := modal(panel, 40, 11)
	lines := strings.Split(out, "\n")
	if len(lines) != 11 {
		t.Fatalf("modal height = %d, want 11", len(lines))
	}
	for i, ln := range lines {
		if lipgloss.Width(ln) != 40 {
			t.Fatalf("line %d width = %d, want 40", i, lipgloss.Width(ln))
		}
	}
	if !strings.Contains(out, "Actions") {
		t.Fatalf("modal should contain the panel: %q", out)
	}
}
```

Add to `internal/ui/prlist_test.go`:

```go
func TestActionMenuRendersAsFloatingModal(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.SetRepo("r")
	m.width, m.height = 120, 30
	m.setPRs([]gh.PR{{Number: 1, Title: "x"}})
	u, _ := m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	m = u.(Model)
	out := m.render()
	if !strings.Contains(out, "Actions") || !strings.Contains(out, "╭") {
		t.Fatalf("action menu should be a bordered floating panel titled Actions: %q", out)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/ -run 'TestModalCentersPanel|TestActionMenuRendersAsFloatingModal' -v`
Expected: FAIL — `modal` undefined; action menu is a plain list, not titled/bordered.

- [ ] **Step 3: Write minimal implementation**

Add `modal` to `box.go`:

```go
// modal centers panel on a cleared w×h frame — a floating dialog.
func modal(panel string, w, h int) string {
	return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, panel)
}
```

In `render()` (`prlist.go`), replace the `if m.showActions { … }` block with a centered bordered panel:

```go
	if m.showActions {
		acts := filterActions(m.actions, m.actionFilter.Value())
		var b strings.Builder
		b.WriteString(m.actionFilter.View() + "\n")
		for i, a := range acts {
			cursor := "  "
			line := fmt.Sprintf("%-6s %s", a.Key, a.Label)
			if i == m.actionCursor {
				cursor = accentStyle.Render("▸ ")
				line = accentStyle.Render(line)
			} else {
				line = statusBarStyle.Render(line)
			}
			b.WriteString(cursor + line + "\n")
		}
		panel := titledBox(strings.TrimRight(b.String(), "\n"), 40, len(acts)+3, "Actions")
		return modal(panel, m.width, m.height)
	}
```

(`fmt` and `strings` are already imported in `prlist.go`.)

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/ui/ -run 'TestModal|TestActionMenu|TestActionsFilter' -v`
Expected: PASS. (Existing action-filter behavior is unchanged — only the rendering is wrapped.)

- [ ] **Step 5: Commit**

```bash
git add internal/ui/box.go internal/ui/prlist.go internal/ui/box_test.go internal/ui/prlist_test.go
git commit -m "feat(ui): float the action menu as a centered bordered modal"
```

---

### Task 4: `?` legend overlay

**Files:**
- Modify: `internal/ui/prlist.go` (`showLegend` field; `?` toggles it; `render()` shows the legend modal)
- Test: `internal/ui/prlist_test.go`

**Interfaces:**
- Produces: `Model.showLegend bool`; a `?`-toggled centered modal listing the board glyphs and keys.

- [ ] **Step 1: Write the failing test**

Add to `internal/ui/prlist_test.go`:

```go
func TestLegendToggle(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.SetRepo("r")
	m.width, m.height = 120, 30
	m.setPRs([]gh.PR{{Number: 1, Title: "x"}})

	u, _ := m.Update(tea.KeyPressMsg{Code: '?', Text: "?"})
	m = u.(Model)
	if !m.showLegend {
		t.Fatal("? should open the legend")
	}
	out := m.render()
	if !strings.Contains(out, "Legend") || !strings.Contains(out, "conflict") {
		t.Fatalf("legend should explain the glyphs: %q", out)
	}
	u, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape}) // any key dismisses
	m = u.(Model)
	if m.showLegend {
		t.Fatal("a key should close the legend")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/ -run TestLegendToggle -v`
Expected: FAIL — no legend; `showLegend` undefined.

- [ ] **Step 3: Write minimal implementation**

Add `showLegend bool` to `Model` (near `showActions`).

Add a `legendView` method (near `statusBar` in `prlist.go`):

```go
// legendView is the ?-toggled glyph + key reference, as a centered modal.
func (m Model) legendView() string {
	rows := []string{
		accentStyle.Render("CI / review") + statusBarStyle.Render("  ✓ pass   ✗ fail   ● running   · none"),
		accentStyle.Render("!") + statusBarStyle.Render("           ⚠ conflict / behind base"),
		accentStyle.Render("row") + statusBarStyle.Render("         ▎ focus   ● selected   [draft] dimmed"),
		"",
		accentStyle.Render("↵") + statusBarStyle.Render(" worktree   ") + accentStyle.Render("y") + statusBarStyle.Render(" copy   ") + accentStyle.Render("o") + statusBarStyle.Render(" open   ") + accentStyle.Render("a") + statusBarStyle.Render(" actions"),
		accentStyle.Render("f") + statusBarStyle.Render(" filter   ") + accentStyle.Render("F") + statusBarStyle.Render(" author   ") + accentStyle.Render("R") + statusBarStyle.Render(" reviewers   ") + accentStyle.Render("D") + statusBarStyle.Render(" drafts"),
		accentStyle.Render("ctrl+j/k") + statusBarStyle.Render(" scroll preview   ") + accentStyle.Render("z") + statusBarStyle.Render(" maximize   ") + accentStyle.Render("esc") + statusBarStyle.Render(" close"),
	}
	body := strings.Join(rows, "\n")
	panel := titledBox(body, lipgloss.Width(body)+4, len(rows)+2, "Legend")
	return modal(panel, m.width, m.height)
}
```

In `render()`, add the legend branch (before the normal return, alongside `showActions`):

```go
	if m.showLegend {
		return m.legendView()
	}
```

Wire the `?` key and its `esc` in the list-view key switch. Add:

```go
		case "?":
			m.showLegend = true
			return m, nil
```

And handle closing — at the top of the `tea.KeyMsg` block, before the main switch, add a guard:

```go
			if m.showLegend {
				m.showLegend = false // any key dismisses the legend
				return m, nil
			}
```

(Place this guard after the `m.expanded` / `m.pending` / `m.filtering` / `m.showPicker` / `m.showActions` guards, so those modes keep priority.)

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/ui/ -run 'TestLegend' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/prlist.go internal/ui/prlist_test.go
git commit -m "feat(ui): ? opens a floating glyph/key legend"
```

---

### Task 5: Bordered bottom bar

**Files:**
- Modify: `internal/ui/prlist.go` (`statusBar` wraps its keys in a top-ruled bar)
- Test: `internal/ui/prlist_test.go`

**Interfaces:**
- Produces: `statusBar` returns the key line under a thin top rule spanning the width, so it reads as a distinct footer.

- [ ] **Step 1: Write the failing test**

Add to `internal/ui/prlist_test.go`:

```go
func TestStatusBarHasTopRule(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.SetRepo("r")
	m.width, m.height = 120, 30
	m.setPRs([]gh.PR{{Number: 1, Title: "x"}})
	if !strings.Contains(m.statusBar(), "─") {
		t.Fatalf("status bar should have a top rule separating it: %q", m.statusBar())
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/ -run TestStatusBarHasTopRule -v`
Expected: FAIL — no rule in the bar.

- [ ] **Step 3: Write minimal implementation**

In `statusBar` (`prlist.go`), prefix the key line with a full-width rule. Change the final `return`:

```go
	rule := sepStyle.Render(strings.Repeat("─", max(m.width, 1)))
	return rule + "\n  " + strings.Join(parts, "  ")
```

(A full box would cost two extra rows; a single top rule delimits the footer at one row cost. `sepStyle`/`max` are already in the package.)

The bar is now **2 rows** (rule + keys), so the chrome budget grows by one. In `layout.go`, bump `chromeRows`:

```go
// chromeRows is the vertical space taken by the header + spacer + status bar
// (rule + keys = 2 rows).
const chromeRows = 4
```

Without this the bottom row overflows the terminal. `computeLayout`'s tests key off the returned `ContentHeight = h - chromeRows`; update any that assert an exact `ContentHeight` for a given height.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/ui/ -v`
Expected: PASS (full package). Confirm `TestViewShowsHeaderAndStatus` still passes (the rule is added, keys remain).

> **Layout note:** the bar is now 2 rows (rule + keys). `chromeRows` (currently 3: header + spacer + status) must become 4 so the panes don't overflow the terminal. Update `layout.go`'s `chromeRows = 4` and confirm `computeLayout` tests still pass; add the rule row to the height budget.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/prlist.go internal/ui/layout.go internal/ui/prlist_test.go
git commit -m "feat(ui): delimit the status bar with a top rule"
```

---

### Task 6: Full verification + visual smoke

**Files:** none (verification task)

- [ ] **Step 1:** `go test ./... && go vet ./...` — all pass, no warnings.
- [ ] **Step 2:** `nix build` — clean.
- [ ] **Step 3: Manual smoke.** Run `./result/bin/prdash` wide. Confirm: `a` opens a centered bordered **Actions** modal (filter + list, cursor row accented), `esc` closes; `?` opens the **Legend** modal, any key closes; `ctrl+j`/`ctrl+k` scroll a long side preview (and it resets when you `j`/`k` to another PR); in the expanded view `h`/`←` on the Conversation tab exits to the board; the bottom bar sits under a thin rule. If `ctrl+j` does nothing in this terminal, note it — the fallback is `ctrl+d`/`ctrl+u`.
- [ ] **Step 4: No commit** (verification only). Phase D complete — the sweep is done.
