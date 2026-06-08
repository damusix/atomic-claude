package engine_test

// End-to-end test: index a multi-dialect SQL file and verify SQL nodes land in DB.
// This is the CP2 + CP3 verification gate — it proves the full pipeline:
//   extractor → orchestrator → DB → query.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/engine"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// ---------------------------------------------------------------------------
// CP4 e2e fixture: cross-object reference edges
// ---------------------------------------------------------------------------

// sqlCP4Fixture defines a schema exercising every CP4 edge class:
//   - FK inline REFERENCES (orders → customers)
//   - view FROM (active_orders → orders)
//   - trigger ON table (trg_orders → orders) + EXECUTE FUNCTION (trg_orders → audit_fn)
//   - synonym → target (orders_alias → orders)
//   - policy ON table (row_policy → orders) + fn call in USING (row_policy → current_user_fn)
const sqlCP4Fixture = `
CREATE TABLE customers (
    customer_id SERIAL PRIMARY KEY,
    email       VARCHAR(255)
);

CREATE TABLE orders (
    order_id    BIGSERIAL PRIMARY KEY,
    customer_id INT NOT NULL REFERENCES customers(customer_id)
);

CREATE OR REPLACE VIEW active_orders AS
SELECT * FROM orders WHERE order_id > 0;

CREATE OR REPLACE FUNCTION audit_fn() RETURNS TRIGGER AS $$
BEGIN RETURN NEW; END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION current_user_fn() RETURNS INT AS $$
BEGIN RETURN 1; END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_orders
AFTER INSERT ON orders
FOR EACH ROW EXECUTE FUNCTION audit_fn();

CREATE SYNONYM orders_alias FOR orders;

CREATE POLICY row_policy ON orders
USING (current_user_fn() = customer_id);
`

