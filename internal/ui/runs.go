package ui

import (
	"cmp"
	"slices"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/sahilm/fuzzy"

	"github.com/noamsto/prdash/internal/action"
	"github.com/noamsto/prdash/internal/gh"
)

// RunSection is the Runs board: repo-wide workflow runs, live ones first.
// It is deliberately not `numbered`: a run has no item number the cursor
// anchor could follow, so the board anchors on run IDs instead.
type RunSection struct {
	filter, state string
	runs          []gh.WorkflowRun
	shown         []int
	held          map[int64]bool // rows a poll dropped; dim and frozen until a requested replace
	pinned        map[int64]bool // rows the active/failed view has shown since the last replace
	grouped       bool
	forceFlat     bool

	workflowW, branchW, eventW, actorW int // widest shown value per column, capped
}

var _ Section = (*RunSection)(nil)
var _ grouper = (*RunSection)(nil)

const (
	runWorkflowMax = 24
	runBranchMax   = 24
	runEventMax    = 8
	runActorMax    = 14
	runMinTitle    = 8
	runInitialsW   = 2
	// runTimeCells reserves the time column for the longest live elapsed
	// ("1h 15m"), so a row's columns don't move when its time text changes.
	runTimeCells = 7
)

func NewRunSection(filter, state string) *RunSection {
	return &RunSection{filter: filter, state: state}
}

func (s *RunSection) Kind() string       { return "run" }
func (s *RunSection) Filter() string     { return s.filter }
func (s *RunSection) Len() int           { return len(s.shown) }
func (s *RunSection) SetState(st string) { s.state = st }

func (s *RunSection) runAt(i int) gh.WorkflowRun { return s.runs[s.shown[i]] }

func (s *RunSection) SetRuns(runs []gh.WorkflowRun, held map[int64]bool) {
	if runs == nil {
		runs = []gh.WorkflowRun{}
	}
	slices.SortStableFunc(runs, func(a, b gh.WorkflowRun) int {
		if c := b.CreatedAt.Compare(a.CreatedAt); c != 0 {
			return c
		}
		return cmp.Compare(b.ID, a.ID)
	})
	s.runs = runs
	s.held = held
	s.SetShown(allIdx(len(runs)))
}

func (s *RunSection) resetView() {
	s.held, s.pinned = nil, nil
}

// dropHeld forgets the rows a poll dropped, for a requested replace.
func (s *RunSection) dropHeld() {
	s.runs = slices.DeleteFunc(s.runs, func(r gh.WorkflowRun) bool { return s.held[r.ID] })
	s.resetView()
	s.SetShown(allIdx(len(s.runs)))
}

func (s *RunSection) isHeld(id int64) bool { return s.held[id] }

func (s *RunSection) live(r gh.WorkflowRun) bool { return r.Active() && !s.isHeld(r.ID) }

func (s *RunSection) anyLive() bool { return slices.ContainsFunc(s.runs, s.live) }

func (s *RunSection) shownIDs() []int64 {
	ids := make([]int64, len(s.shown))
	for i, ix := range s.shown {
		ids[i] = s.runs[ix].ID
	}
	return ids
}

func (s *RunSection) isGrouped() bool { return s.grouped }

func (s *RunSection) groupLabelAt(i int) string {
	if s.live(s.runAt(i)) {
		return "Active"
	}
	return "Recent"
}

func (s *RunSection) unitLabelAt(i int) string { return s.groupLabelAt(i) }

func (s *RunSection) SetShown(idx []int) {
	liveAt := func(ix int) bool { return s.live(s.runs[ix]) }
	s.grouped = s.state == "recent" && !s.forceFlat && slices.ContainsFunc(idx, liveAt)
	s.shown = slices.Clone(idx)
	if s.grouped {
		// Stable partition: live rows first, order kept within each half.
		slices.SortStableFunc(s.shown, func(a, b int) int {
			switch la, lb := liveAt(a), liveAt(b); {
			case la == lb:
				return 0
			case la:
				return -1
			default:
				return 1
			}
		})
	}
	s.workflowW, s.branchW, s.eventW, s.actorW = 0, 0, 0, 0
	for _, ix := range s.shown {
		r := s.runs[ix]
		s.workflowW = min(runWorkflowMax, max(s.workflowW, lipgloss.Width(r.Name)))
		s.branchW = min(runBranchMax, max(s.branchW, lipgloss.Width(r.HeadBranch)))
		s.eventW = min(runEventMax, max(s.eventW, lipgloss.Width(runEventLabel(r.Event))))
		s.actorW = min(runActorMax, max(s.actorW, lipgloss.Width(r.Actor)))
	}
}

func (s *RunSection) Haystacks() []string {
	h := make([]string, len(s.runs))
	for i, r := range s.runs {
		h[i] = runHaystack(r)
	}
	return h
}

func runHaystack(r gh.WorkflowRun) string {
	return strings.Join([]string{r.Name, r.DisplayTitle, r.HeadBranch, r.Event, r.Actor, r.Status, r.Conclusion}, " ")
}

func (s *RunSection) VarsAt(i int) action.Vars {
	r := s.runAt(i)
	return action.Vars{Number: r.RunNumber, Title: r.DisplayTitle, Author: r.Actor,
		URL: r.HTMLURL, HeadRefName: r.HeadBranch, Branch: r.HeadBranch}
}

func runFailed(r gh.WorkflowRun) bool {
	switch r.Conclusion {
	case "failure", "timed_out", "startup_failure":
		return true
	}
	return false
}

var runQualifiers = map[string]bool{"branch": true, "workflow": true, "event": true, "actor": true}

