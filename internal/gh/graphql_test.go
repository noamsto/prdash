package gh

import (
	"testing"
	"time"

	"github.com/shurcooL/githubv4"
)

func TestMapPRBotAuthorPrefix(t *testing.T) {
	var bot qlPR
	bot.Author.Login = "dependabot"
	bot.Author.Typename = "Bot"
	if got := mapPR(bot).Author.Login; got != "app/dependabot" {
		t.Errorf("bot author = %q, want app/dependabot (gh's app/ prefix)", got)
	}

	var human qlPR
	human.Author.Login = "octocat"
	human.Author.Typename = "User"
	if got := mapPR(human).Author.Login; got != "octocat" {
		t.Errorf("user author = %q, want octocat (no prefix)", got)
	}
}

// TestMapPRNodeID pins the field the PR mutations (merge, ready, etc.) key
// off — the node ID, not the number — is carried through from qlPR into
// gh.PR.
func TestMapPRNodeID(t *testing.T) {
	p := mapPR(qlPR{ID: "PR_kwDOtest", Number: 42})
	if p.ID != "PR_kwDOtest" {
		t.Errorf("ID = %q, want PR_kwDOtest", p.ID)
	}
}

func TestMapPRNullableTimes(t *testing.T) {
	p := mapPR(qlPR{UpdatedAt: githubv4.DateTime{Time: time.Unix(100, 0)}}) // MergedAt/ClosedAt nil
	if p.UpdatedAt.IsZero() {
		t.Error("UpdatedAt should be set")
	}
	if !p.MergedAt.IsZero() || !p.ClosedAt.IsZero() {
		t.Error("nil MergedAt/ClosedAt should map to zero time")
	}
}

// TestMapRollupUnion pins the flattening Check.Result/CIState depend on: a
// CheckRun keeps name+conclusion, a StatusContext keeps context+state.
func TestMapRollupUnion(t *testing.T) {
	run := qlCheckNode{Typename: "CheckRun"}
	run.CheckRun.Name = "unit-tests"
	run.CheckRun.Conclusion = "FAILURE"
	run.CheckRun.DetailsURL = "https://example/job/9"

	ext := qlCheckNode{Typename: "StatusContext"}
	ext.StatusContext.Context = "ci/external"
	ext.StatusContext.State = "SUCCESS"

	rollup := &qlRollup{}
	rollup.Contexts.Nodes = []qlCheckNode{run, ext}

	var g qlPR
	g.Commits.Nodes = []struct {
		Commit struct {
			StatusCheckRollup *qlRollup
		}
	}{{Commit: struct {
		StatusCheckRollup *qlRollup
	}{StatusCheckRollup: rollup}}}

	p := mapPR(g)
	if len(p.StatusCheckRollup) != 2 {
		t.Fatalf("rollup len = %d, want 2", len(p.StatusCheckRollup))
	}
	if c := p.StatusCheckRollup[0]; c.Name != "unit-tests" || c.Result() != "fail" {
		t.Errorf("checkrun = %+v, want name=unit-tests result=fail", c)
	}
	if c := p.StatusCheckRollup[1]; c.Context != "ci/external" || c.Result() != "pass" {
		t.Errorf("statuscontext = %+v, want context=ci/external result=pass", c)
	}
	if p.CIState() != "fail" {
		t.Errorf("CIState = %q, want fail", p.CIState())
	}
}
