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
