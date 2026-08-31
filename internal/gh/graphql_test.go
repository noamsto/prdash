package gh

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestMapPRCarriesMergeState(t *testing.T) {
	p := mapPR(qlPR{Mergeable: "CONFLICTING", MergeStateStatus: "DIRTY"})
	if p.Mergeable != "CONFLICTING" || p.MergeStateStatus != "DIRTY" {
		t.Errorf("Mergeable/MergeStateStatus = %q/%q, want CONFLICTING/DIRTY", p.Mergeable, p.MergeStateStatus)
	}
}

func testQuerySource(srv *httptest.Server, repo string) GraphSource {
	hc := &http.Client{}
	st := newRateStore()
	hc.Transport = &rateTransport{next: http.DefaultTransport, store: st}
	return GraphSource{repo: repo, http: hc, client: githubv4.NewEnterpriseClient(srv.URL+"/graphql", hc), rate: st}
}

// TestFetchPRsSendsMergeInfoHeaderAndParsesMergeState exercises the typed
// githubv4.Client list-query path end to end: the Accept header injected by
// rateTransport and qlPR's untagged field names must work together against a
// real (stubbed) HTTP round trip.
func TestFetchPRsSendsMergeInfoHeaderAndParsesMergeState(t *testing.T) {
	var gotAccept, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAccept = r.Header.Get("Accept")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"search":{"nodes":[{"number":1,"mergeable":"CONFLICTING","mergeStateStatus":"DIRTY"}]}}}`))
	}))
	defer srv.Close()

	s := testQuerySource(srv, "owner/repo")
	prs, _, err := s.FetchPRs("is:open", 10)
	if err != nil {
		t.Fatalf("FetchPRs: %v", err)
	}
	if gotAccept != "application/vnd.github.merge-info-preview+json" {
		t.Errorf("Accept = %q, want merge-info-preview", gotAccept)
	}
	if !strings.Contains(gotBody, "mergeable") || !strings.Contains(gotBody, "mergeStateStatus") {
		t.Errorf("query body missing merge fields: %s", gotBody)
	}
	if len(prs) != 1 || prs[0].Mergeable != "CONFLICTING" || prs[0].MergeStateStatus != "DIRTY" {
		t.Errorf("prs = %+v, want one PR with CONFLICTING/DIRTY", prs)
	}
}
