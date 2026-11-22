package ui

import (
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/noamsto/prdash/internal/gh"
	"github.com/noamsto/prdash/internal/triage"
)

func TestRenderCardShowsHeadlineAndAction(t *testing.T) {
	c := triage.Card{Kind: triage.KindChecksFailing, Headline: "2 checks failing",
		Failing: []triage.FailedCheck{{Label: "lint"}, {Label: "e2e"}}, ActionKey: "r", ActionLabel: "rerun checks"}
	out := renderCard(c, 40, time.Now())
	if !strings.Contains(out, "2 checks failing") {
		t.Fatalf("headline missing: %q", out)
	}
	if !strings.Contains(out, "lint") || !strings.Contains(out, "e2e") {
		t.Fatalf("failing checks missing: %q", out)
	}
	if !strings.Contains(out, "r") || !strings.Contains(out, "rerun checks") {
		t.Fatalf("suggested action missing: %q", out)
	}
}

func TestRenderCardShowsFailingAndRunningGlyphs(t *testing.T) {
	c := triage.Card{Kind: triage.KindChecksFailing, Headline: "1 failing · 1 running",
		Failing: []triage.FailedCheck{{Label: "lint"}}, Running: []triage.RunningCheck{{Label: "build"}},
		ActionKey: "r", ActionLabel: "rerun checks"}
	out := renderCard(c, 40, time.Now())
	if !strings.Contains(out, "✗ lint") {
		t.Fatalf("failing glyph/label missing: %q", out)
	}
	if !strings.Contains(out, ciRunningGlyph+" build") {
		t.Fatalf("running glyph/label missing: %q", out)
	}
}

func TestRenderCardRunningOnlyHasNoFailGlyph(t *testing.T) {
	c := triage.Card{Kind: triage.KindChecksRunning, Headline: "Checks running…",
		Running: []triage.RunningCheck{{Label: "build"}}}
	out := renderCard(c, 40, time.Now())
	if !strings.Contains(out, ciRunningGlyph+" build") {
		t.Fatalf("running glyph/label missing: %q", out)
	}
	if strings.Contains(out, "✗") {
		t.Fatalf("unexpected fail glyph on running-only card: %q", out)
	}
}

func TestRenderCardReadyNoAction(t *testing.T) {
	out := renderCard(triage.Card{Kind: triage.KindReady, Headline: "Ready to merge",
		ActionKey: "m", ActionLabel: "merge (squash)"}, 40, time.Now())
	if !strings.Contains(out, "Ready to merge") {
		t.Fatalf("headline missing: %q", out)
	}
}

func TestRenderCardShowsAutoMergeLine(t *testing.T) {
	c := triage.Card{Kind: triage.KindReady, Headline: "Ready to merge",
		ActionKey: "m", ActionLabel: "merge (squash)", AutoMerge: true}
	out := renderCard(c, 40, time.Now())
	if !strings.Contains(out, "auto-merge armed") {
		t.Fatalf("auto-merge line missing: %q", out)
	}
}

func TestRenderCardOmitsAutoMergeLineWhenNotArmed(t *testing.T) {
	c := triage.Card{Kind: triage.KindReady, Headline: "Ready to merge",
		ActionKey: "m", ActionLabel: "merge (squash)"}
	out := renderCard(c, 40, time.Now())
	if strings.Contains(out, "auto-merge armed") {
		t.Fatalf("auto-merge line should not appear: %q", out)
	}
}

func assertCardFits(t *testing.T, out string, width int) {
	t.Helper()
	for line := range strings.SplitSeq(out, "\n") {
		if w := lipgloss.Width(line); w > width {
			t.Fatalf("line %q is %d columns, want <= %d", ansi.Strip(line), w, width)
		}
	}
}

func TestRenderCardFailingShowsTook(t *testing.T) {
	c := triage.Card{Kind: triage.KindChecksFailing, Headline: "1 check failing",
		Failing: []triage.FailedCheck{{Label: "lint", StartedAt: "2026-01-01T10:00:00Z", CompletedAt: "2026-01-01T10:01:20Z"}}}
	out := renderCard(c, 40, time.Now())
	assertCardFits(t, out, 40)
	if got := ansi.Strip(out); !strings.Contains(got, "✗ lint · took 1m 20s") {
		t.Fatalf("took suffix missing: %q", got)
	}
}

func TestRenderCardFailingWithoutStampsHasNoSuffix(t *testing.T) {
	c := triage.Card{Kind: triage.KindChecksFailing, Headline: "2 checks failing",
		Failing: []triage.FailedCheck{
			{Label: "lint"},
			{Label: "e2e", StartedAt: "2026-01-01T10:00:00Z", CompletedAt: "2026-01-01T10:00:00Z"},
		}}
	out := renderCard(c, 40, time.Now())
	assertCardFits(t, out, 40)
	got := ansi.Strip(out)
	for _, label := range []string{"lint", "e2e"} {
		if !strings.Contains(got, "✗ "+label) {
			t.Fatalf("label %q missing: %q", label, got)
		}
	}
	if strings.Contains(got, "took") || strings.Contains(got, "·") {
		t.Fatalf("unexpected timing suffix: %q", got)
	}
}

func TestRenderCardFailingTimingDroppedAtNarrowWidth(t *testing.T) {
	const width = 14
	c := triage.Card{Kind: triage.KindChecksFailing, Headline: "1 failing",
		Failing: []triage.FailedCheck{{Label: "integration-tests", StartedAt: "2026-01-01T10:00:00Z", CompletedAt: "2026-01-01T10:01:20Z"}}}
	out := renderCard(c, width, time.Now())
	assertCardFits(t, out, width)
	got := ansi.Strip(out)
	if strings.Contains(got, "took") {
		t.Fatalf("timing should be dropped at width %d: %q", width, got)
	}
	if !strings.Contains(got, "✗ integrati") {
		t.Fatalf("truncated label missing: %q", got)
	}
}

