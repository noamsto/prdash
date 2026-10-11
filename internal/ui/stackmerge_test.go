package ui

import (
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/noamsto/prdash/internal/gh"
)

func smember(n int, state string) gh.StackMember { return gh.StackMember{Number: n, State: state} }

func openMembers(nums ...int) []gh.StackMember {
	out := make([]gh.StackMember, len(nums))
	for i, n := range nums {
		out[i] = smember(n, "OPEN")
	}
	return out
}

func memberNums(ms []gh.StackMember) []int {
	out := make([]int, len(ms))
	for i, m := range ms {
		out[i] = m.Number
	}
	return out
}

func cleanRow(n int) stackBoardRow {
	return stackBoardRow{pr: gh.PR{Number: n, State: "OPEN"}, mergeable: "MERGEABLE", mss: "CLEAN"}
}

func boardOf(rows ...stackBoardRow) map[int]stackBoardRow {
	b := make(map[int]stackBoardRow, len(rows))
	for _, r := range rows {
		b[r.pr.Number] = r
	}
	return b
}

func wantErr(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil || err.Error() != want {
		t.Errorf("err = %v, want %q", err, want)
	}
}

func TestStackRange(t *testing.T) {
	st := gh.Stack{Members: openMembers(10, 11, 12, 13)}
	tests := []struct {
		name   string
		st     gh.Stack
		target int
		want   []int
	}{
		{"bottom", st, 10, []int{10}},
		{"middle", st, 12, []int{10, 11, 12}},
		{"top", st, 13, []int{10, 11, 12, 13}},
		{"merged bottom dropped", gh.Stack{Members: []gh.StackMember{smember(10, "MERGED"), smember(11, "OPEN"), smember(12, "OPEN")}}, 12, []int{11, 12}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := stackRange(tt.target, tt.st)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(memberNums(got), tt.want) {
				t.Errorf("range = %v, want %v", memberNums(got), tt.want)
			}
		})
	}
}

func TestStackRangeRefusals(t *testing.T) {
	st := gh.Stack{Members: []gh.StackMember{smember(10, "OPEN"), smember(11, "MERGED"), smember(12, "CLOSED")}}

	_, err := stackRange(99, st)
	wantErr(t, err, "PR #99 is no longer in this stack — ctrl+r to refresh")
	_, err = stackRange(11, st)
	wantErr(t, err, "PR #11 is not open")
	_, err = stackRange(12, st)
	wantErr(t, err, "PR #12 is not open")
}

func TestStackNeedsDetail(t *testing.T) {
	unresolved := cleanRow(2)
	unresolved.mergeable = "UNKNOWN"
	board := boardOf(cleanRow(1), unresolved, cleanRow(4))

	got := stackNeedsDetail(openMembers(1, 2, 3, 4), board)
	if want := []int{2, 3}; !slices.Equal(got, want) {
		t.Errorf("needs = %v, want %v", got, want)
	}
}

func TestCheckStackLayersCleanKeepsBoardPRAndSynthesisesOffBoard(t *testing.T) {
	onBoard := cleanRow(1)
	onBoard.pr.Title = "board row"
	members := []gh.StackMember{smember(1, "OPEN"), smember(2, "OPEN")}
	fresh := map[int]gh.PRDetail{2: {Mergeable: "MERGEABLE", MergeStateStatus: "CLEAN"}}

	got, err := checkStackLayers(members, boardOf(onBoard), fresh)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Title != "board row" {
		t.Fatalf("layers = %+v, want board PR first", got)
	}
	if got[1].Number != 2 || got[1].State != "OPEN" || got[1].IsDraft {
		t.Errorf("off-board layer = %+v, want {Number: 2, State: OPEN}", got[1])
	}
}

