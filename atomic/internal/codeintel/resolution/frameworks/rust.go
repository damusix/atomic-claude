// Package frameworks — Rust framework resolvers (CP15 batch D).
//
// This file implements three FrameworkResolver + FrameworkExtractor pairs for
// the three major Rust web frameworks: actix-web, axum, and rocket.
//
// # Language
//
// All three resolvers set Language = types.LanguageRust.
//
// # Comment stripping
//
// Rust comments use the same delimiters as JS/TS:
//   - Single-line: // to end of line.
//   - Block: /* ... */ (can span lines).
//
// stripJSComments (defined in frameworks.go) handles both — no Rust-specific
// stripper needed. Comments are stripped before route regexes run, so
// commented-out routes never emit route nodes.
//
// # Route node format (appendix H — via MakeRouteNode)
//
//	id:            route:{filePath}:{line}:{METHOD}:{path}
//	qualifiedName: {filePath}::METHOD:{path}
//	name:          "METHOD /path"
//
// # Attribute-macro pattern (actix-web, rocket)
//
// Both actix-web and rocket use Rust attribute macros above functions:
//
//	#[get("/path")]
//	async fn handler_name() -> ... { ... }
//
// The route decorator is on its own line; the function definition follows on
// the next non-blank, non-attribute line. The bounded-lookahead (modelled on
// nestHandlerName in node.go) scans forward, skipping:
//   - blank lines
//   - lines starting with '#' (additional attribute lines like #[allow(...)])
//
// It stops and returns "" at any line starting with '}' or 'pub ' followed
// by 'struct'/'enum'/'mod' (struct/module boundaries) to avoid misattribution.
//
// # Axum Router chain form
//
// axum uses a builder chain: Router::new().route("/p", get(handler)).
// Multiple method handlers on the same path appear as a method chain:
//
//	.route("/p", get(h1).post(h2))
//
// Each method call in the chain produces one route node.
//
// # Detect
//
// Each resolver reads Cargo.toml in the project root and looks for the
// framework's crate name as a substring match.
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
// Shared Rust helpers
// ---------------------------------------------------------------------------

// cargoHasDep returns true if the project's Cargo.toml contains the given
// crate name as a substring (e.g. "actix-web").
func cargoHasDep(projectRoot, crateName string) bool {
	data, err := os.ReadFile(filepath.Join(projectRoot, "Cargo.toml"))
	if err != nil {
		return false
	}
	return strings.Contains(string(data), crateName)
}

// lineOf returns the 1-based line number for a byte offset in src.
func lineOf(src string, offset int) int {
	return strings.Count(src[:offset], "\n") + 1
}

// rustHandlerName performs a bounded lookahead from rest (text after a route
// attribute macro) to find the function name immediately following. It scans
// forward line-by-line, skipping:
//   - blank lines
//   - lines starting with '#' (stacked attribute macros)
//
// Returns "" if a class/struct boundary is reached (line starts with '}',
// "pub struct", "pub enum", "pub mod") before a function definition.
func rustHandlerName(rest string) string {
	for _, line := range strings.Split(rest, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue // blank line — keep scanning
		}
		if strings.HasPrefix(trimmed, "#") {
			continue // stacked attribute — keep scanning
		}
		if strings.HasPrefix(trimmed, "}") {
			return "" // module/impl boundary — stop
		}
		// Match: (pub )?(async )?fn name(
		if m := rustFnDefRe.FindStringSubmatch(line); m != nil {
			return m[1]
		}
		// Non-blank, non-attribute, non-fn line — stop
		return ""
	}
	return ""
}

// rustFnDefRe matches a Rust function definition line and captures the fn name.
// Handles: fn name(, async fn name(, pub fn name(, pub async fn name(
var rustFnDefRe = regexp.MustCompile(
	`^\s*(?:pub\s+)?(?:async\s+)?fn\s+([A-Za-z_][A-Za-z0-9_]*)\s*[(<]`,
)

// rustResolve is the shared Resolve implementation for Rust framework resolvers.
// Confidence 0.85 (midpoint of 0.8–0.9, appendix H).
func rustResolve(
	claimed map[string]bool,
	ctx context.Context,
	ref types.UnresolvedReference,
) (resolution.ResolvedRef, error) {
	if !claimed[ref.ReferenceName] {
		return resolution.ResolvedRef{}, nil
	}
	return resolution.ResolvedRef{Confidence: 0.85}, nil
}

