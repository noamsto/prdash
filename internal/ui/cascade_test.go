package ui

import (
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/noamsto/prdash/internal/action"
	"github.com/noamsto/prdash/internal/gh"
)

// stackedPR builds an OPEN PR at the given stack position; number and position
// coincide for readability since the two are unrelated in production data.
func stackedPR(stackNum, pos, size int) gh.PR {
	return gh.PR{
		Number:        pos,
		State:         "OPEN",
		Stack:         &gh.PRStack{Number: stackNum, Size: size},
		StackPosition: pos,
	}
}

func mergedStackedPR(stackNum, pos, size int) gh.PR {
	pr := stackedPR(stackNum, pos, size)
	pr.State = "MERGED"
	return pr
}

func noState(gh.PR) (string, string) { return "", "" }

func chainNumbers(c cascadeChain) []int {
	nums := make([]int, len(c))
	for i, l := range c {
		nums[i] = l.pr.Number
	}
	return nums
}

func intsEqual(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestBuildCascadePlanSingleStackFromBottomSeed(t *testing.T) {
	shown := []gh.PR{stackedPR(1, 1, 4), stackedPR(1, 2, 4), stackedPR(1, 3, 4), stackedPR(1, 4, 4)}
	plan := buildCascadePlan(shown, []gh.PR{shown[0]}, noState)

	if len(plan.chains) != 1 {
		t.Fatalf("chains = %d, want 1", len(plan.chains))
	}
	if got := chainNumbers(plan.chains[0]); !intsEqual(got, []int{1, 2, 3, 4}) {
		t.Errorf("chain = %v, want [1 2 3 4]", got)
	}
	if len(plan.dropped) != 0 {
		t.Errorf("dropped = %v, want none", plan.dropped)
	}
}

func TestBuildCascadePlanMidStackSeedOnlyLinksAbove(t *testing.T) {
	shown := []gh.PR{stackedPR(1, 1, 4), stackedPR(1, 2, 4), stackedPR(1, 3, 4), stackedPR(1, 4, 4)}
	plan := buildCascadePlan(shown, []gh.PR{shown[1]}, noState)

	if len(plan.chains) != 1 {
		t.Fatalf("chains = %d, want 1", len(plan.chains))
	}
	if got := chainNumbers(plan.chains[0]); !intsEqual(got, []int{2, 3, 4}) {
		t.Errorf("chain = %v, want [2 3 4], seed's walk must not reach below its own position", got)
	}
}

func TestBuildCascadePlanTopLinkSeedIsSingleNonCascadingChain(t *testing.T) {
	shown := []gh.PR{stackedPR(1, 1, 4), stackedPR(1, 2, 4)}
	plan := buildCascadePlan(shown, []gh.PR{shown[1]}, noState)

	if len(plan.chains) != 1 || len(plan.chains[0]) != 1 {
		t.Fatalf("chains = %+v, want one chain of length 1", plan.chains)
	}
	if plan.cascades() {
		t.Error("cascades() = true, want false for a top-link seed")
	}
}

func TestBuildCascadePlanGapDropsUnreachableTail(t *testing.T) {
	shown := []gh.PR{stackedPR(1, 1, 5), stackedPR(1, 2, 5), stackedPR(1, 4, 5), stackedPR(1, 5, 5)}
	plan := buildCascadePlan(shown, []gh.PR{shown[0]}, noState)

	if len(plan.chains) != 1 {
		t.Fatalf("chains = %d, want 1", len(plan.chains))
	}
	if got := chainNumbers(plan.chains[0]); !intsEqual(got, []int{1, 2}) {
		t.Errorf("chain = %v, want [1 2]", got)
	}
	if !intsEqual(plan.dropped, []int{4, 5}) {
		t.Errorf("dropped = %v, want [4 5]", plan.dropped)
	}
}

func TestBuildCascadePlanMergedLinkAboveGapIsNotDropped(t *testing.T) {
	shown := []gh.PR{
		stackedPR(1, 1, 5), stackedPR(1, 2, 5), mergedStackedPR(1, 3, 5),
		stackedPR(1, 4, 5), stackedPR(1, 5, 5),
	}
	plan := buildCascadePlan(shown, []gh.PR{shown[0]}, noState)

	if got := chainNumbers(plan.chains[0]); !intsEqual(got, []int{1, 2}) {
		t.Errorf("chain = %v, want [1 2]", got)
	}
	if !intsEqual(plan.dropped, []int{4, 5}) {
		t.Errorf("dropped = %v, want [4 5]: the MERGED link at 3 is terminal, not skipped", plan.dropped)
	}
}

func TestBuildCascadePlanClosedLinkAboveGapIsNotDropped(t *testing.T) {
	shown := []gh.PR{stackedPR(1, 1, 5), stackedPR(1, 2, 5)}
	closed := stackedPR(1, 4, 5)
	closed.State = "CLOSED"
	shown = append(shown, closed, stackedPR(1, 5, 5))

	plan := buildCascadePlan(shown, []gh.PR{shown[0]}, noState)

	if got := chainNumbers(plan.chains[0]); !intsEqual(got, []int{1, 2}) {
		t.Errorf("chain = %v, want [1 2]", got)
	}
	if !intsEqual(plan.dropped, []int{5}) {
		t.Errorf("dropped = %v, want [5]: the CLOSED link at 4 is terminal, not skipped", plan.dropped)
	}
}

func TestBuildCascadePlanMidStackSeedDoesNotDropLinksBelowIt(t *testing.T) {
	shown := []gh.PR{stackedPR(1, 1, 4), stackedPR(1, 2, 4), stackedPR(1, 3, 4), stackedPR(1, 4, 4)}
	plan := buildCascadePlan(shown, []gh.PR{shown[2]}, noState)

	if got := chainNumbers(plan.chains[0]); !intsEqual(got, []int{3, 4}) {
		t.Errorf("chain = %v, want [3 4]", got)
	}
	if len(plan.dropped) != 0 {
		t.Errorf("dropped = %v, want none: #1 and #2 were never targets, so they are not \"not attempted\"", plan.dropped)
	}
}

func TestBuildCascadePlanGapWithTwoSeedsMakesTwoChainsNoneDropped(t *testing.T) {
	shown := []gh.PR{stackedPR(1, 1, 5), stackedPR(1, 2, 5), stackedPR(1, 4, 5), stackedPR(1, 5, 5)}
	seeds := []gh.PR{shown[0], stackedPR(1, 4, 5)}
	plan := buildCascadePlan(shown, seeds, noState)

	if len(plan.chains) != 2 {
		t.Fatalf("chains = %d, want 2", len(plan.chains))
	}
	if got := chainNumbers(plan.chains[0]); !intsEqual(got, []int{1, 2}) {
		t.Errorf("first chain = %v, want [1 2]", got)
	}
	if got := chainNumbers(plan.chains[1]); !intsEqual(got, []int{4, 5}) {
		t.Errorf("second chain = %v, want [4 5]", got)
	}
	if len(plan.dropped) != 0 {
		t.Errorf("dropped = %v, want none: seed at 4 reaches the rest of the held tail", plan.dropped)
	}
}

func TestBuildCascadePlanDedupesRegardlessOfSeedOrder(t *testing.T) {
	shown := []gh.PR{stackedPR(1, 1, 4), stackedPR(1, 2, 4), stackedPR(1, 3, 4), stackedPR(1, 4, 4)}

	assertOneFullChain := func(t *testing.T, seeds []gh.PR) {
		t.Helper()
		plan := buildCascadePlan(shown, seeds, noState)

		if len(plan.chains) != 1 {
			t.Fatalf("chains = %d, want 1: a higher seed must land in the lower seed's chain, not start its own", len(plan.chains))
		}
		if got := chainNumbers(plan.chains[0]); !intsEqual(got, []int{1, 2, 3, 4}) {
			t.Errorf("chain = %v, want [1 2 3 4]", got)
		}
		if len(plan.dropped) != 0 {
			t.Errorf("dropped = %v, want none", plan.dropped)
		}
		seen := map[int]bool{}
		for _, c := range plan.chains {
			for _, l := range c {
				if seen[l.pr.Number] {
					t.Fatalf("PR #%d appears in more than one chain", l.pr.Number)
				}
				seen[l.pr.Number] = true
			}
		}
	}

	assertOneFullChain(t, []gh.PR{shown[2], shown[0]}) // pos3, pos1: descending
	assertOneFullChain(t, []gh.PR{shown[0], shown[2]}) // pos1, pos3: ascending — same plan
}

func TestBuildCascadePlanMergedSeedSurvives(t *testing.T) {
	seed := mergedStackedPR(1, 3, 3)
	plan := buildCascadePlan([]gh.PR{seed}, []gh.PR{seed}, noState)

	if len(plan.chains) != 1 || len(plan.chains[0]) != 1 || plan.chains[0][0].pr.Number != seed.Number {
		t.Fatalf("chains = %+v, want a single chain holding the MERGED seed unfiltered", plan.chains)
	}
}

func TestBuildCascadePlanBulkSelectionDedupesToOneEntryPerNumber(t *testing.T) {
	shown := []gh.PR{stackedPR(1, 1, 2), stackedPR(1, 2, 2)}
	// Both rows of the same stack selected explicitly, plus the same PR seeded twice.
	seeds := []gh.PR{shown[0], shown[1], shown[0]}
	plan := buildCascadePlan(shown, seeds, noState)

	if plan.count() != 2 {
		t.Fatalf("count() = %d, want 2 (no duplicate entries)", plan.count())
	}
	if len(plan.chains) != 1 {
		t.Fatalf("chains = %d, want 1: the second seed is absorbed by the first seed's walk", len(plan.chains))
	}
}

func TestBuildCascadePlanOrdersStacksByNumberNonStackedFirst(t *testing.T) {
	nonStackedA := gh.PR{Number: 100, State: "OPEN"}
	nonStackedB := gh.PR{Number: 101, State: "OPEN"}
	stackFive := stackedPR(5, 50, 1)
	stackTwo := stackedPR(2, 20, 1)
	shown := []gh.PR{nonStackedA, nonStackedB, stackFive, stackTwo}
	seeds := []gh.PR{nonStackedA, stackFive, nonStackedB, stackTwo}

	plan := buildCascadePlan(shown, seeds, noState)

	if len(plan.chains) != 4 {
		t.Fatalf("chains = %d, want 4", len(plan.chains))
	}
	var firstNums []int
	for _, c := range plan.chains {
		firstNums = append(firstNums, c[0].pr.Number)
	}
	want := []int{nonStackedA.Number, nonStackedB.Number, stackTwo.Number, stackFive.Number}
	if !intsEqual(firstNums, want) {
		t.Errorf("chain order = %v, want %v (non-stacked in seed order, then ascending Stack.Number)", firstNums, want)
	}
}

func TestBuildCascadePlanNilPlanDoesNotCascade(t *testing.T) {
	var plan *cascadePlan
	if plan.cascades() {
		t.Error("cascades() on a nil plan = true, want false")
	}
	if plan.count() != 0 {
		t.Errorf("count() on a nil plan = %d, want 0", plan.count())
	}
}

// mergeReading is a (mergeable, mss) pair — either a link's plan-time snapshot
// or one probe's answer, with err set only for a probe that failed.
type mergeReading struct {
	mergeable string
	mss       string
	err       error
}

func snapshots(byNumber map[int]mergeReading) func(gh.PR) (string, string) {
	return func(pr gh.PR) (string, string) {
		s := byNumber[pr.Number]
		return s.mergeable, s.mss
	}
}

// cascadePump answers every decision the run returns until it settles, so a
// test scripts GitHub's answers instead of orchestrating the machine. A
// cascadeMutate is answered from mutErrs, a cascadeProbe from that PR's queued
// readings (the last repeats, and an empty queue reads as "nothing computed
// yet"), which is how a never-resolving source is expressed.
type cascadePump struct {
	run      *cascadeRun
	readings map[int][]mergeReading
	mutErrs  map[int]error

	mutated []int // numbers UpdateBranch was fired for, in order
	probed  []int // numbers probed, one entry per probe
	steps   []cascadeStep
}

func newPump(plan *cascadePlan) *cascadePump {
	return &cascadePump{
		run:      &cascadeRun{plan: plan, skipped: plan.dropped},
		readings: map[int][]mergeReading{},
		mutErrs:  map[int]error{},
	}
}

func (p *cascadePump) currentNumber() int {
	return p.run.plan.chains[p.run.chain][p.run.link].pr.Number
}

func (p *cascadePump) nextReading(number int) mergeReading {
	q := p.readings[number]
	if len(q) == 0 {
		return mergeReading{}
	}
	if len(q) > 1 {
		p.readings[number] = q[1:]
	}
	return q[0]
}

func (p *cascadePump) drive(t *testing.T) {
	t.Helper()
	step := p.run.start()
	p.steps = append(p.steps, step)
	for budget := 0; step != cascadeSettle; budget++ {
		if budget > 8*cascadeWaitProbes {
			t.Fatalf("run did not settle after %d decisions: steps=%v", budget, p.steps)
		}
		if p.run.step != step {
			t.Fatalf("run.step = %v, want %v: the pump reads step to tell a mutation from a beat", p.run.step, step)
		}
		n := p.currentNumber()
		switch step {
		case cascadeMutate:
			p.mutated = append(p.mutated, n)
			step = p.run.onUpdated(p.mutErrs[n])
		case cascadeProbe:
			p.probed = append(p.probed, n)
			r := p.nextReading(n)
			step = p.run.onProbed(r.mergeable, r.mss, r.err)
		}
		p.steps = append(p.steps, step)
	}
	if p.run.step != cascadeSettle {
		t.Fatalf("run.step = %v after settling, want cascadeSettle", p.run.step)
	}
}

func (p *cascadePump) failures() map[int]error {
	errs := map[int]error{}
	for _, o := range p.run.done {
		if o.err != nil {
			errs[o.number] = o.err
		}
	}
	return errs
}

func stackPlan(t *testing.T, positions []int, seedPos int, states map[int]mergeReading) *cascadePlan {
	t.Helper()
	shown := make([]gh.PR, len(positions))
	var seed gh.PR
	for i, pos := range positions {
		shown[i] = stackedPR(1, pos, len(positions))
		if pos == seedPos {
			seed = shown[i]
		}
	}
	return buildCascadePlan(shown, []gh.PR{seed}, snapshots(states))
}

func TestCascadeStartAlwaysMutates(t *testing.T) {
	plan := stackPlan(t, []int{1, 2}, 1, nil)
	run := &cascadeRun{plan: plan}

	if got := run.start(); got != cascadeMutate {
		t.Errorf("start() = %v, want cascadeMutate", got)
	}
	if run.step != cascadeMutate {
		t.Errorf("run.step = %v, want cascadeMutate", run.step)
	}
}

func TestCascadeWaitProceedsOnlyAfterTheResolvedProbe(t *testing.T) {
	plan := stackPlan(t, []int{1, 2}, 1, map[int]mergeReading{
		1: {mergeable: "MERGEABLE", mss: "BEHIND"},
		2: {mergeable: "MERGEABLE", mss: "CLEAN"},
	})
	p := newPump(plan)
	p.readings[2] = []mergeReading{
		{mergeable: "UNKNOWN", mss: "BEHIND"},
		{mergeable: "UNKNOWN", mss: "BEHIND"},
		{mergeable: "MERGEABLE", mss: "BEHIND"},
	}

	p.drive(t)

	// One decision per answer: the mutation of #1, then a probe after each of
	// the two unresolved readings, then #2's mutation on the third.
	want := []cascadeStep{
		cascadeMutate, cascadeProbe, cascadeProbe, cascadeProbe, cascadeMutate, cascadeSettle,
	}
	if len(p.steps) != len(want) {
		t.Fatalf("steps = %v, want %v", p.steps, want)
	}
	for i := range want {
		if p.steps[i] != want[i] {
			t.Fatalf("steps = %v, want %v: #2 must mutate only after the third probe", p.steps, want)
		}
	}
	if !intsEqual(p.mutated, []int{1, 2}) {
		t.Errorf("mutated = %v, want [1 2]", p.mutated)
	}
	if !intsEqual(p.probed, []int{2, 2, 2}) {
		t.Errorf("probed = %v, want three probes of #2", p.probed)
	}
}

// The carry-forward regression: links 2 and 3 are snapshotted CLEAN against
// their own not-yet-moved bases, so a decision taken from the snapshot would
// skip the wait before #3 and issue zero probes for it.
func TestCascadeCarriesObservedStateForwardAcrossLinks(t *testing.T) {
	plan := stackPlan(t, []int{1, 2, 3}, 1, map[int]mergeReading{
		1: {mergeable: "MERGEABLE", mss: "BEHIND"},
		2: {mergeable: "MERGEABLE", mss: "CLEAN"},
		3: {mergeable: "MERGEABLE", mss: "CLEAN"},
	})
	p := newPump(plan)
	p.readings[2] = []mergeReading{{mergeable: "MERGEABLE", mss: "BEHIND"}}
	p.readings[3] = []mergeReading{{mergeable: "MERGEABLE", mss: "BEHIND"}}

	p.drive(t)

	if !intsEqual(p.mutated, []int{1, 2, 3}) {
		t.Fatalf("mutated = %v, want [1 2 3]", p.mutated)
	}
	if !intsEqual(p.probed, []int{2, 3}) {
		t.Errorf("probed = %v, want [2 3]: #3's wait must be decided from what #2's own wait observed, not #2's CLEAN snapshot", p.probed)
	}
}

func TestCascadeAlreadyBehindNextLinkStillProbesOnce(t *testing.T) {
	plan := stackPlan(t, []int{1, 2}, 1, map[int]mergeReading{
		1: {mergeable: "MERGEABLE", mss: "BEHIND"},
		2: {mergeable: "MERGEABLE", mss: "BEHIND"},
	})
	p := newPump(plan)
	p.readings[2] = []mergeReading{{mergeable: "MERGEABLE", mss: "BEHIND"}}

	p.drive(t)

	if !intsEqual(p.mutated, []int{1, 2}) {
		t.Fatalf("mutated = %v, want [1 2]", p.mutated)
	}
	if !intsEqual(p.probed, []int{2}) {
		t.Errorf("probed = %v, want exactly one probe of #2, never zero", p.probed)
	}
}

func TestCascadeSkipsWaitWhenLinkIsResolvedAndNotBehind(t *testing.T) {
	for _, mss := range []string{"CLEAN", "BLOCKED"} {
		t.Run(mss, func(t *testing.T) {
			plan := stackPlan(t, []int{1, 2}, 1, map[int]mergeReading{
				1: {mergeable: "MERGEABLE", mss: mss},
				2: {mergeable: "MERGEABLE", mss: "CLEAN"},
			})
			p := newPump(plan)

			p.drive(t)

			if !intsEqual(p.mutated, []int{1, 2}) {
				t.Fatalf("mutated = %v, want [1 2]", p.mutated)
			}
			if len(p.probed) != 0 {
				t.Errorf("probed = %v, want none: a resolved not-BEHIND link is a no-op update", p.probed)
			}
		})
	}
}

func TestCascadeWaitsWhenLinkStateIsUnresolved(t *testing.T) {
	for _, mss := range []string{"", "UNKNOWN"} {
		t.Run("mss="+mss, func(t *testing.T) {
			plan := stackPlan(t, []int{1, 2}, 1, map[int]mergeReading{
				1: {mergeable: "UNKNOWN", mss: mss},
				2: {mergeable: "MERGEABLE", mss: "CLEAN"},
			})
			p := newPump(plan)
			p.readings[2] = []mergeReading{{mergeable: "MERGEABLE", mss: "BEHIND"}}

			p.drive(t)

			if !intsEqual(p.probed, []int{2}) {
				t.Errorf("probed = %v, want one probe: an unresolved link may still move its head", p.probed)
			}
			if !intsEqual(p.mutated, []int{1, 2}) {
				t.Errorf("mutated = %v, want [1 2]", p.mutated)
			}
		})
	}
}

func TestCascadeTimesOutOnANeverResolvingSource(t *testing.T) {
	plan := stackPlan(t, []int{1, 2}, 1, map[int]mergeReading{
		1: {mergeable: "MERGEABLE", mss: "BEHIND"},
	})
	p := newPump(plan)

	p.drive(t)

	if !intsEqual(p.mutated, []int{1}) {
		t.Fatalf("mutated = %v, want [1]: #2 must never mutate after a timeout", p.mutated)
	}
	if len(p.probed) != cascadeWaitProbes {
		t.Errorf("probes = %d, want exactly %d", len(p.probed), cascadeWaitProbes)
	}
	if err := p.failures()[2]; err == nil {
		t.Fatal("#2 has no failure, want a timeout")
	} else if !strings.Contains(err.Error(), "timed out") {
		t.Errorf("#2 failed with %v, want a timeout", err)
	}
}

func TestCascadeErroredProbesConsumeBudgetAndTimeOut(t *testing.T) {
	plan := stackPlan(t, []int{1, 2}, 1, map[int]mergeReading{
		1: {mergeable: "MERGEABLE", mss: "BEHIND"},
	})
	p := newPump(plan)
	// A probe that failed carries no answer, and one whose number was absent
	// from the returned map arrives as the zero reading; both spend budget.
	p.readings[2] = []mergeReading{{err: errors.New("api down")}}

	p.drive(t)

	if len(p.probed) != cascadeWaitProbes {
		t.Errorf("probes = %d, want exactly %d: an errored probe consumes budget", len(p.probed), cascadeWaitProbes)
	}
	if err := p.failures()[2]; err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Errorf("#2 failed with %v, want a timeout", err)
	}
	if !intsEqual(p.mutated, []int{1}) {
		t.Errorf("mutated = %v, want [1]", p.mutated)
	}
}

