package standalone_test

// The extractor covers four dialects: Postgres/ANSI, MySQL backticks, and T-SQL
// brackets with GO and CREATE OR ALTER. Stripping -- and /* */ before matching is
// load-bearing: a CREATE TABLE inside a comment or string must never mint a node.

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction/standalone"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

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

// Exact match. findSQLNode uses strings.Contains, which passes when a longer name
// merely contains the term ("id" matches "old_id") — wrong for identity assertions.
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

	schemaNode := findSQLNode(nodes, types.NodeKindNamespace, "myapp")
	if schemaNode == nil {
		t.Error("expected namespace node 'myapp'")
	}

	usersNode := findSQLNode(nodes, types.NodeKindTable, "users")
	if usersNode == nil {
		t.Fatal("expected table node 'users'")
	}
	ordersNode := findSQLNode(nodes, types.NodeKindTable, "orders")
	if ordersNode == nil {
		t.Fatal("expected table node 'orders'")
	}

	emailCol := findSQLNode(nodes, types.NodeKindColumn, "email")
	if emailCol == nil {
		t.Error("expected column 'email'")
	}
	fullNameCol := findSQLNode(nodes, types.NodeKindColumn, "full_name")
	if fullNameCol == nil {
		t.Error("expected column 'full_name'")
	} else if !metadataHas(fullNameCol.Metadata, "generated", "true") {
		t.Error("full_name column should have metadata {\"generated\":true}")
	}

	constraintAsCol := findSQLNode(nodes, types.NodeKindColumn, "uq_users_email")
	if constraintAsCol != nil {
		t.Error("CONSTRAINT line must not produce a column node")
	}

	totalCol := findSQLNode(nodes, types.NodeKindColumn, "total")
	if totalCol == nil {
		t.Error("expected column 'total' from ALTER TABLE ADD COLUMN")
	}
	if !hasContainsEdge(edges, "orders", "total", nodes) {
		t.Error("expected contains edge orders→total (from ALTER TABLE ADD COLUMN)")
	}

	feedNode := findSQLNode(nodes, types.NodeKindTable, "ext_feed")
	if feedNode == nil {
		t.Error("expected table node 'ext_feed' from CREATE FOREIGN TABLE")
	} else if !metadataHas(feedNode.Metadata, "foreign", "true") {
		t.Error("ext_feed should have metadata {\"foreign\":true}")
	}

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

	fnNode := findSQLNode(nodes, types.NodeKindFunction, "get_user")
	if fnNode == nil {
		t.Error("expected function node 'get_user'")
	}

	procNode := findSQLNode(nodes, types.NodeKindProcedure, "archive_orders")
	if procNode == nil {
		t.Error("expected procedure node 'archive_orders'")
	}

	trigNode := findSQLNode(nodes, types.NodeKindTrigger, "trg_audit")
	if trigNode == nil {
		t.Error("expected trigger node 'trg_audit'")
	}

	idxNode := findSQLNode(nodes, types.NodeKindIndex, "idx_users_email")
	if idxNode == nil {
		t.Error("expected index node 'idx_users_email'")
	}
	if !hasContainsEdge(edges, "users", "idx_users_email", nodes) {
		t.Error("expected contains edge users→idx_users_email")
	}

	seqNode := findSQLNode(nodes, types.NodeKindSequence, "order_seq")
	if seqNode == nil {
		t.Error("expected sequence node 'order_seq'")
	}

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

	domainNode := findSQLNode(nodes, types.NodeKindTypeAlias, "positive_int")
	if domainNode == nil {
		t.Error("expected type_alias node 'positive_int' from CREATE DOMAIN")
	}

	dbNode := findSQLNode(nodes, types.NodeKindModule, "mydb")
	if dbNode == nil {
		t.Error("expected module node 'mydb' from CREATE DATABASE")
	}

	for _, n := range nodes {
		if n.Language != types.LanguageSQL {
			t.Errorf("node %s has language %s, want sql", n.ID, n.Language)
		}
		if !n.IsExported {
			t.Errorf("node %s IsExported=false, want true", n.ID)
		}
	}
}

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

	prodNode := findSQLNode(nodes, types.NodeKindTable, "products")
	if prodNode == nil {
		t.Error("expected table 'products' (backtick normalized)")
	}
	nameCol := findSQLNode(nodes, types.NodeKindColumn, "name")
	if nameCol == nil {
		t.Error("expected column 'name'")
	}
	// product_id is a real column; only the table-level PRIMARY KEY line is skipped.
	pkCol := findSQLNode(nodes, types.NodeKindColumn, "product_id")
	if pkCol == nil {
		t.Error("expected column 'product_id'")
	}

	viewNode := findSQLNode(nodes, types.NodeKindView, "active_products")
	if viewNode == nil {
		t.Error("expected view 'active_products'")
	}

	idxNode := findSQLNode(nodes, types.NodeKindIndex, "idx_product_name")
	if idxNode == nil {
		t.Error("expected index 'idx_product_name'")
	}
	if !hasContainsEdge(edges, "products", "idx_product_name", nodes) {
		t.Error("expected contains edge products→idx_product_name")
	}

	procNode := findSQLNode(nodes, types.NodeKindProcedure, "update_price")
	if procNode == nil {
		t.Error("expected procedure 'update_price'")
	}

	fnNode := findSQLNode(nodes, types.NodeKindFunction, "calc_tax")
	if fnNode == nil {
		t.Error("expected function 'calc_tax'")
	}
}

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

	custNode := findSQLNode(nodes, types.NodeKindTable, "Customers")
	if custNode == nil {
		t.Fatal("expected table node 'Customers' (brackets normalized)")
	}

	fullNameCol := findSQLNode(nodes, types.NodeKindColumn, "FullName")
	if fullNameCol == nil {
		t.Error("expected computed column 'FullName'")
	} else if !metadataHas(fullNameCol.Metadata, "generated", "true") {
		t.Error("FullName column should have metadata {\"generated\":true}")
	}

	pkCol := findSQLNode(nodes, types.NodeKindColumn, "PK_Customers")
	if pkCol != nil {
		t.Error("CONSTRAINT line must not produce a column node")
	}

	procNode := findSQLNode(nodes, types.NodeKindProcedure, "usp_GetCustomer")
	if procNode == nil {
		t.Error("expected procedure 'usp_GetCustomer' (CREATE OR ALTER)")
	}

	fnNode := findSQLNode(nodes, types.NodeKindFunction, "fn_FormatName")
	if fnNode == nil {
		t.Error("expected function 'fn_FormatName' (CREATE OR ALTER)")
	}

	trigNode := findSQLNode(nodes, types.NodeKindTrigger, "trg_Customer_Audit")
	if trigNode == nil {
		t.Error("expected trigger 'trg_Customer_Audit'")
	}

	idxNode := findSQLNode(nodes, types.NodeKindIndex, "idx_Customer_Email")
	if idxNode == nil {
		t.Error("expected index 'idx_Customer_Email'")
	}

	ssnType := findSQLNode(nodes, types.NodeKindTypeAlias, "SSNType")
	if ssnType == nil {
		t.Error("expected type_alias 'SSNType' from CREATE TYPE ... FROM")
	}

	tvpType := findSQLNode(nodes, types.NodeKindTypeAlias, "CustomerTableType")
	if tvpType == nil {
		t.Error("expected type_alias 'CustomerTableType' from CREATE TYPE ... AS TABLE")
	} else if !metadataHas(tvpType.Metadata, "table_type", "true") {
		t.Error("CustomerTableType should have metadata {\"table_type\":true}")
	}

	// Exact-name lookup: "CustomerTableType" also contains "Cust".
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

	dbNode := findSQLNode(nodes, types.NodeKindModule, "CorpDB")
	if dbNode == nil {
		t.Error("expected module node 'CorpDB'")
	}

	schemaNode := findSQLNode(nodes, types.NodeKindNamespace, "reporting")
	if schemaNode == nil {
		t.Error("expected namespace node 'reporting'")
	}
}

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

	if n := findSQLNode(nodes, types.NodeKindTable, "ghost"); n != nil {
		t.Error("CREATE TABLE inside -- comment must not produce a node")
	}
	if n := findSQLNode(nodes, types.NodeKindTable, "also_ghost"); n != nil {
		t.Error("CREATE TABLE inside /* */ comment must not produce a node")
	}
	if n := findSQLNode(nodes, types.NodeKindView, "fake_view"); n != nil {
		t.Error("CREATE VIEW inside -- comment must not produce a node")
	}
	// Mid-line: the ^ anchor alone guards this one.
	if n := findSQLNode(nodes, types.NodeKindTable, "fake"); n != nil {
		t.Error("CREATE TABLE inside single-quoted string literal must not produce a node")
	}
	// At column 0 only stripStrings guards it — the ^ anchor cannot.
	if n := findSQLNode(nodes, types.NodeKindTable, "evil"); n != nil {
		t.Error("CREATE TABLE at column 0 inside multi-line single-quoted string must not produce a node")
	}

	if n := findSQLNode(nodes, types.NodeKindTable, "real_table"); n == nil {
		t.Error("expected table 'real_table'")
	}
	if n := findSQLNode(nodes, types.NodeKindView, "real_view"); n == nil {
		t.Error("expected view 'real_view'")
	}
}

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

// Exact match: constraint names are identity assertions, and strings.Contains
// would pass on a longer name that merely contains the term ("pk" / "pk_accounts").
func hasConstraintNode(nodes []types.Node, name string) *types.Node {
	return findSQLNodeExact(nodes, types.NodeKindConstraint, name)
}

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

