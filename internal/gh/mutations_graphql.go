package gh

import (
	"context"

	"github.com/shurcooL/githubv4"
)

// MergePR immediately squash-merges the PR, replacing `gh pr merge --squash`.
// prID is the PR's GraphQL node ID (gh.PR.ID), not its number.
func (s GraphSource) MergePR(prID string) error {
	var m struct {
		MergePullRequest struct {
			PullRequest struct{ ID string }
		} `graphql:"mergePullRequest(input: $input)"`
	}
	method := githubv4.PullRequestMergeMethodSquash
	input := githubv4.MergePullRequestInput{
		PullRequestID: githubv4.ID(prID),
		MergeMethod:   &method,
	}
	return s.client.Mutate(context.Background(), &m, input, nil)
}

// EnableAutoMerge arms squash auto-merge, replacing `gh pr merge --auto --squash`.
func (s GraphSource) EnableAutoMerge(prID string) error {
	var m struct {
		EnablePullRequestAutoMerge struct {
			PullRequest struct{ ID string }
		} `graphql:"enablePullRequestAutoMerge(input: $input)"`
	}
	method := githubv4.PullRequestMergeMethodSquash
	input := githubv4.EnablePullRequestAutoMergeInput{
		PullRequestID: githubv4.ID(prID),
		MergeMethod:   &method,
	}
	return s.client.Mutate(context.Background(), &m, input, nil)
}

// MarkReady marks a draft PR ready for review, replacing `gh pr ready`.
func (s GraphSource) MarkReady(prID string) error {
	var m struct {
		MarkPullRequestReadyForReview struct {
			PullRequest struct{ ID string }
		} `graphql:"markPullRequestReadyForReview(input: $input)"`
	}
	input := githubv4.MarkPullRequestReadyForReviewInput{PullRequestID: githubv4.ID(prID)}
	return s.client.Mutate(context.Background(), &m, input, nil)
}

// UpdateBranch merges the base branch into head, replacing `gh pr
// update-branch`. expectedHeadOid is deliberately omitted (see
// research/ready-updatebranch.md §3) — the operation is non-destructive either
// way, and prdash doesn't fetch the head SHA needed for that race guard.
func (s GraphSource) UpdateBranch(prID string) error {
	var m struct {
		UpdatePullRequestBranch struct {
			PullRequest struct{ ID string }
		} `graphql:"updatePullRequestBranch(input: $input)"`
	}
	method := githubv4.PullRequestBranchUpdateMethodMerge
	input := githubv4.UpdatePullRequestBranchInput{
		PullRequestID: githubv4.ID(prID),
		UpdateMethod:  &method,
	}
	return s.client.Mutate(context.Background(), &m, input, nil)
}

// RequestReviews replaces the PR's requested-reviewer set with logins in a
// single call (union:false — see research/request-reviews.md). logins is the
// full desired set, not a diff: an empty (but non-nil) slice is the valid
// "remove all reviewers" encoding, so callers must not skip calling this when
// logins is empty — only when nothing actually changed.
func (s GraphSource) RequestReviews(prID string, logins []string) error {
	var m struct {
		RequestReviewsByLogin struct {
			PullRequest struct{ ID string }
		} `graphql:"requestReviewsByLogin(input: $input)"`
	}
	ul := make([]githubv4.String, len(logins))
	for i, l := range logins {
		ul[i] = githubv4.String(l)
	}
	input := githubv4.RequestReviewsByLoginInput{
		PullRequestID: githubv4.ID(prID),
		UserLogins:    &ul,
		Union:         githubv4.NewBoolean(false),
	}
	return s.client.Mutate(context.Background(), &m, input, nil)
}
