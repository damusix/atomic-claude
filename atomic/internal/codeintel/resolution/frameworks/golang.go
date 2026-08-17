// Go web-framework resolvers: Gin, Echo, Fiber, Gorilla Mux, and Chi.
//
// Across all five, the last argument of a route registration is the handler
// and anything before it is middleware. Handler names are reduced to their
// last dot-segment, since that is the form the name matcher looks up.
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

// goModHasDep substring-matches go.mod rather than parsing it: a module path
// is distinctive enough that a false positive is not a real risk.
func goModHasDep(projectRoot, modulePath string) bool {
	data, err := os.ReadFile(filepath.Join(projectRoot, "go.mod"))
	if err != nil {
		return false
	}
	return strings.Contains(string(data), modulePath)
}

// goHandlerLastSegment reduces "handlers.ListItems" to "ListItems", the form
// the name matcher looks up.
func goHandlerLastSegment(s string) string {
	s = strings.TrimSpace(s)
	end := strings.IndexAny(s, " \t\n\r,)")
	if end >= 0 {
		s = s[:end]
	}
	if idx := strings.LastIndex(s, "."); idx >= 0 {
		s = s[idx+1:]
	}
	return s
}

// goLineOf returns the 1-based line number of byte offset in src.
func goLineOf(src string, offset int) int {
	return strings.Count(src[:offset], "\n") + 1
}

// ginRouteRe accepts either case: Gin exposes both r.GET and r.Any.
var ginRouteRe = regexp.MustCompile(
	`(?m)(?:[A-Za-z_][A-Za-z0-9_]*)\.` +
		`(GET|POST|PUT|DELETE|PATCH|HEAD|OPTIONS|Any|get|post|put|delete|patch|head|options|any)` +
		`\s*\(\s*"([^"]+)"\s*,\s*([^)]+)\)`,
)

var ginImportRe = regexp.MustCompile(`"github\.com/gin-gonic/gin`)

type GinResolver struct {
	projectRoot string
	claimed     map[string]bool
}

func NewGinResolver(projectRoot string) *GinResolver {
	return &GinResolver{projectRoot: projectRoot, claimed: make(map[string]bool)}
}

func (r *GinResolver) Name() string { return "gin" }

func (r *GinResolver) Languages() []types.Language {
	return []types.Language{types.LanguageGo}
}

func (r *GinResolver) Detect(ctx context.Context) bool {
	if goModHasDep(r.projectRoot, "github.com/gin-gonic/gin") {
		return true
	}
	return goContentHasPattern(r.projectRoot, ginImportRe, 30)
}

func (r *GinResolver) Extract(filePath, content string) ([]types.Node, []types.UnresolvedReference) {
	stripped := stripJSComments(content)
	totalLines := strings.Count(content, "\n") + 1

	var nodes []types.Node
	var refs []types.UnresolvedReference

	matches := ginRouteRe.FindAllStringSubmatchIndex(stripped, -1)
	for _, loc := range matches {
		if len(loc) < 8 {
			continue
		}
		method := strings.ToUpper(stripped[loc[2]:loc[3]])
		if method == "ANY" {
			method = "ANY"
		}
		routePath := stripped[loc[4]:loc[5]]
		argsRaw := strings.TrimSpace(stripped[loc[6]:loc[7]])

		line := goLineOf(stripped, loc[0])
		if line > totalLines {
			line = totalLines
		}

		handlerName := extractGoLastArg(argsRaw)
		if handlerName == "" {
			continue
		}
		handlerName = goHandlerLastSegment(handlerName)

		node := MakeRouteNode(filePath, line, method, routePath, types.LanguageGo)
		nodes = append(nodes, node)

		r.claimed[handlerName] = true
		refs = append(refs, types.UnresolvedReference{
			ID:            fmt.Sprintf("ref:%s:%d:%s:%s", filePath, line, method, handlerName),
			FromNodeID:    node.ID,
			ReferenceName: handlerName,
			ReferenceKind: types.EdgeKindReferences,
			Line:          line,
			FilePath:      filePath,
			Language:      types.LanguageGo,
		})
	}

	return nodes, refs
}

