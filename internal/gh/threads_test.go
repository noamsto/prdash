package gh

import (
	"os"
	"strings"
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
	var sawResolved, sawUnresolved bool
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
	}
	if !sawResolved || !sawUnresolved {
		t.Fatalf("want both resolved and unresolved threads; resolved=%v unresolved=%v", sawResolved, sawUnresolved)
	}
}

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
