package resolution

// pipeline.go — CP13 resolver pipeline.
//
// Implements resolveOne (appendix-F sub-order) and resolveAndPersistBatched
// (re-read-at-offset-0 batch loop) for the code-intelligence engine.
//
// # resolveOne sub-order (appendix F)
//
//  1. Built-in/external skip: per-language name sets; matched refs are silently
//     dropped (will never resolve to an internal node — retaining them would
//     pollute subsequent runs).
//  2. Pre-filter: hasAnyPossibleMatch + matchesAnyImport + framework
//     claimsReference. If none match, skip without attempting resolution.
//  3. JVM FQN fast path: fully-qualified name containing "." → conf 0.95, return.
//  4. Framework resolve (FrameworkResolver seam): returns if conf ≥ 0.9 else
//     accumulates. The registry is EMPTY until CP14/CP15 fill it.
//  5. resolveViaImport (CP11): returns if conf ≥ 0.9 else accumulates.
//  6. matchReference (CP12): accumulates.
//  7. Return highest-confidence candidate.
//
// # resolveAndPersistBatched loop
//
// Reads unresolved_refs at offset 0 (re-read after delete — the delete shrinks
// the set so the offset must NOT advance). Calls resolveOne per ref, collects
// edges + ref ids to delete, then deletes in bulk and writes edges in a single
// transaction. Breaks when a batch resolves NOTHING (avoids infinite loop on
// unresolvable refs).
//
// # Edge-kind promotion (appendix F)
//
//   - calls  → instantiates when target is class or struct.
//   - extends → implements when target is interface, trait, or protocol.
//
// # Framework + synthesis seams
//
// FrameworkResolver (CP14/CP15) and CallbackSynthesizer (CP16) are proper Go
// interfaces. EmptyFrameworkRegistry and NoopSynthesizer are the CP13 stubs.
// The Pipeline struct holds a FrameworkRegistry and a CallbackSynthesizer; all
// call sites exist so CP14/15/16 can fill them without touching the pipeline
// logic.
//
// # Fuzzy cap
//
// byFuzzy in name_matcher.go generates edit-distance variants; for maxDist=2
// the variant set grows as O(n * 26^2) which is manageable for typical names
// (≤ 40 chars). For names longer than fuzzyNameLenCap (40 chars), resolveOne
// calls MatchReferenceNoFuzzy instead of MatchReference, skipping byFuzzy
// entirely. Non-fuzzy strategies (exact/qualified/methodCall/filePath) still
// run. This is noted here so a future reader can distinguish "we forgot" from
// "we deliberately bounded".
//
// See: appendix F (resolution order), appendix G (synthesis seam).

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/db"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// ---------------------------------------------------------------------------
// Batch size constant
// ---------------------------------------------------------------------------

// DefaultBatchSize is the batch size for resolveAndPersistBatched (appendix F).
const DefaultBatchSize = 5000

// ---------------------------------------------------------------------------
// ResolveProfile — per-phase timing
// ---------------------------------------------------------------------------

// ResolveProfile captures wall-time and item counts for each sub-phase of
// the resolve pipeline. Returned by ResolveAndPersistBatched.
//
// Phase definitions:
//   - WarmDur:   warmCaches call (known-files + known-names DB load).
//   - MatchDur:  the resolveOne batch loop (total time across all batches).
//   - SynthDur:  SynthesizeCallbackEdges.
//   - NodeCount: number of nodes in the knownNames cache after warmCaches.
//   - RefCount:  number of unresolved refs processed across all batches.
type ResolveProfile struct {
	WarmDur  time.Duration
	MatchDur time.Duration
	SynthDur time.Duration

	NodeCount int // knownNames cache size after warmCaches
	RefCount  int // total refs processed across all batches
}

// PhaseEmitFunc is called immediately after each sub-phase of the resolve
// pipeline completes — before the next phase starts. This allows callers to
// flush a profile line to stderr right away so a process killed mid-resolve
// still shows the already-completed phases.
//
// Arguments:
//   - phase: one of "resolve.warm", "resolve.match", "resolve.synth".
//   - d:     wall-time duration of the phase.
//   - count: phase-specific count: warm → node count, match → ref count,
//     synth → 0 (no applicable count).
//
// A nil PhaseEmitFunc is safe — the pipeline treats it as a no-op.
type PhaseEmitFunc func(phase string, d time.Duration, count int)

