package ui

import (
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
)

// boxBodyRef is boxBody as it stood before the fast path existed. The whole
// differential rests on it being an untouched copy, so it stays test-only and
// never shares code with the production path.
func boxBodyRef(content string, w, h int) string {
	rb := lipgloss.RoundedBorder()
	return lipgloss.NewStyle().
		Border(rb, false, true, true, true).
		BorderForeground(lipgloss.Color(theme.Rule)).
		Width(w).Height(h - 1).MaxWidth(w).MaxHeight(h - 1).
		Render(clipLines(content, h-2))
}

type boxFixture struct{ name, content string }

// boxFixtureCount is asserted against the executed case count so a fixture that
// falls out of the table below shows up as a failure rather than as silence.
const boxFixtureCount = 27

// boxFixtures spans the shapes a width comparison alone cannot decide: the ones
// Style.Render rewrites before measuring (tabs, CRLF, pens left open at a line
// end), the ones whose cell width diverges from their rune or byte count, and
// the interior-edge boundary — which is width-dependent, hence the parameter.
func boxFixtures(w int) []boxFixture {
	inner := max(0, w-2)
	return []boxFixture{
		{"empty", ""},
		{"oneLine", "abc"},
		{"trailingNewline", "abc\n"},
		{"blankLineMid", "a\n\nb"},
		{"overTall", strings.Repeat("line\n", 30)},
		{"exactFill", strings.Repeat("x", inner)},
		{"oneCellOver", strings.Repeat("x", inner+1)},
		{"farOverflow", strings.Repeat("x", 40)},
		{"tab", "a\tb"},
		{"crlf", "a\r\nb"},
		{"loneCR", "a\rb"},
		{"sgrOpenAcrossNewline", "\x1b[31mred\nstill\x1b[0m"},
		{"sgrBalanced", "\x1b[31mred\x1b[0m\n\x1b[32mgreen\x1b[0m"},
		{"sgrOpenAtEnd", "\x1b[31mred"},
		{"osc8OpenST", "\x1b]8;;https://example.com\x1b\\click\nhere\x1b]8;;\x1b\\"},
		{"osc8OpenBEL", "\x1b]8;;https://example.com\aclick\nhere\x1b]8;;\a"},
		{"osc8Balanced", "\x1b]8;;https://x.com\x1b\\a\x1b]8;;\x1b\\\n\x1b]8;;https://y.com\x1b\\b\x1b]8;;\x1b\\"},
		{"osc8OpenAtEnd", "\x1b]8;;https://x.com\x1b\\click"},
		{"osc8URISemicolon", "\x1b]8;;https://x.com/a;\x1b\\click\nhere\x1b]8;;\x1b\\"},
		{"cjk", "重试重试重试"},
		{"cjkStraddlesEdge", strings.Repeat("x", max(0, inner-1)) + "重"},
		{"zwjEmoji", "👨‍👩‍👧‍👦 fam"},
		{"combiningMarks", "e\u0301cole"},
		{"blockGlyphs", "▌▐▌▐"},
		{"trailingSpaces", "ab   "},
		{"spacesExactFill", strings.Repeat(" ", inner)},
		{"styledWideRunes", lipgloss.NewStyle().Foreground(lipgloss.Color("#ff0000")).Render("重试 retry")},
	}
}

func TestBoxBodyMatchesLipgloss(t *testing.T) {
	widths := []int{2, 3, 4, 5, 6, 10, 11, 20, 41, 80}
	heights := []int{2, 3, 4, 5, 9, 20}

	cases, mismatches := 0, 0
	for _, w := range widths {
		for _, h := range heights {
			for _, f := range boxFixtures(w) {
				cases++
				got, want := boxBody(f.content, w, h), boxBodyRef(f.content, w, h)
				if got == want {
					continue
				}
				mismatches++
				if mismatches <= 8 {
					t.Errorf("w=%d h=%d %s:\n got %q\nwant %q", w, h, f.name, got, want)
				}
			}
		}
	}
	if mismatches > 8 {
		t.Errorf("%d mismatches total (first 8 shown)", mismatches)
	}
	if want := len(widths) * len(heights) * boxFixtureCount; cases != want {
		t.Fatalf("ran %d cases, want %d — a fixture went missing", cases, want)
	}
}

