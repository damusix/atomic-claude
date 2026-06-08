package synthesis_test

// event_emitter_test.go — TDD tests for EventEmitterSynthesizer and
// RNEventChannelSynthesizer (CP16 batch 3).
//
// Ground truth (probe 2026-06-05): .on('login', onLogin) and .emit('login', user)
// produce UnresolvedReferences with ReferenceName="emitter.on" / "emitter.emit"
// and Arguments=["login"] (EE2 captures the string event-name arg). The handler
// identifier (onLogin) is NOT captured — only string-literal args are recorded.
//
// Granularity choice (documented): the synthesizer correlates by event name,
// emitting an enclosing-function → enclosing-function edge (emit-site → on-site).
// This is coarser than emit-site → specific-handler but it is honest (no fabricated
// identity), and sufficient for call-graph tracing.

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/db"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/indexer"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/resolution"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/resolution/synthesis"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// seedRefWithArgs inserts an unresolved ref with string arguments.
func seedRefWithArgs(t *testing.T, d *db.DB, id, fromID, name string, kind types.EdgeKind, args []string) {
	t.Helper()
	if err := d.InsertUnresolvedRef(context.Background(), types.UnresolvedReference{
		ID:            id,
		FromNodeID:    fromID,
		ReferenceName: name,
		ReferenceKind: kind,
		FilePath:      "test.ts",
		Language:      types.LanguageTypeScript,
		Arguments:     args,
	}); err != nil {
		t.Fatalf("InsertUnresolvedRef %s: %v", id, err)
	}
}

// ---------------------------------------------------------------------------
// EventEmitterSynthesizer — unit tests with seeded DB
// ---------------------------------------------------------------------------

// TestEventEmitterSynthesizer_CorrelatesByEventName verifies the core signal:
// a .on('login', ...) registration ref + .emit('login', ...) dispatch ref →
// one calls+heuristic edge from the emit-enclosing function to the on-enclosing
// function, metadata.event="login", synthesizedBy="event-emitter".
func TestEventEmitterSynthesizer_CorrelatesByEventName(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	// Two functions: setupListeners (contains .on call) and triggerLogin (contains .emit call).
	seedNode(t, d, "setup-fn", "setupListeners", "svc.ts", types.NodeKindFunction, types.LanguageTypeScript)
	seedNode(t, d, "trigger-fn", "triggerLogin", "svc.ts", types.NodeKindFunction, types.LanguageTypeScript)

	// .on('login', ...) ref — event name in Arguments[0].
	seedRefWithArgs(t, d, "ref-on-login", "setup-fn", "emitter.on", types.EdgeKindCalls, []string{"login"})
	// .emit('login', ...) ref — same event name.
	seedRefWithArgs(t, d, "ref-emit-login", "trigger-fn", "emitter.emit", types.EdgeKindCalls, []string{"login"})

	s := &synthesis.EventEmitterSynthesizer{}
	edges, err := s.Synthesize(ctx, d)
	if err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	if len(edges) != 1 {
		t.Fatalf("got %d edges, want 1", len(edges))
	}
	e := edges[0]
	if e.Source != "trigger-fn" {
		t.Errorf("source = %q, want trigger-fn (emit site)", e.Source)
	}
	if e.Target != "setup-fn" {
		t.Errorf("target = %q, want setup-fn (on registration site)", e.Target)
	}
	// Metadata must carry event name.
	var meta map[string]string
	if err := json.Unmarshal(e.Metadata, &meta); err != nil {
		t.Fatalf("unmarshal metadata: %v", err)
	}
	if meta["event"] != "login" {
		t.Errorf("metadata.event = %q, want login", meta["event"])
	}
}

