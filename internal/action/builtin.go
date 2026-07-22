package action

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/noamsto/prdash/internal/gh"
)

// RerunCheck reruns a single Actions job (one check) by its job ID.
func RerunCheck(r gh.Runner, dir, jobID string) error {
	_, err := r.Run(dir, "run", "rerun", "--job", jobID)
	return err
}

// RerunFailed reruns the failed sibling runs at the branch's current head
// SHA. native is the REST backend installed under PRDASH_GH_GRAPHQL (see
// main.go); nil keeps the gh-CLI path below untouched.
func RerunFailed(r gh.Runner, dir, branch string, native gh.ActionsSource) error {
	if native != nil {
		return rerunFailedNative(native, branch)
	}
	out, err := r.Run(dir, "run", "list", "--branch", branch, "-L", "20",
		"--json", "databaseId,conclusion,headSha")
	if err != nil {
		return err
	}
	var runs []struct {
		DatabaseID int    `json:"databaseId"`
		Conclusion string `json:"conclusion"`
		HeadSha    string `json:"headSha"`
	}
	if err := json.Unmarshal(out, &runs); err != nil {
		return err
	}
	if len(runs) == 0 {
		return fmt.Errorf("no runs for branch %s", branch)
	}
	// One push fans out into a run per top-level workflow, all on the same head
	// SHA; the latest is an arbitrary sibling that may have passed (gh rejects
	// rerun --failed on it). Scope to the head SHA (gh lists newest first) so a
	// stale earlier-push failure isn't swept in, and rerun every failed sibling.
	head := runs[0].HeadSha
	var reran bool
	for _, run := range runs {
		if run.HeadSha != head || run.Conclusion != "failure" {
			continue
		}
		if _, err := r.Run(dir, "run", "rerun", strconv.Itoa(run.DatabaseID), "--failed"); err != nil {
			return err
		}
		reran = true
	}
	if !reran {
		return fmt.Errorf("no failed runs for branch %s at %.7s", branch, head)
	}
	return nil
}

// rerunFailedNative is RerunFailed's REST counterpart: same head-SHA-scoped
// failed-sibling selection, against gh.ActionsSource instead of gh.Runner.
func rerunFailedNative(native gh.ActionsSource, branch string) error {
	runs, err := native.ListRunsForBranch(branch)
	if err != nil {
		return err
	}
	if len(runs) == 0 {
		return fmt.Errorf("no runs for branch %s", branch)
	}
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

// JobLog fetches one Actions job's log. failedOnly limits it to failed steps
// (gh --log-failed); otherwise the whole job log. native is the REST backend
// installed under PRDASH_GH_GRAPHQL; nil keeps the gh-CLI path below untouched.
func JobLog(r gh.Runner, dir, jobID string, failedOnly bool, native gh.ActionsSource) ([]byte, error) {
	if native != nil {
		id, err := strconv.ParseInt(jobID, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("job log: bad job ID %q: %w", jobID, err)
		}
		return native.JobLog(id, failedOnly)
	}
	flag := "--log"
	if failedOnly {
		flag = "--log-failed"
	}
	return r.Run(dir, "run", "view", "--job", jobID, flag)
}
