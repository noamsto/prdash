# prdash Redesign — Phase A: Visual foundation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the prototype-looking UI with an airy, contained, responsive layout — a custom viewport-based list (no more `bubbles/table`), a theme/style module, a responsive layout calculator, a contained side preview, and a status/key bar — so the TUI looks professional and nothing overflows.

**Architecture:** The model stops embedding `bubbles/table` and instead owns a cursor index (`m.cursor`, indexing the *shown* set) plus a `bubbles/viewport` that scrolls a pre-rendered string. The `Section` interface swaps its column/row methods for a single `RenderRow` method, so the model+sections own all row layout (cursor highlight, `● ` selection marker, the airy 2-line form). A pure `layout.Compute(w,h)` decides pane geometry and whether the side pane shows. A `theme` module is the single source of styles/glyphs.

**Tech Stack:** Go 1.26, bubbletea v1, `bubbles/viewport`, `bubbles/textinput`, lipgloss. Spec: `docs/superpowers/specs/2026-06-23-prdash-tui-redesign-design.md`. Builds on merged Plans 1–4.

---

## Conventions

- **Worktree:** work on `feat/tui-redesign` (already created). Run Go as plain `go test ./...` / `go build ./...` from the worktree root (direnv provides the toolchain).
- **TDD:** write the failing test, watch it fail, implement, watch it pass, commit. One logical change per commit.
- After each task: `go build ./... && go vet ./... && gofmt -l .` must be clean.

## File structure (Phase A)

- `internal/ui/theme.go` — **new.** lipgloss styles (accent/dim/muted/state), state glyphs, the divider + status-bar styles. Single source of visual truth.
- `internal/ui/theme_test.go` — **new.**
- `internal/ui/layout.go` — **new.** `Layout` struct + `Compute(w, h int) Layout` (pure): list/side widths, `ShowSide`, content height.
- `internal/ui/layout_test.go` — **new.**
- `internal/ui/section.go` — **modify.** Replace `Columns()`/`Rows()` with `RenderRow(i int, o RowOpts) string` on the `Section` interface and both sections.
- `internal/ui/section_test.go` — **modify.** Replace the `Rows()` test with `RenderRow` tests.
- `internal/ui/prlist.go` — **modify (heaviest).** Drop `table.Model`; add `cursor int`, `vp viewport.Model`, `width/height int`; rebuild navigation, `applyFilter`, and `View`.
- `internal/ui/prlist_test.go` — **modify.** Re-point cursor/row assertions off `m.table`.
- `internal/ui/actions.go` — **modify.** `cursorVars()`/`runBulk` use `m.cursor`, not `m.table.Cursor()`.
- `internal/ui/preview.go` — **modify.** `detailCmdForCursor`/`previewWidth`/`tableWithPreview` use `m.cursor` + `layout`/`theme`; rename `tableWithPreview` → `renderMain`.

---

## Task 1: Theme module (styles + state glyphs)

**Files:**
- Create: `internal/ui/theme.go`, `internal/ui/theme_test.go`

- [ ] **Step 1: Write the failing test**

`internal/ui/theme_test.go`:

```go
package ui

import (
	"strings"
	"testing"
)

func TestCIGlyph(t *testing.T) {
	cases := map[string]string{"pass": "✓", "fail": "✗", "pending": "●", "none": "·"}
	for state, want := range cases {
		if got := ciGlyph(state); !strings.Contains(got, want) {
			t.Errorf("ciGlyph(%q) = %q, want it to contain %q", state, got, want)
		}
	}
}

func TestCIGlyphUnknownIsNone(t *testing.T) {
	if !strings.Contains(ciGlyph("whatever"), "·") {
		t.Errorf("unknown CI state should fall back to the none glyph")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/ -run TestCIGlyph`
Expected: FAIL — `undefined: ciGlyph`.

- [ ] **Step 3: Implement `internal/ui/theme.go`**

