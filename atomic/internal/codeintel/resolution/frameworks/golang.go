// Package frameworks — Go framework resolvers (CP15 batch B).
//
// This file implements five FrameworkResolver + FrameworkExtractor pairs for
// the five major Go web frameworks: Gin, Echo, Fiber, Gorilla Mux, and Chi.
//
// # Language
//
// All five resolvers set Language = types.LanguageGo.
//
// # Comment stripping
//
// Go comments use the same delimiters as JS/TS:
//   - Single-line: // to end of line.
//   - Block: /* ... */ (can span lines).
//
// stripJSComments (defined in frameworks.go) handles both — no Go-specific
// stripper needed. Comments are stripped before route regexes run, so
// commented-out routes never emit route nodes.
//
// # Route node format (appendix H — via MakeRouteNode)
//
//	id:            route:{filePath}:{line}:{METHOD}:{path}
//	qualifiedName: {filePath}::METHOD:{path}
//	name:          "METHOD /path"
//
// # Handler extraction
//
// Go handlers are identifiers or qualified names (pkg.Fn, h.Method).
// The LAST argument in a route registration is the handler; preceding args are
// middleware. The handler is reduced to its last dot-segment for the ref name.
// Example: "handlers.ListItems" → ref name "ListItems".
//
// # Gorilla .Methods() fan-out
//
// r.HandleFunc("/path", handler).Methods("GET","POST") emits ONE route node per
// method (same pattern as Flask's methods list). If .Methods() is absent the
// method is "ANY".
//
// # Detect
//
// Each resolver reads go.mod in the project root and looks for the framework's
// module path as a substring match (e.g. "github.com/gin-gonic/gin").
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
// Shared Go helpers
// ---------------------------------------------------------------------------

// goModHasDep returns true if the project's go.mod file contains the given
// module path as a substring (e.g. "github.com/gin-gonic/gin").
func goModHasDep(projectRoot, modulePath string) bool {
	data, err := os.ReadFile(filepath.Join(projectRoot, "go.mod"))
	if err != nil {
		return false
	}
	return strings.Contains(string(data), modulePath)
}

// goHandlerLastSegment returns the last dot-segment of a handler expression.
// "handlers.ListItems" → "ListItems"; "myHandler" → "myHandler".
func goHandlerLastSegment(s string) string {
	s = strings.TrimSpace(s)
	// Stop at non-identifier characters (space, comma, newline, paren, etc.)
	end := strings.IndexAny(s, " \t\n\r,)")
	if end >= 0 {
		s = s[:end]
	}
	// Take last dot-segment.
	if idx := strings.LastIndex(s, "."); idx >= 0 {
		s = s[idx+1:]
	}
	return s
}

// goLineOf returns the 1-based line number of byte offset in src.
func goLineOf(src string, offset int) int {
	return strings.Count(src[:offset], "\n") + 1
}

// ---------------------------------------------------------------------------
// Gin resolver
// ---------------------------------------------------------------------------

// ginMethods are the Gin route-registering method names (lowercase) and "Any".
// The regex below matches the method name as a capture group.
var ginRouteRe = regexp.MustCompile(
	`(?m)(?:[A-Za-z_][A-Za-z0-9_]*)\.` +
		`(GET|POST|PUT|DELETE|PATCH|HEAD|OPTIONS|Any|get|post|put|delete|patch|head|options|any)` +
		`\s*\(\s*"([^"]+)"\s*,\s*([^)]+)\)`,
)

// ginImportRe detects gin import patterns in go source.
var ginImportRe = regexp.MustCompile(`"github\.com/gin-gonic/gin`)

// GinResolver implements FrameworkResolver + FrameworkExtractor for Gin.
type GinResolver struct {
	projectRoot string
	claimed     map[string]bool
}

// NewGinResolver creates a GinResolver without a DB.
func NewGinResolver(projectRoot string) *GinResolver {
	return &GinResolver{projectRoot: projectRoot, claimed: make(map[string]bool)}
}

// Name returns "gin".
func (r *GinResolver) Name() string { return "gin" }

// Languages returns [go].
func (r *GinResolver) Languages() []types.Language {
	return []types.Language{types.LanguageGo}
}

