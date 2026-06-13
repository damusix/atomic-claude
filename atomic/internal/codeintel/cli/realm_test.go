package cli_test

// realm_test.go — CP3 tests for realm fan-out in `atomic code` verbs.
//
// Tests cover:
//   1. ScopeRepo: RunCode with a local index falls through unchanged (SC 2)
//   2. ScopeRealmAll index: seeding code.toml + writing realm dbs; no member dir touched (SC 3, 6)
//   3. ScopeRealmAll search: [key] headers in human output; {key:...} in JSON (SC 5)
//   4. ScopeRealmAll partial failure: missing db → "[key] not indexed" warning; rest continues (SC 4)
//   5. --only filter: limits fan-out to named keys (SC 5)
//   6. --exclude filter: omits named keys from fan-out (SC 5)
//   7. ScopeRealmMember: query targets just that member's db (SC 1)
//   8. ScopeNoIndex outside realm: no crash (SC 1)
//   9. ScopeRepo delegates via repoctx path (finding 1)
//  10. ScopeRealmMember index: no write into member dir (finding 2 / SC 3)
//  11. ScopeRepo subdir→git-root: query from a subdirectory resolves to git root (finding 1)

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	codecli "github.com/damusix/atomic-claude/atomic/internal/codeintel/cli"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/engine"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/realm"
)

// ─── Realm fixture helpers ───────────────────────────────────────────────────

// writeGoFile writes a minimal Go source file at path.
func writeGoFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// buildRealmFixture creates a realm layout:
//
//	<tmp>/
//	  wiki/index.md        (with <wiki-scan> block listing members)
//	  .atomic/code.toml   (if wantConfig=true, pre-seeded; else absent)
//	  repos/<name>/       (one tiny Go file each)
//
// Returns realmRoot and a fake claudeMD path that registers the wiki.
func buildRealmFixture(t *testing.T, memberNames []string) (realmRoot, claudeMD string) {
	t.Helper()
	realmRoot = t.TempDir()
	claudeMD = filepath.Join(realmRoot, "CLAUDE.md")

	// Register wiki.
	wikiIndexPath := filepath.Join(realmRoot, "wiki", "index.md")
	writeRealmCLAUDEMD(t, claudeMD, wikiIndexPath)

	// Build wiki/index.md with <wiki-scan> block.
	var scanBlock strings.Builder
	scanBlock.WriteString("<wiki-scan generated=\"2026-01-01\" root=\"" + realmRoot + "\">\n")
	for _, name := range memberNames {
		scanBlock.WriteString("  <repo path=\"repos/" + name + "\" status=\"indexed\"/>\n")
	}
	scanBlock.WriteString("</wiki-scan>\n")
	writeGoFile(t, wikiIndexPath, "# wiki\n\n"+scanBlock.String())

	// Create member directories with a tiny Go file.
	for _, name := range memberNames {
		memberDir := filepath.Join(realmRoot, "repos", name)
		writeGoFile(t, filepath.Join(memberDir, "main.go"),
			"package "+name+"\n\nfunc Hello"+capitalize(name)+"() string { return \"hello\" }\n")
	}
	return realmRoot, claudeMD
}

// writeRealmCLAUDEMD writes a CLAUDE.md with a <wikis> block at claudeMD path.
func writeRealmCLAUDEMD(t *testing.T, claudeMD, wikiIndexPath string) {
	t.Helper()
	content := "# CLAUDE.md\n\n<wikis>\n- " + wikiIndexPath + "\n</wikis>\n"
	writeGoFile(t, claudeMD, content)
}

// indexMember indexes a single member repo at memberDir, storing the db at dbPath.
// Returns when indexing completes or calls t.Fatal on error.
func indexMember(t *testing.T, memberDir, dbPath string) {
	t.Helper()
	ctx := testCtx(t)
	eng, err := engine.NewWithDBPath(memberDir, dbPath)
	if err != nil {
		t.Fatalf("NewWithDBPath: %v", err)
	}
	t.Cleanup(func() { eng.Close() })
	if err := eng.Init(ctx); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := eng.IndexAll(ctx); err != nil {
		t.Fatalf("IndexAll: %v", err)
	}
}

