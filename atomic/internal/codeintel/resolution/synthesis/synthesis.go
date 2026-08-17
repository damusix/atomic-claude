// Package synthesis infers dynamic-dispatch edges that static extraction
// cannot see — a setState call reaching render, an emit reaching its listener,
// an interface method reaching its implementation.
//
// Composite runs every registered Synthesizer after all static edges are
// persisted, since synthesizers read those edges. It stamps each proposal
// Kind='calls' / Provenance='heuristic', dedups on "source>target" against
// both this run and the DB, and persists in one transaction. That dedup is
// what makes re-running idempotent. Synthesizers add edges only, never nodes.
//
// Contract: docs/spec/code-intel-resolution.md.
package synthesis

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/db"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// Fan-out caps, named rather than inlined so tests assert the exact literals.

// MAX_CALLBACKS_PER_CHANNEL caps the callback (field-backed observer) synthesizer.
const MAX_CALLBACKS_PER_CHANNEL = 40

// EVENT_FANOUT_CAP caps the event-emitter synthesizer, per event name.
const EVENT_FANOUT_CAP = 6

// CC_FANOUT_CAP caps the closure-collection synthesizer, per receiver channel.
const CC_FANOUT_CAP = 8

// Synthesizer proposes edges; the Composite owns everything else. A proposal
// needs only Source and Target — Kind, Provenance, and synthesizedBy are
// stamped by the Composite, which merges rather than overwrites any Metadata
// the synthesizer set.
type Synthesizer interface {
	// Name is the synthesizedBy tag, e.g. "react-render".
	Name() string
	Synthesize(ctx context.Context, d *db.DB) ([]types.Edge, error)
}

type synthMeta struct {
	SynthesizedBy string `json:"synthesizedBy"`
	Via           string `json:"via,omitempty"`
	Field         string `json:"field,omitempty"`
	Event         string `json:"event,omitempty"`
	RegisteredAt  string `json:"registeredAt,omitempty"`
}

func marshalMeta(m synthMeta) json.RawMessage {
	b, err := json.Marshal(m)
	if err != nil {
		return json.RawMessage(`{"synthesizedBy":"unknown"}`)
	}
	return b
}

// Composite implements resolution.CallbackSynthesizer.
type Composite struct {
	db           *db.DB
	synthesizers []Synthesizer
}

func NewComposite(d *db.DB, ss ...Synthesizer) *Composite {
	return &Composite{db: d, synthesizers: ss}
}

// Default registers every synthesizer. Order matters where two match the same
// pair: Composite dedup is first-wins, so rn-event-channel precedes
// event-emitter to keep the more specific synthesizedBy tag.
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

// SynthesizerNames returns each registered synthesizer's Name, in order.
func (c *Composite) SynthesizerNames() []string {
	names := make([]string, len(c.synthesizers))
	for i, s := range c.synthesizers {
		names[i] = s.Name()
	}
	return names
}

// SynthesizeCallbackEdges implements resolution.CallbackSynthesizer.
func (c *Composite) SynthesizeCallbackEdges(ctx context.Context) error {
	existingEdges, err := c.loadExistingSynthEdges(ctx)
	if err != nil {
		return fmt.Errorf("synthesis: load existing edges: %w", err)
	}

	// Seeded from the DB so a re-run proposes nothing already persisted.
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
				continue
			}
			key := e.Source + ">" + e.Target
			if seen[key] {
				continue
			}
			seen[key] = true

			e.Kind = types.EdgeKindCalls
			e.Provenance = "heuristic"

			meta := buildMeta(s.Name(), e.Metadata)
			e.Metadata = meta

			toInsert = append(toInsert, e)
		}
	}

	if len(toInsert) == 0 {
		return nil
	}

	return c.db.WithTx(ctx, func(tx *db.Tx) error {
		for _, e := range toInsert {
			if _, err := tx.InsertEdge(ctx, e); err != nil {
				return fmt.Errorf("insert synth edge %s→%s: %w", e.Source, e.Target, err)
			}
		}
		return nil
	})
}

// loadExistingSynthEdges keys every heuristic edge as "source>target", via one
// provenance-filtered query rather than an O(nodes) per-node scan.
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

