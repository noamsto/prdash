package ui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/noamsto/prdash/internal/action"
	"github.com/noamsto/prdash/internal/gh"
)

// matrixWidths/matrixHeights straddle footerMinWidth/footerMinHeight and
// include a couple of degenerate sizes, plus the two terminal widths the
// measurement doc pinned numbers to: 120 (SideWidth=66, ListWidth=52) and 180.
var matrixWidths = []int{5, 40, footerMinWidth - 1, footerMinWidth, footerMinWidth + 1, 120, 180}
var matrixHeights = []int{3, 10, footerMinHeight - 1, footerMinHeight, footerMinHeight + 1, 45}

// matrixOpts is the state-axis point in the board matrix: everything that
// changes block shape, which is exactly what a memoisation would have had to
// invalidate against. ShowSide/ShowPanel/ShowFooter are derived by
// computeLayout from width/height, not driven directly.
type matrixOpts struct {
	mode        string // "pr" | "issue"
	filtering   bool
	filterQuery string
	batch       bool
	previewMax  bool
}

// matrixStates enumerates every state-axis combination the matrix sweeps.
func matrixStates() []matrixOpts {
	var out []matrixOpts
	for _, mode := range []string{"pr", "issue"} {
		for _, filt := range []struct {
			on bool
			q  string
		}{{false, ""}, {true, ""}, {true, "re"}} {
			for _, batch := range []bool{false, true} {
				for _, previewMax := range []bool{false, true} {
					out = append(out, matrixOpts{mode, filt.on, filt.q, batch, previewMax})
				}
			}
		}
	}
	return out
}

// sweepIssues gives the issue-mode axis real content to render, mirroring
// sweepPRs' role on the PR side (layout_sweep_regression_test.go).
func sweepIssues() []gh.Issue {
	mk := func(n int, title, login string) gh.Issue {
		is := gh.Issue{Number: n, Title: title, UpdatedAt: time.Now()}
		is.Author.Login = login
		return is
	}
	return []gh.Issue{
		mk(9001, "Improve error messages for failed migrations", "al"),
		mk(42, "Investigate flaky integration test on CI", "carol"),
		mk(777, "重试 retry issue with a CJK title", "dana"),
	}
}

// matrixModel builds a model at one geometry x state matrix point, real
// content and all. richBoard (richbench_test.go) takes a *testing.B and can't
// be reused here; this draws on the same *testing.T fixtures sweepPRs and
// newTestModelWithRows do.
func matrixModel(t *testing.T, w, h int, opt matrixOpts) Model {
	t.Helper()
	m := NewModel("/repo", "is:open", nil)
	m.SetRepo("owner/repo")
	m.viewerLogin = "me"
	if opt.mode == "issue" {
		m.mode = "issue"
		sec := NewIssueSection("is:open")
		sec.SetIssues(sweepIssues())
		m.section = sec
		m.actions = action.DefaultIssueActions()
		m.applyFilter()
	} else {
		m.setPRs(sweepPRs())
	}
	u, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	m = u.(Model)
	if opt.filtering {
		m.filtering = true
		m.filterInput.Focus()
		m.filterInput.SetValue(opt.filterQuery)
		m.applyFilter()
	}
	if opt.batch {
		m.sel.toggle(0)
	}
	m.previewMax = opt.previewMax
	m.renderList()
	return m
}

// boxCase names one of the three real box producers a matrix point exercises,
// for error/log messages.
type boxCase struct {
	name    string
	content string
	w, h    int
}

// clampBoxDims applies the same floor titledBoxTinted/titledBox apply to w and
// h before ever calling boxBody — every real caller goes through one of those,
// never boxBody directly, so a case built from the unclamped layout numbers
// would exercise a precondition (h>=2) boxBody's callers never actually violate.
func clampBoxDims(w, h int) (int, int) {
	if w < 4 {
		w = 4
	}
	if h < 2 {
		h = 2
	}
	return w, h
}

