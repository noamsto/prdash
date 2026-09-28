package ui

import (
	"errors"
	"strings"
	"testing"

	"github.com/noamsto/prdash/internal/gh"
)

// halfFailPRSource fails FetchPRs for the filters named in fail and returns the
// rows for every other filter, so a test can drive one half of the sections
// fan-out into an error while the other two succeed.
type halfFailPRSource struct {
	fail map[string]error
	prs  map[string][]gh.PR
}

func (f halfFailPRSource) FetchPRs(filter string, limit int) ([]gh.PR, []byte, error) {
	if err, ok := f.fail[filter]; ok {
		return nil, nil, err
	}
	return f.prs[filter], nil, nil
}

// failDetailSource fails every batched detail fetch.
type failDetailSource struct{ err error }

func (f failDetailSource) FetchDetails([]int) (map[int]gh.PRDetail, map[int][]byte, error) {
	return nil, nil, f.err
}

// failMembersSource fails every assignable-users fetch.
type failMembersSource struct{ err error }

func (f failMembersSource) FetchAssignableUsers() ([]gh.User, []byte, error) {
	return nil, nil, f.err
}

// TestSectionsHalfFailureSurfacesError: a failed review-requested half on the
// sections-default board must set the board error and clear the refresh spinner,
// not bail on the filter guard because the half's filter differs from m.filter.
func TestSectionsHalfFailureSurfacesError(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.SetRepo("o/r")
	m.viewerLogin = "me"
	m.width, m.height = 100, 30
	reviewF := searchFor("pr", "open", reviewBody)
	m.SetPRSource(halfFailPRSource{fail: map[string]error{reviewF: errors.New("rate limited")}})
	m.refreshing = true

	msg := m.sectionsFetchCmd()()
	got, _ := m.Update(msg)
	out := got.(Model)
	if out.err == nil {
		t.Fatalf("review-requested half failure must set the board error (message %#v)", msg)
	}
	if out.refreshing {
		t.Fatalf("refreshing must clear when a sections half fails")
	}
}

// TestSuccessClearsStaleBoardError: a board error must not latch — a later
// successful fetch clears it, so an empty shown set afterwards paints the
// empty-state hint instead of the stale "Error:" screen.
func TestSuccessClearsStaleBoardError(t *testing.T) {
	m := NewModel("/repo", "is:open author:@me", nil)
	m.SetRepo("noamsto/prdash")
	m.width, m.height = 100, 30
	m.loaded = true // past launch so an empty set would otherwise paint the hint

	u, _ := m.Update(fetchFailedMsg{mode: "pr", filter: m.filter, err: errors.New("stale boom")})
	m = u.(Model)
	if m.err == nil {
		t.Fatal("setup: the injected failure did not set m.err")
	}

	u, _ = m.Update(prsFetchedMsg{filter: m.filter, prs: []gh.PR{{Number: 1, Title: "one"}}})
	m = u.(Model)
	m.setPRs(nil) // shown set becomes legitimately empty
	m.renderList()
	out := m.render()
	if strings.Contains(out, "Error:") {
		t.Fatalf("stale error survived a successful fetch: %q", out)
	}
	if !strings.Contains(out, "No open PRs.") {
		t.Fatalf("empty board should show the empty-state hint, got: %q", out)
	}
}

// TestDetailPrefetchFailureNotBoardError: a failed background detail prefetch
// must not poison the board-level error state.
func TestDetailPrefetchFailureNotBoardError(t *testing.T) {
	m := NewModel("/repo", "is:open author:@me", nil)
	m.SetRepo("noamsto/prdash")
	m.width, m.height = 100, 30
	m.loaded = true
	m.setPRs([]gh.PR{{Number: 1, Title: "one"}})
	m.SetDetailSource(failDetailSource{err: errors.New("detail boom")})

	msg := m.batchDetailCmd([]int{1})()
	got, _ := m.Update(msg)
	out := got.(Model)
	if out.err != nil {
		t.Fatalf("detail prefetch failure must not set the board error: %v", out.err)
	}

	out.setPRs(nil)
	out.renderList()
	if strings.Contains(out.render(), "Error:") {
		t.Fatalf("detail failure leaked into the board error screen: %q", out.render())
	}
}

// TestExpandedBodyShowsDetailFailure: the full-screen expanded tabs share the
// detail surface, so a failed detail fetch must show the failure there too, not
// "Loading…" forever (m.err is deliberately not involved).
func TestExpandedBodyShowsDetailFailure(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.SetRepo("noamsto/prdash")
	m.width, m.height = 100, 30
	m.setPRs([]gh.PR{{Number: 1, Title: "one"}})
	m.expanded = true
	m.expandedTab = tabReviews
	m.SetDetailSource(failDetailSource{err: errors.New("detail boom")})

	got, _ := m.Update(m.batchDetailCmd([]int{1})())
	out := got.(Model)
	body := out.expandedBody(80)
	if strings.Contains(body, "Loading") {
		t.Fatalf("expanded tab stuck on Loading after a detail failure: %q", body)
	}
	if !strings.Contains(body, "detail boom") {
		t.Fatalf("expanded tab should surface the detail failure: %q", body)
	}
}

// TestOmniDropdownShowsMembersFailureWhenPickerClosed: a member fetch that fails
// while the picker is closed must not vanish — the @-mention dropdown says the
// list is unavailable instead of showing an ordinary empty result.
func TestOmniDropdownShowsMembersFailureWhenPickerClosed(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.SetRepo("noamsto/prdash")
	m.width, m.height = 100, 30
	m.setPRs([]gh.PR{{Number: 7, Title: "hi"}})

	got, _ := m.Update(membersFailedMsg{err: errors.New("members boom")})
	m = got.(Model)
	if m.membersErr == nil {
		t.Fatal("members failure should be recorded on the model")
	}

	m.filtering = true
	m.filterInput.Focus()
	m.filterInput.SetValue("@")
	m.filterInput.SetCursor(len("@"))
	dd := m.omniSuggestDropdown()
	if dd == "" {
		t.Fatal("a failed member fetch with an @-partial should show a hint, not an empty dropdown")
	}
	if !strings.Contains(dd, "unavailable") {
		t.Fatalf("hint should name the failure: %q", dd)
	}
}

// TestPickerShowsMembersFailure: when the assignable-users fetch fails with the
// picker open, the picker must render the failure, not stay on "Loading…".
func TestPickerShowsMembersFailure(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.SetRepo("noamsto/prdash")
	m.width, m.height = 100, 30
	m.setPRs([]gh.PR{{Number: 7, Title: "hi"}})
	m.SetMembersSource(failMembersSource{err: errors.New("members boom")})

	cmd := m.openPicker("reviewer")
	if cmd == nil {
		t.Fatal("openPicker should fetch members when none are cached")
	}
	got, _ := m.Update(cmd())
	out := got.(Model)
	if !out.showPicker {
		t.Fatal("picker should stay open on a member-fetch failure")
	}
	view := out.render()
	if strings.Contains(view, "Loading") {
		t.Fatalf("picker stuck on Loading after a member-fetch failure: %q", view)
	}
	if !strings.Contains(view, "members boom") {
		t.Fatalf("picker should surface the member-fetch failure: %q", view)
	}
}
