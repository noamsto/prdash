# prdash Plan 4 — Preview pane + folding comments

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans. Steps use checkbox (`- [ ]`) syntax.

**Goal:** A preview pane for the selected PR with proper markdown (tables + chroma, no pipe-strip) and a comment timeline folded "latest N expanded, older collapsed".

**Architecture:** On selection change, fetch the PR detail (`gh pr view --json comments,reviews,latestReviews`) lazily and cache it. Assemble a time-sorted timeline (conversation comments + review summaries), render each body through a glamour renderer (gh-dash's chroma dark style, ported verbatim **minus** the table-stripping regex). Fold to the latest `N`, with an expander for older.

**Tech Stack:** Go 1.23, bubbletea v1, `github.com/charmbracelet/glamour`. Builds on Plans 1–3. Spec: `2026-06-22-prdash-design.md`.

---

## File structure
- `internal/gh/prview.go` — PR detail fetch (comments + reviews)
- `internal/preview/timeline.go` — timeline assembly + fold
- `internal/preview/render.go` — glamour renderer (ported style, no strip)
- `internal/ui/preview.go` — preview pane + lazy fetch on selection

---

## Task 1: PR detail fetch

**Files:** Create `internal/gh/prview.go`, `internal/gh/prview_test.go`

- [ ] **Step 1: Failing test**

```go
package gh

import "testing"

func TestParsePRDetail(t *testing.T) {
	d, err := ParsePRDetail([]byte(`{
		"comments":[{"author":{"login":"a"},"body":"hi","createdAt":"2026-06-01T10:00:00Z"}],
		"reviews":[{"author":{"login":"b"},"body":"LGTM","state":"APPROVED","submittedAt":"2026-06-02T10:00:00Z"}]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Comments) != 1 || d.Comments[0].Author.Login != "a" {
		t.Fatalf("comments=%+v", d.Comments)
	}
	if len(d.Reviews) != 1 || d.Reviews[0].State != "APPROVED" {
		t.Fatalf("reviews=%+v", d.Reviews)
	}
}
```

- [ ] **Step 2: Run, verify fail.**

- [ ] **Step 3: Implement `internal/gh/prview.go`**

```go
package gh

import (
	"encoding/json"
	"strconv"
	"time"
)

type Comment struct {
	Author    struct{ Login string `json:"login"` } `json:"author"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"createdAt"`
}

type Review struct {
	Author      struct{ Login string `json:"login"` } `json:"author"`
	Body        string    `json:"body"`
	State       string    `json:"state"`
	SubmittedAt time.Time `json:"submittedAt"`
}

type PRDetail struct {
	Comments []Comment `json:"comments"`
	Reviews  []Review  `json:"reviews"`
}

func PRViewArgs(number int) []string {
	return []string{"pr", "view", strconv.Itoa(number), "--json", "comments,reviews,latestReviews"}
}

func FetchPRDetail(r Runner, dir string, number int) (PRDetail, error) {
	out, err := r.Run(dir, PRViewArgs(number)...)
	if err != nil {
		return PRDetail{}, err
	}
	return ParsePRDetail(out)
}

func ParsePRDetail(b []byte) (PRDetail, error) {
	var d PRDetail
	err := json.Unmarshal(b, &d)
	return d, err
}
```

- [ ] **Step 4: Run, verify pass. Step 5: Commit** — `feat(gh): PR detail (comments + reviews) fetch`.

---

## Task 2: Timeline assembly + fold

**Files:** Create `internal/preview/timeline.go`, `internal/preview/timeline_test.go`

- [ ] **Step 1: Failing test**

```go
package preview

import (
	"testing"
	"time"

	"github.com/noamsto/prdash/internal/gh"
)

func at(s string) time.Time { t, _ := time.Parse(time.RFC3339, s); return t }

