// Package frameworks — Ruby framework resolver (CP15 batch E, R2 DSL expansion).
//
// This file implements one FrameworkResolver + FrameworkExtractor pair for
// Ruby on Rails.
//
// # Language
//
// The resolver sets Language = types.LanguageRuby.
//
// # Comment stripping
//
// Ruby uses # for single-line comments. stripPyComments (defined in python.go)
// strips # lines — its triple-quote handling (' or ") is harmless on Ruby route
// files (triple-quoted strings don't appear in config/routes.rb).
//
// =begin/=end block comments (rare in route files) are NOT stripped — this is
// documented and acceptable, as they do not appear in real Rails route files.
//
// # Route node format (appendix H — via MakeRouteNode)
//
//	id:            route:{filePath}:{line}:{METHOD}:{path}
//	qualifiedName: {filePath}::METHOD:{path}
//	name:          "METHOD /path"
//
// # Rails route DSL idioms
//
// Supported forms in config/routes.rb:
//  1. `get '/path', to: 'controller#action'`       — colon-to form
//  2. `post '/path' => 'controller#action'`         — hash-rocket form
//  3. `root 'controller#action'`                   — root verb → path "/"
//  4. `resources :name [, param: :x] [, only: [...]] [, except: [...]]` — RESTful set (7 routes)
//  5. `resource :name [, only: [...]] [, except: [...]]`                 — singular set (no index, no :id)
//  6. `get :action, on: :collection` / `get :action, on: :member`        — nested collection/member routes
//
// HTTP verbs: get post put patch delete → uppercase in route node name.
// Handler = the action segment (after '#') of 'controller#action' for imperative forms;
// for resources/resource DSL, handler = "<controller>#<action>" convention.
//
// Nesting: one level of nested resources/resource is resolved using the parent
// path prefix. Deeper nesting is flattened (nested resource emitted at its own
// top-level path). This is a documented simplification for the code-intel index.
//
// # Detect
//
// Primary: Gemfile in projectRoot contains a line with `gem 'rails'` or
// `gem "rails"`. The match is substring-based so gem 'rails', '~> 7.1' also
// matches; it also avoids false-positives from gem names like 'activerecord'
// (which doesn't trigger 'rails').
package frameworks

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/resolution"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// ---------------------------------------------------------------------------
// Rails helper: # line comment stripper
// ---------------------------------------------------------------------------

// stripHashLineComments strips Ruby/Elixir-style # line comments from src,
// preserving line count. Reuses pyStripLine from python.go which handles #
// as a line comment. The triple-quote handling in pyStripLine is harmless for
// Ruby route files (triple-quoted strings don't appear in config/routes.rb).
//
// This function is shared by both the Rails and Phoenix resolvers.
func stripHashLineComments(src string) string {
	return stripPyComments(src)
}

// ---------------------------------------------------------------------------
// Rails regexes
// ---------------------------------------------------------------------------

// railsVerbRe matches the standard Rails DSL verb forms:
//
//	get  '/path', to: 'controller#action'
//	post '/path' => 'controller#action'
//	put  '/path', to: 'controller#action'
//	...
//
// Capture groups:
//
//	1 — HTTP verb (get|post|put|patch|delete)
//	2 — route path (single- or double-quoted)
//	3 — controller#action string (after `to:` or `=>`)
//
// Uses [^\S\n]* (spaces/tabs only, no newline) instead of \s* before the verb
// so that the match starts on the correct line and line-number calculation via
// strings.Count(src[:loc[0]], "\n") is accurate.
var railsVerbRe = regexp.MustCompile(
	`(?m)^[^\S\n]*(get|post|put|patch|delete)[^\S\n]+` +
		`['"]([^'"]+)['"]\s*` +
		`(?:,\s*to:\s*|=>\s*)` +
		`['"]([^'"#]+#[^'"]+)['"]`,
)