// ---------------------------------------------------------------------------
// Fuzzy cap constants (see package comment)
// ---------------------------------------------------------------------------

// fuzzyNameLenCap is the maximum reference name length for which the fuzzy
// name-matcher path is attempted. Names longer than this are unlikely to be
// meaningful fuzzy matches and would generate large variant sets.
const fuzzyNameLenCap = 40

// ---------------------------------------------------------------------------
// Framework resolver seam (CP14/CP15)
// ---------------------------------------------------------------------------

// ResolvedRef is the result of one framework or import resolution attempt.
type ResolvedRef struct {
	// TargetNodeID is the resolved node id, or "" if unresolved.
	TargetNodeID string
	// Confidence is 0.0–1.0.
	Confidence float64
}

// FrameworkResolver is the seam that CP14/CP15 implement. The registry is
// empty until those checkpoints run; all calls return nothing for now.
//
// Detect, ClaimsReference, and Resolve are the three mandatory methods used
// by the registry and pipeline. Extract and PostExtract are optional (see
// FrameworkExtractor and FrameworkPostExtractor below).
type FrameworkResolver interface {
	// Name returns the resolver's identifier (e.g. "express", "django").
	Name() string
	// Languages returns the language tags this resolver handles, or nil for any.
	Languages() []types.Language
	// Detect returns true if this framework is present in the project at
	// projectRoot. Reads a config file (package.json / go.mod / Gemfile) and
	// falls back to path + content patterns. Used by the registry's
	// DetectFrameworks to filter the full resolver list to active ones.
	Detect(ctx context.Context) bool
	// ClaimsReference returns true if this resolver knows about a reference by
	// this name (pre-filter step — fast, no DB access).
	ClaimsReference(name string) bool
	// Resolve attempts to resolve the reference. Returns ResolvedRef with
	// TargetNodeID=="" when the resolver cannot handle it.
	Resolve(ctx context.Context, ref types.UnresolvedReference) (ResolvedRef, error)
}

// FrameworkExtractor is an optional capability interface. Resolvers that can
// extract route nodes from source files implement this alongside
// FrameworkResolver. The pipeline checks for this interface via type assertion;
// resolvers that only do Resolve need not implement it.
type FrameworkExtractor interface {
	// Extract scans filePath/content for framework-specific constructs (e.g.
	// Express routes) and returns the route nodes and unresolved handler
	// references to persist before the resolution pipeline runs.
	Extract(filePath, content string) (nodes []types.Node, references []types.UnresolvedReference)
}

// FrameworkPostExtractor is an optional capability interface. Resolvers that
// need a post-extraction pass (e.g. to emit cross-file route aggregation nodes)
// implement this alongside FrameworkResolver.
type FrameworkPostExtractor interface {
	// PostExtract runs after all files have been extracted. Returns any
	// additional nodes to persist.
	PostExtract(ctx context.Context) ([]types.Node, error)
}

// FrameworkRegistry is an ordered list of FrameworkResolver instances.
// getApplicableResolvers filters by language.
type FrameworkRegistry []FrameworkResolver

// getApplicableResolvers returns resolvers that apply to the given language.
func (fr FrameworkRegistry) getApplicableResolvers(lang types.Language) []FrameworkResolver {
	var result []FrameworkResolver
	for _, r := range fr {
		langs := r.Languages()
		if langs == nil {
			result = append(result, r)
			continue
		}
		for _, l := range langs {
			if l == lang {
				result = append(result, r)
				break
			}
		}
	}
	return result
}

// claimsAny returns true if any resolver in the registry claims the name.
func (fr FrameworkRegistry) claimsAny(name string) bool {
	for _, r := range fr {
		if r.ClaimsReference(name) {
			return true
		}
	}
	return false
}

// EmptyFrameworkRegistry is the CP13 stub — no framework resolvers registered.
// CP14 and CP15 populate the registry.
var EmptyFrameworkRegistry FrameworkRegistry

// ---------------------------------------------------------------------------
// Callback synthesizer seam (CP16)
// ---------------------------------------------------------------------------