func TestCountStagesFromPreliminary(t *testing.T) {
	pass := []gh.Check{{State: "SUCCESS"}}
	prs := []gh.PR{
		{Number: 1, StatusCheckRollup: []gh.Check{{State: "FAILURE"}}},
		{Number: 2, ReviewDecision: "CHANGES_REQUESTED", StatusCheckRollup: pass},
		{Number: 3, StatusCheckRollup: []gh.Check{{State: "PENDING"}}},
		{Number: 4, ReviewDecision: "REVIEW_REQUIRED", StatusCheckRollup: pass},
		{Number: 5, IsDraft: true, StatusCheckRollup: pass},
		{Number: 6, ReviewDecision: "APPROVED", StatusCheckRollup: pass},
		{Number: 7, ReviewDecision: "APPROVED"},
		{Number: 8, StatusCheckRollup: pass},
		{Number: 9, Stack: &gh.PRStack{}, StackPosition: 2, StatusCheckRollup: pass},
	}
	parent := func(p gh.PR) int {
		if p.Stack != nil {
			return 4
		}
		return 0
	}
	c := countStages(prs, "viewer", parent)
	want := map[stageBucket]int{
		stageFailing: 2, stageRunning: 1, stageReview: 1, stageBlocked: 1,
		stageDraft: 1, stageReady: 2, stageOther: 1,
	}
	for i, n := range c {
		b := stageBucket(i)
		if n != want[b] {
			t.Errorf("bucket %s = %d, want %d", stageLabel(b), n, want[b])
		}
	}
	if c.total() != 9 {
		t.Fatalf("total = %d, want 9", c.total())
	}
}

func TestRenderRepoHeader(t *testing.T) {
	c := stageCounts{stageFailing: 1, stageRunning: 1, stageReady: 1}
	head := "owner/repo  3 · ✗ 1 " + ciRunningGlyph + " 1 ✓ 1"
	if got := ansi.Strip(renderRepoHeader("owner/repo", c, 80, false)); !strings.HasPrefix(got, head) ||
		lipgloss.Width(got) != 80 || !strings.HasSuffix(got, "─") {
		t.Fatalf("wide header = %q, want %q plus a rule filling 80", got, head)
	}
	for w := 12; w <= 120; w++ {
		out := renderRepoHeader("owner/repo", c, w, true)
		if lipgloss.Width(out) > w {
			t.Fatalf("width %d: header is %d wide: %q", w, lipgloss.Width(out), ansi.Strip(out))
		}
		if !strings.HasPrefix(ansi.Strip(out), "owner/repo") {
			t.Fatalf("width %d: repo name dropped: %q", w, ansi.Strip(out))
		}
	}
	if got := ansi.Strip(renderRepoHeader("owner/repo", c, 14, false)); got != "owner/repo  3" {
		t.Fatalf("width 14 = %q, want %q", got, "owner/repo  3")
	}
	if got := ansi.Strip(renderRepoHeader("owner/repo", c, 11, false)); got != "owner/repo" {
		t.Fatalf("width 11 = %q, want %q", got, "owner/repo")
	}
}

func TestRenderRepoHeaderSpacesGlyphFromCount(t *testing.T) {
	c := stageCounts{stageFailing: 2, stageReady: 3}
	fail := ansi.Strip(stageGlyph(stageFailing))
	ready := ansi.Strip(stageGlyph(stageReady))
	wide := ansi.Strip(renderRepoHeader("owner/repo", c, 80, false))
	if !strings.HasPrefix(wide, "owner/repo  5 · "+fail+" 2 "+ready+" 3") {
		t.Fatalf("wide header = %q, want glyph and count spaced", wide)
	}
	// 19 columns fit "repo  5 · ✗ 2" but not the second bucket, so it sheds right to left.
	narrow := ansi.Strip(renderRepoHeader("owner/repo", c, 19, false))
	if narrow != "owner/repo  5 · "+fail+" 2" {
		t.Fatalf("width 19 = %q, want first bucket only", narrow)
	}
}

func TestRenderRepoName(t *testing.T) {
	for _, repo := range []string{"owner/repo", "solo", "a/b/c"} {
		if got := ansi.Strip(renderRepoName(repo)); got != repo {
			t.Errorf("renderRepoName(%q) = %q, want the text unchanged", repo, got)
		}
	}
	// The owner recedes and the name carries the hue, so groups that share an
	// owner still read apart at a glance.
	if got := renderRepoName("owner/repo"); !strings.Contains(got, repoOwnerStyle.Render("owner/")) ||
		!strings.Contains(got, repoNameStyle.Render("repo")) {
		t.Errorf("repo header is not two-tone: %q", got)
	}
	if got := renderRepoName("solo"); !strings.Contains(got, repoNameStyle.Render("solo")) {
		t.Errorf("a repo without an owner lost its hue: %q", got)
	}
}

func TestRenderCardShowsAlt(t *testing.T) {
	c := triage.Card{Kind: triage.KindChecksFailing, Headline: "1 check failing",
		ActionKey: "r", ActionLabel: "rerun checks", Alt: "d: ask Dependabot to rebase if the failure is stale"}
	if out := renderCard(c, 60, time.Now()); !strings.Contains(out, "ask Dependabot to rebase") {
		t.Fatalf("alt hint missing: %q", out)
	}
}
