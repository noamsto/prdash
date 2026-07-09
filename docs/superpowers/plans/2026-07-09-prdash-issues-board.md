# Issues Board Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Wire the already-built issue layers into the TUI as a first-class board the user toggles into from the PR list with `i`.

**Architecture:** Add a `mode` axis (`"pr"|"issue"`) to the Model. `state`, `preset`, `section`, and `actions` become functions of the mode; `i` swaps them and re-fetches (cached → instant). The existing `Section` interface already renders both row kinds, so no new list/board chrome is needed — only fetch/cache/detail twins and a mode-aware preview branch.

**Tech Stack:** Go 1.26, bubbletea/v2, lipgloss/v2, glamour/v2 (via `internal/preview`), `gh` CLI, the repo's on-disk `internal/cache`.

## Global Constraints

- Spec: `docs/superpowers/specs/2026-07-09-prdash-issues-view-design.md` — every task traces to it.
- `i` toggles `pr ⟷ issue`. `s`/`f`/`/`/`space`/`V`/`enter`/`W`/`o`/`y`/`Y`/`b` and nav work in both modes.
- Issue states cycle **open · closed** only (no "merged"). Issue presets: **mine (`assignee:@me`) · all**.
- Issue "mine" is a **single** search — never wire `mineFetchCmd`/`setMine` for issues.
- Expanded view (`l`/`right`) is **disabled** in issue mode (v1). `backgroundRefresh` stays PR-only.
- PR and issue caches must never collide (distinct `cache.Key` kind prefixes).
- `gh.IssueDetail` ships now with `Body string` and an empty `Timeline []TimelineItem` (both new) so the comments timeline can land later with no structural change.
- Follow existing style: flat fields on `Model`, table tests in the `_test.go` files, `slog.Debug` on cache-unmarshal failure. Run `gofmt` before every commit.
- Verify: `go build ./... && go test ./...` from the worktree root.

---

### Task 1: `gh.IssueDetail` — issue detail fetch/parse (grow-path stub included)

**Files:**
- Create: `internal/gh/issuedetail.go`
- Test: `internal/gh/issuedetail_test.go`

**Interfaces:**
- Consumes: nothing (leaf).
- Produces:
  - `type gh.TimelineItem struct{}` (empty stub for the future comments timeline)
  - `type gh.IssueDetail struct { Body string; Timeline []TimelineItem }`
  - `func gh.IssueViewArgs(number int) []string`
  - `func gh.ParseIssueDetail(b []byte) (IssueDetail, error)`

- [ ] **Step 1: Write the failing test**

```go
package gh

import "testing"

func TestParseIssueDetail(t *testing.T) {
	d, err := ParseIssueDetail([]byte(`{"body":"## Hello\nworld"}`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if d.Body != "## Hello\nworld" {
		t.Errorf("body = %q", d.Body)
	}
	if d.Timeline != nil {
		t.Errorf("Timeline should be empty in v1, got %v", d.Timeline)
	}
}

func TestIssueViewArgs(t *testing.T) {
	got := IssueViewArgs(42)
	want := []string{"issue", "view", "42", "--json", "body"}
	if len(got) != len(want) {
		t.Fatalf("args = %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("args[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/gh/ -run 'IssueDetail|IssueViewArgs' -v`
Expected: FAIL — `undefined: ParseIssueDetail` / `undefined: IssueViewArgs`.

- [ ] **Step 3: Write the implementation**

Create `internal/gh/issuedetail.go`:

```go
package gh

import (
	"encoding/json"
	"strconv"
)

// TimelineItem is the placeholder for the future issue comments/events timeline.
// Defined empty now so IssueDetail's shape is stable when the timeline lands.
type TimelineItem struct{}

type IssueDetail struct {
	Body     string         `json:"body"`
	Timeline []TimelineItem `json:"-"` // populated by a later milestone, not fetched yet
}

func IssueViewArgs(number int) []string {
	return []string{"issue", "view", strconv.Itoa(number), "--json", "body"}
}

func ParseIssueDetail(b []byte) (IssueDetail, error) {
	var d IssueDetail
	err := json.Unmarshal(b, &d)
	return d, err
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/gh/ -run 'IssueDetail|IssueViewArgs' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/gh/issuedetail.go internal/gh/issuedetail_test.go
git add internal/gh/issuedetail.go internal/gh/issuedetail_test.go
git commit -m "feat(gh): add IssueDetail fetch/parse (#22)"
```

---

### Task 2: Mode-keyed states & presets

**Files:**
- Modify: `internal/ui/filter_presets.go`
- Modify (callers): `internal/ui/prlist.go` (`NewModel` at :72, `s` handler at :848, `f` handler at :842, `confirmPicker` at :562)
- Test: `internal/ui/filter_presets_test.go`

**Interfaces:**
- Consumes: nothing new.
- Produces:
  - `var issueStates = []string{"open", "closed"}`
  - `var issuePresets = []filterPreset{{"mine", "assignee:@me"}, {"all", ""}}`
  - `func statesFor(mode string) []string`
  - `func presetsFor(mode string) []filterPreset`
  - `func nextState(s string, states []string) string` *(signature changed — now takes the state list)*
  - `func splitState(search string, states []string) (state, body string)` *(signature changed)*
  - `func nextPreset(i int, presets []filterPreset) int` *(signature changed)*
  - `func presetIndexFor(body string, presets []filterPreset) int` *(signature changed)*

- [ ] **Step 1: Write the failing test**

Add to `internal/ui/filter_presets_test.go`:

