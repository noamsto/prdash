package gh

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestFetchStackForPR(t *testing.T) {
	var gotPath, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.String(), r.Method
		requireHeaders(t, r, "Bearer tok123")
		_, _ = w.Write([]byte(`[{"number":7,"base":{"ref":"main"},"pull_requests":[
			{"number":140,"state":"closed","draft":false,"merged_at":"2026-10-01T00:00:00Z"},
			{"number":141,"state":"closed","draft":false,"merged_at":null},
			{"number":142,"state":"open","draft":true,"merged_at":null},
			{"number":143,"state":"open","draft":false}
		]}]`))
	}))
	defer srv.Close()

	st, ok, err := testSource(srv, "owner/repo", "tok123").FetchStackForPR(142)
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if gotMethod != http.MethodGet {
		t.Errorf("method = %q, want GET", gotMethod)
	}
	if want := "/repos/owner/repo/stacks?pull_request=142"; gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
	want := Stack{Number: 7, BaseRefName: "main", Members: []StackMember{
		{Number: 140, State: "MERGED"},
		{Number: 141, State: "CLOSED"},
		{Number: 142, State: "OPEN", IsDraft: true},
		{Number: 143, State: "OPEN"},
	}}
	if !reflect.DeepEqual(st, want) {
		t.Errorf("stack = %+v, want %+v", st, want)
	}
}

func TestFetchStackForPREmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	st, ok, err := testSource(srv, "owner/repo", "tok").FetchStackForPR(5)
	if err != nil || ok {
		t.Fatalf("ok=%v err=%v, want false, nil", ok, err)
	}
	if !reflect.DeepEqual(st, Stack{}) {
		t.Errorf("stack = %+v, want zero", st)
	}
}

func TestFetchStackForPRNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
	}))
	defer srv.Close()

	_, _, err := testSource(srv, "owner/repo", "tok").FetchStackForPR(5)
	if err == nil || err.Error() != "stacked PRs aren't available for this repo" {
		t.Errorf("err = %v", err)
	}
}

func TestBaseUsesMergeQueue(t *testing.T) {
	tests := []struct {
		name string
		resp string
		want bool
	}{
		{"merge queue", `{"data":{"repository":{"mergeQueue":{"id":"MQ1"},"ref":{"rules":{"nodes":[]}}}}}`, true},
		{"queue rule only", `{"data":{"repository":{"mergeQueue":null,"ref":{"rules":{"nodes":[{"type":"DELETION"},{"type":"MERGE_QUEUE"}]}}}}}`, true},
		{"neither", `{"data":{"repository":{"mergeQueue":null,"ref":{"rules":{"nodes":[{"type":"DELETION"}]}}}}}`, false},
		{"missing ref", `{"data":{"repository":{"mergeQueue":null,"ref":null}}}`, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var gotPath, gotMethod string
			var posted struct {
				Variables map[string]string `json:"variables"`
			}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotPath, gotMethod = r.URL.Path, r.Method
				if got := r.Header.Get("Authorization"); got != "Bearer tok123" {
					t.Errorf("Authorization = %q", got)
				}
				if got := r.Header.Get("Content-Type"); got != "application/json" {
					t.Errorf("Content-Type = %q", got)
				}
				raw, _ := io.ReadAll(r.Body)
				if err := json.Unmarshal(raw, &posted); err != nil {
					t.Errorf("body: %v", err)
				}
				_, _ = w.Write([]byte(tc.resp))
			}))
			defer srv.Close()

			got, err := testSource(srv, "owner/repo", "tok123").BaseUsesMergeQueue("release/1")
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Errorf("got %v, want %v", got, tc.want)
			}
			if gotMethod != http.MethodPost || gotPath != "/graphql" {
				t.Errorf("%s %s, want POST /graphql", gotMethod, gotPath)
			}
			wantVars := map[string]string{"owner": "owner", "name": "repo", "branch": "release/1", "qualified": "refs/heads/release/1"}
			if !reflect.DeepEqual(posted.Variables, wantVars) {
				t.Errorf("variables = %v, want %v", posted.Variables, wantVars)
			}
		})
	}
}