// TestEventEmitterSynthesizer_AllRegistrationAPIs verifies that
// .on / .addListener / .addEventListener all count as registration.
func TestEventEmitterSynthesizer_AllRegistrationAPIs(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	seedNode(t, d, "fn-on", "useOn", "a.ts", types.NodeKindFunction, types.LanguageTypeScript)
	seedNode(t, d, "fn-al", "useAddListener", "a.ts", types.NodeKindFunction, types.LanguageTypeScript)
	seedNode(t, d, "fn-ae", "useAddEventListener", "a.ts", types.NodeKindFunction, types.LanguageTypeScript)
	seedNode(t, d, "fn-emit", "doEmit", "a.ts", types.NodeKindFunction, types.LanguageTypeScript)

	// Three registrations, all for event "tick".
	seedRefWithArgs(t, d, "r-on", "fn-on", "ee.on", types.EdgeKindCalls, []string{"tick"})
	seedRefWithArgs(t, d, "r-al", "fn-al", "ee.addListener", types.EdgeKindCalls, []string{"tick"})
	seedRefWithArgs(t, d, "r-ae", "fn-ae", "ee.addEventListener", types.EdgeKindCalls, []string{"tick"})
	// One dispatch.
	seedRefWithArgs(t, d, "r-emit", "fn-emit", "ee.emit", types.EdgeKindCalls, []string{"tick"})

	s := &synthesis.EventEmitterSynthesizer{}
	edges, err := s.Synthesize(ctx, d)
	if err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	// 3 registrations × 1 emit = 3 edges (all distinct targets).
	if len(edges) != 3 {
		t.Fatalf("got %d edges, want 3 (one per registration API)", len(edges))
	}
	// All edges must be from fn-emit.
	for _, e := range edges {
		if e.Source != "fn-emit" {
			t.Errorf("edge source = %q, want fn-emit", e.Source)
		}
	}
}

// TestEventEmitterSynthesizer_DispatchAPIs verifies .emit / .dispatch both
// count as dispatch.
func TestEventEmitterSynthesizer_DispatchAPIs(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	seedNode(t, d, "fn-listen", "listen", "b.ts", types.NodeKindFunction, types.LanguageTypeScript)
	seedNode(t, d, "fn-emit2", "doEmit2", "b.ts", types.NodeKindFunction, types.LanguageTypeScript)
	seedNode(t, d, "fn-disp", "doDispatch", "b.ts", types.NodeKindFunction, types.LanguageTypeScript)

	seedRefWithArgs(t, d, "r-listen", "fn-listen", "bus.on", types.EdgeKindCalls, []string{"change"})
	seedRefWithArgs(t, d, "r-emit2", "fn-emit2", "bus.emit", types.EdgeKindCalls, []string{"change"})
	seedRefWithArgs(t, d, "r-disp", "fn-disp", "bus.dispatch", types.EdgeKindCalls, []string{"change"})

	s := &synthesis.EventEmitterSynthesizer{}
	edges, err := s.Synthesize(ctx, d)
	if err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	// 2 dispatchers × 1 registration = 2 edges.
	if len(edges) != 2 {
		t.Fatalf("got %d edges, want 2 (emit + dispatch)", len(edges))
	}
	targets := make(map[string]bool)
	for _, e := range edges {
		targets[e.Target] = true
	}
	if !targets["fn-listen"] {
		t.Errorf("expected edge → fn-listen")
	}
}

// TestEventEmitterSynthesizer_NoEventName verifies that refs without Arguments
// (no event name captured) produce no edges. This is the guard for the no-signal
// case — emitter.on / emitter.emit calls where the arg was not a string literal.
func TestEventEmitterSynthesizer_NoEventName(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	seedNode(t, d, "fn-noarg-on", "noArgOn", "c.ts", types.NodeKindFunction, types.LanguageTypeScript)
	seedNode(t, d, "fn-noarg-emit", "noArgEmit", "c.ts", types.NodeKindFunction, types.LanguageTypeScript)
	// No Arguments — emitter.on(dynamicEvent, handler) where event is a variable.
	seedRef(t, d, "r-noarg-on", "fn-noarg-on", "emitter.on", types.EdgeKindCalls)
	seedRef(t, d, "r-noarg-emit", "fn-noarg-emit", "emitter.emit", types.EdgeKindCalls)

	s := &synthesis.EventEmitterSynthesizer{}
	edges, err := s.Synthesize(ctx, d)
	if err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	if len(edges) != 0 {
		t.Errorf("got %d edges, want 0 (no event name = no correlation possible)", len(edges))
	}
}

