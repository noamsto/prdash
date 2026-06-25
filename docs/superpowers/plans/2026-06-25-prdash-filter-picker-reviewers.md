# Filter switching, member picker & reviewer assignment — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let the user switch the PR-list filter (cycle presets + filter-by-author), assign reviewers to a PR through a reusable multi-select member picker, and see "no reviewers" in the quick window.

**Architecture:** Two phases. Phase 1 adds built-in filter presets cycled by `f` and a reviewers line in the side preview — both small and independent. Phase 2 adds a generic multi-select picker overlay (modeled on the existing `showActions` overlay) backed by `gh api graphql` assignable users, then wires `F` (filter by author) and `R` (assign reviewers, add/remove diff on the cursor PR).

**Tech Stack:** Go, bubbletea v1, lipgloss, `gh` CLI via `gh.Runner`.

**Spec:** `docs/superpowers/specs/2026-06-25-prdash-filter-picker-reviewers-design.md`

## File structure

- `internal/ui/filter_presets.go` (new) — preset list + cycle logic.
- `internal/ui/prlist.go` (modify) — `presets`/`presetIdx` state, `f` key, header label.
- `internal/ui/preview.go` (modify) — reviewers line in the quick window.
- `internal/gh/members.go` (new) — `User` type + `FetchAssignableUsers` (GraphQL).
- `internal/ui/picker.go` (new) — generic multi-select picker (state helpers, update, view).
- `internal/ui/prlist.go` (modify) — picker state, `F`/`R` keys, member-fetch msg.
- `internal/ui/actions.go` (modify) — `assignReviewersCmd` (the `gh pr edit` diff).

---

# Phase 1 — Filter presets + quick-window reviewers line

### Task 1: Filter preset type + cycle

**Files:**
- Create: `internal/ui/filter_presets.go`
- Test: `internal/ui/filter_presets_test.go`

- [ ] **Step 1: Write the failing test**

```go
package ui

import "testing"

func TestFilterPresetCycleWraps(t *testing.T) {
	p := defaultPresets
	if p[0].search != "is:open author:@me" {
		t.Fatalf("first preset should be mine, got %q", p[0].search)
	}
	// cycle from each index advances by one, wrapping at the end
	for i := range p {
		want := (i + 1) % len(p)
		if got := nextPreset(i); got != want {
			t.Fatalf("nextPreset(%d) = %d, want %d", i, got, want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/ -run TestFilterPresetCycleWraps`
Expected: FAIL — `undefined: defaultPresets` / `nextPreset`.

- [ ] **Step 3: Write minimal implementation**

