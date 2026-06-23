package gh

import "testing"

func TestParsePRDetail(t *testing.T) {
	d, err := ParsePRDetail([]byte(`{
		"comments":[{"author":{"login":"a"},"body":"hi","createdAt":"2026-06-01T10:00:00Z"}],
		"reviews":[{"author":{"login":"b"},"body":"LGTM","state":"APPROVED","submittedAt":"2026-06-02T10:00:00Z"}]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Comments) != 1 || d.Comments[0].Author.Login != "a" {
		t.Fatalf("comments=%+v", d.Comments)
	}
	if len(d.Reviews) != 1 || d.Reviews[0].State != "APPROVED" {
		t.Fatalf("reviews=%+v", d.Reviews)
	}
}