// railsRootRe matches `root 'controller#action'` or `root "controller#action"`.
// Capture group 1 = controller#action string.
// Uses [^\S\n]* (spaces/tabs only) so that the match starts on the correct line.
var railsRootRe = regexp.MustCompile(
	`(?m)^[^\S\n]*root[^\S\n]+['"]([^'"#]+#[^'"]+)['"]`,
)

// railsActionFromTarget extracts the action (last segment after '#') from a
// 'controller#action' string.
func railsActionFromTarget(target string) string {
	idx := strings.LastIndex(target, "#")
	if idx < 0 || idx == len(target)-1 {
		return ""
	}
	return strings.TrimSpace(target[idx+1:])
}

// ---------------------------------------------------------------------------
// Rails DSL: resources / resource expansion
// ---------------------------------------------------------------------------

// railsParamRe extracts the param: :xxx value from a resources line.
// Capture group 1 = param name (without colon).
var railsParamRe = regexp.MustCompile(`param:\s*:([A-Za-z_][A-Za-z0-9_]*)`)

// railsOnlyRe extracts the only: [...] list from a resources line.
// Capture group 1 = bracket contents (e.g. ":show, :update").
var railsOnlyRe = regexp.MustCompile(`only:\s*\[([^\]]+)\]`)

// railsExceptRe extracts the except: [...] list from a resources line.
// Capture group 1 = bracket contents.
var railsExceptRe = regexp.MustCompile(`except:\s*\[([^\]]+)\]`)

// railsCollectionMemberRe matches standalone `get :action, on: :collection|:member`
// inside a do block. Capture groups:
//
//	1 — HTTP verb (get|post|put|patch|delete)
//	2 — action name (symbol, without colon)
//	3 — "collection" or "member"
var railsCollectionMemberRe = regexp.MustCompile(
	`(?m)^[^\S\n]*(get|post|put|patch|delete)[^\S\n]+:([A-Za-z_][A-Za-z0-9_]*)\s*,\s*on:\s*:(collection|member)`,
)

// parseActionFilter parses an only:/except: action list string into a set.
// Input: ":show, :update" → {"show": true, "update": true}.
func parseActionFilter(s string) map[string]bool {
	result := map[string]bool{}
	for _, part := range strings.Split(s, ",") {
		name := strings.TrimSpace(part)
		name = strings.TrimPrefix(name, ":")
		if name != "" {
			result[name] = true
		}
	}
	return result
}

// resourcesBlockRe matches `resources/resource :name ... do` to detect block start.
var resourcesBlockRe = regexp.MustCompile(
	`^[^\S\n]*(resources?)[^\S\n]+:([A-Za-z_][A-Za-z0-9_]*)([^#\n]*)\s+do\s*$`,
)

// resourceLineRe matches `resources/resource :name [options]` without a do block.
var resourceLineRe = regexp.MustCompile(
	`^[^\S\n]*(resources?)[^\S\n]+:([A-Za-z_][A-Za-z0-9_]*)([^\n]*)$`,
)

// railsScopeBlockRe matches `scope`/`namespace` block openers that contribute a
// path segment. Supported forms (all must end with `do`):
//
//	namespace :api do
//	scope :api do
//	scope '/api' do
//	scope "api" do
//	scope path: :api do        (symbol value)
//	scope path: '/api' do      (string value)
//	scope path: "api" do
//
// Capture groups:
//
//	1 — "namespace" or "scope"
//	2 — path segment (symbol name OR quoted string, without leading slash normalization)
//
// A `scope` with no positional path argument and no `path:` key (e.g.
// `scope module: :api do`) does not contribute a path segment; those forms
// are not captured by this regex and are treated as opaque block openers
// (depth++, no scope prefix pushed).
var railsScopeBlockRe = regexp.MustCompile(
	`^[^\S\n]*(namespace|scope)[^\S\n]+` +
		`(?:` +
		// positional symbol:  scope :api do
		`:([A-Za-z_][A-Za-z0-9_/]*)` +
		`|` +
		// positional quoted string: scope '/api' do  or  scope "api" do
		`['"]([^'"]+)['"]` +
		`|` +
		// path: keyword with symbol:  scope path: :api do
		`(?:[^#\n]*[,\s])?path:\s*:([A-Za-z_][A-Za-z0-9_/]*)` +
		`|` +
		// path: keyword with quoted string: scope path: '/api' do
		`(?:[^#\n]*[,\s])?path:\s*['"]([^'"]+)['"]` +
		`)` +
		`[^\S\n]*(?:[^#\n]*)[^\S\n]+do\s*$`,
)

