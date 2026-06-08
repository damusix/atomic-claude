// Package frameworks implements the FrameworkResolver registry and the Express
// resolver (CP14 template), plus all CP15 batches A–E (Python, Go, Node/JS-TS,
// Rust/Java, and PHP/Ruby/Elixir resolvers). CP15 is complete — all 23
// framework resolvers are registered.
//
// # File input type
//
// FileInput is the named type for (path, content) pairs passed to
// ExtractAndPersist. Tests and callers use this type directly.
//
// # Architecture
//
// Each resolver implements resolution.FrameworkResolver (Name/Languages/
// Detect/ClaimsReference/Resolve). Extract and PostExtract are optional — a
// resolver that only does Resolve need not stub them; the Registry checks for
// resolution.FrameworkExtractor and resolution.FrameworkPostExtractor via type
// assertion at ExtractAndPersist time.
//
// # Registry construction
//
// NewRegistry(projectRoot, db) creates the Registry seeded with every
// framework resolver. DetectFrameworks filters to the ones whose Detect(ctx)
// returns true. GetApplicableFrameworks filters by language. FrameworkRegistry()
// returns the full list as resolution.FrameworkRegistry for NewPipelineWithSeams.
//
// # Route node format (appendix H — verbatim)
//
//   - id:            route:{filePath}:{line}:{METHOD}:{path}
//   - qualifiedName: {filePath}::METHOD:{path}
//   - name:          "METHOD /path"  (e.g. "GET /users/:id")
//
// MakeRouteNode is exported so tests can assert the exact format independently.
//
// # CP15 complete — all 23 resolvers registered
//
// allResolvers() holds all framework resolvers in language-cluster order.
// Batch A (django, flask, fastapi), B (gin, echo, fiber, gorilla, chi),
// C (nestjs, koa, hapi, fastify, sails, adonisjs), D (actix, axum, rocket,
// spring), and E (rails, laravel, symfony, phoenix) are all registered.
// To add a future resolver, append to allResolvers() and create a new
// <framework>.go + <framework>_test.go pair in this package.
package frameworks

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/db"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/resolution"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// ---------------------------------------------------------------------------
// Extension → language mapper (appendix D subset for JS/TS family)
// ---------------------------------------------------------------------------

// jsExtToLanguage maps lower-case file extensions to the canonical
// types.Language for the JS/TS family. The source of truth is the indexer's
// extToLanguage table (appendix D); this local copy covers only the extensions
// that framework resolvers care about. The indexer's table is unexported, so
// we keep a small local copy here rather than duplicating the full table or
// creating a cross-package import cycle.
var jsExtToLanguage = map[string]types.Language{
	".ts":  types.LanguageTypeScript,
	".mts": types.LanguageTypeScript,
	".cts": types.LanguageTypeScript,
	".tsx": types.LanguageTSX,
	".jsx": types.LanguageJSX,
	".js":  types.LanguageJavaScript,
	".mjs": types.LanguageJavaScript,
	".cjs": types.LanguageJavaScript,
}

// langFromFilePath infers the types.Language from a file path's extension.
// Falls back to LanguageJavaScript for unrecognised extensions so that
// JS/TS framework resolvers remain functional on ambiguous inputs.
func langFromFilePath(filePath string) types.Language {
	ext := strings.ToLower(filepath.Ext(filePath))
	if lang, ok := jsExtToLanguage[ext]; ok {
		return lang
	}
	return types.LanguageJavaScript
}

// ---------------------------------------------------------------------------
// Route node helpers (appendix H verbatim)
// ---------------------------------------------------------------------------

// MakeRouteNode constructs a types.Node for an HTTP route with the exact
// id / qualifiedName / name format mandated by appendix H.
//
//	id:            route:{filePath}:{line}:{METHOD}:{path}
//	qualifiedName: {filePath}::METHOD:{path}
//	name:          "METHOD /path"
func MakeRouteNode(filePath string, line int, method, path string, lang types.Language) types.Node {
	id := fmt.Sprintf("route:%s:%d:%s:%s", filePath, line, method, path)
	qualifiedName := fmt.Sprintf("%s::METHOD:%s", filePath, path)
	name := fmt.Sprintf("%s %s", method, path)
	return types.Node{
		ID:            id,
		Kind:          types.NodeKindRoute,
		Name:          name,
		QualifiedName: qualifiedName,
		FilePath:      filePath,
		Language:      lang,
		StartLine:     line,
		EndLine:       line,
		IsExported:    true, // routes are entry points
	}
}

