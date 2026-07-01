package ui

import (
	"strings"
	"testing"
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