```go
package ui

import "github.com/charmbracelet/lipgloss"

// Palette roles. Concrete colors inherit the terminal's theme (lazytmux
// Catppuccin overlay); these adaptive defaults read well on dark backgrounds.
var (
	accentStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("39"))  // blue
	dimStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("241")) // gray
	mutedStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	passStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("78"))  // green
	failStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("203")) // red
	pendStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("214")) // yellow
	selMarkStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("141")) // mauve
	cursorRowStyle = lipgloss.NewStyle().Background(lipgloss.Color("236"))
	headerStyle  = accentStyle.Bold(true)
	statusBarStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
)

// ciGlyph maps a CIState() value to a colored single-rune glyph.
func ciGlyph(state string) string {
	switch state {
	case "pass":
		return passStyle.Render("✓")
	case "fail":
		return failStyle.Render("✗")
	case "pending":
		return pendStyle.Render("●")
	default: // "none" and anything unexpected
		return dimStyle.Render("·")
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/ui/ -run TestCIGlyph -v`
Expected: PASS (both tests).

- [ ] **Step 5: Commit**

```bash
git add internal/ui/theme.go internal/ui/theme_test.go
git commit -m "feat(ui): theme module — styles + CI state glyphs"
```

---

## Task 2: Responsive layout calculator (pure)

**Files:**
- Create: `internal/ui/layout.go`, `internal/ui/layout_test.go`

- [ ] **Step 1: Write the failing test**

`internal/ui/layout_test.go`:

```go
package ui

import "testing"

func TestLayoutWideShowsSide(t *testing.T) {
	l := computeLayout(160, 40)
	if !l.ShowSide {
		t.Fatal("wide terminal should show the side pane")
	}
	if l.ListWidth <= 0 || l.SideWidth <= 0 {
		t.Fatalf("both panes need positive width: %+v", l)
	}
	if l.ListWidth+l.SideWidth+l.Gap > 160 {
		t.Fatalf("panes (%d + gap %d + %d) exceed terminal width 160", l.ListWidth, l.Gap, l.SideWidth)
	}
}

func TestLayoutNarrowHidesSide(t *testing.T) {
	l := computeLayout(90, 40)
	if l.ShowSide {
		t.Fatal("narrow terminal should hide the side pane")
	}
	if l.ListWidth != 90 {
		t.Fatalf("list should take full width when side is hidden: got %d", l.ListWidth)
	}
}

func TestLayoutContentHeight(t *testing.T) {
	// total height minus header (1) + blank (1) + status bar (1) = 3 chrome rows.
	l := computeLayout(160, 40)
	if l.ContentHeight != 37 {
		t.Fatalf("ContentHeight = %d, want 37", l.ContentHeight)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/ -run TestLayout`
Expected: FAIL — `undefined: computeLayout`.

- [ ] **Step 3: Implement `internal/ui/layout.go`**

```go
package ui

// sideThreshold is the minimum terminal width at which the side preview shows.
const sideThreshold = 120

// chromeRows is the vertical space taken by the header + spacer + status bar.
const chromeRows = 3

// Layout is the computed geometry for one frame. Pure output of computeLayout.
type Layout struct {
	ShowSide      bool
	ListWidth     int
	SideWidth     int
	Gap           int // columns between list and side pane
	ContentHeight int // rows available for the list/side bodies
}

// computeLayout derives pane geometry from the terminal size. Pure + tested.
func computeLayout(w, h int) Layout {
	ch := h - chromeRows
	if ch < 1 {
		ch = 1
	}
	if w < sideThreshold {
		return Layout{ShowSide: false, ListWidth: w, ContentHeight: ch}
	}
	const gap = 2
	side := w * 45 / 100
	list := w - side - gap
	return Layout{ShowSide: true, ListWidth: list, SideWidth: side, Gap: gap, ContentHeight: ch}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/ui/ -run TestLayout -v`
Expected: PASS (3 tests).

- [ ] **Step 5: Commit**

```bash
git add internal/ui/layout.go internal/ui/layout_test.go
git commit -m "feat(ui): responsive layout calculator"
```

---

