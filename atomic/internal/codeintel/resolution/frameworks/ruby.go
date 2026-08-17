// Ruby on Rails router resolver.
//
// Rails routes are mostly not written one per line: `resources :articles`
// stands for seven routes, so most of this file expands DSL macros rather
// than matching declarations.
//
// Known gaps: `=begin`/`=end` block comments are not stripped, and nesting
// deeper than one level is flattened to top-level paths. Neither appears in
// real config/routes.rb often enough to pay for.
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

// stripHashLineComments serves both Rails and Phoenix. It borrows Python's
// line stripper, whose triple-quote handling is inert on Ruby route files.
func stripHashLineComments(src string) string {
	return stripPyComments(src)
}

// railsVerbRe matches `get '/p', to: 'c#a'` and the `=>` spelling of it.
// Groups: verb, path, controller#action. Horizontal-whitespace classes are
// used rather than \s, so a match cannot start on one line and be attributed
// to another.
var railsVerbRe = regexp.MustCompile(
	`(?m)^[^\S\n]*(get|post|put|patch|delete)[^\S\n]+` +
		`['"]([^'"]+)['"]\s*` +
		`(?:,\s*to:\s*|=>\s*)` +
		`['"]([^'"#]+#[^'"]+)['"]`,
)

// railsRootRe captures the target of `root 'controller#action'`.
var railsRootRe = regexp.MustCompile(
	`(?m)^[^\S\n]*root[^\S\n]+['"]([^'"#]+#[^'"]+)['"]`,
)

func railsActionFromTarget(target string) string {
	idx := strings.LastIndex(target, "#")
	if idx < 0 || idx == len(target)-1 {
		return ""
	}
	return strings.TrimSpace(target[idx+1:])
}

// railsParamRe captures a `param: :slug` override of the default :id segment.
var railsParamRe = regexp.MustCompile(`param:\s*:([A-Za-z_][A-Za-z0-9_]*)`)

var railsOnlyRe = regexp.MustCompile(`only:\s*\[([^\]]+)\]`)

var railsExceptRe = regexp.MustCompile(`except:\s*\[([^\]]+)\]`)

// railsCollectionMemberRe matches `get :action, on: :collection` inside a
// resource block. Groups: verb, action, collection-or-member.
var railsCollectionMemberRe = regexp.MustCompile(
	`(?m)^[^\S\n]*(get|post|put|patch|delete)[^\S\n]+:([A-Za-z_][A-Za-z0-9_]*)\s*,\s*on:\s*:(collection|member)`,
)

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

var resourcesBlockRe = regexp.MustCompile(
	`^[^\S\n]*(resources?)[^\S\n]+:([A-Za-z_][A-Za-z0-9_]*)([^#\n]*)\s+do\s*$`,
)

var resourceLineRe = regexp.MustCompile(
	`^[^\S\n]*(resources?)[^\S\n]+:([A-Za-z_][A-Za-z0-9_]*)([^\n]*)$`,
)

// railsScopeBlockRe matches the `scope`/`namespace` openers that contribute a
// path segment, in symbol, string, and `path:` spellings. A form carrying no
// path at all (`scope module: :api do`) deliberately does not match: it opens
// a block but adds nothing to the prefix.
var railsScopeBlockRe = regexp.MustCompile(
	`^[^\S\n]*(namespace|scope)[^\S\n]+` +
		`(?:` +
		`:([A-Za-z_][A-Za-z0-9_/]*)` +
		`|` +
		`['"]([^'"]+)['"]` +
		`|` +
		`(?:[^#\n]*[,\s])?path:\s*:([A-Za-z_][A-Za-z0-9_/]*)` +
		`|` +
		`(?:[^#\n]*[,\s])?path:\s*['"]([^'"]+)['"]` +
		`)` +
		`[^\S\n]*(?:[^#\n]*)[^\S\n]+do\s*$`,
)

func railsParseIDParam(line string) string {
	if m := railsParamRe.FindStringSubmatch(line); len(m) >= 2 {
		return m[1]
	}
	return "id"
}

// railsFilterActions applies a line's only:/except: options to the candidate
// action set.
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
	result := map[string]bool{}
	for _, a := range actions {
		result[a] = true
	}
	return result
}

// emitPluralResources expands `resources :name` into its seven REST routes.
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

// emitSingularResource expands `resource :name`, which has no index action and
// no :id segment.
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

type railsBlockContext struct {
	// parentPath prefixes anything nested in this block, e.g.
	// "/articles/:slug" for `resources :articles, param: :slug do`.
	basePath   string // e.g. "/articles"
	idPath     string // e.g. "/articles/:slug"
	isSingular bool   // true = resource (singular), false = resources (plural)
	depth      int    // block nesting depth when this context was opened
}

// railsScopeSegment reads whichever of the regex's four path alternatives
// matched, or "" when the opener carried no path.
func railsScopeSegment(m []string) string {
	for _, g := range m[2:] {
		if g != "" {
			return strings.TrimPrefix(g, "/")
		}
	}
	return ""
}

