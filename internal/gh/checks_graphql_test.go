package gh

import (
	"strings"
	"testing"
)

func TestBuildChecksQueryAliasesEachNumber(t *testing.T) {
	q := buildChecksQuery([]int{7, 12})
	for _, want := range []string{"pr7:pullRequest(number:7)", "pr12:pullRequest(number:12)"} {
		if !strings.Contains(q, want) {
			t.Errorf("query missing %q:\n%s", want, q)
		}
	}
	// The whole point of this query is what it does NOT ask for: one point covers
	// the batch only while it stays a rollup fetch.
	for _, unwanted := range []string{"labels(", "assignees(", "files(", "comments("} {
		if strings.Contains(q, unwanted) {
			t.Errorf("checks query must not select %q:\n%s", unwanted, q)
		}
	}
}

func TestParseChecksMapsBothContextKinds(t *testing.T) {
	body := `{"data":{"repository":{
		"pr1":{"commits":{"nodes":[{"commit":{"statusCheckRollup":{"contexts":{"nodes":[
			{"__typename":"CheckRun","name":"build","conclusion":"SUCCESS","detailsUrl":"d",
			 "startedAt":"2026-07-30T09:00:00Z","checkSuite":{"workflowRun":{"workflow":{"name":"CI"}}}},
			{"__typename":"StatusContext","context":"legacy","state":"PENDING","targetUrl":"t"}
		]}}}}]}}
	}}}`
	got, err := parseChecks([]byte(body), []int{1})
	if err != nil {
		t.Fatalf("parseChecks: %v", err)
	}
	checks := got[1]
	if len(checks) != 2 {
		t.Fatalf("want 2 checks, got %d: %+v", len(checks), checks)
	}
	if checks[0].Name != "build" || checks[0].Conclusion != "SUCCESS" || checks[0].WorkflowName != "CI" {
		t.Errorf("CheckRun mapped wrong: %+v", checks[0])
	}
	if checks[0].StartedAt != "2026-07-30T09:00:00Z" {
		t.Errorf("StartedAt: got %q", checks[0].StartedAt)
	}
	if checks[1].Context != "legacy" || checks[1].State != "PENDING" {
		t.Errorf("StatusContext mapped wrong: %+v", checks[1])
	}
}

// A PR with no rollup must still land in the map: "checks disappeared" is a state
// the poll has to be able to paint, and a missing key would leave the stale
// pending glyph on the row forever.
func TestParseChecksNullRollupYieldsPresentEmptyEntry(t *testing.T) {
	body := `{"data":{"repository":{"pr4":{"commits":{"nodes":[{"commit":{"statusCheckRollup":null}}]}}}}}`
	got, err := parseChecks([]byte(body), []int{4})
	if err != nil {
		t.Fatalf("parseChecks: %v", err)
	}
	c, ok := got[4]
	if !ok {
		t.Fatal("PR 4 must be present in the result map")
	}
	if len(c) != 0 {
		t.Errorf("want empty rollup, got %+v", c)
	}
}

func TestParseChecksSurfacesGraphQLErrors(t *testing.T) {
	_, err := parseChecks([]byte(`{"errors":[{"message":"boom"}]}`), []int{1})
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("want the GraphQL error surfaced, got %v", err)
	}
}
