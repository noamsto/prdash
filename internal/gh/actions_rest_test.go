package gh

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

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

func TestListRunsMergesLiveLists(t *testing.T) {
	var mu sync.Mutex
	var requests []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requireHeaders(t, r, "Bearer tok")
		mu.Lock()
		requests = append(requests, r.URL.RawQuery)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("status") {
		case "in_progress":
			_, _ = w.Write([]byte(`{"workflow_runs":[
				{"id":3,"status":"in_progress","created_at":"2024-01-01T00:00:00Z","updated_at":"2024-01-01T00:05:00Z"}
			]}`))
		case "queued":
			_, _ = w.Write([]byte(`{"workflow_runs":[]}`))
		default:
			_, _ = w.Write([]byte(`{"workflow_runs":[
				{"id":9999999999,"name":"CI","display_title":"fix: thing","run_number":42,"status":"completed",
				 "conclusion":"failure","head_branch":"feat/x","head_sha":"abc123","event":"push",
				 "actor":{"login":"octo"},"created_at":"2024-02-01T10:00:00Z","updated_at":"2024-02-01T10:05:00Z",
				 "run_started_at":"2024-02-01T10:00:02Z","html_url":"https://github.com/o/r/actions/runs/9999999999"},
				{"id":2,"status":"completed","conclusion":"success","created_at":"2024-01-15T00:00:00Z","updated_at":"2024-01-15T00:01:00Z"}
			]}`))
		}
	}))
	defer srv.Close()

	runs, _, err := testSource(srv, "owner/repo", "tok").ListRuns(50)
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	sort.Strings(requests)
	got := strings.Join(requests, "|")
	mu.Unlock()
	const base = "per_page=50&exclude_pull_requests=true"
	want := base + "|" + base + "&status=in_progress|" + base + "&status=queued"
	if got != want {
		t.Errorf("requests = %q, want %q", got, want)
	}
	if len(runs) != 3 {
		t.Fatalf("runs = %+v, want 3", runs)
	}
	wantFirst := WorkflowRun{
		ID: 9999999999, Conclusion: "failure", HeadSHA: "abc123",
		Name: "CI", DisplayTitle: "fix: thing", RunNumber: 42, Status: "completed",
		HeadBranch: "feat/x", Event: "push", Actor: "octo",
		CreatedAt:    time.Date(2024, 2, 1, 10, 0, 0, 0, time.UTC),
		UpdatedAt:    time.Date(2024, 2, 1, 10, 5, 0, 0, time.UTC),
		RunStartedAt: time.Date(2024, 2, 1, 10, 0, 2, 0, time.UTC),
		HTMLURL:      "https://github.com/o/r/actions/runs/9999999999",
	}
	if runs[0] != wantFirst {
		t.Errorf("runs[0] = %+v, want %+v", runs[0], wantFirst)
	}
	if runs[1].ID != 2 || runs[2].ID != 3 {
		t.Errorf("order = %d, %d, %d; want newest first (9999999999, 2, 3)", runs[0].ID, runs[1].ID, runs[2].ID)
	}
	if !runs[2].Active() {
		t.Errorf("live run absent from the unfiltered page should be present and active: %+v", runs[2])
	}
}

func TestMergeRunsKeepsLatestUpdate(t *testing.T) {
	t0 := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	t.Run("later update wins", func(t *testing.T) {
		stale := WorkflowRun{ID: 1, Status: "in_progress", CreatedAt: t0, UpdatedAt: t0}
		fresh := WorkflowRun{ID: 1, Status: "completed", Conclusion: "success", CreatedAt: t0, UpdatedAt: t0.Add(time.Minute)}
		for _, lists := range [][][]WorkflowRun{{{fresh}, {stale}}, {{stale}, {fresh}}} {
			got := mergeRuns(lists...)
			if len(got) != 1 || got[0].Status != "completed" {
				t.Errorf("got %+v, want the later update", got)
			}
		}
	})
	t.Run("tie prefers completed", func(t *testing.T) {
		live := WorkflowRun{ID: 1, Status: "in_progress", CreatedAt: t0, UpdatedAt: t0}
		done := WorkflowRun{ID: 1, Status: "completed", Conclusion: "success", CreatedAt: t0, UpdatedAt: t0}
		for _, lists := range [][][]WorkflowRun{{{done}, {live}}, {{live}, {done}}} {
			got := mergeRuns(lists...)
			if len(got) != 1 || got[0].Status != "completed" {
				t.Errorf("got %+v, want completed on a tie", got)
			}
		}
	})
	t.Run("ties on created sort by id descending", func(t *testing.T) {
		got := mergeRuns([]WorkflowRun{{ID: 1, CreatedAt: t0}, {ID: 5, CreatedAt: t0}, {ID: 3, CreatedAt: t0}})
		if len(got) != 3 || got[0].ID != 5 || got[1].ID != 3 || got[2].ID != 1 {
			t.Errorf("got %+v", got)
		}
	})
}

func TestListRunsRoundTripsThroughRaw(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"workflow_runs":[
			{"id":1,"name":"CI","status":"completed","conclusion":"success","actor":{"login":"a"},
			 "created_at":"2024-01-01T00:00:00Z","updated_at":"2024-01-01T00:01:00Z","run_started_at":"2024-01-01T00:00:01Z"}
		]}`))
	}))
	defer srv.Close()

	runs, raw, err := testSource(srv, "owner/repo", "tok").ListRuns(50)
	if err != nil {
		t.Fatal(err)
	}
	var back []WorkflowRun
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	if len(back) != len(runs) || back[0] != runs[0] {
		t.Errorf("raw round-trip = %+v, want %+v", back, runs)
	}
}

func TestListRunsEmptyIsNotNil(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"workflow_runs":[]}`))
	}))
	defer srv.Close()

	runs, raw, err := testSource(srv, "owner/repo", "tok").ListRuns(50)
	if err != nil {
		t.Fatal(err)
	}
	if runs == nil || string(raw) != "[]" {
		t.Errorf("runs = %#v, raw = %q; want empty non-nil and []", runs, raw)
	}
}

