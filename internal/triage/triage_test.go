package triage

import (
	"testing"

	"github.com/noamsto/prdash/internal/gh"
)

func pr(rollup ...gh.Check) gh.PR { return gh.PR{Number: 1, StatusCheckRollup: rollup} }

func TestLadderPriority(t *testing.T) {
	fail := gh.Check{State: "FAILURE"}
	pass := gh.Check{State: "SUCCESS"}
	cases := []struct {
		name string
		p    gh.PR
		d    gh.PRDetail
		want Kind
	}{
		{"draft wins over everything", gh.PR{IsDraft: true, StatusCheckRollup: []gh.Check{fail}},
			gh.PRDetail{MergeStateStatus: "DIRTY"}, KindDraft},
		{"conflict", pr(pass), gh.PRDetail{MergeStateStatus: "DIRTY"}, KindConflict},
		{"conflict via mergeable", pr(pass), gh.PRDetail{Mergeable: "CONFLICTING"}, KindConflict},
		{"failing checks", pr(pass, fail), gh.PRDetail{MergeStateStatus: "BLOCKED"}, KindChecksFailing},
		{"changes requested",
			gh.PR{Number: 1, ReviewDecision: "CHANGES_REQUESTED", StatusCheckRollup: []gh.Check{pass}},
			gh.PRDetail{MergeStateStatus: "BLOCKED"}, KindChangesRequested},
		{"behind base", pr(pass), gh.PRDetail{MergeStateStatus: "BEHIND"}, KindBehind},
		{"awaiting review", gh.PR{ReviewDecision: "REVIEW_REQUIRED", StatusCheckRollup: []gh.Check{pass}},
			gh.PRDetail{MergeStateStatus: "BLOCKED", ReviewRequests: []gh.ReviewRequest{{Login: "x"}}}, KindAwaitingReview},
		{"pending", pr(gh.Check{State: "PENDING"}), gh.PRDetail{MergeStateStatus: "UNSTABLE"}, KindChecksRunning},
		{"ready", pr(pass), gh.PRDetail{MergeStateStatus: "CLEAN"}, KindReady},
		{"unknown", pr(pass), gh.PRDetail{MergeStateStatus: "UNKNOWN"}, KindPending},
	}
	for _, c := range cases {
		if got := Compute(c.p, c.d).Kind; got != c.want {
			t.Errorf("%s: Kind = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestFailingChecksListed(t *testing.T) {
	card := Compute(pr(gh.Check{State: "FAILURE", Name: "lint"}, gh.Check{State: "SUCCESS", Name: "build"}),
		gh.PRDetail{MergeStateStatus: "BLOCKED"})
	if card.ActionKey != "r" {
		t.Errorf("failing-checks action = %q, want r", card.ActionKey)
	}
	if len(card.Lines) == 0 || card.Lines[0] != "lint" {
		t.Errorf("expected failing check 'lint' listed: %+v", card.Lines)
	}
}
