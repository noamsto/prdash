package gh

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// StackMember is one PR in a stack. State is OPEN, CLOSED or MERGED.
type StackMember struct {
	Number  int
	State   string
	IsDraft bool
}

// Stack is a GitHub stacked-PR chain; Members run bottom to top.
type Stack struct {
	Number      int
	BaseRefName string
	Members     []StackMember
}

// StackMergeResult is the async stack-merge endpoint's answer, from both the
// submit and the status probe. Status is one of the StackMerge* constants (or
// whatever GitHub adds later; callers must not treat unknown as done).
type StackMergeResult struct {
	Status, Message, UUID, SHA string
}

const (
	StackMergePending  = "pending"
	StackMergeMerged   = "merged"
	StackMergeEnqueued = "enqueued"
	StackMergeFailed   = "failed"
)

// restReply is a fully read REST response, so callers never hold a body.
type restReply struct {
	code   int
	status string
	body   []byte
}

func (s GraphSource) doREST(req *http.Request) (restReply, error) {
	resp, err := s.http.Do(req) //nolint:gosec // G704: req targets restBase() with a PathEscaped uuid, not an arbitrary URL
	if err != nil {
		return restReply{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return restReply{}, err
	}
	return restReply{code: resp.StatusCode, status: resp.Status, body: body}, nil
}

// FetchStackForPR returns the stack containing prNumber; false when the PR is
// not in one.
func (s GraphSource) FetchStackForPR(prNumber int) (Stack, bool, error) {
	owner, name, err := s.repoParts()
	if err != nil {
		return Stack{}, false, err
	}
	req, err := s.restRequest(http.MethodGet, fmt.Sprintf("/repos/%s/%s/stacks?pull_request=%d", owner, name, prNumber))
	if err != nil {
		return Stack{}, false, err
	}
	reply, err := s.doREST(req)
	if err != nil {
		return Stack{}, false, err
	}
	if reply.code == http.StatusNotFound {
		return Stack{}, false, errors.New("stacked PRs aren't available for this repo")
	}
	if reply.code != http.StatusOK {
		return Stack{}, false, fmt.Errorf("stack lookup: %s: %s", reply.status, strings.TrimSpace(string(reply.body)))
	}
	var wire []struct {
		Number int `json:"number"`
		Base   struct {
			Ref string `json:"ref"`
		} `json:"base"`
		PullRequests []struct {
			Number   int    `json:"number"`
			State    string `json:"state"`
			Draft    bool   `json:"draft"`
			MergedAt string `json:"merged_at"`
		} `json:"pull_requests"`
	}
	if err := json.Unmarshal(reply.body, &wire); err != nil {
		return Stack{}, false, fmt.Errorf("parse stack: %w", err)
	}
	if len(wire) == 0 {
		return Stack{}, false, nil
	}
	st := Stack{Number: wire[0].Number, BaseRefName: wire[0].Base.Ref}
	for _, pr := range wire[0].PullRequests {
		state := strings.ToUpper(pr.State)
		if pr.MergedAt != "" {
			state = "MERGED"
		}
		st.Members = append(st.Members, StackMember{Number: pr.Number, State: state, IsDraft: pr.Draft})
	}
	return st, true, nil
}

const baseMergeQueueQuery = `query($owner:String!,$name:String!,$branch:String!,$qualified:String!){repository(owner:$owner,name:$name){mergeQueue(branch:$branch){id} ref(qualifiedName:$qualified){rules(first:50){nodes{type}}}}}`

// BaseUsesMergeQueue reports whether baseRef is merge-queue protected, either
// through the branch's merge queue or a MERGE_QUEUE ruleset rule.
func (s GraphSource) BaseUsesMergeQueue(baseRef string) (bool, error) {
	owner, name, err := s.repoParts()
	if err != nil {
		return false, err
	}
	reqBody, err := json.Marshal(map[string]any{
		"query": baseMergeQueueQuery,
		"variables": map[string]string{
			"owner": owner, "name": name, "branch": baseRef, "qualified": "refs/heads/" + baseRef,
		},
	})
	if err != nil {
		return false, err
	}
	req, err := http.NewRequest(http.MethodPost, s.restBase()+"/graphql", bytes.NewReader(reqBody))
	if err != nil {
		return false, err
	}
	req.Header.Set("Content-Type", "application/json")
	reply, err := s.doREST(req)
	if err != nil {
		return false, err
	}
	if reply.code != http.StatusOK {
		return false, fmt.Errorf("graphql merge queue: %s: %s", reply.status, strings.TrimSpace(string(reply.body)))
	}
	var parsed struct {
		Data struct {
			Repository struct {
				MergeQueue *struct{ ID string } `json:"mergeQueue"`
				Ref        *struct {
					Rules struct {
						Nodes []struct{ Type string } `json:"nodes"`
					} `json:"rules"`
				} `json:"ref"`
			} `json:"repository"`
		} `json:"data"`
		Errors []struct{ Message string } `json:"errors"`
	}
	if err := json.Unmarshal(reply.body, &parsed); err != nil {
		return false, fmt.Errorf("parse merge queue: %w", err)
	}
	if len(parsed.Errors) > 0 {
		return false, fmt.Errorf("graphql merge queue: %s", parsed.Errors[0].Message)
	}
	repo := parsed.Data.Repository
	if repo.MergeQueue != nil {
		return true, nil
	}
	if repo.Ref == nil {
		return false, nil
	}
	for _, rule := range repo.Ref.Rules.Nodes {
		if rule.Type == "MERGE_QUEUE" {
			return true, nil
		}
	}
	return false, nil
}

// decodeStackMerge parses the merge-async submit/status body.
func decodeStackMerge(body []byte) (StackMergeResult, error) {
	var wire struct {
		Status  string `json:"status"`
		Details struct {
			Message string `json:"message"`
			UUID    string `json:"uuid"`
			SHA     string `json:"sha"`
		} `json:"details"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return StackMergeResult{}, fmt.Errorf("parse stack merge: %w", err)
	}
	return StackMergeResult{Status: wire.Status, Message: wire.Details.Message, UUID: wire.Details.UUID, SHA: wire.Details.SHA}, nil
}

// MergeStack asks GitHub to merge the stack up to and including prNumber:
// squash-merged directly, or enqueued when the base is merge-queue protected.
// It takes the PR number (REST), not the node ID the GraphQL mutations use.
func (s GraphSource) MergeStack(prNumber int, mergeQueue bool) (StackMergeResult, error) {
	owner, name, err := s.repoParts()
	if err != nil {
		return StackMergeResult{}, err
	}
	payloadBody := struct {
		MergeMethod string `json:"merge_method,omitempty"`
		MergeAction string `json:"merge_action"`
	}{MergeMethod: "squash", MergeAction: "direct_merge"}
	if mergeQueue {
		payloadBody.MergeMethod, payloadBody.MergeAction = "", "merge_queue"
	}
	payload, err := json.Marshal(payloadBody)
	if err != nil {
		return StackMergeResult{}, err
	}
	req, err := s.restRequestJSON(http.MethodPut, fmt.Sprintf("/repos/%s/%s/pulls/%d/merge-async", owner, name, prNumber), payload)
	if err != nil {
		return StackMergeResult{}, err
	}
	reply, err := s.doREST(req)
	if err != nil {
		return StackMergeResult{}, err
	}
	switch reply.code {
	case http.StatusOK, http.StatusAccepted:
		return decodeStackMerge(reply.body)
	case http.StatusNotFound:
		return StackMergeResult{}, errors.New("async stack merge isn't available for this repo")
	case http.StatusConflict:
		return StackMergeResult{}, errors.New("a merge for this stack is already in progress")
	}
	return StackMergeResult{}, fmt.Errorf("stack merge: %s: %s", reply.status, strings.TrimSpace(string(reply.body)))
}

// StackMergeStatus probes an in-flight merge started by MergeStack.
func (s GraphSource) StackMergeStatus(prNumber int, uuid string) (StackMergeResult, error) {
	owner, name, err := s.repoParts()
	if err != nil {
		return StackMergeResult{}, err
	}
	req, err := s.restRequest(http.MethodGet, fmt.Sprintf("/repos/%s/%s/pulls/%d/merge-async/%s", owner, name, prNumber, url.PathEscape(uuid)))
	if err != nil {
		return StackMergeResult{}, err
	}
	reply, err := s.doREST(req)
	if err != nil {
		return StackMergeResult{}, err
	}
	if reply.code != http.StatusOK {
		return StackMergeResult{}, fmt.Errorf("stack merge status: %s: %s", reply.status, strings.TrimSpace(string(reply.body)))
	}
	return decodeStackMerge(reply.body)
}
