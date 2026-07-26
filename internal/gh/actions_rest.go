package gh

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
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
		if i := strings.Index(line, "##[group]"); i >= 0 {
			step = strings.TrimSpace(line[i+len("##[group]"):])
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
