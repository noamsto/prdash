package ui

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/noamsto/prdash/internal/cache"
	"github.com/noamsto/prdash/internal/gh"
)

type fakeDetailSource struct {
	got [][]int // numbers passed to each FetchDetails call
	ret map[int]gh.PRDetail
}

func (f *fakeDetailSource) FetchDetails(nums []int) (map[int]gh.PRDetail, map[int][]byte, error) {
	f.got = append(f.got, append([]int(nil), nums...))
	details := map[int]gh.PRDetail{}
	raws := map[int][]byte{}
	for _, n := range nums {
		details[n] = f.ret[n]
		raws[n], _ = json.Marshal(f.ret[n])
	}
	return details, raws, nil
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
	if len(fd.got) != 1 || len(fd.got[0]) != 1 || fd.got[0][0] != 5 {
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
	want := []int{1, 3}
	if len(got) != len(want) || got[0] != 1 || got[1] != 3 {
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
	if len(fd.got) != 1 || fd.got[0][0] != 9 {
		t.Errorf("source called with %v, want one call for [9]", fd.got)
	}
}
