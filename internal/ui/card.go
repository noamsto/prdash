package ui

import (
	"strconv"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/noamsto/prdash/internal/gh"
	"github.com/noamsto/prdash/internal/triage"
)

// cardGlyph picks a leading glyph + style for the card's kind.
func cardGlyph(k triage.Kind) string {
	switch k {
	case triage.KindReady:
		return passStyle.Render("✓")
	case triage.KindChecksFailing, triage.KindConflict, triage.KindChangesRequested:
		return failStyle.Render("✗")
	case triage.KindChecksRunning, triage.KindPending:
		return pendStyle.Render(ciRunningGlyph)
	default:
		return dimStyle.Render("•")
	}
}

// stageBucket is one segment of a repo header's stage strip, in display order.
type stageBucket int

const (
	stageFailing stageBucket = iota
	stageRunning
	stageReview
	stageBlocked
	stageDraft
	stageReady
	stageOther
	stageBucketCount
)

type stageCounts [stageBucketCount]int

func (c stageCounts) total() int {
	n := 0
	for _, v := range c {
		n += v
	}
	return n
}

// headerBucket maps a preliminary triage kind to its header bucket. Only an
// approved Fallback with passing or absent CI counts as ready.
func headerBucket(p gh.PR, k triage.Kind) stageBucket {
	switch k {
	case triage.KindChecksFailing, triage.KindChangesRequested:
		return stageFailing
	case triage.KindChecksRunning:
		return stageRunning
	case triage.KindAwaitingReview:
		return stageReview
	case triage.KindBlocked:
		return stageBlocked
	case triage.KindDraft:
		return stageDraft
	case triage.KindFallback:
		if ci := p.CIState(); p.ReviewDecision == "APPROVED" && (ci == "pass" || ci == "none") {
			return stageReady
		}
		return stageOther
	default:
		return stageOther
	}
}

func stageGlyph(b stageBucket) string {
	switch b {
	case stageFailing:
		return cardGlyph(triage.KindChecksFailing)
	case stageRunning:
		return cardGlyph(triage.KindChecksRunning)
	case stageReview:
		return pendStyle.Render("●")
	case stageBlocked:
		return dimStyle.Render("⧉")
	case stageDraft:
		return dimStyle.Render(draftGlyph)
	case stageReady:
		return cardGlyph(triage.KindReady)
	default:
		return cardGlyph(triage.KindFallback)
	}
}

func stageLabel(b stageBucket) string {
	return [...]string{"failing", "running", "review", "blocked", "draft", "ready", "other"}[b]
}

func countStages(prs []gh.PR, viewer string, parent func(gh.PR) int) stageCounts {
	var c stageCounts
	for _, p := range prs {
		c[headerBucket(p, triage.Preliminary(p, viewer, parent(p)).Kind)]++
	}
	return c
}

// renderRepoHeader lays out "repo  N · glyph count ..." within width, shedding
// bucket counts right to left, then the separator, then the total, then
// truncating the repo name. A rule fills the rest of the line so each repo
// group opens on a divider, unlike the shorter rule under a category header.
func renderRepoHeader(repo string, c stageCounts, width int, focused bool) string {
	if width <= 0 {
		return ""
	}
	parts := make([]string, 0, stageBucketCount)
	for i, n := range c {
		if n > 0 {
			parts = append(parts, stageGlyph(stageBucket(i))+strconv.Itoa(n))
		}
	}
	total := "  " + strconv.Itoa(c.total())
	const sep = " · "
	name := sectionLabelStyle.Render(repo)
	var line string
	switch {
	case lipgloss.Width(name+total) > width:
		line = truncate(name, width)
	default:
		line = name + total
		for n := len(parts); n > 0; n-- {
			cand := line + sep + strings.Join(parts[:n], " ")
			if lipgloss.Width(cand) <= width {
				line = cand
				break
			}
		}
	}
	line = rulePad(line, width)
	if !focused {
		return line
	}
	line += strings.Repeat(" ", max(width-lipgloss.Width(line), 0))
	return rowBgWrap(line, theme.RowBg)
}

// rulePad fills the remainder of a header line with a separator rule.
func rulePad(line string, width int) string {
	n := width - lipgloss.Width(line) - 1
	if n < 1 {
		return line
	}
	return line + " " + sepStyle.Render(strings.Repeat("─", n))
}

// runningRow is one in-flight check: glyph, label and elapsed time. Shared by the
// triage card and the Overview checks section so both read the same.
func runningRow(label, startedAt string, width int, now time.Time) string {
	text, timing := timedLabel(label, elapsedSince(startedAt, now), width-4)
	return "  " + pendStyle.Render(ciRunningGlyph+" "+text) + timing
}

// renderCard renders the triage card: glyph + headline, any detail lines, and
// the suggested action. Empty headline (fallback) renders nothing.
func renderCard(c triage.Card, width int, now time.Time) string {
	if c.Headline == "" {
		return ""
	}
	var b strings.Builder
	b.WriteString(cardGlyph(c.Kind) + " " + headerStyle.Render(c.Headline) + "\n")
	for _, l := range c.Failing {
		label, timing := timedLabel(l.Label, tookTiming(l.StartedAt, l.CompletedAt), width-4)
		b.WriteString("  " + failStyle.Render("✗ "+label) + timing + "\n")
	}
	for _, l := range c.Running {
		b.WriteString(runningRow(l.Label, l.StartedAt, width, now) + "\n")
	}
	if c.ActionKey != "" {
		b.WriteString(dimStyle.Render(c.ActionLabel+" → ") + accentStyle.Render(c.ActionKey) + "\n")
	}
	if c.AutoMerge {
		b.WriteString("  " + autoMergeGlyph(true) + " " + dimStyle.Render("auto-merge armed") + "\n")
	}
	return b.String()
}
