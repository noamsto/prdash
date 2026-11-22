package gh

import "strings"

// IsDependabot reports whether login is Dependabot's bot account. The GraphQL
// loader prefixes bot logins with "app/", while REST-sourced logins carry a
// "[bot]" suffix.
func IsDependabot(login string) bool {
	l := strings.ToLower(login)
	l = strings.TrimPrefix(l, "app/")
	l = strings.TrimSuffix(l, "[bot]")
	return l == "dependabot"
}
