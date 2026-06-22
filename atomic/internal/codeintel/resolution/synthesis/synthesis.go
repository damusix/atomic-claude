// Package synthesis implements the CP16 callback synthesizer infrastructure.
//
// # Architecture
//
// The Composite type implements resolution.CallbackSynthesizer and runs every
// registered Synthesizer after all static edges have been persisted (appendix
// F: synthesis runs LAST). Each Synthesizer returns proposed edges; the
// Composite stamps them with Kind='calls', Provenance='heuristic', and
// Metadata carrying synthesizedBy + any optional fields, then dedups and
// persists in a single transaction.
//
// # Dedup key
//
// Appendix G: dedup by "source>target". Two synthesized edges with the same
// source+target are collapsed to one. If an identical source>target edge
// already exists in the DB (from a previous synthesis run), the new edge is
// skipped — making synthesis idempotent.
//
// # Caps (appendix G, verbatim)
//
// Each synthesizer applies its own cap before returning proposed edges.
// The composite enforces dedup on top. Caps are centralized here as named
// constants so tests can assert the exact literals.
//
//	MAX_CALLBACKS_PER_CHANNEL = 40  — callback (field-backed observer)
//	EVENT_FANOUT_CAP          = 6   — event-emitter
//	CC_FANOUT_CAP             = 8   — closure-collection (future synthesizer)
//
// # Node-count stability
//
// Synthesizers add EDGES only — no new nodes. Dedup makes re-runs idempotent:
// a second call to SynthesizeCallbackEdges produces no new edges when the
// first run already persisted them.
package synthesis

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/db"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// ---------------------------------------------------------------------------
// Cap constants (appendix G, R6 — asserted literally by tests)
// ---------------------------------------------------------------------------

// MAX_CALLBACKS_PER_CHANNEL is the per-channel cap for the callback
// (field-backed observer) synthesizer.
const MAX_CALLBACKS_PER_CHANNEL = 40

// EVENT_FANOUT_CAP is the per-event cap for the event-emitter synthesizer.
const EVENT_FANOUT_CAP = 6

// CC_FANOUT_CAP is the per-channel cap for the closure-collection synthesizer
// (reserved for a later CP16 batch; centralized here per spec).
const CC_FANOUT_CAP = 8

// ---------------------------------------------------------------------------
// Synthesizer interface
// ---------------------------------------------------------------------------

// Synthesizer proposes synthesized edges. The Composite stamps Provenance and
// Metadata, dedups, and persists — synthesizers only need to return the
// proposed (source, target) pairs with any optional metadata hints.
//
// Name() returns the synthesizedBy tag (e.g. "react-render", "event-emitter").
// Synthesize() queries the DB and returns proposed edges. Each returned edge
// MUST have Source and Target set; Kind, Provenance, and Metadata are stamped
// by the Composite and need not be set by the synthesizer.
type Synthesizer interface {
	// Name returns the synthesizedBy metadata tag for this synthesizer.
	Name() string
	// Synthesize queries the DB and returns proposed edges. The Composite
	// stamps Kind='calls', Provenance='heuristic', and Metadata. The returned
	// edges should have Source and Target set; any Metadata already present is
	// MERGED (synthesizer-set fields take precedence) with the composite's stamp.
	Synthesize(ctx context.Context, d *db.DB) ([]types.Edge, error)
}

// ---------------------------------------------------------------------------
// Metadata helpers
// ---------------------------------------------------------------------------

// synthMeta is the JSON metadata struct for synthesized edges.
type synthMeta struct {
	SynthesizedBy string `json:"synthesizedBy"`
	Via           string `json:"via,omitempty"`
	Field         string `json:"field,omitempty"`
	Event         string `json:"event,omitempty"`
	RegisteredAt  string `json:"registeredAt,omitempty"`
}

// marshalMeta encodes a synthMeta into json.RawMessage.
func marshalMeta(m synthMeta) json.RawMessage {
	b, err := json.Marshal(m)
	if err != nil {
		// Should never fail for this fixed struct.
		return json.RawMessage(`{"synthesizedBy":"unknown"}`)
	}
	return b
}

// ---------------------------------------------------------------------------
// Composite
// ---------------------------------------------------------------------------

// Composite implements resolution.CallbackSynthesizer. It runs each registered
// Synthesizer, collects proposed edges, stamps provenance + metadata, dedups
// by "source>target", skips edges already present in the DB, and persists the
// remainder in a single transaction.
type Composite struct {
	db           *db.DB
	synthesizers []Synthesizer
}

// NewComposite constructs a Composite with the given synthesizers.
func NewComposite(d *db.DB, ss ...Synthesizer) *Composite {
	return &Composite{db: d, synthesizers: ss}
}

// Default constructs a Composite seeded with all batch-1 through batch-6
// synthesizers (14 total):
//
//	batch 1: react-render, jsx-render, vue-handler
//	batch 2: rn-event-channel, event-emitter
//	batch 3: callback, closure-collection, flutter-build (stub — Dart grammar gap)
//	batch 4: interface-impl, cpp-override
//	batch 5: gin-middleware-chain, go-grpc-stub-impl (stub — Go interface method gap)
//	batch 6: mybatis-java-xml, fabric-native-impl (stub — no cross-language correlation)
//
// rn-event-channel runs before event-emitter so its synthesizedBy tag wins
// for RN-pattern source>target pairs (Composite dedup is first-wins).
func Default(d *db.DB) *Composite {
	return NewComposite(d,
		&ReactRenderSynthesizer{},
		&JSXRenderSynthesizer{},
		&VueHandlerSynthesizer{},
		&RNEventChannelSynthesizer{},
		&EventEmitterSynthesizer{},
		&CallbackSynthesizer{},
		&ClosureCollectionSynthesizer{},
		&FlutterBuildSynthesizer{},
		&InterfaceImplSynthesizer{},
		&CppOverrideSynthesizer{},
		&GinMiddlewareChainSynthesizer{},
		&GoGRPCStubImplSynthesizer{},
		&MyBatisJavaXMLSynthesizer{},
		&FabricNativeImplSynthesizer{},
	)
}

// SynthesizerNames returns the Name() of each registered synthesizer in order.
// Useful for tests that need to assert the full roster without reflection.
func (c *Composite) SynthesizerNames() []string {
	names := make([]string, len(c.synthesizers))
	for i, s := range c.synthesizers {
		names[i] = s.Name()
	}
	return names
}

// SynthesizeCallbackEdges implements resolution.CallbackSynthesizer.
// It runs every registered Synthesizer, stamps each proposed edge, dedups,
// and persists in one transaction.
func (c *Composite) SynthesizeCallbackEdges(ctx context.Context) error {
	// Collect all existing synth edges for dedup (source>target → true).
	existingEdges, err := c.loadExistingSynthEdges(ctx)
	if err != nil {
		return fmt.Errorf("synthesis: load existing edges: %w", err)
	}

	// Run each synthesizer and collect stamped, deduped proposals.
	// Dedup map: "source>target" → true (within this run + existing DB).
	seen := make(map[string]bool, len(existingEdges))
	for key := range existingEdges {
		seen[key] = true
	}

	var toInsert []types.Edge

	for _, s := range c.synthesizers {
		proposed, err := s.Synthesize(ctx, c.db)
		if err != nil {
			return fmt.Errorf("synthesis: %s: %w", s.Name(), err)
		}

		for _, e := range proposed {
			if e.Source == "" || e.Target == "" {
				continue // guard against malformed proposals
			}
			key := e.Source + ">" + e.Target
			if seen[key] {
				continue // dedup
			}
			seen[key] = true

			// Stamp kind + provenance.
			e.Kind = types.EdgeKindCalls
			e.Provenance = "heuristic"

			// Build metadata: synthesizedBy + any fields the synthesizer set.
			meta := buildMeta(s.Name(), e.Metadata)
			e.Metadata = meta

			toInsert = append(toInsert, e)
		}
	}

	if len(toInsert) == 0 {
		return nil
	}

	// Persist in one transaction.
	return c.db.WithTx(ctx, func(tx *db.Tx) error {
		for _, e := range toInsert {
			if _, err := tx.InsertEdge(ctx, e); err != nil {
				return fmt.Errorf("insert synth edge %s→%s: %w", e.Source, e.Target, err)
			}
		}
		return nil
	})
}

