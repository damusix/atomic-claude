package signals_test

import (
	"bytes"
	"flag"
	"fmt"
	"io"
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
	// a_dir/ should appear before z_file.go (tree glyphs prefix each line).
	dirIdx, fileIdx := -1, -1
	for i, l := range lines {
		if strings.Contains(l, "a_dir/") {
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
	// Lines have tree glyphs: "├── a.go", "├── m.go", "└── z.go".
	if !strings.Contains(lines[0], "a.go") || !strings.Contains(lines[1], "m.go") || !strings.Contains(lines[2], "z.go") {
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
	// Capture the first scan's output — the prev file should contain these bytes.
	firstOutput, err := os.ReadFile(signals.SignalsPath(root))
	if err != nil {
		t.Fatalf("read first output: %v", err)
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
	// Content of the prev file must equal the first scan's output.
	prevContent, err := os.ReadFile(prevPath)
	if err != nil {
		t.Fatalf("read prev file: %v", err)
	}
	if string(prevContent) != string(firstOutput) {
		t.Errorf("prev file content mismatch:\nwant: %s\ngot:  %s", firstOutput, prevContent)
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
	err := signals.Diff(root, io.Discard)
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

	err := signals.Diff(root, io.Discard)
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

	// Directly modify the signals file to add a known marker line.
	// This forces a diff that contains a '+' line with that marker.
	const marker = "# test-diff-marker-line"
	sigPath := signals.SignalsPath(root)
	existing, err := os.ReadFile(sigPath)
	if err != nil {
		t.Fatalf("read signals file: %v", err)
	}
	modified := string(existing) + marker + "\n"
	if err := os.WriteFile(sigPath, []byte(modified), 0o644); err != nil {
		t.Fatalf("modify signals file: %v", err)
	}

	// git diff should show the added marker line.
	var out bytes.Buffer
	err = signals.Diff(root, &out)
	if err != signals.ErrDiffPresent {
		t.Errorf("expected ErrDiffPresent, got: %v", err)
	}
	if out.Len() == 0 {
		t.Error("expected non-empty diff output on stdout")
	}
	// The diff body must contain a '+' line with the marker we added.
	diffStr := out.String()
	if !strings.Contains(diffStr, "+"+marker) {
		t.Errorf("diff output does not contain expected '+' line with marker %q:\n%s", marker, diffStr)
	}
}

func TestDiff_MissingSignalsFile(t *testing.T) {
	root := makeRepo(t, nil)
	err := signals.Diff(root, io.Discard)
	if err == nil {
		t.Fatal("expected error for missing signals file")
	}
}

// ---- Golden tests (testdata/signals/<scenario>/ fixture layout) ----
//
// Run with -update to regenerate expected.md fixtures.

var update = flag.Bool("update", false, "regenerate golden testdata fixtures")

// fixedClock returns a deterministic time for golden tests.
// All golden fixtures contain this exact timestamp as generated_at.
var fixedClockTime = time.Date(2026, 5, 16, 18, 32, 11, 0, time.UTC)

func fixedClockFn() time.Time { return fixedClockTime }

// goldenTest scans a temp repo seeded from testdata/signals/<scenario>/repo/,
// uses a fixed clock for generated_at, and compares against
// testdata/signals/<scenario>/expected.md.
// With -update, it writes the expected file instead of comparing.
func goldenTest(t *testing.T, scenario string) {
	t.Helper()

	// Seed the temp repo from testdata fixture.
	srcRepo := filepath.Join("testdata", "signals", scenario, "repo")

	// repo/ must exist. An intentionally-empty repo must contain a .keep marker.
	repoInfo, err := os.Stat(srcRepo)
	if err != nil {
		t.Fatalf("goldenTest: scenario %q is missing repo/ directory (expected at %s)", scenario, srcRepo)
	}
	if !repoInfo.IsDir() {
		t.Fatalf("goldenTest: %s is not a directory", srcRepo)
	}

	root := t.TempDir()

	// Copy all files from the scenario repo into the temp dir.
	entries, readErr := os.ReadDir(srcRepo)
	if readErr != nil {
		t.Fatalf("read repo/: %v", readErr)
	}
	// Allow an empty repo/ only if it contains a .keep marker.
	hasKeep := false
	for _, e := range entries {
		if e.Name() == ".keep" {
			hasKeep = true
		}
	}
	if len(entries) == 0 {
		t.Fatalf("goldenTest: repo/ is empty with no .keep marker for scenario %q", scenario)
	}
	if len(entries) == 1 && hasKeep {
		// Intentionally empty repo — don't copy .keep into the scan dir.
	} else {
		if err := copyDir(t, srcRepo, root); err != nil {
			t.Fatalf("copy fixture: %v", err)
		}
	}

	opts := &signals.Options{Clock: fixedClockFn}
	if err := signals.ScanWithOptions(root, opts); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	data, err := os.ReadFile(signals.SignalsPath(root))
	if err != nil {
		t.Fatalf("read signals file: %v", err)
	}

	got := string(data)

	expectedPath := filepath.Join("testdata", "signals", scenario, "expected.md")
	if *update {
		if err := os.MkdirAll(filepath.Dir(expectedPath), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(expectedPath, []byte(got), 0o644); err != nil {
			t.Fatalf("write expected: %v", err)
		}
		t.Logf("updated %s", expectedPath)
		return
	}

	expected, err := os.ReadFile(expectedPath)
	if err != nil {
		t.Fatalf("read expected file %s: %v (run with -update to generate)", expectedPath, err)
	}
	if got != string(expected) {
		t.Errorf("output mismatch for scenario %q:\n%s", scenario, diffSnippet(string(expected), got))
	}
}

// diffSnippet produces a minimal unified-diff-style snippet for two strings.
// Only changed lines are shown (no context lines). No external dependency.
func diffSnippet(want, got string) string {
	wantLines := strings.Split(want, "\n")
	gotLines := strings.Split(got, "\n")

	var sb strings.Builder
	sb.WriteString("--- want\n+++ got\n")

	max := len(wantLines)
	if len(gotLines) > max {
		max = len(gotLines)
	}

	for i := 0; i < max; i++ {
		var w, g string
		if i < len(wantLines) {
			w = wantLines[i]
		}
		if i < len(gotLines) {
			g = gotLines[i]
		}
		if w == g {
			continue
		}
		if i < len(wantLines) {
			fmt.Fprintf(&sb, "-%s\n", w)
		}
		if i < len(gotLines) {
			fmt.Fprintf(&sb, "+%s\n", g)
		}
	}
	return sb.String()
}

// copyDir recursively copies src into dst.
func copyDir(t *testing.T, src, dst string) error {
	t.Helper()
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}

func TestGolden_EmptyRepo(t *testing.T) {
	goldenTest(t, "empty-repo")
}

func TestGolden_Multilang(t *testing.T) {
	goldenTest(t, "multilang")
}

// ---- U1: Tree glyph rendering ----

func TestScanTree_Glyphs(t *testing.T) {
	root := makeRepo(t, map[string]string{
		"a.go":       "package main\n",
		"b.go":       "package main\n",
		"pkg/lib.go": "package pkg\n",
	})
	out, err := signals.ScanTree(root)
	if err != nil {
		t.Fatalf("ScanTree: %v", err)
	}
	// Output must contain tree branch glyphs.
	if !strings.Contains(out, "├──") {
		t.Errorf("expected ├── glyph in output:\n%s", out)
	}
	if !strings.Contains(out, "└──") {
		t.Errorf("expected └── glyph in output:\n%s", out)
	}
	// pkg/ is a dir and should appear before files; its child uses │   prefix.
	if !strings.Contains(out, "pkg/") {
		t.Errorf("expected pkg/ in output:\n%s", out)
	}
	// Continuation prefix for non-last dir.
	lines := strings.Split(out, "\n")
	foundContinuation := false
	for _, l := range lines {
		if strings.Contains(l, "│") {
			foundContinuation = true
			break
		}
	}
	if !foundContinuation {
		t.Errorf("expected │ continuation prefix in output:\n%s", out)
	}
}

func TestScanTree_LastEntryGlyph(t *testing.T) {
	root := makeRepo(t, map[string]string{
		"only.go": "package main\n",
	})
	out, err := signals.ScanTree(root)
	if err != nil {
		t.Fatalf("ScanTree: %v", err)
	}
	// Single entry is always the last: must use └──.
	if !strings.Contains(out, "└──") {
		t.Errorf("single entry should use └──:\n%s", out)
	}
	if strings.Contains(out, "├──") {
		t.Errorf("single entry should not use ├──:\n%s", out)
	}
}

// ---- U2: Git-backed enumeration shows dotfile dirs ----

func TestEnumerateFiles_GitShowsDotfiles(t *testing.T) {
	root := makeRepo(t, map[string]string{
		"main.go":                  "package main\n",
		".github/workflows/ci.yml": "on: push\n",
		".claude/rules/ts.md":      "# ts rules\n",
	})
	initGit(t, root)
	exec.Command("git", "-C", root, "add", ".").Run()
	exec.Command("git", "-C", root, "commit", "-m", "init").Run()

	out, err := signals.ScanTree(root)
	if err != nil {
		t.Fatalf("ScanTree: %v", err)
	}
	if !strings.Contains(out, ".github") {
		t.Errorf("expected .github in tree (git-tracked):\n%s", out)
	}
	if !strings.Contains(out, ".claude") {
		t.Errorf("expected .claude in tree (git-tracked):\n%s", out)
	}
}

func TestEnumerateFiles_GitExcludesScratchpad(t *testing.T) {
	root := makeRepo(t, map[string]string{
		"main.go": "package main\n",
		".claude/.scratchpad/2026-01-01-task/BRIEF.md": "# brief\n",
		".claude/rules/ts.md":                          "# ts\n",
	})
	initGit(t, root)
	exec.Command("git", "-C", root, "add", ".").Run()
	exec.Command("git", "-C", root, "commit", "-m", "init").Run()

	out, err := signals.ScanTree(root)
	if err != nil {
		t.Fatalf("ScanTree: %v", err)
	}
	if strings.Contains(out, ".scratchpad") {
		t.Errorf("expected .scratchpad to be excluded:\n%s", out)
	}
	if !strings.Contains(out, ".claude") {
		t.Errorf("expected .claude to appear (has rules/):\n%s", out)
	}
}

// ---- U3: Languages file count format ----

func TestScanLanguages_FileCountFormat(t *testing.T) {
	root := makeRepo(t, map[string]string{
		"a.go": "package main\n\nfunc A() {}\n",
		"b.go": "package main\n\nfunc B() {}\n",
		"c.go": "package main\n\nfunc C() {}\n",
	})
	out, err := signals.ScanLanguages(root)
	if err != nil {
		t.Fatalf("ScanLanguages: %v", err)
	}
	// Must contain "files" column.
	if !strings.Contains(out, "files") {
		t.Errorf("expected 'files' in languages output: %s", out)
	}
	// Format: "- Go: N LOC (P%), M files (Q%)".
	if !strings.Contains(out, "3 files") {
		t.Errorf("expected '3 files' in output: %s", out)
	}
}

// ---- F-1: EmitOrdered preserves caller-specified key order ----

func TestEmitOrdered_PreservesOrder(t *testing.T) {
	// Order: generated_at before atomic_version (spec example order).
	out, err := frontmatter.EmitOrdered([]frontmatter.KV{
		{Key: "generated_at", Value: "2026-05-17T00:00:00Z"},
		{Key: "atomic_version", Value: "v0.1.0"},
	}, "body\n")
	if err != nil {
		t.Fatalf("EmitOrdered: %v", err)
	}
	gaIdx := strings.Index(out, "generated_at:")
	avIdx := strings.Index(out, "atomic_version:")
	if gaIdx == -1 || avIdx == -1 {
		t.Fatalf("missing keys in output:\n%s", out)
	}
	if gaIdx > avIdx {
		t.Errorf("generated_at should appear before atomic_version:\n%s", out)
	}
}

func TestEmitOrdered_Stable(t *testing.T) {
	kvs := []frontmatter.KV{
		{Key: "z_key", Value: "first"},
		{Key: "a_key", Value: "second"},
	}
	out1, err := frontmatter.EmitOrdered(kvs, "body\n")
	if err != nil {
		t.Fatalf("EmitOrdered: %v", err)
	}
	out2, err := frontmatter.EmitOrdered(kvs, "body\n")
	if err != nil {
		t.Fatalf("EmitOrdered: %v", err)
	}
	if out1 != out2 {
		t.Errorf("EmitOrdered not stable: %q vs %q", out1, out2)
	}
	// z_key should appear before a_key (caller-specified order, not alphabetical).
	zIdx := strings.Index(out1, "z_key:")
	aIdx := strings.Index(out1, "a_key:")
	if zIdx > aIdx {
		t.Errorf("caller order not preserved: z_key=%d a_key=%d in:\n%s", zIdx, aIdx, out1)
	}
}

func TestScan_FrontmatterOrder(t *testing.T) {
	root := makeRepo(t, map[string]string{
		"main.go": "package main\n",
	})
	if err := signals.Scan(root); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	data, _ := os.ReadFile(signals.SignalsPath(root))
	content := string(data)
	// generated_at must appear before atomic_version in the file.
	gaIdx := strings.Index(content, "generated_at:")
	avIdx := strings.Index(content, "atomic_version:")
	if gaIdx == -1 || avIdx == -1 {
		t.Fatalf("missing frontmatter keys:\n%s", content)
	}
	if gaIdx > avIdx {
		t.Errorf("expected generated_at before atomic_version:\n%s", content)
	}
}

// ---- F-4: parseComposerJSON handles array-valued scripts ----

func TestScanManifests_ComposerArrayScripts(t *testing.T) {
	root := makeRepo(t, map[string]string{
		// scripts.post-install is an array — this caused json.Unmarshal to fail
		// when the field was typed as map[string]string.
		"composer.json": `{
			"name": "vendor/myapp",
			"scripts": {
				"test": "phpunit",
				"post-install-cmd": ["@auto-scripts"]
			}
		}`,
	})
	out, err := signals.ScanManifests(root)
	if err != nil {
		t.Fatalf("ScanManifests: %v", err)
	}
	if out == "" {
		t.Fatal("expected non-empty output (composer.json should not be skipped)")
	}
	if !strings.Contains(out, "name=vendor/myapp") {
		t.Errorf("missing name: %s", out)
	}
	// Both script keys should appear sorted.
	if !strings.Contains(out, "post-install-cmd") {
		t.Errorf("missing post-install-cmd: %s", out)
	}
	if !strings.Contains(out, "test") {
		t.Errorf("missing test script: %s", out)
	}
}

// ---- Tree directory annotations ----

func TestScanTree_NormalDirChildCount(t *testing.T) {
	// pkg/ has 2 children (lib.go, util.go) — should show (2).
	root := makeRepo(t, map[string]string{
		"pkg/lib.go":  "package pkg\n",
		"pkg/util.go": "package pkg\n",
		"main.go":     "package main\n",
	})
	out, err := signals.ScanTree(root)
	if err != nil {
		t.Fatalf("ScanTree: %v", err)
	}
	// Directory line must end with " (2)".
	if !strings.Contains(out, "pkg/ (2)") {
		t.Errorf("expected 'pkg/ (2)' in tree:\n%s", out)
	}
	// Files must NOT be annotated.
	if strings.Contains(out, "main.go (") {
		t.Errorf("file main.go should not be annotated:\n%s", out)
	}
}

func TestScanTree_NormalDirChildCountSingular(t *testing.T) {
	// sub/ has exactly 1 child — should show (1).
	root := makeRepo(t, map[string]string{
		"sub/only.go": "package sub\n",
	})
	out, err := signals.ScanTree(root)
	if err != nil {
		t.Fatalf("ScanTree: %v", err)
	}
	if !strings.Contains(out, "sub/ (1)") {
		t.Errorf("expected 'sub/ (1)' in tree:\n%s", out)
	}
}

func TestScanTree_DepthCapAnnotation(t *testing.T) {
	// a/b/c/ is at depth 3; d/ inside it is depth 4 — c/ should be depth-capped.
	// c/ has 2 direct children: deep.go (file) and d/ (dir) = 2 direct.
	// Total beneath c/: deep.go + d/too.go = 2 total.
	root := makeRepo(t, map[string]string{
		"a/b/c/deep.go":  "package main\n",
		"a/b/c/d/too.go": "package main\n",
	})
	out, err := signals.ScanTree(root)
	if err != nil {
		t.Fatalf("ScanTree: %v", err)
	}
	// c/ should have depth-cap annotation with correct counts.
	if !strings.Contains(out, "c/ (2 subitems) (2 total items)") {
		t.Errorf("expected depth-cap annotation on c/:\n%s", out)
	}
	// too.go must not appear (depth 4).
	if strings.Contains(out, "too.go") {
		t.Errorf("too.go should be pruned:\n%s", out)
	}
}

func TestScanTree_DepthCapAnnotationSingularSubitem(t *testing.T) {
	// c/ has only 1 direct child (dir d/) — should say "1 subitem".
	root := makeRepo(t, map[string]string{
		"a/b/c/d/only.go": "package main\n",
	})
	out, err := signals.ScanTree(root)
	if err != nil {
		t.Fatalf("ScanTree: %v", err)
	}
	if !strings.Contains(out, "c/ (1 subitem)") {
		t.Errorf("expected '1 subitem' (singular) annotation on c/:\n%s", out)
	}
}

func TestScanTree_AnnotationIdempotent(t *testing.T) {
	// Two consecutive ScanTree calls on the same directory produce identical output.
	root := makeRepo(t, map[string]string{
		"pkg/a.go":     "package pkg\n",
		"pkg/b.go":     "package pkg\n",
		"main.go":      "package main\n",
		"a/b/c/d/x.go": "package main\n",
	})
	out1, err := signals.ScanTree(root)
	if err != nil {
		t.Fatalf("ScanTree first: %v", err)
	}
	out2, err := signals.ScanTree(root)
	if err != nil {
		t.Fatalf("ScanTree second: %v", err)
	}
	if out1 != out2 {
		t.Errorf("ScanTree not idempotent:\nfirst:\n%s\nsecond:\n%s", out1, out2)
	}
}
