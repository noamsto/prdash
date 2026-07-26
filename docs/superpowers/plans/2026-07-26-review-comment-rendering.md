# Review Comment Rendering Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make PR review comments readable in prdash — render their markdown, drop bot badge/URL noise, and show the diff context each comment was written against.

**Architecture:** Two seams in `internal/preview`, so the Conversation tab benefits alongside the threads UI. `Render` gains a markdown pre-pass that deletes inline images and collapses links to their label; a new `PlainTitle` distills a body to one plain line for summary contexts. The glamour style configs stop printing literal heading markers. Then `internal/gh` selects the per-comment `diffHunk` already available in the existing threads query, and `renderFileThreads` paints it above a fully-rendered body.

**Tech Stack:** Go, `charm.land/glamour/v2` (markdown→ANSI), `charm.land/lipgloss/v2` (styling), githubv4 GraphQL.

## Global Constraints

- Branch: `feat/55-review-comment-rendering`, worktree `~/Data/git/.worktrees/noamsto/prdash/feat-55-review-comment-rendering`. Already rebased onto `main` including #53.
- Spec: `docs/superpowers/specs/2026-07-26-review-comment-rendering-design.md`. Read it before starting.
- Two PRs. Tasks 1–4 are **PR A** (rendering, no GraphQL change, no cache bump). Tasks 5–6 are **PR B** (`diffHunk` + layout, `threadsSchemaVer` v1→v2). PR A merges before PR B opens.
- DoD per task: `go build ./...`, `go vet ./...`, `go test ./...` green; `golangci-lint run` introduces no new findings. Pre-existing findings are out of scope and must not be "fixed": `internal/ui/expanded.go` QF1012 ×2, `internal/cache/cache.go` errcheck ×4, `internal/action/handoff.go` errcheck ×1, `main.go` errcheck ×1.
- One conventional commit per task, each referencing `#55`. Do NOT push to `main`, do NOT merge.
- Do NOT reformat or restructure code the task doesn't name.

## File Structure

| File | Responsibility | Task |
|---|---|---|
| `internal/preview/theme.go` | modify — wrap stock glamour configs to drop heading markers | 1 |
| `internal/preview/plaintext.go` | **create** — shared markdown regexes, `prePass`, `PlainTitle` | 2, 3 |
| `internal/preview/plaintext_test.go` | **create** — table tests for both | 2, 3 |
| `internal/preview/render.go` | modify — call `prePass` inside `Render` | 2 |
| `internal/preview/render_test.go` | modify — end-to-end no-`###`/no-URL assertions | 1, 2 |
| `internal/ui/threads_render.go` | modify — `PlainTitle` swap (4), hunk block + full body (6) | 4, 6 |
| `internal/ui/threads_render_test.go` | **create** — render assertions | 4, 6 |
| `internal/gh/threads.go` | modify — `diffHunk` in query, `ThreadComment.DiffHunk` | 5 |
| `internal/gh/testdata/reviewthreads.json` | modify — add `diffHunk` to fixture | 5 |
| `internal/gh/threads_test.go` | modify — assert `DiffHunk` parses | 5 |
| `internal/ui/preview.go` | modify — `threadsSchemaVer` v1→v2 | 5 |

`prePass` and `PlainTitle` share the same four regexes, so they live in one file. Both are markdown→plainer-markdown/text transforms; that is the file's single responsibility.

---

# PR A — rendering

## Task 1: Stop glamour printing literal heading markers

Stock `styles.DarkStyleConfig` sets `H3.Prefix = "### "`, so a Cursor BugBot finding titled with `###` renders the marker verbatim in the pane.

**Files:**
- Modify: `internal/preview/theme.go` (whole file, 17 lines)
- Test: `internal/preview/render_test.go` (append)

**Interfaces:**
- Consumes: nothing
- Produces: `darkStyle`, `lightStyle` (`ansi.StyleConfig`) — unchanged names and types, so `Render`/`SetMode` need no edit

- [ ] **Step 1: Write the failing test**

Append to `internal/preview/render_test.go`:

```go
func TestRenderDropsHeadingMarkers(t *testing.T) {
	out, err := Render("### Advisory lost after commit retry\n", 80)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "###") {
		t.Errorf("heading marker leaked into output: %q", out)
	}
	if !strings.Contains(out, "Advisory lost after commit retry") {
		t.Errorf("heading text missing: %q", out)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/preview/ -run TestRenderDropsHeadingMarkers -v`
Expected: FAIL — `heading marker leaked into output:` followed by output containing `### Advisory lost…`

- [ ] **Step 3: Write the implementation**

