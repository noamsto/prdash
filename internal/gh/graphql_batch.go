package gh

import (
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
)

// repoGroup is one repository's slice of a batch: the PR numbers to select
// under a single aliased repository(...) block.
type repoGroup struct {
	Repo    string
	Numbers []int
}

// batchSpec describes one kind of aliased lookup. maxPRs bounds the PR
// selections per request so a chunk stays far below GitHub's 500k node limit
// (details cost ~2.4k nodes per PR, hence the lower cap).
type batchSpec struct {
	label       string // error prefix: "checks", "states", "detail"
	accept      string // optional Accept header (merge-info preview for details)
	aliasPrefix string // per-number alias prefix: pr7, n7
	field       string // PR lookup field: "pullRequest", "issueOrPullRequest"
	selection   string
	maxPRs      int
	// strict fails a repo's PRs in the request on any error scoped to it (a repo
	// split across chunks fails per chunk). Non-strict (states) fails
	// it only when its block resolved to null, so a null PR alias doesn't
	// discard the siblings that did resolve.
	strict bool
}

// chunkRefs groups refs by repo (sorted) and packs them into chunks of at most
// maxPRs selections. A repo's numbers may straddle two chunks.
func chunkRefs(refs []PRRef, maxPRs int) [][]repoGroup {
	repos, numbers := groupRefs(refs)
	var chunks [][]repoGroup
	var cur []repoGroup
	room := maxPRs
	for _, repo := range repos {
		ns := numbers[repo]
		for len(ns) > 0 {
			if room == 0 {
				chunks = append(chunks, cur)
				cur, room = nil, maxPRs
			}
			take := min(room, len(ns))
			cur = append(cur, repoGroup{Repo: repo, Numbers: ns[:take]})
			ns = ns[take:]
			room -= take
		}
	}
	if len(cur) > 0 {
		chunks = append(chunks, cur)
	}
	return chunks
}

// buildBatchQuery aliases one repository block per group, each holding one
// aliased PR lookup per number. Owner and name travel as per-alias variables, so
// nothing user-derived is ever spliced into the query text; numbers are ints.
// Groups must already have passed splitRepo.
func buildBatchQuery(groups []repoGroup, spec batchSpec) (string, map[string]string) {
	vars := make(map[string]string, 2*len(groups))
	var decl, body strings.Builder
	for i, g := range groups {
		owner, name, _ := splitRepo(g.Repo)
		if i > 0 {
			decl.WriteByte(',')
		}
		fmt.Fprintf(&decl, "$o%d:String!,$n%d:String!", i, i)
		vars[fmt.Sprintf("o%d", i)] = owner
		vars[fmt.Sprintf("n%d", i)] = name
		fmt.Fprintf(&body, "r%d:repository(owner:$o%d,name:$n%d){", i, i, i)
		for _, n := range g.Numbers {
			fmt.Fprintf(&body, "%s%d:%s(number:%d){%s}", spec.aliasPrefix, n, spec.field, n, spec.selection)
		}
		body.WriteByte('}')
	}
	return "query(" + decl.String() + "){" + body.String() + "}", vars
}

// fetchAliased resolves refs in as few requests as the chunk cap allows and
// hands every resolved PR's raw JSON to apply. A failing repo leaves the others'
// results applied and joins its error (once per repo) into the returned error;
// a transport or HTTP failure fails only its own chunk.
func (s GraphSource) fetchAliased(spec batchSpec, refs []PRRef, apply func(PRRef, json.RawMessage) error) error {
	var errs []error
	bad := map[string]bool{}
	valid := make([]PRRef, 0, len(refs))
	for _, r := range refs {
		if _, _, err := splitRepo(r.Repo); err != nil {
			if !bad[r.Repo] {
				bad[r.Repo] = true
				errs = append(errs, err)
			}
			continue
		}
		valid = append(valid, r)
	}
	repoErrs := map[string]error{}
	for _, chunk := range chunkRefs(valid, spec.maxPRs) {
		chunkErr := s.postBatch(spec, chunk, apply, repoErrs)
		if chunkErr != nil {
			errs = append(errs, chunkErr)
		}
	}
	repos := make([]string, 0, len(repoErrs))
	for repo := range repoErrs {
		repos = append(repos, repo)
	}
	sort.Strings(repos)
	for _, repo := range repos {
		errs = append(errs, repoErrs[repo])
	}
	return errors.Join(errs...)
}

