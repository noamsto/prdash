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
		if got := Preliminary(c.p).Kind; got != c.want {
			t.Errorf("%s: Kind = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestChangesRequestedHeadlineNamesReviewers(t *testing.T) {
	rv := func(login string) gh.Review {
		r := gh.Review{State: "CHANGES_REQUESTED"}
		r.Author.Login = login
		return r
	}
	p := gh.PR{Number: 1, ReviewDecision: "CHANGES_REQUESTED", StatusCheckRollup: []gh.Check{{State: "SUCCESS"}}}

	c := Compute(p, gh.PRDetail{MergeStateStatus: "BLOCKED", LatestReviews: []gh.Review{rv("alice"), rv("bob")}})
	if c.Headline != "Changes requested by @alice, @bob" {
		t.Fatalf("Headline = %q, want %q", c.Headline, "Changes requested by @alice, @bob")
	}

	// No reviewer in detail (e.g. team review) → bare headline.
	c = Compute(p, gh.PRDetail{MergeStateStatus: "BLOCKED"})
	if c.Headline != "Changes requested" {
		t.Fatalf("Headline = %q, want %q", c.Headline, "Changes requested")
	}
}

func TestFailingChecksListed(t *testing.T) {
	card := Compute(pr(gh.Check{State: "FAILURE", Name: "lint"}, gh.Check{State: "SUCCESS", Name: "build"}),
		gh.PRDetail{MergeStateStatus: "BLOCKED"})
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
	c := Compute(p, gh.PRDetail{MergeStateStatus: "BLOCKED"})
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
	c := Compute(pr(gh.Check{State: "FAILURE", Name: "lint"}), gh.PRDetail{MergeStateStatus: "BLOCKED"})
	if c.Headline != "1 check failing" {
		t.Fatalf("Headline = %q, want %q", c.Headline, "1 check failing")
	}
	if len(c.Running) != 0 {
		t.Fatalf("Running = %v, want empty", c.Running)
	}
}

func TestChecksRunningCardPopulatesRunning(t *testing.T) {
	c := Compute(pr(gh.Check{State: "PENDING", Name: "build"}), gh.PRDetail{MergeStateStatus: "UNSTABLE"})
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
	))
	if c.Kind != KindChecksFailing {
		t.Fatalf("Kind = %v, want KindChecksFailing", c.Kind)
	}
	if len(c.Failing) != 1 || len(c.Running) != 1 {
		t.Fatalf("Failing=%v Running=%v, want one each", c.Failing, c.Running)
	}
}

func TestComputeSetsAutoMergeFromPR(t *testing.T) {
	pr := gh.PR{State: "OPEN", ReviewDecision: "", AutoMergeRequest: &gh.AutoMergeRequest{MergeMethod: "SQUASH"}}
	c := Compute(pr, gh.PRDetail{MergeStateStatus: "CLEAN"})
	if !c.AutoMerge {
		t.Fatalf("Compute card should carry AutoMerge=true: %+v", c)
	}
}

func TestComputeAutoMergeFalseWhenNotArmed(t *testing.T) {
	pr := gh.PR{}
	c := Compute(pr, gh.PRDetail{MergeStateStatus: "CLEAN"})
	if c.AutoMerge {
		t.Fatalf("Compute card should carry AutoMerge=false: %+v", c)
	}
}

func TestPreliminarySetsAutoMergeFromPR(t *testing.T) {
	pr := gh.PR{State: "OPEN", AutoMergeRequest: &gh.AutoMergeRequest{MergeMethod: "SQUASH"}}
	c := Preliminary(pr)
	if !c.AutoMerge {
		t.Fatalf("Preliminary card should carry AutoMerge=true: %+v", c)
	}
}
