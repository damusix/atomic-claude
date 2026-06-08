package standalone_test

// Tests for the SQL standalone extractor (CP2–CP5).
//
// Why these tests: the extractor must produce correct node kinds, names, lines,
// and contains edges across four SQL dialects (Postgres/ANSI, MySQL backticks,
// T-SQL brackets + GO + CREATE OR ALTER). Comment/string false-positive guard
// is load-bearing — the extractor strips -- and /* */ before matching so a
// CREATE TABLE inside a comment never produces a node.
//
// CP5 tests: routine/view body edges (reads/writes/calls), CTE-shadow guard,
// LATERAL/UNNEST keyword filter (F-6), policy fn-call scope to USING (F-7).

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction/standalone"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func newSQL() *standalone.SQLExtractor {
	return standalone.NewSQLExtractor()
}

func findSQLNode(nodes []types.Node, kind types.NodeKind, namePart string) *types.Node {
	for i := range nodes {
		if nodes[i].Kind == kind && strings.Contains(nodes[i].Name, namePart) {
			return &nodes[i]
		}
	}
	return nil
}

// findSQLNodeExact returns the first node of the given kind whose Name is an
// exact (case-insensitive) match for name. Used in tests that must not
// accidentally pass on a partial-name collision.
// WHY (F-5): findSQLNode uses strings.Contains, which silently passes when a
// longer name happens to contain the search term (e.g. "id" matches "old_id").
// Exact match is required for constraint/column identity assertions.
func findSQLNodeExact(nodes []types.Node, kind types.NodeKind, name string) *types.Node {
	lower := strings.ToLower(name)
	for i := range nodes {
		if nodes[i].Kind == kind && strings.ToLower(nodes[i].Name) == lower {
			return &nodes[i]
		}
	}
	return nil
}

func hasContainsEdge(edges []types.Edge, parentName, childName string, nodes []types.Node) bool {
	nodeByID := make(map[string]types.Node, len(nodes))
	for _, n := range nodes {
		nodeByID[n.ID] = n
	}
	for _, e := range edges {
		if e.Kind != types.EdgeKindContains {
			continue
		}
		src, srcOK := nodeByID[e.Source]
		dst, dstOK := nodeByID[e.Target]
		if srcOK && dstOK &&
			strings.Contains(src.Name, parentName) &&
			strings.Contains(dst.Name, childName) {
			return true
		}
	}
	return false
}

func metadataHas(raw json.RawMessage, key, val string) bool {
	if raw == nil {
		return false
	}
	var m map[string]interface{}
	if err := json.Unmarshal(raw, &m); err != nil {
		return false
	}
	v, ok := m[key]
	if !ok {
		return false
	}
	switch tv := v.(type) {
	case string:
		return tv == val
	case bool:
		return val == "true" && tv
	}
	return false
}

// ---------------------------------------------------------------------------
// Postgres DDL fixture
// ---------------------------------------------------------------------------

const pgFixture = `
-- Postgres DDL fixture
CREATE SCHEMA myapp;

CREATE TABLE myapp.users (
    id          SERIAL PRIMARY KEY,
    email       VARCHAR(255) NOT NULL,
    created_at  TIMESTAMP DEFAULT NOW(),
    full_name   TEXT GENERATED ALWAYS AS (first_name || ' ' || last_name) STORED,
    CONSTRAINT uq_users_email UNIQUE (email)
);

CREATE TABLE orders (
    id      BIGSERIAL,
    user_id INT NOT NULL
);

ALTER TABLE orders ADD COLUMN total NUMERIC(10,2);

CREATE FOREIGN TABLE ext_feed (
    feed_id INT,
    data    TEXT
) SERVER remote_srv;

CREATE OR REPLACE VIEW active_users AS
SELECT id, email FROM myapp.users WHERE active = true;

CREATE MATERIALIZED VIEW order_summary AS
SELECT user_id, COUNT(*) FROM orders GROUP BY user_id;

CREATE OR REPLACE FUNCTION get_user(p_id INT) RETURNS users AS $$
BEGIN
  RETURN NULL;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE PROCEDURE archive_orders(cutoff TIMESTAMP)
LANGUAGE plpgsql AS $$
BEGIN
  DELETE FROM orders WHERE created_at < cutoff;
END;
$$;

CREATE TRIGGER trg_audit
AFTER INSERT OR UPDATE ON myapp.users
FOR EACH ROW EXECUTE FUNCTION audit_fn();

CREATE UNIQUE INDEX idx_users_email ON myapp.users (email);

CREATE INDEX idx_orders_user ON orders (user_id);

CREATE SEQUENCE order_seq START 1000;

CREATE TYPE mood AS ENUM ('happy', 'sad', 'ok');

CREATE DOMAIN positive_int AS INTEGER CHECK (VALUE > 0);

CREATE DATABASE mydb;
`