// CallbackSynthesizer synthesizes dynamic-dispatch edges after all static
// edges are persisted (appendix G). The no-op stub is used until CP16.
type CallbackSynthesizer interface {
	// SynthesizeCallbackEdges creates and persists synthesized edges. It is
	// called LAST, after the resolveAndPersistBatched loop completes.
	SynthesizeCallbackEdges(ctx context.Context) error
}

// NoopSynthesizer is the CP13 stub — does nothing. CP16 replaces it.
type NoopSynthesizer struct{}

func (NoopSynthesizer) SynthesizeCallbackEdges(_ context.Context) error { return nil }

// ---------------------------------------------------------------------------
// Per-language built-in / stdlib name sets
// ---------------------------------------------------------------------------

// builtinNames returns true if name is a well-known built-in or stdlib symbol
// for lang that will never resolve to an internal node. The sets are modest
// and documented — these are the most common false-positive ref targets that
// extraction emits. Not exhaustive; add sparingly with A/B evidence.
//
// Skip policy: a matched ref is silently removed from unresolved_refs. It will
// never produce an internal edge; keeping it would pollute subsequent runs.
func isBuiltinOrExternal(name string, lang types.Language) bool {
	switch lang {
	case types.LanguageTypeScript, types.LanguageJavaScript,
		types.LanguageTSX, types.LanguageJSX:
		return jsBuiltins[name]
	case types.LanguagePython:
		return pyBuiltins[name]
	case types.LanguageGo:
		return goBuiltins[name]
	case types.LanguageRust:
		return rustBuiltins[name]
	case types.LanguageJava, types.LanguageKotlin:
		return jvmBuiltins[name]
	}
	return false
}

// jsBuiltins: common JS/TS global objects and functions that extraction emits
// as call/reference targets. console, process, Math, JSON, etc. are never
// internal nodes. The list is intentionally modest — only clear, high-frequency
// hits; not an exhaustive ECMA list.
var jsBuiltins = map[string]bool{
	"console":       true,
	"process":       true,
	"Math":          true,
	"JSON":          true,
	"Object":        true,
	"Array":         true,
	"String":        true,
	"Number":        true,
	"Boolean":       true,
	"Symbol":        true,
	"Promise":       true,
	"Error":         true,
	"TypeError":     true,
	"RangeError":    true,
	"setTimeout":    true,
	"setInterval":   true,
	"clearTimeout":  true,
	"clearInterval": true,
	"parseInt":      true,
	"parseFloat":    true,
	"isNaN":         true,
	"isFinite":      true,
	"require":       true, // CommonJS
	"module":        true,
	"exports":       true,
	"__dirname":     true,
	"__filename":    true,
	"Buffer":        true,
}

// pyBuiltins: common Python built-in functions and types.
var pyBuiltins = map[string]bool{
	"print":      true,
	"len":        true,
	"range":      true,
	"enumerate":  true,
	"zip":        true,
	"map":        true,
	"filter":     true,
	"sorted":     true,
	"reversed":   true,
	"list":       true,
	"dict":       true,
	"set":        true,
	"tuple":      true,
	"str":        true,
	"int":        true,
	"float":      true,
	"bool":       true,
	"bytes":      true,
	"type":       true,
	"isinstance": true,
	"issubclass": true,
	"hasattr":    true,
	"getattr":    true,
	"setattr":    true,
	"delattr":    true,
	"open":       true,
	"super":      true,
	"object":     true,
	"Exception":  true,
	"ValueError": true,
	"TypeError":  true,
	"KeyError":   true,
	"IndexError": true,
}

// goBuiltins: Go built-in functions and types (spec §Universe block).
var goBuiltins = map[string]bool{
	"append":  true,
	"cap":     true,
	"close":   true,
	"complex": true,
	"copy":    true,
	"delete":  true,
	"imag":    true,
	"len":     true,
	"make":    true,
	"new":     true,
	"panic":   true,
	"print":   true,
	"println": true,
	"real":    true,
	"recover": true,
	"error":   true,
	"bool":    true,
	"string":  true,
	"int":     true,
	"int8":    true,
	"int16":   true,
	"int32":   true,
	"int64":   true,
	"uint":    true,
	"uint8":   true,
	"uint16":  true,
	"uint32":  true,
	"uint64":  true,
	"float32": true,
	"float64": true,
	"byte":    true,
	"rune":    true,
}