func TestTimelineSortedAndFolded(t *testing.T) {
	d := gh.PRDetail{
		Comments: []gh.Comment{
			{Body: "c1", CreatedAt: at("2026-06-01T10:00:00Z")},
			{Body: "c3", CreatedAt: at("2026-06-03T10:00:00Z")},
		},
		Reviews: []gh.Review{
			{Body: "r2", State: "APPROVED", SubmittedAt: at("2026-06-02T10:00:00Z")},
		},
	}
	items := Timeline(d)
	if len(items) != 3 || items[0].Body != "c1" || items[2].Body != "c3" {
		t.Fatalf("order wrong: %+v", items)
	}

	older, latest := Fold(items, 2)
	if older != 1 || len(latest) != 2 || latest[0].Body != "r2" {
		t.Fatalf("fold wrong: older=%d latest=%+v", older, latest)
	}
}
```

- [ ] **Step 2: Run, verify fail.**

- [ ] **Step 3: Implement `internal/preview/timeline.go`**

```go
package preview

import (
	"sort"
	"time"

	"github.com/noamsto/prdash/internal/gh"
)

type Kind int

const (
	KindComment Kind = iota
	KindReview
)

type Item struct {
	Author string
	Body   string
	At     time.Time
	Kind   Kind
	State  string // review state, when KindReview
}

// Timeline merges conversation comments and review summaries, sorted oldest→newest.
func Timeline(d gh.PRDetail) []Item {
	items := make([]Item, 0, len(d.Comments)+len(d.Reviews))
	for _, c := range d.Comments {
		items = append(items, Item{Author: c.Author.Login, Body: c.Body, At: c.CreatedAt, Kind: KindComment})
	}
	for _, r := range d.Reviews {
		if r.Body == "" && r.State == "" {
			continue
		}
		items = append(items, Item{Author: r.Author.Login, Body: r.Body, At: r.SubmittedAt, Kind: KindReview, State: r.State})
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].At.Before(items[j].At) })
	return items
}

// Fold returns (olderCount, latestN). The latest n items are shown expanded; the
// rest collapse behind a "▸ {older} earlier" row.
func Fold(items []Item, n int) (int, []Item) {
	if len(items) <= n {
		return 0, items
	}
	return len(items) - n, items[len(items)-n:]
}
```

- [ ] **Step 4: Run, verify pass. Step 5: Commit** — `feat(preview): timeline assembly + fold (latest N)`.

---

## Task 3: Markdown renderer (chroma, no pipe-strip)

**Files:** Create `internal/preview/render.go`, `internal/preview/theme.go`, `internal/preview/render_test.go`

- [ ] **Step 1: Failing test**

```go
package preview

import (
	"strings"
	"testing"
)

func TestRenderInlineCodeAndTable(t *testing.T) {
	out, err := Render("Use `go test`.\n\n| a | b |\n|---|---|\n| 1 | 2 |\n", 80)
	if err != nil {
		t.Fatal(err)
	}
	// table content must survive (no pipe-strip), and inline code present.
	if !strings.Contains(out, "go test") {
		t.Fatalf("inline code missing: %q", out)
	}
	if !strings.Contains(out, "1") || !strings.Contains(out, "2") {
		t.Fatalf("table content stripped: %q", out)
	}
}
```

- [ ] **Step 2: Run, verify fail.**

- [ ] **Step 3: Port the chroma style**

Create `internal/preview/theme.go` by copying gh-dash's `internal/tui/markdown/theme.go`
`CustomDarkStyleConfig` **verbatim** (the `ansi.StyleConfig` with the `Code`,
`CodeBlock.Chroma`, headings, list, etc.), adjusting the import to
`github.com/charmbracelet/glamour/ansi`. Expose it as `var darkStyle = ansi.StyleConfig{…}`.
**Do not** port `lineCleanupRegex` — its absence is the fix.

- [ ] **Step 4: Implement `internal/preview/render.go`**

```go
package preview

import "github.com/charmbracelet/glamour"

