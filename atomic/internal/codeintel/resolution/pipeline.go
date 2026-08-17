package resolution

// Resolver pipeline: resolveOne (ordered strategy cascade) and
// ResolveAndPersistBatched (keyset batch loop, package mint/sweep, synthesis).
// Contract: docs/spec/code-intel-resolution.md.

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/db"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction/standalone"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// DefaultBatchSize is the ResolveAndPersistBatched window size.
const DefaultBatchSize = 5000

// ResolveProfile captures wall-time and item counts per sub-phase of a resolve run.
type ResolveProfile struct {
	WarmDur  time.Duration
	MatchDur time.Duration
	SynthDur time.Duration

	NodeCount int // knownNames cache size after warmCaches
	RefCount  int // total refs processed across all batches
}

// PhaseEmitFunc receives each sub-phase result the moment it completes, so a
// process killed mid-resolve has still reported the phases that finished.
// phase is "resolve.warm" | "resolve.match" | "resolve.synth"; count is nodes,
// refs, and 0 respectively. A nil PhaseEmitFunc is a no-op.
type PhaseEmitFunc func(phase string, d time.Duration, count int)

// fuzzyNameLenCap bounds byFuzzy, whose variant set grows O(n*26^maxDist).
// Longer names skip fuzzy matching entirely rather than stall a batch.
const fuzzyNameLenCap = 40

// ResolvedRef is the result of one framework or import resolution attempt.
// TargetNodeID is "" when unresolved; Confidence is 0.0–1.0.
type ResolvedRef struct {
	TargetNodeID string
	Confidence   float64
}

// FrameworkResolver recognizes and resolves framework-specific references.
// Extract and PostExtract are optional, reached by type assertion — see
// FrameworkExtractor and FrameworkPostExtractor.
type FrameworkResolver interface {
	Name() string
	// Languages returns the languages this resolver handles, or nil for any.
	Languages() []types.Language
	// Detect reports whether the framework is present in the project, by
	// config file (package.json / go.mod / Gemfile) then path/content patterns.
	Detect(ctx context.Context) bool
	// ClaimsReference is the pre-filter probe: fast, no DB access.
	ClaimsReference(name string) bool
	// Resolve returns TargetNodeID=="" when it cannot handle the reference.
	Resolve(ctx context.Context, ref types.UnresolvedReference) (ResolvedRef, error)
}

// FrameworkExtractor is implemented by resolvers that mint nodes from source.
type FrameworkExtractor interface {
	// Extract returns framework constructs (e.g. Express routes) and their
	// handler references, persisted before the resolution pipeline runs.
	Extract(filePath, content string) (nodes []types.Node, references []types.UnresolvedReference)
}

// FrameworkPostExtractor is implemented by resolvers needing a whole-project
// pass after extraction, e.g. cross-file route aggregation nodes.
type FrameworkPostExtractor interface {
	PostExtract(ctx context.Context) ([]types.Node, error)
}

// FrameworkRegistry is an ordered list of FrameworkResolver instances.
type FrameworkRegistry []FrameworkResolver

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

func (fr FrameworkRegistry) claimsAny(name string) bool {
	for _, r := range fr {
		if r.ClaimsReference(name) {
			return true
		}
	}
	return false
}

// EmptyFrameworkRegistry registers no resolvers.
var EmptyFrameworkRegistry FrameworkRegistry

// CallbackSynthesizer synthesizes dynamic-dispatch edges. It runs last, after
// every static edge is persisted, because it reads those edges.
type CallbackSynthesizer interface {
	SynthesizeCallbackEdges(ctx context.Context) error
}

// NoopSynthesizer is the do-nothing default.
type NoopSynthesizer struct{}

func (NoopSynthesizer) SynthesizeCallbackEdges(_ context.Context) error { return nil }

// isBuiltinOrExternal reports names that can never resolve to an internal
// node, so the pipeline drops the ref outright rather than let it accumulate
// across runs. The sets cover extraction's most frequent false-positive
// targets, not the full language surface — add only with A/B evidence.
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

// goBuiltins is the Go spec's universe block.
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