// rustBuiltins: Rust built-in macros and common std traits that extraction emits.
var rustBuiltins = map[string]bool{
	"println":       true,
	"print":         true,
	"eprintln":      true,
	"eprint":        true,
	"format":        true,
	"vec":           true,
	"panic":         true,
	"assert":        true,
	"assert_eq":     true,
	"assert_ne":     true,
	"todo":          true,
	"unimplemented": true,
	"unreachable":   true,
}

// jvmBuiltins: Java/Kotlin stdlib top-level names that are never internal nodes.
var jvmBuiltins = map[string]bool{
	"System":           true,
	"Object":           true,
	"String":           true,
	"Integer":          true,
	"Long":             true,
	"Double":           true,
	"Float":            true,
	"Boolean":          true,
	"Math":             true,
	"Exception":        true,
	"RuntimeException": true,
	"println":          true, // Kotlin
}

// ---------------------------------------------------------------------------
// warm-cache types
// ---------------------------------------------------------------------------

// knownFilesCache is a set of file paths that exist in the DB at pipeline start.
type knownFilesCache map[string]bool

// knownNamesCache is a set of symbol names that exist in the DB at pipeline start.
type knownNamesCache map[string]bool

// ---------------------------------------------------------------------------
// Pipeline
// ---------------------------------------------------------------------------

// Pipeline wires the CP11 import resolver, the CP12 name matcher, the
// framework registry seam (CP14/15), and the synthesis seam (CP16) into the
// ordered batch resolution loop described in appendix F.
type Pipeline struct {
	db         *db.DB
	resolver   *Resolver
	matcher    *NameMatcher
	frameworks FrameworkRegistry
	synth      CallbackSynthesizer
}

// NewPipeline constructs a Pipeline with the default (empty) framework registry
// and the no-op synthesizer seam. Use NewPipelineWithSeams to inject
// framework resolvers (CP14/15) or a real synthesizer (CP16).
func NewPipeline(d *db.DB) *Pipeline {
	return &Pipeline{
		db:         d,
		resolver:   NewResolver(d),
		matcher:    NewNameMatcher(d),
		frameworks: EmptyFrameworkRegistry,
		synth:      NoopSynthesizer{},
	}
}

// NewPipelineWithSeams constructs a Pipeline with caller-supplied framework
// registry and synthesizer. Called by CP14/15 (framework resolvers) and CP16
// (synthesizer) once those checkpoints land.
func NewPipelineWithSeams(d *db.DB, projectRoot string, registry FrameworkRegistry, synth CallbackSynthesizer) *Pipeline {
	return &Pipeline{
		db:         d,
		resolver:   NewResolverWithProject(d, projectRoot),
		matcher:    NewNameMatcher(d),
		frameworks: registry,
		synth:      synth,
	}
}

// ---------------------------------------------------------------------------
// warmCaches
// ---------------------------------------------------------------------------

// warmCaches builds the knownFiles and knownNames caches by scanning the DB.
// These caches are used by hasAnyPossibleMatch and matchesAnyImport in the
// pre-filter step, avoiding per-ref DB round-trips for the common "this name
// doesn't exist at all" case.
func (p *Pipeline) warmCaches(ctx context.Context) (knownFilesCache, knownNamesCache, error) {
	// Warm known files: collect all file_path values from the nodes table.
	// We re-use GetNodesByKind(file) which returns all file nodes.
	fileNodes, err := p.db.GetNodesByKind(ctx, types.NodeKindFile)
	if err != nil {
		return nil, nil, err
	}
	files := make(knownFilesCache, len(fileNodes))
	for _, n := range fileNodes {
		files[n.FilePath] = true
	}

	// Warm known names: scan a broad sample of nodes to build a name → true map.
	// We load all nodes (the DB is expected to be a project-sized index, not
	// multi-GB). For very large repos this scan may be slow; it runs once per
	// batch-loop invocation. If profiling reveals it as a bottleneck, a dedicated
	// "SELECT DISTINCT lower(name) FROM nodes" query can replace it.
	allNodes, err := p.db.GetAllNodes(ctx)
	if err != nil {
		return nil, nil, err
	}
	names := make(knownNamesCache, len(allNodes))
	for _, n := range allNodes {
		names[strings.ToLower(n.Name)] = true
	}
	return files, names, nil
}

