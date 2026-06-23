package ui

import (
	"testing"

	"github.com/noamsto/prdash/internal/gh"
)

func TestPRSectionRows(t *testing.T) {
	s := NewPRSection("is:open")
	s.SetPRs([]gh.PR{{Number: 7, Title: "x"}})
	rows := s.Rows()
	if len(rows) != 1 || rows[0][0] != "#7" {
		t.Fatalf("rows=%v", rows)
	}
}
