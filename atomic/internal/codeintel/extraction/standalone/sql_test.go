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

// ---------------------------------------------------------------------------
// A1 — Snowflake preamble class/security modifiers
// ---------------------------------------------------------------------------

// TestA1SnowflakeTransientTable verifies that CREATE OR REPLACE TRANSIENT TABLE
// produces a table node (not a false-negative due to the TRANSIENT modifier).
// A1 spec: TRANSIENT is a class modifier between OR REPLACE and TABLE.
const a1TransientTableFixture = `
CREATE OR REPLACE TRANSIENT TABLE dbo.t (
    id   INT,
    name VARCHAR(100)
);
`

func TestA1SnowflakeTransientTable(t *testing.T) {
	ext := newSQL()
	result, err := ext.Extract("/db/snowflake_a1.sql", a1TransientTableFixture)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	node := findSQLNodeExact(result.Nodes, types.NodeKindTable, "t")
	if node == nil {
		t.Error("expected table node 't' from CREATE OR REPLACE TRANSIENT TABLE dbo.t")
	}
}

// TestA1SnowflakeSecureView verifies that CREATE OR REPLACE SECURE VIEW produces
// a view node AND a references edge to the source table in the view body.
// A1 spec: SECURE is a security modifier between OR REPLACE and VIEW.
const a1SecureViewFixture = `
CREATE OR REPLACE SECURE VIEW v AS
SELECT id, name FROM base;
`

func TestA1SnowflakeSecureView(t *testing.T) {
	ext := newSQL()
	result, err := ext.Extract("/db/snowflake_a1_view.sql", a1SecureViewFixture)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	viewNode := findSQLNodeExact(result.Nodes, types.NodeKindView, "v")
	if viewNode == nil {
		t.Fatal("expected view node 'v' from CREATE OR REPLACE SECURE VIEW")
	}
	if !hasUnresolvedRef(result.UnresolvedReferences, "base", types.EdgeKindReferences) {
		t.Error("expected references edge to 'base' from SECURE VIEW body")
	}
}

// TestA1AdditionalClassModifiers verifies TEMPORARY, TEMP, VOLATILE, LOCAL,
// GLOBAL for tables and RECURSIVE for views all parse correctly.
const a1AdditionalModifiersFixture = `
CREATE OR REPLACE TEMPORARY TABLE tmp_orders (id INT);
CREATE OR REPLACE TEMP TABLE tmp_items (id INT);
CREATE OR REPLACE VOLATILE TABLE vol_cache (id INT);
CREATE OR REPLACE LOCAL TEMPORARY TABLE local_tmp (id INT);
CREATE OR REPLACE GLOBAL TEMPORARY TABLE global_tmp (id INT);
CREATE OR REPLACE RECURSIVE VIEW rv AS SELECT 1 AS n;
`

func TestA1AdditionalClassModifiers(t *testing.T) {
	ext := newSQL()
	result, err := ext.Extract("/db/snowflake_a1_more.sql", a1AdditionalModifiersFixture)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	for _, name := range []string{"tmp_orders", "tmp_items", "vol_cache", "local_tmp", "global_tmp"} {
		if findSQLNodeExact(result.Nodes, types.NodeKindTable, name) == nil {
			t.Errorf("expected table node %q from CREATE OR REPLACE <modifier> TABLE", name)
		}
	}
	if findSQLNodeExact(result.Nodes, types.NodeKindView, "rv") == nil {
		t.Error("expected view node 'rv' from CREATE OR REPLACE RECURSIVE VIEW")
	}
}

// TestA1GlobalTemporaryTableValid verifies that CREATE GLOBAL TEMPORARY TABLE
// (SQL-standard compound modifier) produces a table node. LOCAL/GLOBAL are only
// legal as prefixes to TEMP/TEMPORARY — this is the valid two-keyword form.
const a1GlobalTempTableFixture = `
CREATE GLOBAL TEMPORARY TABLE gt (id INT);
`

func TestA1GlobalTemporaryTableValid(t *testing.T) {
	ext := newSQL()
	result, err := ext.Extract("/db/snowflake_a1_global_tmp.sql", a1GlobalTempTableFixture)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if findSQLNodeExact(result.Nodes, types.NodeKindTable, "gt") == nil {
		t.Error("expected table node 'gt' from CREATE GLOBAL TEMPORARY TABLE")
	}
}

// TestA1StandaloneLocalGlobalNotCaptured guards that bare CREATE LOCAL TABLE
// and CREATE GLOBAL TABLE (invalid SQL — LOCAL/GLOBAL require TEMP/TEMPORARY)
// do NOT produce table nodes under the tightened tableClassPat.
const a1StandaloneLocalGlobalFixture = `
CREATE LOCAL TABLE bad_local (id INT);
CREATE GLOBAL TABLE bad_global (id INT);
`

func TestA1StandaloneLocalGlobalNotCaptured(t *testing.T) {
	ext := newSQL()
	result, err := ext.Extract("/db/snowflake_a1_bad.sql", a1StandaloneLocalGlobalFixture)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if findSQLNodeExact(result.Nodes, types.NodeKindTable, "bad_local") != nil {
		t.Error("CREATE LOCAL TABLE must not produce a table node — LOCAL requires TEMP/TEMPORARY")
	}
	if findSQLNodeExact(result.Nodes, types.NodeKindTable, "bad_global") != nil {
		t.Error("CREATE GLOBAL TABLE must not produce a table node — GLOBAL requires TEMP/TEMPORARY")
	}
}

// ---------------------------------------------------------------------------
// A5 — CREATE STAGE
// ---------------------------------------------------------------------------

// TestA5CreateStage verifies that CREATE STAGE <name> emits a stage node.
// A5 spec: new stage node (top-level definition loop), no outbound edges.
const a5StageFixture = `
CREATE STAGE my_stage URL='s3://bucket/path' CREDENTIALS=(AWS_KEY_ID='key');
`

func TestA5CreateStage(t *testing.T) {
	ext := newSQL()
	result, err := ext.Extract("/db/snowflake_a5.sql", a5StageFixture)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if findSQLNodeExact(result.Nodes, types.NodeKindStage, "my_stage") == nil {
		t.Error("expected stage node 'my_stage' from CREATE STAGE my_stage URL='s3://...'")
	}
}

// TestA5CreateStageOrReplace verifies CREATE OR REPLACE STAGE <name>.
const a5StageOrReplaceFixture = `
CREATE OR REPLACE STAGE etl_stage;
`

func TestA5CreateStageOrReplace(t *testing.T) {
	ext := newSQL()
	result, err := ext.Extract("/db/snowflake_a5_orreplace.sql", a5StageOrReplaceFixture)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if findSQLNodeExact(result.Nodes, types.NodeKindStage, "etl_stage") == nil {
		t.Error("expected stage node 'etl_stage' from CREATE OR REPLACE STAGE etl_stage")
	}
}

// TestA5CreateStageTempIfNotExists verifies TEMPORARY/TEMP modifiers and IF NOT EXISTS.
const a5StageTempIfNotExistsFixture = `
CREATE OR REPLACE TEMPORARY STAGE IF NOT EXISTS temp_stage;
CREATE TEMP STAGE raw_stage;
`

func TestA5CreateStageTempIfNotExists(t *testing.T) {
	ext := newSQL()
	result, err := ext.Extract("/db/snowflake_a5_temp.sql", a5StageTempIfNotExistsFixture)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if findSQLNodeExact(result.Nodes, types.NodeKindStage, "temp_stage") == nil {
		t.Error("expected stage node 'temp_stage' from CREATE OR REPLACE TEMPORARY STAGE IF NOT EXISTS temp_stage")
	}
	if findSQLNodeExact(result.Nodes, types.NodeKindStage, "raw_stage") == nil {
		t.Error("expected stage node 'raw_stage' from CREATE TEMP STAGE raw_stage")
	}
}

// ---------------------------------------------------------------------------
// A2 — COPY INTO (body edge, owned by enclosing routine/task)
// ---------------------------------------------------------------------------

// TestA2CopyIntoBodyEdges verifies both COPY INTO directions inside a procedure body.
// A2 spec:
//   - COPY INTO <tbl> FROM @<stage> → writes to tbl + references to stage.
//   - COPY INTO @<stage> FROM <tbl> → writes to stage + references to tbl.
//
// WHY: direction decided by whether the COPY target starts with '@'.
const a2CopyIntoFixture = `
CREATE TABLE fact (id INT, amount NUMERIC);
CREATE PROCEDURE load_fact()
LANGUAGE SQL AS $$
BEGIN
  COPY INTO fact FROM @load_stage/path/to/file.csv;
  COPY INTO @out_stage FROM fact;
END;
$$;
`

func TestA2CopyIntoBodyEdges(t *testing.T) {
	ext := newSQL()
	result, err := ext.Extract("/db/snowflake_a2.sql", a2CopyIntoFixture)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	refs := result.UnresolvedReferences

	// COPY INTO fact FROM @load_stage → writes to fact
	if !hasUnresolvedRef(refs, "fact", types.EdgeKindWrites) {
		t.Error("expected writes edge to 'fact' from COPY INTO fact FROM @load_stage")
	}
	// COPY INTO fact FROM @load_stage → references to load_stage (@ stripped)
	if !hasUnresolvedRef(refs, "load_stage", types.EdgeKindReferences) {
		t.Error("expected references edge to 'load_stage' from COPY INTO fact FROM @load_stage")
	}
	// COPY INTO @out_stage FROM fact → writes to out_stage (@ stripped)
	if !hasUnresolvedRef(refs, "out_stage", types.EdgeKindWrites) {
		t.Error("expected writes edge to 'out_stage' from COPY INTO @out_stage FROM fact")
	}
	// COPY INTO @out_stage FROM fact → references to fact
	if !hasUnresolvedRef(refs, "fact", types.EdgeKindReferences) {
		t.Error("expected references edge to 'fact' from COPY INTO @out_stage FROM fact")
	}
}

// TestA2CopyIntoBodyVsTopLevel is a discriminating test: a file with both a
// standalone top-level COPY INTO AND a procedure body COPY INTO. The body COPY
// must be owned by the procedure node; the top-level COPY must be owned by the
// script node (F4: lazy script node for standalone top-level COPYs).
//
// WHY: v1 only captured COPY INTO inside routine/task bodies. F4 extends this:
// a top-level COPY (not inside any definition) is now owned by a lazily-created
// script node named by the file basename. Both copies must produce edges; the
// key assertion is that each is owned by the correct node (proc vs script).
const a2CopyIntoBodyVsTopLevelFixture = `
CREATE TABLE body_tbl (id INT);
CREATE TABLE toplevel_tbl (id INT);

-- Standalone top-level COPY (not inside any definition) — owned by script node (F4).
COPY INTO toplevel_tbl FROM @toplevel_stage;

CREATE PROCEDURE load_proc()
LANGUAGE SQL AS $$
BEGIN
  COPY INTO body_tbl FROM @body_stage;
END;
$$;
`