// unresolvedRefsBatchSize bounds peak memory when paging the whole table.
const unresolvedRefsBatchSize = 5000

// calleeOf returns the callee expression including its receiver
// ("emitter.on", "this.setState") — the form these synthesizers pattern-match
// on. Falls back to ReferenceName for plain calls, pre-v3 indexes, and
// hand-seeded test refs, where the extractor set no CalleeExpr.
func calleeOf(ref types.UnresolvedReference) string {
	if ref.CalleeExpr != "" {
		return ref.CalleeExpr
	}
	return ref.ReferenceName
}

// loadAllUnresolvedRefs pages the table rather than reading it in one query,
// which OOMs on large repos.
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

// buildMeta injects synthesizedBy into whatever metadata the synthesizer set,
// falling back to a minimal object when there is none or it is malformed.
func buildMeta(name string, existing json.RawMessage) json.RawMessage {
	if len(existing) == 0 {
		return marshalMeta(synthMeta{SynthesizedBy: name})
	}
	var m map[string]any
	if err := json.Unmarshal(existing, &m); err != nil {
		return marshalMeta(synthMeta{SynthesizedBy: name})
	}
	m["synthesizedBy"] = name
	b, err := json.Marshal(m)
	if err != nil {
		return marshalMeta(synthMeta{SynthesizedBy: name})
	}
	return b
}

// ReactRenderSynthesizer edges a setState caller to its class's render method:
// setState is what actually triggers render at runtime. Uncapped — one
// setState call reaches at most one render.
type ReactRenderSynthesizer struct{}

func (r *ReactRenderSynthesizer) Name() string { return "react-render" }