func TestCheckStackLayersRefusals(t *testing.T) {
	conflictedRow := cleanRow(2)
	conflictedRow.mergeable, conflictedRow.mss = "CONFLICTING", "DIRTY"
	unresolvedRow := cleanRow(2)
	unresolvedRow.mergeable = "UNKNOWN"
	heldRow := cleanRow(2)
	heldRow.mutableErr = errors.New("PR #2 is no longer on this board — ctrl+r to refresh")

	tests := []struct {
		name    string
		members []gh.StackMember
		board   map[int]stackBoardRow
		fresh   map[int]gh.PRDetail
		want    string
	}{
		{"closed", []gh.StackMember{smember(1, "OPEN"), smember(2, "CLOSED")}, boardOf(cleanRow(1), cleanRow(2)), nil, "PR #2 is not open"},
		{"draft", []gh.StackMember{smember(1, "OPEN"), {Number: 2, State: "OPEN", IsDraft: true}}, boardOf(cleanRow(1), cleanRow(2)), nil, "PR #2 is a draft"},
		{"conflicted board reading", openMembers(1, 2), boardOf(cleanRow(1), conflictedRow), nil, "PR #2 has conflicts"},
		{"conflicted fresh reading", openMembers(1, 2), boardOf(cleanRow(1)), map[int]gh.PRDetail{2: {Mergeable: "CONFLICTING", MergeStateStatus: "DIRTY"}}, "PR #2 has conflicts"},
		{"fresh overrides clean board", openMembers(1, 2), boardOf(cleanRow(1), cleanRow(2)), map[int]gh.PRDetail{2: {Mergeable: "CONFLICTING"}}, "PR #2 has conflicts"},
		{"pending after refetch", openMembers(1, 2), boardOf(cleanRow(1), unresolvedRow), map[int]gh.PRDetail{2: {Mergeable: "UNKNOWN"}}, "PR #2 merge state is still pending — try again shortly"},
		{"pending unresolved board, no fresh", openMembers(1, 2), boardOf(cleanRow(1), unresolvedRow), nil, "PR #2 merge state is still pending — try again shortly"},
		{"pending off board, no fresh", openMembers(1, 2), boardOf(cleanRow(1)), nil, "PR #2 merge state is still pending — try again shortly"},
		{"held row", openMembers(1, 2), boardOf(cleanRow(1), heldRow), nil, "PR #2 is no longer on this board — ctrl+r to refresh"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := checkStackLayers(tt.members, tt.board, tt.fresh)
			wantErr(t, err, tt.want)
		})
	}
}

func TestCheckStackLayersFirstOffenderWins(t *testing.T) {
	conflicted := cleanRow(1)
	conflicted.mergeable = "CONFLICTING"
	members := []gh.StackMember{smember(1, "OPEN"), {Number: 2, State: "OPEN", IsDraft: true}}

	_, err := checkStackLayers(members, boardOf(conflicted, cleanRow(2)), nil)
	wantErr(t, err, "PR #1 has conflicts")
}

func TestStackMergePlanQuestion(t *testing.T) {
	plan := &stackMergePlan{target: 142, layers: []gh.PR{{Number: 140}, {Number: 141}, {Number: 142}}}
	if got, want := plan.question(), "Merge stack up to #142 (3 PRs: #140 #141 #142)?"; got != want {
		t.Errorf("question = %q, want %q", got, want)
	}
	if got := plan.numbers(); !slices.Equal(got, []int{140, 141, 142}) {
		t.Errorf("numbers = %v", got)
	}

	one := &stackMergePlan{target: 7, layers: []gh.PR{{Number: 7}}}
	if got, want := one.question(), "Merge stack up to #7 (1 PR: #7)?"; got != want {
		t.Errorf("question = %q, want %q", got, want)
	}
}

type fakeStackSource struct {
	st       gh.Stack
	found    bool
	err      error
	queue    bool
	queueErr error
	baseRef  string
	calls    int // FetchStackForPR calls
}

func (f *fakeStackSource) FetchStackForPR(int) (gh.Stack, bool, error) {
	f.calls++
	return f.st, f.found, f.err
}

