# Merged/Closed PRs + Live Summary Refresh Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let the TUI browse merged/closed PRs under the existing filters via an `s` state toggle, and refetch the list + PR summary after any action that mutates a PR.

**Architecture:** Split the open/merged/closed state out of the preset search string into an orthogonal `Model.state` dimension composed with a state-agnostic `Model.body`; the resolved `m.filter` (`is:<state> <body>`) stays the cache key + fetch arg. For refresh, tag mutating actions with `Refresh`, carry the affected PR numbers on `actionStat`, and on action success clear those numbers' detail-freshness and call the existing `backgroundRefresh()`.

**Tech Stack:** Go 1.26, charm.land/bubbletea/v2 (Elm-style Model/Update/View), `gh` CLI via `internal/gh`.

## Global Constraints

- Go module `github.com/noamsto/prdash`; package under test is `internal/ui` and `internal/action`.
- `m.filter` must remain the fully-resolved gh search string (`is:<state> <body>`) — it is the cache key (`prKey(repo, filter)`), the `gh pr list --search` arg, and drives `presetIndexFor`.
- Run tests from the worktree root: `go test ./internal/ui/ ./internal/action/`.
- Follow existing style: lean diffs, comments only for non-obvious WHY, early returns, no speculative robustness.
- All work happens in the worktree `~/Data/git/noamsto/prdash-worktrees/feat-show-merged-live-summary` (branch `feat/show-merged-live-summary`), already rebased on latest `main` (commit #14).

---

## File Structure

- `internal/ui/filter_presets.go` — MODIFY. Presets store a state-agnostic body; add `searchFor`, `splitState`, `nextState`, `prStates`; body-key `presetIndexFor`. (Task 1 adds pure helpers; Task 2 flips presets/constants.)
- `internal/ui/filter_presets_test.go` — MODIFY. Cover the new pure helpers.
- `internal/ui/prlist.go` — MODIFY. `Model.state`/`Model.body` fields; `NewModel` seed; `s` key; recompute at `f`/author picker; state-aware `mineFetchCmd`/`mineFetchedMsg`/`hydrate`; header shows state. (Task 2)
- `internal/ui/messages.go` — MODIFY. `mineFetchedMsg` gains `state string`. (Task 2)
- `internal/ui/perf_actions_test.go` — MODIFY. Rewrite the mine-prewarm guard test; add `s`-toggle + state-cache tests. (Task 2)
- `internal/action/action.go` — MODIFY. Add `Refresh bool` to `Action`. (Task 3)
- `internal/action/defaults.go` — MODIFY. Set `Refresh: true` on `m`/`u`/`M`/`r`. (Task 3)
- `internal/action/defaults_test.go` — MODIFY. Assert `Refresh` flags. (Task 3)
- `internal/ui/actions.go` — MODIFY. `actionStat` gains `refresh`/`nums`; set in `runAction`/`runBulk`; `assignReviewersCmd` clears freshness. (Task 4)
- `internal/ui/expanded.go` — MODIFY. `rerunHoveredCheck`/`rerunAllFailedChecks` set `refresh`/`nums`. (Task 4)
- `internal/ui/prlist.go` — MODIFY (again). `actionDoneMsg` handler refetches on mutating success. (Task 4)
- `internal/ui/perf_actions_test.go` — MODIFY (again). Refresh-on-action tests. (Task 4)

---

## Task 1: State primitives in filter_presets.go (pure, additive)

Add the pure helpers with no behavior change yet, so they can be unit-tested in isolation. The existing `mineFilter`/`reviewFilter`/`defaultPresets`/`presetIndexFor` stay untouched in this task — Task 2 flips them.

**Files:**
- Modify: `internal/ui/filter_presets.go`
- Test: `internal/ui/filter_presets_test.go`

**Interfaces:**
- Produces: `prStates []string`; `searchFor(state, body string) string`; `splitState(search string) (state, body string)`; `nextState(s string) string`.

- [ ] **Step 1: Write failing tests**

Append to `internal/ui/filter_presets_test.go`:

```go
func TestSearchFor(t *testing.T) {
	cases := []struct{ state, body, want string }{
		{"open", "author:@me", "is:open author:@me"},
		{"merged", "author:@me", "is:merged author:@me"},
		{"open", "", "is:open"},
		{"closed", "", "is:closed"},
	}
	for _, c := range cases {
		if got := searchFor(c.state, c.body); got != c.want {
			t.Errorf("searchFor(%q,%q)=%q want %q", c.state, c.body, got, c.want)
		}
	}
}

func TestSplitState(t *testing.T) {
	cases := []struct{ in, state, body string }{
		{"is:open author:@me", "open", "author:@me"},
		{"is:merged author:@me", "merged", "author:@me"},
		{"is:open", "open", ""},
		{"is:closed", "closed", ""},
		{"author:@me", "open", "author:@me"}, // no state token → default open
	}
	for _, c := range cases {
		state, body := splitState(c.in)
		if state != c.state || body != c.body {
			t.Errorf("splitState(%q)=(%q,%q) want (%q,%q)", c.in, state, body, c.state, c.body)
		}
	}
}

func TestNextStateWraps(t *testing.T) {
	if got := nextState("open"); got != "merged" {
		t.Fatalf("nextState(open)=%q want merged", got)
	}
	if got := nextState("closed"); got != "open" {
		t.Fatalf("nextState(closed)=%q want open (wrap)", got)
	}
	if got := nextState("bogus"); got != "open" {
		t.Fatalf("nextState(bogus)=%q want open (fallback)", got)
	}
}
```

- [ ] **Step 2: Run tests, verify they fail**

Run: `cd ~/Data/git/noamsto/prdash-worktrees/feat-show-merged-live-summary && go test ./internal/ui/ -run 'TestSearchFor|TestSplitState|TestNextStateWraps'`
Expected: FAIL — `undefined: searchFor` (etc.), build error.

- [ ] **Step 3: Add the pure helpers**

Add to `internal/ui/filter_presets.go` (top-level, below the existing consts; ensure `strings` is imported):

```go
// prStates is the PR state cycle for the `s` toggle. Order = cycle order.
var prStates = []string{"open", "merged", "closed"}

// searchFor composes a gh search from a state (open/merged/closed) and an
// optional body qualifier (e.g. "author:@me"). Empty body yields "is:<state>".
func searchFor(state, body string) string {
	s := "is:" + state
	if body == "" {
		return s
	}
	return s + " " + body
}

// splitState strips a leading is:<state> token, returning the state (default
// "open" when none is present) and the remaining body. Inverse of searchFor.
func splitState(search string) (state, body string) {
	search = strings.TrimSpace(search)
	for _, s := range prStates {
		tok := "is:" + s
		if search == tok {
			return s, ""
		}
		if rest, ok := strings.CutPrefix(search, tok+" "); ok {
			return s, strings.TrimSpace(rest)
		}
	}
	return "open", search
}

// nextState returns the state after s in prStates, wrapping; unknown → first.
func nextState(s string) string {
	for i, st := range prStates {
		if st == s {
			return prStates[(i+1)%len(prStates)]
		}
	}
	return prStates[0]
}
```

- [ ] **Step 4: Run tests, verify they pass**

Run: `cd ~/Data/git/noamsto/prdash-worktrees/feat-show-merged-live-summary && go test ./internal/ui/ -run 'TestSearchFor|TestSplitState|TestNextStateWraps'`
Expected: PASS. Also `go build ./...` succeeds.

- [ ] **Step 5: Commit**

```bash
cd ~/Data/git/noamsto/prdash-worktrees/feat-show-merged-live-summary
git add internal/ui/filter_presets.go internal/ui/filter_presets_test.go
git commit -m "feat(ui): add open/merged/closed state search primitives"
```

---

## Task 2: Wire state as an orthogonal dimension

Flip presets/constants to bodies, add `Model.state`/`Model.body`, rewire every consumer, and add the `s` key + state-aware header/mine view. This is one atomic migration (a half-renamed constant won't compile), so it lands together.

**Files:**
- Modify: `internal/ui/filter_presets.go`, `internal/ui/prlist.go`, `internal/ui/messages.go`
- Test: `internal/ui/perf_actions_test.go`

**Interfaces:**
- Consumes: `searchFor`, `splitState`, `nextState`, `prStates` (Task 1).
- Produces: `Model.state string`, `Model.body string`; `mineBody`/`reviewBody` consts; `mineFetchedMsg.state string`; `presetIndexFor(body string) int` (body-keyed).

- [ ] **Step 1: Write the failing behavior tests**

In `internal/ui/perf_actions_test.go`, **replace** `TestBackgroundFetchCachesWithoutClobbering` (the mine-prewarm guard test) and add two new tests:

```go
func TestBackgroundFetchCachesWithoutClobbering(t *testing.T) {
	c := cache.Open(filepath.Join(t.TempDir(), "c.json"))
	m := NewModel("/repo", "is:open author:@me", c)
	m.SetRepo("x")
	m.width, m.height = 120, 30
	m.setPRs([]gh.PR{{Number: 1, Title: "mine"}})
	m.loaded = true

	other := "is:open review-requested:@me"
	raw, _ := json.Marshal([]gh.PR{{Number: 50}})
	u, _ := m.Update(prsFetchedMsg{filter: other, prs: []gh.PR{{Number: 50}}, raw: raw})
	m = u.(Model)

	ps := m.section.(*PRSection)
	if len(ps.prs) != 1 || ps.prs[0].Number != 1 {
		t.Fatalf("background fetch clobbered the current view: %+v", ps.prs)
	}
	if _, ok := c.Get(prKey("x", other)); !ok {
		t.Fatal("background fetch did not populate the cache")
	}
}

func TestStateToggleRecomputesFilter(t *testing.T) {
	m := NewModel("/repo", "is:open author:@me", nil)
	m.SetRepo("x")
	m.width, m.height = 120, 30
	m.setPRs([]gh.PR{{Number: 1}})
	m.renderList()

	u, _ := m.Update(tea.KeyPressMsg{Code: 's', Text: "s"})
	m = u.(Model)
	if m.state != "merged" || m.filter != "is:merged author:@me" {
		t.Fatalf("s toggle: state=%q filter=%q", m.state, m.filter)
	}
	u, _ = m.Update(tea.KeyPressMsg{Code: 's', Text: "s"})
	m = u.(Model)
	if m.state != "closed" || m.filter != "is:closed author:@me" {
		t.Fatalf("second s toggle: state=%q filter=%q", m.state, m.filter)
	}
}

func TestMineFetchedCachesPerState(t *testing.T) {
	c := cache.Open(filepath.Join(t.TempDir(), "c.json"))
	m := NewModel("/repo", "is:open author:@me", c) // mine view, open
	m.SetRepo("x")
	m.width, m.height = 120, 30
	m.loaded = true

	// A merged-state mine result arriving while viewing open: cache only, no repaint.
	mineRaw, _ := json.Marshal([]gh.PR{{Number: 7}})
	revRaw, _ := json.Marshal([]gh.PR{})
	u, _ := m.Update(mineFetchedMsg{state: "merged", mine: []gh.PR{{Number: 7}}, mineRaw: mineRaw, reviewRaw: revRaw})
	m = u.(Model)

	if _, ok := c.Get(prKey("x", "is:merged author:@me")); !ok {
		t.Fatal("merged mine result not cached under its per-state key")
	}
	if ps := m.section.(*PRSection); ps.Len() != 0 {
		t.Fatalf("merged prewarm should not repaint the open view, got %d rows", ps.Len())
	}
}
```

- [ ] **Step 2: Run tests, verify they fail**

Run: `cd ~/Data/git/noamsto/prdash-worktrees/feat-show-merged-live-summary && go test ./internal/ui/ -run 'TestStateToggle|TestMineFetchedCachesPerState|TestBackgroundFetchCachesWithoutClobbering'`
Expected: FAIL — `mineFetchedMsg{state: ...}` unknown field / no `s` handler / `m.state` undefined.

- [ ] **Step 3: Flip filter_presets.go to bodies**

In `internal/ui/filter_presets.go`, replace the constants, presets, and `presetIndexFor`:

```go
// mineBody / reviewBody are the two state-agnostic qualifiers the "mine" view
// combines. searchFor(state, body) resolves them to a gh search per state.
const (
	mineBody   = "author:@me"
	reviewBody = "review-requested:@me"
)

var defaultPresets = []filterPreset{
	{"mine", mineBody},
	{"all", ""}, // empty body → is:<state>
}
```

And change `presetIndexFor` to match on body (its `filterPreset.search` field now holds a body):

```go
// presetIndexFor returns the index of the preset whose body equals body, or -1
// when it is a custom (author) query.
func presetIndexFor(body string) int {
	for i, p := range defaultPresets {
		if p.search == body {
			return i
		}
	}
	return -1
}
```

(Delete the old `mineFilter`/`reviewFilter` consts and the old `is:open`-based presets. Keep `nextPreset` as-is.)

- [ ] **Step 4: Add Model.state/Model.body and seed them in NewModel**

In `internal/ui/prlist.go`, add two fields to the `Model` struct (next to `filter`):

```go
	filter          string
	state           string // open | merged | closed; the s-toggle dimension
	body            string // state-agnostic qualifier (e.g. "author:@me", "")
```

Replace `NewModel`'s return to seed them via `splitState`:

```go
func NewModel(dir, filter string, c *cache.Cache) Model {
	ti := textinput.New()
	ti.Prompt = "/"
	af := textinput.New()
	af.Prompt = "› "
	state, body := splitState(filter)
	resolved := searchFor(state, body)
	return Model{
		dir: dir, filter: resolved, state: state, body: body,
		cache: c, section: NewPRSection(resolved),
		vp: viewport.New(), filterInput: ti, actionFilter: af,
		actions: action.DefaultPRActions(), detail: map[int]gh.PRDetail{}, fresh: map[int]bool{}, previewN: 2,
		presetIdx: presetIndexFor(body), refreshing: true,
	}
}
```

- [ ] **Step 5: Add the `s` key and update the `f` key**

In the list key-handler switch (`internal/ui/prlist.go`), update `case "f"` and add `case "s"` right after it:

```go
		case "f":
			// presetIdx is -1 for a custom (author) filter; max(...,0) makes f resume from "mine".
			m.presetIdx = nextPreset(max(m.presetIdx, 0))
			m.body = defaultPresets[m.presetIdx].search
			m.filter = searchFor(m.state, m.body)
			return m, m.switchToFilter()
		case "s":
			m.state = nextState(m.state)
			m.filter = searchFor(m.state, m.body)
			return m, m.switchToFilter()
```

- [ ] **Step 6: Make the author picker set body**

In `confirmPicker` (`internal/ui/prlist.go`, the `case "author"` branch), replace the filter build:

```go
		slices.Sort(terms)
		m.body = strings.Join(terms, " ")
		m.filter = searchFor(m.state, m.body)
		m.presetIdx = -1
		return m.switchToFilter()
```

- [ ] **Step 7: Make the mine fetch/cache/hydrate state-aware**

In `mineFetchCmd` (`internal/ui/prlist.go`), resolve both halves per current state and tag errors with the resolved filter:

```go
func (m Model) mineFetchCmd() tea.Cmd {
	r, dir := m.runner, m.dir
	state := m.state
	mineF, reviewF := searchFor(state, mineBody), searchFor(state, reviewBody)
	list := func(filter string) ([]gh.PR, []byte, error) {
		raw, err := r.Run(dir, gh.PRListArgs(filter, defaultLimit)...)
		if err != nil {
			return nil, nil, err
		}
		prs, err := gh.ParsePRs(raw)
		return prs, raw, err
	}
	return func() tea.Msg {
		mine, mineRaw, err := list(mineF)
		if err != nil {
			return fetchFailedMsg{err: err, filter: mineF}
		}
		rev, revRaw, err := list(reviewF)
		if err != nil {
			return fetchFailedMsg{err: err, filter: mineF}
		}
		return mineFetchedMsg{state: state, mine: mine, mineRaw: mineRaw, review: rev, reviewRaw: revRaw}
	}
}
```

Add the `state` field to `mineFetchedMsg` in `internal/ui/messages.go`:

```go
type mineFetchedMsg struct {
	state              string // the PR state (open/merged/closed) this result is for
	mine, review       []gh.PR
	mineRaw, reviewRaw []byte
}
```

Update the `case mineFetchedMsg` handler (`internal/ui/prlist.go`) to cache per-state and guard on view+state:

```go
	case mineFetchedMsg:
		if m.cache != nil {
			m.cache.Set(prKey(m.repo, searchFor(msg.state, mineBody)), msg.mineRaw)
			m.cache.Set(prKey(m.repo, searchFor(msg.state, reviewBody)), msg.reviewRaw)
		}
		if !m.isMineView() || msg.state != m.state {
			return m, nil // prewarm while viewing another preset/state
		}
		m.refreshing = false
		m.loaded = true
		m.sel.clear()
		m.setMine(msg.mine, msg.review)
		if m.expanded && m.section.Len() == 0 {
			m.expanded = false
		}
		return m, tea.Batch(m.detailCmdForCursor(), m.prefetchCmd(), m.maybeStartPoll())
```

Update the mine branch of `hydrate` (`internal/ui/prlist.go`) to read per-state keys:

```go
	if m.isMineView() {
		mine, ok1 := m.cachedPRs(searchFor(m.state, mineBody))
		rev, ok2 := m.cachedPRs(searchFor(m.state, reviewBody))
		if !ok1 && !ok2 {
			return false
		}
		m.setMine(mine, rev)
		m.hydrateDetail()
		return true
	}
```

- [ ] **Step 8: Show state in the header**

In `header` (`internal/ui/prlist.go`), use body for the custom-filter label and add the state segment (drop the hardcoded "open"):

```go
func (m Model) header() string {
	label := m.body
	if m.presetIdx >= 0 {
		label = defaultPresets[m.presetIdx].name
	}
	h := headerStyle.Render("  "+m.repo) + dimStyle.Render(fmt.Sprintf("   %s · %s · %d", label, m.state, m.section.Len()))
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

- [ ] **Step 9: Fix the remaining `mineFilter` reference in perf_actions_test.go**

`TestMineViewSections` calls `NewModel("/repo", mineFilter, nil)`. Change the arg to the resolved string:

```go
	m := NewModel("/repo", "is:open author:@me", nil)
```

- [ ] **Step 10: Add the `s` key to the legend + docked panel**

In `legendView` (`internal/ui/prlist.go`), add an `s state` entry to the row that already has `f filter`:

```go
		accentStyle.Render("f") + statusBarStyle.Render(" filter   ") + accentStyle.Render("s") + statusBarStyle.Render(" state   ") + accentStyle.Render("F") + statusBarStyle.Render(" author   ") + accentStyle.Render("R") + statusBarStyle.Render(" reviewers   ") + accentStyle.Render("D") + statusBarStyle.Render(" drafts"),
```

And add `{"s", "state"}` to `navHints` (the docked-panel cheatsheet) next to `{"f", "filter"}`:

```go
	{"f", "filter"}, {"s", "state"}, {"F", "author"}, {"R", "reviewers"}, {"/", "find"},
```

- [ ] **Step 11: Build and run the full ui test suite**

Run: `cd ~/Data/git/noamsto/prdash-worktrees/feat-show-merged-live-summary && go build ./... && go test ./internal/ui/`
Expected: PASS (all existing + the three new/updated tests). Fix any leftover `mineFilter`/`reviewFilter`/`presetIndexFor(fullstring)` compile errors surfaced by the build.

- [ ] **Step 12: Commit**

```bash
cd ~/Data/git/noamsto/prdash-worktrees/feat-show-merged-live-summary
git add internal/ui/
git commit -m "feat(ui): s toggles PR state (open/merged/closed) across all filters"
```

---

## Task 3: Mark mutating actions with Refresh

**Files:**
- Modify: `internal/action/action.go`, `internal/action/defaults.go`
- Test: `internal/action/defaults_test.go`

**Interfaces:**
- Produces: `action.Action.Refresh bool` — true for `m`, `u`, `M`, `r`.

- [ ] **Step 1: Write the failing test**

Append to `internal/action/defaults_test.go`:

```go
func TestMutatingActionsMarkedRefresh(t *testing.T) {
	a := DefaultPRActions()
	for _, k := range []string{"m", "u", "M", "r"} {
		if !a[k].Refresh {
			t.Errorf("action %q should be Refresh:true", k)
		}
	}
	for _, k := range []string{"y", "Y", "b", "o", "enter"} {
		if a[k].Refresh {
			t.Errorf("non-mutating action %q should not be Refresh", k)
		}
	}
}
```

- [ ] **Step 2: Run test, verify it fails**

Run: `cd ~/Data/git/noamsto/prdash-worktrees/feat-show-merged-live-summary && go test ./internal/action/ -run TestMutatingActionsMarkedRefresh`
Expected: FAIL — `a[k].Refresh undefined (type Action has no field Refresh)`.

- [ ] **Step 3: Add the field**

In `internal/action/action.go`, add to the `Action` struct (after `Confirm`):

```go
	Confirm  bool
	Refresh  bool // action mutates the PR; the UI refetches list+detail on success
```

- [ ] **Step 4: Set it on the mutating defaults**

In `internal/action/defaults.go`, add `Refresh: true` to the `m`, `r`, `u`, `M` entries. For example:

```go
		"m": {Key: "m", Label: "Merge (squash)",
			Command: Command{Argv: []string{"gh", "pr", "merge", "{{.Number}}", "--squash"}},
			Confirm: true, Scope: "per-selected", Refresh: true,
			Progress: "Merging", Past: "Merged", Fail: "Merge failed"},
		"r": {Key: "r", Label: "Rerun checks",
			Command: Command{Builtin: "rerun-failed"}, Scope: "single", Refresh: true,
			Progress: "Rerunning checks", Past: "Checks rerun", Fail: "Rerun failed"},
		"u": {Key: "u", Label: "Update branch",
			Command: Command{Argv: []string{"gh", "pr", "update-branch", "{{.Number}}"}}, Scope: "per-selected", Refresh: true,
			Progress: "Updating branch", Past: "Branch updated", Fail: "Update failed"},
		"M": {Key: "M", Label: "Mark ready",
			Command: Command{Argv: []string{"gh", "pr", "ready", "{{.Number}}"}}, Scope: "per-selected", Refresh: true,
			Progress: "Marking ready", Past: "Marked ready", Fail: "Mark-ready failed"},
```

- [ ] **Step 5: Run tests, verify they pass**

Run: `cd ~/Data/git/noamsto/prdash-worktrees/feat-show-merged-live-summary && go test ./internal/action/`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
cd ~/Data/git/noamsto/prdash-worktrees/feat-show-merged-live-summary
git add internal/action/
git commit -m "feat(action): flag mutating PR actions with Refresh"
```

---

## Task 4: Refetch list + summary after mutating actions

Carry the affected PR numbers + refresh intent on `actionStat`; on `actionDoneMsg` success, clear those numbers' detail-freshness and call `backgroundRefresh()`. Wire the list, bulk, expanded-rerun, and reviewer paths.

**Files:**
- Modify: `internal/ui/actions.go`, `internal/ui/expanded.go`, `internal/ui/prlist.go`
- Test: `internal/ui/perf_actions_test.go`

**Interfaces:**
- Consumes: `Action.Refresh` (Task 3), `Model.backgroundRefresh()` (existing), `Model.fresh` (existing).
- Produces: `actionStat.refresh bool`, `actionStat.nums []int`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/ui/perf_actions_test.go`:

```go
func TestMutatingActionRefetchesAndRevalidates(t *testing.T) {
	m := NewModel("/repo", "is:open author:@me", nil)
	m.SetRepo("x")
	m.SetRunner(stubRunner{}) // returns "[]"; backgroundRefresh just needs non-nil
	m.width, m.height = 120, 30
	m.setPRs([]gh.PR{{Number: 42}})
	m.renderList()
	m.fresh[42] = true
	m.actionStatus = &actionStat{run: "Updating", ok: "Updated", fail: "Failed", refresh: true, nums: []int{42}}

	u, cmd := m.Update(actionDoneMsg{})
	m = u.(Model)

	if m.fresh[42] {
		t.Fatal("successful mutating action should clear detail freshness for #42")
	}
	if !m.refreshing {
		t.Fatal("successful mutating action should trigger a refetch (refreshing=true)")
	}
	if cmd == nil {
		t.Fatal("expected a refetch command batch")
	}
}

func TestFailedMutatingActionDoesNotRefetch(t *testing.T) {
	m := NewModel("/repo", "is:open author:@me", nil)
	m.SetRepo("x")
	m.SetRunner(stubRunner{})
	m.width, m.height = 120, 30
	m.setPRs([]gh.PR{{Number: 42}})
	m.fresh[42] = true
	m.actionStatus = &actionStat{run: "Updating", ok: "Updated", fail: "Failed", refresh: true, nums: []int{42}}

	u, _ := m.Update(actionDoneMsg{err: fmt.Errorf("boom")})
	m = u.(Model)

	if !m.fresh[42] {
		t.Fatal("failed action must not clear freshness")
	}
	if m.refreshing {
		t.Fatal("failed action must not refetch")
	}
}
```

`stubRunner` already exists in `internal/ui/perf_actions_test.go` (`Run` returns `[]byte("[]")`); the tests only need `m.runner != nil` so `backgroundRefresh` produces a command.

- [ ] **Step 2: Run tests, verify they fail**

Run: `cd ~/Data/git/noamsto/prdash-worktrees/feat-show-merged-live-summary && go test ./internal/ui/ -run 'TestMutatingActionRefetches|TestFailedMutatingActionDoesNotRefetch'`
Expected: FAIL — `actionStat` has no field `refresh`/`nums`, and the handler doesn't refetch.

- [ ] **Step 3: Add refresh/nums to actionStat**

In `internal/ui/actions.go`, extend the `actionStat` struct:

```go
type actionStat struct {
	run     string // gerund shown while in flight
	ok      string // past tense shown on success
	fail    string // shown on failure
	settled bool
	err     error
	refresh bool  // true when the action mutated the PR(s) → refetch on success
	nums    []int // PR numbers the action touched, for detail-freshness invalidation
}
```

- [ ] **Step 4: Populate refresh/nums in runAction and runBulk**

In `runAction` (`internal/ui/actions.go`), after each `m.actionStatus = statFor(a)` assignment (the `rerun-failed` case and the `default` argv case), stamp the fields. Add right after `m.actionStatus = statFor(a)` in **both** cases:

```go
		m.actionStatus.refresh = a.Refresh
		m.actionStatus.nums = []int{v.Number}
```

In `runBulk` (`internal/ui/actions.go`), after `m.actionStatus = statForBulk(a, n)`, collect the touched numbers (the `argvs` loop already iterated the selection; capture numbers there). Change the collection loop to also gather numbers, then stamp:

```go
	var argvs [][]string
	var nums []int
	for _, i := range m.selectedOrCursor() {
		if i < 0 || i >= m.section.Len() {
			continue
		}
		v := m.section.VarsAt(i)
		v.Repo = m.repo
		argv, err := a.ExpandArgv(v)
		if err != nil {
			m.err = err
			continue
		}
		if a.ExitsTUI {
			m.queueExit(a.Key, argv)
		} else {
			argvs = append(argvs, argv)
			nums = append(nums, v.Number)
		}
	}
```

and after `m.actionStatus = statForBulk(a, n)`:

```go
	m.actionStatus.refresh = a.Refresh
	m.actionStatus.nums = nums
```

- [ ] **Step 5: Stamp the expanded rerun paths**

In `internal/ui/expanded.go`, `rerunHoveredCheck`: add `refresh` + `nums` to the success `actionStat` literal (the cursor PR is `ps.prAt(m.cursor).Number`):

```go
	m.actionStatus = &actionStat{run: "rerunning " + c.Label(), ok: "rerun queued: " + c.Label(), fail: "rerun failed",
		refresh: true, nums: []int{ps.prAt(m.cursor).Number}}
```

In `rerunAllFailedChecks` (it has `v` from `cursorVars`):

```go
	m.actionStatus = &actionStat{run: "rerunning failed checks", ok: "rerun-all queued", fail: "rerun failed",
		refresh: true, nums: []int{v.Number}}
```

- [ ] **Step 6: Refetch on success in the actionDoneMsg handler**

In `internal/ui/prlist.go`, replace the `case actionDoneMsg` body:

```go
	case actionDoneMsg:
		// Scope the error to the status line rather than m.err, which blanks the board.
		if m.actionStatus == nil {
			return m, clearStatusCmd()
		}
		m.actionStatus.settled = true
		m.actionStatus.err = msg.err
		if msg.ok != "" {
			m.actionStatus.ok = msg.ok
		}
		if msg.fail != "" {
			m.actionStatus.fail = msg.fail
		}
		cmds := []tea.Cmd{clearStatusCmd()}
		if msg.err == nil && m.actionStatus.refresh {
			for _, n := range m.actionStatus.nums {
				delete(m.fresh, n) // force the detail/summary to revalidate
			}
			cmds = append(cmds, m.backgroundRefresh())
		}
		return m, tea.Batch(cmds...)
```

- [ ] **Step 7: Revalidate detail on reviewer change**

In `assignReviewersCmd` (`internal/ui/actions.go`), clear the acted PR's freshness so the refetched list re-pulls its detail (reviewer changes live in the summary). This method takes `number`; the model is a value receiver, so mutate before capturing the fetch. Change its start:

```go
func (m Model) assignReviewersCmd(number int, add, remove []string) tea.Cmd {
	if len(add) == 0 && len(remove) == 0 {
		return nil
	}
	delete(m.fresh, number) // reviewer set changed → summary must revalidate
	r, dir := m.runner, m.dir
	...
```

- [ ] **Step 8: Run the full suite**

Run: `cd ~/Data/git/noamsto/prdash-worktrees/feat-show-merged-live-summary && go build ./... && go test ./internal/ui/ ./internal/action/`
Expected: PASS.

- [ ] **Step 9: Commit**

```bash
cd ~/Data/git/noamsto/prdash-worktrees/feat-show-merged-live-summary
git add internal/ui/
git commit -m "feat(ui): refetch list + summary after mutating actions"
```

---

## Task 5: Manual verification

Not automatable here (private repo, real `gh` auth). Run the built binary against a repo with merged PRs.

- [ ] **Step 1: Build and run**

Run: `cd ~/Data/git/noamsto/prdash-worktrees/feat-show-merged-live-summary && go build -o /tmp/prdash . && cd <a-repo-with-merged-prs> && /tmp/prdash`

- [ ] **Step 2: Verify state toggle**
  - Press `s`: header shows `… · merged · N`; the list shows merged PRs.
  - Press `s` again → `closed`; again → back to `open`.
  - Press `f` to change preset (mine/all) and confirm `s` still composes (e.g. `all · merged`).
  - Press `F`, pick an author, confirm `s` composes with the custom filter and the header label reads the author (not a doubled state).
  - Confirms the smoke-test assumption: `gh pr list --search "is:merged …"` returns merged PRs.

- [ ] **Step 3: Verify live refresh**
  - On an open PR, run `u` (Update branch) → after the badge settles, the list/summary refetch (spinner blips) and the summary reflects the new state.
  - Run `M` (Mark ready) on a draft → the draft marker clears without a manual refresh.
  - Open the expanded Checks tab, rerun a check → summary/checks refresh.
  - `m` (Merge) on an `is:open` view → the PR drops out of the list after refetch.

- [ ] **Step 4: Push branch and open PR** (once the user confirms the manual pass)

---

## Self-Review

- **Spec coverage:** Part 1 state dimension → Tasks 1–2 (primitives, model fields, `s` key, `f`/author recompute, state-aware mine fetch/cache/hydrate, header, legend). Part 2 refresh → Tasks 3–4 (`Refresh` flag, `actionStat` fields, `actionDoneMsg` refetch, list/bulk/expanded-rerun/reviewer wiring). Spec's error-tag fix → Task 2 Step 7 (`mineFetchCmd` tags with `mineF`). Header double-state fix → Task 2 Step 8 (label uses `m.body`). Auto-poll overlap → covered by reusing `backgroundRefresh`; no extra work. Smoke-test assumption → Task 5 Step 2. Test churn → Task 2 Step 1/9.
- **Placeholder scan:** none — every code step shows full code; the only deferred item is the legend line (Step 10) and the `fakeRunner` name (Step 1), both explicitly instructing to match the existing pattern found by grep.
- **Type consistency:** `searchFor`/`splitState`/`nextState` signatures match across Tasks 1–2; `mineFetchedMsg.state`, `actionStat.refresh`/`nums`, `Action.Refresh` are defined before use; `backgroundRefresh()` and `m.fresh` are pre-existing.
