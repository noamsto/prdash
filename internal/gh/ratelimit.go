package gh

import (
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// RateSnapshot is one rate-limit bucket's budget as of the most recent response
// that reported it. GitHub returns these numbers on every API response, so
// prdash learns them passively and never spends a request to ask.
type RateSnapshot struct {
	Limit     int
	Remaining int
	Reset     time.Time
	Resource  string // "graphql" | "core", from x-ratelimit-resource
}

// fraction is the share of the bucket still available. A zero Limit is not a
// reading at all, so it reports 0 instead of dividing by zero; tightest skips
// those entries outright.
func (s RateSnapshot) fraction() float64 {
	if s.Limit <= 0 {
		return 0
	}
	return float64(s.Remaining) / float64(s.Limit)
}

// rateStore holds the latest snapshot per resource. GraphSource is used as a
// value and copied freely, so it holds this as a pointer: every copy must file
// into and read from one store, or a snapshot recorded through one copy would be
// invisible to the UI reading another.
type rateStore struct {
	mu sync.Mutex
	by map[string]RateSnapshot
}

func newRateStore() *rateStore { return &rateStore{by: map[string]RateSnapshot{}} }

// record files h's rate headers under their resource. A response whose limit,
// remaining and reset don't all parse is ignored — that gate is what keeps
// JobLog's unauthenticated blob-storage hop from clobbering the core bucket with
// a reading it never made, without having to exclude it by host.
func (s *rateStore) record(h http.Header, path string) {
	limit, errLimit := strconv.Atoi(h.Get("X-RateLimit-Limit"))
	remaining, errRemaining := strconv.Atoi(h.Get("X-RateLimit-Remaining"))
	reset, errReset := strconv.ParseInt(h.Get("X-RateLimit-Reset"), 10, 64)
	if errLimit != nil || errRemaining != nil || errReset != nil {
		return
	}
	s.put(RateSnapshot{
		Limit:     limit,
		Remaining: remaining,
		Reset:     time.Unix(reset, 0),
		Resource:  resourceOf(h, path),
	})
}

func (s *rateStore) put(snap RateSnapshot) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.by[snap.Resource] = snap
}

// resourceOf names the bucket a response spent from. GitHub labels it directly;
// the path fallback covers a response that omits the header.
func resourceOf(h http.Header, path string) string {
	if r := h.Get("X-RateLimit-Resource"); r != "" {
		return r
	}
	if strings.HasSuffix(path, "/graphql") {
		return "graphql"
	}
	return "core"
}

// tightest returns the bucket worth showing: graphql, unless another bucket has
// a strictly lower fraction remaining. Only buckets a real response reported are
// in the store, so core is simply absent until an Actions call spends from it.
func (s *rateStore) tightest() (RateSnapshot, bool) {
	if s == nil {
		return RateSnapshot{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	best, ok := RateSnapshot{}, false
	if g := s.by["graphql"]; g.Limit > 0 {
		best, ok = g, true
	}
	for res, snap := range s.by {
		if res == "graphql" || snap.Limit <= 0 {
			continue
		}
		if !ok || snap.fraction() < best.fraction() {
			best, ok = snap, true
		}
	}
	return best, ok
}

type rateTransport struct {
	next  http.RoundTripper
	store *rateStore
}

// RoundTrip injects the merge-info-preview Accept header on every outgoing
// GraphQL request that doesn't already carry one. Because GraphSource's checks,
// issues, viewer, members, and mutation calls all share this one transport,
// this header goes out on every one of them, not just the PR list query. That's
// expected to be harmless: GitHub's preview media types are additive — they
// unlock extra fields on the shapes that ask for them, and change nothing for
// queries that don't. It never overwrites an Accept header a caller already
// set, so detail_graphql.go's own explicit header (set directly on a raw
// http.Request built from s.http, not through this typed client) is untouched.
func (t *rateTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Path == "/graphql" && req.Header.Get("Accept") == "" {
		r2 := *req
		r2.Header = req.Header.Clone()
		r2.Header.Set("Accept", "application/vnd.github.merge-info-preview+json")
		req = &r2
	}
	resp, err := t.next.RoundTrip(req)
	if err != nil {
		return resp, err
	}
	t.store.record(resp.Header, req.URL.Path)
	return resp, nil
}

// RateLimit reports the tightest rate-limit budget observed so far, or false
// when no response has advertised one yet.
func (s GraphSource) RateLimit() (RateSnapshot, bool) { return s.rate.tightest() }