func (r *GinResolver) ClaimsReference(name string) bool { return r.claimed[name] }

func (r *GinResolver) Resolve(ctx context.Context, ref types.UnresolvedReference) (resolution.ResolvedRef, error) {
	if !r.claimed[ref.ReferenceName] {
		return resolution.ResolvedRef{}, nil
	}
	return resolution.ResolvedRef{Confidence: 0.85}, nil
}

// echoRouteRe matches `e.GET("/p", handler)`.
var echoRouteRe = regexp.MustCompile(
	`(?m)(?:[A-Za-z_][A-Za-z0-9_]*)\.` +
		`(GET|POST|PUT|DELETE|PATCH|HEAD|OPTIONS|get|post|put|delete|patch|head|options)` +
		`\s*\(\s*"([^"]+)"\s*,\s*([^)]+)\)`,
)

var echoImportRe = regexp.MustCompile(`"github\.com/labstack/echo`)

type EchoResolver struct {
	projectRoot string
	claimed     map[string]bool
}

func NewEchoResolver(projectRoot string) *EchoResolver {
	return &EchoResolver{projectRoot: projectRoot, claimed: make(map[string]bool)}
}

func (r *EchoResolver) Name() string { return "echo" }

func (r *EchoResolver) Languages() []types.Language {
	return []types.Language{types.LanguageGo}
}

func (r *EchoResolver) Detect(ctx context.Context) bool {
	if goModHasDep(r.projectRoot, "github.com/labstack/echo") {
		return true
	}
	return goContentHasPattern(r.projectRoot, echoImportRe, 30)
}

func (r *EchoResolver) Extract(filePath, content string) ([]types.Node, []types.UnresolvedReference) {
	stripped := stripJSComments(content)
	totalLines := strings.Count(content, "\n") + 1

	var nodes []types.Node
	var refs []types.UnresolvedReference

	matches := echoRouteRe.FindAllStringSubmatchIndex(stripped, -1)
	for _, loc := range matches {
		if len(loc) < 8 {
			continue
		}
		method := strings.ToUpper(stripped[loc[2]:loc[3]])
		routePath := stripped[loc[4]:loc[5]]
		handlerRaw := strings.TrimSpace(stripped[loc[6]:loc[7]])

		line := goLineOf(stripped, loc[0])
		if line > totalLines {
			line = totalLines
		}

		handlerName := goHandlerLastSegment(handlerRaw)
		if handlerName == "" {
			continue
		}

		node := MakeRouteNode(filePath, line, method, routePath, types.LanguageGo)
		nodes = append(nodes, node)

		r.claimed[handlerName] = true
		refs = append(refs, types.UnresolvedReference{
			ID:            fmt.Sprintf("ref:%s:%d:%s:%s", filePath, line, method, handlerName),
			FromNodeID:    node.ID,
			ReferenceName: handlerName,
			ReferenceKind: types.EdgeKindReferences,
			Line:          line,
			FilePath:      filePath,
			Language:      types.LanguageGo,
		})
	}

	return nodes, refs
}

func (r *EchoResolver) ClaimsReference(name string) bool { return r.claimed[name] }

func (r *EchoResolver) Resolve(ctx context.Context, ref types.UnresolvedReference) (resolution.ResolvedRef, error) {
	if !r.claimed[ref.ReferenceName] {
		return resolution.ResolvedRef{}, nil
	}
	return resolution.ResolvedRef{Confidence: 0.85}, nil
}

// fiberRouteRe matches `app.Get("/p", handler)`. Fiber's method names are
// Title-case, unlike Gin's and Echo's.
var fiberRouteRe = regexp.MustCompile(
	`(?m)(?:[A-Za-z_][A-Za-z0-9_]*)\.` +
		`(Get|Post|Put|Delete|Patch|Head|Options|All)` +
		`\s*\(\s*"([^"]+)"\s*,\s*([^)]+)\)`,
)