// ---------------------------------------------------------------------------
// FileInput — named type for (path, content) pairs
// ---------------------------------------------------------------------------

// FileInput is a (filePath, content) pair passed to ExtractAndPersist.
// Using a named type avoids anonymous-struct type-identity mismatches between
// the caller and the function signature.
type FileInput struct {
	Path    string
	Content string
}

// ---------------------------------------------------------------------------
// Registry
// ---------------------------------------------------------------------------

// Registry holds the full ordered list of framework resolvers and the project
// context needed for Detect and Extract.
type Registry struct {
	projectRoot string
	db          *db.DB
	resolvers   []resolution.FrameworkResolver
}

// NewRegistry creates a Registry pre-seeded with all 23 framework resolvers.
// Pass db=nil when no DB access is needed (e.g. in Detect-only flows).
//
// CP15 is complete: all batches A–E are registered (batch E = laravel, symfony,
// rails, phoenix).
func NewRegistry(projectRoot string, d *db.DB) *Registry {
	r := &Registry{
		projectRoot: projectRoot,
		db:          d,
	}
	r.resolvers = r.allResolvers()
	return r
}

// allResolvers returns the full ordered registry of all 23 framework resolvers.
//
// Express is first (CP14 template). CP15 batch A adds the three Python
// frameworks; batch B adds the five Go frameworks; batch C adds the six
// Node/JS-TS frameworks; batch D adds the Rust and Java frameworks; batch E
// adds the Ruby, PHP, and Elixir (Phoenix) frameworks. CP15 complete.
// Insertion order = resolution priority within a language.
func (r *Registry) allResolvers() []resolution.FrameworkResolver {
	return []resolution.FrameworkResolver{
		NewExpressResolverWithDB(r.projectRoot, r.db),
		// CP15 batch A — Python cluster
		NewDjangoResolver(r.projectRoot),
		NewFlaskResolver(r.projectRoot),
		NewFastAPIResolver(r.projectRoot),
		// CP15 batch B — Go cluster
		NewGinResolver(r.projectRoot),
		NewEchoResolver(r.projectRoot),
		NewFiberResolver(r.projectRoot),
		NewGorillaResolver(r.projectRoot),
		NewChiResolver(r.projectRoot),
		// CP15 batch C — Node/JS-TS cluster
		NewNestJSResolver(r.projectRoot),
		NewKoaResolver(r.projectRoot),
		NewHapiResolver(r.projectRoot),
		NewFastifyResolver(r.projectRoot),
		NewSailsResolver(r.projectRoot),
		NewAdonisResolver(r.projectRoot),
		// CP15 batch D — Rust cluster
		NewActixResolver(r.projectRoot),
		NewAxumResolver(r.projectRoot),
		NewRocketResolver(r.projectRoot),
		// CP15 batch D — Java/Kotlin cluster
		NewSpringResolver(r.projectRoot),
		// CP15 batch E — Ruby cluster
		NewRailsResolver(r.projectRoot),
		// CP15 batch E — PHP cluster
		NewLaravelResolver(r.projectRoot),
		NewSymfonyResolver(r.projectRoot),
		// CP15 batch E — Elixir cluster (uses LanguageUnknown; Elixir absent from appendix C)
		NewPhoenixResolver(r.projectRoot),
	}
}

// DetectFrameworks returns the subset of resolvers for which Detect returns
// true. This is called once per pipeline session (after the index is ready)
// to determine which frameworks are active in the project.
func (r *Registry) DetectFrameworks(ctx context.Context) []resolution.FrameworkResolver {
	var active []resolution.FrameworkResolver
	for _, res := range r.resolvers {
		if res.Detect(ctx) {
			active = append(active, res)
		}
	}
	return active
}

// GetApplicableFrameworks returns all resolvers that handle the given language
// (i.e. whose Languages() includes lang, or whose Languages() is nil meaning
// "any language").
func (r *Registry) GetApplicableFrameworks(lang types.Language) []resolution.FrameworkResolver {
	var result []resolution.FrameworkResolver
	for _, res := range r.resolvers {
		langs := res.Languages()
		if langs == nil {
			result = append(result, res)
			continue
		}
		for _, l := range langs {
			if l == lang {
				result = append(result, res)
				break
			}
		}
	}
	return result
}

