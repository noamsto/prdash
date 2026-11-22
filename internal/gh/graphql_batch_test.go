package gh

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
)

// redirectTransport sends every request to srv, so the raw GraphQL calls that
// target githubGraphQLURL land on the test server instead of the network.
type redirectTransport struct{ srv *url.URL }

func (rt redirectTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.URL.Scheme, r.URL.Host = rt.srv.Scheme, rt.srv.Host
	return http.DefaultTransport.RoundTrip(r)
}

type batchReq struct {
	Query     string            `json:"query"`
	Variables map[string]string `json:"variables"`
}

// batchServer answers every POST via handler and returns a GraphSource whose
// traffic flows through the rate transport, so source.rate.requests() counts
// the round trips.
func batchServer(t *testing.T, handler func(batchReq) (int, string)) GraphSource {
	t.Helper()
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var req batchReq
		if err := json.Unmarshal(raw, &req); err != nil {
			t.Errorf("decode request: %v", err)
		}
		mu.Lock()
		status, body := handler(req)
		mu.Unlock()
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	rate := newRateStore()
	hc := &http.Client{Transport: &rateTransport{next: redirectTransport{u}, store: rate}}
	return GraphSource{repo: "a/x", http: hc, rate: rate}
}

// serveBody answers every request with a fixed 200 body.
func serveBody(t *testing.T, body string) GraphSource {
	return batchServer(t, func(batchReq) (int, string) { return http.StatusOK, body })
}

func refsOf(repo string, numbers ...int) []PRRef {
	refs := make([]PRRef, 0, len(numbers))
	for _, n := range numbers {
		refs = append(refs, PRRef{Repo: repo, Number: n})
	}
	return refs
}

var threeRepoRefs = []PRRef{{"a/x", 1}, {"b/y", 2}, {"c/z", 3}}

// ok200 answers a 3-repo request where every repo resolves its one PR.
func ok200(prefix, fields string) func(batchReq) (int, string) {
	return func(batchReq) (int, string) {
		return http.StatusOK, fmt.Sprintf(`{"data":{"r0":{"%[1]s1":%[2]s},"r1":{"%[1]s2":%[2]s},"r2":{"%[1]s3":%[2]s}}}`, prefix, fields)
	}
}

func TestFetchersSendOneRequestForThreeRepos(t *testing.T) {
	t.Run("checks", func(t *testing.T) {
		s := batchServer(t, ok200("pr", `{"commits":{"nodes":[]}}`))
		got, err := s.FetchChecks(threeRepoRefs)
		if err != nil || len(got) != 3 {
			t.Fatalf("got %d results, err %v", len(got), err)
		}
		if n := s.rate.requests(); n != 1 {
			t.Errorf("requests = %d, want 1", n)
		}
	})
	t.Run("states", func(t *testing.T) {
		s := batchServer(t, ok200("n", `{"__typename":"PullRequest","state":"OPEN"}`))
		got, err := s.FetchStates(threeRepoRefs)
		if err != nil || len(got) != 3 {
			t.Fatalf("got %d results, err %v", len(got), err)
		}
		if n := s.rate.requests(); n != 1 {
			t.Errorf("requests = %d, want 1", n)
		}
	})
	t.Run("details", func(t *testing.T) {
		s := batchServer(t, ok200("pr", `{"mergeStateStatus":"CLEAN"}`))
		got, raws, err := s.FetchDetails(threeRepoRefs)
		if err != nil || len(got) != 3 || len(raws) != 3 {
			t.Fatalf("got %d details %d raws, err %v", len(got), len(raws), err)
		}
		if n := s.rate.requests(); n != 1 {
			t.Errorf("requests = %d, want 1", n)
		}
	})
}

func TestFetchersChunkAboveTheCap(t *testing.T) {
	refs := refsOf("a/x", func() []int {
		ns := make([]int, detailSpec.maxPRs+1)
		for i := range ns {
			ns[i] = i + 1
		}
		return ns
	}()...)
	s := batchServer(t, func(batchReq) (int, string) { return http.StatusOK, `{"data":{"r0":{}}}` })
	if _, _, err := s.FetchDetails(refs); err != nil {
		t.Fatal(err)
	}
	if n := s.rate.requests(); n != 2 {
		t.Errorf("requests = %d, want 2 (cap %d + 1 PR)", n, detailSpec.maxPRs)
	}
}

