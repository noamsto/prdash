# Rate-limit Header Segment Design

**Status:** Approved (design reviewed)
**Issue:** #64

---

## Problem

Every GitHub response prdash receives carries `x-ratelimit-limit`,
`x-ratelimit-remaining`, `x-ratelimit-reset` and `x-ratelimit-resource`
headers, and all of them are discarded. There is no way to see how much budget
is left, or when it resets, until a call actually fails.

## Part 1 — Capture (`internal/gh/ratelimit.go`, new)

Passive scraping of responses prdash already makes. No polling, no
`GET /rate_limit` probe, no extra request of any kind.

```go
// RateSnapshot is one bucket's budget as of the last response that reported it.
type RateSnapshot struct {
	Limit     int
	Remaining int
	Reset     time.Time
	Resource  string // "graphql" | "core", from x-ratelimit-resource
}

type rateStore struct {
	mu sync.Mutex
	by map[string]RateSnapshot
}

// record files h's rate headers under their resource. Responses without an
// x-ratelimit-limit header are ignored.
func (s *rateStore) record(h http.Header, path string)

// tightest returns the bucket to display: graphql unless another bucket has a
// strictly lower fraction remaining.
func (s *rateStore) tightest() (RateSnapshot, bool)
```

**Recording is gated on `x-ratelimit-limit` being present.** That single
condition is what keeps the unauthenticated blob-storage hop in `JobLog` from
being a special case — blob storage sends no such header, so it is not a case
to exclude by host, it simply never records. Malformed or unparsable values are
treated the same way (no record), so a header format change degrades to "no
segment" rather than to a wrong number.

`Resource` comes from `x-ratelimit-resource`. When that header is absent, the
resource falls back to the request path: `/graphql` ⇒ `graphql`, else `core`.

### Bucket choice

`tightest()` returns `graphql` by default. Another bucket wins the slot only
when its `Remaining/Limit` fraction is *strictly* lower; graphql wins ties.
Because the store only ever holds buckets that a real response reported,
`core` has no entry at all until Actions is touched — so a session that never
opens the checks view reads as graphql-only without any special casing.

### Transport wiring

```go
type rateTransport struct {
	next  http.RoundTripper
	store *rateStore
}
```

`NewGraphSource` wraps the oauth2 transport with `rateTransport` and stores
`rate *rateStore` on `GraphSource`. The field is a **pointer**: `GraphSource`
is used as a value and copied freely, and every copy must share one store.

`JobLog`'s first hop deliberately bypasses `s.http` (its doc comment explains
why: the oauth2 transport would re-inject the token on the redirect to blob
storage and leak it off github.com). That hop is a real `core` spend, so
`timeoutHTTPClient` takes the recording transport for it. The second hop —
the unauthenticated blob fetch — stays bare, and needs no guard because of the
header gate above.

### Public surface

`internal/gh/source.go` gains, alongside the other backend interfaces:

```go
// RateSource reports the tightest rate-limit budget observed so far.
type RateSource interface {
	RateLimit() (RateSnapshot, bool)
}
```

`GraphSource.RateLimit()` delegates to `s.rate.tightest()`.

## Part 2 — Render (`internal/ui/ratebadge.go`, new)

```go
func rateSegment(s gh.RateSnapshot, now time.Time, avail int) string
```

A free function taking `now` explicitly, so every formatting and threshold case
is testable without a clock. It returns `""` — the segment is simply absent —
in each of these cases:

| Case | Why |
|---|---|
| `s.Limit == 0` | nothing observed yet |
| `!s.Reset.After(now)` | the window rolled over, so `Remaining` describes a spent budget and means nothing |
| rendered width `> avail` | no room in the header |

The expiry rule is what keeps staleness out of the model: a snapshot goes quiet
on its own once its window ends, and the next response repopulates it. No
freshness timestamp, no invalidation pass.

Otherwise the text is `<glyph> <remaining>/<limit> · <countdown>`, with a
`core ` label before the numbers only when `Resource != "graphql"` (the default
bucket is unlabelled — it is the normal reading):

```
  noamsto/prdash   PR  ISSUE                    ◔ 4832/5000 · 23m
  noamsto/prdash   PR  ISSUE                 ◔ core 120/5000 · 41m
```

Countdown is ceil-minutes to `Reset`, rendered `23m`, or `<1m` below a minute.
GitHub's windows are an hour, so minutes never exceed `60m` and no hour unit is
needed.

`rateGlyph` is declared next to the other glyphs in `internal/ui/theme.go` with
a `// nerd: nf-md-gauge` hint, using a plain-unicode placeholder for now.

### Styling tiers

Fraction remaining drives the style, reusing existing theme styles:

| Remaining | Style | Glyph |
|---|---|---|
| ≥ 25% | `dimStyle` | `rateGlyph` |
| < 25% | `pendStyle` | `rateGlyph` |
| < 10% | `failStyle` | `warnGlyph` |

### Header placement

`Model.header()` appends the segment last, right-aligned to the header's right
edge:

```go
seg := rateSegment(m.rate, time.Now(), m.width-lipgloss.Width(h)-rateGap)
if seg != "" {
	h += strings.Repeat(" ", m.width-lipgloss.Width(h)-lipgloss.Width(seg)) + seg
}
```

Appending last establishes the priority order **action badge > selection count >
rate segment**: the transient badge and the selection count claim their space
first, and the rate segment is the first thing to vanish on a narrow terminal.
The `avail` budget guarantees the header total never exceeds `m.width`, so the
segment can never widen the board frame it sits above.

## Part 3 — Freshness

`themeWatchTick` (`internal/ui/prlist.go`) is the only tick that runs for the
whole session, re-arming every second. Its handler samples the store into the
model:

```go
if m.rateSource != nil {
	if s, ok := m.rateSource.RateLimit(); ok {
		m.rate = s
	}
}
```

That gives the countdown its per-second re-render for free — no second ticker —
and keeps `View()` reading model state only. `Model` gains `rate
gh.RateSnapshot` and `rateSource gh.RateSource`, with `SetRateSource` following
the existing `Set*Source` pattern; `main.go` wires it from the same
`GraphSource` as every other backend.

A zero-value `rate` is the "nothing yet" state, so no parallel `bool` is
needed — `rateSegment`'s `Limit == 0` case already covers it.

## Testing

**`internal/gh`:**

- `record` parses limit/remaining/reset/resource from headers.
- A response with no `x-ratelimit-limit` records nothing (covers the blob hop).
- Malformed numeric and reset values record nothing.
- Resource falls back to the request path when `x-ratelimit-resource` is absent.
- `tightest` returns graphql alone; core alone; graphql when core is looser;
  core when core is strictly tighter; graphql on a tie.
- `RateLimit` reports what a `httptest`-served GraphQL response advertised
  (end-to-end through the transport).
- `JobLog`'s authenticated hop records `core`; its blob hop does not.
- Concurrent `record`/`tightest` is race-clean under `-race`.

**`internal/ui`:**

- Countdown formatting: multi-minute, `<1m`, and expired ⇒ `""`.
- `Limit == 0` ⇒ `""`.
- `core ` label present only for the core bucket.
- The three styling tiers select the expected style.
- Width degradation: segment dropped when `avail` is short, and
  `lipgloss.Width(m.header()) <= m.width` across a width sweep, following the
  existing `overflow_test.go` conventions.