// TestSQLEdgesEndToEnd is the CP4 gate: it indexes sqlCP4Fixture, resolves
// all references, then asserts each expected edge is present in the DB.
func TestSQLEdgesEndToEnd(t *testing.T) {
	root := t.TempDir()
	sqlPath := filepath.Join(root, "cp4.sql")
	if err := os.WriteFile(sqlPath, []byte(sqlCP4Fixture), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	idxDir := filepath.Join(root, ".claude", ".atomic-index")
	if err := os.MkdirAll(idxDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	eng, err := engine.New(root)
	if err != nil {
		t.Fatalf("engine.New: %v", err)
	}
	defer eng.Close()

	ctx := context.Background()
	if err := eng.Init(ctx); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := eng.IndexAll(ctx); err != nil {
		t.Fatalf("IndexAll: %v", err)
	}
	if err := eng.ResolveReferences(ctx); err != nil {
		t.Fatalf("ResolveReferences: %v", err)
	}

	// Helper: find a node by kind+name (case-insensitive).
	findNode := func(kind types.NodeKind, name string) (types.Node, bool) {
		nodes, err := eng.GetNodesByKind(ctx, kind)
		if err != nil {
			t.Fatalf("GetNodesByKind(%s): %v", kind, err)
		}
		for _, n := range nodes {
			if strings.EqualFold(n.Name, name) {
				return n, true
			}
		}
		return types.Node{}, false
	}

	// Helper: assert that a specific edge (kind) from fromNode to a node
	// with targetName exists among outgoing edges.
	assertEdge := func(fromNode types.Node, edgeKind types.EdgeKind, targetName string) {
		t.Helper()
		edges, err := eng.GetOutgoingEdges(ctx, fromNode.ID)
		if err != nil {
			t.Errorf("GetOutgoingEdges(%s): %v", fromNode.ID, err)
			return
		}
		// We need to find the target node to get its ID.
		// Look through edge targets for one whose To node matches targetName.
		// GetOutgoingEdges returns []types.Edge{From, To, Kind}.
		for _, e := range edges {
			if e.Kind != edgeKind {
				continue
			}
			// Resolve target node by ID to check its name.
			tgt, err := eng.GetNode(ctx, e.Target)
			if err != nil {
				continue
			}
			if strings.EqualFold(tgt.Name, targetName) {
				return // edge found
			}
		}
		t.Errorf("missing edge %s -[%s]-> %q (from=%s)\n  outgoing edges: %v",
			fromNode.Name, edgeKind, targetName, fromNode.ID, summarizeEdges(edges))
	}

	// Verify source nodes exist.
	ordersNode, ok := findNode(types.NodeKindTable, "orders")
	if !ok {
		t.Fatal("table 'orders' not found in DB")
	}
	viewNode, ok := findNode(types.NodeKindView, "active_orders")
	if !ok {
		t.Fatal("view 'active_orders' not found in DB")
	}
	triggerNode, ok := findNode(types.NodeKindTrigger, "trg_orders")
	if !ok {
		t.Fatal("trigger 'trg_orders' not found in DB")
	}
	// Synonyms are stored as NodeKindTypeAlias per the spec mapping:
	// CREATE SYNONYM → type_alias node with {"synonym":true} metadata.
	synNode, ok := findNode(types.NodeKindTypeAlias, "orders_alias")
	if !ok {
		t.Fatal("synonym 'orders_alias' not found in DB")
	}
	policyNode, ok := findNode(types.NodeKindPolicy, "row_policy")
	if !ok {
		t.Fatal("policy 'row_policy' not found in DB")
	}

	// Assert CP4 edges.
	// FK: orders -[references]-> customers (inline REFERENCES in CREATE TABLE)
	assertEdge(ordersNode, types.EdgeKindReferences, "customers")

	// View: active_orders -[references]-> orders (FROM in view body)
	assertEdge(viewNode, types.EdgeKindReferences, "orders")

	// Trigger: trg_orders -[references]-> orders (ON clause)
	assertEdge(triggerNode, types.EdgeKindReferences, "orders")

	// Trigger: trg_orders -[calls]-> audit_fn (EXECUTE FUNCTION)
	assertEdge(triggerNode, types.EdgeKindCalls, "audit_fn")

	// Synonym: orders_alias -[references]-> orders (FOR target)
	assertEdge(synNode, types.EdgeKindReferences, "orders")

	// Policy: row_policy -[references]-> orders (ON table)
	assertEdge(policyNode, types.EdgeKindReferences, "orders")

	// Policy: row_policy -[calls]-> current_user_fn (USING expression)
	assertEdge(policyNode, types.EdgeKindCalls, "current_user_fn")
}

// summarizeEdges returns a compact description of edges for test error output.
func summarizeEdges(edges []types.Edge) []string {
	out := make([]string, len(edges))
	for i, e := range edges {
		out[i] = string(e.Kind) + "→" + e.Target
	}
	return out
}

// ---------------------------------------------------------------------------
// CP5 e2e fixture: writes-vs-reads distinction
// ---------------------------------------------------------------------------

// sqlCP5Fixture defines a procedure that:
//   - INSERTs into archive_orders (writes)
//   - UPDATEs orders (writes)
//   - SELECTs FROM customers (references / read)
//   - EXECs another procedure audit_proc (calls)
//
// After resolve, the engine must show:
//   - proc_archive -[writes]-> archive_orders
//   - proc_archive -[writes]-> orders
//   - proc_archive -[references]-> customers
//   - proc_archive -[calls]-> audit_proc
//
// And GetIncomingEdges on archive_orders must include the writes edge,
// distinguishable from any references edge.
const sqlCP5Fixture = `
CREATE TABLE orders (
    order_id    SERIAL PRIMARY KEY,
    status      TEXT,
    customer_id INT
);

CREATE TABLE archive_orders (
    order_id    INT,
    status      TEXT,
    archived_at TIMESTAMP
);

CREATE TABLE customers (
    customer_id SERIAL PRIMARY KEY,
    email       TEXT
);

CREATE OR REPLACE PROCEDURE audit_proc(msg TEXT)
LANGUAGE plpgsql AS $$
BEGIN
  -- no-op audit
END;
$$;

CREATE OR REPLACE PROCEDURE proc_archive()
LANGUAGE plpgsql AS $$
BEGIN
  INSERT INTO archive_orders
    SELECT order_id, status, NOW() FROM orders WHERE status = 'closed';
  UPDATE orders SET status = 'archived' WHERE status = 'closed';
  SELECT customer_id FROM customers WHERE customer_id > 0;
  CALL audit_proc('archived');
END;
$$;
`

// TestSQLWritesVsReadsEndToEnd is the CP5 gate: it indexes sqlCP5Fixture,
// resolves all references, then asserts writes and references are DISTINCT
// resolved edges and GetIncomingEdges on a written table surfaces the writer.
func TestSQLWritesVsReadsEndToEnd(t *testing.T) {
	root := t.TempDir()
	sqlPath := filepath.Join(root, "cp5.sql")
	if err := os.WriteFile(sqlPath, []byte(sqlCP5Fixture), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	idxDir := filepath.Join(root, ".claude", ".atomic-index")
	if err := os.MkdirAll(idxDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	eng, err := engine.New(root)
	if err != nil {
		t.Fatalf("engine.New: %v", err)
	}
	defer eng.Close()

	ctx := context.Background()
	if err := eng.Init(ctx); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := eng.IndexAll(ctx); err != nil {
		t.Fatalf("IndexAll: %v", err)
	}
	if err := eng.ResolveReferences(ctx); err != nil {
		t.Fatalf("ResolveReferences: %v", err)
	}

	findNode := func(kind types.NodeKind, name string) (types.Node, bool) {
		nodes, err := eng.GetNodesByKind(ctx, kind)
		if err != nil {
			t.Fatalf("GetNodesByKind(%s): %v", kind, err)
		}
		for _, n := range nodes {
			if strings.EqualFold(n.Name, name) {
				return n, true
			}
		}
		return types.Node{}, false
	}

	// assertEdge checks that an edge of edgeKind from fromNode to targetName exists.
	assertEdge := func(fromNode types.Node, edgeKind types.EdgeKind, targetName string) {
		t.Helper()
		edges, err := eng.GetOutgoingEdges(ctx, fromNode.ID)
		if err != nil {
			t.Errorf("GetOutgoingEdges(%s): %v", fromNode.ID, err)
			return
		}
		for _, e := range edges {
			if e.Kind != edgeKind {
				continue
			}
			tgt, err := eng.GetNode(ctx, e.Target)
			if err != nil {
				continue
			}
			if strings.EqualFold(tgt.Name, targetName) {
				return // found
			}
		}
		t.Errorf("missing edge %s -[%s]-> %q\n  outgoing: %v",
			fromNode.Name, edgeKind, targetName, summarizeEdges(edges))
	}

	// assertNoEdge checks that no edge of edgeKind to targetName exists from fromNode.
	assertNoEdge := func(fromNode types.Node, edgeKind types.EdgeKind, targetName string) {
		t.Helper()
		edges, err := eng.GetOutgoingEdges(ctx, fromNode.ID)
		if err != nil {
			return // can't check
		}
		for _, e := range edges {
			if e.Kind != edgeKind {
				continue
			}
			tgt, err := eng.GetNode(ctx, e.Target)
			if err != nil {
				continue
			}
			if strings.EqualFold(tgt.Name, targetName) {
				t.Errorf("unexpected edge %s -[%s]-> %q (should not exist)",
					fromNode.Name, edgeKind, targetName)
				return
			}
		}
	}

	// Locate nodes.
	procNode, ok := findNode(types.NodeKindProcedure, "proc_archive")
	if !ok {
		t.Fatal("procedure 'proc_archive' not found")
	}
	archiveNode, ok := findNode(types.NodeKindTable, "archive_orders")
	if !ok {
		t.Fatal("table 'archive_orders' not found")
	}

	// proc_archive -[writes]-> archive_orders (INSERT INTO)
	assertEdge(procNode, types.EdgeKindWrites, "archive_orders")
	// proc_archive -[writes]-> orders (UPDATE)
	assertEdge(procNode, types.EdgeKindWrites, "orders")
	// proc_archive -[references]-> customers (SELECT FROM / read)
	assertEdge(procNode, types.EdgeKindReferences, "customers")
	// proc_archive -[calls]-> audit_proc (CALL)
	assertEdge(procNode, types.EdgeKindCalls, "audit_proc")

	// Distinction: customers is a read target, NOT a write target.
	assertNoEdge(procNode, types.EdgeKindWrites, "customers")

	// Incoming edges on archive_orders must include the writes edge from proc_archive.
	// This is the "code impact <table>" query: who writes this table?
	incomingEdges, err := eng.GetIncomingEdges(ctx, archiveNode.ID)
	if err != nil {
		t.Fatalf("GetIncomingEdges(archive_orders): %v", err)
	}
	foundWritesIncoming := false
	for _, e := range incomingEdges {
		if e.Kind == types.EdgeKindWrites {
			src, err := eng.GetNode(ctx, e.Source)
			if err != nil {
				continue
			}
			if strings.EqualFold(src.Name, "proc_archive") {
				foundWritesIncoming = true
				break
			}
		}
	}
	if !foundWritesIncoming {
		t.Errorf("GetIncomingEdges(archive_orders) did not return a writes edge from proc_archive\n  incoming: %v",
			summarizeEdges(incomingEdges))
	}
}

// ---------------------------------------------------------------------------
// CP6 regression: ALTER TABLE ONLY … FOREIGN KEY … REFERENCES schema.target
// ---------------------------------------------------------------------------

// sqlCP6Fixture exercises the real-repo FK shape that CP4's inline fixture did
// NOT cover: schema-qualified ALTER TABLE ONLY … ADD CONSTRAINT … FOREIGN KEY
// … REFERENCES schema.target.  This is the exact pattern emitted by pg_dump
// for every Northwind / Chinook / pagila FK.
//
// Root cause (fixed by CP6 r1): the original alterFKRefRE used modPat which
// did NOT include (?:ONLY\s+)?. pg_dump emits "ALTER TABLE ONLY <table>",
// causing the capture group for the table name to capture the literal "ONLY",
// making findNodeID return "" and silently dropping the FK reference.
// Fix: alterTablePat = (?:ONLY\s+)? + modPat; alterFKRefRE uses alterTablePat.
const sqlCP6Fixture = `
CREATE TABLE public.orders (
    order_id smallint NOT NULL
);

CREATE TABLE public.customers (
    customer_id character varying(5) NOT NULL
);

CREATE TABLE public.employees (
    employee_id smallint NOT NULL
);

ALTER TABLE ONLY public.orders
    ADD CONSTRAINT fk_orders_customers FOREIGN KEY (customer_id) REFERENCES public.customers;

ALTER TABLE ONLY public.orders
    ADD CONSTRAINT fk_orders_employees FOREIGN KEY (employee_id) REFERENCES public.employees;
`

// TestSQLCP6AlterTableFKResolution is the CP6 regression gate.
// It verifies that schema-qualified ALTER TABLE ONLY … FOREIGN KEY … REFERENCES
// schema.target produces resolved "references" edges between the tables.
//
// Fails on base SHA (34f3a9b) where alterFKRefRE used modPat (no ONLY support).
// Passes after alterTablePat = (?:ONLY\s+)? + modPat.
func TestSQLCP6AlterTableFKResolution(t *testing.T) {
	root := t.TempDir()
	sqlPath := filepath.Join(root, "schema.sql")
	if err := os.WriteFile(sqlPath, []byte(sqlCP6Fixture), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	idxDir := filepath.Join(root, ".claude", ".atomic-index")
	if err := os.MkdirAll(idxDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	eng, err := engine.New(root)
	if err != nil {
		t.Fatalf("engine.New: %v", err)
	}
	defer eng.Close()

	ctx := context.Background()
	if err := eng.Init(ctx); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := eng.IndexAll(ctx); err != nil {
		t.Fatalf("IndexAll: %v", err)
	}
	if err := eng.ResolveReferences(ctx); err != nil {
		t.Fatalf("ResolveReferences: %v", err)
	}

	findNode := func(kind types.NodeKind, name string) (types.Node, bool) {
		nodes, err := eng.GetNodesByKind(ctx, kind)
		if err != nil {
			t.Fatalf("GetNodesByKind(%s): %v", kind, err)
		}
		for _, n := range nodes {
			if strings.EqualFold(n.Name, name) {
				return n, true
			}
		}
		return types.Node{}, false
	}

	assertEdge := func(fromNode types.Node, edgeKind types.EdgeKind, targetName string) {
		t.Helper()
		edges, err := eng.GetOutgoingEdges(ctx, fromNode.ID)
		if err != nil {
			t.Errorf("GetOutgoingEdges(%s): %v", fromNode.ID, err)
			return
		}
		for _, e := range edges {
			if e.Kind != edgeKind {
				continue
			}
			tgt, err := eng.GetNode(ctx, e.Target)
			if err != nil {
				continue
			}
			if strings.EqualFold(tgt.Name, targetName) {
				return // found
			}
		}
		t.Errorf("missing edge %s -[%s]-> %q\n  outgoing: %v",
			fromNode.Name, edgeKind, targetName, summarizeEdges(edges))
	}

	ordersNode, ok := findNode(types.NodeKindTable, "orders")
	if !ok {
		t.Fatal("table 'orders' not found in DB")
	}

	// FK via schema-qualified ALTER TABLE ONLY … REFERENCES public.customers
	assertEdge(ordersNode, types.EdgeKindReferences, "customers")

	// FK via schema-qualified ALTER TABLE ONLY … REFERENCES public.employees
	assertEdge(ordersNode, types.EdgeKindReferences, "employees")
}

const sqlE2EFixture = `
-- Multi-dialect SQL fixture for CP2 end-to-end test
CREATE SCHEMA corp;

CREATE TABLE corp.customers (
    customer_id SERIAL,
    email       VARCHAR(255),
    active      BOOLEAN DEFAULT TRUE
);

CREATE TABLE corp.orders (
    order_id   BIGSERIAL,
    customer_id INT NOT NULL,
    total      NUMERIC(12,2)
);

ALTER TABLE corp.orders ADD COLUMN status VARCHAR(20);

CREATE OR REPLACE VIEW corp.active_customers AS
SELECT * FROM corp.customers WHERE active = true;

CREATE SEQUENCE corp.order_seq;

CREATE TYPE corp.order_status AS ENUM ('new', 'shipped', 'returned');

CREATE OR REPLACE FUNCTION corp.get_customer(p_id INT) RETURNS corp.customers AS $$
BEGIN RETURN NULL; END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE PROCEDURE corp.close_order(p_id INT)
LANGUAGE plpgsql AS $$
BEGIN UPDATE corp.orders SET status='closed' WHERE order_id=p_id; END;
$$;

CREATE TRIGGER trg_order_log
AFTER INSERT ON corp.orders
FOR EACH ROW EXECUTE FUNCTION log_fn();

CREATE UNIQUE INDEX idx_customer_email ON corp.customers (email);

CREATE TABLE [dbo].[Products] (
    [ProductId]  INT IDENTITY(1,1),
    [Name]       NVARCHAR(200),
    [Price]      AS ([BasePrice] * 1.1),
    CONSTRAINT [PK_Products] PRIMARY KEY ([ProductId])
);

CREATE TYPE [dbo].[PriceType] FROM DECIMAL(19,4) NOT NULL;

CREATE SYNONYM [dbo].[Prod] FOR [dbo].[Products];

CREATE DATABASE SalesDB;
`

func TestSQLEndToEnd(t *testing.T) {
	// Set up an isolated project root with the SQL fixture.
	root := t.TempDir()
	sqlPath := filepath.Join(root, "schema.sql")
	if err := os.WriteFile(sqlPath, []byte(sqlE2EFixture), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	// engine.New looks for .claude/.atomic-index/ under root.
	idxDir := filepath.Join(root, ".claude", ".atomic-index")
	if err := os.MkdirAll(idxDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	eng, err := engine.New(root)
	if err != nil {
		t.Fatalf("engine.New: %v", err)
	}
	defer eng.Close()

	ctx := context.Background()
	if err := eng.Init(ctx); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := eng.IndexAll(ctx); err != nil {
		t.Fatalf("IndexAll: %v", err)
	}

	// Verify core SQL node kinds are present.
	checks := []struct {
		kind     types.NodeKind
		namePart string
	}{
		{types.NodeKindNamespace, "corp"},
		{types.NodeKindTable, "customers"},
		{types.NodeKindTable, "orders"},
		{types.NodeKindColumn, "email"},
		{types.NodeKindColumn, "status"}, // from ALTER TABLE ADD COLUMN
		{types.NodeKindView, "active_customers"},
		{types.NodeKindSequence, "order_seq"},
		{types.NodeKindEnum, "order_status"},
		{types.NodeKindEnumMember, "new"},
		{types.NodeKindFunction, "get_customer"},
		{types.NodeKindProcedure, "close_order"},
		{types.NodeKindTrigger, "trg_order_log"},
		{types.NodeKindIndex, "idx_customer_email"},
		// T-SQL
		{types.NodeKindTable, "Products"},
		{types.NodeKindColumn, "Name"},
		{types.NodeKindColumn, "Price"},        // AS computed → generated metadata
		{types.NodeKindTypeAlias, "PriceType"}, // CREATE TYPE … FROM
		{types.NodeKindTypeAlias, "Prod"},      // SYNONYM
		{types.NodeKindModule, "SalesDB"},
		// CP3: constraint node — named CONSTRAINT in the Products T-SQL table body.
		{types.NodeKindConstraint, "PK_Products"},
	}

	for _, c := range checks {
		nodes, err := eng.GetNodesByKind(ctx, c.kind)
		if err != nil {
			t.Errorf("GetNodesByKind(%s): %v", c.kind, err)
			continue
		}
		found := false
		for _, n := range nodes {
			if strings.Contains(n.Name, c.namePart) {
				found = true
				// All SQL nodes must have Language=sql and IsExported=true.
				if n.Language != types.LanguageSQL {
					t.Errorf("node %s/%s has language %s, want sql", c.kind, n.Name, n.Language)
				}
				if !n.IsExported {
					t.Errorf("node %s/%s IsExported=false, want true", c.kind, n.Name)
				}
				break
			}
		}
		if !found {
			t.Errorf("no %s node with name containing %q found in DB", c.kind, c.namePart)
		}
	}
}