// FrameworkRegistry returns the full resolver list as a resolution.FrameworkRegistry
// (an alias for []resolution.FrameworkResolver). Pass this to
// resolution.NewPipelineWithSeams so the pipeline uses framework resolution.
func (r *Registry) FrameworkRegistry() resolution.FrameworkRegistry {
	return resolution.FrameworkRegistry(r.resolvers)
}

// ExtractAndPersist runs the Extract step for each active framework resolver
// over the provided files, persisting route nodes and unresolved handler
// references to the DB in one transaction per file. Call this BEFORE
// resolution.Pipeline.ResolveAndPersistBatched so the route nodes and their
// handler refs are in the DB when resolution runs.
//
// files is a slice of (path, content) pairs — the same files the generic
// extractor already processed. The framework layer adds route nodes on top.
//
// This method is the "ExtractFrameworkNodes" seam (brief §5). The engine
// facade (CP20) will call it as part of the index pipeline.
func (r *Registry) ExtractAndPersist(ctx context.Context, files []FileInput) error {
	if r.db == nil {
		return nil // no-op in DB-less mode
	}

	active := r.DetectFrameworks(ctx)
	if len(active) == 0 {
		return nil
	}

	for _, f := range files {
		for _, res := range active {
			ext, ok := res.(resolution.FrameworkExtractor)
			if !ok {
				continue
			}
			nodes, refs := ext.Extract(f.Path, f.Content)
			if len(nodes) == 0 && len(refs) == 0 {
				continue
			}
			// Persist route nodes + unresolved refs in one transaction.
			if err := r.db.WithTx(ctx, func(tx *db.Tx) error {
				for _, n := range nodes {
					if err := tx.UpsertNodeAt(ctx, n, 0); err != nil {
						return err
					}
				}
				for _, ref := range refs {
					if err := tx.InsertUnresolvedRef(ctx, ref); err != nil {
						return err
					}
				}
				return nil
			}); err != nil {
				return fmt.Errorf("frameworks: ExtractAndPersist %s: %w", f.Path, err)
			}
		}
	}

	// Run PostExtract for resolvers that implement it.
	for _, res := range active {
		pe, ok := res.(resolution.FrameworkPostExtractor)
		if !ok {
			continue
		}
		extraNodes, err := pe.PostExtract(ctx)
		if err != nil {
			return fmt.Errorf("frameworks: PostExtract %s: %w", res.Name(), err)
		}
		if len(extraNodes) == 0 {
			continue
		}
		if err := r.db.WithTx(ctx, func(tx *db.Tx) error {
			for _, n := range extraNodes {
				if err := tx.UpsertNodeAt(ctx, n, 0); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			return fmt.Errorf("frameworks: PostExtract persist %s: %w", res.Name(), err)
		}
	}

	return nil
}

// ---------------------------------------------------------------------------
// Comment stripping helper (shared by all JS/TS framework resolvers)
// ---------------------------------------------------------------------------

// stripJSComments removes single-line (//) and multi-line (/* */) comments
// from JS/TS source, preserving line numbers so regex matches return the
// correct original line. The implementation is line-oriented and handles the
// common cases; it does not handle nested comments (JS has none) or template
// literals with comment-like sequences (rare in route files).
func stripJSComments(src string) string {
	var out strings.Builder
	lines := strings.Split(src, "\n")
	inBlock := false
	for _, line := range lines {
		if inBlock {
			end := strings.Index(line, "*/")
			if end >= 0 {
				inBlock = false
				// Replace the consumed part with spaces to preserve column positions.
				line = strings.Repeat(" ", end+2) + line[end+2:]
			} else {
				out.WriteByte('\n')
				continue
			}
		}
		// Scan for // and /* in the remaining line.
		result := removeInlineComments(line, &inBlock)
		out.WriteString(result)
		out.WriteByte('\n')
	}
	return out.String()
}

// removeInlineComments handles a single line, modifying inBlock as it finds
// block-comment delimiters.
func removeInlineComments(line string, inBlock *bool) string {
	var b strings.Builder
	i := 0
	for i < len(line) {
		if i+1 < len(line) && line[i] == '/' && line[i+1] == '/' {
			// Single-line comment — rest of line is comment.
			break
		}
		if i+1 < len(line) && line[i] == '/' && line[i+1] == '*' {
			// Start of block comment.
			*inBlock = true
			end := strings.Index(line[i+2:], "*/")
			if end >= 0 {
				*inBlock = false
				i = i + 2 + end + 2
				continue
			}
			break
		}
		b.WriteByte(line[i])
		i++
	}
	return b.String()
}

// ---------------------------------------------------------------------------
// Reserved JS/TS names (inline body — not real handler references)
// ---------------------------------------------------------------------------

// jsReservedInlineNames is the set of identifiers that appear inside route
// handler bodies but are not themselves handlers (built-ins, common patterns).
// Used by Extract's inline-body call extraction to skip non-meaningful refs.
var jsReservedInlineNames = map[string]bool{
	"console": true, "process": true, "require": true,
	"module": true, "exports": true, "Error": true,
	"JSON": true, "Math": true, "Object": true,
	"Array": true, "String": true, "Number": true,
	"Boolean": true, "Promise": true, "setTimeout": true,
	"setInterval": true, "clearTimeout": true, "clearInterval": true,
	"parseInt": true, "parseFloat": true, "isNaN": true,
	"undefined": true, "null": true, "true": true, "false": true,
	"next": true, "res": true, "req": true, "err": true,
	"send": true, "json": true, "status": true, "end": true,
	"render": true, "redirect": true, "set": true, "get": true,
	"use": true, "Router": true, "express": true, "app": true,
	"router": true,
}

// readFirstNLines reads up to n lines from file at path, returning them joined.
// Used for content-pattern detection without loading whole files.
func readFirstNLines(path string, n int) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	var lines []string
	sc := bufio.NewScanner(f)
	for sc.Scan() && len(lines) < n {
		lines = append(lines, sc.Text())
	}
	return strings.Join(lines, "\n")
}

// ---------------------------------------------------------------------------
// Express resolver
// ---------------------------------------------------------------------------

// expressHTTPMethods is the set of Express route-registering method names.
var expressHTTPMethods = []string{
	"get", "post", "put", "delete", "patch", "all", "use",
}

// routeRegexp matches Express route registrations of the form:
//
//	app.METHOD('path', handler)
//	router.METHOD('path', handler)
//	app.METHOD("path", handler)
//
// Capture groups:
//
//	1 — HTTP METHOD (lowercased)
//	2 — route path (single- or double-quoted)
//	3 — first handler argument (identifier, or start of inline function)
var routeRegexp = regexp.MustCompile(
	`(?m)(?:app|router)\.(get|post|put|delete|patch|all|use)\s*\(\s*` +
		`['"]([^'"]+)['"]\s*,\s*` +
		`([^)]+)`,
)

// expressRequireRegexp matches `require('express')` or `require("express")`.
var expressRequireRegexp = regexp.MustCompile(`require\s*\(\s*['"]express['"]\s*\)`)

// expressImportRegexp matches `import ... from 'express'` or `"express"`.
var expressImportRegexp = regexp.MustCompile(`from\s+['"]express['"]`)

// expressRouterCallRegexp matches `app.get(` or `router.post(` etc. as a
// content-based detection fallback.
var expressRouterCallRegexp = regexp.MustCompile(`(?:app|router)\.(get|post|put|delete|patch|use)\s*\(`)

// ExpressResolver implements resolution.FrameworkResolver + FrameworkExtractor
// for Express.js / Express.Router routes.
type ExpressResolver struct {
	projectRoot string
	db          *db.DB
	// claimed is the set of handler names seen during Extract. It backs
	// ClaimsReference so the pipeline pre-filter passes handler refs.
	claimed map[string]bool
}

// NewExpressResolver creates an ExpressResolver without a DB. Useful for
// Detect and Extract calls when no resolution is needed.
func NewExpressResolver(projectRoot string) *ExpressResolver {
	return &ExpressResolver{
		projectRoot: projectRoot,
		claimed:     make(map[string]bool),
	}
}

// NewExpressResolverWithDB creates an ExpressResolver that can perform DB
// lookups during Resolve.
func NewExpressResolverWithDB(projectRoot string, d *db.DB) *ExpressResolver {
	return &ExpressResolver{
		projectRoot: projectRoot,
		db:          d,
		claimed:     make(map[string]bool),
	}
}

// Name returns "express".
func (e *ExpressResolver) Name() string { return "express" }

// Languages returns the JS/TS family supported by Express.
func (e *ExpressResolver) Languages() []types.Language {
	return []types.Language{
		types.LanguageTypeScript,
		types.LanguageJavaScript,
		types.LanguageTSX,
		types.LanguageJSX,
	}
}

// Detect returns true when the project uses Express:
//  1. package.json lists "express" as a dependency or devDependency.
//  2. Fallback: any .js/.ts file in projectRoot (top-level) uses
//     require('express') / from 'express' / app.get( / router.post(.
//
// The fallback is intentionally modest — only top-level files are scanned to
// avoid a full directory walk at detection time.
func (e *ExpressResolver) Detect(ctx context.Context) bool {
	// Primary: package.json — match the JSON key form ("express":) so that
	// substrings like "express-validator" or devDep names don't trip this check.
	pkgPath := filepath.Join(e.projectRoot, "package.json")
	if data, err := os.ReadFile(pkgPath); err == nil {
		s := string(data)
		if strings.Contains(s, `"express":`) {
			return true
		}
	}

	// Fallback: scan top-level .js/.ts/.jsx/.tsx files for express patterns.
	entries, err := os.ReadDir(e.projectRoot)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !isJSFile(name) {
			continue
		}
		snippet := readFirstNLines(filepath.Join(e.projectRoot, name), 30)
		if expressRequireRegexp.MatchString(snippet) ||
			expressImportRegexp.MatchString(snippet) ||
			expressRouterCallRegexp.MatchString(snippet) {
			return true
		}
	}
	return false
}

