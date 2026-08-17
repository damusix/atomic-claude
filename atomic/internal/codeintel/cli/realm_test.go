package cli_test

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

func writeGoFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// buildRealmFixture returns the realm root and a CLAUDE.md path registering it.
func buildRealmFixture(t *testing.T, memberNames []string) (realmRoot, claudeMD string) {
	t.Helper()
	realmRoot = t.TempDir()
	claudeMD = filepath.Join(realmRoot, "CLAUDE.md")

	wikiIndexPath := filepath.Join(realmRoot, "wiki", "index.md")
	writeRealmCLAUDEMD(t, claudeMD, wikiIndexPath)

	var scanBlock strings.Builder
	scanBlock.WriteString("<wiki-scan generated=\"2026-01-01\" root=\"" + realmRoot + "\">\n")
	for _, name := range memberNames {
		scanBlock.WriteString("  <repo path=\"repos/" + name + "\" status=\"indexed\"/>\n")
	}
	scanBlock.WriteString("</wiki-scan>\n")
	writeGoFile(t, wikiIndexPath, "# wiki\n\n"+scanBlock.String())

	for _, name := range memberNames {
		memberDir := filepath.Join(realmRoot, "repos", name)
		writeGoFile(t, filepath.Join(memberDir, "main.go"),
			"package "+name+"\n\nfunc Hello"+capitalize(name)+"() string { return \"hello\" }\n")
	}
	return realmRoot, claudeMD
}

func writeRealmCLAUDEMD(t *testing.T, claudeMD, wikiIndexPath string) {
	t.Helper()
	content := "# CLAUDE.md\n\n<wikis>\n- " + wikiIndexPath + "\n</wikis>\n"
	writeGoFile(t, claudeMD, content)
}

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

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func TestRunCodeRealm_ScopeRepo_Unchanged(t *testing.T) {
	dir := writeFixture(t)
	indexedEngine(t, dir)

	var stdout, stderr bytes.Buffer
	// No wikis, so the realm path is bypassed.
	claudeMD := filepath.Join(t.TempDir(), "CLAUDE.md")
	writeGoFile(t, claudeMD, "# no wikis\n")

	code := codecli.RunCodeWithRealm([]string{"status"}, dir, claudeMD, &stdout, &stderr, noStdin())
	if code != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", code, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "initialized:") {
		t.Fatalf("expected 'initialized:' in output, got: %s", out)
	}
}

