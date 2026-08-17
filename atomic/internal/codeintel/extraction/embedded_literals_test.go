package extraction_test

// One generic harvester serves 16 host languages, so the cases below are picked
// to span both grammar shapes crossed with interpolation: Shape 1 keeps string
// content in a child node, Shape 2 has none and needs the delimiters stripped
// inline. The per-language configs are probed ground truth — see
// docs/spec/embedded-sql-language-expansion.md.

import (
	"context"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction"
)

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

// findEmbSpan matches Text exactly, not by substring — a partial match would
// pass on a span still carrying its delimiters.
func findEmbSpan(spans []extraction.EmbeddedSpan, want string) *extraction.EmbeddedSpan {
	for i := range spans {
		if spans[i].Text == want {
			return &spans[i]
		}
	}
	return nil
}

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

// Shape 1, no interpolation: string_literal wrapping a string_content child.
func TestHarvestEmbeddedLiterals_C_ContentChild(t *testing.T) {
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
	if span.StartLine != 2 {
		t.Errorf("StartLine=%d, want 2", span.StartLine)
	}
}

// Java 13+ text blocks land on multiline_string_fragment, a second content kind
// javaConfig must carry alongside string_fragment.
func TestHarvestEmbeddedLiterals_Java_ContentChild(t *testing.T) {
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
	// The fragment keeps the text block's indentation, so trim first — but then
	// compare exactly, since a substring check would pass on surrounding garbage.
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

// Shape 1 + interpolation. Substituting "?" for the interpolated segment is what
// makes an interpolated table name yield zero SQL refs downstream.
func TestHarvestEmbeddedLiterals_CSharp_InterpolatedString(t *testing.T) {
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
	pool := newEmbPool(t)
	src := `q = "SELECT a FROM #{t}"`
	spans := embHarvest(t, pool, src, extraction.LangRuby, rubyConfig())

	const want = "SELECT a FROM ?"
	span := findEmbSpan(spans, want)
	if span == nil {
		t.Fatalf("expected span with Text=%q; got spans: %v", want, spans)
	}
}

// Interpolating a value must not blank out the literal table name beside it,
// or the SQL ref is lost with it.
func TestHarvestEmbeddedLiterals_Ruby_LiteralTableInterpolatedValue(t *testing.T) {
	pool := newEmbPool(t)
	src := `q = "SELECT a FROM users WHERE id = #{id}"`
	spans := embHarvest(t, pool, src, extraction.LangRuby, rubyConfig())

	const want = "SELECT a FROM users WHERE id = ?"
	span := findEmbSpan(spans, want)
	if span == nil {
		t.Fatalf("expected span with Text=%q; got spans: %v", want, spans)
	}
}

// Shape 2: no content child, so the delimiter-alphabet stripper has to remove
// the [[ ]] pair itself.
func TestHarvestEmbeddedLiterals_Lua_LongBracketString(t *testing.T) {
	pool := newEmbPool(t)
	src := `local q = [[SELECT a FROM users]]`
	spans := embHarvest(t, pool, src, extraction.LangLua, luaConfig())

	const want = "SELECT a FROM users"
	span := findEmbSpan(spans, want)
	if span == nil {
		t.Fatalf("expected span with Text=%q; got spans: %v", want, spans)
	}
}

// The Lua grammar gives no string_content child for quoted strings either, so
// this shares the long-bracket path rather than the content-child one.
func TestHarvestEmbeddedLiterals_Lua_DoubleQuotedString(t *testing.T) {
	pool := newEmbPool(t)
	src := `local q = "CREATE TABLE x (id INT)"`
	spans := embHarvest(t, pool, src, extraction.LangLua, luaConfig())

	const want = "CREATE TABLE x (id INT)"
	span := findEmbSpan(spans, want)
	if span == nil {
		t.Fatalf("expected span with Text=%q; got spans: %v", want, spans)
	}
}

// Pascal exercises a single-quote delimiter through the same stripper.
func TestHarvestEmbeddedLiterals_Pascal_SingleQuotedString(t *testing.T) {
	pool := newEmbPool(t)
	src := `var q: string = 'CREATE TABLE x (id INT)';`
	spans := embHarvest(t, pool, src, extraction.LangPascal, pascalConfig())

	const want = "CREATE TABLE x (id INT)"
	span := findEmbSpan(spans, want)
	if span == nil {
		t.Fatalf("expected span with Text=%q; got spans: %v", want, spans)
	}
}

// Shape 2 + interpolation: with no content child to work from, the "?" must be
// byte-spliced over each template_substitution span.
func TestHarvestEmbeddedLiterals_Dart_InterpolatedString(t *testing.T) {
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
	pool := newEmbPool(t)
	src := `val q = s"SELECT a FROM $t"`
	spans := embHarvest(t, pool, src, extraction.LangScala, scalaConfig())

	const want = "SELECT a FROM ?"
	span := findEmbSpan(spans, want)
	if span == nil {
		t.Fatalf("expected span with Text=%q; got spans: %v", want, spans)
	}
}

// StartLine is file-absolute and 1-based, never relative to an enclosing scope.
func TestHarvestEmbeddedLiterals_FileAbsoluteLineNumbers(t *testing.T) {
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

// The same, for Shape 2 — where there is no content child to take the offset
// from, so pyByteToLine works off the string node itself.
func TestHarvestEmbeddedLiterals_Shape2_FileAbsoluteLineNumbers(t *testing.T) {
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

// A string that is nothing but an interpolation still leaves a non-empty "?",
// so the harvester emits it; dropping it is IsSQLLiteral's job downstream, not
// the harvester's. The single span is what proves the splice ran.
func TestHarvestEmbeddedLiterals_Dart_ZeroSpansWhenOnlyInterp(t *testing.T) {
	pool := newEmbPool(t)
	src := `var x = "$t";`
	spans := embHarvest(t, pool, src, extraction.LangDart, dartConfig())

	if len(spans) != 1 {
		t.Fatalf("expected 1 span (placeholder '?'), got %d: %v", len(spans), spans)
	}
	if spans[0].Text != "?" {
		t.Errorf("expected Text=%q, got %q", "?", spans[0].Text)
	}
}