func (f *fakeStackSource) BaseUsesMergeQueue(ref string) (bool, error) {
	f.baseRef = ref
	return f.queue, f.queueErr
}

type fakeStackDetails struct {
	calls [][]int
	ret   map[int]gh.PRDetail
	err   error
}

func (f *fakeStackDetails) FetchDetails(nums []int) (map[int]gh.PRDetail, map[int][]byte, error) {
	f.calls = append(f.calls, slices.Clone(nums))
	if f.err != nil {
		return nil, nil, f.err
	}
	out := map[int]gh.PRDetail{}
	for _, n := range nums {
		if d, ok := f.ret[n]; ok {
			out[n] = d
		}
	}
	return out, nil, nil
}

func cleanDetail() gh.PRDetail { return gh.PRDetail{Mergeable: "MERGEABLE", MergeStateStatus: "CLEAN"} }

func TestLoadStackPlanFetchesExactlyTheNeededDetails(t *testing.T) {
	stacks := &fakeStackSource{found: true, st: gh.Stack{BaseRefName: "main", Members: openMembers(1, 2, 3, 4)}}
	details := &fakeStackDetails{ret: map[int]gh.PRDetail{1: cleanDetail(), 2: cleanDetail(), 3: cleanDetail()}}
	board := boardOf(cleanRow(3), cleanRow(4))

	plan, err := loadStackPlan(gh.PR{Number: 3}, board, stacks, details)
	if err != nil {
		t.Fatal(err)
	}
	if len(details.calls) != 1 || !slices.Equal(details.calls[0], []int{1, 2}) {
		t.Errorf("FetchDetails calls = %v, want one call with [1 2]", details.calls)
	}
	if plan.target != 3 || !slices.Equal(plan.numbers(), []int{1, 2, 3}) {
		t.Errorf("plan = target %d numbers %v, want target 3 numbers [1 2 3]", plan.target, plan.numbers())
	}
	if plan.layers[2].Number != 3 || plan.layers[0].Number != 1 || plan.layers[0].State != "OPEN" {
		t.Errorf("layers = %+v", plan.layers)
	}
	if stacks.baseRef != "main" {
		t.Errorf("queue detection asked about %q, want main", stacks.baseRef)
	}
}

func TestLoadStackPlanSkipsFetchWhenBoardCoversEverything(t *testing.T) {
	stacks := &fakeStackSource{found: true, st: gh.Stack{Members: openMembers(1, 2)}}
	details := &fakeStackDetails{}

	if _, err := loadStackPlan(gh.PR{Number: 2}, boardOf(cleanRow(1), cleanRow(2)), stacks, details); err != nil {
		t.Fatal(err)
	}
	if len(details.calls) != 0 {
		t.Errorf("FetchDetails called %v, want none", details.calls)
	}
}

func TestLoadStackPlanUnknownReadingIsPending(t *testing.T) {
	stacks := &fakeStackSource{found: true, st: gh.Stack{Members: openMembers(1, 2)}}
	const pending = "PR #1 merge state is still pending — try again shortly"

	_, err := loadStackPlan(gh.PR{Number: 2}, boardOf(cleanRow(2)), stacks, &fakeStackDetails{err: errors.New("boom")})
	wantErr(t, err, pending)

	_, err = loadStackPlan(gh.PR{Number: 2}, boardOf(cleanRow(2)), stacks, &fakeStackDetails{})
	wantErr(t, err, pending)

	_, err = loadStackPlan(gh.PR{Number: 2}, boardOf(cleanRow(2)), stacks, nil)
	wantErr(t, err, pending)
}

