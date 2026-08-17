// Package frameworks recovers the routes and handlers a web framework wires up
// at runtime, which generic extraction cannot see: a route is a string and a
// callback, not a declaration.
//
// Each resolver implements resolution.FrameworkResolver; Extract and
// PostExtract are optional and reached by type assertion, so a resolver that
// only resolves need not stub them.
//
// Adding a framework means appending to allResolvers and adding a
// <framework>.go / <framework>_test.go pair here.
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

// jsExtToLanguage duplicates the JS/TS slice of the indexer's extension table.
// That table is unexported, and importing it here would cycle.
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

// langFromFilePath falls back to JavaScript on an unknown extension, so a
// JS/TS resolver stays functional rather than emitting unknown-language nodes.
func langFromFilePath(filePath string) types.Language {
	ext := strings.ToLower(filepath.Ext(filePath))
	if lang, ok := jsExtToLanguage[ext]; ok {
		return lang
	}
	return types.LanguageJavaScript
}

// MakeRouteNode is the one place route node ids are formatted, so every
// resolver produces identical shapes. Exported so tests can assert the format
// without going through a resolver.
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
		// A route is always an entry point.
		IsExported: true,
	}
}

// FileInput is a (filePath, content) pair passed to ExtractAndPersist.
type FileInput struct {
	Path    string
	Content string
}

// Registry holds the ordered resolver list plus the project context Detect
// and Extract need.
type Registry struct {
	projectRoot string
	db          *db.DB
	resolvers   []resolution.FrameworkResolver
}

// NewRegistry seeds every framework resolver. Pass d=nil for Detect-only use.
func NewRegistry(projectRoot string, d *db.DB) *Registry {
	r := &Registry{
		projectRoot: projectRoot,
		db:          d,
	}
	r.resolvers = r.allResolvers()
	return r
}

// allResolvers is grouped by language cluster; within a language, insertion
// order is resolution priority.
func (r *Registry) allResolvers() []resolution.FrameworkResolver {
	return []resolution.FrameworkResolver{
		NewExpressResolverWithDB(r.projectRoot, r.db),
		// Python
		NewDjangoResolver(r.projectRoot),
		NewFlaskResolver(r.projectRoot),
		NewFastAPIResolver(r.projectRoot),
		// Go
		NewGinResolver(r.projectRoot),
		NewEchoResolver(r.projectRoot),
		NewFiberResolver(r.projectRoot),
		NewGorillaResolver(r.projectRoot),
		NewChiResolver(r.projectRoot),
		// Node / JS-TS
		NewNestJSResolver(r.projectRoot),
		NewKoaResolver(r.projectRoot),
		NewHapiResolver(r.projectRoot),
		NewFastifyResolver(r.projectRoot),
		NewSailsResolver(r.projectRoot),
		NewAdonisResolver(r.projectRoot),
		// Rust
		NewActixResolver(r.projectRoot),
		NewAxumResolver(r.projectRoot),
		NewRocketResolver(r.projectRoot),
		// Java / Kotlin
		NewSpringResolver(r.projectRoot),
		// Ruby
		NewRailsResolver(r.projectRoot),
		// PHP
		NewLaravelResolver(r.projectRoot),
		NewSymfonyResolver(r.projectRoot),
		// Elixir — no types.Language of its own, so it uses LanguageUnknown.
		NewPhoenixResolver(r.projectRoot),
	}
}

// DetectFrameworks narrows to the frameworks actually present. Called once per
// pipeline session, after the index is ready.
func (r *Registry) DetectFrameworks(ctx context.Context) []resolution.FrameworkResolver {
	var active []resolution.FrameworkResolver
	for _, res := range r.resolvers {
		if res.Detect(ctx) {
			active = append(active, res)
		}
	}
	return active
}

// GetApplicableFrameworks returns resolvers handling lang, plus any whose
// Languages is nil (meaning any language).
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

// FrameworkRegistry adapts the resolver list for NewPipelineWithSeams.
func (r *Registry) FrameworkRegistry() resolution.FrameworkRegistry {
	return resolution.FrameworkRegistry(r.resolvers)
}

