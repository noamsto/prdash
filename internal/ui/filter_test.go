package ui

import (
	"testing"

	"github.com/noamsto/prdash/internal/gh"
)

func sample() []gh.PR {
	a := gh.PR{Number: 7, Title: "add cache", HeadRefName: "feat/cache"}
	a.Author.Login = "noam"
	b := gh.PR{Number: 12, Title: "fix render", HeadRefName: "fix/render"}
	b.Author.Login = "dlvhdr"
	b.Assignees = []struct {
		Login string `json:"login"`
	}{{Login: "noam"}}
	return []gh.PR{a, b}
}

func TestFilterEmptyReturnsAll(t *testing.T) {
	if got := filterPRs(sample(), ""); len(got) != 2 {
		t.Fatalf("empty query = %d rows, want 2", len(got))
	}
}

func TestFilterByNumber(t *testing.T) {
	got := filterPRs(sample(), "12")
	if len(got) == 0 || got[0].Number != 12 {
		t.Fatalf("query '12' = %+v", got)
	}
}

func TestFilterByAssignee(t *testing.T) {
	if got := filterPRs(sample(), "noam"); len(got) != 2 {
		t.Fatalf("query 'noam' = %d, want 2", len(got))
	}
}

func TestFilterByBranch(t *testing.T) {
	got := filterPRs(sample(), "render")
	if len(got) != 1 || got[0].Number != 12 {
		t.Fatalf("query 'render' = %+v", got)
	}
}

func TestFilterPRsMatchesBody(t *testing.T) {
	prs := []gh.PR{
		{Number: 1, Title: "fix header", Body: "resolves a flaky race in the poller"},
		{Number: 2, Title: "add cache"},
	}
	got := filterPRs(prs, "flaky")
	if len(got) != 1 || got[0].Number != 1 {
		t.Fatalf("body match failed: got %+v", got)
	}
}
