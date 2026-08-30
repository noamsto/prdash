package ui

import (
	"fmt"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/noamsto/prdash/internal/action"
	"github.com/noamsto/prdash/internal/gh"
)

// composedFrame renders fn once with both fast paths live and once with both
// forced onto their lipgloss fallbacks. There is no other seam to check a
// composed frame through — render -> renderInner -> board ->
// renderMain/renderDocked -> titledBoxTinted -> boxBody is a chain of direct
// static calls — so the "expected" side is built by the production code
// itself under the kill switches. Restoring via t.Cleanup means a failing
// case can't leak the switches into a later test.
func composedFrame(t *testing.T, fn func() string) (fast, slow string) {
	t.Helper()
	t.Cleanup(func() {
		boxFastDisabled = false
		joinFastDisabled = false
	})
	boxFastDisabled, joinFastDisabled = false, false
	fast = fn()
	boxFastDisabled, joinFastDisabled = true, true
	slow = fn()
	boxFastDisabled, joinFastDisabled = false, false
	return fast, slow
}

// withOuterFrame turns on the float chrome and re-sends the WindowSizeMsg m
// was already sized with. SetOuterFrame has to precede that message: it's the
// WindowSizeMsg handler that shrinks m.width/m.height by the border, and
// repaintActive (called from the same handler) reflows whatever view is
// active at the new size.
func withOuterFrame(t *testing.T, m Model, w, h int) Model {
	t.Helper()
	m.SetOuterFrame(true)
	u, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return u.(Model)
}

// asOuterFramed wraps an already-sized model in the float chrome without
// re-deriving its content at a new size. The log-view fixture builds its
// viewport content once at a fixed width; going through SetOuterFrame +
// WindowSizeMsg would reflow it via repaintActive's setLogContent, which
// isn't what a "same content, now framed" case is checking.
func asOuterFramed(m Model) Model {
	m.outerFrame = true
	m.termW, m.termH = m.width+2, m.height+2
	return m
}

// frameThemes is the theme axis every case in this file runs under.
func frameThemes() []struct {
	name string
	fn   func() Theme
} {
	return []struct {
		name string
		fn   func() Theme
	}{{"Mocha", Mocha}, {"Latte", Latte}}
}

// TestRenderInnerMatchesLipglossAcrossMatrix covers the shipped default
// (outerFrame off): renderInner is the entry point render() falls through to
// unchanged whenever the float chrome isn't active, and nothing before this
// file diffed it as a composed whole.
func TestRenderInnerMatchesLipglossAcrossMatrix(t *testing.T) {
	t.Cleanup(func() { applyTheme(Mocha()) })

	cases := 0
	for _, w := range matrixWidths {
		for _, h := range matrixHeights {
			t.Run(fmt.Sprintf("w=%d,h=%d", w, h), func(t *testing.T) {
				for _, th := range frameThemes() {
					applyTheme(th.fn())
					for _, opt := range matrixStates() {
						cases++
						m := matrixModel(t, w, h, opt)
						fast, slow := composedFrame(t, m.renderInner)
						if fast != slow {
							t.Errorf("theme=%s opt=%+v: renderInner mismatch\nfast: %q\nslow: %q",
								th.name, opt, fast, slow)
						}
					}
				}
			})
		}
	}
	if cases == 0 {
		t.Fatal("matrix executed zero cases (test would pass vacuously)")
	}
	t.Logf("renderInner matrix cases: %d", cases)
}

// TestRenderOuterFrameMatchesLipglossAcrossMatrix covers render() with the
// float chrome on: its content is header + renderMain/renderDocked +
// statusBar concatenated raggedly, none of it padded to a common width, which
// is a shape nothing else in the box/join differentials produces.
func TestRenderOuterFrameMatchesLipglossAcrossMatrix(t *testing.T) {
	t.Cleanup(func() { applyTheme(Mocha()) })

	cases := 0
	for _, w := range matrixWidths {
		for _, h := range matrixHeights {
			t.Run(fmt.Sprintf("w=%d,h=%d", w, h), func(t *testing.T) {
				for _, th := range frameThemes() {
					applyTheme(th.fn())
					for _, opt := range matrixStates() {
						cases++
						m := withOuterFrame(t, matrixModel(t, w, h, opt), w, h)
						fast, slow := composedFrame(t, m.render)
						if fast != slow {
							t.Errorf("theme=%s opt=%+v: render() mismatch with outerFrame on\nfast: %q\nslow: %q",
								th.name, opt, fast, slow)
						}
					}
				}
			})
		}
	}
	if cases == 0 {
		t.Fatal("matrix executed zero cases (test would pass vacuously)")
	}
	t.Logf("outer-frame render matrix cases: %d", cases)
}

// overlayKinds are the floating panels renderInner composes over the board
// via overlayAt/overlayTop. Each is checked because overlayAt returns a
// lipgloss.Canvas render — arbitrary cell-buffer ANSI, not the styled-text
// output every other producer in this package hands the outer box.
var overlayKinds = []string{"legend", "picker", "actions", "confirm"}

// armOverlay puts m into the state that makes renderInner route through the
// named overlay.
func armOverlay(m Model, kind string) Model {
	switch kind {
	case "legend":
		m.showLegend = true
	case "picker":
		m.showPicker = true
		m.pick = newPicker("reviewers", []gh.User{
			{Login: "al"}, {Login: "bob", Name: "Bob Bobson"}, {Login: "carol"},
		}, map[string]bool{"bob": true})
	case "actions":
		m.showActions = true
	case "confirm":
		a := action.Action{Key: "m", Label: "Merge"}
		m.pending = &a
	}
	return m
}