var fiberImportRe = regexp.MustCompile(`"github\.com/gofiber/fiber`)

type FiberResolver struct {
	projectRoot string
	claimed     map[string]bool
}

func NewFiberResolver(projectRoot string) *FiberResolver {
	return &FiberResolver{projectRoot: projectRoot, claimed: make(map[string]bool)}
}

func (r *FiberResolver) Name() string { return "fiber" }

func (r *FiberResolver) Languages() []types.Language {
	return []types.Language{types.LanguageGo}
}

func (r *FiberResolver) Detect(ctx context.Context) bool {
	if goModHasDep(r.projectRoot, "github.com/gofiber/fiber") {
		return true
	}
	return goContentHasPattern(r.projectRoot, fiberImportRe, 30)
}

func (r *FiberResolver) Extract(filePath, content string) ([]types.Node, []types.UnresolvedReference) {
	stripped := stripJSComments(content)
	totalLines := strings.Count(content, "\n") + 1

	var nodes []types.Node
	var refs []types.UnresolvedReference

	matches := fiberRouteRe.FindAllStringSubmatchIndex(stripped, -1)
	for _, loc := range matches {
		if len(loc) < 8 {
			continue
		}
		method := strings.ToUpper(stripped[loc[2]:loc[3]])
		routePath := stripped[loc[4]:loc[5]]
		argsRaw := strings.TrimSpace(stripped[loc[6]:loc[7]])

		line := goLineOf(stripped, loc[0])
		if line > totalLines {
			line = totalLines
		}

		handlerName := extractGoLastArg(argsRaw)
		if handlerName == "" {
			continue
		}
		handlerName = goHandlerLastSegment(handlerName)

		node := MakeRouteNode(filePath, line, method, routePath, types.LanguageGo)
		nodes = append(nodes, node)

		r.claimed[handlerName] = true
		refs = append(refs, types.UnresolvedReference{
			ID:            fmt.Sprintf("ref:%s:%d:%s:%s", filePath, line, method, handlerName),
			FromNodeID:    node.ID,
			ReferenceName: handlerName,
			ReferenceKind: types.EdgeKindReferences,
			Line:          line,
			FilePath:      filePath,
			Language:      types.LanguageGo,
		})
	}

	return nodes, refs
}

func (r *FiberResolver) ClaimsReference(name string) bool { return r.claimed[name] }

func (r *FiberResolver) Resolve(ctx context.Context, ref types.UnresolvedReference) (resolution.ResolvedRef, error) {
	if !r.claimed[ref.ReferenceName] {
		return resolution.ResolvedRef{}, nil
	}
	return resolution.ResolvedRef{Confidence: 0.85}, nil
}

// gorillaHandleFuncRe matches `r.HandleFunc("/p", handler)`. The optional
// .Methods chain that follows is gorillaMethodsRe's job.
var gorillaHandleFuncRe = regexp.MustCompile(
	`(?m)(?:[A-Za-z_][A-Za-z0-9_]*)\.HandleFunc\s*\(\s*"([^"]+)"\s*,\s*([A-Za-z_][A-Za-z0-9_.]*)\s*\)`,
)

// gorillaMethodsRe tolerates whitespace after the dot, since the chain is
// idiomatically split across lines.
var gorillaMethodsRe = regexp.MustCompile(`\.\s*Methods\s*\(([^)]+)\)`)

var gorillaMethodTokenRe = regexp.MustCompile(`"([A-Z]+)"`)

var gorillaImportRe = regexp.MustCompile(`"github\.com/gorilla/mux"`)

type GorillaResolver struct {
	projectRoot string
	claimed     map[string]bool
}

func NewGorillaResolver(projectRoot string) *GorillaResolver {
	return &GorillaResolver{projectRoot: projectRoot, claimed: make(map[string]bool)}
}

func (r *GorillaResolver) Name() string { return "gorilla" }

func (r *GorillaResolver) Languages() []types.Language {
	return []types.Language{types.LanguageGo}
}