// loadExistingSynthEdges returns the set of "source>target" keys for all
// existing heuristic edges. Uses a single provenance-filtered query instead
// of an O(N-nodes) per-node scan.
func (c *Composite) loadExistingSynthEdges(ctx context.Context) (map[string]bool, error) {
	edges, err := c.db.GetEdgesByProvenance(ctx, "heuristic")
	if err != nil {
		return nil, err
	}
	existing := make(map[string]bool, len(edges))
	for _, e := range edges {
		existing[e.Source+">"+e.Target] = true
	}
	return existing, nil
}

// unresolvedRefsBatchSize is the page size used by loadAllUnresolvedRefs.
// 5 000 rows keeps peak memory bounded while keeping round-trips low.
const unresolvedRefsBatchSize = 5000

// loadAllUnresolvedRefs pages through the unresolved_refs table in bounded
// batches and returns the full set. This avoids loading the entire table into
// memory in a single query (OOM risk on large repos).
// calleeOf returns the full callee expression a ref was written with
// ("emitter.on", "this.setState", "router.Use") — the form the callback
// synthesizers pattern-match on, including the receiver. It prefers CalleeExpr
// (set by the extractor for member/selector calls) and falls back to
// ReferenceName for plain calls, refs from pre-v3 indexes, and seeded test refs.
func calleeOf(ref types.UnresolvedReference) string {
	if ref.CalleeExpr != "" {
		return ref.CalleeExpr
	}
	return ref.ReferenceName
}

func loadAllUnresolvedRefs(ctx context.Context, d *db.DB) ([]types.UnresolvedReference, error) {
	var all []types.UnresolvedReference
	offset := 0
	for {
		batch, err := d.GetUnresolvedRefs(ctx, unresolvedRefsBatchSize, offset)
		if err != nil {
			return nil, err
		}
		all = append(all, batch...)
		if len(batch) < unresolvedRefsBatchSize {
			break
		}
		offset += unresolvedRefsBatchSize
	}
	return all, nil
}

// buildMeta merges the synthesizer name into metadata. If the synthesizer
// already set metadata (as a JSON object), we parse it and inject synthesizedBy.
// Otherwise we create a minimal {"synthesizedBy": name} object.
func buildMeta(name string, existing json.RawMessage) json.RawMessage {
	if len(existing) == 0 {
		return marshalMeta(synthMeta{SynthesizedBy: name})
	}
	// Parse existing metadata from synthesizer and inject synthesizedBy.
	var m map[string]any
	if err := json.Unmarshal(existing, &m); err != nil {
		// Fallback if malformed.
		return marshalMeta(synthMeta{SynthesizedBy: name})
	}
	m["synthesizedBy"] = name
	b, err := json.Marshal(m)
	if err != nil {
		return marshalMeta(synthMeta{SynthesizedBy: name})
	}
	return b
}

// ---------------------------------------------------------------------------
// react-render synthesizer
// ---------------------------------------------------------------------------

// ReactRenderSynthesizer implements the react-render synthesizer (appendix G).
//
// Signal: find unresolved_refs where ReferenceName ends with ".setState".
// For each: the FromNodeID is a method. Find the class that contains this method
// (via contains edges). If that class also has a "render" method, synthesize a
// calls edge from the setState-calling method → the render method.
//
// Metadata: {synthesizedBy: "react-render", via: "setState"}
//
// Cap: no per-synthesizer cap (each setState call → at most one render target).
type ReactRenderSynthesizer struct{}

func (r *ReactRenderSynthesizer) Name() string { return "react-render" }

func (r *ReactRenderSynthesizer) Synthesize(ctx context.Context, d *db.DB) ([]types.Edge, error) {
	// Get all unresolved_refs.
	refs, err := loadAllUnresolvedRefs(ctx, d)
	if err != nil {
		return nil, err
	}

	// Build a map of methodID → classID via contains edges (target→source).
	// We need all method nodes first.
	methodNodes, err := d.GetNodesByKind(ctx, types.NodeKindMethod)
	if err != nil {
		return nil, err
	}

	// For each method, find its containing class.
	methodToClass := make(map[string]string, len(methodNodes))
	for _, m := range methodNodes {
		edges, err := d.GetEdgesByTarget(ctx, m.ID)
		if err != nil {
			return nil, err
		}
		for _, e := range edges {
			if e.Kind == types.EdgeKindContains {
				methodToClass[m.ID] = e.Source
				break
			}
		}
	}

	// Build classID → render method ID map.
	// For each class, scan its methods for name="render".
	classToRender := make(map[string]string)
	for _, m := range methodNodes {
		if m.Name == "render" {
			if classID, ok := methodToClass[m.ID]; ok {
				classToRender[classID] = m.ID
			}
		}
	}

	// Find refs that are setState calls.
	var edges []types.Edge
	seen := make(map[string]bool)
	for _, ref := range refs {
		if ref.ReferenceKind != types.EdgeKindCalls {
			continue
		}
		if !strings.HasSuffix(calleeOf(ref), ".setState") {
			continue
		}
		fromID := ref.FromNodeID
		classID, ok := methodToClass[fromID]
		if !ok {
			continue
		}
		renderID, ok := classToRender[classID]
		if !ok {
			continue
		}
		// Don't emit self-loops (e.g. if render calls setState).
		if fromID == renderID {
			continue
		}
		key := fromID + ">" + renderID
		if seen[key] {
			continue
		}
		seen[key] = true

		meta := json.RawMessage(`{"via":"setState"}`)
		edges = append(edges, types.Edge{
			Source:   fromID,
			Target:   renderID,
			Metadata: meta,
		})
	}
	return edges, nil
}

// ---------------------------------------------------------------------------
// jsx-render synthesizer
// ---------------------------------------------------------------------------

// JSXRenderSynthesizer implements the jsx-render synthesizer (appendix G).
//
// # Signal
//
// EE1 (extraction enrichment 1) captures JSX child-component usages in .tsx
// and .jsx files. Each <ChildWidget/> encountered in a function/method body
// emits an UnresolvedReference with:
//
//   - ReferenceKind = "references"
//   - ReferenceName = "ChildWidget" (the PascalCase component name)
//   - Arguments[0]  = "jsx:ChildWidget" — the EE1 JSX discriminator
//
// Resolution turns these unresolved refs into static "references" edges. The
// createEdges function propagates Arguments into the edge's Metadata as
// {"refArgs":["jsx:ChildWidget"]} so synthesis can recover the discriminator
// without re-querying the already-deleted unresolved_ref.
//
// # How this synthesizer works
//
//  1. Walk all nodes. For each node, get its outgoing edges.
//  2. Filter edges: Kind == "references", empty Provenance, Metadata has
//     refArgs[0] with "jsx:" prefix.
//  3. Resolve the target node. Skip if target is not function/class/component.
//  4. Emit a (source, target) pair → the Composite stamps Kind="calls",
//     Provenance="heuristic", Metadata {"synthesizedBy":"jsx-render",
//     "registeredAt": line}.
//
// # No per-synthesizer cap
//
// jsx-render has no hard cap. Each JSX child usage in a render produces at
// most one edge (source→target). The Composite's global dedup (source>target)
// collapses duplicate usages of the same child in the same parent.
type JSXRenderSynthesizer struct{}

func (j *JSXRenderSynthesizer) Name() string { return "jsx-render" }