func TestCascadeConflictFailsTheLinkWithoutMutating(t *testing.T) {
	tests := []struct {
		name    string
		reading mergeReading
	}{
		{"CONFLICTING", mergeReading{mergeable: "CONFLICTING", mss: "BEHIND"}},
		{"DIRTY", mergeReading{mergeable: "MERGEABLE", mss: "DIRTY"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			plan := stackPlan(t, []int{1, 2}, 1, map[int]mergeReading{
				1: {mergeable: "MERGEABLE", mss: "BEHIND"},
			})
			p := newPump(plan)
			p.readings[2] = []mergeReading{tc.reading}

			p.drive(t)

			if !intsEqual(p.mutated, []int{1}) {
				t.Fatalf("mutated = %v, want [1]: a conflict is terminal, so no mutation is sent", p.mutated)
			}
			if len(p.probed) != 1 {
				t.Errorf("probes = %d, want 1: a conflict is answered on the first probe", len(p.probed))
			}
			if err := p.failures()[2]; err == nil || !strings.Contains(err.Error(), "conflicts") {
				t.Errorf("#2 failed with %v, want a conflict", err)
			}
		})
	}
}

func TestCascadeFailureStopsOnlyItsOwnChain(t *testing.T) {
	shown := []gh.PR{
		stackedPR(1, 1, 3), stackedPR(1, 2, 3), stackedPR(1, 3, 3),
		stackedPR(2, 20, 2), stackedPR(2, 21, 2),
	}
	states := map[int]mergeReading{
		1: {mergeable: "MERGEABLE", mss: "BEHIND"},
		2: {mergeable: "MERGEABLE", mss: "CLEAN"},
		3: {mergeable: "MERGEABLE", mss: "CLEAN"},

		20: {mergeable: "MERGEABLE", mss: "BEHIND"},
		21: {mergeable: "MERGEABLE", mss: "CLEAN"},
	}
	plan := buildCascadePlan(shown, []gh.PR{shown[0], shown[3]}, snapshots(states))
	p := newPump(plan)
	p.mutErrs[1] = errors.New("update rejected")
	p.readings[21] = []mergeReading{{mergeable: "MERGEABLE", mss: "BEHIND"}}

	p.drive(t)

	if !intsEqual(p.mutated, []int{1, 20, 21}) {
		t.Fatalf("mutated = %v, want [1 20 21]: chain 1's failure must not stop chain 2", p.mutated)
	}
	if !intsEqual(p.run.skipped, []int{2, 3}) {
		t.Errorf("skipped = %v, want [2 3]: only the failed chain's tail", p.run.skipped)
	}
	if p.failures()[1] == nil {
		t.Error("#1 has no failure recorded")
	}
	if len(p.failures()) != 1 {
		t.Errorf("failures = %v, want only #1", p.failures())
	}
}

