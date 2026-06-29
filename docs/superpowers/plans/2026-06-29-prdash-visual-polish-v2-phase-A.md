# prdash visual polish v2 — Phase A (palette + rows) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the inherited 256-color palette with an owned, mauve-led Catppuccin Mocha theme, sort the board by actionability with drafts dimmed and last, and align the number/age columns.

**Architecture:** A `Theme` value (named role fields) constructed by `Mocha()` becomes the single source of palette truth in `theme.go`; the existing package-level role styles are rebuilt from it (every other file keeps using `accentStyle`, `dimStyle`, … unchanged). Sorting + draft styling live in `section.go` (`PRSection.SetPRs` sorts on a list-reliable rank); column alignment is a per-frame width computed in `renderList` and threaded through `RowOpts`.

**Tech Stack:** Go, `charm.land/lipgloss/v2` v2.0.4, `charm.land/bubbletea/v2`, table-driven `testing`.

## Global Constraints

- **Palette is owned, not inherited** — hardcoded Catppuccin Mocha hex, no 256-color indices, no terminal-theme dependence.
- **Accent is mauve `#cba6f7`** and dominant; focus (sky `#89dceb`) and select (pink `#f5c2e7`) stay visually distinct from it.
- **Sort is stable and list-reliable only** — rank uses `CIState()`, `ReviewDecision`, `IsDraft`; never `mergeStateStatus`/conflict (detail-derived → would reshuffle as prefetch lands).
- **Drafts always sort last** and render dimmed.
- Match existing test style: table-driven, `strings.Contains`, `tea.KeyPressMsg` for keys.
- Run `nix build` / `go test ./...` from the worktree; commit after each task.

---

### Task 1: Owned Catppuccin Mocha theme

**Files:**
- Modify: `internal/ui/theme.go:11-40` (replace the palette var block + `authorPalette`)
- Test: `internal/ui/theme_test.go` (add cases; existing `TestCIGlyph*` must keep passing)

**Interfaces:**
- Produces: `type Theme struct{…}`, `func Mocha() Theme`, package var `theme Theme`. The existing role styles (`titleStyle`, `accentStyle`, `dimStyle`, `sepStyle`, `passStyle`, `failStyle`, `pendStyle`, `selMarkStyle`, `focusBarStyle`, `headerStyle`, `statusBarStyle`) keep their names and are rebuilt from `theme`. `authorStyle(login)` keeps its signature.

- [ ] **Step 1: Write the failing test**

Add to `internal/ui/theme_test.go`:

```go
func TestMochaPaletteIsMauveLed(t *testing.T) {
	th := Mocha()
	if th.Accent != "#cba6f7" {
		t.Errorf("accent = %q, want mauve #cba6f7", th.Accent)
	}
	if th.Header != th.Accent {
		t.Errorf("header should be mauve-led like accent: header=%q accent=%q", th.Header, th.Accent)
	}
	if th.Focus == th.Accent || th.Select == th.Accent || th.Focus == th.Select {
		t.Errorf("focus, select, accent must all be distinct: accent=%q focus=%q select=%q",
			th.Accent, th.Focus, th.Select)
	}
}

func TestAuthorPaletteExcludesAccent(t *testing.T) {
	for _, c := range Mocha().Author {
		if c == Mocha().Accent {
			t.Fatalf("author hue %q collides with the accent mauve", c)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/ -run 'TestMochaPalette|TestAuthorPalette' -v`
Expected: FAIL — `undefined: Mocha`.

- [ ] **Step 3: Write minimal implementation**

Replace `internal/ui/theme.go:11-40` (the `var (...)` palette block through the `authorPalette` line) with:

