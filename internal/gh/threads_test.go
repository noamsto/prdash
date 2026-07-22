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

func TestReviewThreadsArgsShape(t *testing.T) {
	args := ReviewThreadsArgs("noamsto", "prdash", 49)
	if args[0] != "api" || args[1] != "graphql" {
		t.Fatalf("args should start with 'api graphql', got %v", args[:2])
	}
}