// Detect returns true if go.mod requires github.com/gin-gonic/gin, or a top-level
// .go file imports gin.
func (r *GinResolver) Detect(ctx context.Context) bool {
	if goModHasDep(r.projectRoot, "github.com/gin-gonic/gin") {
		return true
	}
	return goContentHasPattern(r.projectRoot, ginImportRe, 30)
}

// Extract scans filePath/content for Gin route registrations and returns route
// nodes + handler refs. Comments are stripped first (reusing stripJSComments).
// The LAST argument is the handler; earlier arguments are middleware.
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

		// The LAST comma-separated segment is the handler.
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

// ClaimsReference returns true if a handler with this name was seen in Extract.
func (r *GinResolver) ClaimsReference(name string) bool { return r.claimed[name] }

// Resolve returns confidence 0.85 for claimed handlers (appendix H: 0.8–0.9).
func (r *GinResolver) Resolve(ctx context.Context, ref types.UnresolvedReference) (resolution.ResolvedRef, error) {
	if !r.claimed[ref.ReferenceName] {
		return resolution.ResolvedRef{}, nil
	}
	return resolution.ResolvedRef{Confidence: 0.85}, nil
}

// ---------------------------------------------------------------------------
// Echo resolver
// ---------------------------------------------------------------------------

// echoRouteRe matches Echo route registrations of the form:
//
//	e.GET("/path", handler)
//	e.POST("/path", handler)
var echoRouteRe = regexp.MustCompile(
	`(?m)(?:[A-Za-z_][A-Za-z0-9_]*)\.` +
		`(GET|POST|PUT|DELETE|PATCH|HEAD|OPTIONS|get|post|put|delete|patch|head|options)` +
		`\s*\(\s*"([^"]+)"\s*,\s*([^)]+)\)`,
)

// echoImportRe detects echo import patterns.
var echoImportRe = regexp.MustCompile(`"github\.com/labstack/echo`)

// EchoResolver implements FrameworkResolver + FrameworkExtractor for Echo.
type EchoResolver struct {
	projectRoot string
	claimed     map[string]bool
}

// NewEchoResolver creates an EchoResolver without a DB.
func NewEchoResolver(projectRoot string) *EchoResolver {
	return &EchoResolver{projectRoot: projectRoot, claimed: make(map[string]bool)}
}

// Name returns "echo".
func (r *EchoResolver) Name() string { return "echo" }

// Languages returns [go].
func (r *EchoResolver) Languages() []types.Language {
	return []types.Language{types.LanguageGo}
}

// Detect returns true if go.mod requires github.com/labstack/echo.
func (r *EchoResolver) Detect(ctx context.Context) bool {
	if goModHasDep(r.projectRoot, "github.com/labstack/echo") {
		return true
	}
	return goContentHasPattern(r.projectRoot, echoImportRe, 30)
}

// Extract scans filePath/content for Echo route registrations.
// Handler is the second argument (no middleware list in Echo's basic form).
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

// ClaimsReference returns true if a handler with this name was seen in Extract.
func (r *EchoResolver) ClaimsReference(name string) bool { return r.claimed[name] }

// Resolve returns confidence 0.85 for claimed handlers.
func (r *EchoResolver) Resolve(ctx context.Context, ref types.UnresolvedReference) (resolution.ResolvedRef, error) {
	if !r.claimed[ref.ReferenceName] {
		return resolution.ResolvedRef{}, nil
	}
	return resolution.ResolvedRef{Confidence: 0.85}, nil
}

// ---------------------------------------------------------------------------
// Fiber resolver
// ---------------------------------------------------------------------------

// fiberRouteRe matches Fiber route registrations of the form:
//
//	app.Get("/path", handler)
//	app.Post("/path", handler)
//
// Fiber uses Title-case method names: Get, Post, Put, Delete, Patch, Head, Options, All.
var fiberRouteRe = regexp.MustCompile(
	`(?m)(?:[A-Za-z_][A-Za-z0-9_]*)\.` +
		`(Get|Post|Put|Delete|Patch|Head|Options|All)` +
		`\s*\(\s*"([^"]+)"\s*,\s*([^)]+)\)`,
)

// fiberImportRe detects fiber import patterns.
var fiberImportRe = regexp.MustCompile(`"github\.com/gofiber/fiber`)

// FiberResolver implements FrameworkResolver + FrameworkExtractor for Fiber.
type FiberResolver struct {
	projectRoot string
	claimed     map[string]bool
}

