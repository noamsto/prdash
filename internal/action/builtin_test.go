package action

import (
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

func TestRerunFailedResolvesRunID(t *testing.T) {
	r := &seqRunner{outs: [][]byte{
		[]byte(`[{"databaseId":555}]`), // gh run list
		[]byte(``),                     // gh run rerun
	}}
	err := RerunFailed(r, "/repo", "feat/x")
	if err != nil {
		t.Fatal(err)
	}
	if r.calls[0][0] != "run" || r.calls[0][1] != "list" {
		t.Fatalf("first call not run list: %v", r.calls[0])
	}
	last := r.calls[1]
	if last[0] != "run" || last[1] != "rerun" || last[2] != "555" {
		t.Fatalf("rerun call wrong: %v", last)
	}
}