// Used for asserting that no references edges are emitted at all.
func hasReferencesEdge(edges []types.Edge) bool {
	for _, e := range edges {
		if e.Kind == types.EdgeKindReferences {
			return true
		}
	}
	return false
}

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

	// Anonymous constraint names are synthesized as <table>_<suffix>_<counter>.
	anonFKNode := hasConstraintNode(nodes, "accounts_fk_1")
	if anonFKNode == nil {
		t.Error("expected anonymous FK constraint node named exactly 'accounts_fk_1'")
	} else if ct := constraintTypeOf(anonFKNode); ct != "foreign_key" {
		t.Errorf("accounts_fk_1 constraint_type = %q, want foreign_key", ct)
	}

	itemsPKNode := hasConstraintNode(nodes, "items_pk_1")
	if itemsPKNode == nil {
		t.Error("expected anonymous PRIMARY KEY constraint node named exactly 'items_pk_1'")
	} else if ct := constraintTypeOf(itemsPKNode); ct != "primary_key" {
		t.Errorf("items_pk_1 constraint_type = %q, want primary_key", ct)
	}

	if hasReferencesEdge(edges) {
		t.Error("must not emit any references edges; found one")
	}

	// An inline column PK is a column, not a second constraint node.
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
	// The FK target is stashed in metadata; no references edge is emitted.
	if hasReferencesEdge(edges) {
		t.Error("must not emit references edges")
	}
}

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

	// The table is "Orders" after bracket normalization, so the anonymous PK is Orders_pk_1.
	anonAltPKNode := hasConstraintNode(nodes, "Orders_pk_1")
	if anonAltPKNode == nil {
		t.Error("expected anonymous PK constraint node named exactly 'Orders_pk_1' from ALTER TABLE ADD PRIMARY KEY")
	} else if ct := constraintTypeOf(anonAltPKNode); ct != "primary_key" {
		t.Errorf("Orders_pk_1 constraint_type = %q, want primary_key", ct)
	}

	if hasReferencesEdge(edges) {
		t.Error("must not emit references edges")
	}
}

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

	if findSQLNode(nodes, types.NodeKindColumn, "id") == nil {
		t.Error("expected column 'id'")
	}
	if findSQLNode(nodes, types.NodeKindColumn, "sku") == nil {
		t.Error("expected column 'sku'")
	}

	for _, n := range nodes {
		if n.Kind == types.NodeKindConstraint {
			t.Errorf("inline column-level constraint must not produce constraint node; got %s", n.Name)
		}
	}
}

func TestColumnExtractionSkipsConstraintLines(t *testing.T) {
	ext := newSQL()
	result, err := ext.Extract("/db/schema.sql", pgFixture)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	nodes := result.Nodes

	if findSQLNode(nodes, types.NodeKindColumn, "uq_users_email") != nil {
		t.Error("CONSTRAINT line (uq_users_email) must not be emitted as a column node")
	}

	// Same name, but as a constraint node.
	if hasConstraintNode(nodes, "uq_users_email") == nil {
		t.Error("expected constraint node 'uq_users_email' from constraint extraction")
	}
}

// alterAddAnonConstraintRE must not double-fire when a CONSTRAINT keyword sits
// between ADD and the type keyword — that minted two nodes for one constraint.
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

func hasUnresolvedRef(refs []types.UnresolvedReference, name string, kind types.EdgeKind) bool {
	for _, r := range refs {
		if r.ReferenceName == name && r.ReferenceKind == kind {
			return true
		}
	}
	return false
}

func countUnresolvedRefs(refs []types.UnresolvedReference, name string) int {
	n := 0
	for _, r := range refs {
		if r.ReferenceName == name {
			n++
		}
	}
	return n
}

// Reads, writes, and calls must stay distinct kinds — "impact <table>" is only useful
// if a procedure that writes a table is distinguishable from one that reads it.
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

	if !hasUnresolvedRef(refs, "archive", types.EdgeKindWrites) {
		t.Error("expected writes edge to 'archive' (INSERT INTO)")
	}
	if !hasUnresolvedRef(refs, "orders", types.EdgeKindWrites) {
		t.Error("expected writes edge to 'orders' (UPDATE)")
	}
	if !hasUnresolvedRef(refs, "audit_log", types.EdgeKindWrites) {
		t.Error("expected writes edge to 'audit_log' (DELETE FROM)")
	}
	// orders is both an UPDATE target and a FROM source.
	if !hasUnresolvedRef(refs, "orders", types.EdgeKindReferences) {
		t.Error("expected references edge to 'orders' (SELECT FROM)")
	}
	if !hasUnresolvedRef(refs, "log_event", types.EdgeKindCalls) {
		t.Error("expected calls edge to 'log_event' (EXEC)")
	}
}

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

// A CTE is statement-local, so an edge to it would reference a table that does not
// exist. The resolver drops such refs anyway; this pins the intent at extraction.
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
	if n := countUnresolvedRefs(result.UnresolvedReferences, "cte_name"); n > 0 {
		t.Errorf("CTE name 'cte_name' must not produce any edge, got %d refs", n)
	}
	if !hasUnresolvedRef(result.UnresolvedReferences, "real_table", types.EdgeKindReferences) {
		t.Error("expected references edge to 'real_table' in CTE body")
	}
}

// LATERAL is a clause modifier, not a table name; the keyword filter must drop it.
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
	if countUnresolvedRefs(result.UnresolvedReferences, "LATERAL") > 0 ||
		countUnresolvedRefs(result.UnresolvedReferences, "lateral") > 0 {
		t.Error("LATERAL must not produce a reference edge (it is a SQL keyword, not a table name)")
	}
	if !hasUnresolvedRef(result.UnresolvedReferences, "events", types.EdgeKindReferences) {
		t.Error("expected references edge to 'events' in lateral_view")
	}
}

// Fn-call capture in a policy is scoped to USING(...) and WITH CHECK(...); scanning
// the whole statement grabs builtins like current_setting() that never resolve.
const policyF7Fixture = `
CREATE TABLE docs (id INT, owner TEXT);
CREATE OR REPLACE FUNCTION owner_check(p TEXT) RETURNS BOOL AS $$ BEGIN RETURN TRUE; END; $$ LANGUAGE plpgsql;
CREATE POLICY doc_policy ON docs
USING (owner_check(owner));
`

// The fn call here sits outside USING/WITH CHECK and must not be captured.
const policyF7FixtureNoExtraFn = `
CREATE TABLE docs (id INT, owner TEXT);
CREATE POLICY doc_policy ON docs
AS PERMISSIVE FOR SELECT
TO public
USING (owner = current_user);
`

func TestF7PolicyFnCallScopedToUSING(t *testing.T) {
	ext := newSQL()
	result, err := ext.Extract("/db/policy_using.sql", policyF7Fixture)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if !hasUnresolvedRef(result.UnresolvedReferences, "owner_check", types.EdgeKindCalls) {
		t.Error("expected calls edge to 'owner_check' inside USING expression")
	}

	// A USING with no fn call must not emit calls edges to keywords like current_user.
	result2, err2 := ext.Extract("/db/policy_nofn.sql", policyF7FixtureNoExtraFn)
	if err2 != nil {
		t.Fatalf("Extract: %v", err2)
	}
	for _, r := range result2.UnresolvedReferences {
		if r.ReferenceKind == types.EdgeKindCalls {
			t.Errorf("unexpected calls edge to %q from policy with no fn in USING", r.ReferenceName)
		}
	}
}

// Function bodies go through the same body scan as procedures.
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

	if !hasUnresolvedRef(refs, "Archive", types.EdgeKindWrites) {
		t.Error("expected writes edge to 'Archive' (INSERT INTO [dbo].[Archive])")
	}
	if !hasUnresolvedRef(refs, "Orders", types.EdgeKindWrites) {
		t.Error("expected writes edge to 'Orders' (UPDATE [dbo].[Orders])")
	}
	// Orders is both a write target and a FROM source.
	if !hasUnresolvedRef(refs, "Orders", types.EdgeKindReferences) {
		t.Error("expected references edge to 'Orders' (SELECT FROM)")
	}
}

// scanBodyEdges reuses viewBodyFROMRE ("FROM|JOIN <name>"), so without the keyword
// guard a JOIN LATERAL inside a routine body mints a reference to "LATERAL".
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

	if countUnresolvedRefs(refs, "LATERAL") > 0 || countUnresolvedRefs(refs, "lateral") > 0 {
		t.Error("LATERAL must not produce a reference edge inside a routine body (it is a SQL keyword, not a table name)")
	}

	if !hasUnresolvedRef(refs, "orders", types.EdgeKindReferences) {
		t.Error("expected references edge to 'orders' from routine body FROM clause")
	}
}

// TRANSIENT is a class modifier between OR REPLACE and TABLE.
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

// SECURE is a security modifier between OR REPLACE and VIEW.
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

// LOCAL and GLOBAL are legal only as prefixes to TEMP/TEMPORARY.
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

// Bare LOCAL/GLOBAL without TEMP is invalid SQL and must not mint a table.
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

// A stage is a top-level definition with no outbound edges.
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

// COPY direction turns on whether the target starts with '@': into a table it writes
// the table and reads the stage; into a stage it writes the stage and reads the table.
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

	if !hasUnresolvedRef(refs, "fact", types.EdgeKindWrites) {
		t.Error("expected writes edge to 'fact' from COPY INTO fact FROM @load_stage")
	}
	if !hasUnresolvedRef(refs, "load_stage", types.EdgeKindReferences) {
		t.Error("expected references edge to 'load_stage' from COPY INTO fact FROM @load_stage")
	}
	if !hasUnresolvedRef(refs, "out_stage", types.EdgeKindWrites) {
		t.Error("expected writes edge to 'out_stage' from COPY INTO @out_stage FROM fact")
	}
	if !hasUnresolvedRef(refs, "fact", types.EdgeKindReferences) {
		t.Error("expected references edge to 'fact' from COPY INTO @out_stage FROM fact")
	}
}

// A body COPY is owned by its enclosing routine. A top-level COPY has no enclosing
// definition, so it is owned by a lazily-created script node named for the file.
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

	if !hasUnresolvedRef(refs, "body_tbl", types.EdgeKindWrites) {
		t.Error("expected writes edge to 'body_tbl' from proc-body COPY INTO")
	}
	if !hasUnresolvedRef(refs, "body_stage", types.EdgeKindReferences) {
		t.Error("expected references edge to 'body_stage' from proc-body COPY INTO")
	}

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

