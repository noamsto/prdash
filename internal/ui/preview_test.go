package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/noamsto/prdash/internal/gh"
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

func TestCILineFailingIsVerticalList(t *testing.T) {
	pr := gh.PR{StatusCheckRollup: []gh.Check{
		{State: "FAILURE", Name: "lint"},
		{State: "FAILURE", Name: "test"},
		{State: "SUCCESS", Name: "build"},
	}}
	out := ciLine(pr)
	if !strings.Contains(out, "2 checks failing") {
		t.Errorf("want failing count header: %q", out)
	}
	if !strings.Contains(out, "lint") || !strings.Contains(out, "test") {
		t.Errorf("want each failing check name: %q", out)
	}
	if strings.Count(out, "\n") < 2 { // header + 2 names = ≥2 newlines
		t.Errorf("want a vertical list, got: %q", out)
	}
}

func TestReviewersLine(t *testing.T) {
	got := reviewersLine(nil)
	if !strings.Contains(got, "no reviewers") {
		t.Fatalf("empty reviewers should warn: %q", got)
	}
	got = reviewersLine([]gh.ReviewRequest{{Login: "alice"}, {Login: "bob"}})
	if !strings.Contains(got, "alice") || !strings.Contains(got, "bob") {
		t.Fatalf("should list reviewers: %q", got)
	}
}
