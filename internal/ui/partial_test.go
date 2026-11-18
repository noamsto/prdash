package ui

import (
	"errors"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/noamsto/prdash/internal/gh"
)

// The batched gh sources fetch per repo: one failing repo leaves the others'
// results in the map and joins its error. These pin that the UI keeps them.

var errOtherRepo = errors.New("other/repo: boom")

type partialChecksSource struct{ ret map[gh.PRRef][]gh.Check }

func (f partialChecksSource) FetchChecks([]gh.PRRef) (map[gh.PRRef][]gh.Check, error) {
	return f.ret, errOtherRepo
}

type partialDetailSource struct{ ret map[gh.PRRef]gh.PRDetail }

func (f partialDetailSource) FetchDetails([]gh.PRRef) (map[gh.PRRef]gh.PRDetail, map[gh.PRRef][]byte, error) {
	return f.ret, map[gh.PRRef][]byte{}, errOtherRepo
}

type partialStateSource struct{ ret map[gh.PRRef]gh.ItemState }

func (f partialStateSource) FetchStates([]gh.PRRef) (map[gh.PRRef]gh.ItemState, error) {
	return f.ret, errOtherRepo
}

// updateAll feeds msg to m, unpacking a tea.BatchMsg into its sub-messages.
func updateAll(t *testing.T, m Model, msg tea.Msg) Model {
	t.Helper()
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, c := range batch {
			m = updateAll(t, m, c())
		}
		return m
	}
	u, _ := m.Update(msg)
	return u.(Model)
}

func TestPartialChecksApplied(t *testing.T) {
	launch := gh.PRRef{Repo: "owner/repo", Number: 1}
	prs := []gh.PR{
		{Number: 1, Repo: "owner/repo", StatusCheckRollup: pending()},
		{Number: 2, Repo: "other/repo", StatusCheckRollup: pending()},
	}

	t.Run("partial result applies and only hints", func(t *testing.T) {
		m := pollModel(t, &fakeChecksSource{}, prs)
		m.SetChecksSource(partialChecksSource{ret: map[gh.PRRef][]gh.Check{launch: passing()}})
		m = updateAll(t, m, m.pollChecksCmd()())

		if m.err != nil {
			t.Fatalf("m.err = %v, want nil — partial checks are not a board error", m.err)
		}
		ps := m.section.(*PRSection)
		if got := ps.prAt(rowIndexOf(t, ps, 1)).StatusCheckRollup; len(got) != 1 || got[0].State != "SUCCESS" {
			t.Errorf("#1 rollup = %+v, want the fetched SUCCESS", got)
		}
		if m.actionStatus == nil {
			t.Error("no hint for the failed repo's checks")
		}
	})

	t.Run("zero results still fail the fetch", func(t *testing.T) {
		m := pollModel(t, &fakeChecksSource{}, prs)
		m.SetChecksSource(partialChecksSource{})
		if msg, ok := m.pollChecksCmd()().(fetchFailedMsg); !ok || !errors.Is(msg.err, errOtherRepo) {
			t.Fatalf("msg = %#v, want fetchFailedMsg carrying the error", msg)
		}
	})
}

func TestPartialDetailsApplied(t *testing.T) {
	m := batchModel(t, &fakeDetailSource{}, []gh.PR{{Number: 5}, {Number: 7, Repo: "other/repo"}})
	launch, other := m.launchRef(5), gh.PRRef{Repo: "other/repo", Number: 7}
	m.SetDetailSource(partialDetailSource{ret: map[gh.PRRef]gh.PRDetail{launch: {MergeStateStatus: "CLEAN"}}})

	m = updateAll(t, m, m.batchDetailCmd([]gh.PRRef{launch, other})())

	if !m.fresh[launch] || m.detail[launch].MergeStateStatus != "CLEAN" {
		t.Errorf("launch ref fresh = %v, detail = %+v, want fresh CLEAN", m.fresh[launch], m.detail[launch])
	}
	if m.detailErr[launch] != nil {
		t.Errorf("detailErr[launch] = %v, want nil", m.detailErr[launch])
	}
	if m.fresh[other] || !errors.Is(m.detailErr[other], errOtherRepo) {
		t.Errorf("other ref fresh = %v, detailErr = %v, want unfresh with the error", m.fresh[other], m.detailErr[other])
	}
}

func TestPartialHeldStatesApplied(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.width, m.height = 100, 40
	m.setPRs([]gh.PR{openPR(30, "alice"), openPR(29, "alice"), openPR(28, "alice")})
	u, _ := m.Update(prsFetchedMsg{filter: m.filter, prs: []gh.PR{openPR(30, "alice"), openPR(28, "alice")}})
	m = u.(Model)
	m.SetStateSource(partialStateSource{ret: map[gh.PRRef]gh.ItemState{m.launchRef(29): {State: "CLOSED"}}})

	m = updateAll(t, m, m.heldStatesCmd()())

	if got := m.held[m.launchRef(29)]; got != "CLOSED" {
		t.Errorf("held[#29] = %q, want CLOSED from the partial lookup", got)
	}
}