func TestChunkRefs(t *testing.T) {
	refs := []PRRef{{"b/y", 1}, {"a/x", 1}, {"a/x", 2}, {"a/x", 3}, {"c/z", 9}}
	chunks := chunkRefs(refs, 3)
	want := [][]repoGroup{
		{{"a/x", []int{1, 2, 3}}},
		{{"b/y", []int{1}}, {"c/z", []int{9}}},
	}
	if fmt.Sprint(chunks) != fmt.Sprint(want) {
		t.Errorf("chunks = %v, want %v", chunks, want)
	}
	split := chunkRefs([]PRRef{{"a/x", 1}, {"a/x", 2}, {"a/x", 3}}, 2)
	if fmt.Sprint(split) != fmt.Sprint([][]repoGroup{{{"a/x", []int{1, 2}}}, {{"a/x", []int{3}}}}) {
		t.Errorf("a repo must split across chunks, got %v", split)
	}
	if chunkRefs(nil, 3) != nil {
		t.Error("no refs, no chunks")
	}
}

func TestBuildBatchQueryAliasesReposAndVariables(t *testing.T) {
	q, vars := buildBatchQuery([]repoGroup{{"a/x", []int{1, 2}}, {"b/y", []int{3}}}, checksSpec)
	for _, want := range []string{
		"query($o0:String!,$n0:String!,$o1:String!,$n1:String!){",
		"r0:repository(owner:$o0,name:$n0){pr1:pullRequest(number:1)",
		"pr2:pullRequest(number:2)",
		"r1:repository(owner:$o1,name:$n1){pr3:pullRequest(number:3)",
	} {
		if !strings.Contains(q, want) {
			t.Errorf("query missing %q:\n%s", want, q)
		}
	}
	if vars["o0"] != "a" || vars["n0"] != "x" || vars["o1"] != "b" || vars["n1"] != "y" {
		t.Errorf("vars = %v", vars)
	}
}

// A hostile owner/name must only ever appear as a variable value, never in the
// query text.
func TestBuildBatchQueryNeverInlinesRepoNames(t *testing.T) {
	evil := `a"){x} #/y"){z`
	q, vars := buildBatchQuery([]repoGroup{{evil, []int{1}}}, checksSpec)
	if strings.Contains(q, "#") || strings.Contains(q, `"`) || strings.Contains(q, "x}") {
		t.Errorf("repo name leaked into query:\n%s", q)
	}
	if vars["o0"] != `a"){x} #` || vars["n0"] != `y"){z` {
		t.Errorf("vars = %v", vars)
	}
}

func TestFetchChecksOneRepoErrorsOthersSucceed(t *testing.T) {
	body := `{"data":{"r0":{"pr1":{"commits":{"nodes":[]}}},"r1":null,"r2":{"pr3":{"commits":{"nodes":[]}}}},
		"errors":[{"message":"Could not resolve to a Repository with the name 'b/y'.","path":["r1"],"type":"NOT_FOUND"}]}`
	got, err := serveBody(t, body).FetchChecks(threeRepoRefs)
	if err == nil || !strings.Contains(err.Error(), "b/y") {
		t.Fatalf("want b/y's error joined, got %v", err)
	}
	if _, ok := got[PRRef{"a/x", 1}]; !ok {
		t.Error("a/x#1 must survive b/y failing")
	}
	if _, ok := got[PRRef{"c/z", 3}]; !ok {
		t.Error("c/z#3 must survive b/y failing")
	}
	if _, ok := got[PRRef{"b/y", 2}]; ok {
		t.Error("b/y#2 must be absent")
	}
}

