package indexer_test

// embedded_sql_lang_test.go — CP3 per-language end-to-end tests for the
// generic embedded-SQL harvester.
//
// Covers the 12 languages not smoke-tested in CP2 (Java/C#/Lua/Dart):
//
//   C, C++, Kotlin, Luau, Objective-C, PHP, Ruby, Rust, Scala, Swift,
//   Pascal, JavaScript.
//
// Each test runs a real orchestrator index over a temp dir and makes
// falsifiable assertions:
//   - Exact table node name and exact file-absolute StartLine (not vacuous < N).
//   - DML → UnresolvedReference owned by the enclosing function/method node.
//   - GetEdgesByProvenance("embedded") returns ≥1 (DDL contains edges).
//   - Secondary string forms (raw/heredoc/triple-quoted) where idiomatic.
//   - Interpolation: table-target substituted → zero refs; value substituted → real ref.
//   - Prose-rejection: non-SQL strings produce zero embedded nodes/edges.

import (
	"context"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/indexer"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// ---------------------------------------------------------------------------
// TestEmbeddedSQLInCFile — C, content-child (string_literal / string_content)
// ---------------------------------------------------------------------------

// TestEmbeddedSQLInCFile verifies embedded SQL extraction for C .c files.
//
// Uses a plain double-quoted string literal (the only form for C).
// Fixture (1-based lines):
//
//	1: static const char *DDL = "CREATE TABLE widgets (id INTEGER PRIMARY KEY, name TEXT NOT NULL)";
//	2:
//	3: void load_widget(int id) {
//	4:     const char *q = "SELECT id, name FROM widgets WHERE id = ?";
//	5: }
//
// Asserts:
//   - table node "widgets" with StartLine == 1
//   - unresolved ref to "widgets" owned by the load_widget function node
//   - GetEdgesByProvenance("embedded") ≥1
func TestEmbeddedSQLInCFile(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	database := openTestDB(t)

	dir := t.TempDir()
	initGitRepo(t, dir)

	const cFixture = `static const char *DDL = "CREATE TABLE widgets (id INTEGER PRIMARY KEY, name TEXT NOT NULL)";

void load_widget(int id) {
    const char *q = "SELECT id, name FROM widgets WHERE id = ?";
}
`
	writeFile(t, dir, "widget.c", cFixture)
	gitAdd(t, dir, ".")
	gitCommit(t, dir, "init")

	orch := indexer.NewOrchestrator(database, pool)
	if err := orch.IndexAll(ctx, dir); err != nil {
		t.Fatalf("IndexAll: %v", err)
	}

	cNodes, err := database.GetNodesInFile(ctx, "widget.c")
	if err != nil {
		t.Fatalf("GetNodesInFile(widget.c): %v", err)
	}

	// --- Criterion 1: DDL → table node "widgets" at line 1 ---
	var widgetsNode *types.Node
	for i := range cNodes {
		if cNodes[i].Kind == types.NodeKindTable && cNodes[i].Name == "widgets" {
			widgetsNode = &cNodes[i]
			break
		}
	}
	if widgetsNode == nil {
		t.Fatalf("FAIL: no table node 'widgets' in widget.c — C content-child harvester not wired (CP3)")
	}
	if widgetsNode.StartLine != 1 {
		t.Errorf("widgets table StartLine=%d, want 1 (file-absolute)", widgetsNode.StartLine)
	}

	// --- Criterion 2: DML → unresolved ref owned by load_widget ---
	allRefs, err := database.GetUnresolvedRefs(ctx, 0, 0)
	if err != nil {
		t.Fatalf("GetUnresolvedRefs: %v", err)
	}

	var loadWidgetNode *types.Node
	for i := range cNodes {
		if cNodes[i].Kind == types.NodeKindFunction && cNodes[i].Name == "load_widget" {
			loadWidgetNode = &cNodes[i]
			break
		}
	}
	if loadWidgetNode == nil {
		t.Fatal("FAIL: load_widget function node not found in widget.c — needed for ownership assertion")
	}

	var dmlRef *types.UnresolvedReference
	for i := range allRefs {
		if allRefs[i].FilePath == "widget.c" &&
			allRefs[i].ReferenceName == "widgets" &&
			allRefs[i].FromNodeID == loadWidgetNode.ID {
			dmlRef = &allRefs[i]
			break
		}
	}
	if dmlRef == nil {
		t.Errorf("FAIL: no unresolved ref for 'widgets' from widget.c owned by load_widget — C DML not extracted (CP3)")
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
		t.Fatalf("FAIL: GetEdgesByProvenance(embedded) returned 0 edges for C fixture (CP3)")
	}
}

// ---------------------------------------------------------------------------
// TestEmbeddedSQLInCppFile — C++, raw string literal (secondary idiomatic form)
// ---------------------------------------------------------------------------

// TestEmbeddedSQLInCppFile verifies embedded SQL extraction for C++ .cpp files.
//
// Uses the raw string `R"(...)"` form — the idiomatic multi-line form for C++.
// Fixture (1-based lines):
//
//	1: #include <string>
//	2:
//	3: static const std::string DDL = R"(CREATE TABLE orders (id INTEGER PRIMARY KEY, total REAL))";
//	4:
//	5: void fetch_order(int id) {
//	6:     std::string q = "SELECT id, total FROM orders WHERE id = ?";
//	7: }
//
// Asserts:
//   - table node "orders" with StartLine == 3 (raw string literal on line 3)
//   - unresolved ref to "orders" owned by fetch_order function node
//   - GetEdgesByProvenance("embedded") ≥1
func TestEmbeddedSQLInCppFile(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	database := openTestDB(t)

	dir := t.TempDir()
	initGitRepo(t, dir)

	const cppFixture = `#include <string>

static const std::string DDL = R"(CREATE TABLE orders (id INTEGER PRIMARY KEY, total REAL))";

void fetch_order(int id) {
    std::string q = "SELECT id, total FROM orders WHERE id = ?";
}
`
	writeFile(t, dir, "orders.cpp", cppFixture)
	gitAdd(t, dir, ".")
	gitCommit(t, dir, "init")

	orch := indexer.NewOrchestrator(database, pool)
	if err := orch.IndexAll(ctx, dir); err != nil {
		t.Fatalf("IndexAll: %v", err)
	}

	cppNodes, err := database.GetNodesInFile(ctx, "orders.cpp")
	if err != nil {
		t.Fatalf("GetNodesInFile(orders.cpp): %v", err)
	}

	// --- Criterion 1: DDL → table node "orders" at line 3 (raw string) ---
	var ordersNode *types.Node
	for i := range cppNodes {
		if cppNodes[i].Kind == types.NodeKindTable && cppNodes[i].Name == "orders" {
			ordersNode = &cppNodes[i]
			break
		}
	}
	if ordersNode == nil {
		t.Fatalf("FAIL: no table node 'orders' in orders.cpp — C++ raw_string_literal harvester not wired (CP3)")
	}
	if ordersNode.StartLine != 3 {
		t.Errorf("orders table StartLine=%d, want 3 (file-absolute)", ordersNode.StartLine)
	}

	// --- Criterion 2: DML → unresolved ref owned by fetch_order ---
	allRefs, err := database.GetUnresolvedRefs(ctx, 0, 0)
	if err != nil {
		t.Fatalf("GetUnresolvedRefs: %v", err)
	}

	var fetchOrderNode *types.Node
	for i := range cppNodes {
		if cppNodes[i].Kind == types.NodeKindFunction && cppNodes[i].Name == "fetch_order" {
			fetchOrderNode = &cppNodes[i]
			break
		}
	}
	if fetchOrderNode == nil {
		t.Fatal("FAIL: fetch_order function node not found in orders.cpp — needed for ownership assertion")
	}

	var dmlRef *types.UnresolvedReference
	for i := range allRefs {
		if allRefs[i].FilePath == "orders.cpp" &&
			allRefs[i].ReferenceName == "orders" &&
			allRefs[i].FromNodeID == fetchOrderNode.ID {
			dmlRef = &allRefs[i]
			break
		}
	}
	if dmlRef == nil {
		t.Errorf("FAIL: no unresolved ref for 'orders' from orders.cpp owned by fetch_order — C++ DML not extracted (CP3)")
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
		t.Fatalf("FAIL: GetEdgesByProvenance(embedded) returned 0 edges for C++ fixture (CP3)")
	}
}

// ---------------------------------------------------------------------------
// TestEmbeddedSQLInKotlinFile — Kotlin, triple-quoted + interpolation
// ---------------------------------------------------------------------------

// TestEmbeddedSQLInKotlinFile verifies embedded SQL extraction for Kotlin .kt files.
//
// Uses the triple-quoted `"""..."""` form — the idiomatic multi-line form for Kotlin.
// Also covers the interpolation criterion:
//   - Interpolated table target (`"SELECT a FROM $t"`) → zero refs for "t".
//   - Plain table + interpolated value (`"SELECT a FROM invoices WHERE id = $id"`)
//     → a real ref to "invoices".
//
// Fixture (1-based lines):
//
//	1: class InvoiceRepo {
//	2:     val ddl = """CREATE TABLE invoices (id INT PRIMARY KEY, amount DECIMAL)"""
//	3:
//	4:     fun fetch(id: Int, t: String) {
//	5:         val q1 = "SELECT id, amount FROM invoices WHERE id = $id"
//	6:         val q2 = "SELECT id, amount FROM $t WHERE id = 1"
//	7:     }
//	8: }
//
// Asserts:
//   - table node "invoices" with StartLine == 2
//   - DML ref to "invoices" from fetch method
//   - zero refs named "t" from this file (interpolated table target)
//   - GetEdgesByProvenance("embedded") ≥1
func TestEmbeddedSQLInKotlinFile(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	database := openTestDB(t)

	dir := t.TempDir()
	initGitRepo(t, dir)

	const kotlinFixture = `class InvoiceRepo {
    val ddl = """CREATE TABLE invoices (id INT PRIMARY KEY, amount DECIMAL)"""

    fun fetch(id: Int, t: String) {
        val q1 = "SELECT id, amount FROM invoices WHERE id = $id"
        val q2 = "SELECT id, amount FROM $t WHERE id = 1"
    }
}
`
	writeFile(t, dir, "InvoiceRepo.kt", kotlinFixture)
	gitAdd(t, dir, ".")
	gitCommit(t, dir, "init")

	orch := indexer.NewOrchestrator(database, pool)
	if err := orch.IndexAll(ctx, dir); err != nil {
		t.Fatalf("IndexAll: %v", err)
	}

	ktNodes, err := database.GetNodesInFile(ctx, "InvoiceRepo.kt")
	if err != nil {
		t.Fatalf("GetNodesInFile(InvoiceRepo.kt): %v", err)
	}

	// --- Criterion 1: DDL → table node "invoices" at line 2 ---
	var invoicesNode *types.Node
	for i := range ktNodes {
		if ktNodes[i].Kind == types.NodeKindTable && ktNodes[i].Name == "invoices" {
			invoicesNode = &ktNodes[i]
			break
		}
	}
	if invoicesNode == nil {
		t.Fatalf("FAIL: no table node 'invoices' in InvoiceRepo.kt — Kotlin triple-quoted harvester not wired (CP3)")
	}
	if invoicesNode.StartLine != 2 {
		t.Errorf("invoices table StartLine=%d, want 2 (file-absolute)", invoicesNode.StartLine)
	}

	// --- Criterion 2: DML ref from InvoiceRepo.kt to "invoices" owned by fetch ---
	allRefs, err := database.GetUnresolvedRefs(ctx, 0, 0)
	if err != nil {
		t.Fatalf("GetUnresolvedRefs: %v", err)
	}

	var fetchNode *types.Node
	for i := range ktNodes {
		if ktNodes[i].Kind == types.NodeKindFunction && ktNodes[i].Name == "fetch" {
			fetchNode = &ktNodes[i]
			break
		}
	}
	if fetchNode == nil {
		t.Fatal("FAIL: fetch function node not found in InvoiceRepo.kt — needed for ownership assertion")
	}

	var invoicesRef *types.UnresolvedReference
	for i := range allRefs {
		if allRefs[i].FilePath == "InvoiceRepo.kt" &&
			allRefs[i].ReferenceName == "invoices" &&
			allRefs[i].FromNodeID == fetchNode.ID {
			invoicesRef = &allRefs[i]
			break
		}
	}
	if invoicesRef == nil {
		t.Errorf("FAIL: no unresolved ref for 'invoices' from InvoiceRepo.kt owned by fetch — Kotlin DML not extracted (CP3)")
	}
	if invoicesRef != nil && invoicesRef.Language != types.LanguageSQL {
		t.Errorf("invoices ref Language=%q, want %q", invoicesRef.Language, types.LanguageSQL)
	}

	// --- Criterion 3: interpolated table target → zero refs for "t" ---
	// q2 = "SELECT id, amount FROM $t WHERE id = 1" → FROM ? → no ref named "t".
	var tRefs []types.UnresolvedReference
	for _, ref := range allRefs {
		if ref.FilePath == "InvoiceRepo.kt" && ref.ReferenceName == "t" {
			tRefs = append(tRefs, ref)
		}
	}
	// WHY len==0: $t is an interpolated_identifier; the harvester replaces it
	// with "?", so the SQL fragment reads FROM ?, not FROM t.
	if len(tRefs) != 0 {
		t.Errorf("FAIL: interpolated table target '$t' must yield 0 refs, got %d — Kotlin interpolation not substituted (CP3)", len(tRefs))
	}

	// --- Criterion 4: GetEdgesByProvenance("embedded") ≥1 ---
	embeddedEdges, err := database.GetEdgesByProvenance(ctx, "embedded")
	if err != nil {
		t.Fatalf("GetEdgesByProvenance(embedded): %v", err)
	}
	if len(embeddedEdges) == 0 {
		t.Fatalf("FAIL: GetEdgesByProvenance(embedded) returned 0 edges for Kotlin fixture (CP3)")
	}
}

// ---------------------------------------------------------------------------
// TestEmbeddedSQLInLuauFile — Luau, content-child (string / string_content)
// ---------------------------------------------------------------------------

// TestEmbeddedSQLInLuauFile verifies embedded SQL extraction for Luau .luau files.
//
// Luau is the Roblox Lua dialect; its grammar exposes string_content children
// (unlike Lua which is Shape-2). Uses plain double-quoted strings.
//
// Fixture (1-based lines):
//
//	1: local DDL = "CREATE TABLE inventory (id INTEGER PRIMARY KEY, item TEXT NOT NULL)"
//	2:
//	3: local function getItem(db, id)
//	4:     local q = "SELECT id, item FROM inventory WHERE id = ?"
//	5:     return db:query(q)
//	6: end
//
// Asserts:
//   - table node "inventory" with StartLine == 1
//   - unresolved ref to "inventory" from getItem function
//   - GetEdgesByProvenance("embedded") ≥1
func TestEmbeddedSQLInLuauFile(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	database := openTestDB(t)

	dir := t.TempDir()
	initGitRepo(t, dir)

	const luauFixture = `local DDL = "CREATE TABLE inventory (id INTEGER PRIMARY KEY, item TEXT NOT NULL)"

local function getItem(db, id)
    local q = "SELECT id, item FROM inventory WHERE id = ?"
    return db:query(q)
end
`
	writeFile(t, dir, "inventory.luau", luauFixture)
	gitAdd(t, dir, ".")
	gitCommit(t, dir, "init")

	orch := indexer.NewOrchestrator(database, pool)
	if err := orch.IndexAll(ctx, dir); err != nil {
		t.Fatalf("IndexAll: %v", err)
	}

	luauNodes, err := database.GetNodesInFile(ctx, "inventory.luau")
	if err != nil {
		t.Fatalf("GetNodesInFile(inventory.luau): %v", err)
	}

	// --- Criterion 1: DDL → table node "inventory" at line 1 ---
	var inventoryNode *types.Node
	for i := range luauNodes {
		if luauNodes[i].Kind == types.NodeKindTable && luauNodes[i].Name == "inventory" {
			inventoryNode = &luauNodes[i]
			break
		}
	}
	if inventoryNode == nil {
		t.Fatalf("FAIL: no table node 'inventory' in inventory.luau — Luau content-child harvester not wired (CP3)")
	}
	if inventoryNode.StartLine != 1 {
		t.Errorf("inventory table StartLine=%d, want 1 (file-absolute)", inventoryNode.StartLine)
	}

	// --- Criterion 2: DML → unresolved ref to "inventory" owned by getItem ---
	allRefs, err := database.GetUnresolvedRefs(ctx, 0, 0)
	if err != nil {
		t.Fatalf("GetUnresolvedRefs: %v", err)
	}

	var getItemNode *types.Node
	for i := range luauNodes {
		if luauNodes[i].Kind == types.NodeKindFunction && luauNodes[i].Name == "getItem" {
			getItemNode = &luauNodes[i]
			break
		}
	}
	if getItemNode == nil {
		t.Fatal("FAIL: getItem function node not found in inventory.luau — needed for ownership assertion")
	}

	var dmlRef *types.UnresolvedReference
	for i := range allRefs {
		if allRefs[i].FilePath == "inventory.luau" &&
			allRefs[i].ReferenceName == "inventory" &&
			allRefs[i].FromNodeID == getItemNode.ID {
			dmlRef = &allRefs[i]
			break
		}
	}
	if dmlRef == nil {
		t.Errorf("FAIL: no unresolved ref for 'inventory' from inventory.luau owned by getItem — Luau DML not extracted (CP3)")
	}
	if dmlRef != nil && dmlRef.Language != types.LanguageSQL {
		t.Errorf("inventory ref Language=%q, want %q", dmlRef.Language, types.LanguageSQL)
	}

	// --- Criterion 3: GetEdgesByProvenance("embedded") ≥1 ---
	embeddedEdges, err := database.GetEdgesByProvenance(ctx, "embedded")
	if err != nil {
		t.Fatalf("GetEdgesByProvenance(embedded): %v", err)
	}
	if len(embeddedEdges) == 0 {
		t.Fatalf("FAIL: GetEdgesByProvenance(embedded) returned 0 edges for Luau fixture (CP3)")
	}
}

// ---------------------------------------------------------------------------
// TestEmbeddedSQLInObjCFile — Objective-C, content-child (string_literal / string_content)
// ---------------------------------------------------------------------------

// TestEmbeddedSQLInObjCFile verifies embedded SQL extraction for ObjC .m files.
//
// ObjC uses @"..." NS string literals parsed as string_literal nodes.
// Fixture (1-based lines):
//
//	1: #import <Foundation/Foundation.h>
//	2:
//	3: static NSString *DDL = @"CREATE TABLE contacts (id INTEGER PRIMARY KEY, name TEXT)";
//	4:
//	5: void loadContact(int contactId) {
//	6:     NSString *q = @"SELECT id, name FROM contacts WHERE id = ?";
//	7: }
//
// Asserts:
//   - table node "contacts" with StartLine == 3
//   - unresolved ref to "contacts" owned by loadContact
//   - GetEdgesByProvenance("embedded") ≥1
func TestEmbeddedSQLInObjCFile(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	database := openTestDB(t)

	dir := t.TempDir()
	initGitRepo(t, dir)

	const objcFixture = `#import <Foundation/Foundation.h>

static NSString *DDL = @"CREATE TABLE contacts (id INTEGER PRIMARY KEY, name TEXT)";

void loadContact(int contactId) {
    NSString *q = @"SELECT id, name FROM contacts WHERE id = ?";
}
`
	writeFile(t, dir, "contacts.m", objcFixture)
	gitAdd(t, dir, ".")
	gitCommit(t, dir, "init")

	orch := indexer.NewOrchestrator(database, pool)
	if err := orch.IndexAll(ctx, dir); err != nil {
		t.Fatalf("IndexAll: %v", err)
	}

	objcNodes, err := database.GetNodesInFile(ctx, "contacts.m")
	if err != nil {
		t.Fatalf("GetNodesInFile(contacts.m): %v", err)
	}

	// --- Criterion 1: DDL → table node "contacts" at line 3 ---
	var contactsNode *types.Node
	for i := range objcNodes {
		if objcNodes[i].Kind == types.NodeKindTable && objcNodes[i].Name == "contacts" {
			contactsNode = &objcNodes[i]
			break
		}
	}
	if contactsNode == nil {
		t.Fatalf("FAIL: no table node 'contacts' in contacts.m — ObjC content-child harvester not wired (CP3)")
	}
	if contactsNode.StartLine != 3 {
		t.Errorf("contacts table StartLine=%d, want 3 (file-absolute)", contactsNode.StartLine)
	}

	// --- Criterion 2: DML → unresolved ref owned by loadContact ---
	allRefs, err := database.GetUnresolvedRefs(ctx, 0, 0)
	if err != nil {
		t.Fatalf("GetUnresolvedRefs: %v", err)
	}

	var loadContactNode *types.Node
	for i := range objcNodes {
		if objcNodes[i].Kind == types.NodeKindFunction && objcNodes[i].Name == "loadContact" {
			loadContactNode = &objcNodes[i]
			break
		}
	}
	if loadContactNode == nil {
		t.Fatal("FAIL: loadContact function node not found in contacts.m — needed for ownership assertion")
	}

	var dmlRef *types.UnresolvedReference
	for i := range allRefs {
		if allRefs[i].FilePath == "contacts.m" &&
			allRefs[i].ReferenceName == "contacts" &&
			allRefs[i].FromNodeID == loadContactNode.ID {
			dmlRef = &allRefs[i]
			break
		}
	}
	if dmlRef == nil {
		t.Errorf("FAIL: no unresolved ref for 'contacts' from contacts.m owned by loadContact — ObjC DML not extracted (CP3)")
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
		t.Fatalf("FAIL: GetEdgesByProvenance(embedded) returned 0 edges for ObjC fixture (CP3)")
	}
}

// ---------------------------------------------------------------------------
// TestEmbeddedSQLInPHPFile — PHP, heredoc + interpolation
// ---------------------------------------------------------------------------

// TestEmbeddedSQLInPHPFile verifies embedded SQL extraction for PHP .php files.
//
// Uses a heredoc `<<<SQL\n...\nSQL;` — the idiomatic multi-line SQL form in PHP.
// Also covers the interpolation criterion: `"SELECT a FROM $t"` → zero refs for "t".
//
// Fixture (1-based lines):
//
//	1: <?php
//	2:
//	3: $ddl = <<<SQL
//	4: CREATE TABLE payments (id INT PRIMARY KEY, amount DECIMAL(10,2))
//	5: SQL;
//	6:
//	7: function fetchPayment($id, $t) {
//	8:     $q1 = "SELECT id, amount FROM payments WHERE id = ?";
//	9:     $q2 = "SELECT id, amount FROM $t WHERE id = 1";
//	10: }
//
// NOTE on heredoc StartLine: The heredoc node in the PHP grammar starts at the
// `<<<SQL` delimiter line (line 3). StartLine==3 is the expected value.
//
// Asserts:
//   - table node "payments" (StartLine == 3, the heredoc opener)
//   - unresolved ref to "payments" from fetchPayment
//   - zero refs named "t" (interpolated table target)
//   - GetEdgesByProvenance("embedded") ≥1
func TestEmbeddedSQLInPHPFile(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	database := openTestDB(t)

	dir := t.TempDir()
	initGitRepo(t, dir)

	const phpFixture = `<?php

$ddl = <<<SQL
CREATE TABLE payments (id INT PRIMARY KEY, amount DECIMAL(10,2))
SQL;

function fetchPayment($id, $t) {
    $q1 = "SELECT id, amount FROM payments WHERE id = ?";
    $q2 = "SELECT id, amount FROM $t WHERE id = 1";
}
`
	writeFile(t, dir, "payment.php", phpFixture)
	gitAdd(t, dir, ".")
	gitCommit(t, dir, "init")

	orch := indexer.NewOrchestrator(database, pool)
	if err := orch.IndexAll(ctx, dir); err != nil {
		t.Fatalf("IndexAll: %v", err)
	}

	phpNodes, err := database.GetNodesInFile(ctx, "payment.php")
	if err != nil {
		t.Fatalf("GetNodesInFile(payment.php): %v", err)
	}

	// --- Criterion 1: DDL → table node "payments" ---
	var paymentsNode *types.Node
	for i := range phpNodes {
		if phpNodes[i].Kind == types.NodeKindTable && phpNodes[i].Name == "payments" {
			paymentsNode = &phpNodes[i]
			break
		}
	}
	if paymentsNode == nil {
		t.Fatalf("FAIL: no table node 'payments' in payment.php — PHP heredoc harvester not wired (CP3)")
	}
	// The heredoc node spans from the <<<SQL line (line 3) through the closing
	// delimiter. StartLine must be exactly 3.
	if paymentsNode.StartLine != 3 {
		t.Errorf("payments table StartLine=%d, want 3 (heredoc opener, file-absolute)", paymentsNode.StartLine)
	}

	// --- Criterion 2: DML → ref to "payments" from fetchPayment (owned by fetchPayment) ---
	allRefs, err := database.GetUnresolvedRefs(ctx, 0, 0)
	if err != nil {
		t.Fatalf("GetUnresolvedRefs: %v", err)
	}

	var fetchPaymentNode *types.Node
	for i := range phpNodes {
		if phpNodes[i].Kind == types.NodeKindFunction && phpNodes[i].Name == "fetchPayment" {
			fetchPaymentNode = &phpNodes[i]
			break
		}
	}
	if fetchPaymentNode == nil {
		t.Fatal("FAIL: fetchPayment function node not found in payment.php — needed for ownership assertion")
	}

	var paymentsRef *types.UnresolvedReference
	for i := range allRefs {
		if allRefs[i].FilePath == "payment.php" &&
			allRefs[i].ReferenceName == "payments" &&
			allRefs[i].FromNodeID == fetchPaymentNode.ID {
			paymentsRef = &allRefs[i]
			break
		}
	}
	if paymentsRef == nil {
		t.Errorf("FAIL: no unresolved ref for 'payments' from payment.php owned by fetchPayment — PHP DML not extracted (CP3)")
	}
	if paymentsRef != nil && paymentsRef.Language != types.LanguageSQL {
		t.Errorf("payments ref Language=%q, want %q", paymentsRef.Language, types.LanguageSQL)
	}

	// --- Criterion 3: interpolated table target → zero refs for "t" ---
	// q2 = "SELECT id, amount FROM $t WHERE id = 1" → $t is variable_name interp;
	// after substitution FROM ? → no ref named "t".
	var tRefs []types.UnresolvedReference
	for _, ref := range allRefs {
		if ref.FilePath == "payment.php" && ref.ReferenceName == "t" {
			tRefs = append(tRefs, ref)
		}
	}
	if len(tRefs) != 0 {
		t.Errorf("FAIL: interpolated table target '$t' must yield 0 refs, got %d — PHP interpolation not substituted (CP3)", len(tRefs))
	}

	// --- Criterion 4: GetEdgesByProvenance("embedded") ≥1 ---
	embeddedEdges, err := database.GetEdgesByProvenance(ctx, "embedded")
	if err != nil {
		t.Fatalf("GetEdgesByProvenance(embedded): %v", err)
	}
	if len(embeddedEdges) == 0 {
		t.Fatalf("FAIL: GetEdgesByProvenance(embedded) returned 0 edges for PHP fixture (CP3)")
	}
}

// ---------------------------------------------------------------------------
// TestEmbeddedSQLInRubyFile — Ruby, heredoc + interpolation (two criteria)
// ---------------------------------------------------------------------------

// TestEmbeddedSQLInRubyFile verifies embedded SQL extraction for Ruby .rb files.
//
// Uses `<<~SQL\n...\nSQL` — the idiomatic squiggly heredoc form in Ruby.
// Covers two interpolation criteria:
//   - Interpolated table target (`"SELECT a FROM #{t}"`) → zero refs for "t".
//   - Plain table + interpolated value (`"SELECT a FROM users WHERE id = #{id}"`)
//     → a real ref to "users".
//
// Fixture (1-based lines):
//
//	1: DDL = <<~SQL
//	2:   CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT NOT NULL)
//	3: SQL
//	4:
//	5: def fetch_user(id, t)
//	6:   q1 = "SELECT id, name FROM users WHERE id = #{id}"
//	7:   q2 = "SELECT id, name FROM #{t} WHERE id = 1"
//	8: end
//
// Asserts:
//   - table node "users" (StartLine == 2, heredoc content line — Ruby grammar
//     places heredoc_body start at the content line, not the <<~SQL opener)
//   - ref to "users" from fetch_user (q1 — literal table + interp value)
//   - zero refs named "t" (q2 — interpolated table target)
//   - GetEdgesByProvenance("embedded") ≥1
func TestEmbeddedSQLInRubyFile(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	database := openTestDB(t)

	dir := t.TempDir()
	initGitRepo(t, dir)

	const rubyFixture = `DDL = <<~SQL
  CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT NOT NULL)
SQL

def fetch_user(id, t)
  q1 = "SELECT id, name FROM users WHERE id = #{id}"
  q2 = "SELECT id, name FROM #{t} WHERE id = 1"
end
`
	writeFile(t, dir, "user.rb", rubyFixture)
	gitAdd(t, dir, ".")
	gitCommit(t, dir, "init")

	orch := indexer.NewOrchestrator(database, pool)
	if err := orch.IndexAll(ctx, dir); err != nil {
		t.Fatalf("IndexAll: %v", err)
	}

	rbNodes, err := database.GetNodesInFile(ctx, "user.rb")
	if err != nil {
		t.Fatalf("GetNodesInFile(user.rb): %v", err)
	}

	// --- Criterion 1: DDL → table node "users" ---
	var usersNode *types.Node
	for i := range rbNodes {
		if rbNodes[i].Kind == types.NodeKindTable && rbNodes[i].Name == "users" {
			usersNode = &rbNodes[i]
			break
		}
	}
	if usersNode == nil {
		t.Fatalf("FAIL: no table node 'users' in user.rb — Ruby heredoc harvester not wired (CP3)")
	}
	// The Ruby grammar places the heredoc_body node start at the content line
	// (line 2 in this fixture — the line after the <<~SQL opener). The opener
	// line itself (line 1) is not part of the heredoc_body node.
	if usersNode.StartLine != 2 {
		t.Errorf("users table StartLine=%d, want 2 (heredoc content line, file-absolute)", usersNode.StartLine)
	}

	allRefs, err := database.GetUnresolvedRefs(ctx, 0, 0)
	if err != nil {
		t.Fatalf("GetUnresolvedRefs: %v", err)
	}

	// --- Criterion 2: literal table + interpolated value → ref to "users" owned by fetch_user ---
	// q1 = "SELECT id, name FROM users WHERE id = #{id}" — "users" is literal.
	var fetchUserNode *types.Node
	for i := range rbNodes {
		if rbNodes[i].Kind == types.NodeKindFunction && rbNodes[i].Name == "fetch_user" {
			fetchUserNode = &rbNodes[i]
			break
		}
	}
	if fetchUserNode == nil {
		t.Fatal("FAIL: fetch_user function node not found in user.rb — needed for ownership assertion")
	}

	var usersRef *types.UnresolvedReference
	for i := range allRefs {
		if allRefs[i].FilePath == "user.rb" &&
			allRefs[i].ReferenceName == "users" &&
			allRefs[i].FromNodeID == fetchUserNode.ID {
			usersRef = &allRefs[i]
			break
		}
	}
	if usersRef == nil {
		t.Errorf("FAIL: no unresolved ref for 'users' from user.rb owned by fetch_user (q1 literal table) — Ruby DML not extracted (CP3)")
	}
	if usersRef != nil && usersRef.Language != types.LanguageSQL {
		t.Errorf("users ref Language=%q, want %q", usersRef.Language, types.LanguageSQL)
	}

	// --- Criterion 3: interpolated table target → zero refs for "t" ---
	// q2 = "SELECT id, name FROM #{t} WHERE id = 1" → #{t} is interpolation;
	// after substitution FROM ? → no ref named "t".
	var tRefs []types.UnresolvedReference
	for _, ref := range allRefs {
		if ref.FilePath == "user.rb" && ref.ReferenceName == "t" {
			tRefs = append(tRefs, ref)
		}
	}
	// WHY len==0: #{t} is an interpolation node; the harvester replaces it with
	// "?", so the SQL fragment reads FROM ?, not FROM t. Any non-zero count here
	// means the Ruby interpolation substitution is not working.
	if len(tRefs) != 0 {
		t.Errorf("FAIL: interpolated table target '#{t}' must yield 0 refs, got %d — Ruby interpolation not substituted (CP3)", len(tRefs))
	}

	// --- Criterion 4: GetEdgesByProvenance("embedded") ≥1 ---
	embeddedEdges, err := database.GetEdgesByProvenance(ctx, "embedded")
	if err != nil {
		t.Fatalf("GetEdgesByProvenance(embedded): %v", err)
	}
	if len(embeddedEdges) == 0 {
		t.Fatalf("FAIL: GetEdgesByProvenance(embedded) returned 0 edges for Ruby fixture (CP3)")
	}
}

// ---------------------------------------------------------------------------
// TestEmbeddedSQLInRustFile — Rust, raw string literal r#"..."#
// ---------------------------------------------------------------------------

// TestEmbeddedSQLInRustFile verifies embedded SQL extraction for Rust .rs files.
//
// Uses `r#"..."#` — the idiomatic raw string literal form in Rust for multi-line SQL.
// Fixture (1-based lines):
//
//	1: static DDL: &str = r#"CREATE TABLE shipments (id INTEGER PRIMARY KEY, destination TEXT)"#;
//	2:
//	3: fn get_shipment(id: i64) {
//	4:     let q = "SELECT id, destination FROM shipments WHERE id = ?";
//	5: }
//
// Asserts:
//   - table node "shipments" with StartLine == 1
//   - unresolved ref to "shipments" owned by get_shipment
//   - GetEdgesByProvenance("embedded") ≥1
func TestEmbeddedSQLInRustFile(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	database := openTestDB(t)

	dir := t.TempDir()
	initGitRepo(t, dir)

	const rustFixture = `static DDL: &str = r#"CREATE TABLE shipments (id INTEGER PRIMARY KEY, destination TEXT)"#;

fn get_shipment(id: i64) {
    let q = "SELECT id, destination FROM shipments WHERE id = ?";
}
`
	writeFile(t, dir, "shipment.rs", rustFixture)
	gitAdd(t, dir, ".")
	gitCommit(t, dir, "init")

	orch := indexer.NewOrchestrator(database, pool)
	if err := orch.IndexAll(ctx, dir); err != nil {
		t.Fatalf("IndexAll: %v", err)
	}

	rsNodes, err := database.GetNodesInFile(ctx, "shipment.rs")
	if err != nil {
		t.Fatalf("GetNodesInFile(shipment.rs): %v", err)
	}

	// --- Criterion 1: DDL → table node "shipments" at line 1 ---
	var shipmentsNode *types.Node
	for i := range rsNodes {
		if rsNodes[i].Kind == types.NodeKindTable && rsNodes[i].Name == "shipments" {
			shipmentsNode = &rsNodes[i]
			break
		}
	}
	if shipmentsNode == nil {
		t.Fatalf("FAIL: no table node 'shipments' in shipment.rs — Rust raw_string_literal harvester not wired (CP3)")
	}
	if shipmentsNode.StartLine != 1 {
		t.Errorf("shipments table StartLine=%d, want 1 (file-absolute)", shipmentsNode.StartLine)
	}

	// --- Criterion 2: DML → unresolved ref owned by get_shipment ---
	allRefs, err := database.GetUnresolvedRefs(ctx, 0, 0)
	if err != nil {
		t.Fatalf("GetUnresolvedRefs: %v", err)
	}

	var getShipmentNode *types.Node
	for i := range rsNodes {
		if rsNodes[i].Kind == types.NodeKindFunction && rsNodes[i].Name == "get_shipment" {
			getShipmentNode = &rsNodes[i]
			break
		}
	}
	if getShipmentNode == nil {
		t.Fatal("FAIL: get_shipment function node not found in shipment.rs — needed for ownership assertion")
	}

	var dmlRef *types.UnresolvedReference
	for i := range allRefs {
		if allRefs[i].FilePath == "shipment.rs" &&
			allRefs[i].ReferenceName == "shipments" &&
			allRefs[i].FromNodeID == getShipmentNode.ID {
			dmlRef = &allRefs[i]
			break
		}
	}
	if dmlRef == nil {
		t.Errorf("FAIL: no unresolved ref for 'shipments' from shipment.rs owned by get_shipment — Rust DML not extracted (CP3)")
	}
	if dmlRef != nil && dmlRef.Language != types.LanguageSQL {
		t.Errorf("DML ref Language=%q, want %q", dmlRef.Language, types.LanguageSQL)
	}

	// Also test the prose-rejection guard on a Rust file (see dedicated test
	// TestEmbeddedSQLProseRejectionInRustFile below for the full assertion).

	// --- Criterion 3: GetEdgesByProvenance("embedded") ≥1 ---
	embeddedEdges, err := database.GetEdgesByProvenance(ctx, "embedded")
	if err != nil {
		t.Fatalf("GetEdgesByProvenance(embedded): %v", err)
	}
	if len(embeddedEdges) == 0 {
		t.Fatalf("FAIL: GetEdgesByProvenance(embedded) returned 0 edges for Rust fixture (CP3)")
	}
}

// ---------------------------------------------------------------------------
// TestEmbeddedSQLInScalaFile — Scala, triple-quoted + interpolation
// ---------------------------------------------------------------------------

// TestEmbeddedSQLInScalaFile verifies embedded SQL extraction for Scala .scala files.
//
// Uses `"""..."""` — the idiomatic multi-line form. Also tests the interpolated
// string form `s"SELECT a FROM $t"` → zero refs for "t".
//
// Fixture (1-based lines):
//
//	1: object CatalogRepo {
//	2:   val ddl = """CREATE TABLE catalog (id INT PRIMARY KEY, sku TEXT NOT NULL)"""
//	3:
//	4:   def fetch(id: Int, t: String): Unit = {
//	5:     val q1 = "SELECT id, sku FROM catalog WHERE id = ?"
//	6:     val q2 = s"SELECT id, sku FROM $t WHERE id = 1"
//	7:   }
//	8: }
//
// Asserts:
//   - table node "catalog" with StartLine == 2
//   - ref to "catalog" from fetch method
//   - zero refs named "t" (interpolated table target via s"...")
//   - GetEdgesByProvenance("embedded") ≥1
func TestEmbeddedSQLInScalaFile(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	database := openTestDB(t)

	dir := t.TempDir()
	initGitRepo(t, dir)

	const scalaFixture = `object CatalogRepo {
  val ddl = """CREATE TABLE catalog (id INT PRIMARY KEY, sku TEXT NOT NULL)"""

  def fetch(id: Int, t: String): Unit = {
    val q1 = "SELECT id, sku FROM catalog WHERE id = ?"
    val q2 = s"SELECT id, sku FROM $t WHERE id = 1"
  }
}
`
	writeFile(t, dir, "CatalogRepo.scala", scalaFixture)
	gitAdd(t, dir, ".")
	gitCommit(t, dir, "init")

	orch := indexer.NewOrchestrator(database, pool)
	if err := orch.IndexAll(ctx, dir); err != nil {
		t.Fatalf("IndexAll: %v", err)
	}

	scalaNodes, err := database.GetNodesInFile(ctx, "CatalogRepo.scala")
	if err != nil {
		t.Fatalf("GetNodesInFile(CatalogRepo.scala): %v", err)
	}

	// --- Criterion 1: DDL → table node "catalog" at line 2 ---
	var catalogNode *types.Node
	for i := range scalaNodes {
		if scalaNodes[i].Kind == types.NodeKindTable && scalaNodes[i].Name == "catalog" {
			catalogNode = &scalaNodes[i]
			break
		}
	}
	if catalogNode == nil {
		t.Fatalf("FAIL: no table node 'catalog' in CatalogRepo.scala — Scala triple-quoted harvester not wired (CP3)")
	}
	if catalogNode.StartLine != 2 {
		t.Errorf("catalog table StartLine=%d, want 2 (file-absolute)", catalogNode.StartLine)
	}

	// --- Criterion 2: DML → ref to "catalog" owned by fetch ---
	allRefs, err := database.GetUnresolvedRefs(ctx, 0, 0)
	if err != nil {
		t.Fatalf("GetUnresolvedRefs: %v", err)
	}

	var fetchScalaNode *types.Node
	for i := range scalaNodes {
		if scalaNodes[i].Kind == types.NodeKindFunction && scalaNodes[i].Name == "fetch" {
			fetchScalaNode = &scalaNodes[i]
			break
		}
	}
	if fetchScalaNode == nil {
		t.Fatal("FAIL: fetch function node not found in CatalogRepo.scala — needed for ownership assertion")
	}

	var catalogRef *types.UnresolvedReference
	for i := range allRefs {
		if allRefs[i].FilePath == "CatalogRepo.scala" &&
			allRefs[i].ReferenceName == "catalog" &&
			allRefs[i].FromNodeID == fetchScalaNode.ID {
			catalogRef = &allRefs[i]
			break
		}
	}
	if catalogRef == nil {
		t.Errorf("FAIL: no unresolved ref for 'catalog' from CatalogRepo.scala owned by fetch — Scala DML not extracted (CP3)")
	}
	if catalogRef != nil && catalogRef.Language != types.LanguageSQL {
		t.Errorf("catalog ref Language=%q, want %q", catalogRef.Language, types.LanguageSQL)
	}

	// --- Criterion 3: interpolated table target → zero refs for "t" ---
	// q2 = s"SELECT id, sku FROM $t WHERE id = 1" → $t is an interpolation;
	// after substitution FROM ? → no ref named "t".
	var tRefs []types.UnresolvedReference
	for _, ref := range allRefs {
		if ref.FilePath == "CatalogRepo.scala" && ref.ReferenceName == "t" {
			tRefs = append(tRefs, ref)
		}
	}
	if len(tRefs) != 0 {
		t.Errorf("FAIL: interpolated table target '$t' must yield 0 refs, got %d — Scala interpolation not substituted (CP3)", len(tRefs))
	}

	// --- Criterion 4: GetEdgesByProvenance("embedded") ≥1 ---
	embeddedEdges, err := database.GetEdgesByProvenance(ctx, "embedded")
	if err != nil {
		t.Fatalf("GetEdgesByProvenance(embedded): %v", err)
	}
	if len(embeddedEdges) == 0 {
		t.Fatalf("FAIL: GetEdgesByProvenance(embedded) returned 0 edges for Scala fixture (CP3)")
	}
}

// ---------------------------------------------------------------------------
// TestEmbeddedSQLInSwiftFile — Swift, multi-line string literal + interpolation
// ---------------------------------------------------------------------------

// TestEmbeddedSQLInSwiftFile verifies embedded SQL extraction for Swift .swift files.
//
// Uses `"""..."""` — the multi-line string literal form (multi_line_string_literal node).
// Also tests: `"SELECT a FROM \(t)"` → zero refs for "t" (interpolated table target).
//
// Fixture (1-based lines):
//
//	1: class EventRepo {
//	2:     let ddl = """
//	3:         CREATE TABLE events (id INTEGER PRIMARY KEY, title TEXT NOT NULL)
//	4:         """
//	5:
//	6:     func fetch(id: Int, t: String) {
//	7:         let q1 = "SELECT id, title FROM events WHERE id = ?"
//	8:         let q2 = "SELECT id, title FROM \(t) WHERE id = 1"
//	9:     }
//	10: }
//
// NOTE on multi-line string StartLine: The Swift grammar reports the
// multi_line_string_literal node start at the first content line (line 3 in
// this fixture), not at the `"""` opener line (line 2). StartLine==3 is expected.
//
// Asserts:
//   - table node "events" (StartLine == 3, first content line of multi_line_string_literal)
//   - ref to "events" from fetch method
//   - zero refs named "t" (interpolated table target via \(...))
//   - GetEdgesByProvenance("embedded") ≥1
func TestEmbeddedSQLInSwiftFile(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	database := openTestDB(t)

	dir := t.TempDir()
	initGitRepo(t, dir)

	const swiftFixture = `class EventRepo {
    let ddl = """
        CREATE TABLE events (id INTEGER PRIMARY KEY, title TEXT NOT NULL)
        """

    func fetch(id: Int, t: String) {
        let q1 = "SELECT id, title FROM events WHERE id = ?"
        let q2 = "SELECT id, title FROM \(t) WHERE id = 1"
    }
}
`
	writeFile(t, dir, "EventRepo.swift", swiftFixture)
	gitAdd(t, dir, ".")
	gitCommit(t, dir, "init")

	orch := indexer.NewOrchestrator(database, pool)
	if err := orch.IndexAll(ctx, dir); err != nil {
		t.Fatalf("IndexAll: %v", err)
	}

	swiftNodes, err := database.GetNodesInFile(ctx, "EventRepo.swift")
	if err != nil {
		t.Fatalf("GetNodesInFile(EventRepo.swift): %v", err)
	}

	// --- Criterion 1: DDL → table node "events" ---
	var eventsNode *types.Node
	for i := range swiftNodes {
		if swiftNodes[i].Kind == types.NodeKindTable && swiftNodes[i].Name == "events" {
			eventsNode = &swiftNodes[i]
			break
		}
	}
	if eventsNode == nil {
		t.Fatalf("FAIL: no table node 'events' in EventRepo.swift — Swift multi_line_string_literal harvester not wired (CP3)")
	}
	// The Swift grammar places the multi_line_string_literal node start at the
	// first content line (line 3 in this fixture — the line after the opening """).
	// The """ delimiter line itself is not reported as the node start.
	if eventsNode.StartLine != 3 {
		t.Errorf("events table StartLine=%d, want 3 (multi-line content line, file-absolute)", eventsNode.StartLine)
	}

	// --- Criterion 2: DML → ref to "events" owned by fetch ---
	allRefs, err := database.GetUnresolvedRefs(ctx, 0, 0)
	if err != nil {
		t.Fatalf("GetUnresolvedRefs: %v", err)
	}

	var fetchSwiftNode *types.Node
	for i := range swiftNodes {
		if swiftNodes[i].Kind == types.NodeKindFunction && swiftNodes[i].Name == "fetch" {
			fetchSwiftNode = &swiftNodes[i]
			break
		}
	}
	if fetchSwiftNode == nil {
		t.Fatal("FAIL: fetch function node not found in EventRepo.swift — needed for ownership assertion")
	}

	var eventsRef *types.UnresolvedReference
	for i := range allRefs {
		if allRefs[i].FilePath == "EventRepo.swift" &&
			allRefs[i].ReferenceName == "events" &&
			allRefs[i].FromNodeID == fetchSwiftNode.ID {
			eventsRef = &allRefs[i]
			break
		}
	}
	if eventsRef == nil {
		t.Errorf("FAIL: no unresolved ref for 'events' from EventRepo.swift owned by fetch — Swift DML not extracted (CP3)")
	}
	if eventsRef != nil && eventsRef.Language != types.LanguageSQL {
		t.Errorf("events ref Language=%q, want %q", eventsRef.Language, types.LanguageSQL)
	}

	// --- Criterion 3: interpolated table target → zero refs for "t" ---
	// q2 = "SELECT id, title FROM \(t) WHERE id = 1" → \(t) is interpolated_expression;
	// after substitution FROM ? → no ref named "t".
	var tRefs []types.UnresolvedReference
	for _, ref := range allRefs {
		if ref.FilePath == "EventRepo.swift" && ref.ReferenceName == "t" {
			tRefs = append(tRefs, ref)
		}
	}
	if len(tRefs) != 0 {
		t.Errorf("FAIL: interpolated table target '\\(t)' must yield 0 refs, got %d — Swift interpolation not substituted (CP3)", len(tRefs))
	}

	// --- Criterion 4: GetEdgesByProvenance("embedded") ≥1 ---
	embeddedEdges, err := database.GetEdgesByProvenance(ctx, "embedded")
	if err != nil {
		t.Fatalf("GetEdgesByProvenance(embedded): %v", err)
	}
	if len(embeddedEdges) == 0 {
		t.Fatalf("FAIL: GetEdgesByProvenance(embedded) returned 0 edges for Swift fixture (CP3)")
	}
}

// ---------------------------------------------------------------------------
// TestEmbeddedSQLInPascalFile — Pascal, Shape-2 (literalString)
// ---------------------------------------------------------------------------

// TestEmbeddedSQLInPascalFile verifies embedded SQL extraction for Pascal .pas files.
//
// Pascal uses single-quoted string literals (`'...'`) parsed as literalString nodes
// (Shape-2 — inline content, no separate content child). Delimiter-stripping peels
// the surrounding quotes before passing to the SQL gate.
//
// Fixture (1-based lines):
//
//	1: program PascalDB;
//	2: var
//	3:   DDL: string = 'CREATE TABLE records (id INTEGER PRIMARY KEY, data TEXT)';
//	4: begin
//	5: end.
//
// WHY no DML function node: Pascal's top-level begin/end is not a function
// boundary the tree-sitter extractor wraps as a NodeKindFunction. To keep the
// test honest (no vacuous ownership claims), the DML ref assertion uses any ref
// from the file regardless of owning node.
//
// Asserts:
//   - table node "records" with StartLine == 3
//   - at least one unresolved ref from records.pas to "records" (DML in begin block)
//   - GetEdgesByProvenance("embedded") ≥1
func TestEmbeddedSQLInPascalFile(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	database := openTestDB(t)

	dir := t.TempDir()
	initGitRepo(t, dir)

	const pascalFixture = `program PascalDB;
var
  DDL: string = 'CREATE TABLE records (id INTEGER PRIMARY KEY, data TEXT)';
  Q: string = 'SELECT id, data FROM records WHERE id = 1';
begin
end.
`
	writeFile(t, dir, "records.pas", pascalFixture)
	gitAdd(t, dir, ".")
	gitCommit(t, dir, "init")

	orch := indexer.NewOrchestrator(database, pool)
	if err := orch.IndexAll(ctx, dir); err != nil {
		t.Fatalf("IndexAll: %v", err)
	}

	pasNodes, err := database.GetNodesInFile(ctx, "records.pas")
	if err != nil {
		t.Fatalf("GetNodesInFile(records.pas): %v", err)
	}

	// --- Criterion 1: DDL → table node "records" at line 3 ---
	var recordsNode *types.Node
	for i := range pasNodes {
		if pasNodes[i].Kind == types.NodeKindTable && pasNodes[i].Name == "records" {
			recordsNode = &pasNodes[i]
			break
		}
	}
	if recordsNode == nil {
		t.Fatalf("FAIL: no table node 'records' in records.pas — Pascal Shape-2 harvester not wired (CP3)")
	}
	if recordsNode.StartLine != 3 {
		t.Errorf("records table StartLine=%d, want 3 (file-absolute)", recordsNode.StartLine)
	}

	// --- Criterion 2: DML → unresolved ref to "records" ---
	allRefs, err := database.GetUnresolvedRefs(ctx, 0, 0)
	if err != nil {
		t.Fatalf("GetUnresolvedRefs: %v", err)
	}

	var dmlRef *types.UnresolvedReference
	for i := range allRefs {
		if allRefs[i].FilePath == "records.pas" && allRefs[i].ReferenceName == "records" {
			dmlRef = &allRefs[i]
			break
		}
	}
	if dmlRef == nil {
		t.Errorf("FAIL: no unresolved ref for 'records' from records.pas — Pascal DML not extracted (CP3)")
	}
	if dmlRef != nil && dmlRef.Language != types.LanguageSQL {
		t.Errorf("records ref Language=%q, want %q", dmlRef.Language, types.LanguageSQL)
	}

	// --- Criterion 3: GetEdgesByProvenance("embedded") ≥1 ---
	embeddedEdges, err := database.GetEdgesByProvenance(ctx, "embedded")
	if err != nil {
		t.Fatalf("GetEdgesByProvenance(embedded): %v", err)
	}
	if len(embeddedEdges) == 0 {
		t.Fatalf("FAIL: GetEdgesByProvenance(embedded) returned 0 edges for Pascal fixture (CP3)")
	}
}

// ---------------------------------------------------------------------------
// TestEmbeddedSQLInJavaScriptFile — JavaScript, template literal + interpolation
// ---------------------------------------------------------------------------

// TestEmbeddedSQLInJavaScriptFile verifies embedded SQL extraction for JS .js files.
//
// Uses template literals “ `...` “ — the idiomatic multi-line form for JS.
// Also covers the interpolation criterion: “ `SELECT a FROM ${t}` “ → zero refs for "t".
//
// Fixture (1-based lines):
//
//	1: const DDL = `CREATE TABLE tasks (id INTEGER PRIMARY KEY, title TEXT NOT NULL)`;
//	2:
//	3: function loadTask(id, t) {
//	4:     const q1 = `SELECT id, title FROM tasks WHERE id = ${id}`;
//	5:     const q2 = `SELECT id, title FROM ${t} WHERE id = 1`;
//	6: }
//
// Asserts:
//   - table node "tasks" with StartLine == 1
//   - ref to "tasks" from loadTask (q1 — literal table + interp value)
//   - zero refs named "t" (q2 — interpolated table target)
//   - GetEdgesByProvenance("embedded") ≥1
func TestEmbeddedSQLInJavaScriptFile(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	database := openTestDB(t)

	dir := t.TempDir()
	initGitRepo(t, dir)

	const jsFixture = "const DDL = `CREATE TABLE tasks (id INTEGER PRIMARY KEY, title TEXT NOT NULL)`;\n\nfunction loadTask(id, t) {\n    const q1 = `SELECT id, title FROM tasks WHERE id = ${id}`;\n    const q2 = `SELECT id, title FROM ${t} WHERE id = 1`;\n}\n"

	writeFile(t, dir, "task.js", jsFixture)
	gitAdd(t, dir, ".")
	gitCommit(t, dir, "init")

	orch := indexer.NewOrchestrator(database, pool)
	if err := orch.IndexAll(ctx, dir); err != nil {
		t.Fatalf("IndexAll: %v", err)
	}

	jsNodes, err := database.GetNodesInFile(ctx, "task.js")
	if err != nil {
		t.Fatalf("GetNodesInFile(task.js): %v", err)
	}

	// --- Criterion 1: DDL → table node "tasks" at line 1 ---
	var tasksNode *types.Node
	for i := range jsNodes {
		if jsNodes[i].Kind == types.NodeKindTable && jsNodes[i].Name == "tasks" {
			tasksNode = &jsNodes[i]
			break
		}
	}
	if tasksNode == nil {
		t.Fatalf("FAIL: no table node 'tasks' in task.js — JavaScript template_string harvester not wired (CP3)")
	}
	if tasksNode.StartLine != 1 {
		t.Errorf("tasks table StartLine=%d, want 1 (file-absolute)", tasksNode.StartLine)
	}

	// --- Criterion 2: DML → ref to "tasks" from loadTask (owned by loadTask) ---
	allRefs, err := database.GetUnresolvedRefs(ctx, 0, 0)
	if err != nil {
		t.Fatalf("GetUnresolvedRefs: %v", err)
	}

	var loadTaskNode *types.Node
	for i := range jsNodes {
		if jsNodes[i].Kind == types.NodeKindFunction && jsNodes[i].Name == "loadTask" {
			loadTaskNode = &jsNodes[i]
			break
		}
	}
	if loadTaskNode == nil {
		t.Fatal("FAIL: loadTask function node not found in task.js — needed for ownership assertion")
	}

	var tasksRef *types.UnresolvedReference
	for i := range allRefs {
		if allRefs[i].FilePath == "task.js" &&
			allRefs[i].ReferenceName == "tasks" &&
			allRefs[i].FromNodeID == loadTaskNode.ID {
			tasksRef = &allRefs[i]
			break
		}
	}
	if tasksRef == nil {
		t.Errorf("FAIL: no unresolved ref for 'tasks' from task.js owned by loadTask — JavaScript DML not extracted (CP3)")
	}
	if tasksRef != nil && tasksRef.Language != types.LanguageSQL {
		t.Errorf("tasks ref Language=%q, want %q", tasksRef.Language, types.LanguageSQL)
	}

	// --- Criterion 3: interpolated table target → zero refs for "t" ---
	// q2 = `SELECT id, title FROM ${t} WHERE id = 1` → ${t} is template_substitution;
	// after substitution FROM ? → no ref named "t".
	var tRefs []types.UnresolvedReference
	for _, ref := range allRefs {
		if ref.FilePath == "task.js" && ref.ReferenceName == "t" {
			tRefs = append(tRefs, ref)
		}
	}
	if len(tRefs) != 0 {
		t.Errorf("FAIL: interpolated table target '${t}' must yield 0 refs, got %d — JS interpolation not substituted (CP3)", len(tRefs))
	}

	// --- Criterion 4: GetEdgesByProvenance("embedded") ≥1 ---
	embeddedEdges, err := database.GetEdgesByProvenance(ctx, "embedded")
	if err != nil {
		t.Fatalf("GetEdgesByProvenance(embedded): %v", err)
	}
	if len(embeddedEdges) == 0 {
		t.Fatalf("FAIL: GetEdgesByProvenance(embedded) returned 0 edges for JavaScript fixture (CP3)")
	}
}

// ---------------------------------------------------------------------------
// TestEmbeddedSQLProseRejectionInRustFile — prose strings rejected (gate check)
// ---------------------------------------------------------------------------

// TestEmbeddedSQLProseRejectionInRustFile proves that the IsSQLLiteral gate
// still rejects non-SQL prose strings in a new-language (Rust) host file.
//
// Fixture contains only prose strings:
//   - "choose an item from the dropdown"
//   - "Copied from the original repo"
//
// Neither matches the SQL gate. The test asserts that the embedded SQL
// post-pass produces zero table nodes and zero embedded edges from this file.
//
// Fixture (1-based lines):
//
//	1: fn ui_copy() {
//	2:     let msg1 = "choose an item from the dropdown";
//	3:     let msg2 = "Copied from the original repo";
//	4: }
func TestEmbeddedSQLProseRejectionInRustFile(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	database := openTestDB(t)

	dir := t.TempDir()
	initGitRepo(t, dir)

	const rustProseFixture = `fn ui_copy() {
    let msg1 = "choose an item from the dropdown";
    let msg2 = "Copied from the original repo";
}
`
	writeFile(t, dir, "prose.rs", rustProseFixture)
	gitAdd(t, dir, ".")
	gitCommit(t, dir, "init")

	orch := indexer.NewOrchestrator(database, pool)
	if err := orch.IndexAll(ctx, dir); err != nil {
		t.Fatalf("IndexAll: %v", err)
	}

	rsNodes, err := database.GetNodesInFile(ctx, "prose.rs")
	if err != nil {
		t.Fatalf("GetNodesInFile(prose.rs): %v", err)
	}

	// --- Gate check: zero table nodes from prose strings ---
	var tableNodes []types.Node
	for _, n := range rsNodes {
		if n.Kind == types.NodeKindTable {
			tableNodes = append(tableNodes, n)
		}
	}
	// WHY len==0: neither prose string passes IsSQLLiteral — the gate requires
	// SQL keyword patterns. Any non-zero count means the gate is broken for
	// the Rust language, or a false-positive SQL pattern is in the fixture.
	if len(tableNodes) != 0 {
		t.Errorf("FAIL: prose-only Rust file must yield 0 table nodes, got %d: %+v — IsSQLLiteral gate regression (CP3)", len(tableNodes), tableNodes)
	}

	// --- Gate check: zero embedded edges from prose strings ---
	allEdges, err := database.GetEdgesByProvenance(ctx, "embedded")
	if err != nil {
		t.Fatalf("GetEdgesByProvenance(embedded): %v", err)
	}

	// Each test uses openTestDB(t) — a fresh temp DB. prose.rs is the only file
	// in this index, so allEdges == the embedded edges from this file.
	// WHY len==0: neither prose string passes IsSQLLiteral, so no DDL edges are
	// emitted with Provenance:"embedded". Any non-zero count means the gate is broken.
	if len(allEdges) != 0 {
		t.Errorf("FAIL: prose-only Rust file must yield 0 embedded edges, got %d — IsSQLLiteral gate regression (CP3)", len(allEdges))
	}
}
