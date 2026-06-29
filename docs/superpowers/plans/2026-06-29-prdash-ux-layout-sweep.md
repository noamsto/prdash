# prdash UX Layout Sweep Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Reshape the prdash list into a dense, scannable PR status board with a blocker-led side card and context-aware actions, optimized for the glance → act loop.

**Architecture:** The list keeps its single-`Section` model (one PR *or* Issue section at a time — there is no simultaneous grouping, so no section headers). Rows collapse from a 2-line block to one columnar line; the side card gains an identity header and reorders to lead with the blocker; the `!` conflict column and the side card are fed from the in-memory per-PR detail map (`Model.detail`), which is filled by a debounced focus-fetch plus a bounded prefetch. No new disk cache; detail stays session-scoped as today.

**Tech Stack:** Go, charm.land/bubbletea/v2 + lipgloss/v2 + bubbles/v2, `gh` CLI via `gh.Runner`. Tests are plain `testing` table/substring style (no testify).

## Global Constraints

- Module paths are **v2**: `charm.land/bubbletea/v2`, `charm.land/lipgloss/v2`, `charm.land/bubbles/v2/*`. Never import v1.
- Colors are **256-color palette roles** from `internal/ui/theme.go` (`accentStyle`, `dimStyle`, `passStyle`, `failStyle`, `pendStyle`, `selMarkStyle`, `focusBarStyle`, `headerStyle`, `authorStyle`). Never hard-code hex; the terminal's Catppuccin overlay maps the indices.
- Width math uses `lipgloss.Width(...)` (not `len`) for any string that may contain styled/wide runes.
- `mergeStateStatus`/`mergeable` are reliable **only** from the per-PR detail fetch (`gh.PRDetail`), never from the bulk list. The `!` column and any conflict/behind signal must read `Model.detail`, and render blank when the PR's detail is not yet cached — never guess.
- Run tests with `go test ./internal/ui/... ./internal/triage/... ./internal/action/...` from the repo root.
- Commit messages: conventional commits, suffix `(#5)`.

---

## Task 1: Dense single-line board rows

Collapse the 2-line airy row to one columnar line: `▎ ● CI RV ! #num  title … author  age`. Add the `!` (flag) slot to `RowOpts` (filled later in Task 4) and a compact single-rune review glyph. Labels leave the row (still searchable via `Haystacks`).

**Files:**
- Modify: `internal/ui/section.go` (`RowOpts`, `renderItemRow`, `PRSection.RenderRow`, `IssueSection.RenderRow`; add `reviewDot`)
- Modify: `internal/ui/prlist.go` (`rowLines` const, `renderList` row spacing)
- Test: `internal/ui/section_test.go`

**Interfaces:**
- Produces: `RowOpts{Width int; Focused bool; Selected bool; Flag string}`; `renderItemRow(o RowOpts, num, title, author, age, ci, review string) string` (one line, no trailing newline); `reviewDot(decision string) string` (single colored rune).
- Consumes: existing `ciGlyph`, `truncate`, `ageString`, `authorStyle`, palette styles.

- [ ] **Step 1: Write the failing test for `reviewDot`**

Add to `internal/ui/section_test.go`:

```go
func TestReviewDot(t *testing.T) {
	cases := map[string]string{
		"APPROVED":          "✓",
		"CHANGES_REQUESTED": "✗",
		"REVIEW_REQUIRED":   "●",
		"":                  "·",
	}
	for decision, want := range cases {
		if got := reviewDot(decision); !strings.Contains(got, want) {
			t.Errorf("reviewDot(%q) = %q, want to contain %q", decision, got, want)
		}
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/ui/ -run TestReviewDot`
Expected: FAIL — `undefined: reviewDot`.

- [ ] **Step 3: Implement `reviewDot`**

Add to `internal/ui/section.go` (next to `reviewGlyph`):

```go
// reviewDot is the single-rune review-decision glyph for the dense board row.
func reviewDot(decision string) string {
	switch decision {
	case "APPROVED":
		return passStyle.Render("✓")
	case "CHANGES_REQUESTED":
		return failStyle.Render("✗")
	case "REVIEW_REQUIRED":
		return pendStyle.Render("●")
	default:
		return dimStyle.Render("·")
	}
}
```

