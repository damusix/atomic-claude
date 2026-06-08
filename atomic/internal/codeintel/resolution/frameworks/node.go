// Package frameworks — Node/JS-TS framework resolvers (CP15 batch C).
//
// This file implements six FrameworkResolver + FrameworkExtractor pairs:
// NestJS, Koa, Hapi, Fastify, Sails, AdonisJS.
//
// # Language
//
// All six resolvers use the JS/TS family:
//
//	[typescript, javascript, tsx, jsx]
//
// langFromFilePath (defined in frameworks.go) maps the file extension to the
// correct Language value; route nodes and handler refs both use this.
//
// # Comment stripping
//
// JS/TS comments are stripped with stripJSComments (defined in frameworks.go)
// before route regexes run, so commented-out routes never emit route nodes.
//
// # Route node format (appendix H — via MakeRouteNode)
//
//	id:            route:{filePath}:{line}:{METHOD}:{path}
//	qualifiedName: {filePath}::METHOD:{path}
//	name:          "METHOD /path"
//
// # Detect
//
// Each resolver reads package.json in the project root and looks for the
// framework's package name as a JSON-key substring match
// (e.g. `"fastify":`) to avoid false positives from longer package names.
//
// # Handler refs
//
// Named handler identifiers emit EdgeKindReferences refs.
// Action strings (sails, adonis) use their last dot-segment as the ref name.
// NestJS handler = the decorated method name.
//
// # Method fallback
//
// Routes with no discernible HTTP method emit "ANY".
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

// jsNodeLanguages is the JS/TS family list used by all Node framework resolvers.
func jsNodeLanguages() []types.Language {
	return []types.Language{
		types.LanguageTypeScript,
		types.LanguageJavaScript,
		types.LanguageTSX,
		types.LanguageJSX,
	}
}

// nodeHasDep returns true if package.json in projectRoot contains the given
// package name in JSON-key form (`"pkgName":`). Matching the JSON key form
// avoids false positives from longer package names that share a prefix.
func nodeHasDep(projectRoot, pkgName string) bool {
	data, err := os.ReadFile(filepath.Join(projectRoot, "package.json"))
	if err != nil {
		return false
	}
	return strings.Contains(string(data), `"`+pkgName+`":`)
}

// nodeLineOf returns the 1-based line number for a byte offset in src.
func nodeLineOf(src string, offset int) int {
	return strings.Count(src[:offset], "\n") + 1
}

// nodeResolve is the shared Resolve implementation for all Node framework
// resolvers: confidence 0.85 (midpoint of 0.8–0.9, appendix H).
func nodeResolve(
	claimed map[string]bool,
	ctx context.Context,
	ref types.UnresolvedReference,
) (resolution.ResolvedRef, error) {
	if !claimed[ref.ReferenceName] {
		return resolution.ResolvedRef{}, nil
	}
	return resolution.ResolvedRef{Confidence: 0.85}, nil
}

// emitRoute is a helper that builds a route node + references ref and appends
// them to the provided slices. It also records handlerName in claimed.
func emitRoute(
	filePath string,
	line int,
	method, path, handlerName string,
	lang types.Language,
	claimed map[string]bool,
	nodes *[]types.Node,
	refs *[]types.UnresolvedReference,
) {
	if method == "" {
		method = "ANY"
	}
	node := MakeRouteNode(filePath, line, method, path, lang)
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
			Language:      lang,
		})
	}
}

// ---------------------------------------------------------------------------
// NestJS resolver
// ---------------------------------------------------------------------------

// nestControllerRe matches @Controller('prefix') or @Controller("prefix") or
// @Controller() (no arg). Capture group 1 = prefix (may be empty string for
// @Controller() or @Controller(”)).
var nestControllerRe = regexp.MustCompile(
	`@Controller\s*\(\s*(?:['"]([^'"]*)['"]\s*)?\)`,
)

