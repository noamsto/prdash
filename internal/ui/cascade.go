package ui

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/noamsto/prdash/internal/gh"
)

// cascadeLink is one PR in a chain plus the merge state it was planned under.
// The state is snapshotted at plan time so a backgroundRefresh landing mid-run
// cannot change what the prompt promised or the run acts on.
type cascadeLink struct {
	pr  gh.PR
	mss string
}

type cascadeChain []cascadeLink

// cascadePlan is the result of walking every seed's stack. dropped holds the
// PR numbers of held stack members no chain reached, reported "not attempted"
// so a truncated cascade is never silent about the links it skipped.
type cascadePlan struct {
	chains  []cascadeChain
	dropped []int
}

// cascades reports whether any chain has more than one link — the trigger for
// the confirm prompt (C4). nil-safe so a *cascadePlan can be carried through
// the no-cascade path with no separate guard.
func (p *cascadePlan) cascades() bool {
	if p == nil {
		return false
	}
	for _, c := range p.chains {
		if len(c) > 1 {
			return true
		}
	}
	return false
}

// count returns the total number of links across all chains, for the
// "Update branch for N PRs in stack?" prompt.
func (p *cascadePlan) count() int {
	if p == nil {
		return 0
	}
	n := 0
	for _, c := range p.chains {
		n += len(c)
	}
	return n
}

// buildCascadePlan walks each seed's stack upward from its own position,
// stopping at the first position absent from shown or not OPEN (C2). A walk
// that reaches another seed absorbs it into the same chain rather than
// starting a second one — dedupe by PR.Number across the whole plan.
func buildCascadePlan(shown, seeds []gh.PR, state func(gh.PR) (mergeable, mss string)) *cascadePlan {
	byStack := make(map[int]map[int]gh.PR)
	for _, pr := range shown {
		if pr.Stack == nil {
			continue
		}
		positions := byStack[pr.Stack.Number]
		if positions == nil {
			positions = make(map[int]gh.PR)
			byStack[pr.Stack.Number] = positions
		}
		positions[pr.StackPosition] = pr
	}

	link := func(pr gh.PR) cascadeLink {
		_, mss := state(pr)
		return cascadeLink{pr: pr, mss: mss}
	}

	absorbed := make(map[int]bool)
	walkFrom := func(seed gh.PR) cascadeChain {
		chain := cascadeChain{link(seed)}
		absorbed[seed.Number] = true
		if seed.Stack != nil {
			positions := byStack[seed.Stack.Number]
			for pos := seed.StackPosition + 1; ; pos++ {
				next, ok := positions[pos]
				if !ok || next.State != "OPEN" {
					break
				}
				chain = append(chain, link(next))
				absorbed[next.Number] = true
			}
		}
		return chain
	}

	var chains []cascadeChain

	// Pass 1: walk each stack the seeds touch from its lowest-position seed
	// first. A walk only ever absorbs upward, so a higher seed given before a
	// lower one on the same stack must still land in the lower seed's chain,
	// not start a redundant one of its own — dedupe cannot depend on the
	// order seeds arrive in.
	seenStack := make(map[int]bool)
	for _, seed := range seeds {
		if seed.Stack == nil || seenStack[seed.Stack.Number] {
			continue
		}
		seenStack[seed.Stack.Number] = true
		lowest := seed
		for _, other := range seeds {
			if other.Stack != nil && other.Stack.Number == seed.Stack.Number && other.StackPosition < lowest.StackPosition {
				lowest = other
			}
		}
		chains = append(chains, walkFrom(lowest))
	}

	// Pass 2: non-stacked seeds, and any stacked seed pass 1's walk did not
	// reach (a gap higher up in the same stack), each start their own chain —
	// in seed order, so non-stacked chains keep the relative order the final
	// sort's stability depends on.
	for _, seed := range seeds {
		if absorbed[seed.Number] {
			continue
		}
		chains = append(chains, walkFrom(seed))
	}

	// Non-stacked seeds first in seed order, then stack chains by ascending
	// Stack.Number; a stable sort keeps that seed order for the former and
	// breaks ties within the same stack by the chain's own start position.
	slices.SortStableFunc(chains, func(a, b cascadeChain) int {
		as, bs := a[0].pr.Stack, b[0].pr.Stack
		switch {
		case as == nil && bs == nil:
			return 0
		case as == nil:
			return -1
		case bs == nil:
			return 1
		case as.Number != bs.Number:
			return as.Number - bs.Number
		default:
			return a[0].pr.StackPosition - b[0].pr.StackPosition
		}
	})

	// minSeed is, per stack, the lowest StackPosition among that stack's own
	// seeds — not the highest position any chain reached. Two seeds on the
	// same stack can produce two disjoint chains with a held gap between
	// them (held {1,2,4,5,6}, seeds 1 and 6 → chains [1,2] and [6]); keying
	// on "highest position any chain reached" would put the mark at 6 and
	// miss the held, OPEN, unabsorbed #4 and #5 sitting between the two
	// chains — a silent partial cascade. A walk only ever goes upward from
	// its seed, so everything above the lowest seed is in scope for this
	// run, and anything in scope that no chain absorbed was attempted and
	// missed, not merely out of range.
	minSeed := make(map[int]int)
	for _, seed := range seeds {
		if seed.Stack == nil {
			continue
		}
		if cur, ok := minSeed[seed.Stack.Number]; !ok || seed.StackPosition < cur {
			minSeed[seed.Stack.Number] = seed.StackPosition
		}
	}
	stackNums := make([]int, 0, len(minSeed))
	for n := range minSeed {
		stackNums = append(stackNums, n)
	}
	slices.Sort(stackNums)

	// A link above the lowest seed that is OPEN and unabsorbed is what the run
	// skipped; a MERGED or CLOSED link there is terminal, not skipped, and an
	// absorbed one is already accounted for inside a chain.
	var dropped []int
	for _, num := range stackNums {
		positions := byStack[num]
		posKeys := make([]int, 0, len(positions))
		for pos := range positions {
			if pos > minSeed[num] {
				posKeys = append(posKeys, pos)
			}
		}
		slices.Sort(posKeys)
		for _, pos := range posKeys {
			pr := positions[pos]
			if pr.State == "OPEN" && !absorbed[pr.Number] {
				dropped = append(dropped, pr.Number)
			}
		}
	}

	return &cascadePlan{chains: chains, dropped: dropped}
}