- [ ] **Step 4: Run it to verify it passes**

Run: `go test ./internal/ui/ -run TestReviewDot`
Expected: PASS.

- [ ] **Step 5: Write the failing test for the dense row**

Add to `internal/ui/section_test.go`:

```go
func TestRenderItemRowIsSingleLine(t *testing.T) {
	o := RowOpts{Width: 80, Focused: true, Selected: true, Flag: failStyle.Render("⚠")}
	row := renderItemRow(o, "#7", "hello world", "alice", "2d",
		ciGlyph("fail"), reviewDot("APPROVED"))
	if strings.Contains(row, "\n") {
		t.Fatalf("dense row must be one line: %q", row)
	}
	for _, want := range []string{"#7", "hello world", "alice", "2d", "▎", "●", "⚠"} {
		if !strings.Contains(row, want) {
			t.Fatalf("row missing %q: %q", want, row)
		}
	}
}
```

- [ ] **Step 6: Run it to verify it fails**

Run: `go test ./internal/ui/ -run TestRenderItemRowIsSingleLine`
Expected: FAIL — `renderItemRow` signature mismatch / `Flag` unknown field.

- [ ] **Step 7: Add `Flag` to `RowOpts` and rewrite `renderItemRow`**

In `internal/ui/section.go`, add the field:

```go
type RowOpts struct {
	Width    int
	Focused  bool
	Selected bool
	Flag     string // pre-rendered ! column glyph (conflict/behind), "" when unknown
}
```

Replace `renderItemRow` (and delete the old `metaIndent`/`metaTail` helpers if now unused — check with the compiler) with:

```go
// renderItemRow renders one dense board line:
//
//	‹bar›‹mark› ‹ci› ‹rv› ‹!› ‹num› ‹title…›            ‹author›  ‹age›
func renderItemRow(o RowOpts, num, title, author, age, ci, review string) string {
	w := o.Width
	if w < 24 {
		w = 24 // floor keeps truncation sane before the first WindowSizeMsg
	}
	bar, mark := " ", " "
	if o.Focused {
		bar = focusBarStyle.Render("▎")
	}
	if o.Selected {
		mark = selMarkStyle.Render("●")
	}
	flag := o.Flag
	if flag == "" {
		flag = " "
	}
	if ci == "" {
		ci = dimStyle.Render("·")
	}
	if review == "" {
		review = dimStyle.Render("·")
	}
	left := bar + mark + " " + ci + " " + review + " " + flag + " " + accentStyle.Render(num) + " "
	right := authorStyle(author).Render(author) + dimStyle.Render("  "+age)
	leftW, rightW := lipgloss.Width(left), lipgloss.Width(right)

	titleRoom := w - leftW - rightW - 2
	if titleRoom < 1 {
		titleRoom = 1
	}
	titleSt := titleStyle
	if o.Focused {
		titleSt = titleSt.Bold(true)
	}
	titleTxt := titleSt.Render(truncate(title, titleRoom))

	gap := w - leftW - lipgloss.Width(titleTxt) - rightW
	if gap < 1 {
		gap = 1
	}
	return left + titleTxt + strings.Repeat(" ", gap) + right
}
```

Update `PRSection.RenderRow` to pass the compact glyphs and drop labels:

```go
func (s *PRSection) RenderRow(i int, o RowOpts) string {
	p := s.prs[s.shown[i]]
	return renderItemRow(o, fmt.Sprintf("#%d", p.Number), p.Title,
		p.Author.Login, ageString(p.UpdatedAt),
		ciGlyph(p.CIState()), reviewDot(p.ReviewDecision))
}
```

Update `IssueSection.RenderRow` (no CI/review/flag):

```go
func (s *IssueSection) RenderRow(i int, o RowOpts) string {
	is := s.issues[s.shown[i]]
	return renderItemRow(o, fmt.Sprintf("#%d", is.Number), is.Title,
		is.Author.Login, ageString(is.UpdatedAt), "", "")
}
```

