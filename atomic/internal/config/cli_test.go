package config

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// helper: run Run with captured stdout/stderr, returns (exitCode, stdout, stderr).
func runCLI(t *testing.T, home string, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := Run(args, home, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

// --- path ---

func TestRun_path(t *testing.T) {
	home := t.TempDir()
	code, out, _ := runCLI(t, home, "path")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}
	want := TOMLPath(home)
	if strings.TrimSpace(out) != want {
		t.Fatalf("path: got %q, want %q", strings.TrimSpace(out), want)
	}
}

// --- get ---

func TestRun_get_default(t *testing.T) {
	home := t.TempDir()
	// No config.toml present — should return built-in default.
	code, out, _ := runCLI(t, home, "get", "output.intensity")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}
	if strings.TrimSpace(out) != "full" {
		t.Fatalf("get default: got %q, want %q", strings.TrimSpace(out), "full")
	}
}

func TestRun_get_after_set(t *testing.T) {
	home := t.TempDir()
	if code, _, _ := runCLI(t, home, "set", "output.intensity", "lite"); code != 0 {
		t.Fatal("set failed")
	}
	code, out, _ := runCLI(t, home, "get", "output.intensity")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}
	if strings.TrimSpace(out) != "lite" {
		t.Fatalf("get after set: got %q, want %q", strings.TrimSpace(out), "lite")
	}
}

func TestRun_get_unknown_key(t *testing.T) {
	home := t.TempDir()
	code, _, stderr := runCLI(t, home, "get", "output.bogus")
	if code != 1 {
		t.Fatalf("expected exit 1, got %d", code)
	}
	if !strings.Contains(stderr, "unknown key") {
		t.Fatalf("expected 'unknown key' in stderr, got %q", stderr)
	}
}

func TestRun_get_missing_arg(t *testing.T) {
	home := t.TempDir()
	code, _, stderr := runCLI(t, home, "get")
	if code != 2 {
		t.Fatalf("expected exit 2, got %d", code)
	}
	if !strings.Contains(stderr, "Usage") {
		t.Fatalf("expected usage in stderr, got %q", stderr)
	}
}

// --- set ---

func TestRun_set_valid(t *testing.T) {
	home := t.TempDir()
	code, _, _ := runCLI(t, home, "set", "output.intensity", "lite")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}
	// TOML file must exist.
	if _, err := os.Stat(TOMLPath(home)); err != nil {
		t.Fatalf("config.toml not created: %v", err)
	}
	// Resolved file must exist.
	if _, err := os.Stat(ResolvedPath(home)); err != nil {
		t.Fatalf("config.resolved.md not created: %v", err)
	}
}

func TestRun_set_invalid_value(t *testing.T) {
	home := t.TempDir()
	code, _, stderr := runCLI(t, home, "set", "output.intensity", "bogus")
	if code != 1 {
		t.Fatalf("expected exit 1, got %d", code)
	}
	if !strings.Contains(stderr, "bogus") {
		t.Fatalf("expected 'bogus' in stderr, got %q", stderr)
	}
}

func TestRun_set_unknown_key_typo_suggestion(t *testing.T) {
	home := t.TempDir()
	// "outpot.intensity" is Levenshtein distance 1 from "output.intensity"
	code, _, stderr := runCLI(t, home, "set", "outpot.intensity", "lite")
	if code != 1 {
		t.Fatalf("expected exit 1, got %d", code)
	}
	if !strings.Contains(stderr, "did you mean") {
		t.Fatalf("expected 'did you mean' in stderr, got %q", stderr)
	}
	if !strings.Contains(stderr, "output.intensity") {
		t.Fatalf("expected suggestion 'output.intensity' in stderr, got %q", stderr)
	}
}

func TestRun_set_unknown_key_no_suggestion(t *testing.T) {
	home := t.TempDir()
	code, _, stderr := runCLI(t, home, "set", "completely.unknown.key", "val")
	if code != 1 {
		t.Fatalf("expected exit 1, got %d", code)
	}
	if !strings.Contains(stderr, "unknown key") {
		t.Fatalf("expected 'unknown key' in stderr, got %q", stderr)
	}
}

func TestRun_set_missing_args(t *testing.T) {
	home := t.TempDir()
	code, _, stderr := runCLI(t, home, "set", "output.intensity")
	if code != 2 {
		t.Fatalf("expected exit 2, got %d", code)
	}
	if !strings.Contains(stderr, "Usage") {
		t.Fatalf("expected usage in stderr, got %q", stderr)
	}
}

func TestRun_set_rerenders_resolved(t *testing.T) {
	home := t.TempDir()
	runCLI(t, home, "set", "output.intensity", "lite")
	data, err := os.ReadFile(ResolvedPath(home))
	if err != nil {
		t.Fatalf("read resolved: %v", err)
	}
	if !strings.Contains(string(data), "lite") {
		t.Fatalf("resolved.md should contain 'lite', got: %s", string(data))
	}
}

// --- unset ---

func TestRun_unset_reverts_to_default(t *testing.T) {
	home := t.TempDir()
	runCLI(t, home, "set", "output.intensity", "lite")
	code, _, _ := runCLI(t, home, "unset", "output.intensity")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}
	code2, out, _ := runCLI(t, home, "get", "output.intensity")
	if code2 != 0 {
		t.Fatalf("expected exit 0 from get, got %d", code2)
	}
	if strings.TrimSpace(out) != "full" {
		t.Fatalf("after unset, expected 'full', got %q", strings.TrimSpace(out))
	}
}