// TestRenderOuterFrameOverlaysMatchLipgloss covers render() with the outer
// frame on and an overlay active: the outer box's content is then an
// overlayAt canvas render laid over the board, not styled text.
func TestRenderOuterFrameOverlaysMatchLipgloss(t *testing.T) {
	t.Cleanup(func() { applyTheme(Mocha()) })

	cases := 0
	for _, w := range matrixWidths {
		for _, h := range matrixHeights {
			t.Run(fmt.Sprintf("w=%d,h=%d", w, h), func(t *testing.T) {
				for _, th := range frameThemes() {
					applyTheme(th.fn())
					for _, kind := range overlayKinds {
						cases++
						m := armOverlay(withOuterFrame(t, matrixModel(t, w, h, matrixOpts{mode: "pr"}), w, h), kind)
						fast, slow := composedFrame(t, m.render)
						if fast != slow {
							t.Errorf("theme=%s overlay=%s: render() mismatch\nfast: %q\nslow: %q",
								th.name, kind, fast, slow)
						}
					}
				}
			})
		}
	}
	if cases == 0 {
		t.Fatal("matrix executed zero cases (test would pass vacuously)")
	}
	t.Logf("outer-frame overlay cases: %d", cases)
}

// findStatusBarOverflowGeometry locates a terminal size where board()'s
// status-bar footer (the ShowFooter && !ShowPanel branch) is wider than the
// outer box's interior once the float chrome claims 2 cells on each side.
// statusBar's hint text is close to fixed width while the interior it has to
// fit inside grows with the terminal, so this is only true in a narrow band
// just above footerMinWidth/footerMinHeight — found by measurement rather
// than asserted at a guessed number, per the geometry it's tied to.
func findStatusBarOverflowGeometry(t *testing.T) (w, h int) {
	t.Helper()
	for w := footerMinWidth; w < footerMinWidth+60; w++ {
		for h := footerMinHeight; h < footerMinHeight+20; h++ {
			if l := computeLayout(w-2, h-2); !l.ShowFooter || l.ShowPanel {
				continue
			}
			m := withOuterFrame(t, matrixModel(t, w, h, matrixOpts{mode: "pr"}), w, h)
			if _, _, reason := boxFastReason(m.renderInner(), m.termW, m.termH); reason != "" {
				return w, h
			}
		}
	}
	t.Fatal("no geometry found where the status-bar footer overflows the outer box interior")
	return 0, 0
}

// TestRenderFrameStatusBarOverflowFallback exercises the outer box's fallback
// path end to end, at the one geometry where the content it wraps is proven
// (not assumed) to overflow its interior.
func TestRenderFrameStatusBarOverflowFallback(t *testing.T) {
	t.Cleanup(func() { applyTheme(Mocha()) })
	w, h := findStatusBarOverflowGeometry(t)

	for _, th := range frameThemes() {
		t.Run(th.name, func(t *testing.T) {
			applyTheme(th.fn())
			m := withOuterFrame(t, matrixModel(t, w, h, matrixOpts{mode: "pr"}), w, h)
			if _, _, reason := boxFastReason(m.renderInner(), m.termW, m.termH); reason == "" {
				t.Fatalf("expected the outer box to fall back at w=%d h=%d (statusBar wider than its interior), got fast path", w, h)
			}
			fast, slow := composedFrame(t, m.render)
			if fast != slow {
				t.Errorf("w=%d h=%d: render() mismatch at the statusBar-overflow geometry\nfast: %q\nslow: %q", w, h, fast, slow)
			}
		})
	}
}

// TestRenderExpandedAndLogViewMatchLipgloss covers tabbedBox's only two call
// sites (the expanded view and the log view), each with and without the
// outer frame — nothing before this file exercised tabbedBox as a composed
// frame at all.
func TestRenderExpandedAndLogViewMatchLipgloss(t *testing.T) {
	t.Cleanup(func() { applyTheme(Mocha()) })
	cases := 0

	t.Run("expanded", func(t *testing.T) {
		for _, w := range matrixWidths {
			for _, h := range matrixHeights {
				t.Run(fmt.Sprintf("w=%d,h=%d", w, h), func(t *testing.T) {
					for _, th := range frameThemes() {
						applyTheme(th.fn())
						for _, outer := range []bool{false, true} {
							cases++
							m := matrixModel(t, w, h, matrixOpts{mode: "pr"})
							if outer {
								m = withOuterFrame(t, m, w, h)
							}
							m.enterExpanded()
							fast, slow := composedFrame(t, m.render)
							if fast != slow {
								t.Errorf("theme=%s outer=%v: expanded render() mismatch\nfast: %q\nslow: %q",
									th.name, outer, fast, slow)
							}
						}
					}
				})
			}
		}
	})

	t.Run("logView", func(t *testing.T) {
		for _, th := range frameThemes() {
			applyTheme(th.fn())
			for _, outer := range []bool{false, true} {
				t.Run(fmt.Sprintf("outer=%v", outer), func(t *testing.T) {
					cases++
					m := logViewModel(t)
					m.logView = true
					if outer {
						m = asOuterFramed(m)
					}
					fast, slow := composedFrame(t, m.render)
					if fast != slow {
						t.Errorf("theme=%s outer=%v: log view render() mismatch\nfast: %q\nslow: %q",
							th.name, outer, fast, slow)
					}
				})
			}
		}
	})

	if cases == 0 {
		t.Fatal("matrix executed zero cases (test would pass vacuously)")
	}
	t.Logf("expanded/log view cases: %d", cases)
}
