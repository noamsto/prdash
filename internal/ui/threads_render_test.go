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
