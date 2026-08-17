// Rust web-framework resolvers: actix-web, axum, and rocket.
//
// Rust shares JS comment syntax, so stripJSComments serves here too — run
// before the route regexes, so a commented-out route emits no node.
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

// cargoHasDep substring-matches Cargo.toml rather than parsing it: a crate
// name is distinctive enough that a false positive is not a real risk.
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

// rustHandlerName finds the function a route attribute macro decorates,
// skipping blank lines and stacked attributes. It gives up at the first line
// that is neither, rather than scan on and misattribute a distant function.
func rustHandlerName(rest string) string {
	for _, line := range strings.Split(rest, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.HasPrefix(trimmed, "}") {
			return ""
		}
		if m := rustFnDefRe.FindStringSubmatch(line); m != nil {
			return m[1]
		}
		return ""
	}
	return ""
}

// rustFnDefRe captures the name from any of `fn n(`, `async fn n(`, `pub fn
// n(`, `pub async fn n(`.
var rustFnDefRe = regexp.MustCompile(
	`^\s*(?:pub\s+)?(?:async\s+)?fn\s+([A-Za-z_][A-Za-z0-9_]*)\s*[(<]`,
)

// rustResolve backs Resolve for all three Rust resolvers.
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

// emitRustRoute appends the route node and its handler ref, and records the
// handler as claimed.
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

// actixAttrRe matches `#[get("/path")]`. Groups: method, path.
var actixAttrRe = regexp.MustCompile(
	`(?m)#\[(get|post|put|delete|patch|head|options)\s*\(\s*"([^"]+)"\s*(?:,[^)]*)??\)\]`,
)

// `web::resource("/p").route(web::get().to(h))` needs two regexes: the path
// lives on the resource call, the method and handler on each chained .route.

var actixResourcePathRe = regexp.MustCompile(
	`(?m)web::resource\s*\(\s*"([^"]+)"\s*\)`,
)

// actixRouteMethodRe groups: method, handler.
var actixRouteMethodRe = regexp.MustCompile(
	`\.route\s*\(\s*web::([a-z]+)\s*\(\s*\)\s*\.to\s*\(\s*([A-Za-z_][A-Za-z0-9_:]*)\s*\)`,
)

// actixDirectRouteRe matches `.route("/p", web::get().to(h))`. Groups: path,
// method, handler. The web:: prefix is optional because the method functions
// are commonly imported directly; the path may be "" inside a scope; (?s)
// covers arguments split across lines.
var actixDirectRouteRe = regexp.MustCompile(
	`(?s)\.route\s*\(\s*"([^"]*)"\s*,\s*(?:web::)?(get|post|put|delete|patch|head|options)\s*\(\s*\)\s*\.to\s*\(\s*([A-Za-z_][A-Za-z0-9_:]*)\s*\)`,
)

// actixScopeRe captures the prefix from `web::scope("/api")`.
var actixScopeRe = regexp.MustCompile(
	`web::scope\s*\(\s*"([^"]*)"\s*\)`,
)

func actixJoinPaths(prefix, rel string) string {
	if rel == "" {
		return prefix
	}
	// rel always leads with "/" in actix .route() calls, so plain concatenation
	// cannot produce a double slash.
	return prefix + rel
}

// actixExtractScopedRoutes prepends the enclosing web::scope prefix to each
// route path, which requires tracking paren depth: a scope's reach ends where
// its call closes, and only depth says where that is.
//
// A scope whose prefix is a variable rather than a literal is treated as
// unscoped. Routes inside web::resource are left to the resource pass.
func actixExtractScopedRoutes(
	filePath, stripped string,
	totalLines int,
	claimed map[string]bool,
	nodes *[]types.Node,
	refs *[]types.UnresolvedReference,
) {
	type scopeEntry struct {
		prefix string
		depth  int
	}

	var scopeStack []scopeEntry
	parenDepth := 0
	pos := 0
	src := []byte(stripped)
	n := len(src)

	// Nested scopes concatenate, so the prefix is the whole stack.
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
			for len(scopeStack) > 0 && scopeStack[len(scopeStack)-1].depth > parenDepth {
				scopeStack = scopeStack[:len(scopeStack)-1]
			}
			pos++
			continue
		}

		if loc := actixScopeRe.FindIndex(src[pos:]); loc != nil && loc[0] == 0 {
			matchBytes := src[pos : pos+loc[1]]
			m := actixScopeRe.FindSubmatch(matchBytes)
			prefix := string(m[1])
			// The match is consumed whole, so its own parens must be accounted
			// for here or the depth counter drifts.
			delta := countParens(matchBytes)
			parenDepth += delta
			if parenDepth < 0 {
				parenDepth = 0
			}
			scopeStack = append(scopeStack, scopeEntry{prefix: prefix, depth: parenDepth})
			pos += loc[1]
			continue
		}

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

// ActixResolver handles actix-web.
type ActixResolver struct {
	projectRoot string
	claimed     map[string]bool
}

func NewActixResolver(projectRoot string) *ActixResolver {
	return &ActixResolver{projectRoot: projectRoot, claimed: make(map[string]bool)}
}

func (r *ActixResolver) Name() string { return "actix" }

func (r *ActixResolver) Languages() []types.Language {
	return []types.Language{types.LanguageRust}
}

// Detect returns true when Cargo.toml lists actix-web.
func (r *ActixResolver) Detect(_ context.Context) bool {
	return cargoHasDep(r.projectRoot, "actix-web")
}

