package resolution_test

// Path-alias loading tests (CP11).
//
// WHY these tests exist separately: the alias loader (tsconfig JSONC parse via
// hujson + alias map build) is a self-contained unit with its own edge cases
// (JSONC with comments/trailing commas, missing tsconfig, wildcard paths,
// baseUrl-only). Testing it in isolation makes failures easier to pin.

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/resolution"
)

func TestLoadPathAliases_JSONC(t *testing.T) {
	// WHY: tsconfig.json with JSONC syntax (comments + trailing commas) must
	// parse via hujson without error and produce the correct alias map.
	dir := t.TempDir()
	content := `{
		// project tsconfig — JSONC syntax
		"compilerOptions": {
			"baseUrl": ".",
			"paths": {
				"@app/*": ["src/*"],   // wildcard alias
				"@config": ["config/index.ts"],  // exact alias
			}
		}
	}`
	if err := os.WriteFile(filepath.Join(dir, "tsconfig.json"), []byte(content), 0o644); err != nil {
		t.Fatalf("write tsconfig: %v", err)
	}

	aliases, err := resolution.LoadPathAliases(dir)
	if err != nil {
		t.Fatalf("LoadPathAliases: %v", err)
	}
	if aliases == nil {
		t.Fatal("LoadPathAliases returned nil")
	}

	// Wildcard: @app/util → src/util
	resolved := aliases.Resolve("@app/util")
	if resolved == "" {
		t.Errorf("@app/util: expected a resolved path, got empty")
	}
	// Should contain "src/util" (base + substitution).
	want := "src/util"
	if resolved != want {
		t.Errorf("@app/util resolved to %q, want %q", resolved, want)
	}

	// Exact: @config → config/index.ts (strip .ts suffix for path matching)
	resolvedConfig := aliases.Resolve("@config")
	if resolvedConfig == "" {
		t.Errorf("@config: expected a resolved path, got empty")
	}
}

func TestLoadPathAliases_NoTsconfig(t *testing.T) {
	// WHY: if no tsconfig or jsconfig exists, LoadPathAliases must return an
	// empty (non-nil) AliasMap without error — not finding a tsconfig is normal
	// for non-TS repos.
	dir := t.TempDir()
	aliases, err := resolution.LoadPathAliases(dir)
	if err != nil {
		t.Fatalf("LoadPathAliases with no config: %v", err)
	}
	if aliases == nil {
		t.Fatal("expected non-nil AliasMap even when tsconfig absent")
	}
	// Resolve on an empty map must return "".
	if got := aliases.Resolve("@app/util"); got != "" {
		t.Errorf("empty alias map Resolve returned %q, want empty", got)
	}
}

func TestLoadPathAliases_BaseUrlOnly(t *testing.T) {
	// WHY: some tsconfigs only set baseUrl without paths; the loader must
	// record baseUrl and not error on missing paths key.
	dir := t.TempDir()
	content := `{ "compilerOptions": { "baseUrl": "src" } }`
	if err := os.WriteFile(filepath.Join(dir, "tsconfig.json"), []byte(content), 0o644); err != nil {
		t.Fatalf("write tsconfig: %v", err)
	}

	aliases, err := resolution.LoadPathAliases(dir)
	if err != nil {
		t.Fatalf("LoadPathAliases baseUrl-only: %v", err)
	}
	if aliases == nil {
		t.Fatal("expected non-nil AliasMap")
	}
	// BaseUrl should be "src"
	if aliases.BaseURL() != "src" {
		t.Errorf("BaseURL = %q, want %q", aliases.BaseURL(), "src")
	}
}

func TestLoadPathAliases_JsConfig(t *testing.T) {
	// WHY: jsconfig.json is the JS equivalent of tsconfig.json; it should be
	// loaded when tsconfig.json is absent.
	dir := t.TempDir()
	content := `{
		"compilerOptions": {
			"baseUrl": ".",
			"paths": { "@lib/*": ["lib/*"] }
		}
	}`
	if err := os.WriteFile(filepath.Join(dir, "jsconfig.json"), []byte(content), 0o644); err != nil {
		t.Fatalf("write jsconfig: %v", err)
	}

	aliases, err := resolution.LoadPathAliases(dir)
	if err != nil {
		t.Fatalf("LoadPathAliases jsconfig: %v", err)
	}

	got := aliases.Resolve("@lib/utils")
	want := "lib/utils"
	if got != want {
		t.Errorf("jsconfig @lib/utils = %q, want %q", got, want)
	}
}

func TestLoadPathAliases_Cached(t *testing.T) {
	// WHY: LoadPathAliases must cache the result so repeated calls for the same
	// projectRoot don't re-read the file each time.
	dir := t.TempDir()
	content := `{ "compilerOptions": { "paths": { "@x": ["x.ts"] } } }`
	p := filepath.Join(dir, "tsconfig.json")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write tsconfig: %v", err)
	}

	a1, err := resolution.LoadPathAliases(dir)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	// Remove the file and call again — should get cached result.
	_ = os.Remove(p)
	a2, err := resolution.LoadPathAliases(dir)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	// Both should point to the same underlying object (pointer equality).
	if a1 != a2 {
		t.Errorf("LoadPathAliases: second call returned a different object (not cached)")
	}
}
