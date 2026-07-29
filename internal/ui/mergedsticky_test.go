package ui

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/noamsto/prdash/internal/gh"
)

func openPR(number int, author string) gh.PR {
	p := gh.PR{Number: number, Title: "pr " + author, State: "OPEN", HeadRefName: "feat/x"}
	p.Author.Login = author
	return p
}

func mergedPR(number int, author string) gh.PR {
	p := openPR(number, author)
	p.State, p.MergedAt = "MERGED", time.Now().Add(-2*time.Minute)
	return p
}

func TestApplyMergedStickyAppendsWhatTheFetchDropped(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.mergedSticky[61] = mergedPR(61, "alice")

	got := m.applyMergedSticky([]gh.PR{openPR(62, "bob")})

	if len(got) != 2 {
		t.Fatalf("len = %d, want the fetched PR plus the sticky one:\n%+v", len(got), got)
	}
	if got[1].Number != 61 || !got[1].IsMerged() {
		t.Errorf("appended PR = %+v, want merged #61", got[1])
	}
}

func TestApplyMergedStickyNoDuplicate(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.mergedSticky[61] = mergedPR(61, "alice")

	got := m.applyMergedSticky([]gh.PR{mergedPR(61, "alice"), openPR(62, "bob")})

	if len(got) != 2 {
		t.Fatalf("len = %d, want no duplicate of the PR the fetch already returned:\n%+v", len(got), got)
	}
}

// The merged board returns these PRs on its own, and on the closed board
// (is:unmerged) a merged row would be a lie.
func TestApplyMergedStickyOnlyOnTheOpenPRBoard(t *testing.T) {
	tests := []struct {
		name  string
		setup func(m *Model)
	}{
		{"merged state", func(m *Model) { m.state = "merged" }},
		{"closed state", func(m *Model) { m.state = "closed" }},
		{"issue board", func(m *Model) { m.mode = "issue" }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := NewModel("/repo", "is:open", nil)
			m.mergedSticky[61] = mergedPR(61, "alice")
			tc.setup(&m)

			if got := m.applyMergedSticky([]gh.PR{openPR(62, "bob")}); len(got) != 1 {
				t.Errorf("len = %d, want the fetched PR alone:\n%+v", len(got), got)
			}
		})
	}
}

func TestMergeSuccessMakesThePRSticky(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.SetRepo("owner/repo")
	m.width, m.height = 100, 40
	m.setPRs([]gh.PR{openPR(61, "alice")})
	m.ciRerun[61] = time.Now() // a rerun stamp from before the merge
	m.actionStatus = &actionStat{run: "Merging", ok: "Merged", merged: []gh.PR{openPR(61, "alice")}}

	updated, _ := m.Update(actionDoneMsg{})
	m = updated.(Model)

	sticky, ok := m.mergedSticky[61]
	if !ok {
		t.Fatal("merged PR was not made sticky")
	}
	if !sticky.IsMerged() {
		t.Errorf("sticky PR state = %q, want MERGED", sticky.State)
	}
	if sticky.MergedAt.IsZero() {
		t.Error("sticky PR has no MergedAt, so its row would show no age")
	}
	if _, stamped := m.ciRerun[61]; stamped {
		t.Error("a merged PR kept its ciRerun stamp; its row would paint checks-in-progress")
	}
}

func TestFailedMergeMakesNothingSticky(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.SetRepo("owner/repo")
	m.width, m.height = 100, 40
	m.actionStatus = &actionStat{run: "Merging", fail: "Merge failed", merged: []gh.PR{openPR(61, "alice")}}

	updated, _ := m.Update(actionDoneMsg{err: errors.New("not mergeable")})
	m = updated.(Model)

	if len(m.mergedSticky) != 0 {
		t.Errorf("mergedSticky = %+v, want empty after a failed merge", m.mergedSticky)
	}
}

// A landed row survives every automatic refetch; only ctrl+r drops it.
func TestStickyRowSurvivesRefetchAndClearsOnManualRefresh(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.SetRepo("owner/repo")
	m.width, m.height = 100, 40
	m.mergedSticky[61] = mergedPR(61, "alice")

	m.setPRs([]gh.PR{openPR(62, "bob")}) // the fetch no longer returns #61
	if m.section.Len() != 2 {
		t.Fatalf("shown rows = %d, want the landed PR to still be on the board", m.section.Len())
	}

	m.backgroundRefresh() // post-action / CI-poll path
	if len(m.mergedSticky) != 1 {
		t.Errorf("an automatic refresh cleared mergedSticky = %+v", m.mergedSticky)
	}

	updated, _ := m.Update(tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
	m = updated.(Model)
	if len(m.mergedSticky) != 0 {
		t.Errorf("mergedSticky = %+v, want ctrl+r to clear it", m.mergedSticky)
	}
	m.setPRs([]gh.PR{openPR(62, "bob")})
	if m.section.Len() != 1 {
		t.Errorf("shown rows = %d, want the landed PR gone after ctrl+r", m.section.Len())
	}
}

func TestLandedTagOnlyOnStickyRows(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.SetRepo("owner/repo")
	m.width, m.height = 100, 40
	m.mergedSticky[61] = mergedPR(61, "alice")
	m.setPRs([]gh.PR{openPR(62, "bob")})
	m.renderList()

	var landed, plain string
	for i := range m.rowText {
		if strings.Contains(ansi.Strip(m.rowText[i]), "#61") {
			landed = ansi.Strip(m.rowText[i])
		}
		if strings.Contains(ansi.Strip(m.rowText[i]), "#62") {
			plain = ansi.Strip(m.rowText[i])
		}
	}
	if !strings.Contains(landed, "landed") {
		t.Errorf("sticky row lacks the landed tag: %q", landed)
	}
	if strings.Contains(plain, "landed") {
		t.Errorf("an ordinary open row carries the landed tag: %q", plain)
	}
}

// The row cache is keyed by rowKey; without landed in the key it would keep
// serving the pre-merge row.
func TestLandedIsPartOfTheRowCacheKey(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.SetRepo("owner/repo")
	m.width, m.height = 100, 40
	m.setPRs([]gh.PR{openPR(61, "alice")})
	m.renderList()
	before := ansi.Strip(m.rowText[0])

	m.mergedSticky[61] = mergedPR(61, "alice")
	m.setPRs([]gh.PR{mergedPR(61, "alice")})
	m.renderList()

	if after := ansi.Strip(m.rowText[0]); after == before {
		t.Errorf("row was served from cache unchanged after landing: %q", after)
	}
}