```go
// Theme is the owned palette. Roles are concrete Catppuccin hex — prdash does
// NOT inherit the terminal theme, so mauve is mauve everywhere. Adding a flavor
// (Latte/Frappé/Macchiato) or a dark/light toggle later is a second constructor.
type Theme struct {
	Accent  lipgloss.Color // mauve — #, keys, links, headline, header/active tab
	Header  lipgloss.Color
	Focus   lipgloss.Color // sky — cursor-row bar
	Select  lipgloss.Color // pink — multi-select ●
	Text    lipgloss.Color // row titles, body
	Meta    lipgloss.Color // age, labels, dim hints
	Rule    lipgloss.Color // dividers, borders
	RowBg   lipgloss.Color // cursor-row background
	Pass    lipgloss.Color // green
	Fail    lipgloss.Color // red
	Pending lipgloss.Color // yellow
	Author  []lipgloss.Color
}

// Mocha is the Catppuccin Mocha flavor.
func Mocha() Theme {
	return Theme{
		Accent: "#cba6f7", Header: "#cba6f7", Focus: "#89dceb", Select: "#f5c2e7",
		Text: "#cdd6f4", Meta: "#a6adc8", Rule: "#585b70", RowBg: "#313244",
		Pass: "#a6e3a1", Fail: "#f38ba8", Pending: "#f9e2af",
		// Distinct author hues — deliberately excludes mauve (accent), sky (focus),
		// pink (select), and the green/red/yellow state colors.
		Author: []lipgloss.Color{
			"#b4befe", "#94e2d5", "#fab387", "#74c7ec",
			"#eba0ac", "#f5e0dc", "#f2cdcd", "#89b4fa",
		},
	}
}

// theme is the active palette. A future toggle reassigns this.
var theme = Mocha()

var (
	titleStyle     = lipgloss.NewStyle().Foreground(theme.Text)
	accentStyle    = lipgloss.NewStyle().Foreground(theme.Accent)
	dimStyle       = lipgloss.NewStyle().Foreground(theme.Meta)
	sepStyle       = lipgloss.NewStyle().Foreground(theme.Rule)
	passStyle      = lipgloss.NewStyle().Foreground(theme.Pass)
	failStyle      = lipgloss.NewStyle().Foreground(theme.Fail)
	pendStyle      = lipgloss.NewStyle().Foreground(theme.Pending)
	selMarkStyle   = lipgloss.NewStyle().Foreground(theme.Select)
	focusBarStyle  = lipgloss.NewStyle().Foreground(theme.Focus)
	headerStyle    = lipgloss.NewStyle().Foreground(theme.Header).Bold(true)
	statusBarStyle = lipgloss.NewStyle().Foreground(theme.Meta)
)
```

Then update `authorStyle` (currently `theme.go:33-40`) to source from the theme:

```go
func authorStyle(login string) lipgloss.Style {
	if isBot(login) {
		return dimStyle
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(login))
	return lipgloss.NewStyle().Foreground(theme.Author[h.Sum32()%uint32(len(theme.Author))])
}
```

(The old `var authorPalette = []string{…}` is deleted — `theme.Author` replaces it.)

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/ui/ -run 'TestMochaPalette|TestAuthorPalette|TestCIGlyph' -v`
Expected: PASS (all four).

- [ ] **Step 5: Build to confirm no references to the removed `authorPalette` remain**

Run: `go build ./...`
Expected: clean build.

- [ ] **Step 6: Commit**

```bash
git add internal/ui/theme.go internal/ui/theme_test.go
git commit -m "feat(ui): own Catppuccin Mocha palette, mauve-led"
```

---

### Task 2: Actionability sort (drafts last)

**Files:**
- Modify: `internal/ui/section.go` (add `import "slices"`; add `prRank`, `sortPRs`, rank consts; sort inside `PRSection.SetPRs`)
- Test: `internal/ui/section_test.go`

**Interfaces:**
- Consumes: `gh.PR.CIState() string`, `gh.PR.ReviewDecision string`, `gh.PR.IsDraft bool`, `gh.PR.UpdatedAt time.Time`.
- Produces: `func prRank(p gh.PR) int` (lower = higher on board); `PRSection.SetPRs` now stores a sorted slice.

- [ ] **Step 1: Write the failing test**

Add to `internal/ui/section_test.go`:

```go
func TestSetPRsSortsByActionability(t *testing.T) {
	s := NewPRSection("")
	s.SetPRs([]gh.PR{
		{Number: 1, IsDraft: true},
		{Number: 2, ReviewDecision: "APPROVED", StatusCheckRollup: []gh.Check{{Conclusion: "SUCCESS"}}},
		{Number: 3, ReviewDecision: "CHANGES_REQUESTED"},
		{Number: 4, StatusCheckRollup: []gh.Check{{Conclusion: "FAILURE"}}},
		{Number: 5, StatusCheckRollup: []gh.Check{{Conclusion: "IN_PROGRESS"}}},
		{Number: 6, ReviewDecision: "REVIEW_REQUIRED"},
	})
	var got []int
	for i := 0; i < s.Len(); i++ {
		got = append(got, s.prAt(i).Number)
	}
	// ready(2) → changes(3) → fail(4) → running(5) → waiting(6) → draft(1)
	want := []int{2, 3, 4, 5, 6, 1}
	if !slices.Equal(got, want) {
		t.Fatalf("sort order = %v, want %v", got, want)
	}
}
```

(Add `"slices"` to the test file's imports.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/ -run TestSetPRsSortsByActionability -v`
Expected: FAIL — order is the unsorted `[1 2 3 4 5 6]`.

