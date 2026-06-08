// Package resolution implements the import resolver (CP11) for the
// code-intelligence engine.
//
// # Scope (CP11 only)
//
// This package resolves import-kind UnresolvedReferences into target node IDs.
// Edge creation and persistence are CP13's responsibility — this package only
// returns a ResolvedImport describing what the target is.
//
// # Resolution strategy
//
//  1. External classification: if the specifier is a known external prefix
//     (no leading "./", "../", nor an alias prefix) → ResolvedKindExternal.
//  2. Alias expansion: if the specifier matches an alias in the AliasMap
//     (from tsconfig.json / jsconfig.json) → expand to a real path, then
//     probe the DB as if it were a relative import.
//  3. Relative resolution: join importer dir + specifier, then probe the DB
//     with per-language extension candidates.
//  4. Re-export chain: if the direct target is a file node that has exports
//     edges to other files, follow up to REEXPORT_MAX_DEPTH hops (cycle-safe).
//  5. If no DB node is found → ResolvedKindUnresolved.
//
// # Per-language extension candidates
//
//	TypeScript / TSX: .ts, .tsx, .d.ts, index.ts, index.tsx, index.d.ts
//	JavaScript / JSX: .js, .jsx, index.js, index.jsx
//	Python:           .py, /__init__.py (package)
//	Go:               (directory package; probe the dir itself as a file node)
//	Rust:             .rs, /mod.rs
//	Java / Kotlin / Scala: FQN → directory path heuristic
//	Others:           .ts, .js, .py, .go, .rs (broad fallback)
//
// # Re-export depth cap
//
//	REEXPORT_MAX_DEPTH = 8  (appendix F, named constant)
package resolution