// TestEventEmitterSynthesizer_EventFanoutCap verifies that more than
// EVENT_FANOUT_CAP (6) on-registrations for the same event name result in
// exactly EVENT_FANOUT_CAP edges from the emit site, not more.
func TestEventEmitterSynthesizer_EventFanoutCap(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	// 8 distinct on-registration sites (> cap of 6).
	for i := 0; i < 8; i++ {
		id := nodeID("fn-reg", i)
		seedNode(t, d, id, id, "d.ts", types.NodeKindFunction, types.LanguageTypeScript)
		seedRefWithArgs(t, d, "r-reg-"+id, id, "emitter.on", types.EdgeKindCalls, []string{"data"})
	}
	seedNode(t, d, "fn-emitter", "doEmit", "d.ts", types.NodeKindFunction, types.LanguageTypeScript)
	seedRefWithArgs(t, d, "r-emit-data", "fn-emitter", "emitter.emit", types.EdgeKindCalls, []string{"data"})

	s := &synthesis.EventEmitterSynthesizer{}
	edges, err := s.Synthesize(ctx, d)
	if err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	if len(edges) != synthesis.EVENT_FANOUT_CAP {
		t.Errorf("got %d edges, want EVENT_FANOUT_CAP=%d", len(edges), synthesis.EVENT_FANOUT_CAP)
	}
}

// TestEventEmitterSynthesizer_DedupSameEmitSiteMultipleEmits verifies that
// multiple .emit refs from the same enclosing function to the same event/target
// produce only one edge per (source, target) pair — no duplicates.
func TestEventEmitterSynthesizer_DedupSameEmitSiteMultipleEmits(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	seedNode(t, d, "fn-dup-on", "listen", "e.ts", types.NodeKindFunction, types.LanguageTypeScript)
	seedNode(t, d, "fn-dup-emit", "trigger", "e.ts", types.NodeKindFunction, types.LanguageTypeScript)

	// Two .emit calls from same function → same event → same target.
	seedRefWithArgs(t, d, "r-dup-on", "fn-dup-on", "bus.on", types.EdgeKindCalls, []string{"ready"})
	seedRefWithArgs(t, d, "r-dup-emit-1", "fn-dup-emit", "bus.emit", types.EdgeKindCalls, []string{"ready"})
	seedRefWithArgs(t, d, "r-dup-emit-2", "fn-dup-emit", "bus.emit", types.EdgeKindCalls, []string{"ready"})

	s := &synthesis.EventEmitterSynthesizer{}
	edges, err := s.Synthesize(ctx, d)
	if err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	// Should be exactly 1 edge (fn-dup-emit → fn-dup-on), not 2.
	if len(edges) != 1 {
		t.Errorf("got %d edges, want 1 (dedup same source>target)", len(edges))
	}
}

// TestEventEmitterSynthesizer_NoEdgesWhenNoDispatch verifies that on-only
// (no emit) produces no synthesized edges — there is no dispatch to correlate.
func TestEventEmitterSynthesizer_NoEdgesWhenNoDispatch(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	seedNode(t, d, "fn-only-on", "listenOnly", "f.ts", types.NodeKindFunction, types.LanguageTypeScript)
	seedRefWithArgs(t, d, "r-only-on", "fn-only-on", "ee.on", types.EdgeKindCalls, []string{"startup"})

	s := &synthesis.EventEmitterSynthesizer{}
	edges, err := s.Synthesize(ctx, d)
	if err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	if len(edges) != 0 {
		t.Errorf("got %d edges, want 0 (no emit side)", len(edges))
	}
}

