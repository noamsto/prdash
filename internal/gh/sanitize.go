package gh

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"unicode"

	"github.com/charmbracelet/x/ansi"
	"github.com/shurcooL/githubv4"
)

// CleanLine makes an API string safe for one terminal row: escape sequences
// are removed, line and paragraph breaks become spaces, and any other control
// rune becomes U+FFFD rather than vanishing, so two distinct refs never
// collapse to the same cleaned value.
func CleanLine(s string) string {
	s = strings.ReplaceAll(ansi.Strip(s), "\r\n", " ")
	return strings.Map(func(r rune) rune {
		switch r {
		case '\n', '\r', '\t', '\v', '\f', '\u0085', '\u2028', '\u2029':
			return ' '
		}
		if unicode.IsControl(r) {
			return unicode.ReplacementChar
		}
		return r
	}, s)
}

// CleanText is CleanLine for multi-line bodies: newlines and tabs survive, and
// a bare CR is dropped rather than turned into a line break.
func CleanText(s string) string {
	s = strings.ReplaceAll(ansi.Strip(s), "\r\n", "\n")
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return r
		}
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
}

// sanitize cleans every settable string reachable from v in place. Strings use
// CleanLine unless their field is tagged sanitize:"text". Shapes it cannot
// reach (map values are not addressable) panic so a new field type fails a
// test instead of silently bypassing the filter.
func sanitize[T any](v *T) {
	sanitizeValue(reflect.ValueOf(v).Elem(), false)
}

func sanitizeValue(v reflect.Value, text bool) {
	switch v.Kind() {
	case reflect.String:
		if !v.CanSet() {
			return
		}
		if text {
			v.SetString(CleanText(v.String()))
		} else {
			v.SetString(CleanLine(v.String()))
		}
	case reflect.Pointer:
		if !v.IsNil() {
			sanitizeValue(v.Elem(), text)
		}
	case reflect.Slice, reflect.Array:
		for i := range v.Len() {
			sanitizeValue(v.Index(i), text)
		}
	case reflect.Struct:
		t := v.Type()
		for i := range v.NumField() {
			f := v.Field(i)
			if !f.CanSet() {
				continue
			}
			sanitizeValue(f, text || t.Field(i).Tag.Get("sanitize") == "text")
		}
	case reflect.Map, reflect.Interface, reflect.Chan, reflect.Func, reflect.UnsafePointer:
		panic("gh.sanitize: unsupported kind " + v.Kind().String())
	default:
	}
}

// DecodeCached unmarshals a cached payload and sanitizes it, so a cache written
// before the filter existed (or edited on disk) cannot bypass it.
func DecodeCached[T any](raw []byte, v *T) error {
	if err := json.Unmarshal(raw, v); err != nil {
		return err
	}
	sanitize(v)
	return nil
}

func cleanBody(b []byte) string {
	return strings.TrimSpace(CleanLine(string(b)))
}

type cleanError struct {
	msg string
	err error
}

func (e *cleanError) Error() string { return e.msg }
func (e *cleanError) Unwrap() error { return e.err }

func sanitizeErr(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	clean := CleanLine(msg)
	if clean == msg {
		return err
	}
	return &cleanError{msg: clean, err: err}
}

// graphClient sanitizes the error text of every githubv4 call, which can echo
// server-supplied messages, while keeping the original reachable for errors.As.
type graphClient struct{ *githubv4.Client }

func (c graphClient) Query(ctx context.Context, q any, variables map[string]any) error {
	return sanitizeErr(c.Client.Query(ctx, q, variables))
}

func (c graphClient) Mutate(ctx context.Context, m any, input githubv4.Input, variables map[string]any) error {
	return sanitizeErr(c.Client.Mutate(ctx, m, input, variables))
}
