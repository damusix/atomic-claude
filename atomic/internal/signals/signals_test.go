package signals_test

import (
	"bytes"
	"crypto/sha256"
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

func TestScanLanguages_Stylesheets(t *testing.T) {
	root := makeRepo(t, map[string]string{
		"a.css":  strings.Repeat("line\n", 10),
		"b.scss": strings.Repeat("line\n", 10),
		"c.sass": strings.Repeat("line\n", 10),
		"d.less": strings.Repeat("line\n", 10),
		"e.styl": strings.Repeat("line\n", 10),
	})
	out, err := signals.ScanLanguages(root)
	if err != nil {
		t.Fatalf("ScanLanguages: %v", err)
	}
	for _, lang := range []string{"CSS:", "SCSS:", "Sass:", "Less:", "Stylus:"} {
		if !strings.Contains(out, lang) {
			t.Errorf("expected %s in output: %s", lang, out)
		}
	}
}

func TestScanLanguages_WebFrameworks(t *testing.T) {
	root := makeRepo(t, map[string]string{
		"page.html": strings.Repeat("line\n", 10),
		"app.vue":   strings.Repeat("line\n", 10),
		"x.svelte":  strings.Repeat("line\n", 10),
		"y.astro":   strings.Repeat("line\n", 10),
		"esm.mjs":   strings.Repeat("line\n", 10),
		"cjs.cjs":   strings.Repeat("line\n", 10),
		"doc.mdx":   strings.Repeat("line\n", 10),
	})
	out, err := signals.ScanLanguages(root)
	if err != nil {
		t.Fatalf("ScanLanguages: %v", err)
	}
	for _, lang := range []string{"HTML:", "Vue:", "Svelte:", "Astro:", "JavaScript:", "MDX:"} {
		if !strings.Contains(out, lang) {
			t.Errorf("expected %s in output: %s", lang, out)
		}
	}
}

func TestScanLanguages_Templates(t *testing.T) {
	root := makeRepo(t, map[string]string{
		"a.hbs":        strings.Repeat("line\n", 10),
		"b.handlebars": strings.Repeat("line\n", 10),
		"c.ejs":        strings.Repeat("line\n", 10),
		"d.pug":        strings.Repeat("line\n", 10),
		"e.liquid":     strings.Repeat("line\n", 10),
		"f.erb":        strings.Repeat("line\n", 10),
		"g.twig":       strings.Repeat("line\n", 10),
	})
	out, err := signals.ScanLanguages(root)
	if err != nil {
		t.Fatalf("ScanLanguages: %v", err)
	}
	for _, lang := range []string{"Handlebars:", "EJS:", "Pug:", "Liquid:", "ERB:", "Twig:"} {
		if !strings.Contains(out, lang) {
			t.Errorf("expected %s in output: %s", lang, out)
		}
	}
}

func TestScanLanguages_OtherWeb(t *testing.T) {
	root := makeRepo(t, map[string]string{
		"a.coffee":  strings.Repeat("line\n", 10),
		"b.graphql": strings.Repeat("line\n", 10),
		"c.gql":     strings.Repeat("line\n", 10),
		"d.dart":    strings.Repeat("line\n", 10),
		"e.sol":     strings.Repeat("line\n", 10),
		"f.elm":     strings.Repeat("line\n", 10),
	})
	out, err := signals.ScanLanguages(root)
	if err != nil {
		t.Fatalf("ScanLanguages: %v", err)
	}
	for _, lang := range []string{"CoffeeScript:", "GraphQL:", "Dart:", "Solidity:", "Elm:"} {
		if !strings.Contains(out, lang) {
			t.Errorf("expected %s in output: %s", lang, out)
		}
	}
}

func TestScanLanguages_ConfigData(t *testing.T) {
	root := makeRepo(t, map[string]string{
		"a.json": strings.Repeat("line\n", 10),
		"b.yml":  strings.Repeat("line\n", 10),
		"c.yaml": strings.Repeat("line\n", 10),
		"d.toml": strings.Repeat("line\n", 10),
		"e.xml":  strings.Repeat("line\n", 10),
	})
	out, err := signals.ScanLanguages(root)
	if err != nil {
		t.Fatalf("ScanLanguages: %v", err)
	}
	for _, lang := range []string{"JSON:", "YAML:", "TOML:", "XML:"} {
		if !strings.Contains(out, lang) {
			t.Errorf("expected %s in output: %s", lang, out)
		}
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
	if _, err := signals.Stale(root); err != nil {
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

	// Add a new source file. This changes the deterministic body (file count,
	// tree listing, language LOC) — so a fresh scan would differ from stored.
	if err := os.WriteFile(filepath.Join(root, "helper.go"), []byte("package main\n\nfunc helper() {}\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	info, err := signals.Stale(root)
	if err != signals.ErrStale {
		t.Errorf("expected ErrStale, got: %v", err)
	}
	// Evidence drives the imperative CLI output: the orchestrator sees a
	// concrete magnitude of drift rather than a bare exit code it can dismiss.
	if info.ChangedLines < 1 {
		t.Errorf("expected changed-line evidence > 0, got ChangedLines=%d", info.ChangedLines)
	}
}

// TestStale_FreshAfterIdempotentRewrite is the regression test for the
// commit-time-regeneration treadmill: a generated file (e.g. manifest.go) is
// rewritten with identical bytes at commit time, bumping its mtime without
// changing what a scan would produce. A pure-mtime staleness check called this
// stale forever; the content-based check must call it fresh.
func TestStale_FreshAfterIdempotentRewrite(t *testing.T) {
	root := makeRepo(t, map[string]string{
		"main.go": "package main\n",
	})
	if err := signals.Scan(root); err != nil {
		t.Fatalf("Scan: %v", err)
	}

	// Make the signals file older so any mtime-based check would call it stale.
	signalsPath := signals.SignalsPath(root)
	past := time.Now().Add(-2 * time.Second)
	if err := os.Chtimes(signalsPath, past, past); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	// Rewrite a source file with byte-identical content — newer mtime, same bytes.
	srcPath := filepath.Join(root, "main.go")
	if err := os.WriteFile(srcPath, []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	if _, err := signals.Stale(root); err != nil {
		t.Errorf("expected fresh (content unchanged despite newer mtime), got: %v", err)
	}
}

func TestStale_MissingFile(t *testing.T) {
	root := makeRepo(t, nil)
	_, err := signals.Stale(root)
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
	// Files carry metadata annotations — main.go should have a "(" annotation.
	if !strings.Contains(out, "main.go (") {
		t.Errorf("file main.go should have metadata annotation:\n%s", out)
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
	// a/b/c/ is at depth 3; d/ inside it would be depth 4 (max_depth+1).
	// c/ shows all its children (d/ + deep.go) — uses simple (N) annotation.
	// d/ carries the new (N files, M dirs) summary annotation.
	root := makeRepo(t, map[string]string{
		"a/b/c/deep.go":  "package main\n",
		"a/b/c/d/too.go": "package main\n",
	})
	out, err := signals.ScanTree(root)
	if err != nil {
		t.Fatalf("ScanTree: %v", err)
	}
	// c/ shows all its children — simple annotation.
	if !strings.Contains(out, "c/ (2)") {
		t.Errorf("expected simple annotation 'c/ (2)' (all children shown):\n%s", out)
	}
	// d/ is the depth-cap dir with (N files, M dirs) annotation.
	if !strings.Contains(out, "d/ (1 file, 0 dirs)") {
		t.Errorf("expected 'd/ (1 file, 0 dirs)' summary annotation:\n%s", out)
	}
	// too.go must not appear (depth 4).
	if strings.Contains(out, "too.go") {
		t.Errorf("too.go should be pruned:\n%s", out)
	}
}

func TestScanTree_DepthCapAnnotationSingularSubitem(t *testing.T) {
	// c/ has only 1 direct child (dir d/) which is at depth 4 (max_depth+1).
	// c/ shows d/ → simple annotation (1). d/ carries the new (N files, M dirs) summary.
	root := makeRepo(t, map[string]string{
		"a/b/c/d/only.go": "package main\n",
	})
	out, err := signals.ScanTree(root)
	if err != nil {
		t.Fatalf("ScanTree: %v", err)
	}
	// c/ shows its one child (d/) — simple annotation.
	if !strings.Contains(out, "c/ (1)") {
		t.Errorf("expected 'c/ (1)' (simple annotation, child shown):\n%s", out)
	}
	// d/ is the depth-cap dir with (1 file, 0 dirs) summary.
	if !strings.Contains(out, "d/ (1 file, 0 dirs)") {
		t.Errorf("expected 'd/ (1 file, 0 dirs)' summary annotation:\n%s", out)
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

// ---- Fix 1: Languages singular/plural file count ----

func TestScanLanguages_FileCountSingular(t *testing.T) {
	// Exactly 1 file — must render "1 file" not "1 files".
	root := makeRepo(t, map[string]string{
		"main.go": "package main\n\nfunc main() {}\n",
	})
	out, err := signals.ScanLanguages(root)
	if err != nil {
		t.Fatalf("ScanLanguages: %v", err)
	}
	if strings.Contains(out, "1 files") {
		t.Errorf("expected '1 file' (singular), got '1 files':\n%s", out)
	}
	if !strings.Contains(out, "1 file") {
		t.Errorf("expected '1 file' in output:\n%s", out)
	}
}

// ---- Fix 2: Total items singular ----

func TestScanTree_DepthCapAnnotationSingularTotalItem(t *testing.T) {
	// c/ has 1 direct child (dir d/) which is at depth 4 (max_depth+1).
	// d/ shows (1 file, 0 dirs) summary. Verify no stale "total items" text.
	root := makeRepo(t, map[string]string{
		"a/b/c/d/only.go": "package main\n",
	})
	out, err := signals.ScanTree(root)
	if err != nil {
		t.Fatalf("ScanTree: %v", err)
	}
	// Old format should not appear.
	if strings.Contains(out, "total items") || strings.Contains(out, "total item") {
		t.Errorf("old 'total item(s)' annotation should not appear:\n%s", out)
	}
	// New format: d/ shows file/dir counts.
	if !strings.Contains(out, "d/ (1 file, 0 dirs)") {
		t.Errorf("expected 'd/ (1 file, 0 dirs)' summary:\n%s", out)
	}
}

// ---- Fix 3: Directory entries visible at depth cap ----

func TestScanTree_DepthCapShowsDirEntry(t *testing.T) {
	// a/b/c/ is at depth 3 — it has two children:
	//   - deep.go (file, shown with metadata)
	//   - d/ (dir at depth 4 = max_depth+1, shown as summary)
	root := makeRepo(t, map[string]string{
		"a/b/c/deep.go":  "package main\n",
		"a/b/c/d/too.go": "package main\n",
	})
	out, err := signals.ScanTree(root)
	if err != nil {
		t.Fatalf("ScanTree: %v", err)
	}
	// d/ must appear (depth 4 = max_depth+1).
	if !strings.Contains(out, "d/") {
		t.Errorf("expected 'd/' (depth-cap dir entry) to be visible:\n%s", out)
	}
	// d/ must carry the new (N files, M dirs) annotation.
	if !strings.Contains(out, "d/ (1 file, 0 dirs)") {
		t.Errorf("expected 'd/ (1 file, 0 dirs)' annotation:\n%s", out)
	}
	// too.go must NOT appear (it's a file at depth 4).
	if strings.Contains(out, "too.go") {
		t.Errorf("too.go should be pruned (depth 4):\n%s", out)
	}
	// c/ shows all children — must use simple (N) annotation, not (N files, M dirs) summary.
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "c/") && strings.Contains(l, "file") {
			t.Errorf("c/ should use simple annotation, not file/dir summary; got: %q\nfull output:\n%s", l, out)
		}
	}
	if !strings.Contains(out, "c/ (2)") {
		t.Errorf("expected 'c/ (2)' (simple annotation):\n%s", out)
	}
}

func TestScanTree_DepthCapParentAnnotation(t *testing.T) {
	// When the parent now shows all its children (including the dir),
	// it should carry the simple (N) annotation, not (N subitems)(M total items).
	root := makeRepo(t, map[string]string{
		"a/b/c/deep.go":  "package main\n",
		"a/b/c/d/too.go": "package main\n",
	})
	out, err := signals.ScanTree(root)
	if err != nil {
		t.Fatalf("ScanTree: %v", err)
	}
	// c/ has 2 children (d/ + deep.go); it shows both — use simple annotation.
	if !strings.Contains(out, "c/ (2)") {
		t.Errorf("expected 'c/ (2)' (simple annotation after showing all children):\n%s", out)
	}
}

// ---- CP-1: Bounded tree + per-path metadata ----

func TestScanTree_FileMetadata(t *testing.T) {
	// File entries at depth ≤ max_depth must carry per-file metadata:
	// "<filename> (<sha>, <lines>L, <chars>ch, <bytes>B)"
	// sha = 7-char hex prefix of SHA-256 of file bytes.
	content := "package main\n\nfunc main() {}\n"
	root := makeRepo(t, map[string]string{
		"main.go": content,
	})
	out, err := signals.ScanTree(root)
	if err != nil {
		t.Fatalf("ScanTree: %v", err)
	}
	// Must contain filename.
	if !strings.Contains(out, "main.go") {
		t.Fatalf("expected main.go in output:\n%s", out)
	}
	// Must contain lines annotation.
	if !strings.Contains(out, "L,") {
		t.Errorf("expected lines metadata (e.g. '3L,') in output:\n%s", out)
	}
	// Must contain chars annotation.
	if !strings.Contains(out, "ch,") {
		t.Errorf("expected chars metadata (e.g. '30ch,') in output:\n%s", out)
	}
	// Must contain bytes annotation.
	if !strings.Contains(out, "B)") {
		t.Errorf("expected bytes metadata (e.g. '30B)') in output:\n%s", out)
	}
	// Must contain a 7-char hex string (sha prefix).
	// Format: "main.go (abc1234, 3L, 30ch, 30B)"
	// The sha is 7 hex chars followed by a comma.
	lines := strings.Split(strings.TrimSpace(out), "\n")
	found := false
	for _, l := range lines {
		if strings.Contains(l, "main.go") {
			// Extract what's in the parentheses: (sha, NL, Nch, NB)
			open := strings.Index(l, "(")
			close := strings.LastIndex(l, ")")
			if open == -1 || close == -1 || close <= open {
				t.Errorf("metadata parentheses not found in line: %q", l)
				break
			}
			meta := l[open+1 : close]
			parts := strings.Split(meta, ", ")
			if len(parts) != 4 {
				t.Errorf("expected 4 metadata parts, got %d in %q", len(parts), meta)
				break
			}
			// parts[0] = sha (7 hex chars)
			sha := parts[0]
			if len(sha) != 7 {
				t.Errorf("expected 7-char sha, got %q (len=%d)", sha, len(sha))
			}
			for _, c := range sha {
				if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
					t.Errorf("sha contains non-hex char %q in %q", c, sha)
				}
			}
			found = true
			break
		}
	}
	if !found {
		t.Errorf("no line containing 'main.go' found in:\n%s", out)
	}
}

func TestScanTree_MetadataSingleRead(t *testing.T) {
	// Verify that metadata values are consistent with actual file content
	// (proving a single read is used for both LOC and SHA derivation).
	content := "line one\nline two\nline three\n"
	// SHA-256 of content, 7-char prefix.
	wantSHA := fmt.Sprintf("%x", sha256.Sum256([]byte(content)))[:7]
	lines := 3
	chars := len([]rune(content))
	bytesz := len(content)

	root := makeRepo(t, map[string]string{
		"file.go": content,
	})
	out, err := signals.ScanTree(root)
	if err != nil {
		t.Fatalf("ScanTree: %v", err)
	}

	// Find the line for file.go.
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "file.go") {
			open := strings.Index(l, "(")
			close := strings.LastIndex(l, ")")
			if open == -1 || close == -1 {
				t.Fatalf("no metadata parens in line %q", l)
			}
			meta := l[open+1 : close]
			parts := strings.Split(meta, ", ")
			if len(parts) != 4 {
				t.Fatalf("expected 4 parts, got %d: %q", len(parts), meta)
			}
			if parts[0] != wantSHA {
				t.Errorf("sha mismatch: want %q, got %q", wantSHA, parts[0])
			}
			wantLines := fmt.Sprintf("%dL", lines)
			if parts[1] != wantLines {
				t.Errorf("lines mismatch: want %q, got %q", wantLines, parts[1])
			}
			wantChars := fmt.Sprintf("%dch", chars)
			if parts[2] != wantChars {
				t.Errorf("chars mismatch: want %q, got %q", wantChars, parts[2])
			}
			wantBytes := fmt.Sprintf("%dB", bytesz)
			if parts[3] != wantBytes {
				t.Errorf("bytes mismatch: want %q, got %q", wantBytes, parts[3])
			}
			return
		}
	}
	t.Errorf("file.go not found in output:\n%s", out)
}

func TestScanTree_DepthPlusOneShowsFolderSummary(t *testing.T) {
	// Directory at max_depth+1 (depth 4 with default max_depth=3) must show:
	// "<dirname>/ (<N> files, <M> dirs)" — NOT contents, NOT file metadata.
	// Structure: a/b/c/ is depth 3 (shown with files); d/ inside is depth 4 (summary only).
	root := makeRepo(t, map[string]string{
		"a/b/c/deep.go":      "package main\n",
		"a/b/c/d/file1.go":   "package main\n",
		"a/b/c/d/file2.go":   "package main\n",
		"a/b/c/d/sub/too.go": "package main\n",
	})
	out, err := signals.ScanTree(root)
	if err != nil {
		t.Fatalf("ScanTree: %v", err)
	}
	// d/ must appear (depth 4 = max_depth+1).
	if !strings.Contains(out, "d/") {
		t.Fatalf("expected d/ (depth 4 = max_depth+1) in output:\n%s", out)
	}
	// d/ must show files=2, dirs=1 summary. Format: "d/ (2 files, 1 dir)" or "d/ (2 files, 1 dirs)".
	// The exact format per spec: "<N> files, <M> dirs".
	if !strings.Contains(out, "2 files") {
		t.Errorf("expected '2 files' in d/ summary:\n%s", out)
	}
	// file1.go and file2.go must NOT appear individually.
	if strings.Contains(out, "file1.go") {
		t.Errorf("file1.go should not appear (depth 4 content):\n%s", out)
	}
	if strings.Contains(out, "file2.go") {
		t.Errorf("file2.go should not appear (depth 4 content):\n%s", out)
	}
	// too.go must NOT appear (depth 5 = > max_depth+1 — elided entirely).
	if strings.Contains(out, "too.go") {
		t.Errorf("too.go should not appear (depth 5 — elided):\n%s", out)
	}
}

func TestScanTree_BeyondDepthPlusOneElided(t *testing.T) {
	// Directories > max_depth+1 are elided (contribute to parent count only).
	// Structure: a/b/c/d/e/f.go — e/ is depth 5, elided.
	root := makeRepo(t, map[string]string{
		"a/b/c/d/e/f.go": "package main\n",
	})
	out, err := signals.ScanTree(root)
	if err != nil {
		t.Fatalf("ScanTree: %v", err)
	}
	// d/ appears at depth 4 (max_depth+1) — should show summary.
	if !strings.Contains(out, "d/") {
		t.Fatalf("expected d/ at depth 4 in output:\n%s", out)
	}
	// e/ must NOT appear (depth 5 — elided).
	if strings.Contains(out, "e/") {
		t.Errorf("e/ should not appear (depth 5 — elided):\n%s", out)
	}
	// f.go must NOT appear.
	if strings.Contains(out, "f.go") {
		t.Errorf("f.go should not appear (depth 5+ — elided):\n%s", out)
	}
	// d/ summary should mention 1 dir (e/).
	if !strings.Contains(out, "1 dir") {
		t.Errorf("expected '1 dir' in d/ summary:\n%s", out)
	}
}

func TestScanTree_MaxDepthParameterized(t *testing.T) {
	// When MaxDepth=1 is passed via Options:
	//   - Files at depth 1 (direct children of root) appear with metadata.
	//   - pkg/ (dir at depth 1) is fully expanded — its direct file children appear.
	//   - Subdirs of pkg/ at depth 2 (= max_depth+1) appear as summaries.
	//   - Files inside those subdirs (depth 3) are elided.
	root := makeRepo(t, map[string]string{
		"top.go":          "package main\n",
		"pkg/lib.go":      "package pkg\n",
		"pkg/util.go":     "package pkg\n",
		"pkg/sub/deep.go": "package sub\n",
	})
	opts := &signals.Options{MaxDepth: 1}
	out, err := signals.ScanTreeWithOptions(root, opts)
	if err != nil {
		t.Fatalf("ScanTreeWithOptions: %v", err)
	}
	// top.go is a direct child of root — must appear with metadata.
	if !strings.Contains(out, "top.go") {
		t.Errorf("expected top.go in output:\n%s", out)
	}
	// top.go must have metadata annotation.
	if !strings.Contains(out, "L,") {
		t.Errorf("expected metadata on top.go:\n%s", out)
	}
	// pkg/ is at depth 1 — shown.
	if !strings.Contains(out, "pkg/") {
		t.Errorf("expected pkg/ in output:\n%s", out)
	}
	// pkg/sub/ is a dir at depth 2 (max_depth+1) — must appear as summary.
	if !strings.Contains(out, "sub/") {
		t.Errorf("expected sub/ (depth 2 = max_depth+1) as summary:\n%s", out)
	}
	// sub/ summary must list file/dir counts.
	if !strings.Contains(out, "1 file") {
		t.Errorf("expected '1 file' in sub/ summary:\n%s", out)
	}
	// deep.go is inside sub/ (summarized) — must NOT appear individually.
	if strings.Contains(out, "deep.go") {
		t.Errorf("deep.go should not appear (inside summarized sub/):\n%s", out)
	}
}

func TestScanWithOptions_MaxDepthDefault(t *testing.T) {
	// Options with MaxDepth=0 (zero value) should fall back to default of 3.
	root := makeRepo(t, map[string]string{
		"a/b/c/file.go": "package main\n",
	})
	opts := &signals.Options{}
	out, err := signals.ScanTreeWithOptions(root, opts)
	if err != nil {
		t.Fatalf("ScanTreeWithOptions: %v", err)
	}
	// file.go is at depth 3 — must appear (default max_depth=3).
	if !strings.Contains(out, "file.go") {
		t.Errorf("expected file.go (depth 3) with default max_depth=3:\n%s", out)
	}
}

// ---- CP-2: .signalsignore read + [generated] flagging ----

func TestSignalsIgnore_MatchingPathFlagged(t *testing.T) {
	// Files matching '+'-prefixed .signalsignore globs must appear in tree with [generated] marker.
	// WHY: inferrer must be able to identify generated files without omitting them from the
	// deterministic substrate (their SHA is still needed for change detection).
	content := "package main\n"
	root := makeRepo(t, map[string]string{
		"main.go":              content,
		"generated/openapi.go": content,
		".signalsignore":       "+generated/*\n",
	})
	out, err := signals.ScanTree(root)
	if err != nil {
		t.Fatalf("ScanTree: %v", err)
	}
	// main.go must NOT be flagged.
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "main.go") && strings.Contains(l, "[generated]") {
			t.Errorf("main.go should not have [generated] flag:\n%s", l)
		}
	}
	// openapi.go must be flagged.
	found := false
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "openapi.go") {
			found = true
			if !strings.Contains(l, "[generated]") {
				t.Errorf("openapi.go should have [generated] flag, got: %q", l)
			}
		}
	}
	if !found {
		t.Errorf("openapi.go not found in tree output (must appear even when generated):\n%s", out)
	}
}

