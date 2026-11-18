package ui

import (
	"encoding/json"
	"path/filepath"
	"slices"
	"testing"

	"github.com/noamsto/prdash/internal/cache"
	"github.com/noamsto/prdash/internal/gh"
)

type fakeDetailSource struct {
	got [][]gh.PRRef // refs passed to each FetchDetails call
	ret map[int]gh.PRDetail
}

func (f *fakeDetailSource) FetchDetails(refs []gh.PRRef) (map[gh.PRRef]gh.PRDetail, map[gh.PRRef][]byte, error) {
	f.got = append(f.got, append([]gh.PRRef(nil), refs...))
	details := map[gh.PRRef]gh.PRDetail{}
	raws := map[gh.PRRef][]byte{}
	for _, r := range refs {
		details[r] = f.ret[r.Number]
		raws[r], _ = json.Marshal(f.ret[r.Number])
	}
	return details, raws, nil
}

// refsIn qualifies nums with repo, for asserting what a fake source received.
func refsIn(repo string, nums ...int) []gh.PRRef {
	refs := make([]gh.PRRef, len(nums))
	for i, n := range nums {
		refs[i] = gh.PRRef{Repo: repo, Number: n}
	}
	return refs
}

func batchModel(t *testing.T, fd *fakeDetailSource, prs []gh.PR) Model {
	t.Helper()
	c := cache.Open(filepath.Join(t.TempDir(), "c.json"))
	m := NewModel("/repo", "is:open", c)
	m.SetRepo("owner/repo")
	m.SetDetailSource(fd)
	m.setPRs(prs)
	return m
}

func TestBatchDetailCmdPopulatesDetail(t *testing.T) {
	fd := &fakeDetailSource{ret: map[int]gh.PRDetail{5: {MergeStateStatus: "CLEAN"}}}
	m := batchModel(t, fd, []gh.PR{{Number: 5}})

	cmd := m.batchDetailCmd([]int{5})
	if cmd == nil {
		t.Fatal("batchDetailCmd should return a command for uncached PRs")
	}
	u, _ := m.Update(cmd())
	got := u.(Model)
	if got.detail[5].MergeStateStatus != "CLEAN" {
		t.Errorf("detail[5] = %+v, want MergeStateStatus=CLEAN", got.detail[5])
	}
	if !got.fresh[5] {
		t.Error("PR 5 should be marked fresh after the batch lands")
	}
	if len(fd.got) != 1 || !slices.Equal(fd.got[0], refsIn("owner/repo", 5)) {
		t.Errorf("source called with %v, want one call for [5]", fd.got)
	}
}

// TestDetailWindowSkipsFresh: the batch window is the shown PRs still needing
// detail — session-fresh ones are excluded so a refresh doesn't refetch them.
func TestDetailWindowSkipsFresh(t *testing.T) {
	fd := &fakeDetailSource{ret: map[int]gh.PRDetail{}}
	m := batchModel(t, fd, []gh.PR{{Number: 1}, {Number: 2}, {Number: 3}})
	m.fresh[2] = true

	ps := m.section.(*PRSection)
	got := m.detailWindow(ps)
	// Shown order is number descending: 3, 2, 1. PR 2 is fresh, so it's skipped.
	want := []int{3, 1}
	if len(got) != len(want) || got[0] != 3 || got[1] != 1 {
		t.Errorf("detailWindow = %v, want %v (PR 2 is fresh)", got, want)
	}
}

// TestCursorDetailRoutesToBatchSource: with a batch source, the cursor's own
// detail is a batch-of-one (fast paint) rather than a gh pr view subprocess.
func TestCursorDetailRoutesToBatchSource(t *testing.T) {
	fd := &fakeDetailSource{ret: map[int]gh.PRDetail{9: {}}}
	m := batchModel(t, fd, []gh.PR{{Number: 9}})

	cmd := m.detailCmdForCursor()
	if cmd == nil {
		t.Fatal("cold cursor detail should fetch")
	}
	if _, ok := cmd().(detailsBatchMsg); !ok {
		t.Error("cursor detail should route through the batch source (detailsBatchMsg)")
	}
	if len(fd.got) != 1 || fd.got[0][0] != (gh.PRRef{Repo: "owner/repo", Number: 9}) {
		t.Errorf("source called with %v, want one call for [9]", fd.got)
	}
}
