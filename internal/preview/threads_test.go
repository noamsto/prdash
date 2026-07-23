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