```go
func TestStatesFor(t *testing.T) {
	if got := statesFor("issue"); len(got) != 2 || got[0] != "open" || got[1] != "closed" {
		t.Errorf("issue states = %v", got)
	}
	if got := statesFor("pr"); len(got) != 3 {
		t.Errorf("pr states = %v", got)
	}
}

func TestNextStateIssueWraps(t *testing.T) {
	st := statesFor("issue")
	if got := nextState("open", st); got != "closed" {
		t.Errorf("open -> %q, want closed", got)
	}
	if got := nextState("closed", st); got != "open" {
		t.Errorf("closed -> %q, want open (wrap)", got)
	}
}

func TestIssueMinePreset(t *testing.T) {
	ps := presetsFor("issue")
	i := presetIndexFor("assignee:@me", ps)
	if i < 0 || ps[i].name != "mine" {
		t.Errorf("issue mine preset not found: idx=%d", i)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/ -run 'StatesFor|NextStateIssue|IssueMinePreset' -v`
Expected: FAIL — `undefined: statesFor` and signature mismatches.

- [ ] **Step 3: Rewrite `filter_presets.go`**

Replace the bodies of `splitState`, `nextState`, `nextPreset`, `presetIndexFor` with the parameterized versions and add the issue tables + selectors:

```go
package ui

import "strings"

type filterPreset struct{ name, search string }

// prStates / issueStates are the `s`-toggle cycles per board. Order = cycle order.
var prStates = []string{"open", "merged", "closed"}
var issueStates = []string{"open", "closed"}

// searchFor composes a gh search from a state and an optional body qualifier.
func searchFor(state, body string) string {
	s := "is:" + state
	if body == "" {
		return s
	}
	return s + " " + body
}

// splitState strips a leading is:<state> token, returning the state (default
// states[0] when none is present) and the remaining body. Inverse of searchFor.
func splitState(search string, states []string) (state, body string) {
	search = strings.TrimSpace(search)
	for _, s := range states {
		tok := "is:" + s
		if search == tok {
			return s, ""
		}
		if rest, ok := strings.CutPrefix(search, tok+" "); ok {
			return s, strings.TrimSpace(rest)
		}
	}
	return states[0], search
}

// nextState returns the state after s in states, wrapping; unknown → first.
func nextState(s string, states []string) string {
	for i, st := range states {
		if st == s {
			return states[(i+1)%len(states)]
		}
	}
	return states[0]
}

// mineBody / reviewBody are the two state-agnostic qualifiers the PR "mine" view
// combines. Issues use a single assignee qualifier (assigneeBody), no dual fetch.
const (
	mineBody     = "author:@me"
	reviewBody   = "review-requested:@me"
	assigneeBody = "assignee:@me"
)

var defaultPresets = []filterPreset{
	{"mine", mineBody},
	{"all", ""},
}

var issuePresets = []filterPreset{
	{"mine", assigneeBody},
	{"all", ""},
}

// statesFor / presetsFor select the cycle tables for the active board mode.
func statesFor(mode string) []string {
	if mode == "issue" {
		return issueStates
	}
	return prStates
}

func presetsFor(mode string) []filterPreset {
	if mode == "issue" {
		return issuePresets
	}
	return defaultPresets
}

// nextPreset returns the index after i, wrapping to 0.
func nextPreset(i int, presets []filterPreset) int { return (i + 1) % len(presets) }

// presetIndexFor returns the index of the preset whose body equals body, or -1
// when it is a custom (author) query.
func presetIndexFor(body string, presets []filterPreset) int {
	for i, p := range presets {
		if p.search == body {
			return i
		}
	}
	return -1
}
```

- [ ] **Step 4: Update the PR-side callers to pass the tables**

In `internal/ui/prlist.go`:

`NewModel` (currently `:77-78`):
```go
	state, body := splitState(filter, prStates)
	resolved := searchFor(state, body)
```
and its return field (currently `:86`): `presetIdx: presetIndexFor(body, defaultPresets), refreshing: true,`

`s` handler (currently `:848-851`):
```go
		case "s":
			m.state = nextState(m.state, statesFor(m.mode))
			m.filter = searchFor(m.state, m.body)
			return m, m.switchToFilter()
```

`f` handler (currently `:842-847`):
```go
		case "f":
			ps := presetsFor(m.mode)
			m.presetIdx = nextPreset(max(m.presetIdx, 0), ps)
			m.body = ps[m.presetIdx].search
			m.filter = searchFor(m.state, m.body)
			return m, m.switchToFilter()
```

`confirmPicker` author branch (currently `:563`): `m.presetIdx = -1` stays as-is (author filter is PR-only; picker keys are guarded to PR mode in Task 4).

Update `isMineView` (currently `:1073-1075`) to be PR-only so issue "mine" never triggers the dual fetch:
```go
func (m Model) isMineView() bool {
	return m.mode == "pr" && m.presetIdx >= 0 && defaultPresets[m.presetIdx].name == "mine"
}
```
Update the header label (currently `:1029-1030`):
```go
	if m.presetIdx >= 0 {
		label = presetsFor(m.mode)[m.presetIdx].name
	}
```

> `m.mode` does not exist yet — Task 3 adds it and initializes it to `"pr"`. Do Task 3's field addition before compiling. If implementing strictly task-by-task, temporarily hardcode `"pr"` here and switch to `m.mode` in Task 3; the tests in this task don't touch `m.mode`.

- [ ] **Step 5: Run tests + build**

Run: `go test ./internal/ui/ -run 'StatesFor|NextStateIssue|IssueMinePreset' -v`
Expected: PASS.
Run: `go build ./...`
Expected: builds (with the `m.mode` note above resolved).

