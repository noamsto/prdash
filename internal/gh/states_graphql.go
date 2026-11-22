package gh

import (
	"encoding/json"
	"time"
)

// statesFields is the narrowest selection that answers "what is this row's
// real state now?": the state plus the timestamps a held row's tag and sort
// key need, nothing else.
const statesFields = "__typename ... on PullRequest{state mergedAt closedAt} ... on Issue{state closedAt}"

// statesSpec is non-strict: an alias resolving to null (deleted, transferred)
// carries a scoped error that must not discard its siblings.
var statesSpec = batchSpec{
	label: "states", aliasPrefix: "n", field: "issueOrPullRequest",
	selection: statesFields, maxPRs: 100,
}

// FetchStates looks up the current state of every ref, batching every repo into
// one aliased request per chunk. This is the held-row lookup: a departed or
// stale row's real state without a full list refetch. A failing repo leaves the
// others' results in the map and joins its error into err.
func (s GraphSource) FetchStates(refs []PRRef) (map[PRRef]ItemState, error) {
	out := make(map[PRRef]ItemState, len(refs))
	err := s.fetchAliased(statesSpec, refs, func(ref PRRef, raw json.RawMessage) error {
		var ql qlState
		if err := json.Unmarshal(raw, &ql); err != nil {
			return err
		}
		out[ref] = mapState(ql)
		return nil
	})
	return out, err
}

// qlState is one issueOrPullRequest lookup result. PR and Issue share no
// field names beyond state, so both fragments flatten into one struct and
// __typename says which half is populated.
type qlState struct {
	Typename string     `json:"__typename"`
	State    string     `json:"state"`
	MergedAt *time.Time `json:"mergedAt"`
	ClosedAt *time.Time `json:"closedAt"`
}

func mapState(ql qlState) ItemState {
	st := ItemState{State: ql.State}
	if ql.MergedAt != nil {
		st.MergedAt = *ql.MergedAt
	}
	if ql.ClosedAt != nil {
		st.ClosedAt = *ql.ClosedAt
	}
	sanitize(&st)
	return st
}
