package ui

import (
	"testing"

	"github.com/noamsto/prdash/internal/gh"
)

// fakeThreadsSource mirrors fakeIssueDetailSource (issuesource_test.go) for the
// review-threads seam.
type fakeThreadsSource struct {
	got     []int
	threads []gh.ReviewThread
	raw     []byte
}

func (f *fakeThreadsSource) FetchReviewThreads(number int) ([]gh.ReviewThread, []byte, error) {
	f.got = append(f.got, number)
	return f.threads, f.raw, nil
}

func TestFetchThreadsCmdUsesNativeSource(t *testing.T) {
	fts := &fakeThreadsSource{
		threads: []gh.ReviewThread{{Path: "main.go", Line: 12, Comments: []gh.ThreadComment{{Author: "alice", Body: "nit"}}}},
		raw:     []byte(`{"data":{}}`),
	}
	m := issueSourceModel(t)
	m.SetThreadsSource(fts)

	cmd := m.fetchThreadsCmd(42)
	if cmd == nil {
		t.Fatal("fetchThreadsCmd should return a command")
	}
	msg := cmd()
	got, ok := msg.(threadsMsg)
	if !ok {
		t.Fatalf("msg = %T, want threadsMsg", msg)
	}
	if got.number != 42 || len(got.threads) != 1 || got.threads[0].Path != "main.go" {
		t.Errorf("threads = %+v, want number=42 with the fake source's thread", got)
	}
	if len(fts.got) != 1 || fts.got[0] != 42 {
		t.Errorf("source called with %v, want one call for 42", fts.got)
	}
}