func (r *GorillaResolver) Detect(ctx context.Context) bool {
	if goModHasDep(r.projectRoot, "github.com/gorilla/mux") {
		return true
	}
	return goContentHasPattern(r.projectRoot, gorillaImportRe, 30)
}

// Extract emits one route node per method in the .Methods chain.
func (r *GorillaResolver) Extract(filePath, content string) ([]types.Node, []types.UnresolvedReference) {
	stripped := stripJSComments(content)
	totalLines := strings.Count(content, "\n") + 1

	var nodes []types.Node
	var refs []types.UnresolvedReference

	matches := gorillaHandleFuncRe.FindAllStringSubmatchIndex(stripped, -1)
	for _, loc := range matches {
		if len(loc) < 6 {
			continue
		}
		routePath := stripped[loc[2]:loc[3]]
		handlerRaw := strings.TrimSpace(stripped[loc[4]:loc[5]])

		line := goLineOf(stripped, loc[0])
		if line > totalLines {
			line = totalLines
		}

		handlerName := goHandlerLastSegment(handlerRaw)
		if handlerName == "" {
			continue
		}

		// A fixed window stands in for the statement's real extent.
		matchEnd := loc[1]
		windowEnd := matchEnd + 200
		if windowEnd > len(stripped) {
			windowEnd = len(stripped)
		}
		window := stripped[matchEnd:windowEnd]
		methods := extractGorillaMethods(window)

		r.claimed[handlerName] = true

		for _, method := range methods {
			node := MakeRouteNode(filePath, line, method, routePath, types.LanguageGo)
			nodes = append(nodes, node)
			refs = append(refs, types.UnresolvedReference{
				ID:            fmt.Sprintf("ref:%s:%d:%s:%s", filePath, line, method, handlerName),
				FromNodeID:    node.ID,
				ReferenceName: handlerName,
				ReferenceKind: types.EdgeKindReferences,
				Line:          line,
				FilePath:      filePath,
				Language:      types.LanguageGo,
			})
		}
	}

	return nodes, refs
}

// extractGorillaMethods falls back to ANY when no .Methods chain is present,
// which is Gorilla's own semantics for an unrestricted route.
func extractGorillaMethods(window string) []string {
	m := gorillaMethodsRe.FindStringSubmatchIndex(window)
	if m == nil {
		return []string{"ANY"}
	}

	argsText := window[m[2]:m[3]]
	var methods []string
	for _, tok := range gorillaMethodTokenRe.FindAllStringSubmatch(argsText, -1) {
		if len(tok) >= 2 && tok[1] != "" {
			methods = append(methods, tok[1])
		}
	}
	if len(methods) == 0 {
		return []string{"ANY"}
	}
	return methods
}

func (r *GorillaResolver) ClaimsReference(name string) bool { return r.claimed[name] }

func (r *GorillaResolver) Resolve(ctx context.Context, ref types.UnresolvedReference) (resolution.ResolvedRef, error) {
	if !r.claimed[ref.ReferenceName] {
		return resolution.ResolvedRef{}, nil
	}
	return resolution.ResolvedRef{Confidence: 0.85}, nil
}

// chiShorthandRe matches `r.Get("/p", handler)`; Chi's method names are
// Title-case.
var chiShorthandRe = regexp.MustCompile(
	`(?m)(?:[A-Za-z_][A-Za-z0-9_]*)\.` +
		`(Get|Post|Put|Delete|Patch|Head|Options)` +
		`\s*\(\s*"([^"]+)"\s*,\s*([A-Za-z_][A-Za-z0-9_.]*)\s*\)`,
)

// chiMethodRe matches r.Method("GET", "/path", handler) form.
var chiMethodRe = regexp.MustCompile(
	`(?m)(?:[A-Za-z_][A-Za-z0-9_]*)\.Method\s*\(\s*"([A-Z]+)"\s*,\s*"([^"]+)"\s*,\s*([A-Za-z_][A-Za-z0-9_.]*)\s*\)`,
)

var chiImportRe = regexp.MustCompile(`"github\.com/go-chi/chi`)