// postBatch sends one chunk. Per-repo failures land in repoErrs (first one
// wins, so a repo split across chunks reports once); the return is a failure
// that took the whole chunk down.
func (s GraphSource) postBatch(spec batchSpec, chunk []repoGroup, apply func(PRRef, json.RawMessage) error, repoErrs map[string]error) error {
	query, vars := buildBatchQuery(chunk, spec)
	reqBody, err := json.Marshal(map[string]any{"query": query, "variables": vars})
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, githubGraphQLURL, bytes.NewReader(reqBody))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if spec.accept != "" {
		req.Header.Set("Accept", spec.accept)
	}
	resp, err := s.http.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("graphql %s: %s: %s", spec.label, resp.Status, cleanBody(body))
	}
	return parseBatch(spec, chunk, body, apply, repoErrs)
}

// parseBatch maps the aliased response back onto chunk. Errors carry a path
// whose head names the failing repository alias; one without a path is not
// attributable, so it fails the chunk (strict kinds, or nothing resolved).
func parseBatch(spec batchSpec, chunk []repoGroup, body []byte, apply func(PRRef, json.RawMessage) error, repoErrs map[string]error) error {
	var resp struct {
		Data   map[string]json.RawMessage `json:"data"`
		Errors []struct {
			Message string `json:"message"`
			Path    []any  `json:"path"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return fmt.Errorf("parse %s: %w", spec.label, err)
	}
	scoped := map[string]string{}
	var unscoped string
	for _, e := range resp.Errors {
		alias, _ := firstString(e.Path)
		if alias == "" {
			if spec.strict || len(resp.Data) == 0 {
				return fmt.Errorf("graphql %s: %s", spec.label, CleanLine(e.Message))
			}
			if unscoped == "" {
				unscoped = CleanLine(e.Message)
			}
			continue
		}
		if _, ok := scoped[alias]; !ok {
			scoped[alias] = CleanLine(e.Message)
		}
	}
	fail := func(repo string, err error) {
		if _, ok := repoErrs[repo]; !ok {
			repoErrs[repo] = err
		}
	}
	for i, g := range chunk {
		alias := fmt.Sprintf("r%d", i)
		msg, hasErr := scoped[alias]
		block := resp.Data[alias]
		isNull := len(block) == 0 || string(block) == "null"
		if (spec.strict && hasErr) || isNull {
			if !hasErr {
				msg = cmp.Or(unscoped, "repository not resolved")
			}
			fail(g.Repo, fmt.Errorf("graphql %s: %s: %s", spec.label, g.Repo, msg))
			continue
		}
		var prs map[string]json.RawMessage
		if err := json.Unmarshal(block, &prs); err != nil {
			fail(g.Repo, fmt.Errorf("parse %s: %s: %w", spec.label, g.Repo, err))
			continue
		}
		for _, n := range g.Numbers {
			raw, ok := prs[fmt.Sprintf("%s%d", spec.aliasPrefix, n)]
			if !ok || string(raw) == "null" {
				continue
			}
			if err := apply(PRRef{Repo: g.Repo, Number: n}, raw); err != nil {
				fail(g.Repo, fmt.Errorf("parse %s: %s#%d: %w", spec.label, g.Repo, n, err))
			}
		}
	}
	return nil
}

func firstString(path []any) (string, bool) {
	if len(path) == 0 {
		return "", false
	}
	s, ok := path[0].(string)
	return s, ok
}