// @~ (user stage) and @%tbl (table stage) are anonymous internal stages with no node
// to point at; the writes edge to the target table must still be emitted.
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

	if !hasUnresolvedRef(refs, "dest", types.EdgeKindWrites) {
		t.Error("expected writes edge to 'dest' even when source is an internal stage")
	}

	if countUnresolvedRefs(refs, "~") > 0 {
		t.Error("@~ user-stage must not emit a references edge to '~'")
	}
	if countUnresolvedRefs(refs, "othertbl") > 0 {
		t.Error("table-stage sigil must not emit a references edge to 'othertbl'")
	}
	if countUnresolvedRefs(refs, "%othertbl") > 0 {
		t.Error("table-stage sigil must not emit a references edge to the raw sigil token")
	}
}

// A CLONE body has no FROM — CLONE is the only lineage signal.
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

// cloneRE scans only the preamble before '(', so a column literally named CLONE in a
// table body can never match — a real CLONE statement has no column list.
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
	if findSQLNodeExact(result.Nodes, types.NodeKindTable, "t") == nil {
		t.Error("expected table node 't'")
	}
	if hasUnresolvedRef(result.UnresolvedReferences, "INT", types.EdgeKindReferences) {
		t.Error("column named CLONE inside table body must not produce a references edge to 'INT'")
	}
	for _, r := range result.UnresolvedReferences {
		if r.ReferenceKind == types.EdgeKindReferences {
			t.Errorf("expected no references edges from table with CLONE column; got references to %q", r.ReferenceName)
		}
	}
}

// The Jinja pre-pass is a no-op when the source has no {{, {% or {#; pgFixture is
// the plain-SQL baseline it must leave untouched.
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
	for _, n := range result.Nodes {
		if n.Kind == types.NodeKindModel {
			t.Errorf("plain SQL file must not produce a model node; got model %q", n.Name)
		}
	}
	if findSQLNodeExact(result.Nodes, types.NodeKindTable, "plain_tbl") == nil {
		t.Error("expected table node 'plain_tbl' from plain SQL")
	}
	if findSQLNodeExact(result.Nodes, types.NodeKindView, "plain_view") == nil {
		t.Error("expected view node 'plain_view' from plain SQL")
	}
}

// The model node is named for the file basename, extension stripped.
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

// dbt 1.5 cross-project refs carry a package AND a version= in one call. With v= as
// an alternative inside the second-arg group, matching both was structurally
// impossible — version= has to be an independent trailing optional group.
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

	// Versioned targets are asserted in TestE4VersionedRefDistinctTargets.
	if !hasUnresolvedRef(refs, "stg_orders", types.EdgeKindReferences) {
		t.Error("expected references edge to 'stg_orders' from unversioned ref('stg_orders')")
	}

	// The package arg must never become the edge target.
	if countUnresolvedRefs(refs, "pkg") > 0 {
		t.Errorf("package arg 'pkg' must not appear as edge target; got %d ref(s)", countUnresolvedRefs(refs, "pkg"))
	}
}

// Harvest runs after comment removal, so a ref inside {# … #} is deliberately dead.
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

// A ref() inside a {% if %} block is still captured.
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

// source() always takes two args and its edge target is "schema.table".
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

// Both the ref() and the real joined table appear, owned by the model node.
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

	if !hasUnresolvedRef(refs, "stg", types.EdgeKindReferences) {
		t.Error("expected references edge to 'stg' from {{ ref('stg') }}")
	}

	if !hasUnresolvedRef(refs, "real_tbl", types.EdgeKindReferences) {
		t.Error("expected references edge to 'real_tbl' from JOIN real_tbl in residual body scan")
	}

	for _, r := range refs {
		if strings.HasPrefix(r.ReferenceName, "__dbt_ref_") || strings.HasPrefix(r.ReferenceName, "__dbt_src_") {
			t.Errorf("__dbt_* placeholder reference must not survive in final refs; got %q", r.ReferenceName)
		}
	}
}

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

// The residual used to be built from the raw source, so {# … #} prose survived into
// the body scan and "select from the table" minted a references edge to "the".
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

	if !hasUnresolvedRef(refs, "raw_x", types.EdgeKindReferences) {
		t.Error("expected exactly one references edge to 'raw_x' from {{ ref('raw_x') }}")
	}

	for _, badWord := range []string{"the", "table", "here", "seeds", "simple", "so", "this", "query", "is", "intentionally", "minimal"} {
		if countUnresolvedRefs(refs, badWord) > 0 {
			t.Errorf("spurious references edge to comment-prose word %q — Jinja comment text must not leak into residual body scan", badWord)
		}
	}
}

// Same bug on the join-keyword path.
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

	if !hasUnresolvedRef(refs, "raw_orders", types.EdgeKindReferences) {
		t.Error("expected references edge to 'raw_orders'")
	}

	if countUnresolvedRefs(refs, "other_table") > 0 {
		t.Error("spurious references edge to 'other_table' from join-prose inside {# #} comment")
	}
}

// A stream node also carries a reference to the object it is declared ON.
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

// Every ON <object-kind> variant matches, not only ON TABLE.
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

// AFTER is in sqlKeywordsForRef, so task predecessors cannot route through
// scanBodyEdges — a dedicated AFTER regex has to emit those edges.
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

	if !hasUnresolvedRef(refs, "stg_t", types.EdgeKindReferences) {
		t.Error("expected references edge to AFTER predecessor 'stg_t' (keyword-denylist trap guard)")
	}
	if !hasUnresolvedRef(refs, "dim_t", types.EdgeKindReferences) {
		t.Error("expected references edge to AFTER predecessor 'dim_t' (keyword-denylist trap guard)")
	}

	if !hasUnresolvedRef(refs, "fact", types.EdgeKindWrites) {
		t.Error("expected writes edge to 'fact' from INSERT INTO in task body")
	}
	if !hasUnresolvedRef(refs, "stg", types.EdgeKindReferences) {
		t.Error("expected references edge to 'stg' from FROM clause in task body")
	}
}

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

// QUALIFY is a Snowflake window-filter clause placed after WHERE. A scanner that
// treats it as a statement break drops every FROM edge that follows it.
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

	if !hasUnresolvedRef(refs, "orders", types.EdgeKindReferences) {
		t.Error("expected references edge to 'orders' from FROM clause; QUALIFY must not truncate body-edge scanning")
	}

	if countUnresolvedRefs(refs, "QUALIFY") > 0 || countUnresolvedRefs(refs, "qualify") > 0 {
		t.Error("QUALIFY must not produce a references edge (it is a Snowflake window-filter clause, not a table name)")
	}
}

// 'col::VARIANT' is an identifier, the Snowflake cast operator, and a type name. A
// greedy identifier match after FROM would capture the type as a table.
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

	if !hasUnresolvedRef(refs, "src_tbl", types.EdgeKindReferences) {
		t.Error("expected references edge to 'src_tbl' from FROM clause in cast_view")
	}

	for _, badName := range []string{"VARIANT", "variant", "NUMBER", "number", "TEXT", "text"} {
		if countUnresolvedRefs(refs, badName) > 0 {
			t.Errorf("cast type %q must not appear as a reference target (it is a type name, not a table)", badName)
		}
	}

	// col is a column, not a FROM source.
	if countUnresolvedRefs(refs, "col") > 0 {
		t.Error("column name 'col' must not appear as a reference target")
	}
}

// sqlQNameRaw starts with [A-Za-z_], so a bare $1 cannot match the FROM/JOIN capture.
// The real table anchors the test so the negatives are not vacuous.
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

	if !hasUnresolvedRef(refs, "real_tbl", types.EdgeKindReferences) {
		t.Error("expected references edge to 'real_tbl' from FROM clause (positive anchor — proves scan ran)")
	}

	for _, badName := range []string{"$1", "$2", "$3", "1", "2", "3"} {
		if countUnresolvedRefs(refs, badName) > 0 {
			t.Errorf("positional column ref %q must not produce a reference edge", badName)
		}
	}
}

// FLATTEN is a Snowflake table function and TABLE is a keyword; neither is a relation.
// Snowflake counterpart of TestF6LateralNoEdge.
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

// dbt resolves {{ ref('stg') }} to the node named 'stg', so a model node named
// 'stg.sql' silently breaks every sibling ref — the compound suffix has to go.
const d1D2SqlJinjaFixture = `
{{ config(materialized='table') }}

SELECT *
FROM {{ ref('base_orders') }}
WHERE status = 'active'
`

func TestD1D2SqlJinjaModelNode(t *testing.T) {
	ext := newSQL()

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

	result2, err := ext.Extract("models/stg.sql.jinja", d1D2SqlJinjaFixture)
	if err != nil {
		t.Fatalf("Extract (.sql.jinja): %v", err)
	}

	jinjaModel := findSQLNodeExact(result2.Nodes, types.NodeKindModel, "stg")
	if jinjaModel == nil {
		var names []string
		for _, n := range result2.Nodes {
			if n.Kind == types.NodeKindModel {
				names = append(names, n.Name)
			}
		}
		t.Fatalf(".sql.jinja: expected model node named 'stg' (D2: strip full .sql.jinja suffix); got model names: %v", names)
	}

	if !hasUnresolvedRef(result2.UnresolvedReferences, "base_orders", types.EdgeKindReferences) {
		t.Error(".sql.jinja: expected references edge to 'base_orders' (ref DAG identical to .sql twin)")
	}
}

// Path role decides node kind: macros/ mints a macro node and no model node.
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

	result, err := ext.Extract("macros/util.sql", e1MacroFileFixture)
	if err != nil {
		t.Fatalf("Extract (macros/util.sql): %v", err)
	}

	macroNode := findSQLNodeExact(result.Nodes, types.NodeKindMacro, "u")
	if macroNode == nil {
		t.Fatal("macros/util.sql: expected macro node 'u' from {% macro u() %}")
	}

	for _, n := range result.Nodes {
		if n.Kind == types.NodeKindModel {
			t.Errorf("macros/util.sql: must NOT produce a model node; got model %q", n.Name)
		}
	}
}