// railsParseIDParam extracts param name from a line (defaults to "id").
func railsParseIDParam(line string) string {
	if m := railsParamRe.FindStringSubmatch(line); len(m) >= 2 {
		return m[1]
	}
	return "id"
}

// railsFilterActions returns the actions to emit given only/except options from a line.
// actions is the full candidate set; returns the filtered subset.
func railsFilterActions(actions []string, line string) map[string]bool {
	if m := railsOnlyRe.FindStringSubmatch(line); len(m) >= 2 {
		return parseActionFilter(m[1])
	}
	if m := railsExceptRe.FindStringSubmatch(line); len(m) >= 2 {
		excluded := parseActionFilter(m[1])
		result := map[string]bool{}
		for _, a := range actions {
			if !excluded[a] {
				result[a] = true
			}
		}
		return result
	}
	// No filter: all actions allowed
	result := map[string]bool{}
	for _, a := range actions {
		result[a] = true
	}
	return result
}

// emitPluralResources appends route nodes and refs for a `resources :name` line.
// parentPath is the prefix path (empty string means top-level).
func emitPluralResources(
	filePath string, lineNum int, name string, line string, parentPath string,
	lang types.Language,
	nodes *[]types.Node, refs *[]types.UnresolvedReference,
	claimed map[string]bool,
) {
	paramName := railsParseIDParam(line)
	basePath := "/" + name
	if parentPath != "" {
		basePath = parentPath + "/" + name
	}
	idPath := basePath + "/:" + paramName

	allActions := []string{"index", "create", "show", "update", "destroy", "new", "edit"}
	allowed := railsFilterActions(allActions, line)

	controller := name

	type routeDef struct {
		method string
		path   string
		action string
	}
	routes := []routeDef{}

	if allowed["index"] {
		routes = append(routes, routeDef{"GET", basePath, "index"})
	}
	if allowed["create"] {
		routes = append(routes, routeDef{"POST", basePath, "create"})
	}
	if allowed["new"] {
		routes = append(routes, routeDef{"GET", basePath + "/new", "new"})
	}
	if allowed["show"] {
		routes = append(routes, routeDef{"GET", idPath, "show"})
	}
	if allowed["edit"] {
		routes = append(routes, routeDef{"GET", idPath + "/edit", "edit"})
	}
	if allowed["update"] {
		routes = append(routes, routeDef{"PATCH", idPath, "update"})
		routes = append(routes, routeDef{"PUT", idPath, "update"})
	}
	if allowed["destroy"] {
		routes = append(routes, routeDef{"DELETE", idPath, "destroy"})
	}

	for _, rt := range routes {
		node := MakeRouteNode(filePath, lineNum, rt.method, rt.path, lang)
		*nodes = append(*nodes, node)
		handler := controller + "#" + rt.action
		claimed[rt.action] = true
		*refs = append(*refs, types.UnresolvedReference{
			ID:            fmt.Sprintf("ref:%s:%d:%s:%s", filePath, lineNum, rt.method, rt.action),
			FromNodeID:    node.ID,
			ReferenceName: handler,
			ReferenceKind: types.EdgeKindReferences,
			Line:          lineNum,
			FilePath:      filePath,
			Language:      lang,
		})
	}
}

