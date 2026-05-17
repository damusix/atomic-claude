package signals_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/frontmatter"
	"github.com/damusix/atomic-claude/atomic/internal/signals"
)

// makeRepo creates a minimal temporary repo directory with the given files.
// files maps relative path → content.
func makeRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, content := range files {
		abs := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatalf("makeRepo: mkdir %s: %v", rel, err)
		}
		if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
			t.Fatalf("makeRepo: write %s: %v", rel, err)
		}
	}
	return root
}

func initGit(t *testing.T, root string) {
	t.Helper()
	cmds := [][]string{
		{"git", "-C", root, "init"},
		{"git", "-C", root, "config", "user.email", "test@test.com"},
		{"git", "-C", root, "config", "user.name", "Test"},
	}
	for _, args := range cmds {
		if out, err := exec.Command(args[0], args[1:]...).CombinedOutput(); err != nil {
			t.Fatalf("initGit %v: %v\n%s", args, err, out)
		}
	}
}

// ---- Tree scanner ----

func TestScanTree_EmptyRepo(t *testing.T) {
	root := makeRepo(t, nil)
	out, err := signals.ScanTree(root)
	if err != nil {
		t.Fatalf("ScanTree empty: %v", err)
	}
	if out != "" {
		t.Errorf("expected empty tree, got %q", out)
	}
}

func TestScanTree_SingleFile(t *testing.T) {
	root := makeRepo(t, map[string]string{
		"main.go": "package main\n",
	})
	out, err := signals.ScanTree(root)
	if err != nil {
		t.Fatalf("ScanTree: %v", err)
	}
	if !strings.Contains(out, "main.go") {
		t.Errorf("expected main.go in tree, got:\n%s", out)
	}
}

func TestScanTree_SkipDirs(t *testing.T) {
	root := makeRepo(t, map[string]string{
		"main.go":                      "package main\n",
		".git/HEAD":                    "ref: refs/heads/main\n",
		"node_modules/lodash/index.js": "// lodash\n",
		".claude/project/signals.md":   "---\n---\n",
		"vendor/lib/lib.go":            "package lib\n",
	})
	out, err := signals.ScanTree(root)
	if err != nil {
		t.Fatalf("ScanTree: %v", err)
	}
	for _, bad := range []string{".git", "node_modules", ".claude", "vendor"} {
		if strings.Contains(out, bad) {
			t.Errorf("ScanTree should skip %s, but found it in:\n%s", bad, out)
		}
	}
	if !strings.Contains(out, "main.go") {
		t.Errorf("expected main.go in output")
	}
}

func TestScanTree_DepthCap(t *testing.T) {
	// Create a directory 4 levels deep — level 4 should not appear.
	root := makeRepo(t, map[string]string{
		"a/b/c/deep.go":  "package main\n",
		"a/b/c/d/too.go": "package main\n",
	})
	out, err := signals.ScanTree(root)
	if err != nil {
		t.Fatalf("ScanTree: %v", err)
	}
	// deep.go is at depth 3 — should be present.
	if !strings.Contains(out, "deep.go") {
		t.Errorf("expected deep.go (depth 3), not found:\n%s", out)
	}
	// too.go is at depth 4 — should be absent.
	if strings.Contains(out, "too.go") {
		t.Errorf("expected too.go (depth 4) to be excluded:\n%s", out)
	}
}