func TestE1PathRoleModelFile(t *testing.T) {
	ext := newSQL()

	result, err := ext.Extract("models/stg.sql", e1ModelFileFixture)
	if err != nil {
		t.Fatalf("Extract (models/stg.sql): %v", err)
	}

	model := findSQLNodeExact(result.Nodes, types.NodeKindModel, "stg")
	if model == nil {
		t.Fatal("models/stg.sql: expected model node 'stg' (v1 behaviour, no regression)")
	}
}

// A package-qualified call (dbt_utils.star) and the ref/config denylist emit no calls
// edge; only the local macro call does.
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

	if !hasUnresolvedRef(refs, "my_macro", types.EdgeKindCalls) {
		t.Error("expected calls edge to 'my_macro' from {{ my_macro() }}")
	}

	if countUnresolvedRefs(refs, "dbt_utils") > 0 {
		t.Error("package 'dbt_utils' must not appear as an edge target (package-qualified skip)")
	}
	if countUnresolvedRefs(refs, "star") > 0 {
		t.Error("'star' from dbt_utils.star() must not appear as an edge target (package-qualified skip)")
	}

	if hasUnresolvedRef(refs, "ref", types.EdgeKindCalls) {
		t.Error("'ref' is in the denylist; must not produce a calls edge")
	}
	if hasUnresolvedRef(refs, "config", types.EdgeKindCalls) {
		t.Error("'config' is in the denylist; must not produce a calls edge")
	}

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

// Presence alone does not show who owns a ref — only FromNodeID separates "owned by
// the model" from "owned by macro m".
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

	modelNode := findSQLNodeExact(result.Nodes, types.NodeKindModel, "e3_span")
	if modelNode == nil {
		t.Fatal("expected model node 'e3_span'")
	}

	macroNode := findSQLNodeExact(result.Nodes, types.NodeKindMacro, "m")
	if macroNode == nil {
		t.Fatal("expected macro node 'm' from {% macro m() %}")
	}

	refs := result.UnresolvedReferences

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

	for _, r := range refs {
		if r.ReferenceName == "b" && r.FromNodeID == modelNode.ID {
			t.Error("ref('b') inside macro body must NOT be owned by the model node")
		}
		if r.ReferenceName == "a" && r.FromNodeID == macroNode.ID {
			t.Error("ref('a') outside macro body must NOT be owned by the macro node")
		}
	}
}

// blankPreserveNewlines collapses every multi-byte rune to one space, so a unicode
// {# comment #} makes rawForHarvest shorter than source. Macro spans must be computed
// on rawForHarvest, or a ref inside the macro body falls outside the shifted span.
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

// The dedup key must be owner:callee — keyed on callee alone, only the first owner
// gets its calls edge to my_helper.
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

// Plain dbt models, no macros: the macro work must leave this path alone.
func TestEV1NonRegression(t *testing.T) {
	ext := newSQL()
	result, err := ext.Extract("models/b5_residual.sql", b5PlaceholderResidualFixture)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	modelNode := findSQLNodeExact(result.Nodes, types.NodeKindModel, "b5_residual")
	if modelNode == nil {
		t.Fatal("v1 non-regression: expected model node 'b5_residual'")
	}

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

	for _, n := range result.Nodes {
		if n.Kind == types.NodeKindMacro {
			t.Errorf("v1 non-regression: plain model must not produce macro nodes; got %q", n.Name)
		}
	}
}

func TestC4FlattenNoEdge(t *testing.T) {
	ext := newSQL()

	result, err := ext.Extract("/db/snowflake_c4_lateral.sql", c4FlattenFixture)
	if err != nil {
		t.Fatalf("Extract (lateral flatten): %v", err)
	}
	refs := result.UnresolvedReferences

	if !hasUnresolvedRef(refs, "src_tbl", types.EdgeKindReferences) {
		t.Error("(lateral flatten) expected references edge to 'src_tbl' from FROM clause")
	}
	for _, badName := range []string{"FLATTEN", "flatten", "LATERAL", "lateral", "TABLE", "table"} {
		if countUnresolvedRefs(refs, badName) > 0 {
			t.Errorf("(lateral flatten) %q must not produce a references edge (it is a SQL keyword/function, not a table)", badName)
		}
	}

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

// dbt 1.5+ compiles ref('orders', v=2) to orders_v2. With version= in a non-capturing
// group N was never extracted and the target stayed the bare model name.
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

	if !hasUnresolvedRef(refs, "orders", types.EdgeKindReferences) {
		t.Error("expected references edge to 'orders' from unversioned ref('orders')")
	}

	if !hasUnresolvedRef(refs, "orders_v2", types.EdgeKindReferences) {
		t.Error("expected references edge to 'orders_v2' from ref('orders', v=2)")
	}

	if !hasUnresolvedRef(refs, "orders_v3", types.EdgeKindReferences) {
		t.Error("expected references edge to 'orders_v3' from ref('pkg', 'orders', version=3)")
	}

	// Exactly one bare "orders" ref — the versioned forms must not leak one.
	ordersCount := countUnresolvedRefs(refs, "orders")
	if ordersCount != 1 {
		t.Errorf("expected exactly 1 edge to bare 'orders' (unversioned form only); got %d", ordersCount)
	}

	// The __dbt_ref_ drop is a prefix match, so versioned placeholders are caught too.
	for _, r := range refs {
		if strings.HasPrefix(r.ReferenceName, "__dbt_ref_") || strings.HasPrefix(r.ReferenceName, "__dbt_src_") {
			t.Errorf("__dbt_* placeholder reference must not survive in final refs; got %q", r.ReferenceName)
		}
	}
}

// config(alias=) deploys a model under a name other than its filename; recording it
// in Metadata lets downstream tooling find the deployed name without a dbt manifest.
const e5ConfigAliasFixture = `
{{ config(materialized='table', alias='daily_orders') }}
SELECT order_id FROM raw_orders
`

const e5NoAliasFixture = `
{{ config(materialized='view') }}
SELECT * FROM raw
`

func TestE5ConfigAliasCapture(t *testing.T) {
	ext := newSQL()

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

	result2, err2 := ext.Extract("models/e5_no_alias.sql", e5NoAliasFixture)
	if err2 != nil {
		t.Fatalf("Extract (no-alias model): %v", err2)
	}
	modelNode2 := findSQLNodeExact(result2.Nodes, types.NodeKindModel, "e5_no_alias")
	if modelNode2 == nil {
		t.Fatal("expected model node 'e5_no_alias'")
	}
	if modelNode2.Metadata != nil {
		var m map[string]interface{}
		if err := json.Unmarshal(modelNode2.Metadata, &m); err == nil {
			if _, hasAlias := m["alias"]; hasAlias {
				t.Errorf("model with no alias= must not have 'alias' key in Metadata; got %s", modelNode2.Metadata)
			}
		}
	}
}

// A file format has no dependency to track, so it emits no outbound edges.
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

	csvNode := findSQLNodeExact(result.Nodes, types.NodeKindFileFormat, "my_csv")
	if csvNode == nil {
		t.Error("expected file_format node 'my_csv' from CREATE OR REPLACE FILE FORMAT my_csv")
	}

	jsonNode := findSQLNodeExact(result.Nodes, types.NodeKindFileFormat, "my_json")
	if jsonNode == nil {
		t.Error("expected file_format node 'my_json' from CREATE FILE FORMAT analytics.my_json")
	}

	if len(result.UnresolvedReferences) != 0 {
		t.Errorf("expected no unresolved references; got %d", len(result.UnresolvedReferences))
	}
}

// Every column carries Metadata.type, not only the semi-structured trio.
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

// isSQLKeyword rejects only DML/DDL verbs, so without an extended denylist
// "col DEFAULT 0" would record Metadata.type = "DEFAULT".
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
	for _, colName := range []string{"qty", "fk_id", "flag"} {
		node := findSQLNodeExact(result.Nodes, types.NodeKindColumn, colName)
		if node == nil {
			t.Errorf("expected column node %q", colName)
			continue
		}
		if node.Metadata == nil {
			continue
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

// A generated column carries both keys; the type merge must not clobber generated.
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

// A FLATTEN input is overwhelmingly a VARIANT column (t.payload), so only a bare
// unqualified identifier counts as a relation; dotted forms emit nothing.
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

	if !hasUnresolvedRef(refs, "raw", types.EdgeKindReferences) {
		t.Error("expected references edge to 'raw' from the FROM clause")
	}

	if !hasUnresolvedRef(refs, "other_tbl", types.EdgeKindReferences) {
		t.Error("expected references edge to 'other_tbl' (unqualified FLATTEN input treated as relation)")
	}

	if countUnresolvedRefs(refs, "payload") > 0 {
		t.Errorf("'payload' (dotted FLATTEN input) must not produce an edge; got %d ref(s)",
			countUnresolvedRefs(refs, "payload"))
	}

	for _, badName := range []string{"FLATTEN", "flatten", "LATERAL", "lateral", "TABLE", "table", "INPUT", "input"} {
		if countUnresolvedRefs(refs, badName) > 0 {
			t.Errorf("%q must not produce a references edge; got %d ref(s)", badName, countUnresolvedRefs(refs, badName))
		}
	}
}

// scanBodyEdges dispatches bodyFlattenRE separately from the view-body scan.
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

	var xTblOwned bool
	for _, r := range refs {
		if r.ReferenceName == "x_tbl" && r.ReferenceKind == types.EdgeKindReferences && r.FromNodeID == procNode.ID {
			xTblOwned = true
		}
	}
	if !xTblOwned {
		t.Error("expected references edge to 'x_tbl' (bare FLATTEN input) owned by procedure node")
	}

	if countUnresolvedRefs(refs, "nested_col") > 0 {
		t.Errorf("'nested_col' (dotted FLATTEN input) must produce no edge; got %d ref(s)",
			countUnresolvedRefs(refs, "nested_col"))
	}

	for _, badName := range []string{"FLATTEN", "flatten", "LATERAL", "lateral", "INPUT", "input"} {
		if countUnresolvedRefs(refs, badName) > 0 {
			t.Errorf("%q must not produce a references edge in proc body; got %d ref(s)",
				badName, countUnresolvedRefs(refs, badName))
		}
	}
}

