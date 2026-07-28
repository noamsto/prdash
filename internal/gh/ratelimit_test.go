package gh

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

// testRateSource is testSource plus the recording transport NewGraphSource
// installs, so a call through s.http files its response headers.
func testRateSource(srv *httptest.Server, repo, token string) GraphSource {
	hc := oauth2.NewClient(context.Background(),
		oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token}))
	st := newRateStore()
	hc.Transport = &rateTransport{next: hc.Transport, store: st}
	return GraphSource{repo: repo, http: hc, token: token, apiBase: srv.URL, rate: st}
}

func rateHeaders(limit, remaining, reset, resource string) map[string]string {
	h := map[string]string{}
	if limit != "" {
		h["X-RateLimit-Limit"] = limit
	}
	if remaining != "" {
		h["X-RateLimit-Remaining"] = remaining
	}
	if reset != "" {
		h["X-RateLimit-Reset"] = reset
	}
	if resource != "" {
		h["X-RateLimit-Resource"] = resource
	}
	return h
}

func TestRateStoreRecord(t *testing.T) {
	tests := []struct {
		name string
		hdr  map[string]string
		path string
		want *RateSnapshot // nil ⇒ nothing recorded
	}{
		{
			name: "full graphql headers",
			hdr:  rateHeaders("5000", "4832", "1750000000", "graphql"),
			path: "/graphql",
			want: &RateSnapshot{Limit: 5000, Remaining: 4832, Reset: time.Unix(1750000000, 0), Resource: "graphql"},
		},
		{
			// The JobLog blob-storage hop: no rate headers, so nothing to file.
			name: "no rate headers",
			hdr:  nil,
			path: "/blob/abc",
			want: nil,
		},
		{
			name: "missing remaining",
			hdr:  rateHeaders("5000", "", "1750000000", "core"),
			path: "/repos/o/r/actions/runs",
			want: nil,
		},
		{
			name: "malformed limit",
			hdr:  rateHeaders("lots", "4832", "1750000000", "graphql"),
			path: "/graphql",
			want: nil,
		},
		{
			name: "malformed reset",
			hdr:  rateHeaders("5000", "4832", "soon", "graphql"),
			path: "/graphql",
			want: nil,
		},
		{
			name: "resource falls back to path: graphql",
			hdr:  rateHeaders("5000", "4832", "1750000000", ""),
			path: "/graphql",
			want: &RateSnapshot{Limit: 5000, Remaining: 4832, Reset: time.Unix(1750000000, 0), Resource: "graphql"},
		},
		{
			name: "resource falls back to path: core",
			hdr:  rateHeaders("5000", "4991", "1750000000", ""),
			path: "/repos/o/r/actions/runs",
			want: &RateSnapshot{Limit: 5000, Remaining: 4991, Reset: time.Unix(1750000000, 0), Resource: "core"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			st := newRateStore()
			h := http.Header{}
			for k, v := range tc.hdr {
				h.Set(k, v)
			}
			st.record(h, tc.path)

			got, ok := st.tightest()
			if tc.want == nil {
				if ok {
					t.Fatalf("tightest() = %+v, want nothing recorded", got)
				}
				return
			}
			if !ok {
				t.Fatal("tightest() recorded nothing, want a snapshot")
			}
			if got.Limit != tc.want.Limit || got.Remaining != tc.want.Remaining ||
				got.Resource != tc.want.Resource || !got.Reset.Equal(tc.want.Reset) {
				t.Errorf("tightest() = %+v, want %+v", got, *tc.want)
			}
		})
	}
}

func TestRateStoreRecordNilStoreIsNoop(t *testing.T) {
	var st *rateStore
	st.record(http.Header{"X-Ratelimit-Limit": {"5000"}}, "/graphql")
	if _, ok := st.tightest(); ok {
		t.Error("nil store reported a snapshot")
	}
}

func TestRateStoreTightest(t *testing.T) {
	reset := time.Unix(1750000000, 0)
	snap := func(res string, remaining int) RateSnapshot {
		return RateSnapshot{Limit: 5000, Remaining: remaining, Reset: reset, Resource: res}
	}
	tests := []struct {
		name string
		have []RateSnapshot
		want string // expected Resource
	}{
		{"graphql alone", []RateSnapshot{snap("graphql", 4832)}, "graphql"},
		{"core alone", []RateSnapshot{snap("core", 4991)}, "core"},
		{"graphql wins when core is looser", []RateSnapshot{snap("graphql", 400), snap("core", 4991)}, "graphql"},
		{"core wins when strictly tighter", []RateSnapshot{snap("graphql", 4832), snap("core", 120)}, "core"},
		{"graphql wins a tie", []RateSnapshot{snap("graphql", 500), snap("core", 500)}, "graphql"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			st := newRateStore()
			for _, s := range tc.have {
				st.put(s)
			}
			got, ok := st.tightest()
			if !ok {
				t.Fatal("tightest() recorded nothing")
			}
			if got.Resource != tc.want {
				t.Errorf("tightest() resource = %q, want %q", got.Resource, tc.want)
			}
		})
	}
}

