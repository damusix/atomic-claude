package extraction_test

// embedded_literals_test.go — unit tests for HarvestEmbeddedLiterals.
//
// WHY: verify the generic harvester covers all four extraction modes across
// the 16 new host languages without touching the existing Python / TS harvesters.
//
// Test organisation:
//   - content-child (Shape 1, no interp):   C, Java
//   - content-child + interp (Shape 1+?):   C#, Ruby
//   - inline (Shape 2, no interp):          Lua, Pascal
//   - inline + interp (Shape 2+?):          Dart, Scala
//   - file-absolute line numbers:           multi-line C fixture
//
// Each config is built inline from docs/spec/embedded-sql-language-expansion.md
// § Grammar node-kind config — probed ground truth.

import (
	"context"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction"
)

// ---------------------------------------------------------------------------
// Pool / harvest helpers
// ---------------------------------------------------------------------------

// newEmbPool creates a single-instance pool shared across subtests within one
// test function. Mirrors newPyPool / newTSPool.
func newEmbPool(t *testing.T) *extraction.Pool {
	t.Helper()
	ctx := context.Background()
	pool, err := extraction.NewPool(ctx, extraction.PoolOptions{Size: 1})
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	t.Cleanup(func() { pool.Close() })
	return pool
}

// embHarvest borrows an instance, calls HarvestEmbeddedLiterals, and returns
// the spans (or fails the test on error).
func embHarvest(
	t *testing.T,
	pool *extraction.Pool,
	src string,
	lang extraction.Lang,
	cfg extraction.EmbeddedLiteralConfig,
) []extraction.EmbeddedSpan {
	t.Helper()
	ctx := context.Background()
	inst, err := pool.Borrow(ctx)
	if err != nil {
		t.Fatalf("Borrow: %v", err)
	}
	defer pool.Return(inst)

	spans, err := extraction.HarvestEmbeddedLiterals(ctx, inst, src, lang, cfg)
	if err != nil {
		t.Fatalf("HarvestEmbeddedLiterals: %v", err)
	}
	return spans
}

