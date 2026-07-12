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

func TestMochaPaletteAccentsAreDistinct(t *testing.T) {
	th := Mocha()
	if th.Accent != "#94e2d5" {
		t.Errorf("accent = %q, want teal #94e2d5", th.Accent)
	}
	if th.Header != "#cba6f7" {
		t.Errorf("header = %q, want mauve #cba6f7 (the app identity)", th.Header)
	}
	// The repo wordmark (Header) and the PR accent must not match, or the header
	// blurs into the active PRs tab — the whole reason PRs moved off mauve.
	if th.Header == th.Accent {
		t.Errorf("header and PR accent must differ: header=%q accent=%q", th.Header, th.Accent)
	}
	if th.Accent == th.Issue {
		t.Errorf("the two board accents must differ: pr=%q issue=%q", th.Accent, th.Issue)
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

func TestLattePaletteIsLight(t *testing.T) {
	l := Latte()
	if l.Accent != "#179299" {
		t.Errorf("latte accent = %q, want teal #179299", l.Accent)
	}
	if l.Base != "#eff1f5" {
		t.Errorf("latte base = %q, want light #eff1f5", l.Base)
	}
	if l.Text == Mocha().Text {
		t.Error("latte text must differ from mocha text")
	}
	if len(l.Author) != len(Mocha().Author) {
		t.Errorf("latte author rotation len = %d, want %d", len(l.Author), len(Mocha().Author))
	}
}

func TestThemeFor(t *testing.T) {
	if themeFor("light").Accent != Latte().Accent {
		t.Error(`themeFor("light") should be Latte`)
	}
	if themeFor("dark").Accent != Mocha().Accent {
		t.Error(`themeFor("dark") should be Mocha`)
	}
	if themeFor("").Accent != Mocha().Accent {
		t.Error(`themeFor("") should default to Mocha`)
	}
}

func TestApplyThemeReassignsGlobals(t *testing.T) {
	t.Cleanup(func() { applyTheme(Mocha()) })
	applyTheme(Latte())
	if theme.Accent != Latte().Accent {
		t.Errorf("applyTheme did not swap the active palette: %q", theme.Accent)
	}
	latteRender := accentStyle.Render("x")
	applyTheme(Mocha())
	if latteRender == accentStyle.Render("x") {
		t.Error("accentStyle must render differently under Latte vs Mocha")
	}
}
