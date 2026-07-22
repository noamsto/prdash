package ui

import (
	"testing"

	"github.com/noamsto/prdash/internal/action"
	"github.com/noamsto/prdash/internal/gh"
)

// fakeActionsSource mirrors fakeMutationSource (mutationsource_test.go) for
// the Actions rerun/job-log seam: it records calls instead of hitting
// GitHub.
type fakeActionsSource struct {
	runs             []gh.WorkflowRun
	rerunFailedCalls []int64
	jobLogCalls      []struct {
		jobID      int64
		failedOnly bool
	}
	jobLogOut []byte
}

func (f *fakeActionsSource) ListRunsForBranch(string) ([]gh.WorkflowRun, error) { return f.runs, nil }

func (f *fakeActionsSource) RerunFailedJobs(runID int64) error {
	f.rerunFailedCalls = append(f.rerunFailedCalls, runID)
	return nil
}

func (f *fakeActionsSource) RerunJob(int64) error { return nil }

func (f *fakeActionsSource) JobLog(jobID int64, failedOnly bool) ([]byte, error) {
	f.jobLogCalls = append(f.jobLogCalls, struct {
		jobID      int64
		failedOnly bool
	}{jobID, failedOnly})
	return f.jobLogOut, nil
}

func TestRerunFailedRoutesToNativeSource(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.SetRepo("owner/repo")
	cr := &countingRunner{}
	m.SetRunner(cr)
	m.setPRs([]gh.PR{{Number: 7, HeadRefName: "feat/x"}})
	fs := &fakeActionsSource{runs: []gh.WorkflowRun{{ID: 200, Conclusion: "failure", HeadSHA: "abc"}}}
	m.SetActionsSource(fs)

	msg := driveBulk(t, m.runAction(action.DefaultPRActions()["r"]))
	if done, ok := msg.(actionDoneMsg); !ok || done.err != nil {
		t.Fatalf("msg = %+v, want a successful actionDoneMsg", msg)
	}
	if cr.calls != 0 {
		t.Errorf("native path must not touch the gh-CLI runner, got %d calls", cr.calls)
	}
	if len(fs.rerunFailedCalls) != 1 || fs.rerunFailedCalls[0] != 200 {
		t.Errorf("rerunFailedCalls = %v, want [200]", fs.rerunFailedCalls)
	}
}

func TestRerunFailedGateOffUsesRunner(t *testing.T) {
	// No SetActionsSource call: actionsSource stays nil, so the gh-CLI path
	// must still be taken untouched.
	m := NewModel("/repo", "is:open", nil)
	m.SetRepo("owner/repo")
	cr := &countingRunner{}
	m.SetRunner(cr)
	m.setPRs([]gh.PR{{Number: 7, HeadRefName: "feat/x"}})

	driveBulk(t, m.runAction(action.DefaultPRActions()["r"]))
	if cr.calls == 0 {
		t.Error("gate-off path should still shell out via the Runner")
	}
}

func TestFetchJobLogCmdRoutesToNativeSource(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	cr := &countingRunner{}
	m.SetRunner(cr)
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
	if cr.calls != 0 {
		t.Errorf("native path must not touch the gh-CLI runner, got %d calls", cr.calls)
	}
	if want := []struct {
		jobID      int64
		failedOnly bool
	}{{123, true}}; len(fs.jobLogCalls) != 1 || fs.jobLogCalls[0] != want[0] {
		t.Errorf("jobLogCalls = %+v, want %+v", fs.jobLogCalls, want)
	}
}

func TestFetchJobLogCmdGateOffUsesRunner(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	cr := &countingRunner{}
	m.SetRunner(cr)

	cmd := m.fetchJobLogCmd("123", false)
	_ = cmd()
	if cr.calls == 0 {
		t.Error("gate-off path should still shell out via the Runner")
	}
}