// emitRustRoute builds a route node + references ref and appends them to the
// provided slices, recording handlerName in claimed.
func emitRustRoute(
	filePath string,
	line int,
	method, path, handlerName string,
	claimed map[string]bool,
	nodes *[]types.Node,
	refs *[]types.UnresolvedReference,
) {
	if method == "" {
		method = "ANY"
	}
	node := MakeRouteNode(filePath, line, method, path, types.LanguageRust)
	*nodes = append(*nodes, node)

	if handlerName != "" {
		claimed[handlerName] = true
		*refs = append(*refs, types.UnresolvedReference{
			ID:            fmt.Sprintf("ref:%s:%d:%s:%s", filePath, line, method, handlerName),
			FromNodeID:    node.ID,
			ReferenceName: handlerName,
			ReferenceKind: types.EdgeKindReferences,
			Line:          line,
			FilePath:      filePath,
			Language:      types.LanguageRust,
		})
	}
}

// ---------------------------------------------------------------------------
// actix-web resolver
// ---------------------------------------------------------------------------

// actixAttrRe matches actix-web HTTP attribute macros of the form:
//
//	#[get("/path")]
//	#[post("/path")]
//	#[put("/path")]    etc.
//
// Capture groups: 1=method (lowercase), 2=path.
var actixAttrRe = regexp.MustCompile(
	`(?m)#\[(get|post|put|delete|patch|head|options)\s*\(\s*"([^"]+)"\s*(?:,[^)]*)??\)\]`,
)

// actixResourceRe matches the actix-web web::resource(...).route(web::METHOD().to(handler))
// form by first finding the resource path and then extracting .route chains.
//
// Form: web::resource("/path").route(web::get().to(handler))
//
// We use two regexes:
//  1. actixResourcePathRe — finds web::resource("/path") and captures the path + end offset.
//  2. actixRouteMethodRe  — matches .route(web::METHOD().to(handler)) after the resource.
var actixResourcePathRe = regexp.MustCompile(
	`(?m)web::resource\s*\(\s*"([^"]+)"\s*\)`,
)

// actixRouteMethodRe matches a .route(web::METHOD().to(handler)) form (resource chain).
// Capture groups: 1=method (lowercase), 2=handler name.
var actixRouteMethodRe = regexp.MustCompile(
	`\.route\s*\(\s*web::([a-z]+)\s*\(\s*\)\s*\.to\s*\(\s*([A-Za-z_][A-Za-z0-9_:]*)\s*\)`,
)

// actixDirectRouteRe matches the direct-route form used with App::new(), cfg, or
// inside web::scope(...) chains:
//
//	App::new().route("/path", web::get().to(handler))
//	cfg.route("/path", web::get().to(handler))
//	web::scope("/users").route("", get().to(handler))   ← no web:: prefix
//
// The web:: prefix on the method is optional — actix-web allows importing the
// method functions directly (use actix_web::web::{get, post, ...}) and calling
// them as get(), post(), etc. without the web:: qualifier.
//
// Path is the FIRST argument (accepts "" for scope-relative routes); method +
// handler are the second argument. (?s) allows the match to span newlines when
// .route( arguments are split across lines.
//
// Capture groups: 1=path, 2=method (lowercase), 3=handler name (may be qualified).
var actixDirectRouteRe = regexp.MustCompile(
	`(?s)\.route\s*\(\s*"([^"]*)"\s*,\s*(?:web::)?(get|post|put|delete|patch|head|options)\s*\(\s*\)\s*\.to\s*\(\s*([A-Za-z_][A-Za-z0-9_:]*)\s*\)`,
)

// actixScopeRe matches a web::scope("PREFIX") declaration and captures the
// prefix string. Used by actixExtractScopedRoutes to build the scope stack.
//
// Capture group 1 = scope path prefix (e.g. "/api", "/users").
var actixScopeRe = regexp.MustCompile(
	`web::scope\s*\(\s*"([^"]*)"\s*\)`,
)