func (j *JSXRenderSynthesizer) Synthesize(ctx context.Context, d *db.DB) ([]types.Edge, error) {
	nodes, err := d.GetAllNodes(ctx)
	if err != nil {
		return nil, err
	}

	// Build a node kind index for O(1) target-kind lookup.
	nodeKind := make(map[string]types.NodeKind, len(nodes))
	for _, n := range nodes {
		nodeKind[n.ID] = n.Kind
	}

	var proposed []types.Edge
	seen := make(map[string]bool)

	for _, n := range nodes {
		edges, err := d.GetEdgesBySource(ctx, n.ID)
		if err != nil {
			return nil, err
		}
		for _, e := range edges {
			// Only static references edges (no provenance) with a JSX discriminator.
			if e.Kind != types.EdgeKindReferences || e.Provenance != "" {
				continue
			}
			if !hasJSXDiscriminator(e.Metadata) {
				continue
			}
			// Target must be a real component: function, class, or component node.
			tk, ok := nodeKind[e.Target]
			if !ok {
				continue
			}
			if tk != types.NodeKindFunction && tk != types.NodeKindClass && tk != types.NodeKindComponent {
				continue
			}
			key := e.Source + ">" + e.Target
			if seen[key] {
				continue
			}
			seen[key] = true

			// Carry registeredAt from the static edge's line number.
			var registeredAt string
			if e.Line > 0 {
				registeredAt = fmt.Sprintf("%d", e.Line)
			}
			var meta json.RawMessage
			if registeredAt != "" {
				meta, _ = json.Marshal(map[string]string{"registeredAt": registeredAt})
			}
			proposed = append(proposed, types.Edge{
				Source:   e.Source,
				Target:   e.Target,
				Metadata: meta,
			})
		}
	}
	return proposed, nil
}

// hasJSXDiscriminator returns true if the edge Metadata contains a refArgs
// array whose first element starts with "jsx:".
// This is the check that identifies EE1 JSX-origin static references edges.
func hasJSXDiscriminator(meta json.RawMessage) bool {
	if len(meta) == 0 {
		return false
	}
	var m map[string][]string
	if err := json.Unmarshal(meta, &m); err != nil {
		return false
	}
	args := m["refArgs"]
	return len(args) > 0 && strings.HasPrefix(args[0], "jsx:")
}

// ---------------------------------------------------------------------------
// vue-handler synthesizer
// ---------------------------------------------------------------------------

// VueHandlerSynthesizer implements the vue-handler synthesizer (appendix G).
//
// # Signal
//
// The standalone Vue SFC extractor (CP9, extraction/standalone) emits a
// component node for each .vue file (Language=vue, Kind=component) and uses
// extractTemplateRefs to emit UnresolvedReference values (Kind=references) for
// each PascalCase or kebab-case component tag found in the <template> block.
// Resolution turns these into static "references" edges from the Vue component
// node → the referenced child component node.
//
// # How this synthesizer works
//
//  1. Walk all component nodes whose Language == vue.
//  2. For each, get outgoing static references edges (Kind=references,
//     Provenance=""). These are the resolved template child references.
//  3. If the target is a function, class, or component node, emit a calls edge.
//
// # @event="handler" support
//
// The Vue extractor's extractHandlerRefs captures @event="handler" and
// v-on:event="handler" bindings in the <template> block, emitting
// UnresolvedReference values (Kind=references) from the component node to
// the handler method name. Resolution turns these into static references
// edges. This synthesizer then promotes those edges to heuristic calls edges,
// including edges to NodeKindMethod targets (Vue options-API methods live in
// the `methods:` object and are NodeKindMethod, not NodeKindFunction).
//
// # No per-synthesizer cap
//
// vue-handler has no hard cap per call site. The Composite's global dedup
// (source>target) collapses duplicate tag usages of the same child in one
// template.
type VueHandlerSynthesizer struct{}

func (v *VueHandlerSynthesizer) Name() string { return "vue-handler" }

func (v *VueHandlerSynthesizer) Synthesize(ctx context.Context, d *db.DB) ([]types.Edge, error) {
	// Walk component nodes with Vue language.
	compNodes, err := d.GetNodesByKind(ctx, types.NodeKindComponent)
	if err != nil {
		return nil, err
	}

	// Build a node kind index from all nodes for target-kind lookup.
	allNodes, err := d.GetAllNodes(ctx)
	if err != nil {
		return nil, err
	}
	nodeKind := make(map[string]types.NodeKind, len(allNodes))
	for _, n := range allNodes {
		nodeKind[n.ID] = n.Kind
	}

	var proposed []types.Edge
	seen := make(map[string]bool)

	for _, comp := range compNodes {
		if comp.Language != types.LanguageVue {
			continue
		}
		edges, err := d.GetEdgesBySource(ctx, comp.ID)
		if err != nil {
			return nil, err
		}
		for _, e := range edges {
			// Only static references edges from the template (no provenance,
			// no JSX discriminator — Vue template refs carry no Arguments).
			if e.Kind != types.EdgeKindReferences || e.Provenance != "" {
				continue
			}
			// Skip edges that carry the JSX discriminator (shouldn't happen
			// from Vue component nodes, but be defensive).
			if hasJSXDiscriminator(e.Metadata) {
				continue
			}
			// Target must be a callable or component node. Methods are included
			// because Vue <script> options-API handlers are NodeKindMethod
			// (JS method_definition nodes).
			tk, ok := nodeKind[e.Target]
			if !ok {
				continue
			}
			if tk != types.NodeKindFunction && tk != types.NodeKindMethod &&
				tk != types.NodeKindClass && tk != types.NodeKindComponent {
				continue
			}
			key := e.Source + ">" + e.Target
			if seen[key] {
				continue
			}
			seen[key] = true

			var meta json.RawMessage
			if e.Line > 0 {
				meta, _ = json.Marshal(map[string]string{"registeredAt": fmt.Sprintf("%d", e.Line)})
			}
			proposed = append(proposed, types.Edge{
				Source:   e.Source,
				Target:   e.Target,
				Metadata: meta,
			})
		}
	}
	return proposed, nil
}

// ---------------------------------------------------------------------------
// event-emitter synthesizer
// ---------------------------------------------------------------------------

// EventEmitterSynthesizer implements the event-emitter synthesizer (appendix G).
//
// # Signal (EE2)
//
// EE2 captures string-literal arguments of call_expression nodes into
// UnresolvedReference.Arguments. For emitter.on('login', handler), the
// unresolved ref has ReferenceName="emitter.on" and Arguments=["login"].
// Handler identity is NOT captured (only string-literal args are recorded).
//
// # Correlation
//
// Registration APIs: callee suffix .on / .addListener / .addEventListener
// Dispatch APIs:     callee suffix .emit / .dispatch
//
// For each (dispatchRef, registrationRef) pair sharing the same Arguments[0]
// (event name), emit an edge from the dispatch enclosing function →
// registration enclosing function.
//
// # Cap
//
// EVENT_FANOUT_CAP (6) registration sites per dispatch site per event name.
//
// # Granularity
//
// Since handler identity is not captured, edges are enclosing-function →
// enclosing-function (emit-site fn → on-site fn). Coarser but honest.
type EventEmitterSynthesizer struct{}

func (e *EventEmitterSynthesizer) Name() string { return "event-emitter" }

func (e *EventEmitterSynthesizer) Synthesize(ctx context.Context, d *db.DB) ([]types.Edge, error) {
	return synthesizeEventEdges(ctx, d, isEERegistration, isEEDispatch)
}

// isEERegistration returns true for generic event-emitter registration callees.
func isEERegistration(callee string) bool {
	return strings.HasSuffix(callee, ".on") ||
		strings.HasSuffix(callee, ".addListener") ||
		strings.HasSuffix(callee, ".addEventListener")
}

// isEEDispatch returns true for generic event-emitter dispatch callees.
func isEEDispatch(callee string) bool {
	return strings.HasSuffix(callee, ".emit") ||
		strings.HasSuffix(callee, ".dispatch")
}

// ---------------------------------------------------------------------------
// rn-event-channel synthesizer
// ---------------------------------------------------------------------------

