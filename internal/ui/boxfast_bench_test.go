package ui

import (
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/noamsto/prdash/internal/cache"
)

// richBoardAt is richBoard (richbench_test.go) parameterized on terminal
// width/height, so the fallback-geometry bench below can drive the same rich
// fixture at the width that puts the side box on the fallback path instead of
// the board sweep's 180.
func richBoardAt(b *testing.B, w, h int, outerFrame bool) Model {
	b.Helper()
	c := cache.Open(filepath.Join(b.TempDir(), "c.json"))
	m := NewModel("/repo", "is:open", c)
	m.SetRepo("owner/repo")
	// main.go flips the float chrome on before the program starts, so the first
	// WindowSizeMsg is what insets width/height by the frame. Setting it after a
	// resize would leave the board sized to the full terminal and hand the outer
	// box content two cells wider than its own interior.
	m.SetOuterFrame(outerFrame)
	u, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	m = u.(Model)
	m.setPRs(richPRs(80))
	return m
}

// requireSideBoxFastPath and requireSideBoxFallback pin the assumption each
// geometry bench below is named for, using the production predicate rather
// than a hardcoded width — a fixture edit that changes which line overflows
// would otherwise relabel the headline numbers with nothing failing.
func requireSideBoxFastPath(b *testing.B, m Model) {
	b.Helper()
	l := computeLayout(m.width, m.height)
	if _, _, reason := boxFastReason(m.previewScrolled(), l.SideWidth, m.previewHeight(l)); reason != "" {
		b.Fatalf("want the side box on the fast path at w=%d h=%d, got fallback: %s", m.width, m.height, reason)
	}
}

func requireSideBoxFallback(b *testing.B, m Model) {
	b.Helper()
	l := computeLayout(m.width, m.height)
	if _, _, reason := boxFastReason(m.previewScrolled(), l.SideWidth, m.previewHeight(l)); reason == "" {
		b.Fatalf("want the side box on the fallback path at w=%d h=%d, got none", m.width, m.height)
	}
}

// BenchmarkFrameOuterFrame is a parked frame with the float chrome lazytmux
// sets via PRDASH_FRAME=1 — it wraps renderInner's output in the largest box
// in the app, so it pays boxBody a second time at the full terminal size.
func BenchmarkFrameOuterFrame(b *testing.B) {
	m := richBoardAt(b, 180, 45, true)
	requireSideBoxFastPath(b, m)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		_ = m.render()
	}
}

// BenchmarkFrameFallbackGeometry is a parked frame at the one live geometry
// where the side box falls back: at terminal w=120, previewScrolled's
// overview title overflows the 64-cell interior lipgloss.Width would still
// wrap it into. Kept separate from the 180-wide benches so a headline speedup
// is never quoted for a configuration this trips.
func BenchmarkFrameFallbackGeometry(b *testing.B) {
	m := richBoardAt(b, 120, 45, false)
	requireSideBoxFallback(b, m)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		_ = m.render()
	}
}

// BenchmarkBoxBodyList isolates boxBody on the list pane's real content,
// clamped exactly as titledBoxTinted clamps its own w/h before ever calling
// boxBody, so the bench doesn't exercise a precondition production never
// violates.
func BenchmarkBoxBodyList(b *testing.B) {
	m := richBoard(b)
	requireSideBoxFastPath(b, m)
	l := computeLayout(m.width, m.height)
	content := m.listBody()
	w, h := clampBoxDims(l.ListWidth, m.contentHeight(l))
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		_ = boxBody(content, w, h)
	}
}

// BenchmarkBoxBodySide is BenchmarkBoxBodyList's counterpart for the preview
// pane, at the 180-wide geometry where the side box takes the fast path.
func BenchmarkBoxBodySide(b *testing.B) {
	m := richBoard(b)
	requireSideBoxFastPath(b, m)
	l := computeLayout(m.width, m.height)
	content := m.previewScrolled()
	w, h := clampBoxDims(l.SideWidth, m.previewHeight(l))
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		_ = boxBody(content, w, h)
	}
}

// BenchmarkJoinBoardDocked isolates joinBoard on renderDocked's own ingredients
// — the bar, list, panel and side blocks it actually stacks — with everything
// but the join itself computed outside the timed loop.
func BenchmarkJoinBoardDocked(b *testing.B) {
	m := richBoard(b)
	requireSideBoxFastPath(b, m)
	l := computeLayout(m.width, m.height)
	tint := accentFor(m.mode)
	bar := m.filterBar()
	ch := max(1, l.ContentHeight-m.filterBarRows())
	list := titledBoxTinted(m.listBody(), l.ListWidth, ch, m.listTitle(), tint)
	panel := m.keysActionsPanel(l.ListWidth)
	side := titledBoxTinted(m.previewScrolled(), l.SideWidth, m.previewHeight(l), m.previewTitle(), tint)
	stack := []string{bar, list, panel}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		_ = joinBoard(stack, side, l.Gap)
	}
}

// BenchmarkKeysActionsPanel is the docked keys/actions panel alone — the one
// genuinely static component (spec's "why not memoise" section), measured
// here to size panelBody's share of the frame, the largest slice left.
func BenchmarkKeysActionsPanel(b *testing.B) {
	m := richBoard(b)
	l := computeLayout(m.width, m.height)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		_ = m.keysActionsPanel(l.ListWidth)
	}
}

// BenchmarkPanelBodyOnly isolates panelBody — keysActionsPanel's interior,
// minus its own titledBox call — the half of the panel the box fast path does
// not reach, and so the one still carrying Style.Width's cost.
func BenchmarkPanelBodyOnly(b *testing.B) {
	m := richBoard(b)
	l := computeLayout(m.width, m.height)
	label, acts := m.actionHints()
	hints := navHintsFor(m.mode)
	innerW := l.ListWidth - 2
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		_ = panelBody(innerW, hints, label, acts)
	}
}