// ---------------------------------------------------------------------------
// pre-filter
// ---------------------------------------------------------------------------

// hasAnyPossibleMatch returns true if the reference name (or a close variant)
// appears in the known-names cache. This is a fast false-negative filter: a
// name NOT in the cache definitely has no match; a name IN the cache may or
// may not resolve (the full resolver confirms). The filter avoids launching
// the full resolution pipeline for references to names that don't exist at all.
func hasAnyPossibleMatch(name string, known knownNamesCache) bool {
	return known[strings.ToLower(name)]
}

// matchesAnyImport returns true if any file in the known-files cache looks
// like a plausible import target for the specifier. For import-kind refs this
// is always true (the import resolver handles the detail). For non-import refs
// it defers to hasAnyPossibleMatch.
func matchesAnyImport(ref types.UnresolvedReference, files knownFilesCache) bool {
	if ref.ReferenceKind != types.EdgeKindImports {
		return true // non-import: pass through to hasAnyPossibleMatch
	}
	// For import refs: any file exists → pass (the import resolver will probe
	// exact candidates). If no files at all, skip.
	return len(files) > 0
}

// ---------------------------------------------------------------------------
// resolveOne
// ---------------------------------------------------------------------------

// resolveCandidate bundles a resolved node id + confidence.
type resolveCandidate struct {
	targetNodeID string
	confidence   float64
}

// resolveOne resolves a single UnresolvedReference following the appendix-F
// sub-order. Returns (targetNodeID, edgeKind, skip, error):
//   - skip=true means the ref should be dropped with no edge (built-in,
//     pre-filter miss, or no candidate found at all).
//   - On skip, targetNodeID and edgeKind are zero values.
//   - edgeKind is the (possibly promoted) edge kind for the resolved edge.
func (p *Pipeline) resolveOne(
	ctx context.Context,
	ref types.UnresolvedReference,
	files knownFilesCache,
	names knownNamesCache,
) (targetNodeID string, edgeKind types.EdgeKind, skip bool, err error) {

	// Step 1 — built-in/external skip.
	if isBuiltinOrExternal(ref.ReferenceName, ref.Language) {
		return "", "", true, nil
	}

	// Step 2 — pre-filter (appendix F): pass if ANY of the three conditions holds.
	//   - hasAnyPossibleMatch: name exists in the known-names cache.
	//   - import ref with at least one file: the import resolver handles the detail.
	//   - frameworkClaims: a framework resolver knows this name even if the cache doesn't.
	importKind := ref.ReferenceKind == types.EdgeKindImports
	nameMatch := hasAnyPossibleMatch(ref.ReferenceName, names)
	frameworkClaims := p.frameworks.claimsAny(ref.ReferenceName)
	pass := nameMatch || (importKind && matchesAnyImport(ref, files)) || frameworkClaims
	if !pass {
		return "", "", true, nil
	}

	var candidates []resolveCandidate

	// Step 3 — JVM FQN fast path.
	if isJVMLanguage(ref.Language) && isJVMFQN(ref.ReferenceName) {
		// FQN: look up by qualified name directly in the DB.
		fqnNode, fqnErr := p.resolveJVMFQN(ctx, ref.ReferenceName)
		if fqnErr != nil {
			return "", "", false, fqnErr
		}
		if fqnNode != "" {
			tk, tkErr := p.targetKind(ctx, fqnNode)
			if tkErr != nil {
				return "", "", false, tkErr
			}
			kind := promoteEdgeKind(ref.ReferenceKind, tk)
			return fqnNode, kind, false, nil
		}
	}

	// Step 4 — framework resolve.
	applicable := p.frameworks.getApplicableResolvers(ref.Language)
	for _, fr := range applicable {
		result, frErr := fr.Resolve(ctx, ref)
		if frErr != nil {
			return "", "", false, frErr
		}
		if result.TargetNodeID != "" {
			if result.Confidence >= 0.9 {
				tk, tkErr := p.targetKind(ctx, result.TargetNodeID)
				if tkErr != nil {
					return "", "", false, tkErr
				}
				kind := promoteEdgeKind(ref.ReferenceKind, tk)
				return result.TargetNodeID, kind, false, nil
			}
			candidates = append(candidates, resolveCandidate{
				targetNodeID: result.TargetNodeID,
				confidence:   result.Confidence,
			})
		}
	}

	// Step 5 — resolveViaImport (CP11).
	if ref.ReferenceKind == types.EdgeKindImports {
		ri, riErr := p.resolver.ResolveImport(ctx, ref, ref.FilePath)
		if riErr != nil {
			return "", "", false, riErr
		}
		if ri.Kind == ResolvedKindInternal && ri.TargetNodeID != "" {
			if ri.Confidence >= 0.9 {
				tk, tkErr := p.targetKind(ctx, ri.TargetNodeID)
				if tkErr != nil {
					return "", "", false, tkErr
				}
				kind := promoteEdgeKind(ref.ReferenceKind, tk)
				return ri.TargetNodeID, kind, false, nil
			}
			candidates = append(candidates, resolveCandidate{
				targetNodeID: ri.TargetNodeID,
				confidence:   ri.Confidence,
			})
		}
	}

	// Step 6 — matchReference (CP12).
	// Fuzzy cap: for names longer than fuzzyNameLenCap, call MatchReferenceNoFuzzy
	// to skip byFuzzy entirely. byFuzzy generates O(n*26^maxDist) edit-distance
	// variants; for n=41+ that set grows large enough to stall a batch.
	// Non-fuzzy strategies (exact/qualified/methodCall/filePath) still run.
	var mr *MatchResult
	var mrErr error
	if len(ref.ReferenceName) > fuzzyNameLenCap {
		mr, mrErr = p.matcher.MatchReferenceNoFuzzy(ctx, ref)
	} else {
		mr, mrErr = p.matcher.MatchReference(ctx, ref)
	}
	if mrErr != nil {
		return "", "", false, mrErr
	}
	if mr != nil && mr.Node.ID != "" {
		candidates = append(candidates, resolveCandidate{
			targetNodeID: mr.Node.ID,
			confidence:   mr.Confidence,
		})
	}

	// Step 7 — return highest-confidence candidate.
	best := bestCandidate(candidates)
	if best.targetNodeID == "" {
		return "", "", true, nil // no candidate — skip (no edge)
	}
	tk, tkErr := p.targetKind(ctx, best.targetNodeID)
	if tkErr != nil {
		return "", "", false, tkErr
	}
	kind := promoteEdgeKind(ref.ReferenceKind, tk)
	return best.targetNodeID, kind, false, nil
}