- [ ] **Step 3: Write minimal implementation**

Add `"slices"` to `internal/ui/section.go` imports. Add near the bottom of the PR section block:

```go
// Actionability ranks (lower sorts higher). Drafts always last.
const (
	rankReady = iota
	rankChanges
	rankFail
	rankRunning
	rankWaiting
	rankDraft
)

// prRank scores a PR by how much it needs the author, using only signals that
// are reliable from the bulk `gh pr list` (CI rollup, reviewDecision, isDraft).
// It deliberately ignores mergeStateStatus/conflict — those are detail-derived
// and would reshuffle the board as background prefetch lands.
func prRank(p gh.PR) int {
	switch {
	case p.IsDraft:
		return rankDraft
	case p.ReviewDecision == "CHANGES_REQUESTED":
		return rankChanges
	case p.CIState() == "fail":
		return rankFail
	case p.CIState() == "pending":
		return rankRunning
	case p.ReviewDecision == "APPROVED":
		return rankReady
	default:
		return rankWaiting
	}
}

// sortPRs orders by actionability rank, ties broken most-recently-updated first.
func sortPRs(prs []gh.PR) {
	slices.SortStableFunc(prs, func(a, b gh.PR) int {
		if d := prRank(a) - prRank(b); d != 0 {
			return d
		}
		return b.UpdatedAt.Compare(a.UpdatedAt)
	})
}
```

Change `SetPRs` (currently `section.go:43`) to sort first:

```go
func (s *PRSection) SetPRs(p []gh.PR) { sortPRs(p); s.prs = p; s.shown = allIdx(len(p)) }
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/ui/ -run 'TestSetPRs|TestPRSection|TestSetPRsBuildsRows' -v`
Expected: PASS. (`TestSetPRsBuildsRows` feeds #7 then #9; both rank `rankWaiting`, tie broken by zero `UpdatedAt` so order is stable input order #7,#9 — assertion `#7` first still holds.)

- [ ] **Step 5: Commit**

```bash
git add internal/ui/section.go internal/ui/section_test.go
git commit -m "feat(ui): sort board by actionability, drafts last"
```

---

### Task 3: Draft rows render dimmed

**Files:**
- Modify: `internal/ui/section.go` (`RowOpts` gains `Draft bool`; `renderItemRow` dims the title; `PRSection.RenderRow` sets it)
- Test: `internal/ui/section_test.go`

**Interfaces:**
- Consumes: `RowOpts`, `renderItemRow`, `PRSection.RenderRow`.
- Produces: `RowOpts.Draft bool`; draft rows styled distinctly.

- [ ] **Step 1: Write the failing test**

Add to `internal/ui/section_test.go`:

```go
func TestDraftRowIsStyledDistinctly(t *testing.T) {
	args := func(o RowOpts) string {
		return renderItemRow(o, "#1", "title", "alice", "2d", ciGlyph("pass"), reviewDot(""))
	}
	plain := args(RowOpts{Width: 80})
	draft := args(RowOpts{Width: 80, Draft: true})
	if plain == draft {
		t.Fatal("a draft row must render distinctly (dimmed) from a normal row")
	}
}

func TestPRSectionMarksDraftRow(t *testing.T) {
	s := NewPRSection("")
	s.SetPRs([]gh.PR{{Number: 1, Title: "wip", IsDraft: true}})
	normal := NewPRSection("")
	normal.SetPRs([]gh.PR{{Number: 1, Title: "wip"}})
	if s.RenderRow(0, RowOpts{Width: 80}) == normal.RenderRow(0, RowOpts{Width: 80}) {
		t.Fatal("PRSection.RenderRow should style a draft PR distinctly")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/ -run 'TestDraftRow|TestPRSectionMarksDraftRow' -v`
Expected: FAIL — `RowOpts` has no field `Draft` (compile error), then once added, the rows render identically.

- [ ] **Step 3: Write minimal implementation**

Add `Draft bool` to `RowOpts` (`section.go:16-21`):

```go
type RowOpts struct {
	Width    int
	Focused  bool
	Selected bool
	Draft    bool   // dim the title; drafts sort last (see prRank)
	Flag     string // pre-rendered ! column glyph, "" when unknown
}
```

In `PRSection.RenderRow` (`section.go:50`), set it from the PR:

```go
func (s *PRSection) RenderRow(i int, o RowOpts) string {
	p := s.prs[s.shown[i]]
	o.Draft = p.IsDraft
	return renderItemRow(o, fmt.Sprintf("#%d", p.Number), p.Title,
		p.Author.Login, ageString(p.UpdatedAt),
		ciGlyph(p.CIState()), reviewDot(p.ReviewDecision))
}
```

In `renderItemRow` (`section.go:159-163`), pick the title style — draft beats focus:

```go
	titleSt := titleStyle
	switch {
	case o.Draft:
		titleSt = dimStyle
	case o.Focused:
		titleSt = titleSt.Bold(true)
	}
	titleTxt := titleSt.Render(truncate(title, titleRoom))
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/ui/ -run 'TestDraftRow|TestPRSectionMarksDraftRow|TestRenderItemRowIsSingleLine' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/section.go internal/ui/section_test.go
git commit -m "feat(ui): dim draft PR rows"
```

---

### Task 4: Fixed-width number + age columns

**Files:**
- Modify: `internal/ui/section.go` (`RowOpts.NumWidth`; `padNum` helper; `renderItemRow` uses them; `columnWidths` helper)
- Modify: `internal/ui/prlist.go:100-121` (`renderList` computes width, passes via `RowOpts`)
- Test: `internal/ui/section_test.go`

**Interfaces:**
- Consumes: `Section`, `*PRSection`, `*IssueSection`, `RowOpts`.
- Produces: `func padNum(num string, w int) string`; `func columnWidths(s Section) int`; `RowOpts.NumWidth int`.

- [ ] **Step 1: Write the failing test**

Add to `internal/ui/section_test.go`:

```go
func TestPadNumRightAligns(t *testing.T) {
	if got := padNum("#7", 5); got != "   #7" {
		t.Fatalf("padNum(#7,5) = %q, want %q", got, "   #7")
	}
	if got := padNum("#1234", 3); got != "#1234" { // never truncates below content
		t.Fatalf("padNum(#1234,3) = %q, want %q", got, "#1234")
	}
}

func TestColumnWidthsUsesWidestNumber(t *testing.T) {
	s := NewPRSection("")
	s.SetPRs([]gh.PR{{Number: 7}, {Number: 1234}})
	if got := columnWidths(s); got != len("#1234") {
		t.Fatalf("columnWidths = %d, want %d", got, len("#1234"))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/ -run 'TestPadNum|TestColumnWidths' -v`
Expected: FAIL — `undefined: padNum` / `undefined: columnWidths`.

- [ ] **Step 3: Write minimal implementation**

Add to `internal/ui/section.go`:

```go
// padNum right-aligns a plain "#123" string to w cells; never truncates.
func padNum(num string, w int) string {
	if len(num) >= w {
		return num
	}
	return strings.Repeat(" ", w-len(num)) + num
}

// columnWidths returns the cell width for the number column: the widest "#N"
// across the shown set, floored at 4 ("#999") per design §2 ("min 4").
func columnWidths(s Section) int {
	w := 4
	switch x := s.(type) {
	case *PRSection:
		for _, i := range x.shown {
			w = max(w, len(fmt.Sprintf("#%d", x.prs[i].Number)))
		}
	case *IssueSection:
		for _, i := range x.shown {
			w = max(w, len(fmt.Sprintf("#%d", x.issues[i].Number)))
		}
	}
	return w
}
```

Add `NumWidth int` to `RowOpts`:

```go
type RowOpts struct {
	Width    int
	NumWidth int // cell width for the right-aligned number column (0 = natural)
	Focused  bool
	Selected bool
	Draft    bool
	Flag     string
}
```

In `renderItemRow` (`section.go:151`), right-align the number and pad age to 3:

```go
	numCell := num
	if o.NumWidth > 0 {
		numCell = padNum(num, o.NumWidth)
	}
	left := bar + mark + " " + ci + " " + review + " " + flag + " " + accentStyle.Render(numCell) + " "
	right := authorStyle(author).Render(author) + dimStyle.Render(fmt.Sprintf("  %3s", age))
```

In `renderList` (`prlist.go:100-116`), compute once and pass it:

```go
func (m *Model) renderList() {
	l := computeLayout(m.width, m.height)
	listW := l.ListWidth
	numW := columnWidths(m.section)
	ps, isPR := m.section.(*PRSection)
	var b strings.Builder
	for i := 0; i < m.section.Len(); i++ {
		flag := ""
		if isPR {
			num := ps.prAt(i).Number
			d, cached := m.detail[num]
			flag = flagGlyph(d, cached)
		}
		b.WriteString(m.section.RenderRow(i, RowOpts{
			Width: listW, NumWidth: numW, Focused: i == m.cursor, Selected: m.sel.has(i), Flag: flag,
		}))
		b.WriteString("\n")
	}
	m.vp.SetWidth(listW)
	m.vp.SetHeight(l.ContentHeight)
	m.vp.SetContent(b.String())
	m.scrollToCursor()
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/ui/ -v`
Expected: PASS (full package — confirms no row-rendering test regressed).

- [ ] **Step 5: Commit**

```bash
git add internal/ui/section.go internal/ui/prlist.go internal/ui/section_test.go
git commit -m "feat(ui): right-align number + age columns"
```

---

### Task 5: Full build + visual smoke check

**Files:** none (verification task)

- [ ] **Step 1: Full test + vet**

Run: `go test ./... && go vet ./...`
Expected: all pass, no vet warnings.

- [ ] **Step 2: Nix build**

Run: `nix build` (from the worktree root)
Expected: builds clean (no `go.mod`/`go.sum` change in this phase, so `vendorHash` is untouched).

- [ ] **Step 3: Manual smoke**

Run the binary against a repo with a mix of open/draft PRs (`./result/bin/prdash` or `go run .`). Confirm by eye: titles read mauve-accented numbers, the cursor bar is sky, multi-select `●` is pink, drafts are dimmed and sit at the bottom, ready/blocked PRs are at the top, and the `#`/age columns line up vertically. Note anything off for Phase C (borders) rather than fixing here.

- [ ] **Step 4: No commit** (verification only). Phase A is complete.

---

## Subsequent phases (separate plans, written just-in-time)

Each ships a working board on its own and gets its own detailed TDD plan written immediately before execution — Phase B because it changes viewport scroll math, C/D because they need lipgloss-v2 border/overlay/viewport APIs verified against the running app before writing no-placeholder code.

### Phase B — author-cardinality grouping
- Distinct-author count over the shown set drives the layout: 1 author → flat list, per-row author column dropped (the redundant handle disappears); ≥2 → rows grouped under dim author-rule headers, handle hoisted into the header.
- Group order by each author's most-actionable member; within a group, the Phase-A ladder.
- **Scroll-math change:** `renderList` must build a display-line list (headers interleaved with rows) and map the cursor's shown-index to its display line so `scrollToCursor` stays correct. This is the crux task and why B is isolated.

### Phase C — borders + signage
- Rounded (`lipgloss.RoundedBorder`) panes on list / preview / action-bar, drawn in `theme.Rule`; titled tops (`PRs · 12`, `#309`) via a small title-overlay helper (lipgloss has no native titled border).
- Preview split into labeled dim subsections (identity · blocker · checks · review · latest).
- Persistent dim column-header row above the list; `?` floating legend overlay.
- `computeLayout`/`previewWidth` adjusted for border + padding so nothing clips.

### Phase D — overlays + interaction
- Floating, centered, bordered action menu (mauve chrome, selected row highlighted); `?` legend reuses it.
- Side preview gets its own `viewport`; `Ctrl+j`/`Ctrl+k` scroll it (verify `ctrl+j` ≠ Enter in the real terminal; fall back to `Ctrl+d`/`Ctrl+u`).
- Expanded view: `h`/`←` on the first tab exits (and un-maximizes from `z`).
- Restore the context-aware action bar; add a regression guard test that no card/bar `ActionKey` is multi-character.
