# Unified Tabbed Detail Pane + Inline Review Comments — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Merge prdash's side preview pane and full-screen dive-in into one tabbed detail component, and add inline review-comment threads surfaced unresolved-first.

**Architecture:** A single set of tab renderers drives two containers — the side pane when `computeLayout.ShowSide` is true, full-screen when narrow. A new default **Overview** tab carries today's triage summary plus a top-unresolved-threads block. Inline threads come from GitHub GraphQL `reviewThreads` via `gh api graphql`, fetched and cached in parallel with existing PR detail, and rendered under files in the **Diff** tab.

**Tech Stack:** Go, charm.land/bubbletea v2, charm.land/lipgloss v2, `gh` CLI (`gh api graphql`), the repo's `internal/cache` disk cache.

## Global Constraints

- Read-only in v1: no replying/resolving threads from prdash.
- No full unified-diff / code-hunk rendering in v1 — Diff tab shows file list + threads; `file:line + comment` only.
- Threads are **PR-only**; issue rows keep today's simpler pane (no PR tabs).
- Unresolved-first everywhere; resolved threads hidden behind a `▸ N resolved` toggle, collapsed by default.
- Follow existing patterns: lazy fetch on cursor settle (debounced), disk cache keyed by repo with a schema-version constant, section renderers width-parameterized.
- `tab` key stays the PR/Issue mode toggle — do not repurpose it.
- Every rendered tab must fit its container width — no horizontal overflow (charm-tui border-bleed class of bug).

---

## Phase 1 — Restructure into one tabbed detail component

### Task 1: Add the Overview tab constant and put it first

**Files:**
- Modify: `internal/ui/expanded.go:16-24` (tab constants + `expandedTabs`)
- Modify: `internal/ui/expanded.go:265` (`"1".."5"` → include `"6"`)
- Test: `internal/ui/expanded_test.go`

**Interfaces:**
- Produces: `tabOverview` constant (value 0); `expandedTabs` slice of length 6 with `"Overview"` first; existing `tabDescription`, `tabConversation`, `tabReviews`, `tabChecks`, `tabDiff` renumbered.

- [ ] **Step 1: Write the failing test**

```go
// internal/ui/expanded_test.go
func TestExpandedTabsIncludeOverviewFirst(t *testing.T) {
	if expandedTabs[0] != "Overview" {
		t.Fatalf("first tab = %q, want Overview", expandedTabs[0])
	}
	if tabOverview != 0 {
		t.Fatalf("tabOverview = %d, want 0", tabOverview)
	}
	if len(expandedTabs) != 6 {
		t.Fatalf("len(expandedTabs) = %d, want 6", len(expandedTabs))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/ -run TestExpandedTabsIncludeOverviewFirst`
Expected: FAIL — `undefined: tabOverview`

- [ ] **Step 3: Implement the constant + slice change**

```go
// internal/ui/expanded.go
const (
	tabOverview = iota
	tabDescription
	tabConversation
	tabReviews
	tabChecks
	tabDiff
	discussionMaxWidth = 104
)

var expandedTabs = []string{"Overview", "Description", "Conversation", "Reviews", "Checks", "Diff"}
```

Update the digit case at `expanded.go:265` to accept `"6"`:

```go
	case "1", "2", "3", "4", "5", "6":
		m.expandedTab = int(msg.String()[0] - '1')
		m.checkCursor = 0
		m.renderExpanded()
		return m, nil
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/ui/ -run TestExpandedTabsIncludeOverviewFirst`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/ui/expanded.go internal/ui/expanded_test.go
git commit -m "feat(ui): add Overview tab constant, first in the detail tab set (#49)"
```

---

### Task 2: Make Overview the renderer for today's summary; keep parity

**Files:**
- Modify: `internal/ui/expanded.go:148-182` (`expandedBody` switch — add `tabOverview` case)
- Modify: `internal/ui/preview.go:244-290` (extract the summary body from `previewPane` into `renderOverview`)
- Test: `internal/ui/expanded_test.go`

**Interfaces:**
- Produces: `func (m Model) renderOverview(w int) string` — the identity-less summary body (blocker card, checks line, review line, latest timeline, description snippet) at width `w`. Consumes `m.detail`, `m.cursorVars`, `triage`.
- Consumes: `tabOverview` (Task 1).

**Context:** Today `previewPane()` (`preview.go:244`) builds identity header + description snippet + blocker + checks + review + latest. The identity header stays owned by the pane container (Task 3); the *body sections below identity* become `renderOverview`. The `section` helper is a local closure: `sectionRule(label, w) + "\n" + indentLines(strings.TrimRight(body, "\n"), 2)`.

- [ ] **Step 1: Write the failing test**

```go
// internal/ui/expanded_test.go
func TestRenderOverviewShowsBlockerAndLatest(t *testing.T) {
	m := newTestModelWithPR(t) // helper already used in expanded_test.go / preview_test.go
	out := m.renderOverview(60)
	if !strings.Contains(out, "LATEST") {
		t.Fatalf("overview missing LATEST section:\n%s", out)
	}
}
```

If no `newTestModelWithPR` helper exists, reuse the construction pattern already in `preview_test.go` (search for `Model{` there) to build a model with one PR and a cached `gh.PRDetail`.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/ -run TestRenderOverviewShowsBlockerAndLatest`
Expected: FAIL — `m.renderOverview undefined`

- [ ] **Step 3: Extract `renderOverview` and call it from the tab switch**

In `preview.go`, add (mirroring the body-building half of `previewPane`, minus the identity header):

```go
// renderOverview is the Overview tab body: the triage summary shown by default.
// Identity is owned by the container; this is everything below it.
func (m Model) renderOverview(w int) string {
	v, ok := m.cursorVars()
	if !ok {
		return ""
	}
	bw := w - 2
	section := func(label, body string) string {
		return sectionRule(label, w) + "\n" + indentLines(strings.TrimRight(body, "\n"), 2)
	}
	d, cached := m.detail[v.Number]
	var blocks []string
	if ps, ok := m.section.(*PRSection); ok {
		pr := ps.prAt(m.cursor)
		if body := previewDescriptionBody(pr, m.viewerLogin, bw); body != "" {
			blocks = append(blocks, section("description", body))
		}
		tc := triage.Preliminary(pr)
		if cached {
			tc = triage.Compute(pr, d)
		}
		if card := renderCard(tc, bw); card != "" {
			blocks = append(blocks, section("blocker", card))
		}
		if tc.Kind != triage.KindChecksFailing && tc.Kind != triage.KindChecksRunning {
			if ci := ciLine(pr); ci != "" {
				blocks = append(blocks, section("checks", ci))
			}
		}
	}
	if !cached {
		blocks = append(blocks, dimStyle.Render("  loading details…"))
		return strings.Join(blocks, "\n\n")
	}
	blocks = append(blocks, section("review", reviewLine(d)))
	blocks = append(blocks, section("latest", renderTimeline(preview.Timeline(d), m.previewN, bw, m.previewExpanded)))
	return strings.Join(blocks, "\n\n")
}
```

In `expanded.go`, add the `tabOverview` case to `expandedBody` (before the `default`):

```go
	case tabOverview:
		return m.renderOverview(w)
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/ui/ -run TestRenderOverview`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/ui/preview.go internal/ui/expanded.go internal/ui/expanded_test.go
git commit -m "feat(ui): render triage summary as the Overview tab body (#49)"
```

---

### Task 3: Render a tab bar + active tab inside the side pane

**Files:**
- Modify: `internal/ui/preview.go:244-290` (`previewPane` → identity header + tab bar + active-tab body)
- Create: `internal/ui/tabbar.go` (`renderTabBar`)
- Test: `internal/ui/preview_test.go`

**Interfaces:**
- Produces: `func renderTabBar(tabs []string, active, w int) string` — one-line tab bar; active tab uses `tabActiveStyle`, others `tabInactiveStyle` (both exist in `theme.go`).
- Consumes: `m.expandedTab`, `renderOverview` (Task 2), `expandedBody` (for non-Overview tabs), `identityHeader`.

**Context:** `previewPane` currently returns the summary directly. It now returns: identity header, then `renderTabBar`, then the active tab's body. Non-Overview tabs reuse `expandedBody(w)` so there's a single content source. Issue rows keep `issuePreviewPane` untouched (no tabs).

- [ ] **Step 1: Write the failing test**

```go
// internal/ui/preview_test.go
func TestPreviewPaneShowsTabBar(t *testing.T) {
	m := newTestModelWithPR(t)
	out := m.previewPane()
	for _, label := range []string{"Overview", "Diff", "Reviews"} {
		if !strings.Contains(out, label) {
			t.Fatalf("preview pane missing tab %q:\n%s", label, out)
		}
	}
}

func TestRenderTabBarMarksActive(t *testing.T) {
	bar := renderTabBar([]string{"Overview", "Diff"}, 1, 60)
	if !strings.Contains(bar, "Diff") || !strings.Contains(bar, "Overview") {
		t.Fatalf("tab bar missing labels: %s", bar)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/ -run 'TestPreviewPaneShowsTabBar|TestRenderTabBarMarksActive'`
Expected: FAIL — `renderTabBar undefined`

- [ ] **Step 3: Implement `renderTabBar` and rewire `previewPane`**

```go
// internal/ui/tabbar.go
package ui

import "strings"

// renderTabBar draws a single-line tab strip. The active tab is a filled accent
// badge; the rest are dim padded names. Overflow-safe: truncated to width w.
func renderTabBar(tabs []string, active, w int) string {
	cells := make([]string, len(tabs))
	for i, name := range tabs {
		if i == active {
			cells[i] = tabActiveStyle.Render(name)
		} else {
			cells[i] = tabInactiveStyle.Render(name)
		}
	}
	return truncate(strings.Join(cells, ""), w)
}
```

Rewrite the PR branch of `previewPane` (`preview.go:244`) so it returns identity + tab bar + active-tab body. Replace the block that builds `blocks` for the PR case with:

```go
func (m Model) previewPane() string {
	v, ok := m.cursorVars()
	if !ok {
		return ""
	}
	w := m.previewWidth()
	if is, ok := m.section.(*IssueSection); ok {
		return m.issuePreviewPane(is, w, w-2)
	}
	ps, ok := m.section.(*PRSection)
	if !ok {
		return ""
	}
	header := identityHeader(ps.prAt(m.cursor))
	bar := renderTabBar(expandedTabs, m.expandedTab, w)
	body := m.expandedBody(w) // Overview when m.expandedTab==tabOverview
	_ = v
	return strings.Join([]string{header, bar, body}, "\n\n")
}
```

Remove the now-duplicated summary-building code from `previewPane` (it lives in `renderOverview` after Task 2).

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/ui/`
Expected: PASS (existing preview tests still green; new tab-bar tests pass)

- [ ] **Step 5: Commit**

```bash
git add internal/ui/tabbar.go internal/ui/preview.go internal/ui/preview_test.go
git commit -m "feat(ui): tab bar + active-tab body in the side detail pane (#49)"
```

---

### Task 4: Drive pane tabs from the main view (h/l/1-6, Enter maximizes)

**Files:**
- Modify: `internal/ui/prlist.go:1456-1462` (`"right","l"` case) and add `"left","h"`, digit cases in the main-view key switch
- Modify: `internal/ui/prlist.go:1392-1394` (`z`/maximize) and `"enter"` handling
- Test: `internal/ui/prlist_test.go`

**Interfaces:**
- Consumes: `m.expandedTab`, `expandedTabs`, `m.previewMax`, `enterExpanded` (narrow path).

**Context:** In the wide layout the pane owns the tabs, so `l`/`h` cycle `m.expandedTab` and re-render the pane rather than diving in. On a narrow terminal (`!computeLayout.ShowSide`), there is no pane, so `Enter`/`l` opens the full-screen tabbed view (`enterExpanded`, unchanged path). `Enter` in the wide layout toggles `previewMax` (maximize current tab). The active tab must re-render on cursor move — the pane is rebuilt every `render()` from `m.expandedTab`, so no extra wiring is needed for live re-render beyond the existing `debounceDetailCmd`.

- [ ] **Step 1: Write the failing test**

```go
// internal/ui/prlist_test.go
func TestMainViewCyclesTabsWithL(t *testing.T) {
	m := newTestModelWideWithPR(t) // wide enough that computeLayout.ShowSide is true
	if m.expandedTab != tabOverview {
		t.Fatalf("start tab = %d, want Overview", m.expandedTab)
	}
	nm, _ := m.Update(tea.KeyPressMsg{Code: 'l', Text: "l"})
	if nm.(Model).expandedTab != tabDescription {
		t.Fatalf("after l, tab = %d, want Description", nm.(Model).expandedTab)
	}
}

func TestMainViewJumpsTabWithDigit(t *testing.T) {
	m := newTestModelWideWithPR(t)
	nm, _ := m.Update(tea.KeyPressMsg{Code: '2', Text: "2"})
	if nm.(Model).expandedTab != tabConversation {
		t.Fatalf("after 2, tab = %d, want Conversation(2)", nm.(Model).expandedTab)
	}
}
```

Build `newTestModelWideWithPR` from the existing test-model construction, setting `m.width = 160, m.height = 40` so `computeLayout(160,40).ShowSide` is true (see `layout_test.go:6`).

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/ -run 'TestMainViewCyclesTabsWithL|TestMainViewJumpsTabWithDigit'`
Expected: FAIL — `l` currently calls `enterExpanded`, so `expandedTab` is unchanged / expanded is entered.

- [ ] **Step 3: Rewire the main-view keys**

Replace the `"right", "l"` case (`prlist.go:1456`) and add siblings. Use a small helper so wide vs narrow is explicit:

```go
		case "right", "l":
			if m.mode != "pr" {
				return m, nil // tabs are PR-only in v1
			}
			if computeLayout(m.width, m.height).ShowSide {
				m.expandedTab = (m.expandedTab + 1) % len(expandedTabs)
				m.checkCursor = 0
				return m, nil
			}
			m.enterExpanded() // narrow: no pane, open full-screen tabs
			m.detailSeq++
			return m, m.debounceDetailCmd()
		case "left", "h":
			if m.mode != "pr" || !computeLayout(m.width, m.height).ShowSide {
				return m, nil
			}
			m.expandedTab = (m.expandedTab + len(expandedTabs) - 1) % len(expandedTabs)
			m.checkCursor = 0
			return m, nil
		case "1", "2", "3", "4", "5", "6":
			if m.mode != "pr" || !computeLayout(m.width, m.height).ShowSide {
				return m, nil
			}
			m.expandedTab = int(msg.String()[0] - '1')
			m.checkCursor = 0
			return m, nil
```

Change `"enter"` in the main view to maximize when a pane is present (keep existing enter-behavior for narrow). Locate the main-view `"enter"` case and set it to:

```go
		case "enter":
			if m.mode == "pr" && computeLayout(m.width, m.height).ShowSide {
				m.previewMax = !m.previewMax
				return m, nil
			}
			// narrow / issues: fall through to existing behavior
```

(If there is no distinct main-view `"enter"` case yet, add one alongside the tab cases above. Keep `z` at `prlist.go:1392` as-is — it remains a maximize alias.)

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/ui/`
Expected: PASS

- [ ] **Step 5: Manual smoke + commit**

Build and eyeball: `go build ./... && ./prdash` (wide terminal → `h`/`l`/`1`-`6` switch tabs in the pane, arrow keys still move the list, `Enter` maximizes; shrink terminal → `Enter` opens full-screen tabs).

```bash
git add internal/ui/prlist.go internal/ui/prlist_test.go
git commit -m "feat(ui): h/l + 1-6 switch detail tabs in the pane; Enter maximizes (#49)"
```

---

### Task 5: Update the legend/keybinding help for the new tab keys

**Files:**
- Modify: wherever the main-view legend groups are defined (search: `rg -n "expandedLegendGroups|legendGroups|h/l|switch tab" internal/ui`)
- Test: none (copy change); verify by building.

- [ ] **Step 1: Find the legend source**

Run: `rg -n "func .*[lL]egendGroups" internal/ui`

- [ ] **Step 2: Add the tab-navigation hints**

Add entries to the main-view legend for: `h/l tabs`, `1-6 jump`, `enter maximize`. Match the existing hint-string format used in that function (key + space + label).

- [ ] **Step 3: Build**

Run: `go build ./...`
Expected: builds clean.

- [ ] **Step 4: Commit**

```bash
git add internal/ui
git commit -m "docs(ui): legend hints for tab navigation keys (#49)"
```

---

## Phase 2 — Inline review comments

### Task 6: gh review-threads types, query builder, and parser

**Files:**
- Create: `internal/gh/threads.go`
- Create: `internal/gh/threads_test.go`
- Create: `internal/gh/testdata/reviewthreads.json` (captured GraphQL response)

**Interfaces:**
- Produces:
  - `type ThreadComment struct { Author string; Body string; CreatedAt time.Time }`
  - `type ReviewThread struct { Path string; Line int; IsResolved bool; Comments []ThreadComment }`
  - `func ReviewThreadsArgs(owner, repo string, number int) []string` — args for `gh api graphql`.
  - `func ParseReviewThreads(b []byte) ([]ReviewThread, error)`.

**Context:** `gh api graphql` returns `{"data":{"repository":{"pullRequest":{"reviewThreads":{"nodes":[...]}}}}}`. Each node has `isResolved`, `path`, `line`, `originalLine`, and `comments.nodes[]{author{login},body,createdAt}`. `line` is null on outdated threads → fall back to `originalLine`.

- [ ] **Step 1: Capture a real response as testdata**

Run against any PR with inline comments (replace OWNER/REPO/NUM):

```bash
gh api graphql -f query='query($owner:String!,$repo:String!,$num:Int!){repository(owner:$owner,name:$repo){pullRequest(number:$num){reviewThreads(first:100){nodes{isResolved path line originalLine comments(first:100){nodes{author{login} body createdAt}}}}}}}' -F owner=OWNER -F repo=REPO -F num=NUM > internal/gh/testdata/reviewthreads.json
```

If no such PR is handy, hand-write `internal/gh/testdata/reviewthreads.json` with two threads (one resolved, one unresolved with a reply) and one thread where `line` is null and `originalLine` is set.

- [ ] **Step 2: Write the failing test**

```go
// internal/gh/threads_test.go
package gh

import (
	"os"
	"testing"
)

func TestParseReviewThreads(t *testing.T) {
	b, err := os.ReadFile("testdata/reviewthreads.json")
	if err != nil {
		t.Fatal(err)
	}
	ts, err := ParseReviewThreads(b)
	if err != nil {
		t.Fatal(err)
	}
	if len(ts) == 0 {
		t.Fatal("no threads parsed")
	}
	var sawResolved, sawUnresolved, sawFallbackLine bool
	for _, th := range ts {
		if th.IsResolved {
			sawResolved = true
		} else {
			sawUnresolved = true
		}
		if th.Line <= 0 {
			t.Errorf("thread on %s has Line %d; originalLine fallback not applied", th.Path, th.Line)
		}
		if len(th.Comments) == 0 {
			t.Errorf("thread on %s has no comments", th.Path)
		}
		_ = sawFallbackLine
	}
	if !sawResolved || !sawUnresolved {
		t.Fatalf("want both resolved and unresolved threads; resolved=%v unresolved=%v", sawResolved, sawUnresolved)
	}
}

func TestReviewThreadsArgsShape(t *testing.T) {
	args := ReviewThreadsArgs("noamsto", "prdash", 49)
	if args[0] != "api" || args[1] != "graphql" {
		t.Fatalf("args should start with 'api graphql', got %v", args[:2])
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/gh/ -run TestParseReviewThreads`
Expected: FAIL — `ParseReviewThreads undefined`

- [ ] **Step 4: Implement `threads.go`**

```go
// internal/gh/threads.go
package gh

import (
	"encoding/json"
	"strconv"
	"time"
)

type ThreadComment struct {
	Author    string
	Body      string
	CreatedAt time.Time
}

type ReviewThread struct {
	Path       string
	Line       int
	IsResolved bool
	Comments   []ThreadComment
}

const reviewThreadsQuery = `query($owner:String!,$repo:String!,$num:Int!){repository(owner:$owner,name:$repo){pullRequest(number:$num){reviewThreads(first:100){nodes{isResolved path line originalLine comments(first:100){nodes{author{login} body createdAt}}}}}}}`

func ReviewThreadsArgs(owner, repo string, number int) []string {
	return []string{
		"api", "graphql",
		"-f", "query=" + reviewThreadsQuery,
		"-F", "owner=" + owner,
		"-F", "repo=" + repo,
		"-F", "num=" + strconv.Itoa(number),
	}
}

func ParseReviewThreads(b []byte) ([]ReviewThread, error) {
	var env struct {
		Data struct {
			Repository struct {
				PullRequest struct {
					ReviewThreads struct {
						Nodes []struct {
							IsResolved   bool   `json:"isResolved"`
							Path         string `json:"path"`
							Line         *int   `json:"line"`
							OriginalLine *int   `json:"originalLine"`
							Comments     struct {
								Nodes []struct {
									Author struct {
										Login string `json:"login"`
									} `json:"author"`
									Body      string    `json:"body"`
									CreatedAt time.Time `json:"createdAt"`
								} `json:"nodes"`
							} `json:"comments"`
						} `json:"nodes"`
					} `json:"reviewThreads"`
				} `json:"pullRequest"`
			} `json:"repository"`
		} `json:"data"`
	}
	if err := json.Unmarshal(b, &env); err != nil {
		return nil, err
	}
	nodes := env.Data.Repository.PullRequest.ReviewThreads.Nodes
	out := make([]ReviewThread, 0, len(nodes))
	for _, n := range nodes {
		line := 0
		if n.Line != nil {
			line = *n.Line
		} else if n.OriginalLine != nil {
			line = *n.OriginalLine
		}
		cs := make([]ThreadComment, 0, len(n.Comments.Nodes))
		for _, c := range n.Comments.Nodes {
			cs = append(cs, ThreadComment{Author: c.Author.Login, Body: c.Body, CreatedAt: c.CreatedAt})
		}
		out = append(out, ReviewThread{Path: n.Path, Line: line, IsResolved: n.IsResolved, Comments: cs})
	}
	return out, nil
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/gh/`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/gh/threads.go internal/gh/threads_test.go internal/gh/testdata/reviewthreads.json
git commit -m "feat(gh): parse PR review threads from graphql (#49)"
```

---

### Task 7: Fetch + cache review threads alongside PR detail

**Files:**
- Modify: `internal/ui/prlist.go:40-64` (add `threads map[int][]gh.ReviewThread`, `threadsFresh map[int]bool`) and the `NewModel` initializer (`prlist.go:108`)
- Modify: `internal/ui/preview.go` (add `threadsSchemaVer`, `threadsKey`, `fetchThreadsCmd`, `threadsMsg`, hook into `detailCmdForCursor`)
- Modify: `internal/ui/prlist.go` (handle `threadsMsg`; hydrate from cache near `hydrateDetail`)
- Test: `internal/ui/preview_test.go`

**Interfaces:**
- Produces: `m.threads[number] []gh.ReviewThread`; `func (m Model) fetchThreadsCmd(number int) tea.Cmd`; `type threadsMsg struct { number int; threads []gh.ReviewThread; raw []byte }`.
- Consumes: `gh.ReviewThreadsArgs`, `gh.ParseReviewThreads`, `m.repo`, `m.cache`.

**Context:** Mirror `fetchDetailCmd` (`preview.go:41`) and the `prDetailMsg` handler (`prlist.go:1085-1090`). Owner/repo: `m.repo` holds `"owner/name"`; split on `/` for the args. Cache with a dedicated schema version so a query change is a clean miss (like `detailSchemaVer`, `preview.go:25`).

- [ ] **Step 1: Write the failing test**

```go
// internal/ui/preview_test.go
func TestThreadsKeyIsRepoScoped(t *testing.T) {
	a := threadsKey("noamsto/prdash", 7)
	b := threadsKey("noamsto/other", 7)
	if a == b {
		t.Fatal("threadsKey must differ across repos")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/ -run TestThreadsKeyIsRepoScoped`
Expected: FAIL — `threadsKey undefined`

- [ ] **Step 3: Add fields, key, cmd, msg, handler, hydrate**

In the `Model` struct (`prlist.go:63` area) add:

```go
	threads      map[int][]gh.ReviewThread // inline review threads, per PR number
	threadsFresh map[int]bool              // PR numbers whose threads were refetched this session
```

Initialize them in `NewModel` where `detail`/`fresh` are made (search `detail: map[int]` in `NewModel`), e.g. `threads: map[int][]gh.ReviewThread{}, threadsFresh: map[int]bool{}`.

In `preview.go`, add near `detailKey`:

```go
const threadsSchemaVer = "v1"

func threadsKey(repo string, number int) string {
	return cache.Key("threads", repo+"#"+strconv.Itoa(number), 0, threadsSchemaVer)
}

type threadsMsg struct {
	number  int
	threads []gh.ReviewThread
	raw     []byte
}

// fetchThreadsCmd lazily loads the selected PR's inline review threads.
func (m Model) fetchThreadsCmd(number int) tea.Cmd {
	r, dir, repo := m.runner, m.dir, m.repo
	return func() tea.Msg {
		owner, name, ok := strings.Cut(repo, "/")
		if !ok {
			return fetchFailedMsg{err: fmt.Errorf("bad repo %q", repo)}
		}
		raw, err := r.Run(dir, gh.ReviewThreadsArgs(owner, name, number)...)
		if err != nil {
			return fetchFailedMsg{err: err}
		}
		ts, err := gh.ParseReviewThreads(raw)
		if err != nil {
			return fetchFailedMsg{err: err}
		}
		return threadsMsg{number: number, threads: ts, raw: raw}
	}
}
```

In `detailCmdForCursor` (`preview.go:89`, the `case "pr"` branch), batch the threads fetch with the detail fetch:

```go
	case "pr":
		var cmds []tea.Cmd
		if !m.fresh[v.Number] && !m.cacheFresh(detailKey(m.repo, v.Number)) {
			cmds = append(cmds, m.fetchDetailCmd(v.Number))
		}
		if !m.threadsFresh[v.Number] && !m.cacheFresh(threadsKey(m.repo, v.Number)) {
			cmds = append(cmds, m.fetchThreadsCmd(v.Number))
		}
		return tea.Batch(cmds...)
```

In the `Update` msg switch (near `case prDetailMsg:`, `prlist.go:1085`), add:

```go
	case threadsMsg:
		m.threads[msg.number] = msg.threads
		m.threadsFresh[msg.number] = true
		if m.cache != nil {
			m.cache.Set(threadsKey(m.repo, msg.number), msg.raw)
		}
		m.repaintActive()
		return m, nil
```

Add hydration mirroring `hydrateDetail` (`prlist.go:545-560`): for each shown PR without in-memory threads, read `threadsKey` from cache, `ParseReviewThreads`, store into `m.threads` (leave `threadsFresh` false so the live fetch still revalidates). Call it wherever `hydrateDetail` is called.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/ui/`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/ui/prlist.go internal/ui/preview.go internal/ui/preview_test.go
git commit -m "feat(ui): fetch + cache PR review threads per cursor (#49)"
```

---

### Task 8: Thread fold/summary logic (pure, in the preview package)

**Files:**
- Create: `internal/preview/threads.go`
- Create: `internal/preview/threads_test.go`

**Interfaces:**
- Produces:
  - `func Unresolved(ts []gh.ReviewThread) []gh.ReviewThread`
  - `func CountResolved(ts []gh.ReviewThread) int`
  - `func TopUnresolved(ts []gh.ReviewThread, n int) (top []gh.ReviewThread, more int)` — first n unresolved + count of the remaining unresolved.
  - `func GroupByFile(ts []gh.ReviewThread) []FileThreads` where `type FileThreads struct { Path string; Threads []gh.ReviewThread }`, files in first-seen order, unresolved threads before resolved within each file.

**Context:** Pure functions so both the Overview block and the Diff tab share one ordering/counting source. No lipgloss here.

- [ ] **Step 1: Write the failing tests**

```go
// internal/preview/threads_test.go
package preview

import (
	"testing"

	"github.com/noamsto/prdash/internal/gh"
)

func mkThread(path string, resolved bool) gh.ReviewThread {
	return gh.ReviewThread{Path: path, IsResolved: resolved, Comments: []gh.ThreadComment{{Author: "a", Body: "b"}}}
}

func TestTopUnresolved(t *testing.T) {
	ts := []gh.ReviewThread{
		mkThread("a.go", false), mkThread("b.go", true),
		mkThread("c.go", false), mkThread("d.go", false),
	}
	top, more := TopUnresolved(ts, 2)
	if len(top) != 2 {
		t.Fatalf("top len = %d, want 2", len(top))
	}
	if more != 1 {
		t.Fatalf("more = %d, want 1 (3 unresolved - 2 shown)", more)
	}
}

func TestCountResolved(t *testing.T) {
	ts := []gh.ReviewThread{mkThread("a.go", true), mkThread("b.go", false), mkThread("c.go", true)}
	if got := CountResolved(ts); got != 2 {
		t.Fatalf("CountResolved = %d, want 2", got)
	}
}

func TestGroupByFileOrdersUnresolvedFirst(t *testing.T) {
	ts := []gh.ReviewThread{mkThread("a.go", true), mkThread("a.go", false), mkThread("b.go", false)}
	groups := GroupByFile(ts)
	if len(groups) != 2 || groups[0].Path != "a.go" {
		t.Fatalf("groups = %+v, want a.go first", groups)
	}
	if groups[0].Threads[0].IsResolved {
		t.Fatal("within a file, unresolved thread must sort before resolved")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/preview/ -run 'TestTopUnresolved|TestCountResolved|TestGroupByFile'`
Expected: FAIL — undefined functions.

- [ ] **Step 3: Implement `threads.go`**

```go
// internal/preview/threads.go
package preview

import (
	"sort"

	"github.com/noamsto/prdash/internal/gh"
)

func Unresolved(ts []gh.ReviewThread) []gh.ReviewThread {
	out := make([]gh.ReviewThread, 0, len(ts))
	for _, t := range ts {
		if !t.IsResolved {
			out = append(out, t)
		}
	}
	return out
}

func CountResolved(ts []gh.ReviewThread) int {
	n := 0
	for _, t := range ts {
		if t.IsResolved {
			n++
		}
	}
	return n
}

// TopUnresolved returns the first n unresolved threads plus the count of
// unresolved threads beyond n.
func TopUnresolved(ts []gh.ReviewThread, n int) (top []gh.ReviewThread, more int) {
	u := Unresolved(ts)
	if len(u) <= n {
		return u, 0
	}
	return u[:n], len(u) - n
}

type FileThreads struct {
	Path    string
	Threads []gh.ReviewThread
}

// GroupByFile groups threads by path in first-seen order; within a file,
// unresolved threads sort before resolved.
func GroupByFile(ts []gh.ReviewThread) []FileThreads {
	order := []string{}
	byPath := map[string][]gh.ReviewThread{}
	for _, t := range ts {
		if _, seen := byPath[t.Path]; !seen {
			order = append(order, t.Path)
		}
		byPath[t.Path] = append(byPath[t.Path], t)
	}
	out := make([]FileThreads, 0, len(order))
	for _, p := range order {
		g := byPath[p]
		sort.SliceStable(g, func(i, j int) bool { return !g[i].IsResolved && g[j].IsResolved })
		out = append(out, FileThreads{Path: p, Threads: g})
	}
	return out
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/preview/`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/preview/threads.go internal/preview/threads_test.go
git commit -m "feat(preview): thread fold/group/count helpers (#49)"
```

---

### Task 9: Overview THREADS block

**Files:**
- Modify: `internal/ui/preview.go` (`renderOverview` — insert the THREADS section)
- Create: `internal/ui/threads_render.go` (`renderThreadsSummary`)
- Test: `internal/ui/preview_test.go`

**Interfaces:**
- Produces: `func renderThreadsSummary(ts []gh.ReviewThread, n, w int) string` — the block body (empty string when no unresolved threads).
- Consumes: `preview.TopUnresolved`, `preview.CountResolved`, thread types.

**Context:** Insert between the `review` and `latest` sections in `renderOverview`, only when the block is non-empty. Header count comes from the section label; the block body is the top-N lines + the "more/resolved" tail. Use `firstLine(body)` to keep each preview to one line.

- [ ] **Step 1: Write the failing test**

```go
// internal/ui/preview_test.go
func TestRenderThreadsSummaryEmptyWhenAllResolved(t *testing.T) {
	ts := []gh.ReviewThread{{Path: "a.go", IsResolved: true, Comments: []gh.ThreadComment{{Author: "x", Body: "y"}}}}
	if got := renderThreadsSummary(ts, 2, 60); got != "" {
		t.Fatalf("want empty for all-resolved, got %q", got)
	}
}

func TestRenderThreadsSummaryShowsFileAndAuthor(t *testing.T) {
	ts := []gh.ReviewThread{{Path: "internal/ui/preview.go", Line: 288, IsResolved: false,
		Comments: []gh.ThreadComment{{Author: "alice", Body: "allocates every frame"}}}}
	out := renderThreadsSummary(ts, 2, 80)
	for _, want := range []string{"preview.go:288", "alice", "allocates"} {
		if !strings.Contains(out, want) {
			t.Fatalf("summary missing %q:\n%s", want, out)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/ -run TestRenderThreadsSummary`
Expected: FAIL — `renderThreadsSummary undefined`

- [ ] **Step 3: Implement `renderThreadsSummary` + wire into Overview**

```go
// internal/ui/threads_render.go
package ui

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/noamsto/prdash/internal/gh"
	"github.com/noamsto/prdash/internal/preview"
)

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// renderThreadsSummary is the Overview THREADS block body: top-N unresolved
// threads then a "more / resolved hidden" tail. Empty when nothing is unresolved.
func renderThreadsSummary(ts []gh.ReviewThread, n, w int) string {
	top, more := preview.TopUnresolved(ts, n)
	if len(top) == 0 {
		return ""
	}
	var b strings.Builder
	for _, t := range top {
		loc := fmt.Sprintf("%s:%d", filepath.Base(t.Path), t.Line)
		author := ""
		body := ""
		if len(t.Comments) > 0 {
			author = t.Comments[0].Author
			body = firstLine(t.Comments[0].Body)
		}
		b.WriteString(focusBarStyle.Render(loc) + "  " + authorStyle(author).Render(author) + "\n")
		b.WriteString("  " + dimStyle.Render(truncate(body, w-2)) + "\n")
	}
	tail := []string{}
	if more > 0 {
		tail = append(tail, fmt.Sprintf("%d more", more))
	}
	if r := preview.CountResolved(ts); r > 0 {
		tail = append(tail, fmt.Sprintf("%d resolved hidden", r))
	}
	if len(tail) > 0 {
		b.WriteString(dimStyle.Render("▸ " + strings.Join(tail, " · ")))
	}
	return strings.TrimRight(b.String(), "\n")
}
```

In `renderOverview` (Task 2), after the `review` section and before `latest`, insert:

```go
	if ts := m.threads[v.Number]; len(ts) > 0 {
		label := fmt.Sprintf("threads  %d unresolved", len(preview.Unresolved(ts)))
		if body := renderThreadsSummary(ts, m.previewN, bw); body != "" {
			blocks = append(blocks, sectionRule(label, w)+"\n"+indentLines(body, 2))
		}
	}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/ui/`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/ui/threads_render.go internal/ui/preview.go internal/ui/preview_test.go
git commit -m "feat(ui): top unresolved threads block on the Overview tab (#49)"
```

---

### Task 10: Diff tab — threads grouped by file with resolved-collapse

**Files:**
- Modify: `internal/ui/expanded.go:105-127` (`renderDiffstat` → interleave threads under each file)
- Modify: `internal/ui/threads_render.go` (add `renderFileThreads`)
- Modify: `internal/ui/expanded.go:174-175` (`tabDiff` case passes threads)
- Test: `internal/ui/expanded_test.go`

**Interfaces:**
- Produces: `func renderFileThreads(g preview.FileThreads, w int, showResolved bool) string`; `renderDiffstat` gains a threads parameter.
- Consumes: `preview.GroupByFile`, `m.threads`.

**Context:** `renderDiffstat(d, w)` currently prints a totals line + one row per file. Change its signature to `renderDiffstat(d gh.PRDetail, threads []gh.ReviewThread, w int) string` and, per file, append its threads (from a path→FileThreads map built via `preview.GroupByFile`). v1 collapses resolved threads by default (`showResolved=false`) — render a `▸ N resolved` line instead of their bodies. Update the `tabDiff` case and any other `renderDiffstat` caller (search: `rg -n renderDiffstat internal/ui`).

- [ ] **Step 1: Write the failing test**

```go
// internal/ui/expanded_test.go
func TestRenderFileThreadsHidesResolvedBodies(t *testing.T) {
	g := preview.FileThreads{Path: "a.go", Threads: []gh.ReviewThread{
		{Path: "a.go", Line: 10, IsResolved: false, Comments: []gh.ThreadComment{{Author: "alice", Body: "fix this"}}},
		{Path: "a.go", Line: 20, IsResolved: true, Comments: []gh.ThreadComment{{Author: "bob", Body: "old nit"}}},
	}}
	out := renderFileThreads(g, 80, false)
	if !strings.Contains(out, "fix this") {
		t.Fatalf("unresolved body must show:\n%s", out)
	}
	if strings.Contains(out, "old nit") {
		t.Fatalf("resolved body must be hidden by default:\n%s", out)
	}
	if !strings.Contains(out, "1 resolved") {
		t.Fatalf("resolved count line missing:\n%s", out)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/ -run TestRenderFileThreadsHidesResolvedBodies`
Expected: FAIL — `renderFileThreads undefined`

- [ ] **Step 3: Implement `renderFileThreads` and interleave into `renderDiffstat`**

Add to `threads_render.go`:

```go
// renderFileThreads renders one file's threads: unresolved with bodies, resolved
// collapsed to a count line unless showResolved.
func renderFileThreads(g preview.FileThreads, w int, showResolved bool) string {
	var b strings.Builder
	resolved := 0
	for _, t := range g.Threads {
		if t.IsResolved && !showResolved {
			resolved++
			continue
		}
		dot := failStyle.Render("●") + " " + failStyle.Render("unresolved")
		if t.IsResolved {
			dot = passStyle.Render("✓ resolved")
		}
		if len(t.Comments) == 0 {
			continue
		}
		head := t.Comments[0]
		b.WriteString("    " + focusBarStyle.Render(fmt.Sprintf("L%d", t.Line)) + "  " +
			authorStyle(head.Author).Render(head.Author) + "   " + dot + "\n")
		b.WriteString("      " + dimStyle.Render(truncate(firstLine(head.Body), w-6)) + "\n")
		for _, reply := range t.Comments[1:] {
			b.WriteString("      " + sepStyle.Render("└ ") + authorStyle(reply.Author).Render(reply.Author) + "\n")
			b.WriteString("        " + dimStyle.Render(truncate(firstLine(reply.Body), w-8)) + "\n")
		}
	}
	if resolved > 0 {
		b.WriteString("    " + dimStyle.Render(fmt.Sprintf("▸ %d resolved", resolved)) + "\n")
	}
	return b.String()
}
```

Change `renderDiffstat` (`expanded.go:105`) signature and body to interleave threads. Build a path→FileThreads map from `preview.GroupByFile(threads)` and, after each file's stat row, append `renderFileThreads` when that file has threads:

```go
func renderDiffstat(d gh.PRDetail, threads []gh.ReviewThread, w int) string {
	s := d.Diffstat()
	if s.Files == 0 {
		return dimStyle.Render("  No file changes.")
	}
	byFile := map[string]preview.FileThreads{}
	for _, g := range preview.GroupByFile(threads) {
		byFile[g.Path] = g
	}
	var b strings.Builder
	unresolved := len(preview.Unresolved(threads))
	b.WriteString(fmt.Sprintf("  %s files  %s  %s     %s · %s\n\n",
		accentStyle.Render(fmt.Sprintf("%d", s.Files)),
		passStyle.Render(fmt.Sprintf("+%d", s.Additions)),
		failStyle.Render(fmt.Sprintf("-%d", s.Deletions)),
		failStyle.Render(fmt.Sprintf("%d unresolved", unresolved)),
		passStyle.Render(fmt.Sprintf("%d resolved", preview.CountResolved(threads)))))
	paths := make([]string, len(d.Files))
	pathW := 0
	for i, f := range d.Files {
		paths[i] = truncate(f.Path, w-16)
		if l := lipgloss.Width(paths[i]); l > pathW {
			pathW = l
		}
	}
	for i, f := range d.Files {
		pad := strings.Repeat(" ", pathW-lipgloss.Width(paths[i]))
		b.WriteString(fmt.Sprintf("  %s%s  %s %s\n", paths[i], pad,
			passStyle.Render(fmt.Sprintf("+%d", f.Additions)), failStyle.Render(fmt.Sprintf("-%d", f.Deletions))))
		if g, ok := byFile[f.Path]; ok {
			b.WriteString(renderFileThreads(g, w, false))
		}
	}
	return b.String()
}
```

Add the `preview` import to `expanded.go` if not present. Update the `tabDiff` case (`expanded.go:174`):

```go
	case tabDiff:
		return renderDiffstat(d, m.threads[v.Number], w)
```

(`v` is already in scope in `expandedBody`.) Fix any other `renderDiffstat(` caller to pass threads (pass `nil` if none is available there).

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/ui/`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/ui/expanded.go internal/ui/threads_render.go internal/ui/expanded_test.go
git commit -m "feat(ui): inline review threads under files in the Diff tab (#49)"
```

---

### Task 11: Full-suite verification + manual smoke

**Files:** none (verification task)

- [ ] **Step 1: Run the full test suite**

Run: `go test ./...`
Expected: all PASS.

- [ ] **Step 2: Vet + build**

Run: `go vet ./... && go build ./...`
Expected: clean.

- [ ] **Step 3: Manual smoke against a real PR with inline comments**

Run: `./prdash` on a repo/PR that has unresolved and resolved inline comments. Verify:
- Overview shows the `THREADS  N unresolved` block with top items; resolved hidden.
- `l`/`2` reaches the Diff tab; threads appear under their files; resolved collapse to `▸ N resolved`.
- Arrow keys move the list and the active tab re-renders live.
- Narrow the terminal below the side threshold → `Enter` opens the full-screen tabbed view with the same content.

- [ ] **Step 4: Update the memory pointer**

Append a line to the prdash status memory noting issue #49 landed (tabbed detail pane + inline comments).

- [ ] **Step 5: Push + open PR**

```bash
git push -u origin feat/49-tabbed-detail-inline-comments
gh pr create --assignee @me --title "feat(ui): unified tabbed detail pane + inline review comments" --body "Closes #49"
```

---

## Self-Review

**Spec coverage:**
- Unified tabbed component, two containers → Tasks 1–4.
- Overview default tab with triage summary → Tasks 2, 3.
- `h`/`l` + `1`–`6` nav, `Enter` maximize, narrow fallback → Task 4.
- GraphQL `reviewThreads` data source → Task 6.
- Fetch + cache parallel to detail → Task 7.
- Unresolved-first fold/group logic → Task 8.
- Overview THREADS block → Task 9.
- Diff-tab threads grouped by file, resolved-collapse → Task 10.
- Verification + PR → Task 11.

**Placeholder scan:** No TBD/TODO; every code step shows real code; every command has expected output.

**Type consistency:** `ReviewThread`/`ThreadComment` defined in Task 6, consumed in 7–10. `FileThreads`/`TopUnresolved`/`GroupByFile`/`Unresolved`/`CountResolved` defined in Task 8, consumed in 9–10. `renderThreadsSummary`/`renderFileThreads`/`firstLine` defined in 9–10. `renderDiffstat` signature change (Task 10) flagged to update callers. `tabOverview` and the 6-tab set (Task 1) used throughout.