Replace the whole body of `internal/preview/theme.go` below the imports:

```go
package preview

import (
	"charm.land/glamour/v2/ansi"
	"charm.land/glamour/v2/styles"
)

// noHeadingMarkers strips glamour's literal "#"-marker prefixes from H1-H6 and
// bolds the heading instead. Bot review comments lead with a markdown heading
// (Cursor BugBot titles its findings with ###), and the stock configs print the
// marker verbatim into the pane.
//
// s is a copy, and Bold is replaced rather than written through, so the stock
// package-level configs are left intact.
func noHeadingMarkers(s ansi.StyleConfig) ansi.StyleConfig {
	bold := true
	for _, h := range []*ansi.StyleBlock{&s.H1, &s.H2, &s.H3, &s.H4, &s.H5, &s.H6} {
		h.Prefix = ""
		h.Bold = &bold
	}
	return s
}

// darkStyle/lightStyle are glamour's built-in chroma styles, minus the heading
// markers. We deliberately do NOT post-process rendered output (no
// pipe-stripping), so tables render intact.
var (
	darkStyle  = noHeadingMarkers(styles.DarkStyleConfig)
	lightStyle = noHeadingMarkers(styles.LightStyleConfig)
)

// activeStyle is what Render builds renderers from; SetMode swaps it.
var activeStyle = darkStyle
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/preview/ -v`
Expected: PASS, including the pre-existing `TestRenderInlineCodeAndTable` and `TestSetModeChangesOutputAndFlushes` (light and dark still differ by color, so the flush assertion is unaffected).

- [ ] **Step 5: Commit**

```bash
git add internal/preview/theme.go internal/preview/render_test.go
git commit -m "fix(preview): stop glamour printing literal heading markers (#55)"
```

---

## Task 2: Markdown pre-pass — drop images, collapse links

Glamour renders an inline image as `Image: alt → url` and a link as `text url`. Bot severity badges are images, so a codex comment prints `P1 Badge https://img.shields.io/badge/P1-orange?style=flat`. This is **not** style-configurable — `ansi/image.go` gates URL output on `ImageElement.TextOnly` and `ansi/link.go` on `LinkElement.SkipHref`, and `ansi/elements.go` sets both only when `isInsideTable(node)`.

**Files:**
- Create: `internal/preview/plaintext.go`
- Create: `internal/preview/plaintext_test.go`
- Modify: `internal/preview/render.go:46`
- Test: `internal/preview/render_test.go` (append)

**Interfaces:**
- Consumes: nothing
- Produces: `prePass(md string) string` (unexported); package vars `mdImage`, `mdLink`, `htmlTag`, `mdEmph`, `wsRun` (`*regexp.Regexp`) — Task 3 reuses all five

- [ ] **Step 1: Write the failing test**

Create `internal/preview/plaintext_test.go`:

```go
package preview

import "testing"

func TestPrePass(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{
			name: "image stripped entirely",
			in:   "![P1 Badge](https://img.shields.io/badge/P1-orange?style=flat)  Handle nulls",
			want: "  Handle nulls",
		},
		{
			name: "link collapses to its label",
			in:   "see [the schema](https://github.com/x/y/blob/main/a.sql#L1) for detail",
			want: "see the schema for detail",
		},
		{
			name: "plain prose untouched",
			in:   "The error rolls back the entire binding transaction.",
			want: "The error rolls back the entire binding transaction.",
		},
		{
			name: "index expression inside a fence is not mistaken for a link",
			in:   "```go\nv := arr[0](ctx)\n```",
			want: "```go\nv := arr[0](ctx)\n```",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := prePass(c.in); got != c.want {
				t.Errorf("prePass() = %q, want %q", got, c.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/preview/ -run TestPrePass -v`
Expected: FAIL to build — `undefined: prePass`

- [ ] **Step 3: Write the implementation**

Create `internal/preview/plaintext.go`:

```go
package preview

import (
	"regexp"
	"strings"
)

// Markdown inline patterns shared by prePass and PlainTitle.
//
// mdImage must be applied before mdLink: an image is a link with a leading "!",
// so collapsing links first would consume the "[alt](url)" part and leave a
// stray "!" behind.
//
// mdEmph deliberately omits "_": underscore emphasis is rare in review-comment
// titles, while snake_case identifiers are common, and stripping "_" would turn
// substitution_value into substitutionvalue.
var (
	mdImage = regexp.MustCompile(`!\[[^\]]*\]\([^)]*\)`)
	mdLink  = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`)
	htmlTag = regexp.MustCompile(`</?[a-zA-Z][^>]*>`)
	mdEmph  = regexp.MustCompile("[*`]+")
	wsRun   = regexp.MustCompile(`\s+`)
)

