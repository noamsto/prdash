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

func TestMochaPaletteIsMauveLed(t *testing.T) {
	th := Mocha()
	if th.Accent != "#cba6f7" {
		t.Errorf("accent = %q, want mauve #cba6f7", th.Accent)
	}
	if th.Header != th.Accent {
		t.Errorf("header should be mauve-led like accent: header=%q accent=%q", th.Header, th.Accent)
	}
	if th.Focus == th.Accent || th.Select == th.Accent || th.Focus == th.Select {
		t.Errorf("focus, select, accent must all be distinct: accent=%q focus=%q select=%q",
			th.Accent, th.Focus, th.Select)
	}
}

func TestAuthorPaletteExcludesAccent(t *testing.T) {
	for _, c := range Mocha().Author {
		if c == Mocha().Accent {
			t.Fatalf("author hue %q collides with the accent mauve", c)
		}
	}
}

func TestDraftColorStaysOutOfAuthorPalette(t *testing.T) {
	th := Mocha()
	for _, c := range th.Author {
		if c == th.Draft {
			t.Fatalf("draft tag color %q must stay out of the author rotation", th.Draft)
		}
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
	if !lightText("zzz") { // unparsable → safe default
		t.Error("invalid hex should default to light text")
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