// knownFilesCache is the set of file paths in the DB at pipeline start.
type knownFilesCache map[string]bool

// knownNamesCache is the set of lowercased symbol names in the DB at pipeline start.
type knownNamesCache map[string]bool

// Pipeline wires the import resolver, name matcher, framework registry, and
// synthesizer into the ordered batch resolution loop.
type Pipeline struct {
	db         *db.DB
	resolver   *Resolver
	matcher    *NameMatcher
	frameworks FrameworkRegistry
	synth      CallbackSynthesizer
}

// NewPipeline builds a Pipeline with no framework resolvers and no
// synthesizer. Use NewPipelineWithSeams to supply either.
func NewPipeline(d *db.DB) *Pipeline {
	return &Pipeline{
		db:         d,
		resolver:   NewResolver(d),
		matcher:    NewNameMatcher(d),
		frameworks: EmptyFrameworkRegistry,
		synth:      NoopSynthesizer{},
	}
}

// NewPipelineWithSeams builds a Pipeline with a caller-supplied framework
// registry and synthesizer.
func NewPipelineWithSeams(d *db.DB, projectRoot string, registry FrameworkRegistry, synth CallbackSynthesizer) *Pipeline {
	return &Pipeline{
		db:         d,
		resolver:   NewResolverWithProject(d, projectRoot),
		matcher:    NewNameMatcher(d),
		frameworks: registry,
		synth:      synth,
	}
}