func TestPostgresDefinitions(t *testing.T) {
	ext := newSQL()
	result, err := ext.Extract("/db/schema.sql", pgFixture)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	nodes := result.Nodes
	edges := result.Edges

	// Schema → namespace
	schemaNode := findSQLNode(nodes, types.NodeKindNamespace, "myapp")
	if schemaNode == nil {
		t.Error("expected namespace node 'myapp'")
	}

	// Tables
	usersNode := findSQLNode(nodes, types.NodeKindTable, "users")
	if usersNode == nil {
		t.Fatal("expected table node 'users'")
	}
	ordersNode := findSQLNode(nodes, types.NodeKindTable, "orders")
	if ordersNode == nil {
		t.Fatal("expected table node 'orders'")
	}

	// Columns inside users table
	emailCol := findSQLNode(nodes, types.NodeKindColumn, "email")
	if emailCol == nil {
		t.Error("expected column 'email'")
	}
	// GENERATED column
	fullNameCol := findSQLNode(nodes, types.NodeKindColumn, "full_name")
	if fullNameCol == nil {
		t.Error("expected column 'full_name'")
	} else if !metadataHas(fullNameCol.Metadata, "generated", "true") {
		t.Error("full_name column should have metadata {\"generated\":true}")
	}

	// Constraint line must NOT produce a column node
	constraintAsCol := findSQLNode(nodes, types.NodeKindColumn, "uq_users_email")
	if constraintAsCol != nil {
		t.Error("CONSTRAINT line must not produce a column node")
	}

	// ALTER TABLE ADD COLUMN
	totalCol := findSQLNode(nodes, types.NodeKindColumn, "total")
	if totalCol == nil {
		t.Error("expected column 'total' from ALTER TABLE ADD COLUMN")
	}
	if !hasContainsEdge(edges, "orders", "total", nodes) {
		t.Error("expected contains edge orders→total (from ALTER TABLE ADD COLUMN)")
	}

	// FOREIGN TABLE → table with metadata
	feedNode := findSQLNode(nodes, types.NodeKindTable, "ext_feed")
	if feedNode == nil {
		t.Error("expected table node 'ext_feed' from CREATE FOREIGN TABLE")
	} else if !metadataHas(feedNode.Metadata, "foreign", "true") {
		t.Error("ext_feed should have metadata {\"foreign\":true}")
	}

	// Views
	viewNode := findSQLNode(nodes, types.NodeKindView, "active_users")
	if viewNode == nil {
		t.Error("expected view node 'active_users'")
	}
	matView := findSQLNode(nodes, types.NodeKindView, "order_summary")
	if matView == nil {
		t.Error("expected view node 'order_summary' (materialized)")
	} else if !metadataHas(matView.Metadata, "materialized", "true") {
		t.Error("order_summary should have metadata {\"materialized\":true}")
	}

	// Function
	fnNode := findSQLNode(nodes, types.NodeKindFunction, "get_user")
	if fnNode == nil {
		t.Error("expected function node 'get_user'")
	}

	// Procedure
	procNode := findSQLNode(nodes, types.NodeKindProcedure, "archive_orders")
	if procNode == nil {
		t.Error("expected procedure node 'archive_orders'")
	}

	// Trigger
	trigNode := findSQLNode(nodes, types.NodeKindTrigger, "trg_audit")
	if trigNode == nil {
		t.Error("expected trigger node 'trg_audit'")
	}

	// Indexes
	idxNode := findSQLNode(nodes, types.NodeKindIndex, "idx_users_email")
	if idxNode == nil {
		t.Error("expected index node 'idx_users_email'")
	}
	if !hasContainsEdge(edges, "users", "idx_users_email", nodes) {
		t.Error("expected contains edge users→idx_users_email")
	}

	// Sequence
	seqNode := findSQLNode(nodes, types.NodeKindSequence, "order_seq")
	if seqNode == nil {
		t.Error("expected sequence node 'order_seq'")
	}

	// Enum
	enumNode := findSQLNode(nodes, types.NodeKindEnum, "mood")
	if enumNode == nil {
		t.Fatal("expected enum node 'mood'")
	}
	happyMember := findSQLNode(nodes, types.NodeKindEnumMember, "happy")
	if happyMember == nil {
		t.Error("expected enum_member 'happy'")
	}
	if !hasContainsEdge(edges, "mood", "happy", nodes) {
		t.Error("expected contains edge mood→happy")
	}

	// DOMAIN → type_alias
	domainNode := findSQLNode(nodes, types.NodeKindTypeAlias, "positive_int")
	if domainNode == nil {
		t.Error("expected type_alias node 'positive_int' from CREATE DOMAIN")
	}

	// Database → module
	dbNode := findSQLNode(nodes, types.NodeKindModule, "mydb")
	if dbNode == nil {
		t.Error("expected module node 'mydb' from CREATE DATABASE")
	}

	// All nodes have language=sql and IsExported
	for _, n := range nodes {
		if n.Language != types.LanguageSQL {
			t.Errorf("node %s has language %s, want sql", n.ID, n.Language)
		}
		if !n.IsExported {
			t.Errorf("node %s IsExported=false, want true", n.ID)
		}
	}
}

// ---------------------------------------------------------------------------
// MySQL backtick fixture
// ---------------------------------------------------------------------------

const mysqlFixture = "`" + `db` + "`" + `.` + "`" + `products` + "`"

const mysqlFixtureFull = `
CREATE TABLE ` + "`products`" + ` (
    ` + "`product_id`" + ` INT AUTO_INCREMENT,
    ` + "`name`" + `        VARCHAR(255),
    ` + "`price`" + `       DECIMAL(10,2),
    PRIMARY KEY (` + "`product_id`" + `)
);

CREATE OR REPLACE VIEW ` + "`active_products`" + ` AS
SELECT * FROM ` + "`products`" + ` WHERE active = 1;

CREATE INDEX ` + "`idx_product_name`" + ` ON ` + "`products`" + ` (` + "`name`" + `);

CREATE PROCEDURE ` + "`update_price`" + `(IN new_price DECIMAL)
BEGIN
  UPDATE ` + "`products`" + ` SET price = new_price;
END;

CREATE FUNCTION ` + "`calc_tax`" + `(price DECIMAL) RETURNS DECIMAL
BEGIN
  RETURN price * 0.1;
END;
`

func TestMySQLBacktickDefinitions(t *testing.T) {
	ext := newSQL()
	result, err := ext.Extract("/db/mysql.sql", mysqlFixtureFull)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	nodes := result.Nodes
	edges := result.Edges

	// Table (backtick-quoted name should normalize to bare)
	prodNode := findSQLNode(nodes, types.NodeKindTable, "products")
	if prodNode == nil {
		t.Error("expected table 'products' (backtick normalized)")
	}
	// Columns
	nameCol := findSQLNode(nodes, types.NodeKindColumn, "name")
	if nameCol == nil {
		t.Error("expected column 'name'")
	}
	// PRIMARY KEY line must not produce a column
	pkCol := findSQLNode(nodes, types.NodeKindColumn, "product_id")
	if pkCol == nil {
		// product_id IS a column — the PRIMARY KEY constraint line (table-level)
		// should be skipped, not the column definition itself
		t.Error("expected column 'product_id'")
	}

	// View (backtick)
	viewNode := findSQLNode(nodes, types.NodeKindView, "active_products")
	if viewNode == nil {
		t.Error("expected view 'active_products'")
	}

	// Index with contains edge
	idxNode := findSQLNode(nodes, types.NodeKindIndex, "idx_product_name")
	if idxNode == nil {
		t.Error("expected index 'idx_product_name'")
	}
	if !hasContainsEdge(edges, "products", "idx_product_name", nodes) {
		t.Error("expected contains edge products→idx_product_name")
	}

	// Procedure
	procNode := findSQLNode(nodes, types.NodeKindProcedure, "update_price")
	if procNode == nil {
		t.Error("expected procedure 'update_price'")
	}

	// Function
	fnNode := findSQLNode(nodes, types.NodeKindFunction, "calc_tax")
	if fnNode == nil {
		t.Error("expected function 'calc_tax'")
	}
}

// ---------------------------------------------------------------------------
// T-SQL [bracket] fixture (CREATE OR ALTER, GO, CREATE TYPE FROM, synonyms)
// ---------------------------------------------------------------------------