// A top-level COPY has no enclosing definition to own its lineage, so a script node
// named for the file basename anchors it. Lazy — never minted for other files.
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

	scriptNode := findSQLNodeExact(result.Nodes, types.NodeKindScript, "load_facts")
	if scriptNode == nil {
		t.Fatal("expected script node 'load_facts' (basename of load_facts.sql) for a file with top-level COPY INTO")
	}

	refs := result.UnresolvedReferences

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

// The script node is lazy: a file with no top-level COPY must not get one.
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

// A COPY inside a task body is already owned by the task; minting a script node too
// would double-count the lineage.
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

	for _, n := range result.Nodes {
		if n.Kind == types.NodeKindScript {
			t.Errorf("unexpected script node %q: COPY inside a task body must be owned by the task, not a script node", n.Name)
		}
	}

	taskNode := findSQLNodeExact(result.Nodes, types.NodeKindTask, "load_task")
	if taskNode == nil {
		t.Fatal("expected task node 'load_task'")
	}

	refs := result.UnresolvedReferences
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

// APPLY is the T-SQL idiom for correlated TVF joins; without it proc→TVF is invisible.
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
	ext := newSQL()
	result, err := ext.Extract("/db/tsql_cross_apply.sql", tsqlCrossApplyFixture)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	refs := result.UnresolvedReferences

	if !hasUnresolvedRef(refs, "GetLines", types.EdgeKindCalls) {
		t.Error("expected calls edge to 'GetLines' from CROSS APPLY [dbo].[GetLines](...)")
	}
	if !hasUnresolvedRef(refs, "Orders", types.EdgeKindReferences) {
		t.Error("expected references edge to 'Orders' from FROM clause alongside CROSS APPLY")
	}
}

// OUTER APPLY is the left-join variant with the same lineage semantics.
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
	ext := newSQL()
	result, err := ext.Extract("/db/tsql_outer_apply.sql", tsqlOuterApplyFixture)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	refs := result.UnresolvedReferences

	if !hasUnresolvedRef(refs, "GetTags", types.EdgeKindCalls) {
		t.Error("expected calls edge to 'GetTags' from OUTER APPLY [dbo].[GetTags](...)")
	}
}

// parseQName strips the schema prefix so edges match the node's unqualified name.
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
	ext := newSQL()
	result, err := ext.Extract("/db/tsql_apply_schema.sql", tsqlApplySchemaFixture)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	refs := result.UnresolvedReferences

	if !hasUnresolvedRef(refs, "fn_rollup", types.EdgeKindCalls) {
		t.Error("expected calls edge to 'fn_rollup' with schema stripped from CROSS APPLY analytics.fn_rollup(...)")
	}
	if countUnresolvedRefs(refs, "analytics") > 0 {
		t.Error("schema prefix 'analytics' must not appear as a ref target")
	}
}

// bodyApplyRE captures only the identifier between APPLY and '(' — never a keyword.
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
	if !hasUnresolvedRef(refs, "fn", types.EdgeKindCalls) {
		t.Error("expected calls edge to 'fn' from CROSS APPLY dbo.fn(...)")
	}
}

// #tmp has a leading '#' that sqlQNameRaw cannot match, so it produces no edges
// without a synthetic node; encoding the routine keeps two procs' #tmp apart.
const tsqlTempTableLocalFixture = `
CREATE TABLE dbo.source_data (id INT, val NVARCHAR(100));
CREATE PROCEDURE dbo.usp_LoadTemp
AS
BEGIN
    CREATE TABLE #staging (id INT, val NVARCHAR(100));
    INSERT INTO #staging
        SELECT id, val FROM dbo.source_data WHERE val IS NOT NULL;
    SELECT id, val FROM #staging WHERE id > 0;
    DROP TABLE #staging;
END;
GO
`

func TestTSQLTempTableLocal(t *testing.T) {
	ext := newSQL()
	result, err := ext.Extract("/db/tsql_temp_local.sql", tsqlTempTableLocalFixture)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	var tempNode *types.Node
	for i := range result.Nodes {
		n := &result.Nodes[i]
		if n.Kind == types.NodeKindTable && strings.Contains(n.Name, "staging") && strings.HasPrefix(n.Name, "usp_LoadTemp") {
			tempNode = n
			break
		}
	}
	if tempNode == nil {
		t.Fatalf("expected a synthetic temp node for #staging scoped to usp_LoadTemp; nodes: %v",
			nodeNames(result.Nodes))
	}

	if !metadataHas(tempNode.Metadata, "temp", "local") {
		t.Errorf("temp node metadata should have {\"temp\":\"local\"}, got %s", tempNode.Metadata)
	}
	if !metadataHas(tempNode.Metadata, "token", "#staging") {
		t.Errorf("temp node metadata should carry {\"token\":\"#staging\"}, got %s", tempNode.Metadata)
	}

	// The synthetic name carries no '.', '/' or '::' so byExactName routing applies.
	if strings.ContainsAny(tempNode.Name, "./") || strings.Contains(tempNode.Name, "::") {
		t.Errorf("synthetic temp node Name must not contain '.', '/', or '::'; got %q", tempNode.Name)
	}

	syntheticName := tempNode.Name

	if !hasUnresolvedRef(result.UnresolvedReferences, syntheticName, types.EdgeKindWrites) {
		t.Errorf("expected writes edge to %q (INSERT INTO #staging); refs: %v",
			syntheticName, refNames(result.UnresolvedReferences))
	}

	if !hasUnresolvedRef(result.UnresolvedReferences, syntheticName, types.EdgeKindReferences) {
		t.Errorf("expected references edge to %q (SELECT FROM #staging); refs: %v",
			syntheticName, refNames(result.UnresolvedReferences))
	}

	if !hasUnresolvedRef(result.UnresolvedReferences, "source_data", types.EdgeKindReferences) {
		t.Error("expected references edge to 'source_data' from INSERT…SELECT source")
	}

	if hasUnresolvedRef(result.UnresolvedReferences, "#staging", types.EdgeKindWrites) ||
		hasUnresolvedRef(result.UnresolvedReferences, "#staging", types.EdgeKindReferences) {
		t.Error("bare '#staging' must not appear as ReferenceName; only the synthetic form should")
	}
}

// Without a routine-scoped name both procs' #tmp share a ReferenceName and the
// resolver unifies them into false cross-proc lineage.
const tsqlTempTableTwoProcsSC2Fixture = `
CREATE TABLE dbo.a_src (n INT);
CREATE TABLE dbo.b_src (n INT);

CREATE PROCEDURE dbo.ProcA
AS
BEGIN
    CREATE TABLE #tmp (n INT);
    INSERT INTO #tmp SELECT n FROM dbo.a_src;
    SELECT n FROM #tmp;
END;
GO

CREATE PROCEDURE dbo.ProcB
AS
BEGIN
    CREATE TABLE #tmp (n INT);
    INSERT INTO #tmp SELECT n FROM dbo.b_src;
    SELECT n FROM #tmp;
END;
GO
`

func TestTSQLTempTableTwoProcsSC2(t *testing.T) {
	ext := newSQL()
	result, err := ext.Extract("/db/tsql_two_procs.sql", tsqlTempTableTwoProcsSC2Fixture)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	var tmpNodes []types.Node
	for _, n := range result.Nodes {
		if n.Kind == types.NodeKindTable && strings.Contains(n.Name, "tmp") &&
			(strings.HasPrefix(n.Name, "ProcA") || strings.HasPrefix(n.Name, "ProcB")) {
			tmpNodes = append(tmpNodes, n)
		}
	}
	if len(tmpNodes) != 2 {
		t.Fatalf("expected 2 distinct synthetic #tmp nodes (one per proc), got %d; nodes: %v",
			len(tmpNodes), nodeNames(result.Nodes))
	}

	if tmpNodes[0].Name == tmpNodes[1].Name {
		t.Errorf("two procs' #tmp nodes must have distinct synthetic Names; both are %q", tmpNodes[0].Name)
	}

	// Verified indirectly: both synthetic names appear, and neither is the bare "#tmp".
	nameA := tmpNodes[0].Name
	nameB := tmpNodes[1].Name
	if nameA == "#tmp" || nameB == "#tmp" {
		t.Error("synthetic names must not be bare '#tmp'")
	}
	if countUnresolvedRefs(result.UnresolvedReferences, nameA) == 0 {
		t.Errorf("no refs found for %q", nameA)
	}
	if countUnresolvedRefs(result.UnresolvedReferences, nameB) == 0 {
		t.Errorf("no refs found for %q", nameB)
	}
}

// @x is ambiguous, so only the DECLARE @x TABLE(…) form counts as a relation.
const tsqlTableVariableSC3Fixture = `
CREATE TABLE dbo.orders (id INT, amount MONEY);

CREATE PROCEDURE dbo.usp_CalcOrders
AS
BEGIN
    DECLARE @id INT;           -- scalar: must produce NO edge
    DECLARE @results TABLE (   -- table variable: must produce edges
        id     INT,
        amount MONEY
    );

    INSERT INTO @results
        SELECT id, amount FROM dbo.orders WHERE amount > 0;

    SELECT id, amount FROM @results WHERE id > 10;
END;
GO
`

func TestTSQLTableVariableSC3(t *testing.T) {
	ext := newSQL()
	result, err := ext.Extract("/db/tsql_tvar.sql", tsqlTableVariableSC3Fixture)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	var tvarNode *types.Node
	for i := range result.Nodes {
		n := &result.Nodes[i]
		if n.Kind == types.NodeKindTable && strings.Contains(n.Name, "results") &&
			strings.HasPrefix(n.Name, "usp_CalcOrders") {
			tvarNode = n
			break
		}
	}
	if tvarNode == nil {
		t.Fatalf("expected synthetic node for @results table variable; nodes: %v", nodeNames(result.Nodes))
	}

	if !metadataHas(tvarNode.Metadata, "temp", "local") {
		t.Errorf("@results node should have metadata {\"temp\":\"local\"}, got %s", tvarNode.Metadata)
	}
	if !metadataHas(tvarNode.Metadata, "token", "@results") {
		t.Errorf("@results node should carry {\"token\":\"@results\"}, got %s", tvarNode.Metadata)
	}

	synName := tvarNode.Name
	if !hasUnresolvedRef(result.UnresolvedReferences, synName, types.EdgeKindWrites) {
		t.Errorf("expected writes edge to %q (INSERT INTO @results)", synName)
	}
	if !hasUnresolvedRef(result.UnresolvedReferences, synName, types.EdgeKindReferences) {
		t.Errorf("expected references edge to %q (SELECT FROM @results)", synName)
	}

	for _, n := range result.Nodes {
		if n.Kind == types.NodeKindTable && strings.Contains(n.Name, "id") &&
			strings.HasPrefix(n.Name, "usp_CalcOrders") {
			t.Errorf("scalar @id must not produce a temp node; got %q", n.Name)
		}
	}
	if countUnresolvedRefs(result.UnresolvedReferences, "@id") > 0 ||
		countUnresolvedRefs(result.UnresolvedReferences, "usp_CalcOrders@id") > 0 {
		t.Error("scalar @id must not appear in any ref")
	}
}

