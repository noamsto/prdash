package ui

import (
	"regexp"
	"strings"
)

// Branch shapes this machine actually produces (see AGENTS.shared.md): repos
// tracking work in Linear use Linear's generated name, personal repos use
// type/id-desc. Nothing else is recognised.
var (
	ghBranchRe     = regexp.MustCompile(`^(?:feat|fix|refactor|chore|docs)/(\d+)-`)
	linearBranchRe = regexp.MustCompile(`^([a-z]{2,6})-(\d+)-`)
)

// commitTypeWords are branch prefixes that look like a Linear team key but are
// conventional-commit types. Without this, "fix-123-typo" — a branch naming no
// ticket — parses as "FIX-123".
var commitTypeWords = map[string]bool{
	"feat": true, "fix": true, "chore": true, "docs": true, "refactor": true,
	"perf": true, "test": true, "build": true, "ci": true, "style": true,
	"revert": true,
}

// ticketID extracts a ticket reference from a head branch name, or "" when the
// branch names none — which is common: agent branches (agents/…, cursor/…) carry
// no id by construction.
func ticketID(branch string) string {
	if m := ghBranchRe.FindStringSubmatch(branch); m != nil {
		return "#" + m[1]
	}
	m := linearBranchRe.FindStringSubmatch(branch)
	if m == nil || commitTypeWords[m[1]] {
		return ""
	}
	return strings.ToUpper(m[1]) + "-" + m[2]
}