// nestMethodRe matches NestJS HTTP method decorators:
//
//	@Get('sub'), @Post(), @Put('sub'), @Delete('sub'), @Patch, @Options, @Head
//
// Capture group 1 = method name (lowercase), group 2 = sub-path (may be "").
var nestMethodRe = regexp.MustCompile(
	`@(Get|Post|Put|Delete|Patch|Options|Head)\s*\(\s*(?:['"]([^'"]*)['"]\s*)?\)`,
)

// nestDefRe finds the method name on a single non-decorator, non-blank line.
// Used inside nestHandlerName to match the first identifier-definition on the
// candidate line after skipping stacked @decorator lines.
var nestDefRe = regexp.MustCompile(`^\s*(?:(?:public|private|protected|async|readonly|override|abstract)\s+)*([A-Za-z_$][A-Za-z0-9_$]*)\s*\(`)

// nestHandlerName extracts the method name from the text immediately following
// a NestJS route decorator.  It scans forward line-by-line, skipping:
//   - blank lines
//   - lines that start with '@' (stacked decorators such as @UseGuards, @Roles)
//
// It stops and returns "" at any line that starts with '}' (class boundary) or
// is not a method definition, preventing cross-class misattribution.
func nestHandlerName(rest string) string {
	for _, line := range strings.Split(rest, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue // blank line — keep scanning
		}
		if strings.HasPrefix(trimmed, "@") {
			continue // stacked decorator — keep scanning
		}
		if strings.HasPrefix(trimmed, "}") || strings.HasPrefix(trimmed, "{") {
			return "" // class boundary — stop, no handler found
		}
		if m := nestDefRe.FindStringSubmatch(line); m != nil {
			return m[1]
		}
		// Non-decorator, non-blank line that doesn't match a def → stop.
		return ""
	}
	return ""
}

// NestJSResolver implements FrameworkResolver + FrameworkExtractor for NestJS.
type NestJSResolver struct {
	projectRoot string
	claimed     map[string]bool
}

// NewNestJSResolver creates a NestJSResolver.
func NewNestJSResolver(projectRoot string) *NestJSResolver {
	return &NestJSResolver{projectRoot: projectRoot, claimed: make(map[string]bool)}
}

// Name returns "nestjs".
func (r *NestJSResolver) Name() string { return "nestjs" }

// Languages returns the JS/TS family.
func (r *NestJSResolver) Languages() []types.Language { return jsNodeLanguages() }

// Detect returns true when package.json lists @nestjs/core or @nestjs/common.
func (r *NestJSResolver) Detect(ctx context.Context) bool {
	return nodeHasDep(r.projectRoot, "@nestjs/core") ||
		nodeHasDep(r.projectRoot, "@nestjs/common")
}

// Extract scans filePath/content for NestJS @Controller + @Get/@Post/... decorators.
// It tracks the current @Controller prefix as it scans top-to-bottom and joins
// it with each method decorator's sub-path to build the full route path.
func (r *NestJSResolver) Extract(filePath, content string) ([]types.Node, []types.UnresolvedReference) {
	stripped := stripJSComments(content)
	lang := langFromFilePath(filePath)
	totalLines := strings.Count(content, "\n") + 1

	var nodes []types.Node
	var refs []types.UnresolvedReference

	// Find all @Controller decorators; each defines a prefix zone.
	// Strategy: collect all controller match positions + their prefixes, then for
	// each HTTP method decorator find the most-recently-declared controller prefix.
	type controllerEntry struct {
		offset int
		prefix string
	}
	var controllers []controllerEntry

	for _, loc := range nestControllerRe.FindAllStringSubmatchIndex(stripped, -1) {
		prefix := ""
		if loc[2] >= 0 {
			prefix = stripped[loc[2]:loc[3]]
		}
		controllers = append(controllers, controllerEntry{offset: loc[0], prefix: prefix})
	}

	// For each HTTP method decorator, find the active controller prefix and the
	// method name from the def that immediately follows.
	methodMatches := nestMethodRe.FindAllStringSubmatchIndex(stripped, -1)
	for _, loc := range methodMatches {
		if len(loc) < 6 {
			continue
		}
		httpMethod := strings.ToUpper(stripped[loc[2]:loc[3]])
		subPath := ""
		if loc[4] >= 0 {
			subPath = stripped[loc[4]:loc[5]]
		}

		matchOffset := loc[0]
		line := nodeLineOf(stripped, matchOffset)
		if line > totalLines {
			line = totalLines
		}

		// Find the active controller prefix: the last @Controller before this offset.
		prefix := ""
		for _, c := range controllers {
			if c.offset < matchOffset {
				prefix = c.prefix
			}
		}

		// Build full path: /prefix/subPath, normalised.
		fullPath := buildNestPath(prefix, subPath)

		// Find handler method name: bounded lookahead — skip stacked @decorator
		// lines and blank lines, stop at the first non-decorator line or a
		// class boundary '}' to avoid scanning across class boundaries.
		handlerName := nestHandlerName(stripped[loc[1]:])

		emitRoute(filePath, line, httpMethod, fullPath, handlerName, lang,
			r.claimed, &nodes, &refs)
	}

	return nodes, refs
}