// emitSingularResource appends route nodes and refs for a `resource :name` line.
// parentPath is the prefix path (empty string means top-level).
func emitSingularResource(
	filePath string, lineNum int, name string, line string, parentPath string,
	lang types.Language,
	nodes *[]types.Node, refs *[]types.UnresolvedReference,
	claimed map[string]bool,
) {
	basePath := "/" + name
	if parentPath != "" {
		basePath = parentPath + "/" + name
	}

	allActions := []string{"show", "create", "update", "destroy", "new", "edit"}
	allowed := railsFilterActions(allActions, line)

	controller := name

	type routeDef struct {
		method string
		path   string
		action string
	}
	routes := []routeDef{}

	if allowed["show"] {
		routes = append(routes, routeDef{"GET", basePath, "show"})
	}
	if allowed["create"] {
		routes = append(routes, routeDef{"POST", basePath, "create"})
	}
	if allowed["new"] {
		routes = append(routes, routeDef{"GET", basePath + "/new", "new"})
	}
	if allowed["edit"] {
		routes = append(routes, routeDef{"GET", basePath + "/edit", "edit"})
	}
	if allowed["update"] {
		routes = append(routes, routeDef{"PATCH", basePath, "update"})
		routes = append(routes, routeDef{"PUT", basePath, "update"})
	}
	if allowed["destroy"] {
		routes = append(routes, routeDef{"DELETE", basePath, "destroy"})
	}

	for _, rt := range routes {
		node := MakeRouteNode(filePath, lineNum, rt.method, rt.path, lang)
		*nodes = append(*nodes, node)
		handler := controller + "#" + rt.action
		claimed[rt.action] = true
		*refs = append(*refs, types.UnresolvedReference{
			ID:            fmt.Sprintf("ref:%s:%d:%s:%s", filePath, lineNum, rt.method, rt.action),
			FromNodeID:    node.ID,
			ReferenceName: handler,
			ReferenceKind: types.EdgeKindReferences,
			Line:          lineNum,
			FilePath:      filePath,
			Language:      lang,
		})
	}
}

// ---------------------------------------------------------------------------
// DSL block parser: handles resources ... do ... end with one level of nesting
// ---------------------------------------------------------------------------

// railsBlockContext tracks the current resource block context while scanning lines.
type railsBlockContext struct {
	// parentPath is the URL prefix for nested resources inside this block.
	// For `resources :articles, param: :slug do` → parentPath = "/articles/:slug"
	// (collection routes use basePath "/articles", member routes use idPath)
	basePath   string // e.g. "/articles"
	idPath     string // e.g. "/articles/:slug"
	isSingular bool   // true = resource (singular), false = resources (plural)
	depth      int    // block nesting depth when this context was opened
}

// railsScopeSegment extracts the path segment from a railsScopeBlockRe match.
// Capture groups 2–5 correspond to the four alternatives in the regex:
// symbol, quoted string, path: :sym, path: "str". Returns "" when none matched.
func railsScopeSegment(m []string) string {
	for _, g := range m[2:] {
		if g != "" {
			// Normalise: strip leading slash so we can re-add consistently.
			return strings.TrimPrefix(g, "/")
		}
	}
	return ""
}

// railsScopePrefix joins scope segments into a path prefix. Empty segment list
// returns "". Each segment is prepended with "/" → "/api", "/api/v1", etc.
func railsScopePrefix(segs []string) string {
	if len(segs) == 0 {
		return ""
	}
	return "/" + strings.Join(segs, "/")
}