// A wait in chain 1 sets observed BEHIND. Chain 2's first link is CLEAN, so it
// is a no-op update whose head never moves; a leaked observed value would read
// "wait" and burn the whole probe budget on a healthy chain.
func TestCascadeObservedStateDoesNotLeakAcrossChains(t *testing.T) {
	shown := []gh.PR{
		stackedPR(1, 1, 2), stackedPR(1, 2, 2),
		stackedPR(2, 20, 2), stackedPR(2, 21, 2),
	}
	states := map[int]mergeReading{
		1: {mergeable: "MERGEABLE", mss: "BEHIND"},
		2: {mergeable: "MERGEABLE", mss: "CLEAN"},

		20: {mergeable: "MERGEABLE", mss: "CLEAN"},
		21: {mergeable: "MERGEABLE", mss: "CLEAN"},
	}
	plan := buildCascadePlan(shown, []gh.PR{shown[0], shown[2]}, snapshots(states))
	p := newPump(plan)
	p.readings[2] = []mergeReading{{mergeable: "MERGEABLE", mss: "BEHIND"}}

	p.drive(t)

	if !intsEqual(p.mutated, []int{1, 2, 20, 21}) {
		t.Fatalf("mutated = %v, want [1 2 20 21]", p.mutated)
	}
	if !intsEqual(p.probed, []int{2}) {
		t.Errorf("probed = %v, want only [2]: chain 2 must issue zero probes", p.probed)
	}
	if len(p.failures()) != 0 {
		t.Errorf("failures = %v, want none", p.failures())
	}
}