// ExtractAndPersist writes route nodes and their handler refs, one transaction
// per file. Must run before ResolveAndPersistBatched, which is what turns
// those refs into edges.
func (r *Registry) ExtractAndPersist(ctx context.Context, files []FileInput) error {
	if r.db == nil {
		return nil
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

	// PostExtract runs only after every file, since it aggregates across them.
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

// stripJSComments blanks comments while preserving line and column positions,
// so a regex match still reports the source location. It does not handle
// comment-like sequences inside template literals.
func stripJSComments(src string) string {
	var out strings.Builder
	lines := strings.Split(src, "\n")
	inBlock := false
	for _, line := range lines {
		if inBlock {
			end := strings.Index(line, "*/")
			if end >= 0 {
				inBlock = false
				line = strings.Repeat(" ", end+2) + line[end+2:]
			} else {
				out.WriteByte('\n')
				continue
			}
		}
		result := removeInlineComments(line, &inBlock)
		out.WriteString(result)
		out.WriteByte('\n')
	}
	return out.String()
}

// removeInlineComments updates inBlock as it crosses block delimiters.
func removeInlineComments(line string, inBlock *bool) string {
	var b strings.Builder
	i := 0
	for i < len(line) {
		if i+1 < len(line) && line[i] == '/' && line[i+1] == '/' {
			break
		}
		if i+1 < len(line) && line[i] == '/' && line[i+1] == '*' {
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

// jsReservedInlineNames are identifiers common inside handler bodies that are
// never handlers themselves — built-ins and the req/res idiom.
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

// readFirstNLines lets Detect pattern-match imports without reading whole files.
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

var expressHTTPMethods = []string{
	"get", "post", "put", "delete", "patch", "all", "use",
}

// routeRegexp matches `app.get('/p', handler)` and its router/quote variants.
// Groups: method, path, first handler argument.
var routeRegexp = regexp.MustCompile(
	`(?m)(?:app|router)\.(get|post|put|delete|patch|all|use)\s*\(\s*` +
		`['"]([^'"]+)['"]\s*,\s*` +
		`([^)]+)`,
)

var expressRequireRegexp = regexp.MustCompile(`require\s*\(\s*['"]express['"]\s*\)`)

var expressImportRegexp = regexp.MustCompile(`from\s+['"]express['"]`)

var expressRouterCallRegexp = regexp.MustCompile(`(?:app|router)\.(get|post|put|delete|patch|use)\s*\(`)

// ExpressResolver handles Express.js and Express.Router routes.
type ExpressResolver struct {
	projectRoot string
	db          *db.DB
	// claimed accumulates handler names during Extract; ClaimsReference reads
	// it so the pipeline pre-filter admits those refs.
	claimed map[string]bool
}

// NewExpressResolver builds a DB-less resolver, enough for Detect and Extract.
func NewExpressResolver(projectRoot string) *ExpressResolver {
	return &ExpressResolver{
		projectRoot: projectRoot,
		claimed:     make(map[string]bool),
	}
}

// NewExpressResolverWithDB adds the DB access Resolve needs.
func NewExpressResolverWithDB(projectRoot string, d *db.DB) *ExpressResolver {
	return &ExpressResolver{
		projectRoot: projectRoot,
		db:          d,
		claimed:     make(map[string]bool),
	}
}

func (e *ExpressResolver) Name() string { return "express" }

func (e *ExpressResolver) Languages() []types.Language {
	return []types.Language{
		types.LanguageTypeScript,
		types.LanguageJavaScript,
		types.LanguageTSX,
		types.LanguageJSX,
	}
}

// Detect reads package.json first, then falls back to source patterns. The
// fallback scans only top-level files: a full directory walk at detection
// time would cost more than the frameworks it would find.
func (e *ExpressResolver) Detect(ctx context.Context) bool {
	// Matching the JSON key form keeps "express-validator" from tripping this.
	pkgPath := filepath.Join(e.projectRoot, "package.json")
	if data, err := os.ReadFile(pkgPath); err == nil {
		s := string(data)
		if strings.Contains(s, `"express":`) {
			return true
		}
	}

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

func isJSFile(name string) bool {
	switch filepath.Ext(name) {
	case ".js", ".ts", ".jsx", ".tsx", ".mjs", ".cjs":
		return true
	}
	return false
}

// Extract returns one route node per registration, plus a reference to the
// named handler or, for an inline body, calls refs for what that body invokes.
// It also fills the claimed set that ClaimsReference reads.
func (e *ExpressResolver) Extract(filePath, content string) ([]types.Node, []types.UnresolvedReference) {
	stripped := stripJSComments(content)
	lang := langFromFilePath(filePath)

	totalLines := strings.Count(content, "\n") + 1

	var nodes []types.Node
	var refs []types.UnresolvedReference

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
		if line > totalLines {
			line = totalLines
		}

		node := MakeRouteNode(filePath, line, methodStr, routePath, lang)
		nodes = append(nodes, node)

		isInline := strings.HasPrefix(handlerRaw, "function") ||
			strings.HasPrefix(handlerRaw, "(") ||
			strings.Contains(handlerRaw, "=>")

		if !isInline {
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
			// routeRegexp's [^)]+ stops at the first ')', truncating the body,
			// so recover it by rescanning the source with paren tracking.
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

// extractInlineBody returns the handler text from start to the bracket that
// closes the enclosing call, tracking nesting depth as it scans.
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
				return src[start:i]
			}
			depth--
		}
	}
	return src[start:]
}

// extractIdentifier returns the leading identifier from s.
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

var callExprRegexp = regexp.MustCompile(`\b([A-Za-z_$][A-Za-z0-9_$]*)\s*\(`)

// extractCallsFromBody turns an inline handler's call sites into refs, minus
// the reserved names.
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

// ClaimsReference reports a handler name seen during Extract.
func (e *ExpressResolver) ClaimsReference(name string) bool {
	return e.claimed[name]
}

// Resolve looks up a claimed handler by name. Returns an empty TargetNodeID
// when the DB is absent or nothing matches.
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

	best := nodes[0]
	for _, n := range nodes {
		if n.Language == ref.Language {
			best = n
			break
		}
	}

	// Below the pipeline's 0.9 early-return threshold: a name match is strong
	// evidence, but the pipeline should still weigh other candidates.
	return resolution.ResolvedRef{
		TargetNodeID: best.ID,
		Confidence:   0.85,
	}, nil
}
