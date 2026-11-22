package gh

import "testing"

func TestIsDependabot(t *testing.T) {
	for login, want := range map[string]bool{
		"app/dependabot":  true,
		"dependabot":      true,
		"dependabot[bot]": true,
		"Dependabot":      true,
		"noamsto":         false,
		"dependabot-fork": false,
		"":                false,
	} {
		if got := IsDependabot(login); got != want {
			t.Errorf("IsDependabot(%q) = %v, want %v", login, got, want)
		}
	}
}