// RNEventChannelSynthesizer implements the rn-event-channel synthesizer
// (appendix G, CP16 batch 3).
//
// # Signal (EE2)
//
// Same EE2 mechanism as EventEmitterSynthesizer. RN-specific callee patterns
// distinguish this synthesizer from the generic event-emitter.
//
// # Registration APIs
//
//   - DeviceEventEmitter.addListener('E', fn)
//   - NativeEventEmitter(...).addListener('E', fn) — callee ends with
//     ".addListener" and receiver contains "NativeEventEmitter"
//
// # Dispatch APIs
//
//   - DeviceEventEmitter.emit('E', ...)
//   - nativeModule.emit('E', ...) where callee contains "DeviceEventEmitter"
//   - sendEvent('E', ...) — bare sendEvent call
//
// Runs BEFORE EventEmitterSynthesizer in Default(d) so its synthesizedBy tag
// survives the Composite dedup (first-wins for a given source>target pair).
type RNEventChannelSynthesizer struct{}

func (r *RNEventChannelSynthesizer) Name() string { return "rn-event-channel" }

func (r *RNEventChannelSynthesizer) Synthesize(ctx context.Context, d *db.DB) ([]types.Edge, error) {
	return synthesizeEventEdges(ctx, d, isRNRegistration, isRNDispatch)
}

// isRNRegistration returns true for React-Native event registration callees.
func isRNRegistration(callee string) bool {
	if !strings.HasSuffix(callee, ".addListener") {
		return false
	}
	return strings.Contains(callee, "DeviceEventEmitter") ||
		strings.Contains(callee, "NativeEventEmitter")
}

// isRNDispatch returns true for React-Native event dispatch callees.
func isRNDispatch(callee string) bool {
	if callee == "sendEvent" {
		return true
	}
	if !strings.HasSuffix(callee, ".emit") {
		return false
	}
	return strings.Contains(callee, "DeviceEventEmitter") ||
		strings.Contains(callee, "NativeEventEmitter")
}

// ---------------------------------------------------------------------------
// shared event-edge synthesis helper
// ---------------------------------------------------------------------------

// synthesizeEventEdges is the common implementation for EventEmitterSynthesizer
// and RNEventChannelSynthesizer. It scans all unresolved_refs, classifies each
// as registration or dispatch using the provided predicates, groups by event
// name, and emits capped, deduped edges.
//
// Edge direction: dispatchFromNodeID → registrationFromNodeID (emit-site fn →
// on-site fn), correlated by Arguments[0] (event name).
func synthesizeEventEdges(
	ctx context.Context,
	d *db.DB,
	isRegistration func(string) bool,
	isDispatch func(string) bool,
) ([]types.Edge, error) {
	refs, err := loadAllUnresolvedRefs(ctx, d)
	if err != nil {
		return nil, err
	}

	// Group by event name.
	// registrations: eventName → []fromNodeID (the enclosing function)
	// dispatches:    eventName → []fromNodeID (the enclosing function)
	type refEntry struct{ fromNodeID string }
	registrations := make(map[string][]refEntry)
	dispatches := make(map[string][]refEntry)

	for _, ref := range refs {
		if ref.ReferenceKind != types.EdgeKindCalls {
			continue
		}
		if len(ref.Arguments) == 0 || ref.Arguments[0] == "" {
			continue // no event name captured — skip
		}
		eventName := ref.Arguments[0]
		callee := calleeOf(ref)

		switch {
		case isRegistration(callee):
			registrations[eventName] = append(registrations[eventName], refEntry{ref.FromNodeID})
		case isDispatch(callee):
			dispatches[eventName] = append(dispatches[eventName], refEntry{ref.FromNodeID})
		}
	}

	seen := make(map[string]bool)
	var edges []types.Edge

	for eventName, dispatchRefs := range dispatches {
		regRefs, ok := registrations[eventName]
		if !ok {
			continue
		}
		// Apply EVENT_FANOUT_CAP per event × dispatch site.
		for _, dispatch := range dispatchRefs {
			count := 0
			for _, reg := range regRefs {
				if count >= EVENT_FANOUT_CAP {
					break
				}
				src := dispatch.fromNodeID
				tgt := reg.fromNodeID
				if src == tgt {
					continue // skip self-loops
				}
				key := src + ">" + tgt
				if seen[key] {
					continue
				}
				seen[key] = true
				count++

				meta, _ := json.Marshal(map[string]string{"event": eventName})
				edges = append(edges, types.Edge{
					Source:   src,
					Target:   tgt,
					Metadata: meta,
				})
			}
		}
	}
	return edges, nil
}

// ---------------------------------------------------------------------------
// callback synthesizer (EE3 — field-backed observer)
// ---------------------------------------------------------------------------

// CallbackSynthesizer implements the callback (field-backed observer)
// synthesizer (appendix G, CP16 batch 4).
//
// # Signal (EE3)
//
// EE3 (extraction enrichment 3) captures `this.someField = callable` patterns
// as a "references"-kind UnresolvedReference with Arguments=["field:someField"].
// After resolution this becomes a static references edge from the assigning
// method to the callable node, with Metadata={"refArgs":["field:someField"]}.
//
// Invocation sites (`this.someField(...)`) remain in unresolved_refs as
// "calls"-kind refs with ReferenceName="this.someField" (or bare "someField")
// because the field name is not a standalone node name and is never resolved.
//
// # Correlation strategy
//
//  1. Walk all static references edges whose Metadata["refArgs"][0] has a
//     "field:" prefix. Extract fieldName = suffix after "field:".
//     Map fieldName → []callableTargetID.
//  2. Scan all unresolved_refs for "calls"-kind refs where ReferenceName ends
//     with "."+fieldName or equals fieldName. The FromNodeID is the invoker.
//  3. Synthesize a calls+heuristic edge: invoker → callableTargetID.
//     Metadata hint: {field: fieldName} (synthesizedBy stamped by Composite).
//  4. Cap: MAX_CALLBACKS_PER_CHANNEL per field channel (40).
//  5. Skip self-loops (invoker == callableTargetID).
//
// # Dedup
//
// Handled by the Composite (source>target, idempotent across runs).
type CallbackSynthesizer struct{}

func (c *CallbackSynthesizer) Name() string { return "callback" }

func (c *CallbackSynthesizer) Synthesize(ctx context.Context, d *db.DB) ([]types.Edge, error) {
	// Step 1: walk all nodes and collect static references edges with refArgs
	// carrying a "field:" prefix. Build fieldName → []callableTargetID map.
	allNodes, err := d.GetAllNodes(ctx)
	if err != nil {
		return nil, err
	}

	// fieldTargets: fieldName → list of callable node IDs registered to that field.
	fieldTargets := make(map[string][]string)

	for _, n := range allNodes {
		edges, err := d.GetEdgesBySource(ctx, n.ID)
		if err != nil {
			return nil, err
		}
		for _, e := range edges {
			if e.Kind != types.EdgeKindReferences {
				continue
			}
			if len(e.Metadata) == 0 {
				continue
			}
			// Parse {"refArgs":["field:fieldName",...]}
			var meta struct {
				RefArgs []string `json:"refArgs"`
			}
			if err := json.Unmarshal(e.Metadata, &meta); err != nil {
				continue
			}
			if len(meta.RefArgs) == 0 || !strings.HasPrefix(meta.RefArgs[0], "field:") {
				continue
			}
			fieldName := strings.TrimPrefix(meta.RefArgs[0], "field:")
			if fieldName == "" {
				continue
			}
			fieldTargets[fieldName] = append(fieldTargets[fieldName], e.Target)
		}
	}

	if len(fieldTargets) == 0 {
		return nil, nil
	}

	// Step 2: scan all unresolved_refs for calls-kind refs whose ReferenceName
	// matches a known field name (either "this.fieldName" or bare "fieldName").
	allRefs, err := loadAllUnresolvedRefs(ctx, d)
	if err != nil {
		return nil, err
	}

	// invokersByField: fieldName → set of invoker node IDs.
	type pair struct{ invoker, target, field string }
	var proposals []pair

	for _, ref := range allRefs {
		if ref.ReferenceKind != types.EdgeKindCalls {
			continue
		}
		name := calleeOf(ref)
		// Match "this.fieldName" (suffix match) or bare "fieldName" (exact match).
		for fieldName, targets := range fieldTargets {
			if name != fieldName && !strings.HasSuffix(name, "."+fieldName) {
				continue
			}
			for _, tgt := range targets {
				if ref.FromNodeID == tgt {
					continue // skip self-loop
				}
				proposals = append(proposals, pair{ref.FromNodeID, tgt, fieldName})
			}
		}
	}

	// Step 3: group by field channel and apply MAX_CALLBACKS_PER_CHANNEL cap.
	// channel key: fieldName+">"+targetID  — one "channel" per (field, callable) pair.
	channelCount := make(map[string]int)
	// within-synthesizer dedup: invoker+">"+target.
	seen := make(map[string]bool)
	var edges []types.Edge

	for _, p := range proposals {
		dedupKey := p.invoker + ">" + p.target
		if seen[dedupKey] {
			continue
		}
		channelKey := p.field + ">" + p.target
		if channelCount[channelKey] >= MAX_CALLBACKS_PER_CHANNEL {
			continue
		}
		seen[dedupKey] = true
		channelCount[channelKey]++

		meta, _ := json.Marshal(map[string]string{"field": p.field})
		edges = append(edges, types.Edge{
			Source:   p.invoker,
			Target:   p.target,
			Metadata: meta,
		})
	}

	return edges, nil
}