// A timed-out wait leaves the probe counter at the budget, so the next chain's
// own wait must start from zero rather than timing out on its first probe.
func TestCascadeProbeBudgetResetsForTheNextChain(t *testing.T) {
	shown := []gh.PR{
		stackedPR(1, 1, 2), stackedPR(1, 2, 2),
		stackedPR(2, 20, 2), stackedPR(2, 21, 2),
	}
	states := map[int]mergeReading{
		1: {mergeable: "MERGEABLE", mss: "BEHIND"},
		2: {mergeable: "MERGEABLE", mss: "CLEAN"},

		20: {mergeable: "MERGEABLE", mss: "BEHIND"},
		21: {mergeable: "MERGEABLE", mss: "CLEAN"},
	}
	plan := buildCascadePlan(shown, []gh.PR{shown[0], shown[2]}, snapshots(states))
	p := newPump(plan)
	p.readings[21] = []mergeReading{{mergeable: "MERGEABLE", mss: "BEHIND"}}

	p.drive(t)

	if !intsEqual(p.mutated, []int{1, 20, 21}) {
		t.Fatalf("mutated = %v, want [1 20 21]", p.mutated)
	}
	if got := len(p.probed); got != cascadeWaitProbes+1 {
		t.Errorf("probes = %d, want %d: #2's exhausted budget plus one for #21", got, cascadeWaitProbes+1)
	}
	if err := p.failures()[2]; err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Errorf("#2 failed with %v, want a timeout", err)
	}
}

// Reuses TestCascadeFailureStopsOnlyItsOwnChain's scenario: chain 1 fails at
// its first link, dragging #2 and #3 down as unattempted; chain 2 runs to
// completion untouched by chain 1's failure.
func TestCascadeReportGroupsUpdatedFailedAndNotAttempted(t *testing.T) {
	shown := []gh.PR{
		stackedPR(1, 1, 3), stackedPR(1, 2, 3), stackedPR(1, 3, 3),
		stackedPR(2, 20, 2), stackedPR(2, 21, 2),
	}
	states := map[int]mergeReading{
		1: {mergeable: "MERGEABLE", mss: "BEHIND"},
		2: {mergeable: "MERGEABLE", mss: "CLEAN"},
		3: {mergeable: "MERGEABLE", mss: "CLEAN"},

		20: {mergeable: "MERGEABLE", mss: "BEHIND"},
		21: {mergeable: "MERGEABLE", mss: "CLEAN"},
	}
	plan := buildCascadePlan(shown, []gh.PR{shown[0], shown[3]}, snapshots(states))
	p := newPump(plan)
	p.mutErrs[1] = errors.New("update rejected")
	p.readings[21] = []mergeReading{{mergeable: "MERGEABLE", mss: "BEHIND"}}

	p.drive(t)

	if !p.run.errored() {
		t.Error("errored() = false, want true: PR #1's mutation returned an error")
	}
	if !p.run.failed() {
		t.Error("failed() = false, want true")
	}
	if !intsEqual(p.run.updated(), []int{20, 21}) {
		t.Errorf("updated() = %v, want [20 21]", p.run.updated())
	}
	want := []string{
		"updated #20 #21",
		"failed #1 — update rejected",
		"not attempted #2 #3",
	}
	if got := p.run.report(); !slices.Equal(got, want) {
		t.Errorf("report() = %v, want %v", got, want)
	}
	if got, want := p.run.badge(), "Stack update failed · 2/5 updated"; got != want {
		t.Errorf("badge() = %q, want %q", got, want)
	}
}

// A fully-successful run reports nothing: no failed line, no not-attempted
// line, and failed() itself is false so the overlay never opens.
func TestCascadeReportFullSuccessHasNoLines(t *testing.T) {
	plan := stackPlan(t, []int{1, 2}, 1, map[int]mergeReading{
		1: {mergeable: "MERGEABLE", mss: "CLEAN"},
		2: {mergeable: "MERGEABLE", mss: "CLEAN"},
	})
	p := newPump(plan)

	p.drive(t)

	if p.run.errored() {
		t.Error("errored() = true, want false")
	}
	if p.run.failed() {
		t.Error("failed() = true, want false: nothing failed or was left unattempted")
	}
	if got := p.run.report(); len(got) != 0 {
		t.Errorf("report() = %v, want no lines", got)
	}
}

