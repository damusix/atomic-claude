package wiki

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// goldenRepoScaffold is an independently-copied fixture of the repo-scope
// scaffold. Kept separate from repoSteeringScaffold in init.go so this test
// catches accidental drift in the source constant, not just self-consistency
// with it. Invariants the fixture encodes: every illustrative example lives
// inside an HTML comment, and no example names a concrete framework, tool,
// or path — an uncustomized scaffold contains zero repo facts a model could
// pick up from context.
const goldenRepoScaffold = `---
type: Steering
description: Authoritative steering for the signals/wiki inferrer when operating under docs/wiki/.
---

<!-- steering note: user hints to correct framework detection / domain grouping / build-test
 commands; the inferrer reads this and treats it as authoritative. The sections below start
 empty — fill them with facts about THIS repo. Other HTML comments are illustrative examples
 only; the inferrer must never treat them as steering. This note is an HTML comment, not a
 <pseudo-tag>: docs/ directories swept by VitePress feed every .md through the Vue template
 compiler, which rejects pseudo-tag syntax and fails the site build. -->

## Framework

<!-- example: <the real framework> (not <what detection wrongly guessed>) -->

## Domains

<!-- example:
- <dir-a>/ and <dir-b>/ are one domain ("<domain-name>")
- <dir-c>/ is scratch code — not a real domain
-->

## Build

<!-- example:
- Build: <build command>
- Test: <ci test command> (not <the watch-mode command>)
-->

## Ignore for domains

<!-- example:
- <vendored-dir>/
- <generated-output-dir>/
-->
`

func TestInitRepoScope_WritesGoldenScaffold(t *testing.T) {
	root := t.TempDir()

	created, err := InitRepoScope(root)
	if err != nil {
		t.Fatalf("InitRepoScope: %v", err)
	}
	if !created {
		t.Error("expected created=true on first write")
	}

	path := filepath.Join(root, "docs", "wiki", "CLAUDE.md")
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if string(got) != goldenRepoScaffold {
		t.Errorf("content mismatch:\ngot:\n%s\nwant:\n%s", got, goldenRepoScaffold)
	}
}

func TestInitRepoScope_NoopWhenFileExists(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "docs", "wiki", "CLAUDE.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	existing := "user-edited content — do not overwrite\n"
	if err := os.WriteFile(path, []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}

	created, err := InitRepoScope(root)
	if err != nil {
		t.Fatalf("InitRepoScope: %v", err)
	}
	if created {
		t.Error("expected created=false when file already exists")
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != existing {
		t.Errorf("existing file was overwritten:\ngot:\n%s\nwant:\n%s", got, existing)
	}
}

func TestInitRealmScope_WritesIndexReference(t *testing.T) {
	root := t.TempDir()

	created, err := InitRealmScope(root)
	if err != nil {
		t.Fatalf("InitRealmScope: %v", err)
	}
	if !created {
		t.Error("expected created=true on first write")
	}

	path := filepath.Join(root, "wiki", "CLAUDE.md")
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if string(got) != "@index.md\n" {
		t.Errorf("content = %q, want %q", got, "@index.md\n")
	}
}

func TestInitRealmScope_NoopWhenFileExists(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "wiki", "CLAUDE.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	existing := "@index.md\n\nuser addition\n"
	if err := os.WriteFile(path, []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}

	created, err := InitRealmScope(root)
	if err != nil {
		t.Fatalf("InitRealmScope: %v", err)
	}
	if created {
		t.Error("expected created=false when file already exists")
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != existing {
		t.Errorf("existing file was overwritten:\ngot:\n%s\nwant:\n%s", got, existing)
	}
}

func TestWikiInitAction_MissingScope_ExitsUsageError_WritesNothing(t *testing.T) {
	root := t.TempDir()
	var buf bytes.Buffer

	code := wikiInitAction([]string{"--root=" + root}, root, &buf)
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if _, err := os.Stat(filepath.Join(root, "docs", "wiki", "CLAUDE.md")); !os.IsNotExist(err) {
		t.Error("expected no file written on missing --scope")
	}
	if _, err := os.Stat(filepath.Join(root, "wiki", "CLAUDE.md")); !os.IsNotExist(err) {
		t.Error("expected no file written on missing --scope")
	}
}

func TestWikiInitAction_InvalidScope_ExitsUsageError_WritesNothing(t *testing.T) {
	root := t.TempDir()
	var buf bytes.Buffer

	code := wikiInitAction([]string{"--scope=bogus", "--root=" + root}, root, &buf)
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if _, err := os.Stat(filepath.Join(root, "docs", "wiki", "CLAUDE.md")); !os.IsNotExist(err) {
		t.Error("expected no file written on invalid --scope")
	}
}

func TestWikiInitAction_RepoScope_ViaCLI(t *testing.T) {
	root := t.TempDir()
	var buf bytes.Buffer

	code := wikiInitAction([]string{"--scope=repo", "--root=" + root}, root, &buf)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0, output:\n%s", code, buf.String())
	}

	path := filepath.Join(root, "docs", "wiki", "CLAUDE.md")
	if _, err := os.Stat(path); err != nil {
		t.Errorf("expected %s to exist: %v", path, err)
	}
	if !strings.Contains(buf.String(), path) {
		t.Errorf("expected stdout to mention created path %s, got:\n%s", path, buf.String())
	}

	markerPath := filepath.Join(root, ".claude", "atomic.toml")
	got, err := os.ReadFile(markerPath)
	if err != nil {
		t.Fatalf("expected %s to exist: %v", markerPath, err)
	}
	if string(got) != "scope = \"repo\"\n" {
		t.Errorf("marker content = %q, want %q", got, "scope = \"repo\"\n")
	}
	if !strings.Contains(buf.String(), markerPath) {
		t.Errorf("expected stdout to mention created marker %s, got:\n%s", markerPath, buf.String())
	}
}

func TestWikiInitAction_RealmScope_ViaCLI(t *testing.T) {
	root := t.TempDir()
	var buf bytes.Buffer

	code := wikiInitAction([]string{"--scope=realm", "--root=" + root}, root, &buf)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0, output:\n%s", code, buf.String())
	}

	path := filepath.Join(root, "wiki", "CLAUDE.md")
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected %s to exist: %v", path, err)
	}
	if string(got) != "@index.md\n" {
		t.Errorf("content = %q, want %q", got, "@index.md\n")
	}

	markerPath := filepath.Join(root, ".claude", "atomic.toml")
	markerGot, err := os.ReadFile(markerPath)
	if err != nil {
		t.Fatalf("expected %s to exist: %v", markerPath, err)
	}
	if string(markerGot) != "scope = \"realm\"\n" {
		t.Errorf("marker content = %q, want %q", markerGot, "scope = \"realm\"\n")
	}
}