// ---------------------------------------------------------------------------
// closure-collection synthesizer (EE5 activated)
// ---------------------------------------------------------------------------

// ClosureCollectionSynthesizer implements the closure-collection synthesizer
// (appendix G, CP16 batch 4). Activated by EE5 which captures identifier
// arguments (e.g. handler in .append(handler)) with the "arg:" prefix.
//
// # Signal (EE5)
//
// EE5 extends EE2: call_expression refs now also record identifier arguments
// with an "arg:" prefix in Arguments. For `handlers.append(onLogin)`, the
// unresolved ref has ReferenceName="handlers.append" and
// Arguments=["arg:onLogin"].
//
// # Correlation strategy
//
//  1. Scan all calls-kind unresolved_refs.
//  2. Classify each ref as append-side or forEach-side:
//     - Append-side: callee suffix is ".append" or ".add" and at least one
//     Arguments entry has the "arg:" prefix.
//     - forEach-side: callee suffix is ".forEach" or ".each".
//  3. Extract the receiver from each callee (everything before the last "."):
//     "handlers.append" → "handlers", "handlers.forEach" → "handlers".
//  4. Group: receiverName → []handlerName (from append args) and
//     receiverName → []forEachFromNodeID.
//  5. For each matching receiver: forEach-enclosing-fn → handler node.
//     Resolve handlerName to a DB node via GetNodesByName. If no node found
//     (anonymous closure, undeclared local), skip — honest gap.
//  6. Apply CC_FANOUT_CAP (8) per receiver channel.
//
// # Anonymous closure gap (documented)
//
// Swift `handlers.append { ... }` and Kotlin `handlers.add { x -> ... }` pass
// an anonymous block — no identifier is captured by EE5, so Arguments carries
// no "arg:" entry. No edge is emitted. This is by design: fabricating an edge
// to an unknown target would be misleading.
//
// # Dedup
//
// Handled by the Composite (source>target, idempotent across runs).
type ClosureCollectionSynthesizer struct{}

func (c *ClosureCollectionSynthesizer) Name() string { return "closure-collection" }

func (c *ClosureCollectionSynthesizer) Synthesize(ctx context.Context, d *db.DB) ([]types.Edge, error) {
	refs, err := loadAllUnresolvedRefs(ctx, d)
	if err != nil {
		return nil, err
	}

	// appendHandlers: receiver → []handlerName (from "arg:" prefixed Arguments).
	appendHandlers := make(map[string][]string)
	// forEachFroms: receiver → []fromNodeID (enclosing fn of the forEach call).
	forEachFroms := make(map[string][]string)

	for _, ref := range refs {
		if ref.ReferenceKind != types.EdgeKindCalls {
			continue
		}
		callee := calleeOf(ref)
		dotIdx := strings.LastIndex(callee, ".")
		if dotIdx < 0 {
			continue
		}
		receiver := callee[:dotIdx]
		suffix := callee[dotIdx+1:]

		switch {
		case suffix == "append" || suffix == "add":
			// Collect any "arg:" prefixed entries as handler identifiers.
			for _, arg := range ref.Arguments {
				if strings.HasPrefix(arg, "arg:") {
					handlerName := strings.TrimPrefix(arg, "arg:")
					if handlerName != "" {
						appendHandlers[receiver] = append(appendHandlers[receiver], handlerName)
					}
				}
			}
		case suffix == "forEach" || suffix == "each":
			forEachFroms[receiver] = append(forEachFroms[receiver], ref.FromNodeID)
		}
	}

	if len(appendHandlers) == 0 || len(forEachFroms) == 0 {
		return nil, nil
	}

	// Resolve handler names to node IDs. Build a cache to avoid redundant lookups.
	handlerNodeIDs := make(map[string][]string) // handlerName → []nodeID
	for _, handlers := range appendHandlers {
		for _, name := range handlers {
			if _, seen := handlerNodeIDs[name]; seen {
				continue
			}
			nodes, err := d.GetNodesByName(ctx, name, "")
			if err != nil {
				return nil, err
			}
			ids := make([]string, 0, len(nodes))
			for _, n := range nodes {
				ids = append(ids, n.ID)
			}
			handlerNodeIDs[name] = ids
		}
	}

	seen := make(map[string]bool)
	var edges []types.Edge

	for receiver, handlers := range appendHandlers {
		forEachSites, ok := forEachFroms[receiver]
		if !ok {
			continue
		}
		// Collect unique handler node IDs for this receiver.
		var handlerIDs []string
		seenHandler := make(map[string]bool)
		for _, handlerName := range handlers {
			for _, nodeID := range handlerNodeIDs[handlerName] {
				if !seenHandler[nodeID] {
					seenHandler[nodeID] = true
					handlerIDs = append(handlerIDs, nodeID)
				}
			}
		}
		if len(handlerIDs) == 0 {
			continue // all handlers are anonymous — honest gap
		}

		// Emit forEach-caller → handler edges, capped per receiver channel.
		channelCount := 0
		for _, forEachFrom := range forEachSites {
			for _, handlerID := range handlerIDs {
				if channelCount >= CC_FANOUT_CAP {
					break
				}
				if forEachFrom == handlerID {
					continue // skip self-loops
				}
				key := forEachFrom + ">" + handlerID
				if seen[key] {
					continue
				}
				seen[key] = true
				channelCount++
				edges = append(edges, types.Edge{
					Source: forEachFrom,
					Target: handlerID,
				})
			}
			if channelCount >= CC_FANOUT_CAP {
				break
			}
		}
	}

	return edges, nil
}

// ---------------------------------------------------------------------------
// flutter-build synthesizer (documented gap — Dart calls absent)
// ---------------------------------------------------------------------------

// FlutterBuildSynthesizer implements the flutter-build (Dart setState→build)
// synthesizer (appendix G, CP16 batch 4).
//
// # Gap
//
// SIGNAL ABSENT: The Dart tree-sitter grammar binding used by this pipeline
// has no call_expression node (documented in languages/dart.go: "BLOCKED:
// Dart grammar has no call_expression node. CallTypes is left empty.").
// setState calls are not captured as unresolved refs. Without a setState
// invocation signal, there is no way to correlate the calling method with
// the build method.
//
// When the Dart grammar is upgraded to expose call_expression nodes (or an
// alternative extraction strategy captures setState), this synthesizer can
// be implemented: find unresolved calls to "setState" within a State subclass,
// find the sibling "build" method, and synthesize setState-caller → build.
type FlutterBuildSynthesizer struct{}

