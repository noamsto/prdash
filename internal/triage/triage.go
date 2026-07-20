package triage

import (
	"fmt"

	"github.com/noamsto/prdash/internal/gh"
)

type Kind int

const (
	KindFallback Kind = iota
	KindDraft
	KindConflict
	KindChecksFailing
	KindChangesRequested
	KindBehind
	KindBlocked
	KindAwaitingReview
	KindChecksRunning
	KindReady
	KindPending
)

// Card is the triage summary for one PR: the top blocker, its one-key fix, and
// which expanded tab to deep-link into.
type Card struct {
	Kind        Kind
	Headline    string
	Failing     []string // failing check labels
	Running     []string // in-progress check labels
	ActionKey   string   // key the user presses to act ("" if none)
	ActionLabel string
	JumpTab     string // "" | "checks" | "reviews" | "conversation"
	AutoMerge   bool   // GitHub auto-merge is armed on this PR (display-only)
}

// Compute returns the highest-priority triage card for pr given its detail.
// Merge-state comes from detail (reliable per-PR); checks come from the PR rollup.
func Compute(pr gh.PR, d gh.PRDetail) Card {
	mss := d.MergeStateStatus
	failing := checksByState(pr, "fail")
	pending := checksByState(pr, "pending")

	c := computeCard(pr, d, mss, failing, pending)
	c.AutoMerge = pr.AutoMergeEnabled()
	return c
}

// computeCard is Compute's original branch logic, unchanged, extracted so
// Compute can stamp AutoMerge onto whichever branch fires.
func computeCard(pr gh.PR, d gh.PRDetail, mss string, failing, pending []string) Card {
	switch {
	case pr.IsDraft || mss == "DRAFT":
		return Card{Kind: KindDraft, Headline: "Draft — not ready",
			ActionKey: "M", ActionLabel: "Mark ready"}
	case mss == "DIRTY" || d.Mergeable == "CONFLICTING":
		return Card{Kind: KindConflict, Headline: "Conflicts with base",
			ActionKey: "enter", ActionLabel: "worktree to resolve"}
	case len(failing) > 0:
		return checksFailingCard(failing, pending)
	case pr.ReviewDecision == "CHANGES_REQUESTED":
		return Card{Kind: KindChangesRequested, Headline: "Changes requested",
			ActionKey: "enter", ActionLabel: "worktree to address", JumpTab: "reviews"}
	case mss == "BEHIND":
		return Card{Kind: KindBehind, Headline: "Behind base",
			ActionKey: "u", ActionLabel: "update branch"}
	// BLOCKED is often caused by a missing review; when so, the next case shows
	// the specific "awaiting review" card. Reserve this generic one for BLOCKED
	// from other protections (e.g. unresolved threads).
	case mss == "BLOCKED" && pr.ReviewDecision != "REVIEW_REQUIRED":
		return Card{Kind: KindBlocked, Headline: "Blocked by branch protection",
			JumpTab: "conversation"}
	case pr.ReviewDecision == "REVIEW_REQUIRED":
		return Card{Kind: KindAwaitingReview, Headline: awaitingHeadline(d),
			JumpTab: "reviews"}
	case len(pending) > 0 || mss == "UNSTABLE":
		return Card{Kind: KindChecksRunning, Headline: "Checks running…",
			Running: pending, JumpTab: "checks"}
	case mss == "CLEAN" || mss == "HAS_HOOKS":
		return Card{Kind: KindReady, Headline: "Ready to merge",
			ActionKey: "m", ActionLabel: "merge (squash)"}
	case mss == "UNKNOWN" || mss == "":
		return Card{Kind: KindPending, Headline: "Merge state pending…"}
	default:
		return Card{Kind: KindFallback, Headline: "", JumpTab: "conversation"}
	}
}

// Preliminary builds a best-effort card from list-only fields (no merge-state),
// so the quick view can show something the instant the cursor lands — before the
// per-PR detail fetch returns. Compute supersedes it once detail is cached.
func Preliminary(pr gh.PR) Card {
	c := preliminaryCard(pr)
	c.AutoMerge = pr.AutoMergeEnabled()
	return c
}

func preliminaryCard(pr gh.PR) Card {
	failing := checksByState(pr, "fail")
	pending := checksByState(pr, "pending")
	switch {
	case pr.IsDraft:
		return Card{Kind: KindDraft, Headline: "Draft — not ready",
			ActionKey: "M", ActionLabel: "Mark ready"}
	case len(failing) > 0:
		return checksFailingCard(failing, pending)
	case pr.ReviewDecision == "CHANGES_REQUESTED":
		return Card{Kind: KindChangesRequested, Headline: "Changes requested",
			ActionKey: "enter", ActionLabel: "worktree to address", JumpTab: "reviews"}
	case pr.CIState() == "pending":
		return Card{Kind: KindChecksRunning, Headline: "Checks running…",
			Running: pending, JumpTab: "checks"}
	case pr.ReviewDecision == "REVIEW_REQUIRED":
		return Card{Kind: KindAwaitingReview, Headline: "Awaiting review", JumpTab: "reviews"}
	default:
		return Card{Kind: KindFallback, Headline: ""}
	}
}

// checksFailingCard builds the failing-checks card, folding any still-running
// checks in as a second group so the summary shows both at once.
func checksFailingCard(failing, pending []string) Card {
	headline := ChecksFailingHeadline(len(failing))
	if len(pending) > 0 {
		headline = fmt.Sprintf("%d failing · %d running", len(failing), len(pending))
	}
	return Card{Kind: KindChecksFailing, Headline: headline,
		Failing: failing, Running: pending,
		ActionKey: "r", ActionLabel: "rerun checks", JumpTab: "checks"}
}

// ChecksFailingHeadline renders the failing-checks count with correct grammar.
func ChecksFailingHeadline(n int) string {
	if n == 1 {
		return "1 check failing"
	}
	return fmt.Sprintf("%d checks failing", n)
}

func checksByState(pr gh.PR, want string) []string {
	var out []string
	for _, c := range pr.Checks() {
		if checkState(c) == want {
			out = append(out, c.Label())
		}
	}
	return out
}

// checkState collapses one rollup entry, mirroring gh.PR.CIState's vocabulary.
func checkState(c gh.Check) string {
	s := c.State
	if s == "" {
		s = c.Conclusion
	}
	switch s {
	case "FAILURE", "ERROR", "TIMED_OUT", "CANCELLED":
		return "fail"
	case "PENDING", "QUEUED", "IN_PROGRESS", "":
		return "pending"
	default:
		return "pass"
	}
}

func awaitingHeadline(d gh.PRDetail) string {
	if len(d.ReviewRequests) > 0 {
		return "Waiting on @" + d.ReviewRequests[0].Login
	}
	return "Awaiting review"
}