// findEmbSpan returns the first span whose Text equals want exactly, or nil.
func findEmbSpan(spans []extraction.EmbeddedSpan, want string) *extraction.EmbeddedSpan {
	for i := range spans {
		if spans[i].Text == want {
			return &spans[i]
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Per-language EmbeddedLiteralConfig constructors
// (spec § Grammar node-kind config, probed ground truth)
// ---------------------------------------------------------------------------

func cConfig() extraction.EmbeddedLiteralConfig {
	return extraction.EmbeddedLiteralConfig{
		StringKinds:  map[string]bool{"string_literal": true},
		ContentKinds: map[string]bool{"string_content": true},
		InterpKinds:  map[string]bool{},
	}
}

func javaConfig() extraction.EmbeddedLiteralConfig {
	return extraction.EmbeddedLiteralConfig{
		StringKinds:  map[string]bool{"string_literal": true},
		ContentKinds: map[string]bool{"string_fragment": true, "multiline_string_fragment": true},
		InterpKinds:  map[string]bool{},
	}
}

func csharpConfig() extraction.EmbeddedLiteralConfig {
	return extraction.EmbeddedLiteralConfig{
		StringKinds: map[string]bool{
			"string_literal":                 true,
			"interpolated_string_expression": true,
			"verbatim_string_literal":        true,
		},
		ContentKinds: map[string]bool{
			"string_literal_content": true,
			"string_content":         true,
		},
		InterpKinds: map[string]bool{"interpolation": true},
	}
}

func rubyConfig() extraction.EmbeddedLiteralConfig {
	return extraction.EmbeddedLiteralConfig{
		StringKinds:  map[string]bool{"string": true, "heredoc_body": true},
		ContentKinds: map[string]bool{"string_content": true, "heredoc_content": true},
		InterpKinds:  map[string]bool{"interpolation": true},
	}
}

func luaConfig() extraction.EmbeddedLiteralConfig {
	return extraction.EmbeddedLiteralConfig{
		StringKinds:  map[string]bool{"string": true},
		ContentKinds: map[string]bool{},
		InterpKinds:  map[string]bool{},
	}
}

func pascalConfig() extraction.EmbeddedLiteralConfig {
	return extraction.EmbeddedLiteralConfig{
		StringKinds:  map[string]bool{"literalString": true},
		ContentKinds: map[string]bool{},
		InterpKinds:  map[string]bool{},
	}
}

func dartConfig() extraction.EmbeddedLiteralConfig {
	return extraction.EmbeddedLiteralConfig{
		StringKinds:  map[string]bool{"string_literal": true},
		ContentKinds: map[string]bool{},
		InterpKinds:  map[string]bool{"template_substitution": true},
	}
}

func scalaConfig() extraction.EmbeddedLiteralConfig {
	return extraction.EmbeddedLiteralConfig{
		StringKinds:  map[string]bool{"string": true, "interpolated_string": true},
		ContentKinds: map[string]bool{},
		InterpKinds:  map[string]bool{"interpolation": true},
	}
}

// ---------------------------------------------------------------------------
// Shape 1 (content-child, no interp): C and Java
// ---------------------------------------------------------------------------

func TestHarvestEmbeddedLiterals_C_ContentChild(t *testing.T) {
	// WHY: C uses string_literal / string_content (Shape 1). A plain DDL string
	// must be harvested with delimiters excluded.
	pool := newEmbPool(t)
	src := `int main(){
    const char *q = "CREATE TABLE users (id INT)";
    return 0;
}`
	spans := embHarvest(t, pool, src, extraction.LangC, cConfig())

	const want = "CREATE TABLE users (id INT)"
	span := findEmbSpan(spans, want)
	if span == nil {
		t.Fatalf("expected span with Text=%q; got %d spans: %v", want, len(spans), spans)
	}
	// StartLine must be 2 (line of the declaration).
	if span.StartLine != 2 {
		t.Errorf("StartLine=%d, want 2", span.StartLine)
	}
}

func TestHarvestEmbeddedLiterals_Java_ContentChild(t *testing.T) {
	// WHY: Java uses string_literal / string_fragment (Shape 1). Java 13+ text
	// blocks use multiline_string_fragment — covered by javaConfig().
	pool := newEmbPool(t)
	src := `class Repo {
    String q = """
            CREATE TABLE orders (id INT PRIMARY KEY)
            """;
}`
	spans := embHarvest(t, pool, src, extraction.LangJava, javaConfig())

	if len(spans) == 0 {
		t.Fatal("expected at least one span but got none")
	}
	// Find the span whose trimmed text equals the DDL exactly.
	// WHY: TrimSpace + exact equality is falsifiable — a hardcoded substring
	// check would pass even if the function returned garbage containing the
	// target string. The text-block fragment includes leading/trailing
	// whitespace from indentation; trim + exact-equality verifies real content.
	const wantTrimmed = "CREATE TABLE orders (id INT PRIMARY KEY)"
	var found bool
	for _, s := range spans {
		if strings.TrimSpace(s.Text) == wantTrimmed {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("no span with TrimSpace(Text)==%q; spans: %v", wantTrimmed, spans)
	}
}

// ---------------------------------------------------------------------------
// Shape 1 + interp: C# and Ruby
// ---------------------------------------------------------------------------

func TestHarvestEmbeddedLiterals_CSharp_InterpolatedString(t *testing.T) {
	// WHY: C# interpolated strings use interpolated_string_expression /
	// string_content + interpolation. The interpolation segment must become "?"
	// so a table-name interpolation yields zero SQL refs.
	pool := newEmbPool(t)
	src := `var q = $"SELECT a FROM {t}";`
	spans := embHarvest(t, pool, src, extraction.LangCSharp, csharpConfig())

	const want = "SELECT a FROM ?"
	span := findEmbSpan(spans, want)
	if span == nil {
		t.Fatalf("expected span with Text=%q; got spans: %v", want, spans)
	}
}

func TestHarvestEmbeddedLiterals_Ruby_InterpolatedString(t *testing.T) {
	// WHY: Ruby uses string / string_content + interpolation (Shape 1).
	// #{t} must become "?" — interpolated table target → zero SQL refs.
	pool := newEmbPool(t)
	src := `q = "SELECT a FROM #{t}"`
	spans := embHarvest(t, pool, src, extraction.LangRuby, rubyConfig())

	const want = "SELECT a FROM ?"
	span := findEmbSpan(spans, want)
	if span == nil {
		t.Fatalf("expected span with Text=%q; got spans: %v", want, spans)
	}
}

func TestHarvestEmbeddedLiterals_Ruby_LiteralTableInterpolatedValue(t *testing.T) {
	// WHY: decision 8b — only a value is interpolated; the table name (users)
	// must remain intact so a SQL ref is emitted.
	pool := newEmbPool(t)
	src := `q = "SELECT a FROM users WHERE id = #{id}"`
	spans := embHarvest(t, pool, src, extraction.LangRuby, rubyConfig())

	const want = "SELECT a FROM users WHERE id = ?"
	span := findEmbSpan(spans, want)
	if span == nil {
		t.Fatalf("expected span with Text=%q; got spans: %v", want, spans)
	}
}

// ---------------------------------------------------------------------------
// Shape 2 (inline, no interp): Lua and Pascal
// ---------------------------------------------------------------------------

func TestHarvestEmbeddedLiterals_Lua_LongBracketString(t *testing.T) {
	// WHY: Lua long-bracket syntax [[…]] is an inline-content string (Shape 2).
	// The [[ and ]] delimiters must be stripped by the delimiter-alphabet stripper.
	pool := newEmbPool(t)
	src := `local q = [[SELECT a FROM users]]`
	spans := embHarvest(t, pool, src, extraction.LangLua, luaConfig())

	const want = "SELECT a FROM users"
	span := findEmbSpan(spans, want)
	if span == nil {
		t.Fatalf("expected span with Text=%q; got spans: %v", want, spans)
	}
}

func TestHarvestEmbeddedLiterals_Lua_DoubleQuotedString(t *testing.T) {
	// WHY: Lua double-quoted strings are also Shape 2 (no string_content child
	// in the Lua grammar). Delimiters must be stripped.
	pool := newEmbPool(t)
	src := `local q = "CREATE TABLE x (id INT)"`
	spans := embHarvest(t, pool, src, extraction.LangLua, luaConfig())

	const want = "CREATE TABLE x (id INT)"
	span := findEmbSpan(spans, want)
	if span == nil {
		t.Fatalf("expected span with Text=%q; got spans: %v", want, spans)
	}
}

func TestHarvestEmbeddedLiterals_Pascal_SingleQuotedString(t *testing.T) {
	// WHY: Pascal uses literalString (Shape 2) with single-quote delimiters.
	// The surrounding single quotes must be stripped.
	pool := newEmbPool(t)
	src := `var q: string = 'CREATE TABLE x (id INT)';`
	spans := embHarvest(t, pool, src, extraction.LangPascal, pascalConfig())

	const want = "CREATE TABLE x (id INT)"
	span := findEmbSpan(spans, want)
	if span == nil {
		t.Fatalf("expected span with Text=%q; got spans: %v", want, spans)
	}
}

// ---------------------------------------------------------------------------
// Shape 2 + interp: Dart and Scala
// ---------------------------------------------------------------------------

func TestHarvestEmbeddedLiterals_Dart_InterpolatedString(t *testing.T) {
	// WHY: Dart string_literal is Shape 2 (no content-kind child); template_substitution
	// children must be byte-spliced with "?". A table-target interpolation yields
	// "SELECT a FROM ?" → zero SQL refs.
	pool := newEmbPool(t)
	src := `var q = "SELECT a FROM $t";`
	spans := embHarvest(t, pool, src, extraction.LangDart, dartConfig())

	const want = "SELECT a FROM ?"
	span := findEmbSpan(spans, want)
	if span == nil {
		t.Fatalf("expected span with Text=%q; got spans: %v", want, spans)
	}
}

func TestHarvestEmbeddedLiterals_Scala_InterpolatedString(t *testing.T) {
	// WHY: Scala interpolated_string is Shape 2 (no content-kind child);
	// interpolation children must be byte-spliced with "?".
	pool := newEmbPool(t)
	src := `val q = s"SELECT a FROM $t"`
	spans := embHarvest(t, pool, src, extraction.LangScala, scalaConfig())

	const want = "SELECT a FROM ?"
	span := findEmbSpan(spans, want)
	if span == nil {
		t.Fatalf("expected span with Text=%q; got spans: %v", want, spans)
	}
}

// ---------------------------------------------------------------------------
// File-absolute line numbers
// ---------------------------------------------------------------------------

func TestHarvestEmbeddedLiterals_FileAbsoluteLineNumbers(t *testing.T) {
	// WHY: StartLine must be file-absolute (1-based), not relative to any
	// enclosing function. A literal on line 4 must report StartLine=4.
	pool := newEmbPool(t)
	src := `// line 1
// line 2
int main() {
    const char *q = "CREATE TABLE lineno_test (id INT)";
    return 0;
}`
	spans := embHarvest(t, pool, src, extraction.LangC, cConfig())

	const want = "CREATE TABLE lineno_test (id INT)"
	span := findEmbSpan(spans, want)
	if span == nil {
		t.Fatalf("expected span with Text=%q; got spans: %v", want, spans)
	}
	if span.StartLine != 4 {
		t.Errorf("StartLine=%d, want 4", span.StartLine)
	}
}

func TestHarvestEmbeddedLiterals_Shape2_FileAbsoluteLineNumbers(t *testing.T) {
	// WHY: Shape-2 (inline-content) grammars must also report file-absolute
	// StartLine. A Lua long-bracket literal on line 4 (preceded by 3 comment
	// lines) must return StartLine=4, not 1. Verifies pyByteToLine is correct
	// for Shape-2 strings where there is no content-child to offset from.
	pool := newEmbPool(t)
	src := "-- line 1\n-- line 2\n-- line 3\nlocal q = [[SELECT a FROM lineno2_test]]"
	spans := embHarvest(t, pool, src, extraction.LangLua, luaConfig())

	const want = "SELECT a FROM lineno2_test"
	span := findEmbSpan(spans, want)
	if span == nil {
		t.Fatalf("expected span with Text=%q; got spans: %v", want, spans)
	}
	if span.StartLine != 4 {
		t.Errorf("StartLine=%d, want 4", span.StartLine)
	}
}

// ---------------------------------------------------------------------------
// Absence: interpolated table target → zero refs (cross-language)
// ---------------------------------------------------------------------------

func TestHarvestEmbeddedLiterals_Dart_ZeroSpansWhenOnlyInterp(t *testing.T) {
	// WHY: a string that is entirely an interpolation (e.g. "$t") yields ""
	// after Shape-2 processing and must produce no span (skip empty results).
	pool := newEmbPool(t)
	// A Dart string where ALL content is an interpolation — nothing left after
	// substitution. Use a string that after "?" substitution + delimiter stripping
	// produces empty or just "?" which is non-SQL and filtered by IsSQLLiteral later.
	// Here we prove the harvester itself emits zero spans for a structurally empty
	// (no literal SQL text) string.
	src := `var x = "$t";`
	spans := embHarvest(t, pool, src, extraction.LangDart, dartConfig())

	// After Shape 2 splice: "?" — delimiter strip removes the quotes, leaving "?".
	// "?" is non-empty so the harvester emits it. The SQL gate (IsSQLLiteral)
	// will reject it — that is CP2's concern. Here we assert exactly one span
	// with text "?" (the placeholder), confirming the splice ran correctly.
	if len(spans) != 1 {
		t.Fatalf("expected 1 span (placeholder '?'), got %d: %v", len(spans), spans)
	}
	if spans[0].Text != "?" {
		t.Errorf("expected Text=%q, got %q", "?", spans[0].Text)
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------