func TestRunCodeRealm_Index_SeedsConfigAndWritesRealmDBs(t *testing.T) {
	realmRoot, claudeMD := buildRealmFixture(t, []string{"alpha", "beta"})

	var stdout, stderr bytes.Buffer
	code := codecli.RunCodeWithRealm([]string{"index"}, realmRoot, claudeMD, &stdout, &stderr, noStdin())
	if code != 0 {
		t.Fatalf("realm index failed (exit %d);\nstdout: %s\nstderr: %s", code, stdout.String(), stderr.String())
	}

	tomlPath := filepath.Join(realmRoot, ".atomic", "code.toml")
	if _, err := os.Stat(tomlPath); err != nil {
		t.Fatalf("code.toml not created: %v", err)
	}

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

func TestRunCodeRealm_Search_GroupedByKey(t *testing.T) {
	realmRoot, claudeMD := buildRealmFixture(t, []string{"alpha", "beta"})

	var idx bytes.Buffer
	if code := codecli.RunCodeWithRealm([]string{"index"}, realmRoot, claudeMD, &idx, &idx, noStdin()); code != 0 {
		t.Fatalf("index failed: %s", idx.String())
	}

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

func TestRunCodeRealm_PartialFailure_MissingDB(t *testing.T) {
	realmRoot, claudeMD := buildRealmFixture(t, []string{"alpha", "beta"})

	cfg, err := realm.SeedConfig(realmRoot, filepath.Join(realmRoot, "wiki", "index.md"))
	if err != nil || cfg == nil {
		t.Fatalf("seed failed: %v", err)
	}

	for _, m := range cfg.Members {
		if m.Key == "alpha" {
			indexMember(t, filepath.Join(realmRoot, m.Path), realm.Resolution{RealmRoot: realmRoot}.DBPath(m.Key))
		}
	}

	alphaKey := ""
	for _, m := range cfg.Members {
		if strings.HasSuffix(m.Path, "alpha") {
			alphaKey = m.Key
		}
	}
	if alphaKey == "" {
		t.Fatal("could not find alpha member key")
	}

	alphaDB := filepath.Join(realmRoot, ".atomic", alphaKey+".db")
	if _, err := os.Stat(alphaDB); err != nil {
		t.Fatalf("alpha db not found at %s: %v", alphaDB, err)
	}

	var stdout, stderr bytes.Buffer
	code := codecli.RunCodeWithRealm([]string{"search", "Hello"}, realmRoot, claudeMD, &stdout, &stderr, noStdin())
	// A skipped member is not a failure: the aggregate exit stays 0.
	if code != 0 {
		t.Fatalf("expected exit 0 on partial failure, got %d; stderr: %s", code, stderr.String())
	}
	se := stderr.String()
	if !strings.Contains(se, "not indexed") {
		t.Errorf("expected 'not indexed' warning in stderr, got: %s", se)
	}
	so := stdout.String()
	if !strings.Contains(so, "[alpha]") {
		t.Errorf("expected [alpha] results even on partial failure, got: %s", so)
	}
}

func TestRunCodeRealm_OnlyFilter(t *testing.T) {
	realmRoot, claudeMD := buildRealmFixture(t, []string{"alpha", "beta"})

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

func TestRunCodeRealm_ExcludeFilter(t *testing.T) {
	realmRoot, claudeMD := buildRealmFixture(t, []string{"alpha", "beta"})

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

func TestRunCodeRealm_ScopeRealmMember(t *testing.T) {
	realmRoot, claudeMD := buildRealmFixture(t, []string{"alpha", "beta"})

	var idx bytes.Buffer
	if code := codecli.RunCodeWithRealm([]string{"index"}, realmRoot, claudeMD, &idx, &idx, noStdin()); code != 0 {
		t.Fatalf("index failed: %s", idx.String())
	}

	alphaCWD := filepath.Join(realmRoot, "repos", "alpha")

	var stdout, stderr bytes.Buffer
	code := codecli.RunCodeWithRealm([]string{"search", "Hello"}, alphaCWD, claudeMD, &stdout, &stderr, noStdin())
	if code != 0 {
		t.Fatalf("member search failed (exit %d); stderr: %s", code, stderr.String())
	}
	out := stdout.String()
	// Single target, so no [key] grouping header.
	if strings.Contains(out, "[alpha]") {
		t.Errorf("ScopeRealmMember should not wrap with [key] header, got: %s", out)
	}
	if strings.Contains(out, "HelloBeta") {
		t.Errorf("ScopeRealmMember should not include beta results, got: %s", out)
	}
}

// The exit code depends on whether the test process happens to sit in an
// indexed git repo, so the only invariant asserted here is "no crash".
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

// ScopeRepo must query projectRoot.s index without consulting the process cwd —
// the earlier cwd-based resolution passed locally and failed in CI.
func TestRunCodeRealm_ScopeRepo_UsesProjectRootIndex(t *testing.T) {
	dir := writeFixture(t)
	indexedEngine(t, dir)

	claudeMD := filepath.Join(t.TempDir(), "CLAUDE.md")
	writeGoFile(t, claudeMD, "# no wikis\n")

	var stdout, stderr bytes.Buffer
	code := codecli.RunCodeWithRealm([]string{"status"}, dir, claudeMD, &stdout, &stderr, noStdin())
	if code != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "initialized:") {
		t.Errorf("expected 'initialized:' in output, got: %s", stdout.String())
	}
}

// A cwd inside a git repo must resolve to the git root, not be treated as the
// project root itself. t.Chdir makes this test non-parallel.
func TestRunCodeRealm_RepoScope_SubdirResolvesToGitRoot(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	repoRoot := t.TempDir()
	if out, err := exec.Command("git", "init", repoRoot).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}

	subdir := filepath.Join(repoRoot, "src")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatal(err)
	}

	claudeMD := filepath.Join(t.TempDir(), "CLAUDE.md")
	writeGoFile(t, claudeMD, "# no wikis\n")

	t.Chdir(subdir)

	var stdout, stderr bytes.Buffer
	code := codecli.RunCodeWithRealm([]string{"search", "foo"}, subdir, claudeMD, &stdout, &stderr, noStdin())

	se := stderr.String()
	so := stdout.String()

	if strings.Contains(so, "[") && strings.Contains(so, "]") {
		t.Errorf("got realm-style [key] output — should not have taken realm path: %s", so)
	}
	if strings.Contains(se, "not inside a git repository") {
		t.Errorf("repoctx.Resolve failed — subdir→git-root resolution broken: %s", se)
	}
	if code == 0 {
		t.Errorf("expected non-zero exit for no-index repo-scope search, got 0; stdout: %s stderr: %s", so, se)
	}
	if !strings.Contains(se, "index not initialized") {
		t.Errorf("expected 'index not initialized' in stderr; got: %s", se)
	}
}

// Indexing from inside a member repo must leave that repo untouched.
func TestRunCodeRealm_MemberIndex_NoWriteIntoMemberDir(t *testing.T) {
	realmRoot, claudeMD := buildRealmFixture(t, []string{"alpha"})
	alphaCWD := filepath.Join(realmRoot, "repos", "alpha")

	// Without code.toml the resolver cannot match the member.
	wikiIndexPath := filepath.Join(realmRoot, "wiki", "index.md")
	if _, err := realm.SeedConfig(realmRoot, wikiIndexPath); err != nil {
		t.Fatalf("SeedConfig: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := codecli.RunCodeWithRealm([]string{"index"}, alphaCWD, claudeMD, &stdout, &stderr, noStdin())
	if code != 0 {
		t.Fatalf("member index failed (exit %d);\nstdout: %s\nstderr: %s", code, stdout.String(), stderr.String())
	}

	memberLocal := filepath.Join(alphaCWD, ".claude", ".atomic-index")
	if _, err := os.Stat(memberLocal); err == nil {
		t.Errorf("member dir had .claude/.atomic-index/ created — violates SC 3")
	}
	if _, err := os.Stat(filepath.Join(alphaCWD, ".gitignore")); err == nil {
		// The fixture writes none, so any file here came from the indexer.
		t.Errorf("member dir had .gitignore created — violates SC 3")
	}

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

// The block is spliced into the existing CLAUDE.md, never replacing it.
func TestRunCodeRealm_Index_WritesCodeIndexBlock(t *testing.T) {
	realmRoot, claudeMD := buildRealmFixture(t, []string{"alpha", "beta"})

	originalContent, err := os.ReadFile(claudeMD)
	if err != nil {
		t.Fatalf("read original CLAUDE.md: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := codecli.RunCodeWithRealm([]string{"index"}, realmRoot, claudeMD, &stdout, &stderr, noStdin())
	if code != 0 {
		t.Fatalf("realm index failed (exit %d);\nstdout: %s\nstderr: %s", code, stdout.String(), stderr.String())
	}

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

	if !strings.Contains(content, `key="alpha"`) {
		t.Errorf("realm CLAUDE.md <code-index> missing member alpha;\ncontent:\n%s", content)
	}
	if !strings.Contains(content, `key="beta"`) {
		t.Errorf("realm CLAUDE.md <code-index> missing member beta;\ncontent:\n%s", content)
	}

	// A timestamp would make the block diff on every run.
	if strings.Contains(content, "generated=") {
		t.Errorf("<code-index> block must not contain generated= timestamp;\ncontent:\n%s", content)
	}

	if !strings.Contains(content, "<wikis>") {
		t.Errorf("original <wikis> block lost after code-index splice;\ncontent:\n%s", content)
	}

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

// The block advertises realm membership, so --only must not narrow it.
func TestRunCodeRealm_Index_OnlyFilter_BlockContainsAllMembers(t *testing.T) {
	realmRoot, claudeMD := buildRealmFixture(t, []string{"alpha", "beta"})

	var stdout, stderr bytes.Buffer
	code := codecli.RunCodeWithRealm([]string{"index", "--only", "alpha"}, realmRoot, claudeMD, &stdout, &stderr, noStdin())
	if code != 0 {
		t.Fatalf("realm index --only alpha failed (exit %d);\nstdout: %s\nstderr: %s", code, stdout.String(), stderr.String())
	}

	data, err := os.ReadFile(claudeMD)
	if err != nil {
		t.Fatalf("read CLAUDE.md after index: %v", err)
	}
	content := string(data)

	if !strings.Contains(content, `key="alpha"`) {
		t.Errorf("<code-index> block missing alpha; content:\n%s", content)
	}
	if !strings.Contains(content, `key="beta"`) {
		t.Errorf("<code-index> block missing beta after --only alpha index; "+
			"block must reflect full realm membership, not just the --only target;\ncontent:\n%s", content)
	}
}

// sync and status act on the member.s realm db, which lives nowhere near the
// member repo, so both must work in member and realm-root scope alike. They
// were once rejected with advice the user had already followed.

func TestRunCodeRealm_ScopeRealmMember_Sync(t *testing.T) {
	realmRoot, claudeMD := buildRealmFixture(t, []string{"alpha", "beta"})

	var idx bytes.Buffer
	if code := codecli.RunCodeWithRealm([]string{"index"}, realmRoot, claudeMD, &idx, &idx, noStdin()); code != 0 {
		t.Fatalf("index failed: %s", idx.String())
	}

	alphaCWD := filepath.Join(realmRoot, "repos", "alpha")
	var stdout, stderr bytes.Buffer
	code := codecli.RunCodeWithRealm([]string{"sync"}, alphaCWD, claudeMD, &stdout, &stderr, noStdin())
	if code != 0 {
		t.Fatalf("member sync failed (exit %d); stderr: %s", code, stderr.String())
	}
	if strings.Contains(stderr.String(), "not available") {
		t.Errorf("member sync was rejected as 'not available'; the user is already in the member repo;\nstderr: %s", stderr.String())
	}
	if !strings.Contains(stdout.String(), "synced:") {
		t.Errorf("expected 'synced:' in member sync output; got stdout: %s\nstderr: %s", stdout.String(), stderr.String())
	}
	// Sync must never create a local index inside the member repo.
	if _, err := os.Stat(filepath.Join(alphaCWD, ".claude", ".atomic-index")); err == nil {
		t.Errorf("member sync created .claude/.atomic-index/ inside the member repo — must write to the realm db only")
	}
}

func TestRunCodeRealm_ScopeRealmMember_StatusReportsRealmDB(t *testing.T) {
	realmRoot, claudeMD := buildRealmFixture(t, []string{"alpha"})

	var idx bytes.Buffer
	if code := codecli.RunCodeWithRealm([]string{"index"}, realmRoot, claudeMD, &idx, &idx, noStdin()); code != 0 {
		t.Fatalf("index failed: %s", idx.String())
	}

	alphaCWD := filepath.Join(realmRoot, "repos", "alpha")
	var stdout, stderr bytes.Buffer
	code := codecli.RunCodeWithRealm([]string{"status"}, alphaCWD, claudeMD, &stdout, &stderr, noStdin())
	if code != 0 {
		t.Fatalf("member status failed (exit %d); stderr: %s", code, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "initialized:") {
		t.Errorf("expected status report; got: %s", out)
	}
	realmDB := filepath.Join(realmRoot, ".atomic", "alpha.db")
	if !strings.Contains(out, realmDB) {
		t.Errorf("member status should report the realm db path %q; got: %s", realmDB, out)
	}
	if strings.Contains(out, filepath.Join(alphaCWD, ".claude")) {
		t.Errorf("member status reported a member-local index path; it must report the realm db; got: %s", out)
	}
}

func TestRunCodeRealm_ScopeRealmAll_SyncFansOut(t *testing.T) {
	realmRoot, claudeMD := buildRealmFixture(t, []string{"alpha", "beta"})

	var idx bytes.Buffer
	if code := codecli.RunCodeWithRealm([]string{"index"}, realmRoot, claudeMD, &idx, &idx, noStdin()); code != 0 {
		t.Fatalf("index failed: %s", idx.String())
	}

	var stdout, stderr bytes.Buffer
	code := codecli.RunCodeWithRealm([]string{"sync"}, realmRoot, claudeMD, &stdout, &stderr, noStdin())
	if code != 0 {
		t.Fatalf("realm-root sync failed (exit %d); stderr: %s", code, stderr.String())
	}
	if strings.Contains(stderr.String(), "not available") {
		t.Errorf("realm-root sync was rejected; it should fan out across members;\nstderr: %s", stderr.String())
	}
	out := stdout.String()
	for _, key := range []string{"[alpha]", "[beta]"} {
		if !strings.Contains(out, key) {
			t.Errorf("expected %s header in fan-out sync output; got: %s", key, out)
		}
	}
	if !strings.Contains(out, "synced:") {
		t.Errorf("expected 'synced:' lines in fan-out output; got: %s", out)
	}
}