// A gapped stack (C2) that updates every link it reaches fails at nothing —
// errored() is false — but its dropped tail still must not go unreported.
func TestCascadeReportNotAttemptedOnlyForGappedStack(t *testing.T) {
	shown := []gh.PR{stackedPR(1, 1, 5), stackedPR(1, 2, 5), stackedPR(1, 4, 5), stackedPR(1, 5, 5)}
	states := map[int]mergeReading{
		1: {mergeable: "MERGEABLE", mss: "CLEAN"},
		2: {mergeable: "MERGEABLE", mss: "CLEAN"},
	}
	plan := buildCascadePlan(shown, []gh.PR{shown[0]}, snapshots(states))
	p := newPump(plan)

	p.drive(t)

	if p.run.errored() {
		t.Error("errored() = true, want false: the gap failed at nothing")
	}
	if !p.run.failed() {
		t.Error("failed() = false, want true: the dropped tail still owes a report")
	}
	want := []string{
		"updated #1 #2",
		"not attempted #4 #5",
	}
	if got := p.run.report(); !slices.Equal(got, want) {
		t.Errorf("report() = %v, want %v", got, want)
	}
	if got, want := p.run.badge(), "Stack update failed · 2/4 updated"; got != want {
		t.Errorf("badge() = %q, want %q: denominator is attempted (2) + dropped (2), not count() alone", got, want)
	}
}

// sequencedDetailSource answers one FetchDetails call at a time, in order (the
// last reading repeats once exhausted), for whatever single number this
// test's probe asks about. errs scripts an errored call at a given index —
// independent of readings, so an error can sit anywhere in the sequence.
type sequencedDetailSource struct {
	readings []gh.PRDetail
	errs     []error
	calls    int
}

func (f *sequencedDetailSource) FetchDetails(nums []int) (map[int]gh.PRDetail, map[int][]byte, error) {
	i := f.calls
	f.calls++
	if i < len(f.errs) && f.errs[i] != nil {
		return nil, nil, f.errs[i]
	}
	ri := i
	if ri >= len(f.readings) {
		ri = len(f.readings) - 1
	}
	d := f.readings[ri]
	out := map[int]gh.PRDetail{}
	for _, n := range nums {
		out[n] = d
	}
	return out, nil, nil
}

// driveCascadeProbes feeds cascadeProbeMsg directly rather than waiting on the
// real 1.5s tea.Tick, executing each probe's fetch inline, until the run
// leaves the wait (a decision other than cascadeProbe) or the budget runs out.
// It returns the command that decision produced. The caller must only call
// this while m.cascade.step is already cascadeProbe. Named to avoid colliding
// with a later step's general driveCascade pump.
func driveCascadeProbes(t *testing.T, m *Model, budget int) tea.Cmd {
	t.Helper()
	for i := 0; i < budget; i++ {
		next, fetch := m.Update(cascadeProbeMsg{})
		*m = next.(Model)
		if fetch == nil {
			t.Fatal("cascadeProbeMsg produced no fetch command")
		}
		probed, cmd := m.Update(fetch())
		*m = probed.(Model)
		if m.cascade == nil || m.cascade.step != cascadeProbe {
			return cmd
		}
	}
	t.Fatalf("probe budget %d exhausted without leaving the wait", budget)
	return nil
}

// TestCascadeProbeReadsLiveDetailNotThePlanTimeSnapshot pins a regression: the
// probe fold must take the fetched detail verbatim, not run it through
// mergeState. mergeState prefers a resolved *cached* value over an unresolved
// one and falls back to the PR's own (plan-time) fields otherwise — correct
// for a merge pre-check, backwards for a live probe. A link snapshotted as
// already-resolved (the common case) would then read as "settled" on the very
// first beat regardless of what the probe actually saw, defeating the wait
// C5 exists for.
func TestCascadeProbeReadsLiveDetailNotThePlanTimeSnapshot(t *testing.T) {
	pr1 := stackedPR(1, 1, 2)
	pr1.ID = "pr-1"
	pr2 := stackedPR(1, 2, 2)
	pr2.ID = "pr-2"
	// The link being probed already carries a resolved Mergeable/MergeStateStatus
	// on its own gh.PR fields — exactly what mergeState falls back to when the
	// probe's own detail is unresolved (UNKNOWN).
	pr2.Mergeable = "MERGEABLE"
	pr2.MergeStateStatus = "BEHIND"

	// The seed's plan-time snapshot is BEHIND, so onUpdated waits before link 2.
	states := map[int]mergeReading{1: {mergeable: "MERGEABLE", mss: "BEHIND"}}
	plan := buildCascadePlan([]gh.PR{pr1, pr2}, []gh.PR{pr1}, snapshots(states))

	mut := &fakeMutationSource{}
	fd := &sequencedDetailSource{readings: []gh.PRDetail{
		{Mergeable: "UNKNOWN", MergeStateStatus: "BEHIND"},
		{Mergeable: "UNKNOWN", MergeStateStatus: "BEHIND"},
		{Mergeable: "MERGEABLE", MergeStateStatus: "BEHIND"},
	}}

	m := NewModel("/repo", "is:open", nil)
	m.SetMutationSource(mut)
	m.SetDetailSource(fd)
	m.setPRs([]gh.PR{pr1, pr2})
	m.cascade = &cascadeRun{plan: plan, stat: &actionStat{}, skipped: plan.dropped}

	// Fire link 1's mutation, the way runCascade's initial dispatch would.
	next, _ := m.Update(m.cascadeMutateCmd()())
	m = next.(Model)
	if m.cascade.step != cascadeProbe {
		t.Fatalf("step after link 1 mutates = %v, want cascadeProbe", m.cascade.step)
	}

	mutateCmd := driveCascadeProbes(t, &m, 10)

	if fd.calls != 3 {
		t.Fatalf("probes fired = %d, want exactly 3 (two UNKNOWN, then the resolved BEHIND read)", fd.calls)
	}
	if len(mut.updateBranchCalls) != 1 {
		t.Fatalf("UpdateBranch calls while the wait was still open = %d, want 1 (link 1 only)", len(mut.updateBranchCalls))
	}
	if m.cascade.step != cascadeMutate {
		t.Fatalf("step once the wait resolves = %v, want cascadeMutate", m.cascade.step)
	}

	if mutateCmd == nil {
		t.Fatal("no mutate command once the wait resolved")
	}
	if _, _ = m.Update(mutateCmd()); len(mut.updateBranchCalls) != 2 {
		t.Fatalf("UpdateBranch calls after link 2 mutates = %d, want 2", len(mut.updateBranchCalls))
	}
	if mut.updateBranchCalls[1] != "pr-2" {
		t.Fatalf("second UpdateBranch call = %q, want pr-2", mut.updateBranchCalls[1])
	}
}

func TestCascadePanelShowsReportAndTitle(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.width, m.height = 100, 30
	m.setPRs([]gh.PR{{Number: 1}})
	m.cascadeReport = []string{"updated #20 #21", "failed #1 — update rejected"}

	out := ansi.Strip(m.render())
	if !strings.Contains(out, "Stack update") {
		t.Fatalf("render should show the panel title:\n%s", out)
	}
	for _, line := range m.cascadeReport {
		if !strings.Contains(out, line) {
			t.Fatalf("render should contain report line %q:\n%s", line, out)
		}
	}
}

func TestCascadePanelHiddenWhenReportEmpty(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.width, m.height = 100, 30
	m.setPRs([]gh.PR{{Number: 1}})

	if out := ansi.Strip(m.render()); strings.Contains(out, "Stack update") {
		t.Fatalf("render should not show the panel with no report:\n%s", out)
	}
}

// A pending confirm is a surface the user opened; it must win over a report
// that hasn't been dismissed yet (C8's precedence).
func TestCascadePanelPendingLosesToConfirm(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.width, m.height = 100, 30
	m.setPRs([]gh.PR{{Number: 1}})
	m.cascadeReport = []string{"failed #1 — update rejected"}
	a := action.Action{Key: "m", Label: "Merge", Confirm: true}
	m.pending = &a

	out := ansi.Strip(m.render())
	if !strings.Contains(out, "Confirm") {
		t.Fatalf("confirm panel should win over the cascade report:\n%s", out)
	}
	if strings.Contains(out, "Stack update") {
		t.Fatalf("cascade report should not render while a confirm is pending:\n%s", out)
	}
}

