package standalone_test

// TestSQLExtensions_CanonicalSet verifies that:
//   1. SQLExtensions contains exactly the four known SQL file extensions.
//   2. IsSQLExt returns true for every canonical extension (case-insensitive).
//   3. IsSQLExt returns false for non-SQL standalone extensions (.vue, .go).
//   4. NewRegistry maps every canonical SQL extension to a non-nil extractor,
//      ensuring orchestrator and registry are always in sync.
//
// WHY this test exists: .sql/.ddl/.pgsql/.mysql was duplicated across
// standalone.go (NewRegistry), orchestrator.go (extToLanguage + standaloneExts),
// and pipeline.go (isStandaloneSQLExt). A single typo or omission in any
// consumer would cause silent parity drift (new SQL dialects routed by one
// but not the others). This test is the canary — it fails the moment any
// consumer diverges from the canonical set.

import (
	"sort"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction/standalone"
)

// wantSQLExts is the explicit enumeration used to drive all assertions below.
// It must be updated whenever the SQL extension list changes.
var wantSQLExts = []string{".sql", ".ddl", ".pgsql", ".mysql", ".sql.jinja"}

func TestSQLExtensions_CanonicalSet(t *testing.T) {
	t.Run("canonical slice has exactly the five known extensions", func(t *testing.T) {
		got := make([]string, len(standalone.SQLExtensions))
		copy(got, standalone.SQLExtensions)
		sort.Strings(got)

		want := make([]string, len(wantSQLExts))
		copy(want, wantSQLExts)
		sort.Strings(want)

		if len(got) != len(want) {
			t.Fatalf("SQLExtensions length = %d, want %d; got %v", len(got), len(want), got)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("SQLExtensions[%d] = %q, want %q", i, got[i], want[i])
			}
		}
	})

	t.Run("IsSQLExt true for every canonical ext (exact match)", func(t *testing.T) {
		for _, ext := range wantSQLExts {
			path := "/db/schema" + ext
			if !standalone.IsSQLExt(path) {
				t.Errorf("IsSQLExt(%q) = false, want true", path)
			}
		}
	})

	t.Run("IsSQLExt true for canonical exts uppercased (case-insensitive)", func(t *testing.T) {
		for _, ext := range wantSQLExts {
			path := "/db/schema" + strings.ToUpper(ext)
			if !standalone.IsSQLExt(path) {
				t.Errorf("IsSQLExt(%q) = false, want true (case-insensitive)", path)
			}
		}
	})

	t.Run("IsSQLExt false for non-SQL extensions", func(t *testing.T) {
		nonSQL := []string{
			"/app/component.vue",
			"/app/component.svelte",
			"/app/template.liquid",
			"/app/form.dfm",
			"/app/mapper.xml",
			"/main.go",
			"/main.ts",
			"/main.py",
		}
		for _, path := range nonSQL {
			if standalone.IsSQLExt(path) {
				t.Errorf("IsSQLExt(%q) = true, want false", path)
			}
		}
	})

	t.Run("NewRegistry has extractor for every canonical SQL ext", func(t *testing.T) {
		// pool is nil: SQL extractor is regex-based and ignores the pool.
		reg := standalone.NewRegistry(nil)
		for _, ext := range wantSQLExts {
			if e := reg.For(ext); e == nil {
				t.Errorf("NewRegistry().For(%q) = nil, want non-nil extractor", ext)
			}
		}
	})

	// D1: compound extension (.sql.jinja) must be recognised.
	t.Run("IsSQLExt true for .sql.jinja compound extension", func(t *testing.T) {
		cases := []string{
			"models/stg.sql.jinja",
			"/abs/path/stg.sql.jinja",
			"STG.SQL.JINJA", // case-insensitive
		}
		for _, p := range cases {
			if !standalone.IsSQLExt(p) {
				t.Errorf("IsSQLExt(%q) = false, want true (.sql.jinja must be a SQL ext)", p)
			}
		}
	})

}