// ##global temp tables persist across connections, so cross-proc lineage is real;
// file-level dedup keeps a repeated declaration from minting a second node.
const tsqlGlobalTempSC4Fixture = `
CREATE TABLE dbo.real_tbl (x INT);

CREATE PROCEDURE dbo.ProcWriter
AS
BEGIN
    CREATE TABLE ##shared_staging (x INT);
    INSERT INTO ##shared_staging SELECT x FROM dbo.real_tbl;
END;
GO

CREATE PROCEDURE dbo.ProcReader
AS
BEGIN
    -- ##shared_staging was created by ProcWriter; reading it here.
    SELECT x FROM ##shared_staging;
    -- Declaring ##shared_staging again (conditional branch) must NOT create a second node.
    CREATE TABLE ##shared_staging (x INT);
END;
GO
`

func TestTSQLGlobalTempSC4(t *testing.T) {
	ext := newSQL()
	result, err := ext.Extract("/db/tsql_global_temp.sql", tsqlGlobalTempSC4Fixture)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	var globalNodes []types.Node
	for _, n := range result.Nodes {
		if n.Kind == types.NodeKindTable && strings.Contains(n.Name, "shared_staging") {
			globalNodes = append(globalNodes, n)
		}
	}
	if len(globalNodes) != 1 {
		t.Fatalf("expected exactly 1 node for ##shared_staging (file-deduped), got %d; nodes: %v",
			len(globalNodes), nodeNames(result.Nodes))
	}

	globalName := globalNodes[0].Name
	if !strings.Contains(globalName, "shared_staging") {
		t.Errorf("global temp node Name should contain 'shared_staging', got %q", globalName)
	}
	if strings.HasPrefix(globalName, "ProcWriter") || strings.HasPrefix(globalName, "ProcReader") {
		t.Errorf("global temp Name must not be routine-prefixed; got %q", globalName)
	}

	if !metadataHas(globalNodes[0].Metadata, "temp", "global") {
		t.Errorf("##shared_staging should have {\"temp\":\"global\"}, got %s", globalNodes[0].Metadata)
	}

	// Asserting by FromNodeID: otherwise one proc emitting both a writes and a references
	// edge would pass as "cross-proc".
	var writerID, readerID string
	for _, n := range result.Nodes {
		if n.Kind != types.NodeKindProcedure {
			continue
		}
		switch n.Name {
		case "ProcWriter":
			writerID = n.ID
		case "ProcReader":
			readerID = n.ID
		}
	}
	if writerID == "" || readerID == "" {
		t.Fatalf("expected ProcWriter and ProcReader nodes; got writer=%q reader=%q", writerID, readerID)
	}

	hasEdgeFrom := func(fromID, name string, kind types.EdgeKind) bool {
		for _, r := range result.UnresolvedReferences {
			if r.FromNodeID == fromID && r.ReferenceName == name && r.ReferenceKind == kind {
				return true
			}
		}
		return false
	}
	if !hasEdgeFrom(writerID, globalName, types.EdgeKindWrites) {
		t.Errorf("expected writes edge to %q from ProcWriter", globalName)
	}
	if !hasEdgeFrom(readerID, globalName, types.EdgeKindReferences) {
		t.Errorf("expected references edge to %q from ProcReader (cross-proc lineage)", globalName)
	}
}

// SELECT … INTO #tmp is a declaration and a write in one statement.
const tsqlSelectIntoTempFixture = `
CREATE TABLE dbo.events (id INT, ts DATETIME);

CREATE PROCEDURE dbo.usp_Recent
AS
BEGIN
    SELECT id, ts INTO #recent FROM dbo.events WHERE ts > '2024-01-01';
    SELECT id FROM #recent;
END;
GO
`

func TestTSQLSelectIntoTemp(t *testing.T) {
	ext := newSQL()
	result, err := ext.Extract("/db/tsql_select_into.sql", tsqlSelectIntoTempFixture)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	var tempNode *types.Node
	for i := range result.Nodes {
		n := &result.Nodes[i]
		if n.Kind == types.NodeKindTable && strings.Contains(n.Name, "recent") &&
			strings.HasPrefix(n.Name, "usp_Recent") {
			tempNode = n
			break
		}
	}
	if tempNode == nil {
		t.Fatalf("expected synthetic node for #recent; nodes: %v", nodeNames(result.Nodes))
	}

	synName := tempNode.Name
	if !hasUnresolvedRef(result.UnresolvedReferences, synName, types.EdgeKindWrites) {
		t.Errorf("expected writes edge to %q from SELECT INTO", synName)
	}
	if !hasUnresolvedRef(result.UnresolvedReferences, synName, types.EdgeKindReferences) {
		t.Errorf("expected references edge to %q from SELECT FROM", synName)
	}
	if !hasUnresolvedRef(result.UnresolvedReferences, "events", types.EdgeKindReferences) {
		t.Error("expected references edge to 'events'")
	}
}

// bodyApplyRE needs an identifier right after APPLY; a derived-table apply starts
// with '(' instead, so no calls edge — the inner FROM still yields a references edge.
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
	ext := newSQL()
	result, err := ext.Extract("/db/tsql_apply_derived.sql", tsqlApplyDerivedTableFixture)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	refs := result.UnresolvedReferences

	if hasUnresolvedRef(refs, "sub", types.EdgeKindCalls) {
		t.Error("derived-table alias 'sub' must not produce a calls edge from APPLY")
	}
	for _, r := range refs {
		if r.ReferenceKind == types.EdgeKindCalls {
			t.Errorf("no calls edge expected from a derived-table APPLY, got calls to %q", r.ReferenceName)
		}
	}
	if !hasUnresolvedRef(refs, "src", types.EdgeKindReferences) {
		t.Error("expected references edge to 'src' from the left-hand FROM source")
	}
	if !hasUnresolvedRef(refs, "real_inner", types.EdgeKindReferences) {
		t.Error("expected references edge to 'real_inner' from inner FROM inside derived-table APPLY")
	}
}

// OUTPUT INTO routes change-capture rows into a second table — a write that is
// otherwise invisible to the graph.
const mergeOutputIntoRealTableFixture = `
CREATE TABLE dbo.Tgt (id INT, val NVARCHAR(100));
CREATE TABLE dbo.Src (id INT, val NVARCHAR(100));
CREATE TABLE dbo.AuditLog (action NVARCHAR(10), id INT);

CREATE PROCEDURE dbo.usp_MergeAudit
AS
BEGIN
    MERGE INTO dbo.Tgt AS t
    USING dbo.Src AS s ON t.id = s.id
    WHEN MATCHED THEN UPDATE SET t.val = s.val
    WHEN NOT MATCHED THEN INSERT (id, val) VALUES (s.id, s.val)
    OUTPUT $action, inserted.id INTO dbo.AuditLog;
END;
GO
`

func TestMergeOutputIntoRealTable(t *testing.T) {
	// The gap-text scanner must tolerate $ and dots in the OUTPUT list.
	ext := newSQL()
	result, err := ext.Extract("/db/merge_output.sql", mergeOutputIntoRealTableFixture)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	refs := result.UnresolvedReferences

	if !hasUnresolvedRef(refs, "Tgt", types.EdgeKindWrites) {
		t.Error("expected writes edge to 'Tgt' from MERGE INTO")
	}
	if !hasUnresolvedRef(refs, "AuditLog", types.EdgeKindWrites) {
		t.Errorf("expected writes edge to 'AuditLog' from OUTPUT INTO; refs: %v",
			refNames(result.UnresolvedReferences))
	}
}

// Synthetic-name routing must cover OUTPUT INTO targets so @tvar edges land on one node.
const insertOutputIntoTvarFixture = `
CREATE TABLE dbo.A (id INT, val NVARCHAR(100));

CREATE PROCEDURE dbo.usp_InsertCapture
AS
BEGIN
    DECLARE @captured TABLE (id INT);
    INSERT INTO dbo.A (id, val)
    OUTPUT inserted.id INTO @captured
    VALUES (1, 'hello');
    SELECT id FROM @captured;
END;
GO
`

func TestInsertOutputIntoTvar(t *testing.T) {
	ext := newSQL()
	result, err := ext.Extract("/db/insert_output_tvar.sql", insertOutputIntoTvarFixture)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	var capturedNode *types.Node
	for i := range result.Nodes {
		n := &result.Nodes[i]
		if n.Kind == types.NodeKindTable && strings.Contains(n.Name, "captured") &&
			strings.HasPrefix(n.Name, "usp_InsertCapture") {
			capturedNode = n
			break
		}
	}
	if capturedNode == nil {
		t.Fatalf("expected synthetic node for @captured table variable; nodes: %v", nodeNames(result.Nodes))
	}

	synName := capturedNode.Name

	if !hasUnresolvedRef(result.UnresolvedReferences, synName, types.EdgeKindWrites) {
		t.Errorf("expected writes edge to %q from OUTPUT INTO @captured; refs: %v",
			synName, refNames(result.UnresolvedReferences))
	}
	if !hasUnresolvedRef(result.UnresolvedReferences, synName, types.EdgeKindReferences) {
		t.Errorf("expected references edge to %q from SELECT FROM @captured; refs: %v",
			synName, refNames(result.UnresolvedReferences))
	}
	if !hasUnresolvedRef(result.UnresolvedReferences, "A", types.EdgeKindWrites) {
		t.Error("expected writes edge to 'A' from INSERT INTO dbo.A")
	}
	if hasUnresolvedRef(result.UnresolvedReferences, "@captured", types.EdgeKindWrites) {
		t.Error("bare '@captured' must not appear as a writes ref; only the synthetic form should")
	}
}

