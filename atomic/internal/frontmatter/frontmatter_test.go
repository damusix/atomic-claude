package frontmatter_test

import (
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/frontmatter"
)

// happy path: standard YAML frontmatter + body
func TestParse_Standard(t *testing.T) {
	input := "---\nid: r-7b21\ncreated: 2026-05-16\n---\n\nBody text.\n"
	meta, body, err := frontmatter.Parse(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if meta["id"] != "r-7b21" {
		t.Errorf("meta[id] = %q, want %q", meta["id"], "r-7b21")
	}
	if meta["created"] != "2026-05-16" {
		t.Errorf("meta[created] = %v, want %q", meta["created"], "2026-05-16")
	}
	if body != "\nBody text.\n" {
		t.Errorf("body = %q, want %q", body, "\nBody text.\n")
	}
}

// no frontmatter — body only
func TestParse_NoFrontmatter(t *testing.T) {
	input := "Just a body.\nNo frontmatter.\n"
	meta, body, err := frontmatter.Parse(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(meta) != 0 {
		t.Errorf("meta = %v, want empty", meta)
	}
	if body != input {
		t.Errorf("body = %q, want %q", body, input)
	}
}

// empty frontmatter block
func TestParse_EmptyFrontmatter(t *testing.T) {
	input := "---\n---\nBody after empty front.\n"
	meta, body, err := frontmatter.Parse(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(meta) != 0 {
		t.Errorf("meta = %v, want empty", meta)
	}
	if body != "Body after empty front.\n" {
		t.Errorf("body = %q, want %q", body, "Body after empty front.\n")
	}
}

// missing closing delimiter
func TestParse_MissingClosingDelimiter(t *testing.T) {
	input := "---\nid: r-1234\n"
	_, _, err := frontmatter.Parse(input)
	if err == nil {
		t.Fatal("expected error for missing closing delimiter, got nil")
	}
}

// invalid YAML
func TestParse_InvalidYAML(t *testing.T) {
	input := "---\n: invalid: yaml:\n---\nBody.\n"
	_, _, err := frontmatter.Parse(input)
	if err == nil {
		t.Fatal("expected error for invalid YAML, got nil")
	}
}

// Round-trip: body is preserved byte-for-byte; meta fields survive the cycle.
// The YAML block itself may be reformatted (yaml.Marshal reorders/quotes),
// so we check semantic equality of meta, not byte equality of the YAML block.
func TestRoundTrip_BodyPreserved(t *testing.T) {
	input := "---\nid: r-7b21\ncreated: 2026-05-16\n---\n\nBody text.\n"
	meta, body, err := frontmatter.Parse(input)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	got, err := frontmatter.Emit(meta, body)
	if err != nil {
		t.Fatalf("Emit error: %v", err)
	}

	// Re-parse the emitted output and verify we recover the same values.
	meta2, body2, err := frontmatter.Parse(got)
	if err != nil {
		t.Fatalf("re-Parse error: %v", err)
	}
	if body2 != body {
		t.Errorf("body not preserved: want %q, got %q", body, body2)
	}
	if meta2["id"] != "r-7b21" {
		t.Errorf("meta2[id] = %v, want r-7b21", meta2["id"])
	}
	if meta2["created"] != "2026-05-16" {
		t.Errorf("meta2[created] = %v, want 2026-05-16", meta2["created"])
	}
}

// body byte-for-byte preservation: body with special chars
func TestParse_BodyPreservation(t *testing.T) {
	body := "\n# Heading\n\n- item 1\n- item 2\n\n```go\nfmt.Println(\"hello\")\n```\n"
	input := "---\nkey: value\n---\n" + body
	_, gotBody, err := frontmatter.Parse(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotBody != body {
		t.Errorf("body not preserved:\n  want: %q\n  got:  %q", body, gotBody)
	}
}

// Emit with no meta should produce body-only (no frontmatter block)
func TestEmit_NoMeta(t *testing.T) {
	body := "Just a body.\n"
	got, err := frontmatter.Emit(nil, body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != body {
		t.Errorf("Emit(nil, body) = %q, want %q", got, body)
	}
}

// Emit with meta should include a frontmatter block
func TestEmit_WithMeta(t *testing.T) {
	meta := map[string]any{"id": "r-7b21", "created": "2026-05-16"}
	body := "\nSome body.\n"
	got, err := frontmatter.Emit(meta, body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Must start with ---\n and contain the closing delimiter.
	if len(got) < 8 || got[:4] != "---\n" {
		t.Errorf("Emit output does not start with '---\\n': %q", got)
	}
	// Body must be preserved exactly.
	if !containsSuffix(got, body) {
		t.Errorf("body %q not found at end of Emit output: %q", body, got)
	}
}

func containsSuffix(s, suffix string) bool {
	return len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix
}

// Emit must produce byte-identical output when called twice with the same map.
func TestEmit_Deterministic(t *testing.T) {
	meta := map[string]any{
		"zebra":   "last",
		"alpha":   "first",
		"middle":  "mid",
		"created": "2026-05-16",
	}
	body := "body content\n"

	out1, err := frontmatter.Emit(meta, body)
	if err != nil {
		t.Fatalf("Emit error (first call): %v", err)
	}
	out2, err := frontmatter.Emit(meta, body)
	if err != nil {
		t.Fatalf("Emit error (second call): %v", err)
	}
	if out1 != out2 {
		t.Errorf("Emit is not deterministic:\n  first:  %q\n  second: %q", out1, out2)
	}
}

// EmitOrdered preserves caller-specified key order (F-1).
func TestEmitOrdered_PreservesCallerOrder(t *testing.T) {
	kvs := []frontmatter.KV{
		{Key: "generated_at", Value: "2026-05-17T00:00:00Z"},
		{Key: "atomic_version", Value: "v0.1.0"},
	}
	out, err := frontmatter.EmitOrdered(kvs, "body\n")
	if err != nil {
		t.Fatalf("EmitOrdered: %v", err)
	}
	gaIdx := strings.Index(out, "generated_at:")
	avIdx := strings.Index(out, "atomic_version:")
	if gaIdx == -1 || avIdx == -1 {
		t.Fatalf("missing keys:\n%s", out)
	}
	if gaIdx > avIdx {
		t.Errorf("generated_at should precede atomic_version (caller order):\n%s", out)
	}
}

// EmitOrdered with reverse-alphabetical order must not be sorted alphabetically.
func TestEmitOrdered_NotAlphabetical(t *testing.T) {
	kvs := []frontmatter.KV{
		{Key: "z_first", Value: "1"},
		{Key: "a_second", Value: "2"},
	}
	out, err := frontmatter.EmitOrdered(kvs, "")
	if err != nil {
		t.Fatalf("EmitOrdered: %v", err)
	}
	zIdx := strings.Index(out, "z_first")
	aIdx := strings.Index(out, "a_second")
	if zIdx > aIdx {
		t.Errorf("caller order not preserved (z_first should precede a_second):\n%s", out)
	}
}

// EmitOrdered with no kvs returns body-only.
func TestEmitOrdered_EmptyKVs(t *testing.T) {
	out, err := frontmatter.EmitOrdered(nil, "just body\n")
	if err != nil {
		t.Fatalf("EmitOrdered: %v", err)
	}
	if out != "just body\n" {
		t.Errorf("expected body-only, got: %q", out)
	}
}

// ── ParseOrdered tests ────────────────────────────────────────────────────────

// TestParseOrdered_KeyOrder verifies that ParseOrdered preserves YAML source
// order, not alphabetical order. A Go map would produce {generated, repo, title}
// (alphabetical); ParseOrdered must return {title, repo, generated} (source order).
func TestParseOrdered_KeyOrder(t *testing.T) {
	input := "---\ntitle: \"@hapi/nes\"\nrepo: nes\ngenerated: 2026-06-13\n---\n\n# Overview\n"
	kvs, body, err := frontmatter.ParseOrdered(input)
	if err != nil {
		t.Fatalf("ParseOrdered: %v", err)
	}
	if body != "\n# Overview\n" {
		t.Errorf("body = %q, want %q", body, "\n# Overview\n")
	}
	if len(kvs) != 3 {
		t.Fatalf("len(kvs) = %d, want 3; kvs = %v", len(kvs), kvs)
	}
	// Source order must be preserved: title, repo, generated — not alphabetical.
	if kvs[0].Key != "title" {
		t.Errorf("kvs[0].Key = %q, want %q", kvs[0].Key, "title")
	}
	if kvs[1].Key != "repo" {
		t.Errorf("kvs[1].Key = %q, want %q", kvs[1].Key, "repo")
	}
	if kvs[2].Key != "generated" {
		t.Errorf("kvs[2].Key = %q, want %q", kvs[2].Key, "generated")
	}
}

// TestParseOrdered_DateAsString verifies that a date value stays a raw string,
// not coerced to time.Time (the same guarantee Parse gives).
func TestParseOrdered_DateAsString(t *testing.T) {
	input := "---\ngenerated: 2026-06-13\n---\nbody\n"
	kvs, _, err := frontmatter.ParseOrdered(input)
	if err != nil {
		t.Fatalf("ParseOrdered: %v", err)
	}
	if len(kvs) != 1 {
		t.Fatalf("len(kvs) = %d, want 1", len(kvs))
	}
	if kvs[0].Value != "2026-06-13" {
		t.Errorf("date value = %v (%T), want string %q", kvs[0].Value, kvs[0].Value, "2026-06-13")
	}
}

// TestParseOrdered_ListValue verifies that a sequence value parses as []any.
func TestParseOrdered_ListValue(t *testing.T) {
	input := "---\nsources:\n  - a\n  - b\n---\nbody\n"
	kvs, _, err := frontmatter.ParseOrdered(input)
	if err != nil {
		t.Fatalf("ParseOrdered: %v", err)
	}
	if len(kvs) != 1 || kvs[0].Key != "sources" {
		t.Fatalf("unexpected kvs: %v", kvs)
	}
	list, ok := kvs[0].Value.([]any)
	if !ok {
		t.Fatalf("expected []any, got %T", kvs[0].Value)
	}
	if len(list) != 2 || list[0] != "a" || list[1] != "b" {
		t.Errorf("list = %v, want [a b]", list)
	}
}

// TestParseOrdered_InlineListValue verifies that an inline sequence parses correctly.
func TestParseOrdered_InlineListValue(t *testing.T) {
	input := "---\nsources: [a, b]\n---\nbody\n"
	kvs, _, err := frontmatter.ParseOrdered(input)
	if err != nil {
		t.Fatalf("ParseOrdered: %v", err)
	}
	list, ok := kvs[0].Value.([]any)
	if !ok {
		t.Fatalf("expected []any, got %T", kvs[0].Value)
	}
	if len(list) != 2 {
		t.Errorf("list len = %d, want 2", len(list))
	}
}

// TestParseOrdered_NoFrontmatter verifies that a doc without frontmatter
// returns nil kvs and the full input as body.
func TestParseOrdered_NoFrontmatter(t *testing.T) {
	input := "# Just a heading\n\nNo frontmatter here.\n"
	kvs, body, err := frontmatter.ParseOrdered(input)
	if err != nil {
		t.Fatalf("ParseOrdered: %v", err)
	}
	if kvs != nil {
		t.Errorf("expected nil kvs, got %v", kvs)
	}
	if body != input {
		t.Errorf("body = %q, want full input", body)
	}
}

// TestParseOrdered_EmptyBlock verifies that an empty frontmatter block (---\n---\n)
// returns nil kvs and the remainder as body.
func TestParseOrdered_EmptyBlock(t *testing.T) {
	input := "---\n---\nBody after empty.\n"
	kvs, body, err := frontmatter.ParseOrdered(input)
	if err != nil {
		t.Fatalf("ParseOrdered: %v", err)
	}
	if kvs != nil {
		t.Errorf("expected nil kvs, got %v", kvs)
	}
	if body != "Body after empty.\n" {
		t.Errorf("body = %q, want %q", body, "Body after empty.\n")
	}
}

// TestParseOrdered_UnclosedBlock verifies that a missing closing delimiter
// returns an error (same behaviour as Parse).
func TestParseOrdered_UnclosedBlock(t *testing.T) {
	input := "---\ntitle: foo\n"
	_, _, err := frontmatter.ParseOrdered(input)
	if err == nil {
		t.Fatal("expected error for missing closing delimiter, got nil")
	}
}

// ── splitFrontmatter DRY refactor tests ──────────────────────────────────────
//
// These tests verify that after the splitFrontmatter extraction, Parse and
// ParseOrdered agree on the body for every splitting edge case. They are the
// observable contract for the shared delimiter-splitting logic.

// TestParseAndParseOrdered_BodyAgreement_Empty verifies that an empty document
// passes through both Parse and ParseOrdered identically.
func TestParseAndParseOrdered_BodyAgreement_Empty(t *testing.T) {
	inputs := []struct {
		name  string
		input string
	}{
		{"no-frontmatter", "# Heading\nbody\n"},
		{"empty-block", "---\n---\nremainder\n"},
		{"eof-no-newline", "---\n---"},
		{"standard", "---\nkey: val\n---\nbody\n"},
	}
	for _, tc := range inputs {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			_, body1, err1 := frontmatter.Parse(tc.input)
			_, body2, err2 := frontmatter.ParseOrdered(tc.input)
			// Both must agree on error/no-error.
			if (err1 != nil) != (err2 != nil) {
				t.Errorf("Parse err=%v vs ParseOrdered err=%v for input %q", err1, err2, tc.input)
			}
			// Both must agree on body when there is no error.
			if err1 == nil && err2 == nil && body1 != body2 {
				t.Errorf("body mismatch:\n  Parse:        %q\n  ParseOrdered: %q", body1, body2)
			}
		})
	}
}

// Parse→Emit→Parse round-trip: re-parsed map must equal the original.
func TestEmit_RoundTripParseEmitParse(t *testing.T) {
	input := "---\nalpha: one\nbeta: two\nzebra: last\n---\nBody here.\n"
	meta1, body1, err := frontmatter.Parse(input)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	emitted, err := frontmatter.Emit(meta1, body1)
	if err != nil {
		t.Fatalf("Emit error: %v", err)
	}

	meta2, body2, err := frontmatter.Parse(emitted)
	if err != nil {
		t.Fatalf("re-Parse error: %v", err)
	}
	if body2 != body1 {
		t.Errorf("body not preserved: want %q got %q", body1, body2)
	}
	for k, v := range meta1 {
		if meta2[k] != v {
			t.Errorf("meta2[%q] = %v, want %v", k, meta2[k], v)
		}
	}
	for k, v := range meta2 {
		if meta1[k] != v {
			t.Errorf("unexpected key %q in meta2 with value %v", k, v)
		}
	}
}
