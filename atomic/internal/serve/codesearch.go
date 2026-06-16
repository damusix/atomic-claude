// codesearch.go — CP7: federated code search handler (/code/search).
//
// Route: GET /code/search?q=<query>[&only=k1,k2][&exclude=k3]
//
// Scope handling:
//   - ScopeRealmAll: fan out across non-excluded members; each queried
//     independently via MemberSearchFn. Results grouped under [key] headers.
//     A member whose db is missing/unopenable is skipped with a visible
//     "not indexed — run atomic code index" note; the operation continues.
//   - ScopeRealmMember: search the single member db (no [key] wrap).
//   - ScopeRepo / ScopeNoIndex: search the single local index db.
//
// "only" / "exclude" query params (comma-separated keys) filter the member set,
// mirroring the atomic code --only/--exclude flag semantics.
//
// Each result links to /file/<FilePath>#L<StartLine> (the CP2 file view).
//
// Design seam: MemberSearchFn is the injectable seam for opening an engine,
// querying SearchNodes, and closing it.  DefaultMemberSearchFn is the production
// implementation.  Tests inject a fake so they never touch a real SQLite file
// (except in the production-wiring test, which builds a tiny real index).
package serve

import (
	"context"
	"fmt"
	"html/template"
	"net/http"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/engine"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/realm"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// MemberSearchFn is the seam for opening a member's index and running a search.
// ctx is the request context; memberPath is the absolute member repo root;
// dbPath is the absolute path to the member's db file; query is the raw query
// string.  Returns the ranked results, or an error if the db is absent or
// unopenable (treated as "not indexed" by the handler).
type MemberSearchFn func(ctx context.Context, memberPath, dbPath, query string) ([]types.SearchResult, error)

// DefaultMemberSearchFn returns the production MemberSearchFn:
// NewWithDBPath → Open(ctx) → SearchNodes → Close.
// The engine is opened read-only (Open, not Init) and closed before return.
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

// CodeSearchOptions configures NewCodeSearchHandler.
type CodeSearchOptions struct {
	// RealmRoot is the root directory to resolve realm config from.
	RealmRoot string
	// ClaudeMDPath is used by realm.Resolve to find <wikis> registrations.
	ClaudeMDPath string
	// SearchFn is the MemberSearchFn to use. nil → DefaultMemberSearchFn().
	SearchFn MemberSearchFn
}

// codeSearchHandler implements http.Handler for /code/search.
type codeSearchHandler struct {
	realmRoot    string
	claudeMDPath string
	searchFn     MemberSearchFn
}

// NewCodeSearchHandler returns an http.Handler for GET /code/search?q=...
func NewCodeSearchHandler(opts CodeSearchOptions) http.Handler {
	fn := opts.SearchFn
	if fn == nil {
		fn = DefaultMemberSearchFn()
	}
	return &codeSearchHandler{
		realmRoot:    opts.RealmRoot,
		claudeMDPath: opts.ClaudeMDPath,
		searchFn:     fn,
	}
}

// memberResult is the result for one realm member (or the single-repo case).
type memberResult struct {
	Key        string // empty for single-repo scope (no [key] header)
	Prefix     string // realm-relative path prefix for result /file/ links ("" = served root)
	Results    []types.SearchResult
	NotIndexed bool   // true when the member db was absent/unopenable
	ErrMsg     string // descriptive note when NotIndexed == true
}

func (h *codeSearchHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	only := splitCommaParam(r.URL.Query().Get("only"))
	excl := splitCommaParam(r.URL.Query().Get("exclude"))

	ctx := r.Context()

	// Resolve realm scope.
	res, err := realm.Resolve(h.realmRoot, h.claudeMDPath)
	if err != nil {
		http.Error(w, "scope resolve: "+err.Error(), http.StatusInternalServerError)
		return
	}

	var groups []memberResult

	groups = codeSearchGroups(ctx, res, h.realmRoot, only, excl, query, h.searchFn, nil)

	isHTMX := r.Header.Get("HX-Request") == "true"
	h.render(w, query, groups, isHTMX)
}

// codeSearchGroups resolves the search targets for a realm.Resolution and runs
// the per-member search. It is shared by the synchronous /code/search handler
// and the streaming /search/stream endpoint.
//
// Members are discovered via discoverCodeMembers, which unions realm federation
// with per-member self-indexes (so a member indexed with `cd member; atomic code
// index` is searchable even when the realm has no <code-index> federation). They
// are searched CONCURRENTLY (bounded goroutine pool): one slow/large member no
// longer blocks the others. The returned slice is in member order (deterministic);
// when onGroup is non-nil it is also invoked once per member AS THAT MEMBER
// COMPLETES (completion order) — the seam the SSE endpoint uses to push each
// result the moment it is ready. onGroup calls are serialized, so a streaming
// writer never sees interleaved output.
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

// fanOutMembers searches every member concurrently (bounded by CPU count, max 8)
// and returns the results in member order. onGroup, when non-nil, fires once per
// member at completion time (serialized) for streaming.
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
	var emitMu sync.Mutex // serializes onGroup so streaming writes never interleave

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

// searchMember searches one member; never aborts — error → NotIndexed note.
// The searchFn is the sole authority: if it errors (including db absent/unopenable),
// the member is reported "not indexed" and the fan-out continues.
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

// render writes the HTML response.  isHTMX = true → fragment only (no shell).
func (h *codeSearchHandler) render(w http.ResponseWriter, query string, groups []memberResult, isHTMX bool) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	var sb strings.Builder

	if !isHTMX {
		// Minimal full-page wrapper so the page is usable standalone.
		sb.WriteString(`<!DOCTYPE html><html><head><meta charset="utf-8">`)
		sb.WriteString(`<title>Code Search</title></head><body>`)
	}

	// Search box. No hx-push-url: /code/search renders a shell-less fragment, so
	// pushing it would make a history restore replace <body> with this bare
	// fragment. The canonical, history-safe search surface is /search.
	sb.WriteString(`<div class="code-search-box">`)
	sb.WriteString(`<form hx-get="/code/search" hx-target="#main-pane">`)
	sb.WriteString(`<input name="q" type="search" placeholder="Search symbols…" value="`)
	sb.WriteString(template.HTMLEscapeString(query))
	sb.WriteString(`">`)
	sb.WriteString(`<button type="submit">Search</button>`)
	sb.WriteString(`</form></div>`)

	if query == "" {
		sb.WriteString(`<p class="code-search-hint">Enter a query to search the code index.</p>`)
		if !isHTMX {
			sb.WriteString(`</body></html>`)
		}
		fmt.Fprint(w, sb.String())
		return
	}

	// Results section.
	sb.WriteString(`<div class="code-search-results">`)

	if len(groups) == 0 {
		// No code members resolved at all (e.g. a wiki realm with no <code-index>
		// federation, or a bare repo with no index). Say so — never render blank.
		sb.WriteString(codeSearchNoIndexNote())
	} else {
		for _, g := range groups {
			renderMemberGroup(&sb, g)
		}
	}

	sb.WriteString(`</div>`)

	if !isHTMX {
		sb.WriteString(`</body></html>`)
	}

	fmt.Fprint(w, sb.String())
}