func (f *FlutterBuildSynthesizer) Name() string { return "flutter-build" }

func (f *FlutterBuildSynthesizer) Synthesize(_ context.Context, _ *db.DB) ([]types.Edge, error) {
	// SIGNAL ABSENT: Dart grammar has no call_expression → setState not captured.
	// Activate when the Dart extraction pipeline captures setState calls.
	return nil, nil
}

// ---------------------------------------------------------------------------
// interface-impl synthesizer (real — activated after EE4)
// ---------------------------------------------------------------------------

// InterfaceImplSynthesizer implements the interface-impl synthesizer
// (appendix G, CP16 batch 5).
//
// # Signal (EE4)
//
// EE4 wired heritage extraction for TypeScript, C++, and Java. TypeScript
// extractClass() now walks class_heritage and implements_clause, emitting
// EdgeKindImplements UnresolvedReferences for each implemented interface. CP13
// resolution creates the EdgeKindImplements edges. Interface method declarations
// (method_signature) are in MethodTypes — so interface method nodes exist.
//
// # How this synthesizer works
//
//  1. Build methodToClass map: for each method node, find its container via
//     EdgeKindContains edges (GetEdgesByTarget filtered to Contains).
//  2. Build classToMethods map: classID → map[name]methodID.
//  3. For each implements edge C→I: get I's method nodes; for each I.m,
//     look up a method named I.m.Name in C's method map; if found, emit
//     a (I.m, C.m) proposed edge. synthesizedBy="interface-impl".
//
// # No cap
//
// Each interface method dispatches to at most one implementing method per
// class. The Composite's global dedup handles source>target collisions.
type InterfaceImplSynthesizer struct{}

func (s *InterfaceImplSynthesizer) Name() string { return "interface-impl" }

func (s *InterfaceImplSynthesizer) Synthesize(ctx context.Context, d *db.DB) ([]types.Edge, error) {
	// Step 1: get all method nodes and build methodToClass + classToMethods maps.
	methodNodes, err := d.GetNodesByKind(ctx, types.NodeKindMethod)
	if err != nil {
		return nil, err
	}

	// methodToClass: methodID → classID (via contains edge class→method).
	methodToClass := make(map[string]string, len(methodNodes))
	for _, m := range methodNodes {
		incoming, err := d.GetEdgesByTarget(ctx, m.ID)
		if err != nil {
			return nil, err
		}
		for _, e := range incoming {
			if e.Kind == types.EdgeKindContains {
				methodToClass[m.ID] = e.Source
				break
			}
		}
	}

	// classToMethods: classID → {methodName → methodID}.
	classToMethods := make(map[string]map[string]string)
	for _, m := range methodNodes {
		classID, ok := methodToClass[m.ID]
		if !ok {
			continue
		}
		if classToMethods[classID] == nil {
			classToMethods[classID] = make(map[string]string)
		}
		classToMethods[classID][m.Name] = m.ID
	}

	// Step 2: find all implements edges (C→I) by scanning class nodes.
	classNodes, err := d.GetNodesByKind(ctx, types.NodeKindClass)
	if err != nil {
		return nil, err
	}
	// Also scan interface nodes (interface B extends A → implements B→A after promotion).
	ifaceNodes, err := d.GetNodesByKind(ctx, types.NodeKindInterface)
	if err != nil {
		return nil, err
	}
	classLike := append(classNodes, ifaceNodes...)

	// interfaceToMethods: interfaceID → []methodID (method nodes contained by it).
	interfaceToMethods := make(map[string][]string, len(ifaceNodes))
	for _, m := range methodNodes {
		classID, ok := methodToClass[m.ID]
		if !ok {
			continue
		}
		// If the container is an interface node, it belongs in interfaceToMethods.
		// We need to check container kind. Build a quick set of interface node IDs.
		interfaceToMethods[classID] = append(interfaceToMethods[classID], m.ID)
	}
	// Filter interfaceToMethods to only interface-kind containers.
	ifaceIDSet := make(map[string]bool, len(ifaceNodes))
	for _, n := range ifaceNodes {
		ifaceIDSet[n.ID] = true
	}
	for containerID := range interfaceToMethods {
		if !ifaceIDSet[containerID] {
			delete(interfaceToMethods, containerID)
		}
	}

	// Build a method name index per interface for O(1) lookup.
	// interfaceMethodNames: interfaceID → {methodName → methodID}.
	interfaceMethodNames := make(map[string]map[string]string, len(ifaceNodes))
	for ifaceID, methodIDs := range interfaceToMethods {
		nameMap := make(map[string]string, len(methodIDs))
		for _, mid := range methodIDs {
			// Find the method's name by scanning methodNodes.
			for _, m := range methodNodes {
				if m.ID == mid {
					nameMap[m.Name] = mid
					break
				}
			}
		}
		interfaceMethodNames[ifaceID] = nameMap
	}

	seen := make(map[string]bool)
	var edges []types.Edge

	for _, node := range classLike {
		outgoing, err := d.GetEdgesBySource(ctx, node.ID)
		if err != nil {
			return nil, err
		}
		for _, e := range outgoing {
			if e.Kind != types.EdgeKindImplements {
				continue
			}
			classID := node.ID
			ifaceID := e.Target

			ifaceMethods, ok := interfaceMethodNames[ifaceID]
			if !ok || len(ifaceMethods) == 0 {
				continue
			}
			classMethods, ok := classToMethods[classID]
			if !ok {
				continue
			}
			for methodName, ifaceMethodID := range ifaceMethods {
				classMethodID, ok := classMethods[methodName]
				if !ok {
					continue
				}
				if ifaceMethodID == classMethodID {
					continue // no self-loops
				}
				key := ifaceMethodID + ">" + classMethodID
				if seen[key] {
					continue
				}
				seen[key] = true
				edges = append(edges, types.Edge{
					Source: ifaceMethodID,
					Target: classMethodID,
				})
			}
		}
	}
	return edges, nil
}

// ---------------------------------------------------------------------------
// cpp-override synthesizer (real — activated after EE4)
// ---------------------------------------------------------------------------

// CppOverrideSynthesizer implements the cpp-override (C++ vtable override)
// synthesizer (appendix G, CP16 batch 5).
//
// # Signal (EE4)
//
// EE4 wired C++ heritage extraction: CppExtractor() now walks base_class_clause
// and emits EdgeKindExtends UnresolvedReferences for each base class. CP13
// resolution creates EdgeKindExtends edges D→B.
//
// # How this synthesizer works
//
//  1. Get all C++ class nodes. For each, get outgoing extends edges.
//  2. For each extends edge D→B where the B node has Language==cpp: get B's
//     member functions (NodeKindFunction or NodeKindMethod contained by B).
//  3. For each base member function B.m, look for a D.m with the same name
//     contained by D; emit a (B.m, D.m) proposed edge.
//     synthesizedBy="cpp-override".
//
// # C++ language scope
//
// Scoped to Language==cpp on the base node: does not fire on TypeScript or
// Java extends edges. C++ extraction uses NodeKindFunction for member functions
// (CppExtractor has no MethodTypes); this synthesizer handles both
// NodeKindFunction and NodeKindMethod for forward compatibility.
//
// # No cap
//
// Each base method overrides at most one derived method per class pair. The
// Composite's global dedup handles source>target collisions.
type CppOverrideSynthesizer struct{}

func (s *CppOverrideSynthesizer) Name() string { return "cpp-override" }