import (
	"context"
	"path/filepath"
	"strings"
	"sync"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/db"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// REEXPORT_MAX_DEPTH is the maximum number of export-chain hops the resolver
// will follow before giving up (appendix F).
const REEXPORT_MAX_DEPTH = 8

// ResolvedKind classifies the outcome of resolving one import reference.
type ResolvedKind int

const (
	// ResolvedKindInternal means a matching node was found in the DB.
	ResolvedKindInternal ResolvedKind = iota
	// ResolvedKindExternal means the specifier refers to a node_modules package
	// or stdlib module — no DB node expected or fabricated.
	ResolvedKindExternal
	// ResolvedKindUnresolved means the specifier looks internal/aliased but no
	// matching node exists in the current index.
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
	// Kind classifies the resolution outcome.
	Kind ResolvedKind
	// TargetNodeID is the file: node id of the resolved target, or "" when Kind
	// is External or Unresolved.
	TargetNodeID string
	// Confidence is 0.0–1.0; set to 1.0 for exact DB hits.
	Confidence float64
}

// Resolver resolves import-kind UnresolvedReferences.
// Construct with NewResolver or NewResolverWithProject.
type Resolver struct {
	db          *db.DB
	projectRoot string // optional; used for alias loading
	aliasMap    *AliasMap
	aliasOnce   sync.Once
}

// NewResolver constructs a Resolver without a project root. Path-alias
// resolution (tsconfig paths) is disabled.
func NewResolver(d *db.DB) *Resolver {
	return &Resolver{db: d}
}

// NewResolverWithProject constructs a Resolver that will load tsconfig/jsconfig
// from projectRoot for path-alias resolution.
func NewResolverWithProject(d *db.DB, projectRoot string) *Resolver {
	return &Resolver{db: d, projectRoot: projectRoot}
}

// aliases returns the lazily-loaded AliasMap (or an empty one if projectRoot
// is not set or loading fails). Thread-safe: the sync.Once ensures the load
// runs exactly once even when multiple goroutines call ResolveImport on the
// same Resolver concurrently.
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

// ResolveImport resolves a single import-kind UnresolvedReference.
// importerPath is the file_path of the importing file (used for relative
// path join and language detection).
func (r *Resolver) ResolveImport(ctx context.Context, ref types.UnresolvedReference, importerPath string) (ResolvedImport, error) {
	specifier := ref.ReferenceName
	lang := ref.Language

	// Step 1 — alias expansion (tsconfig paths) for non-relative specifiers.
	// This MUST happen before external classification because an alias like
	// "@app/*" would otherwise be misclassified as an npm scoped package.
	if !isRelative(specifier) {
		am := r.aliases(ctx)
		if am != nil {
			expanded := am.Resolve(specifier)
			if expanded != "" {
				// Alias matched — treat as internal.
				nodeID, err := r.probeExtensions(ctx, expanded, lang)
				if err != nil {
					return ResolvedImport{Kind: ResolvedKindUnresolved}, err
				}
				if nodeID != "" {
					return ResolvedImport{Kind: ResolvedKindInternal, TargetNodeID: nodeID, Confidence: 1.0}, nil
				}
				// Alias matched but file not in DB yet → unresolved.
				return ResolvedImport{Kind: ResolvedKindUnresolved}, nil
			}
		}
	}

	// Step 2 — external classification (no alias matched above).
	if isExternal(specifier, lang) {
		return ResolvedImport{Kind: ResolvedKindExternal}, nil
	}

	// Step 3 — relative resolution.
	importerDir := filepath.Dir(importerPath)
	base := filepath.Join(importerDir, specifier)
	// Normalize slashes (filepath.Join uses OS sep; keep forward slashes for
	// consistency with how file paths are stored in the DB).
	base = filepath.ToSlash(base)

	// Probe with language-specific extension candidates.
	nodeID, err := r.probeExtensions(ctx, base, lang)
	if err != nil {
		return ResolvedImport{Kind: ResolvedKindUnresolved}, err
	}
	if nodeID == "" {
		return ResolvedImport{Kind: ResolvedKindUnresolved}, nil
	}

	// Step 4 — re-export chain (cycle-safe, depth-bounded).
	finalNodeID := r.followReExports(ctx, nodeID, nil, 0)

	return ResolvedImport{Kind: ResolvedKindInternal, TargetNodeID: finalNodeID, Confidence: 1.0}, nil
}

// ---------------------------------------------------------------------------
// Extension candidate probing
// ---------------------------------------------------------------------------

// probeExtensions tries the per-language extension candidate list against the
// DB and returns the first file: node id found, or "" if none match.
func (r *Resolver) probeExtensions(ctx context.Context, base string, lang types.Language) (string, error) {
	candidates := extensionCandidates(base, lang)
	for _, path := range candidates {
		fileNodeID := "file:" + path
		n, err := r.db.GetNode(ctx, fileNodeID)
		if err == nil && n.ID == fileNodeID {
			return fileNodeID, nil
		}
		// Also probe via GetFileByPath (some indexers may record only file
		// records without file: nodes; this is belt-and-suspenders).
		f, err2 := r.db.GetFileByPath(ctx, path)
		if err2 == nil && f != nil {
			return fileNodeID, nil
		}
	}
	return "", nil
}

// extensionCandidates returns the ordered list of concrete file paths to probe
// for the given base path (no extension) and language.
func extensionCandidates(base string, lang types.Language) []string {
	// If base already has a recognized extension, return it as-is first.
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
		// Go imports refer to package directories; try the dir itself and
		// conventional file names.
		return []string{
			base, // directory as a "file" node
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
		// Broad fallback: try the most common types.
		return []string{
			base + ".ts",
			base + ".js",
			base + ".py",
			base + ".go",
			base + ".rs",
		}
	}
}

// ---------------------------------------------------------------------------
// Re-export chain follower
// ---------------------------------------------------------------------------

// followReExports follows exports edges from startNodeID up to REEXPORT_MAX_DEPTH
// hops. visited tracks already-seen node IDs to break cycles.  Returns the
// deepest reachable node (may be startNodeID itself if no exports edges exist).
func (r *Resolver) followReExports(ctx context.Context, startNodeID string, visited map[string]bool, depth int) string {
	if depth >= REEXPORT_MAX_DEPTH {
		return startNodeID
	}
	if visited == nil {
		visited = make(map[string]bool)
	}
	if visited[startNodeID] {
		// Cycle detected — stop here.
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
			// Self-loop — skip.
			continue
		}
		return r.followReExports(ctx, e.Target, visited, depth+1)
	}
	return startNodeID
}