func railsScopePrefix(segs []string) string {
	if len(segs) == 0 {
		return ""
	}
	return "/" + strings.Join(segs, "/")
}

// railsParseDSL expands resources/resource blocks line by line, tracking two
// stacks: enclosing resources (which supply a parent path) and enclosing
// scope/namespace openers (which supply a prefix, composing when nested).
//
// Imperative verb routes are deliberately not prefixed here — they carry their
// full literal path already, and Pass 1 handles them over the whole file.
func railsParseDSL(
	filePath, content string,
	lang types.Language,
	nodes *[]types.Node,
	refs *[]types.UnresolvedReference,
	claimed map[string]bool,
) {
	lines := strings.Split(content, "\n")

	// Only resource blocks are stacked; a global depth counter is what tells
	// us when one of them closes.
	type blockEntry struct {
		ctx   railsBlockContext
		depth int // depth at which this resource block was opened
	}

	type scopeEntry struct {
		segment string // path segment (without leading slash), e.g. "api"
		depth   int    // depth at which this scope block was opened
	}

	depth := 0
	var resourceStack []blockEntry // stack of resource block contexts
	var scopeStack []scopeEntry    // stack of scope/namespace path contributors

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

		// Block tracking is textual: a trailing `do` opens, a lone `end` closes.
		opensBlock := strings.HasSuffix(line, " do") ||
			strings.HasSuffix(line, "\tdo") ||
			line == "do" ||
			(strings.HasSuffix(line, "do") && len(line) > 2 && (line[len(line)-3] == ' ' || line[len(line)-3] == '\t'))
		closesBlock := line == "end" || strings.HasPrefix(line, "end ")

		if closesBlock {
			if depth > 0 {
				depth--
			}
			if len(resourceStack) > 0 && resourceStack[len(resourceStack)-1].depth == depth+1 {
				resourceStack = resourceStack[:len(resourceStack)-1]
			}
			if len(scopeStack) > 0 && scopeStack[len(scopeStack)-1].depth == depth+1 {
				scopeStack = scopeStack[:len(scopeStack)-1]
			}
			continue
		}

		var parentCtx *railsBlockContext
		if len(resourceStack) > 0 {
			parentCtx = &resourceStack[len(resourceStack)-1].ctx
		}

		// Tested before resourcesBlockRe: `namespace` also ends in `do`.
		if m := railsScopeBlockRe.FindStringSubmatch(rawLine); len(m) >= 2 {
			seg := railsScopeSegment(m)
			depth++
			if seg != "" {
				scopeStack = append(scopeStack, scopeEntry{segment: seg, depth: depth})
			}
			continue
		}

		if m := resourcesBlockRe.FindStringSubmatch(rawLine); len(m) >= 4 {
			kind := m[1] // "resources" or "resource"
			name := m[2] // resource name
			opts := m[3] // rest of line before " do"

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
			opts := m[3]
			if strings.HasSuffix(strings.TrimSpace(opts), "do") {
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

		if parentCtx != nil {
			if m := railsCollectionMemberRe.FindStringSubmatch(rawLine); len(m) >= 4 {
				verb := strings.ToUpper(m[1])
				action := m[2]
				onType := m[3] // "collection" or "member"

				var routePath string
				if onType == "collection" {
					routePath = parentCtx.basePath + "/" + action
				} else {
					// A member route hangs off the :id segment, not after it.
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

type RailsResolver struct {
	projectRoot string
	claimed     map[string]bool
}

func NewRailsResolver(projectRoot string) *RailsResolver {
	return &RailsResolver{projectRoot: projectRoot, claimed: make(map[string]bool)}
}

func (r *RailsResolver) Name() string { return "rails" }

func (r *RailsResolver) Languages() []types.Language {
	return []types.Language{types.LanguageRuby}
}

// Detect matches the `gem 'rails'` line form, which admits a version
// constraint after it but not a longer gem name containing "rails".
func (r *RailsResolver) Detect(ctx context.Context) bool {
	data, err := os.ReadFile(filepath.Join(r.projectRoot, "Gemfile"))
	if err != nil {
		return false
	}
	return strings.Contains(string(data), `gem 'rails'`) ||
		strings.Contains(string(data), `gem "rails"`)
}

// Extract runs two passes: regex over the imperative verb forms, then a
// line-by-line parse of the resources/resource block DSL.
func (r *RailsResolver) Extract(filePath, content string) ([]types.Node, []types.UnresolvedReference) {
	stripped := stripHashLineComments(content)
	totalLines := strings.Count(content, "\n") + 1

	var nodes []types.Node
	var refs []types.UnresolvedReference

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

	railsParseDSL(filePath, stripped, types.LanguageRuby, &nodes, &refs, r.claimed)

	return nodes, refs
}

func (r *RailsResolver) ClaimsReference(name string) bool { return r.claimed[name] }

func (r *RailsResolver) Resolve(ctx context.Context, ref types.UnresolvedReference) (resolution.ResolvedRef, error) {
	if !r.claimed[ref.ReferenceName] {
		return resolution.ResolvedRef{}, nil
	}
	return resolution.ResolvedRef{Confidence: 0.85}, nil
}