func TestCascadePanelKeyPressClearsReport(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.width, m.height = 100, 30
	m.setPRs([]gh.PR{{Number: 1}})
	m.cascadeReport = []string{"failed #1 — update rejected"}

	next, _ := m.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
	m = next.(Model)

	if m.cascadeReport != nil {
		t.Fatalf("any key should clear cascadeReport, got %v", m.cascadeReport)
	}
}

// The panel's own width is clamped to m.width (see cascadePanel), so a report
// line far longer than the terminal must not push any rendered line past it.
func TestCascadePanelOverflowGuard(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.width, m.height = 40, 20
	m.setPRs([]gh.PR{{Number: 1}})
	m.cascadeReport = []string{
		"failed #103 — PR #103 has conflicts; #204 — timed out waiting for PR #204 to update",
	}

	for i, line := range strings.Split(m.render(), "\n") {
		if w := lipgloss.Width(line); w > m.width {
			t.Errorf("line %d is %d cells wide, want <= %d: %q", i, w, m.width, line)
		}
	}
}

// driveCascade runs a cascade to settle by pumping messages through Update,
// starting from the command startBulk/confirmAnswer/runCascade returned. It
// invokes an ordinary cmd normally, but NEVER the probe beat's tea.Tick — a
// 30-probe timeout test would otherwise sleep cascadeWaitProbes*cascadeProbeEvery
// (45s).
//
// The discriminator is m.cascade.step, checked BEFORE cmd is invoked: while
// the run is waiting (step == cascadeProbe), cmd is whatever cascadeDispatch
// last handed back for that wait — the tick — so it must never be called;
// the pump feeds cascadeProbeMsg{} directly instead, generalizing
// driveCascadeProbes above to run a whole plan to settle. awaitingFetch marks
// the one legitimate exception: cmd right after that synthesized message is
// cascadeProbeFetchCmd, not the tick, and must still be invoked even though
// step still reads cascadeProbe (onProbed hasn't run yet to change it).
func driveCascade(t *testing.T, m Model, cmd tea.Cmd) (Model, actionDoneMsg) {
	t.Helper()
	awaitingFetch := false
	for i := 0; i < 4*cascadeWaitProbes*10; i++ {
		var msg tea.Msg
		switch {
		case awaitingFetch:
			msg = cmd()
			awaitingFetch = false
		case m.cascade != nil && m.cascade.step == cascadeProbe:
			msg = cascadeProbeMsg{}
			awaitingFetch = true
		default:
			if cmd == nil {
				t.Fatal("cascade drive: nil command before settle")
			}
			msg = cmd()
		}
		if batch, ok := msg.(tea.BatchMsg); ok {
			cmd = batch[0] // batch[1] is startSpinner's tick; never invoked
			continue
		}
		next, nextCmd := m.Update(msg)
		m = next.(Model)
		if done, ok := msg.(actionDoneMsg); ok {
			return m, done
		}
		cmd = nextCmd
	}
	t.Fatal("cascade did not settle in time")
	return m, actionDoneMsg{}
}

// TestCascadeThreeLinkStackFiresUpdateBranchInOrderAfterEachWait drives a real
// 3-link stack through Model.Update end to end (AC 1/2/7): links 2 and 3 are
// snapshotted CLEAN against their own not-yet-moved bases (the ordinary case
// for a stack beyond its bottom link), so this pins the carry-forward
// regression — a decision taken from the snapshot alone would fire link 3
// with zero probes — at the model level, not just cascadeRun's own unit tests.
func TestCascadeThreeLinkStackFiresUpdateBranchInOrderAfterEachWait(t *testing.T) {
	pr1 := stackedPR(1, 1, 3)
	pr1.ID, pr1.Mergeable, pr1.MergeStateStatus = "n1", "MERGEABLE", "BEHIND"
	pr2 := stackedPR(1, 2, 3)
	pr2.ID, pr2.Mergeable, pr2.MergeStateStatus = "n2", "MERGEABLE", "CLEAN"
	pr3 := stackedPR(1, 3, 3)
	pr3.ID, pr3.Mergeable, pr3.MergeStateStatus = "n3", "MERGEABLE", "CLEAN"

	mut := &fakeMutationSource{}
	fd := &sequencedDetailSource{readings: []gh.PRDetail{
		{Mergeable: "UNKNOWN", MergeStateStatus: "BEHIND"},
		{Mergeable: "UNKNOWN", MergeStateStatus: "BEHIND"},
		{Mergeable: "MERGEABLE", MergeStateStatus: "BEHIND"}, // link 2's wait resolves on the 3rd probe
		{Mergeable: "MERGEABLE", MergeStateStatus: "BEHIND"}, // link 3's mandatory single probe
	}}

	m := NewModel("/repo", "is:open", nil)
	m.SetMutationSource(mut)
	m.SetDetailSource(fd)
	m.setPRs([]gh.PR{pr3, pr2, pr1}) // a stack shows bottom-up regardless of input order: #1, #2, #3
	m.sel.toggle(0)                  // #1, the bottom of the stack

	cmd := m.startBulk(m.actions["u"])
	if cmd != nil || m.pending == nil || !m.pendingCascade.cascades() {
		t.Fatalf("stacked u should prompt with a cascading plan: cmd=%v pending=%v plan=%+v", cmd, m.pending, m.pendingCascade)
	}

	cmd = m.confirmAnswer(true)
	m, done := driveCascade(t, m, cmd)

	if done.err != nil {
		t.Fatalf("done.err = %v, want nil", done.err)
	}
	if !slices.Equal(mut.updateBranchCalls, []string{"n1", "n2", "n3"}) {
		t.Fatalf("updateBranchCalls = %v, want [n1 n2 n3] in StackPosition order", mut.updateBranchCalls)
	}
	if fd.calls != 4 {
		t.Errorf("probes fired = %d, want exactly 4 (3 for link 2, 1 for link 3)", fd.calls)
	}
}

// TestCascadeMidChainFailureSettlesPartialWithRefreshAndCIRerun covers AC 3
// and C7 together: link 2's mutation fails (an empty node id, the same
// stale-cache guard TestNativeMutationSkipsWhenNodeIDEmpty exercises single-
// action), so link 3 is never attempted. The settle's actionDoneMsg handler
// must still stamp link 1's success into ciRerun without touching link 2 or 3.
func TestCascadeMidChainFailureSettlesPartialWithRefreshAndCIRerun(t *testing.T) {
	pr1 := stackedPR(1, 1, 3)
	pr1.ID, pr1.Mergeable, pr1.MergeStateStatus = "n1", "MERGEABLE", "BEHIND"
	pr2 := stackedPR(1, 2, 3) // ID left unset: nativeMutationFn's stale-cache guard fails it
	pr3 := stackedPR(1, 3, 3)
	pr3.ID = "n3"

	mut := &fakeMutationSource{}
	fd := &sequencedDetailSource{readings: []gh.PRDetail{{Mergeable: "MERGEABLE", MergeStateStatus: "BEHIND"}}}

	m := NewModel("/repo", "is:open", nil)
	m.SetMutationSource(mut)
	m.SetDetailSource(fd)
	m.setPRs([]gh.PR{pr3, pr2, pr1})
	m.sel.toggle(0) // #1, the bottom of the stack (a stack shows bottom-up)

	cmd := m.startBulk(m.actions["u"])
	if cmd != nil || !m.pendingCascade.cascades() {
		t.Fatalf("expected a stacked cascade prompt, got cmd=%v plan=%+v", cmd, m.pendingCascade)
	}
	cmd = m.confirmAnswer(true)
	m, done := driveCascade(t, m, cmd)

	if done.err == nil {
		t.Fatal("done.err = nil, want an error: link 2's mutation failed")
	}
	if !slices.Equal(mut.updateBranchCalls, []string{"n1"}) {
		t.Fatalf("updateBranchCalls = %v, want [n1] only: #2's empty id must short-circuit, #3 must never fire", mut.updateBranchCalls)
	}
	if !intsEqual(m.actionStatus.partial, []int{1}) {
		t.Fatalf("actionStatus.partial = %v, want [1]", m.actionStatus.partial)
	}
	if _, ok := m.ciRerun[1]; !ok {
		t.Error("ciRerun[1] missing: the succeeded link should get the same CI-rerun stamp a full success would")
	}
	if _, ok := m.ciRerun[2]; ok {
		t.Error("ciRerun[2] set, want none: link 2's mutation failed")
	}
	if _, ok := m.ciRerun[3]; ok {
		t.Error("ciRerun[3] set, want none: link 3 was never attempted")
	}
}

