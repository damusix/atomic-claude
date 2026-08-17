// Node/JS-TS web-framework resolvers: NestJS, Koa, Hapi, Fastify, Sails,
// AdonisJS.
//
// Detect matches the JSON-key form ("fastify":) so a longer package name
// containing the same word does not trip it. A route whose method cannot be
// determined is recorded as ANY rather than dropped.
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

func jsNodeLanguages() []types.Language {
	return []types.Language{
		types.LanguageTypeScript,
		types.LanguageJavaScript,
		types.LanguageTSX,
		types.LanguageJSX,
	}
}

// nodeHasDep matches the JSON-key form so a longer package sharing the prefix
// does not register as a hit.
func nodeHasDep(projectRoot, pkgName string) bool {
	data, err := os.ReadFile(filepath.Join(projectRoot, "package.json"))
	if err != nil {
		return false
	}
	return strings.Contains(string(data), `"`+pkgName+`":`)
}

func nodeLineOf(src string, offset int) int {
	return strings.Count(src[:offset], "\n") + 1
}

// nodeResolve backs Resolve for every Node resolver here.
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

// emitRoute appends the route node and its handler ref, and records the
// handler as claimed.
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

// nestControllerRe captures the prefix, which is empty for a bare @Controller().
var nestControllerRe = regexp.MustCompile(
	`@Controller\s*\(\s*(?:['"]([^'"]*)['"]\s*)?\)`,
)

// nestMethodRe matches `@Get('sub')` and its siblings. Groups: method, sub-path.
var nestMethodRe = regexp.MustCompile(
	`@(Get|Post|Put|Delete|Patch|Options|Head)\s*\(\s*(?:['"]([^'"]*)['"]\s*)?\)`,
)

// nestDefRe captures a method name from a class-body line.
var nestDefRe = regexp.MustCompile(`^\s*(?:(?:public|private|protected|async|readonly|override|abstract)\s+)*([A-Za-z_$][A-Za-z0-9_$]*)\s*\(`)

// nestHandlerName finds the method a route decorator decorates, skipping blank
// lines and stacked decorators such as @UseGuards. It gives up at a class
// boundary rather than scan on and attribute a method from the next class.
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
		return ""
	}
	return ""
}

type NestJSResolver struct {
	projectRoot string
	claimed     map[string]bool
}

func NewNestJSResolver(projectRoot string) *NestJSResolver {
	return &NestJSResolver{projectRoot: projectRoot, claimed: make(map[string]bool)}
}

func (r *NestJSResolver) Name() string { return "nestjs" }

func (r *NestJSResolver) Languages() []types.Language { return jsNodeLanguages() }

func (r *NestJSResolver) Detect(ctx context.Context) bool {
	return nodeHasDep(r.projectRoot, "@nestjs/core") ||
		nodeHasDep(r.projectRoot, "@nestjs/common")
}

// Extract joins each method decorator's sub-path onto the prefix of the
// @Controller it falls under.
func (r *NestJSResolver) Extract(filePath, content string) ([]types.Node, []types.UnresolvedReference) {
	stripped := stripJSComments(content)
	lang := langFromFilePath(filePath)
	totalLines := strings.Count(content, "\n") + 1

	var nodes []types.Node
	var refs []types.UnresolvedReference

	// A controller's prefix applies to every method decorator after it, so
	// positions are collected first and looked up by offset below.
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

		prefix := ""
		for _, c := range controllers {
			if c.offset < matchOffset {
				prefix = c.prefix
			}
		}

		fullPath := buildNestPath(prefix, subPath)

		handlerName := nestHandlerName(stripped[loc[1]:])

		emitRoute(filePath, line, httpMethod, fullPath, handlerName, lang,
			r.claimed, &nodes, &refs)
	}

	return nodes, refs
}