## Task 3: `Section.RenderRow` (replace Columns/Rows)

**Files:**
- Modify: `internal/ui/section.go`
- Modify: `internal/ui/section_test.go`

- [ ] **Step 1: Replace the section row test**

In `internal/ui/section_test.go`, replace `TestPRSectionRows` with:

```go
func TestPRSectionRenderRow(t *testing.T) {
	s := NewPRSection("is:open")
	s.SetPRs([]gh.PR{{Number: 7, Title: "hello world", HeadRefName: "feat/x"}})

	row := s.RenderRow(0, RowOpts{Width: 80})
	if !strings.Contains(row, "#7") || !strings.Contains(row, "hello world") {
		t.Fatalf("row missing number/title: %q", row)
	}

	sel := s.RenderRow(0, RowOpts{Width: 80, Selected: true})
	if !strings.Contains(sel, "●") {
		t.Fatalf("selected row should carry the ● marker: %q", sel)
	}
}
```

Add `"strings"` to the test file's imports if absent.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/ -run TestPRSectionRenderRow`
Expected: FAIL — `s.RenderRow undefined` and `RowOpts` undefined (and the old `Rows()`/`Columns()` still referenced elsewhere will be cleaned in Task 4).

- [ ] **Step 3: Change the `Section` interface + both sections in `internal/ui/section.go`**

Replace the `Columns()` and `Rows()` methods on the interface and implementations with `RenderRow`. The new interface:

```go
// RowOpts controls how a section renders one row.
type RowOpts struct {
	Width    int
	Focused  bool
	Selected bool
}

type Section interface {
	Kind() string
	Filter() string
	RenderRow(i int, o RowOpts) string // render shown-row i as an airy 2-line block
	Len() int
	VarsAt(i int) action.Vars
	Haystacks() []string
	SetShown(idx []int)
}
```

Delete the `Columns()` and `Rows()` methods from `PRSection` and `IssueSection`. Add `RenderRow` to each. PRSection:

```go
func (s *PRSection) RenderRow(i int, o RowOpts) string {
	p := s.prs[s.shown[i]]
	return renderItemRow(o, fmt.Sprintf("#%d", p.Number), p.Title,
		p.Author.Login, ageString(p.UpdatedAt), labelNames(p.Labels),
		reviewGlyph(p.ReviewDecision), ciGlyph(p.CIState()))
}
```

IssueSection (no CI / review for issues — pass empty):

```go
func (s *IssueSection) RenderRow(i int, o RowOpts) string {
	is := s.issues[s.shown[i]]
	return renderItemRow(o, fmt.Sprintf("#%d", is.Number), is.Title,
		is.Author.Login, ageString(is.UpdatedAt), labelNames(is.Labels), "", "")
}
```

Add the shared row renderer + helpers at the bottom of `section.go`:

```go
// renderItemRow renders the airy 2-line form:
//   ‹marker›‹num› ‹title›                         ‹ci›
//          ‹author · age · labels · review›
func renderItemRow(o RowOpts, num, title, author, age, labels, review, ci string) string {
	marker := "  "
	if o.Selected {
		marker = selMarkStyle.Render("● ")
	}
	head := fmt.Sprintf("%s%s  %s", marker, accentStyle.Render(num), title)
	meta := dimStyle.Render(strings.TrimRight(author+" · "+age+metaTail(labels, review), " ·"))
	line1 := fitLine(head, ci, o.Width)
	line2 := "         " + meta
	body := line1 + "\n" + line2
	if o.Focused {
		body = cursorRowStyle.Width(o.Width).Render(body)
	}
	return body
}

// fitLine left-aligns left and right-aligns right within width (CI glyph hugs
// the right edge); falls back to a single space when there's no room.
func fitLine(left, right string, width int) string {
	if right == "" {
		return lipgloss.NewStyle().Width(width).Render(left)
	}
	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + right
}

func metaTail(labels, review string) string {
	out := ""
	if labels != "" {
		out += " · " + labels
	}
	if review != "" {
		out += " · " + review
	}
	return out
}

func reviewGlyph(decision string) string {
	switch decision {
	case "APPROVED":
		return passStyle.Render("✓ appr")
	case "CHANGES_REQUESTED":
		return failStyle.Render("✎ changes")
	case "REVIEW_REQUIRED":
		return dimStyle.Render("◌ review")
	default:
		return ""
	}
}

func ageString(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := time.Since(t)
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}
```

