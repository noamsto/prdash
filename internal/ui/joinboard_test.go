package ui

import (
	"fmt"
	"testing"

	"github.com/noamsto/prdash/internal/gh"
)

// joinSweepIssues is sweepPRs' issue-mode counterpart: enough rows, and enough
// author/label/wide-rune variety, to drive the issue board through the same
// geometry × state matrix joinBoard is checked against.
func joinSweepIssues() []gh.Issue {
	mk := func(n int, title, login string, labels ...gh.Label) gh.Issue {
		is := gh.Issue{Number: n, Title: title, Labels: labels}
		is.Author.Login = login
		return is
	}
	return []gh.Issue{
		mk(12, "Crash when opening a closed PR's diff", "al"),
		mk(4321, "Board scans vertically on narrow terminals", "octocat-bot", gh.Label{Name: "bug", Color: "d73a4a"}),
		mk(88, "Spike a caching layer for issue previews", "carol"),
		mk(512, "重试 retry the issue fetch on rate limit", "dana"),
	}
}

// joinMatrixModel builds a loaded board in mode ("pr" or "issue") carrying
// enough rows to drive joinBoard's real inputs — the list body, the preview,
// and the keys/actions panel — across the geometry × state matrix below.
func joinMatrixModel(t *testing.T, mode string) Model {
	t.Helper()
	m := NewModel("/repo", "is:open", nil)
	m.SetRepo("owner/repo")
	m.loaded = true
	if mode == "issue" {
		m.mode = "issue"
		m.section = NewIssueSection("is:open")
		m.setIssues(joinSweepIssues())
		return m
	}
	m.setPRs(sweepPRs())
	return m
}

// TestJoinBoardMatchesLipglossAcrossMatrix drives joinBoard's two real call
// shapes — renderDocked's three-block stack and renderMain's ShowSide
// two-block stack — through geometry (straddling footerMinWidth/
// footerMinHeight, plus degenerate sizes) and state (filtering, batch
// selection, mode, previewMax, theme). The "expected" side of every comparison
// is joinBoard itself falling back to joinBoardLipgloss, never a hand-written
// copy of the lipgloss triple, so the two paths can't independently drift from
// what production calls.
func TestJoinBoardMatchesLipglossAcrossMatrix(t *testing.T) {
	t.Cleanup(func() { applyTheme(Mocha()) })

	widths := []int{10, 40, footerMinWidth - 1, footerMinWidth, footerMinWidth + 1, 250}
	heights := []int{5, footerMinHeight - 1, footerMinHeight, footerMinHeight + 1}
	themes := []struct {
		name string
		fn   func() Theme
	}{{"mocha", Mocha}, {"latte", Latte}}

	cases := 0
	for _, th := range themes {
		applyTheme(th.fn())
		for _, mode := range []string{"pr", "issue"} {
			for _, w := range widths {
				for _, h := range heights {
					for _, filterState := range []string{"off", "emptyQuery", "query"} {
						for _, selected := range []bool{false, true} {
							for _, previewMax := range []bool{false, true} {
								cases++
								label := fmt.Sprintf("theme=%s mode=%s w=%d h=%d filter=%s selected=%v previewMax=%v",
									th.name, mode, w, h, filterState, selected, previewMax)

								m := joinMatrixModel(t, mode)
								m.width, m.height = w, h
								m.filtering = filterState != "off"
								if filterState == "query" {
									m.filterInput.SetValue("a")
								}
								m.applyFilter()
								if selected {
									m.sel.toggle(m.cursor)
								}
								m.previewMax = previewMax
								m.renderList()

								l := computeLayout(w, h)
								checkJoinBoardWiring(t, label, m, l)
							}
						}
					}
				}
			}
		}
	}
	if cases == 0 {
		t.Fatal("matrix executed zero cases (test would pass vacuously)")
	}
}

// checkJoinBoardWiring exercises joinBoard directly with the real ingredients
// renderDocked and renderMain's ShowSide branch build, then re-renders
// renderDocked and renderMain themselves under the flag flip — so both the
// helper and its wiring at the two call sites are covered, not just one.
func checkJoinBoardWiring(t *testing.T, label string, m Model, l Layout) {
	t.Helper()
	tint := accentFor(m.mode)
	bar := m.filterBar()

	dockedCh := max(1, l.ContentHeight-m.filterBarRows())
	list := titledBoxTinted(m.listBody(), l.ListWidth, dockedCh, m.listTitle(), tint)
	panel := m.keysActionsPanel(l.ListWidth)
	dockedSide := titledBoxTinted(m.previewScrolled(), l.SideWidth, m.previewHeight(l), m.previewTitle(), tint)

	joinFastDisabled = false
	gotDocked := joinBoard([]string{bar, list, panel}, dockedSide, l.Gap)
	joinFastDisabled = true
	wantDocked := joinBoard([]string{bar, list, panel}, dockedSide, l.Gap)
	joinFastDisabled = false
	if gotDocked != wantDocked {
		t.Errorf("%s: joinBoard(docked shape) mismatch\nfast:     %q\nlipgloss: %q", label, gotDocked, wantDocked)
	}

	ch := m.contentHeight(l)
	listBox := titledBoxTinted(m.listBody(), l.ListWidth, ch, m.listTitle(), tint)
	mainSide := titledBoxTinted(m.previewScrolled(), l.SideWidth, m.previewHeight(l), m.previewTitle(), tint)

	joinFastDisabled = false
	gotMain := joinBoard([]string{bar, listBox}, mainSide, l.Gap)
	joinFastDisabled = true
	wantMain := joinBoard([]string{bar, listBox}, mainSide, l.Gap)
	joinFastDisabled = false
	if gotMain != wantMain {
		t.Errorf("%s: joinBoard(renderMain shape) mismatch\nfast:     %q\nlipgloss: %q", label, gotMain, wantMain)
	}

	joinFastDisabled = false
	dockedFast := m.renderDocked(l)
	mainFast := m.renderMain()
	joinFastDisabled = true
	dockedSlow := m.renderDocked(l)
	mainSlow := m.renderMain()
	joinFastDisabled = false

	if dockedFast != dockedSlow {
		t.Errorf("%s: renderDocked mismatch under joinFastDisabled flip", label)
	}
	if mainFast != mainSlow {
		t.Errorf("%s: renderMain mismatch under joinFastDisabled flip", label)
	}
}