func TestSignalsIgnore_ContentSHAStillComputed(t *testing.T) {
	// [generated] files must still carry full metadata (SHA, lines, chars, bytes).
	// WHY: content SHA on generated files is needed for change detection (CP-4).
	content := "package main\n\nfunc main() {}\n"
	root := makeRepo(t, map[string]string{
		"gen.go":         content,
		".signalsignore": "+gen.go\n",
	})
	out, err := signals.ScanTree(root)
	if err != nil {
		t.Fatalf("ScanTree: %v", err)
	}
	// gen.go must appear with both metadata and [generated] flag.
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "gen.go") {
			if !strings.Contains(l, "[generated]") {
				t.Errorf("gen.go missing [generated] flag: %q", l)
			}
			// Metadata: must contain a 7-char hex SHA followed by "L," pattern.
			if !strings.Contains(l, "L,") {
				t.Errorf("gen.go missing line count metadata: %q", l)
			}
			if !strings.Contains(l, "ch,") {
				t.Errorf("gen.go missing char count metadata: %q", l)
			}
			if !strings.Contains(l, "B)") {
				t.Errorf("gen.go missing byte count metadata: %q", l)
			}
			return
		}
	}
	t.Errorf("gen.go not found in output:\n%s", out)
}

func TestSignalsIgnore_AbsentFileNoExclusions(t *testing.T) {
	// Absent .signalsignore means no files are flagged — no error.
	// WHY: .signalsignore is opt-in; absence is the common case.
	content := "package main\n"
	root := makeRepo(t, map[string]string{
		"main.go": content,
	})
	out, err := signals.ScanTree(root)
	if err != nil {
		t.Fatalf("ScanTree without .signalsignore: %v", err)
	}
	if strings.Contains(out, "[generated]") {
		t.Errorf("no .signalsignore = no [generated] flags, but got:\n%s", out)
	}
}

