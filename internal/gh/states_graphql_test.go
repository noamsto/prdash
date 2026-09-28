package gh

import (
	"strings"
	"testing"
)

func TestBuildStatesQueryAliasesEachNumber(t *testing.T) {
	q := buildStatesQuery([]int{61, 62})
	for _, want := range []string{
		"n61:issueOrPullRequest(number:61)",
		"n62:issueOrPullRequest(number:62)",
		"... on PullRequest{state mergedAt closedAt}",
		"... on Issue{state closedAt}",
	} {
		if !strings.Contains(q, want) {
			t.Errorf("query missing %q:\n%s", want, q)
		}
	}
}

func TestParseStates(t *testing.T) {
	t.Run("maps PR and issue states, drops null aliases", func(t *testing.T) {
		body := `{"data":{"repository":{
			"n61":{"__typename":"PullRequest","state":"MERGED","mergedAt":"2026-07-30T09:00:00Z","closedAt":"2026-07-30T09:00:00Z"},
			"n62":{"__typename":"Issue","state":"CLOSED","closedAt":"2026-07-29T00:00:00Z"},
			"n63":null
		}}}`
		got, err := parseStates([]byte(body), []int{61, 62, 63})
		if err != nil {
			t.Fatalf("parseStates: %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("want 2 entries, got %d: %+v", len(got), got)
		}
		pr, ok := got[61]
		if !ok {
			t.Fatal("61 missing from result")
		}
		if pr.State != "MERGED" || pr.MergedAt.IsZero() || pr.ClosedAt.IsZero() {
			t.Errorf("pr61 mapped wrong: %+v", pr)
		}
		issue, ok := got[62]
		if !ok {
			t.Fatal("62 missing from result")
		}
		if issue.State != "CLOSED" || !issue.MergedAt.IsZero() || issue.ClosedAt.IsZero() {
			t.Errorf("issue62 mapped wrong: %+v", issue)
		}
		if _, ok := got[63]; ok {
			t.Error("63 (null alias) must be absent from the result")
		}
	})

	t.Run("partial errors alongside data still resolve the other aliases", func(t *testing.T) {
		body := `{"data":{"repository":{
			"n61":{"__typename":"PullRequest","state":"MERGED","mergedAt":"2026-07-30T09:00:00Z"},
			"n62":{"__typename":"Issue","state":"CLOSED","closedAt":"2026-07-29T00:00:00Z"},
			"n63":null
		}},"errors":[{"message":"Could not resolve to an issue or pull request with the number of 63."}]}`
		got, err := parseStates([]byte(body), []int{61, 62, 63})
		if err != nil {
			t.Fatalf("parseStates: %v", err)
		}
		if _, ok := got[61]; !ok {
			t.Error("61 must still resolve despite the 63 NOT_FOUND error")
		}
		if _, ok := got[62]; !ok {
			t.Error("62 must still resolve despite the 63 NOT_FOUND error")
		}
	})

	t.Run("errors with no repository is an error", func(t *testing.T) {
		body := `{"data":{"repository":null},"errors":[{"message":"boom"}]}`
		_, err := parseStates([]byte(body), []int{1})
		if err == nil || !strings.Contains(err.Error(), "boom") {
			t.Fatalf("want the GraphQL error surfaced, got %v", err)
		}
	})
}