func TestBaseUsesMergeQueueErrors(t *testing.T) {
	tests := []struct {
		name   string
		status int
		resp   string
	}{
		{"http error", http.StatusBadGateway, `bad gateway`},
		{"graphql errors", http.StatusOK, `{"errors":[{"message":"boom"}],"data":{"repository":null}}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.resp))
			}))
			defer srv.Close()

			if _, err := testSource(srv, "owner/repo", "tok").BaseUsesMergeQueue("main"); err == nil {
				t.Error("want error")
			}
		})
	}
}

// stackMergeServer captures the one request it serves and answers with
// status and resp.
func stackMergeServer(t *testing.T, status int, resp string) (srv *httptest.Server, gotMethod, gotPath, gotBody, gotType *string) {
	t.Helper()
	gotMethod, gotPath, gotBody, gotType = new(string), new(string), new(string), new(string)
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requireHeaders(t, r, "Bearer tok123")
		raw, _ := io.ReadAll(r.Body)
		*gotMethod, *gotPath, *gotBody, *gotType = r.Method, r.URL.String(), string(raw), r.Header.Get("Content-Type")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(resp))
	}))
	t.Cleanup(srv.Close)
	return srv, gotMethod, gotPath, gotBody, gotType
}

func TestMergeStackDirect(t *testing.T) {
	tests := []struct {
		name   string
		status int
		resp   string
		want   StackMergeResult
	}{
		{"pending", http.StatusAccepted, `{"status":"pending","details":{"uuid":"abc-123"}}`, StackMergeResult{Status: StackMergePending, UUID: "abc-123"}},
		{"merged", http.StatusOK, `{"status":"merged","details":{"sha":"deadbeef"}}`, StackMergeResult{Status: StackMergeMerged, SHA: "deadbeef"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv, method, path, body, ctype := stackMergeServer(t, tc.status, tc.resp)
			got, err := testSource(srv, "owner/repo", "tok123").MergeStack(142, false)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Errorf("result = %+v, want %+v", got, tc.want)
			}
			if *method != http.MethodPut {
				t.Errorf("method = %q, want PUT", *method)
			}
			if want := "/repos/owner/repo/pulls/142/merge-async"; *path != want {
				t.Errorf("path = %q, want %q", *path, want)
			}
			if want := `{"merge_method":"squash","merge_action":"direct_merge"}`; *body != want {
				t.Errorf("body = %s, want %s", *body, want)
			}
			if *ctype != "application/json" {
				t.Errorf("Content-Type = %q", *ctype)
			}
		})
	}
}

func TestMergeStackQueue(t *testing.T) {
	srv, _, _, body, _ := stackMergeServer(t, http.StatusAccepted, `{"status":"enqueued"}`)
	got, err := testSource(srv, "owner/repo", "tok123").MergeStack(142, true)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StackMergeEnqueued {
		t.Errorf("status = %q, want enqueued", got.Status)
	}
	if want := `{"merge_action":"merge_queue"}`; *body != want {
		t.Errorf("body = %s, want %s", *body, want)
	}
}

func TestMergeStackErrors(t *testing.T) {
	tests := []struct {
		status int
		resp   string
		want   string
	}{
		{http.StatusNotFound, `{"message":"Not Found"}`, "async stack merge isn't available for this repo"},
		{http.StatusConflict, `{"message":"busy"}`, "a merge for this stack is already in progress"},
		{http.StatusUnprocessableEntity, " {\"message\":\"Required status check is failing\"}\n", "stack merge: 422 Unprocessable Entity: " + `{"message":"Required status check is failing"}`},
	}
	for _, tc := range tests {
		t.Run(http.StatusText(tc.status), func(t *testing.T) {
			srv, _, _, _, _ := stackMergeServer(t, tc.status, tc.resp)
			_, err := testSource(srv, "owner/repo", "tok123").MergeStack(142, false)
			if err == nil || err.Error() != tc.want {
				t.Errorf("err = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestStackMergeStatus(t *testing.T) {
	srv, method, path, body, _ := stackMergeServer(t, http.StatusOK, `{"status":"failed","details":{"message":"conflict in #141"}}`)
	got, err := testSource(srv, "owner/repo", "tok123").StackMergeStatus(142, "abc-123")
	if err != nil {
		t.Fatal(err)
	}
	want := StackMergeResult{Status: StackMergeFailed, Message: "conflict in #141"}
	if got != want {
		t.Errorf("result = %+v, want %+v", got, want)
	}
	if *method != http.MethodGet {
		t.Errorf("method = %q, want GET", *method)
	}
	if want := "/repos/owner/repo/pulls/142/merge-async/abc-123"; *path != want {
		t.Errorf("path = %q, want %q", *path, want)
	}
	if *body != "" {
		t.Errorf("body = %q, want empty", *body)
	}
}

func TestStackMergeStatusError(t *testing.T) {
	srv, _, _, _, _ := stackMergeServer(t, http.StatusInternalServerError, "oops\n")
	_, err := testSource(srv, "owner/repo", "tok123").StackMergeStatus(142, "u")
	if err == nil || !strings.Contains(err.Error(), "500 Internal Server Error: oops") {
		t.Errorf("err = %v", err)
	}
}