// railsParseDSL processes content line by line, expanding resources/resource blocks.
// It appends to nodes and refs and marks actions in claimed.
//
// Scope/namespace tracking (F-76): a separate scopeSegStack accumulates path
// segments contributed by scope/namespace block openers. The current scope
// prefix is prepended to every resource base path. Nested scopes compose
// (namespace :api { namespace :v1 } → /api/v1). scope/namespace blocks that
// carry no path segment (e.g. `scope module: :admin do`) increment depth
// without contributing to the path stack.
//
// Heuristic limits:
//   - Only handles forms matched by railsScopeBlockRe (sym, str, path: :sym,
//     path: "str"). `scope constraints: {...} do` without a positional path
//     or path: key is treated as an opaque block (no prefix contribution).
//   - `scope` with `module:` only and no path key: no path contribution.
//   - Imperative verb routes (get/post/etc.) inside a scope block are NOT
//     prefixed here; they are handled by Pass 1 regex which runs on the full
//     content — those paths are literal strings in the DSL and already carry
//     their full path.
func railsParseDSL(
	filePath, content string,
	lang types.Language,
	nodes *[]types.Node,
	refs *[]types.UnresolvedReference,
	claimed map[string]bool,
) {
	lines := strings.Split(content, "\n")

	// blockStack tracks open do...end blocks for resources/resource.
	// We only track resource blocks (not all Ruby blocks) for the path context.
	// Each entry = railsBlockContext. We use a simple depth counter for all blocks
	// so we know when a resource block closes.
	type blockEntry struct {
		ctx   railsBlockContext
		depth int // depth at which this resource block was opened
	}

	// scopeEntry tracks a scope/namespace block that contributed a path segment.
	type scopeEntry struct {
		segment string // path segment (without leading slash), e.g. "api"
		depth   int    // depth at which this scope block was opened
	}

	// depth = current do/end nesting depth (for all blocks, not just resources).
	depth := 0
	var resourceStack []blockEntry // stack of resource block contexts
	var scopeStack []scopeEntry    // stack of scope/namespace path contributors

	// currentScopePrefix builds the current URL prefix from scope segments.
	currentScopePrefix := func() string {
		segs := make([]string, len(scopeStack))
		for i, e := range scopeStack {
			segs[i] = e.segment
		}
		return railsScopePrefix(segs)
	}

	for lineIdx, rawLine := range lines {
		lineNum := lineIdx + 1
		line := strings.TrimSpace(rawLine)

		// Count do keywords that open a block on this line.
		// We check for `do` at end of line (possibly after params) or standalone `do`.
		// Also handle `end` closers.
		//
		// Simple heuristic: count "do" endings and "end" lines.
		opensBlock := strings.HasSuffix(line, " do") ||
			strings.HasSuffix(line, "\tdo") ||
			line == "do" ||
			(strings.HasSuffix(line, "do") && len(line) > 2 && (line[len(line)-3] == ' ' || line[len(line)-3] == '\t'))
		closesBlock := line == "end" || strings.HasPrefix(line, "end ")

		if closesBlock {
			if depth > 0 {
				depth--
			}
			// Pop resource context if its depth matches.
			if len(resourceStack) > 0 && resourceStack[len(resourceStack)-1].depth == depth+1 {
				resourceStack = resourceStack[:len(resourceStack)-1]
			}
			// Pop scope/namespace context if its depth matches.
			if len(scopeStack) > 0 && scopeStack[len(scopeStack)-1].depth == depth+1 {
				scopeStack = scopeStack[:len(scopeStack)-1]
			}
			continue
		}

		// Current parent context (innermost resource block, if any).
		var parentCtx *railsBlockContext
		if len(resourceStack) > 0 {
			parentCtx = &resourceStack[len(resourceStack)-1].ctx
		}

		// Check for scope/namespace block opener.
		// Must be tested BEFORE resourcesBlockRe because `namespace` also ends
		// in `do` but is not a resources keyword.
		if m := railsScopeBlockRe.FindStringSubmatch(rawLine); len(m) >= 2 {
			seg := railsScopeSegment(m)
			depth++
			if seg != "" {
				scopeStack = append(scopeStack, scopeEntry{segment: seg, depth: depth})
			}
			// No resource context push for scope/namespace — they only affect path prefix.
			continue
		}

		// Check for resources/resource declaration.
		if m := resourcesBlockRe.FindStringSubmatch(rawLine); len(m) >= 4 {
			// This line opens a block: `resources :name ... do`
			kind := m[1] // "resources" or "resource"
			name := m[2] // resource name
			opts := m[3] // rest of line before " do"

			// Determine path prefix from scope stack + parent resource context.
			scopePrefix := currentScopePrefix()
			parentPath := scopePrefix
			if parentCtx != nil {
				if parentCtx.isSingular {
					parentPath = parentCtx.basePath
				} else {
					parentPath = parentCtx.idPath
				}
			}

			paramName := railsParseIDParam(opts)
			basePath := parentPath + "/" + name
			idPath := basePath + "/:" + paramName

			isSingular := kind == "resource"

			// Emit routes for this resource.
			if isSingular {
				emitSingularResource(filePath, lineNum, name, opts, parentPath, lang, nodes, refs, claimed)
			} else {
				emitPluralResources(filePath, lineNum, name, opts, parentPath, lang, nodes, refs, claimed)
			}

			depth++
			resourceStack = append(resourceStack, blockEntry{
				ctx: railsBlockContext{
					basePath:   basePath,
					idPath:     idPath,
					isSingular: isSingular,
				},
				depth: depth,
			})
			continue
		}

		if m := resourceLineRe.FindStringSubmatch(rawLine); len(m) >= 4 {
			// Line without do block.
			// Skip if it ends with `do` (already handled above).
			opts := m[3]
			if strings.HasSuffix(strings.TrimSpace(opts), "do") {
				// already handled by resourcesBlockRe — shouldn't reach here
				goto handleBlock
			}
			kind := m[1]
			name := m[2]

			scopePrefix := currentScopePrefix()
			parentPath := scopePrefix
			if parentCtx != nil {
				if parentCtx.isSingular {
					parentPath = parentCtx.basePath
				} else {
					parentPath = parentCtx.idPath
				}
			}

			if kind == "resource" {
				emitSingularResource(filePath, lineNum, name, opts, parentPath, lang, nodes, refs, claimed)
			} else {
				emitPluralResources(filePath, lineNum, name, opts, parentPath, lang, nodes, refs, claimed)
			}
			goto handleBlock
		}

		// collection/member routes: `get :feed, on: :collection`
		if parentCtx != nil {
			if m := railsCollectionMemberRe.FindStringSubmatch(rawLine); len(m) >= 4 {
				verb := strings.ToUpper(m[1])
				action := m[2]
				onType := m[3] // "collection" or "member"

				var routePath string
				if onType == "collection" {
					routePath = parentCtx.basePath + "/" + action
				} else {
					// member: insert before :id
					routePath = parentCtx.idPath + "/" + action
				}

				node := MakeRouteNode(filePath, lineNum, verb, routePath, lang)
				*nodes = append(*nodes, node)
				claimed[action] = true
				*refs = append(*refs, types.UnresolvedReference{
					ID:            fmt.Sprintf("ref:%s:%d:%s:%s", filePath, lineNum, verb, action),
					FromNodeID:    node.ID,
					ReferenceName: action,
					ReferenceKind: types.EdgeKindReferences,
					Line:          lineNum,
					FilePath:      filePath,
					Language:      lang,
				})
				goto handleBlock
			}
		}

	handleBlock:
		if opensBlock {
			depth++
		}
	}
}

