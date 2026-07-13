# Merged/Closed View Redesign Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the merged/closed PR views chronological (newest-event-first), paint terminal PRs with state-appropriate glyphs, and move the view descriptor from the global top line onto the list-pane title.

**Architecture:** Four orthogonal changes. (1) `gh.PR` gains `MergedAt`/`ClosedAt`, fetched from `gh pr list`. (2) `sortPRs` becomes state-aware — merged/closed sort by their event time, open keeps the actionability rank. (3) `RenderRow` picks the cell-1 glyph and the right-hand time column per the PR's own `State`. (4) `header()` sheds its `preset · state · count` segment, which `listTitle()` absorbs as `<glyph> <preset> · <state> · <count>`.

**Tech Stack:** Go, charm.land/lipgloss v2, charm.land/bubbletea v2. Tests are standard `testing` package, table-driven where the existing file is.

## Global Constraints

- Follow existing patterns in each file — naming, error handling, comment density. Edit like the file, not greenfield.
- Nerd Font glyphs stay as named variables/consts with a `// nerd: <name>` comment hint. The new closed marker uses the ASCII `✗` (no font dependency) per the approved mockup.
- The merged-state glyph + kept review dot already exist (`section.go:91,98`, shipped in #28) — do not re-implement them; only extend to closed + time-sort.
- Issue rows are out of scope: no CI/review/flag cells, no sort change. Only the shared `listTitle` format touches issues.
- Test commands run from repo root. Expected output shown per step.

---

### Task 1: `gh.PR` gains merge/close timestamps

**Files:**
- Modify: `internal/gh/prs.go:10-13` (prFields), `internal/gh/prs.go:53-71` (PR struct)
- Test: `internal/gh/prs_test.go`

**Interfaces:**
- Produces: `gh.PR.MergedAt time.Time`, `gh.PR.ClosedAt time.Time` (JSON tags `mergedAt`/`closedAt`), and their presence in the `gh pr list --json` field set.

- [ ] **Step 1: Write the failing test**

Add to `internal/gh/prs_test.go`:

```go
func TestParsePRsReadsMergeAndCloseTimes(t *testing.T) {
	raw := []byte(`[{"number":1,"mergedAt":"2026-07-10T09:00:00Z","closedAt":"2026-07-10T09:00:00Z"},
	                {"number":2,"mergedAt":null,"closedAt":"2026-07-11T12:30:00Z"}]`)
	prs, err := ParsePRs(raw)
	if err != nil {
		t.Fatalf("ParsePRs: %v", err)
	}
	if prs[0].MergedAt.IsZero() {
		t.Errorf("PR #1 MergedAt should be parsed, got zero")
	}
	if !prs[1].MergedAt.IsZero() {
		t.Errorf("PR #2 has null mergedAt, want zero time, got %v", prs[1].MergedAt)
	}
	if prs[1].ClosedAt.IsZero() {
		t.Errorf("PR #2 ClosedAt should be parsed, got zero")
	}
}

func TestPRListArgsRequestsTimestamps(t *testing.T) {
	args := PRListArgs("is:merged", 20)
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "mergedAt") || !strings.Contains(joined, "closedAt") {
		t.Fatalf("PRListArgs must request mergedAt,closedAt: %q", joined)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/gh/ -run 'TestParsePRsReadsMergeAndCloseTimes|TestPRListArgsRequestsTimestamps' -v`
Expected: FAIL — `prs[0].MergedAt undefined (type PR has no field or method MergedAt)` (compile error) or the args assertion fails.

- [ ] **Step 3: Add the fields and request them**

In `internal/gh/prs.go`, extend `prFields`:

```go
var prFields = []string{
	"number", "title", "author", "statusCheckRollup", "reviewDecision",
	"labels", "assignees", "headRefName", "baseRefName", "url", "updatedAt",
	"mergedAt", "closedAt", "isDraft", "state",
}
```

In the `PR` struct, after `UpdatedAt time.Time` (`prs.go:68`):

```go
	UpdatedAt   time.Time `json:"updatedAt"`
	MergedAt    time.Time `json:"mergedAt"` // zero unless State == MERGED
	ClosedAt    time.Time `json:"closedAt"` // zero while OPEN; set for MERGED and CLOSED
	IsDraft     bool      `json:"isDraft"`
	State       string    `json:"state"` // OPEN | CLOSED | MERGED
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/gh/ -run 'TestParsePRsReadsMergeAndCloseTimes|TestPRListArgsRequestsTimestamps' -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/gh/prs.go internal/gh/prs_test.go
git commit -m "feat(gh): fetch mergedAt/closedAt on PRs"
```

---

### Task 2: State-aware sort

**Files:**
- Modify: `internal/ui/section.go:38-48` (PRSection struct), `:53-68` (SetPRs/SetCategorized), `:146-154` (sortPRs)
- Modify: `internal/ui/prlist.go:112-118` (setPRs), `:151-153` (setMine)
- Test: `internal/ui/section_test.go`

**Interfaces:**
- Consumes: `gh.PR.MergedAt`, `gh.PR.ClosedAt` (Task 1).
- Produces: `(*PRSection).SetState(state string)` — records the view state used by the next sort. `sortPRs(prs []gh.PR, state string)` — sorts merged by `MergedAt` desc, closed by `ClosedAt` desc, else rank-then-`UpdatedAt`. Call `SetState` **before** `SetPRs`/`SetCategorized` (sorting reads it).

- [ ] **Step 1: Write the failing test**

Add to `internal/ui/section_test.go`:

```go
func TestSetPRsMergedSortsByMergeTime(t *testing.T) {
	mk := func(num int, merged string) gh.PR {
		ts, _ := time.Parse(time.RFC3339, merged)
		return gh.PR{Number: num, State: "MERGED", MergedAt: ts,
			// deliberately varied CI/review so rank order would differ from time order
			ReviewDecision: "APPROVED", StatusCheckRollup: []gh.Check{{Conclusion: "FAILURE"}}}
	}
	s := NewPRSection("")
	s.SetState("merged")
	s.SetPRs([]gh.PR{
		mk(1, "2026-07-10T09:00:00Z"),
		mk(2, "2026-07-12T09:00:00Z"),
		mk(3, "2026-07-11T09:00:00Z"),
	})
	var got []int
	for i := 0; i < s.Len(); i++ {
		got = append(got, s.prAt(i).Number)
	}
	if want := []int{2, 3, 1}; !slices.Equal(got, want) {
		t.Fatalf("merged sort = %v, want newest-merge-first %v", got, want)
	}
}

func TestSetPRsClosedSortsByCloseTime(t *testing.T) {
	mk := func(num int, closed string) gh.PR {
		ts, _ := time.Parse(time.RFC3339, closed)
		return gh.PR{Number: num, State: "CLOSED", ClosedAt: ts}
	}
	s := NewPRSection("")
	s.SetState("closed")
	s.SetPRs([]gh.PR{
		mk(1, "2026-07-10T09:00:00Z"),
		mk(2, "2026-07-12T09:00:00Z"),
	})
	if s.prAt(0).Number != 2 {
		t.Fatalf("closed sort should lead with newest close #2, got #%d", s.prAt(0).Number)
	}
}
```

Add `"time"` to the `internal/ui/section_test.go` imports if not present (it currently imports `slices`, `strings`, `testing`, `lipgloss`, `gh` — add `"time"`).

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/ -run 'TestSetPRsMergedSortsByMergeTime|TestSetPRsClosedSortsByCloseTime' -v`
Expected: FAIL — `s.SetState undefined` (compile error).

- [ ] **Step 3: Add the state field, setter, and state-aware sort**

In `internal/ui/section.go`, add to the `PRSection` struct (after `forceGroup bool`, `:44`):

```go
	forceGroup bool   // group even with a single author (non-"mine" views)
	state      string // active view state (open|merged|closed); selects the sort key
```

Add the setter near `SetForceGroup` (`:161`):

```go
// SetState records the view state so the next SetPRs/SetCategorized sorts by the
// right key (merge/close time for terminal states, actionability for open).
func (s *PRSection) SetState(state string) { s.state = state }
```

Change both sort call sites to pass the state — `SetPRs` (`:55`) and `SetCategorized` (`:63`):

```go
	sortPRs(p, s.state)
```

Replace `sortPRs` (`:146-154`) with:

```go
// sortPRs orders the board. Terminal states are chronological (newest event
// first); the open board keeps the actionability rank, ties broken most-recently
// updated. Rank is meaningless once a PR has landed/closed, so it's skipped there.
func sortPRs(prs []gh.PR, state string) {
	switch state {
	case "merged":
		slices.SortStableFunc(prs, func(a, b gh.PR) int { return b.MergedAt.Compare(a.MergedAt) })
	case "closed":
		slices.SortStableFunc(prs, func(a, b gh.PR) int { return b.ClosedAt.Compare(a.ClosedAt) })
	default:
		slices.SortStableFunc(prs, func(a, b gh.PR) int {
			if d := prRank(a) - prRank(b); d != 0 {
				return d
			}
			return b.UpdatedAt.Compare(a.UpdatedAt)
		})
	}
}
```

In `internal/ui/prlist.go` `setPRs` (`:112-118`), set the state before loading:

```go
func (m *Model) setPRs(prs []gh.PR) {
	if s, ok := m.section.(*PRSection); ok {
		// Outside the "mine" view, group by author even with a single author, so
		// you always see whose PRs you're looking at.
		s.SetState(m.state)
		s.SetForceGroup(!m.isMineView())
		s.SetPRs(prs)
	}
```

In `setMine` (`:151-153`):

```go
	if s, ok := m.section.(*PRSection); ok {
		s.SetState(m.state)
		s.SetCategorized(all, cats, []string{"Mine", "Review requested"})
	}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/ui/ -run 'TestSetPRsMergedSortsByMergeTime|TestSetPRsClosedSortsByCloseTime|TestSetPRsSortsByActionability|TestSetShownOrdered' -v`
Expected: PASS (the existing open-state sort tests still pass — default branch is unchanged).

- [ ] **Step 5: Commit**

```bash
git add internal/ui/section.go internal/ui/prlist.go internal/ui/section_test.go
git commit -m "feat(ui): sort merged/closed PRs by event time"
```

---

### Task 3: Per-row terminal glyph + event-time column

**Files:**
- Modify: `internal/ui/theme.go:242-247` (add closed marker)
- Modify: `internal/ui/section.go:85-99` (RenderRow glyph + time column)
- Modify: `internal/ui/prlist.go:209-213` (suppress the flag cell for terminal PRs)
- Test: `internal/ui/section_test.go`

**Interfaces:**
- Consumes: `gh.PR.MergedAt`, `gh.PR.ClosedAt`, `gh.PR.State`.
- Produces: `closedGlyph` const + `closedMark() string` (dim `✗`). `RenderRow` now shows the merge/close/update time matching the PR's state, and a closed PR shows the dim `✗` in cell 1.

- [ ] **Step 1: Write the failing test**

Add to `internal/ui/section_test.go`:

```go
func TestClosedPRShowsDimClosedMarkNotCI(t *testing.T) {
	// A closed (unmerged) PR whose last CI run failed must show the closed mark,
	// not a red CI ✗, and not the merge mark.
	p := gh.PR{Number: 9, Title: "abandoned", State: "CLOSED",
		StatusCheckRollup: []gh.Check{{Conclusion: "SUCCESS"}}}
	s := NewPRSection("")
	s.SetState("closed")
	s.SetPRs([]gh.PR{p})
	row := s.RenderRow(0, RowOpts{Width: 80})
	if !strings.Contains(row, closedMark()) {
		t.Fatalf("closed PR row should carry the dim closed mark: %q", row)
	}
	if strings.Contains(row, mergedGlyph) {
		t.Fatalf("closed PR must not show the merge mark: %q", row)
	}
}

func TestRowTimeReflectsPRState(t *testing.T) {
	upd, _ := time.Parse(time.RFC3339, "2026-07-01T00:00:00Z")
	mrg, _ := time.Parse(time.RFC3339, "2026-07-12T00:00:00Z") // ~1d before "now"-ish
	merged := gh.PR{Number: 1, Title: "landed", State: "MERGED", UpdatedAt: upd, MergedAt: mrg}
	open := gh.PR{Number: 2, Title: "wip", State: "OPEN", UpdatedAt: mrg}

	s := NewPRSection("")
	s.SetState("merged")
	s.SetPRs([]gh.PR{merged})
	mergedRow := s.RenderRow(0, RowOpts{Width: 80})

	so := NewPRSection("")
	so.SetState("open")
	so.SetPRs([]gh.PR{open})
	openRow := so.RenderRow(0, RowOpts{Width: 80})

	// Both events are the same instant (mrg), so both rows show the same age string;
	// the merged row must derive it from MergedAt, not its (much older) UpdatedAt.
	if want := ageString(mrg); !strings.Contains(mergedRow, want) {
		t.Fatalf("merged row age should come from MergedAt (%q): %q", want, mergedRow)
	}
	if want := ageString(mrg); !strings.Contains(openRow, want) {
		t.Fatalf("open row age should come from UpdatedAt (%q): %q", want, openRow)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/ -run 'TestClosedPRShowsDimClosedMarkNotCI|TestRowTimeReflectsPRState' -v`
Expected: FAIL — `closedMark undefined` (compile error).

- [ ] **Step 3: Add the closed marker and make RenderRow state-aware**

In `internal/ui/theme.go`, after `mergedMark` (`:246`):

```go
// closedGlyph marks a PR closed without merging — a dim ✗, distinct from the red
// CI-fail ✗ by color: the checks no longer matter, the PR just didn't land.
const closedGlyph = "✗"

func closedMark() string { return dimStyle.Render(closedGlyph) }
```

In `internal/ui/section.go`, replace `RenderRow` (`:85-99`) with:

```go
func (s *PRSection) RenderRow(i int, o RowOpts) string {
	p := s.prs[s.shown[i]]
	o.Draft = p.IsDraft
	// A terminal PR's cell-1 glyph reflects how it ended, not its frozen CI rollup:
	// merged → mauve merge mark, closed → dim ✗. The age column likewise shows the
	// event that ended it (merge/close time) rather than the last update.
	status := ciGlyph(p.CIState())
	age := ageString(p.UpdatedAt)
	switch {
	case p.IsMerged():
		status, age = mergedMark(), ageString(p.MergedAt)
	case p.State == "CLOSED":
		status, age = closedMark(), ageString(p.ClosedAt)
	}
	// Author is dropped from the row: it's redundant in a single-author (flat)
	// view and hoisted into the group header when grouped.
	return renderItemRow(o, accentStyle, fmt.Sprintf("#%d", p.Number), p.Title,
		"", age, status, reviewDot(p.ReviewDecision))
}
```

In `internal/ui/prlist.go` `renderList` (`:209-213`), only compute the conflict/behind flag for open PRs (terminal PRs get a blank cell 3):

```go
		flag := ""
		if isPR && ps.prAt(i).State == "OPEN" {
			d, cached := m.detail[ps.prAt(i).Number]
			flag = flagGlyph(d, cached)
		}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/ui/ -run 'TestClosedPRShowsDimClosedMarkNotCI|TestRowTimeReflectsPRState|TestMergedPRShowsMergeMarkNotCI' -v`
Expected: PASS (the existing merged-mark test still passes).

- [ ] **Step 5: Commit**

```bash
git add internal/ui/theme.go internal/ui/section.go internal/ui/prlist.go internal/ui/section_test.go
git commit -m "feat(ui): terminal PR rows show close mark + event-time age"
```

---

### Task 4: Header split — view descriptor moves to the list title

**Files:**
- Modify: `internal/ui/prlist.go:1266-1289` (header + remove count), `:1310-1316` (listTitle + new titleGlyph)
- Test: `internal/ui/prlist_test.go`, `internal/ui/preview_test.go`

**Interfaces:**
- Consumes: `mergedGlyph` (theme.go), `closedGlyph` (Task 3), `prGlyph`/`issueGlyph`, `presetsFor`.
- Produces: `(Model).titleGlyph() string`. `header()` no longer contains the `preset · state · count` segment. `listTitle()` returns `<glyph> <preset> · <state> · <shown-count>`. `(Model).count()` is removed.

- [ ] **Step 1: Update the failing tests**

Rewrite `TestCountTracksShownOverTotal` (`prlist_test.go:32-42`) — the shown count now lives in the title, and `count()` is gone:

```go
func TestListTitleTracksShownCount(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.setPRs([]gh.PR{{Number: 7, Title: "hello"}, {Number: 9, Title: "world"}})
	if got := m.listTitle(); !strings.Contains(got, "· 2") {
		t.Fatalf("listTitle = %q, want to contain %q", got, "· 2")
	}
	m.section.SetShown([]int{0})
	if got := m.listTitle(); !strings.Contains(got, "· 1") {
		t.Fatalf("filtered listTitle = %q, want to contain %q", got, "· 1")
	}
}
```

Rewrite `TestListTitleReflectsSection` (`prlist_test.go:333-340`) to the new format (no "PRs" word; carries the board glyph + state):

```go
func TestListTitleReflectsSection(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.SetRepo("r")
	m.setPRs([]gh.PR{{Number: 1}, {Number: 2}})
	got := m.listTitle()
	if !strings.Contains(got, prGlyph) || !strings.Contains(got, "open") || !strings.Contains(got, "· 2") {
		t.Fatalf("listTitle = %q, want glyph + state + count", got)
	}
}
```

In `preview_test.go:188`, change the assertion from `"PRs · 1"` to the new title fragment:

```go
	if !strings.Contains(out, "· 1") {
		t.Fatalf("list pane should be titled: %q", out)
	}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/ui/ -run 'TestListTitleTracksShownCount|TestListTitleReflectsSection|TestRenderMainBordersListPane' -v`
Expected: `TestListTitleReflectsSection` FAILs — the old `listTitle` emits `<prGlyph> PRs · 2`, which lacks the `open` state token. (The count/border assertions happen to already hold on the old format — the behavior-defining failure is the missing state token; step 3 makes all three pass.)

- [ ] **Step 3: Trim the header, add titleGlyph, rebuild listTitle**

In `internal/ui/prlist.go`, replace `header()` (`:1266-1283`) — drop the label/state/count segment and its now-unused `label` local:

```go
// header is the global top line: repo · board segments · (spinner) · (badge) ·
// (selection). The current view (preset/state/count) lives on the list title.
func (m Model) header() string {
	h := headerStyle.Render("  "+m.repo) + "  " + modeSegments(m.mode)
	if m.refreshing {
		spin := spinnerFrames[m.spinnerFrame%len(spinnerFrames)]
		h += dimStyle.Render(" · ") + refreshStyle.Render(spin+" refreshing")
	}
	h += m.statusBadge()
	if n := m.sel.count(); n > 0 {
		h += "  " + selMarkStyle.Render(fmt.Sprintf("%d selected", n))
	}
	return h
}
```

Delete `count()` entirely (`:1285-1289`, including its doc comment).

Replace `listTitle()` (`:1310-1316`) and add `titleGlyph()`:

```go
// titleGlyph is the list-title marker: the terminal-state glyph for merged/closed
// PRs, else the board glyph. Issues have no merged state, so they always use theirs.
func (m Model) titleGlyph() string {
	if m.mode == "issue" {
		return issueGlyph
	}
	switch m.state {
	case "merged":
		return mergedGlyph
	case "closed":
		return closedGlyph
	default:
		return prGlyph
	}
}

// listTitle is the list pane's border title — the current view: state glyph +
// preset (or custom author body) + state + shown count.
func (m Model) listTitle() string {
	label := m.body
	if m.presetIdx >= 0 {
		label = presetsFor(m.mode)[m.presetIdx].name
	}
	return fmt.Sprintf("%s %s · %s · %d", m.titleGlyph(), label, m.state, m.section.Len())
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/ui/ -run 'TestListTitleTracksShownCount|TestListTitleReflectsSection|TestRenderMainBordersListPane|TestBoardGlyphsPresent|TestSelectionCountInHeaderNotBar' -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/ui/prlist.go internal/ui/prlist_test.go internal/ui/preview_test.go
git commit -m "feat(ui): move view descriptor from header to list title"
```

---

### Task 5: Legend documents the terminal glyphs

**Files:**
- Modify: `internal/ui/prlist.go:1345-1350` (legend glyph rows)
- Test: `internal/ui/prlist_test.go`

**Interfaces:**
- Consumes: `mergedGlyph`, `closedGlyph`.
- Produces: legend text naming the merged and closed marks.

- [ ] **Step 1: Write the failing test**

Add to `internal/ui/prlist_test.go`:

```go
func TestLegendDocumentsTerminalGlyphs(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.SetRepo("r")
	m.width, m.height = 130, 40
	leg := m.legendView()
	if !strings.Contains(leg, mergedGlyph) {
		t.Fatalf("legend should document the merged mark %q: %q", mergedGlyph, leg)
	}
	if !strings.Contains(leg, "merged") || !strings.Contains(leg, "closed") {
		t.Fatalf("legend should name merged and closed states: %q", leg)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/ -run TestLegendDocumentsTerminalGlyphs -v`
Expected: FAIL — legend contains neither `mergedGlyph` nor "merged"/"closed".

- [ ] **Step 3: Add a status-glyph legend row**

In `internal/ui/prlist.go` `legendView`, replace the first `rows` entry block (`:1345-1350`):

```go
	rows := []string{
		accentStyle.Render("CI / review") + statusBarStyle.Render("  ✓ pass   ✗ fail   ● running   · none"),
		accentStyle.Render("state") + statusBarStyle.Render("       "+mergedGlyph+" merged   "+closedGlyph+" closed"),
		accentStyle.Render("!") + statusBarStyle.Render("           ⚠ conflict / behind base"),
		accentStyle.Render("row") + statusBarStyle.Render("         ▎ focus   ● selected   [draft] dimmed"),
		"",
	}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/ui/ -run TestLegendDocumentsTerminalGlyphs -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/ui/prlist.go internal/ui/prlist_test.go
git commit -m "docs(ui): legend documents merged/closed glyphs"
```

---

### Final verification

- [ ] **Run the full suite**

Run: `go test ./...`
Expected: PASS across all packages.

- [ ] **Build and eyeball the merged view**

Run: `go build ./... && go run . ` (in a repo with merged PRs; press `s` to cycle to the merged state)
Expected: top line shows only repo + `PRs │ Issues`; the list-box title reads `<glyph> mine · merged · N`; merged rows sort newest-merge-first with the mauve merge mark; cycle to closed (`s`) → dim `✗`, newest-close-first.
