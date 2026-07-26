package gh

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/google/go-github/v89/github"
)

// repoParts splits s.repo ("owner/name") for the go-github calls, which take
// owner and repo separately.
func (s GraphSource) repoParts() (owner, name string, err error) {
	owner, name, ok := strings.Cut(s.repo, "/")
	if !ok {
		return "", "", fmt.Errorf("bad repo %q", s.repo)
	}
	return owner, name, nil
}

// actionsCtx bounds one Actions call by graphTimeout. GetWorkflowJobLogs
// reaches Transport.RoundTrip directly and so never sees http.Client.Timeout —
// the deadline has to ride on the context or that hop can hang the log view
// forever.
func actionsCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), graphTimeout)
}

// ListRunsForBranch lists the 20 most recent workflow runs for branch,
// newest-first, replacing
// `gh run list --branch <b> -L 20 --json databaseId,conclusion,headSha`.
func (s GraphSource) ListRunsForBranch(branch string) ([]WorkflowRun, error) {
	owner, name, err := s.repoParts()
	if err != nil {
		return nil, err
	}
	ctx, cancel := actionsCtx()
	defer cancel()
	list, _, err := s.actions.Actions.ListRepositoryWorkflowRuns(ctx, owner, name, &github.ListWorkflowRunsOptions{
		Branch:      branch,
		ListOptions: github.ListOptions{PerPage: 20},
	})
	if err != nil {
		return nil, fmt.Errorf("list runs: %w", err)
	}
	runs := make([]WorkflowRun, len(list.WorkflowRuns))
	for i, r := range list.WorkflowRuns {
		runs[i] = WorkflowRun{ID: r.GetID(), Conclusion: r.GetConclusion(), HeadSHA: r.GetHeadSHA()}
	}
	return runs, nil
}

// RerunFailedJobs reruns the failed jobs of runID, replacing
// `gh run rerun <id> --failed`.
func (s GraphSource) RerunFailedJobs(runID int64) error {
	owner, name, err := s.repoParts()
	if err != nil {
		return err
	}
	ctx, cancel := actionsCtx()
	defer cancel()
	_, err = s.actions.Actions.RerunFailedJobsByID(ctx, owner, name, runID)
	return err
}

// RerunJob reruns a single job and its dependent jobs, replacing
// `gh run rerun --job <id>`.
func (s GraphSource) RerunJob(jobID int64) error {
	owner, name, err := s.repoParts()
	if err != nil {
		return err
	}
	ctx, cancel := actionsCtx()
	defer cancel()
	_, err = s.actions.Actions.RerunJobByID(ctx, owner, name, jobID)
	return err
}

// JobLog fetches jobID's plain-text log and converts it into the tab-delimited
// "job\tstep\ttimestamp content" shape `gh run view --log[-failed]` emits, so
// internal/ui's parseJobLog (which expects that shape) consumes either source
// unchanged. failedOnly filters the result to the failed step(s) client-side —
// GitHub's REST API has no server-side "failed only" log filter, unlike gh's
// --log-failed.
//
// The endpoint answers with a short-lived (~1 minute) redirect to blob storage
// rather than the body directly. GetWorkflowJobLogs with maxRedirects 0 issues
// exactly one round trip and hands back the parsed Location without following
// it, which is what keeps the token safe: s.actions' transport is oauth2-wrapped
// and re-injects Authorization on every RoundTrip, so letting it follow the hop
// to blob storage would leak the GitHub token off github.com. The blob fetch
// below is therefore a separate, unauthenticated request — that URL is already a
// fully-signed link, and attaching a token both leaks it needlessly and can trip
// a signature-mismatch rejection from the storage backend.
func (s GraphSource) JobLog(jobID int64, failedOnly bool) ([]byte, error) {
	owner, name, err := s.repoParts()
	if err != nil {
		return nil, err
	}
	ctx, cancel := actionsCtx()
	defer cancel()
	blobURL, _, err := s.actions.Actions.GetWorkflowJobLogs(ctx, owner, name, jobID, 0)
	if err != nil {
		return nil, fmt.Errorf("job log: %w", err)
	}

	// Still needs its own bound: this hop doesn't reuse s.http, so it doesn't
	// inherit its timeout either.
	logResp, err := (&http.Client{Timeout: graphTimeout}).Get(blobURL.String())
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