const tsqlFixture = `
CREATE TABLE [dbo].[Customers] (
    [CustomerId]  INT IDENTITY(1,1) NOT NULL,
    [FirstName]   NVARCHAR(100),
    [LastName]    NVARCHAR(100),
    [FullName]    AS ([FirstName] + ' ' + [LastName]),
    CONSTRAINT [PK_Customers] PRIMARY KEY CLUSTERED ([CustomerId])
)
GO

CREATE OR ALTER PROCEDURE [dbo].[usp_GetCustomer] @Id INT
AS
BEGIN
    SELECT * FROM [dbo].[Customers] WHERE [CustomerId] = @Id
END
GO

CREATE OR ALTER FUNCTION [dbo].[fn_FormatName] (@First NVARCHAR(50), @Last NVARCHAR(50))
RETURNS NVARCHAR(105)
AS
BEGIN
    RETURN @First + ' ' + @Last
END
GO

CREATE TRIGGER [trg_Customer_Audit]
ON [dbo].[Customers]
AFTER INSERT, UPDATE
AS
BEGIN
  INSERT INTO AuditLog SELECT * FROM inserted
END
GO

CREATE UNIQUE INDEX [idx_Customer_Email] ON [dbo].[Customers] ([Email])
GO

CREATE TYPE [dbo].[SSNType] FROM NVARCHAR(11) NOT NULL
GO

CREATE TYPE [dbo].[CustomerTableType] AS TABLE (
    [Id]    INT,
    [Name]  NVARCHAR(100)
)
GO

CREATE SYNONYM [dbo].[Cust] FOR [dbo].[Customers]
GO

CREATE DATABASE CorpDB
GO

CREATE SCHEMA [reporting]
GO
`

func TestTSQLDefinitions(t *testing.T) {
	ext := newSQL()
	result, err := ext.Extract("/db/tsql.sql", tsqlFixture)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	nodes := result.Nodes

	// Table with bracket-quoted names normalised
	custNode := findSQLNode(nodes, types.NodeKindTable, "Customers")
	if custNode == nil {
		t.Fatal("expected table node 'Customers' (brackets normalized)")
	}

	// Computed column → metadata generated:true
	fullNameCol := findSQLNode(nodes, types.NodeKindColumn, "FullName")
	if fullNameCol == nil {
		t.Error("expected computed column 'FullName'")
	} else if !metadataHas(fullNameCol.Metadata, "generated", "true") {
		t.Error("FullName column should have metadata {\"generated\":true}")
	}

	// CONSTRAINT line must NOT produce a column
	pkCol := findSQLNode(nodes, types.NodeKindColumn, "PK_Customers")
	if pkCol != nil {
		t.Error("CONSTRAINT line must not produce a column node")
	}

	// CREATE OR ALTER PROCEDURE
	procNode := findSQLNode(nodes, types.NodeKindProcedure, "usp_GetCustomer")
	if procNode == nil {
		t.Error("expected procedure 'usp_GetCustomer' (CREATE OR ALTER)")
	}

	// CREATE OR ALTER FUNCTION
	fnNode := findSQLNode(nodes, types.NodeKindFunction, "fn_FormatName")
	if fnNode == nil {
		t.Error("expected function 'fn_FormatName' (CREATE OR ALTER)")
	}

	// Trigger
	trigNode := findSQLNode(nodes, types.NodeKindTrigger, "trg_Customer_Audit")
	if trigNode == nil {
		t.Error("expected trigger 'trg_Customer_Audit'")
	}

	// Index
	idxNode := findSQLNode(nodes, types.NodeKindIndex, "idx_Customer_Email")
	if idxNode == nil {
		t.Error("expected index 'idx_Customer_Email'")
	}

	// CREATE TYPE … FROM → type_alias
	ssnType := findSQLNode(nodes, types.NodeKindTypeAlias, "SSNType")
	if ssnType == nil {
		t.Error("expected type_alias 'SSNType' from CREATE TYPE ... FROM")
	}

	// CREATE TYPE … AS TABLE → type_alias with table_type metadata
	tvpType := findSQLNode(nodes, types.NodeKindTypeAlias, "CustomerTableType")
	if tvpType == nil {
		t.Error("expected type_alias 'CustomerTableType' from CREATE TYPE ... AS TABLE")
	} else if !metadataHas(tvpType.Metadata, "table_type", "true") {
		t.Error("CustomerTableType should have metadata {\"table_type\":true}")
	}

	// CREATE SYNONYM → type_alias with synonym metadata.
	// Use exact-name lookup because "CustomerTableType" also contains "Cust".
	var synNode *types.Node
	for i := range nodes {
		if nodes[i].Kind == types.NodeKindTypeAlias && nodes[i].Name == "Cust" {
			synNode = &nodes[i]
			break
		}
	}
	if synNode == nil {
		t.Error("expected type_alias 'Cust' from CREATE SYNONYM")
	} else if !metadataHas(synNode.Metadata, "synonym", "true") {
		t.Error("Cust synonym should have metadata {\"synonym\":true}")
	}

	// Database → module
	dbNode := findSQLNode(nodes, types.NodeKindModule, "CorpDB")
	if dbNode == nil {
		t.Error("expected module node 'CorpDB'")
	}

	// Schema → namespace
	schemaNode := findSQLNode(nodes, types.NodeKindNamespace, "reporting")
	if schemaNode == nil {
		t.Error("expected namespace node 'reporting'")
	}
}

// ---------------------------------------------------------------------------
// ANSI / schema-qualified names
// ---------------------------------------------------------------------------

const ansiFixture = `
CREATE TABLE "public"."events" (
    "event_id"   UUID DEFAULT gen_random_uuid(),
    "payload"    JSONB,
    "ts"         TIMESTAMPTZ
);

CREATE VIEW "public"."recent_events" AS
SELECT * FROM "public"."events" WHERE ts > NOW() - INTERVAL '1 day';

CREATE SEQUENCE "public"."event_seq";

CREATE TYPE "public"."status_enum" AS ENUM ('pending', 'done', 'failed');
`

func TestANSIQuotedNames(t *testing.T) {
	ext := newSQL()
	result, err := ext.Extract("/db/ansi.sql", ansiFixture)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	nodes := result.Nodes

	eventsNode := findSQLNode(nodes, types.NodeKindTable, "events")
	if eventsNode == nil {
		t.Error("expected table 'events' (ANSI-quoted, schema-qualified)")
	}
	// Schema qualification: QualifiedName should include schema
	if eventsNode != nil && !strings.Contains(eventsNode.QualifiedName, "events") {
		t.Errorf("QualifiedName should contain 'events', got %s", eventsNode.QualifiedName)
	}

	viewNode := findSQLNode(nodes, types.NodeKindView, "recent_events")
	if viewNode == nil {
		t.Error("expected view 'recent_events'")
	}

	seqNode := findSQLNode(nodes, types.NodeKindSequence, "event_seq")
	if seqNode == nil {
		t.Error("expected sequence 'event_seq'")
	}

	enumNode := findSQLNode(nodes, types.NodeKindEnum, "status_enum")
	if enumNode == nil {
		t.Error("expected enum 'status_enum'")
	}
	for _, label := range []string{"pending", "done", "failed"} {
		if findSQLNode(nodes, types.NodeKindEnumMember, label) == nil {
			t.Errorf("expected enum_member '%s'", label)
		}
	}
}

