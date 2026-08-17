package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- RepoConfig.Scope / checkUnknownRepoKeys ---

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

// The nesting case: a scope="repo" marker between cwd and a scope="realm" root
// above it. The by-kind walk must resolve both from the same cwd, continuing
// past the marker of the other kind.
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

// A scope value that is neither kind never acts as a marker — the walk continues
// to a valid marker higher up.
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

// Unparseable TOML at an intermediate level is skipped, not a hard failure.
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

func TestFindScopeRoot_NoMarker(t *testing.T) {
	restore := SetHarnessDirForTest(".claude")
	defer restore()

	dir := t.TempDir()
	if _, ok := FindScopeRoot(dir, "repo"); ok {
		t.Error("expected found=false with no marker present")
	}
}

// filepath.Dir on a relative path short-circuits at "." instead of walking up,
// so a relative startDir used to never reach a marker at a real ancestor.
func TestFindScopeRoot_RelativeStartDir(t *testing.T) {
	restore := SetHarnessDirForTest(".claude")
	defer restore()

	root := t.TempDir()
	writeAtomicToml(t, root, "scope = \"repo\"\n")

	nested := filepath.Join(root, "src", "pkg")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}

	origWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(origWd) })

	// filepath.Abs resolves via os.Getwd, which canonicalizes symlinks — resolve
	// the expected root the same way so this is not a false negative there.
	wantRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}

	got, ok := FindScopeRoot(filepath.Join("src", "pkg"), "repo")
	if !ok || got != wantRoot {
		t.Errorf("FindScopeRoot(relative) = (%q, %v), want (%q, true)", got, ok, wantRoot)
	}
}

// writeAtomicToml writes content to <root>/.claude/atomic.toml; the caller pins
// the harness dir via SetHarnessDirForTest.
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

// scope is a top-level key, so on a file that already has a [code] table it must
// land above the header: appending at EOF would put it inside [code] and parse
// as code.scope.
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

	// The decisive assertion: top-level key, not code.scope.
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

// A file with no table header has the line appended, with a separating newline
// when the file lacked a trailing one.
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

// Already declaring this exact scope writes nothing, byte-for-byte.
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

// A different declared scope reports conflict and changes not one byte.
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

// A malformed existing file is never blindly written into.
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

// An interior line of a multi-line array-of-arrays also starts with "[" once
// trimmed. The old detector fired on it and spliced the scope line mid-array,
// producing unparseable TOML. A line counts as a table header only at bracket
// depth zero.
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

	// "matrix" is not a schema key, so its unknown-key warning is expected and
	// beside the point here.
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

// The inserted line matches the file's dominant line ending, so a CRLF-authored
// file does not get one LF line spliced in.
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

// A top-level scope key must not warn as unknown, and every other key's
// unknown-key behavior is unchanged.
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
