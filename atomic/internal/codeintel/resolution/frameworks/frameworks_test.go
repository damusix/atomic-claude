package frameworks_test

// Failing tests first (TDD) for res-CP4 / master CP14:
//
//   1. Route-node id/qn/name format (exact appendix-H strings)
//   2. Express.Detect on a package.json fixture
//   3. Express.Extract emits the route node + handler ref
//   4. End-to-end persist → resolve → edge integration test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/db"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/resolution"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/resolution/frameworks"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// ---------------------------------------------------------------------------
// Test 1: Route-node id / qualifiedName / name format (appendix H verbatim)
// ---------------------------------------------------------------------------

func TestRouteNodeFormat(t *testing.T) {
	filePath := "/project/src/routes/users.js"
	line := 7
	method := "GET"
	path := "/users/:id"

	node := frameworks.MakeRouteNode(filePath, line, method, path, types.LanguageJavaScript)

	// id: route:{filePath}:{line}:{METHOD}:{path}
	wantID := "route:/project/src/routes/users.js:7:GET:/users/:id"
	if node.ID != wantID {
		t.Errorf("route node id\n  got  %q\n  want %q", node.ID, wantID)
	}

	// qualifiedName: {filePath}::METHOD:{path}
	wantQN := "/project/src/routes/users.js::METHOD:/users/:id"
	if node.QualifiedName != wantQN {
		t.Errorf("route node qualifiedName\n  got  %q\n  want %q", node.QualifiedName, wantQN)
	}

	// name: "METHOD /path"
	wantName := "GET /users/:id"
	if node.Name != wantName {
		t.Errorf("route node name\n  got  %q\n  want %q", node.Name, wantName)
	}

	if node.Kind != types.NodeKindRoute {
		t.Errorf("route node kind: got %q want %q", node.Kind, types.NodeKindRoute)
	}
	if !node.IsExported {
		t.Error("route node IsExported should be true")
	}
	if node.StartLine != line {
		t.Errorf("route node StartLine: got %d want %d", node.StartLine, line)
	}
	if node.Language != types.LanguageJavaScript {
		t.Errorf("route node Language: got %q want %q", node.Language, types.LanguageJavaScript)
	}
}

// ---------------------------------------------------------------------------
// Test 2: Express.Detect
// ---------------------------------------------------------------------------

