package ui

import (
	"strings"
	"testing"

	"github.com/noamsto/prdash/internal/gh"
)

func TestCIGlyph(t *testing.T) {
	cases := map[string]string{"pass": "✓", "fail": "✗", "pending": "●", "none": "·"}
	for state, want := range cases {
		if got := ciGlyph(state); !strings.Contains(got, want) {
			t.Errorf("ciGlyph(%q) = %q, want it to contain %q", state, got, want)
		}
	}
}

func TestCIGlyphUnknownIsNone(t *testing.T) {
	if !strings.Contains(ciGlyph("whatever"), "·") {
		t.Errorf("unknown CI state should fall back to the none glyph")
	}
}

func TestLightText(t *testing.T) {
	if !lightText("000000") {
		t.Error("black bg should use light text")
	}
	if lightText("ffffff") {
		t.Error("white bg should use dark text")
	}
	if !lightText("d73a4a") { // GitHub red — medium-dark
		t.Error("medium-dark bg should use light text")
	}
}

func TestRenderChipsOverflow(t *testing.T) {
	labels := []gh.Label{{Name: "bug", Color: "d73a4a"}, {Name: "enhancement", Color: "a2eeef"}}
	out := renderChips(labels, 6) // only room for one chip
	if !strings.Contains(out, "+1") {
		t.Errorf("expected +1 overflow marker, got %q", out)
	}
}

func TestRenderChipsFitsAll(t *testing.T) {
	labels := []gh.Label{{Name: "bug", Color: "d73a4a"}}
	out := renderChips(labels, 40)
	if strings.Contains(out, "+") {
		t.Errorf("no overflow expected, got %q", out)
	}
	if !strings.Contains(out, "bug") {
		t.Errorf("chip text missing, got %q", out)
	}
}