// isJSFile returns true for .js/.ts/.jsx/.tsx file names.
func isJSFile(name string) bool {
	switch filepath.Ext(name) {
	case ".js", ".ts", ".jsx", ".tsx", ".mjs", ".cjs":
		return true
	}
	return false
}

// Extract scans filePath/content for Express route registrations and returns:
//   - One route node per matched route (id/qn/name per appendix H).
//   - For named-handler routes (e.g. `router.get('/p', myHandler)`): one
//     references UnresolvedReference from the route node to the handler name.
//   - For inline-body routes (e.g. `router.get('/p', function(req,res){...})`):
//     calls refs for call sites in the body, minus reserved/builtin names.
//
// Extract also populates the internal claimed set used by ClaimsReference.
func (e *ExpressResolver) Extract(filePath, content string) ([]types.Node, []types.UnresolvedReference) {
	stripped := stripJSComments(content)
	// Infer language from the file extension so that .ts/.tsx/.jsx files get
	// the correct Language value instead of always LanguageJavaScript.
	lang := langFromFilePath(filePath)

	// Total line count for bounds clamping — O(1) scan, no allocation.
	totalLines := strings.Count(content, "\n") + 1

	var nodes []types.Node
	var refs []types.UnresolvedReference

	// Compute line number for a byte offset in stripped content.
	lineOf := func(offset int) int {
		return strings.Count(stripped[:offset], "\n") + 1
	}

	matches := routeRegexp.FindAllStringSubmatchIndex(stripped, -1)
	for _, loc := range matches {
		if len(loc) < 8 {
			continue
		}
		methodStr := strings.ToUpper(stripped[loc[2]:loc[3]])
		routePath := stripped[loc[4]:loc[5]]
		handlerRaw := strings.TrimSpace(stripped[loc[6]:loc[7]])

		line := lineOf(loc[0])
		// Clamp line to valid range.
		if line > totalLines {
			line = totalLines
		}

		// Build the route node using the exact appendix-H format.
		node := MakeRouteNode(filePath, line, methodStr, routePath, lang)
		nodes = append(nodes, node)

		// Determine handler type:
		// - inline function/arrow: starts with "function" or has "=>"
		//   → emit calls refs for identifiers in the body, skip reserved.
		// - named handler: a plain identifier
		//   → emit one references ref.
		isInline := strings.HasPrefix(handlerRaw, "function") ||
			strings.HasPrefix(handlerRaw, "(") ||
			strings.Contains(handlerRaw, "=>")

		if !isInline {
			// Named handler: extract the identifier (stop at comma, space, newline, paren).
			handlerName := extractIdentifier(handlerRaw)
			if handlerName != "" && !jsReservedInlineNames[handlerName] {
				e.claimed[handlerName] = true
				refs = append(refs, types.UnresolvedReference{
					ID:            fmt.Sprintf("ref:%s:%d:%s", filePath, line, handlerName),
					FromNodeID:    node.ID,
					ReferenceName: handlerName,
					ReferenceKind: types.EdgeKindReferences,
					Line:          line,
					FilePath:      filePath,
					Language:      lang,
				})
			}
		} else {
			// Inline body: the regex captures only up to the first ')' in the
			// handler argument (because [^)]+ stops there). To get the actual
			// function body we scan forward from the start of the handler
			// argument in the stripped source, tracking paren depth until the
			// route registration's closing paren is consumed.
			// This is an approximation (no full AST); sufficient for the template.
			bodyText := extractInlineBody(stripped, loc[6])
			callRefs := extractCallsFromBody(filePath, node.ID, line, bodyText, lang)
			for _, cr := range callRefs {
				e.claimed[cr.ReferenceName] = true
			}
			refs = append(refs, callRefs...)
		}
	}

	return nodes, refs
}