```go
package ui

type filterPreset struct{ name, search string }

var defaultPresets = []filterPreset{
	{"mine", "is:open author:@me"},
	{"review-requested", "is:open review-requested:@me"},
	{"all", "is:open"},
}

// nextPreset returns the index after i, wrapping to 0.
func nextPreset(i int) int { return (i + 1) % len(defaultPresets) }

// presetIndexFor returns the index of the preset whose search equals filter,
// or -1 when the filter is a custom (e.g. author) query.
func presetIndexFor(filter string) int {
	for i, p := range defaultPresets {
		if p.search == filter {
			return i
		}
	}
	return -1
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/ui/ -run TestFilterPresetCycleWraps`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/filter_presets.go internal/ui/filter_presets_test.go
git commit -m "feat(ui): filter preset list + cycle helper"
```

### Task 2: Wire `f` to cycle and label the header

**Files:**
- Modify: `internal/ui/prlist.go` (Model struct ~18-43; NewModel ~45-55; key handler list-mode ~265-295; `header` ~346-350)
- Test: `internal/ui/prlist_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestCycleFilterAdvancesPresetAndLabel(t *testing.T) {
	m := NewModel("/repo", "is:open author:@me", nil)
	m.SetRepo("noamsto/prdash")
	m.width, m.height = 100, 30
	if m.presetIdx != 0 {
		t.Fatalf("initial presetIdx = %d, want 0 (mine)", m.presetIdx)
	}
	m2, _ := m.Update(keyMsg("f"))
	m = m2.(Model)
	if m.filter != "is:open review-requested:@me" {
		t.Fatalf("after f, filter = %q", m.filter)
	}
	if !strings.Contains(m.View(), "review-requested") {
		t.Fatalf("header should show the active preset name: %q", m.View())
	}
}
```

Add this helper near the top of `prlist_test.go` if one does not already exist (search first):

```go
func keyMsg(s string) tea.KeyMsg {
	if s == "f" || len(s) == 1 {
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}
```

Add imports `tea "github.com/charmbracelet/bubbletea"` and `strings` to the test file if missing.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/ -run TestCycleFilterAdvancesPresetAndLabel`
Expected: FAIL — `m.presetIdx` undefined.

- [ ] **Step 3: Add state to the Model struct**

In `internal/ui/prlist.go`, add to `type Model struct` (after `loaded bool`):

```go
	presetIdx int // index into defaultPresets; -1 when filter is a custom (author) query
```

- [ ] **Step 4: Initialise it in NewModel**

In `NewModel`, set `presetIdx` from the seed filter. Change the returned struct literal to include:

```go
		presetIdx: presetIndexFor(filter),
```

- [ ] **Step 5: Add the `f` case to the list-mode key handler**

In the list-mode `switch` (the block guarded by not-filtering/not-showActions, near `case "a":`), add:

```go
		case "f":
			m.presetIdx = nextPreset(maxInt(m.presetIdx, 0))
			m.filter = defaultPresets[m.presetIdx].search
			m.cursor = 0
			m.loaded = false
			return m, m.fetchCmd(m.runner)
```

Add a small helper to `filter_presets.go` (Go's builtin `max` works on ints in 1.21+, but a named helper keeps intent clear):

```go
func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
```

(Rationale: from a custom author filter `presetIdx` is -1; `maxInt(-1,0)` makes `f` land on `mine`'s successor `review-requested`. If that feels wrong during review, change to land on `mine` itself — decide live.)

- [ ] **Step 6: Label the header by preset name**

Replace the body of `header()`:

```go
func (m Model) header() string {
	label := m.filter
	if m.presetIdx >= 0 {
		label = defaultPresets[m.presetIdx].name
	}
	return headerStyle.Render("  "+m.repo) + dimStyle.Render(
		fmt.Sprintf("   %s · %d open", label, m.section.Len()))
}
```

- [ ] **Step 7: Run the test and the suite**

Run: `go test ./internal/ui/ -run TestCycleFilterAdvancesPresetAndLabel` then `go test ./...`
Expected: PASS; full suite green.

- [ ] **Step 8: Commit**

```bash
git add internal/ui/prlist.go internal/ui/filter_presets.go internal/ui/prlist_test.go
git commit -m "feat(ui): cycle filter presets with f, label header by preset"
```

### Task 3: Quick-window "no reviewers" line

**Files:**
- Modify: `internal/ui/preview.go` (`previewPane` ~81-100)
- Test: `internal/ui/preview_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestReviewersLine(t *testing.T) {
	if !strings.Contains(reviewersLine(nil), "no reviewers") {
		t.Fatalf("empty reviewers should warn: %q", reviewersLine(nil))
	}
	got := reviewersLine([]gh.ReviewRequest{{Login: "alice"}, {Login: "bob"}})
	if !strings.Contains(got, "alice") || !strings.Contains(got, "bob") {
		t.Fatalf("should list reviewers: %q", got)
	}
}
```

Ensure `preview_test.go` imports `"github.com/noamsto/prdash/internal/gh"` and `"strings"`.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/ -run TestReviewersLine`
Expected: FAIL — `undefined: reviewersLine`.

- [ ] **Step 3: Implement `reviewersLine` and show it in the pane**

Add to `internal/ui/preview.go`:

```go
// reviewersLine summarises requested reviewers for the quick window. Team
// requests have no login and are skipped.
func reviewersLine(reqs []gh.ReviewRequest) string {
	var logins []string
	for _, r := range reqs {
		if r.Login != "" {
			logins = append(logins, r.Login)
		}
	}
	if len(logins) == 0 {
		return pendStyle.Render("  ⚠ no reviewers")
	}
	return dimStyle.Render("  reviewers: " + strings.Join(logins, ", "))
}
```

In `previewPane`, insert the line between the card and the timeline. Change the tail of `previewPane` from:

```go
	timeline := renderTimeline(preview.Timeline(d), m.previewN, w, m.previewExpanded)
	if card == "" {
		return timeline
	}
	return card + "\n" + timeline
```

to:

```go
	revs := reviewersLine(d.ReviewRequests)
	timeline := renderTimeline(preview.Timeline(d), m.previewN, w, m.previewExpanded)
	if card == "" {
		return revs + "\n\n" + timeline
	}
	return card + "\n" + revs + "\n\n" + timeline
```

- [ ] **Step 4: Run test + suite**

Run: `go test ./internal/ui/ -run TestReviewersLine` then `go test ./...`
Expected: PASS; suite green.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/preview.go internal/ui/preview_test.go
git commit -m "feat(ui): show requested reviewers (or 'no reviewers') in quick window"
```

**Phase 1 ships:** `f` cycles mine → review-requested → all (header labelled); the quick window flags un-reviewed PRs. Verify live, then proceed to Phase 2.

---

# Phase 2 — Member picker + author filter + reviewer assignment

### Task 4: `gh.User` + `FetchAssignableUsers` (GraphQL)

**Files:**
- Create: `internal/gh/members.go`
- Test: `internal/gh/members_test.go`

- [ ] **Step 1: Write the failing test** (parse a fixture; use the existing fake runner pattern — check `internal/gh/*_test.go` for the helper, commonly a `func(...) ([]byte,error)` runner)

```go
package gh

import "testing"

type stubRunner struct{ out []byte }

func (s stubRunner) Run(dir string, args ...string) ([]byte, error) { return s.out, nil }

func TestFetchAssignableUsersParses(t *testing.T) {
	resp := `{"data":{"repository":{"assignableUsers":{"nodes":[
		{"login":"alice","name":"Alice A"},
		{"login":"bob","name":""}]}}}}`
	users, err := FetchAssignableUsers(stubRunner{out: []byte(resp)}, "/repo", "noamsto/prdash")
	if err != nil {
		t.Fatal(err)
	}
	if len(users) != 2 || users[0].Login != "alice" || users[0].Name != "Alice A" {
		t.Fatalf("parsed wrong: %+v", users)
	}
}
```

If `internal/gh` already defines a fake/stub runner in a `_test.go`, reuse it and delete the local `stubRunner` to avoid a duplicate type.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/gh/ -run TestFetchAssignableUsersParses`
Expected: FAIL — `undefined: FetchAssignableUsers` / `User`.

- [ ] **Step 3: Implement**

```go
package gh

import (
	"encoding/json"
	"fmt"
	"strings"
)

type User struct {
	Login string `json:"login"`
	Name  string `json:"name"`
}

const assignableUsersQuery = `query($owner:String!,$name:String!){repository(owner:$owner,name:$name){assignableUsers(first:100){nodes{login name}}}}`

// FetchAssignableUsers returns the users GitHub permits as reviewers/assignees
// for repo (owner/name).
func FetchAssignableUsers(r Runner, dir, repo string) ([]User, error) {
	owner, name, ok := strings.Cut(repo, "/")
	if !ok {
		return nil, fmt.Errorf("bad repo %q", repo)
	}
	out, err := r.Run(dir, "api", "graphql",
		"-f", "query="+assignableUsersQuery,
		"-F", "owner="+owner, "-F", "name="+name)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Data struct {
			Repository struct {
				AssignableUsers struct {
					Nodes []User `json:"nodes"`
				} `json:"assignableUsers"`
			} `json:"repository"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		return nil, fmt.Errorf("parse assignable users: %w", err)
	}
	return resp.Data.Repository.AssignableUsers.Nodes, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/gh/ -run TestFetchAssignableUsersParses`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/gh/members.go internal/gh/members_test.go
git commit -m "feat(gh): FetchAssignableUsers via GraphQL"
```

### Task 5: Generic picker model — state + toggle + filter

**Files:**
- Create: `internal/ui/picker.go`
- Test: `internal/ui/picker_test.go`

The picker is a value held on the Model. It owns: the candidate list, a checked set, a cursor, and a fuzzy-filter input. It does not know its purpose — the Model's `pickerMode` decides what confirm does.

- [ ] **Step 1: Write the failing test**

```go
package ui

import "testing"

import "github.com/noamsto/prdash/internal/gh"

func TestPickerToggleAndSelected(t *testing.T) {
	p := newPicker("Reviewers", []gh.User{{Login: "alice"}, {Login: "bob"}}, map[string]bool{"alice": true})
	if !p.checked["alice"] {
		t.Fatal("alice should start checked")
	}
	p.toggleCursor() // cursor at 0 = alice → uncheck
	if p.checked["alice"] {
		t.Fatal("alice should be unchecked after toggle")
	}
	p.cursor = 1
	p.toggleCursor() // bob → check
	sel := p.selected()
	if len(sel) != 1 || sel[0] != "bob" {
		t.Fatalf("selected = %v, want [bob]", sel)
	}
}

func TestPickerFuzzyFilter(t *testing.T) {
	p := newPicker("X", []gh.User{{Login: "alice", Name: "Alice"}, {Login: "bob"}}, nil)
	p.filter.SetValue("ali")
	if got := p.visible(); len(got) != 1 || got[0].Login != "alice" {
		t.Fatalf("fuzzy filter should narrow to alice, got %+v", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/ -run TestPicker`
Expected: FAIL — `undefined: newPicker`.

- [ ] **Step 3: Implement the picker**

```go
package ui

import (
	"github.com/charmbracelet/bubbles/textinput"

	"github.com/noamsto/prdash/internal/gh"
)

type picker struct {
	title   string
	cands   []gh.User
	checked map[string]bool
	cursor  int
	filter  textinput.Model
}

func newPicker(title string, cands []gh.User, checked map[string]bool) picker {
	if checked == nil {
		checked = map[string]bool{}
	}
	ti := textinput.New()
	ti.Prompt = "› "
	ti.Focus()
	return picker{title: title, cands: cands, checked: checked, filter: ti}
}

// visible returns the candidates matching the fuzzy filter, in order.
func (p picker) visible() []gh.User {
	q := p.filter.Value()
	if q == "" {
		return p.cands
	}
	hay := make([]string, len(p.cands))
	for i, u := range p.cands {
		hay[i] = u.Login + " " + u.Name
	}
	idx := matchIdx(hay, q)
	out := make([]gh.User, 0, len(idx))
	for _, i := range idx {
		out = append(out, p.cands[i])
	}
	return out
}

// toggleCursor flips the checked state of the candidate under the cursor in the
// currently-visible (filtered) list.
func (p *picker) toggleCursor() {
	vis := p.visible()
	if p.cursor < 0 || p.cursor >= len(vis) {
		return
	}
	login := vis[p.cursor].Login
	if p.checked[login] {
		delete(p.checked, login)
	} else {
		p.checked[login] = true
	}
}

// selected returns the checked logins (order unspecified).
func (p picker) selected() []string {
	out := make([]string, 0, len(p.checked))
	for login, on := range p.checked {
		if on {
			out = append(out, login)
		}
	}
	return out
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/ui/ -run TestPicker`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/picker.go internal/ui/picker_test.go
git commit -m "feat(ui): generic multi-select picker model"
```

### Task 6: Picker overlay wiring — open, fetch members, navigate, render

**Files:**
- Modify: `internal/ui/prlist.go` (Model struct; key handler; Update msg cases; View overlay branch)
- Modify: `internal/ui/messages.go` (new msg)
- Test: `internal/ui/picker_test.go`

- [ ] **Step 1: Write the failing test** (opening sets state; members msg populates)

```go
func TestOpenPickerFetchesAndPopulates(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.SetRepo("noamsto/prdash")
	m.width, m.height = 100, 30
	m.setPRs([]gh.PR{{Number: 7, Title: "hi"}})

	m.openPicker("author")
	if !m.showPicker || m.pickerMode != "author" {
		t.Fatal("openPicker should show the picker in author mode")
	}
	m2, _ := m.Update(membersFetchedMsg{users: []gh.User{{Login: "alice"}}})
	m = m2.(Model)
	if len(m.pick.cands) != 1 || m.pick.cands[0].Login != "alice" {
		t.Fatalf("members msg should populate candidates: %+v", m.pick.cands)
	}
	if !strings.Contains(m.View(), "alice") {
		t.Fatalf("picker view should list candidates: %q", m.View())
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/ -run TestOpenPickerFetchesAndPopulates`
Expected: FAIL — `undefined: openPicker` / `showPicker` / `membersFetchedMsg`.

- [ ] **Step 3: Add msg** to `internal/ui/messages.go`:

```go
type membersFetchedMsg struct{ users []gh.User }
```

Ensure `messages.go` imports `"github.com/noamsto/prdash/internal/gh"`.

- [ ] **Step 4: Add Model state** (in `type Model struct`):

```go
	showPicker bool
	pickerMode string // "author" | "reviewer"
	pick       picker
	members    []gh.User // cached assignable users for this repo
```

- [ ] **Step 5: Add openPicker + fetch command** to `internal/ui/prlist.go`:

```go
// openPicker shows the member picker in the given mode, pre-checking the right
// set, and fetches the member list if it isn't cached yet.
func (m *Model) openPicker(mode string) tea.Cmd {
	checked := map[string]bool{}
	title := "Filter by author"
	if mode == "reviewer" {
		title = "Assign reviewers"
		if v, ok := m.cursorVars(); ok {
			if d, cached := m.detail[v.Number]; cached {
				for _, r := range d.ReviewRequests {
					if r.Login != "" {
						checked[r.Login] = true
					}
				}
			}
		}
	}
	m.showPicker = true
	m.pickerMode = mode
	m.pick = newPicker(title, m.members, checked)
	if m.members == nil {
		return m.fetchMembersCmd()
	}
	return nil
}

func (m Model) fetchMembersCmd() tea.Cmd {
	r, dir, repo := m.runner, m.dir, m.repo
	return func() tea.Msg {
		users, err := gh.FetchAssignableUsers(r, dir, repo)
		if err != nil {
			return fetchFailedMsg{err}
		}
		return membersFetchedMsg{users: users}
	}
}
```

- [ ] **Step 6: Handle the msg** in `Update` (add a case alongside `prsFetchedMsg`):

```go
	case membersFetchedMsg:
		m.members = msg.users
		if m.showPicker {
			m.pick.cands = msg.users
		}
		return m, nil
```

- [ ] **Step 7: Add the picker key branch** at the TOP of the key handler in `Update` (before the `showActions` branch), so the picker captures keys while open:

```go
		if m.showPicker {
			switch msg.String() {
			case "esc":
				m.showPicker = false
				return m, nil
			case "enter":
				m.showPicker = false
				return m, m.confirmPicker()
			case " ":
				m.pick.toggleCursor()
				return m, nil
			case "up", "ctrl+p":
				if m.pick.cursor > 0 {
					m.pick.cursor--
				}
				return m, nil
			case "down", "ctrl+n":
				if m.pick.cursor < len(m.pick.visible())-1 {
					m.pick.cursor++
				}
				return m, nil
			default:
				var cmd tea.Cmd
				m.pick.filter, cmd = m.pick.filter.Update(msg)
				m.pick.cursor = 0
				return m, cmd
			}
		}
```

Add a stub `confirmPicker` so this compiles (real body in Task 7):

```go
func (m *Model) confirmPicker() tea.Cmd { return nil }
```

- [ ] **Step 8: Render the overlay.** In `View()`, add a branch before the `showActions` branch:

```go
	if m.showPicker {
		return m.pickerView()
	}
```

Add `pickerView` to `internal/ui/picker.go`:

```go
func (m Model) pickerView() string {
	p := m.pick
	var b strings.Builder
	b.WriteString(headerStyle.Render("  "+p.title) + "\n")
	b.WriteString("  " + p.filter.View() + "\n\n")
	if p.cands == nil {
		b.WriteString(dimStyle.Render("  Loading…"))
		return b.String()
	}
	for i, u := range p.visible() {
		mark := "  "
		if p.checked[u.Login] {
			mark = selMarkStyle.Render("● ")
		}
		cur := "  "
		if i == p.cursor {
			cur = "> "
		}
		label := "@" + u.Login
		if u.Name != "" {
			label += dimStyle.Render("  "+u.Name)
		}
		b.WriteString(cur + mark + label + "\n")
	}
	return b.String()
}
```

Add `"strings"` to `picker.go` imports.

- [ ] **Step 9: Run test + suite**

Run: `go test ./internal/ui/ -run TestOpenPickerFetchesAndPopulates` then `go test ./...`
Expected: PASS; suite green.

- [ ] **Step 10: Commit**

```bash
git add internal/ui/prlist.go internal/ui/picker.go internal/ui/messages.go internal/ui/picker_test.go
git commit -m "feat(ui): member-picker overlay — open, fetch, navigate, render"
```

### Task 7: Confirm — author filter + reviewer diff

**Files:**
- Modify: `internal/ui/prlist.go` (`confirmPicker`)
- Modify: `internal/ui/actions.go` (`assignReviewersCmd`)
- Test: `internal/ui/picker_test.go`, `internal/ui/actions_test.go`

- [ ] **Step 1: Write the failing tests**

Reviewer diff (pure function) in `actions_test.go`:

```go
func TestReviewerDiff(t *testing.T) {
	add, rm := reviewerDiff([]string{"a", "c"}, map[string]bool{"b": true, "c": true})
	if len(add) != 1 || add[0] != "b" {
		t.Fatalf("add = %v, want [b]", add)
	}
	if len(rm) != 1 || rm[0] != "a" {
		t.Fatalf("remove = %v, want [a]", rm)
	}
	add, rm = reviewerDiff([]string{"a"}, map[string]bool{"a": true})
	if len(add) != 0 || len(rm) != 0 {
		t.Fatalf("no change expected, got add=%v rm=%v", add, rm)
	}
}
```

Author confirm (in `picker_test.go`):

```go
func TestConfirmAuthorSetsFilter(t *testing.T) {
	m := NewModel("/repo", "is:open author:@me", nil)
	m.SetRepo("noamsto/prdash")
	m.width, m.height = 100, 30
	m.setPRs([]gh.PR{{Number: 7}})
	m.openPicker("author")
	m.pick.cands = []gh.User{{Login: "alice"}, {Login: "bob"}}
	m.pick.checked = map[string]bool{"alice": true, "bob": true}
	_ = m.confirmPicker()
	if m.filter != "is:open author:alice author:bob" && m.filter != "is:open author:bob author:alice" {
		t.Fatalf("author filter = %q", m.filter)
	}
	if m.presetIdx != -1 {
		t.Fatalf("author filter should be custom (presetIdx -1), got %d", m.presetIdx)
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/ui/ -run 'TestReviewerDiff|TestConfirmAuthorSetsFilter'`
Expected: FAIL — `reviewerDiff` undefined; author filter not set.

- [ ] **Step 3: Implement `reviewerDiff` and `assignReviewersCmd`** in `internal/ui/actions.go`:

```go
// reviewerDiff compares the currently-requested reviewers against the picked
// set, returning logins to add and to remove.
func reviewerDiff(current []string, picked map[string]bool) (add, remove []string) {
	cur := map[string]bool{}
	for _, l := range current {
		cur[l] = true
	}
	for l, on := range picked {
		if on && !cur[l] {
			add = append(add, l)
		}
	}
	for l := range cur {
		if !picked[l] {
			remove = append(remove, l)
		}
	}
	return add, remove
}

// assignReviewersCmd applies an add/remove reviewer diff to one PR, then refetches.
func (m Model) assignReviewersCmd(number int, add, remove []string) tea.Cmd {
	if len(add) == 0 && len(remove) == 0 {
		return nil
	}
	r, dir := m.runner, m.dir
	args := []string{"pr", "edit", strconv.Itoa(number)}
	if len(add) > 0 {
		args = append(args, "--add-reviewer", strings.Join(add, ","))
	}
	if len(remove) > 0 {
		args = append(args, "--remove-reviewer", strings.Join(remove, ","))
	}
	fetch := m.fetchCmd(m.runner)
	return func() tea.Msg {
		if _, err := r.Run(dir, args...); err != nil {
			return fetchFailedMsg{err}
		}
		return fetch()
	}
}
```

Ensure `actions.go` imports `"strconv"` and `"strings"` (add if missing).

- [ ] **Step 4: Implement `confirmPicker`** in `internal/ui/prlist.go` (replace the Task 6 stub):

```go
// confirmPicker applies the picker result based on the active mode.
func (m *Model) confirmPicker() tea.Cmd {
	picked := m.pick.checked
	switch m.pickerMode {
	case "author":
		var terms []string
		for login, on := range picked {
			if on {
				terms = append(terms, "author:"+login)
			}
		}
		if len(terms) == 0 {
			return nil // empty selection: keep the current filter
		}
		sort.Strings(terms)
		m.filter = "is:open " + strings.Join(terms, " ")
		m.presetIdx = -1
		m.cursor = 0
		m.loaded = false
		return m.fetchCmd(m.runner)
	case "reviewer":
		v, ok := m.cursorVars()
		if !ok {
			return nil
		}
		var current []string
		if d, cached := m.detail[v.Number]; cached {
			for _, rr := range d.ReviewRequests {
				if rr.Login != "" {
					current = append(current, rr.Login)
				}
			}
		}
		add, remove := reviewerDiff(current, picked)
		return m.assignReviewersCmd(v.Number, add, remove)
	}
	return nil
}
```

Add `"sort"` and `"strings"` to `prlist.go` imports if missing.

(The `TestConfirmAuthorSetsFilter` test uses unsorted equality tolerance, but `sort.Strings` makes the result deterministic — `is:open author:alice author:bob`. Keep the test's two-way check or tighten it to the sorted form.)

- [ ] **Step 5: Run tests + suite**

Run: `go test ./internal/ui/ -run 'TestReviewerDiff|TestConfirmAuthorSetsFilter'` then `go test ./...`
Expected: PASS; suite green.

- [ ] **Step 6: Commit**

```bash
git add internal/ui/prlist.go internal/ui/actions.go internal/ui/picker_test.go internal/ui/actions_test.go
git commit -m "feat(ui): picker confirm — author filter + reviewer add/remove diff"
```

### Task 8: Bind `F` and `R` keys + footer hint

**Files:**
- Modify: `internal/ui/prlist.go` (list-mode key handler; `statusBar` ~352-359)
- Test: manual (live) — overlay opening is covered by Task 6; this is wiring.

- [ ] **Step 1: Add the key cases** to the list-mode `switch` (next to `case "f":`):

```go
		case "F":
			return m, m.openPicker("author")
		case "R":
			return m, m.openPicker("reviewer")
```

- [ ] **Step 2: Update the status bar hint.** In `statusBar`, change the `keys` string to include the new bindings:

```go
	keys := "↑↓ move · → expand · f filter · F author · R reviewers · / find · a actions · q quit"
```

- [ ] **Step 3: Build + full suite**

Run: `go build ./... && go test ./...`
Expected: build clean; suite green.

- [ ] **Step 4: Commit**

```bash
git add internal/ui/prlist.go
git commit -m "feat(ui): bind F (author filter) and R (assign reviewers)"
```

### Task 9: Live verification

- [ ] Build the dev binary: `go build -o /tmp/prdash-dev .`
- [ ] Run from a repo with assignable users and open PRs (e.g. a `factify-inc` repo).
- [ ] `f` cycles mine → review-requested → all; header label updates; counts change.
- [ ] `F` opens the picker, fuzzy filter narrows, `space` checks people, `enter` re-filters the list to `author:…`; `f` returns to presets.
- [ ] `R` on a PR pre-checks current reviewers; toggling + `enter` runs `gh pr edit`; the review column/quick-window updates after refetch.
- [ ] Quick window shows `⚠ no reviewers` on an un-reviewed PR.
- [ ] `esc` closes the picker without changes.

---

## Self-review notes

- **Spec coverage:** presets+cycle (Task 1-2), member source GraphQL (Task 4), generic picker (Task 5), author filter `F` (Task 6-8), reviewer assign `R` add/remove diff (Task 6-8), quick-window reviewers line (Task 3), error scoping (fetchFailedMsg in Tasks 4/6/7). All covered.
- **Picker key precedence:** the picker branch is added at the top of the key handler so it captures input before list/actions branches — confirm placement during Task 6.
- **`maxInt` cycle-from-custom:** flagged as a live decision in Task 2 Step 5.
- **Stub→real `confirmPicker`:** introduced as a stub in Task 6, replaced in Task 7 — intentional so Task 6 compiles independently.