func TestRateStoreTightestIgnoresZeroLimit(t *testing.T) {
	st := newRateStore()
	st.put(RateSnapshot{Limit: 0, Remaining: 0, Resource: "core"})
	st.put(RateSnapshot{Limit: 5000, Remaining: 4832, Resource: "graphql"})
	got, ok := st.tightest()
	if !ok {
		t.Fatal("tightest() recorded nothing")
	}
	if got.Resource != "graphql" {
		t.Errorf("resource = %q, want graphql (a zero-limit bucket has no fraction)", got.Resource)
	}
}

// A live call through s.http files what the response advertised, with no extra
// request of its own.
func TestRateLimitFromResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-RateLimit-Limit", "5000")
		w.Header().Set("X-RateLimit-Remaining", "4700")
		w.Header().Set("X-RateLimit-Reset", "1750000000")
		w.Header().Set("X-RateLimit-Resource", "core")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"total_count":0,"workflow_runs":[]}`))
	}))
	defer srv.Close()

	s := testRateSource(srv, "owner/repo", "tok123")
	if _, ok := s.RateLimit(); ok {
		t.Fatal("RateLimit() reported a snapshot before any call")
	}
	if _, err := s.ListRunsForBranch("feat/x"); err != nil {
		t.Fatal(err)
	}

	got, ok := s.RateLimit()
	if !ok {
		t.Fatal("RateLimit() reported nothing after a call that advertised headers")
	}
	if got.Remaining != 4700 || got.Limit != 5000 || got.Resource != "core" {
		t.Errorf("RateLimit() = %+v, want 4700/5000 core", got)
	}
	if !got.Reset.Equal(time.Unix(1750000000, 0)) {
		t.Errorf("Reset = %v, want %v", got.Reset, time.Unix(1750000000, 0))
	}
}

// The value copies GraphSource is used as must share one store, or a snapshot
// recorded through one copy is invisible to the UI reading another.
func TestRateLimitSharedAcrossCopies(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-RateLimit-Limit", "5000")
		w.Header().Set("X-RateLimit-Remaining", "4321")
		w.Header().Set("X-RateLimit-Reset", "1750000000")
		w.Header().Set("X-RateLimit-Resource", "core")
		_, _ = w.Write([]byte(`{"total_count":0,"workflow_runs":[]}`))
	}))
	defer srv.Close()

	s := testRateSource(srv, "owner/repo", "tok123")
	copyOfS := s
	if _, err := s.ListRunsForBranch("feat/x"); err != nil {
		t.Fatal(err)
	}
	got, ok := copyOfS.RateLimit()
	if !ok || got.Remaining != 4321 {
		t.Errorf("copy RateLimit() = %+v, %v; want 4321 remaining", got, ok)
	}
}

// JobLog's authenticated first hop is a real core spend and must be recorded
// even though it bypasses s.http; its unauthenticated blob hop must not clobber
// that snapshot.
func TestJobLogRecordsAuthHopNotBlob(t *testing.T) {
	blob := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Blob storage advertises no rate budget; if this were recorded it would
		// overwrite core with nonsense.
		w.Header().Set("X-RateLimit-Limit", "")
		_, _ = w.Write([]byte("2024-01-02T03:04:05.0000000Z ##[group]Run x\n"))
	}))
	defer blob.Close()

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-RateLimit-Limit", "5000")
		w.Header().Set("X-RateLimit-Remaining", "4200")
		w.Header().Set("X-RateLimit-Reset", "1750000000")
		w.Header().Set("X-RateLimit-Resource", "core")
		w.Header().Set("Location", blob.URL)
		w.WriteHeader(http.StatusFound)
	}))
	defer api.Close()

	s := testRateSource(api, "owner/repo", "tok123")
	if _, err := s.JobLog(42, false); err != nil {
		t.Fatal(err)
	}
	got, ok := s.RateLimit()
	if !ok {
		t.Fatal("JobLog's authenticated hop recorded nothing")
	}
	if got.Remaining != 4200 || got.Resource != "core" {
		t.Errorf("RateLimit() = %+v, want 4200 remaining on core", got)
	}
}

func TestRateStoreConcurrentAccess(t *testing.T) {
	st := newRateStore()
	var wg sync.WaitGroup
	for range 20 {
		wg.Add(2)
		go func() {
			defer wg.Done()
			h := http.Header{}
			h.Set("X-RateLimit-Limit", "5000")
			h.Set("X-RateLimit-Remaining", "4000")
			h.Set("X-RateLimit-Reset", "1750000000")
			h.Set("X-RateLimit-Resource", "graphql")
			st.record(h, "/graphql")
		}()
		go func() {
			defer wg.Done()
			_, _ = st.tightest()
		}()
	}
	wg.Wait()
}
