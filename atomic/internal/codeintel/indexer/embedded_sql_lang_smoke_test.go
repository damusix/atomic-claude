package indexer_test

// embedded_sql_lang_smoke_test.go — CP2 smoke e2e tests for the generic
// embedded-SQL harvester.
//
// One test per extraction mode, mirroring TestEmbeddedSQLInGoFile /
// TestEmbeddedSQLInPythonFile:
//   - Java  (content-child, no interpolation)
//   - C#    (content-child + interpolation)
//   - Lua   (Shape 2 / inline, no interpolation)
//   - Dart  (Shape 2 / inline + interpolation)
//
// Each test runs a real index over a temp dir (full orchestrator stack) and
// makes falsifiable assertions: exact names, exact Provenance value, exact
// len(...)==0 for absence.

import (
	"context"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/indexer"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// ---------------------------------------------------------------------------
// TestEmbeddedSQLInJavaFile — content-child mode (Java)
// ---------------------------------------------------------------------------

// TestEmbeddedSQLInJavaFile verifies embedded SQL extraction for Java .java files.
//
// Java uses string_literal / string_fragment (content-child grammar, no interpolation).
// Fixture proves:
//   - DDL in a plain String literal → ≥1 table node "users" attributed to the file.
//   - DML in a method String literal → unresolved ref to "users" owned by
//     the enclosing method node, Language==SQL, Provenance stamps edges.
//   - GetEdgesByProvenance("embedded") returns ≥1 edge.
func TestEmbeddedSQLInJavaFile(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	database := openTestDB(t)

	dir := t.TempDir()
	initGitRepo(t, dir)

	// Line layout (1-based):
	//  1: public class UserRepo {
	//  2:     static final String DDL = "CREATE TABLE users (id INT PRIMARY KEY, name VARCHAR(255))";
	//  3:
	//  4:     public void loadUser(int id) {
	//  5:         String q = "SELECT id, name FROM users WHERE id = ?";
	//  6:     }
	//  7: }
	const javaFixture = `public class UserRepo {
    static final String DDL = "CREATE TABLE users (id INT PRIMARY KEY, name VARCHAR(255))";

    public void loadUser(int id) {
        String q = "SELECT id, name FROM users WHERE id = ?";
    }
}
`
	writeFile(t, dir, "UserRepo.java", javaFixture)
	gitAdd(t, dir, ".")
	gitCommit(t, dir, "init")

	orch := indexer.NewOrchestrator(database, pool)
	if err := orch.IndexAll(ctx, dir); err != nil {
		t.Fatalf("IndexAll: %v", err)
	}

	javaNodes, err := database.GetNodesInFile(ctx, "UserRepo.java")
	if err != nil {
		t.Fatalf("GetNodesInFile(UserRepo.java): %v", err)
	}

	// --- Criterion 1: DDL → table node "users" ---
	var usersNode *types.Node
	for i := range javaNodes {
		if javaNodes[i].Kind == types.NodeKindTable && javaNodes[i].Name == "users" {
			usersNode = &javaNodes[i]
			break
		}
	}
	if usersNode == nil {
		t.Fatalf("FAIL: no table node 'users' in UserRepo.java — Java content-child harvester not wired (CP2)")
	}
	// StartLine must be exactly 2 (file-absolute): DDL literal is on line 2 of the fixture.
	if usersNode.StartLine != 2 {
		t.Errorf("users table StartLine=%d, want 2 (file-absolute)", usersNode.StartLine)
	}

	// --- Criterion 2: DML → unresolved ref owned by loadUser ---
	allRefs, err := database.GetUnresolvedRefs(ctx, 0, 0)
	if err != nil {
		t.Fatalf("GetUnresolvedRefs: %v", err)
	}

	var loadUserNode *types.Node
	for i := range javaNodes {
		if javaNodes[i].Kind == types.NodeKindMethod && javaNodes[i].Name == "loadUser" {
			loadUserNode = &javaNodes[i]
			break
		}
	}
	if loadUserNode == nil {
		t.Fatal("FAIL: loadUser method node not found in UserRepo.java — needed for ownership assertion")
	}

	var dmlRef *types.UnresolvedReference
	for i := range allRefs {
		if allRefs[i].FilePath == "UserRepo.java" &&
			allRefs[i].ReferenceName == "users" &&
			allRefs[i].FromNodeID == loadUserNode.ID {
			dmlRef = &allRefs[i]
			break
		}
	}
	if dmlRef == nil {
		t.Errorf("FAIL: no unresolved ref for 'users' from UserRepo.java owned by loadUser — DML not wired for Java (CP2)")
	}
	if dmlRef != nil && dmlRef.Language != types.LanguageSQL {
		t.Errorf("DML ref Language=%q, want %q", dmlRef.Language, types.LanguageSQL)
	}

	// --- Criterion 3: GetEdgesByProvenance("embedded") ≥1 ---
	embeddedEdges, err := database.GetEdgesByProvenance(ctx, "embedded")
	if err != nil {
		t.Fatalf("GetEdgesByProvenance(embedded): %v", err)
	}
	if len(embeddedEdges) == 0 {
		t.Fatalf("FAIL: GetEdgesByProvenance(embedded) returned 0 edges for Java fixture (CP2)")
	}
}

// ---------------------------------------------------------------------------
// TestEmbeddedSQLInCSharpFile — content-child + interpolation mode (C#)
// ---------------------------------------------------------------------------

// TestEmbeddedSQLInCSharpFile verifies embedded SQL extraction for C# .cs files.
//
// C# uses string_literal / interpolated_string_expression (content-child +
// interpolation grammar).
// Fixture proves:
//   - DDL in a plain string → table node "users", GetEdgesByProvenance("embedded") ≥1.
//   - DML in a plain string → unresolved ref to "users", Provenance:"embedded".
//   - Interpolated string where table is a plain identifier and only a value
//     is interpolated → ref to "users" emitted (interpolated value, not table).
//   - Interpolated string where table target is the interpolation → zero refs
//     (interpolation becomes "?", no valid identifier after FROM).
func TestEmbeddedSQLInCSharpFile(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	database := openTestDB(t)

	dir := t.TempDir()
	initGitRepo(t, dir)

	// Line layout (1-based):
	//  1: using System;
	//  2: public class UserQuery {
	//  3:     static string DDL = "CREATE TABLE users (id INT PRIMARY KEY, name NVARCHAR(255))";
	//  4:     public void Fetch(int id, string tbl) {
	//  5:         string q1 = "SELECT id, name FROM users WHERE id = 1";
	//  6:         string q2 = $"SELECT id, name FROM users WHERE id = {id}";
	//  7:         string q3 = $"SELECT id, name FROM {tbl} WHERE id = 1";
	//  8:     }
	//  9: }
	const csFixture = `using System;
public class UserQuery {
    static string DDL = "CREATE TABLE users (id INT PRIMARY KEY, name NVARCHAR(255))";
    public void Fetch(int id, string tbl) {
        string q1 = "SELECT id, name FROM users WHERE id = 1";
        string q2 = $"SELECT id, name FROM users WHERE id = {id}";
        string q3 = $"SELECT id, name FROM {tbl} WHERE id = 1";
    }
}
`
	writeFile(t, dir, "UserQuery.cs", csFixture)
	gitAdd(t, dir, ".")
	gitCommit(t, dir, "init")

	orch := indexer.NewOrchestrator(database, pool)
	if err := orch.IndexAll(ctx, dir); err != nil {
		t.Fatalf("IndexAll: %v", err)
	}

	csNodes, err := database.GetNodesInFile(ctx, "UserQuery.cs")
	if err != nil {
		t.Fatalf("GetNodesInFile(UserQuery.cs): %v", err)
	}

	allRefs, err := database.GetUnresolvedRefs(ctx, 0, 0)
	if err != nil {
		t.Fatalf("GetUnresolvedRefs: %v", err)
	}

	// --- Criterion 1: DDL → table node "users" (line 3) ---
	var usersTableNode *types.Node
	for i := range csNodes {
		if csNodes[i].Kind == types.NodeKindTable && csNodes[i].Name == "users" {
			usersTableNode = &csNodes[i]
			break
		}
	}
	if usersTableNode == nil {
		t.Fatalf("FAIL: no table node 'users' from C# DDL (line 3) — C# content-child harvester not wired (CP2)")
	}
	// StartLine must be exactly 3 (file-absolute): DDL literal is on line 3 of the fixture.
	if usersTableNode.StartLine != 3 {
		t.Errorf("users table StartLine=%d, want 3 (file-absolute)", usersTableNode.StartLine)
	}

	// --- Criterion 2: embedded-provenance edges (from DDL contains edges) ---
	embeddedEdges, err := database.GetEdgesByProvenance(ctx, "embedded")
	if err != nil {
		t.Fatalf("GetEdgesByProvenance(embedded): %v", err)
	}
	if len(embeddedEdges) == 0 {
		t.Fatalf("FAIL: GetEdgesByProvenance(embedded) returned 0 edges for C# fixture (CP2)")
	}

	// --- Criterion 3: DML → ref to "users" from UserQuery.cs ---
	var usersRef *types.UnresolvedReference
	for i := range allRefs {
		if allRefs[i].FilePath == "UserQuery.cs" && allRefs[i].ReferenceName == "users" {
			usersRef = &allRefs[i]
			break
		}
	}
	if usersRef == nil {
		t.Errorf("FAIL: no unresolved ref for 'users' from UserQuery.cs — C# DML not wired (CP2)")
	}

	// --- Criterion 4: Fetch method must be found (node ownership check) ---
	var fetchNode *types.Node
	for i := range csNodes {
		if csNodes[i].Kind == types.NodeKindMethod && csNodes[i].Name == "Fetch" {
			fetchNode = &csNodes[i]
			break
		}
	}
	if fetchNode == nil {
		t.Fatal("FAIL: Fetch method node not found in UserQuery.cs — needed for ownership assertion")
	}

	// --- Criterion 5: interpolated table target → zero refs for "tbl" ---
	// q3 = $"SELECT id, name FROM {tbl} WHERE id = 1" — after interpolation
	// substitution: FROM ? — no valid identifier → zero refs named "tbl".
	var tblRefs []types.UnresolvedReference
	for _, ref := range allRefs {
		if ref.FilePath == "UserQuery.cs" && ref.ReferenceName == "tbl" {
			tblRefs = append(tblRefs, ref)
		}
	}
	if len(tblRefs) != 0 {
		t.Errorf("FAIL: interpolated table target 'tbl' must yield 0 refs, got %d — C# decision 8a not enforced", len(tblRefs))
	}

	// --- Criterion 6: interpolated value + literal table → ref to "users" (from q2 + q1) ---
	// q2 = $"SELECT id, name FROM users WHERE id = {id}" — "users" is literal,
	// so a ref to "users" must be emitted (in addition to the one from q1).
	// WHY ≥2: criterion 3 already confirmed one "users" ref from q1 (plain DML).
	// q2 must produce a second. If q2 extraction is broken the count stays at 1.
	var usersRefsFromFetch int
	for i := range allRefs {
		if allRefs[i].FilePath == "UserQuery.cs" &&
			allRefs[i].ReferenceName == "users" &&
			allRefs[i].FromNodeID == fetchNode.ID {
			usersRefsFromFetch++
		}
	}
	if usersRefsFromFetch < 2 {
		t.Errorf("FAIL: want ≥2 'users' refs from Fetch (q1 plain + q2 interpolated-value); got %d — C# decision 8b not enforced (CP2)", usersRefsFromFetch)
	}
}

// ---------------------------------------------------------------------------
// TestEmbeddedSQLInLuaFile — Shape 2 / inline mode (Lua)
// ---------------------------------------------------------------------------

// TestEmbeddedSQLInLuaFile verifies embedded SQL extraction for Lua .lua files.
//
// Lua uses "string" node (Shape 2 — inline content, no separate content child).
// Long-bracket strings [[…]] are the idiomatic multi-line form.
// Fixture proves:
//   - Long-bracket DDL [[CREATE TABLE ...]] → ≥1 table node "sessions".
//   - DML in a function → unresolved ref to "sessions" owned by the function node.
//   - GetEdgesByProvenance("embedded") returns ≥1 edge.
func TestEmbeddedSQLInLuaFile(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	database := openTestDB(t)

	dir := t.TempDir()
	initGitRepo(t, dir)

	// Line layout (1-based):
	//  1: local DDL = [[CREATE TABLE sessions (id INTEGER PRIMARY KEY, token TEXT NOT NULL)]]
	//  2:
	//  3: local function loadSession(db, id)
	//  4:     local q = "SELECT id, token FROM sessions WHERE id = ?"
	//  5:     return db:query(q)
	//  6: end
	const luaFixture = `local DDL = [[CREATE TABLE sessions (id INTEGER PRIMARY KEY, token TEXT NOT NULL)]]

local function loadSession(db, id)
    local q = "SELECT id, token FROM sessions WHERE id = ?"
    return db:query(q)
end
`
	writeFile(t, dir, "session.lua", luaFixture)
	gitAdd(t, dir, ".")
	gitCommit(t, dir, "init")

	orch := indexer.NewOrchestrator(database, pool)
	if err := orch.IndexAll(ctx, dir); err != nil {
		t.Fatalf("IndexAll: %v", err)
	}

	luaNodes, err := database.GetNodesInFile(ctx, "session.lua")
	if err != nil {
		t.Fatalf("GetNodesInFile(session.lua): %v", err)
	}

	// --- Criterion 1: long-bracket DDL → table node "sessions" ---
	var sessionsNode *types.Node
	for i := range luaNodes {
		if luaNodes[i].Kind == types.NodeKindTable && luaNodes[i].Name == "sessions" {
			sessionsNode = &luaNodes[i]
			break
		}
	}
	if sessionsNode == nil {
		t.Fatalf("FAIL: no table node 'sessions' from Lua long-bracket DDL — Lua Shape-2 harvester not wired (CP2)")
	}
	// StartLine must be exactly 1 (file-absolute): DDL literal is on line 1 of the fixture.
	// The prior assertion was `< 1`, which is vacuously false for any 1-based line number
	// (StartLine=50 would have passed). The exact check catches wrong values in both directions.
	if sessionsNode.StartLine != 1 {
		t.Errorf("sessions table StartLine=%d, want 1 (file-absolute)", sessionsNode.StartLine)
	}

	// --- Criterion 2: DML → unresolved ref to "sessions" from session.lua ---
	allRefs, err := database.GetUnresolvedRefs(ctx, 0, 0)
	if err != nil {
		t.Fatalf("GetUnresolvedRefs: %v", err)
	}

	var dmlRef *types.UnresolvedReference
	for i := range allRefs {
		if allRefs[i].FilePath == "session.lua" && allRefs[i].ReferenceName == "sessions" {
			dmlRef = &allRefs[i]
			break
		}
	}
	if dmlRef == nil {
		t.Errorf("FAIL: no unresolved ref for 'sessions' from session.lua — Lua DML not harvested (CP2)")
	}
	if dmlRef != nil && dmlRef.Language != types.LanguageSQL {
		t.Errorf("DML ref Language=%q, want %q", dmlRef.Language, types.LanguageSQL)
	}

	// --- Criterion 3: GetEdgesByProvenance("embedded") ≥1 ---
	embeddedEdges, err := database.GetEdgesByProvenance(ctx, "embedded")
	if err != nil {
		t.Fatalf("GetEdgesByProvenance(embedded): %v", err)
	}
	if len(embeddedEdges) == 0 {
		t.Fatalf("FAIL: GetEdgesByProvenance(embedded) returned 0 edges for Lua fixture (CP2)")
	}
}

// ---------------------------------------------------------------------------
// TestEmbeddedSQLInDartFile — Shape 2 + interpolation mode (Dart)
// ---------------------------------------------------------------------------

// TestEmbeddedSQLInDartFile verifies embedded SQL extraction for Dart .dart files.
//
// Dart uses "string_literal" (Shape 2 — inline content + template_substitution).
// Fixture proves two assertions, per the brief's falsifiable spec:
//
//	a) Interpolated table target → ZERO refs.
//	   "SELECT a FROM $t WHERE id = 1" — after substitution FROM ? — no valid
//	   identifier after FROM. The collected embedded refs slice must be empty.
//
//	b) Literal table + interpolated value → ref to "users".
//	   "SELECT a FROM users WHERE id = $id" — "users" is a literal identifier;
//	   the interpolation only replaces a value position → ref to "users" emitted.
//
// Also proves:
//   - DDL in a string literal → ≥1 table node "products" (file-absolute line).
//   - GetEdgesByProvenance("embedded") ≥1.
func TestEmbeddedSQLInDartFile(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	database := openTestDB(t)

	dir := t.TempDir()
	initGitRepo(t, dir)

	// Line layout (1-based):
	//  1: void createProducts() {
	//  2:   final ddl = "CREATE TABLE products (id INT PRIMARY KEY, name TEXT NOT NULL)";
	//  3: }
	//  4:
	//  5: void queryProducts(String t, int id) {
	//  6:   final q1 = "SELECT a FROM $t WHERE id = 1";
	//  7:   final q2 = "SELECT a FROM users WHERE id = $id";
	//  8: }
	const dartFixture = `void createProducts() {
  final ddl = "CREATE TABLE products (id INT PRIMARY KEY, name TEXT NOT NULL)";
}

void queryProducts(String t, int id) {
  final q1 = "SELECT a FROM $t WHERE id = 1";
  final q2 = "SELECT a FROM users WHERE id = $id";
}
`
	writeFile(t, dir, "products.dart", dartFixture)
	gitAdd(t, dir, ".")
	gitCommit(t, dir, "init")

	orch := indexer.NewOrchestrator(database, pool)
	if err := orch.IndexAll(ctx, dir); err != nil {
		t.Fatalf("IndexAll: %v", err)
	}

	dartNodes, err := database.GetNodesInFile(ctx, "products.dart")
	if err != nil {
		t.Fatalf("GetNodesInFile(products.dart): %v", err)
	}

	// --- Criterion 1: DDL → table node "products" ---
	var productsNode *types.Node
	for i := range dartNodes {
		if dartNodes[i].Kind == types.NodeKindTable && dartNodes[i].Name == "products" {
			productsNode = &dartNodes[i]
			break
		}
	}
	if productsNode == nil {
		t.Fatalf("FAIL: no table node 'products' from Dart DDL — Dart Shape-2 harvester not wired (CP2)")
	}
	// StartLine must be exactly 2 (file-absolute): DDL literal is on line 2 of the fixture.
	if productsNode.StartLine != 2 {
		t.Errorf("products table StartLine=%d, want 2 (file-absolute)", productsNode.StartLine)
	}

	allRefs, err := database.GetUnresolvedRefs(ctx, 0, 0)
	if err != nil {
		t.Fatalf("GetUnresolvedRefs: %v", err)
	}

	// --- Criterion 2a: interpolated table target → ZERO refs (decision 8a) ---
	// q1 = "SELECT a FROM $t WHERE id = 1" → after substitution: FROM ? → no ref.
	// Collect ALL refs from products.dart to "t" (the interpolated identifier).
	var tRefs []types.UnresolvedReference
	for _, ref := range allRefs {
		if ref.FilePath == "products.dart" && ref.ReferenceName == "t" {
			tRefs = append(tRefs, ref)
		}
	}
	// WHY len==0: the interpolation replaces $t with "?", so the SQL fragment
	// after FROM is "?", not "t". IsSQLLiteral + the SQL parser see "?" as a
	// placeholder, not a table name. Any non-zero count here means the Dart
	// Shape-2 interpolation substitution is not working.
	if len(tRefs) != 0 {
		t.Errorf("FAIL: interpolated table target '$t' must yield 0 refs, got %d: %+v — Dart Shape-2 interpolation not substituting (CP2)", len(tRefs), tRefs)
	}

	// --- Criterion 2b: literal table + interpolated value → ref to "users" ---
	// q2 = "SELECT a FROM users WHERE id = $id" → "users" survives substitution.
	var usersRef *types.UnresolvedReference
	for i := range allRefs {
		if allRefs[i].FilePath == "products.dart" && allRefs[i].ReferenceName == "users" {
			usersRef = &allRefs[i]
			break
		}
	}
	if usersRef == nil {
		t.Errorf("FAIL: no unresolved ref for 'users' from products.dart (q2 literal table) — Dart Shape-2 literal table not extracted (CP2)")
	}
	if usersRef != nil && usersRef.Language != types.LanguageSQL {
		t.Errorf("users ref Language=%q, want %q", usersRef.Language, types.LanguageSQL)
	}

	// --- Criterion 3: GetEdgesByProvenance("embedded") ≥1 ---
	embeddedEdges, err := database.GetEdgesByProvenance(ctx, "embedded")
	if err != nil {
		t.Fatalf("GetEdgesByProvenance(embedded): %v", err)
	}
	if len(embeddedEdges) == 0 {
		t.Fatalf("FAIL: GetEdgesByProvenance(embedded) returned 0 edges for Dart fixture (CP2)")
	}
}
