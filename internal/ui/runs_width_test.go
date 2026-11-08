package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/noamsto/prdash/internal/gh"
)

// VS16 emoji ("❤️") are one rune plus a selector but two cells; a rune-counting
// truncate lets them run past the pane.
func TestRunRowAndPreviewFitWithVS16Emoji(t *testing.T) {
	// Scaled so even the zoomed 160-column pane (156 content cells) overflows.
	hearts := func(n int) string { return strings.Repeat("❤️", 3*n) }
	run := testRun(7, "❤️ deploy "+hearts(30), "in_progress", "", 3*time.Minute)
	run.DisplayTitle = hearts(40)
	run.HeadBranch = "feat/" + hearts(30)
	run.Actor = hearts(20)
	run.HeadSHA = "0123456789abcdef"
	jobs := []gh.RunJob{
		doneJob(1, "lint "+hearts(30), "success", 90*time.Second),
		doneJob(2, hearts(40), "failure", 2*time.Minute),
		runningJob(3, "unit-tests "+hearts(30)),
	}
	jobs[2].Steps = []gh.RunJobStep{{Name: hearts(40), Status: "in_progress"}}
	jobs = append(jobs, gh.RunJob{ID: 4, Name: hearts(40), Status: "queued"})

	for _, tc := range []struct {
		width int
		zoom  bool
	}{{160, false}, {160, true}, {100, true}, {60, true}} {
		t.Run(fmt.Sprintf("%d/zoom=%v", tc.width, tc.zoom), func(t *testing.T) {
			m := resize(runsBoard(t, &fakeRunsSource{}, run), tc.width, 40)
			m = withJobs(m, run, jobs...)
			if tc.zoom {
				m, _ = update(m, keyMsg("z"))
			}
			if !m.previewShown() {
				t.Fatal("setup: no preview is visible")
			}

			rs := m.section.(*RunSection)
			inner := m.layout().ListInner
			if row := rs.RenderRow(0, RowOpts{Width: inner}); lipgloss.Width(row) > inner {
				t.Errorf("list row is %d cells, list is %d: %q", lipgloss.Width(row), inner, ansi.Strip(row))
			}

			pane := min(m.previewWidth(), m.width-2)
			head, body := m.previewParts()
			for i, ln := range strings.Split(head, "\n") {
				if got := lipgloss.Width(ln); got > pane-2 {
					t.Errorf("head line %d is %d cells, content is %d: %q", i, got, pane-2, ansi.Strip(ln))
				}
			}
			for i, ln := range strings.Split(body, "\n") {
				limit := pane - 2
				if i == 0 {
					limit = pane // the jobs section header spans the pane
				}
				if got := lipgloss.Width(ln); got > limit {
					t.Errorf("body line %d is %d cells, content is %d: %q", i, got, limit, ansi.Strip(ln))
				}
			}
		})
	}
}
