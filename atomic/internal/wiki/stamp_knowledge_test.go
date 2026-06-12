package wiki_test

// stamp_knowledge_test.go — CP4: tests for knowledge page stamping and
// knowledge-citation fingerprint resolution.
//
// Success criteria (spec checkpoint 4):
//   - StampKnowledge writes sources: YAML list with correct entries.
//   - Re-stamp is idempotent (second call overwrites, no duplication).
//   - Non-conforming topic name (not kebab-case [a-z0-9-]+.md) → notice printed
//     to stderr, file not written; exit 0 (non-fatal).
//   - Knowledge-citation in concern's reflects: list resolved as SHA-256 of
//     wiki/knowledge/<topic>.md content (not git HEAD).
//   - atomic wiki stale reports STALE concern <path> (knowledge/<topic>.md)
//     when stored hash diverges from current file hash.
//   - atomic validate artifacts passes (no stamp cliusage entry required).

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/frontmatter"
	"github.com/damusix/atomic-claude/atomic/internal/wiki"
)

// --- helpers ------------------------------------------------------------------

// makeKnowledgePage writes a wiki/knowledge/<topic>.md file with the given
// content (no frontmatter). Returns the file path and its SHA-256 hex.
func makeKnowledgePage(t *testing.T, wikiDir, topic, content string) (path, sha256hex string) {
	t.Helper()
	knowledgeDir := filepath.Join(wikiDir, "knowledge")
	if err := os.MkdirAll(knowledgeDir, 0o755); err != nil {
		t.Fatalf("mkdir knowledge: %v", err)
	}
	path = filepath.Join(knowledgeDir, topic)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write knowledge page: %v", err)
	}
	h := sha256.Sum256([]byte(content))
	sha256hex = fmt.Sprintf("%x", h)
	return path, sha256hex
}

// makeKnowledgePageWithFrontmatter writes a knowledge page at
// wiki/knowledge/<topic> with YAML frontmatter.
func makeKnowledgePageWithFrontmatter(t *testing.T, wikiDir, topic string, meta map[string]any, body string) string {
	t.Helper()
	knowledgeDir := filepath.Join(wikiDir, "knowledge")
	if err := os.MkdirAll(knowledgeDir, 0o755); err != nil {
		t.Fatalf("mkdir knowledge: %v", err)
	}
	path := filepath.Join(knowledgeDir, topic)
	doc, err := frontmatter.Emit(meta, body)
	if err != nil {
		t.Fatalf("emit frontmatter: %v", err)
	}
	if err := os.WriteFile(path, []byte(doc), 0o644); err != nil {
		t.Fatalf("write knowledge page: %v", err)
	}
	return path
}

// makeRealmWithWiki sets up a minimal realm: one committed git repo + Scan to
// produce wiki/index.md with a <wiki-scan> block. Returns (root, wikiDir).
func makeRealmWithWiki(t *testing.T) (root, wikiDir string) {
	t.Helper()
	root = t.TempDir()
	makeCommittedRepo(t, root, "repoA")
	_, err := wiki.Scan(root, wiki.Options{Clock: func() time.Time {
		return time.Date(2026, 6, 12, 0, 0, 0, 0, time.UTC)
	}})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	wikiDir = filepath.Join(root, "wiki")
	return root, wikiDir
}

// writeConcernWithReflects writes a concern file at wiki/concerns/<name>.md
// with a reflects: YAML list containing the given entries.
func writeConcernWithReflects(t *testing.T, wikiDir, name string, reflectsEntries []string) string {
	t.Helper()
	concernsDir := filepath.Join(wikiDir, "concerns")
	if err := os.MkdirAll(concernsDir, 0o755); err != nil {
		t.Fatalf("mkdir concerns: %v", err)
	}
	path := filepath.Join(concernsDir, name)
	entries := make([]any, len(reflectsEntries))
	for i, e := range reflectsEntries {
		entries[i] = e
	}
	meta := map[string]any{
		"title":    "test concern",
		"reflects": entries,
	}
	doc, err := frontmatter.Emit(meta, "## Concern body\n")
	if err != nil {
		t.Fatalf("emit concern frontmatter: %v", err)
	}
	if err := os.WriteFile(path, []byte(doc), 0o644); err != nil {
		t.Fatalf("write concern: %v", err)
	}
	return path
}

