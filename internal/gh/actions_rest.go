package gh

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

// githubAPIVersion pins the REST API version prdash's Actions client speaks,
// per GitHub's recommendation to send it explicitly on every call.
const githubAPIVersion = "2022-11-28"

// restBase returns the REST API origin, defaulting to api.github.com; apiBase
// is overridden only by tests (via an httptest.Server URL) so production
// callers always hit the real API.
func (s GraphSource) restBase() string {
	if s.apiBase != "" {
		return s.apiBase
	}
	return "https://api.github.com"
}

// repoParts splits s.repo ("owner/name") for building REST paths.
func (s GraphSource) repoParts() (owner, name string, err error) {
	owner, name, ok := strings.Cut(s.repo, "/")
	if !ok {
		return "", "", fmt.Errorf("bad repo %q", s.repo)
	}
	return owner, name, nil
}

// restRequest builds a REST API request against restBase() carrying the
// Accept and API-version headers every endpoint in this file needs.
// Authorization is injected by s.http's oauth2 transport on every RoundTrip,
// so it isn't set here explicitly.
func (s GraphSource) restRequest(method, path string) (*http.Request, error) {
	req, err := http.NewRequest(method, s.restBase()+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", githubAPIVersion)
	return req, nil
}

// restRequestJSON is restRequest with a JSON body and Content-Type.
func (s GraphSource) restRequestJSON(method, path string, payload []byte) (*http.Request, error) {
	req, err := http.NewRequest(method, s.restBase()+path, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", githubAPIVersion)
	req.Header.Set("Content-Type", "application/json")
	return req, nil
}

// ListRunsForBranch lists the 20 most recent workflow runs for branch,
// newest-first, replacing
// `gh run list --branch <b> -L 20 --json databaseId,conclusion,headSha`.
func (s GraphSource) ListRunsForBranch(branch string) ([]WorkflowRun, error) {
	owner, name, err := s.repoParts()
	if err != nil {
		return nil, err
	}
	path := fmt.Sprintf("/repos/%s/%s/actions/runs?branch=%s&per_page=20", owner, name, url.QueryEscape(branch))
	req, err := s.restRequest(http.MethodGet, path)
	if err != nil {
		return nil, err
	}
	resp, err := s.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list runs: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	var parsed struct {
		WorkflowRuns []struct {
			ID         int64  `json:"id"`
			Conclusion string `json:"conclusion"`
			HeadSHA    string `json:"head_sha"`
		} `json:"workflow_runs"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("parse run list: %w", err)
	}
	runs := make([]WorkflowRun, len(parsed.WorkflowRuns))
	for i, r := range parsed.WorkflowRuns {
		runs[i] = WorkflowRun{ID: r.ID, Conclusion: r.Conclusion, HeadSHA: r.HeadSHA}
	}
	return runs, nil
}

// ErrActionsDisabled marks a run listing refused because Actions is off for the
// repo, which is a state to show rather than a failure to retry.
var ErrActionsDisabled = errors.New("actions disabled")

// ActionsDisabled reports whether err is ErrActionsDisabled.
func ActionsDisabled(err error) bool { return errors.Is(err, ErrActionsDisabled) }

// listRunsPage fetches one page of the repo's runs, optionally filtered by status.
func (s GraphSource) listRunsPage(limit int, status string) ([]WorkflowRun, error) {
	owner, name, err := s.repoParts()
	if err != nil {
		return nil, err
	}
	path := fmt.Sprintf("/repos/%s/%s/actions/runs?per_page=%d&exclude_pull_requests=true", owner, name, limit)
	if status != "" {
		path += "&status=" + status
	}
	req, err := s.restRequest(http.MethodGet, path)
	if err != nil {
		return nil, err
	}
	resp, err := s.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		disabled := resp.StatusCode == http.StatusNotFound ||
			(resp.StatusCode == http.StatusForbidden && strings.Contains(strings.ToLower(string(body)), "disabled"))
		if disabled {
			return nil, fmt.Errorf("list runs: %s: %w", resp.Status, ErrActionsDisabled)
		}
		return nil, fmt.Errorf("list runs: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	var parsed struct {
		WorkflowRuns []struct {
			ID           int64     `json:"id"`
			Name         string    `json:"name"`
			DisplayTitle string    `json:"display_title"`
			RunNumber    int       `json:"run_number"`
			Status       string    `json:"status"`
			Conclusion   string    `json:"conclusion"`
			HeadBranch   string    `json:"head_branch"`
			HeadSHA      string    `json:"head_sha"`
			Event        string    `json:"event"`
			CreatedAt    time.Time `json:"created_at"`
			UpdatedAt    time.Time `json:"updated_at"`
			RunStartedAt time.Time `json:"run_started_at"`
			HTMLURL      string    `json:"html_url"`
			Actor        struct {
				Login string `json:"login"`
			} `json:"actor"`
		} `json:"workflow_runs"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("parse run list: %w", err)
	}
	runs := make([]WorkflowRun, len(parsed.WorkflowRuns))
	for i, r := range parsed.WorkflowRuns {
		runs[i] = WorkflowRun{
			ID: r.ID, Conclusion: r.Conclusion, HeadSHA: r.HeadSHA,
			Name: r.Name, DisplayTitle: r.DisplayTitle, RunNumber: r.RunNumber,
			Status: r.Status, HeadBranch: r.HeadBranch, Event: r.Event, Actor: r.Actor.Login,
			CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt, RunStartedAt: r.RunStartedAt,
			HTMLURL: r.HTMLURL,
		}
	}
	return runs, nil
}

// ListRuns lists the repo's runs for the runs board: the limit most recent plus
// the in-progress and queued ones, so a live run older than the recent window
// still shows. The three requests run concurrently and any failure fails the call.
func (s GraphSource) ListRuns(limit int) ([]WorkflowRun, []byte, error) {
	statuses := [3]string{"", "in_progress", "queued"}
	var pages [3][]WorkflowRun
	var errs [3]error
	var wg sync.WaitGroup
	for i, status := range statuses {
		wg.Go(func() {
			pages[i], errs[i] = s.listRunsPage(limit, status)
		})
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			return nil, nil, err
		}
	}
	runs := mergeRuns(pages[0], pages[1], pages[2])
	raw, err := json.Marshal(runs)
	if err != nil {
		return nil, nil, err
	}
	return runs, raw, nil
}

// mergeRuns dedupes by run ID and returns the runs newest first. On a duplicate
// the entry with the latest UpdatedAt wins, completed winning a tie, so a run
// that finished between the three concurrent requests shows its final state.
func mergeRuns(lists ...[]WorkflowRun) []WorkflowRun {
	byID := map[int64]WorkflowRun{}
	for _, list := range lists {
		for _, r := range list {
			prev, ok := byID[r.ID]
			if !ok || r.UpdatedAt.After(prev.UpdatedAt) ||
				(r.UpdatedAt.Equal(prev.UpdatedAt) && !r.Active() && prev.Active()) {
				byID[r.ID] = r
			}
		}
	}
	runs := make([]WorkflowRun, 0, len(byID))
	for _, r := range byID {
		runs = append(runs, r)
	}
	sort.Slice(runs, func(i, j int) bool {
		if !runs[i].CreatedAt.Equal(runs[j].CreatedAt) {
			return runs[i].CreatedAt.After(runs[j].CreatedAt)
		}
		return runs[i].ID > runs[j].ID
	})
	return runs
}

// ListRunJobs fetches the first page of runID's latest-attempt jobs and the
// total job count.
func (s GraphSource) ListRunJobs(runID int64) ([]RunJob, int, error) {
	owner, name, err := s.repoParts()
	if err != nil {
		return nil, 0, err
	}
	req, err := s.restRequest(http.MethodGet, fmt.Sprintf("/repos/%s/%s/actions/runs/%d/jobs?filter=latest&per_page=100", owner, name, runID))
	if err != nil {
		return nil, 0, err
	}
	resp, err := s.http.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, 0, fmt.Errorf("list run jobs: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	var parsed struct {
		TotalCount int `json:"total_count"`
		Jobs       []struct {
			ID          int64  `json:"id"`
			Name        string `json:"name"`
			Status      string `json:"status"`
			Conclusion  string `json:"conclusion"`
			StartedAt   string `json:"started_at"`
			CompletedAt string `json:"completed_at"`
			HTMLURL     string `json:"html_url"`
			Steps       []struct {
				Name   string `json:"name"`
				Status string `json:"status"`
			} `json:"steps"`
		} `json:"jobs"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, 0, fmt.Errorf("parse run jobs: %w", err)
	}
	jobs := make([]RunJob, len(parsed.Jobs))
	for i, j := range parsed.Jobs {
		steps := make([]RunJobStep, len(j.Steps))
		for k, st := range j.Steps {
			steps[k] = RunJobStep{Name: st.Name, Status: st.Status}
		}
		jobs[i] = RunJob{
			ID: j.ID, Name: j.Name, Status: j.Status, Conclusion: j.Conclusion,
			StartedAt: j.StartedAt, CompletedAt: j.CompletedAt, HTMLURL: j.HTMLURL, Steps: steps,
		}
	}
	return jobs, parsed.TotalCount, nil
}

// postAction POSTs an empty body to path and requires 201 Created, the
// documented success status for both rerun endpoints.
func (s GraphSource) postAction(path string) error {
	req, err := s.restRequest(http.MethodPost, path)
	if err != nil {
		return err
	}
	resp, err := s.http.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("%s: %s: %s", path, resp.Status, strings.TrimSpace(string(body)))
	}
	return nil
}

// RerunFailedJobs reruns the failed jobs of runID, replacing
// `gh run rerun <id> --failed`.
func (s GraphSource) RerunFailedJobs(runID int64) error {
	owner, name, err := s.repoParts()
	if err != nil {
		return err
	}
	return s.postAction(fmt.Sprintf("/repos/%s/%s/actions/runs/%d/rerun-failed-jobs", owner, name, runID))
}

// RerunJob reruns a single job and its dependent jobs, replacing
// `gh run rerun --job <id>`.
func (s GraphSource) RerunJob(jobID int64) error {
	owner, name, err := s.repoParts()
	if err != nil {
		return err
	}
	return s.postAction(fmt.Sprintf("/repos/%s/%s/actions/jobs/%d/rerun", owner, name, jobID))
}

// timeoutHTTPClient builds a bare (non-oauth2) http.Client bounded by the same
// graphTimeout every other call in this package carries via s.http, so the
// job-log path's two hops — which can't reuse s.http (see JobLog's doc
// comment) — don't hang the log view forever on a stalled network. checkRedirect
// is nil for the plain, redirect-following blob fetch.
func timeoutHTTPClient(checkRedirect func(*http.Request, []*http.Request) error) *http.Client {
	return &http.Client{Timeout: graphTimeout, CheckRedirect: checkRedirect}
}

// JobLog fetches jobID's plain-text log via the REST single-job endpoint,
// which responds with a short-lived (~1 minute) redirect to a blob-storage
// URL rather than the body directly, and converts it into the tab-delimited
// "job\tstep\ttimestamp content" shape `gh run view --log[-failed]` emits, so
// internal/ui's parseJobLog (which expects that shape) consumes either source
// unchanged. failedOnly filters the result to the failed step(s) client-side —
// GitHub's REST API has no server-side "failed only" log filter, unlike gh's
// --log-failed.
//
// CRITICAL: this does NOT reuse s.http for the redirect hop. s.http's
// transport is oauth2-wrapped and re-injects the Authorization header on
// every RoundTrip, including a follow-up request to the blob-storage host —
// which would leak the GitHub token off github.com. Instead this builds a
// dedicated, non-oauth2 client that never auto-follows, reads Location off
// the 302 itself, then issues a second, independent, unauthenticated request
// to that URL.
func (s GraphSource) JobLog(jobID int64, failedOnly bool) ([]byte, error) {
	owner, name, err := s.repoParts()
	if err != nil {
		return nil, err
	}
	path := fmt.Sprintf("/repos/%s/%s/actions/jobs/%d/logs", owner, name, jobID)
	req, err := http.NewRequest(http.MethodGet, s.restBase()+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+s.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", githubAPIVersion)

	noRedirect := timeoutHTTPClient(func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	})
	// This hop bypasses s.http but still spends from the core bucket, so it
	// records its own rate headers — otherwise opening a job log undercounts core.
	noRedirect.Transport = &rateTransport{next: http.DefaultTransport, store: s.rate}
	resp, err := noRedirect.Do(req)
	if err != nil {
		return nil, err
	}
	loc := resp.Header.Get("Location")
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusFound || loc == "" {
		return nil, fmt.Errorf("job log: expected 302 with Location, got %s", resp.Status)
	}

	// Second, independent request: no auth headers at all. The blob URL is
	// already a fully-signed, short-lived query-string-authenticated link;
	// attaching a GitHub token here both leaks it needlessly off github.com and
	// can trigger a signature-mismatch rejection from the storage backend. It
	// still needs its own timeout, though — neither hop reuses s.http, so
	// neither inherits its bound.
	logResp, err := timeoutHTTPClient(nil).Get(loc)
	if err != nil {
		return nil, err
	}
	defer func() { _ = logResp.Body.Close() }()
	raw, err := io.ReadAll(logResp.Body)
	if err != nil {
		return nil, err
	}
	if logResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("job log blob: %s", logResp.Status)
	}
	return nativeLogToGHFormat(raw, failedOnly), nil
}

// nativeLogToGHFormat converts the REST single-job log endpoint's raw body
// (timestamped lines only — no job/step columns) into gh CLI's tab-delimited
// shape. Step boundaries are inferred from Actions' own "##[group]"/
// "##[endgroup]" log-folding markers (the standard per-step wrapper every
// step's output is wrapped in); a step counts as failed if its group contains
// a "##[error]" line. There is no REST field carrying per-step conclusion on
// this endpoint alone (that lives on GET .../actions/jobs/{id}, outside this
// task's endpoint contract) — this is a text heuristic, not an authoritative
// signal. If failedOnly is requested but no step matched the heuristic (e.g.
// the failure surfaced without a "##[error]" line), every line is kept rather
// than silently returning an empty log.
func nativeLogToGHFormat(raw []byte, failedOnly bool) []byte {
	type entry struct{ step, line string }

	var lines []string
	if trimmed := strings.TrimRight(string(raw), "\n"); trimmed != "" {
		lines = strings.Split(trimmed, "\n")
	}

	var entries []entry
	failed := map[string]bool{}
	step := "(job)"
	for _, line := range lines {
		if line == "" {
			continue
		}
		if _, after, ok := strings.Cut(line, "##[group]"); ok {
			step = strings.TrimSpace(after)
		}
		if strings.Contains(line, "##[error]") {
			failed[step] = true
		}
		entries = append(entries, entry{step: step, line: line})
	}

	anyFailed := len(failed) > 0
	var b strings.Builder
	for _, e := range entries {
		if failedOnly && anyFailed && !failed[e.step] {
			continue
		}
		b.WriteString("\t")
		b.WriteString(e.step)
		b.WriteString("\t")
		b.WriteString(e.line)
		b.WriteString("\n")
	}
	return []byte(b.String())
}