// settledCascade drives a minimal 2-link stack to a clean, fully-successful
// settle, for tests that only care about state after a cascade has finished.
func settledCascade(t *testing.T) Model {
	t.Helper()
	pr1 := stackedPR(1, 1, 2)
	pr1.ID, pr1.Mergeable, pr1.MergeStateStatus = "n1", "MERGEABLE", "BEHIND"
	pr2 := stackedPR(1, 2, 2)
	pr2.ID = "n2"

	mut := &fakeMutationSource{}
	fd := &sequencedDetailSource{readings: []gh.PRDetail{{Mergeable: "MERGEABLE", MergeStateStatus: "BEHIND"}}}

	m := NewModel("/repo", "is:open", nil)
	m.SetMutationSource(mut)
	m.SetDetailSource(fd)
	m.setPRs([]gh.PR{pr2, pr1})
	m.sel.toggle(0) // #1, the bottom of the stack (a stack shows bottom-up)

	cmd := m.startBulk(m.actions["u"])
	if cmd != nil || !m.pendingCascade.cascades() {
		t.Fatalf("expected a stacked cascade prompt, got cmd=%v plan=%+v", cmd, m.pendingCascade)
	}
	cmd = m.confirmAnswer(true)
	m, done := driveCascade(t, m, cmd)
	if done.err != nil {
		t.Fatalf("done.err = %v, want nil", done.err)
	}
	return m
}

func TestCascadeModelClearsAfterSettle(t *testing.T) {
	m := settledCascade(t)
	if m.cascade != nil {
		t.Fatalf("m.cascade = %+v, want nil directly after the settle actionDoneMsg", m.cascade)
	}
}

// TestStaleCascadeProbeAfterSettleIsNoOp covers C6: a beat already in flight
// when the run settled must not panic or mutate state once it lands.
func TestStaleCascadeProbeAfterSettleIsNoOp(t *testing.T) {
	m := settledCascade(t)
	beforeDetail, beforeFresh := m.detail[2], m.fresh[2]

	next, cmd := m.Update(cascadeProbeMsg{})
	m2 := next.(Model)
	if cmd != nil {
		t.Fatalf("stale cascadeProbeMsg after settle should produce no command, got %v", cmd)
	}
	if m2.cascade != nil {
		t.Fatal("cascade must stay nil")
	}
	if !reflect.DeepEqual(m2.detail[2], beforeDetail) || m2.fresh[2] != beforeFresh {
		t.Fatal("stale cascadeProbeMsg must not mutate state")
	}

	next, cmd = m2.Update(cascadeProbedMsg{number: 2, detail: gh.PRDetail{Mergeable: "MERGEABLE"}, ok: true})
	m3 := next.(Model)
	if cmd != nil {
		t.Fatal("stale cascadeProbedMsg after settle should produce no command")
	}
	if !reflect.DeepEqual(m3.detail[2], beforeDetail) {
		t.Fatal("stale cascadeProbedMsg must not fold detail after settle")
	}
}

// TestCascadeNilDetailSourceDegradesToSingleUpdate covers C5's nil-source
// degradation and AC 5: with no wait mechanism there is no cascade plan, so
// a stacked seed's press updates only itself, exactly like today.
func TestCascadeNilDetailSourceDegradesToSingleUpdate(t *testing.T) {
	pr1 := stackedPR(1, 1, 2)
	pr1.ID = "n1"
	pr2 := stackedPR(1, 2, 2)
	pr2.ID = "n2"

	mut := &fakeMutationSource{}
	m := NewModel("/repo", "is:open", nil)
	m.SetMutationSource(mut) // no SetDetailSource: m.detailSource stays nil
	m.setPRs([]gh.PR{pr2, pr1})
	m.sel.toggle(0) // #1, the bottom of the stack (a stack shows bottom-up)

	cmd := m.startBulk(m.actions["u"])
	if m.pendingCascade != nil {
		t.Fatalf("pendingCascade = %+v, want nil: no DetailSource means no wait mechanism", m.pendingCascade)
	}
	if m.pending != nil {
		t.Fatal("no prompt should appear with no cascade plan")
	}
	msg := driveBulk(t, cmd)
	done, ok := msg.(actionDoneMsg)
	if !ok || done.err != nil {
		t.Fatalf("msg = %+v, want a successful actionDoneMsg", msg)
	}
	if !slices.Equal(mut.updateBranchCalls, []string{"n1"}) {
		t.Fatalf("updateBranchCalls = %v, want [n1] only: no DetailSource means today's single update, no cascade to #2", mut.updateBranchCalls)
	}
}

// TestCascadeUOnNonStackedPRUnchanged pins AC 5's first half: pressing u on a
// non-stacked PR — single (perf_actions_test.go's TestInlineActionShowsFeedback)
// and bulk (perf_actions_test.go's TestBulkInlineRunsPerSelected) — behaves
// exactly as it did before the cascade feature landed: no plan, no prompt, no
// live run.
func TestCascadeUOnNonStackedPRUnchanged(t *testing.T) {
	t.Run("single", func(t *testing.T) {
		m := NewModel("/repo", "is:open", nil)
		m.SetRepo("x")
		m.SetMutationSource(&fakeMutationSource{})
		m.width, m.height = 120, 40
		m.setPRs([]gh.PR{{Number: 1}})
		m.renderList()

		u, _ := m.Update(tea.KeyPressMsg{Code: 'u', Text: "u"})
		mo := u.(Model)
		if !mo.actionRunning() {
			t.Fatal("dispatching an inline action should show it running")
		}
		if mo.pending != nil || mo.pendingCascade != nil || mo.cascade != nil {
			t.Fatal("a non-stacked PR must not build a cascade plan, prompt, or start a run")
		}

		u, _ = mo.Update(actionDoneMsg{err: nil})
		mo = u.(Model)
		if mo.actionRunning() || mo.actionStatus == nil {
			t.Fatal("completion should mark the status done, not running")
		}
		if !strings.Contains(mo.render(), "Branch updated") {
			t.Fatalf("header should surface the finished action in past tense:\n%s", mo.render())
		}
	})

	t.Run("bulk", func(t *testing.T) {
		m := NewModel("/repo", "is:open", nil)
		m.SetRepo("x")
		fs := &fakeMutationSource{}
		m.SetMutationSource(fs)
		m.width, m.height = 120, 40
		m.setPRs([]gh.PR{
			{Number: 1, ID: "n1", State: "OPEN"},
			{Number: 2, ID: "n2", State: "OPEN"},
			{Number: 3, ID: "n3", State: "OPEN"},
		})
		m.sel.toggle(0)
		m.sel.toggle(2)

		cmd := m.startBulk(m.actions["u"])
		if cmd == nil {
			t.Fatal("bulk inline action should return a command")
		}
		if m.pending != nil || m.pendingCascade != nil {
			t.Fatal("non-stacked PRs must not build a cascade plan or prompt")
		}
		if m.actionStatus == nil || m.actionStatus.run != "Updating branch ×2" {
			t.Fatalf("running badge run = %q, want %q", m.actionStatus.run, "Updating branch ×2")
		}
		if m.sel.count() != 0 {
			t.Fatalf("bulk should consume the selection, %d left", m.sel.count())
		}

		if batch, ok := cmd().(tea.BatchMsg); ok {
			for _, c := range batch {
				if c != nil {
					c()
				}
			}
		}
		if len(fs.updateBranchCalls) != 2 {
			t.Fatalf("want one native call per selected PR (2), got %d: %v", len(fs.updateBranchCalls), fs.updateBranchCalls)
		}
	})
}