// ---------------------------------------------------------------------------
// External classification helpers
// ---------------------------------------------------------------------------

// isRelative returns true if the specifier starts with "./" or "../".
func isRelative(specifier string) bool {
	return strings.HasPrefix(specifier, "./") || strings.HasPrefix(specifier, "../")
}

// isExternal returns true when the specifier should be classified as an
// external/stdlib import for the given language — no DB node is expected.
//
// Classification rules:
//   - Any specifier that is not relative ("./", "../") AND not aliased is a
//     candidate. We check for explicit external signals here:
//   - "node:" protocol prefix → Node.js built-in.
//   - Specifiers without "/" or those starting with "@" (scoped npm packages).
//   - Per-language built-in skip sets (Go stdlib, Java stdlib, Python stdlib).
//
// Note: alias specifiers are handled before isExternal is called, so we only
// reach here for non-relative, non-aliased specifiers — they are external.
func isExternal(specifier string, lang types.Language) bool {
	// Already relative — not external.
	if isRelative(specifier) {
		return false
	}

	// node: protocol → Node.js built-in.
	if strings.HasPrefix(specifier, "node:") {
		return true
	}

	// Scoped npm package (@org/pkg) or bare npm package (no leading ".").
	if strings.HasPrefix(specifier, "@") {
		return true
	}

	// Language-specific known-stdlib prefixes.
	switch lang {
	case types.LanguageTypeScript, types.LanguageJavaScript,
		types.LanguageTSX, types.LanguageJSX:
		// Node.js stdlib modules (bare names, no "/").
		if isNodeBuiltin(specifier) {
			return true
		}
		// npm package: bare name without path separators (e.g. "react", "lodash").
		if !strings.Contains(specifier, "/") {
			return true
		}
		// Scoped package sub-path (@org/pkg/utils) already caught above.

	case types.LanguagePython:
		// Python relative imports are handled by isRelative; absolute imports
		// without "." are stdlib/site-packages.
		if !strings.Contains(specifier, ".") {
			return true
		}

	case types.LanguageGo:
		// Go import paths are always absolute; relative ones use "./" or "../".
		// Standard library paths do not contain a "." in the first segment.
		parts := strings.SplitN(specifier, "/", 2)
		if !strings.Contains(parts[0], ".") {
			return true // stdlib (e.g. "fmt", "os", "encoding/json")
		}

	case types.LanguageJava, types.LanguageKotlin, types.LanguageScala:
		// Java/Kotlin FQN imports starting with "java.", "javax.", "android."
		// are stdlib/SDK.
		for _, prefix := range []string{"java.", "javax.", "android.", "kotlin.", "scala.", "org.junit.", "org.springframework."} {
			if strings.HasPrefix(specifier, prefix) {
				return true
			}
		}

	case types.LanguageRust:
		// Rust crate names (no "::" path) are external.
		if !strings.Contains(specifier, "::") && !strings.Contains(specifier, "/") {
			return true
		}
	}

	return false
}

// nodeBuiltins is the set of Node.js built-in module names (no "node:" prefix).
// These are external even without the protocol prefix.
var nodeBuiltins = map[string]bool{
	"assert": true, "async_hooks": true, "buffer": true, "child_process": true,
	"cluster": true, "console": true, "constants": true, "crypto": true,
	"dgram": true, "diagnostics_channel": true, "dns": true, "domain": true,
	"events": true, "fs": true, "http": true, "http2": true, "https": true,
	"inspector": true, "module": true, "net": true, "os": true, "path": true,
	"perf_hooks": true, "process": true, "punycode": true, "querystring": true,
	"readline": true, "repl": true, "stream": true, "string_decoder": true,
	"timers": true, "tls": true, "trace_events": true, "tty": true, "url": true,
	"util": true, "v8": true, "vm": true, "wasi": true, "worker_threads": true,
	"zlib": true,
}

func isNodeBuiltin(specifier string) bool {
	return nodeBuiltins[specifier]
}