func TestScanTree_DirsBeforeFiles(t *testing.T) {
	root := makeRepo(t, map[string]string{
		"z_file.go":  "package main\n",
		"a_dir/f.go": "package main\n",
	})
	out, err := signals.ScanTree(root)
	if err != nil {
		t.Fatalf("ScanTree: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) < 2 {
		t.Fatalf("expected at least 2 lines, got: %v", lines)
	}
	// a_dir/ should appear before z_file.go.
	dirIdx, fileIdx := -1, -1
	for i, l := range lines {
		if strings.HasPrefix(strings.TrimSpace(l), "a_dir/") {
			dirIdx = i
		}
		if strings.Contains(l, "z_file.go") {
			fileIdx = i
		}
	}
	if dirIdx == -1 || fileIdx == -1 {
		t.Fatalf("couldn't find both entries: lines=%v", lines)
	}
	if dirIdx > fileIdx {
		t.Errorf("expected directory before file; dirIdx=%d fileIdx=%d", dirIdx, fileIdx)
	}
}

func TestScanTree_Sorted(t *testing.T) {
	root := makeRepo(t, map[string]string{
		"z.go": "package main\n",
		"a.go": "package main\n",
		"m.go": "package main\n",
	})
	out, err := signals.ScanTree(root)
	if err != nil {
		t.Fatalf("ScanTree: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d:\n%s", len(lines), out)
	}
	if lines[0] != "a.go" || lines[1] != "m.go" || lines[2] != "z.go" {
		t.Errorf("not sorted: %v", lines)
	}
}

// ---- Manifests scanner ----

func TestScanManifests_Empty(t *testing.T) {
	root := makeRepo(t, nil)
	out, err := signals.ScanManifests(root)
	if err != nil {
		t.Fatalf("ScanManifests: %v", err)
	}
	if out != "" {
		t.Errorf("expected empty manifests, got %q", out)
	}
}

func TestScanManifests_PackageJSON(t *testing.T) {
	root := makeRepo(t, map[string]string{
		"package.json": `{"name":"my-app","version":"1.2.3","scripts":{"build":"tsc","test":"jest","lint":"eslint"}}`,
	})
	out, err := signals.ScanManifests(root)
	if err != nil {
		t.Fatalf("ScanManifests: %v", err)
	}
	if !strings.Contains(out, "name=my-app") {
		t.Errorf("missing name: %s", out)
	}
	if !strings.Contains(out, "version=1.2.3") {
		t.Errorf("missing version: %s", out)
	}
	// Scripts should be sorted.
	if !strings.Contains(out, "scripts=[build, lint, test]") {
		t.Errorf("missing sorted scripts: %s", out)
	}
}

func TestScanManifests_GoMod(t *testing.T) {
	root := makeRepo(t, map[string]string{
		"go.mod": "module github.com/example/mymod\n\ngo 1.22\n\nrequire foo/bar v1.0.0\n",
	})
	out, err := signals.ScanManifests(root)
	if err != nil {
		t.Fatalf("ScanManifests: %v", err)
	}
	if !strings.Contains(out, "module=github.com/example/mymod") {
		t.Errorf("missing module: %s", out)
	}
	if !strings.Contains(out, "go=1.22") {
		t.Errorf("missing go version: %s", out)
	}
}

func TestScanManifests_CargoTOML(t *testing.T) {
	root := makeRepo(t, map[string]string{
		"Cargo.toml": "[package]\nname = \"myapp\"\nversion = \"0.5.0\"\nedition = \"2021\"\n",
	})
	out, err := signals.ScanManifests(root)
	if err != nil {
		t.Fatalf("ScanManifests: %v", err)
	}
	if !strings.Contains(out, "name=myapp") {
		t.Errorf("missing name: %s", out)
	}
	if !strings.Contains(out, "version=0.5.0") {
		t.Errorf("missing version: %s", out)
	}
}

func TestScanManifests_PyprojectProject(t *testing.T) {
	root := makeRepo(t, map[string]string{
		"pyproject.toml": "[project]\nname = \"mylib\"\nversion = \"2.1.0\"\n",
	})
	out, err := signals.ScanManifests(root)
	if err != nil {
		t.Fatalf("ScanManifests: %v", err)
	}
	if !strings.Contains(out, "name=mylib") {
		t.Errorf("missing name: %s", out)
	}
}

func TestScanManifests_PyprojectPoetryFallback(t *testing.T) {
	root := makeRepo(t, map[string]string{
		"pyproject.toml": "[tool.poetry]\nname = \"poetryapp\"\nversion = \"3.0.0\"\n",
	})
	out, err := signals.ScanManifests(root)
	if err != nil {
		t.Fatalf("ScanManifests: %v", err)
	}
	if !strings.Contains(out, "name=poetryapp") {
		t.Errorf("missing name: %s", out)
	}
}

func TestScanManifests_RequirementsTXT(t *testing.T) {
	root := makeRepo(t, map[string]string{
		"requirements.txt": "# comment\n\nrequests==2.28.0\nflask==2.2.0\nnumpy==1.24.0\n",
	})
	out, err := signals.ScanManifests(root)
	if err != nil {
		t.Fatalf("ScanManifests: %v", err)
	}
	if !strings.Contains(out, "packages=3") {
		t.Errorf("expected 3 packages: %s", out)
	}
}

func TestScanManifests_Gemfile(t *testing.T) {
	root := makeRepo(t, map[string]string{
		"Gemfile": "source 'https://rubygems.org'\ngem 'rails', '~> 7.0'\ngem 'pg'\n",
	})
	out, err := signals.ScanManifests(root)
	if err != nil {
		t.Fatalf("ScanManifests: %v", err)
	}
	if !strings.Contains(out, "gems=2") {
		t.Errorf("expected 2 gems: %s", out)
	}
}

func TestScanManifests_ComposerJSON(t *testing.T) {
	root := makeRepo(t, map[string]string{
		"composer.json": `{"name":"vendor/pkg","scripts":{"test":"phpunit","lint":"phpcs"}}`,
	})
	out, err := signals.ScanManifests(root)
	if err != nil {
		t.Fatalf("ScanManifests: %v", err)
	}
	if !strings.Contains(out, "name=vendor/pkg") {
		t.Errorf("missing name: %s", out)
	}
	if !strings.Contains(out, "scripts=[lint, test]") {
		t.Errorf("missing sorted scripts: %s", out)
	}
}

func TestScanManifests_PomXML(t *testing.T) {
	root := makeRepo(t, map[string]string{
		"pom.xml": `<project><artifactId>my-service</artifactId><version>1.0.0</version></project>`,
	})
	out, err := signals.ScanManifests(root)
	if err != nil {
		t.Fatalf("ScanManifests: %v", err)
	}
	if !strings.Contains(out, "artifactId=my-service") {
		t.Errorf("missing artifactId: %s", out)
	}
	if !strings.Contains(out, "version=1.0.0") {
		t.Errorf("missing version: %s", out)
	}
}

func TestScanManifests_SortedByFilename(t *testing.T) {
	root := makeRepo(t, map[string]string{
		"package.json": `{"name":"app"}`,
		"go.mod":       "module github.com/x/y\n\ngo 1.22\n",
		"Cargo.toml":   "[package]\nname = \"app\"\nversion = \"0.1.0\"\n",
	})
	out, err := signals.ScanManifests(root)
	if err != nil {
		t.Fatalf("ScanManifests: %v", err)
	}
	// Cargo.toml < go.mod < package.json alphabetically.
	cargoIdx := strings.Index(out, "Cargo.toml")
	goIdx := strings.Index(out, "go.mod")
	pkgIdx := strings.Index(out, "package.json")
	if cargoIdx == -1 || goIdx == -1 || pkgIdx == -1 {
		t.Fatalf("not all manifests found: %s", out)
	}
	if !(cargoIdx < goIdx && goIdx < pkgIdx) {
		t.Errorf("manifests not sorted alphabetically: %s", out)
	}
}

// ---- Languages scanner ----

func TestScanLanguages_Empty(t *testing.T) {
	root := makeRepo(t, nil)
	out, err := signals.ScanLanguages(root)
	if err != nil {
		t.Fatalf("ScanLanguages: %v", err)
	}
	if out != "" {
		t.Errorf("expected empty, got %q", out)
	}
}

func TestScanLanguages_SingleLanguage(t *testing.T) {
	root := makeRepo(t, map[string]string{
		"main.go": "package main\n\nfunc main() {}\n",
		"lib.go":  "package main\n\nfunc Lib() {}\n",
	})
	out, err := signals.ScanLanguages(root)
	if err != nil {
		t.Fatalf("ScanLanguages: %v", err)
	}
	if !strings.Contains(out, "Go:") {
		t.Errorf("expected Go in output: %s", out)
	}
	if !strings.Contains(out, "100%") {
		t.Errorf("expected 100%% for sole language: %s", out)
	}
}

func TestScanLanguages_MultipleLanguages(t *testing.T) {
	root := makeRepo(t, map[string]string{
		"main.go":   strings.Repeat("line\n", 100),
		"index.ts":  strings.Repeat("line\n", 200),
		"script.py": strings.Repeat("line\n", 50),
	})
	out, err := signals.ScanLanguages(root)
	if err != nil {
		t.Fatalf("ScanLanguages: %v", err)
	}
	// TypeScript should be first (most LOC).
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) == 0 || !strings.Contains(lines[0], "TypeScript") {
		t.Errorf("expected TypeScript first (most LOC): %v", lines)
	}
	if !strings.Contains(out, "Go:") {
		t.Errorf("expected Go: %s", out)
	}
	if !strings.Contains(out, "Python:") {
		t.Errorf("expected Python: %s", out)
	}
}

