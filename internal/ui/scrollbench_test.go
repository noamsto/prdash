package ui

import (
	"fmt"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/noamsto/prdash/internal/cache"
	"github.com/noamsto/prdash/internal/gh"
)

// BenchmarkScrollRender measures the synchronous per-keystroke cost of a fast
// scroll: move the cursor one row and re-render, with no detail cached (the
// debounced fetch has not fired yet). This is exactly the View-loop work a held
// `j` pays per frame.
func BenchmarkScrollRender(b *testing.B) {
	c := cache.Open(filepath.Join(b.TempDir(), "c.json"))
	m := NewModel("/repo", "is:open", c)
	m.SetRepo("owner/repo")
	m.SetRunner(stubRunner{})

	prs := make([]gh.PR, 80)
	for i := range prs {
		prs[i] = gh.PR{
			Number: i + 1,
			Title:  fmt.Sprintf("feat: some change number %d that is reasonably long", i+1),
			State:  "OPEN",
			Body:   "## Summary\n\nDoes a thing.\n\n- point one\n- point two\n\n```go\nfunc x() {}\n```\n",
		}
		prs[i].Author.Login = "octocat"
	}

	u, _ := m.Update(tea.WindowSizeMsg{Width: 180, Height: 45})
	m = u.(Model)
	m.setPRs(prs)

	b.ResetTimer()
	for i := range b.N {
		down := i%2 == 0
		if down {
			m.moveCursor(1)
		} else {
			m.moveCursor(-1)
		}
		_ = m.render() // the full View() payload
	}
}

// BenchmarkParkedRender isolates the per-frame cost with the cursor parked (no
// moveCursor/renderList): just render(). The gap between this and
// BenchmarkScrollRender is the renderList (all-rows rebuild) cost per keystroke.
func BenchmarkParkedRender(b *testing.B) {
	m := benchBoard(b)
	b.ResetTimer()
	for range b.N {
		_ = m.render()
	}
}

func BenchmarkPreviewPaneOnly(b *testing.B) {
	m := benchBoard(b)
	b.ResetTimer()
	for range b.N {
		_ = m.previewPane()
	}
}

func BenchmarkViewportViewOnly(b *testing.B) {
	m := benchBoard(b)
	b.ResetTimer()
	for range b.N {
		_ = m.vp.View()
	}
}

func benchBoard(b *testing.B) Model {
	b.Helper()
	c := cache.Open(filepath.Join(b.TempDir(), "c.json"))
	m := NewModel("/repo", "is:open", c)
	m.SetRepo("owner/repo")
	m.SetRunner(stubRunner{})
	prs := make([]gh.PR, 80)
	for i := range prs {
		prs[i] = gh.PR{
			Number: i + 1,
			Title:  fmt.Sprintf("feat: some change number %d that is reasonably long", i+1),
			State:  "OPEN",
			Body:   "## Summary\n\nDoes a thing.\n\n- point one\n- point two\n\n```go\nfunc x() {}\n```\n",
		}
		prs[i].Author.Login = "octocat"
	}
	u, _ := m.Update(tea.WindowSizeMsg{Width: 180, Height: 45})
	m = u.(Model)
	m.setPRs(prs)
	return m
}
