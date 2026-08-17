// Federated code search. In realm scope it fans out across members, each
// queried independently; a member whose db is missing or unopenable is
// reported as not indexed and the rest of the search continues. The only and
// exclude params filter the member set, mirroring `atomic code`'s flags.
//
// MemberSearchFn is the seam that opens, queries, and closes an engine, so
// tests never touch a real SQLite file.
package serve

import (
	"context"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/engine"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/realm"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// MemberSearchFn searches one member's index. An error — including an absent
// or unopenable db — is what the handler reports as "not indexed".
type MemberSearchFn func(ctx context.Context, memberPath, dbPath, query string) ([]types.SearchResult, error)

// DefaultMemberSearchFn opens the engine read-only and closes it before
// returning.
func DefaultMemberSearchFn() MemberSearchFn {
	return func(ctx context.Context, memberPath, dbPath string, query string) ([]types.SearchResult, error) {
		eng, err := engine.NewWithDBPath(memberPath, dbPath)
		if err != nil {
			return nil, fmt.Errorf("code search: create engine: %w", err)
		}
		defer eng.Close()

		if err := eng.Open(ctx); err != nil {
			return nil, fmt.Errorf("code search: open index: %w", err)
		}

		results, err := eng.SearchNodes(ctx, types.SearchOptions{
			Query: query,
			Limit: 50,
		})
		if err != nil {
			return nil, fmt.Errorf("code search: search: %w", err)
		}
		return results, nil
	}
}

// CodeSearchOptions configures NewAPICodeSearchHandler.
type CodeSearchOptions struct {
	// RealmRoot is the root to resolve realm config from.
	RealmRoot string
	// ClaudeMDPath lets realm.Resolve find <wikis> registrations.
	ClaudeMDPath string
	// SearchFn nil takes DefaultMemberSearchFn.
	SearchFn MemberSearchFn
}

// memberResult is one member's slice of a search.
type memberResult struct {
	Key        string // empty in single-repo scope
	Prefix     string // realm-relative prefix for result links
	Results    []types.SearchResult
	NotIndexed bool
	ErrMsg     string
}

// codeSearchGroups runs the per-member search, shared by the synchronous
// handler and the SSE stream. The returned slice is in member order; onGroup,
// when set, fires in completion order instead — the seam the stream uses to
// push each member the moment it lands. Its calls are serialized, so a
// streaming writer never sees interleaved output.
func codeSearchGroups(
	ctx context.Context,
	res realm.Resolution,
	realmRoot string,
	only, excl []string,
	query string,
	fn MemberSearchFn,
	onGroup func(memberResult),
) []memberResult {
	root := res.RealmRoot
	if root == "" {
		root = realmRoot
	}
	wikiIndexPath := filepath.Join(root, "wiki", "index.md")
	members := filterMemberSet(discoverCodeMembers(res, realmRoot, wikiIndexPath), only, excl)
	return fanOutMembers(ctx, members, query, fn, onGroup)
}

// fanOutMembers searches concurrently, bounded by CPU count, so one large
// member does not block the rest.
func fanOutMembers(
	ctx context.Context,
	members []codeMember,
	query string,
	fn MemberSearchFn,
	onGroup func(memberResult),
) []memberResult {
	n := len(members)
	if n == 0 {
		return nil
	}
	out := make([]memberResult, n)

	maxConc := runtime.NumCPU()
	if maxConc > 8 {
		maxConc = 8
	}
	if maxConc < 1 {
		maxConc = 1
	}
	sem := make(chan struct{}, maxConc)
	var wg sync.WaitGroup
	var emitMu sync.Mutex // so streaming writes never interleave

	for i, m := range members {
		wg.Add(1)
		go func(i int, m codeMember) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			mr := searchMember(ctx, fn, m, query)
			out[i] = mr
			if onGroup != nil {
				emitMu.Lock()
				onGroup(mr)
				emitMu.Unlock()
			}
		}(i, m)
	}
	wg.Wait()
	return out
}

// searchMember never aborts the fan-out: any error becomes a NotIndexed note.
func searchMember(ctx context.Context, fn MemberSearchFn, m codeMember, query string) memberResult {
	mr := memberResult{Key: m.Key, Prefix: m.Prefix}
	if query == "" {
		return mr
	}
	results, err := fn(ctx, m.Path, m.DBPath, query)
	if err != nil {
		mr.NotIndexed = true
		mr.ErrMsg = "not indexed — run atomic code index"
		return mr
	}
	mr.Results = results
	return mr
}

// splitCommaParam returns nil for an empty param, meaning no filter.
func splitCommaParam(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// filterMemberSet mirrors cli.filterMembers: only takes precedence over
// exclude.
func filterMemberSet(members []codeMember, only, excl []string) []codeMember {
	if len(only) > 0 {
		onlySet := make(map[string]bool, len(only))
		for _, k := range only {
			onlySet[k] = true
		}
		var out []codeMember
		for _, m := range members {
			if onlySet[m.Key] {
				out = append(out, m)
			}
		}
		return out
	}
	if len(excl) > 0 {
		exclSet := make(map[string]bool, len(excl))
		for _, k := range excl {
			exclSet[k] = true
		}
		var out []codeMember
		for _, m := range members {
			if !exclSet[m.Key] {
				out = append(out, m)
			}
		}
		return out
	}
	return members
}