// The wait between links is bounded by a probe count rather than a deadline,
// so a test drives it by feeding messages with no clock and no sleeping.
const (
	cascadeProbeEvery = 1500 * time.Millisecond
	cascadeWaitProbes = 30
)

// cascadeStep is what the run decided to do next. The set is closed so the tea
// shell holds no policy of its own.
type cascadeStep int

const (
	cascadeMutate cascadeStep = iota // fire UpdateBranch for the current link
	cascadeProbe                     // schedule one probe beat
	cascadeSettle                    // the run is over
)

// cascadeOutcome is one link's result. A nil err means UpdateBranch returned
// successfully; a non-nil one is either that error, or the conflict or timeout
// the wait before the link's own mutation resolved to.
type cascadeOutcome struct {
	number int
	err    error
}

// cascadeRun is the sequential driver: it advances by consuming one outcome —
// a mutation result or a probe result — and returning what to do next.
type cascadeRun struct {
	plan        *cascadePlan
	chain, link int // cursor into plan.chains
	probes      int // probes spent on the current wait

	// lastProbeErr is the most recent probe's error for the current wait, so
	// exhaustion can tell "GitHub genuinely never resolved the state" (nil)
	// from "the probe itself kept failing" (persistent transport/auth/rate-
	// limit error) — the latter must not be reported as a bare timeout.
	lastProbeErr error

	// observedMSS is the mss the wait saw for the link that is about to
	// mutate; hasObserved is false when no wait preceded it.
	observedMSS string
	hasObserved bool

	done    []cascadeOutcome
	skipped []int // chain tails after a failure, plus plan.dropped

	// step is the last decision returned, so a caller can tell a mutation
	// (which it must fire) from a probe beat (which it must only schedule).
	step cascadeStep

	// stat is the run's own badge state. The run owns it rather than reading
	// m.actionStatus, because seven sites in expanded.go and logview.go
	// overwrite m.actionStatus without passing through runAction/startBulk.
	stat *actionStat
}

func (r *cascadeRun) setStep(s cascadeStep) cascadeStep {
	r.step = s
	return s
}

