package action

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestOSC52(t *testing.T) {
	seq := OSC52("feat/x")
	want := base64.StdEncoding.EncodeToString([]byte("feat/x"))
	if !strings.Contains(seq, want) {
		t.Fatalf("osc52 %q missing base64 %q", seq, want)
	}
	if !strings.HasPrefix(seq, "\x1b]52;c;") || !strings.HasSuffix(seq, "\x07") {
		t.Fatalf("osc52 envelope wrong: %q", seq)
	}
}

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
