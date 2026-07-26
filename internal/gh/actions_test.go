package gh

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/go-github/v89/github"
	"golang.org/x/oauth2"
)

// testSource builds a GraphSource whose Actions client and oauth2
// Authorization header target srv, mirroring NewGraphSource's real wiring
// minus the live network origin.
func testSource(t *testing.T, srv *httptest.Server, repo, token string) GraphSource {
	t.Helper()
	hc := oauth2.NewClient(context.Background(),
		oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token}))
	actions, err := github.NewClient(github.WithHTTPClient(hc), github.WithURLs(&srv.URL, nil))
	if err != nil {
		t.Fatal(err)
	}
	return GraphSource{repo: repo, http: hc, actions: actions}
}

// requireHeaders asserts the headers go-github puts on every Actions request.
// The API version is spelled out rather than read back from the library so a
// future change to its default surfaces here instead of passing silently.
func requireHeaders(t *testing.T, r *http.Request, wantAuth string) {
	t.Helper()
	if got := r.Header.Get("Authorization"); got != wantAuth {
		t.Errorf("Authorization = %q, want %q", got, wantAuth)
	}
	if got := r.Header.Get("Accept"); got != "application/vnd.github.v3+json" {
		t.Errorf("Accept = %q, want application/vnd.github.v3+json", got)
	}
	if got := r.Header.Get("X-GitHub-Api-Version"); got != "2022-11-28" {
		t.Errorf("X-GitHub-Api-Version = %q, want 2022-11-28", got)
	}
}

func TestListRunsForBranch(t *testing.T) {
	var gotPath, gotMethod string
	var gotQuery url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod, gotQuery = r.URL.Path, r.Method, r.URL.Query()
		requireHeaders(t, r, "Bearer tok123")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"total_count":2,"workflow_runs":[
			{"id":9999999999,"conclusion":"failure","head_sha":"abc123"},
			{"id":8888888888,"conclusion":null,"head_sha":"abc123"}
		]}`))
	}))
	defer srv.Close()

	s := testSource(t, srv, "owner/repo", "tok123")
	runs, err := s.ListRunsForBranch("feat/x")
	if err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodGet {
		t.Errorf("method = %q, want GET", gotMethod)
	}
	if want := "/repos/owner/repo/actions/runs"; gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
	// Assert the params, not a literal query string: go-github serializes
	// ListWorkflowRunsOptions in struct-field order, which isn't ours to pin.
	if got := gotQuery.Get("branch"); got != "feat/x" {
		t.Errorf("branch = %q, want feat/x", got)
	}
	if got := gotQuery.Get("per_page"); got != "20" {
		t.Errorf("per_page = %q, want 20", got)
	}
	if len(runs) != 2 {
		t.Fatalf("runs = %+v, want 2 entries", runs)
	}
	// id must widen to int64: this value overflows int32.
	if runs[0].ID != 9999999999 || runs[0].Conclusion != "failure" || runs[0].HeadSHA != "abc123" {
		t.Errorf("runs[0] = %+v", runs[0])
	}
	if runs[1].ID != 8888888888 || runs[1].Conclusion != "" {
		t.Errorf("runs[1] (in-progress, null conclusion) = %+v", runs[1])
	}
}

func TestListRunsForBranchHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"Not Found"}`))
	}))
	defer srv.Close()

	s := testSource(t, srv, "owner/repo", "tok")
	if _, err := s.ListRunsForBranch("feat/x"); err == nil {
		t.Fatal("expected an error on 404")
	}
}

func TestRerunFailedJobs(t *testing.T) {
	var gotPath, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		requireHeaders(t, r, "Bearer tok123")
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	s := testSource(t, srv, "owner/repo", "tok123")
	if err := s.RerunFailedJobs(200); err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if want := "/repos/owner/repo/actions/runs/200/rerun-failed-jobs"; gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
}

func TestRerunJob(t *testing.T) {
	var gotPath, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		requireHeaders(t, r, "Bearer tok123")
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	s := testSource(t, srv, "owner/repo", "tok123")
	if err := s.RerunJob(555); err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if want := "/repos/owner/repo/actions/jobs/555/rerun"; gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
}