Update `section.go` imports: add `"strings"`, `"time"`, `"github.com/charmbracelet/lipgloss"`; remove `"github.com/charmbracelet/bubbles/table"` (no longer referenced once Columns/Rows are gone — Task 4 removes the last table refs in the model, but section.go itself drops it now).

> Note: `ageString`/`reviewGlyph`/`renderItemRow`/`fitLine`/`metaTail` are defined once here and used by both sections (DRY).

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/ui/ -run TestPRSectionRenderRow -v`
Expected: PASS. (`go build ./...` will still FAIL — `prlist.go` references the deleted `Columns()`/`Rows()`; fixed in Task 4.)

- [ ] **Step 5: Commit**

```bash
git add internal/ui/section.go internal/ui/section_test.go
git commit -m "feat(ui): Section.RenderRow (airy 2-line rows) replacing Columns/Rows"
```

---

## Task 4: Model owns cursor + viewport; rebuild applyFilter/navigation

**Files:**
- Modify: `internal/ui/prlist.go`
- Modify: `internal/ui/prlist_test.go`

- [ ] **Step 1: Update the existing model tests to the cursor model**

In `internal/ui/prlist_test.go`, replace `m.table.Rows()` assertions. `TestSetPRsBuildsRows` becomes:

```go
func TestSetPRsBuildsRows(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.setPRs([]gh.PR{
		{Number: 7, Title: "hello", HeadRefName: "feat/x"},
		{Number: 9, Title: "world", HeadRefName: "fix/y"},
	})
	if got := m.section.Len(); got != 2 {
		t.Fatalf("shown len = %d, want 2", got)
	}
	if !strings.Contains(m.section.RenderRow(0, RowOpts{Width: 80}), "#7") {
		t.Fatalf("first row should render #7")
	}
}
```

`TestHydrateFromCache` keeps its `m.section.(*PRSection).prs` check (already updated in Plan 3) — no change needed there.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/ -run TestSetPRsBuildsRows`
Expected: FAIL to compile — `m.table` undefined after Step 3 edits / `RowOpts` import. (Before Step 3, it fails because the whole package doesn't build.)

- [ ] **Step 3: Rewrite the model fields, constructor, navigation, and applyFilter in `internal/ui/prlist.go`**

Replace the imports block's `"github.com/charmbracelet/bubbles/table"` with `"github.com/charmbracelet/bubbles/viewport"`.

Model struct — replace `table table.Model` with the cursor+viewport+size fields:

```go
type Model struct {
	dir          string
	filter       string
	cache        *cache.Cache
	runner       gh.Runner
	vp           viewport.Model
	cursor       int // indexes the section's shown set
	width        int
	height       int
	section      Section
	err          error
	filtering    bool
	filterInput  textinput.Model
	repo         string
	actions      map[string]action.Action
	pending      *action.Action
	showActions  bool
	actionFilter textinput.Model
	actionCursor int
	sel          selection
	detail          map[int]gh.PRDetail
	previewExpanded bool
	previewN        int
}
```

`NewModel` — drop the `table.New(...)` block; create the viewport:

```go
func NewModel(dir, filter string, c *cache.Cache) Model {
	ti := textinput.New()
	ti.Prompt = "/"
	af := textinput.New()
	af.Prompt = "› "
	return Model{
		dir: dir, filter: filter, cache: c, section: NewPRSection(filter),
		vp: viewport.New(0, 0), filterInput: ti, actionFilter: af,
		actions: action.DefaultPRActions(), detail: map[int]gh.PRDetail{}, previewN: 3,
	}
}
```

Add cursor navigation + render helpers:

```go
// moveCursor clamps the cursor to the shown set and keeps it visible.
func (m *Model) moveCursor(delta int) {
	n := m.section.Len()
	if n == 0 {
		m.cursor = 0
		return
	}
	m.cursor += delta
	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.cursor >= n {
		m.cursor = n - 1
	}
	m.renderList()
}

// renderList rebuilds the viewport content from the shown rows and scrolls so
// the cursor row is visible. Each row is 2 visual lines (+1 blank spacer).
func (m *Model) renderList() {
	l := computeLayout(m.width, m.height)
	listW := l.ListWidth
	var b strings.Builder
	for i := 0; i < m.section.Len(); i++ {
		b.WriteString(m.section.RenderRow(i, RowOpts{
			Width: listW, Focused: i == m.cursor, Selected: m.sel.has(i),
		}))
		b.WriteString("\n\n")
	}
	m.vp.Width = listW
	m.vp.Height = l.ContentHeight
	m.vp.SetContent(b.String())
	m.vp.SetYOffset(m.cursor * 3) // 3 lines per row; keep cursor in view (good enough; refine live)
}
```

`applyFilter` — drop the `table.Row` building; clamp the cursor and re-render:

```go
func (m *Model) applyFilter() {
	m.section.SetShown(matchIdx(m.section.Haystacks(), m.filterInput.Value()))
	if m.cursor >= m.section.Len() {
		m.cursor = m.section.Len() - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	m.renderList()
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/ui/ -run TestSetPRsBuildsRows -v`
Expected: PASS. (`go build` still fails until Tasks 5–6 rewire `cursorVars`/`Update`/`View` — that's expected mid-refactor.)

- [ ] **Step 5: Commit**

```bash
git add internal/ui/prlist.go internal/ui/prlist_test.go
git commit -m "feat(ui): model owns cursor + viewport; render list via RenderRow"
```

---

## Task 5: Rewire actions/preview/selection off the table cursor

**Files:**
- Modify: `internal/ui/actions.go`, `internal/ui/preview.go`

- [ ] **Step 1: Repoint `cursorVars` and `runBulk` in `internal/ui/actions.go`**

Replace every `m.table.Cursor()` with `m.cursor`:

```go
func (m *Model) cursorVars() (action.Vars, bool) {
	i := m.cursor
	if i < 0 || i >= m.section.Len() {
		return action.Vars{}, false
	}
	v := m.section.VarsAt(i)
	v.Repo = m.repo
	return v, true
}
```

In `runBulk`, change the no-selection fallback from `[]int{m.table.Cursor()}` to `[]int{m.cursor}`.

- [ ] **Step 2: Repoint preview helpers in `internal/ui/preview.go`**

`detailCmdForCursor` already calls `m.cursorVars()` — no change. Replace `previewWidth()` to use the layout, and rename `tableWithPreview` → `renderMain`:

```go
func (m Model) previewWidth() int {
	l := computeLayout(m.width, m.height)
	if !l.ShowSide {
		return 40
	}
	return l.SideWidth
}

// renderMain lays the list and (when wide) the contained side preview together.
func (m Model) renderMain() string {
	l := computeLayout(m.width, m.height)
	if !l.ShowSide {
		return m.vp.View()
	}
	side := lipgloss.NewStyle().Width(l.SideWidth).Height(l.ContentHeight).
		PaddingLeft(2).Render(m.previewPane())
	return lipgloss.JoinHorizontal(lipgloss.Top, m.vp.View(), side)
}
```

Add `"github.com/charmbracelet/lipgloss"` to `preview.go` imports if not present.

- [ ] **Step 3: Run build + existing action/preview tests**

Run: `go test ./internal/ui/ -run 'TestRunAction|TestBulk|TestConfirm|TestRenderPreview'`
Expected: the targeted tests build and PASS. (Full `go build ./...` still fails until Task 6 fixes `Update`/`View`.)

- [ ] **Step 4: Commit**

```bash
git add internal/ui/actions.go internal/ui/preview.go
git commit -m "refactor(ui): actions + preview use model cursor and layout"
```

---

## Task 6: Header, status bar, and the rewired Update/View

**Files:**
- Modify: `internal/ui/prlist.go`

- [ ] **Step 1: Write a failing test for header + status rendering**

Add to `internal/ui/prlist_test.go`:

```go
func TestViewShowsHeaderAndStatus(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.SetRepo("noamsto/prdash")
	m.setPRs([]gh.PR{{Number: 7, Title: "hi"}})
	m.width, m.height = 100, 30
	m.renderList()
	out := m.View()
	if !strings.Contains(out, "noamsto/prdash") {
		t.Fatalf("header should show the repo: %q", out)
	}
	if !strings.Contains(out, "q quit") {
		t.Fatalf("status bar should show key hints: %q", out)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/ -run TestViewShowsHeaderAndStatus`
Expected: FAIL (assertion or build — header/status not rendered yet).

- [ ] **Step 3: Handle `WindowSizeMsg`, cursor keys, and rewrite `View` in `internal/ui/prlist.go`**

In `Update`, add a `tea.WindowSizeMsg` case that stores size and re-renders:

```go
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.renderList()
		return m, nil
```

In the normal-mode `switch msg.String()`, replace cursor movement: instead of falling through to `m.table.Update`, handle nav explicitly and drop the table update tail. Change the `default`/tail so the final lines of `Update` become:

```go
		case "down", "j":
			m.moveCursor(1)
			return m, m.detailCmdForCursor()
		case "up", "k":
			m.moveCursor(-1)
			return m, m.detailCmdForCursor()
		}
	}
	return m, nil
```

(Remove the old `var cmd tea.Cmd; m.table, cmd = m.table.Update(msg); return m, tea.Batch(cmd, m.detailCmdForCursor())` tail entirely — the viewport doesn't need per-key updates; we drive scroll from `renderList`.)

Keep the space/V/filter/action/pending branches; in the `space` and `V` cases call `m.renderList()` instead of `m.applyFilter()` for the marker refresh (applyFilter re-filters unnecessarily — selection doesn't change the shown set):

```go
		case " ":
			m.sel.toggle(m.cursor)
			m.renderList()
			return m, nil
		case "V":
			for i := 0; i < m.section.Len(); i++ {
				if !m.sel.has(i) {
					m.sel.toggle(i)
				}
			}
			m.renderList()
			return m, nil
```

Rewrite `View` to compose header + body + status bar (replacing the old `tableWithPreview`/`m.table.View()` tails). The overlay/pending/filter early-returns stay, but their `m.table.View()` references become `m.vp.View()`:

```go
func (m Model) View() string {
	if m.pending != nil {
		n := 0
		if v, ok := m.cursorVars(); ok {
			n = v.Number
		}
		return m.header() + "\n" + accentStyle.Render(fmt.Sprintf("%s #%d? y/N", m.pending.Label, n)) +
			"\n" + m.renderMain()
	}
	if m.showActions {
		acts := filterActions(m.actions, m.actionFilter.Value())
		var b strings.Builder
		b.WriteString(m.actionFilter.View() + "\n")
		for i, a := range acts {
			cur := "  "
			if i == m.actionCursor {
				cur = "> "
			}
			b.WriteString(fmt.Sprintf("%s%-6s %s\n", cur, a.Key, a.Label))
		}
		return b.String()
	}
	if m.filtering {
		return m.header() + "\n" + m.filterInput.View() + "\n" + m.renderMain()
	}
	if m.section.Len() == 0 && m.err == nil {
		return m.header() + "\n\n" + dimStyle.Render("  Loading…") + "\n" + m.statusBar()
	}
	if m.err != nil && m.section.Len() == 0 {
		return m.header() + "\n\n" + failStyle.Render("  Error: "+m.err.Error()) + "\n" + m.statusBar()
	}
	return m.header() + "\n" + m.renderMain() + "\n" + m.statusBar()
}

// header is the top line: repo · filter · open count.
func (m Model) header() string {
	return headerStyle.Render("  "+m.repo) + dimStyle.Render(
		fmt.Sprintf("   %s · %d open", m.filter, m.section.Len()))
}

// statusBar is the bottom key/context line.
func (m Model) statusBar() string {
	keys := "↑↓ move · → expand · / filter · a actions · space select · q quit"
	if n := m.sel.count(); n > 0 {
		keys = selMarkStyle.Render(fmt.Sprintf("%d selected", n)) + " · " + keys
	}
	return statusBarStyle.Render("  " + keys)
}
```

- [ ] **Step 4: Run the full UI suite**

Run: `go test ./internal/ui/ -v`
Expected: PASS — `TestViewShowsHeaderAndStatus` plus all pre-existing UI tests (TestSetPRsBuildsRows, TestHydrateFromCache, TestRunActionExitsTUIWritesHandoff, TestConfirmDefaultNoCancels, TestBulkWritesPerItem, TestSelectionToggle, TestFilter*, TestFilterActions, TestRenderPreviewBodyShowsOlderMarker, TestPRSectionRenderRow).

- [ ] **Step 5: Build + vet + fmt, then commit**

```bash
go build ./... && go vet ./... && gofmt -l .
git add internal/ui/prlist.go internal/ui/prlist_test.go
git commit -m "feat(ui): header + status bar + cursor-driven Update/View; remove bubbles/table"
```

---

## Task 7: Live smoke test + polish pass

**Files:** none (manual verification)

- [ ] **Step 1: Build and run against a repo with PRs**

```bash
go build -o /tmp/prdash-rd . 
```
From a checkout with open PRs (e.g. the gh-dash checkout), run `/tmp/prdash-rd` in a real terminal (or via the tmux-interactive capture pattern). Verify, at several terminal widths:
- Narrow (<120 cols): full-width airy 2-line list, **no side pane**, nothing overflows.
- Wide (≥120 cols): list + contained side preview; preview text stays inside its pane (no bleed).
- Header shows `repo · filter · N open`; status bar shows keys; `space` marks `● ` rows; `/` filters; cursor highlight tracks `j/k`.

- [ ] **Step 2: Fix any overflow/scroll/highlight issues found**

Likely tuning points (adjust and re-run; commit each fix): the `SetYOffset` scroll math in `renderList` (keep cursor visible without jumping), `fitLine` width when the CI glyph is colored (use `lipgloss.Width`, already used), and the cursor-row background spanning the full `ListWidth`.

- [ ] **Step 3: Commit any polish**

```bash
git add -A
git commit -m "polish(ui): scroll + width tuning from live smoke test"
```

---

## Self-review (done)

- **Spec coverage (Phase A slice):** airy 2-line rows ✓ (T3), full-width + responsive side pane ✓ (T2/T6), no overflow / contained preview ✓ (T5/T7), theme + state glyphs ✓ (T1), header + status/key bar ✓ (T6), `bubbles/table` removed → custom viewport list ✓ (T4/T6), cursor/selection/filter contract preserved on `m.cursor`+`shown[]` ✓ (T4/T5). Deferred to later phases: dynamic triage card (Phase B), expanded tabbed view + deep-link (Phase C), the `u`/Mark-ready actions (Phase B), per-check names + `mergeStateStatus`/`files` fetch (Phase B/C).
- **Placeholders:** none — every code step shows the code; Task 7 is explicitly a manual verification task, not a code stub.
- **Type consistency:** `RowOpts{Width,Focused,Selected}`, `Section.RenderRow`, `computeLayout`/`Layout`, `ciGlyph`/`reviewGlyph`/`ageString`, `m.cursor`/`m.vp`/`m.width`/`m.height`, `renderList`/`renderMain`/`header`/`statusBar` are consistent across tasks. `bubbles/table` import removed in T3 (section) and T4 (model); no task references it afterward.

## Next

Phase B (dynamic triage card) and Phase C (expanded tabbed view) get their own plans once Phase A lands and the `Section.RenderRow` / cursor / preview-pane surfaces are concrete.