func (r *ReactRenderSynthesizer) Synthesize(ctx context.Context, d *db.DB) ([]types.Edge, error) {
	refs, err := loadAllUnresolvedRefs(ctx, d)
	if err != nil {
		return nil, err
	}

	methodNodes, err := d.GetNodesByKind(ctx, types.NodeKindMethod)
	if err != nil {
		return nil, err
	}

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

	classToRender := make(map[string]string)
	for _, m := range methodNodes {
		if m.Name == "render" {
			if classID, ok := methodToClass[m.ID]; ok {
				classToRender[classID] = m.ID
			}
		}
	}

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
		// render itself may call setState.
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

// JSXRenderSynthesizer promotes a static references edge produced by a
// <ChildWidget/> usage into a calls edge: rendering a child component invokes
// it. Such edges are identified by the "jsx:" refArgs discriminator that
// extraction stamps and createEdges carries onto the edge, since the
// originating ref is deleted by the time synthesis runs. Uncapped.
type JSXRenderSynthesizer struct{}

func (j *JSXRenderSynthesizer) Name() string { return "jsx-render" }

func (j *JSXRenderSynthesizer) Synthesize(ctx context.Context, d *db.DB) ([]types.Edge, error) {
	nodes, err := d.GetAllNodes(ctx)
	if err != nil {
		return nil, err
	}

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
			// Static edges only — a synthesized one would compound heuristics.
			if e.Kind != types.EdgeKindReferences || e.Provenance != "" {
				continue
			}
			if !hasJSXDiscriminator(e.Metadata) {
				continue
			}
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

// hasJSXDiscriminator identifies a static references edge whose origin was a
// JSX child usage.
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

// VueHandlerSynthesizer promotes a Vue component's static template references
// — child tags and @event="handler" bindings alike — into calls edges.
// NodeKindMethod counts as a target because options-API handlers live in the
// `methods:` object and extract as methods, not functions. Uncapped.
type VueHandlerSynthesizer struct{}

func (v *VueHandlerSynthesizer) Name() string { return "vue-handler" }

func (v *VueHandlerSynthesizer) Synthesize(ctx context.Context, d *db.DB) ([]types.Edge, error) {
	compNodes, err := d.GetNodesByKind(ctx, types.NodeKindComponent)
	if err != nil {
		return nil, err
	}

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
			if e.Kind != types.EdgeKindReferences || e.Provenance != "" {
				continue
			}
			// Vue template refs carry no Arguments, so a JSX discriminator here
			// would mean jsx-render's territory. Defensive.
			if hasJSXDiscriminator(e.Metadata) {
				continue
			}
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

// EventEmitterSynthesizer pairs an emit site with the listeners registered
// for the same event name. Only string-literal call arguments are extracted,
// so the handler's identity is unavailable and edges run enclosing-function to
// enclosing-function: coarser than the real dispatch, but honest.
type EventEmitterSynthesizer struct{}

func (e *EventEmitterSynthesizer) Name() string { return "event-emitter" }

func (e *EventEmitterSynthesizer) Synthesize(ctx context.Context, d *db.DB) ([]types.Edge, error) {
	return synthesizeEventEdges(ctx, d, isEERegistration, isEEDispatch)
}

func isEERegistration(callee string) bool {
	return strings.HasSuffix(callee, ".on") ||
		strings.HasSuffix(callee, ".addListener") ||
		strings.HasSuffix(callee, ".addEventListener")
}

func isEEDispatch(callee string) bool {
	return strings.HasSuffix(callee, ".emit") ||
		strings.HasSuffix(callee, ".dispatch")
}

// RNEventChannelSynthesizer is EventEmitterSynthesizer narrowed to
// React-Native's channel APIs. It must run first in Default so its more
// specific tag survives the Composite's first-wins dedup.
type RNEventChannelSynthesizer struct{}

func (r *RNEventChannelSynthesizer) Name() string { return "rn-event-channel" }

func (r *RNEventChannelSynthesizer) Synthesize(ctx context.Context, d *db.DB) ([]types.Edge, error) {
	return synthesizeEventEdges(ctx, d, isRNRegistration, isRNDispatch)
}

func isRNRegistration(callee string) bool {
	if !strings.HasSuffix(callee, ".addListener") {
		return false
	}
	return strings.Contains(callee, "DeviceEventEmitter") ||
		strings.Contains(callee, "NativeEventEmitter")
}

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

// synthesizeEventEdges backs both event synthesizers, differing only in the
// callee predicates. Edges run emit-site → on-site, correlated on the event
// name in Arguments[0].
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

	// Keyed by event name; each fromNodeID is an enclosing function.
	type refEntry struct{ fromNodeID string }
	registrations := make(map[string][]refEntry)
	dispatches := make(map[string][]refEntry)

	for _, ref := range refs {
		if ref.ReferenceKind != types.EdgeKindCalls {
			continue
		}
		if len(ref.Arguments) == 0 || ref.Arguments[0] == "" {
			continue
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
		for _, dispatch := range dispatchRefs {
			count := 0
			for _, reg := range regRefs {
				if count >= EVENT_FANOUT_CAP {
					break
				}
				src := dispatch.fromNodeID
				tgt := reg.fromNodeID
				if src == tgt {
					continue
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

// CallbackSynthesizer links a `this.someField(...)` invocation to whatever
// callable was assigned to that field. The assignment survives as a static
// edge carrying a "field:" refArgs discriminator; the invocation stays in
// unresolved_refs forever, because a field name is not a node name and can
// never resolve. Correlating the two on field name is the only bridge.
// Capped at MAX_CALLBACKS_PER_CHANNEL per (field, callable) pair.
type CallbackSynthesizer struct{}

func (c *CallbackSynthesizer) Name() string { return "callback" }

func (c *CallbackSynthesizer) Synthesize(ctx context.Context, d *db.DB) ([]types.Edge, error) {
	allNodes, err := d.GetAllNodes(ctx)
	if err != nil {
		return nil, err
	}

	// fieldName → callable node IDs assigned to that field.
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

	allRefs, err := loadAllUnresolvedRefs(ctx, d)
	if err != nil {
		return nil, err
	}

	type pair struct{ invoker, target, field string }
	var proposals []pair

	for _, ref := range allRefs {
		if ref.ReferenceKind != types.EdgeKindCalls {
			continue
		}
		name := calleeOf(ref)
		// Matches "this.fieldName" and bare "fieldName".
		for fieldName, targets := range fieldTargets {
			if name != fieldName && !strings.HasSuffix(name, "."+fieldName) {
				continue
			}
			for _, tgt := range targets {
				if ref.FromNodeID == tgt {
					continue
				}
				proposals = append(proposals, pair{ref.FromNodeID, tgt, fieldName})
			}
		}
	}

	// One channel per (field, callable) pair.
	channelCount := make(map[string]int)
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

// ClosureCollectionSynthesizer links iteration over a handler collection to
// the handlers appended to it, matching append and forEach sites on their
// shared receiver name. Capped at CC_FANOUT_CAP per receiver.
//
// Anonymous blocks (Swift `handlers.append { ... }`) yield no identifier and
// so no edge. Deliberate: an edge to an unknown target would be a fabrication.
type ClosureCollectionSynthesizer struct{}

func (c *ClosureCollectionSynthesizer) Name() string { return "closure-collection" }

func (c *ClosureCollectionSynthesizer) Synthesize(ctx context.Context, d *db.DB) ([]types.Edge, error) {
	refs, err := loadAllUnresolvedRefs(ctx, d)
	if err != nil {
		return nil, err
	}

	// Both keyed by receiver name: handler identifiers vs. iterating functions.
	appendHandlers := make(map[string][]string)
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

	// Cached: the same handler name recurs across receivers.
	handlerNodeIDs := make(map[string][]string)
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
			continue
		}

		channelCount := 0
		for _, forEachFrom := range forEachSites {
			for _, handlerID := range handlerIDs {
				if channelCount >= CC_FANOUT_CAP {
					break
				}
				if forEachFrom == handlerID {
					continue
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

// FlutterBuildSynthesizer is registered but inert: the Dart grammar binding
// exposes no call_expression node, so setState calls never become refs and
// there is nothing to correlate with build. Implement it (setState caller →
// the State subclass's build) once Dart call extraction exists.
type FlutterBuildSynthesizer struct{}

func (f *FlutterBuildSynthesizer) Name() string { return "flutter-build" }

func (f *FlutterBuildSynthesizer) Synthesize(_ context.Context, _ *db.DB) ([]types.Edge, error) {
	return nil, nil
}

// InterfaceImplSynthesizer edges each interface method to the same-named
// method on every implementing class — the call a static graph attributes to
// the declaration but that dispatches to the implementation. Uncapped: at most
// one implementing method per class.
type InterfaceImplSynthesizer struct{}

func (s *InterfaceImplSynthesizer) Name() string { return "interface-impl" }

func (s *InterfaceImplSynthesizer) Synthesize(ctx context.Context, d *db.DB) ([]types.Edge, error) {
	methodNodes, err := d.GetNodesByKind(ctx, types.NodeKindMethod)
	if err != nil {
		return nil, err
	}

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

	classNodes, err := d.GetNodesByKind(ctx, types.NodeKindClass)
	if err != nil {
		return nil, err
	}
	// Interfaces too: `interface B extends A` promotes to implements B→A.
	ifaceNodes, err := d.GetNodesByKind(ctx, types.NodeKindInterface)
	if err != nil {
		return nil, err
	}
	classLike := append(classNodes, ifaceNodes...)

	// Built over all containers, then narrowed to interface-kind ones below.
	interfaceToMethods := make(map[string][]string, len(ifaceNodes))
	for _, m := range methodNodes {
		classID, ok := methodToClass[m.ID]
		if !ok {
			continue
		}
		interfaceToMethods[classID] = append(interfaceToMethods[classID], m.ID)
	}
	ifaceIDSet := make(map[string]bool, len(ifaceNodes))
	for _, n := range ifaceNodes {
		ifaceIDSet[n.ID] = true
	}
	for containerID := range interfaceToMethods {
		if !ifaceIDSet[containerID] {
			delete(interfaceToMethods, containerID)
		}
	}

	interfaceMethodNames := make(map[string]map[string]string, len(ifaceNodes))
	for ifaceID, methodIDs := range interfaceToMethods {
		nameMap := make(map[string]string, len(methodIDs))
		for _, mid := range methodIDs {
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
					continue
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

// CppOverrideSynthesizer edges a base member function to the same-named
// override on each derived class — the vtable dispatch a static graph misses.
// Gated on the base node's language so TypeScript and Java extends edges,
// which have their own synthesizers, do not match. Both function and method
// kinds count as members: C++ extraction currently mints functions.
type CppOverrideSynthesizer struct{}

func (s *CppOverrideSynthesizer) Name() string { return "cpp-override" }

func (s *CppOverrideSynthesizer) Synthesize(ctx context.Context, d *db.DB) ([]types.Edge, error) {
	allNodes, err := d.GetAllNodes(ctx)
	if err != nil {
		return nil, err
	}

	nodeByID := make(map[string]types.Node, len(allNodes))
	for _, n := range allNodes {
		nodeByID[n.ID] = n
	}

	isCallable := func(kind types.NodeKind) bool {
		return kind == types.NodeKindFunction || kind == types.NodeKindMethod
	}

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
				continue
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
					continue
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

// GinMiddlewareChainSynthesizer edges each Go route node to the middleware
// registered by an `r.Use(...)` call in the same file. File-level is the
// available granularity — nothing records which routes a Use call actually
// guards. Middleware that resolves to no node emits no edge. Uncapped.
type GinMiddlewareChainSynthesizer struct{}

func (g *GinMiddlewareChainSynthesizer) Name() string { return "gin-middleware-chain" }

func (g *GinMiddlewareChainSynthesizer) Synthesize(ctx context.Context, d *db.DB) ([]types.Edge, error) {
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

	// Two kind-specific queries so the DB filters, rather than one broad query
	// filtered in memory.
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

	// Suffix matching, not equality: the indexer stores relative paths and the
	// framework extractor absolute ones, so the two sides disagree.
	seen := make(map[string]bool)
	var edges []types.Edge

	for useFilePath, middlewareNames := range usesByFile {
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

// GoGRPCStubImplSynthesizer is registered but inert. Three signals are
// missing: Go interface method signatures are not extracted as nodes, the
// `&fooImpl{}` in RegisterFooServer is a composite literal rather than an
// identifier, and Go's structural typing produces no implements edges at all.
type GoGRPCStubImplSynthesizer struct{}

func (g *GoGRPCStubImplSynthesizer) Name() string { return "go-grpc-stub-impl" }

func (g *GoGRPCStubImplSynthesizer) Synthesize(_ context.Context, _ *db.DB) ([]types.Edge, error) {
	return nil, nil
}

// MyBatisJavaXMLSynthesizer edges a Java mapper method to the XML statement
// that implements it. MyBatis binds the two by naming convention alone —
// the XML statement's qualified name is "<interface>.<method>" — so that
// string is the only correlation available.
type MyBatisJavaXMLSynthesizer struct{}

func (m *MyBatisJavaXMLSynthesizer) Name() string { return "mybatis-java-xml" }

func (m *MyBatisJavaXMLSynthesizer) Synthesize(ctx context.Context, d *db.DB) ([]types.Edge, error) {
	allNodes, err := d.GetAllNodes(ctx)
	if err != nil {
		return nil, err
	}

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

	javaInterfaces := make(map[string]string)
	for _, n := range allNodes {
		if n.Language == types.LanguageJava && n.Kind == types.NodeKindInterface {
			javaInterfaces[n.Name] = n.ID
		}
	}
	if len(javaInterfaces) == 0 {
		return nil, nil
	}

	// One full edge load beats O(nodes) per-node contains queries.
	allEdges, err := d.GetAllEdges(ctx)
	if err != nil {
		return nil, err
	}
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

	seen := make(map[string]bool)
	var edges []types.Edge

	for _, xf := range xmlFuncs {
		dotIdx := strings.LastIndex(xf.qualifiedName, ".")
		if dotIdx < 0 {
			continue
		}
		namespace := xf.qualifiedName[:dotIdx]
		stmtID := xf.qualifiedName[dotIdx+1:]
		if stmtID != xf.name {
			continue
		}

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

// FabricNativeImplSynthesizer is registered but inert. Native component
// registration (ObjC RCT_EXPORT_VIEW_PROPERTY, Java @ReactModule, C++ template
// specializations) is not extracted, and even the JS side's component-name
// literal has nothing to correlate against without a cross-language name index.
type FabricNativeImplSynthesizer struct{}

func (f *FabricNativeImplSynthesizer) Name() string { return "fabric-native-impl" }

func (f *FabricNativeImplSynthesizer) Synthesize(_ context.Context, _ *db.DB) ([]types.Edge, error) {
	return nil, nil
}
