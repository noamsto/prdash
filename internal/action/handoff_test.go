package action

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAppendHandoff(t *testing.T) {
	p := filepath.Join(t.TempDir(), "actions")
	if err := AppendHandoff(p, "enter", []string{"wt", "switch", "pr:7"}); err != nil {
		t.Fatal(err)
	}
	if err := AppendHandoff(p, "enter", []string{"wt", "switch", "pr:9"}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(lines) != 2 {
		t.Fatalf("want 2 lines, got %d: %q", len(lines), b)
	}
	if !strings.HasPrefix(lines[0], "enter\t[") {
		t.Fatalf("line format: %q", lines[0])
	}
}

func TestAppendHandoffRemote(t *testing.T) {
	p := filepath.Join(t.TempDir(), "actions")
	e := HandoffEntry{Repo: "o/n", Kind: "pr", Target: "feat/x", PR: 4, Args: []string{"switch", "-c", "feat/x"}, Key: "enter"}
	if err := AppendHandoffRemote(p, e); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	want := `enter` + "\t" + `{"repo":"o/n","kind":"pr","target":"feat/x","pr":4,"args":["switch","-c","feat/x"],"key":"enter"}` + "\n"
	if string(b) != want {
		t.Fatalf("got %q, want %q", b, want)
	}
}
