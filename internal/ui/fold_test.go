package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/noamsto/prdash/internal/gh"
)

// foldCross is repoGroupFixture's cross rows with titles to filter on and
// check states, so the folded headers show at least two stage buckets.
func foldCross() []gh.PR {
	_, _, cross := repoGroupFixture()
	for i := range cross {
		switch cross[i].Ref() {
		case gh.PRRef{Repo: "c/d", Number: 9}:
			cross[i].Title = "zebrafish rollout"
			cross[i].StatusCheckRollup = []gh.Check{{Name: "ci", State: "FAILURE"}}
		case gh.PRRef{Repo: "c/d", Number: 8}:
			cross[i].Title = "quokka cleanup"
			cross[i].ReviewDecision = "APPROVED"
			cross[i].StatusCheckRollup = []gh.Check{{Name: "ci", State: "SUCCESS"}}
		}
	}
	return cross
}

func foldModel(t *testing.T) Model {
	t.Helper()
	t.Setenv("PRDASH_ACTION_FILE", filepath.Join(t.TempDir(), "action"))
	return repoGroupModel(t, foldCross())
}

func press(m Model, keys ...string) Model {
	for _, k := range keys {
		m, _ = update(m, keyMsg(k))
	}
	return m
}

// pressOnStop presses k and reports a panic as a failure: a stop row has no
// PR behind it, so an unguarded action path would index past the empty row.
func pressOnStop(t *testing.T, m Model, k string) (out Model) {
	t.Helper()
	out = m
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("%s on a stop panicked: %v", k, r)
		}
	}()
	return press(m, k)
}

func typeText(m Model, s string) Model {
	for _, r := range s {
		m = press(m, string(r))
	}
	return m
}

// onStop moves the cursor to repo's fold stop with j presses.
func onStop(t *testing.T, m Model, repo string) Model {
	t.Helper()
	i, ok := m.section.(*PRSection).stopIndex(repo)
	if !ok {
		t.Fatalf("no fold stop for %s: %v", repo, boardLayout(m))
	}
	for m.cursor < i {
		m = press(m, "j")
	}
	if m.cursor != i {
		t.Fatalf("cursor = %d, want stop %d", m.cursor, i)
	}
	return m
}

func hintText(m Model) string {
	if m.actionStatus == nil {
		return ""
	}
	return m.actionStatus.fail
}

func TestFoldKeyTogglesGroup(t *testing.T) {
	cd9 := gh.PRRef{Repo: "c/d", Number: 9}
	m := onStop(t, foldModel(t), "c/d")

	m = press(m, "e")
	ps := m.section.(*PRSection)
	if ps.isFolded("c/d") {
		t.Fatal("e on the c/d stop left it folded")
	}
	if m.cursor != shownIndexRef(m, cd9) || m.cursor < 0 {
		t.Errorf("cursor = %d after unfold, want on c/d#9 (%d): %v", m.cursor, shownIndexRef(m, cd9), boardLayout(m))
	}

	m = press(m, "e")
	ps = m.section.(*PRSection)
	if !ps.isFolded("c/d") {
		t.Fatal("e on a c/d row left it unfolded")
	}
	if i, _ := ps.stopIndex("c/d"); m.cursor != i {
		t.Errorf("cursor = %d after fold, want the c/d stop (%d)", m.cursor, i)
	}

	m = press(m, "e") // unfold, cursor on c/d#9
	m = press(m, " ")
	if m.sel.count() != 1 {
		t.Fatalf("space selected %d rows, want 1", m.sel.count())
	}
	m = press(m, "e")
	if m.sel.count() != 0 {
		t.Errorf("folding c/d kept %d selected rows", m.sel.count())
	}
}

func TestFoldStopIsInert(t *testing.T) {
	fm := &fakeMutationSource{}
	m := foldModel(t)
	m.SetMutationSource(fm)
	m = onStop(t, m, "c/d")
	before := boardLayout(m)

	for _, k := range []string{"m", "u", "M", "L", "y", "W", "R"} {
		m = pressOnStop(t, m, k)
		if h := hintText(m); !strings.Contains(h, "with e first") {
			t.Errorf("%s on a stop: hint = %q, want it to mention unfolding with e", k, h)
		}
		if m.pickerMode != "" {
			t.Errorf("%s on a stop opened the %s picker", k, m.pickerMode)
		}
		m.actionStatus = nil
	}
	if len(fm.mergeCalls)+len(fm.autoMergeCalls)+len(fm.markReadyCalls)+len(fm.convertToDraftCalls)+len(fm.updateBranchCalls)+len(fm.approveCalls)+len(fm.reviewCalls) != 0 {
		t.Errorf("a stop triggered mutations: %+v", fm)
	}
	if _, err := os.Stat(os.Getenv("PRDASH_ACTION_FILE")); err == nil {
		t.Error("a stop key wrote a worktree action")
	}

	m = press(m, " ")
	if m.sel.count() != 0 {
		t.Errorf("space selected %d on a stop", m.sel.count())
	}
	if got := boardLayout(m); strings.Join(got, ",") != strings.Join(before, ",") {
		t.Errorf("inert keys changed the board: %v -> %v", before, got)
	}

	m = press(m, "enter")
	if m.section.(*PRSection).isFolded("c/d") {
		t.Error("enter on the stop did not unfold c/d")
	}
	if _, err := os.Stat(os.Getenv("PRDASH_ACTION_FILE")); err == nil {
		t.Error("enter on a stop ran a worktree action")
	}
}