- [ ] **Step 6: Commit**

```bash
gofmt -w internal/ui/filter_presets.go internal/ui/filter_presets_test.go internal/ui/prlist.go
git add internal/ui/filter_presets.go internal/ui/filter_presets_test.go internal/ui/prlist.go
git commit -m "feat(ui): mode-keyed state/preset cycles (#22)"
```

---

### Task 3: Issue fetch, cache, and mode-aware hydrate/switch

**Files:**
- Modify: `internal/ui/messages.go` (add `issuesFetchedMsg`)
- Modify: `internal/ui/section.go` (add `IssueSection.issueAt`)
- Modify: `internal/ui/prlist.go` (Model fields; `issueKey`/`cachedIssues`/`setIssues`/`issueFetchCmd`; mode-aware `hydrate`/`switchToFilter`; `issuesFetchedMsg` handler)
- Test: `internal/ui/prlist_test.go`

**Interfaces:**
- Consumes: `statesFor`/`presetsFor`/`presetIndexFor` (Task 2), `gh.FetchIssues`/`gh.IssueListArgs`/`gh.ParseIssues` (existing).
- Produces:
  - Model fields: `mode string`, `other boardView`, `issueDetail map[int]gh.IssueDetail`, `issueFresh map[int]bool`
  - `type boardView struct { state, body, filter string; presetIdx int }`
  - `func issueKey(repo, filter string) string`
  - `func (m *Model) cachedIssues(filter string) ([]gh.Issue, bool)`
  - `func (m *Model) setIssues(is []gh.Issue)`
  - `func (m Model) issueFetchCmd(filter string) tea.Cmd`
  - `func (s *IssueSection) issueAt(i int) gh.Issue`
  - `issuesFetchedMsg{filter string; issues []gh.Issue; raw []byte}`

- [ ] **Step 1: Write the failing test**

Add to `internal/ui/prlist_test.go`:

```go
func TestIssueKeyDistinctFromPRKey(t *testing.T) {
	if issueKey("r", "is:open") == prKey("r", "is:open") {
		t.Error("issue and pr cache keys collide")
	}
}

func TestIssuesFetchedPopulatesRows(t *testing.T) {
	m := NewModel(".", "is:open", nil)
	m.mode = "issue"
	m.section = NewIssueSection("is:open")
	m.filter = "is:open"
	out, _ := m.Update(issuesFetchedMsg{
		filter: "is:open",
		issues: []gh.Issue{{Number: 7, Title: "bug"}, {Number: 9, Title: "feat"}},
	})
	got := out.(Model)
	if got.section.Len() != 2 {
		t.Errorf("rows = %d, want 2", got.section.Len())
	}
	if got.refreshing {
		t.Error("refreshing should clear after fetch")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/ -run 'IssueKeyDistinct|IssuesFetchedPopulates' -v`
Expected: FAIL — `undefined: issueKey`, `undefined: issuesFetchedMsg`.

- [ ] **Step 3: Add the message + section accessor**

In `internal/ui/messages.go`:
```go
type issuesFetchedMsg struct {
	filter string
	issues []gh.Issue
	raw    []byte
}
```

In `internal/ui/section.go`, next to `prAt` (`:81`):
```go
func (s *IssueSection) issueAt(i int) gh.Issue { return s.issues[s.shown[i]] }
```

- [ ] **Step 4: Add Model fields + `boardView` + init**

In `internal/ui/prlist.go` `Model` struct, add:
```go
	mode        string // "pr" | "issue"; the i-toggle dimension
	other       boardView // the inactive board's saved state/preset (restored on toggle-back)
	issueDetail map[int]gh.IssueDetail
	issueFresh  map[int]bool // issue numbers whose body was refetched this session
```

Add near the top of the file:
```go
// boardView is the per-mode selection saved across an i-toggle so flipping back
// lands on the same state/preset the user left.
type boardView struct {
	state, body, filter string
	presetIdx           int
}
```

In `NewModel`, set `mode` and seed the issue board's saved view (so the first `i` opens issues on "open · mine"):
```go
	return Model{
		dir: dir, filter: resolved, state: state, body: body, mode: "pr",
		other: boardView{
			state: "open", body: assigneeBody, filter: searchFor("open", assigneeBody),
			presetIdx: 0, // issuePresets[0] == "mine"
		},
		cache: c, section: NewPRSection(resolved),
		vp: viewport.New(), filterInput: ti, actionFilter: af,
		actions: action.DefaultPRActions(),
		detail:  map[int]gh.PRDetail{}, fresh: map[int]bool{},
		issueDetail: map[int]gh.IssueDetail{}, issueFresh: map[int]bool{},
		previewN:  2,
		presetIdx: presetIndexFor(body, defaultPresets), refreshing: true,
	}
```

- [ ] **Step 5: Add issue cache key, cached read, setter, fetch cmd**

In `internal/ui/prlist.go`, next to `prKey`/`cachedPRs` (`:255-271`):
```go
// issueSchemaVer is bumped whenever issueFields changes shape.
const issueSchemaVer = "v1"

// issueKey scopes the cached issue list by repo, kind-prefixed "issue" so it can
// never collide with the "pr" list cache for the same filter.
func issueKey(repo, filter string) string {
	return cache.Key("issue", repo+"\x00"+filter, defaultLimit, issueSchemaVer)
}

func (m *Model) cachedIssues(filter string) ([]gh.Issue, bool) {
	e, ok := m.cache.Get(issueKey(m.repo, filter))
	if !ok {
		return nil, false
	}
	var is []gh.Issue
	if err := json.Unmarshal(e.Rows, &is); err != nil {
		slog.Debug("issue cache unmarshal failed", "err", err)
		return nil, false
	}
	return is, true
}
```