// matrixBoxCases returns the boxBody call each real render path makes at this
// matrix point — only the ones production would actually draw here. The side
// box is skipped entirely outside previewMax/ShowSide (SideWidth is 0 there,
// a shape no caller ever hands boxBody) and the panel outside ShowPanel.
func matrixBoxCases(m Model) []boxCase {
	l := computeLayout(m.width, m.height)
	var cases []boxCase

	if !m.previewMax {
		w, h := clampBoxDims(l.ListWidth, m.contentHeight(l))
		cases = append(cases, boxCase{"list", m.listBody(), w, h})
	}
	if m.previewMax || l.ShowSide {
		sideW := l.SideWidth
		if m.previewMax {
			sideW = m.width
		}
		w, h := clampBoxDims(sideW, m.previewHeight(l))
		cases = append(cases, boxCase{"side", m.previewScrolled(), w, h})
	}
	if l.ShowPanel && !m.previewMax {
		label, acts := m.actionHints()
		content := panelBody(l.ListWidth-2, navHintsFor(m.mode), label, acts)
		w, h := clampBoxDims(l.ListWidth, l.PanelRows)
		cases = append(cases, boxCase{"panel", content, w, h})
	}
	return cases
}

// TestBoxStateMatrix is the geometry x state x theme sweep: at
// every point it checks that boxBody is byte-identical to the pre-fast-path
// implementation on the real content each render path produces,
// that the fast path actually fires for the list and panel where that's a real
// invariant, and that boxBody/boxTop/filterBar never hand the join a tab
// or a CR. The side box is not required to fast-path anywhere — its
// pinned, known-overflowing case is TestBoxStateSideFallsBackOnKnownOverflow.
func TestBoxStateMatrix(t *testing.T) {
	t.Cleanup(func() { applyTheme(Mocha()) })

	points, mismatches := 0, 0
	fallbacks := map[string]int{}
	checked := map[string]int{}

	for _, th := range []struct {
		name  string
		build func() Theme
	}{{"Mocha", Mocha}, {"Latte", Latte}} {
		applyTheme(th.build())
		for _, w := range matrixWidths {
			for _, h := range matrixHeights {
				for _, opt := range matrixStates() {
					points++
					m := matrixModel(t, w, h, opt)
					l := computeLayout(m.width, m.height)

					// What joinBoard's equivalence rests on: its inputs carry no tab or CR.
					if bar := m.filterBar(); strings.ContainsAny(bar, "\t\r") {
						t.Errorf("filterBar contains a tab/CR at w=%d h=%d theme=%s opt=%+v", w, h, th.name, opt)
					}

					for _, c := range matrixBoxCases(m) {
						checked[c.name]++

						got := boxBody(c.content, c.w, c.h)
						want := boxBodyRef(c.content, c.w, c.h)
						if got != want {
							mismatches++
							if mismatches <= 8 {
								t.Errorf("%s box mismatch at w=%d h=%d theme=%s opt=%+v:\n got %q\nwant %q",
									c.name, w, h, th.name, opt, got, want)
							}
						}
						if strings.ContainsAny(got, "\t\r") {
							t.Errorf("%s boxBody output contains a tab/CR at w=%d h=%d theme=%s opt=%+v", c.name, w, h, th.name, opt)
						}

						_, _, reason := boxFastReason(c.content, c.w, c.h)
						if reason == "" {
							continue
						}
						fallbacks[c.name]++
						switch c.name {
						case "list", "panel":
							if w >= footerMinWidth {
								t.Errorf("%s box fell back at terminal w=%d (>= footerMinWidth) h=%d theme=%s opt=%+v: %s",
									c.name, w, h, th.name, opt, reason)
							} else {
								t.Logf("%s box fallback below footerMinWidth (clamps disagree there, not a defect): w=%d h=%d theme=%s opt=%+v: %s",
									c.name, w, h, th.name, opt, reason)
							}
						}
					}

					// boxTop, via the real wrappers that call it, must not leak a
					// tab/CR either. titledBoxTinted/keysActionsPanel prepend it to
					// boxBody's own output, already checked above, so only the top
					// line itself needs a separate look.
					for _, c := range matrixBoxCases(m) {
						var full string
						switch c.name {
						case "list":
							full = titledBoxTinted(c.content, c.w, c.h, m.listTitle(), accentFor(m.mode))
						case "side":
							full = titledBoxTinted(c.content, c.w, c.h, m.previewTitle(), accentFor(m.mode))
						case "panel":
							full = m.keysActionsPanel(l.ListWidth)
						}
						top, _, _ := strings.Cut(full, "\n")
						if strings.ContainsAny(top, "\t\r") {
							t.Errorf("%s boxTop contains a tab/CR at w=%d h=%d theme=%s opt=%+v", c.name, w, h, th.name, opt)
						}
					}
				}
			}
		}
	}

	if mismatches > 8 {
		t.Errorf("%d box mismatches total across %d matrix points (first 8 shown)", mismatches, points)
	}
	t.Logf("matrix points: %d; boxes checked: list=%d panel=%d side=%d; fallbacks: list=%d panel=%d side=%d",
		points, checked["list"], checked["panel"], checked["side"],
		fallbacks["list"], fallbacks["panel"], fallbacks["side"])
}