func TestScanLanguages_SkipDirs(t *testing.T) {
	root := makeRepo(t, map[string]string{
		"main.go":                   strings.Repeat("line\n", 10),
		"node_modules/dep/index.js": strings.Repeat("line\n", 10000),
	})
	out, err := signals.ScanLanguages(root)
	if err != nil {
		t.Fatalf("ScanLanguages: %v", err)
	}
	if !strings.Contains(out, "Go:") {
		t.Errorf("expected Go: %s", out)
	}
	// JavaScript from node_modules should not bloat the count.
	if strings.Contains(out, "JavaScript: 10000") {
		t.Errorf("node_modules JS should be excluded: %s", out)
	}
}

func TestScanLanguages_Top10Cap(t *testing.T) {
	// Create 12 different languages.
	files := map[string]string{}
	exts := []string{".go", ".ts", ".js", ".py", ".rs", ".rb", ".java", ".kt", ".swift", ".cs", ".c", ".cpp"}
	for i, ext := range exts {
		files["file"+ext] = strings.Repeat("line\n", (i+1)*10)
	}
	root := makeRepo(t, files)
	out, err := signals.ScanLanguages(root)
	if err != nil {
		t.Fatalf("ScanLanguages: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) > 10 {
		t.Errorf("expected at most 10 languages, got %d:\n%s", len(lines), out)
	}
}

// ---- Scan verb ----

func TestScan_CreatesFile(t *testing.T) {
	root := makeRepo(t, map[string]string{
		"main.go": "package main\n",
	})
	if err := signals.Scan(root); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	path := signals.SignalsPath(root)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("signals file not created: %v", err)
	}
}