// buildNestPath normalises `("users", ":id")` to "/users/:id".
func buildNestPath(prefix, subPath string) string {
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

func (r *NestJSResolver) ClaimsReference(name string) bool { return r.claimed[name] }

func (r *NestJSResolver) Resolve(ctx context.Context, ref types.UnresolvedReference) (resolution.ResolvedRef, error) {
	return nodeResolve(r.claimed, ctx, ref)
}

// koaRouteRe matches `router.get('/p', handler)`. Groups: method, path, handler.
var koaRouteRe = regexp.MustCompile(
	`(?m)(?:[A-Za-z_$][A-Za-z0-9_$]*)\.` +
		`(get|post|put|delete|patch|head|options|all)\s*\(\s*` +
		`['"]([^'"]+)['"]\s*,\s*([^)]+)`,
)

type KoaResolver struct {
	projectRoot string
	claimed     map[string]bool
}

func NewKoaResolver(projectRoot string) *KoaResolver {
	return &KoaResolver{projectRoot: projectRoot, claimed: make(map[string]bool)}
}

func (r *KoaResolver) Name() string { return "koa" }

func (r *KoaResolver) Languages() []types.Language { return jsNodeLanguages() }

func (r *KoaResolver) Detect(ctx context.Context) bool {
	return nodeHasDep(r.projectRoot, "koa") ||
		nodeHasDep(r.projectRoot, "@koa/router") ||
		nodeHasDep(r.projectRoot, "koa-router")
}

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

func (r *KoaResolver) ClaimsReference(name string) bool { return r.claimed[name] }

func (r *KoaResolver) Resolve(ctx context.Context, ref types.UnresolvedReference) (resolution.ResolvedRef, error) {
	return nodeResolve(r.claimed, ctx, ref)
}

// hapiRouteMethodRe captures the raw method value, which may be a string, an
// array, or '*'. hapiExtractMethods splits it.
var hapiRouteMethodRe = regexp.MustCompile(
	`(?s)method\s*:\s*((?:\[(?:[^]]*)\])|(?:['"][^'"]*['"]))\s*,`,
)

var hapiRoutePathRe = regexp.MustCompile(`path\s*:\s*['"]([^'"]+)['"]`)

var hapiRouteHandlerRe = regexp.MustCompile(`handler\s*:\s*([A-Za-z_$][A-Za-z0-9_$]*)`)

// hapiServerRouteRe matches only the opening of server.route({; findClosingBrace
// locates the end, which a regex cannot.
var hapiServerRouteStartRe = regexp.MustCompile(`(?m)(?:[A-Za-z_$][A-Za-z0-9_$]*)\.route\s*\(\s*\{`)

var hapiMethodTokenRe = regexp.MustCompile(`['"]([A-Za-z*]+)['"]`)

type HapiResolver struct {
	projectRoot string
	claimed     map[string]bool
}

func NewHapiResolver(projectRoot string) *HapiResolver {
	return &HapiResolver{projectRoot: projectRoot, claimed: make(map[string]bool)}
}

func (r *HapiResolver) Name() string { return "hapi" }

func (r *HapiResolver) Languages() []types.Language { return jsNodeLanguages() }

func (r *HapiResolver) Detect(ctx context.Context) bool {
	return nodeHasDep(r.projectRoot, "@hapi/hapi") ||
		nodeHasDep(r.projectRoot, "hapi")
}

// Extract emits one route node per method in a hapi route's method array.
func (r *HapiResolver) Extract(filePath, content string) ([]types.Node, []types.UnresolvedReference) {
	stripped := stripJSComments(content)
	lang := langFromFilePath(filePath)
	totalLines := strings.Count(content, "\n") + 1

	var nodes []types.Node
	var refs []types.UnresolvedReference

	for _, startLoc := range hapiServerRouteStartRe.FindAllStringIndex(stripped, -1) {
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

		methods := hapiExtractMethods(block)

		routePath := ""
		if pm := hapiRoutePathRe.FindStringSubmatch(block); pm != nil {
			routePath = pm[1]
		}
		if routePath == "" {
			continue
		}

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

// hapiExtractMethods maps hapi's '*' wildcard onto ANY.
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

// findClosingBrace returns the index of the brace matching the one at start,
// or -1.
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

func (r *HapiResolver) ClaimsReference(name string) bool { return r.claimed[name] }

func (r *HapiResolver) Resolve(ctx context.Context, ref types.UnresolvedReference) (resolution.ResolvedRef, error) {
	return nodeResolve(r.claimed, ctx, ref)
}

// fastifyShorthandRe matches `fastify.get('/p', handler)`. Groups: method,
// path, handler.
var fastifyShorthandRe = regexp.MustCompile(
	`(?m)(?:[A-Za-z_$][A-Za-z0-9_$]*)\.` +
		`(get|post|put|delete|patch|head|options|all)\s*\(\s*` +
		`['"]([^'"]+)['"]\s*,\s*([^)]+)`,
)

var fastifyRouteMethodRe = regexp.MustCompile(`method\s*:\s*['"]([A-Z]+)['"]`)

// fastifyRouteURLRe reads `url`, which is fastify's spelling of the route path.
var fastifyRouteURLRe = regexp.MustCompile(`url\s*:\s*['"]([^'"]+)['"]`)

var fastifyRouteHandlerRe = regexp.MustCompile(`handler\s*:\s*([A-Za-z_$][A-Za-z0-9_$]*)`)

var fastifyRouteStartRe = regexp.MustCompile(`(?m)(?:[A-Za-z_$][A-Za-z0-9_$]*)\.route\s*\(\s*\{`)

type FastifyResolver struct {
	projectRoot string
	claimed     map[string]bool
}

func NewFastifyResolver(projectRoot string) *FastifyResolver {
	return &FastifyResolver{projectRoot: projectRoot, claimed: make(map[string]bool)}
}

func (r *FastifyResolver) Name() string { return "fastify" }

func (r *FastifyResolver) Languages() []types.Language { return jsNodeLanguages() }

func (r *FastifyResolver) Detect(ctx context.Context) bool {
	return nodeHasDep(r.projectRoot, "fastify")
}

// Extract covers fastify's shorthand and object registration forms.
func (r *FastifyResolver) Extract(filePath, content string) ([]types.Node, []types.UnresolvedReference) {
	stripped := stripJSComments(content)
	lang := langFromFilePath(filePath)
	totalLines := strings.Count(content, "\n") + 1

	var nodes []types.Node
	var refs []types.UnresolvedReference

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

func (r *FastifyResolver) ClaimsReference(name string) bool { return r.claimed[name] }

func (r *FastifyResolver) Resolve(ctx context.Context, ref types.UnresolvedReference) (resolution.ResolvedRef, error) {
	return nodeResolve(r.claimed, ctx, ref)
}

// sailsRouteRe matches a routes.js entry like `'GET /p': 'FooController.act'`.
// Groups: route key, action string. The method half of the key is optional.
var sailsRouteRe = regexp.MustCompile(
	`(?m)['"]([A-Z]+ /[^'"]*|/[^'"]*)['"]\s*:\s*['"]([^'"]+)['"]`,
)

type SailsResolver struct {
	projectRoot string
	claimed     map[string]bool
}

func NewSailsResolver(projectRoot string) *SailsResolver {
	return &SailsResolver{projectRoot: projectRoot, claimed: make(map[string]bool)}
}

func (r *SailsResolver) Name() string { return "sails" }

func (r *SailsResolver) Languages() []types.Language { return jsNodeLanguages() }

func (r *SailsResolver) Detect(ctx context.Context) bool {
	return nodeHasDep(r.projectRoot, "sails")
}

// Extract takes the handler from the action string's last dot-segment.
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

// parseSailsRouteKey splits "GET /p" into its method and path, defaulting the
// method to ANY when the key carries none.
func parseSailsRouteKey(key string) (method, path string) {
	key = strings.TrimSpace(key)
	if idx := strings.Index(key, " "); idx > 0 {
		m := strings.ToUpper(key[:idx])
		if isHTTPMethod(m) {
			return m, key[idx+1:]
		}
	}
	return "ANY", key
}

func isHTTPMethod(s string) bool {
	for _, c := range s {
		if c < 'A' || c > 'Z' {
			return false
		}
	}
	return len(s) > 0
}

func (r *SailsResolver) ClaimsReference(name string) bool { return r.claimed[name] }

func (r *SailsResolver) Resolve(ctx context.Context, ref types.UnresolvedReference) (resolution.ResolvedRef, error) {
	return nodeResolve(r.claimed, ctx, ref)
}

// adonisRouteRe matches `Route.get('/p', X)` where X is a controller string
// or an inline function. Groups: method, path, handler argument.
var adonisRouteRe = regexp.MustCompile(
	`(?m)Route\.(get|post|put|delete|patch|options|head)\s*\(\s*` +
		`['"]([^'"]+)['"]\s*,\s*([^)]+)`,
)

type AdonisResolver struct {
	projectRoot string
	claimed     map[string]bool
}

func NewAdonisResolver(projectRoot string) *AdonisResolver {
	return &AdonisResolver{projectRoot: projectRoot, claimed: make(map[string]bool)}
}

func (r *AdonisResolver) Name() string { return "adonisjs" }

func (r *AdonisResolver) Languages() []types.Language { return jsNodeLanguages() }

func (r *AdonisResolver) Detect(ctx context.Context) bool {
	return nodeHasDep(r.projectRoot, "@adonisjs/core") ||
		nodeHasDep(r.projectRoot, "adonis")
}

// Extract takes the handler from a controller string's last dot-segment.
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

		handlerName := ""
		isString := strings.HasPrefix(handlerRaw, "'") || strings.HasPrefix(handlerRaw, "\"")
		isInline := !isString && (strings.HasPrefix(handlerRaw, "(") ||
			strings.HasPrefix(handlerRaw, "function") ||
			strings.Contains(handlerRaw, "=>"))

		if isString {
			action := strings.Trim(handlerRaw, `'"`)
			handlerName = extractLastSegment(action)
		} else if !isInline {
			handlerName = extractIdentifier(handlerRaw)
			if jsReservedInlineNames[handlerName] {
				handlerName = ""
			}
		}
		// Inline bodies emit no ref: adonis inline routes are rare enough that
		// the call-extraction Express does is not worth carrying here.

		emitRoute(filePath, line, method, path, handlerName, lang,
			r.claimed, &nodes, &refs)
	}

	return nodes, refs
}

func (r *AdonisResolver) ClaimsReference(name string) bool { return r.claimed[name] }

func (r *AdonisResolver) Resolve(ctx context.Context, ref types.UnresolvedReference) (resolution.ResolvedRef, error) {
	return nodeResolve(r.claimed, ctx, ref)
}