// start opens the run on the first chain's first link. No head has moved yet,
// so there is nothing to wait for.
func (r *cascadeRun) start() cascadeStep {
	return r.setStep(cascadeMutate)
}

// onUpdated records the result of the current link's mutation and decides the
// wait before the next one.
func (r *cascadeRun) onUpdated(err error) cascadeStep {
	chain := r.plan.chains[r.chain]
	cur := chain[r.link]
	r.done = append(r.done, cascadeOutcome{number: cur.pr.Number, err: err})

	var step cascadeStep
	switch {
	case err != nil, r.link+1 >= len(chain):
		step = r.nextChain()
	default:
		// The wait is decided from the *effective* pre-update state of the link
		// that just mutated: what the wait before it observed, else its
		// plan-time snapshot. Links 2..N of a stack are snapshotted CLEAN or
		// BLOCKED against their own, not-yet-moved bases, so deciding from the
		// snapshot alone would skip every wait after the first and degrade the
		// cascade to the fire-and-forget it exists to prevent.
		mss := cur.mss
		if r.hasObserved {
			mss = r.observedMSS
		}
		r.link++
		step = cascadeProbe
		if mergeStateResolved(mss) && mss != "BEHIND" {
			// A resolved, not-BEHIND link is a no-op update: its head does not
			// move, so the next link is never invalidated and there is no
			// timeout to swallow.
			step = cascadeMutate
		}
	}

	// Cleared on every branch, not just the one above: observed is only ever
	// set where mss == "BEHIND", so a value surviving into another chain always
	// reads "wait", and a healthy chain whose first link is CLEAN would burn
	// the whole probe budget waiting for a base that never moves.
	r.hasObserved = false
	r.probes = 0
	r.lastProbeErr = nil
	return r.setStep(step)
}

// onProbed evaluates C5's condition against one probe of the link that is
// waiting to mutate.
func (r *cascadeRun) onProbed(mergeable, mss string, err error) cascadeStep {
	// Budget is spent first, so a probe that errored or came back without this
	// number still consumes one; a source that only ever errors must end in a
	// timeout rather than spinning forever.
	r.probes++
	r.lastProbeErr = err
	cur := r.plan.chains[r.chain][r.link].pr

	// A failed probe's mergeable/mss carry no answer, so neither terminal test
	// may read them. An absent number arrives as the zero value, which fails
	// both tests on its own.
	if err == nil {
		// Conflict first: it is a resolved terminal answer, so no mutation
		// GitHub would reject is ever sent.
		if mergeConflicted(mergeable, mss) {
			return r.failLink(fmt.Errorf("PR #%d has conflicts", cur.Number))
		}
		if mss == "BEHIND" && !mergeUnresolved(mergeable) {
			// GitHub has noticed the moved base and finished recomputing.
			r.observedMSS = mss
			r.hasObserved = true
			return r.setStep(cascadeMutate)
		}
	}
	if r.probes >= cascadeWaitProbes {
		if r.lastProbeErr != nil {
			// The wait ended on a probe that itself failed — GitHub down, an
			// auth failure, a rate limit — which reads nothing like a benign
			// polling timeout and must not be discarded.
			return r.failLink(fmt.Errorf("probing PR #%d failed: %w", cur.Number, r.lastProbeErr))
		}
		return r.failLink(fmt.Errorf("timed out waiting for PR #%d to update", cur.Number))
	}
	return r.setStep(cascadeProbe)
}

// failLink fails the current link — which has not mutated — and stops its
// chain.
func (r *cascadeRun) failLink(err error) cascadeStep {
	cur := r.plan.chains[r.chain][r.link]
	r.done = append(r.done, cascadeOutcome{number: cur.pr.Number, err: err})
	return r.setStep(r.nextChain())
}

// nextChain abandons whatever the current chain has left and moves the cursor
// to the next chain's first link: a chain's failure stops that chain only. A
// chain that ran to its end has no remainder, so the same walk serves both.
func (r *cascadeRun) nextChain() cascadeStep {
	for _, l := range r.plan.chains[r.chain][r.link+1:] {
		r.skipped = append(r.skipped, l.pr.Number)
	}
	r.chain++
	r.link = 0
	if r.chain >= len(r.plan.chains) {
		return cascadeSettle
	}
	return cascadeMutate
}