func TestA2CopyIntoBodyVsTopLevel(t *testing.T) {
	ext := newSQL()
	result, err := ext.Extract("/db/snowflake_a2_vs.sql", a2CopyIntoBodyVsTopLevelFixture)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	refs := result.UnresolvedReferences

	// Proc-body COPY must fire: writes body_tbl + references body_stage.
	if !hasUnresolvedRef(refs, "body_tbl", types.EdgeKindWrites) {
		t.Error("expected writes edge to 'body_tbl' from proc-body COPY INTO")
	}
	if !hasUnresolvedRef(refs, "body_stage", types.EdgeKindReferences) {
		t.Error("expected references edge to 'body_stage' from proc-body COPY INTO")
	}

	// F4: Top-level COPY must also fire, owned by the script node.
	// Confirm the script node exists and owns the top-level COPY edges.
	scriptNode := findSQLNodeExact(result.Nodes, types.NodeKindScript, "snowflake_a2_vs")
	if scriptNode == nil {
		t.Fatal("expected script node 'snowflake_a2_vs' for the top-level COPY INTO (F4)")
	}

	var topWriteOwned, topStageOwned bool
	for _, r := range refs {
		if r.ReferenceName == "toplevel_tbl" && r.ReferenceKind == types.EdgeKindWrites && r.FromNodeID == scriptNode.ID {
			topWriteOwned = true
		}
		if r.ReferenceName == "toplevel_stage" && r.ReferenceKind == types.EdgeKindReferences && r.FromNodeID == scriptNode.ID {
			topStageOwned = true
		}
	}
	if !topWriteOwned {
		t.Error("script node must own writes edge to 'toplevel_tbl' (top-level COPY INTO, F4)")
	}
	if !topStageOwned {
		t.Error("script node must own references edge to 'toplevel_stage' (top-level COPY INTO, F4)")
	}

	// The proc node must own the body COPY edges (not the script node).
	procNode := findSQLNodeExact(result.Nodes, types.NodeKindProcedure, "load_proc")
	if procNode == nil {
		t.Fatal("expected procedure node 'load_proc'")
	}
	var procWriteOwned bool
	for _, r := range refs {
		if r.ReferenceName == "body_tbl" && r.ReferenceKind == types.EdgeKindWrites && r.FromNodeID == procNode.ID {
			procWriteOwned = true
		}
	}
	if !procWriteOwned {
		t.Error("procedure node must own writes edge to 'body_tbl' (body COPY INTO, v1)")
	}
}

// TestA2CopyIntoInternalStageSkipped verifies that @~ (user stage) and @%tbl
// (table stage) sigils are SKIPPED — only named stages emit a references edge.
// WHY: @~ and @%tbl are anonymous internal Snowflake stages; they have no node
// to reference. The writes edge to the target table must still be emitted.
const a2CopyIntoInternalStageFixture = `
CREATE TABLE dest (id INT);
CREATE PROCEDURE copy_from_internal()
LANGUAGE SQL AS $$
BEGIN
  COPY INTO dest FROM @~/path/to/file.csv;
  COPY INTO dest FROM @%othertbl/path;
END;
$$;
`

func TestA2CopyIntoInternalStageSkipped(t *testing.T) {
	ext := newSQL()
	result, err := ext.Extract("/db/snowflake_a2_internal.sql", a2CopyIntoInternalStageFixture)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	refs := result.UnresolvedReferences

	// writes to dest must still be emitted (COPY INTO dest FROM @~...)
	if !hasUnresolvedRef(refs, "dest", types.EdgeKindWrites) {
		t.Error("expected writes edge to 'dest' even when source is an internal stage")
	}

	// @~ user stage: must NOT emit any reference to "~" or similar
	if countUnresolvedRefs(refs, "~") > 0 {
		t.Error("@~ user-stage must not emit a references edge to '~'")
	}
	// @%othertbl table stage: must NOT emit a references edge to "othertbl" or the raw sigil token.
	// (The sigil form is @<percent>tbl — a Snowflake table-stage reference, not a named stage.)
	if countUnresolvedRefs(refs, "othertbl") > 0 {
		t.Error("table-stage sigil must not emit a references edge to 'othertbl'")
	}
	if countUnresolvedRefs(refs, "%othertbl") > 0 {
		t.Error("table-stage sigil must not emit a references edge to the raw sigil token")
	}
}

// ---------------------------------------------------------------------------
// A6 — CLONE
// ---------------------------------------------------------------------------

// TestA6Clone verifies CREATE OR REPLACE TRANSIENT TABLE <new> CLONE <src>
// emits a table node for <new> + a references edge new→src.
// A6 spec: the body has no FROM; CLONE is the only lineage signal.
const a6CloneFixture = `
CREATE OR REPLACE TRANSIENT TABLE staging CLONE prod;
`

func TestA6Clone(t *testing.T) {
	ext := newSQL()
	result, err := ext.Extract("/db/snowflake_a6.sql", a6CloneFixture)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	stagingNode := findSQLNodeExact(result.Nodes, types.NodeKindTable, "staging")
	if stagingNode == nil {
		t.Fatal("expected table node 'staging' from CREATE OR REPLACE TRANSIENT TABLE staging CLONE prod")
	}
	if !hasUnresolvedRef(result.UnresolvedReferences, "prod", types.EdgeKindReferences) {
		t.Error("expected references edge 'staging'→'prod' from CLONE clause")
	}
}

// TestA6CloneView verifies CLONE also works on CREATE VIEW.
const a6CloneViewFixture = `
CREATE OR REPLACE VIEW v_clone CLONE v_original;
`

func TestA6CloneView(t *testing.T) {
	ext := newSQL()
	result, err := ext.Extract("/db/snowflake_a6_view.sql", a6CloneViewFixture)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if findSQLNodeExact(result.Nodes, types.NodeKindView, "v_clone") == nil {
		t.Fatal("expected view node 'v_clone' from CREATE OR REPLACE VIEW v_clone CLONE v_original")
	}
	if !hasUnresolvedRef(result.UnresolvedReferences, "v_original", types.EdgeKindReferences) {
		t.Error("expected references edge 'v_clone'→'v_original' from CLONE clause")
	}
}

// TestCloneFPColumnNamedClone guards the A6 false-positive: a column literally
// named CLONE inside a CREATE TABLE body must NOT produce a references edge.
// WHY: cloneRE is scanned over preamble text before '(', so column definitions
// inside the body are never matched — a real CLONE statement has no column list.
const cloneFPColumnNamedCloneFixture = `
CREATE TABLE t (
    CLONE INT,
    id   INT
);
`

func TestCloneFPColumnNamedClone(t *testing.T) {
	ext := newSQL()
	result, err := ext.Extract("/db/snowflake_a6_fp.sql", cloneFPColumnNamedCloneFixture)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	// Must produce a table node for 't'
	if findSQLNodeExact(result.Nodes, types.NodeKindTable, "t") == nil {
		t.Error("expected table node 't'")
	}
	// Must NOT produce a references edge to 'INT' (from the CLONE column definition)
	if hasUnresolvedRef(result.UnresolvedReferences, "INT", types.EdgeKindReferences) {
		t.Error("column named CLONE inside table body must not produce a references edge to 'INT'")
	}
	// Must NOT produce any references edge at all (no real CLONE source here)
	for _, r := range result.UnresolvedReferences {
		if r.ReferenceKind == types.EdgeKindReferences {
			t.Errorf("expected no references edges from table with CLONE column; got references to %q", r.ReferenceName)
		}
	}
}

// ---------------------------------------------------------------------------
// Part B — dbt Jinja pre-pass
// ---------------------------------------------------------------------------

// B4 + B1 gate: a plain SQL file (no Jinja) must produce ZERO model nodes and
// identical results to before the dbt pre-pass was added.
// WHY: B1 says the pre-pass is a no-op when source has no {{ / {% / {#. We use
// the existing pgFixture (Postgres DDL) as the "before" baseline.
const b1PlainSQLFixture = `
CREATE TABLE plain_tbl (id INT);
CREATE VIEW plain_view AS SELECT id FROM plain_tbl;
`

func TestB1PlainSQLNoModelNode(t *testing.T) {
	ext := newSQL()
	result, err := ext.Extract("/models/plain.sql", b1PlainSQLFixture)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	// No model node must be created for a plain SQL file.
	for _, n := range result.Nodes {
		if n.Kind == types.NodeKindModel {
			t.Errorf("plain SQL file must not produce a model node; got model %q", n.Name)
		}
	}
	// Normal nodes must still be extracted.
	if findSQLNodeExact(result.Nodes, types.NodeKindTable, "plain_tbl") == nil {
		t.Error("expected table node 'plain_tbl' from plain SQL")
	}
	if findSQLNodeExact(result.Nodes, types.NodeKindView, "plain_view") == nil {
		t.Error("expected view node 'plain_view' from plain SQL")
	}
}

// B4: model node is named by the file basename without extension.
// Input path /models/staging/stg_orders.sql → model name "stg_orders".
const b4ModelNodeFixture = `
SELECT order_id FROM {{ ref('raw_orders') }}
`

func TestB4ModelNodeBasename(t *testing.T) {
	ext := newSQL()
	result, err := ext.Extract("/models/staging/stg_orders.sql", b4ModelNodeFixture)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	model := findSQLNodeExact(result.Nodes, types.NodeKindModel, "stg_orders")
	if model == nil {
		t.Fatal("expected model node named 'stg_orders' (basename without extension of /models/staging/stg_orders.sql)")
	}
}

// B2: five ref() grammar forms in one fixture (includes dbt 1.5+ cross-project versioned ref).
// Highest-risk assertions:
//   - ref('pkg','stg_orders') → edge target is 'stg_orders', NOT 'pkg'.
//   - ref('pkg','stg_orders', v=3) → edge target is 'stg_orders_v3' (E4 versioned suffix).
//   - ref('pkg','stg_orders', version=3) → same.
//
// The test only asserts that 'stg_orders' (bare) exists — satisfied by the first
// unversioned form. E4 versioned-target assertions live in TestE4VersionedRefDistinctTargets.
//
// WHY the two-positional-plus-version forms: dbt 1.5 introduced cross-project
// versioned refs where both a package AND a version= keyword co-exist. The old
// regex had v= as an alternative INSIDE the second-arg group, making it structurally
// impossible to match both group 2 AND a trailing version=. The fix makes version=
// an independent trailing optional group.
const b2RefGrammarFixture = `
-- single literal
SELECT * FROM {{ ref('stg_orders') }}
-- two literals: (package, model); model name is SECOND
JOIN {{ ref('pkg','stg_orders') }} ON true
-- version keyword arg alone: ignored, model name is the first positional literal
JOIN {{ ref('stg_orders', v=2) }} ON true
-- two positional PLUS version= (dbt 1.5+ cross-project versioned ref)
JOIN {{ ref('pkg', 'stg_orders', v=3) }} ON true
JOIN {{ ref('pkg', 'stg_orders', version=3) }} ON true
`

func TestB2RefGrammarThreeForms(t *testing.T) {
	ext := newSQL()
	result, err := ext.Extract("/models/b2_test.sql", b2RefGrammarFixture)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	refs := result.UnresolvedReferences

	// The single-literal form ref('stg_orders') → target "stg_orders" (unchanged).
	// Versioned forms now produce distinct targets per E4:
	//   ref('stg_orders', v=2)             → "stg_orders_v2"
	//   ref('pkg', 'stg_orders', v=3)      → "stg_orders_v3"
	//   ref('pkg', 'stg_orders', version=3) → "stg_orders_v3" (deduped)
	// This assertion verifies the unversioned form; see TestE4VersionedRefDistinctTargets
	// for the versioned-target assertions.
	if !hasUnresolvedRef(refs, "stg_orders", types.EdgeKindReferences) {
		t.Error("expected references edge to 'stg_orders' from unversioned ref('stg_orders')")
	}

	// Critical: 'pkg' (the package arg) must NOT appear as an edge target.
	if countUnresolvedRefs(refs, "pkg") > 0 {
		t.Errorf("package arg 'pkg' must not appear as edge target; got %d ref(s)", countUnresolvedRefs(refs, "pkg"))
	}
}

