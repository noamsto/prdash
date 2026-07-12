package gh

import (
	"errors"
	"testing"
)

func TestIssuesDisabled(t *testing.T) {
	if !IssuesDisabled(errors.New("the 'factify-inc/mono' repository has disabled issues")) {
		t.Fatal("expected disabled-issues error to be detected")
	}
	if IssuesDisabled(errors.New("gh: authentication required")) {
		t.Fatal("unrelated error should not match")
	}
	if IssuesDisabled(nil) {
		t.Fatal("nil error should not match")
	}
}

func TestIssueListArgs(t *testing.T) {
	args := IssueListArgs("assignee:@me", 20)
	if args[0] != "issue" || args[1] != "list" || args[2] != "--search" {
		t.Fatalf("args = %v", args)
	}
}

func TestParseIssues(t *testing.T) {
	is, err := ParseIssues([]byte(`[{"number":4,"title":"bug","labels":[{"name":"fix"}]}]`))
	if err != nil || len(is) != 1 || is[0].Number != 4 || is[0].Labels[0].Name != "fix" {
		t.Fatalf("parsed=%+v err=%v", is, err)
	}
}