func TestSignalsIgnore_CommentsAndBlankLinesIgnored(t *testing.T) {
	// Comment lines (# ...) and blank lines in .signalsignore are skipped.
	// WHY: standard .gitignore-style format; comments are documentation, not patterns.
	content := "package main\n"
	root := makeRepo(t, map[string]string{
		"gen.go":  content,
		"real.go": content,
		".signalsignore": "# this is a comment\n" +
			"\n" +
			"+gen.go\n" +
			"  # indented comment\n" +
			"\n",
	})
	out, err := signals.ScanTree(root)
	if err != nil {
		t.Fatalf("ScanTree: %v", err)
	}
	// gen.go matches the '+' pattern — must be flagged as [generated].
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "gen.go") {
			if !strings.Contains(l, "[generated]") {
				t.Errorf("gen.go should be flagged: %q", l)
			}
		}
	}
	// real.go does not match — must NOT be flagged.
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "real.go") && strings.Contains(l, "[generated]") {
			t.Errorf("real.go should not be flagged: %q", l)
		}
	}
}

func TestSignalsIgnore_PlainGlobExcludesEntirely(t *testing.T) {
	// Plain (no '+' prefix) .signalsignore globs exclude files from the tree entirely.
	// WHY: the new default behavior is full exclusion; '+' prefix opts into [generated] flagging.
	content := "package main\n"
	root := makeRepo(t, map[string]string{
		"main.go":              content,
		"vendor/dep/dep.go":    content,
		"generated/openapi.go": content,
		".signalsignore": "vendor/**\n" +
			"generated/**\n",
	})
	out, err := signals.ScanTree(root)
	if err != nil {
		t.Fatalf("ScanTree: %v", err)
	}
	// main.go must appear without any flag.
	if !strings.Contains(out, "main.go") {
		t.Errorf("main.go must appear in tree, got:\n%s", out)
	}
	// vendor and generated files must be absent from the tree entirely.
	if strings.Contains(out, "dep.go") {
		t.Errorf("dep.go (excluded via plain glob) must not appear in tree:\n%s", out)
	}
	if strings.Contains(out, "openapi.go") {
		t.Errorf("openapi.go (excluded via plain glob) must not appear in tree:\n%s", out)
	}
	// No [generated] flags — excluded files are gone, not flagged.
	if strings.Contains(out, "[generated]") {
		t.Errorf("excluded files must not produce [generated] flags:\n%s", out)
	}
}

