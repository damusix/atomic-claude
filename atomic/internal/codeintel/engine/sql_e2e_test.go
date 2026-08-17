package engine_test

// End-to-end SQL tests, exercising extractor → orchestrator → DB → query.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/engine"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// sqlEdgeClassFixture exercises one instance of every SQL edge class: inline FK,
// view FROM, trigger ON plus EXECUTE FUNCTION, synonym target, and policy ON
// plus a function call in USING.
const sqlEdgeClassFixture = `
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

// TestSQLEdgesEndToEnd asserts every edge class survives index and resolve.
func TestSQLEdgesEndToEnd(t *testing.T) {
	root := t.TempDir()
	sqlPath := filepath.Join(root, "schema4.sql")
	if err := os.WriteFile(sqlPath, []byte(sqlEdgeClassFixture), 0o644); err != nil {
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

	// Targets are matched by name, so each candidate edge needs a node lookup.
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
				return // edge found
			}
		}
		t.Errorf("missing edge %s -[%s]-> %q (from=%s)\n  outgoing edges: %v",
			fromNode.Name, edgeKind, targetName, fromNode.ID, summarizeEdges(edges))
	}

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
	// CREATE SYNONYM has no node kind of its own: it becomes a type_alias
	// carrying {"synonym":true} metadata.
	synNode, ok := findNode(types.NodeKindTypeAlias, "orders_alias")
	if !ok {
		t.Fatal("synonym 'orders_alias' not found in DB")
	}
	policyNode, ok := findNode(types.NodeKindPolicy, "row_policy")
	if !ok {
		t.Fatal("policy 'row_policy' not found in DB")
	}

	assertEdge(ordersNode, types.EdgeKindReferences, "customers")

	assertEdge(viewNode, types.EdgeKindReferences, "orders")

	assertEdge(triggerNode, types.EdgeKindReferences, "orders")

	assertEdge(triggerNode, types.EdgeKindCalls, "audit_fn")

	assertEdge(synNode, types.EdgeKindReferences, "orders")

	assertEdge(policyNode, types.EdgeKindReferences, "orders")

	assertEdge(policyNode, types.EdgeKindCalls, "current_user_fn")
}

func summarizeEdges(edges []types.Edge) []string {
	out := make([]string, len(edges))
	for i, e := range edges {
		out[i] = string(e.Kind) + "→" + e.Target
	}
	return out
}

// sqlWritesVsReadsFixture is one procedure that INSERTs, UPDATEs, SELECTs, and
// EXECs, so the four operations must resolve to writes, writes, references, and
// calls respectively rather than collapsing into one kind.
const sqlWritesVsReadsFixture = `
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

// TestSQLWritesVsReadsEndToEnd asserts writes and references stay distinct, and
// that a written table's incoming edges surface its writer.
func TestSQLWritesVsReadsEndToEnd(t *testing.T) {
	root := t.TempDir()
	sqlPath := filepath.Join(root, "schema5.sql")
	if err := os.WriteFile(sqlPath, []byte(sqlWritesVsReadsFixture), 0o644); err != nil {
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

	procNode, ok := findNode(types.NodeKindProcedure, "proc_archive")
	if !ok {
		t.Fatal("procedure 'proc_archive' not found")
	}
	archiveNode, ok := findNode(types.NodeKindTable, "archive_orders")
	if !ok {
		t.Fatal("table 'archive_orders' not found")
	}

	assertEdge(procNode, types.EdgeKindWrites, "archive_orders")
	assertEdge(procNode, types.EdgeKindWrites, "orders")
	assertEdge(procNode, types.EdgeKindReferences, "customers")
	assertEdge(procNode, types.EdgeKindCalls, "audit_proc")

	// Distinction: customers is a read target, NOT a write target.
	assertNoEdge(procNode, types.EdgeKindWrites, "customers")

	// This is what backs the "who writes this table?" impact query.
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

// sqlPgDumpFKFixture covers the schema-qualified ALTER TABLE ONLY … FOREIGN KEY
// form pg_dump emits for every FK. Without ONLY in the table pattern, the table
// name capture swallows the literal "ONLY" and the FK is silently dropped.
const sqlPgDumpFKFixture = `
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

// TestSQLCP6AlterTableFKResolution asserts the pg_dump FK form still resolves to
// references edges between the two tables.
func TestSQLCP6AlterTableFKResolution(t *testing.T) {
	root := t.TempDir()
	sqlPath := filepath.Join(root, "schema.sql")
	if err := os.WriteFile(sqlPath, []byte(sqlPgDumpFKFixture), 0o644); err != nil {
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

	assertEdge(ordersNode, types.EdgeKindReferences, "customers")

	assertEdge(ordersNode, types.EdgeKindReferences, "employees")
}

const sqlE2EFixture = `
-- Multi-dialect SQL fixture for end-to-end test
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
	root := t.TempDir()
	sqlPath := filepath.Join(root, "schema.sql")
	if err := os.WriteFile(sqlPath, []byte(sqlE2EFixture), 0o644); err != nil {
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
		// constraint node — named CONSTRAINT in the Products T-SQL table body.
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
