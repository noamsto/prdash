package ui

import "strings"

// logStep is one Actions step's log: its name, whether it's a failed step
// (true for every step in --log-failed output), and its content lines.
type logStep struct {
	name   string
	failed bool
	lines  []string
}

// parseJobLog turns `gh run view --log[-failed]` output into ordered steps.
// The output is tab-delimited as job⇥step⇥<timestamp> <content>; we group by the
// step column, preserving first-seen order, and strip the leading timestamp.
func parseJobLog(raw []byte, failedOnly bool) []logStep {
	var steps []logStep
	idx := map[string]int{}
	for _, line := range strings.Split(strings.TrimRight(string(raw), "\n"), "\n") {
		if line == "" {
			continue
		}
		step, content := "", line
		if parts := strings.SplitN(line, "\t", 3); len(parts) == 3 {
			step = parts[1]
			content = stripTimestamp(parts[2])
		}
		i, seen := idx[step]
		if !seen {
			i = len(steps)
			idx[step] = i
			steps = append(steps, logStep{name: step, failed: failedOnly})
		}
		steps[i].lines = append(steps[i].lines, content)
	}
	return steps
}

// stripTimestamp drops gh's leading RFC3339 timestamp ("2024-01-02T03:04:05Z ")
// from a log line, leaving the message. Lines without one are returned as-is.
func stripTimestamp(s string) string {
	i := strings.IndexByte(s, ' ')
	if i < 20 || s[4] != '-' || s[10] != 'T' {
		return s
	}
	return s[i+1:]
}

// logLine is one rendered/navigable line: either a step header or a content
// line. step indexes into the []logStep it came from (headers included), so copy
// can target the whole step from any line within it.
type logLine struct {
	text   string
	step   int
	header bool
}

func flattenLog(steps []logStep) []logLine {
	var out []logLine
	for i, s := range steps {
		out = append(out, logLine{text: s.name, step: i, header: true})
		for _, ln := range s.lines {
			out = append(out, logLine{text: ln, step: i})
		}
	}
	return out
}

type lineKind int

const (
	kindPlain lineKind = iota
	kindError
	kindPass
)

// classifyLogLine buckets a content line so the renderer can color it. Errors
// win over passes when a line somehow matches both.
func classifyLogLine(text string) lineKind {
	l := strings.ToLower(text)
	switch {
	case strings.Contains(l, "error") || strings.Contains(l, "fail") || strings.Contains(text, "✗"):
		return kindError
	case strings.Contains(l, "pass") || strings.Contains(text, "✓") || strings.HasPrefix(l, "ok"):
		return kindPass
	default:
		return kindPlain
	}
}

func copyLine(lines []logLine, cursor int) string {
	if cursor < 0 || cursor >= len(lines) {
		return ""
	}
	return lines[cursor].text
}

func copyStep(steps []logStep, lines []logLine, cursor int) string {
	if cursor < 0 || cursor >= len(lines) {
		return ""
	}
	s := steps[lines[cursor].step]
	return s.name + "\n" + strings.Join(s.lines, "\n")
}

func copyWhole(steps []logStep) string {
	var parts []string
	for _, s := range steps {
		parts = append(parts, s.name+"\n"+strings.Join(s.lines, "\n"))
	}
	return strings.Join(parts, "\n")
}