func TestLoadStackPlanRefusals(t *testing.T) {
	board := boardOf(cleanRow(1))

	_, err := loadStackPlan(gh.PR{Number: 1}, board, &fakeStackSource{err: errors.New("404 not found")}, nil)
	wantErr(t, err, "Stack merge failed: 404 not found")

	_, err = loadStackPlan(gh.PR{Number: 1}, board, &fakeStackSource{}, nil)
	wantErr(t, err, "PR #1 is no longer in a stack — ctrl+r to refresh")

	_, err = loadStackPlan(gh.PR{Number: 1}, board, &fakeStackSource{found: true, st: gh.Stack{Members: openMembers(2)}}, nil)
	wantErr(t, err, "PR #1 is no longer in this stack — ctrl+r to refresh")
}

func TestLoadStackPlanMergeQueueDetection(t *testing.T) {
	tests := []struct {
		name  string
		queue bool
		err   error
		want  bool
	}{
		{"queue", true, nil, true},
		{"no queue", false, nil, false},
		{"detection error falls back to direct", true, errors.New("graphql down"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stacks := &fakeStackSource{found: true, st: gh.Stack{Members: openMembers(1)}, queue: tt.queue, queueErr: tt.err}
			plan, err := loadStackPlan(gh.PR{Number: 1}, boardOf(cleanRow(1)), stacks, nil)
			if err != nil {
				t.Fatal(err)
			}
			if plan.mergeQueue != tt.want {
				t.Errorf("mergeQueue = %v, want %v", plan.mergeQueue, tt.want)
			}
		})
	}
}

func newStackRun(nums ...int) *stackMergeRun {
	layers := make([]gh.PR, len(nums))
	for i, n := range nums {
		layers[i] = gh.PR{Number: n, State: "OPEN"}
	}
	return &stackMergeRun{id: 1, plan: &stackMergePlan{target: layers[len(layers)-1].Number, layers: layers}}
}

func wantFailSettle(t *testing.T, r *stackMergeRun, step stackMergeStep, want string) {
	t.Helper()
	if step != stackMergeSettle {
		t.Fatalf("step = %v, want settle", step)
	}
	d := r.done
	if !d.stackMerge || len(d.merged) != 0 {
		t.Errorf("settle = %+v, want stackMerge with nothing merged", d)
	}
	if d.err == nil || d.err.Error() != want || d.fail != want {
		t.Errorf("settle err = %v, fail = %q, want both %q", d.err, d.fail, want)
	}
}

func TestStackMergeRunSubmitMerged(t *testing.T) {
	r := newStackRun(1, 2, 3)
	step := r.onSubmitted(gh.StackMergeResult{Status: gh.StackMergeMerged, SHA: "abc"}, nil)

	if step != stackMergeSettle {
		t.Fatalf("step = %v, want settle", step)
	}
	d := r.done
	if !d.stackMerge || d.err != nil || d.ok != "Stack merged ×3" {
		t.Errorf("settle = %+v", d)
	}
	if !reflect.DeepEqual(d.merged, r.plan.layers) {
		t.Errorf("merged = %v, want every plan layer %v", d.merged, r.plan.layers)
	}
}

func TestStackMergeRunSubmitEnqueuedMergesNothing(t *testing.T) {
	r := newStackRun(1, 2)
	step := r.onSubmitted(gh.StackMergeResult{Status: gh.StackMergeEnqueued}, nil)

	if step != stackMergeSettle {
		t.Fatalf("step = %v, want settle", step)
	}
	d := r.done
	if !d.stackMerge || d.err != nil || d.ok != "Stack queued ×2" || len(d.merged) != 0 {
		t.Errorf("settle = %+v", d)
	}
}

func TestStackMergeRunFailed(t *testing.T) {
	r := newStackRun(1, 2)
	step := r.onSubmitted(gh.StackMergeResult{Status: gh.StackMergeFailed, Message: "head moved"}, nil)
	wantFailSettle(t, r, step, "Stack merge failed: head moved — nothing merged")
}

func TestStackMergeRunSubmitError(t *testing.T) {
	r := newStackRun(1)
	step := r.onSubmitted(gh.StackMergeResult{}, errors.New("a merge for this stack is already in progress"))
	wantFailSettle(t, r, step, "Stack merge failed: a merge for this stack is already in progress")
}