func TestFilterRevealsFoldedGroup(t *testing.T) {
	cd9 := gh.PRRef{Repo: "c/d", Number: 9}
	m := foldModel(t)
	m = press(m, "/")
	m = typeText(m, "zebrafish")
	m = press(m, "enter")

	i := shownIndexRef(m, cd9)
	if i < 0 {
		t.Fatalf("filtered board lacks c/d#9: %v", boardLayout(m))
	}
	if !strings.Contains(ansi.Strip(m.View().Content), "d zebrafish rollout") {
		t.Errorf("flat result lacks the dim d repo prefix:\n%s", ansi.Strip(m.View().Content))
	}

	m = press(m, "esc")
	if m.section.(*PRSection).isFolded("c/d") {
		t.Error("clearing the filter left c/d folded")
	}
	if want := shownIndexRef(m, cd9); want < 0 || m.cursor != want {
		t.Errorf("cursor = %d, want on c/d#9 (%d): %v", m.cursor, want, boardLayout(m))
	}
}

func TestFoldKeyOtherBoards(t *testing.T) {
	t.Run("issue board", func(t *testing.T) {
		m := press(foldModel(t), "tab")
		if m.mode != "issue" {
			t.Fatalf("mode = %q, want issue", m.mode)
		}
		m = press(m, "e")
		if h := hintText(m); !strings.Contains(h, "PR-only") {
			t.Errorf("hint = %q, want the PR-only hint", h)
		}
	})
	t.Run("flat non-default board", func(t *testing.T) {
		m := foldModel(t)
		for range 5 {
			if m.state == "merged" {
				break
			}
			m = press(m, "s")
		}
		if m.state != "merged" {
			t.Fatalf("could not reach the merged board, state = %q", m.state)
		}
		m.setPRs([]gh.PR{mergedPR(30, "alice")})
		before := boardLayout(m)
		m.actionStatus = nil
		m = press(m, "e")
		if hintText(m) == "" {
			t.Error("e on a flat board showed no hint")
		}
		if got := boardLayout(m); strings.Join(got, ",") != strings.Join(before, ",") {
			t.Errorf("e changed a flat board: %v -> %v", before, got)
		}
	})
}

func TestLegendListsFoldKey(t *testing.T) {
	m := foldModel(t)
	for _, g := range m.keyPanes() {
		for _, k := range g.hints {
			if k.key == "e" && strings.Contains(k.label, "fold") {
				return
			}
		}
	}
	t.Fatal("PR-mode legend lacks an e fold hint")
}

func TestGroupedBoardFitsWidths(t *testing.T) {
	glyphs := make([]string, 0, int(stageBucketCount))
	for b := stageBucket(0); b < stageBucketCount; b++ {
		glyphs = append(glyphs, ansi.Strip(stageGlyph(b)))
	}
	for _, w := range []int{120, 80} {
		for _, framed := range []bool{false, true} {
			name := map[bool]string{false: "plain", true: "framed"}[framed]
			t.Run(fmt.Sprintf("%s_%d", name, w), func(t *testing.T) {
				m := foldModel(t)
				m.SetOuterFrame(framed)
				u, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: 40})
				m = u.(Model)
				view := ansi.Strip(m.View().Content)
				t.Logf("width %d framed=%v:\n%s", w, framed, view)

				for i, line := range strings.Split(m.View().Content, "\n") {
					if lw := lipgloss.Width(line); lw > w {
						t.Errorf("line %d is %d wide, max %d: %q", i, lw, w, ansi.Strip(line))
					}
				}
				lines := strings.Split(view, "\n")
				var launchHdr, folded bool
				for _, l := range lines {
					if strings.Contains(l, launchRepo) {
						launchHdr = true
					}
					if strings.Contains(l, "c/d") || strings.Contains(l, "a/b") {
						for _, g := range glyphs {
							if strings.Contains(l, g) {
								folded = true
							}
						}
					}
				}
				if !launchHdr {
					t.Errorf("no %s header line", launchRepo)
				}
				if !folded {
					t.Error("no folded c/d or a/b header with a stage glyph")
				}
				if !strings.Contains(view, "pr bob") || !strings.Contains(view, "pr me") {
					t.Error("launch row titles missing")
				}
			})
		}
	}
}

func TestExpandedHeaderShowsRowRepo(t *testing.T) {
	m := onStop(t, foldModel(t), "c/d")
	m = press(m, "e") // unfold, cursor on c/d#9
	m = press(m, "l")
	if !m.expanded {
		t.Fatal("l did not expand the preview")
	}
	if v := ansi.Strip(m.View().Content); !strings.Contains(v, "c/d #9") {
		t.Errorf("expanded header lacks the row's repo:\n%s", v)
	}
}
