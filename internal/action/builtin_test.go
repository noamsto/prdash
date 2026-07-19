package action

import (
	"reflect"
	"testing"
)

type seqRunner struct {
	calls [][]string
	outs  [][]byte
	i     int
}

func (r *seqRunner) Run(_ string, args ...string) ([]byte, error) {
	r.calls = append(r.calls, args)
	o := r.outs[r.i]
	r.i++
	return o, nil
}

type argRunner struct{ args []string }

func (r *argRunner) Run(_ string, args ...string) ([]byte, error) {
	r.args = args
	return []byte("log-bytes"), nil
}

func TestRerunCheck(t *testing.T) {
	r := &seqRunner{outs: [][]byte{[]byte(``)}}
	if err := RerunCheck(r, "/repo", "83658069205"); err != nil {
		t.Fatal(err)
	}
	got := r.calls[0]
	want := []string{"run", "rerun", "--job", "83658069205"}
	if len(got) != len(want) {
		t.Fatalf("argv = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("argv[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestRerunFailedRerunsFailedSibling(t *testing.T) {
	// One push fans out into sibling runs sharing a head SHA; the latest-listed
	// one passed while another failed. The failed one must be rerun, not the
	// arbitrary latest sibling.
	r := &seqRunner{outs: [][]byte{
		[]byte(`[
			{"databaseId":100,"conclusion":"success","headSha":"abc123"},
			{"databaseId":200,"conclusion":"failure","headSha":"abc123"}
		]`),
		[]byte(``), // gh run rerun
	}}
	if err := RerunFailed(r, "/repo", "feat/x"); err != nil {
		t.Fatal(err)
	}
	if r.calls[0][0] != "run" || r.calls[0][1] != "list" {
		t.Fatalf("first call not run list: %v", r.calls[0])
	}
	if len(r.calls) != 2 {
		t.Fatalf("expected 1 rerun, got calls: %v", r.calls[1:])
	}
	last := r.calls[1]
	if last[0] != "run" || last[1] != "rerun" || last[2] != "200" || last[3] != "--failed" {
		t.Fatalf("rerun call wrong: %v", last)
	}
}

func TestRerunFailedScopesToHeadSHA(t *testing.T) {
	// A failed run from an earlier push (older SHA) must not be swept in.
	r := &seqRunner{outs: [][]byte{
		[]byte(`[
			{"databaseId":100,"conclusion":"failure","headSha":"newsha"},
			{"databaseId":200,"conclusion":"failure","headSha":"oldsha"}
		]`),
		[]byte(``),
	}}
	if err := RerunFailed(r, "/repo", "feat/x"); err != nil {
		t.Fatal(err)
	}
	if len(r.calls) != 2 {
		t.Fatalf("expected exactly 1 rerun (head SHA only), got calls: %v", r.calls[1:])
	}
	if r.calls[1][2] != "100" {
		t.Fatalf("reran wrong run: %v", r.calls[1])
	}
}

func TestRerunFailedNoFailures(t *testing.T) {
	r := &seqRunner{outs: [][]byte{
		[]byte(`[{"databaseId":100,"conclusion":"success","headSha":"abc123"}]`),
	}}
	if err := RerunFailed(r, "/repo", "feat/x"); err == nil {
		t.Fatal("expected error when no runs failed")
	}
	if len(r.calls) != 1 {
		t.Fatalf("expected only the list call, got: %v", r.calls)
	}
}

func TestJobLogArgs(t *testing.T) {
	r := &argRunner{}
	out, err := JobLog(r, "/repo", "123", true)
	if err != nil {
		t.Fatalf("JobLog: %v", err)
	}
	if string(out) != "log-bytes" {
		t.Fatalf("out = %q", out)
	}
	want := []string{"run", "view", "--job", "123", "--log-failed"}
	if !reflect.DeepEqual(r.args, want) {
		t.Fatalf("failedOnly args = %v, want %v", r.args, want)
	}

	r2 := &argRunner{}
	_, _ = JobLog(r2, "/repo", "123", false)
	wantAll := []string{"run", "view", "--job", "123", "--log"}
	if !reflect.DeepEqual(r2.args, wantAll) {
		t.Fatalf("full args = %v, want %v", r2.args, wantAll)
	}
}