// ---------------------------------------------------------------------------
// Comment and string false-positive guard
// ---------------------------------------------------------------------------

const falsePositiveFixture = `
-- This is a comment: CREATE TABLE ghost (id INT);
/* Another block comment
   CREATE TABLE also_ghost (x TEXT);
*/

CREATE TABLE real_table (
    id   INT,
    note VARCHAR(200) DEFAULT 'CREATE TABLE fake (x INT)'
);

-- CREATE VIEW fake_view AS SELECT 1;
CREATE VIEW real_view AS SELECT 1;

INSERT INTO notes(body) VALUES ('
CREATE TABLE evil (x INT);
');
`

func TestCommentStringFalsePositives(t *testing.T) {
	ext := newSQL()
	result, err := ext.Extract("/db/fp.sql", falsePositiveFixture)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	nodes := result.Nodes

	// Must NOT find ghost tables from comments
	if n := findSQLNode(nodes, types.NodeKindTable, "ghost"); n != nil {
		t.Error("CREATE TABLE inside -- comment must not produce a node")
	}
	if n := findSQLNode(nodes, types.NodeKindTable, "also_ghost"); n != nil {
		t.Error("CREATE TABLE inside /* */ comment must not produce a node")
	}
	// Must NOT find fake_view from comment
	if n := findSQLNode(nodes, types.NodeKindView, "fake_view"); n != nil {
		t.Error("CREATE VIEW inside -- comment must not produce a node")
	}
	// Must NOT find fake from inline string literal (mid-line, guarded by ^ anchor)
	if n := findSQLNode(nodes, types.NodeKindTable, "fake"); n != nil {
		t.Error("CREATE TABLE inside single-quoted string literal must not produce a node")
	}
	// Must NOT find evil from multi-line string literal with col-0 CREATE TABLE
	// (this case requires stripStrings to be the actual guard — ^ anchor alone is insufficient)
	if n := findSQLNode(nodes, types.NodeKindTable, "evil"); n != nil {
		t.Error("CREATE TABLE at column 0 inside multi-line single-quoted string must not produce a node")
	}

	// Must find the real table and view
	if n := findSQLNode(nodes, types.NodeKindTable, "real_table"); n == nil {
		t.Error("expected table 'real_table'")
	}
	if n := findSQLNode(nodes, types.NodeKindView, "real_view"); n == nil {
		t.Error("expected view 'real_view'")
	}
}

// ---------------------------------------------------------------------------
// StartLine accuracy
// ---------------------------------------------------------------------------

const lineCheckFixture = `CREATE SCHEMA s1;
CREATE TABLE t1 (id INT);
CREATE VIEW v1 AS SELECT 1;
CREATE FUNCTION f1() RETURNS INT AS $$ BEGIN RETURN 1; END; $$ LANGUAGE plpgsql;
CREATE PROCEDURE p1() LANGUAGE plpgsql AS $$ BEGIN END; $$;
CREATE SEQUENCE seq1;
`

func TestStartLines(t *testing.T) {
	ext := newSQL()
	result, err := ext.Extract("/db/lines.sql", lineCheckFixture)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	nodes := result.Nodes

	cases := []struct {
		kind     types.NodeKind
		name     string
		wantLine int
	}{
		{types.NodeKindNamespace, "s1", 1},
		{types.NodeKindTable, "t1", 2},
		{types.NodeKindView, "v1", 3},
		{types.NodeKindFunction, "f1", 4},
		{types.NodeKindProcedure, "p1", 5},
		{types.NodeKindSequence, "seq1", 6},
	}
	for _, c := range cases {
		n := findSQLNode(nodes, c.kind, c.name)
		if n == nil {
			t.Errorf("node %s/%s not found", c.kind, c.name)
			continue
		}
		if n.StartLine != c.wantLine {
			t.Errorf("node %s/%s StartLine=%d, want %d", c.kind, c.name, n.StartLine, c.wantLine)
		}
	}
}

// ---------------------------------------------------------------------------
// Registry wiring: .sql routed to SQLExtractor
// ---------------------------------------------------------------------------

func TestRegistryWireSQL(t *testing.T) {
	reg := standalone.NewRegistry(nil) // nil pool: SQL extractor doesn't use it
	ext := reg.For(".sql")
	if ext == nil {
		t.Fatal("Registry.For(\".sql\") returned nil — SQL extractor not wired")
	}
	for _, ext2 := range []string{".ddl", ".pgsql", ".mysql"} {
		if e := reg.For(ext2); e == nil {
			t.Errorf("Registry.For(%q) returned nil — alias not wired", ext2)
		}
	}
}

// ---------------------------------------------------------------------------
// CP3 — Constraint node extraction
// ---------------------------------------------------------------------------

// hasConstraintNode returns the constraint node with the given exact name.
// WHY (F-5): constraint names are identity assertions — partial match via
// strings.Contains could silently pass when a longer name contains the search
// term (e.g. "pk" matches "pk_accounts"). Exact match prevents false passes.
func hasConstraintNode(nodes []types.Node, name string) *types.Node {
	return findSQLNodeExact(nodes, types.NodeKindConstraint, name)
}

// constraintTypeOf returns the constraint_type metadata value for a node.
func constraintTypeOf(n *types.Node) string {
	if n == nil || n.Metadata == nil {
		return ""
	}
	var m map[string]interface{}
	if err := json.Unmarshal(n.Metadata, &m); err != nil {
		return ""
	}
	v, _ := m["constraint_type"].(string)
	return v
}