// ---------------------------------------------------------------------------
// JVM FQN helpers
// ---------------------------------------------------------------------------

// isJVMLanguage returns true for Java, Kotlin, Scala.
func isJVMLanguage(lang types.Language) bool {
	return lang == types.LanguageJava || lang == types.LanguageKotlin || lang == types.LanguageScala
}

// isJVMFQN returns true if the reference name looks like a Java/Kotlin FQN
// (contains at least one "." indicating a package-qualified name).
func isJVMFQN(name string) bool {
	return strings.Contains(name, ".")
}

// resolveJVMFQN looks up a node by its qualified_name, returning the node id
// or "" if not found. Confidence is fixed at 0.95 per appendix F.
func (p *Pipeline) resolveJVMFQN(ctx context.Context, fqn string) (string, error) {
	// GetNodesByName finds by name only; for FQN we need to search by qualified_name.
	// Use GetNodesByQualifiedName if available, else fall back to name-based lookup
	// on the last segment.
	simpleName := fqn
	if idx := strings.LastIndex(fqn, "."); idx >= 0 {
		simpleName = fqn[idx+1:]
	}
	nodes, err := p.db.GetNodesByName(ctx, simpleName, "")
	if err != nil {
		return "", err
	}
	lowerFQN := strings.ToLower(fqn)
	for _, n := range nodes {
		if strings.ToLower(n.QualifiedName) == lowerFQN {
			return n.ID, nil
		}
	}
	return "", nil
}

// ---------------------------------------------------------------------------
// Edge-kind promotion (appendix F)
// ---------------------------------------------------------------------------

// targetKind returns the NodeKind of the node with the given id. An error from
// the DB is returned to the caller — a DB failure must not silently suppress
// edge-kind promotion (it would mask a real infrastructure problem as "no
// promotion").
func (p *Pipeline) targetKind(ctx context.Context, nodeID string) (types.NodeKind, error) {
	n, err := p.db.GetNode(ctx, nodeID)
	if err != nil {
		return "", err
	}
	return n.Kind, nil
}

