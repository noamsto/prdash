package gh

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
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
	done := githubv4.DateTime{Time: time.Date(2026, 7, 30, 9, 1, 20, 0, time.UTC)}
	run.CheckRun.CompletedAt = &done

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
	if c := p.StatusCheckRollup[0]; c.CompletedAt != "2026-07-30T09:01:20Z" || c.StartedAt != "" {
		t.Errorf("CompletedAt/StartedAt = %q/%q", c.CompletedAt, c.StartedAt)
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
	return GraphSource{repo: repo, http: hc, client: graphClient{githubv4.NewEnterpriseClient(srv.URL+"/graphql", hc)}, rate: st}
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
		_, _ = w.Write([]byte(`{"data":{"search":{"nodes":[{"number":1,"repository":{"nameWithOwner":"owner/repo"},"mergeable":"CONFLICTING","mergeStateStatus":"DIRTY"}]}}}`))
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
	if prs[0].Repo != "owner/repo" {
		t.Errorf("Repo = %q, want owner/repo", prs[0].Repo)
	}
	if got, want := prs[0].Ref(), (PRRef{Repo: "owner/repo", Number: 1}); got != want {
		t.Errorf("Ref = %+v, want %+v", got, want)
	}
}

func TestFetchCrossRepoPRs(t *testing.T) {
	type crossReq struct {
		Query     string `json:"query"`
		Variables struct {
			Authored string `json:"authored"`
			Owned    string `json:"owned"`
			Limit    int    `json:"limit"`
		} `json:"variables"`
	}
	var reqs []crossReq
	node := func(n int, repo, ts string) string {
		return fmt.Sprintf(`{"number":%d,"repository":{"nameWithOwner":%q},"updatedAt":%q}`, n, repo, ts)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var req struct {
			Query     string `json:"query"`
			Variables struct {
				Authored string `json:"authored"`
				Owned    string `json:"owned"`
				Limit    int    `json:"limit"`
			} `json:"variables"`
		}
		_ = json.Unmarshal(b, &req)
		reqs = append(reqs, req)
		w.Header().Set("Content-Type", "application/json")
		// own/repo#1 is authored by me in my own repo, so both searches hit it.
		_, _ = fmt.Fprintf(w, `{"data":{"authored":{"nodes":[%s,%s]},"owned":{"nodes":[%s,%s,%s]}}}`,
			node(1, "me/own", "2026-01-02T00:00:00Z"), node(2, "up/stream", "2026-01-01T00:00:00Z"),
			node(3, "me/own", "2026-01-05T00:00:00Z"), node(1, "me/own", "2026-01-02T00:00:00Z"),
			node(4, "me/own", "2026-01-03T00:00:00Z"))
	}))
	defer srv.Close()

	s := testQuerySource(srv, "owner/repo")
	prs, raw, err := s.FetchCrossRepoPRs("is:open archived:false sort:updated-desc", 3)
	if err != nil {
		t.Fatalf("FetchCrossRepoPRs: %v", err)
	}
	if len(reqs) != 1 {
		t.Fatalf("HTTP requests = %d, want exactly 1", len(reqs))
	}
	q := reqs[0].Query
	if !strings.Contains(q, "authored: search(") || !strings.Contains(q, "owned: search(") {
		t.Errorf("query lacks both aliased searches: %s", q)
	}
	v := reqs[0].Variables
	if want := "is:pr -repo:owner/repo is:open archived:false sort:updated-desc author:@me"; v.Authored != want {
		t.Errorf("authored q = %q, want %q", v.Authored, want)
	}
	if want := "is:pr -repo:owner/repo is:open archived:false sort:updated-desc user:@me"; v.Owned != want {
		t.Errorf("owned q = %q, want %q", v.Owned, want)
	}
	if strings.Contains(v.Authored+v.Owned, "involves:") {
		t.Error("cross-repo queries must not use involves:")
	}
	if v.Limit != 3 {
		t.Errorf("limit = %d, want 3", v.Limit)
	}
	var got []int
	for _, p := range prs {
		got = append(got, p.Number)
	}
	// #1 deduped, newest first (3, 4, 1, 2), capped at 3 → 3, 4, 1.
	if want := []int{3, 4, 1}; !slices.Equal(got, want) {
		t.Errorf("PR numbers = %v, want %v", got, want)
	}
	var back []PR
	if err := json.Unmarshal(raw, &back); err != nil || len(back) != 3 {
		t.Errorf("raw round-trip = %d PRs, err %v; want 3", len(back), err)
	}
}