// extractInlineBody returns the full text of an inline handler starting at
// offset start in src. It scans forward tracking paren/brace/bracket depth
// until the outer call's closing ')' is found, then returns everything from
// start to that position. This recovers the complete body text that the
// routeRegexp [^)]+ capture group truncates at the first ')'.
func extractInlineBody(src string, start int) string {
	if start >= len(src) {
		return ""
	}
	depth := 0
	for i := start; i < len(src); i++ {
		switch src[i] {
		case '(', '{', '[':
			depth++
		case ')', '}', ']':
			if depth == 0 {
				// We've closed the outer route call's argument list.
				return src[start:i]
			}
			depth--
		}
	}
	return src[start:]
}

// extractIdentifier returns the leading identifier from s, stopping at the
// first non-identifier character.
func extractIdentifier(s string) string {
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '_' || r == '$' {
			b.WriteRune(r)
		} else {
			break
		}
	}
	return b.String()
}

// callExprRegexp finds simple identifier call expressions: word followed by (.
var callExprRegexp = regexp.MustCompile(`\b([A-Za-z_$][A-Za-z0-9_$]*)\s*\(`)

// extractCallsFromBody extracts call-expression identifiers from a handler
// body text, skipping reserved names. Returns calls-kind UnresolvedReferences.
func extractCallsFromBody(
	filePath, fromNodeID string,
	baseLine int,
	body string,
	lang types.Language,
) []types.UnresolvedReference {
	var refs []types.UnresolvedReference
	seen := make(map[string]bool)
	for _, m := range callExprRegexp.FindAllStringSubmatch(body, -1) {
		name := m[1]
		if jsReservedInlineNames[name] || seen[name] {
			continue
		}
		seen[name] = true
		refs = append(refs, types.UnresolvedReference{
			ID:            fmt.Sprintf("ref:%s:%d:%s:inline", filePath, baseLine, name),
			FromNodeID:    fromNodeID,
			ReferenceName: name,
			ReferenceKind: types.EdgeKindCalls,
			Line:          baseLine,
			FilePath:      filePath,
			Language:      lang,
		})
	}
	return refs
}