func (s *CppOverrideSynthesizer) Synthesize(ctx context.Context, d *db.DB) ([]types.Edge, error) {
	// Step 1: build a container lookup for method/function nodes.
	// memberToClass: memberID → classID (via contains edge class→member).
	// classMethods: classID → {name → memberID}.
	allNodes, err := d.GetAllNodes(ctx)
	if err != nil {
		return nil, err
	}

	nodeByID := make(map[string]types.Node, len(allNodes))
	for _, n := range allNodes {
		nodeByID[n.ID] = n
	}

	// isCallable returns true for node kinds that represent callable members.
	isCallable := func(kind types.NodeKind) bool {
		return kind == types.NodeKindFunction || kind == types.NodeKindMethod
	}

	// Build classMethods from contains edges on class nodes.
	// classMethods: classID → {memberName → memberID}.
	classMethods := make(map[string]map[string]string)
	for _, n := range allNodes {
		if n.Kind != types.NodeKindClass {
			continue
		}
		outgoing, err := d.GetEdgesBySource(ctx, n.ID)
		if err != nil {
			return nil, err
		}
		for _, e := range outgoing {
			if e.Kind != types.EdgeKindContains {
				continue
			}
			member, ok := nodeByID[e.Target]
			if !ok || !isCallable(member.Kind) {
				continue
			}
			if classMethods[n.ID] == nil {
				classMethods[n.ID] = make(map[string]string)
			}
			classMethods[n.ID][member.Name] = member.ID
		}
	}

	// Step 2: find C++ extends edges (D→B where B.Language==cpp).
	seen := make(map[string]bool)
	var edges []types.Edge

	for _, n := range allNodes {
		if n.Kind != types.NodeKindClass {
			continue
		}
		outgoing, err := d.GetEdgesBySource(ctx, n.ID)
		if err != nil {
			return nil, err
		}
		for _, e := range outgoing {
			if e.Kind != types.EdgeKindExtends {
				continue
			}
			derivedID := n.ID
			baseID := e.Target

			base, ok := nodeByID[baseID]
			if !ok || base.Language != types.LanguageCpp {
				continue // scope to C++ only
			}

			baseMethods, ok := classMethods[baseID]
			if !ok || len(baseMethods) == 0 {
				continue
			}
			derivedMethodsMap, ok := classMethods[derivedID]
			if !ok {
				continue
			}

			for methodName, baseMethodID := range baseMethods {
				derivedMethodID, ok := derivedMethodsMap[methodName]
				if !ok {
					continue
				}
				if baseMethodID == derivedMethodID {
					continue // no self-loops
				}
				key := baseMethodID + ">" + derivedMethodID
				if seen[key] {
					continue
				}
				seen[key] = true
				edges = append(edges, types.Edge{
					Source: baseMethodID,
					Target: derivedMethodID,
				})
			}
		}
	}
	return edges, nil
}

// ---------------------------------------------------------------------------
// gin-middleware-chain synthesizer (CP16 batch 6 — real)
// ---------------------------------------------------------------------------

// GinMiddlewareChainSynthesizer implements the gin-middleware-chain synthesizer
// (appendix G, CP16 batch 6).
//
// # Signal (EE5 + CP15 route nodes)
//
// EE5 captures `r.Use(authMiddleware)` as a calls-kind UnresolvedReference with
// ReferenceName ending in ".Use" and Arguments containing "arg:authMiddleware".
// CP15 Gin resolver emits NodeKindRoute nodes (Language=go) for each route
// registration (r.GET, r.POST, etc.) in the same file.
//
// # Correlation
//
// File-level heuristic: all route nodes (NodeKindRoute, Language=go) in the
// same file as a .Use() call are assumed to be protected by the registered
// middleware. Synthesize a calls+heuristic edge from each route node to the
// middleware function node.
//
// The middleware identifier (e.g. "authMiddleware") from the "arg:" entry is
// resolved to a function or method node via GetNodesByName. If no node is found
// (unresolved or external middleware), no edge is emitted — honest gap.
//
// # Go-only scope
//
// Scoped to Language=go route nodes. Does not fire on TypeScript or Java nodes.
//
// # No cap
//
// Each route node connects to at most one instance of each named middleware.
// The Composite's global dedup handles source>target collisions.
type GinMiddlewareChainSynthesizer struct{}

func (g *GinMiddlewareChainSynthesizer) Name() string { return "gin-middleware-chain" }

func (g *GinMiddlewareChainSynthesizer) Synthesize(ctx context.Context, d *db.DB) ([]types.Edge, error) {
	// Step 1: collect all .Use() calls with "arg:" arguments (EE5).
	// usesByFile: filePath → []middlewareName
	refs, err := loadAllUnresolvedRefs(ctx, d)
	if err != nil {
		return nil, err
	}

	usesByFile := make(map[string][]string)
	for _, ref := range refs {
		if ref.ReferenceKind != types.EdgeKindCalls {
			continue
		}
		if !strings.HasSuffix(calleeOf(ref), ".Use") {
			continue
		}
		if ref.Language != types.LanguageGo {
			continue
		}
		for _, arg := range ref.Arguments {
			if strings.HasPrefix(arg, "arg:") {
				name := strings.TrimPrefix(arg, "arg:")
				if name != "" {
					usesByFile[ref.FilePath] = append(usesByFile[ref.FilePath], name)
				}
			}
		}
	}

	if len(usesByFile) == 0 {
		return nil, nil
	}

	// Step 2: collect all Go route nodes grouped by file.
	routeNodes, err := d.GetNodesByKind(ctx, types.NodeKindRoute)
	if err != nil {
		return nil, err
	}

	routesByFile := make(map[string][]string)
	for _, n := range routeNodes {
		if n.Language != types.LanguageGo {
			continue
		}
		routesByFile[n.FilePath] = append(routesByFile[n.FilePath], n.ID)
	}

	if len(routesByFile) == 0 {
		return nil, nil
	}

	// Step 3: resolve middleware names to Go function/method node IDs.
	// Use kind-specific queries (function + method) so the DB filters by kind
	// rather than returning all-kind nodes for in-memory filtering.
	middlewareNodeIDs := make(map[string][]string)
	for _, names := range usesByFile {
		for _, name := range names {
			if _, ok := middlewareNodeIDs[name]; ok {
				continue
			}
			fns, err := d.GetNodesByName(ctx, name, types.NodeKindFunction)
			if err != nil {
				return nil, err
			}
			meths, err := d.GetNodesByName(ctx, name, types.NodeKindMethod)
			if err != nil {
				return nil, err
			}
			ids := make([]string, 0, len(fns)+len(meths))
			for _, n := range fns {
				if n.Language == types.LanguageGo {
					ids = append(ids, n.ID)
				}
			}
			for _, n := range meths {
				if n.Language == types.LanguageGo {
					ids = append(ids, n.ID)
				}
			}
			middlewareNodeIDs[name] = ids
		}
	}

	// Step 4: for each file with both .Use() calls and route nodes, emit edges.
	// File paths may be stored as relative (from indexer) or absolute (from framework
	// extractor). We match by suffix: a relative path "router.go" matches an absolute
	// "/abs/path/router.go" when the absolute ends with "/" + relative.
	seen := make(map[string]bool)
	var edges []types.Edge

	for useFilePath, middlewareNames := range usesByFile {
		// Collect route IDs for all route-node file paths that match useFilePath.
		var matchedRouteIDs []string
		for routeFilePath, routeIDs := range routesByFile {
			if routeFilePath == useFilePath ||
				strings.HasSuffix(routeFilePath, "/"+useFilePath) ||
				strings.HasSuffix(useFilePath, "/"+routeFilePath) {
				matchedRouteIDs = append(matchedRouteIDs, routeIDs...)
			}
		}
		if len(matchedRouteIDs) == 0 {
			continue
		}
		for _, middlewareName := range middlewareNames {
			nodeIDs := middlewareNodeIDs[middlewareName]
			for _, routeID := range matchedRouteIDs {
				for _, middlewareID := range nodeIDs {
					if routeID == middlewareID {
						continue
					}
					key := routeID + ">" + middlewareID
					if seen[key] {
						continue
					}
					seen[key] = true
					edges = append(edges, types.Edge{
						Source: routeID,
						Target: middlewareID,
					})
				}
			}
		}
	}
	return edges, nil
}