func TestStackMergeRunEmptyUUID(t *testing.T) {
	r := newStackRun(1)
	step := r.onSubmitted(gh.StackMergeResult{Status: gh.StackMergePending}, nil)
	wantFailSettle(t, r, step, "Stack merge did not start")
}

func TestStackMergeRunPendingThenPollsToMerged(t *testing.T) {
	r := newStackRun(1, 2)
	pending := gh.StackMergeResult{Status: gh.StackMergePending, UUID: "u-1"}

	if step := r.onSubmitted(pending, nil); step != stackMergePoll || r.uuid != "u-1" {
		t.Fatalf("submit: step = %v uuid = %q, want poll u-1", step, r.uuid)
	}
	if step := r.onPolled(pending, nil); step != stackMergePoll || r.beats != 1 {
		t.Fatalf("poll 1: step = %v beats = %d", step, r.beats)
	}
	if step := r.onPolled(pending, nil); step != stackMergePoll || r.beats != 2 {
		t.Fatalf("poll 2: step = %v beats = %d", step, r.beats)
	}
	if step := r.onPolled(gh.StackMergeResult{Status: gh.StackMergeMerged}, nil); step != stackMergeSettle {
		t.Fatalf("poll 3: step = %v, want settle", step)
	}
	if d := r.done; !d.stackMerge || d.ok != "Stack merged ×2" || !reflect.DeepEqual(d.merged, r.plan.layers) {
		t.Errorf("settle = %+v", d)
	}
}

func TestStackMergeRunPolledFailed(t *testing.T) {
	r := newStackRun(1)
	step := r.onPolled(gh.StackMergeResult{Status: gh.StackMergeFailed, Message: "required check failed"}, nil)
	wantFailSettle(t, r, step, "Stack merge failed: required check failed — nothing merged")
}

func TestStackMergeRunUnknownStatusKeepsPolling(t *testing.T) {
	r := newStackRun(1)
	if step := r.onSubmitted(gh.StackMergeResult{Status: "queued_up"}, nil); step != stackMergePoll {
		t.Errorf("submit unknown: step = %v, want poll", step)
	}
	if step := r.onPolled(gh.StackMergeResult{Status: "queued_up"}, nil); step != stackMergePoll {
		t.Errorf("poll unknown: step = %v, want poll", step)
	}
}

func TestStackMergeRunProbeErrorsKeepPollingThenNameLastError(t *testing.T) {
	r := newStackRun(1)
	r.onSubmitted(gh.StackMergeResult{Status: gh.StackMergePending, UUID: "u"}, nil)

	var step stackMergeStep
	for i := 1; i <= stackMergePollBeats; i++ {
		step = r.onPolled(gh.StackMergeResult{}, errors.New("rate limited"))
		if i < stackMergePollBeats && step != stackMergePoll {
			t.Fatalf("beat %d: step = %v, want poll", i, step)
		}
	}
	wantFailSettle(t, r, step, "Stack merge still running on GitHub — check the PRs (last check: rate limited)")
}

func TestStackMergeRunExhaustionWithoutErrorOrAfterRecovery(t *testing.T) {
	r := newStackRun(1)
	pending := gh.StackMergeResult{Status: gh.StackMergePending, UUID: "u"}
	r.onSubmitted(pending, nil)

	var step stackMergeStep
	for i := 1; i <= stackMergePollBeats; i++ {
		var err error
		if i == 1 {
			err = errors.New("blip") // an earlier error must not leak into the exhausted text
		}
		step = r.onPolled(pending, err)
	}
	wantFailSettle(t, r, step, "Stack merge still running on GitHub — check the PRs")
	if strings.Contains(r.done.err.Error(), "last check") {
		t.Errorf("exhausted text names a stale error: %v", r.done.err)
	}
}