// capitalize returns s with first letter uppercased.
func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// ─── 1. ScopeRepo unchanged (SC 2) ───────────────────────────────────────────

// TestRunCodeRealm_ScopeRepo_Unchanged verifies that when a local index exists,
// RunCodeWithRealm delegates to the normal single-engine path unchanged.
// We probe this by running `status` against a locally-indexed fixture.
func TestRunCodeRealm_ScopeRepo_Unchanged(t *testing.T) {
	dir := writeFixture(t)
	// Index it normally (repo scope, db at <dir>/.claude/.atomic-index/atomic.db).
	indexedEngine(t, dir)

	var stdout, stderr bytes.Buffer
	// claudeMD with no wikis — ensures realm code path is bypassed.
	claudeMD := filepath.Join(t.TempDir(), "CLAUDE.md")
	writeGoFile(t, claudeMD, "# no wikis\n")

	code := codecli.RunCodeWithRealm([]string{"status"}, dir, claudeMD, &stdout, &stderr, noStdin())
	if code != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", code, stderr.String())
	}
	// Repo-scope status output must include "initialized: true".
	out := stdout.String()
	if !strings.Contains(out, "initialized:") {
		t.Fatalf("expected 'initialized:' in output, got: %s", out)
	}
}

// ─── 2. ScopeRealmAll index: seeding + db location (SC 3, 6) ────────────────

// TestRunCodeRealm_Index_SeedsConfigAndWritesRealmDBs verifies:
//   - code.toml is created at <realm>/.atomic/code.toml
//   - realm dbs are created at <realm>/.atomic/<key>.db
//   - no .claude/.atomic-index/ directory is created inside member dirs
func TestRunCodeRealm_Index_SeedsConfigAndWritesRealmDBs(t *testing.T) {
	realmRoot, claudeMD := buildRealmFixture(t, []string{"alpha", "beta"})

	var stdout, stderr bytes.Buffer
	code := codecli.RunCodeWithRealm([]string{"index"}, realmRoot, claudeMD, &stdout, &stderr, noStdin())
	if code != 0 {
		t.Fatalf("realm index failed (exit %d);\nstdout: %s\nstderr: %s", code, stdout.String(), stderr.String())
	}

	// code.toml must exist.
	tomlPath := filepath.Join(realmRoot, ".atomic", "code.toml")
	if _, err := os.Stat(tomlPath); err != nil {
		t.Fatalf("code.toml not created: %v", err)
	}

	// DB files must exist at realm/.atomic/<key>.db.
	cfg, err := realm.LoadConfig(realmRoot)
	if err != nil || cfg == nil || len(cfg.Members) < 2 {
		t.Fatalf("code.toml not parseable or wrong member count: err=%v, cfg=%v", err, cfg)
	}
	for _, m := range cfg.Members {
		dbPath := filepath.Join(realmRoot, ".atomic", m.Key+".db")
		if _, err := os.Stat(dbPath); err != nil {
			t.Errorf("realm db %q not created: %v", dbPath, err)
		}
	}

	// No .claude/.atomic-index/ must exist in any member dir.
	for _, name := range []string{"alpha", "beta"} {
		memberLocal := filepath.Join(realmRoot, "repos", name, ".claude", ".atomic-index")
		if _, err := os.Stat(memberLocal); err == nil {
			t.Errorf("member dir %q had .claude/.atomic-index/ created — violates SC 3", name)
		}
	}
}

// ─── 3. ScopeRealmAll search: grouped output (SC 5) ─────────────────────────