// promoteEdgeKind applies the appendix-F promotion rules:
//   - calls  → instantiates when target is class or struct.
//   - extends → implements when target is interface, trait, or protocol.
//
// All other combinations are returned as-is.
func promoteEdgeKind(refKind types.EdgeKind, targetKind types.NodeKind) types.EdgeKind {
	switch refKind {
	case types.EdgeKindCalls:
		if targetKind == types.NodeKindClass || targetKind == types.NodeKindStruct {
			return types.EdgeKindInstantiates
		}
	case types.EdgeKindExtends:
		if targetKind == types.NodeKindInterface || targetKind == types.NodeKindTrait ||
			targetKind == types.NodeKindProtocol {
			return types.EdgeKindImplements
		}
	}
	return refKind
}

// ---------------------------------------------------------------------------
// Candidate selection
// ---------------------------------------------------------------------------

// bestCandidate returns the resolveCandidate with the highest confidence.
// Returns zero value if candidates is empty.
func bestCandidate(cs []resolveCandidate) resolveCandidate {
	var best resolveCandidate
	for _, c := range cs {
		if c.targetNodeID != "" && (best.targetNodeID == "" || c.confidence > best.confidence) {
			best = c
		}
	}
	return best
}

// ---------------------------------------------------------------------------
// createEdges
// ---------------------------------------------------------------------------

// createEdges builds the Edge(s) for a resolved reference. Currently one edge
// per resolved ref; the caller persists it.
//
// Origin-ref discriminator propagation: when the unresolved ref carries
// Arguments (e.g. EE1 "jsx:<Tag>", EE3 "field:<name>"), they are written into
// the edge's Metadata as {"refArgs":["jsx:ChildWidget"]}. This lets
// synthesizers — which read edges, not refs — recover which mechanism produced
// a static edge without re-querying the (already-deleted) unresolved_ref.
// Reused by jsx-render (batch 2), and planned for event-emitter / callback
// (batches 3–6). Static edges from refs without Arguments carry no Metadata
// unless the ref already had a Metadata value (not currently produced).
func createEdges(ref types.UnresolvedReference, targetNodeID string, edgeKind types.EdgeKind) []types.Edge {
	var meta json.RawMessage
	if len(ref.Arguments) > 0 {
		b, err := json.Marshal(map[string][]string{"refArgs": ref.Arguments})
		if err == nil {
			meta = b
		}
	}
	return []types.Edge{
		{
			Source:   ref.FromNodeID,
			Target:   targetNodeID,
			Kind:     edgeKind,
			Line:     ref.Line,
			Column:   ref.Column,
			Metadata: meta,
			// Provenance is empty for static edges (appendix G).
		},
	}
}

// ---------------------------------------------------------------------------
// ResolveAndPersistBatched — the main loop
// ---------------------------------------------------------------------------