// OUTPUT with no INTO only returns rows to the caller — there is no second target.
const outputNoIntoFixture = `
CREATE TABLE dbo.Orders (id INT, status NVARCHAR(20));

CREATE PROCEDURE dbo.usp_FulfillOrder
AS
BEGIN
    UPDATE dbo.Orders
    SET status = 'shipped'
    OUTPUT inserted.id, inserted.status
    WHERE id = 42;
END;
GO
`

func TestOutputNoInto(t *testing.T) {
	ext := newSQL()
	result, err := ext.Extract("/db/output_no_into.sql", outputNoIntoFixture)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	refs := result.UnresolvedReferences

	if !hasUnresolvedRef(refs, "Orders", types.EdgeKindWrites) {
		t.Error("expected writes edge to 'Orders' from UPDATE")
	}
	for _, r := range refs {
		if r.ReferenceKind == types.EdgeKindWrites && r.ReferenceName != "Orders" {
			t.Errorf("unexpected writes edge to %q — OUTPUT with no INTO should produce no secondary writes edge",
				r.ReferenceName)
		}
	}
}

// GhostSemi is reachable only through a false OUTPUT…INTO bridge across the
// semicolon: a real-table SELECT … INTO is not captured as a write, so a zero-writes
// assertion on GhostSemi fails the moment the match spans the statement boundary.
const outputSemicolonBoundaryFixture = `
CREATE TABLE dbo.A (id INT);

CREATE PROCEDURE dbo.usp_TwoStmts
AS
BEGIN
    DELETE FROM dbo.A OUTPUT deleted.id;
    SELECT id INTO dbo.GhostSemi FROM dbo.A;
END;
GO
`

func TestOutputSemicolonBoundary(t *testing.T) {
	ext := newSQL()
	result, err := ext.Extract("/db/sc6b_semicolon.sql", outputSemicolonBoundaryFixture)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	refs := result.UnresolvedReferences

	if !hasUnresolvedRef(refs, "A", types.EdgeKindWrites) {
		t.Error("expected writes edge to 'A' from DELETE FROM dbo.A")
	}
	if hasUnresolvedRef(refs, "GhostSemi", types.EdgeKindWrites) {
		t.Error("false OUTPUT…INTO edge bridged the semicolon to 'GhostSemi' — boundary guard failed")
	}
}

// The same bridge without a semicolon, where the [^;] exclusion cannot help — only
// the DML/SELECT keyword guard on the gap text prevents it.
const outputKeywordBoundaryFixture = `
CREATE TABLE dbo.A (id INT);

CREATE PROCEDURE dbo.usp_KeywordBoundary
AS
BEGIN
    DELETE FROM dbo.A OUTPUT deleted.id SELECT id INTO dbo.GhostKW FROM dbo.A
END;
GO
`

func TestOutputKeywordBoundary(t *testing.T) {
	ext := newSQL()
	result, err := ext.Extract("/db/sc6b_keyword.sql", outputKeywordBoundaryFixture)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	refs := result.UnresolvedReferences

	if !hasUnresolvedRef(refs, "A", types.EdgeKindWrites) {
		t.Error("expected writes edge to 'A' from DELETE FROM dbo.A")
	}
	if hasUnresolvedRef(refs, "GhostKW", types.EdgeKindWrites) {
		t.Error("false OUTPUT…INTO edge bridged the SELECT keyword to 'GhostKW' — keyword guard failed")
	}
}

// PIVOT internals — the operator, the aggregate, the pivoted and spread columns, the
// IN-list values — are columns and operators, not navigable objects.
const pivotFixture = `
CREATE VIEW dbo.SalesByYear AS
SELECT custid, [2020], [2021]
FROM (SELECT custid, yr, amt FROM dbo.SalesRaw) src
PIVOT (SUM(amt) FOR yr IN ([2020], [2021])) pvt;
GO
`

func TestPivotSourceOnly(t *testing.T) {
	ext := newSQL()
	result, err := ext.Extract("/db/pivot.sql", pivotFixture)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	refs := result.UnresolvedReferences

	if !hasUnresolvedRef(refs, "SalesRaw", types.EdgeKindReferences) {
		t.Error("expected references edge to 'SalesRaw' (inner FROM source of the PIVOT)")
	}
	forbidden := []string{"PIVOT", "SUM", "amt", "yr", "2020", "2021", "pvt", "src"}
	for _, r := range refs {
		for _, bad := range forbidden {
			if strings.EqualFold(r.ReferenceName, bad) {
				t.Errorf("unexpected %s edge to %q — PIVOT internals must not become object edges",
					r.ReferenceKind, r.ReferenceName)
			}
		}
	}
}

// UNPIVOT mirrors PIVOT: the FROM source is the only object edge.
const unpivotFixture = `
CREATE PROCEDURE dbo.UnpivotDemo
AS
BEGIN
    SELECT custid, metric, val
    FROM dbo.WideMetrics
    UNPIVOT (val FOR metric IN (q1, q2, q3)) up;
END;
GO
`

func TestUnpivotSourceOnly(t *testing.T) {
	ext := newSQL()
	result, err := ext.Extract("/db/unpivot.sql", unpivotFixture)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	refs := result.UnresolvedReferences

	if !hasUnresolvedRef(refs, "WideMetrics", types.EdgeKindReferences) {
		t.Error("expected references edge to 'WideMetrics' (FROM source of the UNPIVOT)")
	}
	forbidden := []string{"UNPIVOT", "val", "metric", "q1", "q2", "q3", "up"}
	for _, r := range refs {
		for _, bad := range forbidden {
			if strings.EqualFold(r.ReferenceName, bad) {
				t.Errorf("unexpected %s edge to %q — UNPIVOT internals must not become object edges",
					r.ReferenceKind, r.ReferenceName)
			}
		}
	}
}

const viewAliasColFixture = `
CREATE TABLE dbo.acct (id INT, name VARCHAR(100));

CREATE VIEW dbo.v_acct AS
SELECT a.id, a.name
FROM dbo.acct a;
`

func TestColumnRef_ViewAlias(t *testing.T) {
	// A qualified alias.col ref is named "table-as-written.col" so it matches the column
	// node's QualifiedName and can resolve.
	ext := newSQL()
	result, err := ext.Extract("/db/view.sql", viewAliasColFixture)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	refs := result.UnresolvedReferences

	if !hasUnresolvedRef(refs, "dbo.acct.id", types.EdgeKindReferences) {
		t.Errorf("expected references edge to 'dbo.acct.id' (alias a → dbo.acct); got refs: %v", uniqueRefNames(refs))
	}
	if !hasUnresolvedRef(refs, "dbo.acct.name", types.EdgeKindReferences) {
		t.Errorf("expected references edge to 'dbo.acct.name' (alias a → dbo.acct); got refs: %v", uniqueRefNames(refs))
	}

	if !hasUnresolvedRef(refs, "acct", types.EdgeKindReferences) {
		t.Errorf("expected table-level references edge to 'acct'; got refs: %v", uniqueRefNames(refs))
	}
}

const viewJoinAliasFixture = `
CREATE TABLE dbo.acct (id INT);
CREATE TABLE dbo.person (name VARCHAR(100), acct_id INT);

CREATE VIEW dbo.v_joined AS
SELECT a.id, p.name
FROM dbo.acct a
JOIN dbo.person p ON a.id = p.acct_id;
`

func TestColumnRef_JoinAlias(t *testing.T) {
	// The alias map covers the whole body, ON clauses included — those are real column
	// references, not only SELECT-list ones.
	ext := newSQL()
	result, err := ext.Extract("/db/join.sql", viewJoinAliasFixture)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	refs := result.UnresolvedReferences

	if !hasUnresolvedRef(refs, "dbo.acct.id", types.EdgeKindReferences) {
		t.Errorf("expected references edge to 'dbo.acct.id'; got: %v", uniqueRefNames(refs))
	}
	if !hasUnresolvedRef(refs, "dbo.person.name", types.EdgeKindReferences) {
		t.Errorf("expected references edge to 'dbo.person.name'; got: %v", uniqueRefNames(refs))
	}
}

const unqualifiedSkipFixture = `
CREATE TABLE dbo.acct (id INT);

CREATE VIEW dbo.v_bare AS
SELECT id
FROM dbo.acct;
`

func TestColumnRef_UnqualifiedSkipped(t *testing.T) {
	// A bare column name is ambiguous — any table could have an "id".
	ext := newSQL()
	result, err := ext.Extract("/db/unqualified.sql", unqualifiedSkipFixture)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	refs := result.UnresolvedReferences

	for _, r := range refs {
		if strings.HasSuffix(r.ReferenceName, ".id") && r.ReferenceKind == types.EdgeKindReferences {
			// The table-level "acct" ref is fine; any "x.id" would come from the bare SELECT.
			t.Errorf("unexpected column-level ref %q — bare 'id' must not emit a column edge", r.ReferenceName)
		}
	}
}

const aliaslessFixture = `
CREATE TABLE acct (id INT, val INT);

CREATE VIEW v_aliasless AS
SELECT acct.id, acct.val
FROM acct;
`

func TestColumnRef_AliaslessTableSelf(t *testing.T) {
	// An unaliased table maps its bare name to itself.
	ext := newSQL()
	result, err := ext.Extract("/db/aliasless.sql", aliaslessFixture)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	refs := result.UnresolvedReferences

	if !hasUnresolvedRef(refs, "acct.id", types.EdgeKindReferences) {
		t.Errorf("expected references edge to 'acct.id'; got: %v", uniqueRefNames(refs))
	}
	if !hasUnresolvedRef(refs, "acct.val", types.EdgeKindReferences) {
		t.Errorf("expected references edge to 'acct.val'; got: %v", uniqueRefNames(refs))
	}
}

