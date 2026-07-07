package ui

import (
	"os"
	"path/filepath"
	"testing"
)

// writeState points XDG_STATE_HOME at a temp dir and writes theme-state.json.
// An empty body writes no file (simulating a missing state file).
func writeState(t *testing.T, body string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)
	if body != "" {
		if err := os.WriteFile(filepath.Join(dir, "theme-state.json"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestDetectTheme(t *testing.T) {
	cases := []struct{ name, body, want string }{
		{"missing file", "", "dark"},
		{"malformed", "{not json", "dark"},
		{"empty theme", `{"theme":""}`, "dark"},
		{"light", `{"theme":"light","version":1}`, "light"},
		{"dark", `{"theme":"dark","version":1}`, "dark"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			writeState(t, tc.body)
			if got := detectTheme(); got != tc.want {
				t.Errorf("detectTheme() = %q, want %q", got, tc.want)
			}
		})
	}
}
