package action

import (
	"fmt"
	"strconv"

	"github.com/noamsto/prdash/internal/gh"
)

// RerunCheck reruns a single Actions job (one check) by its job ID.
func RerunCheck(native gh.ActionsSource, jobID string) error {
	id, err := strconv.ParseInt(jobID, 10, 64)
	if err != nil {
		return fmt.Errorf("rerun check: bad job ID %q: %w", jobID, err)
	}
	return native.RerunJob(id)
}

// RerunFailed reruns every failed sibling run at the branch's current head SHA.
func RerunFailed(native gh.ActionsSource, branch string) error {
	runs, err := native.ListRunsForBranch(branch)
	if err != nil {
		return err
	}
	if len(runs) == 0 {
		return fmt.Errorf("no runs for branch %s", branch)
	}
	// One push fans out into a run per top-level workflow, all on the same head
	// SHA; the latest is an arbitrary sibling that may have passed. Scope to the
	// head SHA (runs come newest first) so a stale earlier-push failure isn't
	// swept in, and rerun every failed sibling.
	head := runs[0].HeadSHA
	var reran bool
	for _, run := range runs {
		if run.HeadSHA != head || run.Conclusion != "failure" {
			continue
		}
		if err := native.RerunFailedJobs(run.ID); err != nil {
			return err
		}
		reran = true
	}
	if !reran {
		return fmt.Errorf("no failed runs for branch %s at %.7s", branch, head)
	}
	return nil
}

// JobLog fetches one Actions job's log. failedOnly limits it to failed steps;
// otherwise the whole job log.
func JobLog(native gh.ActionsSource, jobID string, failedOnly bool) ([]byte, error) {
	id, err := strconv.ParseInt(jobID, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("job log: bad job ID %q: %w", jobID, err)
	}
	return native.JobLog(id, failedOnly)
}