type ChiResolver struct {
	projectRoot string
	claimed     map[string]bool
}

func NewChiResolver(projectRoot string) *ChiResolver {
	return &ChiResolver{projectRoot: projectRoot, claimed: make(map[string]bool)}
}

func (r *ChiResolver) Name() string { return "chi" }

func (r *ChiResolver) Languages() []types.Language {
	return []types.Language{types.LanguageGo}
}

func (r *ChiResolver) Detect(ctx context.Context) bool {
	if goModHasDep(r.projectRoot, "github.com/go-chi/chi") {
		return true
	}
	return goContentHasPattern(r.projectRoot, chiImportRe, 30)
}

func (r *ChiResolver) Extract(filePath, content string) ([]types.Node, []types.UnresolvedReference) {
	stripped := stripJSComments(content)
	totalLines := strings.Count(content, "\n") + 1

	var nodes []types.Node
	var refs []types.UnresolvedReference

	emitRoute := func(method, routePath, handlerRaw string, line int) {
		handlerName := goHandlerLastSegment(handlerRaw)
		if handlerName == "" {
			return
		}
		node := MakeRouteNode(filePath, line, method, routePath, types.LanguageGo)
		nodes = append(nodes, node)
		r.claimed[handlerName] = true
		refs = append(refs, types.UnresolvedReference{
			ID:            fmt.Sprintf("ref:%s:%d:%s:%s", filePath, line, method, handlerName),
			FromNodeID:    node.ID,
			ReferenceName: handlerName,
			ReferenceKind: types.EdgeKindReferences,
			Line:          line,
			FilePath:      filePath,
			Language:      types.LanguageGo,
		})
	}

	for _, loc := range chiShorthandRe.FindAllStringSubmatchIndex(stripped, -1) {
		if len(loc) < 8 {
			continue
		}
		method := strings.ToUpper(stripped[loc[2]:loc[3]])
		routePath := stripped[loc[4]:loc[5]]
		handlerRaw := strings.TrimSpace(stripped[loc[6]:loc[7]])
		line := goLineOf(stripped, loc[0])
		if line > totalLines {
			line = totalLines
		}
		emitRoute(method, routePath, handlerRaw, line)
	}

	for _, loc := range chiMethodRe.FindAllStringSubmatchIndex(stripped, -1) {
		if len(loc) < 8 {
			continue
		}
		method := stripped[loc[2]:loc[3]] // already upper
		routePath := stripped[loc[4]:loc[5]]
		handlerRaw := strings.TrimSpace(stripped[loc[6]:loc[7]])
		line := goLineOf(stripped, loc[0])
		if line > totalLines {
			line = totalLines
		}
		emitRoute(method, routePath, handlerRaw, line)
	}

	return nodes, refs
}

func (r *ChiResolver) ClaimsReference(name string) bool { return r.claimed[name] }

func (r *ChiResolver) Resolve(ctx context.Context, ref types.UnresolvedReference) (resolution.ResolvedRef, error) {
	if !r.claimed[ref.ReferenceName] {
		return resolution.ResolvedRef{}, nil
	}
	return resolution.ResolvedRef{Confidence: 0.85}, nil
}

// goContentHasPattern scans only top-level .go files: a full walk at detection
// time would cost more than the frameworks it would find.
func goContentHasPattern(projectRoot string, pattern *regexp.Regexp, maxLines int) bool {
	entries, err := os.ReadDir(projectRoot)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if filepath.Ext(entry.Name()) != ".go" {
			continue
		}
		snippet := readFirstNLines(filepath.Join(projectRoot, entry.Name()), maxLines)
		if pattern.MatchString(snippet) {
			return true
		}
	}
	return false
}

// extractGoLastArg picks the handler out of "authMiddleware, myHandler".
func extractGoLastArg(argsRaw string) string {
	argsRaw = strings.TrimRight(argsRaw, " \t\n\r)")
	parts := strings.Split(argsRaw, ",")
	last := strings.TrimSpace(parts[len(parts)-1])
	return last
}
