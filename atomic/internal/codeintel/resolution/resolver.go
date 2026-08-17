// Package resolution turns unresolved references into graph edges: imports to
// their target files, names to their declarations, and — via the synthesis
// subpackage — dynamic dispatch the source never states outright.
//
// This file resolves imports only, and only classifies them; the caller owns
// edge creation and persistence.
//
// Contract: docs/spec/code-intel-resolution.md.
package resolution

import (
	"context"
	"path/filepath"
	"strings"
	"sync"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/db"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// REEXPORT_MAX_DEPTH bounds export-chain following. Barrel files can chain
// arbitrarily deep, and cycle detection alone would not bound the work.
const REEXPORT_MAX_DEPTH = 8

// ResolvedKind classifies the outcome of resolving one import reference.
type ResolvedKind int

const (
	// ResolvedKindInternal — a matching node exists in the DB.
	ResolvedKindInternal ResolvedKind = iota
	// ResolvedKindExternal — a package or stdlib module; no DB node is expected
	// or fabricated.
	ResolvedKindExternal
	// ResolvedKindUnresolved — looks internal, but nothing matches in the
	// current index. May resolve once more files are indexed.
	ResolvedKindUnresolved
)

func (k ResolvedKind) String() string {
	switch k {
	case ResolvedKindInternal:
		return "internal"
	case ResolvedKindExternal:
		return "external"
	default:
		return "unresolved"
	}
}

// ResolvedImport is the result of resolving one import reference.
type ResolvedImport struct {
	Kind ResolvedKind
	// TargetNodeID is the resolved file: node id, "" unless Kind is Internal.
	TargetNodeID string
	// Confidence is 0.0–1.0; 1.0 for exact DB hits.
	Confidence float64
	// PackageName is the shared identity every importer of one package
	// converges on, which is what lets the pipeline mint a single package
	// node. Set only for External JS-family imports; a URL-scheme specifier
	// yields no package and leaves this "".
	PackageName string
}

// Resolver resolves import-kind UnresolvedReferences.
// Construct with NewResolver or NewResolverWithProject.
type Resolver struct {
	db          *db.DB
	projectRoot string
	aliasMap    *AliasMap
	aliasOnce   sync.Once
}

// NewResolver builds a Resolver with tsconfig path-alias resolution disabled.
func NewResolver(d *db.DB) *Resolver {
	return &Resolver{db: d}
}

// NewResolverWithProject builds a Resolver that loads tsconfig/jsconfig path
// aliases from projectRoot.
func NewResolverWithProject(d *db.DB, projectRoot string) *Resolver {
	return &Resolver{db: d, projectRoot: projectRoot}
}

// aliases lazily loads the AliasMap, falling back to an empty one. The Once
// guards concurrent ResolveImport calls on the same Resolver.
func (r *Resolver) aliases(_ context.Context) *AliasMap {
	r.aliasOnce.Do(func() {
		if r.projectRoot == "" {
			r.aliasMap = &AliasMap{}
			return
		}
		am, err := LoadPathAliases(r.projectRoot)
		if err != nil || am == nil {
			r.aliasMap = &AliasMap{}
			return
		}
		r.aliasMap = am
	})
	return r.aliasMap
}

// ResolveImport resolves one import-kind reference. importerPath anchors
// relative specifiers.
func (r *Resolver) ResolveImport(ctx context.Context, ref types.UnresolvedReference, importerPath string) (ResolvedImport, error) {
	specifier := ref.ReferenceName
	lang := ref.Language

	// Before every later step, so all of them see a clean path.
	if isJSFamily(lang) {
		specifier = stripQueryAndFragment(specifier)
	}

	// Alias expansion must precede external classification, or an alias like
	// "@app/*" is misread as an npm scoped package.
	if !isRelative(specifier) {
		am := r.aliases(ctx)
		if am != nil {
			expanded := am.Resolve(specifier)
			if expanded != "" {
				nodeID, err := r.probeExtensions(ctx, expanded, lang)
				if err != nil {
					return ResolvedImport{Kind: ResolvedKindUnresolved}, err
				}
				if nodeID != "" {
					return ResolvedImport{Kind: ResolvedKindInternal, TargetNodeID: nodeID, Confidence: 1.0}, nil
				}
				// An alias match is internal by definition — never fall
				// through to external classification.
				return ResolvedImport{Kind: ResolvedKindUnresolved}, nil
			}
		}
	}

	if isExternal(specifier, lang) {
		result := ResolvedImport{Kind: ResolvedKindExternal}
		if isJSFamily(lang) {
			result.PackageName = npmPackageName(specifier)
		}
		return result, nil
	}

	importerDir := filepath.Dir(importerPath)
	base := filepath.Join(importerDir, specifier)
	// Join uses the OS separator; the DB stores forward slashes.
	base = filepath.ToSlash(base)

	nodeID, err := r.probeExtensions(ctx, base, lang)
	if err != nil {
		return ResolvedImport{Kind: ResolvedKindUnresolved}, err
	}
	if nodeID == "" {
		return ResolvedImport{Kind: ResolvedKindUnresolved}, nil
	}

	finalNodeID := r.followReExports(ctx, nodeID, nil, 0)

	return ResolvedImport{Kind: ResolvedKindInternal, TargetNodeID: finalNodeID, Confidence: 1.0}, nil
}

// probeExtensions returns the first candidate path present in the DB, or "".
func (r *Resolver) probeExtensions(ctx context.Context, base string, lang types.Language) (string, error) {
	candidates := extensionCandidates(base, lang)
	for _, path := range candidates {
		fileNodeID := "file:" + path
		n, err := r.db.GetNode(ctx, fileNodeID)
		if err == nil && n.ID == fileNodeID {
			return fileNodeID, nil
		}
		// Some indexers record a file row without a file: node.
		f, err2 := r.db.GetFileByPath(ctx, path)
		if err2 == nil && f != nil {
			return fileNodeID, nil
		}
	}
	return "", nil
}

// extensionCandidates expands an extensionless base path into the concrete
// paths to probe, in the language's own resolution order.
func extensionCandidates(base string, lang types.Language) []string {
	knownExts := map[string]bool{
		".ts": true, ".tsx": true, ".d.ts": true,
		".js": true, ".jsx": true,
		".py": true,
		".go": true, ".rs": true,
		".java": true, ".kt": true, ".scala": true,
		".rb": true, ".php": true, ".swift": true,
		".c": true, ".cpp": true, ".h": true, ".cs": true,
		".lua": true, ".dart": true,
	}
	ext := filepath.Ext(base)
	if knownExts[ext] {
		return []string{base}
	}

	switch lang {
	case types.LanguageTypeScript, types.LanguageTSX:
		return []string{
			base + ".ts",
			base + ".tsx",
			base + ".d.ts",
			base + "/index.ts",
			base + "/index.tsx",
			base + "/index.d.ts",
		}
	case types.LanguageJavaScript, types.LanguageJSX:
		return []string{
			base + ".js",
			base + ".jsx",
			base + "/index.js",
			base + "/index.jsx",
		}
	case types.LanguagePython:
		return []string{
			base + ".py",
			base + "/__init__.py",
		}
	case types.LanguageGo:
		// A Go import names a package directory, not a file.
		return []string{
			base,
			base + ".go",
		}
	case types.LanguageRust:
		return []string{
			base + ".rs",
			base + "/mod.rs",
		}
	case types.LanguageJava:
		return []string{
			base + ".java",
			base + "/package-info.java",
		}
	case types.LanguageKotlin:
		return []string{
			base + ".kt",
			base + ".kts",
		}
	case types.LanguageScala:
		return []string{
			base + ".scala",
			base + "/package.scala",
		}
	case types.LanguageRuby:
		return []string{
			base + ".rb",
			base + "/index.rb",
		}
	case types.LanguagePHP:
		return []string{
			base + ".php",
			base + "/index.php",
		}
	case types.LanguageSwift:
		return []string{
			base + ".swift",
		}
	default:
		return []string{
			base + ".ts",
			base + ".js",
			base + ".py",
			base + ".go",
			base + ".rs",
		}
	}
}

// followReExports walks exports edges to the deepest reachable node, so an
// import of a barrel file lands on the real definition. Returns startNodeID
// unchanged when it exports nothing, a cycle closes, or the depth cap hits.
func (r *Resolver) followReExports(ctx context.Context, startNodeID string, visited map[string]bool, depth int) string {
	if depth >= REEXPORT_MAX_DEPTH {
		return startNodeID
	}
	if visited == nil {
		visited = make(map[string]bool)
	}
	if visited[startNodeID] {
		return startNodeID
	}
	visited[startNodeID] = true

	edges, err := r.db.GetEdgesBySource(ctx, startNodeID)
	if err != nil {
		return startNodeID
	}

	for _, e := range edges {
		if e.Kind != types.EdgeKindExports {
			continue
		}
		if e.Target == startNodeID {
			continue
		}
		return r.followReExports(ctx, e.Target, visited, depth+1)
	}
	return startNodeID
}

func isRelative(specifier string) bool {
	return strings.HasPrefix(specifier, "./") || strings.HasPrefix(specifier, "../")
}

// isJSFamily gates the JS-only specifier rules: query/fragment stripping,
// npm package identity, and Node resolution semantics.
func isJSFamily(lang types.Language) bool {
	switch lang {
	case types.LanguageJavaScript, types.LanguageJSX,
		types.LanguageTypeScript, types.LanguageTSX:
		return true
	}
	return false
}

// stripQueryAndFragment drops the browser-ESM cache-busting suffix from
// "./editor.js?v=2". Position 0 is exempt: a leading "#" is a package.json
// "imports"-field subpath ("#internal/config"), not a fragment.
func stripQueryAndFragment(specifier string) string {
	if i := strings.IndexAny(specifier, "?#"); i > 0 {
		return specifier[:i]
	}
	return specifier
}

// npmPackageName reduces an external specifier to the package identity that
// subpath imports share, so "@scope/pkg/deep.js" and "@scope/pkg" converge.
// Only reached after isExternal, so relative and aliased specifiers cannot
// arrive here. Two deliberate non-reductions: a URL-scheme specifier yields ""
// (a CDN import has no package), and a "node:" specifier is kept whole rather
// than canonicalized against bare "fs", which would need a Node-version
// allowlist to do correctly.
func npmPackageName(specifier string) string {
	if strings.Contains(specifier, "://") {
		return ""
	}
	if strings.HasPrefix(specifier, "node:") {
		return specifier
	}
	if strings.HasPrefix(specifier, "@") {
		parts := strings.SplitN(specifier, "/", 3)
		if len(parts) >= 2 && parts[1] != "" {
			return parts[0] + "/" + parts[1]
		}
		return parts[0]
	}
	if i := strings.Index(specifier, "/"); i >= 0 {
		return specifier[:i]
	}
	return specifier
}

// isExternal reports a specifier for which no DB node is expected. Aliases are
// expanded by ResolveImport first, so nothing alias-matched arrives here.
func isExternal(specifier string, lang types.Language) bool {
	if isRelative(specifier) {
		return false
	}

	// "://" appears in no repo path or package name in any indexed language,
	// so this is safe ahead of the per-language branches.
	if strings.Contains(specifier, "://") {
		return true
	}

	if strings.HasPrefix(specifier, "node:") {
		return true
	}

	if strings.HasPrefix(specifier, "@") {
		return true
	}

	switch lang {
	case types.LanguageTypeScript, types.LanguageJavaScript,
		types.LanguageTSX, types.LanguageJSX:
		// Node resolution semantics in one rule: scoped packages, bare
		// packages, subpaths, and builtins are all external; "#" subpath
		// imports and paths are not.
		if !strings.HasPrefix(specifier, ".") && !strings.HasPrefix(specifier, "/") && !strings.HasPrefix(specifier, "#") {
			return true
		}

	case types.LanguagePython:
		// Relative imports are dotted; isRelative has already run.
		if !strings.Contains(specifier, ".") {
			return true
		}

	case types.LanguageGo:
		// A dot in the first segment means a module host — stdlib has none.
		parts := strings.SplitN(specifier, "/", 2)
		if !strings.Contains(parts[0], ".") {
			return true
		}

	case types.LanguageJava, types.LanguageKotlin, types.LanguageScala:
		for _, prefix := range []string{"java.", "javax.", "android.", "kotlin.", "scala.", "org.junit.", "org.springframework."} {
			if strings.HasPrefix(specifier, prefix) {
				return true
			}
		}

	case types.LanguageRust:
		// A bare crate name; anything with "::" is a path within one.
		if !strings.Contains(specifier, "::") && !strings.Contains(specifier, "/") {
			return true
		}
	}

	return false
}