// codeSearchNoIndexNote is the message shown when no index is available to search.
func codeSearchNoIndexNote() string {
	return `<p class="code-search-not-indexed">No code index for this realm — run <code>atomic code index</code> to enable code search.</p>`
}

// renderMemberGroup writes one member's result group: an optional [key] header,
// then a not-indexed note, a "no results" note, or the result list. Shared by
// the synchronous handler and the streaming endpoint (one SSE event per group).
func renderMemberGroup(sb *strings.Builder, g memberResult) {
	if g.Key != "" {
		sb.WriteString(`<h3 class="code-search-group-header">[`)
		sb.WriteString(template.HTMLEscapeString(g.Key))
		sb.WriteString(`]</h3>`)
	}

	if g.NotIndexed {
		sb.WriteString(`<p class="code-search-not-indexed">`)
		if g.Key != "" {
			sb.WriteString(template.HTMLEscapeString(g.Key))
			sb.WriteString(`: `)
		}
		sb.WriteString(`not indexed — run <code>atomic code index</code></p>`)
		return
	}

	if len(g.Results) == 0 {
		sb.WriteString(`<p class="code-search-empty">No results.</p>`)
		return
	}

	sb.WriteString(`<ul class="code-search-result-list">`)
	for _, r := range g.Results {
		n := r.Node
		sb.WriteString(`<li class="code-search-result">`)
		// Link: /file/<member-prefix>/<FilePath>#L<StartLine>. The member db stores
		// member-relative paths; prefix the member's realm-relative path so the
		// /file/ route (which serves realm-relative paths) resolves correctly.
		href := fmt.Sprintf("/file/%s#L%d", joinMemberPath(g.Prefix, n.FilePath), n.StartLine)
		sb.WriteString(`<a href="`)
		sb.WriteString(template.HTMLEscapeString(href))
		sb.WriteString(`" class="code-search-link">`)
		sb.WriteString(`<span class="code-search-name">`)
		sb.WriteString(template.HTMLEscapeString(n.Name))
		sb.WriteString(`</span>`)
		sb.WriteString(`<span class="code-search-kind"> `)
		sb.WriteString(template.HTMLEscapeString(string(n.Kind)))
		sb.WriteString(`</span>`)
		sb.WriteString(`<span class="code-search-loc"> — `)
		sb.WriteString(template.HTMLEscapeString(n.FilePath))
		if n.StartLine > 0 {
			sb.WriteString(fmt.Sprintf(":%d", n.StartLine))
		}
		sb.WriteString(`</span>`)
		sb.WriteString(`</a>`)
		sb.WriteString(`</li>`)
	}
	sb.WriteString(`</ul>`)
}

// splitCommaParam splits a comma-separated query param value into trimmed keys.
// Empty param → nil (no filter).
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

// filterMemberSet applies only/exclude filters to the member list,
// mirroring cli.filterMembers semantics: --only takes precedence over --exclude.
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