// A strict kind fails the whole repo when any of its aliases errors, as the
// per-repo request did: a half-resolved repo would paint stale rows as fresh.
func TestStrictKindFailsWholeRepoOnScopedAliasError(t *testing.T) {
	body := `{"data":{"r0":{"pr1":{"commits":{"nodes":[]}},"pr2":null}},
		"errors":[{"message":"Could not resolve to a PullRequest with the number of 2.","path":["r0","pr2"]}]}`
	got, err := serveBody(t, body).FetchChecks(refsOf("a/x", 1, 2))
	if err == nil {
		t.Fatal("want an error")
	}
	if len(got) != 0 {
		t.Errorf("whole repo must fail, got %v", got)
	}
}

func TestFetchStatesKeepsSiblingsOfNullAlias(t *testing.T) {
	body := `{"data":{"r0":{"n1":{"__typename":"PullRequest","state":"MERGED"},"n2":null},"r1":null},
		"errors":[{"message":"gone","path":["r0","n2"]},{"message":"no access","path":["r1"]}]}`
	got, err := serveBody(t, body).FetchStates([]PRRef{{"a/x", 1}, {"a/x", 2}, {"b/y", 5}})
	if err == nil || !strings.Contains(err.Error(), "b/y") {
		t.Fatalf("want b/y's error, got %v", err)
	}
	if got[PRRef{"a/x", 1}].State != "MERGED" || len(got) != 1 {
		t.Errorf("got %v, want only a/x#1", got)
	}
}

func TestFetchHTTPFailureFailsOnlyItsChunk(t *testing.T) {
	calls := 0
	s := batchServer(t, func(batchReq) (int, string) {
		calls++
		if calls == 1 {
			return http.StatusInternalServerError, "boom"
		}
		return http.StatusOK, `{"data":{"r0":{"n40":{"__typename":"Issue","state":"OPEN"}}}}`
	})
	ns := make([]int, statesSpec.maxPRs+1)
	for i := range ns {
		ns[i] = i + 1
	}
	ns[len(ns)-1] = 40
	got, err := s.FetchStates(refsOf("a/x", ns...))
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("want first chunk's failure, got %v", err)
	}
	if len(got) != 1 {
		t.Errorf("second chunk must still apply, got %v", got)
	}
}

func TestSplitAcrossChunksReportsRepoErrorOnce(t *testing.T) {
	s := serveBody(t, `{"data":{"r0":null},"errors":[{"message":"nope","path":["r0"]}]}`)
	ns := make([]int, detailSpec.maxPRs+1)
	for i := range ns {
		ns[i] = i + 1
	}
	_, _, err := s.FetchDetails(refsOf("a/x", ns...))
	if err == nil || strings.Count(err.Error(), "a/x") != 1 {
		t.Fatalf("want one a/x error, got %v", err)
	}
}

func TestFetchBadRepoNameFailsAloneOthersStillGoOut(t *testing.T) {
	s := batchServer(t, func(r batchReq) (int, string) {
		if strings.Contains(r.Query, "$o1") {
			t.Errorf("bad repo must not reach the request: %s", r.Query)
		}
		return http.StatusOK, `{"data":{"r0":{"n1":{"__typename":"Issue","state":"OPEN"}}}}`
	})
	got, err := s.FetchStates([]PRRef{{"a/x", 1}, {"nonsense", 2}})
	if err == nil || !strings.Contains(err.Error(), "nonsense") {
		t.Fatalf("want bad-repo error, got %v", err)
	}
	if len(got) != 1 {
		t.Errorf("got %v, want a/x#1", got)
	}
}

func TestDetailRequestCarriesMergeInfoAccept(t *testing.T) {
	var accept string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		accept = r.Header.Get("Accept")
		_, _ = w.Write([]byte(`{"data":{"r0":{}}}`))
	}))
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	s := GraphSource{http: &http.Client{Transport: redirectTransport{u}}}
	if _, _, err := s.FetchDetails(refsOf("a/x", 1)); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(accept, "merge-info-preview") {
		t.Errorf("Accept = %q", accept)
	}
}

func TestRateStoreRequestsNilSafe(t *testing.T) {
	var s *rateStore
	if s.requests() != 0 {
		t.Error("nil store reports 0")
	}
}
