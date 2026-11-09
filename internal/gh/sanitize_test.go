package gh

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

var unicodeRows = []struct{ name, in string }{
	{"cjk", "日本語のタイトル"},
	{"combining", "éä"},
	{"zwj emoji", "👨\u200D👩\u200D👧 ok"},
	{"variation selector", "❤\uFE0F"},
	{"rtl marks", "\u200Fשלום\u200E abc مرحبا\u200F"},
}

func TestCleanLine(t *testing.T) {
	rows := []struct{ name, in, want string }{
		{"sgr", "\x1b[31mred\x1b[0m", "red"},
		{"osc8", "\x1b]8;;https://evil.example\x07label\x1b]8;;\x07", "label"},
		{"osc8 st", "\x1b]8;;https://evil.example\x1b\\label\x1b]8;;\x1b\\", "label"},
		{"osc52", "a\x1b]52;c;ZXZpbA==\x07b", "ab"},
		{"csi cursor", "a\x1b[2J\x1b[Hb", "ab"},
		{"bel", "a\x07b", "a�b"},
		{"cr", "a\rb", "a b"},
		{"lf", "a\nb", "a b"},
		{"crlf", "a\r\nb", "a b"},
		{"tab", "a\tb", "a b"},
		{"vt ff", "a\v\fb", "a  b"},
		{"nel", "a\u0085b", "a b"},
		{"line separator", "a\u2028b\u2029c", "a b c"},
		{"utf8 c1", "a\u009bb", "a�b"},
		{"strip bypass", "\xc2\x1b[31mx", "��[31mx"},
		{"lone esc", "a\x1b", "a"},
		{"del", "a\x7fb", "a�b"},
		{"8bit csi", "a\x9b31mb", ""},
	}
	for _, tc := range rows {
		t.Run(tc.name, func(t *testing.T) {
			got := CleanLine(tc.in)
			if tc.want != "" && got != tc.want {
				t.Errorf("CleanLine(%q) = %q, want %q", tc.in, got, tc.want)
			}
			if strings.Contains(got, "evil.example") {
				t.Errorf("link target survived: %q", got)
			}
			assertNoControls(t, "got", got, false)
			if again := CleanLine(got); again != got {
				t.Errorf("not idempotent: %q then %q", got, again)
			}
		})
	}
	for _, tc := range unicodeRows {
		t.Run(tc.name, func(t *testing.T) {
			if got := CleanLine(tc.in); got != tc.in {
				t.Errorf("CleanLine(%q) = %q, want unchanged", tc.in, got)
			}
		})
	}
}

func TestCleanText(t *testing.T) {
	rows := []struct{ name, in, want string }{
		{"sgr", "\x1b[31mred\x1b[0m", "red"},
		{"osc8", "\x1b]8;;https://evil.example\x07label\x1b]8;;\x07", "label"},
		{"osc52", "a\x1b]52;c;ZXZpbA==\x07b", "ab"},
		{"csi cursor", "a\x1b[2J\x1b[Hb", "ab"},
		{"bel", "a\x07b", "ab"},
		{"cr", "a\rb", "ab"},
		{"lf", "a\nb", "a\nb"},
		{"crlf", "a\r\nb", "a\nb"},
		{"tab", "a\tb", "a\tb"},
		{"nel", "a\u0085b", "ab"},
		{"line separator", "a\u2028b", "a\u2028b"},
		{"utf8 c1", "a\u009bb", "ab"},
		{"strip bypass", "\xc2\x1b[31mx", "�[31mx"},
		{"lone esc", "a\x1b", "a"},
		{"8bit csi", "a\x9b31mb", ""},
	}
	for _, tc := range rows {
		t.Run(tc.name, func(t *testing.T) {
			got := CleanText(tc.in)
			if tc.want != "" && got != tc.want {
				t.Errorf("CleanText(%q) = %q, want %q", tc.in, got, tc.want)
			}
			if strings.Contains(got, "evil.example") {
				t.Errorf("link target survived: %q", got)
			}
			assertNoControls(t, "got", strings.ReplaceAll(got, "\u2028", ""), true)
			if again := CleanText(got); again != got {
				t.Errorf("not idempotent: %q then %q", got, again)
			}
		})
	}
	for _, tc := range unicodeRows {
		t.Run(tc.name, func(t *testing.T) {
			if got := CleanText(tc.in); got != tc.in {
				t.Errorf("CleanText(%q) = %q, want unchanged", tc.in, got)
			}
		})
	}
}

const sanitizePayload = "\x1b[31mx\x07\ny"

