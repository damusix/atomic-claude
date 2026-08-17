package standalone_test

// The SQL extension list is duplicated across NewRegistry, orchestrator, and
// pipeline; drift between them routes a dialect through one but not the others.
// This is the canary for that.

import (
	"sort"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction/standalone"
)

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