func TestRerunFailedJobsNon2xxIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"rate limited"}`))
	}))
	defer srv.Close()

	s := testSource(t, srv, "owner/repo", "tok")
	if err := s.RerunFailedJobs(1); err == nil {
		t.Fatal("expected an error on non-2xx")
	}
}

// TestJobLogRedirectDropsAuthOnFollowup is the load-bearing security
// assertion: the api.github.com call carries the token, but the follow-up
// request to the redirected blob-storage URL must NOT.
func TestJobLogRedirectDropsAuthOnFollowup(t *testing.T) {
	var blobAuthHeader string
	var blobAuthSeen bool
	blob := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		blobAuthSeen = true
		blobAuthHeader = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(
			"2024-01-02T03:04:05.0000000Z ##[group]Run actions/checkout@v4\n" +
				"2024-01-02T03:04:06.0000000Z checking out\n" +
				"2024-01-02T03:04:07.0000000Z ##[endgroup]\n",
		))
	}))
	defer blob.Close()

	var apiAuthHeader string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiAuthHeader = r.Header.Get("Authorization")
		if got, want := r.URL.Path, "/repos/owner/repo/actions/jobs/42/logs"; got != want {
			t.Errorf("path = %q, want %q", got, want)
		}
		w.Header().Set("Location", blob.URL)
		w.WriteHeader(http.StatusFound)
	}))
	defer api.Close()

	s := testSource(t, api, "owner/repo", "supersecrettoken")
	raw, err := s.JobLog(42, false)
	if err != nil {
		t.Fatal(err)
	}
	if apiAuthHeader != "Bearer supersecrettoken" {
		t.Errorf("api.github.com Authorization = %q, want Bearer supersecrettoken", apiAuthHeader)
	}
	if !blobAuthSeen {
		t.Fatal("blob server never received the follow-up request")
	}
	if blobAuthHeader != "" {
		t.Fatalf("blob-storage follow-up leaked Authorization: %q (must be empty)", blobAuthHeader)
	}
	if !strings.Contains(string(raw), "checking out") {
		t.Errorf("raw log missing expected content: %q", raw)
	}
}

// TestActionsCtxCarriesDeadline guards the job-log first hop against hanging
// forever. GetWorkflowJobLogs reaches Transport.RoundTrip directly, so it never
// sees http.Client.Timeout — graphTimeout only binds it if it rides on the ctx.
func TestActionsCtxCarriesDeadline(t *testing.T) {
	ctx, cancel := actionsCtx()
	defer cancel()
	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("actionsCtx returned a ctx with no deadline")
	}
	if remaining := time.Until(deadline); remaining <= 0 || remaining > graphTimeout {
		t.Errorf("deadline in %v, want (0, %v]", remaining, graphTimeout)
	}
}

func TestJobLogNon302IsError(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer api.Close()

	s := testSource(t, api, "owner/repo", "tok")
	if _, err := s.JobLog(1, false); err == nil {
		t.Fatal("expected an error on non-302 response")
	}
}

func TestNativeLogToGHFormatGroupsSteps(t *testing.T) {
	raw := []byte(
		"2024-01-02T03:04:05.0000000Z ##[group]Run actions/checkout@v4\n" +
			"2024-01-02T03:04:06.0000000Z checking out\n" +
			"2024-01-02T03:04:07.0000000Z ##[endgroup]\n" +
			"2024-01-02T03:04:08.0000000Z ##[group]Run go test ./...\n" +
			"2024-01-02T03:04:09.0000000Z ##[error]test failed\n" +
			"2024-01-02T03:04:10.0000000Z ##[endgroup]\n",
	)
	full := nativeLogToGHFormat(raw, false)
	lines := strings.Split(strings.TrimRight(string(full), "\n"), "\n")
	if len(lines) != 6 {
		t.Fatalf("full log lines = %d, want 6: %q", len(lines), full)
	}
	for _, l := range lines {
		parts := strings.SplitN(l, "\t", 3)
		if len(parts) != 3 {
			t.Fatalf("line not tab-delimited job\\tstep\\tcontent: %q", l)
		}
	}

	failedOnly := nativeLogToGHFormat(raw, true)
	failedLines := strings.Split(strings.TrimRight(string(failedOnly), "\n"), "\n")
	if len(failedLines) != 3 {
		t.Fatalf("failed-only lines = %d, want 3 (only the failing step's group): %q", len(failedLines), failedOnly)
	}
	for _, l := range failedLines {
		if !strings.Contains(l, "go test") && !strings.Contains(l, "##[error]") && !strings.Contains(l, "##[endgroup]") {
			t.Errorf("unexpected line kept in failed-only output: %q", l)
		}
	}
}

func TestNativeLogToGHFormatNoErrorMarkerKeepsEverything(t *testing.T) {
	// A job with no "##[error]" line anywhere (e.g. failure surfaced without
	// the marker) must not silently return an empty log when failedOnly.
	raw := []byte("2024-01-02T03:04:05.0000000Z plain output, no groups\n")
	got := nativeLogToGHFormat(raw, true)
	if len(got) == 0 {
		t.Fatal("expected the fallback to keep all lines when no step matched the failed heuristic")
	}
}