// TestEventEmitterSynthesizer_Gate runs the real fixture through the full
// indexer + resolution + synthesis pipeline and asserts a heuristic edge
// from the emit-enclosing function to the on-enclosing function.
func TestEventEmitterSynthesizer_Gate(t *testing.T) {
	ctx := context.Background()
	d := openTestDB(t)

	fixtureDir := t.TempDir()

	// Handler defined in-project (so it becomes a node).
	writeFixture(t, fixtureDir, "handlers.ts", `
export function onLogin(user: any) {
  console.log("login", user);
}
`)

	// Service that registers + dispatches.
	writeFixture(t, fixtureDir, "service.ts", `
import { onLogin } from "./handlers";

const emitter: any = {};

function setupListeners() {
  emitter.on('login', onLogin);
}

function triggerLogin(user: any) {
  emitter.emit('login', user);
}
`)

	pool, err := extraction.NewPool(ctx, extraction.PoolOptions{Size: 2})
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	orch := indexer.NewOrchestrator(d, pool)
	if err := orch.IndexAll(ctx, fixtureDir); err != nil {
		t.Fatalf("IndexAll: %v", err)
	}

	composite := synthesis.Default(d)
	pipe := resolution.NewPipelineWithSeams(d, fixtureDir, nil, composite)
	if _, _, err := pipe.ResolveAndPersistBatched(ctx, 0, nil); err != nil {
		t.Fatalf("ResolveAndPersistBatched: %v", err)
	}

	// Find function nodes.
	allNodes, err := d.GetAllNodes(ctx)
	if err != nil {
		t.Fatalf("GetAllNodes: %v", err)
	}
	var setupID, triggerID string
	for _, n := range allNodes {
		switch n.Name {
		case "setupListeners":
			setupID = n.ID
		case "triggerLogin":
			triggerID = n.ID
		}
	}
	if setupID == "" || triggerID == "" {
		t.Fatalf("expected setupListeners and triggerLogin nodes; got setupID=%q triggerID=%q", setupID, triggerID)
	}

	// Assert heuristic calls edge: triggerLogin → setupListeners
	// (emit site → registration site), synthesizedBy=event-emitter, event=login.
	assertEventEmitterEdge(t, d, triggerID, setupID, "login")

	// Idempotency: re-run produces same count.
	synthBefore := countEdgesWithProvenance(t, d, "heuristic")
	if err := composite.SynthesizeCallbackEdges(ctx); err != nil {
		t.Fatalf("re-run: %v", err)
	}
	synthAfter := countEdgesWithProvenance(t, d, "heuristic")
	if synthBefore != synthAfter {
		t.Errorf("idempotent: before=%d after=%d, want equal", synthBefore, synthAfter)
	}

	// Node count stable across re-run.
	nodesBefore := countNodes(t, d)
	if err := composite.SynthesizeCallbackEdges(ctx); err != nil {
		t.Fatalf("second re-run: %v", err)
	}
	nodesAfter := countNodes(t, d)
	if nodesBefore != nodesAfter {
		t.Errorf("node count: before=%d after=%d, want equal (no new nodes)", nodesBefore, nodesAfter)
	}
}

// assertEventEmitterEdge asserts a calls+heuristic edge from source→target
// with synthesizedBy="event-emitter" and event=eventName.
func assertEventEmitterEdge(t *testing.T, d *db.DB, sourceID, targetID, eventName string) {
	t.Helper()
	edges := edgesFrom(t, d, sourceID)
	for _, e := range edges {
		if e.Target != targetID || e.Provenance != "heuristic" {
			continue
		}
		if e.Kind != types.EdgeKindCalls {
			t.Errorf("event-emitter edge kind=%s, want calls", e.Kind)
			return
		}
		var meta map[string]string
		if err := json.Unmarshal(e.Metadata, &meta); err != nil {
			t.Fatalf("unmarshal metadata: %v", err)
		}
		if meta["synthesizedBy"] != "event-emitter" {
			t.Errorf("synthesizedBy=%q, want event-emitter", meta["synthesizedBy"])
		}
		if meta["event"] != eventName {
			t.Errorf("event=%q, want %q", meta["event"], eventName)
		}
		return // found
	}
	t.Errorf("no heuristic calls edge %s→%s (synthesizedBy=event-emitter event=%s)", sourceID, targetID, eventName)
}

// nodeID generates a deterministic node id for cap tests.
func nodeID(prefix string, i int) string {
	return prefix + "-" + string(rune('0'+i))
}

// ---------------------------------------------------------------------------
// RNEventChannelSynthesizer — gate test
// ---------------------------------------------------------------------------

