package gh

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"golang.org/x/oauth2"
)

// testSource builds a GraphSource whose REST calls (restBase()) and oauth2
// Authorization header target srv, mirroring NewGraphSource's real wiring
// minus the live network origin.
func testSource(srv *httptest.Server, repo, token string) GraphSource {
	hc := oauth2.NewClient(context.Background(),
		oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token}))
	return GraphSource{repo: repo, http: hc, token: token, apiBase: srv.URL}
}

func requireHeaders(t *testing.T, r *http.Request, wantAuth string) {
	t.Helper()
	if got := r.Header.Get("Authorization"); got != wantAuth {
		t.Errorf("Authorization = %q, want %q", got, wantAuth)
	}
	if got := r.Header.Get("Accept"); got != "application/vnd.github+json" {
		t.Errorf("Accept = %q, want application/vnd.github+json", got)
	}
	if got := r.Header.Get("X-GitHub-Api-Version"); got != githubAPIVersion {
		t.Errorf("X-GitHub-Api-Version = %q, want %q", got, githubAPIVersion)
	}
}

func TestListRunsForBranch(t *testing.T) {
	var gotPath, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.String(), r.Method
		requireHeaders(t, r, "Bearer tok123")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"total_count":2,"workflow_runs":[
			{"id":9999999999,"conclusion":"failure","head_sha":"abc123"},
			{"id":8888888888,"conclusion":null,"head_sha":"abc123"}
		]}`))
	}))
	defer srv.Close()

	s := testSource(srv, "owner/repo", "tok123")
	runs, err := s.ListRunsForBranch("feat/x")
	if err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodGet {
		t.Errorf("method = %q, want GET", gotMethod)
	}
	wantPath := "/repos/owner/repo/actions/runs?branch=feat%2Fx&per_page=20"
	if gotPath != wantPath {
		t.Errorf("path = %q, want %q", gotPath, wantPath)
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

	s := testSource(srv, "owner/repo", "tok")
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

	s := testSource(srv, "owner/repo", "tok123")
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

	s := testSource(srv, "owner/repo", "tok123")
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

func TestRerunFailedJobsNon201IsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"rate limited"}`))
	}))
	defer srv.Close()

	s := testSource(srv, "owner/repo", "tok")
	if err := s.RerunFailedJobs(1); err == nil {
		t.Fatal("expected an error on non-201")
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

	s := testSource(api, "owner/repo", "supersecrettoken")
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

// TestJobLogClientsCarryTimeout guards against both job-log hops hanging
// forever: neither reuses s.http (see JobLog's doc comment), so each must
// build its own client bounded by graphTimeout.
func TestJobLogClientsCarryTimeout(t *testing.T) {
	if got := timeoutHTTPClient(nil).Timeout; got != graphTimeout {
		t.Errorf("blob-fetch client Timeout = %v, want %v", got, graphTimeout)
	}
	noRedirect := timeoutHTTPClient(func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	})
	if noRedirect.Timeout != graphTimeout {
		t.Errorf("no-redirect client Timeout = %v, want %v", noRedirect.Timeout, graphTimeout)
	}
	if noRedirect.CheckRedirect == nil {
		t.Fatal("no-redirect client must still refuse to auto-follow redirects")
	}
}

func TestJobLogNon302IsError(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer api.Close()

	s := testSource(api, "owner/repo", "tok")
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