// B2 Jinja-comment exclusion: a ref() inside {# ... #} must NOT be harvested.
// WHY: spec B2 says harvest runs "after removing {# … #} comments". A ref inside
// a comment is intentionally disabled — emitting its edge would be wrong.
const b2RefInJinjaCommentFixture = `
SELECT id FROM real_tbl
{# This ref is commented out: {{ ref('commented_out') }} #}
`

func TestB2RefInJinjaCommentNotHarvested(t *testing.T) {
	ext := newSQL()
	result, err := ext.Extract("/models/b2_comment.sql", b2RefInJinjaCommentFixture)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if countUnresolvedRefs(result.UnresolvedReferences, "commented_out") > 0 {
		t.Error("ref() inside {# ... #} must not produce a references edge to 'commented_out'")
	}
}

// B2 (inside Jinja block): a ref() inside {% if is_incremental() %} is still captured.
const b2RefInsideJinjaBlockFixture = `
SELECT id FROM base_tbl
{% if is_incremental() %}
  WHERE updated_at > (SELECT MAX(updated_at) FROM {{ ref('events') }})
{% endif %}
`

func TestB2RefInsideJinjaBlock(t *testing.T) {
	ext := newSQL()
	result, err := ext.Extract("/models/b2_incremental.sql", b2RefInsideJinjaBlockFixture)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if !hasUnresolvedRef(result.UnresolvedReferences, "events", types.EdgeKindReferences) {
		t.Error("expected references edge to 'events' from ref() inside {% if is_incremental() %}")
	}
}

// B3: source() harvest — always 2 args; edge target is "schema.table".
const b3SourceFixture = `
SELECT * FROM {{ source('raw', 'orders') }}
`

func TestB3SourceHarvest(t *testing.T) {
	ext := newSQL()
	result, err := ext.Extract("/models/b3_source.sql", b3SourceFixture)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if !hasUnresolvedRef(result.UnresolvedReferences, "raw.orders", types.EdgeKindReferences) {
		t.Error("expected references edge to 'raw.orders' from {{ source('raw','orders') }}")
	}
}

// B5: placeholder + residual scan.
// A model SELECT with ref() and a real table join: both must appear as edges
// owned by the model node. No __dbt_* name may survive in unresolved references.
const b5PlaceholderResidualFixture = `
SELECT s.id, r.amount
FROM {{ ref('stg') }} s
JOIN real_tbl r ON s.id = r.id
`

func TestB5PlaceholderResidual(t *testing.T) {
	ext := newSQL()
	result, err := ext.Extract("/models/b5_residual.sql", b5PlaceholderResidualFixture)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	refs := result.UnresolvedReferences

	// B2 harvest: references edge to 'stg'
	if !hasUnresolvedRef(refs, "stg", types.EdgeKindReferences) {
		t.Error("expected references edge to 'stg' from {{ ref('stg') }}")
	}

	// B5 residual scan: references edge to 'real_tbl' (from the JOIN)
	if !hasUnresolvedRef(refs, "real_tbl", types.EdgeKindReferences) {
		t.Error("expected references edge to 'real_tbl' from JOIN real_tbl in residual body scan")
	}

	// No __dbt_* names must survive
	for _, r := range refs {
		if strings.HasPrefix(r.ReferenceName, "__dbt_ref_") || strings.HasPrefix(r.ReferenceName, "__dbt_src_") {
			t.Errorf("__dbt_* placeholder reference must not survive in final refs; got %q", r.ReferenceName)
		}
	}
}

// B5 + B4: model node owns the residual edges (model node must exist, and the
// references edges must be owned by it — same FromNodeID).
func TestB5ModelOwnsResidualEdges(t *testing.T) {
	ext := newSQL()
	result, err := ext.Extract("/models/b5_residual.sql", b5PlaceholderResidualFixture)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	model := findSQLNodeExact(result.Nodes, types.NodeKindModel, "b5_residual")
	if model == nil {
		t.Fatal("expected model node 'b5_residual'")
	}

	// Both 'stg' and 'real_tbl' references must be owned by the model node.
	modelRefs := make(map[string]bool)
	for _, r := range result.UnresolvedReferences {
		if r.FromNodeID == model.ID {
			modelRefs[r.ReferenceName] = true
		}
	}
	if !modelRefs["stg"] {
		t.Error("references to 'stg' must be owned by the model node (same FromNodeID)")
	}
	if !modelRefs["real_tbl"] {
		t.Error("references to 'real_tbl' must be owned by the model node (same FromNodeID)")
	}
}

// ---------------------------------------------------------------------------
// B5 Jinja comment stripping in residual (jaffle-shop regression)
// ---------------------------------------------------------------------------

// B5JinjaCommentResidualFromJoin: a model whose {#- ... -#} block comment
// contains the word "from" followed by prose words must NOT emit references
// edges to those prose words. Only the real {{ ref('raw_x') }} must appear.
//
// WHY: the B5 residual was built from `source` (raw), so {# … #} comment text
// survived into scanBodyEdges. "select from the table" inside the comment
// produced a spurious `references | the` edge (jaffle-shop-classic regression).
// Fix: start the residual from rawForHarvest (comments already blanked).
const b5JinjaCommentFromProseFixture = `
with source as (
    {#-
    Normally we would select from the table here, but we are using seeds to
    keep it simple — so this query is intentionally minimal.
    -#}
    select * from {{ ref('raw_x') }}
)
select * from source
`

func TestB5JinjaCommentResidualFromProse(t *testing.T) {
	ext := newSQL()
	result, err := ext.Extract("/models/stg_customers.sql", b5JinjaCommentFromProseFixture)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	refs := result.UnresolvedReferences

	// Must have exactly one references edge: to raw_x.
	if !hasUnresolvedRef(refs, "raw_x", types.EdgeKindReferences) {
		t.Error("expected exactly one references edge to 'raw_x' from {{ ref('raw_x') }}")
	}

	// Comment prose words that follow "from" / "join" inside {# #} must NOT appear.
	for _, badWord := range []string{"the", "table", "here", "seeds", "simple", "so", "this", "query", "is", "intentionally", "minimal"} {
		if countUnresolvedRefs(refs, badWord) > 0 {
			t.Errorf("spurious references edge to comment-prose word %q — Jinja comment text must not leak into residual body scan", badWord)
		}
	}
}

// B5JinjaCommentResidualJoinProse: same class of bug but the comment prose
// contains a word after "join". Verifies join-keyword path is also clean.
const b5JinjaCommentJoinProseFixture = `
with base as (
    {#
    We normally join other_table here, but we are bypassing it.
    #}
    select id from {{ ref('raw_orders') }}
)
select * from base
`

func TestB5JinjaCommentResidualJoinProse(t *testing.T) {
	ext := newSQL()
	result, err := ext.Extract("/models/stg_orders.sql", b5JinjaCommentJoinProseFixture)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	refs := result.UnresolvedReferences

	// Must have the real ref edge.
	if !hasUnresolvedRef(refs, "raw_orders", types.EdgeKindReferences) {
		t.Error("expected references edge to 'raw_orders'")
	}

	// "other_table" is inside a {# #} comment after "join" — must not appear.
	if countUnresolvedRefs(refs, "other_table") > 0 {
		t.Error("spurious references edge to 'other_table' from join-prose inside {# #} comment")
	}
}

// ---------------------------------------------------------------------------
// A4 — CREATE STREAM
// ---------------------------------------------------------------------------

// TestA4CreateStreamOnTable verifies CREATE STREAM <name> ON TABLE <source>
// emits a stream node + a references edge to the source table.
// A4 spec success criterion: `CREATE STREAM s ON TABLE orders` → stream node `s`
// + references to `orders`.
const a4StreamOnTableFixture = `
CREATE STREAM s ON TABLE orders;
`

func TestA4CreateStreamOnTable(t *testing.T) {
	ext := newSQL()
	result, err := ext.Extract("/db/snowflake_a4.sql", a4StreamOnTableFixture)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	streamNode := findSQLNodeExact(result.Nodes, types.NodeKindStream, "s")
	if streamNode == nil {
		t.Fatal("expected stream node 's' from CREATE STREAM s ON TABLE orders")
	}
	if !hasUnresolvedRef(result.UnresolvedReferences, "orders", types.EdgeKindReferences) {
		t.Error("expected references edge to 'orders' from CREATE STREAM ON TABLE")
	}
}

// TestA4CreateStreamVariants verifies all ON <object-kind> variants produce a
// stream node + references edge. A4 spec: TABLE, VIEW, EXTERNAL TABLE, STAGE,
// DYNAMIC TABLE, EVENT TABLE all match.
const a4StreamVariantsFixture = `
CREATE OR REPLACE STREAM s_view ON VIEW v_orders;
CREATE STREAM IF NOT EXISTS s_ext ON EXTERNAL TABLE ext_orders;
CREATE OR REPLACE STREAM s_stage ON STAGE my_stage;
CREATE STREAM s_dyn ON DYNAMIC TABLE dyn_orders;
CREATE STREAM s_evt ON EVENT TABLE evt_orders;
`

func TestA4CreateStreamVariants(t *testing.T) {
	ext := newSQL()
	result, err := ext.Extract("/db/snowflake_a4_variants.sql", a4StreamVariantsFixture)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	cases := []struct {
		streamName string
		sourceName string
	}{
		{"s_view", "v_orders"},
		{"s_ext", "ext_orders"},
		{"s_stage", "my_stage"},
		{"s_dyn", "dyn_orders"},
		{"s_evt", "evt_orders"},
	}
	for _, c := range cases {
		if findSQLNodeExact(result.Nodes, types.NodeKindStream, c.streamName) == nil {
			t.Errorf("expected stream node %q", c.streamName)
		}
		if !hasUnresolvedRef(result.UnresolvedReferences, c.sourceName, types.EdgeKindReferences) {
			t.Errorf("expected references edge to %q from stream %q", c.sourceName, c.streamName)
		}
	}
}

// ---------------------------------------------------------------------------
// A3 — CREATE TASK
// ---------------------------------------------------------------------------

// TestA3CreateTaskAfterAndBody is the primary A3 success-criterion test.
// It guards the keyword-denylist trap: the word AFTER is in sqlKeywordsForRef
// so AFTER predecessors must NOT be routed through scanBodyEdges — a dedicated
// AFTER-predecessor regex must handle them.
//
// Fixture: CREATE TASK load_t AFTER stg_t, dim_t AS INSERT INTO fact SELECT * FROM stg
// Expected:
//   - task node load_t
//   - references to stg_t (AFTER predecessor 1)
//   - references to dim_t (AFTER predecessor 2)
//   - writes to fact (INSERT INTO)
//   - references to stg (FROM clause)
const a3TaskAfterAndBodyFixture = `
CREATE TASK load_t
  AFTER stg_t, dim_t
  AS INSERT INTO fact SELECT * FROM stg;
`

func TestA3CreateTaskAfterAndBody(t *testing.T) {
	ext := newSQL()
	result, err := ext.Extract("/db/snowflake_a3.sql", a3TaskAfterAndBodyFixture)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	taskNode := findSQLNodeExact(result.Nodes, types.NodeKindTask, "load_t")
	if taskNode == nil {
		t.Fatal("expected task node 'load_t' from CREATE TASK load_t")
	}

	refs := result.UnresolvedReferences

	// AFTER predecessor edges — these MUST be emitted via a dedicated regex,
	// not scanBodyEdges, because AFTER is in sqlKeywordsForRef.
	if !hasUnresolvedRef(refs, "stg_t", types.EdgeKindReferences) {
		t.Error("expected references edge to AFTER predecessor 'stg_t' (keyword-denylist trap guard)")
	}
	if !hasUnresolvedRef(refs, "dim_t", types.EdgeKindReferences) {
		t.Error("expected references edge to AFTER predecessor 'dim_t' (keyword-denylist trap guard)")
	}

	// Body edges via scanBodyEdges.
	if !hasUnresolvedRef(refs, "fact", types.EdgeKindWrites) {
		t.Error("expected writes edge to 'fact' from INSERT INTO in task body")
	}
	if !hasUnresolvedRef(refs, "stg", types.EdgeKindReferences) {
		t.Error("expected references edge to 'stg' from FROM clause in task body")
	}
}