- [ ] **Step 8: Flip `rowLines` and row spacing**

In `internal/ui/prlist.go`, change the constant and its doc:

```go
// rowLines is the visual height of one rendered row: a single dense line.
const rowLines = 1
```

In `renderList`, change the inter-row spacing from a blank line to a single newline:

```go
		b.WriteString(m.section.RenderRow(i, RowOpts{
			Width: listW, Focused: i == m.cursor, Selected: m.sel.has(i),
		}))
		b.WriteString("\n")
```

- [ ] **Step 9: Run the full ui suite and fix fallout**

Run: `go test ./internal/ui/...`
Expected: PASS. The existing `TestPRSectionRenderRow`, `TestSetPRsBuildsRows`, `TestViewShowsHeaderAndStatus` assert substrings (`#7`, `hello world`, `●`, header, `q quit`) that the dense row still contains, so they should pass unchanged. If the compiler reports `metaIndent`/`metaTail` unused, delete the now-dead ones (keep `labelNames`/`labelSlice` — still used by `Haystacks`/issue branch).

- [ ] **Step 10: Commit**

```bash
git add internal/ui/section.go internal/ui/prlist.go internal/ui/section_test.go
git commit -m "feat(ui): dense single-line board rows (#5)"
```

---

## Task 2: Side card — identity header + latest-2 comments

Lead the side pane with a PR identity header (`#num title` / author · branch · age), keep the existing blocker card + CI-by-name + reviewers, and fold the timeline to the latest **2** comments.

**Files:**
- Modify: `internal/ui/preview.go` (`previewPane`; add `identityHeader`)
- Modify: `internal/ui/prlist.go` (`NewModel`: `previewN` 3 → 2)
- Test: `internal/ui/preview_test.go`

**Interfaces:**
- Produces: `identityHeader(pr gh.PR) string`.
- Consumes: `gh.PR`, `gh.PRDetail`, existing `renderCard`, `ciLine`, `reviewersLine`, `renderTimeline`, `triage.Compute`.

- [ ] **Step 1: Write the failing test for `identityHeader`**

Add to `internal/ui/preview_test.go`:

```go
func TestIdentityHeader(t *testing.T) {
	pr := gh.PR{Number: 309, Title: "Add retry logic", HeadRefName: "feat/309-retry"}
	pr.Author.Login = "bob"
	out := identityHeader(pr)
	for _, want := range []string{"#309", "Add retry logic", "bob", "feat/309-retry"} {
		if !strings.Contains(out, want) {
			t.Fatalf("identity header missing %q: %q", want, out)
		}
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/ui/ -run TestIdentityHeader`
Expected: FAIL — `undefined: identityHeader`.

- [ ] **Step 3: Implement `identityHeader` and prepend it in `previewPane`**

Add to `internal/ui/preview.go`:

```go
// identityHeader is the side card's top block: number + title, then a dim
// author · branch · age line. The branch anchors the copy/worktree actions.
func identityHeader(pr gh.PR) string {
	line1 := accentStyle.Render(fmt.Sprintf("#%d", pr.Number)) + " " + headerStyle.Render(pr.Title)
	line2 := authorStyle(pr.Author.Login).Render(pr.Author.Login) +
		dimStyle.Render(" · "+pr.HeadRefName+" · "+ageString(pr.UpdatedAt))
	return line1 + "\n" + line2
}
```

In `previewPane`, prepend the header inside the `*PRSection` branch (it has `pr` already):

```go
	if ps, ok := m.section.(*PRSection); ok {
		pr := ps.prAt(m.cursor)
		parts = append(parts, identityHeader(pr))
		if card := renderCard(triage.Compute(pr, d), w); card != "" {
			parts = append(parts, strings.TrimRight(card, "\n"))
		}
		if ci := ciLine(pr); ci != "" {
			parts = append(parts, ci)
		}
	}
```

- [ ] **Step 4: Set `previewN` to 2**

In `internal/ui/prlist.go` `NewModel`, change `previewN: 3` to `previewN: 2`.

- [ ] **Step 5: Run the ui suite**

