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
	Lines       []string // e.g. the failing check names
	ActionKey   string   // key the user presses to act ("" if none)
	ActionLabel string
	JumpTab     string // "" | "checks" | "reviews" | "conversation"
}

// Compute returns the highest-priority triage card for pr given its detail.
// Merge-state comes from detail (reliable per-PR); checks come from the PR rollup.
func Compute(pr gh.PR, d gh.PRDetail) Card {
	mss := d.MergeStateStatus
	failing := checksByState(pr, "fail")
	pending := checksByState(pr, "pending")

	switch {
	case pr.IsDraft || mss == "DRAFT":
		return Card{Kind: KindDraft, Headline: "Draft — not ready",
			ActionKey: "M", ActionLabel: "Mark ready"}
	case mss == "DIRTY" || d.Mergeable == "CONFLICTING":
		return Card{Kind: KindConflict, Headline: "Conflicts with base",
			ActionKey: "enter", ActionLabel: "worktree to resolve"}
	case len(failing) > 0:
		return Card{Kind: KindChecksFailing,
			Headline: fmt.Sprintf("%d checks failing", len(failing)), Lines: failing,
			ActionKey: "r", ActionLabel: "rerun failed", JumpTab: "checks"}
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
			Lines: pending, JumpTab: "checks"}
	case mss == "CLEAN" || mss == "HAS_HOOKS":
		return Card{Kind: KindReady, Headline: "Ready to merge",
			ActionKey: "m", ActionLabel: "merge (squash)"}
	case mss == "UNKNOWN" || mss == "":
		return Card{Kind: KindPending, Headline: "Merge state pending…"}
	default:
		return Card{Kind: KindFallback, Headline: "", JumpTab: "conversation"}
	}
}

func checksByState(pr gh.PR, want string) []string {
	var out []string
	for _, c := range pr.StatusCheckRollup {
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
