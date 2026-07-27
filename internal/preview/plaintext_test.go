package preview

import (
	"testing"
)

func TestPlainTitle(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{
			name: "heading marker trimmed",
			in:   "### Advisory lost after commit retry\n\nMedium Severity",
			want: "Advisory lost after commit retry",
		},
		{
			name: "badge in bold in sub tags",
			in:   "**<sub><sub>![P1 Badge](https://img.shields.io/badge/P1-orange?style=flat)</sub></sub>  Handle nullable sibling values without aborting persistence**",
			want: "Handle nullable sibling values without aborting persistence",
		},
		{
			name: "badge-only first line falls through to the next",
			in:   "![P2 Badge](https://img.shields.io/badge/P2-yellow?style=flat)\n\nNormalize currency-formatted values",
			want: "Normalize currency-formatted values",
		},
		{
			name: "snake_case identifier survives",
			in:   "`substitution_value` may be NULL",
			want: "substitution_value may be NULL",
		},
		{
			name: "blockquote marker trimmed",
			in:   "> quoted finding",
			want: "quoted finding",
		},
		{
			name: "empty body yields empty string",
			in:   "",
			want: "",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := PlainTitle(c.in); got != c.want {
				t.Errorf("PlainTitle() = %q, want %q", got, c.want)
			}
		})
	}
}
