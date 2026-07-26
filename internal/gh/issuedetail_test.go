package gh

import (
	"encoding/json"
	"testing"
)

// Issue detail is cached as marshalled IssueDetail and rehydrated with
// json.Unmarshal, so this guards that struct's JSON-tag contract (including the
// json:"-" Timeline that must never decode from the payload).
func TestIssueDetailJSONRoundTrips(t *testing.T) {
	var d IssueDetail
	if err := json.Unmarshal([]byte(`{"body":"## Hello\nworld"}`), &d); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if d.Body != "## Hello\nworld" {
		t.Errorf("body = %q", d.Body)
	}
	if d.Timeline != nil {
		t.Errorf("Timeline should be empty in v1, got %v", d.Timeline)
	}
}
