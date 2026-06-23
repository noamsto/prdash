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