// TestA3CreateTaskOrReplace verifies CREATE OR REPLACE TASK [IF NOT EXISTS] <name>
// still produces a task node.
const a3TaskOrReplaceFixture = `
CREATE OR REPLACE TASK IF NOT EXISTS my_task
  AS SELECT 1;
`

func TestA3CreateTaskOrReplace(t *testing.T) {
	ext := newSQL()
	result, err := ext.Extract("/db/snowflake_a3_orreplace.sql", a3TaskOrReplaceFixture)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if findSQLNodeExact(result.Nodes, types.NodeKindTask, "my_task") == nil {
		t.Error("expected task node 'my_task' from CREATE OR REPLACE TASK IF NOT EXISTS my_task")
	}
}

// TestA3CreateTaskCallBody verifies AS CALL <proc>(...) in the task body emits
// a calls edge via scanBodyEdges.
const a3TaskCallBodyFixture = `
CREATE TASK etl_task
  AS CALL load_proc();
`

func TestA3CreateTaskCallBody(t *testing.T) {
	ext := newSQL()
	result, err := ext.Extract("/db/snowflake_a3_call.sql", a3TaskCallBodyFixture)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if findSQLNodeExact(result.Nodes, types.NodeKindTask, "etl_task") == nil {
		t.Fatal("expected task node 'etl_task'")
	}
	if !hasUnresolvedRef(result.UnresolvedReferences, "load_proc", types.EdgeKindCalls) {
		t.Error("expected calls edge to 'load_proc' from AS CALL in task body")
	}
}

// ---------------------------------------------------------------------------
// Part C — O3 syntax-tolerance guards
// ---------------------------------------------------------------------------

// TestC1QualifyDoesNotTruncateOrEmitRef verifies that a QUALIFY clause in a
// view body does NOT truncate body-edge scanning or emit a 'QUALIFY' reference.
// WHY (C1): QUALIFY is a Snowflake window-filter clause placed after WHERE. A
// naive scanner might treat QUALIFY as a keyword that breaks statement parsing,
// causing FROM-clause edges that follow to be dropped. The extractor must scan
// past QUALIFY transparently and still emit the FROM edge.
const c1QualifyFixture = `
CREATE VIEW ranked_orders AS
SELECT
    id,
    amount,
    ROW_NUMBER() OVER (PARTITION BY customer_id ORDER BY created_at DESC) AS rn
FROM orders o
WHERE amount > 0
QUALIFY ROW_NUMBER() OVER (PARTITION BY customer_id ORDER BY created_at DESC) = 1;
`

func TestC1QualifyDoesNotTruncateOrEmitRef(t *testing.T) {
	ext := newSQL()
	result, err := ext.Extract("/db/snowflake_c1.sql", c1QualifyFixture)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	refs := result.UnresolvedReferences

	// FROM edge to 'orders' must be emitted — QUALIFY must not truncate scanning.
	if !hasUnresolvedRef(refs, "orders", types.EdgeKindReferences) {
		t.Error("expected references edge to 'orders' from FROM clause; QUALIFY must not truncate body-edge scanning")
	}

	// No 'QUALIFY' reference must appear — it is a SQL clause keyword, not a table.
	if countUnresolvedRefs(refs, "QUALIFY") > 0 || countUnresolvedRefs(refs, "qualify") > 0 {
		t.Error("QUALIFY must not produce a references edge (it is a Snowflake window-filter clause, not a table name)")
	}
}

// TestC2ColonColonCastNoSpuriousRef verifies that Snowflake :: cast syntax does
// NOT emit a spurious reference to the cast type and does NOT corrupt the
// identifier that precedes the cast operator.
// WHY (C2): 'col::VARIANT' is read as identifier 'col' followed by the Snowflake
// cast operator '::' and the type name 'VARIANT'. A regex that greedily matches
// identifier characters after FROM could capture 'VARIANT' or 'NUMBER' as a
// table-like name. The extractor must emit only the real FROM source table.
const c2ColonColonCastFixture = `
CREATE VIEW cast_view AS
SELECT col::VARIANT, x::NUMBER(10,2), raw::TEXT
FROM src_tbl;
`

func TestC2ColonColonCastNoSpuriousRef(t *testing.T) {
	ext := newSQL()
	result, err := ext.Extract("/db/snowflake_c2.sql", c2ColonColonCastFixture)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	refs := result.UnresolvedReferences

	// The real FROM source must still appear.
	if !hasUnresolvedRef(refs, "src_tbl", types.EdgeKindReferences) {
		t.Error("expected references edge to 'src_tbl' from FROM clause in cast_view")
	}

	// Cast type names must NOT appear as reference targets.
	for _, badName := range []string{"VARIANT", "variant", "NUMBER", "number", "TEXT", "text"} {
		if countUnresolvedRefs(refs, badName) > 0 {
			t.Errorf("cast type %q must not appear as a reference target (it is a type name, not a table)", badName)
		}
	}

	// 'col' itself must NOT appear as a reference target (it is a column, not a FROM source).
	if countUnresolvedRefs(refs, "col") > 0 {
		t.Error("column name 'col' must not appear as a reference target")
	}
}

// TestC3DollarColumnNoRef verifies that Snowflake positional column references
// ($1, $2, …) in a SELECT/WHERE list do NOT produce spurious reference edges.
// A real table is the FROM source so the positive anchor proves the extractor
// actually processed the statement; the $N negatives are then meaningful.
// WHY (C3): sqlQNameRaw starts with [A-Za-z_] so bare '$1' already cannot match
// the FROM/JOIN name capture group. This test locks that guarantee in.
const c3DollarColumnFixture = `
CREATE VIEW staged_view AS
SELECT t.$1, t.$2
FROM real_tbl t
WHERE t.$3 > 0;
`

func TestC3DollarColumnNoRef(t *testing.T) {
	ext := newSQL()
	result, err := ext.Extract("/db/snowflake_c3.sql", c3DollarColumnFixture)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	refs := result.UnresolvedReferences

	// Positive anchor: real_tbl must appear as a references edge, proving the
	// body scan ran and is not trivially empty.
	if !hasUnresolvedRef(refs, "real_tbl", types.EdgeKindReferences) {
		t.Error("expected references edge to 'real_tbl' from FROM clause (positive anchor — proves scan ran)")
	}

	// Negatives: no reference named $1, $2, $3, or their bare digit forms.
	for _, badName := range []string{"$1", "$2", "$3", "1", "2", "3"} {
		if countUnresolvedRefs(refs, badName) > 0 {
			t.Errorf("positional column ref %q must not produce a reference edge", badName)
		}
	}
}

// TestC4FlattenNoEdge verifies that LATERAL FLATTEN and TABLE(FLATTEN(...))
// patterns in a FROM/JOIN list do NOT produce edges to 'FLATTEN', 'LATERAL',
// or 'TABLE', and DO produce a references edge to the real source table.
// WHY (C4): 'LATERAL' is already in sqlKeywordsForRef (F-6 guard). 'FLATTEN'
// is a Snowflake table function, not a real table — it must be suppressed by
// the keyword filter. 'TABLE' is also a SQL keyword, not a table name.
// This test complements TestF6LateralNoEdge (Postgres LATERAL/unnest) for the
// Snowflake FLATTEN variant.
const c4FlattenFixture = `
CREATE VIEW flatten_view AS
SELECT t.col_val, f.value, f.index
FROM src_tbl t, LATERAL FLATTEN(INPUT => t.col_val) f;
`

const c4TableFlattenFixture = `
CREATE VIEW table_flatten_view AS
SELECT t.col_val, f.value
FROM src_tbl t
JOIN TABLE(FLATTEN(INPUT => t.col_val)) f ON true;
`

// ---------------------------------------------------------------------------
// D1 / D2 — .sql.jinja ingestion
// ---------------------------------------------------------------------------

// TestD1D2SqlJinjaModelNode verifies that a .sql.jinja file:
//   - D1: is accepted by Extract (the router gets it here via IsSQLExt)
//   - D2: produces a model node whose Name is the basename WITHOUT the whole
//     ".sql.jinja" compound suffix (→ "stg", not "stg.sql")
//   - ref() DAG is identical to what the .sql twin would produce
//
// WHY the exact-name assertion: dbt resolves {{ ref('stg') }} to the node
// named 'stg'. If the model node is named 'stg.sql' instead, the ref edge
// from a sibling model never resolves — silent lineage gap.
const d1D2SqlJinjaFixture = `
{{ config(materialized='table') }}

SELECT *
FROM {{ ref('base_orders') }}
WHERE status = 'active'
`

func TestD1D2SqlJinjaModelNode(t *testing.T) {
	ext := newSQL()

	// .sql twin: same content via a .sql path — model name must be "stg".
	result, err := ext.Extract("models/stg.sql", d1D2SqlJinjaFixture)
	if err != nil {
		t.Fatalf("Extract (.sql twin): %v", err)
	}
	sqlModel := findSQLNodeExact(result.Nodes, types.NodeKindModel, "stg")
	if sqlModel == nil {
		t.Fatal(".sql twin: expected model node named 'stg'")
	}
	if !hasUnresolvedRef(result.UnresolvedReferences, "base_orders", types.EdgeKindReferences) {
		t.Error(".sql twin: expected references edge to 'base_orders'")
	}

	// .sql.jinja: must produce the same model name and ref DAG.
	result2, err := ext.Extract("models/stg.sql.jinja", d1D2SqlJinjaFixture)
	if err != nil {
		t.Fatalf("Extract (.sql.jinja): %v", err)
	}

	// D2: model node must be named "stg", not "stg.sql".
	jinjaModel := findSQLNodeExact(result2.Nodes, types.NodeKindModel, "stg")
	if jinjaModel == nil {
		// Produce a helpful diagnosis showing what was actually created.
		var names []string
		for _, n := range result2.Nodes {
			if n.Kind == types.NodeKindModel {
				names = append(names, n.Name)
			}
		}
		t.Fatalf(".sql.jinja: expected model node named 'stg' (D2: strip full .sql.jinja suffix); got model names: %v", names)
	}

	// Ref DAG must be identical: 'base_orders' referenced.
	if !hasUnresolvedRef(result2.UnresolvedReferences, "base_orders", types.EdgeKindReferences) {
		t.Error(".sql.jinja: expected references edge to 'base_orders' (ref DAG identical to .sql twin)")
	}
}

// ---------------------------------------------------------------------------
// Part E — dbt macros and project awareness (E1 / E2 / E3)
// ---------------------------------------------------------------------------

// E1 — path-role detection.
//
// macros/util.sql with {% macro u() %}…{% endmacro %} must:
//   - produce a macro node named "u"
//   - NOT produce a model node
//
// models/stg.sql with Jinja must produce a model node (v1 non-regression).
const e1MacroFileFixture = `
{% macro u() %}
  SELECT 1
{% endmacro %}
`

const e1ModelFileFixture = `
SELECT * FROM {{ ref('base') }}
`