// TestRNEventChannelSynthesizer_Gate runs a React-Native fixture through the
// full pipeline and asserts a heuristic edge from the emit-enclosing function
// to the addListener-enclosing function.
//
// RN-specific signals: DeviceEventEmitter.addListener / NativeEventEmitter.addListener
// for registration; DeviceEventEmitter.emit / nativeModule.sendEvent for dispatch.
// The same EE2 mechanism captures the event-name string arg.
func TestRNEventChannelSynthesizer_Gate(t *testing.T) {
	ctx := context.Background()
	d := openTestDB(t)

	fixtureDir := t.TempDir()

	// RN listener setup.
	writeFixture(t, fixtureDir, "rnListener.ts", `
import { DeviceEventEmitter } from "react-native";

function setupRNListeners() {
  DeviceEventEmitter.addListener('deviceReady', () => {});
}
`)
	// RN emit side.
	writeFixture(t, fixtureDir, "rnEmitter.ts", `
import { DeviceEventEmitter } from "react-native";

function fireDeviceReady() {
  DeviceEventEmitter.emit('deviceReady');
}
`)

	pool, err := extraction.NewPool(ctx, extraction.PoolOptions{Size: 2})
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	orch := indexer.NewOrchestrator(d, pool)
	if err := orch.IndexAll(ctx, fixtureDir); err != nil {
		t.Fatalf("IndexAll: %v", err)
	}

	composite := synthesis.Default(d)
	pipe := resolution.NewPipelineWithSeams(d, fixtureDir, nil, composite)
	if _, _, err := pipe.ResolveAndPersistBatched(ctx, 0, nil); err != nil {
		t.Fatalf("ResolveAndPersistBatched: %v", err)
	}

	// Find function nodes.
	allNodes, err := d.GetAllNodes(ctx)
	if err != nil {
		t.Fatalf("GetAllNodes: %v", err)
	}
	var setupRNID, fireID string
	for _, n := range allNodes {
		switch n.Name {
		case "setupRNListeners":
			setupRNID = n.ID
		case "fireDeviceReady":
			fireID = n.ID
		}
	}
	if setupRNID == "" || fireID == "" {
		t.Fatalf("expected setupRNListeners and fireDeviceReady nodes; got setupRNID=%q fireID=%q", setupRNID, fireID)
	}

	// Assert heuristic calls edge: fireDeviceReady → setupRNListeners
	// synthesizedBy=rn-event-channel, event=deviceReady.
	assertRNEdge(t, d, fireID, setupRNID, "deviceReady")

	// Idempotency.
	synthBefore := countEdgesWithProvenance(t, d, "heuristic")
	if err := composite.SynthesizeCallbackEdges(ctx); err != nil {
		t.Fatalf("re-run: %v", err)
	}
	synthAfter := countEdgesWithProvenance(t, d, "heuristic")
	if synthBefore != synthAfter {
		t.Errorf("idempotent: before=%d after=%d", synthBefore, synthAfter)
	}
}

// assertRNEdge asserts a calls+heuristic edge from source→target with
// synthesizedBy="rn-event-channel" and event=eventName.
func assertRNEdge(t *testing.T, d *db.DB, sourceID, targetID, eventName string) {
	t.Helper()
	edges := edgesFrom(t, d, sourceID)
	for _, e := range edges {
		if e.Target != targetID || e.Provenance != "heuristic" {
			continue
		}
		var meta map[string]string
		if err := json.Unmarshal(e.Metadata, &meta); err != nil {
			t.Fatalf("unmarshal metadata: %v", err)
		}
		if meta["synthesizedBy"] != "rn-event-channel" {
			continue
		}
		if meta["event"] != eventName {
			t.Errorf("event=%q, want %q", meta["event"], eventName)
		}
		if e.Kind != types.EdgeKindCalls {
			t.Errorf("edge kind=%s, want calls", e.Kind)
		}
		return
	}
	t.Errorf("no heuristic calls edge %s→%s (synthesizedBy=rn-event-channel event=%s)", sourceID, targetID, eventName)
}

// TestRNEventChannelSynthesizer_NativeEventEmitter verifies that
// NativeEventEmitter().addListener is recognized as a registration pattern.
func TestRNEventChannelSynthesizer_NativeEventEmitter(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	seedNode(t, d, "fn-rn-listen", "rnListen", "rn.ts", types.NodeKindFunction, types.LanguageTypeScript)
	seedNode(t, d, "fn-rn-fire", "rnFire", "rn.ts", types.NodeKindFunction, types.LanguageTypeScript)

	// NativeEventEmitter().addListener — the callee name ends with ".addListener".
	// RN synthesizer shares the "addListener" suffix check with the RN-specific
	// receiver names (DeviceEventEmitter / NativeEventEmitter).
	seedRefWithArgs(t, d, "r-rn-listen", "fn-rn-listen", "NativeEventEmitter().addListener", types.EdgeKindCalls, []string{"frameUpdate"})
	seedRefWithArgs(t, d, "r-rn-fire", "fn-rn-fire", "DeviceEventEmitter.emit", types.EdgeKindCalls, []string{"frameUpdate"})

	s := &synthesis.RNEventChannelSynthesizer{}
	edges, err := s.Synthesize(ctx, d)
	if err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	if len(edges) != 1 {
		t.Fatalf("got %d edges, want 1", len(edges))
	}
	var meta map[string]string
	json.Unmarshal(edges[0].Metadata, &meta)
	if meta["event"] != "frameUpdate" {
		t.Errorf("event=%q, want frameUpdate", meta["event"])
	}
}
