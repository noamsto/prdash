package ui

import (
	"fmt"

	"github.com/charmbracelet/bubbles/table"

	"github.com/noamsto/prdash/internal/action"
	"github.com/noamsto/prdash/internal/gh"
	"github.com/noamsto/prdash/internal/issue"
)

type Section interface {
	Kind() string
	Filter() string
	Columns() []table.Column
	Rows() []table.Row // from the current (filtered) items
	Len() int
	VarsAt(i int) action.Vars
	Haystacks() []string // for fuzzy filter (full item list)
	SetShown(idx []int)  // indices (into the full list) the table currently shows
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

func (s *PRSection) Columns() []table.Column {
	return []table.Column{{Title: "#", Width: 6}, {Title: "Title", Width: 50},
		{Title: "Author", Width: 14}, {Title: "CI", Width: 8}}
}
func (s *PRSection) Rows() []table.Row {
	rows := make([]table.Row, 0, len(s.shown))
	for _, i := range s.shown {
		p := s.prs[i]
		rows = append(rows, table.Row{fmt.Sprintf("#%d", p.Number), p.Title, p.Author.Login, p.CIState()})
	}
	return rows
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

func (s *IssueSection) Columns() []table.Column {
	return []table.Column{{Title: "#", Width: 6}, {Title: "Title", Width: 50},
		{Title: "Author", Width: 14}, {Title: "Labels", Width: 20}}
}
func (s *IssueSection) Rows() []table.Row {
	rows := make([]table.Row, 0, len(s.shown))
	for _, i := range s.shown {
		is := s.issues[i]
		rows = append(rows, table.Row{fmt.Sprintf("#%d", is.Number), is.Title, is.Author.Login, labelNames(is.Labels)})
	}
	return rows
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
func joinSpace(s []string) string { return fmt.Sprint(s) } // simple; refine rendering later