func TestE1PathRoleMacroFile(t *testing.T) {
	ext := newSQL()

	// macros/ role: should produce macro node, no model node.
	result, err := ext.Extract("macros/util.sql", e1MacroFileFixture)
	if err != nil {
		t.Fatalf("Extract (macros/util.sql): %v", err)
	}

	// Must have a macro node named "u".
	macroNode := findSQLNodeExact(result.Nodes, types.NodeKindMacro, "u")
	if macroNode == nil {
		t.Fatal("macros/util.sql: expected macro node 'u' from {% macro u() %}")
	}

	// Must NOT have a model node.
	for _, n := range result.Nodes {
		if n.Kind == types.NodeKindModel {
			t.Errorf("macros/util.sql: must NOT produce a model node; got model %q", n.Name)
		}
	}
}

func TestE1PathRoleModelFile(t *testing.T) {
	ext := newSQL()

	// models/ role: should produce model node as in v1 (no regression).
	result, err := ext.Extract("models/stg.sql", e1ModelFileFixture)
	if err != nil {
		t.Fatalf("Extract (models/stg.sql): %v", err)
	}

	model := findSQLNodeExact(result.Nodes, types.NodeKindModel, "stg")
	if model == nil {
		t.Fatal("models/stg.sql: expected model node 'stg' (v1 behaviour, no regression)")
	}
}

// E2 — macro nodes + call edges with guards.
//
// Fixture has four invocations:
//   - {{ my_macro() }}         → calls edge to my_macro (local macro call)
//   - {{ dbt_utils.star() }}   → package-qualified, NO edge
//   - {{ ref('x') }}           → denylist, NO calls edge
//   - {{ config(...) }}        → denylist, NO calls edge
//
// Exactly one calls edge must be emitted (to my_macro).
const e2FalseEdgeGuardFixture = `
SELECT
  {{ my_macro() }},
  {{ dbt_utils.star(from=ref('orders')) }},
  {{ ref('x') }},
  {{ config(materialized='table') }}
FROM {{ ref('base_tbl') }}
`

func TestE2FalseEdgeGuard(t *testing.T) {
	ext := newSQL()
	result, err := ext.Extract("models/e2_guard.sql", e2FalseEdgeGuardFixture)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	refs := result.UnresolvedReferences

	// Must have a calls edge to my_macro.
	if !hasUnresolvedRef(refs, "my_macro", types.EdgeKindCalls) {
		t.Error("expected calls edge to 'my_macro' from {{ my_macro() }}")
	}

	// dbt_utils.star is package-qualified — no edge.
	if countUnresolvedRefs(refs, "dbt_utils") > 0 {
		t.Error("package 'dbt_utils' must not appear as an edge target (package-qualified skip)")
	}
	if countUnresolvedRefs(refs, "star") > 0 {
		t.Error("'star' from dbt_utils.star() must not appear as an edge target (package-qualified skip)")
	}

	// ref / config are denylist — no calls edge.
	if hasUnresolvedRef(refs, "ref", types.EdgeKindCalls) {
		t.Error("'ref' is in the denylist; must not produce a calls edge")
	}
	if hasUnresolvedRef(refs, "config", types.EdgeKindCalls) {
		t.Error("'config' is in the denylist; must not produce a calls edge")
	}

	// Count total calls edges — must be exactly 1 (my_macro only).
	callsCount := 0
	for _, r := range refs {
		if r.ReferenceKind == types.EdgeKindCalls {
			callsCount++
		}
	}
	if callsCount != 1 {
		t.Errorf("expected exactly 1 calls edge (to my_macro); got %d", callsCount)
	}
}

// E3 — macro-body ref ownership.
//
// Fixture: a model file with two refs:
//   - top-level {{ ref('a') }}            → owned by the MODEL node
//   - inside {% macro m() %}{{ ref('b') }}{% endmacro %} → owned by macro node m
//
// We assert the FromNodeID of each UnresolvedReference to verify ownership.
// WHY FromNodeID: presence alone doesn't tell us who owns the ref — only
// FromNodeID distinguishes "ref 'a' owned by model" from "ref 'a' owned by
// macro m". The test must fail when ownership is wrong.
const e3SpanBoundaryFixture = `
SELECT * FROM {{ ref('a') }}
{% macro m() %}
  SELECT * FROM {{ ref('b') }}
{% endmacro %}
`

func TestE3SpanBoundaryOwnership(t *testing.T) {
	ext := newSQL()
	result, err := ext.Extract("models/e3_span.sql", e3SpanBoundaryFixture)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	// Model node must exist.
	modelNode := findSQLNodeExact(result.Nodes, types.NodeKindModel, "e3_span")
	if modelNode == nil {
		t.Fatal("expected model node 'e3_span'")
	}

	// Macro node m must exist.
	macroNode := findSQLNodeExact(result.Nodes, types.NodeKindMacro, "m")
	if macroNode == nil {
		t.Fatal("expected macro node 'm' from {% macro m() %}")
	}

	refs := result.UnresolvedReferences

	// ref('a') is OUTSIDE all macro spans → must be owned by the model node.
	var refAOwner string
	for _, r := range refs {
		if r.ReferenceName == "a" && r.ReferenceKind == types.EdgeKindReferences {
			refAOwner = r.FromNodeID
		}
	}
	if refAOwner == "" {
		t.Fatal("expected references edge to 'a' (top-level ref outside macro body)")
	}
	if refAOwner != modelNode.ID {
		t.Errorf("ref('a') must be owned by model node (ID=%s); got owner ID=%s", modelNode.ID, refAOwner)
	}

	// ref('b') is INSIDE the macro body → must be owned by macro node m.
	var refBOwner string
	for _, r := range refs {
		if r.ReferenceName == "b" && r.ReferenceKind == types.EdgeKindReferences {
			refBOwner = r.FromNodeID
		}
	}
	if refBOwner == "" {
		t.Fatal("expected references edge to 'b' (ref inside macro body)")
	}
	if refBOwner != macroNode.ID {
		t.Errorf("ref('b') must be owned by macro node 'm' (ID=%s); got owner ID=%s", macroNode.ID, refBOwner)
	}

	// No model→b edge allowed; no macro→a edge allowed.
	for _, r := range refs {
		if r.ReferenceName == "b" && r.FromNodeID == modelNode.ID {
			t.Error("ref('b') inside macro body must NOT be owned by the model node")
		}
		if r.ReferenceName == "a" && r.FromNodeID == macroNode.ID {
			t.Error("ref('a') outside macro body must NOT be owned by the macro node")
		}
	}
}

// E3 unicode-comment span alignment.
//
// WHY: blankPreserveNewlines replaces every multi-byte UTF-8 rune with a
// single-byte space, so a {# comment #} containing unicode SHORTENS
// rawForHarvest relative to source. If macro spans were computed on source
// but ref offsets are into rawForHarvest, the span boundaries would be wrong
// and a ref inside the macro body would be mis-attributed to the model node.
//
// This test catches that regression: a unicode Jinja comment appears BEFORE
// the macro definition. Without the fix (compute spans on rawForHarvest),
// the macro span start in rawForHarvest coordinates is earlier than the span
// recorded from source, and ref('b') falls outside the (now-shifted) span —
// it gets attributed to the model node instead of the macro node.
const e3UnicodeCommentFixture = `
SELECT * FROM {{ ref('a') }}
{# note: café 你好 — this comment contains multi-byte UTF-8 runes #}
{% macro m() %}
  SELECT * FROM {{ ref('b') }}
{% endmacro %}
`

func TestE3UnicodeCommentSpanAlignment(t *testing.T) {
	ext := newSQL()
	result, err := ext.Extract("models/e3_unicode.sql", e3UnicodeCommentFixture)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	modelNode := findSQLNodeExact(result.Nodes, types.NodeKindModel, "e3_unicode")
	if modelNode == nil {
		t.Fatal("expected model node 'e3_unicode'")
	}
	macroNode := findSQLNodeExact(result.Nodes, types.NodeKindMacro, "m")
	if macroNode == nil {
		t.Fatal("expected macro node 'm'")
	}

	// ref('b') is INSIDE the macro body — must be owned by the macro node.
	// WHY this assertion catches the bug: if spans are computed on `source`
	// but match offsets are into rawForHarvest, the unicode comment shrinks
	// rawForHarvest and ref('b')'s offset falls BEFORE the macro span start,
	// so it would be attributed to the model node.
	var refBOwner string
	for _, r := range result.UnresolvedReferences {
		if r.ReferenceName == "b" && r.ReferenceKind == types.EdgeKindReferences {
			refBOwner = r.FromNodeID
		}
	}
	if refBOwner == "" {
		t.Fatal("expected references edge to 'b' (inside macro body after unicode comment)")
	}
	if refBOwner != macroNode.ID {
		t.Errorf("ref('b') must be owned by macro node 'm' (span-alignment bug); got owner ID=%s, macro ID=%s, model ID=%s",
			refBOwner, macroNode.ID, modelNode.ID)
	}

	// ref('a') remains owned by the model node (outside all macro spans).
	var refAOwner string
	for _, r := range result.UnresolvedReferences {
		if r.ReferenceName == "a" && r.ReferenceKind == types.EdgeKindReferences {
			refAOwner = r.FromNodeID
		}
	}
	if refAOwner == "" {
		t.Fatal("expected references edge to 'a' (top-level, outside macro)")
	}
	if refAOwner != modelNode.ID {
		t.Errorf("ref('a') must be owned by model node; got owner ID=%s", refAOwner)
	}
}

// E2 per-owner call-edge dedup.
//
// WHY: if seenCall is keyed on callee alone, only the FIRST owner (model or
// macro) gets a calls edge to my_helper — the second owner's edge is dropped.
// The dedup key must be "owner:callee" so each distinct owner gets its edge.
const e2PerOwnerCallDedupFixture = `
SELECT {{ my_helper() }}
FROM {{ ref('base') }}
{% macro wrapper() %}
  SELECT {{ my_helper() }}
{% endmacro %}
`

func TestE2PerOwnerCallDedup(t *testing.T) {
	ext := newSQL()
	result, err := ext.Extract("models/e2_dedup.sql", e2PerOwnerCallDedupFixture)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	modelNode := findSQLNodeExact(result.Nodes, types.NodeKindModel, "e2_dedup")
	if modelNode == nil {
		t.Fatal("expected model node 'e2_dedup'")
	}
	macroNode := findSQLNodeExact(result.Nodes, types.NodeKindMacro, "wrapper")
	if macroNode == nil {
		t.Fatal("expected macro node 'wrapper'")
	}

	// Both the model node AND the macro node must have a calls edge to my_helper.
	modelCallsHelper := false
	macroCallsHelper := false
	for _, r := range result.UnresolvedReferences {
		if r.ReferenceName == "my_helper" && r.ReferenceKind == types.EdgeKindCalls {
			if r.FromNodeID == modelNode.ID {
				modelCallsHelper = true
			}
			if r.FromNodeID == macroNode.ID {
				macroCallsHelper = true
			}
		}
	}
	if !modelCallsHelper {
		t.Error("model node must have a calls edge to 'my_helper' (called in top-level SELECT)")
	}
	if !macroCallsHelper {
		t.Error("macro node 'wrapper' must have a calls edge to 'my_helper' (called inside macro body)")
	}
}