const cteAliasColumnFixture = `
CREATE TABLE dbo.acct (id INT);

CREATE VIEW dbo.v_cte AS
WITH cte AS (SELECT id FROM dbo.acct)
SELECT cte.id
FROM cte;
`

func TestColumnRef_CTEAliasSkipped(t *testing.T) {
	// A CTE is a computed relation with no column nodes, so cteShadow gates the alias map.
	ext := newSQL()
	result, err := ext.Extract("/db/cte.sql", cteAliasColumnFixture)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	refs := result.UnresolvedReferences

	for _, r := range refs {
		if r.ReferenceName == "cte.id" {
			t.Errorf("CTE alias 'cte' produced a column edge %q — CTE names must be shadowed", r.ReferenceName)
		}
	}
}

const procAliasColFixture = `
CREATE TABLE dbo.orders (id INT, total MONEY);

CREATE PROCEDURE dbo.usp_GetOrders
AS
BEGIN
    SELECT o.id, o.total
    FROM dbo.orders o
    WHERE o.total > 0;
END;
GO
`

func TestColumnRef_ProcBody(t *testing.T) {
	// The routine alias map is a separate path from the view-body scan.
	ext := newSQL()
	result, err := ext.Extract("/db/proc.sql", procAliasColFixture)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	refs := result.UnresolvedReferences

	if !hasUnresolvedRef(refs, "dbo.orders.id", types.EdgeKindReferences) {
		t.Errorf("expected references edge to 'dbo.orders.id'; got: %v", uniqueRefNames(refs))
	}
	if !hasUnresolvedRef(refs, "dbo.orders.total", types.EdgeKindReferences) {
		t.Errorf("expected references edge to 'dbo.orders.total'; got: %v", uniqueRefNames(refs))
	}
	if !hasUnresolvedRef(refs, "orders", types.EdgeKindReferences) {
		t.Errorf("expected table-level references edge to 'orders'; got: %v", uniqueRefNames(refs))
	}
}

const aliasKeywordBoundaryFixture = `
CREATE TABLE dbo.acct (id INT, val INT);

CREATE VIEW dbo.v_kw AS
SELECT acct.id, acct.val
FROM dbo.acct
WHERE acct.id = 1;
`

func TestColumnRef_KeywordBoundaryNotAlias(t *testing.T) {
	// bodyFromAliasRE would capture a trailing keyword as an alias ("FROM dbo.acct WHERE"
	// → alias "WHERE"); aliasBoundaryKeywords blocks it and the table self-maps instead.
	ext := newSQL()
	result, err := ext.Extract("/db/kw.sql", aliasKeywordBoundaryFixture)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	refs := result.UnresolvedReferences

	if !hasUnresolvedRef(refs, "dbo.acct.id", types.EdgeKindReferences) {
		t.Errorf("expected references edge to 'dbo.acct.id' (unaliased self-map); got: %v", uniqueRefNames(refs))
	}
	if !hasUnresolvedRef(refs, "dbo.acct.val", types.EdgeKindReferences) {
		t.Errorf("expected references edge to 'dbo.acct.val' (unaliased self-map); got: %v", uniqueRefNames(refs))
	}
	for _, r := range refs {
		if strings.HasPrefix(strings.ToLower(r.ReferenceName), "where.") {
			t.Errorf("spurious alias: keyword 'WHERE' was treated as an alias; got ref %q", r.ReferenceName)
		}
	}
}

func uniqueRefNames(refs []types.UnresolvedReference) []string {
	names := make([]string, 0, len(refs))
	seen := map[string]bool{}
	for _, r := range refs {
		if !seen[r.ReferenceName] {
			seen[r.ReferenceName] = true
			names = append(names, r.ReferenceName)
		}
	}
	return names
}

// Column names collide across tables ("parent_no" in both child and parent), so
// identity assertions here key on the qualified name.
func findSQLNodeByQName(nodes []types.Node, kind types.NodeKind, qname string) *types.Node {
	lower := strings.ToLower(qname)
	for i := range nodes {
		if nodes[i].Kind == kind && strings.ToLower(nodes[i].QualifiedName) == lower {
			return &nodes[i]
		}
	}
	return nil
}

func hasUnresolvedRefFrom(refs []types.UnresolvedReference, fromNodeID, name string) bool {
	for _, r := range refs {
		if r.FromNodeID == fromNodeID && r.ReferenceName == name && r.ReferenceKind == types.EdgeKindReferences {
			return true
		}
	}
	return false
}

const fkColumnFixture = `
CREATE TABLE child (
    id         INT NOT NULL,
    parent_no  INT NOT NULL,
    CONSTRAINT fk FOREIGN KEY (parent_no) REFERENCES parent (parent_no)
);

CREATE TABLE inline_child (
    id         INT NOT NULL,
    parent_no  INT REFERENCES parent (parent_no)
);

CREATE TABLE implicit_child (
    id      INT NOT NULL,
    fk_col  INT,
    FOREIGN KEY (fk_col) REFERENCES parent
);

CREATE TABLE composite_child (
    a  INT NOT NULL,
    b  INT NOT NULL,
    FOREIGN KEY (a, b) REFERENCES composite_parent (x, y)
);
`

// Each case asserts the references edge originates from the local COLUMN node rather
// than the table node — column nodes previously had no FK edges of their own.
func TestFKColumnLevelReferences(t *testing.T) {
	ext := newSQL()
	result, err := ext.Extract("/db/fk_columns.sql", fkColumnFixture)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	nodes := result.Nodes
	refs := result.UnresolvedReferences

	childCol := findSQLNodeByQName(nodes, types.NodeKindColumn, "child.parent_no")
	if childCol == nil {
		t.Fatal("expected column node 'child.parent_no'")
	}
	if !hasUnresolvedRefFrom(refs, childCol.ID, "parent.parent_no") {
		t.Errorf("expected references edge from child.parent_no's column node to 'parent.parent_no'; got: %v", uniqueRefNames(refs))
	}

	inlineCol := findSQLNodeByQName(nodes, types.NodeKindColumn, "inline_child.parent_no")
	if inlineCol == nil {
		t.Fatal("expected column node 'inline_child.parent_no'")
	}
	if !hasUnresolvedRefFrom(refs, inlineCol.ID, "parent.parent_no") {
		t.Errorf("expected references edge from inline_child.parent_no's column node to 'parent.parent_no'; got: %v", uniqueRefNames(refs))
	}

	// No target column list: implicit PK reference, target is the bare table.
	implicitCol := findSQLNodeByQName(nodes, types.NodeKindColumn, "implicit_child.fk_col")
	if implicitCol == nil {
		t.Fatal("expected column node 'implicit_child.fk_col'")
	}
	if !hasUnresolvedRefFrom(refs, implicitCol.ID, "parent") {
		t.Errorf("expected references edge from implicit_child.fk_col's column node to bare 'parent'; got: %v", uniqueRefNames(refs))
	}

	// Composite FK pairs positionally.
	colA := findSQLNodeByQName(nodes, types.NodeKindColumn, "composite_child.a")
	colB := findSQLNodeByQName(nodes, types.NodeKindColumn, "composite_child.b")
	if colA == nil || colB == nil {
		t.Fatal("expected column nodes 'composite_child.a' and 'composite_child.b'")
	}
	if !hasUnresolvedRefFrom(refs, colA.ID, "composite_parent.x") {
		t.Errorf("expected references edge from composite_child.a's column node to 'composite_parent.x'; got: %v", uniqueRefNames(refs))
	}
	if !hasUnresolvedRefFrom(refs, colB.ID, "composite_parent.y") {
		t.Errorf("expected references edge from composite_child.b's column node to 'composite_parent.y'; got: %v", uniqueRefNames(refs))
	}

	// The table→table edge must be unchanged.
	childTable := findSQLNodeByQName(nodes, types.NodeKindTable, "child")
	if childTable == nil {
		t.Fatal("expected table node 'child'")
	}
	if !hasUnresolvedRefFrom(refs, childTable.ID, "parent") {
		t.Errorf("expected pre-existing table->table references edge from 'child' table node to 'parent'; got: %v", uniqueRefNames(refs))
	}
}

// Malformed-DDL tolerance: a composite FK whose column lists differ in length pairs
// up to the shorter list and ignores the excess without erroring the file.
const fkColumnMismatchFixture = `
CREATE TABLE mismatch_child (
    a  INT NOT NULL,
    b  INT NOT NULL,
    c  INT NOT NULL,
    FOREIGN KEY (a, b, c) REFERENCES mismatch_parent (x)
);
`

func TestFKColumnLevelMismatchedListsIgnoreExcess(t *testing.T) {
	ext := newSQL()
	result, err := ext.Extract("/db/fk_mismatch.sql", fkColumnMismatchFixture)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	nodes := result.Nodes
	refs := result.UnresolvedReferences

	colA := findSQLNodeByQName(nodes, types.NodeKindColumn, "mismatch_child.a")
	colB := findSQLNodeByQName(nodes, types.NodeKindColumn, "mismatch_child.b")
	colC := findSQLNodeByQName(nodes, types.NodeKindColumn, "mismatch_child.c")
	if colA == nil || colB == nil || colC == nil {
		t.Fatal("expected column nodes 'a', 'b', 'c' on mismatch_child")
	}

	if !hasUnresolvedRefFrom(refs, colA.ID, "mismatch_parent.x") {
		t.Errorf("expected references edge from mismatch_child.a to 'mismatch_parent.x'; got: %v", uniqueRefNames(refs))
	}
	if hasUnresolvedRefFrom(refs, colB.ID, "mismatch_parent.x") || countUnresolvedRefs(refs, "mismatch_parent.y") > 0 {
		t.Errorf("excess local column 'b' must not produce a column-level FK ref; got: %v", uniqueRefNames(refs))
	}
	if hasUnresolvedRefFrom(refs, colC.ID, "mismatch_parent") {
		t.Errorf("excess local column 'c' must not produce a column-level FK ref; got: %v", uniqueRefNames(refs))
	}
}
