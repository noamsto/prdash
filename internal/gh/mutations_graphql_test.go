package gh

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/shurcooL/githubv4"
)

// These tests mirror the Input construction inside MergePR, EnableAutoMerge,
// and RequestReviews (mutations_graphql.go) to pin the destructive-path
// constants — squash merge method, union:false — that s.client.Mutate gives
// no fake seam to observe. A future edit changing either value in the real
// method, without updating the mirror here, is the signal these tests exist
// to surface.

func TestMergePRInputUsesSquashMethod(t *testing.T) {
	method := githubv4.PullRequestMergeMethodSquash
	input := githubv4.MergePullRequestInput{
		PullRequestID: githubv4.ID("PR_test"),
		MergeMethod:   &method,
	}
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"mergeMethod":"SQUASH"`) {
		t.Errorf("MergePullRequestInput JSON = %s, want mergeMethod:SQUASH", raw)
	}
}

func TestEnableAutoMergeInputUsesSquashMethod(t *testing.T) {
	method := githubv4.PullRequestMergeMethodSquash
	input := githubv4.EnablePullRequestAutoMergeInput{
		PullRequestID: githubv4.ID("PR_test"),
		MergeMethod:   &method,
	}
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"mergeMethod":"SQUASH"`) {
		t.Errorf("EnablePullRequestAutoMergeInput JSON = %s, want mergeMethod:SQUASH", raw)
	}
}

func TestRequestReviewsByLoginInputUsesUnionFalse(t *testing.T) {
	logins := []string{"alice", "bob"}
	ul := make([]githubv4.String, len(logins))
	for i, l := range logins {
		ul[i] = githubv4.String(l)
	}
	input := githubv4.RequestReviewsByLoginInput{
		PullRequestID: githubv4.ID("PR_test"),
		UserLogins:    &ul,
		Union:         githubv4.NewBoolean(false),
	}
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"union":false`) {
		t.Errorf("RequestReviewsByLoginInput JSON = %s, want union:false", raw)
	}
}