// warmCaches loads the pre-filter caches once, so the common "this name does
// not exist at all" case costs no per-ref DB round-trip.
func (p *Pipeline) warmCaches(ctx context.Context) (knownFilesCache, knownNamesCache, error) {
	fileNodes, err := p.db.GetNodesByKind(ctx, types.NodeKindFile)
	if err != nil {
		return nil, nil, err
	}
	files := make(knownFilesCache, len(fileNodes))
	for _, n := range fileNodes {
		files[n.FilePath] = true
	}

	// Loads every node: the index is project-sized, and this runs once per
	// batch-loop invocation. A DISTINCT lower(name) query would replace it if
	// profiling ever shows it as a bottleneck.
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

// hasAnyPossibleMatch is a one-sided filter: absence from the cache proves no
// match exists, presence proves nothing — the full resolver confirms.
func hasAnyPossibleMatch(name string, known knownNamesCache) bool {
	return known[strings.ToLower(name)]
}

// matchesAnyImport admits an import ref whenever any file is indexed; the
// import resolver probes exact candidates itself.
func matchesAnyImport(ref types.UnresolvedReference, files knownFilesCache) bool {
	if ref.ReferenceKind != types.EdgeKindImports {
		return true
	}
	return len(files) > 0
}

type resolveCandidate struct {
	targetNodeID string
	confidence   float64
}

// resolveOne runs the strategy cascade for one reference. Strategies are
// ordered cheapest-and-most-certain first, and each returns immediately at
// confidence ≥ 0.9; lower-confidence hits accumulate and the best wins.
// skip=true means no edge at all (built-in, pre-filter miss, or no candidate).
func (p *Pipeline) resolveOne(
	ctx context.Context,
	ref types.UnresolvedReference,
	files knownFilesCache,
	names knownNamesCache,
) (targetNodeID string, edgeKind types.EdgeKind, skip bool, err error) {

	if isBuiltinOrExternal(ref.ReferenceName, ref.Language) {
		return "", "", true, nil
	}

	importKind := ref.ReferenceKind == types.EdgeKindImports
	nameMatch := hasAnyPossibleMatch(ref.ReferenceName, names)
	// SQL qualified column refs carry the full "schema.table.col" path, but the
	// cache holds bare names ("col"), so without this they fail the pre-filter
	// and never reach byQualifiedName. SQL-scoped to leave receiver.method and
	// pkg.Class.member pre-filtering unchanged.
	if !nameMatch && ref.Language == types.LanguageSQL {
		if simple := qualifiedSimpleName(ref.ReferenceName); simple != ref.ReferenceName {
			nameMatch = hasAnyPossibleMatch(simple, names)
		}
	}
	frameworkClaims := p.frameworks.claimsAny(ref.ReferenceName)
	pass := nameMatch || (importKind && matchesAnyImport(ref, files)) || frameworkClaims
	if !pass {
		return "", "", true, nil
	}

	var candidates []resolveCandidate

	if isJVMLanguage(ref.Language) && isJVMFQN(ref.ReferenceName) {
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

	if ref.ReferenceKind == types.EdgeKindImports {
		ri, riErr := p.resolver.ResolveImport(ctx, ref, ref.FilePath)
		if riErr != nil {
			return "", "", false, riErr
		}
		// Returns the package node id without a targetKind probe: the batch
		// loop mints that node after resolveOne returns, so a GetNode here
		// would abort the batch on a package's first appearance. Nothing is
		// lost — promoteEdgeKind is a no-op for imports.
		if ri.Kind == ResolvedKindExternal && ri.PackageName != "" {
			return extraction.GenerateNodeID("", "package", ri.PackageName, 0), ref.ReferenceKind, false, nil
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

	// Import refs are excluded: an import node is named its own specifier and
	// so is the ref, so generic name matching would edge the ref back to the
	// node that owns it. Imports resolve only via the strategies above.
	if !importKind {
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
	}

	best := bestCandidate(candidates)
	if best.targetNodeID == "" {
		return "", "", true, nil
	}
	tk, tkErr := p.targetKind(ctx, best.targetNodeID)
	if tkErr != nil {
		return "", "", false, tkErr
	}
	kind := promoteEdgeKind(ref.ReferenceKind, tk)
	return best.targetNodeID, kind, false, nil
}

func isJVMLanguage(lang types.Language) bool {
	return lang == types.LanguageJava || lang == types.LanguageKotlin || lang == types.LanguageScala
}

// isJVMFQN treats any dotted name as package-qualified.
func isJVMFQN(name string) bool {
	return strings.Contains(name, ".")
}

// resolveJVMFQN matches on qualified_name, narrowing by the last segment
// first because the DB is indexed by name, not qualified name.
func (p *Pipeline) resolveJVMFQN(ctx context.Context, fqn string) (string, error) {
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

// targetKind propagates DB errors rather than defaulting, so an
// infrastructure failure cannot masquerade as "no promotion applies".
func (p *Pipeline) targetKind(ctx context.Context, nodeID string) (types.NodeKind, error) {
	n, err := p.db.GetNode(ctx, nodeID)
	if err != nil {
		return "", err
	}
	return n.Kind, nil
}

// promoteEdgeKind sharpens a ref kind once the target's node kind is known:
// a call to a type is really an instantiation, an extends of an interface is
// really an implements. Everything else passes through.
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

func bestCandidate(cs []resolveCandidate) resolveCandidate {
	var best resolveCandidate
	for _, c := range cs {
		if c.targetNodeID != "" && (best.targetNodeID == "" || c.confidence > best.confidence) {
			best = c
		}
	}
	return best
}

// createEdges builds the edges for a resolved reference; the caller persists
// them. Ref Arguments are copied into Metadata as refArgs because synthesizers
// read edges, not refs, and the originating ref is deleted in the same
// transaction that writes the edge.
func createEdges(ref types.UnresolvedReference, targetNodeID string, edgeKind types.EdgeKind) []types.Edge {
	var meta json.RawMessage
	if len(ref.Arguments) > 0 {
		b, err := json.Marshal(map[string][]string{"refArgs": ref.Arguments})
		if err == nil {
			meta = b
		}
	}

	// A SQL ref filed under a non-SQL file came from embedded extraction, and
	// its edge must carry the same provenance the DDL path already stamps.
	// Refs from real .sql files are static and stay unstamped.
	provenance := ""
	if ref.Language == types.LanguageSQL && !isStandaloneSQLExt(ref.FilePath) {
		provenance = "embedded"
	}

	return []types.Edge{
		{
			Source:     ref.FromNodeID,
			Target:     targetNodeID,
			Kind:       edgeKind,
			Line:       ref.Line,
			Column:     ref.Column,
			Metadata:   meta,
			Provenance: provenance,
		},
	}
}

// isStandaloneSQLExt delegates to standalone.IsSQLExt so every consumer
// shares one canonical extension list.
func isStandaloneSQLExt(filePath string) bool {
	return standalone.IsSQLExt(filePath)
}

// packageNodeIDPrefix mirrors extraction.GenerateNodeID's package-kind
// formula. Edge targets are matched on this prefix rather than probed via
// targetKind, since the node may not be minted yet.
const packageNodeIDPrefix = "package:npm/"

func packageNameFromNodeID(id string) (name string, ok bool) {
	if !strings.HasPrefix(id, packageNodeIDPrefix) {
		return "", false
	}
	return strings.TrimPrefix(id, packageNodeIDPrefix), true
}

// newPackageNode has no file, line, or language: a package is an identity,
// not a location. Shape is fixed by docs/spec/code-intel-package-nodes.md.
func newPackageNode(name string) types.Node {
	return types.Node{
		ID:            extraction.GenerateNodeID("", "package", name, 0),
		Kind:          types.NodeKindPackage,
		Name:          name,
		QualifiedName: name,
		Language:      types.LanguageUnknown,
	}
}

// warmKnownPackages seeds the mint loop's skip set. Re-upserting an existing
// package would churn updated_at and re-fire FTS5 triggers on a no-op run.
func (p *Pipeline) warmKnownPackages(ctx context.Context) (map[string]bool, error) {
	nodes, err := p.db.GetNodesByKind(ctx, types.NodeKindPackage)
	if err != nil {
		return nil, err
	}
	known := make(map[string]bool, len(nodes))
	for _, n := range nodes {
		known[n.ID] = true
	}
	return known, nil
}

// sweepOrphanPackages deletes package nodes left with zero inbound edges.
// Deleting a package's last importer cascades its edges away, and nothing
// else in the pipeline notices the package is now unreachable.
func (p *Pipeline) sweepOrphanPackages(ctx context.Context) error {
	packages, err := p.db.GetNodesByKind(ctx, types.NodeKindPackage)
	if err != nil {
		return err
	}
	for _, pkg := range packages {
		edges, err := p.db.GetEdgesByTarget(ctx, pkg.ID)
		if err != nil {
			return err
		}
		if len(edges) == 0 {
			if err := p.db.DeleteNode(ctx, pkg.ID); err != nil {
				return err
			}
		}
	}
	return nil
}

// ResolveAndPersistBatched resolves every unresolved ref and returns the
// per-phase profile plus the total edges inserted. Phase order is load-bearing:
// name matching runs first, then callback synthesis (which reads the edges
// matching produced), then SQL string matching, and the orphan-package sweep
// last (it must see every edge minted this run). emit may be nil.
func (p *Pipeline) ResolveAndPersistBatched(ctx context.Context, batchSize int, emit PhaseEmitFunc) (ResolveProfile, int, error) {
	if batchSize <= 0 {
		batchSize = DefaultBatchSize
	}

	var prof ResolveProfile

	warmStart := time.Now()
	files, names, err := p.warmCaches(ctx)
	prof.WarmDur = time.Since(warmStart)
	prof.NodeCount = len(names)
	// Checked before the outputs are used: a partial name set would silently
	// corrupt the match phase rather than fail it.
	if err != nil {
		return prof, 0, err
	}
	// byFuzzy scans this slice instead of querying the DB per ref. Read-only
	// for the duration of the batch loop.
	nameSlice := make([]string, 0, len(names))
	for n := range names {
		nameSlice = append(nameSlice, n)
	}
	p.matcher.SetKnownNames(nameSlice)
	if emit != nil {
		emit("resolve.warm", prof.WarmDur, prof.NodeCount)
	}

	// Mutated in place by the mint loop, so a package discovered in batch N is
	// not re-minted when batch N+1 references it again.
	knownPackages, err := p.warmKnownPackages(ctx)
	if err != nil {
		return prof, 0, err
	}

	totalEdges := 0

	// Keyset pagination, not offset: unresolvable refs are deliberately left in
	// the table (a later index run may resolve them), so an offset-0 re-read
	// would hit them as a permanent wall at the front of the scan and never
	// reach the resolvable refs behind it. The cursor advances past every
	// window whether or not anything in it resolved.
	matchStart := time.Now()
	var cursor string
	for {
		refs, err := p.db.GetUnresolvedRefsAfter(ctx, cursor, batchSize)
		if err != nil {
			prof.MatchDur = time.Since(matchStart)
			return prof, totalEdges, err
		}
		if len(refs) == 0 {
			break
		}

		prof.RefCount += len(refs)
		// Advanced before the deletes below, so every ref is visited exactly once.
		cursor = refs[len(refs)-1].ID

		var edges []types.Edge
		var resolvedIDs []string

		for _, ref := range refs {
			if ref.ReferenceKind == types.ReferenceKindSQLString || ref.ReferenceKind == types.ReferenceKindSQLFragment {
				// Discriminators, not real reference kinds — resolveSQLStringRefs
				// consumes them in a later phase. Left in place, not deleted.
				continue
			}
			targetID, edgeKind, skip, err := p.resolveOne(ctx, ref, files, names)
			if err != nil {
				prof.MatchDur = time.Since(matchStart)
				return prof, totalEdges, err
			}
			if skip {
				// A built-in can never resolve, so drop it. Anything else that
				// merely found no candidate stays: a later index run may add
				// the node it needs.
				if isBuiltinOrExternal(ref.ReferenceName, ref.Language) {
					resolvedIDs = append(resolvedIDs, ref.ID)
				}
				continue
			}
			edges = append(edges, createEdges(ref, targetID, edgeKind)...)
			resolvedIDs = append(resolvedIDs, ref.ID)
		}

		// Continue, never break: later windows may hold resolvable refs.
		if len(resolvedIDs) == 0 {
			continue
		}

		// A package node must be upserted before any edge targeting it (FK
		// order) and inside the same transaction. Deduped within the batch —
		// several edges can target the same new package.
		var newPackages []types.Node
		mintedThisBatch := make(map[string]bool)
		for _, e := range edges {
			name, ok := packageNameFromNodeID(e.Target)
			if !ok || knownPackages[e.Target] || mintedThisBatch[e.Target] {
				continue
			}
			mintedThisBatch[e.Target] = true
			newPackages = append(newPackages, newPackageNode(name))
		}

		// Mints, edges, and ref deletes share one transaction: a crash mid-batch
		// must not leave an edge pointing at an unpersisted package node, nor
		// edges written without their refs deleted (duplicates on the next run).
		mintTime := time.Now().Unix()
		if err := p.db.WithTx(ctx, func(tx *db.Tx) error {
			for _, n := range newPackages {
				if err := tx.UpsertNodeAt(ctx, n, mintTime); err != nil {
					return err
				}
			}
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
		for _, n := range newPackages {
			knownPackages[n.ID] = true
		}

		totalEdges += len(edges)
	}
	prof.MatchDur = time.Since(matchStart)
	if emit != nil {
		emit("resolve.match", prof.MatchDur, prof.RefCount)
	}

	synthStart := time.Now()
	if err := p.synth.SynthesizeCallbackEdges(ctx); err != nil {
		prof.SynthDur = time.Since(synthStart)
		return prof, totalEdges, err
	}
	prof.SynthDur = time.Since(synthStart)
	if emit != nil {
		emit("resolve.synth", prof.SynthDur, 0)
	}

	// Consumes the sql_string/sql_fragment refs the batch loop skipped.
	_, sqlStringEdges, err := p.resolveSQLStringRefs(ctx)
	if err != nil {
		return prof, totalEdges, err
	}
	totalEdges += sqlStringEdges

	// Unconditional: a package minted by a prior run may have lost its last
	// importer even if this run resolved nothing.
	if err := p.sweepOrphanPackages(ctx); err != nil {
		return prof, totalEdges, err
	}

	return prof, totalEdges, nil
}
