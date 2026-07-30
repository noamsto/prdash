package ui

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/noamsto/prdash/internal/cache"
	"github.com/noamsto/prdash/internal/gh"
)

// richPRs builds a board fixture with the per-row detail real PRs carry, which
// benchBoard's fixture omits entirely: it sets only Number/Title/State/Body/Author,
// so every row skips labelChip (WCAG contrast math per label), ciGlyph,
// reviewStateLabel and autoMergeGlyph, and every title measures the same width.
//
// That omission biases any frame-level profile: the hint/legend panel costs the
// same regardless of PR content, so on bare rows it looks like a larger share of
// the frame than it is in practice. Comparing BenchmarkParkedRender against
// BenchmarkParkedRenderRich is what tells us whether a hint-panel optimisation
// matters on realistic data or only on the synthetic fixture.
//
// Every PR stays OPEN on purpose: mixing MERGED/CLOSED would trip the state
// filter and change how many rows render, confounding "richer rows" with "fewer
// rows". Only content richness varies.
func richPRs(n int) []gh.PR {
	authors := []string{
		"octocat", "dependabot[bot]", "renovate[bot]", "a-longer-username",
		"kim", "jules-mcdev", "sam", "rt",
	}
	titles := []string{
		"fix: off-by-one",
		"feat(ui): show API rate-limit budget and reset countdown in the board header",
		"chore(deps): bump a handful of transitive dependencies to their latest patch releases and regenerate the lockfile",
		"docs: tidy",
		"refactor(gh): collapse the two rollup queries into one tiered request",
	}
	labelPool := []gh.Label{
		{Name: "enhancement", Color: "a2eeef"},
		{Name: "bug", Color: "d73a4a"},
		{Name: "good first issue", Color: "7057ff"},
		{Name: "dependencies", Color: "0366d6"},
		{Name: "needs-triage", Color: "fbca04"},
		{Name: "perf", Color: "0e8a16"},
	}
	reviews := []string{"", "APPROVED", "CHANGES_REQUESTED", "REVIEW_REQUIRED", "COMMENTED"}
	checkStates := []string{"SUCCESS", "FAILURE", "IN_PROGRESS", "SUCCESS", "QUEUED"}

	prs := make([]gh.PR, n)
	for i := range prs {
		p := &prs[i]
		p.Number = i + 1
		p.Title = titles[i%len(titles)]
		p.Author.Login = authors[i%len(authors)]
		p.State = "OPEN"
		p.ReviewDecision = reviews[i%len(reviews)]
		p.HeadRefName = fmt.Sprintf("feat/branch-name-%d", i)
		p.BaseRefName = "main"
		p.URL = fmt.Sprintf("https://github.com/owner/repo/pull/%d", i+1)
		p.UpdatedAt = time.Now().Add(-time.Duration(i) * 37 * time.Minute)
		p.IsDraft = i%7 == 0
		p.Body = "## Summary\n\nDoes a thing.\n\n- point one\n- point two\n\n```go\nfunc x() {}\n```\n"

		// 0–3 labels, so the empty, single and multi-chip paths all render.
		for j := range i % 4 {
			p.Labels = append(p.Labels, labelPool[(i+j)%len(labelPool)])
		}

		// Every 6th PR has no checks at all (CIState "none"); the rest carry
		// 1–8 checks whose mix drives pass/fail/pending.
		if i%6 != 0 {
			for j := range 1 + i%8 {
				p.StatusCheckRollup = append(p.StatusCheckRollup, gh.Check{
					State:        checkStates[(i+j)%len(checkStates)],
					Name:         fmt.Sprintf("test (%d)", j),
					WorkflowName: "CI",
				})
			}
		}

		if i%5 == 0 {
			p.AutoMergeRequest = &gh.AutoMergeRequest{MergeMethod: "SQUASH"}
		}
	}
	return prs
}

func richBoard(b *testing.B) Model {
	b.Helper()
	c := cache.Open(filepath.Join(b.TempDir(), "c.json"))
	m := NewModel("/repo", "is:open", c)
	m.SetRepo("owner/repo")
	u, _ := m.Update(tea.WindowSizeMsg{Width: 180, Height: 45})
	m = u.(Model)
	m.setPRs(richPRs(80))
	return m
}

// BenchmarkParkedRenderRich is BenchmarkParkedRender on realistic rows.
//
// The two come out equal, and that is the informative result: rows are rendered
// into m.rowText at setPRs/applyFilter time — before b.ResetTimer — and render()
// reuses them, so a parked frame excludes per-row cost by design. The remaining
// per-frame cost is genuinely non-row work (boxes, panels, hints, legend), which
// is why a hint-panel optimisation shows up at frame level at all.
func BenchmarkParkedRenderRich(b *testing.B) {
	m := richBoard(b)
	b.ResetTimer()
	for range b.N {
		_ = m.render()
	}
}

// BenchmarkScrollRenderRich is where row richness actually bites: a cursor move
// re-renders the rows whose focus flipped, so realistic labels, CI rollups and
// review states are paid per keystroke. Compared against BenchmarkScrollRender it
// shows whether a hint-panel win holds up once rows cost what they really cost.
func BenchmarkScrollRenderRich(b *testing.B) {
	m := richBoard(b)
	b.ResetTimer()
	for i := range b.N {
		if i%2 == 0 {
			m.moveCursor(1)
		} else {
			m.moveCursor(-1)
		}
		_ = m.render()
	}
}
