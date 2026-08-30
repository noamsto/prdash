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
		if got := Compute(c.p, c.d, "", 0).Kind; got != c.want {
			t.Errorf("%s: Kind = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestPreliminary(t *testing.T) {
	cases := []struct {
		name string
		p    gh.PR
		want Kind
	}{
		{"draft", gh.PR{IsDraft: true}, KindDraft},
		{"failing", pr(gh.Check{State: "FAILURE", Name: "lint"}), KindChecksFailing},
		{"changes", gh.PR{ReviewDecision: "CHANGES_REQUESTED", StatusCheckRollup: []gh.Check{{State: "SUCCESS"}}}, KindChangesRequested},
		{"running", pr(gh.Check{State: "PENDING"}), KindChecksRunning},
		{"awaiting", gh.PR{ReviewDecision: "REVIEW_REQUIRED", StatusCheckRollup: []gh.Check{{State: "SUCCESS"}}}, KindAwaitingReview},
		{"clean fallback", pr(gh.Check{State: "SUCCESS"}), KindFallback},
	}
	for _, c := range cases {
		if got := Preliminary(c.p, "", 0).Kind; got != c.want {
			t.Errorf("%s: Kind = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestVisibleStackParentBlocksBeforeEveryOtherTriageState(t *testing.T) {
	pr := gh.PR{
		State:          "OPEN",
		IsDraft:        true,
		ReviewDecision: "CHANGES_REQUESTED",
		StatusCheckRollup: []gh.Check{
			{State: "FAILURE", Name: "lint"},
		},
		AutoMergeRequest: &gh.AutoMergeRequest{MergeMethod: "SQUASH"},
	}
	detail := gh.PRDetail{MergeStateStatus: "DIRTY"}
	for _, card := range []Card{
		Preliminary(pr, "", 101),
		Compute(pr, detail, "", 101),
	} {
		if card.Kind != KindBlocked || card.Headline != "Blocked on #101" {
			t.Errorf("card = %+v, want immediate-parent blocker", card)
		}
		if card.ActionKey != "" || card.ActionLabel != "" {
			t.Errorf("blocker actions = %q/%q, want none", card.ActionKey, card.ActionLabel)
		}
		if !card.AutoMerge {
			t.Errorf("blocker card should retain AutoMerge: %+v", card)
		}
	}

	if got := Compute(pr, detail, "", 0); got.Kind != KindDraft {
		t.Errorf("no visible immediate parent Kind = %v, want KindDraft", got.Kind)
	}
}

func TestChangesRequestedHeadlineNamesReviewers(t *testing.T) {
	rv := func(login string) gh.Review {
		r := gh.Review{State: "CHANGES_REQUESTED"}
		r.Author.Login = login
		return r
	}
	p := gh.PR{Number: 1, ReviewDecision: "CHANGES_REQUESTED", StatusCheckRollup: []gh.Check{{State: "SUCCESS"}}}

	c := Compute(p, gh.PRDetail{MergeStateStatus: "BLOCKED", LatestReviews: []gh.Review{rv("alice"), rv("bob")}}, "", 0)
	if c.Headline != "Changes requested by @alice, @bob" {
		t.Fatalf("Headline = %q, want %q", c.Headline, "Changes requested by @alice, @bob")
	}

	// No reviewer in detail (e.g. team review) → bare headline.
	c = Compute(p, gh.PRDetail{MergeStateStatus: "BLOCKED"}, "", 0)
	if c.Headline != "Changes requested" {
		t.Fatalf("Headline = %q, want %q", c.Headline, "Changes requested")
	}
}

func TestFailingChecksListed(t *testing.T) {
	card := Compute(pr(gh.Check{State: "FAILURE", Name: "lint"}, gh.Check{State: "SUCCESS", Name: "build"}),
		gh.PRDetail{MergeStateStatus: "BLOCKED"}, "", 0)
	if card.ActionKey != "r" {
		t.Errorf("failing-checks action = %q, want r", card.ActionKey)
	}
	if len(card.Failing) == 0 || card.Failing[0] != "lint" {
		t.Errorf("expected failing check 'lint' listed: %+v", card.Failing)
	}
}

func TestChecksCardShowsFailingAndRunningTogether(t *testing.T) {
	p := pr(
		gh.Check{State: "FAILURE", Name: "lint"},
		gh.Check{State: "PENDING", Name: "build"},
		gh.Check{State: "PENDING", Name: "e2e"},
	)
	c := Compute(p, gh.PRDetail{MergeStateStatus: "BLOCKED"}, "", 0)
	if c.Kind != KindChecksFailing {
		t.Fatalf("Kind = %v, want KindChecksFailing", c.Kind)
	}
	if got := c.Failing; len(got) != 1 || got[0] != "lint" {
		t.Fatalf("Failing = %v, want [lint]", got)
	}
	if got := c.Running; len(got) != 2 {
		t.Fatalf("Running = %v, want 2 entries", got)
	}
	if c.Headline != "1 failing · 2 running" {
		t.Fatalf("Headline = %q, want %q", c.Headline, "1 failing · 2 running")
	}
}

func TestChecksFailingOnlyHeadlineUnchanged(t *testing.T) {
	c := Compute(pr(gh.Check{State: "FAILURE", Name: "lint"}), gh.PRDetail{MergeStateStatus: "BLOCKED"}, "", 0)
	if c.Headline != "1 check failing" {
		t.Fatalf("Headline = %q, want %q", c.Headline, "1 check failing")
	}
	if len(c.Running) != 0 {
		t.Fatalf("Running = %v, want empty", c.Running)
	}
}

func TestChecksRunningCardPopulatesRunning(t *testing.T) {
	c := Compute(pr(gh.Check{State: "PENDING", Name: "build"}), gh.PRDetail{MergeStateStatus: "UNSTABLE"}, "", 0)
	if c.Kind != KindChecksRunning {
		t.Fatalf("Kind = %v, want KindChecksRunning", c.Kind)
	}
	if got := c.Running; len(got) != 1 || got[0] != "build" {
		t.Fatalf("Running = %v, want [build]", got)
	}
}

func TestPreliminaryFoldsRunningIntoFailingCard(t *testing.T) {
	c := Preliminary(pr(
		gh.Check{State: "FAILURE", Name: "lint"},
		gh.Check{State: "PENDING", Name: "build"},
	), "", 0)
	if c.Kind != KindChecksFailing {
		t.Fatalf("Kind = %v, want KindChecksFailing", c.Kind)
	}
	if len(c.Failing) != 1 || len(c.Running) != 1 {
		t.Fatalf("Failing=%v Running=%v, want one each", c.Failing, c.Running)
	}
}

func TestComputeSetsAutoMergeFromPR(t *testing.T) {
	pr := gh.PR{State: "OPEN", ReviewDecision: "", AutoMergeRequest: &gh.AutoMergeRequest{MergeMethod: "SQUASH"}}
	c := Compute(pr, gh.PRDetail{MergeStateStatus: "CLEAN"}, "", 0)
	if !c.AutoMerge {
		t.Fatalf("Compute card should carry AutoMerge=true: %+v", c)
	}
}

func TestComputeAutoMergeFalseWhenNotArmed(t *testing.T) {
	pr := gh.PR{}
	c := Compute(pr, gh.PRDetail{MergeStateStatus: "CLEAN"}, "", 0)
	if c.AutoMerge {
		t.Fatalf("Compute card should carry AutoMerge=false: %+v", c)
	}
}

func TestPreliminarySetsAutoMergeFromPR(t *testing.T) {
	pr := gh.PR{State: "OPEN", AutoMergeRequest: &gh.AutoMergeRequest{MergeMethod: "SQUASH"}}
	c := Preliminary(pr, "", 0)
	if !c.AutoMerge {
		t.Fatalf("Preliminary card should carry AutoMerge=true: %+v", c)
	}
}

// TestAwaitingReviewSuggestsApprove covers the three viewer cases: another
// author's PR gets the one-key approve, your own never does (GitHub forbids
// self-approval), and an unresolved viewer stays silent rather than guessing.
func TestAwaitingReviewSuggestsApprove(t *testing.T) {
	tests := []struct {
		name    string
		author  string
		viewer  string
		wantKey string
	}{
		{"others PR offers approve", "you", "me", "L"},
		{"own PR does not", "me", "me", ""},
		{"unknown viewer does not", "you", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pr := gh.PR{Number: 1, State: "OPEN", ReviewDecision: "REVIEW_REQUIRED"}
			pr.Author.Login = tt.author
			c := Compute(pr, gh.PRDetail{MergeStateStatus: "CLEAN"}, tt.viewer, 0)
			if c.Kind != KindAwaitingReview {
				t.Fatalf("Kind = %v, want KindAwaitingReview", c.Kind)
			}
			if c.ActionKey != tt.wantKey {
				t.Errorf("ActionKey = %q, want %q", c.ActionKey, tt.wantKey)
			}
		})
	}
}

func TestPreliminaryAwaitingReviewSuggestsApprove(t *testing.T) {
	pr := gh.PR{Number: 1, State: "OPEN", ReviewDecision: "REVIEW_REQUIRED"}
	pr.Author.Login = "you"
	if got := Preliminary(pr, "me", 0).ActionKey; got != "L" {
		t.Errorf("ActionKey = %q, want L", got)
	}
	pr.Author.Login = "me"
	if got := Preliminary(pr, "me", 0).ActionKey; got != "" {
		t.Errorf("ActionKey = %q on own PR, want empty", got)
	}
}
