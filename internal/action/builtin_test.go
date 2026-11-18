package action

import (
	"reflect"
	"testing"

	"github.com/noamsto/prdash/internal/gh"
)

// fakeActionsSource is the native REST backend fake for the action-package
// seam tests: it records calls instead of hitting GitHub, mirroring
// internal/ui's fakeMutationSource convention.
type fakeActionsSource struct {
	runs             []gh.WorkflowRun
	listErr          error
	rerunFailedCalls []int64
	rerunFailedErr   error
	rerunJobCalls    []int64
	jobLogCalls      []jobLogCall
	jobLogOut        []byte
	jobLogErr        error
	repos            []string // the repo every call received, in order
}

type jobLogCall struct {
	jobID      int64
	failedOnly bool
}

func (f *fakeActionsSource) ListRunsForBranch(repo, _ string) ([]gh.WorkflowRun, error) {
	f.repos = append(f.repos, repo)
	return f.runs, f.listErr
}

func (f *fakeActionsSource) RerunFailedJobs(repo string, runID int64) error {
	f.repos = append(f.repos, repo)
	f.rerunFailedCalls = append(f.rerunFailedCalls, runID)
	return f.rerunFailedErr
}

func (f *fakeActionsSource) RerunJob(repo string, jobID int64) error {
	f.repos = append(f.repos, repo)
	f.rerunJobCalls = append(f.rerunJobCalls, jobID)
	return nil
}

func (f *fakeActionsSource) JobLog(repo string, jobID int64, failedOnly bool) ([]byte, error) {
	f.repos = append(f.repos, repo)
	f.jobLogCalls = append(f.jobLogCalls, jobLogCall{jobID, failedOnly})
	return f.jobLogOut, f.jobLogErr
}

func TestRerunCheckRoutesToNativeJob(t *testing.T) {
	native := &fakeActionsSource{}
	if err := RerunCheck(native, "o/r", "83658069205"); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(native.rerunJobCalls, []int64{83658069205}) {
		t.Fatalf("rerunJobCalls = %v, want [83658069205]", native.rerunJobCalls)
	}
	if !reflect.DeepEqual(native.repos, []string{"o/r"}) {
		t.Fatalf("repos = %v, want [o/r]", native.repos)
	}
}

func TestRerunFailedRerunsFailedSibling(t *testing.T) {
	// One push fans out into sibling runs sharing a head SHA; the latest-listed
	// one passed while another failed. The failed one must be rerun, not the
	// arbitrary latest sibling.
	native := &fakeActionsSource{runs: []gh.WorkflowRun{
		{ID: 100, Conclusion: "success", HeadSHA: "abc123"},
		{ID: 200, Conclusion: "failure", HeadSHA: "abc123"},
	}}
	if err := RerunFailed(native, "o/r", "feat/x"); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(native.rerunFailedCalls, []int64{200}) {
		t.Fatalf("rerunFailedCalls = %v, want [200]", native.rerunFailedCalls)
	}
	if !reflect.DeepEqual(native.repos, []string{"o/r", "o/r"}) {
		t.Fatalf("repos = %v, want list and rerun both on o/r", native.repos)
	}
}

func TestRerunFailedScopesToHeadSHA(t *testing.T) {
	// A failed run from an earlier push (older SHA) must not be swept in.
	native := &fakeActionsSource{runs: []gh.WorkflowRun{
		{ID: 100, Conclusion: "failure", HeadSHA: "newsha"},
		{ID: 200, Conclusion: "failure", HeadSHA: "oldsha"},
	}}
	if err := RerunFailed(native, "o/r", "feat/x"); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(native.rerunFailedCalls, []int64{100}) {
		t.Fatalf("rerunFailedCalls = %v, want [100] (head SHA only)", native.rerunFailedCalls)
	}
}

func TestRerunFailedNoFailures(t *testing.T) {
	native := &fakeActionsSource{runs: []gh.WorkflowRun{{ID: 100, Conclusion: "success", HeadSHA: "abc123"}}}
	if err := RerunFailed(native, "o/r", "feat/x"); err == nil {
		t.Fatal("expected error when no runs failed")
	}
	if len(native.rerunFailedCalls) != 0 {
		t.Fatalf("nothing should have been rerun, got %v", native.rerunFailedCalls)
	}
}

func TestJobLog(t *testing.T) {
	native := &fakeActionsSource{jobLogOut: []byte("native-log-bytes")}
	out, err := JobLog(native, "o/r", "123", true)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "native-log-bytes" {
		t.Fatalf("out = %q, want native-log-bytes", out)
	}
	if want := []jobLogCall{{123, true}}; !reflect.DeepEqual(native.jobLogCalls, want) {
		t.Fatalf("jobLogCalls = %+v, want %+v", native.jobLogCalls, want)
	}
	if !reflect.DeepEqual(native.repos, []string{"o/r"}) {
		t.Fatalf("repos = %v, want [o/r]", native.repos)
	}

	native2 := &fakeActionsSource{}
	if _, err := JobLog(native2, "o/r", "123", false); err != nil {
		t.Fatal(err)
	}
	if want := []jobLogCall{{123, false}}; !reflect.DeepEqual(native2.jobLogCalls, want) {
		t.Fatalf("full-log jobLogCalls = %+v, want %+v", native2.jobLogCalls, want)
	}
}