func fillStrings(v reflect.Value) {
	switch v.Kind() {
	case reflect.String:
		if v.CanSet() {
			v.SetString(sanitizePayload)
		}
	case reflect.Pointer:
		if v.CanSet() {
			v.Set(reflect.New(v.Type().Elem()))
			fillStrings(v.Elem())
		}
	case reflect.Slice:
		if v.CanSet() {
			v.Set(reflect.MakeSlice(v.Type(), 1, 1))
			fillStrings(v.Index(0))
		}
	case reflect.Array:
		for i := range v.Len() {
			fillStrings(v.Index(i))
		}
	case reflect.Struct:
		for _, f := range v.Fields() {
			fillStrings(f)
		}
	default:
	}
}

// checkStrings returns how many settable strings it saw and how many of them
// were tagged for CleanText.
func checkStrings(t *testing.T, path string, v reflect.Value, text bool) (seen, tagged int) {
	t.Helper()
	switch v.Kind() {
	case reflect.String:
		if !v.CanSet() {
			return 0, 0
		}
		got := v.String()
		assertNoControls(t, path, got, text)
		if !text {
			return 1, 0
		}
		if !strings.Contains(got, "\n") {
			t.Errorf("%s: tagged string lost its newline: %q", path, got)
		}
		return 1, 1
	case reflect.Pointer:
		if !v.IsNil() {
			return checkStrings(t, path, v.Elem(), text)
		}
	case reflect.Slice, reflect.Array:
		for i := range v.Len() {
			s, g := checkStrings(t, path, v.Index(i), text)
			seen, tagged = seen+s, tagged+g
		}
	case reflect.Struct:
		for f, fv := range v.Fields() {
			s, g := checkStrings(t, path+"."+f.Name, fv, text || f.Tag.Get("sanitize") == "text")
			seen, tagged = seen+s, tagged+g
		}
	default:
	}
	return seen, tagged
}

func walkCase[T any](t *testing.T, wantTagged bool) {
	t.Helper()
	var v T
	rv := reflect.ValueOf(&v).Elem()
	fillStrings(rv)
	sanitize(&v)
	seen, tagged := checkStrings(t, reflect.TypeFor[T]().String(), rv, false)
	if seen == 0 {
		t.Error("no strings reached")
	}
	if wantTagged && tagged == 0 {
		t.Error("no tagged string reached")
	}
}

func TestSanitizeWalksEveryResultType(t *testing.T) {
	t.Run("[]PR", func(t *testing.T) { walkCase[[]PR](t, true) })
	t.Run("[]Issue", func(t *testing.T) { walkCase[[]Issue](t, false) })
	t.Run("PRDetail", func(t *testing.T) { walkCase[PRDetail](t, true) })
	t.Run("IssueDetail", func(t *testing.T) { walkCase[IssueDetail](t, true) })
	t.Run("[]User", func(t *testing.T) { walkCase[[]User](t, false) })
	t.Run("string", func(t *testing.T) { walkCase[string](t, false) })
	t.Run("[]WorkflowRun", func(t *testing.T) { walkCase[[]WorkflowRun](t, false) })
	t.Run("[]RunJob", func(t *testing.T) { walkCase[[]RunJob](t, false) })
	t.Run("[]Check", func(t *testing.T) { walkCase[[]Check](t, false) })
	t.Run("Stack", func(t *testing.T) { walkCase[Stack](t, false) })
	t.Run("StackMergeResult", func(t *testing.T) { walkCase[StackMergeResult](t, false) })
	t.Run("ItemState", func(t *testing.T) { walkCase[ItemState](t, false) })
}

func TestSanitizePanicsOnUnsupportedKind(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("expected panic for map field")
		}
	}()
	sanitize(&struct{ M map[string]string }{})
}

func TestDecodeCached(t *testing.T) {
	var got struct {
		Title string
		Body  string `sanitize:"text"`
	}
	raw := []byte(`{"Title":"a\u001b]8;;https://evil.example\u0007b\u001b]8;;\u0007\r\n","Body":"x\u0007\r\ny"}`)
	if err := DecodeCached(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.Title != "ab " || got.Body != "x\ny" {
		t.Errorf("got %+v", got)
	}
	if err := DecodeCached([]byte(`{"Title":`), &got); err == nil {
		t.Error("malformed JSON returned nil error")
	}
}

func TestCleanLineRefCollision(t *testing.T) {
	got := CleanLine("feat\u0081")
	if got == "feat" {
		t.Errorf("control dropped, collides with another ref: %q", got)
	}
	assertNoControls(t, "got", got, false)
}

func TestSanitizeErr(t *testing.T) {
	if sanitizeErr(nil) != nil {
		t.Error("nil error changed")
	}
	clean := errors.New("plain")
	if got := sanitizeErr(clean); !errors.Is(got, clean) || errors.Unwrap(got) != nil {
		t.Errorf("clean error was wrapped: %v", got)
	}
	dirty := errors.New("a\x1b[31mb\nc")
	got := sanitizeErr(dirty)
	if got.Error() != "ab c" {
		t.Errorf("got %q", got.Error())
	}
	if !errors.Is(got, dirty) {
		t.Error("original not reachable through Unwrap")
	}
}