// TestWikiInitAction_ScopeConflict_ExitsError_LeavesMarkerUntouched: a root
// whose marker already declares a different scope is never rewritten —
// wiki init errors instead of flipping the user's committed declaration.
func TestWikiInitAction_ScopeConflict_ExitsError_LeavesMarkerUntouched(t *testing.T) {
	root := t.TempDir()
	markerPath := filepath.Join(root, ".claude", "atomic.toml")
	if err := os.MkdirAll(filepath.Dir(markerPath), 0o755); err != nil {
		t.Fatal(err)
	}
	existing := "scope = \"repo\"\n"
	if err := os.WriteFile(markerPath, []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	code := wikiInitAction([]string{"--scope=realm", "--root=" + root}, root, &buf)
	if code != 1 {
		t.Errorf("exit code = %d, want 1, output:\n%s", code, buf.String())
	}

	got, err := os.ReadFile(markerPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != existing {
		t.Errorf("marker must be left untouched on conflict:\ngot:\n%s\nwant:\n%s", got, existing)
	}

	if _, err := os.Stat(filepath.Join(root, "wiki", "CLAUDE.md")); !os.IsNotExist(err) {
		t.Error("expected no CLAUDE.md scaffold written when scope marker conflicts")
	}
}

// TestWikiInitAction_ScopeMarkerIdempotent: a second run with the same
// --scope reports success and writes nothing further to the marker.
func TestWikiInitAction_ScopeMarkerIdempotent(t *testing.T) {
	root := t.TempDir()
	var buf1 bytes.Buffer
	if code := wikiInitAction([]string{"--scope=repo", "--root=" + root}, root, &buf1); code != 0 {
		t.Fatalf("first run exit code = %d, output:\n%s", code, buf1.String())
	}

	markerPath := filepath.Join(root, ".claude", "atomic.toml")
	before, err := os.ReadFile(markerPath)
	if err != nil {
		t.Fatal(err)
	}

	var buf2 bytes.Buffer
	if code := wikiInitAction([]string{"--scope=repo", "--root=" + root}, root, &buf2); code != 0 {
		t.Fatalf("second run exit code = %d, output:\n%s", code, buf2.String())
	}

	after, err := os.ReadFile(markerPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Errorf("second run changed the marker file:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

func TestScan_ScaffoldsRealmClaudeMD(t *testing.T) {
	root := t.TempDir()
	opts := Options{Clock: func() time.Time { return time.Date(2026, 6, 6, 12, 0, 0, 0, time.UTC) }}

	if _, err := Scan(root, opts); err != nil {
		t.Fatalf("Scan: %v", err)
	}

	path := filepath.Join(root, "wiki", "CLAUDE.md")
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected Scan to scaffold %s: %v", path, err)
	}
	if string(got) != "@index.md\n" {
		t.Errorf("content = %q, want %q", got, "@index.md\n")
	}
}

func TestScan_PreservesExistingRealmClaudeMD(t *testing.T) {
	root := t.TempDir()
	wikiDir := filepath.Join(root, "wiki")
	opts := Options{Clock: func() time.Time { return time.Date(2026, 6, 6, 12, 0, 0, 0, time.UTC) }}

	// First Scan creates wiki/index.md + wiki/CLAUDE.md.
	if _, err := Scan(root, opts); err != nil {
		t.Fatalf("first Scan: %v", err)
	}

	// User edits the steering file after the first scan.
	claudeMDPath := filepath.Join(wikiDir, "CLAUDE.md")
	existing := "@index.md\n\nuser steering notes\n"
	if err := os.WriteFile(claudeMDPath, []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}

	// Re-scanning must not clobber the user's edit.
	if _, err := Scan(root, opts); err != nil {
		t.Fatalf("second Scan: %v", err)
	}

	got, err := os.ReadFile(claudeMDPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != existing {
		t.Errorf("Scan clobbered existing wiki/CLAUDE.md:\ngot:\n%s\nwant:\n%s", got, existing)
	}
}