// NewFiberResolver creates a FiberResolver without a DB.
func NewFiberResolver(projectRoot string) *FiberResolver {
	return &FiberResolver{projectRoot: projectRoot, claimed: make(map[string]bool)}
}

// Name returns "fiber".
func (r *FiberResolver) Name() string { return "fiber" }

// Languages returns [go].
func (r *FiberResolver) Languages() []types.Language {
	return []types.Language{types.LanguageGo}
}

// Detect returns true if go.mod requires github.com/gofiber/fiber.
func (r *FiberResolver) Detect(ctx context.Context) bool {
	if goModHasDep(r.projectRoot, "github.com/gofiber/fiber") {
		return true
	}
	return goContentHasPattern(r.projectRoot, fiberImportRe, 30)
}

// Extract scans filePath/content for Fiber route registrations.
// Handler is the last argument (middleware can precede it).
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
		// Fiber uses Title-case methods; normalize to UPPER for route node name.
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

// ClaimsReference returns true if a handler with this name was seen in Extract.
func (r *FiberResolver) ClaimsReference(name string) bool { return r.claimed[name] }

// Resolve returns confidence 0.85 for claimed handlers.
func (r *FiberResolver) Resolve(ctx context.Context, ref types.UnresolvedReference) (resolution.ResolvedRef, error) {
	if !r.claimed[ref.ReferenceName] {
		return resolution.ResolvedRef{}, nil
	}
	return resolution.ResolvedRef{Confidence: 0.85}, nil
}

// ---------------------------------------------------------------------------
// Gorilla Mux resolver
// ---------------------------------------------------------------------------

// gorillaHandleFuncRe matches Gorilla route registrations of the form:
//
//	r.HandleFunc("/path", handler)
//
// capturing the path and handler. The optional .Methods("GET","POST") chain
// is handled by gorillMethodsRe applied to the text following the match.
var gorillaHandleFuncRe = regexp.MustCompile(
	`(?m)(?:[A-Za-z_][A-Za-z0-9_]*)\.HandleFunc\s*\(\s*"([^"]+)"\s*,\s*([A-Za-z_][A-Za-z0-9_.]*)\s*\)`,
)

// gorillaMethodsRe extracts methods from a .Methods("GET","POST") chain.
// Allows optional whitespace (including newlines) between the dot and Methods
// so that multi-line continuation chains are matched correctly.
var gorillaMethodsRe = regexp.MustCompile(`\.\s*Methods\s*\(([^)]+)\)`)

// gorillaMethodTokenRe extracts individual method strings from the Methods() argument.
var gorillaMethodTokenRe = regexp.MustCompile(`"([A-Z]+)"`)

// gorillaImportRe detects gorilla/mux import patterns.
var gorillaImportRe = regexp.MustCompile(`"github\.com/gorilla/mux"`)

// GorillaResolver implements FrameworkResolver + FrameworkExtractor for Gorilla Mux.
type GorillaResolver struct {
	projectRoot string
	claimed     map[string]bool
}

// NewGorillaResolver creates a GorillaResolver without a DB.
func NewGorillaResolver(projectRoot string) *GorillaResolver {
	return &GorillaResolver{projectRoot: projectRoot, claimed: make(map[string]bool)}
}

// Name returns "gorilla".
func (r *GorillaResolver) Name() string { return "gorilla" }

// Languages returns [go].
func (r *GorillaResolver) Languages() []types.Language {
	return []types.Language{types.LanguageGo}
}

// Detect returns true if go.mod requires github.com/gorilla/mux.
func (r *GorillaResolver) Detect(ctx context.Context) bool {
	if goModHasDep(r.projectRoot, "github.com/gorilla/mux") {
		return true
	}
	return goContentHasPattern(r.projectRoot, gorillaImportRe, 30)
}

// Extract scans filePath/content for Gorilla HandleFunc registrations.
// .Methods("GET","POST") → one route node per method. No .Methods() → method "ANY".
// Handler is the second argument of HandleFunc.
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

		// Look for a .Methods(...) chain in the rest of the same logical line.
		// We search a short window after the match end to find the chain.
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

// extractGorillaMethods looks for a .Methods(...) chain in the window text
// and returns the list of HTTP method strings found. Returns ["ANY"] if no
// .Methods() is found. The 200-char window already limits scope to the
// current statement; we do not reject based on newlines so that idiomatic
// multi-line chaining (r.HandleFunc(...).⏎\t.Methods("GET")) is handled.
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