func TestSignalsIgnore_MixedPrefixes(t *testing.T) {
	// A .signalsignore file may contain both plain excludes and '+' generated flags.
	// WHY: users need both behaviors in one file.
	content := "package main\n"
	root := makeRepo(t, map[string]string{
		"main.go":           content,
		"node_modules/x.js": content,
		"gen/pb.go":         content,
		".signalsignore": "node_modules/**\n" +
			"+gen/*.go\n",
	})
	out, err := signals.ScanTree(root)
	if err != nil {
		t.Fatalf("ScanTree: %v", err)
	}
	// main.go: present, no flag.
	if !strings.Contains(out, "main.go") {
		t.Errorf("main.go must appear in tree:\n%s", out)
	}
	// x.js: plain exclude — must be absent.
	if strings.Contains(out, "x.js") {
		t.Errorf("x.js (plain exclude) must not appear in tree:\n%s", out)
	}
	// pb.go: '+' generated — must appear with [generated] flag.
	found := false
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "pb.go") {
			found = true
			if !strings.Contains(l, "[generated]") {
				t.Errorf("pb.go should have [generated] flag, got: %q", l)
			}
		}
	}
	if !found {
		t.Errorf("pb.go ('+' generated) must appear in tree:\n%s", out)
	}
}

// ---- CP-3: output.signals.max_depth config wiring ----