// TestCascadeErroredProbeDoesNotTouchDetailOrFresh covers C5: an errored probe
// spends budget but must leave m.detail/m.fresh exactly as they were, so it
// can never blank a PR's already-cached mergeability.
func TestCascadeErroredProbeDoesNotTouchDetailOrFresh(t *testing.T) {
	pr1 := stackedPR(1, 1, 2)
	pr1.ID, pr1.Mergeable, pr1.MergeStateStatus = "n1", "MERGEABLE", "BEHIND"
	pr2 := stackedPR(1, 2, 2)
	pr2.ID = "n2"

	mut := &fakeMutationSource{}
	fd := &sequencedDetailSource{
		errs:     []error{errors.New("api down")},
		readings: []gh.PRDetail{{Mergeable: "MERGEABLE", MergeStateStatus: "BEHIND"}},
	}

	m := NewModel("/repo", "is:open", nil)
	m.SetMutationSource(mut)
	m.SetDetailSource(fd)
	m.setPRs([]gh.PR{pr2, pr1})
	m.sel.toggle(0) // #1, the bottom of the stack (a stack shows bottom-up)

	cmd := m.startBulk(m.actions["u"])
	if cmd != nil || !m.pendingCascade.cascades() {
		t.Fatalf("expected a stacked cascade prompt, got cmd=%v plan=%+v", cmd, m.pendingCascade)
	}
	cmd = m.confirmAnswer(true)

	batch, ok := cmd().(tea.BatchMsg)
	if !ok {
		t.Fatalf("runCascade should return a batch, got %T", cmd())
	}
	next, _ := m.Update(batch[0]()) // fire link 1's mutation
	m = next.(Model)
	if m.cascade == nil || m.cascade.step != cascadeProbe {
		t.Fatalf("step after link 1 mutates = %+v, want cascadeProbe", m.cascade)
	}

	// First probe errors: budget spent, no fold.
	next, fetch := m.Update(cascadeProbeMsg{})
	m = next.(Model)
	probed, cmd2 := m.Update(fetch())
	m = probed.(Model)

	if _, ok := m.detail[2]; ok {
		t.Error("an errored probe must not write m.detail[2]")
	}
	if m.fresh[2] {
		t.Error("an errored probe must not mark #2 fresh")
	}
	if m.cascade.probes != 1 {
		t.Errorf("probes = %d, want 1: an errored probe must still consume budget", m.cascade.probes)
	}

	m, done := driveCascade(t, m, cmd2)
	if done.err != nil {
		t.Fatalf("done.err = %v, want nil: the retry succeeds", done.err)
	}
	if !slices.Equal(mut.updateBranchCalls, []string{"n1", "n2"}) {
		t.Fatalf("updateBranchCalls = %v, want [n1 n2]", mut.updateBranchCalls)
	}
}

// TestCascadeUOnMergedBoardNonStackedContinuesPastFailure covers AC 5's second
// half: two non-stacked PRs on the merged board, one failing, still settle
// with today's "N of M failed" badge and no cascade plan.
func TestCascadeUOnMergedBoardNonStackedContinuesPastFailure(t *testing.T) {
	pr1 := gh.PR{Number: 1, ID: "n1", State: "MERGED"}
	pr2 := gh.PR{Number: 2, State: "MERGED"} // ID left unset: the stale-cache guard fails it

	mut := &fakeMutationSource{}
	m := NewModel("/repo", "is:merged", nil)
	m.SetMutationSource(mut)
	m.setPRs([]gh.PR{pr2, pr1})
	m.sel.toggle(0)
	m.sel.toggle(1)

	cmd := m.startBulk(m.actions["u"])
	if cmd == nil {
		t.Fatal("non-stacked bulk u should run immediately with no prompt")
	}
	if m.pending != nil || m.pendingCascade != nil {
		t.Fatal("non-stacked PRs on the merged board must not build a cascade plan or prompt")
	}

	msg := driveBulk(t, cmd)
	done, ok := msg.(actionDoneMsg)
	if !ok || done.err == nil {
		t.Fatalf("msg = %+v, want a partially-failed actionDoneMsg", msg)
	}
	if done.fail != "1 of 2 failed" {
		t.Errorf("fail = %q, want %q", done.fail, "1 of 2 failed")
	}
	if !slices.Equal(mut.updateBranchCalls, []string{"n1"}) {
		t.Errorf("updateBranchCalls = %v, want [n1]", mut.updateBranchCalls)
	}
}

// TestPerSelectedKeysOnStackedPRDoNotCascade covers AC 6: only update-branch
// builds a cascade plan (C1). A stacked PR pressed with any other
// per-selected key must behave exactly as it does on a non-stacked one — no
// update-branch prompt, no live run.
func TestPerSelectedKeysOnStackedPRDoNotCascade(t *testing.T) {
	seed := stackedPR(1, 1, 3)
	seed.ID, seed.Mergeable, seed.MergeStateStatus = "n1", "MERGEABLE", "BEHIND"
	above := stackedPR(1, 2, 3)
	above.ID = "n2"
	top := stackedPR(1, 3, 3)
	top.ID = "n3"

	for _, key := range []string{"L", "A", "M", "o", "W"} {
		t.Run(key, func(t *testing.T) {
			m := NewModel("/repo", "is:open", nil)
			m.SetMutationSource(&fakeMutationSource{})
			m.SetDetailSource(&fakeDetailSource{ret: map[int]gh.PRDetail{}})
			m.width, m.height = 120, 40
			m.setPRs([]gh.PR{top, above, seed})
			m.cursor = 0 // a stack shows bottom-up: seed (#1) is first

			m.startBulk(m.actions[key])

			if m.pendingCascade != nil {
				t.Fatalf("%s must not build a cascade plan on a stacked PR", key)
			}
			if m.cascade != nil {
				t.Fatalf("%s must not start a live cascade run", key)
			}
			if m.pending != nil && strings.Contains(m.confirmQuestion(), "in stack") {
				t.Fatalf("%s must not render the update-branch cascade prompt: %q", key, m.confirmQuestion())
			}
		})
	}
}

// TestCascadePlanNeverBuildsOnIssueBoard exercises C1's *PRSection half of the
// guard directly: even for update-branch with a live DetailSource, an
// IssueSection board must not build a cascade plan.
func TestCascadePlanNeverBuildsOnIssueBoard(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.SetMutationSource(&fakeMutationSource{})
	m.SetDetailSource(&fakeDetailSource{ret: map[int]gh.PRDetail{}})
	m.section = NewIssueSection("is:open")

	m.startBulk(action.DefaultPRActions()["u"])

	if m.pendingCascade != nil {
		t.Fatalf("pendingCascade = %+v, want nil on the issue board even for update-branch", m.pendingCascade)
	}
}