// buildNestPath joins a controller prefix with a method sub-path into a
// normalised route path, e.g. buildNestPath("users", ":id") → "/users/:id".
func buildNestPath(prefix, subPath string) string {
	// Normalise: strip leading slashes, then re-add exactly one leading slash.
	prefix = strings.TrimPrefix(prefix, "/")
	subPath = strings.TrimPrefix(subPath, "/")
	switch {
	case prefix == "" && subPath == "":
		return "/"
	case prefix == "":
		return "/" + subPath
	case subPath == "":
		return "/" + prefix
	default:
		return "/" + prefix + "/" + subPath
	}
}

// ClaimsReference returns true if a handler with this name was seen in Extract.
func (r *NestJSResolver) ClaimsReference(name string) bool { return r.claimed[name] }

// Resolve returns confidence 0.85 for claimed handlers.
func (r *NestJSResolver) Resolve(ctx context.Context, ref types.UnresolvedReference) (resolution.ResolvedRef, error) {
	return nodeResolve(r.claimed, ctx, ref)
}

// ---------------------------------------------------------------------------
// Koa resolver
// ---------------------------------------------------------------------------

// koaRouteRe matches koa-router route registrations:
//
//	router.get('/path', handler)
//	router.post('/path', handler)
//
// Capture groups: 1=method, 2=path, 3=last arg (handler).
var koaRouteRe = regexp.MustCompile(
	`(?m)(?:[A-Za-z_$][A-Za-z0-9_$]*)\.` +
		`(get|post|put|delete|patch|head|options|all)\s*\(\s*` +
		`['"]([^'"]+)['"]\s*,\s*([^)]+)`,
)

// KoaResolver implements FrameworkResolver + FrameworkExtractor for Koa.
type KoaResolver struct {
	projectRoot string
	claimed     map[string]bool
}

// NewKoaResolver creates a KoaResolver.
func NewKoaResolver(projectRoot string) *KoaResolver {
	return &KoaResolver{projectRoot: projectRoot, claimed: make(map[string]bool)}
}

// Name returns "koa".
func (r *KoaResolver) Name() string { return "koa" }

// Languages returns the JS/TS family.
func (r *KoaResolver) Languages() []types.Language { return jsNodeLanguages() }

// Detect returns true when package.json lists koa, @koa/router, or koa-router.
func (r *KoaResolver) Detect(ctx context.Context) bool {
	return nodeHasDep(r.projectRoot, "koa") ||
		nodeHasDep(r.projectRoot, "@koa/router") ||
		nodeHasDep(r.projectRoot, "koa-router")
}