// --- StampKnowledge tests -----------------------------------------------------

// TestStampKnowledge_WritesSourcesList verifies that StampKnowledge writes
// the sources: YAML list with correctly-formatted <bucket>/<relpath>@<sha256>
// entries and preserves other frontmatter + body.
func TestStampKnowledge_WritesSourcesList(t *testing.T) {
	wikiDir := t.TempDir()
	knowledgePage := makeKnowledgePageWithFrontmatter(t, wikiDir, "vendor-x.md",
		map[string]any{"title": "Vendor X"}, "## Overview\n\nContent here.\n")

	sources := []string{
		"research/vendor-x-notes.md@abc123",
		"raw/vendor-x-spec.pdf@def456",
	}

	if err := wiki.StampKnowledge(knowledgePage, sources); err != nil {
		t.Fatalf("StampKnowledge: %v", err)
	}

	data, err := os.ReadFile(knowledgePage)
	if err != nil {
		t.Fatalf("read knowledge page: %v", err)
	}
	meta, body, err := frontmatter.Parse(string(data))
	if err != nil {
		t.Fatalf("parse stamped page: %v", err)
	}

	// sources: must be present.
	rawSources, ok := meta["sources"]
	if !ok {
		t.Fatalf("sources key not written; meta: %v", meta)
	}
	sourcesSlice, ok := rawSources.([]any)
	if !ok {
		t.Fatalf("sources is not a list: %T %v", rawSources, rawSources)
	}
	if len(sourcesSlice) != 2 {
		t.Fatalf("sources has %d entries, want 2: %v", len(sourcesSlice), sourcesSlice)
	}

	got := map[string]bool{}
	for _, item := range sourcesSlice {
		s, ok := item.(string)
		if !ok {
			t.Fatalf("sources item is not string: %T", item)
		}
		got[s] = true
	}
	for _, want := range sources {
		if !got[want] {
			t.Errorf("sources missing %q; got: %v", want, sourcesSlice)
		}
	}

	// Other frontmatter preserved.
	if meta["title"] != "Vendor X" {
		t.Errorf("title lost; meta: %v", meta)
	}

	// Body preserved.
	if body != "## Overview\n\nContent here.\n" {
		t.Errorf("body changed: %q", body)
	}
}

// TestStampKnowledge_Idempotent verifies that stamping twice produces the same
// sources: list (no duplication, no data loss).
func TestStampKnowledge_Idempotent(t *testing.T) {
	wikiDir := t.TempDir()
	knowledgePage := makeKnowledgePageWithFrontmatter(t, wikiDir, "auth-patterns.md",
		map[string]any{"title": "Auth Patterns"}, "body\n")

	sources := []string{"research/auth-notes.md@aabbcc"}

	// Stamp twice.
	for i := 0; i < 2; i++ {
		if err := wiki.StampKnowledge(knowledgePage, sources); err != nil {
			t.Fatalf("StampKnowledge pass %d: %v", i, err)
		}
	}

	data, err := os.ReadFile(knowledgePage)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	meta, _, err := frontmatter.Parse(string(data))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	rawSources, ok := meta["sources"]
	if !ok {
		t.Fatalf("sources not written; meta: %v", meta)
	}
	sourcesSlice, ok := rawSources.([]any)
	if !ok {
		t.Fatalf("sources not a list: %T", rawSources)
	}
	// Must have exactly 1 entry (not 2 from 2 calls).
	if len(sourcesSlice) != 1 {
		t.Errorf("sources has %d entries after 2 stamps, want 1 (idempotent): %v", len(sourcesSlice), sourcesSlice)
	}
}