// actixJoinPaths joins a scope prefix and a route-relative path cleanly:
//
//	("/users", "")        → "/users"
//	("/users", "/login")  → "/users/login"
//	("/api",   "/v1")     → "/api/v1"
//
// Double slashes are prevented by not appending when rel is empty.
func actixJoinPaths(prefix, rel string) string {
	if rel == "" {
		return prefix
	}
	// rel always starts with "/" in actix-web .route() calls; just concatenate.
	return prefix + rel
}

// actixExtractScopedRoutes performs a scope-aware pass over stripped source,
// tracking web::scope("PREFIX") context via paren depth. For each .route(...)
// call found inside a scope chain, the accumulated scope prefix is prepended to
// the route path. Non-scoped .route() calls (at depth 0) use their literal path.
//
// Algorithm: walk the text byte by byte. On encountering web::scope("X"), push X
// onto the scope stack at the current paren depth. Track open/close parens to
// know when a scope's .service(...) ends and its prefix should be popped.
// On encountering .route("path", METHOD().to(handler)), emit a route with the
// accumulated scope prefix prepended to path.
//
// Heuristic limits:
//   - Scope prefix is determined by the nearest preceding web::scope("...") in
//     the same paren-nesting chain. Multiple independent scope chains in the
//     same file are handled correctly because paren depth is tracked globally
//     and each push/pop corresponds to one nesting level.
//   - Scope forms without a string literal (e.g. web::scope(path_var)) are not
//     captured and treated as unscoped — those rarely appear in practice.
//   - .route() calls inside web::resource(...) are handled by Pass 2 (resource
//     form) and are not re-emitted here (the resource path re is disjoint from
//     the scope re on the same .route() structure).
func actixExtractScopedRoutes(
	filePath, stripped string,
	totalLines int,
	claimed map[string]bool,
	nodes *[]types.Node,
	refs *[]types.UnresolvedReference,
) {
	// scopeEntry records a scope prefix pushed at a given paren depth.
	type scopeEntry struct {
		prefix string
		depth  int // paren depth at which this scope was entered
	}

	var scopeStack []scopeEntry
	parenDepth := 0
	pos := 0
	src := []byte(stripped)
	n := len(src)

	// currentPrefix returns the accumulated scope prefix from all stacked entries.
	currentPrefix := func() string {
		if len(scopeStack) == 0 {
			return ""
		}
		var sb strings.Builder
		for _, e := range scopeStack {
			sb.WriteString(e.prefix)
		}
		return sb.String()
	}

	// countParens counts the net paren delta in a byte slice (open - close).
	countParens := func(b []byte) int {
		delta := 0
		for _, c := range b {
			if c == '(' {
				delta++
			} else if c == ')' {
				delta--
			}
		}
		return delta
	}

	for pos < n {
		// Pop scope entries whose depth exceeds current paren depth (scope closed).
		for len(scopeStack) > 0 && scopeStack[len(scopeStack)-1].depth > parenDepth {
			scopeStack = scopeStack[:len(scopeStack)-1]
		}

		ch := src[pos]

		if ch == '(' {
			parenDepth++
			pos++
			continue
		}
		if ch == ')' {
			if parenDepth > 0 {
				parenDepth--
			}
			// Pop scope entries that opened at depth strictly above current (now-decremented) depth.
			for len(scopeStack) > 0 && scopeStack[len(scopeStack)-1].depth > parenDepth {
				scopeStack = scopeStack[:len(scopeStack)-1]
			}
			pos++
			continue
		}

		// Try to match web::scope("PREFIX") at this position.
		if loc := actixScopeRe.FindIndex(src[pos:]); loc != nil && loc[0] == 0 {
			matchBytes := src[pos : pos+loc[1]]
			m := actixScopeRe.FindSubmatch(matchBytes)
			prefix := string(m[1])
			// Count parens within the match to update parenDepth correctly
			// (web::scope("...") contains exactly one '(' and one ')' → net 0).
			delta := countParens(matchBytes)
			parenDepth += delta
			if parenDepth < 0 {
				parenDepth = 0
			}
			// The scope's body starts after this match; its depth is parenDepth
			// at this point (after processing the parens in the match itself).
			scopeStack = append(scopeStack, scopeEntry{prefix: prefix, depth: parenDepth})
			pos += loc[1]
			continue
		}

		// Try to match .route("path", METHOD().to(handler)) at this position.
		if loc := actixDirectRouteRe.FindIndex(src[pos:]); loc != nil && loc[0] == 0 {
			matchBytes := src[pos : pos+loc[1]]
			m := actixDirectRouteRe.FindSubmatch(matchBytes)
			relPath := string(m[1])
			method := strings.ToUpper(string(m[2]))
			handlerName := rustLastSegment(string(m[3]))

			scopePrefix := currentPrefix()
			fullPath := actixJoinPaths(scopePrefix, relPath)

			line := lineOf(stripped, pos)
			if line > totalLines {
				line = totalLines
			}

			emitRustRoute(filePath, line, method, fullPath, handlerName, claimed, nodes, refs)
			// Count parens in the match to keep depth in sync.
			delta := countParens(matchBytes)
			parenDepth += delta
			if parenDepth < 0 {
				parenDepth = 0
			}
			pos += loc[1]
			continue
		}

		pos++
	}
}