// parseRunQuery splits a filter into qualifier terms (branch:, workflow:,
// event:, actor:, @login) and the remaining bare fuzzy text. An unknown
// `foo:bar` stays bare.
func parseRunQuery(query string) (quals map[string][]string, bare string) {
	var rest []string
	for tok := range strings.FieldsSeq(query) {
		if login, ok := strings.CutPrefix(tok, "@"); ok {
			tok = "actor:" + login
		}
		if key, val, ok := strings.Cut(tok, ":"); ok && runQualifiers[key] {
			if quals == nil {
				quals = map[string][]string{}
			}
			quals[key] = append(quals[key], strings.ToLower(val))
			continue
		}
		rest = append(rest, tok)
	}
	return quals, strings.Join(rest, " ")
}

// runQualFields are the values a qualifier matches: the event also matches the
// label the row shows.
func runQualFields(r gh.WorkflowRun, key string) []string {
	switch key {
	case "branch":
		return []string{r.HeadBranch}
	case "workflow":
		return []string{r.Name}
	case "event":
		return []string{r.Event, runEventLabel(r.Event)}
	}
	return []string{r.Actor}
}

// matchQuery applies the state view, the qualifiers and the bare fuzzy text,
// returning indices into runs. Rows the active/failed view shows are pinned so
// a run that finishes under the cursor stays until the view is reset.
func (s *RunSection) matchQuery(query string) []int {
	quals, bare := parseRunQuery(query)
	s.forceFlat = bare != ""
	cands := make([]int, 0, len(s.runs))
	for i, r := range s.runs {
		ok := true
		for key, vals := range quals {
			fields := runQualFields(r, key)
			for _, v := range vals {
				ok = ok && slices.ContainsFunc(fields, func(f string) bool {
					return strings.Contains(strings.ToLower(f), v)
				})
			}
		}
		if !ok {
			continue
		}
		pass := s.state == "recent" || (s.state == "active" && r.Active()) || (s.state == "failed" && runFailed(r))
		if pass && s.state != "recent" {
			if s.pinned == nil {
				s.pinned = map[int64]bool{}
			}
			s.pinned[r.ID] = true
		}
		if pass || s.pinned[r.ID] {
			cands = append(cands, i)
		}
	}
	if bare == "" {
		return cands
	}
	hay := make([]string, len(cands))
	for i, ix := range cands {
		hay[i] = runHaystack(s.runs[ix])
	}
	matches := fuzzy.Find(bare, hay)
	idx := make([]int, 0, len(matches))
	for _, m := range matches {
		idx = append(idx, cands[m.Index])
	}
	return idx
}

func runEventLabel(event string) string {
	switch event {
	case "pull_request":
		return "pr"
	case "pull_request_target":
		return "pr-t"
	case "workflow_dispatch":
		return "manual"
	case "schedule":
		return "cron"
	}
	return event
}

// padCell right-pads plain text to exactly w cells, truncating first.
func padCell(txt string, w int) string {
	txt = ansi.Truncate(txt, w, "…")
	return txt + strings.Repeat(" ", max(0, w-lipgloss.Width(txt)))
}

// RenderRow lays out one run as
//
//	‹bar›‹glyph› ‹workflow›  ‹branch›  ‹actor›  ‹event›  ‹title…›  ‹time›
//
// by hand: every cell is sized from the budget, so the row is exactly w cells
// wide. Optional columns drop, in reverse order, when they don't fit.
func (s *RunSection) RenderRow(i int, o RowOpts) string {
	r := s.runAt(i)
	w := max(o.Width, 24)
	held := o.Held || s.isHeld(r.ID)

	bar := " "
	if o.Focused {
		bar = focusBarStyle.Render(focusBarGlyph)
	}
	left := bar + oneCell(runStatusGlyph(r.Status, r.Conclusion)) + " "

	var timeTxt string
	if s.live(r) && !o.Now.IsZero() {
		start := r.RunStartedAt
		if start.IsZero() {
			start = r.CreatedAt
		}
		timeTxt = pendStyle.Render(fmtElapsed(o.Now.Sub(start)))
	} else {
		timeTxt = dimStyle.Render(ageString(r.UpdatedAt))
	}
	timeW := 2 + max(runTimeCells, lipgloss.Width(timeTxt))
	right := strings.Repeat(" ", timeW-lipgloss.Width(timeTxt)) + timeTxt

	rem := w - lipgloss.Width(left) - timeW
	wfW := min(s.workflowW, rem)
	wfSt := titleStyle
	if o.Focused {
		wfSt = wfSt.Bold(true)
	}
	var body strings.Builder
	body.WriteString(wfSt.Render(padCell(r.Name, wfW)))
	rem -= wfW

	actorW := s.actorW
	actor := r.Actor
	if w < 60 {
		actorW, actor = runInitialsW, authorInitials(r.Actor)
	}
	cols := []struct {
		width int
		cell  func(w int) string
	}{
		{s.branchW, func(w int) string { return dimStyle.Render(padCell(r.HeadBranch, w)) }},
		{actorW, func(w int) string { return authorStyle(r.Actor).Render(padCell(actor, w)) }},
		{s.eventW, func(w int) string { return dimStyle.Render(padCell(runEventLabel(r.Event), w)) }},
	}
	for _, c := range cols {
		if c.width == 0 {
			continue
		}
		if rem < 2+c.width {
			break
		}
		body.WriteString("  " + c.cell(c.width))
		rem -= 2 + c.width
	}
	if rem-2 >= runMinTitle {
		body.WriteString("  " + titleStyle.Render(padCell(r.DisplayTitle, rem-2)))
		rem = 0
	}
	body.WriteString(strings.Repeat(" ", rem))

	line := left + body.String() + right
	switch {
	case o.Focused:
		line = rowBgWrap(line, theme.RowBg)
	case held:
		line = faintWrap(line)
	}
	return line
}
