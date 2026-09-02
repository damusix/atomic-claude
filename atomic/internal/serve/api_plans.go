// The /api/plans endpoint: one row per slug from the plans aggregator (see
// plans.go), scoped to a realm member when requested. Content reads go
// through the id-keyed resolver in api_plans_page.go — this file only lists.
package serve

import (
	"net/http"
	"path/filepath"
	"sort"
	"sync"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/realm"
	"github.com/damusix/atomic-claude/atomic/internal/wiki"
)

// plansRegistry shares plansAggregator instances between /api/plans and
// /api/plans/page. A worktree id is content-addressed from its resolved
// checkout path (see checkoutID in plans.go), so /api/plans/page must be
// able to resolve an id issued by any member's aggregator — not only the
// one the current request happens to name.
type plansRegistry struct {
	mu            sync.Mutex
	byRoot        map[string]*plansAggregator
	newAggregator func(root string) *plansAggregator
	// idIndex maps a worktree id to the root of the aggregator that issued
	// it, kept current by each aggregator's onBuild callback. It lets
	// resolveWorktree confirm freshness against the one aggregator that
	// owns an id instead of spawning `git worktree list` on every
	// aggregator ever built.
	idIndex map[string]string
}

func newPlansRegistry() *plansRegistry {
	return &plansRegistry{
		byRoot:        map[string]*plansAggregator{},
		newAggregator: newPlansAggregator,
		idIndex:       map[string]string{},
	}
}

func (p *plansRegistry) aggregator(root string) *plansAggregator {
	p.mu.Lock()
	defer p.mu.Unlock()
	a, ok := p.byRoot[root]
	if !ok {
		a = p.newAggregator(root)
		a.onBuild = func(resolver map[string]string) { p.updateIndex(root, resolver) }
		p.byRoot[root] = a
	}
	return a
}

// updateIndex replaces root's entries in idIndex with the ids resolver just
// built, so a removed checkout's id stops resolving on the next lookup.
func (p *plansRegistry) updateIndex(root string, resolver map[string]string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for id, r := range p.idIndex {
		if r == root {
			delete(p.idIndex, id)
		}
	}
	for id := range resolver {
		p.idIndex[id] = root
	}
}

// resolveWorktree finds the filesystem root id names. It looks up which
// aggregator issued id and confirms freshness against that aggregator
// alone — never every aggregator built so far — so an id owned by one
// member root costs one aggregator rebuild, not N. An id absent from the
// index, or no longer present after the owning aggregator refreshes, is
// rejected rather than resolved against a stale cached map.
func (p *plansRegistry) resolveWorktree(id string) (string, bool) {
	p.mu.Lock()
	root, known := p.idIndex[id]
	var owner *plansAggregator
	if known {
		owner = p.byRoot[root]
	}
	p.mu.Unlock()

	if !known || owner == nil {
		return "", false
	}

	_, resolver, _ := owner.rows()
	if root, ok := resolver[id]; ok {
		return root, true
	}
	return "", false
}

// plansOptions configures the /api/plans handler.
type plansOptions struct {
	// Root is the aggregator's target when no ?member is given — the realm
	// root in realm scope, the repo root otherwise (mirrors serve.go's
	// navRoot).
	Root string
	// ScopeRoot is the cwd realm.Resolve computes scope from (serve.go's
	// opts.TargetDir) — can differ from Root inside a realm.
	ScopeRoot     string
	ClaudeMDPath  string
	WikiIndexPath string
	Registry      *plansRegistry
}

// plansHandler serves GET /api/plans[?member=<key>].
func plansHandler(opts plansOptions) http.Handler {
	registry := opts.Registry
	if registry == nil {
		registry = newPlansRegistry()
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		root := opts.Root
		if member := r.URL.Query().Get("member"); member != "" {
			m, ok := findPlansMember(plansMembers(opts.ScopeRoot, opts.ClaudeMDPath, opts.WikiIndexPath), member)
			if !ok {
				writeAPIError(w, http.StatusNotFound, "unknown member: "+member)
				return
			}
			root = m.Path
		}
		rows, _, _ := registry.aggregator(root).rows()
		if rows == nil {
			rows = []planRow{}
		}
		writeAPIJSON(w, rows)
	})
}

func findPlansMember(members []codeMember, key string) (codeMember, bool) {
	for _, m := range members {
		if m.Prefix == key {
			return m, true
		}
	}
	for _, m := range members {
		if m.Key == key {
			return m, true
		}
	}
	return codeMember{}, false
}

// plansMembers enumerates every repo Plans can aggregate: the realm root
// itself — a git repo with its own docs and scratchpad that
// discoverCodeMembers() never lists (code_members.go:103-116 builds only
// from declared and wiki-scanned members) — plus every declared and
// wiki-scanned member, regardless of whether it carries a code index
// (discoverCodeMembers drops an unindexed wiki member as noise; a repo full
// of plans is not noise). Empty outside realm scope — the member picker only
// renders there.
func plansMembers(scopeRoot, claudeMDPath, wikiIndexPath string) []codeMember {
	res, err := realm.Resolve(scopeRoot, claudeMDPath)
	if err != nil || res.Scope != realm.ScopeRealmAll {
		return nil
	}
	root := res.RealmRoot
	if root == "" {
		root = scopeRoot
	}

	out := []codeMember{{Key: "", Prefix: "", Path: root}}
	seen := map[string]bool{"": true}

	for _, m := range res.Members {
		prefix := filepath.ToSlash(m.Path)
		if seen[prefix] {
			continue
		}
		out = append(out, codeMember{Key: m.Key, Prefix: prefix, Path: filepath.Join(root, m.Path)})
		seen[prefix] = true
	}

	if wikiIndexPath == "" {
		wikiIndexPath = filepath.Join(root, "wiki", "index.md")
	}
	if scanned, err := wiki.ReadScanMembers(wikiIndexPath); err == nil {
		for _, m := range scanned {
			prefix := filepath.ToSlash(m.Path)
			if seen[prefix] {
				continue
			}
			out = append(out, codeMember{Key: prefix, Prefix: prefix, Path: filepath.Join(root, m.Path)})
			seen[prefix] = true
		}
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Prefix < out[j].Prefix })
	return out
}

// plansMember is the client-facing projection of a codeMember: the key the
// picker sends back as ?member=, and the prefix it displays. The filesystem
// path never leaves the server.
type plansMember struct {
	Key    string `json:"key"`
	Prefix string `json:"prefix"`
}

type plansMembersResponse struct {
	Members []plansMember `json:"members"`
}

// plansMembersHandler serves GET /api/plans/members so the page can render
// its realm picker. One entry per repo Plans can aggregate, the realm root
// included; in repo scope the list has a single entry and the page renders
// no picker.
func plansMembersHandler(opts plansOptions) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		members := plansMembers(opts.ScopeRoot, opts.ClaudeMDPath, opts.WikiIndexPath)
		resp := plansMembersResponse{Members: make([]plansMember, 0, len(members))}
		for _, m := range members {
			resp.Members = append(resp.Members, plansMember{Key: m.Key, Prefix: m.Prefix})
		}
		writeAPIJSON(w, resp)
	})
}