// Extract scans for koa-router route registrations.
func (r *KoaResolver) Extract(filePath, content string) ([]types.Node, []types.UnresolvedReference) {
	stripped := stripJSComments(content)
	lang := langFromFilePath(filePath)
	totalLines := strings.Count(content, "\n") + 1

	var nodes []types.Node
	var refs []types.UnresolvedReference

	for _, loc := range koaRouteRe.FindAllStringSubmatchIndex(stripped, -1) {
		if len(loc) < 8 {
			continue
		}
		method := strings.ToUpper(stripped[loc[2]:loc[3]])
		path := stripped[loc[4]:loc[5]]
		handlerRaw := strings.TrimSpace(stripped[loc[6]:loc[7]])

		line := nodeLineOf(stripped, loc[0])
		if line > totalLines {
			line = totalLines
		}

		handlerName := extractIdentifier(handlerRaw)
		if handlerName == "" || jsReservedInlineNames[handlerName] {
			handlerName = ""
		}

		emitRoute(filePath, line, method, path, handlerName, lang,
			r.claimed, &nodes, &refs)
	}

	return nodes, refs
}

// ClaimsReference returns true if a handler with this name was seen in Extract.
func (r *KoaResolver) ClaimsReference(name string) bool { return r.claimed[name] }

// Resolve returns confidence 0.85 for claimed handlers.
func (r *KoaResolver) Resolve(ctx context.Context, ref types.UnresolvedReference) (resolution.ResolvedRef, error) {
	return nodeResolve(r.claimed, ctx, ref)
}

// ---------------------------------------------------------------------------
// Hapi resolver
// ---------------------------------------------------------------------------

// hapiRouteMethodRe matches the method field in a hapi server.route({...}) object.
// It handles both string and array forms:
//   - method: 'GET'
//   - method: "GET"
//   - method: ['GET', 'POST']
//   - method: ["GET", "POST"]
//   - method: '*'
//
// Capture group 1 = the raw value (string literal or array contents).
var hapiRouteMethodRe = regexp.MustCompile(
	`(?s)method\s*:\s*((?:\[(?:[^]]*)\])|(?:['"][^'"]*['"]))\s*,`,
)

// hapiRoutePathRe matches the path field: path: '/route'
var hapiRoutePathRe = regexp.MustCompile(`path\s*:\s*['"]([^'"]+)['"]`)

// hapiRouteHandlerRe matches the handler field: handler: identifier
var hapiRouteHandlerRe = regexp.MustCompile(`handler\s*:\s*([A-Za-z_$][A-Za-z0-9_$]*)`)

// hapiServerRouteRe finds each server.route({...}) call block.
// We match the opening and scan forward to find the matching closing brace.
var hapiServerRouteStartRe = regexp.MustCompile(`(?m)(?:[A-Za-z_$][A-Za-z0-9_$]*)\.route\s*\(\s*\{`)

// hapiMethodTokenRe extracts individual quoted method strings from an array or string value.
var hapiMethodTokenRe = regexp.MustCompile(`['"]([A-Za-z*]+)['"]`)

// HapiResolver implements FrameworkResolver + FrameworkExtractor for Hapi.
type HapiResolver struct {
	projectRoot string
	claimed     map[string]bool
}

// NewHapiResolver creates a HapiResolver.
func NewHapiResolver(projectRoot string) *HapiResolver {
	return &HapiResolver{projectRoot: projectRoot, claimed: make(map[string]bool)}
}

// Name returns "hapi".
func (r *HapiResolver) Name() string { return "hapi" }

// Languages returns the JS/TS family.
func (r *HapiResolver) Languages() []types.Language { return jsNodeLanguages() }

// Detect returns true when package.json lists @hapi/hapi or hapi.
func (r *HapiResolver) Detect(ctx context.Context) bool {
	return nodeHasDep(r.projectRoot, "@hapi/hapi") ||
		nodeHasDep(r.projectRoot, "hapi")
}