Next to `setPRs` (`:92`):
```go
func (m *Model) setIssues(is []gh.Issue) {
	if s, ok := m.section.(*IssueSection); ok {
		s.SetIssues(is)
	}
	m.applyFilter()
	if n := m.section.Len(); m.cursor >= n {
		m.cursor = max(0, n-1)
	}
}
```

Next to `fetchCmd` (`:358`):
```go
// issueFetchCmd runs `gh issue list` for filter (gh excludes PRs by default).
func (m Model) issueFetchCmd(filter string) tea.Cmd {
	r, dir := m.runner, m.dir
	return func() tea.Msg {
		raw, err := r.Run(dir, gh.IssueListArgs(filter, defaultLimit)...)
		if err != nil {
			return fetchFailedMsg{err: err, filter: filter}
		}
		is, err := gh.ParseIssues(raw)
		if err != nil {
			return fetchFailedMsg{err: err, filter: filter}
		}
		return issuesFetchedMsg{filter: filter, issues: is, raw: raw}
	}
}
```

- [ ] **Step 6: Make `hydrate` and `switchToFilter` mode-aware**

Replace `hydrate` (`:275`) — add the issue branch at the top:
```go
func (m *Model) hydrate() bool {
	if m.cache == nil {
		return false
	}
	if m.mode == "issue" {
		is, ok := m.cachedIssues(m.filter)
		if !ok {
			return false
		}
		m.setIssues(is)
		m.hydrateIssueDetail()
		return true
	}
	if m.isMineView() {
		// ... unchanged PR path ...
```

Replace `switchToFilter` (`:493`) so the fetch dispatch and empty-row reset branch on mode:
```go
func (m *Model) switchToFilter() tea.Cmd {
	m.cursor = 0
	m.sel.clear()
	m.refreshing = true
	hit := m.hydrate()
	m.loaded = hit
	if m.mode == "issue" {
		if !hit {
			m.setIssues(nil)
		}
		return tea.Batch(m.issueFetchCmd(m.filter), m.startSpinner())
	}
	if !hit {
		m.setPRs(nil)
	}
	fetch := m.fetchCmd(m.filter)
	if m.isMineView() {
		fetch = m.mineFetchCmd()
	}
	return tea.Batch(fetch, m.startSpinner())
}
```

Add a stub `hydrateIssueDetail` (filled in Task 5; empty body is safe now):
```go
// hydrateIssueDetail paints each shown issue's body from the disk cache so the
// preview never opens on a bare Loading…. Filled in with the detail cache in Task 5.
func (m *Model) hydrateIssueDetail() {}
```

- [ ] **Step 7: Handle `issuesFetchedMsg` in `Update`**

In `internal/ui/prlist.go` `Update`, next to the `prsFetchedMsg` case (`:607`):
```go
	case issuesFetchedMsg:
		if m.cache != nil && msg.raw != nil {
			m.cache.Set(issueKey(m.repo, msg.filter), msg.raw)
		}
		if msg.filter != "" && msg.filter != m.filter {
			return m, nil // background prewarm of another issue filter
		}
		m.refreshing = false
		m.loaded = true
		m.sel.clear()
		m.setIssues(msg.issues)
		if m.expanded && m.section.Len() == 0 {
			m.expanded = false
		}
		return m, m.detailCmdForCursor()
```

- [ ] **Step 8: Run tests + build**

Run: `go test ./internal/ui/ -run 'IssueKeyDistinct|IssuesFetchedPopulates' -v`
Expected: PASS.
Run: `go build ./... && go test ./...`
Expected: all pass.

- [ ] **Step 9: Commit**

```bash
gofmt -w internal/ui/prlist.go internal/ui/messages.go internal/ui/section.go internal/ui/prlist_test.go
git add internal/ui/prlist.go internal/ui/messages.go internal/ui/section.go internal/ui/prlist_test.go
git commit -m "feat(ui): issue fetch/cache + mode-aware hydrate/switch (#22)"
```

---

### Task 4: The `i` toggle — board swap, view-state reset, key guards

**Files:**
- Modify: `internal/ui/prlist.go` (`toggleMode`; `i` key; guards on `F`/`R`/`D`/`right`/`l`)
- Test: `internal/ui/prlist_test.go`

**Interfaces:**
- Consumes: `boardView`, `setIssues`/`setPRs`, `switchToFilter` (Task 3); `action.DefaultIssueActions`/`action.DefaultPRActions` (existing).
- Produces: `func (m *Model) toggleMode() tea.Cmd`.

- [ ] **Step 1: Write the failing test**

Add to `internal/ui/prlist_test.go`:

```go
func TestToggleModeSwapsBoard(t *testing.T) {
	m := NewModel(".", "is:open author:@me", nil)
	m.cursor = 3
	m.previewExpanded = true
	m.previewMax = true
	m.hideDrafts = true

	out, _ := m.Update(tea.KeyPressMsg{Code: 'i', Text: "i"})
	got := out.(Model)

	if got.mode != "issue" {
		t.Fatalf("mode = %q, want issue", got.mode)
	}
	if got.section.Kind() != "issue" {
		t.Errorf("section kind = %q", got.section.Kind())
	}
	if _, ok := got.actions["m"]; ok {
		t.Error("issue actions should not contain merge key 'm'")
	}
	if got.cursor != 0 || got.previewExpanded || got.previewMax || got.hideDrafts {
		t.Error("view state not reset on toggle")
	}

	back, _ := got.Update(tea.KeyPressMsg{Code: 'i', Text: "i"})
	b := back.(Model)
	if b.mode != "pr" || b.section.Kind() != "pr" {
		t.Errorf("toggle back failed: mode=%q kind=%q", b.mode, b.section.Kind())
	}
	if b.filter != "is:open author:@me" {
		t.Errorf("pr filter not restored: %q", b.filter)
	}
}

func TestPROnlyKeysInertInIssueMode(t *testing.T) {
	m := NewModel(".", "is:open", nil)
	m.mode = "issue"
	m.section = NewIssueSection("is:open")
	m.hideDrafts = false
	// D must not flip hideDrafts in issue mode.
	out, _ := m.Update(tea.KeyPressMsg{Code: 'D', Text: "D"})
	if out.(Model).hideDrafts {
		t.Error("D toggled drafts in issue mode")
	}
}
```

> Confirm the key-message type: existing key handling switches on `msg.String()` (`prlist.go:733`). Use whatever `tea.KeyMsg` constructor the other tests in `prlist_test.go` already use — match them exactly rather than guessing. If they build keys differently, mirror that.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/ -run 'ToggleModeSwaps|PROnlyKeysInert' -v`
Expected: FAIL — `undefined: toggleMode` / `i` unhandled.

- [ ] **Step 3: Implement `toggleMode`**

In `internal/ui/prlist.go`, near `switchToFilter`:
```go
// toggleMode flips the board between PRs and issues: it saves the active board's
// selection, restores the other's, swaps the section + action set, resets all
// per-item/preview view state, and re-fetches (cached → instant).
func (m *Model) toggleMode() tea.Cmd {
	cur := boardView{state: m.state, body: m.body, filter: m.filter, presetIdx: m.presetIdx}
	m.state, m.body, m.filter, m.presetIdx = m.other.state, m.other.body, m.other.filter, m.other.presetIdx
	m.other = cur

	if m.mode == "pr" {
		m.mode = "issue"
		m.section = NewIssueSection(m.filter)
		m.actions = action.DefaultIssueActions()
	} else {
		m.mode = "pr"
		m.section = NewPRSection(m.filter)
		m.actions = action.DefaultPRActions()
	}

	// Reset view state so nothing from the other board leaks through.
	m.previewExpanded = false
	m.previewMax = false
	m.previewOffset = 0
	m.hideDrafts = false
	m.expanded = false
	m.err = nil
	m.detailSeq++ // cancel any in-flight detail debounce/fetch for the old board

	return m.switchToFilter() // resets cursor + selection, hydrates, fetches
}
```

- [ ] **Step 4: Wire the `i` key + guards**

In the main key switch (`:839+`), add after `case "s":`:
```go
		case "i":
			return m, m.toggleMode()
```

Guard the PR-only cases so they no-op in issue mode. `case "D":` (`:861`), `case "F":` (`:869`), `case "R":` (`:871`) each get a leading:
```go
			if m.mode != "pr" {
				return m, nil
			}
```

Guard expanded entry — `case "right", "l":` (`:908`):
```go
		case "right", "l":
			if m.mode != "pr" {
				return m, nil // expanded view is PR-only in v1
			}
			m.enterExpanded()
			m.detailSeq++
			return m, m.debounceDetailCmd()
```

- [ ] **Step 5: Run tests + build**

Run: `go test ./internal/ui/ -run 'ToggleModeSwaps|PROnlyKeysInert' -v`
Expected: PASS.
Run: `go build ./... && go test ./...`
Expected: all pass.

- [ ] **Step 6: Commit**

```bash
gofmt -w internal/ui/prlist.go internal/ui/prlist_test.go
git add internal/ui/prlist.go internal/ui/prlist_test.go
git commit -m "feat(ui): i toggles the issues board (#22)"
```

---

### Task 5: Issue preview — body in the side pane

**Files:**
- Modify: `internal/ui/preview.go` (`issueDetailKey`, `fetchIssueDetailCmd`, `detailCmdForCursor` issue branch, `previewPane` issue branch, `identityHeaderIssue`)
- Modify: `internal/ui/messages.go` (add `issueDetailMsg`)
- Modify: `internal/ui/prlist.go` (`issueDetailMsg` handler; fill `hydrateIssueDetail`)
- Test: `internal/ui/preview_test.go`

**Interfaces:**
- Consumes: `gh.IssueDetail`/`gh.IssueViewArgs`/`gh.ParseIssueDetail` (Task 1); `IssueSection.issueAt` (Task 3); `preview.Render` (existing).
- Produces:
  - `func issueDetailKey(repo string, number int) string`
  - `func (m Model) fetchIssueDetailCmd(number int) tea.Cmd`
  - `func (m Model) issuePreviewPane(is *IssueSection, w, bw int) string`
  - `func identityHeaderIssue(is gh.Issue) string`
  - `issueDetailMsg{number int; detail gh.IssueDetail; raw []byte}`

- [ ] **Step 1: Write the failing test**

Add to `internal/ui/preview_test.go`:

```go
func TestIssuePreviewRendersBody(t *testing.T) {
	m := NewModel(".", "is:open", nil)
	m.mode = "issue"
	m.width, m.height = 120, 40
	sec := NewIssueSection("is:open")
	sec.SetIssues([]gh.Issue{{Number: 5, Title: "Crash on save", Author: struct {
		Login string `json:"login"`
	}{Login: "octocat"}}})
	m.section = sec
	m.issueDetail[5] = gh.IssueDetail{Body: "Steps to reproduce"}

	pane := m.previewPane()
	if !strings.Contains(pane, "#5") || !strings.Contains(pane, "Steps to reproduce") {
		t.Errorf("issue preview missing number/body:\n%s", pane)
	}
}

