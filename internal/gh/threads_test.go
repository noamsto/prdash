package gh

import (
	"os"
	"strings"
	"testing"
)

// The fixture is a real aliased detail response, so these exercise the threads
// mapping exactly where it now runs: inside the batched detail parse.
func threadsFixture(t *testing.T) []ReviewThread {
	t.Helper()
	b, err := os.ReadFile("testdata/prdetail-threads.json")
	if err != nil {
		t.Fatal(err)
	}
	details, _, err := parseDetails(b, []int{1})
	if err != nil {
		t.Fatal(err)
	}
	return details[1].ReviewThreads
}

func TestDetailParsesReviewThreads(t *testing.T) {
	ts := threadsFixture(t)
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

func TestDetailParsesThreadDiffHunk(t *testing.T) {
	ts := threadsFixture(t)
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

// One request, not two: the detail selection must carry the threads fields, or the
// preview goes back to costing a second round trip per row.
func TestDetailQueryIncludesReviewThreads(t *testing.T) {
	q := buildDetailQuery([]int{3})
	for _, want := range []string{"reviewThreads(", "diffHunk", "originalLine"} {
		if !strings.Contains(q, want) {
			t.Errorf("detail query missing %q:\n%s", want, q)
		}
	}
}

// The thread page size is a cost knob, not a taste choice: at first:100 a
// five-PR detail batch measured 5 points against GitHub, at first:20 it measures
// 1. Guard it so a well-meaning bump doesn't quietly 5× the prefetch.
func TestThreadPageSizeStaysWithinTheOnePointWindow(t *testing.T) {
	if !strings.Contains(threadsFields, "reviewThreads(first:20)") {
		t.Errorf("thread page size changed; re-measure the batch cost first:\n%s", threadsFields)
	}
}
