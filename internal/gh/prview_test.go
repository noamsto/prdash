package gh

import (
	"encoding/json"
	"testing"
)

// PR detail is cached as marshalled PRDetail and rehydrated with json.Unmarshal,
// so these round-trip tests guard that struct's JSON-tag contract.
func TestPRDetailJSONRoundTrips(t *testing.T) {
	var d PRDetail
	if err := json.Unmarshal([]byte(`{
		"comments":[{"author":{"login":"a"},"body":"hi","createdAt":"2026-06-01T10:00:00Z"}],
		"reviews":[{"author":{"login":"b"},"body":"LGTM","state":"APPROVED","submittedAt":"2026-06-02T10:00:00Z"}]
	}`), &d); err != nil {
		t.Fatal(err)
	}
	if len(d.Comments) != 1 || d.Comments[0].Author.Login != "a" {
		t.Fatalf("comments=%+v", d.Comments)
	}
	if len(d.Reviews) != 1 || d.Reviews[0].State != "APPROVED" {
		t.Fatalf("reviews=%+v", d.Reviews)
	}
}

func TestPRDetailMergeState(t *testing.T) {
	var d PRDetail
	if err := json.Unmarshal([]byte(`{
		"mergeStateStatus":"BLOCKED","mergeable":"MERGEABLE","isDraft":false,
		"reviewRequests":[{"login":"octocat"}],
		"files":[{"path":"a.go","additions":10,"deletions":2},{"path":"b.go","additions":1,"deletions":1}]
	}`), &d); err != nil {
		t.Fatal(err)
	}
	if d.MergeStateStatus != "BLOCKED" || d.Mergeable != "MERGEABLE" {
		t.Fatalf("merge state not parsed: %+v", d)
	}
	if len(d.ReviewRequests) != 1 || d.ReviewRequests[0].Login != "octocat" {
		t.Fatalf("review requests: %+v", d.ReviewRequests)
	}
	if d.Diffstat().Files != 2 || d.Diffstat().Additions != 11 || d.Diffstat().Deletions != 3 {
		t.Fatalf("diffstat: %+v", d.Diffstat())
	}
}

func TestCheckLabel(t *testing.T) {
	if got := (Check{Name: "test (ubuntu)"}).Label(); got != "test (ubuntu)" {
		t.Errorf("name-first: %q", got)
	}
	if got := (Check{WorkflowName: "CI"}).Label(); got != "CI" {
		t.Errorf("workflow fallback: %q", got)
	}
	if got := (Check{Context: "ci/circleci"}).Label(); got != "ci/circleci" {
		t.Errorf("context fallback: %q", got)
	}
}