Run: `go test ./internal/ui/...`
Expected: PASS. `TestRenderPreviewBodyShowsOlderMarker` passes `n=3` explicitly, so it is unaffected.

- [ ] **Step 6: Commit**

```bash
git add internal/ui/preview.go internal/ui/prlist.go internal/ui/preview_test.go
git commit -m "feat(ui): side card leads with identity header, latest-2 comments (#5)"
```

---

## Task 3: `!` conflict/behind column fed from detail

Add the flag glyph and wire it into every PR row from the cached detail. Blank when the PR's detail is not cached (the correctness rule).

**Files:**
- Modify: `internal/ui/preview.go` (add `flagGlyph`)
- Modify: `internal/ui/prlist.go` (`renderList`: per-row flag lookup; `Update` `prDetailMsg`: repaint)
- Test: `internal/ui/preview_test.go`

**Interfaces:**
- Produces: `flagGlyph(d gh.PRDetail, cached bool) string`.
- Consumes: `Model.detail` (`map[int]gh.PRDetail`), `PRSection.prAt`.

- [ ] **Step 1: Write the failing test for `flagGlyph`**

Add to `internal/ui/preview_test.go`:

```go
func TestFlagGlyph(t *testing.T) {
	if flagGlyph(gh.PRDetail{MergeStateStatus: "CLEAN"}, false) != "" {
		t.Fatal("uncached detail must render no flag")
	}
	if !strings.Contains(flagGlyph(gh.PRDetail{MergeStateStatus: "DIRTY"}, true), "⚠") {
		t.Fatal("DIRTY should show the conflict flag")
	}
	if !strings.Contains(flagGlyph(gh.PRDetail{MergeStateStatus: "BEHIND"}, true), "⚠") {
		t.Fatal("BEHIND should show the behind flag")
	}
	if flagGlyph(gh.PRDetail{MergeStateStatus: "CLEAN"}, true) != "" {
		t.Fatal("CLEAN should show no flag")
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/ui/ -run TestFlagGlyph`
Expected: FAIL — `undefined: flagGlyph`.

- [ ] **Step 3: Implement `flagGlyph`**

Add to `internal/ui/preview.go`:

```go
// flagGlyph is the board's ! column: a conflict (red) or behind-base (yellow)
// marker. It is detail-derived — blank unless the PR's detail is cached, so the
// board never guesses a blocker from the unreliable bulk list.
func flagGlyph(d gh.PRDetail, cached bool) string {
	if !cached {
		return ""
	}
	switch {
	case d.MergeStateStatus == "DIRTY" || d.Mergeable == "CONFLICTING":
		return failStyle.Render("⚠")
	case d.MergeStateStatus == "BEHIND":
		return pendStyle.Render("⚠")
	default:
		return ""
	}
}
```

- [ ] **Step 4: Wire the flag into `renderList`**

In `internal/ui/prlist.go` `renderList`, look up the flag per row (only the PR section carries detail):

```go
func (m *Model) renderList() {
	l := computeLayout(m.width, m.height)
	listW := l.ListWidth
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
			Width: listW, Focused: i == m.cursor, Selected: m.sel.has(i), Flag: flag,
		}))
		b.WriteString("\n")
	}
	m.vp.SetWidth(listW)
	m.vp.SetHeight(l.ContentHeight)
	m.vp.SetContent(b.String())
	m.scrollToCursor()
}
```

- [ ] **Step 5: Re-render on new detail**

In `internal/ui/prlist.go` `Update`, the `prDetailMsg` case currently only stores the detail. Repaint so the new flag appears:

```go
	case prDetailMsg:
		m.detail[msg.number] = msg.detail
		m.renderList()
		return m, nil
```

- [ ] **Step 6: Run the ui suite**