// TestStampKnowledge_EmptySources verifies that stamping with zero sources
// writes an empty sources: list (not absent).
func TestStampKnowledge_EmptySources(t *testing.T) {
	wikiDir := t.TempDir()
	knowledgePage := makeKnowledgePageWithFrontmatter(t, wikiDir, "topic.md",
		map[string]any{}, "body\n")

	if err := wiki.StampKnowledge(knowledgePage, []string{}); err != nil {
		t.Fatalf("StampKnowledge: %v", err)
	}

	data, err := os.ReadFile(knowledgePage)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	meta, _, err := frontmatter.Parse(string(data))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	rawSources, ok := meta["sources"]
	if !ok {
		t.Fatalf("sources key not written; meta: %v", meta)
	}
	sourcesSlice, ok := rawSources.([]any)
	if !ok {
		t.Fatalf("sources not a list: %T", rawSources)
	}
	if len(sourcesSlice) != 0 {
		t.Errorf("sources has %d entries, want 0: %v", len(sourcesSlice), sourcesSlice)
	}
}

// TestStampKnowledge_ErrorWhenAbsent verifies that StampKnowledge returns a
// non-nil error when the file does not exist, and does not create the file.
// The inferrer authors knowledge pages; stamp only updates existing ones.
func TestStampKnowledge_ErrorWhenAbsent(t *testing.T) {
	wikiDir := t.TempDir()
	knowledgeDir := filepath.Join(wikiDir, "knowledge")
	if err := os.MkdirAll(knowledgeDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	path := filepath.Join(knowledgeDir, "new-topic.md")
	// File does not exist.

	sources := []string{"research/notes.md@deadbeef"}
	err := wiki.StampKnowledge(path, sources)
	if err == nil {
		t.Fatalf("StampKnowledge on absent file: expected non-nil error, got nil")
	}

	// Error must name the path.
	if !strings.Contains(err.Error(), "new-topic.md") {
		t.Errorf("error should name the missing path; got: %v", err)
	}

	// File must NOT be created.
	if _, statErr := os.Stat(path); statErr == nil {
		t.Errorf("StampKnowledge must not create a missing file")
	}
}

// --- CLI dispatch tests (wikiStampAction via WikiAction) ----------------------

// TestStampAction_KnowledgeModeWritesSources verifies the CLI
// `atomic wiki stamp --knowledge --sources <entries> <path>` dispatch writes
// the sources: list.
func TestStampAction_KnowledgeModeWritesSources(t *testing.T) {
	wikiDir := t.TempDir()
	knowledgePage := makeKnowledgePageWithFrontmatter(t, wikiDir, "vendor-x.md",
		map[string]any{"title": "Vendor X"}, "body\n")

	// Flags before positional: --knowledge --sources <entries> <path>
	args := []string{
		"stamp",
		"--knowledge",
		"--sources", "research/notes.md@abc123,raw/dump.txt@def456",
		knowledgePage,
	}
	var out bytes.Buffer
	code := wiki.WikiAction(args, t.TempDir(), t.TempDir(), &out)
	if code != 0 {
		t.Fatalf("WikiAction stamp --knowledge: exit %d; output: %q", code, out.String())
	}

	data, err := os.ReadFile(knowledgePage)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	meta, _, err := frontmatter.Parse(string(data))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, ok := meta["sources"]; !ok {
		t.Errorf("sources not written; meta: %v", meta)
	}
}

// TestStampAction_KnowledgeModeNonConformingTopicName verifies that a knowledge
// path with a non-conforming filename (not kebab-case [a-z0-9-]+.md) is skipped
// with a notice printed to stderr, file NOT written, and exit 0 (non-fatal).
func TestStampAction_KnowledgeModeNonConformingTopicName(t *testing.T) {
	wikiDir := t.TempDir()
	knowledgeDir := filepath.Join(wikiDir, "knowledge")
	if err := os.MkdirAll(knowledgeDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Non-conforming filenames: uppercase, underscores, spaces.
	badNames := []string{
		"Vendor_X.md",
		"Auth Patterns.md",
		"topic_.md",
		"-topic.md",
		"topic.txt",
	}

	for _, name := range badNames {
		path := filepath.Join(knowledgeDir, name)

		// Flags before positional to avoid flag.FlagSet stopping at positional.
		args := []string{
			"stamp",
			"--knowledge",
			"--sources", "research/notes.md@abc123",
			path,
		}
		var stderrBuf bytes.Buffer
		// We capture stderr indirectly: WikiAction writes notices to os.Stderr,
		// so we use the exit code as the primary signal.
		var out bytes.Buffer
		code := wiki.WikiAction(args, t.TempDir(), t.TempDir(), &out)

		// Non-fatal: must exit 0.
		if code != 0 {
			t.Errorf("name=%q: expected exit 0 (non-fatal skip), got %d; stderr: %q", name, code, stderrBuf.String())
		}

		// File must NOT be created.
		if _, err := os.Stat(path); err == nil {
			t.Errorf("name=%q: file should not be created for non-conforming name", name)
		}
	}
}

// TestStampAction_KnowledgeModeConformingNames verifies that conforming topic
// names (kebab-case [a-z0-9-]+.md) are accepted and written.
func TestStampAction_KnowledgeModeConformingNames(t *testing.T) {
	goodNames := []string{
		"vendor-x.md",
		"auth-patterns.md",
		"topic.md",
		"a1b2c3.md",
		"my-long-topic-name.md",
	}

	for _, name := range goodNames {
		wikiDir := t.TempDir()
		knowledgeDir := filepath.Join(wikiDir, "knowledge")
		if err := os.MkdirAll(knowledgeDir, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		path := filepath.Join(knowledgeDir, name)
		// Create file so stamp has something to read.
		if err := os.WriteFile(path, []byte(""), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}

		args := []string{
			"stamp",
			"--knowledge",
			"--sources", "research/f.md@abc123",
			path,
		}
		var out bytes.Buffer
		code := wiki.WikiAction(args, t.TempDir(), t.TempDir(), &out)
		if code != 0 {
			t.Errorf("name=%q: expected exit 0, got %d", name, code)
		}

		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("name=%q: read: %v", name, err)
		}
		meta, _, err := frontmatter.Parse(string(data))
		if err != nil {
			t.Fatalf("name=%q: parse: %v", name, err)
		}
		if _, ok := meta["sources"]; !ok {
			t.Errorf("name=%q: sources not written; meta: %v", name, meta)
		}
	}
}

// TestStampAction_KnowledgeMissingSourcesFlag verifies that --knowledge without
// --sources returns exit 1 (usage error).
func TestStampAction_KnowledgeMissingSourcesFlag(t *testing.T) {
	wikiDir := t.TempDir()
	path := filepath.Join(wikiDir, "knowledge", "topic.md")

	args := []string{"stamp", "--knowledge", path}
	var out bytes.Buffer
	code := wiki.WikiAction(args, t.TempDir(), t.TempDir(), &out)
	if code == 0 {
		t.Errorf("expected non-zero exit when --sources missing with --knowledge, got 0")
	}
}

// --- resolveFingerprint knowledge branch tests --------------------------------

// TestStampConcern_KnowledgeCitation verifies that a concern citing
// "knowledge/<topic>.md" in its reflects: list is fingerprinted using the
// SHA-256 of the knowledge page file content (not a git HEAD).
func TestStampConcern_KnowledgeCitation(t *testing.T) {
	wikiDir := t.TempDir()

	// Write a knowledge page and compute its hash.
	content := "## Knowledge Page\n\nSome synthesized knowledge.\n"
	knowledgePage, wantHash := makeKnowledgePage(t, wikiDir, "vendor-x.md", content)
	_ = knowledgePage

	// Concern file to stamp.
	concernFile := filepath.Join(wikiDir, "concerns", "shared.md")
	writeFrontmatterFile(t, concernFile, map[string]any{"title": "shared"}, "body\n")

	// Cite the knowledge page by its wiki-root-relative id.
	if err := wiki.StampConcern(concernFile, wikiDir, []string{"knowledge/vendor-x.md"}); err != nil {
		t.Fatalf("StampConcern: %v", err)
	}

	data, _ := os.ReadFile(concernFile)
	meta, _, err := frontmatter.Parse(string(data))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	rawReflects, ok := meta["reflects"]
	if !ok {
		t.Fatalf("reflects not written; meta: %v", meta)
	}
	reflects, ok := rawReflects.([]any)
	if !ok {
		t.Fatalf("reflects not a list: %T", rawReflects)
	}
	if len(reflects) != 1 {
		t.Fatalf("reflects has %d entries, want 1: %v", len(reflects), reflects)
	}

	entry, ok := reflects[0].(string)
	if !ok {
		t.Fatalf("reflects[0] not string: %T", reflects[0])
	}

	wantEntry := fmt.Sprintf("knowledge/vendor-x.md@%s", wantHash)
	if entry != wantEntry {
		t.Errorf("reflects[0] = %q, want %q", entry, wantEntry)
	}
}

// TestStampConcern_KnowledgeCitation_MixedWithRepo verifies that a concern
// citing both a repo id and a knowledge page id gets both fingerprinted correctly.
func TestStampConcern_KnowledgeCitation_MixedWithRepo(t *testing.T) {
	wikiDir := t.TempDir()

	// Summarized repo under wikiDir (no signals.md, uses git HEAD).
	repoDir := filepath.Join(wikiDir, "repoA")
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatalf("mkdir repoA: %v", err)
	}
	for _, c := range [][]string{
		{"git", "-C", repoDir, "init"},
		{"git", "-C", repoDir, "config", "user.email", "t@t.com"},
		{"git", "-C", repoDir, "config", "user.name", "T"},
	} {
		if out, err := exec.Command(c[0], c[1:]...).CombinedOutput(); err != nil {
			t.Fatalf("%v: %v\n%s", c, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(repoDir, "f.go"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	for _, c := range [][]string{
		{"git", "-C", repoDir, "add", "."},
		{"git", "-C", repoDir, "commit", "-m", "init"},
	} {
		if out, err := exec.Command(c[0], c[1:]...).CombinedOutput(); err != nil {
			t.Fatalf("%v: %v\n%s", c, err, out)
		}
	}

	// Knowledge page.
	content := "## synthesized\n"
	_, knowledgeHash := makeKnowledgePage(t, wikiDir, "vendor-x.md", content)

	concernFile := filepath.Join(wikiDir, "concerns", "c.md")
	writeFrontmatterFile(t, concernFile, nil, "body\n")

	if err := wiki.StampConcern(concernFile, wikiDir, []string{"repoA", "knowledge/vendor-x.md"}); err != nil {
		t.Fatalf("StampConcern: %v", err)
	}

	data, _ := os.ReadFile(concernFile)
	meta, _, err := frontmatter.Parse(string(data))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	reflects, ok := meta["reflects"].([]any)
	if !ok {
		t.Fatalf("reflects not a list: %T", meta["reflects"])
	}
	if len(reflects) != 2 {
		t.Fatalf("reflects has %d entries, want 2: %v", len(reflects), reflects)
	}

	byID := map[string]string{}
	for _, item := range reflects {
		s := item.(string)
		at := strings.LastIndex(s, "@")
		byID[s[:at]] = s[at+1:]
	}

	if fp, ok := byID["knowledge/vendor-x.md"]; !ok {
		t.Errorf("knowledge/vendor-x.md missing from reflects: %v", byID)
	} else if fp != knowledgeHash {
		t.Errorf("knowledge fp = %q, want %q", fp, knowledgeHash)
	}

	if _, ok := byID["repoA"]; !ok {
		t.Errorf("repoA missing from reflects: %v", byID)
	}
}

// --- atomic wiki stale — knowledge-citation staleness tests -------------------

// TestStale_KnowledgeCitationFresh verifies that a concern citing a knowledge
// page is reported as fresh when the stored hash matches the current file.
func TestStale_KnowledgeCitationFresh(t *testing.T) {
	root, wikiDir := makeRealmWithWiki(t)

	// Write a knowledge page.
	content := "## Knowledge\n"
	knowledgePage, hash := makeKnowledgePage(t, wikiDir, "vendor-x.md", content)
	_ = knowledgePage

	// Write a concern citing the knowledge page with the correct hash.
	writeConcernWithReflects(t, wikiDir, "c.md",
		[]string{fmt.Sprintf("knowledge/vendor-x.md@%s", hash)})

	code, out := runStale(t, root)

	if code != 0 {
		t.Errorf("expected exit 0 (fresh), got %d; stdout: %q", code, out)
	}
	if strings.Contains(out, "STALE concern") {
		t.Errorf("fresh knowledge citation should not emit STALE concern; got: %q", out)
	}
}

// TestStale_KnowledgeCitationStale verifies that a concern citing a knowledge
// page emits STALE concern <path> (knowledge/<topic>.md) when the stored hash
// does not match the current file hash.
func TestStale_KnowledgeCitationStale(t *testing.T) {
	root, wikiDir := makeRealmWithWiki(t)

	// Write a knowledge page.
	content := "## Knowledge v1\n"
	_, oldHash := makeKnowledgePage(t, wikiDir, "vendor-x.md", content)

	// Write a concern with the old hash.
	writeConcernWithReflects(t, wikiDir, "c.md",
		[]string{fmt.Sprintf("knowledge/vendor-x.md@%s", oldHash)})

	// Now update the knowledge page — hash changes.
	knowledgePath := filepath.Join(wikiDir, "knowledge", "vendor-x.md")
	if err := os.WriteFile(knowledgePath, []byte("## Knowledge v2\n"), 0o644); err != nil {
		t.Fatalf("update knowledge: %v", err)
	}

	code, out := runStale(t, root)

	if code != 1 {
		t.Errorf("expected exit 1 (stale), got %d; stdout: %q", code, out)
	}
	wantSubstr := "STALE concern"
	if !strings.Contains(out, wantSubstr) {
		t.Errorf("expected %q in output; got: %q", wantSubstr, out)
	}
	// Must name the knowledge file.
	if !strings.Contains(out, "knowledge/vendor-x.md") {
		t.Errorf("expected knowledge/vendor-x.md in output; got: %q", out)
	}
}

// TestStale_KnowledgeCitationMissingFile verifies that a concern citing a
// knowledge page that no longer exists is reported as stale (fail-safe).
func TestStale_KnowledgeCitationMissingFile(t *testing.T) {
	root, wikiDir := makeRealmWithWiki(t)

	// Write concern with a knowledge citation — but do NOT create the knowledge
	// page file. Missing file → unresolvable → stale (fail-safe).
	writeConcernWithReflects(t, wikiDir, "c.md",
		[]string{"knowledge/missing-topic.md@aaaa1111"})

	code, out := runStale(t, root)

	if code != 1 {
		t.Errorf("expected exit 1 (stale), got %d; stdout: %q", code, out)
	}
	if !strings.Contains(out, "STALE concern") {
		t.Errorf("missing knowledge page should emit STALE concern; got: %q", out)
	}
}

// TestStale_KnowledgeCitationHashMismatchLiteral verifies the exact output
// format: "STALE concern <wikirel-path> (knowledge/<topic>.md)".
func TestStale_KnowledgeCitationHashMismatchLiteral(t *testing.T) {
	root, wikiDir := makeRealmWithWiki(t)

	content := "## v1\n"
	_, oldHash := makeKnowledgePage(t, wikiDir, "auth-patterns.md", content)

	concernsDir := filepath.Join(wikiDir, "concerns")
	if err := os.MkdirAll(concernsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	concernName := "auth.md"
	writeConcernWithReflects(t, wikiDir, concernName,
		[]string{fmt.Sprintf("knowledge/auth-patterns.md@%s", oldHash)})

	// Update knowledge page to change hash.
	if err := os.WriteFile(filepath.Join(wikiDir, "knowledge", "auth-patterns.md"), []byte("## v2\n"), 0o644); err != nil {
		t.Fatalf("update knowledge: %v", err)
	}

	_, out := runStale(t, root)

	// Verify the exact format: "STALE concern wiki/concerns/auth.md (knowledge/auth-patterns.md)"
	expectedLine := fmt.Sprintf("STALE concern wiki/concerns/%s (knowledge/auth-patterns.md)", concernName)
	if !strings.Contains(out, expectedLine) {
		t.Errorf("expected line %q in output; got:\n%s", expectedLine, out)
	}
}