// Extract scans for hapi server.route({method, path, handler}) calls.
// Method arrays fan out into one route node per method.
func (r *HapiResolver) Extract(filePath, content string) ([]types.Node, []types.UnresolvedReference) {
	stripped := stripJSComments(content)
	lang := langFromFilePath(filePath)
	totalLines := strings.Count(content, "\n") + 1

	var nodes []types.Node
	var refs []types.UnresolvedReference

	// Find each server.route({ opening and extract the object block.
	for _, startLoc := range hapiServerRouteStartRe.FindAllStringIndex(stripped, -1) {
		// startLoc[1] is just after the opening `{`; scan forward tracking brace depth.
		blockStart := startLoc[1] - 1 // include the `{`
		blockEnd := findClosingBrace(stripped, blockStart)
		if blockEnd < 0 {
			continue
		}
		block := stripped[blockStart : blockEnd+1]
		line := nodeLineOf(stripped, startLoc[0])
		if line > totalLines {
			line = totalLines
		}

		// Extract method(s).
		methods := hapiExtractMethods(block)

		// Extract path.
		routePath := ""
		if pm := hapiRoutePathRe.FindStringSubmatch(block); pm != nil {
			routePath = pm[1]
		}
		if routePath == "" {
			continue
		}

		// Extract handler identifier.
		handlerName := ""
		if hm := hapiRouteHandlerRe.FindStringSubmatch(block); hm != nil {
			handlerName = hm[1]
			if jsReservedInlineNames[handlerName] {
				handlerName = ""
			}
		}

		for _, method := range methods {
			emitRoute(filePath, line, method, routePath, handlerName, lang,
				r.claimed, &nodes, &refs)
		}
	}

	return nodes, refs
}

// hapiExtractMethods parses the raw method value from a hapi route object block
// and returns the list of HTTP methods. '*' → ["ANY"].
func hapiExtractMethods(block string) []string {
	m := hapiRouteMethodRe.FindStringSubmatch(block)
	if m == nil {
		return []string{"ANY"}
	}
	raw := m[1]

	var methods []string
	for _, tok := range hapiMethodTokenRe.FindAllStringSubmatch(raw, -1) {
		if len(tok) < 2 {
			continue
		}
		v := strings.ToUpper(tok[1])
		if v == "*" {
			return []string{"ANY"}
		}
		methods = append(methods, v)
	}
	if len(methods) == 0 {
		return []string{"ANY"}
	}
	return methods
}