// ---------------------------------------------------------------------------
// go-grpc-stub-impl synthesizer (CP16 batch 6 — documented stub)
// ---------------------------------------------------------------------------

// GoGRPCStubImplSynthesizer implements the go-grpc-stub-impl synthesizer
// (appendix G, CP16 batch 6).
//
// # Gap (documented)
//
// Three missing signals prevent implementation:
//
//  1. Go interface method signatures (inside interface_type) are NOT extracted
//     as method nodes. The Go extractor's MethodTypes captures only
//     method_declaration nodes (concrete methods with a receiver). Interface
//     method signatures (method_spec inside interface_type) are not captured.
//
//  2. RegisterFooServer(s, &fooImpl{}) — the impl type &fooImpl{} is a
//     composite literal, not a plain identifier. EE5 does not capture it.
//
//  3. Go uses structural typing: no explicit implements declarations, so no
//     EdgeKindImplements edges exist for Go in the current graph.
//
// Activate when: (a) Go interface method signatures are extracted, OR (b)
// composite literal type names are captured by an EE variant, AND (c)
// structural type-check results materialize as implements edges.
type GoGRPCStubImplSynthesizer struct{}

func (g *GoGRPCStubImplSynthesizer) Name() string { return "go-grpc-stub-impl" }

func (g *GoGRPCStubImplSynthesizer) Synthesize(_ context.Context, _ *db.DB) ([]types.Edge, error) {
	// Gap 1: Go interface method signatures not extracted as method nodes.
	// Gap 2: &fooImpl{} composite literal arg not captured by EE5.
	// Gap 3: No Go implements edges (structural typing).
	return nil, nil
}

// ---------------------------------------------------------------------------
// mybatis-java-xml synthesizer (CP16 batch 6 — real)
// ---------------------------------------------------------------------------

// MyBatisJavaXMLSynthesizer implements the mybatis-java-xml synthesizer
// (appendix G, CP16 batch 6).
//
// # Signal (CP9 MyBatis XML extractor + Java extractor)
//
// CP9 emits:
//   - A module node per XML mapper: kind=module, language=xml.
//   - A function node per SQL statement: kind=function, language=xml,
//     name=stmtId, qualifiedName="namespace.stmtId".
//   - Contains edges: module → each statement function.
//
// Java extractor emits:
//   - An interface node per mapper interface: kind=interface, language=java.
//   - A method node per interface method: kind=method, language=java,
//     contained by the interface via contains edges.
//
// # Correlation strategy
//
//  1. Find all XML function nodes (language=xml, kind=function).
//  2. For each: parse QualifiedName as "namespace.stmtId".
//     - namespace last segment = Java interface name (e.g. "UserMapper").
//     - stmtId = Java method name (e.g. "findUser").
//  3. Find Java interface with name = last segment of namespace.
//  4. Find Java method with name = stmtId contained by that interface.
//  5. Emit calls+heuristic edge: Java method → XML function.
//
// # Java+XML scope
//
// Source must be Java method; target must be XML function.
type MyBatisJavaXMLSynthesizer struct{}

func (m *MyBatisJavaXMLSynthesizer) Name() string { return "mybatis-java-xml" }

func (m *MyBatisJavaXMLSynthesizer) Synthesize(ctx context.Context, d *db.DB) ([]types.Edge, error) {
	allNodes, err := d.GetAllNodes(ctx)
	if err != nil {
		return nil, err
	}

	// Step 1: collect XML function nodes with QualifiedName.
	type xmlFunc struct {
		id, name, qualifiedName string
	}
	var xmlFuncs []xmlFunc
	for _, n := range allNodes {
		if n.Language == types.LanguageXML && n.Kind == types.NodeKindFunction && n.QualifiedName != "" {
			xmlFuncs = append(xmlFuncs, xmlFunc{n.ID, n.Name, n.QualifiedName})
		}
	}
	if len(xmlFuncs) == 0 {
		return nil, nil
	}

	// Step 2: Java interface index: interfaceName → interfaceID.
	javaInterfaces := make(map[string]string)
	for _, n := range allNodes {
		if n.Language == types.LanguageJava && n.Kind == types.NodeKindInterface {
			javaInterfaces[n.Name] = n.ID
		}
	}
	if len(javaInterfaces) == 0 {
		return nil, nil
	}

	// Step 3: interfaceMethods: interfaceID → {methodName → methodID}.
	// Build a target→contains-edges map in one load instead of O(N) queries.
	allEdges, err := d.GetAllEdges(ctx)
	if err != nil {
		return nil, err
	}
	// targetContains: targetNodeID → []source node IDs via contains edges.
	targetContains := make(map[string][]string, len(allEdges))
	for _, e := range allEdges {
		if e.Kind == types.EdgeKindContains {
			targetContains[e.Target] = append(targetContains[e.Target], e.Source)
		}
	}

	interfaceMethods := make(map[string]map[string]string)
	for _, n := range allNodes {
		if n.Language != types.LanguageJava || n.Kind != types.NodeKindMethod {
			continue
		}
		for _, srcID := range targetContains[n.ID] {
			if interfaceMethods[srcID] == nil {
				interfaceMethods[srcID] = make(map[string]string)
			}
			interfaceMethods[srcID][n.Name] = n.ID
			break
		}
	}

	// Step 4: correlate XML functions with Java methods.
	seen := make(map[string]bool)
	var edges []types.Edge

	for _, xf := range xmlFuncs {
		// Parse "namespace.stmtId" — stmtId = last segment after final dot.
		dotIdx := strings.LastIndex(xf.qualifiedName, ".")
		if dotIdx < 0 {
			continue
		}
		namespace := xf.qualifiedName[:dotIdx]
		stmtID := xf.qualifiedName[dotIdx+1:]
		if stmtID != xf.name {
			continue
		}

		// Java interface name = last segment of namespace.
		nsDotIdx := strings.LastIndex(namespace, ".")
		ifaceName := namespace
		if nsDotIdx >= 0 {
			ifaceName = namespace[nsDotIdx+1:]
		}
		if ifaceName == "" {
			continue
		}

		ifaceID, ok := javaInterfaces[ifaceName]
		if !ok {
			continue
		}
		methods, ok := interfaceMethods[ifaceID]
		if !ok {
			continue
		}
		javaMethodID, ok := methods[stmtID]
		if !ok {
			continue
		}

		key := javaMethodID + ">" + xf.id
		if seen[key] {
			continue
		}
		seen[key] = true
		edges = append(edges, types.Edge{
			Source: javaMethodID,
			Target: xf.id,
		})
	}
	return edges, nil
}

// ---------------------------------------------------------------------------
// fabric-native-impl synthesizer (CP16 batch 6 — documented stub)
// ---------------------------------------------------------------------------

// FabricNativeImplSynthesizer implements the fabric-native-impl synthesizer
// (appendix G, CP16 batch 6).
//
// # Gap (documented)
//
// Three missing signals prevent implementation:
//
//  1. No Fabric-specific native registration extraction. ObjC RCT_EXPORT_VIEW_PROPERTY,
//     Java @ReactModule, and C++ template specializations are not captured.
//
//  2. JS/TS codegenNativeComponent<T>("ComponentName") produces a string-literal
//     arg captured by EE2, but correlating it to native implementations requires
//     cross-language name resolution not present in the current graph.
//
//  3. No cross-language name resolution for ObjC/Java/C++ ↔ JS/TS component
//     names exists in the current pipeline.
//
// Activate when: native component registration is extracted AND a cross-language
// name correlation index exists.
type FabricNativeImplSynthesizer struct{}

func (f *FabricNativeImplSynthesizer) Name() string { return "fabric-native-impl" }

func (f *FabricNativeImplSynthesizer) Synthesize(_ context.Context, _ *db.DB) ([]types.Edge, error) {
	// Gap 1: No Fabric native registration capture (ObjC/Java/C++).
	// Gap 2: Cross-language name resolution absent.
	// Gap 3: No cross-language name resolution mechanism in current pipeline.
	return nil, nil
}