// ClaimsReference returns true if a handler with this name was seen in Extract.
func (r *GorillaResolver) ClaimsReference(name string) bool { return r.claimed[name] }

// Resolve returns confidence 0.85 for claimed handlers.
func (r *GorillaResolver) Resolve(ctx context.Context, ref types.UnresolvedReference) (resolution.ResolvedRef, error) {
	if !r.claimed[ref.ReferenceName] {
		return resolution.ResolvedRef{}, nil
	}
	return resolution.ResolvedRef{Confidence: 0.85}, nil
}

// ---------------------------------------------------------------------------
// Chi resolver
// ---------------------------------------------------------------------------

// chiShorthandRe matches Chi shorthand route registrations of the form:
//
//	r.Get("/path", handler)
//	r.Post("/path", handler)
//
// Chi uses Title-case method names: Get, Post, Put, Delete, Patch, Head, Options.
var chiShorthandRe = regexp.MustCompile(
	`(?m)(?:[A-Za-z_][A-Za-z0-9_]*)\.` +
		`(Get|Post|Put|Delete|Patch|Head|Options)` +
		`\s*\(\s*"([^"]+)"\s*,\s*([A-Za-z_][A-Za-z0-9_.]*)\s*\)`,
)

// chiMethodRe matches r.Method("GET", "/path", handler) form.
var chiMethodRe = regexp.MustCompile(
	`(?m)(?:[A-Za-z_][A-Za-z0-9_]*)\.Method\s*\(\s*"([A-Z]+)"\s*,\s*"([^"]+)"\s*,\s*([A-Za-z_][A-Za-z0-9_.]*)\s*\)`,
)

// chiImportRe detects chi import patterns.
var chiImportRe = regexp.MustCompile(`"github\.com/go-chi/chi`)

// ChiResolver implements FrameworkResolver + FrameworkExtractor for Chi.
type ChiResolver struct {
	projectRoot string
	claimed     map[string]bool
}

// NewChiResolver creates a ChiResolver without a DB.
func NewChiResolver(projectRoot string) *ChiResolver {
	return &ChiResolver{projectRoot: projectRoot, claimed: make(map[string]bool)}
}

// Name returns "chi".
func (r *ChiResolver) Name() string { return "chi" }

// Languages returns [go].
func (r *ChiResolver) Languages() []types.Language {
	return []types.Language{types.LanguageGo}
}

// Detect returns true if go.mod requires github.com/go-chi/chi.
func (r *ChiResolver) Detect(ctx context.Context) bool {
	if goModHasDep(r.projectRoot, "github.com/go-chi/chi") {
		return true
	}
	return goContentHasPattern(r.projectRoot, chiImportRe, 30)
}

// Extract scans filePath/content for Chi route registrations.
// Handles both r.Get("/p", h) and r.Method("GET", "/p", h) forms.
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

	// r.Get("/p", h) and similar shorthand forms.
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

	// r.Method("GET", "/p", h) form.
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

// ClaimsReference returns true if a handler with this name was seen in Extract.
func (r *ChiResolver) ClaimsReference(name string) bool { return r.claimed[name] }

// Resolve returns confidence 0.85 for claimed handlers.
func (r *ChiResolver) Resolve(ctx context.Context, ref types.UnresolvedReference) (resolution.ResolvedRef, error) {
	if !r.claimed[ref.ReferenceName] {
		return resolution.ResolvedRef{}, nil
	}
	return resolution.ResolvedRef{Confidence: 0.85}, nil
}

// ---------------------------------------------------------------------------
// Shared Go content-pattern detection
// ---------------------------------------------------------------------------

// goContentHasPattern reads up to maxLines from each .go file in the top-level
// of projectRoot and returns true if any matches the given regexp.
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

// extractGoLastArg returns the last comma-separated argument in a raw argument
// string. This extracts the handler from calls like "authMiddleware, myHandler"
// where the handler is the final argument.
func extractGoLastArg(argsRaw string) string {
	// The regex capture for args can contain trailing characters from the
	// function call; trim whitespace and trailing ) or , chars.
	argsRaw = strings.TrimRight(argsRaw, " \t\n\r)")
	parts := strings.Split(argsRaw, ",")
	last := strings.TrimSpace(parts[len(parts)-1])
	return last
}
