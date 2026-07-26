package gh

import (
	"encoding/json"
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

// The issue-list cache stores marshalled []Issue and hydrate decodes it with
// json.Unmarshal, so this guards that struct's JSON-tag contract.
func TestIssueJSONRoundTrips(t *testing.T) {
	var is []Issue
	if err := json.Unmarshal([]byte(`[{"number":4,"title":"bug","labels":[{"name":"fix"}]}]`), &is); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(is) != 1 || is[0].Number != 4 || is[0].Labels[0].Name != "fix" {
		t.Fatalf("parsed=%+v", is)
	}
}
