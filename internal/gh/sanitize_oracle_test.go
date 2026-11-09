package gh

import (
	"testing"
	"unicode/utf8"
)

// assertNoControls is written from the contract, independent of CleanLine and
// CleanText: no C0 (bar newline and tab in multiline fields), DEL or C1 rune,
// and no line or paragraph separator in one-line fields.
func assertNoControls(t *testing.T, field, got string, multiline bool) {
	t.Helper()
	for i := 0; i < len(got); {
		r, n := utf8.DecodeRuneInString(got[i:])
		switch {
		case multiline && (r == '\n' || r == '\t'):
		case r < 0x20, r == 0x7F, r >= 0x80 && r <= 0x9F:
			t.Errorf("%s: control rune %U at byte %d in %q", field, r, i, got)
			return
		case !multiline && (r == 0x2028 || r == 0x2029):
			t.Errorf("%s: separator %U at byte %d in %q", field, r, i, got)
			return
		case r == utf8.RuneError && n == 1:
			t.Errorf("%s: invalid UTF-8 at byte %d in %q", field, i, got)
			return
		}
		i += n
	}
}