// TestBoxBodyOverflowBytes pins what a box that overflows its interior actually
// looks like today: lipgloss wraps the long line onto rows that MaxHeight then
// truncates, so the bottom edge disappears entirely. Byte-identity against
// boxBodyRef would still hold if the row model were wrong, so the shape itself
// has to be written down.
func TestBoxBodyOverflowBytes(t *testing.T) {
	applyTheme(Mocha())
	t.Cleanup(func() { applyTheme(Mocha()) })

	const rule = "\x1b[38;2;88;91;112m"
	row := rule + "│\x1b[m" + strings.Repeat("x", 8) + rule + "│\x1b[m"
	want := strings.Join([]string{row, row, row}, "\n")

	if got := boxBody(strings.Repeat("x", 30), 10, 4); got != want {
		t.Errorf("boxBody overflow at w=10 h=4:\n got %q\nwant %q", got, want)
	}
	if got := boxBody("abc", 10, 2); got != rule+"│\x1b[m"+strings.Repeat(" ", 8)+rule+"│\x1b[m" {
		t.Errorf("boxBody at h=2 should be one blank row with no bottom edge, got %q", got)
	}
}

func TestPensOpenAtEnd(t *testing.T) {
	cases := []struct {
		name string
		s    string
		want bool
	}{
		{"plain", "hello", false},
		{"sgrBalanced", "\x1b[31mred\x1b[0m", false},
		{"sgrBalancedBare", "\x1b[31mred\x1b[m", false},
		{"sgrOpen", "\x1b[31mred", true},
		{"sgrReopened", "\x1b[31mred\x1b[0m  \x1b[32mg", true},
		{"osc8OpenST", "\x1b]8;;https://x.com\x1b\\click", true},
		{"osc8OpenBEL", "\x1b]8;;https://x.com\aclick", true},
		{"osc8ClosedST", "\x1b]8;;https://x.com\x1b\\click\x1b]8;;\x1b\\", false},
		{"osc8ClosedBEL", "\x1b]8;;https://x.com\aclick\x1b]8;;\a", false},
		{"osc8ParamsOnly", "\x1b]8;id=1;\x1b\\click", true},
		{"osc8UnclosedByURISemicolon", "\x1b]8;;https://x.com/a;\x1b\\click\x1b]8;;\x1b\\", false},
		{"osc8ThenSGROpen", "\x1b]8;;https://x.com\aclick\x1b]8;;\a\x1b[31m", true},
	}
	for _, c := range cases {
		if got := pensOpenAtEnd(c.s); got != c.want {
			t.Errorf("%s: pensOpenAtEnd(%q) = %v, want %v", c.name, c.s, got, c.want)
		}
	}
}

// TestPensOpenAtEndStaysLinear pins the scan's cost on adversarial input. An
// unterminated OSC introducer used to resume after the introducer and rescan the
// whole tail for a terminator that was not there, once per introducer — quadratic
// on a line of them. Preview and log content is arbitrary text off the network,
// and View() scans it on the Bubble Tea loop, so a quadratic here freezes the app.
func TestPensOpenAtEndStaysLinear(t *testing.T) {
	const small, big = 20000, 160000
	measure := func(n int) time.Duration {
		s := strings.Repeat("\x1b]", n/2)
		start := time.Now()
		_ = pensOpenAtEnd(s)
		return time.Since(start)
	}
	measure(small) // warm

	// Linear scaling would be ~8x for 8x the input; quadratic is ~64x. A 20x
	// ceiling separates them without being flaky on a loaded machine.
	sm, bg := measure(small), measure(big)
	if sm <= 0 {
		sm = time.Microsecond
	}
	if ratio := bg / sm; ratio > 20 {
		t.Errorf("scan cost grew %dx for %dx the input (%v -> %v): superlinear", ratio, big/small, sm, bg)
	}
}

// TestBoxBodyBelowPrecondition covers w and h under boxBody's documented
// minimums. Neither caller can get there — both clamp first — but the fast path
// pads to an interior width it derives from w, so a negative one must be caught
// by the scan rather than reaching strings.Repeat.
func TestBoxBodyBelowPrecondition(t *testing.T) {
	for _, tc := range []struct{ w, h int }{
		{1, 2}, {0, 2}, {1, 1}, {0, 0}, {-1, 3}, {2, 2}, {3, 2}, {2, 0},
	} {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("boxBody(%q, %d, %d) panicked: %v", "", tc.w, tc.h, r)
				}
			}()
			_ = boxBody("", tc.w, tc.h)
			_ = boxBody("abc", tc.w, tc.h)
		}()
	}
}
