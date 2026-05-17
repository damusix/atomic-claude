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