func TestScanWithOptions_ConfigMaxDepthWiring(t *testing.T) {
	// WHY: CP-3 requires that when opts.MaxDepth==0, ScanWithOptions reads
	// output.signals.max_depth from config. This test injects a config that
	// sets max_depth=1 and verifies the tree is bounded accordingly. With
	// max_depth=1, directories at depth 2 (= max_depth+1) appear as summaries;
	// their sub-directories' contents are elided. Verified via the written file.
	root := makeRepo(t, map[string]string{
		"top.go":          "package main\n",
		"pkg/sub/deep.go": "package sub\n",
	})

	// Write a config file with max_depth=1.
	configDir := t.TempDir()
	configPath := filepath.Join(configDir, "config.toml")
	if err := os.WriteFile(configPath, []byte("[output.signals]\nmax_depth = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Inject the config path so we don't touch ~/.claude/.atomic/config.toml.
	// MaxDepth is left 0 — ScanWithOptions must read it from ConfigPath.
	opts := &signals.Options{ConfigPath: configPath}
	if err := signals.ScanWithOptions(root, opts); err != nil {
		t.Fatalf("ScanWithOptions: %v", err)
	}

	data, err := os.ReadFile(signals.SignalsPath(root))
	if err != nil {
		t.Fatalf("read signals file: %v", err)
	}
	out := string(data)

	// top.go is at depth 1 — must appear with metadata.
	if !strings.Contains(out, "top.go") {
		t.Errorf("expected top.go in output:\n%s", out)
	}
	if !strings.Contains(out, "L,") {
		t.Errorf("expected metadata on top.go:\n%s", out)
	}

	// pkg/ is at depth 1 — must appear.
	if !strings.Contains(out, "pkg/") {
		t.Errorf("expected pkg/ in output:\n%s", out)
	}

	// sub/ is a directory at depth 2 (= max_depth+1) — must appear as a file/dir summary.
	if !strings.Contains(out, "sub/") {
		t.Errorf("expected sub/ (depth 2 = max_depth+1) to appear as summary:\n%s", out)
	}

	// deep.go is inside sub/ (depth 3, elided) — must NOT appear individually.
	if strings.Contains(out, "deep.go") {
		t.Errorf("deep.go should not appear (depth 3 > max_depth+1 with max_depth=1):\n%s", out)
	}
}

func TestScanWithOptions_ConfigMaxDepthFallbackDefault(t *testing.T) {
	// WHY: when ConfigPath points to a missing file and MaxDepth is 0,
	// ScanWithOptions must use the built-in default of 3.
	root := makeRepo(t, map[string]string{
		"a/b/c/file.go": "package main\n",
	})

	opts := &signals.Options{ConfigPath: "/nonexistent/path/config.toml"}
	if err := signals.ScanWithOptions(root, opts); err != nil {
		t.Fatalf("ScanWithOptions: %v", err)
	}

	data, err := os.ReadFile(signals.SignalsPath(root))
	if err != nil {
		t.Fatalf("read signals file: %v", err)
	}
	out := string(data)

	// file.go is at depth 3 — must appear with default max_depth=3.
	if !strings.Contains(out, "file.go") {
		t.Errorf("expected file.go (depth 3) with fallback default max_depth=3:\n%s", out)
	}
}

// ---- CP-4: Content-SHA diff: prev vs current deterministic scan ----

func TestParseTreeSHAs_UnchangedNotInChanged(t *testing.T) {
	// WHY: files with identical SHAs in prev and current must NOT appear in the
	// changed set — only actual modifications should trigger domain refresh.
	content := `## Tree

├── main.go (abc1234, 10L, 100ch, 100B)
└── lib.go (def5678, 5L, 50ch, 50B)
`
	shas := signals.ParseTreeSHAs(content)
	if shas["main.go"] != "abc1234" {
		t.Errorf("expected main.go sha=abc1234, got %q", shas["main.go"])
	}
	if shas["lib.go"] != "def5678" {
		t.Errorf("expected lib.go sha=def5678, got %q", shas["lib.go"])
	}

	// Diff against itself: no changes.
	cp := signals.DiffSHAs(shas, shas)
	if len(cp.Changed) != 0 {
		t.Errorf("expected 0 changed, got %v", cp.Changed)
	}
	if len(cp.Added) != 0 {
		t.Errorf("expected 0 added, got %v", cp.Added)
	}
	if len(cp.Removed) != 0 {
		t.Errorf("expected 0 removed, got %v", cp.Removed)
	}
}

func TestParseTreeSHAs_ChangedSHAInChangedSet(t *testing.T) {
	// WHY: a file whose SHA differs between scans means its content changed —
	// the inferrer must refresh any domain that references this path.
	prev := signals.ParseTreeSHAs(`## Tree

└── main.go (abc1234, 10L, 100ch, 100B)
`)
	curr := signals.ParseTreeSHAs(`## Tree

└── main.go (zzz9999, 12L, 120ch, 120B)
`)
	cp := signals.DiffSHAs(prev, curr)
	if len(cp.Changed) != 1 || cp.Changed[0] != "main.go" {
		t.Errorf("expected changed=[main.go], got %v", cp.Changed)
	}
	if len(cp.Added) != 0 {
		t.Errorf("expected 0 added, got %v", cp.Added)
	}
	if len(cp.Removed) != 0 {
		t.Errorf("expected 0 removed, got %v", cp.Removed)
	}
}

func TestParseTreeSHAs_AddedFileInAddedSet(t *testing.T) {
	// WHY: a new file that didn't exist in prev must appear in Added so the
	// inferrer knows to include it in domain analysis.
	prev := signals.ParseTreeSHAs(`## Tree

└── main.go (abc1234, 10L, 100ch, 100B)
`)
	curr := signals.ParseTreeSHAs(`## Tree

├── main.go (abc1234, 10L, 100ch, 100B)
└── new.go (fff0000, 3L, 30ch, 30B)
`)
	cp := signals.DiffSHAs(prev, curr)
	if len(cp.Added) != 1 || cp.Added[0] != "new.go" {
		t.Errorf("expected added=[new.go], got %v", cp.Added)
	}
	if len(cp.Changed) != 0 {
		t.Errorf("expected 0 changed, got %v", cp.Changed)
	}
	if len(cp.Removed) != 0 {
		t.Errorf("expected 0 removed, got %v", cp.Removed)
	}
}

func TestParseTreeSHAs_RemovedFileInRemovedSet(t *testing.T) {
	// WHY: a file deleted between scans must appear in Removed so the inferrer
	// can clean up domain references to that path.
	prev := signals.ParseTreeSHAs(`## Tree

├── main.go (abc1234, 10L, 100ch, 100B)
└── old.go (111aaaa, 5L, 50ch, 50B)
`)
	curr := signals.ParseTreeSHAs(`## Tree

└── main.go (abc1234, 10L, 100ch, 100B)
`)
	cp := signals.DiffSHAs(prev, curr)
	if len(cp.Removed) != 1 || cp.Removed[0] != "old.go" {
		t.Errorf("expected removed=[old.go], got %v", cp.Removed)
	}
	if len(cp.Changed) != 0 {
		t.Errorf("expected 0 changed, got %v", cp.Changed)
	}
	if len(cp.Added) != 0 {
		t.Errorf("expected 0 added, got %v", cp.Added)
	}
}

func TestParseTreeSHAs_GeneratedPathsExcluded(t *testing.T) {
	// WHY: [generated] files must NOT appear in the changed set even when their
	// SHA changes — generated files don't drive domain narratives (spec §Change
	// detection: "Changed content SHAs on generated files do not trigger domain
	// file refresh").
	prev := signals.ParseTreeSHAs(`## Tree

├── main.go (abc1234, 10L, 100ch, 100B)
└── gen.go (old0000, 5L, 50ch, 50B) [generated]
`)
	curr := signals.ParseTreeSHAs(`## Tree

├── main.go (abc1234, 10L, 100ch, 100B)
└── gen.go (new9999, 8L, 80ch, 80B) [generated]
`)
	cp := signals.DiffSHAs(prev, curr)
	// gen.go SHA changed but it's [generated] — must NOT be in any set.
	for _, p := range cp.Changed {
		if p == "gen.go" {
			t.Errorf("gen.go should not appear in Changed (it's [generated])")
		}
	}
	for _, p := range cp.Added {
		if p == "gen.go" {
			t.Errorf("gen.go should not appear in Added (it's [generated])")
		}
	}
	for _, p := range cp.Removed {
		if p == "gen.go" {
			t.Errorf("gen.go should not appear in Removed (it's [generated])")
		}
	}
	if len(cp.Changed) != 0 || len(cp.Added) != 0 || len(cp.Removed) != 0 {
		t.Errorf("expected all-empty sets, got changed=%v added=%v removed=%v", cp.Changed, cp.Added, cp.Removed)
	}
}

func TestParseTreeSHAs_NestedPathsNeverCollide(t *testing.T) {
	// WHY: two files with the same leaf name in different directories must produce
	// distinct map keys. ParseTreeSHAs must reconstruct repo-relative paths by
	// tracking the directory stack from tree indentation, not just returning the
	// leaf filename. Without this fix, src/main.go and cmd/main.go would both map
	// to "main.go" and silently collide — only the last one written survives.
	content := `## Tree

├── cmd/ (1)
│   └── main.go (aaa1111, 5L, 50ch, 50B)
└── src/ (1)
    └── main.go (bbb2222, 8L, 80ch, 80B)
`
	shas := signals.ParseTreeSHAs(content)
	if len(shas) != 2 {
		t.Fatalf("expected 2 entries (cmd/main.go and src/main.go), got %d: %v", len(shas), shas)
	}
	if shas["cmd/main.go"] != "aaa1111" {
		t.Errorf("expected cmd/main.go sha=aaa1111, got %q (map: %v)", shas["cmd/main.go"], shas)
	}
	if shas["src/main.go"] != "bbb2222" {
		t.Errorf("expected src/main.go sha=bbb2222, got %q (map: %v)", shas["src/main.go"], shas)
	}
}

func TestParseTreeSHAs_DeepNestedPath(t *testing.T) {
	// WHY: paths nested more than one level deep must be fully reconstructed.
	// Without directory-stack tracking, internal/signals/diff.go would yield
	// "diff.go" instead of "internal/signals/diff.go".
	content := `## Tree

└── internal/ (1)
    └── signals/ (1)
        └── diff.go (ccc3333, 20L, 200ch, 200B)
`
	shas := signals.ParseTreeSHAs(content)
	if len(shas) != 1 {
		t.Fatalf("expected 1 entry, got %d: %v", len(shas), shas)
	}
	if shas["internal/signals/diff.go"] != "ccc3333" {
		t.Errorf("expected internal/signals/diff.go=ccc3333, got map: %v", shas)
	}
}

func TestDiffPaths_WorksWithoutGit(t *testing.T) {
	// WHY: content-SHA diff must work in any directory — no git commands, no mtime.
	// The spec requires: "works without git; content SHA comparison, not git commands."
	root := makeRepo(t, map[string]string{
		"main.go": "package main\n",
	})

	// First scan — no prev file yet.
	if err := signals.Scan(root); err != nil {
		t.Fatalf("first Scan: %v", err)
	}

	// Add a new file to force a second scan to differ.
	if err := os.WriteFile(filepath.Join(root, "new.go"), []byte("package main\n// new\n"), 0o644); err != nil {
		t.Fatalf("write new.go: %v", err)
	}
	if err := signals.Scan(root); err != nil {
		t.Fatalf("second Scan: %v", err)
	}

	// DiffPaths must succeed without git.
	cp, err := signals.DiffPaths(root)
	if err != nil {
		t.Fatalf("DiffPaths: %v", err)
	}
	// new.go should appear as Added.
	found := false
	for _, p := range cp.Added {
		if p == "new.go" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected new.go in Added, got added=%v changed=%v removed=%v", cp.Added, cp.Changed, cp.Removed)
	}
}

// --- LinkifyFiles tests ---

// TestLinkifyFiles_LinkifiesRouterAndDomains verifies that LinkifyFilesWithBase
// rewrites signals.md and domain files under signals/, and leaves non-path tokens
// untouched.
func TestLinkifyFiles_LinkifiesRouterAndDomains(t *testing.T) {
	root := t.TempDir()

	// Create the target file that the token will resolve to.
	targetPath := filepath.Join(root, "agents", "atomic-builder.md")
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(targetPath, []byte("# builder\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Create signals.md with a plain backtick token.
	projectDir := filepath.Join(root, ".claude", "project")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	routerContent := "# router\n\nSee `agents/atomic-builder.md` for details.\n"
	routerPath := filepath.Join(projectDir, "signals.md")
	if err := os.WriteFile(routerPath, []byte(routerContent), 0o644); err != nil {
		t.Fatal(err)
	}

	// Create a domain file under signals/ with a plain backtick token.
	domainDir := filepath.Join(projectDir, "signals")
	if err := os.MkdirAll(domainDir, 0o755); err != nil {
		t.Fatal(err)
	}
	domainContent := "# domain\n\n`agents/atomic-builder.md` is key.\n"
	domainPath := filepath.Join(domainDir, "workflow.md")
	if err := os.WriteFile(domainPath, []byte(domainContent), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := signals.LinkifyFilesWithBase(root, root); err != nil {
		t.Fatalf("LinkifyFilesWithBase: %v", err)
	}

	// Router should be linkified.
	gotRouter, err := os.ReadFile(routerPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(gotRouter), "[`agents/atomic-builder.md`]") {
		t.Errorf("router not linkified:\n%s", gotRouter)
	}

	// Domain file should be linkified.
	gotDomain, err := os.ReadFile(domainPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(gotDomain), "[`agents/atomic-builder.md`]") {
		t.Errorf("domain file not linkified:\n%s", gotDomain)
	}
}

// TestLinkifyFiles_Idempotent verifies that running twice produces no change on
// the second run (byte-identical output).
func TestLinkifyFiles_Idempotent(t *testing.T) {
	root := t.TempDir()

	targetPath := filepath.Join(root, "agents", "atomic-builder.md")
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(targetPath, []byte("# builder\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	projectDir := filepath.Join(root, ".claude", "project")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	routerPath := filepath.Join(projectDir, "signals.md")
	if err := os.WriteFile(routerPath, []byte("# router\n\nSee `agents/atomic-builder.md`.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := signals.LinkifyFilesWithBase(root, root); err != nil {
		t.Fatalf("first run: %v", err)
	}
	after1, _ := os.ReadFile(routerPath)

	if err := signals.LinkifyFilesWithBase(root, root); err != nil {
		t.Fatalf("second run: %v", err)
	}
	after2, _ := os.ReadFile(routerPath)

	if string(after1) != string(after2) {
		t.Errorf("not idempotent:\nafter1: %q\nafter2: %q", after1, after2)
	}
}

// TestLinkifyFiles_NoOp_WhenNoFiles verifies no error when signals.md and
// signals/ directory don't exist.
func TestLinkifyFiles_NoOp_WhenNoFiles(t *testing.T) {
	root := t.TempDir()
	if err := signals.LinkifyFilesWithBase(root, root); err != nil {
		t.Errorf("expected no error on empty root: %v", err)
	}
}
