package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- RepoConfig.Scope / checkUnknownRepoKeys ---

// TestLoadRepoConfig_ScopeParses: a top-level scope key parses onto
// RepoConfig.Scope with no warning.
func TestLoadRepoConfig_ScopeParses(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "atomic.toml")
	content := "scope = \"repo\"\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, warns, err := LoadRepoConfig(path)
	if err != nil {
		t.Fatalf("LoadRepoConfig: %v", err)
	}
	if len(warns) != 0 {
		t.Errorf("unexpected warnings: %v", warns)
	}
	if cfg.Scope != "repo" {
		t.Errorf("cfg.Scope = %q, want %q", cfg.Scope, "repo")
	}
}

// TestLoadRepoConfig_ScopeAlongsideCode: scope coexists with the [code]
// table with no warning and both values parse.
func TestLoadRepoConfig_ScopeAlongsideCode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "atomic.toml")
	content := "scope = \"realm\"\n[code]\nignore = [\"vendor/**\"]\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, warns, err := LoadRepoConfig(path)
	if err != nil {
		t.Fatalf("LoadRepoConfig: %v", err)
	}
	if len(warns) != 0 {
		t.Errorf("unexpected warnings: %v", warns)
	}
	if cfg.Scope != "realm" {
		t.Errorf("cfg.Scope = %q, want %q", cfg.Scope, "realm")
	}
	if len(cfg.Code.Ignore) != 1 || cfg.Code.Ignore[0] != "vendor/**" {
		t.Errorf("cfg.Code.Ignore = %v, want [vendor/**]", cfg.Code.Ignore)
	}
}

// --- ValidScope ---

func TestValidScope(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"repo", true},
		{"realm", true},
		{"", false},
		{"bogus", false},
		{"Repo", false},
	}
	for _, tc := range cases {
		if got := ValidScope(tc.in); got != tc.want {
			t.Errorf("ValidScope(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

// --- FindScopeRoot ---

// TestFindScopeRoot_NestingByKind: the design doc's nesting case — a
// scope="repo" marker sits between cwd and a scope="realm" root above it.
// The by-kind walk must resolve both kinds correctly from the same cwd,
// continuing past the marker of the other kind.
func TestFindScopeRoot_NestingByKind(t *testing.T) {
	restore := SetHarnessDirForTest(".claude")
	defer restore()

	realmRoot := t.TempDir()
	repoRoot := filepath.Join(realmRoot, "server")
	cwd := filepath.Join(repoRoot, "src")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}

	writeAtomicToml(t, realmRoot, "scope = \"realm\"\n")
	writeAtomicToml(t, repoRoot, "scope = \"repo\"\n")

	repoFound, ok := FindScopeRoot(cwd, "repo")
	if !ok || repoFound != repoRoot {
		t.Errorf("FindScopeRoot(cwd, \"repo\") = (%q, %v), want (%q, true)", repoFound, ok, repoRoot)
	}

	realmFound, ok := FindScopeRoot(cwd, "realm")
	if !ok || realmFound != realmRoot {
		t.Errorf("FindScopeRoot(cwd, \"realm\") = (%q, %v), want (%q, true)", realmFound, ok, realmRoot)
	}
}

// TestFindScopeRoot_InvalidValueIgnored: a scope value that is neither
// "repo" nor "realm" never acts as a marker of either kind — the walk
// continues past it to a valid marker higher up.
func TestFindScopeRoot_InvalidValueIgnored(t *testing.T) {
	restore := SetHarnessDirForTest(".claude")
	defer restore()

	realmRoot := t.TempDir()
	invalidRoot := filepath.Join(realmRoot, "mid")
	cwd := filepath.Join(invalidRoot, "leaf")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}

	writeAtomicToml(t, realmRoot, "scope = \"realm\"\n")
	writeAtomicToml(t, invalidRoot, "scope = \"bogus\"\n")

	got, ok := FindScopeRoot(cwd, "realm")
	if !ok || got != realmRoot {
		t.Errorf("FindScopeRoot = (%q, %v), want (%q, true)", got, ok, realmRoot)
	}

	if _, ok := FindScopeRoot(cwd, "repo"); ok {
		t.Error("FindScopeRoot(cwd, \"repo\") should find nothing — invalid marker must not resolve either kind")
	}
}

// TestFindScopeRoot_MalformedFileIgnored: unparseable TOML at an
// intermediate level is skipped, not treated as a hard failure — the walk
// continues to a valid marker higher up.
func TestFindScopeRoot_MalformedFileIgnored(t *testing.T) {
	restore := SetHarnessDirForTest(".claude")
	defer restore()

	realmRoot := t.TempDir()
	malformedRoot := filepath.Join(realmRoot, "mid")
	cwd := filepath.Join(malformedRoot, "leaf")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}

	writeAtomicToml(t, realmRoot, "scope = \"realm\"\n")
	writeAtomicToml(t, malformedRoot, "scope = \"realm\n[code\n")

	got, ok := FindScopeRoot(cwd, "realm")
	if !ok || got != realmRoot {
		t.Errorf("FindScopeRoot = (%q, %v), want (%q, true)", got, ok, realmRoot)
	}
}

