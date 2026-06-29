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