// ClaimsReference returns true if a handler with this name was seen during
// Extract. This is the pre-filter used by resolveOne (appendix F step 2).
func (e *ExpressResolver) ClaimsReference(name string) bool {
	return e.claimed[name]
}

// Resolve resolves handler-name references that ClaimsReference accepted.
// It looks up the handler by name in the DB and returns confidence 0.85
// (midpoint of 0.8–0.9 per appendix H). Returns empty TargetNodeID when
// the DB is nil or no matching node is found.
func (e *ExpressResolver) Resolve(ctx context.Context, ref types.UnresolvedReference) (resolution.ResolvedRef, error) {
	if e.db == nil {
		return resolution.ResolvedRef{}, nil
	}
	if !e.claimed[ref.ReferenceName] {
		return resolution.ResolvedRef{}, nil
	}

	nodes, err := e.db.GetNodesByName(ctx, ref.ReferenceName, "")
	if err != nil {
		return resolution.ResolvedRef{}, fmt.Errorf("express.Resolve %q: %w", ref.ReferenceName, err)
	}
	if len(nodes) == 0 {
		return resolution.ResolvedRef{}, nil
	}

	// Prefer a node in the same language; otherwise take the first match.
	best := nodes[0]
	for _, n := range nodes {
		if n.Language == ref.Language {
			best = n
			break
		}
	}

	return resolution.ResolvedRef{
		TargetNodeID: best.ID,
		Confidence:   0.85, // midpoint of 0.8–0.9 (appendix H)
	}, nil
}