// TestFindScopeRoot_NoMarker: no marker anywhere up to the filesystem root
// returns found=false with no error and no panic.
func TestFindScopeRoot_NoMarker(t *testing.T) {
	restore := SetHarnessDirForTest(".claude")
	defer restore()

	dir := t.TempDir()
	if _, ok := FindScopeRoot(dir, "repo"); ok {
		t.Error("expected found=false with no marker present")
	}
}

// writeAtomicToml writes content to <root>/.claude/atomic.toml (harness dir
// fixed to ".claude" by the caller via SetHarnessDirForTest).
func writeAtomicToml(t *testing.T, root, content string) {
	t.Helper()
	path := RepoConfigPath(root)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// --- EnsureScopeMarker ---

// TestEnsureScopeMarker_CreatesWhenAbsent: no repo config exists — it is
// created with just the scope line.
func TestEnsureScopeMarker_CreatesWhenAbsent(t *testing.T) {
	restore := SetHarnessDirForTest(".claude")
	defer restore()

	root := t.TempDir()

	outcome, err := EnsureScopeMarker(root, "repo")
	if err != nil {
		t.Fatalf("EnsureScopeMarker: %v", err)
	}
	if outcome != ScopeMarkerCreated {
		t.Errorf("outcome = %q, want %q", outcome, ScopeMarkerCreated)
	}

	got, err := os.ReadFile(RepoConfigPath(root))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "scope = \"repo\"\n" {
		t.Errorf("content = %q, want %q", got, "scope = \"repo\"\n")
	}
}

// TestEnsureScopeMarker_InsertsAboveFirstTable is the highest-risk case in
// this slice: scope is a top-level key, so on a file that already has a
// [code] table (this repo's own .claude/atomic.toml shape), the key must
// land above the table header. Appending at EOF would land it inside
// [code] and parse as code.scope instead of the top-level key.
func TestEnsureScopeMarker_InsertsAboveFirstTable(t *testing.T) {
	restore := SetHarnessDirForTest(".claude")
	defer restore()

	root := t.TempDir()
	existing := "[code]\nignore = [\"atomic/internal/serve/assets/vendor/**\"]\n"
	writeAtomicToml(t, root, existing)

	outcome, err := EnsureScopeMarker(root, "repo")
	if err != nil {
		t.Fatalf("EnsureScopeMarker: %v", err)
	}
	if outcome != ScopeMarkerAdded {
		t.Errorf("outcome = %q, want %q", outcome, ScopeMarkerAdded)
	}

	got, err := os.ReadFile(RepoConfigPath(root))
	if err != nil {
		t.Fatal(err)
	}
	want := "scope = \"repo\"\n" + existing
	if string(got) != want {
		t.Errorf("content:\ngot:\n%s\nwant:\n%s", got, want)
	}

	// The decisive assertion: it must parse as the top-level key, not
	// code.scope.
	cfg, warns, err := LoadRepoConfig(RepoConfigPath(root))
	if err != nil {
		t.Fatalf("LoadRepoConfig: %v", err)
	}
	if len(warns) != 0 {
		t.Errorf("unexpected warnings: %v", warns)
	}
	if cfg.Scope != "repo" {
		t.Errorf("cfg.Scope = %q, want %q (top-level, not code.scope)", cfg.Scope, "repo")
	}
	if len(cfg.Code.Ignore) != 1 {
		t.Errorf("cfg.Code.Ignore = %v, want the original ignore entry preserved", cfg.Code.Ignore)
	}
}

// TestEnsureScopeMarker_NoTableHeader_AppendsAtEOF: an existing file with
// no table header (e.g. only comments) has the line appended, with a
// separating newline inserted if the file lacked a trailing one.
func TestEnsureScopeMarker_NoTableHeader_AppendsAtEOF(t *testing.T) {
	restore := SetHarnessDirForTest(".claude")
	defer restore()

	root := t.TempDir()
	existing := "# a comment, no trailing newline"
	writeAtomicToml(t, root, existing)

	outcome, err := EnsureScopeMarker(root, "realm")
	if err != nil {
		t.Fatalf("EnsureScopeMarker: %v", err)
	}
	if outcome != ScopeMarkerAdded {
		t.Errorf("outcome = %q, want %q", outcome, ScopeMarkerAdded)
	}

	got, err := os.ReadFile(RepoConfigPath(root))
	if err != nil {
		t.Fatal(err)
	}
	want := existing + "\nscope = \"realm\"\n"
	if string(got) != want {
		t.Errorf("content:\ngot:\n%s\nwant:\n%s", got, want)
	}
}

// TestEnsureScopeMarker_OKWhenAlreadyPresent: the file already declares
// this exact scope — nothing is written, byte-for-byte.
func TestEnsureScopeMarker_OKWhenAlreadyPresent(t *testing.T) {
	restore := SetHarnessDirForTest(".claude")
	defer restore()

	root := t.TempDir()
	existing := "scope = \"repo\"\n[code]\nignore = [\"vendor/**\"]\n"
	writeAtomicToml(t, root, existing)

	outcome, err := EnsureScopeMarker(root, "repo")
	if err != nil {
		t.Fatalf("EnsureScopeMarker: %v", err)
	}
	if outcome != ScopeMarkerOK {
		t.Errorf("outcome = %q, want %q", outcome, ScopeMarkerOK)
	}

	got, err := os.ReadFile(RepoConfigPath(root))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != existing {
		t.Errorf("file must be untouched:\ngot:\n%s\nwant:\n%s", got, existing)
	}
}

// TestEnsureScopeMarker_ConflictLeavesFileUntouched: the file declares a
// different scope — the outcome reports conflict and not one byte changes.
func TestEnsureScopeMarker_ConflictLeavesFileUntouched(t *testing.T) {
	restore := SetHarnessDirForTest(".claude")
	defer restore()

	root := t.TempDir()
	existing := "scope = \"realm\"\n"
	writeAtomicToml(t, root, existing)

	outcome, err := EnsureScopeMarker(root, "repo")
	if err != nil {
		t.Fatalf("EnsureScopeMarker: %v", err)
	}
	if outcome != ScopeMarkerConflict {
		t.Errorf("outcome = %q, want %q", outcome, ScopeMarkerConflict)
	}

	got, err := os.ReadFile(RepoConfigPath(root))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != existing {
		t.Errorf("file must be untouched on conflict:\ngot:\n%s\nwant:\n%s", got, existing)
	}
}

// TestEnsureScopeMarker_Idempotent: created then re-run reports ok and
// writes nothing further.
func TestEnsureScopeMarker_Idempotent(t *testing.T) {
	restore := SetHarnessDirForTest(".claude")
	defer restore()

	root := t.TempDir()

	if _, err := EnsureScopeMarker(root, "repo"); err != nil {
		t.Fatalf("first EnsureScopeMarker: %v", err)
	}
	before, err := os.ReadFile(RepoConfigPath(root))
	if err != nil {
		t.Fatal(err)
	}

	outcome, err := EnsureScopeMarker(root, "repo")
	if err != nil {
		t.Fatalf("second EnsureScopeMarker: %v", err)
	}
	if outcome != ScopeMarkerOK {
		t.Errorf("outcome = %q, want %q", outcome, ScopeMarkerOK)
	}
	after, err := os.ReadFile(RepoConfigPath(root))
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Errorf("second run changed the file:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

// TestEnsureScopeMarker_MalformedFileErrors: a malformed existing file is
// never blindly written into — the caller gets an error instead.
func TestEnsureScopeMarker_MalformedFileErrors(t *testing.T) {
	restore := SetHarnessDirForTest(".claude")
	defer restore()

	root := t.TempDir()
	existing := "[code\nignore = [\"vendor/**\"\n"
	writeAtomicToml(t, root, existing)

	if _, err := EnsureScopeMarker(root, "repo"); err == nil {
		t.Fatal("expected error on malformed existing file, got nil")
	}

	got, err := os.ReadFile(RepoConfigPath(root))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != existing {
		t.Errorf("malformed file must be untouched:\ngot:\n%s\nwant:\n%s", got, existing)
	}
}

// TestEnsureScopeMarker_MultilineArrayOfArrays_NotMistakenForHeader:
// reviewer-reported bug — an interior line of a multi-line array-of-arrays
// (e.g. "  [1, 2],") also starts with "[" once trimmed. The old table-header
// detector fired on it and spliced the scope line mid-array, producing
// unparseable TOML. A line only counts as a table header at bracket depth
// zero.
func TestEnsureScopeMarker_MultilineArrayOfArrays_NotMistakenForHeader(t *testing.T) {
	restore := SetHarnessDirForTest(".claude")
	defer restore()

	root := t.TempDir()
	existing := "matrix = [\n  [1, 2],\n  [3, 4],\n]\n[code]\nignore = [\"vendor/**\"]\n"
	writeAtomicToml(t, root, existing)

	outcome, err := EnsureScopeMarker(root, "repo")
	if err != nil {
		t.Fatalf("EnsureScopeMarker: %v", err)
	}
	if outcome != ScopeMarkerAdded {
		t.Errorf("outcome = %q, want %q", outcome, ScopeMarkerAdded)
	}

	// "matrix" is not a schema key, so LoadRepoConfig reports it as an
	// unrelated unknown-key warning — this test only cares that the file
	// still parses and scope landed at top level, not about that warning.
	cfg, _, err := LoadRepoConfig(RepoConfigPath(root))
	if err != nil {
		t.Fatalf("LoadRepoConfig after insert: %v (file did not round-trip)", err)
	}
	if cfg.Scope != "repo" {
		t.Errorf("cfg.Scope = %q, want %q (top-level)", cfg.Scope, "repo")
	}

	want := "matrix = [\n  [1, 2],\n  [3, 4],\n]\nscope = \"repo\"\n[code]\nignore = [\"vendor/**\"]\n"
	got, err := os.ReadFile(RepoConfigPath(root))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Errorf("content:\ngot:\n%s\nwant:\n%s", got, want)
	}
}

// TestEnsureScopeMarker_PreservesCRLFLineEnding: the inserted line matches
// the file's dominant existing line ending instead of always using LF, so a
// CRLF-authored file doesn't get one LF line spliced into an otherwise-CRLF
// file.
func TestEnsureScopeMarker_PreservesCRLFLineEnding(t *testing.T) {
	restore := SetHarnessDirForTest(".claude")
	defer restore()

	root := t.TempDir()
	existing := "[code]\r\nignore = [\"vendor/**\"]\r\n"
	writeAtomicToml(t, root, existing)

	outcome, err := EnsureScopeMarker(root, "repo")
	if err != nil {
		t.Fatalf("EnsureScopeMarker: %v", err)
	}
	if outcome != ScopeMarkerAdded {
		t.Errorf("outcome = %q, want %q", outcome, ScopeMarkerAdded)
	}

	got, err := os.ReadFile(RepoConfigPath(root))
	if err != nil {
		t.Fatal(err)
	}
	want := "scope = \"repo\"\r\n" + existing
	if string(got) != want {
		t.Errorf("content:\ngot:\n%s\nwant:\n%s", got, want)
	}

	cfg, _, err := LoadRepoConfig(RepoConfigPath(root))
	if err != nil {
		t.Fatalf("LoadRepoConfig after insert: %v", err)
	}
	if cfg.Scope != "repo" {
		t.Errorf("cfg.Scope = %q, want %q", cfg.Scope, "repo")
	}
}

// TestScopeSource_String locks in the lowercase output tokens.
func TestScopeSource_String(t *testing.T) {
	cases := map[ScopeSource]string{
		ScopeSourceNone:     "none",
		ScopeSourceMarker:   "marker",
		ScopeSourceGit:      "git",
		ScopeSourceRegistry: "registry",
		ScopeSourceCwd:      "cwd",
	}
	for src, want := range cases {
		if got := src.String(); got != want {
			t.Errorf("%v.String() = %q, want %q", src, got, want)
		}
	}
}

// TestCheckUnknownRepoKeys_ScopeIsKnownLeaf: scope at the top level must not
// warn as unknown, and every other key's unknown-key behavior is unchanged.
func TestCheckUnknownRepoKeys_ScopeIsKnownLeaf(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "atomic.toml")
	content := "scope = \"repo\"\n[code]\nignore = [\"vendor/**\"]\n[bogus]\nkey = \"value\"\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	_, warns, err := LoadRepoConfig(path)
	if err != nil {
		t.Fatalf("LoadRepoConfig: %v", err)
	}
	if len(warns) != 1 {
		t.Fatalf("warns = %v, want exactly 1 (the unrelated [bogus] table, not scope)", warns)
	}
	if !strings.Contains(warns[0].Message, "bogus") {
		t.Errorf("warning %q does not mention unknown key %q", warns[0].Message, "bogus")
	}
}