// Extract covers actix-web's three registration forms: attribute macro,
// web::resource chain, and direct .route call.
func (r *ActixResolver) Extract(filePath, content string) ([]types.Node, []types.UnresolvedReference) {
	stripped := stripJSComments(content)
	totalLines := strings.Count(content, "\n") + 1

	var nodes []types.Node
	var refs []types.UnresolvedReference

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

		handlerName := rustHandlerName(stripped[loc[1]:])

		emitRustRoute(filePath, line, method, path, handlerName, r.claimed, &nodes, &refs)
	}

	for _, resourceLoc := range actixResourcePathRe.FindAllStringSubmatchIndex(stripped, -1) {
		if len(resourceLoc) < 4 {
			continue
		}
		path := stripped[resourceLoc[2]:resourceLoc[3]]
		resourceLine := lineOf(stripped, resourceLoc[0])
		if resourceLine > totalLines {
			resourceLine = totalLines
		}

		// A fixed window stands in for the chain's real extent: enough for a
		// realistic .route chain, short enough not to swallow the next
		// statement's routes.
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

	actixExtractScopedRoutes(filePath, stripped, totalLines, r.claimed, &nodes, &refs)

	return nodes, refs
}

// rustLastSegment reduces "handlers::list_users" to "list_users", the form the
// name matcher looks up.
func rustLastSegment(s string) string {
	s = strings.TrimSpace(s)
	if idx := strings.LastIndex(s, "::"); idx >= 0 {
		return s[idx+2:]
	}
	if idx := strings.LastIndex(s, "."); idx >= 0 {
		return s[idx+1:]
	}
	return s
}

func (r *ActixResolver) ClaimsReference(name string) bool { return r.claimed[name] }

func (r *ActixResolver) Resolve(ctx context.Context, ref types.UnresolvedReference) (resolution.ResolvedRef, error) {
	return rustResolve(r.claimed, ctx, ref)
}

// axumRouteFullRe matches `.route("/p", get(h).post(h2))`. Groups: path, and
// the whole method chain, which axumMethodCallRe then splits.
var axumRouteFullRe = regexp.MustCompile(
	`(?m)\.route\s*\(\s*"([^"]+)"\s*,\s*((?:(?:get|post|put|delete|patch|head|options)\s*\([A-Za-z_][A-Za-z0-9_:]*\)\.?)+)\s*\)`,
)

// axumMethodCallRe groups: method, handler.
var axumMethodCallRe = regexp.MustCompile(
	`\b(get|post|put|delete|patch|head|options)\s*\(\s*([A-Za-z_][A-Za-z0-9_:]*)\s*\)`,
)

// AxumResolver handles axum.
type AxumResolver struct {
	projectRoot string
	claimed     map[string]bool
}

func NewAxumResolver(projectRoot string) *AxumResolver {
	return &AxumResolver{projectRoot: projectRoot, claimed: make(map[string]bool)}
}

func (r *AxumResolver) Name() string { return "axum" }

func (r *AxumResolver) Languages() []types.Language {
	return []types.Language{types.LanguageRust}
}

// Detect returns true when Cargo.toml lists axum.
func (r *AxumResolver) Detect(_ context.Context) bool {
	return cargoHasDep(r.projectRoot, "axum")
}

// Extract fans a chained path out into one route node per method.
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

		methodMatches := axumMethodCallRe.FindAllStringSubmatch(chain, -1)
		if len(methodMatches) == 0 {
			// The route exists even when its method is unrecognizable.
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

func (r *AxumResolver) ClaimsReference(name string) bool { return r.claimed[name] }

func (r *AxumResolver) Resolve(ctx context.Context, ref types.UnresolvedReference) (resolution.ResolvedRef, error) {
	return rustResolve(r.claimed, ctx, ref)
}

// rocketAttrRe matches `#[get("/p")]` and `#[post("/p", data = "<in>")]`.
// Groups: method, path. The match deliberately spans the closing `]` so
// rustHandlerName starts scanning at the following line.
var rocketAttrRe = regexp.MustCompile(
	`(?m)#\[(get|post|put|delete|patch|head|options)\s*\(\s*"([^"]+)"[^\]]*\]`,
)

// RocketResolver handles rocket.
type RocketResolver struct {
	projectRoot string
	claimed     map[string]bool
}

func NewRocketResolver(projectRoot string) *RocketResolver {
	return &RocketResolver{projectRoot: projectRoot, claimed: make(map[string]bool)}
}

func (r *RocketResolver) Name() string { return "rocket" }

func (r *RocketResolver) Languages() []types.Language {
	return []types.Language{types.LanguageRust}
}

// Detect returns true when Cargo.toml lists rocket.
func (r *RocketResolver) Detect(_ context.Context) bool {
	return cargoHasDep(r.projectRoot, "rocket")
}

// Extract reads rocket's attribute macros and the functions they decorate.
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

		handlerName := rustHandlerName(stripped[loc[1]:])

		emitRustRoute(filePath, line, method, path, handlerName, r.claimed, &nodes, &refs)
	}

	return nodes, refs
}

func (r *RocketResolver) ClaimsReference(name string) bool { return r.claimed[name] }

func (r *RocketResolver) Resolve(ctx context.Context, ref types.UnresolvedReference) (resolution.ResolvedRef, error) {
	return rustResolve(r.claimed, ctx, ref)
}