// ---------------------------------------------------------------------------
// Rails resolver
// ---------------------------------------------------------------------------

// RailsResolver implements FrameworkResolver + FrameworkExtractor for Rails.
type RailsResolver struct {
	projectRoot string
	claimed     map[string]bool
}

// NewRailsResolver creates a RailsResolver.
func NewRailsResolver(projectRoot string) *RailsResolver {
	return &RailsResolver{projectRoot: projectRoot, claimed: make(map[string]bool)}
}

// Name returns "rails".
func (r *RailsResolver) Name() string { return "rails" }

// Languages returns [ruby].
func (r *RailsResolver) Languages() []types.Language {
	return []types.Language{types.LanguageRuby}
}

// Detect returns true when the project Gemfile contains a `gem 'rails'` or
// `gem "rails"` entry.
func (r *RailsResolver) Detect(ctx context.Context) bool {
	data, err := os.ReadFile(filepath.Join(r.projectRoot, "Gemfile"))
	if err != nil {
		return false
	}
	return strings.Contains(string(data), `gem 'rails'`) ||
		strings.Contains(string(data), `gem "rails"`)
}

// Extract scans filePath/content for Rails route DSL entries and returns route
// nodes + handler refs. # line comments are stripped first.
//
// Two passes:
//  1. Imperative verb forms (get/post/put/patch/delete + root) via regex.
//  2. DSL resources/resource blocks via railsParseDSL line-by-line parser.
func (r *RailsResolver) Extract(filePath, content string) ([]types.Node, []types.UnresolvedReference) {
	stripped := stripHashLineComments(content)
	totalLines := strings.Count(content, "\n") + 1

	var nodes []types.Node
	var refs []types.UnresolvedReference

	// Pass 1: imperative verb routes (get/post/put/patch/delete).
	for _, loc := range railsVerbRe.FindAllStringSubmatchIndex(stripped, -1) {
		if len(loc) < 8 {
			continue
		}
		verb := strings.ToUpper(stripped[loc[2]:loc[3]])
		routePath := stripped[loc[4]:loc[5]]
		target := stripped[loc[6]:loc[7]]

		line := strings.Count(stripped[:loc[0]], "\n") + 1
		if line > totalLines {
			line = totalLines
		}

		action := railsActionFromTarget(target)
		node := MakeRouteNode(filePath, line, verb, routePath, types.LanguageRuby)
		nodes = append(nodes, node)

		if action != "" {
			r.claimed[action] = true
			refs = append(refs, types.UnresolvedReference{
				ID:            fmt.Sprintf("ref:%s:%d:%s:%s", filePath, line, verb, action),
				FromNodeID:    node.ID,
				ReferenceName: action,
				ReferenceKind: types.EdgeKindReferences,
				Line:          line,
				FilePath:      filePath,
				Language:      types.LanguageRuby,
			})
		}
	}

	// Pass 1b: root routes → GET /.
	for _, loc := range railsRootRe.FindAllStringSubmatchIndex(stripped, -1) {
		if len(loc) < 4 {
			continue
		}
		target := stripped[loc[2]:loc[3]]

		line := strings.Count(stripped[:loc[0]], "\n") + 1
		if line > totalLines {
			line = totalLines
		}

		action := railsActionFromTarget(target)
		node := MakeRouteNode(filePath, line, "GET", "/", types.LanguageRuby)
		nodes = append(nodes, node)

		if action != "" {
			r.claimed[action] = true
			refs = append(refs, types.UnresolvedReference{
				ID:            fmt.Sprintf("ref:%s:%d:GET:%s", filePath, line, action),
				FromNodeID:    node.ID,
				ReferenceName: action,
				ReferenceKind: types.EdgeKindReferences,
				Line:          line,
				FilePath:      filePath,
				Language:      types.LanguageRuby,
			})
		}
	}

	// Pass 2: DSL resources/resource blocks (use stripped content so comments
	// are already removed, preserving line numbers).
	railsParseDSL(filePath, stripped, types.LanguageRuby, &nodes, &refs, r.claimed)

	return nodes, refs
}

// ClaimsReference returns true if an action with this name was seen in Extract.
func (r *RailsResolver) ClaimsReference(name string) bool { return r.claimed[name] }

// Resolve returns confidence 0.85 for claimed action names.
func (r *RailsResolver) Resolve(ctx context.Context, ref types.UnresolvedReference) (resolution.ResolvedRef, error) {
	if !r.claimed[ref.ReferenceName] {
		return resolution.ResolvedRef{}, nil
	}
	return resolution.ResolvedRef{Confidence: 0.85}, nil
}