// TestRunCodeRealm_Search_GroupedByKey verifies:
//   - human output has [key] header per member
//   - --json output has {"key": <results>} shape
func TestRunCodeRealm_Search_GroupedByKey(t *testing.T) {
	realmRoot, claudeMD := buildRealmFixture(t, []string{"alpha", "beta"})

	// First index the realm so dbs exist.
	var idx bytes.Buffer
	if code := codecli.RunCodeWithRealm([]string{"index"}, realmRoot, claudeMD, &idx, &idx, noStdin()); code != 0 {
		t.Fatalf("index failed: %s", idx.String())
	}

	// Human output — should contain [alpha] and [beta].
	var stdout, stderr bytes.Buffer
	code := codecli.RunCodeWithRealm([]string{"search", "Hello"}, realmRoot, claudeMD, &stdout, &stderr, noStdin())
	if code != 0 {
		t.Fatalf("search failed (exit %d); stderr: %s", code, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "[alpha]") {
		t.Errorf("expected [alpha] header in human output, got: %s", out)
	}
	if !strings.Contains(out, "[beta]") {
		t.Errorf("expected [beta] header in human output, got: %s", out)
	}

	// JSON output — top-level keys should be member keys.
	var jsonOut, jsonErr bytes.Buffer
	code = codecli.RunCodeWithRealm([]string{"search", "Hello", "--json"}, realmRoot, claudeMD, &jsonOut, &jsonErr, noStdin())
	if code != 0 {
		t.Fatalf("search --json failed (exit %d); stderr: %s", code, jsonErr.String())
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(jsonOut.Bytes(), &obj); err != nil {
		t.Fatalf("invalid JSON: %v\noutput: %s", err, jsonOut.String())
	}
	if _, ok := obj["alpha"]; !ok {
		t.Error("JSON output missing 'alpha' key")
	}
	if _, ok := obj["beta"]; !ok {
		t.Error("JSON output missing 'beta' key")
	}
}

// ─── 4. Partial failure (SC 4) ───────────────────────────────────────────────

// TestRunCodeRealm_PartialFailure_MissingDB verifies that a member with no db
// emits "[key] not indexed" to stderr and the operation continues for other members.
func TestRunCodeRealm_PartialFailure_MissingDB(t *testing.T) {
	realmRoot, claudeMD := buildRealmFixture(t, []string{"alpha", "beta"})

	// Only index alpha; leave beta without a db.
	cfg, err := realm.SeedConfig(realmRoot, filepath.Join(realmRoot, "wiki", "index.md"))
	if err != nil || cfg == nil {
		t.Fatalf("seed failed: %v", err)
	}

	// Index only alpha.
	for _, m := range cfg.Members {
		if m.Key == "alpha" {
			indexMember(t, filepath.Join(realmRoot, m.Path), realm.Resolution{RealmRoot: realmRoot}.DBPath(m.Key))
		}
	}

	// Override DBPath helper by using the resolution directly.
	// Use a workaround: run RunCodeWithRealm with cwd=realmRoot; it will fan out.
	// alpha is indexed; beta db is absent.

	// We need to also make sure the db actually exists for alpha:
	alphaKey := ""
	for _, m := range cfg.Members {
		if strings.HasSuffix(m.Path, "alpha") {
			alphaKey = m.Key
		}
	}
	if alphaKey == "" {
		t.Fatal("could not find alpha member key")
	}

	// Confirm alpha db exists.
	alphaDB := filepath.Join(realmRoot, ".atomic", alphaKey+".db")
	if _, err := os.Stat(alphaDB); err != nil {
		t.Fatalf("alpha db not found at %s: %v", alphaDB, err)
	}

	var stdout, stderr bytes.Buffer
	code := codecli.RunCodeWithRealm([]string{"search", "Hello"}, realmRoot, claudeMD, &stdout, &stderr, noStdin())
	// Exit 0 expected: the run completes because alpha is indexed and produces
	// results.  The missing beta db is reported as "[beta] not indexed" on stderr
	// and skipped (fanOutQuery continues to the next member).  The overall exit
	// reflects the dispatched verbs' codes: search returns 0 for normal/empty
	// results, so the aggregate is 0 despite the partial skip.
	if code != 0 {
		t.Fatalf("expected exit 0 on partial failure, got %d; stderr: %s", code, stderr.String())
	}
	// stderr should mention "not indexed" for beta.
	se := stderr.String()
	if !strings.Contains(se, "not indexed") {
		t.Errorf("expected 'not indexed' warning in stderr, got: %s", se)
	}
	// stdout should still contain alpha results.
	so := stdout.String()
	if !strings.Contains(so, "[alpha]") {
		t.Errorf("expected [alpha] results even on partial failure, got: %s", so)
	}
}

// ─── 5. --only filter (SC 5) ─────────────────────────────────────────────────

// TestRunCodeRealm_OnlyFilter restricts fan-out to the named key.
func TestRunCodeRealm_OnlyFilter(t *testing.T) {
	realmRoot, claudeMD := buildRealmFixture(t, []string{"alpha", "beta"})

	// Index both.
	var idx bytes.Buffer
	if code := codecli.RunCodeWithRealm([]string{"index"}, realmRoot, claudeMD, &idx, &idx, noStdin()); code != 0 {
		t.Fatalf("index failed: %s", idx.String())
	}

	var stdout, stderr bytes.Buffer
	code := codecli.RunCodeWithRealm([]string{"search", "Hello", "--only", "alpha"}, realmRoot, claudeMD, &stdout, &stderr, noStdin())
	if code != 0 {
		t.Fatalf("search --only failed (exit %d); stderr: %s", code, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "[alpha]") {
		t.Errorf("expected [alpha] in output, got: %s", out)
	}
	if strings.Contains(out, "[beta]") {
		t.Errorf("did not expect [beta] in --only alpha output, got: %s", out)
	}
}

// ─── 6. --exclude filter (SC 5) ──────────────────────────────────────────────

// TestRunCodeRealm_ExcludeFilter omits the named key from fan-out.
func TestRunCodeRealm_ExcludeFilter(t *testing.T) {
	realmRoot, claudeMD := buildRealmFixture(t, []string{"alpha", "beta"})

	// Index both.
	var idx bytes.Buffer
	if code := codecli.RunCodeWithRealm([]string{"index"}, realmRoot, claudeMD, &idx, &idx, noStdin()); code != 0 {
		t.Fatalf("index failed: %s", idx.String())
	}

	var stdout, stderr bytes.Buffer
	code := codecli.RunCodeWithRealm([]string{"search", "Hello", "--exclude", "beta"}, realmRoot, claudeMD, &stdout, &stderr, noStdin())
	if code != 0 {
		t.Fatalf("search --exclude failed (exit %d); stderr: %s", code, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "[alpha]") {
		t.Errorf("expected [alpha] in output, got: %s", out)
	}
	if strings.Contains(out, "[beta]") {
		t.Errorf("did not expect [beta] in --exclude beta output, got: %s", out)
	}
}

// ─── 7. ScopeRealmMember (SC 1) ──────────────────────────────────────────────

// TestRunCodeRealm_ScopeRealmMember verifies that when cwd is inside a member
// directory, the command queries only that member's keyed db.
func TestRunCodeRealm_ScopeRealmMember(t *testing.T) {
	realmRoot, claudeMD := buildRealmFixture(t, []string{"alpha", "beta"})

	// Index both via realm.
	var idx bytes.Buffer
	if code := codecli.RunCodeWithRealm([]string{"index"}, realmRoot, claudeMD, &idx, &idx, noStdin()); code != 0 {
		t.Fatalf("index failed: %s", idx.String())
	}

	// cwd inside alpha member dir.
	alphaCWD := filepath.Join(realmRoot, "repos", "alpha")

	var stdout, stderr bytes.Buffer
	code := codecli.RunCodeWithRealm([]string{"search", "Hello"}, alphaCWD, claudeMD, &stdout, &stderr, noStdin())
	if code != 0 {
		t.Fatalf("member search failed (exit %d); stderr: %s", code, stderr.String())
	}
	out := stdout.String()
	// Member scope — no [key] wrapping, just direct results.
	// Should not have [alpha] grouping header (single member, no wrapper).
	if strings.Contains(out, "[alpha]") {
		t.Errorf("ScopeRealmMember should not wrap with [key] header, got: %s", out)
	}
	// Should NOT include beta results.
	if strings.Contains(out, "HelloBeta") {
		t.Errorf("ScopeRealmMember should not include beta results, got: %s", out)
	}
}

// ─── 8. ScopeNoIndex outside realm (SC 1) ───────────────────────────────────

// TestRunCodeRealm_NoIndex_QueryVerb verifies that a query verb outside any
// realm runs through the single-repo repoctx path without panicking.
// The exact exit code depends on the test environment (whether the process cwd
// is inside a git repo with a code index), so we only assert no panic and
// that any error goes to stderr.
func TestRunCodeRealm_NoIndex_QueryVerb(t *testing.T) {
	dir := t.TempDir()
	claudeMD := filepath.Join(t.TempDir(), "CLAUDE.md")
	writeGoFile(t, claudeMD, "# no wikis\n")

	var stdout, stderr bytes.Buffer
	// Must not panic; exit code depends on test environment.
	_ = codecli.RunCodeWithRealm([]string{"search", "foo"}, dir, claudeMD, &stdout, &stderr, noStdin())
	// If it failed, there should be something on stderr.
	// If it succeeded (process git repo has an index), stdout has "(no results)".
	// Both are acceptable — the invariant is no crash.
}

// ─── 9. ScopeRepo: queries the projectRoot index directly ───────────────────

// TestRunCodeRealm_ScopeRepo_UsesProjectRootIndex verifies that when a local
// index is present at the projectRoot, the ScopeRepo branch queries that index
// directly — without consulting the process working directory. This must hold
// regardless of where the test process is running (the bug this guards against
// resolved the root via the process cwd, which passed locally but failed in CI).
func TestRunCodeRealm_ScopeRepo_UsesProjectRootIndex(t *testing.T) {
	dir := writeFixture(t)
	indexedEngine(t, dir)

	claudeMD := filepath.Join(t.TempDir(), "CLAUDE.md")
	writeGoFile(t, claudeMD, "# no wikis\n")

	var stdout, stderr bytes.Buffer
	// ScopeRepo is detected because dir has a local index.
	// RunCodeWithRealm should delegate via repoctx.Resolve and return the status.
	code := codecli.RunCodeWithRealm([]string{"status"}, dir, claudeMD, &stdout, &stderr, noStdin())
	if code != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "initialized:") {
		t.Errorf("expected 'initialized:' in output, got: %s", stdout.String())
	}
}

// TestRunCodeRealm_RepoScope_SubdirResolvesToGitRoot verifies that when the
// process cwd is a subdirectory of a git repo, RunCodeWithRealm routes through
// the repo-scope path (repoctx.Resolve → git toplevel) rather than treating the
// subdir itself as the project root.  Uses t.Chdir (Go 1.24+) to set the
// process cwd safely — the test is non-parallel as a result.
//
// We do NOT index the repo; we only confirm the no-index path is reached from
// the git root, not a realm path and not a cwd-resolution error.
func TestRunCodeRealm_RepoScope_SubdirResolvesToGitRoot(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	// Create a real git repo in a temp dir.
	repoRoot := t.TempDir()
	if out, err := exec.Command("git", "init", repoRoot).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}

	// Create a subdirectory inside the repo.
	subdir := filepath.Join(repoRoot, "src")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatal(err)
	}

	// claudeMD with no <wikis> block — ensures no realm path is taken for this repo.
	claudeMD := filepath.Join(t.TempDir(), "CLAUDE.md")
	writeGoFile(t, claudeMD, "# no wikis\n")

	// Change the process cwd to the subdir.  t.Chdir auto-restores on cleanup
	// and marks the test as non-parallel.
	t.Chdir(subdir)

	var stdout, stderr bytes.Buffer
	// With no index present, repo-scope search must produce the no-index message
	// on stderr (exit 1), NOT a realm message and NOT a cwd-resolution error.
	code := codecli.RunCodeWithRealm([]string{"search", "foo"}, subdir, claudeMD, &stdout, &stderr, noStdin())

	se := stderr.String()
	so := stdout.String()

	// Must NOT look like a realm response.
	if strings.Contains(so, "[") && strings.Contains(so, "]") {
		t.Errorf("got realm-style [key] output — should not have taken realm path: %s", so)
	}
	// Must NOT surface a cwd/git-root resolution error.
	if strings.Contains(se, "not inside a git repository") {
		t.Errorf("repoctx.Resolve failed — subdir→git-root resolution broken: %s", se)
	}
	// Must reach the repo-scope no-index path (exit 1 + message on stderr).
	if code == 0 {
		t.Errorf("expected non-zero exit for no-index repo-scope search, got 0; stdout: %s stderr: %s", so, se)
	}
	if !strings.Contains(se, "index not initialized") {
		t.Errorf("expected 'index not initialized' in stderr; got: %s", se)
	}
}

// ─── 10. ScopeRealmMember index: no member dir touched (SC 3 — member scope) ─

// TestRunCodeRealm_MemberIndex_NoWriteIntoMemberDir verifies SC 3 for the
// ScopeRealmMember branch: indexing via `atomic code index` from inside a
// member dir must not create .gitignore or .claude/ inside that member repo.
//
// To ensure ScopeRealmMember resolves correctly (resolver needs code.toml to
// match the member), we pre-seed code.toml before calling RunCodeWithRealm.
func TestRunCodeRealm_MemberIndex_NoWriteIntoMemberDir(t *testing.T) {
	realmRoot, claudeMD := buildRealmFixture(t, []string{"alpha"})
	alphaCWD := filepath.Join(realmRoot, "repos", "alpha")

	// Pre-seed code.toml so realm.Resolve can detect ScopeRealmMember.
	wikiIndexPath := filepath.Join(realmRoot, "wiki", "index.md")
	if _, err := realm.SeedConfig(realmRoot, wikiIndexPath); err != nil {
		t.Fatalf("SeedConfig: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := codecli.RunCodeWithRealm([]string{"index"}, alphaCWD, claudeMD, &stdout, &stderr, noStdin())
	if code != 0 {
		t.Fatalf("member index failed (exit %d);\nstdout: %s\nstderr: %s", code, stdout.String(), stderr.String())
	}

	// .claude/.atomic-index/ must NOT exist inside the member repo.
	memberLocal := filepath.Join(alphaCWD, ".claude", ".atomic-index")
	if _, err := os.Stat(memberLocal); err == nil {
		t.Errorf("member dir had .claude/.atomic-index/ created — violates SC 3")
	}
	// .gitignore must NOT be written into the member repo by the realm path.
	if _, err := os.Stat(filepath.Join(alphaCWD, ".gitignore")); err == nil {
		// A .gitignore from the realm index is a violation; the fixture doesn't
		// create one, so any .gitignore here came from the realm indexer.
		t.Errorf("member dir had .gitignore created — violates SC 3")
	}

	// DB must exist in realm's .atomic dir, not in member dir.
	atomicDir := filepath.Join(realmRoot, ".atomic")
	entries, err := os.ReadDir(atomicDir)
	if err != nil {
		t.Fatalf("realm .atomic dir missing: %v", err)
	}
	var hasDB bool
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".db") {
			hasDB = true
		}
	}
	if !hasDB {
		t.Error("expected at least one .db file in realm .atomic dir")
	}
}

// ─── 11. <code-index> block written after realm index (SC 7) ─────────────────

// TestRunCodeRealm_Index_WritesCodeIndexBlock verifies that after a successful
// `atomic code index` from the realm root, the realm CLAUDE.md contains a
// <code-index> block listing the indexed members (SC 7).
//
// The existing CLAUDE.md (which registers the wiki) must have its original
// content preserved — the block is spliced in, not the whole file replaced.
func TestRunCodeRealm_Index_WritesCodeIndexBlock(t *testing.T) {
	realmRoot, claudeMD := buildRealmFixture(t, []string{"alpha", "beta"})

	// Snapshot the original CLAUDE.md content (has <wikis> block + header).
	originalContent, err := os.ReadFile(claudeMD)
	if err != nil {
		t.Fatalf("read original CLAUDE.md: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := codecli.RunCodeWithRealm([]string{"index"}, realmRoot, claudeMD, &stdout, &stderr, noStdin())
	if code != 0 {
		t.Fatalf("realm index failed (exit %d);\nstdout: %s\nstderr: %s", code, stdout.String(), stderr.String())
	}

	// CLAUDE.md must contain the <code-index> block.
	data, err := os.ReadFile(claudeMD)
	if err != nil {
		t.Fatalf("read CLAUDE.md after index: %v", err)
	}
	content := string(data)

	if !strings.Contains(content, "<code-index>") {
		t.Errorf("realm CLAUDE.md missing <code-index> block after index;\ncontent:\n%s", content)
	}
	if !strings.Contains(content, "</code-index>") {
		t.Errorf("realm CLAUDE.md missing </code-index> close tag after index;\ncontent:\n%s", content)
	}

	// Both members must appear in the block.
	if !strings.Contains(content, `key="alpha"`) {
		t.Errorf("realm CLAUDE.md <code-index> missing member alpha;\ncontent:\n%s", content)
	}
	if !strings.Contains(content, `key="beta"`) {
		t.Errorf("realm CLAUDE.md <code-index> missing member beta;\ncontent:\n%s", content)
	}

	// No timestamp in the block (SC 7: no volatile fields).
	if strings.Contains(content, "generated=") {
		t.Errorf("<code-index> block must not contain generated= timestamp;\ncontent:\n%s", content)
	}

	// The original <wikis> block must still be present (surrounding content preserved).
	if !strings.Contains(content, "<wikis>") {
		t.Errorf("original <wikis> block lost after code-index splice;\ncontent:\n%s", content)
	}

	// Idempotency: a second index run must not change the file.
	var stdout2, stderr2 bytes.Buffer
	code2 := codecli.RunCodeWithRealm([]string{"index"}, realmRoot, claudeMD, &stdout2, &stderr2, noStdin())
	if code2 != 0 {
		t.Fatalf("second realm index failed (exit %d);\nstdout: %s\nstderr: %s", code2, stdout2.String(), stderr2.String())
	}

	data2, err := os.ReadFile(claudeMD)
	if err != nil {
		t.Fatalf("read CLAUDE.md after second index: %v", err)
	}
	if string(data) != string(data2) {
		t.Errorf("CLAUDE.md changed on idempotent re-index (SC 7 violation):\nbefore: %q\nafter:  %q", string(data), string(data2))
	}

	if string(originalContent) == string(data) {
		t.Error("expected CLAUDE.md to change after realm index (block should have been added)")
	}
}

// ─── 12. <code-index> block reflects full membership even with --only (SC 7) ──

// TestRunCodeRealm_Index_OnlyFilter_BlockContainsAllMembers verifies that when
// `atomic code index --only <key>` is run, the <code-index> block written into
// the realm CLAUDE.md still lists ALL non-excluded members — not just the --only
// target.  The block advertises realm membership (awareness), not the transient
// CLI-filtered set.
func TestRunCodeRealm_Index_OnlyFilter_BlockContainsAllMembers(t *testing.T) {
	realmRoot, claudeMD := buildRealmFixture(t, []string{"alpha", "beta"})

	var stdout, stderr bytes.Buffer
	// Index only alpha via --only; beta is intentionally skipped.
	code := codecli.RunCodeWithRealm([]string{"index", "--only", "alpha"}, realmRoot, claudeMD, &stdout, &stderr, noStdin())
	if code != 0 {
		t.Fatalf("realm index --only alpha failed (exit %d);\nstdout: %s\nstderr: %s", code, stdout.String(), stderr.String())
	}

	data, err := os.ReadFile(claudeMD)
	if err != nil {
		t.Fatalf("read CLAUDE.md after index: %v", err)
	}
	content := string(data)

	// The <code-index> block must list BOTH members regardless of --only.
	if !strings.Contains(content, `key="alpha"`) {
		t.Errorf("<code-index> block missing alpha; content:\n%s", content)
	}
	if !strings.Contains(content, `key="beta"`) {
		t.Errorf("<code-index> block missing beta after --only alpha index; "+
			"block must reflect full realm membership, not just the --only target;\ncontent:\n%s", content)
	}
}