// TestBoxStateSideFallsBackOnKnownOverflow pins the one live fallback trigger:
// at terminal w=120 in PR mode on the overview tab, cursor 0 is
// sweepPRs' #4321 ("Rework the whole layout engine so the board scans
// vertically") — its identity header is 66 cells against the side box's
// 64-cell interior (SideWidth=66). Asserting on the reason being non-empty,
// not the literal 66, keeps this from silently going vacuous if the fixture's
// title changes length.
func TestBoxStateSideFallsBackOnKnownOverflow(t *testing.T) {
	applyTheme(Mocha())
	t.Cleanup(func() { applyTheme(Mocha()) })

	m := matrixModel(t, 120, 45, matrixOpts{mode: "pr"})
	l := computeLayout(m.width, m.height)
	if !l.ShowSide {
		t.Fatalf("terminal w=120 should show the side pane (SideWidth=%d)", l.SideWidth)
	}

	content := m.previewScrolled()
	w, h := l.SideWidth, m.previewHeight(l)
	if _, _, reason := boxFastReason(content, w, h); reason == "" {
		t.Fatalf("expected the side box to fall back at w=120 (PR #4321's identity header overflows its 64-cell interior), got fast path")
	}
	if got, want := boxBody(content, w, h), boxBodyRef(content, w, h); got != want {
		t.Errorf("fallback output diverges from lipgloss at the pinned overflow case:\n got %q\nwant %q", got, want)
	}
}

// fastVsFallback runs fn twice — once on boxBody's fast path, once forced onto
// boxBodyLipgloss via the test-only kill switch — and returns both outputs,
// restoring the switch afterward. boxBodyLipgloss is verbatim today's
// boxBodyRef, so this is the same differential as boxBodyRef without needing
// to reconstruct each caller's internal content/geometry by hand.
func fastVsFallback(t *testing.T, fn func() string) (fast, fallback string) {
	t.Helper()
	t.Cleanup(func() { boxFastDisabled = false })
	boxFastDisabled = false
	fast = fn()
	boxFastDisabled = true
	fallback = fn()
	boxFastDisabled = false
	return fast, fallback
}

// TestBoxBodyOtherCallersMatchLipgloss covers the boxBody callers the board
// sweep never exercises: the picker, the confirm panel, the log box, and —
// nothing else currently touches tabbedBox at all — the expanded view.
func TestBoxBodyOtherCallersMatchLipgloss(t *testing.T) {
	t.Run("picker", func(t *testing.T) {
		m := NewModel("/repo", "is:open", nil)
		m.height = 30
		m.pick = newPicker("reviewers", []gh.User{
			{Login: "al"}, {Login: "bob", Name: "Bob Bobson"}, {Login: "carol"},
		}, map[string]bool{"bob": true})
		fast, fallback := fastVsFallback(t, m.pickerView)
		if fast != fallback {
			t.Errorf("picker box: fast path diverges from lipgloss:\n got %q\nwant %q", fast, fallback)
		}
	})

	t.Run("confirm", func(t *testing.T) {
		m := NewModel("/repo", "is:open", nil)
		m.setPRs([]gh.PR{{Number: 42, Title: "Rename the connection pool"}})
		a := action.Action{Key: "m", Label: "Merge"}
		m.pending = &a
		fast, fallback := fastVsFallback(t, m.confirmPanel)
		if fast != fallback {
			t.Errorf("confirm box: fast path diverges from lipgloss:\n got %q\nwant %q", fast, fallback)
		}
	})

	t.Run("logBox", func(t *testing.T) {
		m := logViewModel(t)
		fast, fallback := fastVsFallback(t, m.logViewRender)
		if fast != fallback {
			t.Errorf("log box: fast path diverges from lipgloss:\n got %q\nwant %q", fast, fallback)
		}
	})

	t.Run("expandedTabbedBox", func(t *testing.T) {
		m := matrixModel(t, 120, 45, matrixOpts{mode: "pr"})
		m.enterExpanded()
		fast, fallback := fastVsFallback(t, m.expandedView)
		if fast != fallback {
			t.Errorf("expanded (tabbedBox) diverges from lipgloss:\n got %q\nwant %q", fast, fallback)
		}
	})
}