func TestListRunsDisabled(t *testing.T) {
	tests := []struct {
		name         string
		status       int
		body         string
		wantDisabled bool
	}{
		{"not found", http.StatusNotFound, `{"message":"Not Found"}`, true},
		{"actions disabled", http.StatusForbidden, `{"message":"Actions is disabled for this repository"}`, true},
		{"rate limited", http.StatusForbidden, `{"message":"API rate limit exceeded"}`, false},
		{"server error", http.StatusInternalServerError, `{"message":"boom"}`, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()

			_, _, err := testSource(srv, "owner/repo", "tok").ListRuns(50)
			if err == nil {
				t.Fatal("expected an error")
			}
			if got := ActionsDisabled(err); got != tc.wantDisabled {
				t.Errorf("ActionsDisabled(%v) = %v, want %v", err, got, tc.wantDisabled)
			}
		})
	}
}

func TestListRunJobs(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.String()
		requireHeaders(t, r, "Bearer tok")
		_, _ = w.Write([]byte(`{"total_count":120,"jobs":[
			{"id":9999999999,"name":"build","status":"completed","conclusion":"failure",
			 "started_at":"2024-01-01T00:00:00Z","completed_at":"2024-01-01T00:02:00Z",
			 "html_url":"https://github.com/o/r/actions/runs/1/job/9999999999",
			 "steps":[{"name":"checkout","status":"completed"},{"name":"test","status":"completed"}]},
			{"id":8,"name":"deploy","status":"in_progress","conclusion":null,
			 "started_at":"2024-01-01T00:03:00Z","completed_at":null,
			 "steps":[{"name":"push","status":"in_progress"}]}
		]}`))
	}))
	defer srv.Close()

	jobs, total, err := testSource(srv, "owner/repo", "tok").ListRunJobs(777)
	if err != nil {
		t.Fatal(err)
	}
	if want := "/repos/owner/repo/actions/runs/777/jobs?filter=latest&per_page=100"; gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
	if total != 120 || len(jobs) != 2 {
		t.Fatalf("total = %d, jobs = %+v", total, jobs)
	}
	want := RunJob{
		ID: 9999999999, Name: "build", Status: "completed", Conclusion: "failure",
		StartedAt: "2024-01-01T00:00:00Z", CompletedAt: "2024-01-01T00:02:00Z",
		HTMLURL: "https://github.com/o/r/actions/runs/1/job/9999999999",
		Steps:   []RunJobStep{{"checkout", "completed"}, {"test", "completed"}},
	}
	if !reflect.DeepEqual(jobs[0], want) {
		t.Errorf("jobs[0] = %+v, want %+v", jobs[0], want)
	}
	if jobs[1].Conclusion != "" || jobs[1].CompletedAt != "" || jobs[1].Steps[0].Status != "in_progress" {
		t.Errorf("jobs[1] (null conclusion/completed_at) = %+v", jobs[1])
	}
}

func TestRunJobCheckResult(t *testing.T) {
	tests := []struct {
		status, conclusion, want string
	}{
		{"queued", "", "pending"},
		{"waiting", "", "pending"},
		{"in_progress", "", "pending"},
		{"completed", "success", "pass"},
		{"completed", "skipped", "pass"},
		{"completed", "neutral", "pass"},
		{"completed", "failure", "fail"},
		{"completed", "timed_out", "fail"},
		{"completed", "cancelled", "fail"},
	}
	for _, tc := range tests {
		t.Run(tc.status+"/"+tc.conclusion, func(t *testing.T) {
			j := RunJob{Name: "build", Status: tc.status, Conclusion: tc.conclusion, StartedAt: "s", CompletedAt: "c"}
			c := j.Check()
			if got := c.Result(); got != tc.want {
				t.Errorf("Result() = %q, want %q (check %+v)", got, tc.want, c)
			}
			if c.Name != "build" || c.StartedAt != "s" || c.CompletedAt != "c" {
				t.Errorf("check = %+v, want name and times carried over", c)
			}
		})
	}
}

func TestWorkflowRunActive(t *testing.T) {
	for status, want := range map[string]bool{
		"": false, "completed": false, "queued": true, "in_progress": true, "waiting": true,
	} {
		if got := (WorkflowRun{Status: status}).Active(); got != want {
			t.Errorf("Active(%q) = %v, want %v", status, got, want)
		}
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

func TestJobLogBlobTransportFailureRedactsURL(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}
	blob := fmt.Sprintf("http://127.0.0.1:%d/log?sig=SECRETSIG&se=2099", port)
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, blob, http.StatusFound)
	}))
	defer api.Close()

	s := testSource(api, "owner/repo", "tok")
	_, err = s.JobLog(1, false)
	if err == nil {
		t.Fatal("expected a transport error")
	}
	msg := err.Error()
	for _, leak := range []string{"SECRETSIG", "sig=", "/log?"} {
		if strings.Contains(msg, leak) {
			t.Errorf("error %q leaks %q", msg, leak)
		}
	}
	if !strings.Contains(msg, "connection refused") {
		t.Errorf("error %q lost the transport reason", msg)
	}
	var opErr *net.OpError
	if !errors.As(err, &opErr) {
		t.Errorf("error chain %v no longer reaches *net.OpError", err)
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
