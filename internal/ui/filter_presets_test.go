package ui

import "testing"

func TestFilterPresetCycleWraps(t *testing.T) {
	p := defaultPresets
	if p[0].name != "mine" {
		t.Fatalf("first preset should be mine, got %q", p[0].name)
	}
	for i := range p {
		want := (i + 1) % len(p)
		if got := nextPreset(i); got != want {
			t.Fatalf("nextPreset(%d) = %d, want %d", i, got, want)
		}
	}
}