func TestScan_ValidFrontmatter(t *testing.T) {
	root := makeRepo(t, map[string]string{
		"main.go": "package main\n",
	})
	if err := signals.Scan(root); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	data, _ := os.ReadFile(signals.SignalsPath(root))
	meta, body, err := frontmatter.Parse(string(data))
	if err != nil {
		t.Fatalf("parse frontmatter: %v", err)
	}
	if meta == nil {
		t.Fatal("expected frontmatter, got nil")
	}
	if meta["generated_at"] == "" {
		t.Error("missing generated_at")
	}
	if meta["atomic_version"] == "" {
		t.Error("missing atomic_version")
	}
	if !strings.Contains(body, "# Deterministic signals") {
		t.Errorf("body missing header: %s", body)
	}
}

func TestScan_Idempotent(t *testing.T) {
	root := makeRepo(t, map[string]string{
		"main.go": "package main\n",
	})
	if err := signals.Scan(root); err != nil {
		t.Fatalf("Scan first: %v", err)
	}
	first, _ := os.ReadFile(signals.SignalsPath(root))

	// Second scan on unchanged repo.
	if err := signals.Scan(root); err != nil {
		t.Fatalf("Scan second: %v", err)
	}
	second, _ := os.ReadFile(signals.SignalsPath(root))

	if string(first) != string(second) {
		t.Errorf("scan not idempotent:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
}

func TestScan_BacksUpPrevFile(t *testing.T) {
	root := makeRepo(t, map[string]string{
		"main.go": "package main\n",
	})
	if err := signals.Scan(root); err != nil {
		t.Fatalf("Scan first: %v", err)
	}

	// Modify a source file to force a rewrite.
	if err := os.WriteFile(filepath.Join(root, "new.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("write new.go: %v", err)
	}
	if err := signals.Scan(root); err != nil {
		t.Fatalf("Scan second: %v", err)
	}

	prevPath := signals.PrevPath(root)
	if _, err := os.Stat(prevPath); err != nil {
		t.Fatalf("prev file not created: %v", err)
	}
}

// ---- Show verb ----

func TestShow_PrintsFile(t *testing.T) {
	root := makeRepo(t, map[string]string{
		"main.go": "package main\n",
	})
	if err := signals.Scan(root); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	// Redirect stdout.
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := signals.Show(root)

	w.Close()
	os.Stdout = old

	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	output := string(buf[:n])

	if err != nil {
		t.Fatalf("Show: %v", err)
	}
	if !strings.Contains(output, "# Deterministic signals") {
		t.Errorf("Show output missing header: %s", output)
	}
}

func TestShow_MissingFile(t *testing.T) {
	root := makeRepo(t, nil)
	err := signals.Show(root)
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' in error: %v", err)
	}
}

// ---- Stale verb ----

func TestStale_FreshAfterScan(t *testing.T) {
	root := makeRepo(t, map[string]string{
		"main.go": "package main\n",
	})
	if err := signals.Scan(root); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	// Signals file was just written — should not be stale.
	if err := signals.Stale(root); err != nil {
		t.Errorf("expected fresh (nil), got: %v", err)
	}
}

func TestStale_StaleAfterSourceChange(t *testing.T) {
	root := makeRepo(t, map[string]string{
		"main.go": "package main\n",
	})
	if err := signals.Scan(root); err != nil {
		t.Fatalf("Scan: %v", err)
	}

	// Force the signals file's mtime to be slightly older.
	signalsPath := signals.SignalsPath(root)
	past := time.Now().Add(-2 * time.Second)
	if err := os.Chtimes(signalsPath, past, past); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	// Now touch a source file.
	srcPath := filepath.Join(root, "main.go")
	if err := os.WriteFile(srcPath, []byte("package main\n// updated\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	if err := signals.Stale(root); err != signals.ErrStale {
		t.Errorf("expected ErrStale, got: %v", err)
	}
}

func TestStale_MissingFile(t *testing.T) {
	root := makeRepo(t, nil)
	err := signals.Stale(root)
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' in error: %v", err)
	}
}

// ---- Diff verb ----

func TestDiff_NoPriorFallback(t *testing.T) {
	root := makeRepo(t, map[string]string{
		"main.go": "package main\n",
	})
	if err := signals.Scan(root); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	// No prev file exists, and this is not a git repo → should get ErrNoPrior.
	err := signals.Diff(root)
	if err != signals.ErrNoPrior {
		t.Errorf("expected ErrNoPrior, got: %v", err)
	}
}

func TestDiff_GitNoDiff(t *testing.T) {
	root := makeRepo(t, map[string]string{
		"main.go": "package main\n",
	})
	initGit(t, root)

	if err := signals.Scan(root); err != nil {
		t.Fatalf("Scan: %v", err)
	}

	// Stage the signals file so git diff shows nothing.
	exec.Command("git", "-C", root, "add", ".").Run()
	exec.Command("git", "-C", root, "commit", "-m", "initial", "--allow-empty-message").Run()

	err := signals.Diff(root)
	if err != nil {
		t.Errorf("expected nil (no diff), got: %v", err)
	}
}

func TestDiff_GitDiffPresent(t *testing.T) {
	root := makeRepo(t, map[string]string{
		"main.go": "package main\n",
	})
	initGit(t, root)

	if err := signals.Scan(root); err != nil {
		t.Fatalf("Scan first: %v", err)
	}

	// Stage initial scan.
	exec.Command("git", "-C", root, "add", ".").Run()
	exec.Command("git", "-C", root, "commit", "-m", "initial").Run()

	// Add a file and rescan — signals file changes.
	os.WriteFile(filepath.Join(root, "new.go"), []byte("package main\n"), 0o644)
	if err := signals.Scan(root); err != nil {
		t.Fatalf("Scan second: %v", err)
	}

	// Stage the new source but not the signals file — git diff should show the change.
	err := signals.Diff(root)
	if err != signals.ErrDiffPresent {
		t.Errorf("expected ErrDiffPresent, got: %v", err)
	}
}

func TestDiff_MissingSignalsFile(t *testing.T) {
	root := makeRepo(t, nil)
	err := signals.Diff(root)
	if err == nil {
		t.Fatal("expected error for missing signals file")
	}
}

// ---- Golden tests ----

func TestGolden_EmptyRepo(t *testing.T) {
	root := makeRepo(t, nil)
	if err := signals.Scan(root); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	data, err := os.ReadFile(signals.SignalsPath(root))
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	// Normalize: strip generated_at line for comparison.
	normalized := normalizeGeneratedAt(string(data))

	// For empty repo: tree/manifests/languages sections should all be empty/present.
	if !strings.Contains(normalized, "## Tree") {
		t.Error("missing ## Tree section")
	}
	if !strings.Contains(normalized, "## Manifests") {
		t.Error("missing ## Manifests section")
	}
	if !strings.Contains(normalized, "## Languages") {
		t.Error("missing ## Languages section")
	}
}

func TestGolden_Multilang(t *testing.T) {
	root := makeRepo(t, map[string]string{
		"main.go":   strings.Repeat("go\n", 50),
		"index.ts":  strings.Repeat("ts\n", 100),
		"script.py": strings.Repeat("py\n", 30),
		"go.mod":    "module github.com/example/test\n\ngo 1.22\n",
	})
	if err := signals.Scan(root); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	data, _ := os.ReadFile(signals.SignalsPath(root))
	normalized := normalizeGeneratedAt(string(data))

	// TypeScript should be listed first (most LOC).
	tsIdx := strings.Index(normalized, "TypeScript")
	goIdx := strings.Index(normalized, "Go:")
	pyIdx := strings.Index(normalized, "Python")

	if tsIdx == -1 || goIdx == -1 || pyIdx == -1 {
		t.Fatalf("missing language entries:\n%s", normalized)
	}
	if !(tsIdx < goIdx && goIdx < pyIdx) {
		t.Errorf("language order wrong (expected TS > Go > Py by LOC):\n%s", normalized)
	}

	// go.mod should appear in manifests.
	if !strings.Contains(normalized, "go.mod") {
		t.Error("go.mod missing from manifests")
	}
}

// normalizeGeneratedAt replaces the generated_at value with a placeholder.
func normalizeGeneratedAt(s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		if strings.HasPrefix(strings.TrimSpace(l), "generated_at:") {
			lines[i] = "generated_at: NORMALIZED"
		}
	}
	return strings.Join(lines, "\n")
}