func TestIssueDetailMsgStores(t *testing.T) {
	m := NewModel(".", "is:open", nil)
	m.mode = "issue"
	m.section = NewIssueSection("is:open")
	out, _ := m.Update(issueDetailMsg{number: 5, detail: gh.IssueDetail{Body: "b"}})
	got := out.(Model)
	if got.issueDetail[5].Body != "b" || !got.issueFresh[5] {
		t.Error("issue detail not stored / not marked fresh")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/ -run 'IssuePreviewRendersBody|IssueDetailMsgStores' -v`
Expected: FAIL — `undefined: issueDetailMsg`; preview shows no body.

- [ ] **Step 3: Add the message + detail cache key + fetch cmd**

In `internal/ui/messages.go`:
```go
type issueDetailMsg struct {
	number int
	detail gh.IssueDetail
	raw    []byte
}
```

In `internal/ui/preview.go`, next to `detailKey` (`:29`):
```go
// issueDetailSchemaVer is bumped whenever IssueViewArgs' --json field set changes.
const issueDetailSchemaVer = "v1"

func issueDetailKey(repo string, number int) string {
	return cache.Key("issuedetail", repo+"#"+strconv.Itoa(number), 0, issueDetailSchemaVer)
}

func (m Model) fetchIssueDetailCmd(number int) tea.Cmd {
	r, dir := m.runner, m.dir
	return func() tea.Msg {
		raw, err := r.Run(dir, gh.IssueViewArgs(number)...)
		if err != nil {
			return fetchFailedMsg{err: err}
		}
		d, err := gh.ParseIssueDetail(raw)
		if err != nil {
			return fetchFailedMsg{err: err}
		}
		return issueDetailMsg{number: number, detail: d, raw: raw}
	}
}
```

- [ ] **Step 4: Route `detailCmdForCursor` by mode**

Replace `detailCmdForCursor` (`:51`):
```go
func (m *Model) detailCmdForCursor() tea.Cmd {
	if m.runner == nil {
		return nil
	}
	v, ok := m.cursorVars()
	if !ok {
		return nil
	}
	switch m.section.Kind() {
	case "issue":
		if m.issueFresh[v.Number] {
			return nil
		}
		return m.fetchIssueDetailCmd(v.Number)
	case "pr":
		if m.fresh[v.Number] {
			return nil
		}
		return m.fetchDetailCmd(v.Number)
	}
	return nil
}
```

- [ ] **Step 5: Add the issue preview branch + identity header**

In `internal/ui/preview.go` `previewPane` (`:162`), immediately after the `w`/`bw`/`section` closure are set up and before the `if ps, ok := m.section.(*PRSection)` block, add:
```go
	if is, ok := m.section.(*IssueSection); ok {
		return m.issuePreviewPane(is, w, bw)
	}
```

Add the method + header (reuse `section` styling from `sectionRule`):
```go
// issuePreviewPane renders the issue identity header + its markdown body. The
// body is the whole v1 story; the comments timeline lands in a later milestone.
func (m Model) issuePreviewPane(is *IssueSection, w, bw int) string {
	iss := is.issueAt(m.cursor)
	blocks := []string{identityHeaderIssue(iss)}
	d, cached := m.issueDetail[iss.Number]
	if !cached {
		blocks = append(blocks, dimStyle.Render("  loading details…"))
		return strings.Join(blocks, "\n\n")
	}
	body, err := preview.Render(d.Body, bw)
	if err != nil {
		body = d.Body
	}
	blocks = append(blocks, sectionRule("body", w)+"\n"+indentLines(strings.TrimRight(body, "\n"), 2))
	return strings.Join(blocks, "\n\n")
}

// identityHeaderIssue mirrors identityHeader for issues (no branch/head ref line).
func identityHeaderIssue(is gh.Issue) string {
	line1 := accentStyle.Render(fmt.Sprintf("#%d", is.Number)) + " " + headerStyle.Render(is.Title)
	line2 := authorStyle(is.Author.Login).Render(is.Author.Login) +
		dimStyle.Render(" · "+ageString(is.UpdatedAt))
	return line1 + "\n" + line2
}
```

> Verify `indentLines` exists (used by the PR `section` closure at `:173`). If its name differs, reuse the exact helper the PR path uses so indentation matches.

- [ ] **Step 6: Handle `issueDetailMsg` + fill `hydrateIssueDetail`**

In `internal/ui/prlist.go` `Update`, next to the `prDetailMsg` case (`:656`):
```go
	case issueDetailMsg:
		m.issueDetail[msg.number] = msg.detail
		m.issueFresh[msg.number] = true
		if m.cache != nil && msg.raw != nil {
			m.cache.Set(issueDetailKey(m.repo, msg.number), msg.raw)
		}
		m.renderList()
		return m, nil
```

Fill `hydrateIssueDetail` (stubbed in Task 3):
```go
func (m *Model) hydrateIssueDetail() {
	if m.cache == nil {
		return
	}
	is, ok := m.section.(*IssueSection)
	if !ok {
		return
	}
	for i := 0; i < is.Len(); i++ {
		num := is.issueAt(i).Number
		if _, ok := m.issueDetail[num]; ok {
			continue
		}
		e, hit := m.cache.Get(issueDetailKey(m.repo, num))
		if !hit {
			continue
		}
		var d gh.IssueDetail
		if err := json.Unmarshal(e.Rows, &d); err != nil {
			slog.Debug("issue detail cache unmarshal failed", "err", err)
			continue
		}
		m.issueDetail[num] = d
	}
}
```

- [ ] **Step 7: Run tests + build**

Run: `go test ./internal/ui/ -run 'IssuePreviewRendersBody|IssueDetailMsgStores' -v`
Expected: PASS.
Run: `go build ./... && go test ./...`
Expected: all pass.

- [ ] **Step 8: Commit**

```bash
gofmt -w internal/ui/preview.go internal/ui/messages.go internal/ui/prlist.go internal/ui/preview_test.go
git add internal/ui/preview.go internal/ui/messages.go internal/ui/prlist.go internal/ui/preview_test.go
git commit -m "feat(ui): issue preview renders the issue body (#22)"
```

---

### Task 6: Pretty header + issue copy actions + mode-aware chrome

**Files:**
- Modify: `internal/action/defaults.go` (add `y`/`Y`/`b` to `DefaultIssueActions`)
- Modify: `internal/ui/actions.go` (`copiedLabel` generic noun)
- Modify: `internal/ui/prlist.go` (`modeSegments` in header; empty-state text; mode-aware `navHints`/legend)
- Test: `internal/action/defaults_test.go`, `internal/ui/prlist_test.go`

**Interfaces:**
- Consumes: `m.mode`, `m.section.Kind()`.
- Produces: `func modeSegments(active string) string`; `func navHintsFor(mode string) []keyHint`.

- [ ] **Step 1: Write the failing tests**

Add to `internal/action/defaults_test.go`:
```go
func TestIssueActionsHaveCopy(t *testing.T) {
	a := DefaultIssueActions()
	for _, k := range []string{"y", "Y", "b"} {
		if _, ok := a[k]; !ok {
			t.Errorf("issue actions missing copy key %q", k)
		}
	}
}
```

Add to `internal/ui/prlist_test.go`:
```go
func TestModeSegmentsHighlightsActive(t *testing.T) {
	pr := modeSegments("pr")
	is := modeSegments("issue")
	if pr == is {
		t.Error("segments identical across modes")
	}
	if !strings.Contains(pr, "PRs") || !strings.Contains(pr, "Issues") {
		t.Errorf("segments missing a label: %q", pr)
	}
}

func TestEmptyStateSaysIssues(t *testing.T) {
	m := NewModel(".", "is:open", nil)
	m.mode = "issue"
	m.section = NewIssueSection("is:open")
	m.width, m.height = 120, 40
	m.loaded = true
	m.renderList()
	if !strings.Contains(m.vp.View(), "issues") {
		t.Errorf("empty state should mention issues:\n%s", m.vp.View())
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/action/ -run IssueActionsHaveCopy -v`
Run: `go test ./internal/ui/ -run 'ModeSegments|EmptyStateSaysIssues' -v`
Expected: FAIL.

- [ ] **Step 3: Add copy actions to `DefaultIssueActions`**

In `internal/action/defaults.go`, extend the returned map:
```go
		"y": {Key: "y", Label: "Copy issue #",
			Command: Command{Builtin: "copy-number"}, Scope: "single"},
		"Y": {Key: "Y", Label: "Copy URL",
			Command: Command{Builtin: "copy-url"}, Scope: "single"},
		"b": {Key: "b", Label: "Copy branch",
			Command: Command{Builtin: "copy-branch"}, Scope: "single"},
```

- [ ] **Step 4: Make `copiedLabel` mode-neutral**

In `internal/ui/actions.go` `copiedLabel` (`:47`), change the `copy-number` noun so it's correct for both boards:
```go
	case "copy-number":
		noun, plural = "number", "numbers"
```

- [ ] **Step 5: Segmented mode indicator in the header**

In `internal/ui/prlist.go`, add:
```go
// modeSegments renders the "PRs │ Issues" board switch, the active one lit.
func modeSegments(active string) string {
	seg := func(name, mode string) string {
		if mode == active {
			return accentStyle.Bold(true).Render(name)
		}
		return dimStyle.Render(name)
	}
	return seg("PRs", "pr") + dimStyle.Render(" │ ") + seg("Issues", "issue")
}
```

Update `header` (`:1032`) to insert the segments after the repo:
```go
	h := headerStyle.Render("  "+m.repo) + "  " + modeSegments(m.mode) +
		dimStyle.Render(fmt.Sprintf("   %s · %s · %d", label, m.state, m.section.Len()))
```

- [ ] **Step 6: Kind-aware empty state**

In `renderList` (`:194-197`):
```go
		hint := "Loading…"
		if m.loaded {
			noun := "PRs"
			if m.section.Kind() == "issue" {
				noun = "issues"
			}
			hint = fmt.Sprintf("No %s %s.", m.state, noun)
		}
```

- [ ] **Step 7: Mode-aware nav hints + legend**

Replace the static `navHints` var (`:1112`) with a selector and update its consumers to call `navHintsFor(m.mode)`:
```go
// navHintsFor is the docked-panel cheatsheet for the active board. Issue mode
// drops the PR-only author/reviewer/drafts hints; both modes show the i-toggle.
func navHintsFor(mode string) []keyHint {
	base := []keyHint{
		{"↑↓", "move"}, {"i", "PRs/Issues"}, {"f", "filter"}, {"s", "state"},
		{"/", "find"}, {"space", "select"}, {"V", "all"}, {"q", "quit"},
	}
	if mode == "pr" {
		pr := []keyHint{
			{"→", "expand"}, {"z", "max"}, {"ctrl+j/k", "scroll"},
			{"F", "author"}, {"R", "reviewers"}, {"D", "drafts"},
		}
		return append(base, pr...)
	}
	return base
}
```
Find every reference to `navHints` (grep `navHints`) and replace with `navHintsFor(m.mode)`. In `legendView` (`:1098`), gate the PR-only key row behind `if m.mode == "pr"` and add an `i` entry to the shared row (e.g. prepend `accentStyle.Render("i") + statusBarStyle.Render(" PRs/Issues   ")`).

- [ ] **Step 8: Run tests + build**

Run: `go test ./internal/action/ -run IssueActionsHaveCopy -v`
Run: `go test ./internal/ui/ -run 'ModeSegments|EmptyStateSaysIssues' -v`
Expected: PASS.
Run: `go build ./... && go test ./...`
Expected: all pass.

- [ ] **Step 9: Commit**

```bash
gofmt -w internal/action/defaults.go internal/action/defaults_test.go internal/ui/actions.go internal/ui/prlist.go internal/ui/prlist_test.go
git add internal/action/defaults.go internal/action/defaults_test.go internal/ui/actions.go internal/ui/prlist.go internal/ui/prlist_test.go
git commit -m "feat(ui): segmented board header + issue copy actions (#22)"
```

---

### Task 7: Prewarm issues at startup + full verification

**Files:**
- Modify: `internal/ui/prlist.go` (`Init` prewarm)
- Manual: run the app.

**Interfaces:**
- Consumes: `issueFetchCmd` (Task 3).

- [ ] **Step 1: Prewarm the default issue view in `Init`**

In `Init` (`:587`), add the issue "mine/open" prewarm so the first `i` paints from cache:
```go
	return tea.Batch(
		m.mineFetchCmd(),
		m.fetchCmd("is:open"),
		m.issueFetchCmd(searchFor("open", assigneeBody)),
		m.fetchMembersCmd(),
		spinnerTick(),
		themeWatchTick(m.themeModTime),
	)
```

- [ ] **Step 2: Full build + test + format check**

Run: `go build ./... && go test ./... && gofmt -l internal/`
Expected: build + tests pass; `gofmt -l` prints nothing.

- [ ] **Step 3: Manual smoke test**

Run the app against a repo with open issues (use the `run` skill or the README's launch command). Verify:
- `i` flips PRs → Issues; header shows `Issues` lit, `PRs` dim; list shows issue rows.
- `s` cycles open ⟷ closed (no "merged"); `f` cycles mine ⟷ all.
- Moving the cursor loads the issue body into the side pane.
- `y`/`Y`/`b`/`o`/`enter`/`W` work; `F`/`R`/`D` and `l`/`right` do nothing.
- `i` flips back to PRs on the same state/preset you left; PR preview/expanded still work.

- [ ] **Step 4: Commit**

```bash
gofmt -w internal/ui/prlist.go
git add internal/ui/prlist.go
git commit -m "feat(ui): prewarm the issues board at startup (#22)"
```

- [ ] **Step 5: Push + open PR**

```bash
git push -u origin feat/22-issues-board
gh pr create --assignee @me --title "feat(ui): issues board — toggle from the PR list with i (#22)" --body "Closes #22. Wires the issue layers into the TUI as a toggle-able board with a light body preview, per docs/superpowers/specs/2026-07-09-prdash-issues-view-design.md."
```

---

## Self-Review

**Spec coverage:**
- Board mode / `i` toggle → Task 4. ✔
- Mode-dependent state/preset (open·closed / mine=`assignee:@me`·all) → Task 2. ✔
- View-state reset on toggle (cursor, sel, previewExpanded, previewMax, previewOffset, hideDrafts, detailSeq, err) → Task 4 `toggleMode`. ✔
- PR-only keys inert; `l`/expanded disabled in issue mode → Task 4. ✔
- Copy actions added to `DefaultIssueActions`; `copiedLabel` fixed → Task 6. ✔
- Issue fetch + `issueKey` (distinct prefix) + mode-aware hydrate/switch (`refreshing`/`loaded`) → Task 3. ✔
- `backgroundRefresh` stays PR-only → untouched (constraint noted). ✔
- Light issue body preview, lazy-fetched; `gh.IssueDetail` with `Body` + empty `Timeline` → Tasks 1, 5. ✔
- `mineFetchCmd` not wired for issues; `isMineView` PR-only → Task 2/3. ✔
- Pretty segmented header; mode-aware legend/panel; kind-aware empty state → Task 6. ✔
- Tests per the spec's Testing section → each task's Step 1. ✔

**Placeholder scan:** No "TBD"/"handle edge cases"/"similar to". The three `>` notes (m.mode ordering in Task 2, key-msg constructor in Task 4, `indentLines` name in Task 5) are explicit verification instructions against the real code, not deferred work.

**Type consistency:** `boardView` fields (`state,body,filter,presetIdx`) used identically in `NewModel`, `toggleMode`. `issuesFetchedMsg`/`issueDetailMsg` field names match their producers (`issueFetchCmd`/`fetchIssueDetailCmd`) and handlers. `issueDetail map[int]gh.IssueDetail` / `issueFresh map[int]bool` consistent across Tasks 3/5. `statesFor`/`presetsFor`/`nextState`/`splitState`/`nextPreset`/`presetIndexFor` signatures match every call site updated in Task 2.