// hasReferencesEdge returns true if any edge has kind references — used to
// assert CP3 does NOT emit references edges.
func hasReferencesEdge(edges []types.Edge) bool {
	for _, e := range edges {
		if e.Kind == types.EdgeKindReferences {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Postgres named + table-level constraints
// ---------------------------------------------------------------------------

const pgConstraintFixture = `
CREATE TABLE accounts (
    id          BIGSERIAL NOT NULL,
    email       VARCHAR(255) NOT NULL,
    org_id      INT NOT NULL,
    balance     NUMERIC(15,2),
    code        VARCHAR(10),
    CONSTRAINT pk_accounts PRIMARY KEY (id),
    CONSTRAINT uq_accounts_email UNIQUE (email),
    CONSTRAINT chk_balance CHECK (balance >= 0),
    FOREIGN KEY (org_id) REFERENCES orgs(id)
);

CREATE TABLE items (
    id    INT NOT NULL,
    name  TEXT,
    PRIMARY KEY (id),
    UNIQUE (name),
    CHECK (char_length(name) > 0)
);
`

func TestPGNamedConstraints(t *testing.T) {
	ext := newSQL()
	result, err := ext.Extract("/db/pg_constraints.sql", pgConstraintFixture)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	nodes := result.Nodes
	edges := result.Edges

	// Named CONSTRAINT nodes must exist with correct type metadata.
	pk := hasConstraintNode(nodes, "pk_accounts")
	if pk == nil {
		t.Fatal("expected constraint node 'pk_accounts'")
	}
	if ct := constraintTypeOf(pk); ct != "primary_key" {
		t.Errorf("pk_accounts constraint_type = %q, want primary_key", ct)
	}
	if !hasContainsEdge(edges, "accounts", "pk_accounts", nodes) {
		t.Error("expected contains edge accounts→pk_accounts")
	}

	uq := hasConstraintNode(nodes, "uq_accounts_email")
	if uq == nil {
		t.Fatal("expected constraint node 'uq_accounts_email'")
	}
	if ct := constraintTypeOf(uq); ct != "unique" {
		t.Errorf("uq_accounts_email constraint_type = %q, want unique", ct)
	}
	if !hasContainsEdge(edges, "accounts", "uq_accounts_email", nodes) {
		t.Error("expected contains edge accounts→uq_accounts_email")
	}

	chk := hasConstraintNode(nodes, "chk_balance")
	if chk == nil {
		t.Fatal("expected constraint node 'chk_balance'")
	}
	if ct := constraintTypeOf(chk); ct != "check" {
		t.Errorf("chk_balance constraint_type = %q, want check", ct)
	}

	// Anonymous FK — synthesized name must be exactly "accounts_fk_1" (stable
	// deterministic name: <table>_<suffix>_<counter>, first FK in the table body).
	anonFKNode := hasConstraintNode(nodes, "accounts_fk_1")
	if anonFKNode == nil {
		t.Error("expected anonymous FK constraint node named exactly 'accounts_fk_1'")
	} else if ct := constraintTypeOf(anonFKNode); ct != "foreign_key" {
		t.Errorf("accounts_fk_1 constraint_type = %q, want foreign_key", ct)
	}

	// Table-level anonymous PK on 'items' — synthesized name must be exactly
	// "items_pk_1" (first PK in the items body).
	itemsPKNode := hasConstraintNode(nodes, "items_pk_1")
	if itemsPKNode == nil {
		t.Error("expected anonymous PRIMARY KEY constraint node named exactly 'items_pk_1'")
	} else if ct := constraintTypeOf(itemsPKNode); ct != "primary_key" {
		t.Errorf("items_pk_1 constraint_type = %q, want primary_key", ct)
	}

	// No references edges — CP3 must NOT emit them.
	if hasReferencesEdge(edges) {
		t.Error("CP3 must NOT emit any references edges; found one")
	}

	// Inline column PK (id INT NOT NULL on accounts) must NOT produce a constraint node.
	// The column 'id' should exist as a column node, not spawn a second constraint node.
	var idConstraintCount int
	for _, n := range nodes {
		if n.Kind == types.NodeKindConstraint && strings.ToLower(n.Name) == "id" {
			idConstraintCount++
		}
	}
	if idConstraintCount > 0 {
		t.Error("inline column NOT NULL / implicit constraint on 'id' must not produce a constraint node")
	}
}

// ---------------------------------------------------------------------------
// MySQL backtick constraints
// ---------------------------------------------------------------------------

const mysqlConstraintFixture = `
CREATE TABLE ` + "`orders`" + ` (
    ` + "`order_id`" + `   INT NOT NULL,
    ` + "`customer_id`" + ` INT NOT NULL,
    ` + "`amount`" + `      DECIMAL(10,2),
    CONSTRAINT ` + "`pk_orders`" + ` PRIMARY KEY (` + "`order_id`" + `),
    CONSTRAINT ` + "`fk_orders_customer`" + ` FOREIGN KEY (` + "`customer_id`" + `) REFERENCES ` + "`customers`" + `(` + "`id`" + `)
);
`

func TestMySQLConstraints(t *testing.T) {
	ext := newSQL()
	result, err := ext.Extract("/db/mysql_constraints.sql", mysqlConstraintFixture)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	nodes := result.Nodes
	edges := result.Edges

	pk := hasConstraintNode(nodes, "pk_orders")
	if pk == nil {
		t.Fatal("expected constraint node 'pk_orders' (backtick-quoted)")
	}
	if ct := constraintTypeOf(pk); ct != "primary_key" {
		t.Errorf("pk_orders constraint_type = %q, want primary_key", ct)
	}
	if !hasContainsEdge(edges, "orders", "pk_orders", nodes) {
		t.Error("expected contains edge orders→pk_orders")
	}

	fk := hasConstraintNode(nodes, "fk_orders_customer")
	if fk == nil {
		t.Fatal("expected constraint node 'fk_orders_customer'")
	}
	if ct := constraintTypeOf(fk); ct != "foreign_key" {
		t.Errorf("fk_orders_customer constraint_type = %q, want foreign_key", ct)
	}
	// FK references target stashed in metadata (CP4 prep), but no references edge.
	if hasReferencesEdge(edges) {
		t.Error("CP3 must NOT emit references edges")
	}
}

// ---------------------------------------------------------------------------
// T-SQL [bracket] constraints + ALTER TABLE ADD CONSTRAINT
// ---------------------------------------------------------------------------

const tsqlConstraintFixture = `
CREATE TABLE [dbo].[Employees] (
    [EmpId]    INT NOT NULL,
    [DeptId]   INT NOT NULL,
    [Email]    NVARCHAR(255) NOT NULL,
    [Salary]   DECIMAL(12,2),
    CONSTRAINT [PK_Employees] PRIMARY KEY ([EmpId]),
    CONSTRAINT [UQ_Employees_Email] UNIQUE ([Email]),
    CONSTRAINT [CHK_Salary] CHECK ([Salary] > 0),
    CONSTRAINT [FK_Employees_Dept] FOREIGN KEY ([DeptId]) REFERENCES [dbo].[Departments]([DeptId])
)
GO

ALTER TABLE [dbo].[Employees] ADD CONSTRAINT [DF_Salary] CHECK ([Salary] < 1000000)
GO

ALTER TABLE [dbo].[Employees] ADD CONSTRAINT [UQ_EmpId_Email] UNIQUE ([EmpId], [Email])
GO

ALTER TABLE [dbo].[Orders] ADD PRIMARY KEY ([OrderId])
GO
`

func TestTSQLConstraints(t *testing.T) {
	ext := newSQL()
	result, err := ext.Extract("/db/tsql_constraints.sql", tsqlConstraintFixture)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	nodes := result.Nodes
	edges := result.Edges

	// Named constraints in CREATE TABLE body.
	pk := hasConstraintNode(nodes, "PK_Employees")
	if pk == nil {
		t.Fatal("expected constraint node 'PK_Employees' (bracket-quoted)")
	}
	if ct := constraintTypeOf(pk); ct != "primary_key" {
		t.Errorf("PK_Employees constraint_type = %q, want primary_key", ct)
	}
	if !hasContainsEdge(edges, "Employees", "PK_Employees", nodes) {
		t.Error("expected contains edge Employees→PK_Employees")
	}

	uq := hasConstraintNode(nodes, "UQ_Employees_Email")
	if uq == nil {
		t.Fatal("expected constraint node 'UQ_Employees_Email'")
	}
	if ct := constraintTypeOf(uq); ct != "unique" {
		t.Errorf("UQ_Employees_Email constraint_type = %q, want unique", ct)
	}

	chk := hasConstraintNode(nodes, "CHK_Salary")
	if chk == nil {
		t.Fatal("expected constraint node 'CHK_Salary'")
	}
	if ct := constraintTypeOf(chk); ct != "check" {
		t.Errorf("CHK_Salary constraint_type = %q, want check", ct)
	}

	fk := hasConstraintNode(nodes, "FK_Employees_Dept")
	if fk == nil {
		t.Fatal("expected constraint node 'FK_Employees_Dept'")
	}
	if ct := constraintTypeOf(fk); ct != "foreign_key" {
		t.Errorf("FK_Employees_Dept constraint_type = %q, want foreign_key", ct)
	}

	// ALTER TABLE ADD CONSTRAINT — named.
	dfSalary := hasConstraintNode(nodes, "DF_Salary")
	if dfSalary == nil {
		t.Fatal("expected constraint node 'DF_Salary' from ALTER TABLE ADD CONSTRAINT")
	}
	if ct := constraintTypeOf(dfSalary); ct != "check" {
		t.Errorf("DF_Salary constraint_type = %q, want check", ct)
	}

	uqAlt := hasConstraintNode(nodes, "UQ_EmpId_Email")
	if uqAlt == nil {
		t.Fatal("expected constraint node 'UQ_EmpId_Email' from ALTER TABLE ADD CONSTRAINT")
	}
	if ct := constraintTypeOf(uqAlt); ct != "unique" {
		t.Errorf("UQ_EmpId_Email constraint_type = %q, want unique", ct)
	}

	// ALTER TABLE ADD PRIMARY KEY (anonymous) — synthesized name must be exactly
	// "Orders_pk_1" (table = "Orders" after bracket normalization, first PK, suffix = pk).
	anonAltPKNode := hasConstraintNode(nodes, "Orders_pk_1")
	if anonAltPKNode == nil {
		t.Error("expected anonymous PK constraint node named exactly 'Orders_pk_1' from ALTER TABLE ADD PRIMARY KEY")
	} else if ct := constraintTypeOf(anonAltPKNode); ct != "primary_key" {
		t.Errorf("Orders_pk_1 constraint_type = %q, want primary_key", ct)
	}

	// No references edges.
	if hasReferencesEdge(edges) {
		t.Error("CP3 must NOT emit references edges")
	}
}

// ---------------------------------------------------------------------------
// Inline column-level PK does NOT spawn a constraint node
// ---------------------------------------------------------------------------

const inlineConstraintFixture = `
CREATE TABLE widgets (
    id   INT PRIMARY KEY,
    sku  VARCHAR(20) UNIQUE,
    qty  INT NOT NULL
);
`

func TestInlineColumnConstraintNoNode(t *testing.T) {
	ext := newSQL()
	result, err := ext.Extract("/db/inline.sql", inlineConstraintFixture)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	nodes := result.Nodes

	// Columns must exist.
	if findSQLNode(nodes, types.NodeKindColumn, "id") == nil {
		t.Error("expected column 'id'")
	}
	if findSQLNode(nodes, types.NodeKindColumn, "sku") == nil {
		t.Error("expected column 'sku'")
	}

	// No constraint node for inline column PK/UNIQUE.
	for _, n := range nodes {
		if n.Kind == types.NodeKindConstraint {
			t.Errorf("inline column-level constraint must not produce constraint node; got %s", n.Name)
		}
	}
}

// ---------------------------------------------------------------------------
// Existing CP2 still passes: constraint lines not double-counted as columns
// ---------------------------------------------------------------------------

func TestCP2ColumnExtractionStillSkipsConstraintLines(t *testing.T) {
	// Re-run the Postgres fixture and verify constraint lines aren't emitted as columns.
	ext := newSQL()
	result, err := ext.Extract("/db/schema.sql", pgFixture)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	nodes := result.Nodes

	// The CONSTRAINT uq_users_email line in the Postgres fixture must not appear
	// as a column node — this was already tested in CP2 and must remain true.
	if findSQLNode(nodes, types.NodeKindColumn, "uq_users_email") != nil {
		t.Error("CONSTRAINT line (uq_users_email) must not be emitted as a column node")
	}

	// But it SHOULD now be a constraint node (CP3).
	if hasConstraintNode(nodes, "uq_users_email") == nil {
		t.Error("expected constraint node 'uq_users_email' from CP3 extraction")
	}
}

// ---------------------------------------------------------------------------
// ALTER TABLE ADD CONSTRAINT (named) produces exactly one node — not two
// ---------------------------------------------------------------------------

// TestAlterAddNamedConstraintExactlyOneNode asserts that
// ALTER TABLE t ADD CONSTRAINT foo PRIMARY KEY(...) emits exactly ONE
// constraint node (named "foo"), not two. This is the structural-exclusion
// regression guard: alterAddAnonConstraintRE must not double-fire when a
// CONSTRAINT keyword is present between ADD and the type keyword.
const alterNamedOnlyFixture = `
CREATE TABLE t (id INT, val TEXT);
ALTER TABLE t ADD CONSTRAINT foo PRIMARY KEY (id);
`

func TestAlterAddNamedConstraintExactlyOneNode(t *testing.T) {
	ext := newSQL()
	result, err := ext.Extract("/db/alter_named.sql", alterNamedOnlyFixture)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	nodes := result.Nodes

	var constraintNodes []types.Node
	for _, n := range nodes {
		if n.Kind == types.NodeKindConstraint {
			constraintNodes = append(constraintNodes, n)
		}
	}
	if len(constraintNodes) != 1 {
		names := make([]string, len(constraintNodes))
		for i, n := range constraintNodes {
			names[i] = n.Name
		}
		t.Fatalf("ALTER TABLE ADD CONSTRAINT foo PRIMARY KEY should emit exactly 1 constraint node, got %d: %v", len(constraintNodes), names)
	}
	if constraintNodes[0].Name != "foo" {
		t.Errorf("constraint node name = %q, want %q", constraintNodes[0].Name, "foo")
	}
	if ct := constraintTypeOf(&constraintNodes[0]); ct != "primary_key" {
		t.Errorf("constraint type = %q, want primary_key", ct)
	}
}

// ---------------------------------------------------------------------------
// CP5 — Routine-body edges (reads / writes / calls)
// ---------------------------------------------------------------------------

// hasUnresolvedRef returns the first UnresolvedReference matching name + kind.
func hasUnresolvedRef(refs []types.UnresolvedReference, name string, kind types.EdgeKind) bool {
	for _, r := range refs {
		if r.ReferenceName == name && r.ReferenceKind == kind {
			return true
		}
	}
	return false
}

// countUnresolvedRefs counts refs with the given name (any kind).
func countUnresolvedRefs(refs []types.UnresolvedReference, name string) int {
	n := 0
	for _, r := range refs {
		if r.ReferenceName == name {
			n++
		}
	}
	return n
}

// TestRoutineBodyEdgesReadsWritesCalls verifies that a procedure body emits
// references (FROM/JOIN reads), writes (INSERT/UPDATE/DELETE/MERGE), and
// calls (EXEC/CALL) as distinct UnresolvedReferences.
// WHY: the core value proposition — route writes-through-procedures makes
// "code impact <table>" only useful if writers are distinguished from readers.
const routineBodyFixture = `
CREATE TABLE orders (id INT, status TEXT, amount NUMERIC);
CREATE TABLE archive (id INT, status TEXT);
CREATE TABLE audit_log (event TEXT);
CREATE PROCEDURE close_orders()
LANGUAGE plpgsql AS $$
BEGIN
  INSERT INTO archive SELECT id, status FROM orders WHERE status = 'closed';
  UPDATE orders SET status = 'archived' WHERE status = 'closed';
  DELETE FROM audit_log WHERE event IS NULL;
  SELECT id FROM orders WHERE amount > 100;
  EXEC log_event('closed');
END;
$$;
`

func TestRoutineBodyEdgesReadsWritesCalls(t *testing.T) {
	ext := newSQL()
	result, err := ext.Extract("/db/proc_body.sql", routineBodyFixture)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	refs := result.UnresolvedReferences

	// archive is INSERT INTO target → writes
	if !hasUnresolvedRef(refs, "archive", types.EdgeKindWrites) {
		t.Error("expected writes edge to 'archive' (INSERT INTO)")
	}
	// orders is UPDATE target → writes
	if !hasUnresolvedRef(refs, "orders", types.EdgeKindWrites) {
		t.Error("expected writes edge to 'orders' (UPDATE)")
	}
	// audit_log is DELETE FROM target → writes
	if !hasUnresolvedRef(refs, "audit_log", types.EdgeKindWrites) {
		t.Error("expected writes edge to 'audit_log' (DELETE FROM)")
	}
	// orders is also SELECT FROM → references
	if !hasUnresolvedRef(refs, "orders", types.EdgeKindReferences) {
		t.Error("expected references edge to 'orders' (SELECT FROM)")
	}
	// log_event is EXEC target → calls
	if !hasUnresolvedRef(refs, "log_event", types.EdgeKindCalls) {
		t.Error("expected calls edge to 'log_event' (EXEC)")
	}
}

// TestRoutineBodyMergeInto verifies MERGE INTO is captured as a writes edge.
const mergeBodyFixture = `
CREATE TABLE target_tbl (id INT, val TEXT);
CREATE PROCEDURE merge_proc()
LANGUAGE plpgsql AS $$
BEGIN
  MERGE INTO target_tbl AS t USING source_tbl AS s ON t.id = s.id
  WHEN MATCHED THEN UPDATE SET val = s.val
  WHEN NOT MATCHED THEN INSERT VALUES (s.id, s.val);
END;
$$;
`

func TestRoutineBodyMergeInto(t *testing.T) {
	ext := newSQL()
	result, err := ext.Extract("/db/merge_proc.sql", mergeBodyFixture)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if !hasUnresolvedRef(result.UnresolvedReferences, "target_tbl", types.EdgeKindWrites) {
		t.Error("expected writes edge to 'target_tbl' from MERGE INTO")
	}
}

// TestCTEShadowGuard verifies that a name bound by WITH x AS (...) does NOT
// produce a references or writes edge to x.
// WHY: a CTE is statement-local — emitting an edge to it would be a false
// reference to a non-existent table. The resolver drops unresolved refs, but
// this guard is asserted explicitly so the intent is encoded in the test.
const cteShadowFixture = `
CREATE TABLE real_table (id INT);
CREATE PROCEDURE cte_proc()
LANGUAGE plpgsql AS $$
BEGIN
  WITH cte_name AS (SELECT id FROM real_table)
  SELECT id FROM cte_name;
END;
$$;
`

func TestCTEShadowGuard(t *testing.T) {
	ext := newSQL()
	result, err := ext.Extract("/db/cte_shadow.sql", cteShadowFixture)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	// cte_name must NOT produce any edge (it is CTE-local)
	if n := countUnresolvedRefs(result.UnresolvedReferences, "cte_name"); n > 0 {
		t.Errorf("CTE name 'cte_name' must not produce any edge, got %d refs", n)
	}
	// real_table SHOULD produce a references edge (it is a real table)
	if !hasUnresolvedRef(result.UnresolvedReferences, "real_table", types.EdgeKindReferences) {
		t.Error("expected references edge to 'real_table' in CTE body")
	}
}

// TestF6LateralNoEdge verifies that FROM/JOIN LATERAL does not produce a
// spurious 'LATERAL' edge. LATERAL is a SQL clause modifier, not a table name.
// WHY (F-6): the keyword filter must cover LATERAL/UNNEST so no false edge to
// an imaginary "LATERAL" node is emitted.
const lateralFixture = `
CREATE TABLE events (id INT, data TEXT);
CREATE VIEW lateral_view AS
SELECT e.id, s.word
FROM events e, LATERAL unnest(string_to_array(e.data, ',')) AS s(word);
`

func TestF6LateralNoEdge(t *testing.T) {
	ext := newSQL()
	result, err := ext.Extract("/db/lateral_view.sql", lateralFixture)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	// LATERAL must NOT appear as a reference target
	if countUnresolvedRefs(result.UnresolvedReferences, "LATERAL") > 0 ||
		countUnresolvedRefs(result.UnresolvedReferences, "lateral") > 0 {
		t.Error("LATERAL must not produce a reference edge (it is a SQL keyword, not a table name)")
	}
	// events SHOULD appear as a references target (it IS a real table)
	if !hasUnresolvedRef(result.UnresolvedReferences, "events", types.EdgeKindReferences) {
		t.Error("expected references edge to 'events' in lateral_view")
	}
}

// TestF7PolicyFnCallScopedToUSING verifies that fn-call capture in a policy
// statement is limited to the USING (...) and WITH CHECK (...) expressions,
// not the entire statement text. A function call outside those clauses (e.g.
// in a comment or the policy name itself) must not be captured.
// WHY (F-7): scanning the whole statement grabs SQL builtins like
// current_setting(...) that appear in non-expression positions, producing
// noisy calls edges that don't resolve.
const policyF7Fixture = `
CREATE TABLE docs (id INT, owner TEXT);
CREATE OR REPLACE FUNCTION owner_check(p TEXT) RETURNS BOOL AS $$ BEGIN RETURN TRUE; END; $$ LANGUAGE plpgsql;
CREATE POLICY doc_policy ON docs
USING (owner_check(owner));
`

// policyF7FixtureNoExtraFn has a function call in the policy body outside
// USING/WITH CHECK — this must NOT be captured.
const policyF7FixtureNoExtraFn = `
CREATE TABLE docs (id INT, owner TEXT);
CREATE POLICY doc_policy ON docs
AS PERMISSIVE FOR SELECT
TO public
USING (owner = current_user);
`

func TestF7PolicyFnCallScopedToUSING(t *testing.T) {
	// Part 1: fn call inside USING IS captured.
	ext := newSQL()
	result, err := ext.Extract("/db/policy_using.sql", policyF7Fixture)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if !hasUnresolvedRef(result.UnresolvedReferences, "owner_check", types.EdgeKindCalls) {
		t.Error("expected calls edge to 'owner_check' inside USING expression")
	}

	// Part 2: when the policy uses only a simple expression in USING (no fn call),
	// no spurious calls edge to SQL keywords like 'current_user' or 'public' appears.
	result2, err2 := ext.Extract("/db/policy_nofn.sql", policyF7FixtureNoExtraFn)
	if err2 != nil {
		t.Fatalf("Extract: %v", err2)
	}
	// current_user and public should not appear as calls edges.
	for _, r := range result2.UnresolvedReferences {
		if r.ReferenceKind == types.EdgeKindCalls {
			t.Errorf("unexpected calls edge to %q from policy with no fn in USING", r.ReferenceName)
		}
	}
}

// TestFunctionBodyReferences verifies FROM/JOIN in a function body also
// produces references edges (same logic as procedure).
const fnBodyFixture = `
CREATE TABLE products (id INT, price NUMERIC);
CREATE TABLE categories (id INT, name TEXT);
CREATE FUNCTION get_products(cat_id INT) RETURNS TABLE(id INT) AS $$
BEGIN
  RETURN QUERY SELECT p.id FROM products p JOIN categories c ON p.id = c.id
               WHERE c.id = cat_id;
END;
$$ LANGUAGE plpgsql;
`

func TestFunctionBodyReferences(t *testing.T) {
	ext := newSQL()
	result, err := ext.Extract("/db/fn_body.sql", fnBodyFixture)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	refs := result.UnresolvedReferences
	if !hasUnresolvedRef(refs, "products", types.EdgeKindReferences) {
		t.Error("expected references edge to 'products' from function body FROM clause")
	}
	if !hasUnresolvedRef(refs, "categories", types.EdgeKindReferences) {
		t.Error("expected references edge to 'categories' from function body JOIN clause")
	}
}

// TestTSQLRoutineBodyWrites verifies T-SQL procedure body INSERT/UPDATE/DELETE.
const tsqlRoutineBodyFixture = `
CREATE TABLE [dbo].[Orders] ([OrderId] INT, [Status] NVARCHAR(50));
CREATE TABLE [dbo].[Archive] ([OrderId] INT, [Status] NVARCHAR(50));
CREATE PROCEDURE [dbo].[ArchiveOrders]
AS
BEGIN
  INSERT INTO [dbo].[Archive] SELECT [OrderId], [Status] FROM [dbo].[Orders] WHERE [Status] = 'closed';
  UPDATE [dbo].[Orders] SET [Status] = 'archived' WHERE [Status] = 'closed';
  DELETE FROM [dbo].[Orders] WHERE [Status] = 'archived';
END;
GO
`

func TestTSQLRoutineBodyWrites(t *testing.T) {
	ext := newSQL()
	result, err := ext.Extract("/db/tsql_proc.sql", tsqlRoutineBodyFixture)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	refs := result.UnresolvedReferences

	// Archive is INSERT INTO target
	if !hasUnresolvedRef(refs, "Archive", types.EdgeKindWrites) {
		t.Error("expected writes edge to 'Archive' (INSERT INTO [dbo].[Archive])")
	}
	// Orders is UPDATE target
	if !hasUnresolvedRef(refs, "Orders", types.EdgeKindWrites) {
		t.Error("expected writes edge to 'Orders' (UPDATE [dbo].[Orders])")
	}
	// Orders is also DELETE FROM target (still writes)
	// (already covered above — orders should appear as writes)
	// Orders is also SELECT FROM → references
	if !hasUnresolvedRef(refs, "Orders", types.EdgeKindReferences) {
		t.Error("expected references edge to 'Orders' (SELECT FROM)")
	}
}

// TestF14RoutineBodyLateralNoEdge verifies that JOIN LATERAL inside a routine
// body does not produce a spurious 'LATERAL' reference edge. The keyword filter
// that protects view bodies (F-6) must also apply to function and procedure
// bodies scanned by scanBodyEdges.
// WHY (F-14): scanBodyEdges reuses viewBodyFROMRE which matches "FROM|JOIN
// <name>". Without the isSQLRefKeyword guard, "LATERAL" following JOIN would
// be captured as a table reference to an imaginary "LATERAL" node.
const routineLateralFixture = `
CREATE TABLE orders (id INT, tags TEXT[]);

CREATE FUNCTION tagged_orders(tag TEXT)
RETURNS TABLE(id INT) AS $$
BEGIN
  RETURN QUERY
    SELECT o.id
    FROM orders o
    JOIN LATERAL unnest(o.tags) AS t(tag) ON t.tag = tagged_orders.tag;
END;
$$ LANGUAGE plpgsql;

CREATE PROCEDURE refresh_tagged(tag TEXT) AS $$
BEGIN
  SELECT o.id
  FROM orders o
  JOIN LATERAL unnest(o.tags) AS t(tag) ON TRUE;
END;
$$ LANGUAGE plpgsql;
`

func TestF14RoutineBodyLateralNoEdge(t *testing.T) {
	ext := newSQL()
	result, err := ext.Extract("/db/routine_lateral.sql", routineLateralFixture)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	refs := result.UnresolvedReferences

	// LATERAL must NOT appear as any reference target in either routine body.
	if countUnresolvedRefs(refs, "LATERAL") > 0 || countUnresolvedRefs(refs, "lateral") > 0 {
		t.Error("LATERAL must not produce a reference edge inside a routine body (it is a SQL keyword, not a table name)")
	}

	// orders SHOULD appear as a references edge (it IS a real table referenced in both bodies).
	if !hasUnresolvedRef(refs, "orders", types.EdgeKindReferences) {
		t.Error("expected references edge to 'orders' from routine body FROM clause")
	}
}
