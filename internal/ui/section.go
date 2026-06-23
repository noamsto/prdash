package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/noamsto/prdash/internal/action"
	"github.com/noamsto/prdash/internal/gh"
	"github.com/noamsto/prdash/internal/issue"
)

// RowOpts controls how a section renders one row.
type RowOpts struct {
	Width    int
	Focused  bool
	Selected bool
}

type Section interface {
	Kind() string
	Filter() string
	RenderRow(i int, o RowOpts) string // render shown-row i as an airy 2-line block
	Len() int
	VarsAt(i int) action.Vars
	Haystacks() []string
	SetShown(idx []int)
}

// --- PR section ---
type PRSection struct {
	filter string
	prs    []gh.PR
	shown  []int
}

func NewPRSection(filter string) *PRSection { return &PRSection{filter: filter} }
func (s *PRSection) Kind() string           { return "pr" }
func (s *PRSection) Filter() string         { return s.filter }
func (s *PRSection) SetPRs(p []gh.PR)       { s.prs = p; s.shown = allIdx(len(p)) }
func (s *PRSection) Len() int               { return len(s.shown) }
func (s *PRSection) SetShown(idx []int)     { s.shown = idx }

// prAt returns the gh.PR at shown-row i (for triage, which needs list fields).
func (s *PRSection) prAt(i int) gh.PR { return s.prs[s.shown[i]] }

func (s *PRSection) RenderRow(i int, o RowOpts) string {
	p := s.prs[s.shown[i]]
	return renderItemRow(o, fmt.Sprintf("#%d", p.Number), p.Title,
		p.Author.Login, ageString(p.UpdatedAt), labelNames(p.Labels),
		reviewGlyph(p.ReviewDecision), ciGlyph(p.CIState()))
}

func (s *PRSection) VarsAt(i int) action.Vars {
	p := s.prs[s.shown[i]]
	return action.Vars{Number: p.Number, Title: p.Title, HeadRefName: p.HeadRefName,
		BaseRefName: p.BaseRefName, URL: p.URL, Author: p.Author.Login, Branch: p.HeadRefName}
}
func (s *PRSection) Haystacks() []string {
	h := make([]string, len(s.prs))
	for i, p := range s.prs {
		h[i] = haystack(p)
	}
	return h
}

// --- Issue section ---
type IssueSection struct {
	filter string
	issues []gh.Issue
	shown  []int
}

func NewIssueSection(filter string) *IssueSection { return &IssueSection{filter: filter} }
func (s *IssueSection) Kind() string              { return "issue" }
func (s *IssueSection) Filter() string            { return s.filter }
func (s *IssueSection) SetIssues(is []gh.Issue)   { s.issues = is; s.shown = allIdx(len(is)) }
func (s *IssueSection) Len() int                  { return len(s.shown) }
func (s *IssueSection) SetShown(idx []int)        { s.shown = idx }

func (s *IssueSection) RenderRow(i int, o RowOpts) string {
	is := s.issues[s.shown[i]]
	return renderItemRow(o, fmt.Sprintf("#%d", is.Number), is.Title,
		is.Author.Login, ageString(is.UpdatedAt), labelNames(is.Labels), "", "")
}

func (s *IssueSection) VarsAt(i int) action.Vars {
	is := s.issues[s.shown[i]]
	return action.Vars{Number: is.Number, Title: is.Title, Author: is.Author.Login,
		URL: is.URL, Branch: issue.Branch(is.Number, is.Title, labelSlice(is.Labels))}
}
func (s *IssueSection) Haystacks() []string {
	h := make([]string, len(s.issues))
	for i, is := range s.issues {
		h[i] = fmt.Sprintf("#%d %s %s %s", is.Number, is.Title, is.Author.Login, labelNames(is.Labels))
	}
	return h
}

func allIdx(n int) []int {
	r := make([]int, n)
	for i := range r {
		r[i] = i
	}
	return r
}
func labelNames(ls []gh.Label) string {
	out := make([]string, len(ls))
	for i, l := range ls {
		out[i] = l.Name
	}
	return joinSpace(out)
}
func labelSlice(ls []gh.Label) []string {
	out := make([]string, len(ls))
	for i, l := range ls {
		out[i] = l.Name
	}
	return out
}
func joinSpace(s []string) string { return strings.Join(s, " ") }

const metaIndent = "         " // 9 cols — aligns the meta line under the title

// renderItemRow renders the airy 2-line form, truncating title + meta so each
// row is exactly two lines and never wraps past the pane width:
//
//	‹marker›‹num› ‹title›                         ‹ci›
//	       ‹author · age · labels · review›
func renderItemRow(o RowOpts, num, title, author, age, labels, review, ci string) string {
	w := o.Width
	if w < 10 {
		w = 10 // floor keeps truncation sane before the first WindowSizeMsg
	}
	marker := "  "
	if o.Selected {
		marker = selMarkStyle.Render("● ")
	}
	prefix := marker + accentStyle.Render(num) + "  "
	ciW := lipgloss.Width(ci)
	titleRoom := w - lipgloss.Width(prefix) - ciW - 1 // 1 = min gap before ci
	left := prefix + truncate(title, titleRoom)
	line1 := left
	if ci != "" {
		gap := w - lipgloss.Width(left) - ciW
		if gap < 1 {
			gap = 1
		}
		line1 = left + strings.Repeat(" ", gap) + ci
	}
	metaRaw := strings.TrimRight(author+" · "+age+metaTail(labels, review), " ·")
	line2 := metaIndent + dimStyle.Render(truncate(metaRaw, w-len(metaIndent)))
	body := line1 + "\n" + line2
	if o.Focused {
		body = cursorRowStyle.Width(w).Render(body)
	}
	return body
}

// truncate shortens a plain (unstyled) string to at most w display cells, adding
// an ellipsis when it cuts. Safe only for plain text (the row title/meta).
func truncate(s string, w int) string {
	if w <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= w {
		return s
	}
	if w == 1 {
		return "…"
	}
	return string(r[:w-1]) + "…"
}

func metaTail(labels, review string) string {
	out := ""
	if labels != "" {
		out += " · " + labels
	}
	if review != "" {
		out += " · " + review
	}
	return out
}

func reviewGlyph(decision string) string {
	switch decision {
	case "APPROVED":
		return passStyle.Render("✓ appr")
	case "CHANGES_REQUESTED":
		return failStyle.Render("✎ changes")
	case "REVIEW_REQUIRED":
		return dimStyle.Render("◌ review")
	default:
		return ""
	}
}

func ageString(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := time.Since(t)
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}
