package preview

import (
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestRenderInlineCodeAndTable(t *testing.T) {
	out, err := Render("Use `go test`.\n\n| a | b |\n|---|---|\n| 1 | 2 |\n", 80)
	if err != nil {
		t.Fatal(err)
	}
	// table content must survive (no pipe-strip), and inline code present.
	if !strings.Contains(out, "go test") {
		t.Fatalf("inline code missing: %q", out)
	}
	if !strings.Contains(out, "1") || !strings.Contains(out, "2") {
		t.Fatalf("table content stripped: %q", out)
	}
}

func TestSetModeChangesOutputAndFlushes(t *testing.T) {
	t.Cleanup(func() { SetMode("dark") })
	const md = "# Hello\n\nsome **bold** text"

	SetMode("dark")
	dark, err := Render(md, 60)
	if err != nil {
		t.Fatal(err)
	}
	before := renderMisses

	SetMode("light")
	light, err := Render(md, 60)
	if err != nil {
		t.Fatal(err)
	}
	if dark == light {
		t.Error("light and dark render of the same markdown must differ")
	}
	if renderMisses != before+1 {
		t.Errorf("SetMode should flush caches so Render misses once: misses=%d want=%d",
			renderMisses, before+1)
	}
}

func TestRenderDropsHeadingMarkers(t *testing.T) {
	out, err := Render("### Advisory lost after commit retry\n", 80)
	if err != nil {
		t.Fatal(err)
	}
	plain := ansi.Strip(out)
	if strings.Contains(plain, "###") {
		t.Errorf("heading marker leaked into output: %q", plain)
	}
	if !strings.Contains(plain, "Advisory lost after commit retry") {
		t.Errorf("heading text missing: %q", plain)
	}
}

var wsCollapse = regexp.MustCompile(`\s+`)

func TestRenderSuppressesBadgeAndURLNoise(t *testing.T) {
	const md = "**<sub><sub>![P1 Badge](https://img.shields.io/badge/P1-orange?style=flat)</sub></sub>  Handle nulls**\n\n" +
		"See [the schema](https://github.com/x/y/blob/main/a.sql) for detail.\n"
	out, err := Render(md, 72)
	if err != nil {
		t.Fatal(err)
	}
	// Collapse whitespace: glamour word-wraps, and a long URL split across
	// lines would otherwise slip past a plain substring check.
	painted := wsCollapse.ReplaceAllString(ansi.Strip(out), "")
	for _, bad := range []string{"img.shields.io", "Image:", "P1Badge", "github.com/x/y"} {
		if strings.Contains(painted, bad) {
			t.Errorf("%q is painted but should be suppressed: %q", bad, painted)
		}
	}
	if !strings.Contains(painted, "Handlenulls") {
		t.Errorf("title text missing: %q", painted)
	}
	if !strings.Contains(painted, "theschema") {
		t.Errorf("link label missing: %q", painted)
	}
	// The link must remain clickable: its OSC 8 wrapper survives even though
	// the URL is no longer painted.
	if !strings.Contains(out, "\x1b]8;") {
		t.Error("link lost its OSC 8 hyperlink; it is no longer clickable")
	}
}

func TestRenderTableLinkHasNoFootnote(t *testing.T) {
	const md = "| ref | note |\n|---|---|\n| [the schema](https://github.com/x/y/blob/main/a.sql) | check this |\n"
	out, err := Render(md, 72)
	if err != nil {
		t.Fatal(err)
	}
	painted := ansi.Strip(out)
	if strings.Contains(painted, "[1]") {
		t.Errorf("table link left a footnote marker: %q", painted)
	}
	if !strings.Contains(painted, "the schema") {
		t.Errorf("link label missing: %q", painted)
	}
	if !strings.Contains(painted, "check this") {
		t.Errorf("table content missing: %q", painted)
	}
	if !strings.Contains(out, "\x1b]8;") {
		t.Error("table link lost its OSC 8 hyperlink; it is no longer clickable")
	}
}