// ActixResolver implements FrameworkResolver + FrameworkExtractor for actix-web.
type ActixResolver struct {
	projectRoot string
	claimed     map[string]bool
}

// NewActixResolver creates an ActixResolver.
func NewActixResolver(projectRoot string) *ActixResolver {
	return &ActixResolver{projectRoot: projectRoot, claimed: make(map[string]bool)}
}

// Name returns "actix".
func (r *ActixResolver) Name() string { return "actix" }

// Languages returns [LanguageRust].
func (r *ActixResolver) Languages() []types.Language {
	return []types.Language{types.LanguageRust}
}

// Detect returns true when Cargo.toml lists actix-web.
func (r *ActixResolver) Detect(_ context.Context) bool {
	return cargoHasDep(r.projectRoot, "actix-web")
}

// Extract scans filePath/content for actix-web route registrations:
//  1. Attribute macros: #[get("/p")] above async fn name
//  2. Resource form: web::resource("/p").route(web::get().to(handler))
//  3. Direct-route form: App::new().route("/p", web::get().to(handler))
//
// Comments are stripped first so commented routes emit nothing.
func (r *ActixResolver) Extract(filePath, content string) ([]types.Node, []types.UnresolvedReference) {
	stripped := stripJSComments(content)
	totalLines := strings.Count(content, "\n") + 1

	var nodes []types.Node
	var refs []types.UnresolvedReference

	// 1. Attribute macro form: #[get("/path")] above fn
	for _, loc := range actixAttrRe.FindAllStringSubmatchIndex(stripped, -1) {
		if len(loc) < 6 {
			continue
		}
		method := strings.ToUpper(stripped[loc[2]:loc[3]])
		path := stripped[loc[4]:loc[5]]

		line := lineOf(stripped, loc[0])
		if line > totalLines {
			line = totalLines
		}

		// Bounded lookahead: find fn name after the attribute
		handlerName := rustHandlerName(stripped[loc[1]:])

		emitRustRoute(filePath, line, method, path, handlerName, r.claimed, &nodes, &refs)
	}

	// 2. Resource form: web::resource("/p").route(web::get().to(handler))
	// For each resource declaration, find all .route() chains that follow
	// on the same or subsequent lines (up to the next statement).
	for _, resourceLoc := range actixResourcePathRe.FindAllStringSubmatchIndex(stripped, -1) {
		if len(resourceLoc) < 4 {
			continue
		}
		path := stripped[resourceLoc[2]:resourceLoc[3]]
		resourceLine := lineOf(stripped, resourceLoc[0])
		if resourceLine > totalLines {
			resourceLine = totalLines
		}

		// Scan forward from the end of the resource(...) call to find .route() chains.
		// We look within a reasonable window (500 bytes) for the associated routes.
		end := resourceLoc[1]
		window := 500
		if end+window > len(stripped) {
			window = len(stripped) - end
		}
		segment := stripped[end : end+window]

		for _, routeMatch := range actixRouteMethodRe.FindAllStringSubmatch(segment, -1) {
			if len(routeMatch) < 3 {
				continue
			}
			method := strings.ToUpper(routeMatch[1])
			handlerName := rustLastSegment(routeMatch[2])

			emitRustRoute(filePath, resourceLine, method, path, handlerName, r.claimed, &nodes, &refs)
		}
	}

	// 3. Direct-route and scope-prefixed form.
	// actixExtractScopedRoutes walks the source tracking web::scope("PREFIX")
	// context via paren depth and prepends the accumulated prefix to .route()
	// paths. Non-scoped .route() calls use their literal path unchanged.
	actixExtractScopedRoutes(filePath, stripped, totalLines, r.claimed, &nodes, &refs)

	return nodes, refs
}