// Render renders markdown to ANSI at the given wrap width. No pipe-stripping —
// tables and pipe-containing code render normally.
func Render(md string, width int) (string, error) {
	r, err := glamour.NewTermRenderer(
		glamour.WithStyles(darkStyle),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		return "", err
	}
	return r.Render(md)
}
```

- [ ] **Step 5: Run, verify pass. Step 6: Commit** — `feat(preview): glamour renderer (chroma, no pipe-strip)`.

---

## Task 4: Preview pane in the UI (lazy fetch on selection)

**Files:** Create `internal/ui/preview.go`, `internal/ui/preview_test.go`; modify `prlist.go`

- [ ] **Step 1: Failing test (fold renders N + older marker)**

```go
package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/noamsto/prdash/internal/preview"
)

func TestRenderPreviewBodyShowsOlderMarker(t *testing.T) {
	items := make([]preview.Item, 5)
	for i := range items {
		items[i] = preview.Item{Author: "a", Body: "msg", At: time.Unix(int64(i), 0), Kind: preview.KindComment}
	}
	out := renderTimeline(items, 3, 80, false) // n=3, not expanded
	if !strings.Contains(out, "earlier") {
		t.Fatalf("expected older marker: %q", out)
	}
}
```

- [ ] **Step 2: Run, verify fail.**

- [ ] **Step 3: Implement `internal/ui/preview.go`**

```go
package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/noamsto/prdash/internal/gh"
	"github.com/noamsto/prdash/internal/preview"
)

type prDetailMsg struct {
	number int
	detail gh.PRDetail
}

// fetchDetailCmd lazily loads the selected PR's comments/reviews.
func (m Model) fetchDetailCmd(number int) tea.Cmd {
	r, dir := m.runner, m.dir
	return func() tea.Msg {
		d, err := gh.FetchPRDetail(r, dir, number)
		if err != nil {
			return fetchFailedMsg{err}
		}
		return prDetailMsg{number: number, detail: d}
	}
}

// renderTimeline renders the latest n items expanded, older collapsed.
func renderTimeline(items []preview.Item, n, width int, expanded bool) string {
	older, latest := preview.Fold(items, n)
	if expanded {
		older, latest = 0, items
	}
	var b strings.Builder
	if older > 0 {
		b.WriteString(fmt.Sprintf("▸ %d earlier comments\n\n", older))
	}
	for _, it := range latest {
		hdr := "@" + it.Author
		if it.Kind == preview.KindReview && it.State != "" {
			hdr += " · " + it.State
		}
		body, _ := preview.Render(it.Body, width)
		b.WriteString(hdr + "\n" + body + "\n")
	}
	return b.String()
}
```

In `prlist.go`: add `detail map[int]gh.PRDetail`, `previewExpanded bool`, `previewN int` (default 3). On cursor move (or on `prsFetchedMsg`), if the cursor PR's detail isn't cached, return `m.fetchDetailCmd(number)`. Handle `prDetailMsg` by storing `m.detail[number]`. Bind a key (e.g. `tab`) to toggle `previewExpanded`. Render the right pane via `lipgloss.JoinHorizontal(table.View(), renderTimeline(...))` sized to `preview.width` (default 0.45 of screen width).

- [ ] **Step 4: Run, verify pass. Step 5: Commit** — `feat(ui): preview pane with folded comment timeline`.

---

## Self-review
- **Spec coverage:** PR detail fetch ✓ T1, timeline (comments + review summaries, no inline threads) ✓ T2, glamour+chroma no-pipe-strip ✓ T3, fold model A (latest N + older expander) ✓ T4, preview pane + lazy fetch + expand toggle ✓ T4. Inline review-threads remain deferred per spec.
- **Types:** `gh.PRDetail/Comment/Review/PRViewArgs/FetchPRDetail`, `preview.Item/Timeline/Fold/Render`, `ui.fetchDetailCmd/renderTimeline` consistent.
- **Placeholders:** none. (Task 3 Step 3 ports an existing style file verbatim — the only "copy", and an intentional reuse, not a stub.)
