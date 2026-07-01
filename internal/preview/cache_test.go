package preview

import (
	"fmt"
	"testing"
)

const benchBody = "## Summary\n\nThis PR **refactors** the fetch path.\n\n" +
	"- warms every preset at launch\n- memoizes `preview.Render`\n\n" +
	"```go\nfunc Render(md string, w int) (string, error) { return \"\", nil }\n```\n"

// BenchmarkRenderCached is the steady state: the same body re-rendered every
// frame (cursor idle) is served from the memo.
func BenchmarkRenderCached(b *testing.B) {
	if _, err := Render(benchBody, 72); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Render(benchBody, 72); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkRenderCold is the miss path: a distinct body each iteration, i.e.
// what every frame cost before memoization.
func BenchmarkRenderCold(b *testing.B) {
	for i := 0; i < b.N; i++ {
		if _, err := Render(fmt.Sprintf("%s\n<!-- %d -->", benchBody, i), 72); err != nil {
			b.Fatal(err)
		}
	}
}

func TestRenderMemoizesByBodyAndWidth(t *testing.T) {
	const md = "hello **world**\n\n- one\n- two\n"

	before := renderMisses
	out1, err := Render(md, 60)
	if err != nil {
		t.Fatal(err)
	}
	out2, err := Render(md, 60)
	if err != nil {
		t.Fatal(err)
	}

	if out1 != out2 {
		t.Fatal("memoized render must return identical output")
	}
	if got := renderMisses - before; got != 1 {
		t.Fatalf("same (body,width) rendered %d times, want 1 (second call must hit cache)", got)
	}

	// A different width is a distinct entry and must render fresh.
	if _, err := Render(md, 80); err != nil {
		t.Fatal(err)
	}
	if got := renderMisses - before; got != 2 {
		t.Fatalf("distinct width rendered %d times total, want 2", got)
	}
}