// prePass strips the inline markdown glamour insists on expanding to "text url"
// in a terminal: images go entirely (bot severity badges are images) and links
// collapse to their label. Glamour still emits the OSC 8 hyperlink from the AST,
// so links stay clickable — only the printed URL goes away.
//
// Fenced code blocks pass through untouched: a line like arr[0](ctx) inside a
// fence matches mdLink and would otherwise be silently corrupted.
func prePass(md string) string {
	lines := strings.Split(md, "\n")
	inFence := false
	for i, ln := range lines {
		if strings.HasPrefix(strings.TrimSpace(ln), "```") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		s := mdImage.ReplaceAllString(ln, "")
		lines[i] = mdLink.ReplaceAllString(s, "$1")
	}
	return strings.Join(lines, "\n")
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/preview/ -run TestPrePass -v`
Expected: PASS, all four subtests.

- [ ] **Step 5: Wire prePass into Render**

In `internal/preview/render.go`, change line 46 from:

```go
	out, err := r.Render(md)
```

to:

```go
	out, err := r.Render(prePass(md))
```

The `outputByKey` cache key stays keyed on the original `md` (line 29) — `prePass` is deterministic, so the same input still maps to the same output and the memoization is unaffected.

- [ ] **Step 6: Write the end-to-end test**

Append to `internal/preview/render_test.go`:

```go
func TestRenderDropsBadgeAndURLNoise(t *testing.T) {
	const md = "**![P1 Badge](https://img.shields.io/badge/P1-orange?style=flat)  Handle nulls**\n\n" +
		"See [the schema](https://github.com/x/y/blob/main/a.sql) for detail.\n"
	out, err := Render(md, 80)
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"img.shields.io", "Image:", "github.com/x/y"} {
		if strings.Contains(out, bad) {
			t.Errorf("%q leaked into output: %q", bad, out)
		}
	}
	if !strings.Contains(out, "Handle nulls") || !strings.Contains(out, "the schema") {
		t.Errorf("meaningful text missing: %q", out)
	}
}
```

- [ ] **Step 7: Run the full package tests**

Run: `go test ./internal/preview/ -v`
Expected: PASS. `TestRenderInlineCodeAndTable` still passes — its markdown has no images or links.

- [ ] **Step 8: Commit**

```bash
git add internal/preview/plaintext.go internal/preview/plaintext_test.go internal/preview/render.go internal/preview/render_test.go
git commit -m "fix(preview): drop badge images and printed URLs from rendered markdown (#55)"
```

---

## Task 3: `PlainTitle` — one plain line for summary contexts

Glamour emits multi-line styled blocks. The Overview `THREADS` block needs exactly one line of plain text, so it needs a different function, not a `Render` mode.

**Files:**
- Modify: `internal/preview/plaintext.go` (append)
- Modify: `internal/preview/plaintext_test.go` (append)

**Interfaces:**
- Consumes: `mdImage`, `mdLink`, `htmlTag`, `mdEmph`, `wsRun` from Task 2
- Produces: `PlainTitle(body string) string` — exported; Task 4 calls it from `internal/ui`

- [ ] **Step 1: Write the failing test**

Append to `internal/preview/plaintext_test.go`:

```go
func TestPlainTitle(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{
			name: "heading marker trimmed",
			in:   "### Advisory lost after commit retry\n\nMedium Severity",
			want: "Advisory lost after commit retry",
		},
		{
			name: "badge in bold in sub tags",
			in:   "**<sub><sub>![P1 Badge](https://img.shields.io/badge/P1-orange?style=flat)</sub></sub>  Handle nullable sibling values without aborting persistence**",
			want: "Handle nullable sibling values without aborting persistence",
		},
		{
			name: "badge-only first line falls through to the next",
			in:   "![P2 Badge](https://img.shields.io/badge/P2-yellow?style=flat)\n\nNormalize currency-formatted values",
			want: "Normalize currency-formatted values",
		},
		{
			name: "snake_case identifier survives",
			in:   "`substitution_value` may be NULL",
			want: "substitution_value may be NULL",
		},
		{
			name: "blockquote marker trimmed",
			in:   "> quoted finding",
			want: "quoted finding",
		},
		{
			name: "empty body yields empty string",
			in:   "",
			want: "",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := PlainTitle(c.in); got != c.want {
				t.Errorf("PlainTitle() = %q, want %q", got, c.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/preview/ -run TestPlainTitle -v`
Expected: FAIL to build — `undefined: PlainTitle`

- [ ] **Step 3: Write the implementation**

Append to `internal/preview/plaintext.go`:

```go
// PlainTitle distills a comment body to a single line of plain text, for
// summary rows that have one line to spend. It returns the first line that is
// non-empty *after* distillation — a bot comment whose first line is only a
// severity badge distills to "" and must fall through rather than yielding a
// blank row. Returns "" when no line survives.
func PlainTitle(body string) string {
	for _, ln := range strings.Split(body, "\n") {
		s := mdImage.ReplaceAllString(ln, "")
		s = mdLink.ReplaceAllString(s, "$1")
		s = htmlTag.ReplaceAllString(s, "")
		s = strings.TrimLeft(s, "#> \t")
		s = mdEmph.ReplaceAllString(s, "")
		s = strings.TrimSpace(wsRun.ReplaceAllString(s, " "))
		if s != "" {
			return s
		}
	}
	return ""
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/preview/ -run TestPlainTitle -v`
Expected: PASS, all six subtests.

- [ ] **Step 5: Commit**

```bash
git add internal/preview/plaintext.go internal/preview/plaintext_test.go
git commit -m "feat(preview): add PlainTitle for one-line comment summaries (#55)"
```

---

## Task 4: Swap the three `firstLine` call sites, delete `firstLine`

`firstLine` truncates a body to its first line of *raw markdown*, which is why the Overview shows `**<sub><sub>![P2 Badge](…)</sub></sub>  Guard JSON casts with CASE**`.

**Files:**
- Modify: `internal/ui/threads_render.go:14-19` (delete `firstLine`), `:35`, `:84`, `:87`
- Create: `internal/ui/threads_render_test.go`

**Interfaces:**
- Consumes: `preview.PlainTitle` from Task 3
- Produces: nothing new — `renderThreadsSummary` and `renderFileThreads` keep their signatures

- [ ] **Step 1: Write the failing test**

Create `internal/ui/threads_render_test.go`:

```go
package ui

import (
	"strings"
	"testing"

	"github.com/noamsto/prdash/internal/gh"
	"github.com/noamsto/prdash/internal/preview"
)

// badgeBody is the real shape chatgpt-codex-connector posts: a severity badge
// image wrapped in <sub> inside bold, then the finding title.
const badgeBody = "**<sub><sub>![P2 Badge](https://img.shields.io/badge/P2-yellow?style=flat)</sub></sub>  Guard JSON casts with CASE**\n\nmore detail here"

func TestRenderThreadsSummaryDistillsMarkdown(t *testing.T) {
	ts := []gh.ReviewThread{{
		Path:     "db/20260723075117_add_rubric_selection_shaped_fn.sql",
		Line:     20,
		Comments: []gh.ThreadComment{{Author: "chatgpt-codex-connector", Body: badgeBody}},
	}}
	out := renderThreadsSummary(ts, 3, 100)
	for _, bad := range []string{"**", "<sub>", "![", "img.shields.io"} {
		if strings.Contains(out, bad) {
			t.Errorf("%q leaked into the summary: %q", bad, out)
		}
	}
	if !strings.Contains(out, "Guard JSON casts with CASE") {
		t.Errorf("finding title missing: %q", out)
	}
}

func TestRenderFileThreadsDistillsMarkdown(t *testing.T) {
	g := preview.FileThreads{
		Path: "db/x.sql",
		Threads: []gh.ReviewThread{{
			Path:     "db/x.sql",
			Line:     20,
			Comments: []gh.ThreadComment{{Author: "chatgpt-codex-connector", Body: badgeBody}},
		}},
	}
	out := renderFileThreads(g, 100, false)
	for _, bad := range []string{"**", "<sub>", "![", "img.shields.io"} {
		if strings.Contains(out, bad) {
			t.Errorf("%q leaked into the file threads: %q", bad, out)
		}
	}
	if !strings.Contains(out, "Guard JSON casts with CASE") {
		t.Errorf("finding title missing: %q", out)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/ -run 'TestRenderThreadsSummaryDistills|TestRenderFileThreadsDistills' -v`
Expected: FAIL — both tests report `"**" leaked into…` and `"![" leaked into…`

- [ ] **Step 3: Replace the three call sites**

In `internal/ui/threads_render.go`, line 35:

```go
			body = preview.PlainTitle(t.Comments[0].Body)
```

Line 84:

```go
		b.WriteString("      " + dimStyle.Render(truncate(preview.PlainTitle(head.Body), w-6)) + "\n")
```

Line 87:

```go
			b.WriteString("        " + dimStyle.Render(truncate(preview.PlainTitle(reply.Body), w-8)) + "\n")
```

- [ ] **Step 4: Delete `firstLine`**

Remove lines 14–19 of `internal/ui/threads_render.go` entirely:

```go
func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
```

Keep the `strings` import — `strings.Builder`, `strings.Join`, and `strings.TrimRight` are still used in this file.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/ui/ -v -run 'Thread'`
Expected: PASS. Then `go build ./... && go vet ./... && go test ./...` — all green, no `firstLine` references remain.

- [ ] **Step 6: Verify no stragglers**

Run: `grep -rn "firstLine" internal/ --include="*.go"`
Expected: no output.

- [ ] **Step 7: Commit**

```bash
git add internal/ui/threads_render.go internal/ui/threads_render_test.go
git commit -m "fix(ui): distill review comment markdown in thread summaries (#55)"
```

- [ ] **Step 8: Open PR A**

```bash
git push -u origin feat/55-review-comment-rendering
gh pr create --assignee @me --title "fix: render review comment markdown, drop badge and URL noise (#55)" --body "Part 1 of #55.

Review comment bodies were written straight to \`dimStyle\` after \`firstLine\`, so the Overview THREADS block and the Diff tab showed raw markdown — a bot severity badge rendered as \`**<sub><sub>![P2 Badge](https://img.shields.io/...)</sub></sub>  Guard JSON casts with CASE**\`.

### What changed
- \`preview/theme.go\` — wrap the stock glamour configs to drop literal heading markers (\`### \`), bolding instead.
- \`preview/plaintext.go\` — new \`prePass\` runs inside \`Render\`: strips inline images (bot badges) and collapses links to their label. Glamour still emits the OSC 8 hyperlink, so links stay clickable — only the printed URL goes away. Fenced code blocks pass through untouched so \`arr[0](ctx)\` isn't mistaken for a link.
- \`preview.PlainTitle\` — distills a body to one plain line for summary rows, falling through badge-only first lines.
- \`ui/threads_render.go\` — all three \`firstLine\` call sites now use \`PlainTitle\`; \`firstLine\` deleted.

The pre-pass lives in \`Render\`, not the threads renderer, because **the Conversation tab has the same badge/URL noise today** — this fixes both surfaces.

### Why no HTML sanitizer
Spiking glamour over the real bodies on \`factify-inc/mono#2936\` showed it already strips \`<!-- -->\` marker comments, \`<details>\` blocks, and Cursor BugBot's ~600-char base64 \"Fix in Cursor\" button. The only gaps were heading markers (style-configurable) and inline image/link URLs (not — \`SkipHref\`/\`TextOnly\` are set only inside tables). See the design doc for the file-level evidence.

### Verification
\`go build\` / \`go vet\` / \`go test ./...\` green. New table tests cover \`prePass\` (incl. the code-fence case), \`PlainTitle\` (incl. badge-only fall-through and snake_case preservation), and both render functions asserting no markdown leaks.

Diff context (\`diffHunk\`) follows in part 2."
```

---

# PR B — diff context

Open only after PR A merges, then rebase onto `main`.

## Task 5: Select and parse `diffHunk`

The threads query already selects `comments(first:100)`, so adding `diffHunk` costs no extra request.

**Files:**
- Modify: `internal/gh/threads.go:8-12` (`ThreadComment`), `:21` (query), `:35-41` + `:63` (parse)
- Modify: `internal/gh/testdata/reviewthreads.json`
- Modify: `internal/gh/threads_test.go`
- Modify: `internal/ui/preview.go:42` (`threadsSchemaVer`)

**Interfaces:**
- Consumes: nothing
- Produces: `gh.ThreadComment.DiffHunk string` — Task 6 reads it

- [ ] **Step 1: Add `diffHunk` to the fixture**

In `internal/gh/testdata/reviewthreads.json`, add a `diffHunk` key to the **first** comment of the **first** thread, so it sits alongside `body`:

```json
                  {
                    "author": { "login": "alice" },
                    "body": "Nit: could this be a named constant?",
                    "diffHunk": "@@ -39,6 +39,9 @@ func ParseReviewThreads(b []byte) {\n \tnodes := env.Data.Repository\n-\tout := make([]ReviewThread, 0)\n+\tout := make([]ReviewThread, 0, len(nodes))",
                    "createdAt": "2026-07-10T14:03:00Z"
                  },
```

Leave every other comment without a `diffHunk` — that exercises the empty-string path Task 6 must handle.

- [ ] **Step 2: Write the failing test**

Append to `internal/gh/threads_test.go`:

```go
func TestParseReviewThreadsDiffHunk(t *testing.T) {
	b, err := os.ReadFile("testdata/reviewthreads.json")
	if err != nil {
		t.Fatal(err)
	}
	ts, err := ParseReviewThreads(b)
	if err != nil {
		t.Fatal(err)
	}
	got := ts[0].Comments[0].DiffHunk
	if !strings.HasPrefix(got, "@@ -39,6 +39,9 @@") {
		t.Errorf("DiffHunk = %q, want the fixture's hunk", got)
	}
	if !strings.Contains(got, "\n+\tout := make([]ReviewThread, 0, len(nodes))") {
		t.Errorf("DiffHunk lost its added line: %q", got)
	}
	if h := ts[0].Comments[1].DiffHunk; h != "" {
		t.Errorf("comment without diffHunk should parse to empty, got %q", h)
	}
}
```

Add `"strings"` to that file's import block.

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/gh/ -run TestParseReviewThreadsDiffHunk -v`
Expected: FAIL to build — `c.DiffHunk undefined (type ThreadComment has no field or method DiffHunk)`

- [ ] **Step 4: Add the field, the selection, and the parse**

In `internal/gh/threads.go`, add `DiffHunk` to `ThreadComment`:

```go
type ThreadComment struct {
	Author    string
	Body      string
	DiffHunk  string
	CreatedAt time.Time
}
```

Add `diffHunk` to the comments selection in `reviewThreadsQuery` (line 21) — it goes inside `comments(first:100){nodes{…}}`:

```go
const reviewThreadsQuery = `query($owner:String!,$repo:String!,$num:Int!){repository(owner:$owner,name:$repo){pullRequest(number:$num){reviewThreads(first:100){nodes{isResolved path line originalLine comments(first:100){nodes{author{login} body diffHunk createdAt}}}}}}}`
```

Add the JSON field to the anonymous comment struct (after `Body`):

```go
									Body      string    `json:"body"`
									DiffHunk  string    `json:"diffHunk"`
									CreatedAt time.Time `json:"createdAt"`
```

And populate it in the mapping loop (line 63):

```go
			cs = append(cs, ThreadComment{Author: c.Author.Login, Body: c.Body, DiffHunk: c.DiffHunk, CreatedAt: c.CreatedAt})
```

- [ ] **Step 5: Bump the threads cache schema version**

In `internal/ui/preview.go`, line 42:

```go
const threadsSchemaVer = "v2"
```

Without this, a `v1` cache entry written before this change stays fresh and paints threads with no hunk. The bump makes those a clean miss.

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./internal/gh/ -v -run Thread`
Expected: PASS, both `TestParseReviewThreads` and `TestParseReviewThreadsDiffHunk`.

Then: `go build ./... && go vet ./... && go test ./...`
Expected: all green.

- [ ] **Step 7: Commit**

```bash
git add internal/gh/threads.go internal/gh/threads_test.go internal/gh/testdata/reviewthreads.json internal/ui/preview.go
git commit -m "feat(gh): select per-comment diffHunk on review threads (#55)"
```

---

## Task 6: Paint the hunk and the full body

**Files:**
- Modify: `internal/ui/threads_render.go` (`renderFileThreads`, plus a new `renderDiffHunk` and `indentBlock`)
- Modify: `internal/ui/threads_render_test.go` (append)

**Interfaces:**
- Consumes: `gh.ThreadComment.DiffHunk` from Task 5; `preview.Render`
- Produces: nothing outside the file

- [ ] **Step 1: Write the failing test**

Append to `internal/ui/threads_render_test.go`:

```go
const twoParaBody = "First paragraph explaining the finding in enough words to wrap.\n\nSecond paragraph with the suggested fix."

func TestRenderFileThreadsShowsHunkAndFullBody(t *testing.T) {
	g := preview.FileThreads{
		Path: "internal/gh/threads.go",
		Threads: []gh.ReviewThread{{
			Path: "internal/gh/threads.go",
			Line: 42,
			Comments: []gh.ThreadComment{{
				Author:   "alice",
				Body:     twoParaBody,
				DiffHunk: "@@ -39,6 +39,9 @@ func f() {\n \tnodes := env.Data\n-\tout := make([]T, 0)\n+\tout := make([]T, 0, len(nodes))",
			}},
		}},
	}
	out := renderFileThreads(g, 100, false)

	if !strings.Contains(out, "out := make([]T, 0, len(nodes))") {
		t.Errorf("hunk's added line missing: %q", out)
	}
	if strings.Contains(out, "@@ -39,6 +39,9 @@") {
		t.Errorf("hunk header should be dropped (the L42 label already locates it): %q", out)
	}
	if !strings.Contains(out, "Second paragraph with the suggested fix.") {
		t.Errorf("body truncated to its first paragraph: %q", out)
	}
}

func TestRenderFileThreadsWithoutHunk(t *testing.T) {
	g := preview.FileThreads{
		Path: "internal/gh/threads.go",
		Threads: []gh.ReviewThread{{
			Path:     "internal/gh/threads.go",
			Line:     42,
			Comments: []gh.ThreadComment{{Author: "alice", Body: "no hunk here"}},
		}},
	}
	out := renderFileThreads(g, 100, false)
	if strings.Contains(out, "│") {
		t.Errorf("empty DiffHunk must not draw a gutter: %q", out)
	}
	if !strings.Contains(out, "no hunk here") {
		t.Errorf("body missing: %q", out)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/ -run 'TestRenderFileThreadsShowsHunk|TestRenderFileThreadsWithoutHunk' -v`
Expected: FAIL — `hunk's added line missing` and `body truncated to its first paragraph` (the current code renders `PlainTitle`, one line, and never reads `DiffHunk`).

- [ ] **Step 3: Add the two helpers**

Append to `internal/ui/threads_render.go`:

```go
// hunkTailLines bounds how much of a diffHunk we paint. GitHub returns the full
// leading context, but only the lines nearest the comment locate it, and an
// unbounded hunk would push the body it belongs to off screen.
const hunkTailLines = 6

// renderDiffHunk paints a comment's diffHunk as a gutter-prefixed block. The @@
// header is dropped — the L<line> label above already says where we are — and
// only the last hunkTailLines lines are kept. Returns "" for an empty hunk so
// callers draw no gutter at all.
func renderDiffHunk(hunk string, w int) string {
	if hunk == "" {
		return ""
	}
	lines := strings.Split(strings.TrimRight(hunk, "\n"), "\n")
	if len(lines) > 0 && strings.HasPrefix(lines[0], "@@") {
		lines = lines[1:]
	}
	if len(lines) > hunkTailLines {
		lines = lines[len(lines)-hunkTailLines:]
	}
	var b strings.Builder
	for _, ln := range lines {
		st := dimStyle
		switch {
		case strings.HasPrefix(ln, "+"):
			st = passStyle
		case strings.HasPrefix(ln, "-"):
			st = failStyle
		}
		b.WriteString("      " + sepStyle.Render("│ ") + st.Render(truncate(ln, w-8)) + "\n")
	}
	return b.String()
}

// indentBlock prefixes every non-blank line with pad. Prefixing is safe on
// already-styled text (unlike slicing, which can cut an ANSI escape), so this
// nests glamour output under its thread without re-wrapping it.
func indentBlock(s, pad string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i, ln := range lines {
		if strings.TrimSpace(ln) != "" {
			lines[i] = pad + ln
		}
	}
	return strings.Join(lines, "\n")
}

// renderCommentBody renders a comment body as nested markdown, falling back to
// the distilled one-liner if glamour fails — the same precedent as
// renderDiscussionItem, which falls back to raw markdown rather than nothing.
func renderCommentBody(body string, w int, pad string) string {
	out, err := preview.Render(body, w-lipgloss.Width(pad))
	if err != nil {
		return pad + dimStyle.Render(truncate(preview.PlainTitle(body), w-lipgloss.Width(pad))) + "\n"
	}
	return indentBlock(out, pad) + "\n"
}
```

- [ ] **Step 4: Rewrite the body lines in `renderFileThreads`**

Replace line 84 (the head-comment body):

```go
		b.WriteString(renderDiffHunk(head.DiffHunk, w))
		b.WriteString(renderCommentBody(head.Body, w, "      "))
```

Replace the reply body inside the loop (line 87), keeping the existing author line above it:

```go
			b.WriteString(renderCommentBody(reply.Body, w, "        "))
```

`renderFileThreads` keeps its signature. `preview.PlainTitle` is still used by `renderThreadsSummary` and by `renderCommentBody`'s fallback.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/ui/ -v -run Thread`
Expected: PASS — all four thread render tests. `TestRenderThreadsSummaryDistillsMarkdown` still passes (Overview is untouched); `TestRenderFileThreadsDistillsMarkdown` still passes because glamour output contains neither `**` nor `![`.

Then: `go build ./... && go vet ./... && go test ./... && golangci-lint run ./...`
Expected: green; lint reports only the pre-existing findings listed in Global Constraints.

- [ ] **Step 6: Commit**

```bash
git add internal/ui/threads_render.go internal/ui/threads_render_test.go
git commit -m "feat(ui): show diff context and full bodies on review threads (#55)"
```

- [ ] **Step 7: Live-verify in the TUI**

Build and run fish-native — never via `env`, which breaks bubbletea's terminal setup in this tmux/kitty stack:

```fish
go build -o ~/prdash-test .
cd ~/Data/git/factify-inc/mono
~/prdash-test
```

Open a PR with inline review comments (e.g. `#2936`), go to the **Diff** tab, and confirm: the hunk appears above each comment with `+`/`-` lines colored, the body reads as prose across multiple paragraphs, and no `@@` header or raw markdown is visible. Check a thread whose comment has no hunk draws no gutter.

- [ ] **Step 8: Open PR B**

```bash
git push -u origin feat/55-review-comment-rendering
gh pr create --assignee @me --title "feat: show diff context on review threads (#55)" --body "Part 2 of #55, on top of the rendering fixes in part 1. Closes #55.

The Diff tab renders a *diffstat*, so review threads hung under bare filenames with no code to anchor to.

### What changed
- \`gh/threads.go\` — select \`diffHunk\` per comment. The query already selects \`comments(first:100)\`, so this adds **no extra request**.
- \`threadsSchemaVer\` v1→v2, so cached hunk-less responses are a clean miss rather than threads that silently render without code.
- \`renderDiffHunk\` — gutter-prefixed block, \`+\`/\`-\` lines colored, \`@@\` header dropped (the \`L<line>\` label already locates it), tail-bounded to 6 lines so a long hunk can't push the body off screen.
- Comment bodies now render in full through \`preview.Render\` instead of being distilled to one line. The Diff tab is a scrollable viewport, so a long bot finding makes the pane longer rather than hiding what you opened it to read.

Bot footers (\`Useful? React with 👍 / 👎\`, \`Reviewed by Cursor Bugbot…\`) are deliberately kept — stripping them means pattern-matching each vendor's boilerplate, which drifts silently.

### Verification
\`go build\` / \`go vet\` / \`go test ./...\` green. Tests cover the hunk block (added line present, \`@@\` header dropped), the empty-\`DiffHunk\` path drawing no gutter, and a two-paragraph body no longer truncating. Live-verified in the TUI against a PR with inline comments."
```

---

## Self-Review

**Spec coverage** — every spec section maps to a task:

| Spec section | Task |
|---|---|
| Findings: heading prefixes | 1 |
| Findings: inline image/link URLs not configurable | 2 |
| Architecture: `prePass` inside `Render` | 2 (Step 5) |
| Architecture: `PlainTitle` separate function | 3 |
| Components: `theme.go` heading prefixes | 1 |
| Components: `prePass` | 2 |
| Components: `PlainTitle` incl. fall-through rule | 3 |
| Components: `diffHunk` + `threadsSchemaVer` v1→v2 | 5 |
| Components: layout (hunk block, full body, replies) | 6 |
| Error handling: `Render` failure falls back | 6 (`renderCommentBody`) |
| Error handling: `PlainTitle` `""` floor | 3 (empty-body case) |
| Error handling: absent `DiffHunk` draws no box | 6 (`TestRenderFileThreadsWithoutHunk`) |
| Testing: all listed assertions | 1–6 |
| Staging: PR A = 1–4, PR B = 5–6 | Global Constraints |

**Two additions beyond the spec**, both defensible and noted here so review can reject them:

1. **Code-fence awareness in `prePass`** (Task 2). The spec said "regex-based, markdown link/image syntax does not nest in these bodies." It missed that `arr[0](ctx)` inside a code fence matches `mdLink` and would be silently corrupted. Review comments frequently contain code. Covered by a test case.
2. **`mdEmph` omits `_`** (Task 2). The spec's distillation list said "strip emphasis and backtick runs," which would turn `substitution_value` into `substitutionvalue`. Snake_case in identifiers is far more common in these titles than underscore emphasis. Covered by a test case.

**Placeholder scan:** none — every code step contains complete code, every run step an exact command and expected result.

**Type consistency:** `PlainTitle(string) string` defined Task 3, called Tasks 4 and 6. `prePass(string) string` defined Task 2, called in `Render`. `ThreadComment.DiffHunk string` defined Task 5, read Task 6. `renderDiffHunk(string, int) string`, `indentBlock(string, string) string`, `renderCommentBody(string, int, string) string` all defined and called within Task 6. `dimStyle`/`passStyle`/`failStyle`/`sepStyle` are all `lipgloss.Style` (`internal/ui/theme.go:110-113`), so the `st := dimStyle` reassignment in `renderDiffHunk` type-checks.