func TestExpressDetect_PackageJSON(t *testing.T) {
	dir := t.TempDir()

	// package.json listing express as a dependency
	pkgJSON := `{"dependencies": {"express": "^4.18.0"}}`
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(pkgJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	ex := frameworks.NewExpressResolver(dir)
	ctx := context.Background()
	if !ex.Detect(ctx) {
		t.Error("Express.Detect should return true when package.json lists express")
	}
}

func TestExpressDetect_NoPackageJSON(t *testing.T) {
	dir := t.TempDir()
	// no package.json, no express-looking files — should be false
	ex := frameworks.NewExpressResolver(dir)
	if ex.Detect(context.Background()) {
		t.Error("Express.Detect should return false in an empty directory")
	}
}

func TestExpressDetect_ContentFallback(t *testing.T) {
	dir := t.TempDir()
	// no package.json, but a JS file with require('express')
	jsContent := `const express = require('express');`
	if err := os.WriteFile(filepath.Join(dir, "app.js"), []byte(jsContent), 0o644); err != nil {
		t.Fatal(err)
	}

	ex := frameworks.NewExpressResolver(dir)
	if !ex.Detect(context.Background()) {
		t.Error("Express.Detect should return true when a JS file requires express")
	}
}

// ---------------------------------------------------------------------------
// Test 3: Express.Extract
// ---------------------------------------------------------------------------

func TestExpressExtract_NamedHandler(t *testing.T) {
	filePath := "src/routes/users.js"
	content := `
const express = require('express');
const router = express.Router();

router.get('/users/:id', getUser);
router.post('/users', createUser);
`

	ex := frameworks.NewExpressResolver("/project")
	nodes, refs := ex.Extract(filePath, content)

	if len(nodes) < 2 {
		t.Fatalf("Extract: want at least 2 route nodes, got %d", len(nodes))
	}

	// Find the GET /users/:id node
	var getNode *types.Node
	for i := range nodes {
		if nodes[i].Name == "GET /users/:id" {
			getNode = &nodes[i]
		}
	}
	if getNode == nil {
		t.Fatal("Extract: missing route node for GET /users/:id")
	}

	wantID := "route:src/routes/users.js:5:GET:/users/:id"
	if getNode.ID != wantID {
		t.Errorf("Extract: route node id\n  got  %q\n  want %q", getNode.ID, wantID)
	}
	if getNode.Kind != types.NodeKindRoute {
		t.Errorf("Extract: route node kind: got %q want %q", getNode.Kind, types.NodeKindRoute)
	}
	if !getNode.IsExported {
		t.Error("Extract: route node IsExported should be true")
	}

	// There should be a reference from the route node to "getUser"
	var handlerRef *types.UnresolvedReference
	for i := range refs {
		if refs[i].FromNodeID == getNode.ID && refs[i].ReferenceName == "getUser" {
			handlerRef = &refs[i]
		}
	}
	if handlerRef == nil {
		t.Errorf("Extract: missing handler reference getUser from route node %s; refs=%v", getNode.ID, refs)
	} else {
		if handlerRef.ReferenceKind != types.EdgeKindReferences {
			t.Errorf("Extract: handler ref kind: got %q want %q", handlerRef.ReferenceKind, types.EdgeKindReferences)
		}
	}
}

// ---------------------------------------------------------------------------
// Test 4: ClaimsReference
// ---------------------------------------------------------------------------

func TestExpressClaimsReference(t *testing.T) {
	filePath := "src/routes/users.js"
	content := `
router.get('/users/:id', getUser);
`
	ex := frameworks.NewExpressResolver("/project")
	ex.Extract(filePath, content) // populate internal set

	if !ex.ClaimsReference("getUser") {
		t.Error("ClaimsReference should return true for handler extracted by Extract")
	}
	if ex.ClaimsReference("unrelated") {
		t.Error("ClaimsReference should return false for handler not seen in Extract")
	}
}

// ---------------------------------------------------------------------------
// Test 5: Express.Resolve
// ---------------------------------------------------------------------------

func TestExpressResolve(t *testing.T) {
	tmp := t.TempDir()
	d, err := db.Open(filepath.Join(tmp, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	ctx := context.Background()

	// Insert a function node for "getUser"
	getUserNode := types.Node{
		ID:            "function:abc123",
		Kind:          types.NodeKindFunction,
		Name:          "getUser",
		QualifiedName: "src/controllers/users.js::getUser",
		FilePath:      "src/controllers/users.js",
		Language:      types.LanguageJavaScript,
		StartLine:     1,
		EndLine:       10,
		IsExported:    true,
	}
	if err := d.UpsertNode(ctx, getUserNode); err != nil {
		t.Fatal(err)
	}

	ex := frameworks.NewExpressResolverWithDB("/project", d)

	// Populate the claims set
	ex.Extract("src/routes/users.js", `router.get('/users/:id', getUser);`)

	ref := types.UnresolvedReference{
		ID:            "ref:001",
		FromNodeID:    "route:src/routes/users.js:1:GET:/users/:id",
		ReferenceName: "getUser",
		ReferenceKind: types.EdgeKindReferences,
		FilePath:      "src/routes/users.js",
		Language:      types.LanguageJavaScript,
	}

	result, err := ex.Resolve(ctx, ref)
	if err != nil {
		t.Fatal(err)
	}
	if result.TargetNodeID != getUserNode.ID {
		t.Errorf("Resolve: TargetNodeID got %q want %q", result.TargetNodeID, getUserNode.ID)
	}
	if result.Confidence < 0.8 || result.Confidence > 0.9 {
		t.Errorf("Resolve: Confidence got %f want 0.8–0.9", result.Confidence)
	}
}

// ---------------------------------------------------------------------------
// Test 6: Registry — DetectFrameworks + GetApplicableFrameworks
// ---------------------------------------------------------------------------

func TestRegistry_DetectFrameworks(t *testing.T) {
	dir := t.TempDir()
	pkgJSON := `{"dependencies": {"express": "^4.18.0"}}`
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(pkgJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	reg := frameworks.NewRegistry(dir, nil)
	ctx := context.Background()
	detected := reg.DetectFrameworks(ctx)

	if len(detected) == 0 {
		t.Fatal("DetectFrameworks: expected at least Express to be detected")
	}
	var found bool
	for _, r := range detected {
		if r.Name() == "express" {
			found = true
		}
	}
	if !found {
		t.Error("DetectFrameworks: express not in detected list")
	}
}

func TestRegistry_GetApplicableFrameworks(t *testing.T) {
	dir := t.TempDir()
	reg := frameworks.NewRegistry(dir, nil)

	jsFrameworks := reg.GetApplicableFrameworks(types.LanguageJavaScript)
	if len(jsFrameworks) == 0 {
		t.Error("GetApplicableFrameworks(javascript): expected at least Express")
	}

	// Go language should return zero frameworks from this registry
	goFrameworks := reg.GetApplicableFrameworks(types.LanguageGo)
	_ = goFrameworks // Go frameworks come in CP15
}

// ---------------------------------------------------------------------------
// Test 7: End-to-end — Express fixture index → route node + references edge
// ---------------------------------------------------------------------------

func TestEndToEnd_ExpressRouteToEdge(t *testing.T) {
	tmp := t.TempDir()
	d, err := db.Open(filepath.Join(tmp, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	ctx := context.Background()

	// Create package.json so Detect passes
	pkgJSON := `{"dependencies": {"express": "^4.18.0"}}`
	if err := os.WriteFile(filepath.Join(tmp, "package.json"), []byte(pkgJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	// Pre-index a "getUser" function node (simulates what the extractor would do)
	getUserNode := types.Node{
		ID:            "function:deadbeef00000000000000000000000",
		Kind:          types.NodeKindFunction,
		Name:          "getUser",
		QualifiedName: "src/controllers/users.js::getUser",
		FilePath:      "src/controllers/users.js",
		Language:      types.LanguageJavaScript,
		StartLine:     3,
		EndLine:       12,
		IsExported:    true,
	}
	if err := d.UpsertNode(ctx, getUserNode); err != nil {
		t.Fatal(err)
	}

	// Express fixture: one route with a named handler
	const expressFixturePath = "src/routes/users.js"
	const expressFixtureContent = `
const express = require('express');
const app = express();

app.get('/users/:id', getUser);
`

	// Build registry and run framework extraction
	reg := frameworks.NewRegistry(tmp, d)

	files := []frameworks.FileInput{
		{Path: expressFixturePath, Content: expressFixtureContent},
	}

	// ExtractAndPersist persists framework-discovered route nodes + unresolved refs
	if err := reg.ExtractAndPersist(ctx, files); err != nil {
		t.Fatalf("ExtractAndPersist: %v", err)
	}

	// Verify route node was persisted
	const wantRouteID = "route:src/routes/users.js:5:GET:/users/:id"
	routeNode, err := d.GetNode(ctx, wantRouteID)
	if err != nil {
		t.Fatalf("GetNode(%q): %v — route node not persisted", wantRouteID, err)
	}
	if routeNode.Kind != types.NodeKindRoute {
		t.Errorf("route node kind: got %q want %q", routeNode.Kind, types.NodeKindRoute)
	}
	if routeNode.Name != "GET /users/:id" {
		t.Errorf("route node name: got %q want %q", routeNode.Name, "GET /users/:id")
	}

	// Run the resolution pipeline to turn unresolved refs into edges
	pipe := resolution.NewPipelineWithSeams(d, tmp, reg.FrameworkRegistry(), resolution.NoopSynthesizer{})
	_, edges, err := pipe.ResolveAndPersistBatched(ctx, 0, nil)
	if err != nil {
		t.Fatalf("ResolveAndPersistBatched: %v", err)
	}
	if edges == 0 {
		t.Error("ResolveAndPersistBatched: expected at least 1 edge (route→getUser), got 0")
	}

	// Verify the references edge from route to getUser exists
	outgoing, err := d.GetEdgesBySource(ctx, wantRouteID)
	if err != nil {
		t.Fatalf("GetEdgesBySource: %v", err)
	}
	var foundEdge bool
	for _, e := range outgoing {
		if e.Target == getUserNode.ID && (e.Kind == types.EdgeKindReferences || e.Kind == types.EdgeKindCalls) {
			foundEdge = true
		}
	}
	if !foundEdge {
		t.Errorf("expected references/calls edge from %q to %q; outgoing edges: %+v", wantRouteID, getUserNode.ID, outgoing)
	}
}

// ---------------------------------------------------------------------------
// Test 8: Language inference from file extension (item 1 fix)
// ---------------------------------------------------------------------------

// TestExtract_TSFileLang asserts that a .ts file gets Language=typescript,
// not javascript. This was broken before the fix: Extract hardcoded
// LanguageJavaScript regardless of filePath extension.
func TestExtract_TSFileLang(t *testing.T) {
	filePath := "src/routes/users.ts"
	content := `
import express from 'express';
const router = express.Router();
router.get('/users/:id', getUser);
`
	ex := frameworks.NewExpressResolver("/project")
	nodes, refs := ex.Extract(filePath, content)

	if len(nodes) == 0 {
		t.Fatal("Extract: expected at least one route node for .ts file")
	}
	if nodes[0].Language != types.LanguageTypeScript {
		t.Errorf("Extract: .ts file route node Language: got %q want %q",
			nodes[0].Language, types.LanguageTypeScript)
	}
	if len(refs) == 0 {
		t.Fatal("Extract: expected at least one handler ref")
	}
	if refs[0].Language != types.LanguageTypeScript {
		t.Errorf("Extract: .ts file handler ref Language: got %q want %q",
			refs[0].Language, types.LanguageTypeScript)
	}
}

// ---------------------------------------------------------------------------
// Test 9: Comment stripping (item 2 fix)
// ---------------------------------------------------------------------------

// TestExtract_CommentStripping proves that the comment stripper runs before
// the route regex. Commented-out routes must emit ZERO nodes; a real
// uncommented route must still be extracted.
func TestExtract_CommentStripping(t *testing.T) {
	filePath := "src/app.js"
	content := `
// router.get('/x', h)
/* app.post('/y', h) */
app.get('/real', realHandler);
`
	ex := frameworks.NewExpressResolver("/project")
	nodes, refs := ex.Extract(filePath, content)

	// Only one real route, not the two commented-out ones.
	if len(nodes) != 1 {
		t.Errorf("Extract: want 1 route node (commented routes stripped), got %d", len(nodes))
	}
	if len(nodes) > 0 && nodes[0].Name != "GET /real" {
		t.Errorf("Extract: expected route name %q, got %q", "GET /real", nodes[0].Name)
	}
	if len(refs) == 0 {
		t.Errorf("Extract: expected ref for realHandler, got none")
	}
}

// ---------------------------------------------------------------------------
// Test 10: Inline-body handler (item 3 fix)
// ---------------------------------------------------------------------------

// TestExtract_InlineBodyHandler verifies that an inline arrow handler emits
// a route node + a calls ref for identifiers inside the body, excluding
// reserved/builtin names.
func TestExtract_InlineBodyHandler(t *testing.T) {
	filePath := "src/app.js"
	content := `app.get('/z', (req, res) => { doThing(); res.json({}) });`

	ex := frameworks.NewExpressResolver("/project")
	nodes, refs := ex.Extract(filePath, content)

	if len(nodes) != 1 {
		t.Fatalf("Extract inline: want 1 route node, got %d", len(nodes))
	}
	if nodes[0].Name != "GET /z" {
		t.Errorf("Extract inline: route name got %q want %q", nodes[0].Name, "GET /z")
	}

	// Must have a calls ref for "doThing"; res/json are reserved and must be absent.
	var foundDoThing bool
	for _, r := range refs {
		if r.ReferenceName == "doThing" && r.ReferenceKind == types.EdgeKindCalls {
			foundDoThing = true
		}
		if r.ReferenceName == "res" || r.ReferenceName == "json" {
			t.Errorf("Extract inline: reserved name %q must not appear in refs", r.ReferenceName)
		}
	}
	if !foundDoThing {
		t.Errorf("Extract inline: expected calls ref for doThing; refs=%v", refs)
	}
}
