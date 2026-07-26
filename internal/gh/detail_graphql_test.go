package gh

import (
	"strings"
	"testing"
)

func TestBuildDetailQuery(t *testing.T) {
	q := buildDetailQuery([]int{12, 15})
	for _, want := range []string{
		"pr12:pullRequest(number:12){",
		"pr15:pullRequest(number:15){",
		"mergeStateStatus",
		"reviewRequests(first:100)",
		"files(first:100)",
	} {
		if !strings.Contains(q, want) {
			t.Errorf("query missing %q\nquery: %s", want, q)
		}
	}
}

func TestParseDetails(t *testing.T) {
	body := []byte(`{"data":{"repository":{
		"pr7":{
			"comments":{"nodes":[{"author":{"login":"dependabot","__typename":"Bot"},"body":"bump","createdAt":"2024-01-01T00:00:00Z"}]},
			"reviews":{"nodes":[{"author":{"login":"alice","__typename":"User"},"body":"lgtm","state":"APPROVED","submittedAt":"2024-01-02T00:00:00Z"}]},
			"latestReviews":{"nodes":[{"author":{"login":"alice","__typename":"User"},"body":"lgtm","state":"APPROVED","submittedAt":"2024-01-02T00:00:00Z"}]},
			"mergeStateStatus":"BLOCKED","mergeable":"MERGEABLE","isDraft":false,
			"reviewRequests":{"nodes":[{"requestedReviewer":{"__typename":"User","login":"bob"}},{"requestedReviewer":null},{"requestedReviewer":{"__typename":"Team","name":"platform"}}]},
			"files":{"nodes":[{"path":"a.go","additions":3,"deletions":1}]}
		}
	}}}`)

	details, raws, err := parseDetails(body, []int{7, 99})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := details[99]; ok {
		t.Error("absent alias pr99 should be skipped, not fabricated")
	}
	d, ok := details[7]
	if !ok {
		t.Fatal("pr7 missing from parsed details")
	}
	if d.MergeStateStatus != "BLOCKED" || d.Mergeable != "MERGEABLE" || d.IsDraft {
		t.Errorf("scalar mismatch: %+v", d)
	}
	if len(d.Comments) != 1 || d.Comments[0].Author.Login != "app/dependabot" {
		t.Errorf("comment bot author = %+v, want app/dependabot", d.Comments)
	}
	if len(d.Reviews) != 1 || d.Reviews[0].State != "APPROVED" {
		t.Errorf("review mismatch: %+v", d.Reviews)
	}
	if len(d.ReviewRequests) != 2 || d.ReviewRequests[0].Login != "bob" || d.ReviewRequests[1].Login != "platform" {
		t.Errorf("reviewRequests = %+v, want [bob platform(team)]", d.ReviewRequests)
	}
	if ds := d.Diffstat(); ds.Files != 1 || ds.Additions != 3 || ds.Deletions != 1 {
		t.Errorf("diffstat = %+v, want {1 3 1}", ds)
	}
	if raws[7] == nil {
		t.Error("cache bytes for pr7 should be populated")
	}
}

func TestParseDetailsSurfacesGraphQLErrors(t *testing.T) {
	body := []byte(`{"errors":[{"message":"Field 'mergeStateStatus' needs a preview header"}]}`)
	if _, _, err := parseDetails(body, []int{7}); err == nil {
		t.Fatal("GraphQL errors array must surface as an error")
	}
}