// ResolveAndPersistBatched runs the resolution batch loop as described in
// appendix F:
//
//  1. warmCaches (knownFiles, knownNames).
//  2. Read unresolved_refs at offset 0 (NOT advancing — delete shrinks the set).
//  3. Per ref: resolveOne → createEdges.
//  4. insertEdges (in a transaction).
//  5. deleteSpecificResolvedReferences (bulk delete resolved + skipped refs).
//  6. Break when a batch yields nothing new (avoids infinite loop on
//     unresolvable refs).
//  7. Call synthesizeCallbackEdges LAST (CP16 seam — no-op until then).
//
// Returns a ResolveProfile (per-phase wall-time + counts), the total number of
// edges inserted, and any error.
//
// emit is called immediately after each sub-phase completes, before the next
// phase starts. Pass nil for no-op behaviour (existing call sites unchanged).
// See PhaseEmitFunc for the argument semantics.
func (p *Pipeline) ResolveAndPersistBatched(ctx context.Context, batchSize int, emit PhaseEmitFunc) (ResolveProfile, int, error) {
	if batchSize <= 0 {
		batchSize = DefaultBatchSize
	}

	var prof ResolveProfile

	// Phase 1: warmCaches — measure wall-time.
	warmStart := time.Now()
	files, names, err := p.warmCaches(ctx)
	prof.WarmDur = time.Since(warmStart)
	prof.NodeCount = len(names)
	// Check warmCaches error before using its outputs. If warmCaches returned a
	// partial result alongside an error, emitting and populating the matcher with
	// a partial name set would silently corrupt the match phase. Check first.
	if err != nil {
		return prof, 0, err
	}
	// Thread the warmed name set into the matcher so byFuzzy can scan it
	// without issuing per-ref DB queries. The slice is built once here and
	// read-only during the batch loop.
	nameSlice := make([]string, 0, len(names))
	for n := range names {
		nameSlice = append(nameSlice, n)
	}
	p.matcher.SetKnownNames(nameSlice)
	// Emit resolve.warm immediately so a killed process sees it before match starts.
	if emit != nil {
		emit("resolve.warm", prof.WarmDur, prof.NodeCount)
	}

	totalEdges := 0

	// Phase 2: batch loop (resolve.match).
	matchStart := time.Now()
	for {
		// Read at offset 0 every iteration (re-read after delete).
		refs, err := p.db.GetUnresolvedRefs(ctx, batchSize, 0)
		if err != nil {
			prof.MatchDur = time.Since(matchStart)
			return prof, totalEdges, err
		}
		if len(refs) == 0 {
			// No more refs — done.
			break
		}

		prof.RefCount += len(refs)

		var edges []types.Edge
		var resolvedIDs []string // ref ids to delete (resolved + skipped built-ins)

		for _, ref := range refs {
			targetID, edgeKind, skip, err := p.resolveOne(ctx, ref, files, names)
			if err != nil {
				prof.MatchDur = time.Since(matchStart)
				return prof, totalEdges, err
			}
			if skip {
				// Built-in skip or no match. If built-in → remove from unresolved_refs
				// so it doesn't pollute future runs. If truly unresolvable (no matching
				// node) → do NOT remove, because a future re-index might add the node.
				//
				// Distinction: isBuiltinOrExternal check in resolveOne fires first;
				// if skip=true due to built-in, we remove the ref. If skip=true because
				// no candidate was found (hasAnyPossibleMatch=false or matchReference
				// returned nil), we do NOT remove the ref.
				if isBuiltinOrExternal(ref.ReferenceName, ref.Language) {
					resolvedIDs = append(resolvedIDs, ref.ID)
				}
				// All other non-matching refs remain — they might resolve after
				// more files are indexed.
				continue
			}
			edges = append(edges, createEdges(ref, targetID, edgeKind)...)
			resolvedIDs = append(resolvedIDs, ref.ID)
		}

		// If nothing in this batch resolved (and nothing was skipped as built-in),
		// break — all remaining refs are unresolvable for now. Guards against
		// infinite loops when a set of refs is permanently unresolvable.
		if len(resolvedIDs) == 0 {
			break
		}

		// Persist edges AND delete resolved refs in ONE transaction.
		// Both operations share the same transaction so a crash between them
		// cannot leave edges persisted without the corresponding refs deleted
		// (which would cause duplicate edges on the next run).
		if err := p.db.WithTx(ctx, func(tx *db.Tx) error {
			for _, e := range edges {
				if _, err := tx.InsertEdge(ctx, e); err != nil {
					return err
				}
			}
			return tx.DeleteUnresolvedRefsByIDs(ctx, resolvedIDs)
		}); err != nil {
			prof.MatchDur = time.Since(matchStart)
			return prof, totalEdges, err
		}

		totalEdges += len(edges)
	}
	prof.MatchDur = time.Since(matchStart)
	// Emit resolve.match immediately — before synth starts.
	if emit != nil {
		emit("resolve.match", prof.MatchDur, prof.RefCount)
	}

	// Phase 3: SynthesizeCallbackEdges (resolve.synth).
	// This is a no-op in CP13; CP16 fills the synthesizer.
	synthStart := time.Now()
	if err := p.synth.SynthesizeCallbackEdges(ctx); err != nil {
		prof.SynthDur = time.Since(synthStart)
		return prof, totalEdges, err
	}
	prof.SynthDur = time.Since(synthStart)
	// Emit resolve.synth immediately after it completes.
	if emit != nil {
		emit("resolve.synth", prof.SynthDur, 0)
	}

	return prof, totalEdges, nil
}