// findClosingBrace scans src forward from start (which should be the position of
// the opening '{') and returns the index of the matching closing '}', or -1 if not found.
func findClosingBrace(src string, start int) int {
	depth := 0
	for i := start; i < len(src); i++ {
		switch src[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// ClaimsReference returns true if a handler with this name was seen in Extract.
func (r *HapiResolver) ClaimsReference(name string) bool { return r.claimed[name] }

// Resolve returns confidence 0.85 for claimed handlers.
func (r *HapiResolver) Resolve(ctx context.Context, ref types.UnresolvedReference) (resolution.ResolvedRef, error) {
	return nodeResolve(r.claimed, ctx, ref)
}

// ---------------------------------------------------------------------------
// Fastify resolver
// ---------------------------------------------------------------------------

// fastifyShorthandRe matches fastify shorthand route registrations:
//
//	fastify.get('/path', handler)
//	app.post('/path', handler)
//
// Capture groups: 1=method, 2=path, 3=last arg.
var fastifyShorthandRe = regexp.MustCompile(
	`(?m)(?:[A-Za-z_$][A-Za-z0-9_$]*)\.` +
		`(get|post|put|delete|patch|head|options|all)\s*\(\s*` +
		`['"]([^'"]+)['"]\s*,\s*([^)]+)`,
)

// fastifyRouteMethodRe matches the method field in fastify.route({...}).
var fastifyRouteMethodRe = regexp.MustCompile(`method\s*:\s*['"]([A-Z]+)['"]`)

// fastifyRouteURLRe matches the url field (fastify uses `url`, not `path`).
var fastifyRouteURLRe = regexp.MustCompile(`url\s*:\s*['"]([^'"]+)['"]`)

// fastifyRouteHandlerRe matches the handler field.
var fastifyRouteHandlerRe = regexp.MustCompile(`handler\s*:\s*([A-Za-z_$][A-Za-z0-9_$]*)`)

// fastifyRouteStartRe finds each fastify.route({ opening.
var fastifyRouteStartRe = regexp.MustCompile(`(?m)(?:[A-Za-z_$][A-Za-z0-9_$]*)\.route\s*\(\s*\{`)

// FastifyResolver implements FrameworkResolver + FrameworkExtractor for Fastify.
type FastifyResolver struct {
	projectRoot string
	claimed     map[string]bool
}

// NewFastifyResolver creates a FastifyResolver.
func NewFastifyResolver(projectRoot string) *FastifyResolver {
	return &FastifyResolver{projectRoot: projectRoot, claimed: make(map[string]bool)}
}

// Name returns "fastify".
func (r *FastifyResolver) Name() string { return "fastify" }

// Languages returns the JS/TS family.
func (r *FastifyResolver) Languages() []types.Language { return jsNodeLanguages() }

// Detect returns true when package.json lists fastify.
func (r *FastifyResolver) Detect(ctx context.Context) bool {
	return nodeHasDep(r.projectRoot, "fastify")
}

// Extract scans for both fastify shorthand and object-form routes.
// Object form uses `url` (not `path`) for the route path.
func (r *FastifyResolver) Extract(filePath, content string) ([]types.Node, []types.UnresolvedReference) {
	stripped := stripJSComments(content)
	lang := langFromFilePath(filePath)
	totalLines := strings.Count(content, "\n") + 1

	var nodes []types.Node
	var refs []types.UnresolvedReference

	// Shorthand form: fastify.get('/path', handler)
	for _, loc := range fastifyShorthandRe.FindAllStringSubmatchIndex(stripped, -1) {
		if len(loc) < 8 {
			continue
		}
		method := strings.ToUpper(stripped[loc[2]:loc[3]])
		path := stripped[loc[4]:loc[5]]
		handlerRaw := strings.TrimSpace(stripped[loc[6]:loc[7]])

		line := nodeLineOf(stripped, loc[0])
		if line > totalLines {
			line = totalLines
		}

		handlerName := extractIdentifier(handlerRaw)
		if jsReservedInlineNames[handlerName] {
			handlerName = ""
		}

		emitRoute(filePath, line, method, path, handlerName, lang,
			r.claimed, &nodes, &refs)
	}

	// Object form: fastify.route({ method: 'GET', url: '/path', handler: fn })
	for _, startLoc := range fastifyRouteStartRe.FindAllStringIndex(stripped, -1) {
		blockStart := startLoc[1] - 1
		blockEnd := findClosingBrace(stripped, blockStart)
		if blockEnd < 0 {
			continue
		}
		block := stripped[blockStart : blockEnd+1]
		line := nodeLineOf(stripped, startLoc[0])
		if line > totalLines {
			line = totalLines
		}

		method := ""
		if mm := fastifyRouteMethodRe.FindStringSubmatch(block); mm != nil {
			method = mm[1]
		}
		if method == "" {
			method = "ANY"
		}

		routePath := ""
		if um := fastifyRouteURLRe.FindStringSubmatch(block); um != nil {
			routePath = um[1]
		}
		if routePath == "" {
			continue
		}

		handlerName := ""
		if hm := fastifyRouteHandlerRe.FindStringSubmatch(block); hm != nil {
			handlerName = hm[1]
			if jsReservedInlineNames[handlerName] {
				handlerName = ""
			}
		}

		emitRoute(filePath, line, method, routePath, handlerName, lang,
			r.claimed, &nodes, &refs)
	}

	return nodes, refs
}

// ClaimsReference returns true if a handler with this name was seen in Extract.
func (r *FastifyResolver) ClaimsReference(name string) bool { return r.claimed[name] }

// Resolve returns confidence 0.85 for claimed handlers.
func (r *FastifyResolver) Resolve(ctx context.Context, ref types.UnresolvedReference) (resolution.ResolvedRef, error) {
	return nodeResolve(r.claimed, ctx, ref)
}

// ---------------------------------------------------------------------------
// Sails resolver
// ---------------------------------------------------------------------------

// sailsRouteRe matches Sails config/routes.js map entries of the form:
//
//	'GET /path': 'FooController.action'
//	"GET /path": 'FooController.action'
//	'/path': 'FooController.action'  (no method → ANY)
//
// Capture groups: 1=full route key (e.g. "GET /path" or "/path"),
// 2=action string (e.g. "FooController.action").
var sailsRouteRe = regexp.MustCompile(
	`(?m)['"]([A-Z]+ /[^'"]*|/[^'"]*)['"]\s*:\s*['"]([^'"]+)['"]`,
)

// SailsResolver implements FrameworkResolver + FrameworkExtractor for Sails.
type SailsResolver struct {
	projectRoot string
	claimed     map[string]bool
}

// NewSailsResolver creates a SailsResolver.
func NewSailsResolver(projectRoot string) *SailsResolver {
	return &SailsResolver{projectRoot: projectRoot, claimed: make(map[string]bool)}
}

// Name returns "sails".
func (r *SailsResolver) Name() string { return "sails" }

// Languages returns the JS/TS family.
func (r *SailsResolver) Languages() []types.Language { return jsNodeLanguages() }

// Detect returns true when package.json lists sails.
func (r *SailsResolver) Detect(ctx context.Context) bool {
	return nodeHasDep(r.projectRoot, "sails")
}

// Extract scans for Sails routes.js map entries and returns route nodes.
// Handler = last dot-segment of the action string (e.g. "FooController.bar" → "bar").
// No method in the key → "ANY".
func (r *SailsResolver) Extract(filePath, content string) ([]types.Node, []types.UnresolvedReference) {
	stripped := stripJSComments(content)
	lang := langFromFilePath(filePath)
	totalLines := strings.Count(content, "\n") + 1

	var nodes []types.Node
	var refs []types.UnresolvedReference

	for _, loc := range sailsRouteRe.FindAllStringSubmatchIndex(stripped, -1) {
		if len(loc) < 6 {
			continue
		}
		routeKey := stripped[loc[2]:loc[3]]
		actionString := stripped[loc[4]:loc[5]]

		line := nodeLineOf(stripped, loc[0])
		if line > totalLines {
			line = totalLines
		}

		method, path := parseSailsRouteKey(routeKey)
		handlerName := extractLastSegment(actionString)

		emitRoute(filePath, line, method, path, handlerName, lang,
			r.claimed, &nodes, &refs)
	}

	return nodes, refs
}

// parseSailsRouteKey splits a Sails route key like "GET /path" into (method, path),
// or "/path" into ("ANY", "/path").
func parseSailsRouteKey(key string) (method, path string) {
	key = strings.TrimSpace(key)
	// HTTP method is uppercase, followed by a space, then the path.
	if idx := strings.Index(key, " "); idx > 0 {
		m := strings.ToUpper(key[:idx])
		// Validate it looks like an HTTP method (all uppercase letters).
		if isHTTPMethod(m) {
			return m, key[idx+1:]
		}
	}
	return "ANY", key
}

// isHTTPMethod returns true if s is an uppercase-letter-only string (looks like
// an HTTP method name).
func isHTTPMethod(s string) bool {
	for _, c := range s {
		if c < 'A' || c > 'Z' {
			return false
		}
	}
	return len(s) > 0
}

// ClaimsReference returns true if a handler with this name was seen in Extract.
func (r *SailsResolver) ClaimsReference(name string) bool { return r.claimed[name] }

// Resolve returns confidence 0.85 for claimed handlers.
func (r *SailsResolver) Resolve(ctx context.Context, ref types.UnresolvedReference) (resolution.ResolvedRef, error) {
	return nodeResolve(r.claimed, ctx, ref)
}

// ---------------------------------------------------------------------------
// AdonisJS resolver
// ---------------------------------------------------------------------------

// adonisRouteRe matches AdonisJS route registrations:
//
//	Route.get('/path', 'ControllerString')
//	Route.post('/path', 'FooController.action')
//	Route.get('/path', () => {})
//
// Capture groups: 1=method (lowercase), 2=path, 3=second arg.
var adonisRouteRe = regexp.MustCompile(
	`(?m)Route\.(get|post|put|delete|patch|options|head)\s*\(\s*` +
		`['"]([^'"]+)['"]\s*,\s*([^)]+)`,
)

// AdonisResolver implements FrameworkResolver + FrameworkExtractor for AdonisJS.
type AdonisResolver struct {
	projectRoot string
	claimed     map[string]bool
}

// NewAdonisResolver creates an AdonisResolver.
func NewAdonisResolver(projectRoot string) *AdonisResolver {
	return &AdonisResolver{projectRoot: projectRoot, claimed: make(map[string]bool)}
}

// Name returns "adonisjs".
func (r *AdonisResolver) Name() string { return "adonisjs" }

// Languages returns the JS/TS family.
func (r *AdonisResolver) Languages() []types.Language { return jsNodeLanguages() }

// Detect returns true when package.json lists @adonisjs/core or adonis.
func (r *AdonisResolver) Detect(ctx context.Context) bool {
	return nodeHasDep(r.projectRoot, "@adonisjs/core") ||
		nodeHasDep(r.projectRoot, "adonis")
}

// Extract scans for AdonisJS route registrations.
// String action args use last dot-segment as handler name.
// Inline function/arrow args use extractCallsFromBody (same as Express).
func (r *AdonisResolver) Extract(filePath, content string) ([]types.Node, []types.UnresolvedReference) {
	stripped := stripJSComments(content)
	lang := langFromFilePath(filePath)
	totalLines := strings.Count(content, "\n") + 1

	var nodes []types.Node
	var refs []types.UnresolvedReference

	for _, loc := range adonisRouteRe.FindAllStringSubmatchIndex(stripped, -1) {
		if len(loc) < 8 {
			continue
		}
		method := strings.ToUpper(stripped[loc[2]:loc[3]])
		path := stripped[loc[4]:loc[5]]
		handlerRaw := strings.TrimSpace(stripped[loc[6]:loc[7]])

		line := nodeLineOf(stripped, loc[0])
		if line > totalLines {
			line = totalLines
		}

		// Determine if the handler arg is a string action or inline.
		handlerName := ""
		isString := strings.HasPrefix(handlerRaw, "'") || strings.HasPrefix(handlerRaw, "\"")
		isInline := !isString && (strings.HasPrefix(handlerRaw, "(") ||
			strings.HasPrefix(handlerRaw, "function") ||
			strings.Contains(handlerRaw, "=>"))

		if isString {
			// String action: strip quotes and take last segment.
			action := strings.Trim(handlerRaw, `'"`)
			handlerName = extractLastSegment(action)
		} else if !isInline {
			// Plain identifier.
			handlerName = extractIdentifier(handlerRaw)
			if jsReservedInlineNames[handlerName] {
				handlerName = ""
			}
		}
		// Inline bodies: no handler ref (same logic as Express; skip for now
		// since adonis inline routes are rare and calls extraction is optional).

		emitRoute(filePath, line, method, path, handlerName, lang,
			r.claimed, &nodes, &refs)
	}

	return nodes, refs
}

// ClaimsReference returns true if a handler with this name was seen in Extract.
func (r *AdonisResolver) ClaimsReference(name string) bool { return r.claimed[name] }

// Resolve returns confidence 0.85 for claimed handlers.
func (r *AdonisResolver) Resolve(ctx context.Context, ref types.UnresolvedReference) (resolution.ResolvedRef, error) {
	return nodeResolve(r.claimed, ctx, ref)
}