// rustLastSegment returns the final :: or . segment of a Rust qualified name.
// "handlers::list_users" → "list_users"; "list_users" → "list_users".
func rustLastSegment(s string) string {
	s = strings.TrimSpace(s)
	// Handle :: separator (Rust paths)
	if idx := strings.LastIndex(s, "::"); idx >= 0 {
		return s[idx+2:]
	}
	// Handle . separator
	if idx := strings.LastIndex(s, "."); idx >= 0 {
		return s[idx+1:]
	}
	return s
}

// ClaimsReference returns true if a handler with this name was seen in Extract.
func (r *ActixResolver) ClaimsReference(name string) bool { return r.claimed[name] }

// Resolve returns confidence 0.85 for claimed handlers.
func (r *ActixResolver) Resolve(ctx context.Context, ref types.UnresolvedReference) (resolution.ResolvedRef, error) {
	return rustResolve(r.claimed, ctx, ref)
}

// ---------------------------------------------------------------------------
// axum resolver
// ---------------------------------------------------------------------------

// axumRouteFullRe matches axum .route("/path", CHAIN) where CHAIN is a
// sequence of method(handler) calls optionally chained with dots.
//
//	.route("/users", get(list_users))
//	.route("/orders", get(list_orders).post(create_order))
//
// Capture groups: 1=path, 2=full method chain.
//
// The chain capture group matches one or more method(handler) calls joined
// by optional dots — it stops at the outer closing ')'.
var axumRouteFullRe = regexp.MustCompile(
	`(?m)\.route\s*\(\s*"([^"]+)"\s*,\s*((?:(?:get|post|put|delete|patch|head|options)\s*\([A-Za-z_][A-Za-z0-9_:]*\)\.?)+)\s*\)`,
)

// axumMethodCallRe matches a single method call in an axum chain: get(handler)
// Capture groups: 1=method, 2=handler identifier.
var axumMethodCallRe = regexp.MustCompile(
	`\b(get|post|put|delete|patch|head|options)\s*\(\s*([A-Za-z_][A-Za-z0-9_:]*)\s*\)`,
)

// AxumResolver implements FrameworkResolver + FrameworkExtractor for axum.
type AxumResolver struct {
	projectRoot string
	claimed     map[string]bool
}

// NewAxumResolver creates an AxumResolver.
func NewAxumResolver(projectRoot string) *AxumResolver {
	return &AxumResolver{projectRoot: projectRoot, claimed: make(map[string]bool)}
}

// Name returns "axum".
func (r *AxumResolver) Name() string { return "axum" }

// Languages returns [LanguageRust].
func (r *AxumResolver) Languages() []types.Language {
	return []types.Language{types.LanguageRust}
}

// Detect returns true when Cargo.toml lists axum.
func (r *AxumResolver) Detect(_ context.Context) bool {
	return cargoHasDep(r.projectRoot, "axum")
}

// Extract scans for axum Router::new().route("/path", get(handler)) chains.
// Method chains on a single path (get(h).post(h2)) fan out into one node per method.
func (r *AxumResolver) Extract(filePath, content string) ([]types.Node, []types.UnresolvedReference) {
	stripped := stripJSComments(content)
	totalLines := strings.Count(content, "\n") + 1

	var nodes []types.Node
	var refs []types.UnresolvedReference

	for _, loc := range axumRouteFullRe.FindAllStringSubmatchIndex(stripped, -1) {
		if len(loc) < 6 {
			continue
		}
		path := stripped[loc[2]:loc[3]]
		chain := stripped[loc[4]:loc[5]]

		line := lineOf(stripped, loc[0])
		if line > totalLines {
			line = totalLines
		}

		// Fan out: one node per method in the chain
		methodMatches := axumMethodCallRe.FindAllStringSubmatch(chain, -1)
		if len(methodMatches) == 0 {
			// No recognisable method — emit ANY with no handler
			emitRustRoute(filePath, line, "ANY", path, "", r.claimed, &nodes, &refs)
			continue
		}
		for _, m := range methodMatches {
			method := strings.ToUpper(m[1])
			handlerName := rustLastSegment(m[2])
			emitRustRoute(filePath, line, method, path, handlerName, r.claimed, &nodes, &refs)
		}
	}

	return nodes, refs
}

