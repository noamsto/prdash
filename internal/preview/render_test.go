package preview

import (
	"regexp"
	"strings"
	"testing"

	"charm.land/glamour/v2"
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

var glamourOwnSeq = regexp.MustCompile(`\x1b\[[0-9;:]*m|\x1b\]8;[^\x07\x1b]*\x07`)

// assertNoPayloadControls removes glamour's own SGR and OSC 8 sequences from
// the raw output, then fails on any control rune left. ansi.Strip is not used:
// it consumes a lone ESC and would hide the leak.
func assertNoPayloadControls(t *testing.T, out string) {
	t.Helper()
	rest := glamourOwnSeq.ReplaceAllString(out, "")
	for _, r := range rest {
		if r == '\n' || r == '\t' {
			continue
		}
		if r < 0x20 || r == 0x7F || (r >= 0x80 && r <= 0x9F) || r == '\uE000' {
			t.Fatalf("control rune %U left in output %q", r, out)
		}
	}
}

func TestRenderNeutralizesControlReferences(t *testing.T) {
	payloads := []string{
		"&#27;", "&#x1b;", "&#X1B;", "&#0027;", "&#27", "&#157;", "&#4294967323;",
		"&#&#27;27;", "~~&#*2*7;~~", "~~&#x*1*b;~~", "~~&`#`27;~~", "~~&[#](x)27;~~",
		"~~&#0*2*7;~~", "~~&\n#27;~~", "&amp;#27;",
		"&#27;]52;c;aGk=&#7;", "&#27;]8;;http://e&#7;x&#27;]8;;&#7;",
	}
	contexts := []struct{ name, format string }{
		{"paragraph", "before %s after"},
		{"link", "[%s](https://e.example/x)"},
		{"heading", "# %s"},
		{"codespan", "`%s`"},
	}
	for _, p := range payloads {
		for _, c := range contexts {
			t.Run(c.name+"/"+p, func(t *testing.T) {
				out, err := Render(strings.Replace(c.format, "%s", p, 1), 80)
				if err != nil {
					t.Fatal(err)
				}
				assertNoPayloadControls(t, out)
			})
		}
	}
}

func TestRenderShowsControlReferenceLiterally(t *testing.T) {
	for _, md := range []string{"&#27;[31m", "&#27;]8;;http://e&#7;"} {
		out, err := Render(md, 80)
		if err != nil {
			t.Fatal(err)
		}
		assertNoPayloadControls(t, out)
		if !strings.Contains(ansi.Strip(out), "&#27;") {
			t.Fatalf("literal &#27; not visible for %q: %q", md, ansi.Strip(out))
		}
	}
}

func baselineText(t *testing.T, md string, width int) string {
	t.Helper()
	r, err := glamour.NewTermRenderer(
		glamour.WithStyles(activeStyle),
		glamour.WithWordWrap(width),
		glamour.WithInlineTableLinks(true),
	)
	if err != nil {
		t.Fatal(err)
	}
	out, err := r.Render(md)
	if err != nil {
		t.Fatal(err)
	}
	return ansi.Strip(out)
}

func TestRenderKeepsBenignAmpersands(t *testing.T) {
	out, err := Render("it&#8217;s a &amp; b", 80)
	if err != nil {
		t.Fatal(err)
	}
	if text := ansi.Strip(out); !strings.Contains(text, "it’s a & b") {
		t.Fatalf("benign references changed: %q", text)
	}

	for _, md := range []string{
		"```go\nif a && b {}\n```\n",
		"x `a && b` y",
		"https://x/d?a=1&from=now&to=now",
		"**\"foo\"**&bar",
		"_foo_&bar",
	} {
		got, err := Render(md, 80)
		if err != nil {
			t.Fatal(err)
		}
		if want := baselineText(t, md, 80); ansi.Strip(got) != want {
			t.Fatalf("%q differs from baseline:\n got %q\nwant %q", md, ansi.Strip(got), want)
		}
		if strings.ContainsRune(got, '\uE000') {
			t.Fatalf("sentinel leaked for %q", md)
		}
	}
}

func TestRenderFencedAmpersandsHaveNoErrorBackground(t *testing.T) {
	out, err := Render("```go\nif a && b {}\n```\n", 81)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "48;5;203") {
		t.Fatalf("&& carries the Error background: %q", out)
	}
}
