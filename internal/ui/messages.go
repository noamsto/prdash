package ui

import "github.com/noamsto/prdash/internal/gh"

type prsFetchedMsg struct {
	prs []gh.PR
	raw []byte
}

type fetchFailedMsg struct{ err error }
