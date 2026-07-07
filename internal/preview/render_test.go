package preview

import (
	"strings"
	"testing"
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
