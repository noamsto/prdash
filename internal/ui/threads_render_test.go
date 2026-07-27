package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

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
	out := ansi.Strip(renderFileThreads(g, 100, false))

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
	if !strings.Contains(ansi.Strip(out), "no hunk here") {
		t.Errorf("body missing: %q", out)
	}
}

