package action

import (
	"bytes"
	"text/template"
)

// Vars are the per-item template values. Built from a gh.PR / gh.Issue by the UI.
type Vars struct {
	Number      int
	Title       string
	HeadRefName string
	BaseRefName string
	URL         string
	Repo        string
	Author      string
	Branch      string // derived branch (issue) or HeadRefName (PR)
}

type Command struct {
	Argv    []string // templated, exec'd directly (no shell) — injection-safe
	Builtin string   // e.g. "rerun-failed", "copy-url", "copy-branch"
	Shell   string   // opt-in: run through `sh -c` (user actions only)
}

type Action struct {
	Key      string
	Label    string // imperative, shown in menus/legend
	Command  Command
	ExitsTUI bool
	Scope    string // "single" | "per-selected"
	Confirm  bool

	// Inline-status wording, per state. Empty fields fall back to Label.
	Progress string // gerund while running, e.g. "Merging"
	Past     string // past tense on success, e.g. "Merged"
	Fail     string // on failure, e.g. "Merge failed"
}

// ExpandArgv renders each argv element as a template against v.
func (a Action) ExpandArgv(v Vars) ([]string, error) {
	out := make([]string, 0, len(a.Command.Argv))
	for _, raw := range a.Command.Argv {
		t, err := template.New("a").Parse(raw)
		if err != nil {
			return nil, err
		}
		var b bytes.Buffer
		if err := t.Execute(&b, v); err != nil {
			return nil, err
		}
		out = append(out, b.String())
	}
	return out, nil
}
