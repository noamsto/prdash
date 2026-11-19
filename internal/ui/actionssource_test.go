package ui

import (
	"slices"
	"testing"

	"github.com/noamsto/prdash/internal/action"
	"github.com/noamsto/prdash/internal/gh"
)

// fakeActionsSource mirrors fakeMutationSource (mutationsource_test.go) for
// the Actions rerun/job-log seam: it records calls instead of hitting
// GitHub.
type fakeActionsSource struct {
	repos            []string // repo passed to each call, in order
	branches         []string // branch passed to each ListRunsForBranch call
	runs             []gh.WorkflowRun
	rerunFailedCalls []int64
	jobLogCalls      []struct {
		jobID      int64
		failedOnly bool
	}
	jobLogOut []byte
}

func (f *fakeActionsSource) ListRunsForBranch(repo, branch string) ([]gh.WorkflowRun, error) {
	f.repos = append(f.repos, repo)
	f.branches = append(f.branches, branch)
	return f.runs, nil
}

func (f *fakeActionsSource) RerunFailedJobs(repo string, runID int64) error {
	f.repos = append(f.repos, repo)
	f.rerunFailedCalls = append(f.rerunFailedCalls, runID)
	return nil
}

func (f *fakeActionsSource) RerunJob(repo string, _ int64) error {
	f.repos = append(f.repos, repo)
	return nil
}

func (f *fakeActionsSource) JobLog(repo string, jobID int64, failedOnly bool) ([]byte, error) {
	f.repos = append(f.repos, repo)
	f.jobLogCalls = append(f.jobLogCalls, struct {
		jobID      int64
		failedOnly bool
	}{jobID, failedOnly})
	return f.jobLogOut, nil
}

func TestRerunFailedRoutesToNativeSource(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.SetRepo("owner/repo")
	m.setPRs([]gh.PR{{Number: 7, HeadRefName: "feat/x"}})
	fs := &fakeActionsSource{runs: []gh.WorkflowRun{{ID: 200, Conclusion: "failure", HeadSHA: "abc"}}}
	m.SetActionsSource(fs)

	msg := driveBulk(t, m.runAction(action.DefaultPRActions()["r"]))
	if done, ok := msg.(actionDoneMsg); !ok || done.err != nil {
		t.Fatalf("msg = %+v, want a successful actionDoneMsg", msg)
	}
	if len(fs.rerunFailedCalls) != 1 || fs.rerunFailedCalls[0] != 200 {
		t.Errorf("rerunFailedCalls = %v, want [200]", fs.rerunFailedCalls)
	}
	if !slices.Equal(fs.repos, []string{"owner/repo", "owner/repo"}) {
		t.Errorf("repos = %v, want list and rerun both on owner/repo", fs.repos)
	}
}

func TestFetchJobLogCmdRoutesToNativeSource(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	fs := &fakeActionsSource{jobLogOut: []byte("native-log-bytes")}
	m.SetActionsSource(fs)

	cmd := m.fetchJobLogCmd("123", false)
	msg := cmd()
	got, ok := msg.(logFetchedMsg)
	if !ok {
		t.Fatalf("msg = %T, want logFetchedMsg", msg)
	}
	if got.err != nil || string(got.raw) != "native-log-bytes" {
		t.Fatalf("logFetchedMsg = %+v, want native-log-bytes", got)
	}
	if want := []struct {
		jobID      int64
		failedOnly bool
	}{{123, true}}; len(fs.jobLogCalls) != 1 || fs.jobLogCalls[0] != want[0] {
		t.Errorf("jobLogCalls = %+v, want %+v", fs.jobLogCalls, want)
	}
}