Run: `go test ./internal/ui/...`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/ui/preview.go internal/ui/prlist.go internal/ui/preview_test.go
git commit -m "feat(ui): ! column shows conflict/behind from cached detail (#5)"
```

---

## Task 4: Bounded detail prefetch

Fill the `!` column without spamming `gh`: prefetch detail for a bounded window of uncached PRs from the cursor downward.

**Files:**
- Modify: `internal/ui/preview.go` (add `prefetchNumbers`, `prefetchCmd`)
- Modify: `internal/ui/prlist.go` (`Update` `prsFetchedMsg`: also prefetch)
- Test: `internal/ui/preview_test.go`

**Interfaces:**
- Produces: `prefetchNumbers(ps *PRSection, cursor int, detail map[int]gh.PRDetail, window int) []int`; `(m Model) prefetchCmd() tea.Cmd`.
- Consumes: `Model.fetchDetailCmd`, `tea.Batch`.

- [ ] **Step 1: Write the failing test for `prefetchNumbers`**

Add to `internal/ui/preview_test.go`:

```go
func TestPrefetchNumbers(t *testing.T) {
	ps := NewPRSection("is:open")
	ps.SetPRs([]gh.PR{{Number: 1}, {Number: 2}, {Number: 3}, {Number: 4}, {Number: 5}})
	detail := map[int]gh.PRDetail{2: {}} // #2 already cached

	got := prefetchNumbers(ps, 0, detail, 3)
	want := []int{1, 3, 4} // skips cached #2, capped at window=3
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}

	all := map[int]gh.PRDetail{1: {}, 2: {}, 3: {}, 4: {}, 5: {}}
	if n := prefetchNumbers(ps, 0, all, 3); n != nil {
		t.Fatalf("all cached should yield nil, got %v", n)
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/ui/ -run TestPrefetchNumbers`
Expected: FAIL — `undefined: prefetchNumbers`.

- [ ] **Step 3: Implement `prefetchNumbers` and `prefetchCmd`**

Add to `internal/ui/preview.go`:

```go
// prefetchWindow bounds how many uncached PR details we fan out per settle.
const prefetchWindow = 5

// prefetchNumbers returns up to window uncached PR numbers from cursor downward.
func prefetchNumbers(ps *PRSection, cursor int, detail map[int]gh.PRDetail, window int) []int {
	var out []int
	for i := cursor; i < ps.Len() && len(out) < window; i++ {
		num := ps.prAt(i).Number
		if _, cached := detail[num]; cached {
			continue
		}
		out = append(out, num)
	}
	return out
}

// prefetchCmd warms detail for a bounded window of visible PRs so the ! column
// and the side card fill in without a fetch per keystroke.
func (m Model) prefetchCmd() tea.Cmd {
	ps, ok := m.section.(*PRSection)
	if !ok || m.runner == nil {
		return nil
	}
	nums := prefetchNumbers(ps, m.cursor, m.detail, prefetchWindow)
	if len(nums) == 0 {
		return nil
	}
	cmds := make([]tea.Cmd, 0, len(nums))
	for _, n := range nums {
		cmds = append(cmds, m.fetchDetailCmd(n))
	}
	return tea.Batch(cmds...)
}
```

- [ ] **Step 4: Kick off prefetch after a fresh list**

In `internal/ui/prlist.go` `Update`, the `prsFetchedMsg` case ends with `return m, m.detailCmdForCursor()`. Batch in the prefetch:

```go
		return m, tea.Batch(m.detailCmdForCursor(), m.prefetchCmd())
```

(`tea` is already imported in `prlist.go`.)

- [ ] **Step 5: Run the ui suite**

Run: `go test ./internal/ui/...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/ui/preview.go internal/ui/prlist.go internal/ui/preview_test.go
git commit -m "feat(ui): bounded prefetch warms detail for the ! column (#5)"
```

---

## Task 5: Debounced focus fetch

Holding `j/k` must not fire a detail fetch per row. Gate the focus-fetch behind a short tick keyed on a sequence number.

**Files:**
- Modify: `internal/ui/messages.go` (add `detailDebounceMsg`)
- Modify: `internal/ui/prlist.go` (`Model.detailSeq`; `debounceDetailCmd`; movement handlers; `Update` case; `time` import)
- Test: `internal/ui/prlist_test.go`

**Interfaces:**
- Produces: `detailDebounceMsg{seq int}`; `(m Model) debounceDetailCmd() tea.Cmd`; `Model.detailSeq int`.
- Consumes: `tea.Tick`, `detailCmdForCursor`, `prefetchCmd`.

- [ ] **Step 1: Write the failing test for the debounce seq guard**

Add to `internal/ui/prlist_test.go`:

```go
func TestDebounceSeqGuardsStaleTicks(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.setPRs([]gh.PR{{Number: 1}, {Number: 2}, {Number: 3}})
	m.width, m.height = 130, 40
	m.renderList()

	// two quick moves bump the seq to 2
	u, _ := m.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	m = u.(Model)
	u, _ = m.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	m = u.(Model)
	if m.detailSeq != 2 {
		t.Fatalf("detailSeq = %d, want 2", m.detailSeq)
	}

	// a stale tick (seq 1) must do nothing
	_, cmd := m.Update(detailDebounceMsg{seq: 1})
	if cmd != nil {
		t.Fatal("stale debounce tick should yield no command")
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/ui/ -run TestDebounceSeqGuardsStaleTicks`
Expected: FAIL — `undefined: detailDebounceMsg` and `m.detailSeq`.

- [ ] **Step 3: Add the message and the field**

In `internal/ui/messages.go`:

```go
type detailDebounceMsg struct{ seq int }
```

In `internal/ui/prlist.go`, add to the `Model` struct (next to `detail`):

```go
	detailSeq int // bumped on cursor move; gates the debounced detail fetch
```

Add the `time` import to `prlist.go` and the command builder:

```go
// debounceDetailCmd schedules a detail fetch ~150ms out, tagged with the current
// seq so a later move cancels it (the stale tick is ignored on arrival).
func (m Model) debounceDetailCmd() tea.Cmd {
	seq := m.detailSeq
	return tea.Tick(150*time.Millisecond, func(time.Time) tea.Msg {
		return detailDebounceMsg{seq: seq}
	})
}
```

- [ ] **Step 4: Route moves through the debounce and handle the tick**

In `internal/ui/prlist.go` `Update`, change the movement cases to bump the seq and schedule a tick instead of fetching immediately:

```go
		case "tab":
			m.previewExpanded = !m.previewExpanded
			m.detailSeq++
			return m, m.debounceDetailCmd()
		case "down", "j":
			m.moveCursor(1)
			m.detailSeq++
			return m, m.debounceDetailCmd()
		case "up", "k":
			m.moveCursor(-1)
			m.detailSeq++
			return m, m.debounceDetailCmd()
		case "right", "l":
			m.enterExpanded()
			m.detailSeq++
			return m, m.debounceDetailCmd()
```

Add the tick handler alongside the other message cases (e.g. after `prDetailMsg`):

```go
	case detailDebounceMsg:
		if msg.seq != m.detailSeq {
			return m, nil
		}
		return m, tea.Batch(m.detailCmdForCursor(), m.prefetchCmd())
```

- [ ] **Step 5: Run the ui suite**

Run: `go test ./internal/ui/...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/ui/messages.go internal/ui/prlist.go internal/ui/prlist_test.go
git commit -m "feat(ui): debounce per-PR detail fetch on fast j/k (#5)"
```

---

## Task 6: `y` copies URL, `Y` copies branch

Rebind the share action: `y` → Copy URL, add `Y` → Copy branch.

**Files:**
- Modify: `internal/action/defaults.go` (`y` label/builtin; add `Y`)
- Modify: `internal/ui/actions.go` (`runAction` builtin switch; add `clipboardText`)
- Test: `internal/ui/actions_test.go`, `internal/action/defaults_test.go`

**Interfaces:**
- Produces: `clipboardText(builtin string, v action.Vars) string`.
- Consumes: `action.OSC52`, `action.Vars{URL, Branch}`.

- [ ] **Step 1: Write the failing tests**

Add to `internal/action/defaults_test.go`:

```go
func TestCopyActionsRebound(t *testing.T) {
	a := DefaultPRActions()
	if a["y"].Label != "Copy URL" {
		t.Fatalf(`y label = %q, want "Copy URL"`, a["y"].Label)
	}
	if a["y"].Command.Builtin != "copy-url" {
		t.Fatalf(`y builtin = %q, want "copy-url"`, a["y"].Command.Builtin)
	}
	if a["Y"].Command.Builtin != "copy-branch" {
		t.Fatalf(`Y builtin = %q, want "copy-branch"`, a["Y"].Command.Builtin)
	}
}
```

Add to `internal/ui/actions_test.go`:

```go
func TestClipboardText(t *testing.T) {
	v := action.Vars{URL: "https://x/pr/7", Branch: "feat/x"}
	if got := clipboardText("copy-url", v); got != v.URL {
		t.Fatalf("copy-url = %q, want %q", got, v.URL)
	}
	if got := clipboardText("copy-branch", v); got != v.Branch {
		t.Fatalf("copy-branch = %q, want %q", got, v.Branch)
	}
}
```

(`internal/ui/actions_test.go` already imports `github.com/noamsto/prdash/internal/action`; if not, add it.)

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/action/ -run TestCopyActionsRebound` then `go test ./internal/ui/ -run TestClipboardText`
Expected: FAIL — wrong label / `undefined: clipboardText`.

- [ ] **Step 3: Rebind the actions**

In `internal/action/defaults.go`, replace the `"y"` entry and add `"Y"`:

```go
		"y": {Key: "y", Label: "Copy URL",
			Command: Command{Builtin: "copy-url"}, Scope: "single"},
		"Y": {Key: "Y", Label: "Copy branch",
			Command: Command{Builtin: "copy-branch"}, Scope: "single"},
```

- [ ] **Step 4: Implement `clipboardText` and update the builtin switch**

In `internal/ui/actions.go`, add the helper:

```go
// clipboardText is the payload an OSC52 copy action writes for the focused PR.
func clipboardText(builtin string, v action.Vars) string {
	switch builtin {
	case "copy-url":
		return v.URL
	case "copy-branch":
		return v.Branch
	default:
		return ""
	}
}
```

Replace the `case "copy":` arm of the `switch a.Command.Builtin` in `runAction` with:

```go
	case "copy-url", "copy-branch":
		text := clipboardText(a.Command.Builtin, v)
		return func() tea.Msg { print(action.OSC52(text)); return nil }
```

- [ ] **Step 5: Run the suites**

Run: `go test ./internal/ui/... ./internal/action/...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/action/defaults.go internal/ui/actions.go internal/ui/actions_test.go internal/action/defaults_test.go
git commit -m "feat(ui): y copies URL, Y copies branch (#5)"
```

---

## Task 7: Context-aware status bar

Surface the focused PR's recommended fix (from the triage card) at the front of the status bar, so the unblocking key is always one visible keystroke.

**Files:**
- Modify: `internal/ui/prlist.go` (`statusBar`; add `cursorCard`; `triage` import)
- Test: `internal/ui/prlist_test.go`

**Interfaces:**
- Produces: `(m Model) cursorCard() (triage.Card, bool)`.
- Consumes: `triage.Compute`, `PRSection.prAt`, `Model.detail`.

- [ ] **Step 1: Write the failing test**

Add to `internal/ui/prlist_test.go` (add the `triage` import if absent):

```go
func TestStatusBarSurfacesRecommendedFix(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.SetRepo("noamsto/prdash")
	m.width, m.height = 130, 40
	m.setPRs([]gh.PR{{
		Number: 7, Title: "x",
		StatusCheckRollup: []gh.Check{{State: "FAILURE", Name: "lint"}},
	}})
	m.detail[7] = gh.PRDetail{MergeStateStatus: "BLOCKED"}
	m.renderList()
	out := m.statusBar()
	if !strings.Contains(out, "rerun failed") {
		t.Fatalf("failing-checks PR should surface the rerun fix: %q", out)
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/ui/ -run TestStatusBarSurfacesRecommendedFix`
Expected: FAIL — the static bar has no "rerun failed".

- [ ] **Step 3: Add `cursorCard` and update `statusBar`**

In `internal/ui/prlist.go`, add the `triage` import (`"github.com/noamsto/prdash/internal/triage"`) and the helper:

```go
// cursorCard is the triage card for the focused PR, when its detail is cached.
func (m Model) cursorCard() (triage.Card, bool) {
	ps, ok := m.section.(*PRSection)
	if !ok || m.section.Len() == 0 {
		return triage.Card{}, false
	}
	d, cached := m.detail[ps.prAt(m.cursor).Number]
	if !cached {
		return triage.Card{}, false
	}
	return triage.Compute(ps.prAt(m.cursor), d), true
}
```

Replace `statusBar` with a version that prepends the recommended fix:

```go
func (m Model) statusBar() string {
	keys := "↑↓ move · → expand · z max · f filter · F author · R reviewers · / find · a actions · space select · q quit"
	prefix := ""
	if n := m.sel.count(); n > 0 {
		prefix = selMarkStyle.Render(fmt.Sprintf("%d selected", n)) + " · "
	}
	if card, ok := m.cursorCard(); ok && card.ActionKey != "" {
		prefix += accentStyle.Render(card.ActionKey) + " " + card.ActionLabel + " · "
	}
	return statusBarStyle.Render("  " + prefix + keys)
}
```

- [ ] **Step 4: Run the ui suite**

Run: `go test ./internal/ui/...`
Expected: PASS. `TestViewShowsHeaderAndStatus` still finds `q quit` (the PR there has no cached detail, so no prefix is added).

- [ ] **Step 5: Commit**

```bash
git add internal/ui/prlist.go internal/ui/prlist_test.go
git commit -m "feat(ui): status bar surfaces the focused PR's recommended fix (#5)"
```

---

## Task 8: Full verification

**Files:** none (verification only).

- [ ] **Step 1: Full test run**

Run: `go test ./...`
Expected: PASS across all packages.

- [ ] **Step 2: Vet + build**

Run: `go vet ./...` then `go build ./...`
Expected: no output, clean build.

- [ ] **Step 3: Manual smoke (real terminal, ≥120 cols)**

Run prdash in a real repo and confirm: dense one-line rows; focusing a PR fills its `!` flag and the side card (identity → blocker → checks → reviewers → latest 2); fast `j/k` does not stutter; `y` copies the URL, `Y` the branch; the status bar shows the focused PR's fix key. (TUI behavior isn't unit-testable; this step is the gate.)

- [ ] **Step 4: Commit any fixes**

```bash
git add -A
git commit -m "chore(ui): layout sweep verification fixes (#5)"
```

---

## Self-Review

**Spec coverage:**
- Dense board rows → Task 1. `!` reliable/detail-derived rule → Tasks 3 (render) + 4 (prefetch). CI/review reliable from list → Task 1 (`ciGlyph`/`reviewDot` from `gh.PR`).
- Side card blocker-led, checks-by-name, reviewers, latest-2 → Task 2 (+ existing `renderCard`/`ciLine`/`reviewersLine`/`renderTimeline`).
- 45/55 split & 120 threshold → unchanged (`computeLayout`); no task needed.
- Context-aware action bar → Task 7.
- `y`→URL, `Y`→branch → Task 6.
- Snappy: cached-first (existing `m.detail` check in `previewPane`/`detailCmdForCursor`) + debounce → Task 5 + prefetch → Task 4.
- Expanded view unchanged, inherits `y` rebinding → no task (binding lives in the shared `actions` map).
- Spec deviation noted: the spec's "compact section headers" does not apply — the model holds a single `Section` at a time, so there is no simultaneous PR/Issue grouping to header. Recorded in Architecture above.

**Placeholder scan:** none — every code step shows the actual code.

**Type consistency:** `RowOpts.Flag` (Task 1) consumed in `renderList` (Task 3). `flagGlyph(d, cached)` defined Task 3, signature matches Task 4 usage via `m.detail` lookup. `prefetchNumbers`/`prefetchCmd` (Task 4) consume `fetchDetailCmd` (existing). `detailDebounceMsg{seq}` + `detailSeq` consistent across Task 5. `clipboardText` builtin strings (`copy-url`/`copy-branch`) match `defaults.go` (Task 6). `cursorCard` returns `triage.Card` consumed by `statusBar` (Task 7).
