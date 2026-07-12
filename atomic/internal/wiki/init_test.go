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
// with it. Invariant the fixture encodes: every illustrative example lives
// inside an HTML comment, so an uncustomized scaffold contains zero live
// steering facts.
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

<!-- example: NestJS monorepo (not plain Express) -->

## Domains

<!-- example:
- src/billing/ and src/payments/ are one domain ("payments")
- src/internal-tools/ is scratch code — not a real domain
-->

## Build

<!-- example:
- Build: pnpm turbo build
- Test: pnpm test:ci (not pnpm test — that runs watch mode)
-->

## Ignore for domains

<!-- example:
- vendor/
- generated/
-->
`

// --- InitRepoScope ---

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

// --- InitRealmScope ---

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

// --- wiki init CLI dispatch ---

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
}

// --- Scan() realm scaffold hook ---

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
