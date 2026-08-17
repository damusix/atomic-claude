package standalone_test

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// Re-runnable Postgres: IF NOT EXISTS on every ALTER, CREATE DOMAIN wrapped in a
// DO block (Postgres has no IF NOT EXISTS form for DOMAIN). Both used to mis-parse —
// "IF" and "CONSTRAINT" captured as column names, DO-wrapped domains missed entirely.
const idempotentPgFixture = `
DO $$ BEGIN CREATE DOMAIN tg_no   AS bigint;      EXCEPTION WHEN duplicate_object THEN NULL; END $$;
DO $$ BEGIN CREATE DOMAIN tg_code AS varchar(50); EXCEPTION WHEN duplicate_object THEN NULL; END $$;

CREATE TABLE IF NOT EXISTS app_setting (
    param  text  NOT NULL,
    value  jsonb NOT NULL,
    CONSTRAINT pk_app_setting PRIMARY KEY (param)
);

CREATE TABLE IF NOT EXISTS sales_order_item (
    party_no       tg_no   NOT NULL,
    sales_order_no tg_no   NOT NULL,
    type           tg_code NOT NULL,

    CONSTRAINT pk_sales_order_item PRIMARY KEY (party_no, sales_order_no),
    CONSTRAINT fk_sales_order_item_is_a_line_of_sales_order
        FOREIGN KEY (party_no, sales_order_no)
        REFERENCES sales_order (party_no, sales_order_no) ON DELETE CASCADE,
    CONSTRAINT fk_sales_order_item_is_classified_by_item_type
        FOREIGN KEY (type) REFERENCES item_type (type)
);

DO $$
BEGIN
    ALTER TABLE app_setting ADD COLUMN IF NOT EXISTS updated_at tg_ts NOT NULL DEFAULT now();
    ALTER TABLE app_setting ADD COLUMN IF NOT EXISTS updated_by tg_no NOT NULL DEFAULT 0;
    ALTER TABLE app_setting ADD CONSTRAINT ck_app_setting_kind CHECK (kind IN ('a', 'b'));
END $$;
`

func TestIdempotentPostgresScript(t *testing.T) {
	ext := newSQL()
	result, err := ext.Extract("/db/config.sql", idempotentPgFixture)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	nodes := result.Nodes

	for _, name := range []string{"tg_no", "tg_code"} {
		if findSQLNode(nodes, types.NodeKindTypeAlias, name) == nil {
			t.Errorf("expected domain %q — a DO-wrapped CREATE DOMAIN is the only re-runnable form", name)
		}
	}

	for _, name := range []string{"updated_at", "updated_by"} {
		if findSQLNode(nodes, types.NodeKindColumn, name) == nil {
			t.Errorf("expected column %q from ALTER TABLE ADD COLUMN IF NOT EXISTS", name)
		}
	}

	// REFERENCES leads the continuation line of a multi-line table constraint,
	// which reads exactly like a `<name> <type>` column definition.
	for _, bogus := range []string{
		"IF", "if", "CONSTRAINT", "constraint", "NOT", "EXISTS", "REFERENCES", "references", "FOREIGN",
	} {
		if n := findSQLNode(nodes, types.NodeKindColumn, bogus); n != nil {
			t.Errorf("keyword %q was minted as a column node", bogus)
		}
	}

	for _, name := range []string{"party_no", "sales_order_no"} {
		if findSQLNode(nodes, types.NodeKindColumn, name) == nil {
			t.Errorf("expected column %q on sales_order_item", name)
		}
	}

	if findSQLNode(nodes, types.NodeKindConstraint, "ck_app_setting_kind") == nil {
		t.Error("expected constraint 'ck_app_setting_kind' from ALTER TABLE ADD CONSTRAINT")
	}
}

func constraintCols(t *testing.T, nodes []types.Node, name string) []string {
	t.Helper()
	n := findSQLNode(nodes, types.NodeKindConstraint, name)
	if n == nil {
		t.Fatalf("constraint %q not extracted", name)
	}
	var meta struct {
		Columns []string `json:"columns"`
	}
	if len(n.Metadata) > 0 {
		if err := json.Unmarshal(n.Metadata, &meta); err != nil {
			t.Fatalf("constraint %q metadata does not parse: %v", name, err)
		}
	}
	return meta.Columns
}

// Columns come from the declaration, never the constraint's name: pk_<table>
// naming defeats name-guessing, and so does a table named after its own column.
func TestConstraintsRecordTheirActualColumns(t *testing.T) {
	ext := newSQL()
	result, err := ext.Extract("/db/config.sql", idempotentPgFixture)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	nodes := result.Nodes

	// The name says nothing about the column it covers.
	if got := constraintCols(t, nodes, "pk_app_setting"); !slices.Equal(got, []string{"param"}) {
		t.Errorf("pk_app_setting columns = %v, want [param]", got)
	}

	if got := constraintCols(t, nodes, "pk_sales_order_item"); !slices.Equal(got, []string{"party_no", "sales_order_no"}) {
		t.Errorf("pk_sales_order_item columns = %v, want [party_no sales_order_no]", got)
	}

	// Local columns, not the REFERENCES target's list — spelled the same on purpose.
	if got := constraintCols(t, nodes, "fk_sales_order_item_is_a_line_of_sales_order"); !slices.Equal(got, []string{"party_no", "sales_order_no"}) {
		t.Errorf("multi-line FK columns = %v, want [party_no sales_order_no]", got)
	}

	if got := constraintCols(t, nodes, "fk_sales_order_item_is_classified_by_item_type"); !slices.Equal(got, []string{"type"}) {
		t.Errorf("single-line FK columns = %v, want [type]", got)
	}

	// A CHECK holds an expression, not a column list.
	if got := constraintCols(t, nodes, "ck_app_setting_kind"); len(got) != 0 {
		t.Errorf("CHECK constraint reported columns %v; expressions are not column lists", got)
	}
}
