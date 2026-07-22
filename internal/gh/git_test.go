package gh

import "testing"

func TestParseGitHubRemote(t *testing.T) {
	ok := map[string]string{
		"git@github.com:noamsto/prdash.git":                          "noamsto/prdash",
		"git@github.com:noamsto/prdash":                              "noamsto/prdash",
		"https://github.com/noamsto/prdash.git":                      "noamsto/prdash",
		"https://github.com/noamsto/prdash":                          "noamsto/prdash",
		"ssh://git@github.com/noamsto/prdash.git":                    "noamsto/prdash",
		"https://x-access-token:TOKEN@github.com/noamsto/prdash.git": "noamsto/prdash",
		"git@github.com:factify-inc/mono.git\n":                      "factify-inc/mono",
	}
	for in, want := range ok {
		got, ok := parseGitHubRemote(in)
		if !ok || got != want {
			t.Errorf("parseGitHubRemote(%q) = %q,%v; want %q,true", in, got, ok, want)
		}
	}

	for _, in := range []string{
		"git@gitlab.com:x/y.git",
		"https://example.com/a/b",
		"github.com/onlyowner",
		"",
	} {
		if got, ok := parseGitHubRemote(in); ok {
			t.Errorf("parseGitHubRemote(%q) = %q,true; want _,false", in, got)
		}
	}
}