// ClaimsReference returns true if a handler with this name was seen in Extract.
func (r *AxumResolver) ClaimsReference(name string) bool { return r.claimed[name] }

// Resolve returns confidence 0.85 for claimed handlers.
func (r *AxumResolver) Resolve(ctx context.Context, ref types.UnresolvedReference) (resolution.ResolvedRef, error) {
	return rustResolve(r.claimed, ctx, ref)
}

// ---------------------------------------------------------------------------
// rocket resolver
// ---------------------------------------------------------------------------

// rocketAttrRe matches rocket HTTP attribute macros:
//
//	#[get("/path")]
//	#[post("/path", data = "<input>")]
//	#[put("/path")]   etc.
//
// Capture groups: 1=method (lowercase), 2=path.
// rocketAttrRe matches rocket HTTP attribute macros including the full )] close
// so that the match end points AFTER the attribute line and rustHandlerName
// receives the text starting on the NEXT line (the fn definition line).
//
//	#[get("/path")]
//	#[post("/path", data = "<input>")]
//
// Capture groups: 1=method (lowercase), 2=path.
var rocketAttrRe = regexp.MustCompile(
	`(?m)#\[(get|post|put|delete|patch|head|options)\s*\(\s*"([^"]+)"[^\]]*\]`,
)

// RocketResolver implements FrameworkResolver + FrameworkExtractor for rocket.
type RocketResolver struct {
	projectRoot string
	claimed     map[string]bool
}

// NewRocketResolver creates a RocketResolver.
func NewRocketResolver(projectRoot string) *RocketResolver {
	return &RocketResolver{projectRoot: projectRoot, claimed: make(map[string]bool)}
}

// Name returns "rocket".
func (r *RocketResolver) Name() string { return "rocket" }

// Languages returns [LanguageRust].
func (r *RocketResolver) Languages() []types.Language {
	return []types.Language{types.LanguageRust}
}

// Detect returns true when Cargo.toml lists rocket.
func (r *RocketResolver) Detect(_ context.Context) bool {
	return cargoHasDep(r.projectRoot, "rocket")
}

// Extract scans for rocket attribute macros above fn definitions.
// Uses bounded lookahead (rustHandlerName) to skip additional #[...] attribute
// lines between the route macro and the fn — same pattern as nestHandlerName.
func (r *RocketResolver) Extract(filePath, content string) ([]types.Node, []types.UnresolvedReference) {
	stripped := stripJSComments(content)
	totalLines := strings.Count(content, "\n") + 1

	var nodes []types.Node
	var refs []types.UnresolvedReference

	for _, loc := range rocketAttrRe.FindAllStringSubmatchIndex(stripped, -1) {
		if len(loc) < 6 {
			continue
		}
		method := strings.ToUpper(stripped[loc[2]:loc[3]])
		path := stripped[loc[4]:loc[5]]

		line := lineOf(stripped, loc[0])
		if line > totalLines {
			line = totalLines
		}

		// Bounded lookahead: find fn name after the attribute
		handlerName := rustHandlerName(stripped[loc[1]:])

		emitRustRoute(filePath, line, method, path, handlerName, r.claimed, &nodes, &refs)
	}

	return nodes, refs
}

// ClaimsReference returns true if a handler with this name was seen in Extract.
func (r *RocketResolver) ClaimsReference(name string) bool { return r.claimed[name] }

// Resolve returns confidence 0.85 for claimed handlers.
func (r *RocketResolver) Resolve(ctx context.Context, ref types.UnresolvedReference) (resolution.ResolvedRef, error) {
	return rustResolve(r.claimed, ctx, ref)
}