// E-nonregression — existing v1 dbt model (no macros) must be unchanged.
//
// WHY: E1/E2/E3 must not disturb the happy path for plain dbt models.
// Re-using the b5PlaceholderResidualFixture (already tested for model node +
// ref DAG) with a models/ path confirms v1 behaviour is preserved.
func TestEV1NonRegression(t *testing.T) {
	ext := newSQL()
	result, err := ext.Extract("models/b5_residual.sql", b5PlaceholderResidualFixture)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	// Model node must exist (v1).
	modelNode := findSQLNodeExact(result.Nodes, types.NodeKindModel, "b5_residual")
	if modelNode == nil {
		t.Fatal("v1 non-regression: expected model node 'b5_residual'")
	}

	// ref('stg') and 'real_tbl' must be owned by the model node.
	for _, want := range []string{"stg", "real_tbl"} {
		found := false
		for _, r := range result.UnresolvedReferences {
			if r.ReferenceName == want && r.FromNodeID == modelNode.ID {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("v1 non-regression: references to %q must be owned by the model node", want)
		}
	}

	// No macro nodes must be present in a plain model file.
	for _, n := range result.Nodes {
		if n.Kind == types.NodeKindMacro {
			t.Errorf("v1 non-regression: plain model must not produce macro nodes; got %q", n.Name)
		}
	}
}

func TestC4FlattenNoEdge(t *testing.T) {
	ext := newSQL()

	// Sub-test 1: LATERAL FLATTEN(...) form.
	result, err := ext.Extract("/db/snowflake_c4_lateral.sql", c4FlattenFixture)
	if err != nil {
		t.Fatalf("Extract (lateral flatten): %v", err)
	}
	refs := result.UnresolvedReferences

	// src_tbl must be referenced (it is a real table in the FROM list).
	if !hasUnresolvedRef(refs, "src_tbl", types.EdgeKindReferences) {
		t.Error("(lateral flatten) expected references edge to 'src_tbl' from FROM clause")
	}
	// FLATTEN, LATERAL, TABLE must NOT appear as reference targets.
	for _, badName := range []string{"FLATTEN", "flatten", "LATERAL", "lateral", "TABLE", "table"} {
		if countUnresolvedRefs(refs, badName) > 0 {
			t.Errorf("(lateral flatten) %q must not produce a references edge (it is a SQL keyword/function, not a table)", badName)
		}
	}

	// Sub-test 2: TABLE(FLATTEN(...)) form.
	result2, err2 := ext.Extract("/db/snowflake_c4_table.sql", c4TableFlattenFixture)
	if err2 != nil {
		t.Fatalf("Extract (table flatten): %v", err2)
	}
	refs2 := result2.UnresolvedReferences

	if !hasUnresolvedRef(refs2, "src_tbl", types.EdgeKindReferences) {
		t.Error("(table flatten) expected references edge to 'src_tbl' from FROM clause")
	}
	for _, badName := range []string{"FLATTEN", "flatten", "LATERAL", "lateral", "TABLE", "table"} {
		if countUnresolvedRefs(refs2, badName) > 0 {
			t.Errorf("(table flatten) %q must not produce a references edge", badName)
		}
	}
}

// ---------------------------------------------------------------------------
// Part E4 — versioned refs → distinct reference target (<model>_v<N>)
// ---------------------------------------------------------------------------

// E4: three ref grammar forms in one fixture asserting versioned target names.
//
// WHY: dbt 1.5+ compiled versioned models to <model>_v<N> by default (e.g.
// ref('orders', v=2) → edge target "orders_v2"). The old regex had version=
// in a non-capturing group so N was never extracted and the target was the bare
// model name. v2 must capture N and append _v<N> to the target.
//
// Guards:
//   - No bare "orders" edge must survive for the versioned forms.
//   - The B5 placeholder-drop prefix (__dbt_ref_) must still catch
//     __dbt_ref_orders_v2 (prefix match, not exact match).
const e4VersionedRefFixture = `
-- unversioned: target = "orders"
SELECT * FROM {{ ref('orders') }}
-- v= form: target = "orders_v2"
JOIN {{ ref('orders', v=2) }} ON true
-- version= with package: target = "orders_v3"
JOIN {{ ref('pkg', 'orders', version=3) }} ON true
`

func TestE4VersionedRefDistinctTargets(t *testing.T) {
	ext := newSQL()
	result, err := ext.Extract("models/e4_versioned.sql", e4VersionedRefFixture)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	refs := result.UnresolvedReferences

	// Unversioned ref('orders') → target "orders".
	if !hasUnresolvedRef(refs, "orders", types.EdgeKindReferences) {
		t.Error("expected references edge to 'orders' from unversioned ref('orders')")
	}

	// ref('orders', v=2) → target "orders_v2".
	if !hasUnresolvedRef(refs, "orders_v2", types.EdgeKindReferences) {
		t.Error("expected references edge to 'orders_v2' from ref('orders', v=2)")
	}

	// ref('pkg', 'orders', version=3) → target "orders_v3".
	if !hasUnresolvedRef(refs, "orders_v3", types.EdgeKindReferences) {
		t.Error("expected references edge to 'orders_v3' from ref('pkg', 'orders', version=3)")
	}

	// No bare "orders" leak from a versioned ref (the versioned forms must use
	// "orders_v2"/"orders_v3", not "orders").
	// Count references to "orders" — must be exactly 1 (from the unversioned form).
	ordersCount := countUnresolvedRefs(refs, "orders")
	if ordersCount != 1 {
		t.Errorf("expected exactly 1 edge to bare 'orders' (unversioned form only); got %d", ordersCount)
	}

	// No __dbt_ref_* placeholder must survive in unresolved references.
	// WHY: the B5 prefix-drop logic strips names starting with "__dbt_ref_".
	// This catches versioned placeholders (__dbt_ref_orders_v2) as well because
	// strings.HasPrefix("__dbt_ref_orders_v2", "__dbt_ref_") == true.
	for _, r := range refs {
		if strings.HasPrefix(r.ReferenceName, "__dbt_ref_") || strings.HasPrefix(r.ReferenceName, "__dbt_src_") {
			t.Errorf("__dbt_* placeholder reference must not survive in final refs; got %q", r.ReferenceName)
		}
	}
}

// ---------------------------------------------------------------------------
// Part E5 — config(alias=…) capture
// ---------------------------------------------------------------------------

// E5: config(alias=...) sets the model node's Metadata to {"alias":"<name>"}.
//
// WHY: dbt's config(alias=) lets a model deploy under a different identifier
// than its filename. The extractor annotates the model node's Metadata so
// downstream tooling knows the deployed name without a dbt manifest.
const e5ConfigAliasFixture = `
{{ config(materialized='table', alias='daily_orders') }}
SELECT order_id FROM raw_orders
`

// e5NoAliasFixture: a model with config() but no alias= kwarg.
const e5NoAliasFixture = `
{{ config(materialized='view') }}
SELECT * FROM raw
`

func TestE5ConfigAliasCapture(t *testing.T) {
	ext := newSQL()

	// Model WITH alias= → Metadata must include alias:"daily_orders".
	result, err := ext.Extract("models/e5_alias.sql", e5ConfigAliasFixture)
	if err != nil {
		t.Fatalf("Extract (alias model): %v", err)
	}
	modelNode := findSQLNodeExact(result.Nodes, types.NodeKindModel, "e5_alias")
	if modelNode == nil {
		t.Fatal("expected model node 'e5_alias'")
	}
	if !metadataHas(modelNode.Metadata, "alias", "daily_orders") {
		t.Errorf("model node Metadata must include alias='daily_orders'; got %s", modelNode.Metadata)
	}

	// Model WITHOUT alias= → Metadata must be nil or absent (no spurious alias).
	result2, err2 := ext.Extract("models/e5_no_alias.sql", e5NoAliasFixture)
	if err2 != nil {
		t.Fatalf("Extract (no-alias model): %v", err2)
	}
	modelNode2 := findSQLNodeExact(result2.Nodes, types.NodeKindModel, "e5_no_alias")
	if modelNode2 == nil {
		t.Fatal("expected model node 'e5_no_alias'")
	}
	if modelNode2.Metadata != nil {
		// If Metadata is non-nil, the "alias" key must be absent.
		var m map[string]interface{}
		if err := json.Unmarshal(modelNode2.Metadata, &m); err == nil {
			if _, hasAlias := m["alias"]; hasAlias {
				t.Errorf("model with no alias= must not have 'alias' key in Metadata; got %s", modelNode2.Metadata)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// F1 — CREATE FILE FORMAT
// ---------------------------------------------------------------------------

// TestF1CreateFileFormat verifies that CREATE [OR REPLACE] FILE FORMAT <name>
// produces a file_format node with the correct name and no spurious edges.
//
// WHY (F1): Snowflake file formats are first-class named objects; consumers
// need to locate them by name (e.g. to find which stages or COPY INTO
// statements use a given format). No outbound edges are expected because
// the format itself has no dependency to track.
const f1FileFormatFixture = `
CREATE OR REPLACE FILE FORMAT my_csv TYPE = CSV FIELD_DELIMITER=',' SKIP_HEADER=1;
CREATE FILE FORMAT analytics.my_json TYPE=JSON;
`

func TestF1CreateFileFormat(t *testing.T) {
	ext := newSQL()
	result, err := ext.Extract("/db/snowflake_f1.sql", f1FileFormatFixture)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	// CREATE OR REPLACE FILE FORMAT my_csv → file_format node named "my_csv"
	csvNode := findSQLNodeExact(result.Nodes, types.NodeKindFileFormat, "my_csv")
	if csvNode == nil {
		t.Error("expected file_format node 'my_csv' from CREATE OR REPLACE FILE FORMAT my_csv")
	}

	// CREATE FILE FORMAT analytics.my_json → file_format node named "my_json" (schema-qualified)
	jsonNode := findSQLNodeExact(result.Nodes, types.NodeKindFileFormat, "my_json")
	if jsonNode == nil {
		t.Error("expected file_format node 'my_json' from CREATE FILE FORMAT analytics.my_json")
	}

	// No spurious unresolved references from file format definitions.
	if len(result.UnresolvedReferences) != 0 {
		t.Errorf("expected no unresolved references; got %d", len(result.UnresolvedReferences))
	}
}

// ---------------------------------------------------------------------------
// F2 — VARIANT/OBJECT/ARRAY column typing
// ---------------------------------------------------------------------------

// TestF2ColumnTyping verifies that column nodes carry Metadata.type for all
// column kinds: VARIANT, OBJECT, ARRAY, and parameterised NUMBER(38,0).
//
// WHY (F2): downstream tooling needs to know which columns hold semi-structured
// data (VARIANT/OBJECT/ARRAY) to apply Snowflake-specific query patterns.
// Capturing ALL column types (not only the semi-structured trio) is simpler
// and more useful — the wire-format changes intentionally.
const f2ColumnTypingFixture = `
CREATE TABLE sf_typed (
    c VARIANT,
    o OBJECT,
    a ARRAY,
    n NUMBER(38,0),
    v VARCHAR(256),
    id INTEGER NOT NULL
);
`

func TestF2ColumnTyping(t *testing.T) {
	ext := newSQL()
	result, err := ext.Extract("/db/snowflake_f2.sql", f2ColumnTypingFixture)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	cases := []struct {
		colName  string
		wantType string
	}{
		{"c", "VARIANT"},
		{"o", "OBJECT"},
		{"a", "ARRAY"},
		{"n", "NUMBER"},
		{"v", "VARCHAR"},
		{"id", "INTEGER"},
	}
	for _, tc := range cases {
		node := findSQLNodeExact(result.Nodes, types.NodeKindColumn, tc.colName)
		if node == nil {
			t.Errorf("expected column node %q", tc.colName)
			continue
		}
		if node.Metadata == nil {
			t.Errorf("column %q: Metadata is nil, want type=%q", tc.colName, tc.wantType)
			continue
		}
		var m map[string]interface{}
		if err := json.Unmarshal(node.Metadata, &m); err != nil {
			t.Errorf("column %q: Metadata unmarshal error: %v", tc.colName, err)
			continue
		}
		gotType, _ := m["type"].(string)
		if gotType != tc.wantType {
			t.Errorf("column %q: Metadata.type = %q, want %q", tc.colName, gotType, tc.wantType)
		}
	}
}

// TestF2NoTypeWhenAttributeKeyword verifies that when the token after the column
// name is a column-attribute keyword (DEFAULT, REFERENCES, NOT, NULL, …) rather
// than a type name, no "type" key appears in Metadata.
//
// WHY: isSQLKeyword only rejects DML/DDL verbs. Without an extended denylist,
// `col DEFAULT 0` would emit Metadata.type = "DEFAULT", which is wrong.
const f2NoTypeAttributeFixture = `
CREATE TABLE attr_cols (
    qty   DEFAULT 0,
    fk_id REFERENCES other(id),
    flag  NOT NULL
);
`

func TestF2NoTypeWhenAttributeKeyword(t *testing.T) {
	ext := newSQL()
	result, err := ext.Extract("/db/snowflake_f2_attr.sql", f2NoTypeAttributeFixture)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	// None of these columns have a declared type — the second token is an
	// attribute keyword. Metadata must either be nil OR contain no "type" key.
	for _, colName := range []string{"qty", "fk_id", "flag"} {
		node := findSQLNodeExact(result.Nodes, types.NodeKindColumn, colName)
		if node == nil {
			t.Errorf("expected column node %q", colName)
			continue
		}
		if node.Metadata == nil {
			continue // nil Metadata is fine — no type key present
		}
		var m map[string]interface{}
		if err := json.Unmarshal(node.Metadata, &m); err != nil {
			t.Errorf("column %q: Metadata unmarshal error: %v", colName, err)
			continue
		}
		if _, hasType := m["type"]; hasType {
			t.Errorf("column %q: Metadata must not have 'type' key when second token is an attribute keyword; got %s", colName, node.Metadata)
		}
	}
}

// TestF2GeneratedColumnPreservesType verifies that a generated column carries
// BOTH the declared type and generated:true — the merge must not overwrite
// the generated flag.
//
// WHY (F2 merge rule): before F2, generated columns had Metadata={"generated":true}.
// After F2, ALL columns carry {"type":"<TYPE>"} and generated columns additionally
// carry {"generated":true}. The merge must produce {"type":"<TYPE>","generated":true}.
const f2GeneratedColumnFixture = `
CREATE TABLE sf_gen (
    id    INTEGER,
    score FLOAT,
    label TEXT GENERATED ALWAYS AS (CONCAT('score-', CAST(score AS TEXT))) VIRTUAL
);
`

func TestF2GeneratedColumnPreservesType(t *testing.T) {
	ext := newSQL()
	result, err := ext.Extract("/db/snowflake_f2_gen.sql", f2GeneratedColumnFixture)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	labelCol := findSQLNodeExact(result.Nodes, types.NodeKindColumn, "label")
	if labelCol == nil {
		t.Fatal("expected column 'label'")
	}
	if labelCol.Metadata == nil {
		t.Fatal("label column Metadata is nil; want {type:TEXT, generated:true}")
	}
	var m map[string]interface{}
	if err := json.Unmarshal(labelCol.Metadata, &m); err != nil {
		t.Fatalf("label column Metadata unmarshal: %v", err)
	}
	if gotType, _ := m["type"].(string); gotType != "TEXT" {
		t.Errorf("label column Metadata.type = %q, want TEXT", gotType)
	}
	if gen, _ := m["generated"].(bool); !gen {
		t.Errorf("label column Metadata.generated must be true; got %v", m["generated"])
	}
}

// ---------------------------------------------------------------------------
// F3 — LATERAL FLATTEN argument reference (guarded)
// ---------------------------------------------------------------------------

// TestF3FlattenArgRef verifies the F3 spec contract:
//
//   - LATERAL FLATTEN(INPUT => dotted.expr) → NO edge to the dotted expr
//     (dotted forms are column expressions, not relation names).
//   - LATERAL FLATTEN(INPUT => bare_tbl) → references edge to bare_tbl
//     (single unqualified identifier is treated as a relation).
//   - Never emit edges to FLATTEN, LATERAL, TABLE, or INPUT.
//
// WHY: the FLATTEN input is overwhelmingly a VARIANT *column* (t.payload),
// so dotted forms would produce noisy false edges. Only a bare identifier
// (no dot) is unambiguously a relation reference.
const f3FlattenArgFixture = `
CREATE VIEW f3_view AS
SELECT t.col_val, f.value
FROM raw, LATERAL FLATTEN(INPUT => raw.payload) f,
     LATERAL FLATTEN(INPUT => other_tbl) g;
`

func TestF3FlattenArgRef(t *testing.T) {
	ext := newSQL()
	result, err := ext.Extract("/db/snowflake_f3.sql", f3FlattenArgFixture)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	refs := result.UnresolvedReferences

	// "raw" must be referenced (real table in FROM clause via viewBodyFROMRE).
	if !hasUnresolvedRef(refs, "raw", types.EdgeKindReferences) {
		t.Error("expected references edge to 'raw' from the FROM clause")
	}

	// "other_tbl" must be referenced (unqualified FLATTEN input = relation).
	if !hasUnresolvedRef(refs, "other_tbl", types.EdgeKindReferences) {
		t.Error("expected references edge to 'other_tbl' (unqualified FLATTEN input treated as relation)")
	}

	// Dotted FLATTEN input raw.payload must produce NO edge to "payload".
	if countUnresolvedRefs(refs, "payload") > 0 {
		t.Errorf("'payload' (dotted FLATTEN input) must not produce an edge; got %d ref(s)",
			countUnresolvedRefs(refs, "payload"))
	}

	// FLATTEN/LATERAL/TABLE/INPUT must never appear as edge targets.
	for _, badName := range []string{"FLATTEN", "flatten", "LATERAL", "lateral", "TABLE", "table", "INPUT", "input"} {
		if countUnresolvedRefs(refs, badName) > 0 {
			t.Errorf("%q must not produce a references edge; got %d ref(s)", badName, countUnresolvedRefs(refs, badName))
		}
	}
}

// ---------------------------------------------------------------------------
// F4 — standalone top-level COPY INTO (lazy script owner)
// ---------------------------------------------------------------------------

// TestF3FlattenArgRefInProcBody verifies the scanBodyEdges FLATTEN dispatch
// (routine/task body path) — the positive case that bodyFlattenRE fires and
// emits a references edge to a bare identifier inside a procedure body.
//
// WHY: TestF3FlattenArgRef only exercises the VIEW body path (inline scan).
// The scanBodyEdges path (functions, procedures, tasks) has a separate dispatch
// of bodyFlattenRE that must also be tested so neither path can regress silently.
const f3FlattenProcBodyFixture = `
CREATE OR REPLACE PROCEDURE flatten_proc()
LANGUAGE SQL AS $$
BEGIN
  SELECT f.value
  FROM raw_tbl t,
       LATERAL FLATTEN(INPUT => x_tbl) f,
       LATERAL FLATTEN(INPUT => t.nested_col) g;
END;
$$;
`

func TestF3FlattenArgRefInProcBody(t *testing.T) {
	ext := newSQL()
	result, err := ext.Extract("/db/snowflake_f3_proc.sql", f3FlattenProcBodyFixture)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	procNode := findSQLNodeExact(result.Nodes, types.NodeKindProcedure, "flatten_proc")
	if procNode == nil {
		t.Fatal("expected procedure node 'flatten_proc'")
	}

	refs := result.UnresolvedReferences

	// Unqualified FLATTEN input "x_tbl" → references edge owned by the proc.
	var xTblOwned bool
	for _, r := range refs {
		if r.ReferenceName == "x_tbl" && r.ReferenceKind == types.EdgeKindReferences && r.FromNodeID == procNode.ID {
			xTblOwned = true
		}
	}
	if !xTblOwned {
		t.Error("expected references edge to 'x_tbl' (bare FLATTEN input) owned by procedure node")
	}

	// Dotted FLATTEN input t.nested_col → NO edge to "nested_col".
	if countUnresolvedRefs(refs, "nested_col") > 0 {
		t.Errorf("'nested_col' (dotted FLATTEN input) must produce no edge; got %d ref(s)",
			countUnresolvedRefs(refs, "nested_col"))
	}

	// FLATTEN/LATERAL/INPUT must never appear as edge targets.
	for _, badName := range []string{"FLATTEN", "flatten", "LATERAL", "lateral", "INPUT", "input"} {
		if countUnresolvedRefs(refs, badName) > 0 {
			t.Errorf("%q must not produce a references edge in proc body; got %d ref(s)",
				badName, countUnresolvedRefs(refs, badName))
		}
	}
}

// TestF4TopLevelCopyScriptOwner verifies that a .sql file whose only statements
// are top-level COPY INTO commands produces a lazily-created script node (named
// by the file basename) that OWNS the writes/references edges.
//
// WHY: a standalone COPY INTO (not inside a routine/task body) has no enclosing
// definition to own its lineage. The script node provides a stable anchor so the
// graph stays connected. The node is lazy — never created for files without a
// top-level COPY.
const f4CopyScriptFixture = `
COPY INTO fact FROM @load_stage;
COPY INTO @out_stage FROM fact;
`

func TestF4TopLevelCopyScriptOwner(t *testing.T) {
	ext := newSQL()
	result, err := ext.Extract("/etl/load_facts.sql", f4CopyScriptFixture)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	// A script node must be present (lazy creation on first top-level COPY).
	scriptNode := findSQLNodeExact(result.Nodes, types.NodeKindScript, "load_facts")
	if scriptNode == nil {
		t.Fatal("expected script node 'load_facts' (basename of load_facts.sql) for a file with top-level COPY INTO")
	}

	refs := result.UnresolvedReferences

	// COPY INTO fact FROM @load_stage → script writes to "fact", references "load_stage".
	var factWriteOwned, stageRefOwned bool
	for _, r := range refs {
		if r.ReferenceName == "fact" && r.ReferenceKind == types.EdgeKindWrites && r.FromNodeID == scriptNode.ID {
			factWriteOwned = true
		}
		if r.ReferenceName == "load_stage" && r.ReferenceKind == types.EdgeKindReferences && r.FromNodeID == scriptNode.ID {
			stageRefOwned = true
		}
	}
	if !factWriteOwned {
		t.Error("expected script node to own a writes edge to 'fact' (COPY INTO fact FROM @load_stage)")
	}
	if !stageRefOwned {
		t.Error("expected script node to own a references edge to 'load_stage' (COPY INTO fact FROM @load_stage)")
	}

	// COPY INTO @out_stage FROM fact → script writes to "out_stage", references "fact".
	var outStageWriteOwned, factRefOwned bool
	for _, r := range refs {
		if r.ReferenceName == "out_stage" && r.ReferenceKind == types.EdgeKindWrites && r.FromNodeID == scriptNode.ID {
			outStageWriteOwned = true
		}
		if r.ReferenceName == "fact" && r.ReferenceKind == types.EdgeKindReferences && r.FromNodeID == scriptNode.ID {
			factRefOwned = true
		}
	}
	if !outStageWriteOwned {
		t.Error("expected script node to own a writes edge to 'out_stage' (COPY INTO @out_stage FROM fact)")
	}
	if !factRefOwned {
		t.Error("expected script node to own a references edge to 'fact' (COPY INTO @out_stage FROM fact)")
	}
}

// TestF4NoScriptForNonCopyFile verifies that a .sql file with only CREATE TABLE
// and SELECT statements (no top-level COPY INTO) does NOT produce a script node.
//
// WHY: the script node is lazily created — we must not pollute files that have no
// top-level COPY with a spurious node.
const f4NoCopyFixture = `
CREATE TABLE dim_date (
    date_id INTEGER,
    full_date DATE
);

SELECT * FROM dim_date;
`

func TestF4NoScriptForNonCopyFile(t *testing.T) {
	ext := newSQL()
	result, err := ext.Extract("/etl/dim_date.sql", f4NoCopyFixture)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	for _, n := range result.Nodes {
		if n.Kind == types.NodeKindScript {
			t.Errorf("unexpected script node %q in a file with no top-level COPY INTO; script nodes are lazy", n.Name)
		}
	}
}

// TestF4CopyInsideTaskNotScript verifies the critical dedup contract: a COPY INTO
// that lives inside a CREATE TASK body is owned by the TASK node and must NOT
// also create a script node.
//
// WHY: in-body COPYs are already owned by the routine/task via v1's scanBodyEdges.
// Creating an additional script node would double-count the lineage and produce a
// spurious top-level node for what is really a body-scoped statement.
const f4CopyInTaskFixture = `
CREATE OR REPLACE TASK load_task
  SCHEDULE = '1 minute'
  AS
  COPY INTO fact FROM @stg;
`

func TestF4CopyInsideTaskNotScript(t *testing.T) {
	ext := newSQL()
	result, err := ext.Extract("/etl/task_load.sql", f4CopyInTaskFixture)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	// No script node should exist — the COPY is inside a task body.
	for _, n := range result.Nodes {
		if n.Kind == types.NodeKindScript {
			t.Errorf("unexpected script node %q: COPY inside a task body must be owned by the task, not a script node", n.Name)
		}
	}

	// The task node must exist and own the COPY lineage.
	taskNode := findSQLNodeExact(result.Nodes, types.NodeKindTask, "load_task")
	if taskNode == nil {
		t.Fatal("expected task node 'load_task'")
	}

	refs := result.UnresolvedReferences
	// Task body COPY INTO fact FROM @stg → task writes to "fact", references "stg".
	var taskWritesFact, taskRefStg bool
	for _, r := range refs {
		if r.ReferenceName == "fact" && r.ReferenceKind == types.EdgeKindWrites && r.FromNodeID == taskNode.ID {
			taskWritesFact = true
		}
		if r.ReferenceName == "stg" && r.ReferenceKind == types.EdgeKindReferences && r.FromNodeID == taskNode.ID {
			taskRefStg = true
		}
	}
	if !taskWritesFact {
		t.Error("task node must own writes edge to 'fact' (COPY INTO inside task body)")
	}
	if !taskRefStg {
		t.Error("task node must own references edge to 'stg' (COPY INTO inside task body)")
	}
}

// TestTSQLCrossApplyCalls verifies that CROSS APPLY <tvf>(...) in a T-SQL
// procedure body emits a calls edge to the invoked table-valued function.
// WHY (issue #70): APPLY is the canonical T-SQL idiom for correlated TVF joins.
// Without this, proc→TVF lineage is invisible to the code-intel graph.
const tsqlCrossApplyFixture = `
CREATE TABLE [dbo].[Orders] ([OrderId] INT);
CREATE FUNCTION [dbo].[GetLines](@id INT) RETURNS TABLE AS RETURN SELECT 1 AS x;
CREATE PROCEDURE [dbo].[ProcessOrders]
AS
BEGIN
  SELECT o.OrderId, l.x
  FROM [dbo].[Orders] o
  CROSS APPLY [dbo].[GetLines](o.OrderId) l;
END;
GO
`

func TestTSQLCrossApplyCalls(t *testing.T) {
	// criterion 1: CROSS APPLY <tvf>(...) → calls edge to bare function name
	ext := newSQL()
	result, err := ext.Extract("/db/tsql_cross_apply.sql", tsqlCrossApplyFixture)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	refs := result.UnresolvedReferences

	// GetLines is CROSS APPLY target → calls
	if !hasUnresolvedRef(refs, "GetLines", types.EdgeKindCalls) {
		t.Error("expected calls edge to 'GetLines' from CROSS APPLY [dbo].[GetLines](...)")
	}
	// Orders is also referenced in FROM → references (criterion 4 combined check)
	if !hasUnresolvedRef(refs, "Orders", types.EdgeKindReferences) {
		t.Error("expected references edge to 'Orders' from FROM clause alongside CROSS APPLY")
	}
}

// TestTSQLOuterApplyCalls verifies that OUTER APPLY <tvf>(...) also emits a
// calls edge — both flavors must be covered.
// WHY (criterion 2): OUTER APPLY is a left-join variant; same lineage semantics.
const tsqlOuterApplyFixture = `
CREATE TABLE [dbo].[Tags] ([TagId] INT);
CREATE FUNCTION [dbo].[GetTags](@id INT) RETURNS TABLE AS RETURN SELECT 1 AS x;
CREATE PROCEDURE [dbo].[ReadTags]
AS
BEGIN
  SELECT t.TagId, g.x
  FROM [dbo].[Tags] t
  OUTER APPLY [dbo].[GetTags](t.TagId) g;
END;
GO
`

func TestTSQLOuterApplyCalls(t *testing.T) {
	// criterion 2: OUTER APPLY <tvf>(...) → calls edge
	ext := newSQL()
	result, err := ext.Extract("/db/tsql_outer_apply.sql", tsqlOuterApplyFixture)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	refs := result.UnresolvedReferences

	// GetTags is OUTER APPLY target → calls
	if !hasUnresolvedRef(refs, "GetTags", types.EdgeKindCalls) {
		t.Error("expected calls edge to 'GetTags' from OUTER APPLY [dbo].[GetTags](...)")
	}
}

// TestTSQLApplySchemaStripped verifies that a schema-qualified APPLY target
// emits a calls edge to the bare function name with schema stripped.
// WHY (criterion 3): parseQName strips schema prefix so all edges match the
// node's unqualified name — consistent with every other body edge kind.
const tsqlApplySchemaFixture = `
CREATE SCHEMA analytics;
CREATE FUNCTION analytics.fn_rollup(@x INT) RETURNS TABLE AS RETURN SELECT 1 AS v;
CREATE PROCEDURE dbo.Run
AS
BEGIN
  SELECT v FROM analytics.fn_rollup(1) r
  CROSS APPLY analytics.fn_rollup(r.v) q;
END;
GO
`

func TestTSQLApplySchemaStripped(t *testing.T) {
	// criterion 3: schema-qualified target → calls edge to bare name
	ext := newSQL()
	result, err := ext.Extract("/db/tsql_apply_schema.sql", tsqlApplySchemaFixture)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	refs := result.UnresolvedReferences

	// fn_rollup is the bare name after parseQName strips "analytics."
	if !hasUnresolvedRef(refs, "fn_rollup", types.EdgeKindCalls) {
		t.Error("expected calls edge to 'fn_rollup' with schema stripped from CROSS APPLY analytics.fn_rollup(...)")
	}
	// no edge to the schema prefix itself
	if countUnresolvedRefs(refs, "analytics") > 0 {
		t.Error("schema prefix 'analytics' must not appear as a ref target")
	}
}

// TestTSQLApplyKeywordsNotEdges verifies that CROSS, OUTER, and APPLY never
// appear as reference or call targets.
// WHY (criterion 5): bodyApplyRE captures only the identifier after APPLY and
// before '(' — the CROSS/OUTER/APPLY keywords themselves are never in group 1.
// The fixture uses valid T-SQL: a table source precedes CROSS/OUTER APPLY.
const tsqlApplyKeywordsFixture = `
CREATE TABLE dbo.items (id INT);
CREATE FUNCTION dbo.fn(@x INT) RETURNS TABLE AS RETURN SELECT 1 AS v;
CREATE PROCEDURE dbo.P
AS
BEGIN
  SELECT v
  FROM dbo.items i
  CROSS APPLY dbo.fn(i.id) x
  UNION ALL
  SELECT v
  FROM dbo.items i
  OUTER APPLY dbo.fn(i.id) y;
END;
GO
`

func TestTSQLApplyKeywordsNotEdges(t *testing.T) {
	// criterion 5: CROSS / OUTER / APPLY are never reference or call targets
	ext := newSQL()
	result, err := ext.Extract("/db/tsql_apply_kw.sql", tsqlApplyKeywordsFixture)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	refs := result.UnresolvedReferences

	for _, kw := range []string{"CROSS", "cross", "OUTER", "outer", "APPLY", "apply"} {
		if countUnresolvedRefs(refs, kw) > 0 {
			t.Errorf("keyword %q must not appear as a ref target", kw)
		}
	}
	// fn IS a real TVF → should get a calls edge
	if !hasUnresolvedRef(refs, "fn", types.EdgeKindCalls) {
		t.Error("expected calls edge to 'fn' from CROSS APPLY dbo.fn(...)")
	}
}

// TestTSQLApplyDerivedTableNoCallEdge verifies that a correlated derived-table
// apply (FROM dbo.src s CROSS APPLY (SELECT col FROM real_inner) sub) does NOT
// emit an APPLY-derived calls edge to the subquery alias, but the inner
// FROM real_inner still emits a references edge and the left source dbo.src
// emits a references edge.
// WHY (criterion 6): bodyApplyRE requires a sqlQNameRaw identifier immediately
// after APPLY, followed by '('. A derived-table apply starts with '(' after
// APPLY — not an identifier — so the regex cannot match. The inner FROM clause
// is caught by the existing viewBodyFROMRE scan, preserving that lineage.
const tsqlApplyDerivedTableFixture = `
CREATE TABLE dbo.src (id INT);
CREATE TABLE dbo.real_inner (col INT);
CREATE PROCEDURE dbo.Q
AS
BEGIN
  SELECT s.id, sub.col
  FROM dbo.src s
  CROSS APPLY (SELECT col FROM dbo.real_inner WHERE col > s.id) sub;
END;
GO
`

func TestTSQLApplyDerivedTableNoCallEdge(t *testing.T) {
	// criterion 6: derived-table apply → no APPLY calls edge; inner FROM → references
	ext := newSQL()
	result, err := ext.Extract("/db/tsql_apply_derived.sql", tsqlApplyDerivedTableFixture)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	refs := result.UnresolvedReferences

	// The derived-table alias 'sub' must never appear as a calls target.
	if hasUnresolvedRef(refs, "sub", types.EdgeKindCalls) {
		t.Error("derived-table alias 'sub' must not produce a calls edge from APPLY")
	}
	// Nothing should get a calls edge from this APPLY at all (no TVF invocation).
	for _, r := range refs {
		if r.ReferenceKind == types.EdgeKindCalls {
			t.Errorf("no calls edge expected from a derived-table APPLY, got calls to %q", r.ReferenceName)
		}
	}
	// The left source dbo.src → references edge (schema stripped to bare name).
	if !hasUnresolvedRef(refs, "src", types.EdgeKindReferences) {
		t.Error("expected references edge to 'src' from the left-hand FROM source")
	}
	// The inner FROM dbo.real_inner → references edge (schema stripped).
	if !hasUnresolvedRef(refs, "real_inner", types.EdgeKindReferences) {
		t.Error("expected references edge to 'real_inner' from inner FROM inside derived-table APPLY")
	}
}