// errored reports whether some link actually returned an error — a conflict,
// a timed-out wait, or UpdateBranch itself failing. It excludes links that
// were merely never attempted (skipped chain tails, or dropped gaps), which
// is what makes it narrower than failed().
func (r *cascadeRun) errored() bool {
	for _, o := range r.done {
		if o.err != nil {
			return true
		}
	}
	return false
}

// failed is the broader test: errored(), or anything skipped — skipped
// already carries plan.dropped (see its field doc), so a gapped stack that
// updated every link it reached has errored() == false but still fails here
// on its dropped tail alone.
func (r *cascadeRun) failed() bool {
	return r.errored() || len(r.skipped) > 0
}

// badge is the width-independent status-bar summary shown only when
// errored() put a non-nil error on the settle — statusBadge truncates it, so
// it carries no per-PR detail. The denominator is attempted (count()) plus
// dropped, not count() alone, so a gapped stack's untouched tail is never
// hidden behind a deceptively whole-looking ratio.
func (r *cascadeRun) badge() string {
	total := r.plan.count() + len(r.plan.dropped)
	return fmt.Sprintf("Stack update failed · %d/%d updated", len(r.updated()), total)
}

// updated returns the numbers of links whose mutation succeeded, feeding
// actionStat.partial so a partial cascade's landed links get the same
// refresh and CI-rerun treatment a full success would have given them.
func (r *cascadeRun) updated() []int {
	var nums []int
	for _, o := range r.done {
		if o.err == nil {
			nums = append(nums, o.number)
		}
	}
	return nums
}

// report renders the overlay body: one line per non-empty group, in
// updated / failed / not-attempted order. A failed link's own error text is
// folded into the failed line so a distinct conflict or timeout on each
// chain stays visible even though the group is a single line. A run that
// didn't fail has nothing to report — the overlay never opens for it (C8),
// so a fully-successful run's "updated" numbers stay on the badge alone.
func (r *cascadeRun) report() []string {
	if !r.failed() {
		return nil
	}

	var lines []string

	if nums := r.updated(); len(nums) > 0 {
		lines = append(lines, "updated "+numList(nums))
	}

	var failures []string
	for _, o := range r.done {
		if o.err != nil {
			failures = append(failures, fmt.Sprintf("#%d — %s", o.number, o.err))
		}
	}
	if len(failures) > 0 {
		lines = append(lines, "failed "+strings.Join(failures, "; "))
	}

	if len(r.skipped) > 0 {
		lines = append(lines, "not attempted "+numList(r.skipped))
	}

	return lines
}

// numList renders PR numbers as space-separated "#N" tokens.
func numList(nums []int) string {
	parts := make([]string, len(nums))
	for i, n := range nums {
		parts[i] = fmt.Sprintf("#%d", n)
	}
	return strings.Join(parts, " ")
}

// cascadePanel is the dismissible report overlay for a settled cascade (C8).
// It is only ever built while m.cascadeReport is non-empty, which run.report()
// only sets for a run that failed or left links unattempted — a clean run
// never reaches this.
func (m Model) cascadePanel() string {
	hint := statusBarStyle.Render("press ") + accentStyle.Render("any key") + statusBarStyle.Render(" to dismiss")

	longest := lipgloss.Width(hint)
	for _, line := range m.cascadeReport {
		longest = max(longest, lipgloss.Width(line))
	}
	w := max(34, longest+6)
	w = min(w, max(4, m.width)) // never wider than the terminal, even if that clips below 34

	// Interior width is fixed now, so re-clamp every line to it — a line still
	// too long past the terminal clamp must be cut rather than bleed the box.
	inner := max(1, w-2)
	lines := make([]string, len(m.cascadeReport))
	for i, line := range m.cascadeReport {
		lines[i] = truncate(line, inner)
	}
	hint = ansi.Truncate(hint, inner, "")

	body := strings.Join(lines, "\n") + "\n\n" + hint
	// overlayTop composites onto a canvas sized to the terminal, so an
	// unclamped height in a short terminal silently drops the bottom rows —
	// the dismiss hint and border included — off a box that still looks like
	// it should be dismissible. Measured off body itself, like
	// renderLegendPanes, rather than hand-counted: a hand-count desyncs the
	// moment body's shape changes.
	h := min(lipgloss.Height(body)+2, max(2, m.height))
	return titledBox(body, w, h, "Stack update")
}