func TestRun_unset_rerenders_resolved(t *testing.T) {
	home := t.TempDir()
	runCLI(t, home, "set", "output.intensity", "lite")
	runCLI(t, home, "unset", "output.intensity")
	data, err := os.ReadFile(ResolvedPath(home))
	if err != nil {
		t.Fatalf("read resolved: %v", err)
	}
	if !strings.Contains(string(data), "full") {
		t.Fatalf("resolved.md should contain 'full' after unset, got: %s", string(data))
	}
}

func TestRun_get_unknown_key_typo_suggestion(t *testing.T) {
	home := t.TempDir()
	// "outpot.intensity" is Levenshtein distance 1 from "output.intensity"
	code, _, stderr := runCLI(t, home, "get", "outpot.intensity")
	if code != 1 {
		t.Fatalf("expected exit 1, got %d", code)
	}
	if !strings.Contains(stderr, "did you mean") {
		t.Fatalf("expected 'did you mean' in stderr, got %q", stderr)
	}
	if !strings.Contains(stderr, `"output.intensity"`) {
		t.Fatalf("expected suggestion 'output.intensity' in stderr, got %q", stderr)
	}
}

func TestRun_unset_unknown_key_typo_suggestion(t *testing.T) {
	home := t.TempDir()
	// "outpot.intensity" is Levenshtein distance 1 from "output.intensity"
	code, _, stderr := runCLI(t, home, "unset", "outpot.intensity")
	if code != 1 {
		t.Fatalf("expected exit 1, got %d", code)
	}
	if !strings.Contains(stderr, "did you mean") {
		t.Fatalf("expected 'did you mean' in stderr, got %q", stderr)
	}
	if !strings.Contains(stderr, `"output.intensity"`) {
		t.Fatalf("expected suggestion 'output.intensity' in stderr, got %q", stderr)
	}
}

func TestRun_unset_unknown_key(t *testing.T) {
	home := t.TempDir()
	code, _, stderr := runCLI(t, home, "unset", "output.bogus")
	if code != 1 {
		t.Fatalf("expected exit 1, got %d", code)
	}
	if !strings.Contains(stderr, "unknown key") {
		t.Fatalf("expected 'unknown key' in stderr, got %q", stderr)
	}
}

func TestRun_unset_missing_arg(t *testing.T) {
	home := t.TempDir()
	code, _, stderr := runCLI(t, home, "unset")
	if code != 2 {
		t.Fatalf("expected exit 2, got %d", code)
	}
	if !strings.Contains(stderr, "Usage") {
		t.Fatalf("expected usage in stderr, got %q", stderr)
	}
}

// --- list ---

func TestRun_list_human(t *testing.T) {
	home := t.TempDir()
	// Without any config, should still list defaults.
	code, out, _ := runCLI(t, home, "list")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}
	if !strings.Contains(out, "output.intensity=full") {
		t.Fatalf("expected 'output.intensity=full' in output, got %q", out)
	}
}

func TestRun_list_human_after_set(t *testing.T) {
	home := t.TempDir()
	runCLI(t, home, "set", "output.intensity", "ultra")
	code, out, _ := runCLI(t, home, "list")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}
	if !strings.Contains(out, "output.intensity=ultra") {
		t.Fatalf("expected 'output.intensity=ultra', got %q", out)
	}
}

func TestRun_list_json(t *testing.T) {
	home := t.TempDir()
	code, out, _ := runCLI(t, home, "list", "--json")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}
	var m map[string]string
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatalf("invalid JSON: %v\noutput: %q", err, out)
	}
	if m["output.intensity"] != "full" {
		t.Fatalf("JSON: expected output.intensity=full, got %q", m["output.intensity"])
	}
}

func TestRun_list_json_sorted_keys(t *testing.T) {
	home := t.TempDir()
	_, out, _ := runCLI(t, home, "list", "--json")
	// Parse and re-encode to verify it's valid JSON with expected structure.
	var m map[string]string
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	// All known keys should be present.
	for _, k := range knownKeys {
		if _, ok := m[k]; !ok {
			t.Errorf("list --json missing key %q", k)
		}
	}
}

// --- no args / unknown verb ---

func TestRun_no_args(t *testing.T) {
	home := t.TempDir()
	code, _, stderr := runCLI(t, home /* no args */)
	if code != 2 {
		t.Fatalf("expected exit 2, got %d", code)
	}
	if !strings.Contains(stderr, "Usage") {
		t.Fatalf("expected usage in stderr, got %q", stderr)
	}
}

func TestRun_unknown_verb(t *testing.T) {
	home := t.TempDir()
	code, _, stderr := runCLI(t, home, "frobnicate")
	if code != 2 {
		t.Fatalf("expected exit 2, got %d", code)
	}
	if !strings.Contains(stderr, "unknown") {
		t.Fatalf("expected 'unknown' in stderr, got %q", stderr)
	}
}

// --- resolved.md parent dir creation ---

func TestRun_set_creates_parent_dirs(t *testing.T) {
	home := t.TempDir()
	// home/.atomic/ does not exist yet.
	atomicDir := filepath.Join(home, ".atomic")
	if _, err := os.Stat(atomicDir); !os.IsNotExist(err) {
		t.Fatal("pre-condition: .atomic dir should not exist yet")
	}
	code, _, _ := runCLI(t, home, "set", "output.intensity", "full")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}
	if _, err := os.Stat(atomicDir); err != nil {
		t.Fatalf(".atomic dir not created: %v", err)
	}
}
