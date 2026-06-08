package synthesis_test

// batch6_test.go — CP16 batch-6 synthesizer tests:
//
//   - gin-middleware-chain (real — EE5 .Use args + route nodes from CP15)
//   - go-grpc-stub-impl   (documented stub — missing Go interface method signal)
//   - mybatis-java-xml    (real — XML function QualifiedName ↔ Java method name)
//   - fabric-native-impl  (documented stub — no cross-language component correlation)

import (
	"context"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/db"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/indexer"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/resolution"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/resolution/frameworks"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/resolution/synthesis"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// ---------------------------------------------------------------------------
// gin-middleware-chain: real fixture through full pipeline
// ---------------------------------------------------------------------------

// TestGinMiddlewareChainSynthesizer_Gate indexes a Go file that registers a
// middleware with r.Use(authMiddleware) and a route r.GET("/api/items", listHandler).
// After the full pipeline (indexer + framework extract + resolution + synthesis),
// a heuristic calls edge must exist from the route node to the authMiddleware
// function node with synthesizedBy="gin-middleware-chain".
//
// The full pipeline:
//  1. IndexAll: Go extractor extracts authMiddleware/listHandler as function nodes,
//     and emits a calls unresolved_ref for r.Use(authMiddleware) with EE5 "arg:".
//  2. reg.ExtractAndPersist: Gin framework extractor emits the route node for
//     r.GET("/api/items", listHandler) and a references unresolved_ref for listHandler.
//  3. ResolveAndPersistBatched: resolves handler ref + static refs.
//  4. SynthesizeCallbackEdges (via Default composite): gin-middleware-chain
//     synthesizer emits route → authMiddleware edges.
func TestGinMiddlewareChainSynthesizer_Gate(t *testing.T) {
	ctx := context.Background()
	d := openTestDB(t)
	fixtureDir := t.TempDir()

	const routerContent = `package main

import "github.com/gin-gonic/gin"

func authMiddleware(c *gin.Context) {
	c.Next()
}

func listHandler(c *gin.Context) {
	c.JSON(200, gin.H{"items": []string{}})
}

func setupRouter() *gin.Engine {
	r := gin.Default()
	r.Use(authMiddleware)
	r.GET("/api/items", listHandler)
	return r
}
`

	// go.mod so Gin detector fires.
	writeFixture(t, fixtureDir, "go.mod", `module example.com/app

go 1.21

require github.com/gin-gonic/gin v1.9.0
`)
	routerPath := writeFixture(t, fixtureDir, "router.go", routerContent)

	// Step 1: index via the generic extractor (emits function nodes + .Use() unresolved_ref).
	pool, err := extraction.NewPool(ctx, extraction.PoolOptions{Size: 2})
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	if err := indexer.NewOrchestrator(d, pool).IndexAll(ctx, fixtureDir); err != nil {
		t.Fatalf("IndexAll: %v", err)
	}

	// Step 2: framework extraction — Gin resolver emits route nodes.
	reg := frameworks.NewRegistry(fixtureDir, d)
	files := []frameworks.FileInput{{Path: routerPath, Content: routerContent}}
	if err := reg.ExtractAndPersist(ctx, files); err != nil {
		t.Fatalf("ExtractAndPersist: %v", err)
	}

	// Step 3+4: resolution + synthesis.
	composite := synthesis.Default(d)
	pipe := resolution.NewPipelineWithSeams(d, fixtureDir, reg.FrameworkRegistry(), composite)
	if _, _, err := pipe.ResolveAndPersistBatched(ctx, 0, nil); err != nil {
		t.Fatalf("ResolveAndPersistBatched: %v", err)
	}

	// Find the route node and the authMiddleware function node.
	allNodes, err := d.GetAllNodes(ctx)
	if err != nil {
		t.Fatalf("GetAllNodes: %v", err)
	}

	var routeID, authMiddlewareID string
	for _, n := range allNodes {
		switch {
		case n.Kind == types.NodeKindRoute && n.Language == types.LanguageGo:
			routeID = n.ID
		case (n.Kind == types.NodeKindFunction || n.Kind == types.NodeKindMethod) &&
			n.Name == "authMiddleware" && n.Language == types.LanguageGo:
			authMiddlewareID = n.ID
		}
	}

	if routeID == "" {
		t.Fatal("no Go route node found — Gin resolver did not emit a route node")
	}
	if authMiddlewareID == "" {
		t.Fatal("no authMiddleware function node found")
	}

	// Assert heuristic calls edge: route → authMiddleware with synthesizedBy="gin-middleware-chain".
	assertSynthEdgeByName(t, d, routeID, authMiddlewareID, "gin-middleware-chain")

	// Idempotency: re-running synthesis produces no new edges.
	before := countEdgesWithProvenance(t, d, "heuristic")
	if err := composite.SynthesizeCallbackEdges(ctx); err != nil {
		t.Fatalf("re-run: %v", err)
	}
	after := countEdgesWithProvenance(t, d, "heuristic")
	if before != after {
		t.Errorf("idempotent: before=%d after=%d, want equal", before, after)
	}
}

// TestGinMiddlewareChainSynthesizer_NoEdgesWithoutRoutes verifies no edges are
// emitted when there are no route nodes in the graph (Go-only scope guard).
func TestGinMiddlewareChainSynthesizer_NoEdgesWithoutRoutes(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	// Seed a Go function node and a .Use unresolved_ref — but no route node.
	seedNode(t, d, "auth-fn", "authMiddleware", "router.go", types.NodeKindFunction, types.LanguageGo)
	// Seed a calls ref for r.Use(authMiddleware) with EE5 arg.
	if err := d.InsertUnresolvedRef(ctx, types.UnresolvedReference{
		ID:            "ref-use-1",
		FromNodeID:    "auth-fn",
		ReferenceName: "r.Use",
		ReferenceKind: types.EdgeKindCalls,
		Arguments:     []string{"arg:authMiddleware"},
		FilePath:      "router.go",
		Language:      types.LanguageGo,
	}); err != nil {
		t.Fatalf("InsertUnresolvedRef: %v", err)
	}

	s := &synthesis.GinMiddlewareChainSynthesizer{}
	edges, err := s.Synthesize(ctx, d)
	if err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	if len(edges) != 0 {
		t.Errorf("expected 0 edges without route nodes, got %d", len(edges))
	}
}

// ---------------------------------------------------------------------------
// go-grpc-stub-impl: documented gap — zero edges, gap documented
// ---------------------------------------------------------------------------

// TestGoGRPCStubImplSynthesizer_GapDocumented verifies the synthesizer emits
// zero edges and documents the missing signals:
//  1. Go interface method signatures inside interface_type are NOT extracted as
//     method nodes (Go MethodTypes captures only method_declaration = concrete
//     methods with receivers; interface method bodies are not method_declaration).
//  2. RegisterFooServer(s, &fooImpl{}) — the impl type &fooImpl{} is a
//     composite literal, not a plain identifier; EE5 does not capture it.
//  3. Go uses structural typing; no explicit implements edges exist.
func TestGoGRPCStubImplSynthesizer_GapDocumented(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	// Simulate what would be indexed from a gRPC service file.
	// Go extractor does NOT emit method nodes for interface method signatures.
	// RegisterFooServer(s, &fooImpl{}) arg is a composite literal — EE5 misses it.
	seedNode(t, d, "foo-server-iface", "FooServer", "foo_grpc.pb.go", types.NodeKindInterface, types.LanguageGo)
	seedNode(t, d, "foo-impl-struct", "fooImpl", "server.go", types.NodeKindStruct, types.LanguageGo)
	// No interface method nodes exist — Go extractor doesn't extract them.
	// No implements edges — Go uses structural typing, no explicit declaration.

	s := &synthesis.GoGRPCStubImplSynthesizer{}
	edges, err := s.Synthesize(ctx, d)
	if err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	if len(edges) != 0 {
		t.Errorf(
			"GoGRPCStubImplSynthesizer: expected 0 edges (gap: Go interface methods not extracted; "+
				"composite literal &fooImpl{} not captured by EE5; no Go implements edges), got %d",
			len(edges),
		)
	}
}

// ---------------------------------------------------------------------------
// mybatis-java-xml: real fixture through full pipeline
// ---------------------------------------------------------------------------

// TestMyBatisJavaXMLSynthesizer_Gate indexes a Java mapper interface + MyBatis
// XML mapper file. After the full pipeline, a heuristic calls edge must exist
// from the Java mapper method node to the XML statement function node with
// synthesizedBy="mybatis-java-xml".
//
// Correlation: XML function.QualifiedName = "com.example.UserMapper.findUser"
//   - namespace = "com.example.UserMapper"
//   - stmtId    = "findUser"
//   - Java interface name = last segment of namespace = "UserMapper"
//   - Java method name = stmtId = "findUser"
func TestMyBatisJavaXMLSynthesizer_Gate(t *testing.T) {
	ctx := context.Background()
	d := openTestDB(t)
	fixtureDir := t.TempDir()

	// Java mapper interface with a findUser method.
	writeFixture(t, fixtureDir, "UserMapper.java", `package com.example;

public interface UserMapper {
    User findUser(int id);
    List<User> listUsers();
}
`)

	// MyBatis XML mapper file for the same namespace.
	writeFixture(t, fixtureDir, "UserMapper.xml", `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE mapper PUBLIC "-//mybatis.org//DTD Mapper 3.0//EN"
  "http://mybatis.org/dtd/mybatis-3-mapper.dtd">
<mapper namespace="com.example.UserMapper">
  <select id="findUser" resultType="User">
    SELECT * FROM users WHERE id = #{id}
  </select>
  <select id="listUsers" resultType="User">
    SELECT * FROM users
  </select>
</mapper>
`)

	pool, err := extraction.NewPool(ctx, extraction.PoolOptions{Size: 2})
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	if err := indexer.NewOrchestrator(d, pool).IndexAll(ctx, fixtureDir); err != nil {
		t.Fatalf("IndexAll: %v", err)
	}

	composite := synthesis.Default(d)
	pipe := resolution.NewPipelineWithSeams(d, fixtureDir, nil, composite)
	if _, _, err := pipe.ResolveAndPersistBatched(ctx, 0, nil); err != nil {
		t.Fatalf("ResolveAndPersistBatched: %v", err)
	}

	allNodes, err := d.GetAllNodes(ctx)
	if err != nil {
		t.Fatalf("GetAllNodes: %v", err)
	}

	// Find the Java findUser method and XML findUser function.
	var javaMethodID, xmlFuncID string
	for _, n := range allNodes {
		switch {
		case n.Language == types.LanguageJava && n.Kind == types.NodeKindMethod && n.Name == "findUser":
			javaMethodID = n.ID
		case n.Language == types.LanguageXML && n.Kind == types.NodeKindFunction && n.Name == "findUser":
			xmlFuncID = n.ID
		}
	}

	if javaMethodID == "" {
		t.Fatal("Java findUser method node not found — Java extractor did not emit a method node")
	}
	if xmlFuncID == "" {
		t.Fatal("XML findUser function node not found — MyBatis XML extractor did not emit a function node")
	}

	// Assert heuristic calls edge: Java method → XML function.
	assertSynthEdgeByName(t, d, javaMethodID, xmlFuncID, "mybatis-java-xml")

	// Also check listUsers is linked.
	var javaListID, xmlListID string
	for _, n := range allNodes {
		switch {
		case n.Language == types.LanguageJava && n.Kind == types.NodeKindMethod && n.Name == "listUsers":
			javaListID = n.ID
		case n.Language == types.LanguageXML && n.Kind == types.NodeKindFunction && n.Name == "listUsers":
			xmlListID = n.ID
		}
	}
	if javaListID != "" && xmlListID != "" {
		assertSynthEdgeByName(t, d, javaListID, xmlListID, "mybatis-java-xml")
	}

	// Idempotency.
	before := countEdgesWithProvenance(t, d, "heuristic")
	if err := composite.SynthesizeCallbackEdges(ctx); err != nil {
		t.Fatalf("re-run: %v", err)
	}
	after := countEdgesWithProvenance(t, d, "heuristic")
	if before != after {
		t.Errorf("idempotent: before=%d after=%d, want equal", before, after)
	}
}

// TestMyBatisJavaXMLSynthesizer_ScopedToJavaAndXML verifies the synthesizer
// only fires for Java+XML pairs (not TypeScript or Go).
func TestMyBatisJavaXMLSynthesizer_ScopedToJavaAndXML(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	// Seed a TypeScript method and an XML function — should NOT produce an edge.
	seedNode(t, d, "ts-method", "findUser", "mapper.ts", types.NodeKindMethod, types.LanguageTypeScript)
	seedNode(t, d, "xml-func", "findUser", "mapper.xml", types.NodeKindFunction, types.LanguageXML)

	s := &synthesis.MyBatisJavaXMLSynthesizer{}
	edges, err := s.Synthesize(ctx, d)
	if err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	if len(edges) != 0 {
		t.Errorf("MyBatisJavaXMLSynthesizer fired on TypeScript+XML pair (not Java), got %d edges", len(edges))
	}
}

// ---------------------------------------------------------------------------
// fabric-native-impl: documented gap — zero edges, gap documented
// ---------------------------------------------------------------------------

// TestFabricNativeImplSynthesizer_GapDocumented verifies the synthesizer emits
// zero edges and documents the missing signals:
//  1. No Fabric-specific native registration extraction: ObjC RCT_EXPORT_VIEW_PROPERTY,
//     Java @ReactModule, C++ template specializations are not captured.
//  2. JS/TS codegenNativeComponent<T>("ComponentName") string-literal arg may
//     appear in Arguments (EE2), but correlating to native language implementations
//     requires cross-language name matching not present in the current graph.
//  3. No cross-language name resolution mechanism for ObjC/Java/C++ ↔ JS/TS
//     component names exists in the current pipeline.
func TestFabricNativeImplSynthesizer_GapDocumented(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	// Simulate what a Fabric fixture would produce.
	seedNode(t, d, "ts-spec", "MyComponent", "MyComponent.ts", types.NodeKindFunction, types.LanguageTypeScript)
	seedNode(t, d, "java-impl", "MyComponent", "MyComponentManager.java", types.NodeKindClass, types.LanguageJava)
	// codegenNativeComponent call with string literal arg (EE2 captures this).
	if err := d.InsertUnresolvedRef(ctx, types.UnresolvedReference{
		ID:            "ref-fabric-1",
		FromNodeID:    "ts-spec",
		ReferenceName: "codegenNativeComponent",
		ReferenceKind: types.EdgeKindCalls,
		Arguments:     []string{"MyComponent"},
		FilePath:      "MyComponent.ts",
		Language:      types.LanguageTypeScript,
	}); err != nil {
		t.Fatalf("InsertUnresolvedRef: %v", err)
	}

	s := &synthesis.FabricNativeImplSynthesizer{}
	edges, err := s.Synthesize(ctx, d)
	if err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	if len(edges) != 0 {
		t.Errorf(
			"FabricNativeImplSynthesizer: expected 0 edges (gap: no Fabric native registration capture; "+
				"no cross-language name resolution for ObjC/Java/C++ ↔ JS/TS component names), got %d",
			len(edges),
		)
	}
}

// ---------------------------------------------------------------------------
// Default composite has all batch-6 synthesizers registered
// ---------------------------------------------------------------------------

// TestDefaultCompositeHasFourteenSynthesizers verifies Default(d) includes all
// batch-6 synthesizers by running each against an empty DB (no panic, no error)
// and checking they appear in the composite's output via SynthesizerNames.
func TestDefaultCompositeHasFourteenSynthesizers(t *testing.T) {
	d := openTestDB(t)
	composite := synthesis.Default(d)
	if composite == nil {
		t.Fatal("Default returned nil")
	}
	// Verify composite runs without error on an empty DB.
	if err := composite.SynthesizeCallbackEdges(context.Background()); err != nil {
		t.Fatalf("Default composite on empty DB: %v", err)
	}
	// Verify all 14 synthesizer names are present.
	names := composite.SynthesizerNames()
	wantNames := []string{
		"react-render", "jsx-render", "vue-handler", "rn-event-channel",
		"event-emitter", "callback", "closure-collection", "flutter-build",
		"interface-impl", "cpp-override",
		"gin-middleware-chain", "go-grpc-stub-impl", "mybatis-java-xml", "fabric-native-impl",
	}
	if len(names) != len(wantNames) {
		t.Errorf("Default has %d synthesizers, want %d\ngot:  %v\nwant: %v", len(names), len(wantNames), names, wantNames)
	}
	nameSet := make(map[string]bool, len(names))
	for _, n := range names {
		nameSet[n] = true
	}
	for _, want := range wantNames {
		if !nameSet[want] {
			t.Errorf("synthesizer %q not in Default composite", want)
		}
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// assertSynthEdgeByName verifies a heuristic calls edge exists from source → target
// with the given synthesizedBy tag in metadata. Unlike assertSynthEdge it does not
// check the "via" field (batch-6 synthesizers don't all set via).
func assertSynthEdgeByName(t *testing.T, d *db.DB, sourceID, targetID, synthesizedBy string) {
	t.Helper()
	// Delegate to assertSynthEdge with an empty via — it skips via check when "".
	assertSynthEdge(t, d, sourceID, targetID, synthesizedBy, "")
}
