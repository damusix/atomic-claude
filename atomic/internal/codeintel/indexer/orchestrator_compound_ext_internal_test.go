package indexer

// Internal tests for compoundExt and the extToLanguage / standaloneExts maps
// as they relate to .sql.jinja routing (Part D1).
//
// WHY internal (not indexer_test): compoundExt is an unexported helper. Testing
// it through the public API would require a full IndexAll fixture, which is too
// coarse to catch a one-character typo in the helper. Internal test is the
// minimal path to a precise failing signal.

import (
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// TestCompoundExt verifies that compoundExt returns ".sql.jinja" for
// compound-extension paths and delegates to filepath.Ext for everything else.
//
// WHY: the orchestrator calls filepath.Ext at two sites (file-walk filter and
// per-file language lookup). A plain filepath.Ext("stg.sql.jinja") returns
// ".jinja", which has no entry in extToLanguage — the file is silently skipped.
// compoundExt is the fix; this test is the canary.
func TestCompoundExt(t *testing.T) {
	cases := []struct {
		path string
		want string
	}{
		// Compound extension: must return the full compound form.
		{"models/stg.sql.jinja", ".sql.jinja"},
		{"/abs/path/to/stg_orders.sql.jinja", ".sql.jinja"},
		{"STG.SQL.JINJA", ".sql.jinja"}, // case-insensitive via ToLower in caller
		// Plain SQL: single extension returned unchanged.
		{"a/b.sql", ".sql"},
		{"schema.ddl", ".ddl"},
		// Non-SQL: filepath.Ext behaviour preserved.
		{"main.go", ".go"},
		{"main.ts", ".ts"},
		{"no_ext", ""},
	}
	for _, c := range cases {
		got := compoundExt(c.path)
		if got != c.want {
			t.Errorf("compoundExt(%q) = %q, want %q", c.path, got, c.want)
		}
	}
}

// TestSqlJinjaInExtMaps verifies that init() has populated both extToLanguage
// and standaloneExts with ".sql.jinja" from standalone.SQLExtensions.
//
// WHY: the init() loop iterates standalone.SQLExtensions, so adding the entry
// to the slice is sufficient — but only if no one accidentally replaced the
// loop. This test guards that contract end-to-end.
func TestSqlJinjaInExtMaps(t *testing.T) {
	lang, ok := extToLanguage[".sql.jinja"]
	if !ok {
		t.Error("extToLanguage[\".sql.jinja\"] is absent — init() did not populate it from SQLExtensions")
	}
	if lang != types.LanguageSQL {
		t.Errorf("extToLanguage[\".sql.jinja\"] = %v, want LanguageSQL", lang)
	}

	if !standaloneExts[".sql.jinja"] {
		t.Error("standaloneExts[\".sql.jinja\"] is false — init() did not populate it from SQLExtensions")
	}
}
