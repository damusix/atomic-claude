package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	codecli "github.com/damusix/atomic-claude/atomic/internal/codeintel/cli"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/engine"
	"github.com/damusix/atomic-claude/atomic/internal/config"
)

func noStdin() io.Reader { return strings.NewReader("") }

const fixtureA = `package greeter

// Greet returns a greeting for name.
func Greet(name string) string {
	return "Hello, " + name
}
`

const fixtureB = `package main

import "github.com/example/greeter"

func main() {
	msg := greeter.Greet("world")
	_ = msg
}
`

// The import sits in its own declaration because the Go extractor takes only
// the first path out of a multi-import block.
const fixtureTest = `package greeter_test

import _ "./greeter"

func TestGreet() {}
`

func writeFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	must(t, os.WriteFile(filepath.Join(dir, "greeter.go"), []byte(fixtureA), 0o644))
	must(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte(fixtureB), 0o644))
	return dir
}

func writeFixtureWithTest(t *testing.T) string {
	t.Helper()
	dir := writeFixture(t)
	must(t, os.WriteFile(filepath.Join(dir, "greeter_test.go"), []byte(fixtureTest), 0o644))
	return dir
}

func indexedEngine(t *testing.T, dir string) *engine.Engine {
	t.Helper()
	ctx := testCtx(t)
	eng, err := engine.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { eng.Close() })
	if err := eng.Init(ctx); err != nil {
		t.Fatal("Init:", err)
	}
	if err := eng.IndexAll(ctx); err != nil {
		t.Fatal("IndexAll:", err)
	}
	if err := eng.ResolveReferences(ctx); err != nil {
		t.Fatal("ResolveReferences:", err)
	}
	return eng
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func testCtx(t *testing.T) context.Context {
	t.Helper()
	return context.Background()
}

func TestDispatch_UnknownVerb(t *testing.T) {
	dir := t.TempDir()
	var stdout, stderr bytes.Buffer
	code := codecli.RunCode([]string{"nonsense"}, dir, &stdout, &stderr, noStdin())
	if code == 0 {
		t.Fatal("unknown verb should return non-zero exit code")
	}
	if !strings.Contains(stderr.String(), "unknown verb") {
		t.Fatalf("expected 'unknown verb' in stderr, got: %s", stderr.String())
	}
}

func TestDispatch_NoArgs_PrintsUsage(t *testing.T) {
	dir := t.TempDir()
	var stdout, stderr bytes.Buffer
	code := codecli.RunCode([]string{}, dir, &stdout, &stderr, noStdin())
	if code != 0 {
		t.Fatalf("no args should return 0, got %d", code)
	}
	combined := stdout.String() + stderr.String()
	if !strings.Contains(combined, "atomic code") {
		t.Fatalf("expected usage text, got: %s", combined)
	}
}

func TestDispatch_NoArgs_PrintsHarnessAwareDBPath(t *testing.T) {
	restore := config.SetHarnessDirForTest(".pi")
	defer restore()

	dir := t.TempDir()
	var stdout, stderr bytes.Buffer
	codecli.RunCode([]string{}, dir, &stdout, &stderr, noStdin())
	combined := stdout.String() + stderr.String()
	want := "DB path: <project>/.pi/.atomic-index/atomic.db"
	if !strings.Contains(combined, want) {
		t.Fatalf("expected usage text to contain %q, got: %s", want, combined)
	}
	if strings.Contains(combined, ".claude/.atomic-index") {
		t.Fatalf("usage text must not show the default-harness literal under a .pi harness, got: %s", combined)
	}
}

func TestStatus_JSON_Fields(t *testing.T) {
	dir := writeFixture(t)
	indexedEngine(t, dir)

	var stdout, stderr bytes.Buffer
	code := codecli.RunCode([]string{"status", "--json"}, dir, &stdout, &stderr, noStdin())
	if code != 0 {
		t.Fatalf("status --json exit %d; stderr: %s", code, stderr.String())
	}

	var s codecli.StatusJSON
	if err := json.Unmarshal(stdout.Bytes(), &s); err != nil {
		t.Fatalf("status --json output is not valid JSON: %v\noutput: %s", err, stdout.String())
	}

	if !s.Initialized {
		t.Error("initialized should be true")
	}
	if s.Version == "" {
		t.Error("version should be non-empty")
	}
	if s.IndexPath == "" {
		t.Error("indexPath should be non-empty")
	}
	if s.FileCount == 0 {
		t.Error("fileCount should be > 0 after indexing")
	}
	if s.NodeCount == 0 {
		t.Error("nodeCount should be > 0 after indexing")
	}
	if s.Backend != "sqlite" {
		t.Errorf("backend: want sqlite, got %q", s.Backend)
	}
	if s.JournalMode != "wal" {
		t.Errorf("journalMode: want wal, got %q", s.JournalMode)
	}
	if s.NodesByKind == nil {
		t.Error("nodesByKind should be non-nil")
	}
}

func TestStatus_JSON_PendingChanges(t *testing.T) {
	dir := writeFixture(t)
	indexedEngine(t, dir)

	modifiedContent := fixtureA + "\n// modified\n"
	must(t, os.WriteFile(filepath.Join(dir, "greeter.go"), []byte(modifiedContent), 0o644))

	var stdout, stderr bytes.Buffer
	code := codecli.RunCode([]string{"status", "--json"}, dir, &stdout, &stderr, noStdin())
	if code != 0 {
		t.Fatalf("status --json exit %d; stderr: %s", code, stderr.String())
	}

	var s codecli.StatusJSON
	must(t, json.Unmarshal(stdout.Bytes(), &s))

	if s.PendingChanges < 1 {
		t.Errorf("pendingChanges should be >= 1 after modifying a file, got %d", s.PendingChanges)
	}
}

func TestSearch_JSON(t *testing.T) {
	dir := writeFixture(t)
	indexedEngine(t, dir)

	var stdout, stderr bytes.Buffer
	code := codecli.RunCode([]string{"search", "--json", "Greet"}, dir, &stdout, &stderr, noStdin())
	if code != 0 {
		t.Fatalf("search exit %d; stderr: %s", code, stderr.String())
	}

	var results []interface{}
	if err := json.Unmarshal(stdout.Bytes(), &results); err != nil {
		t.Fatalf("search --json not valid JSON: %v\noutput: %s", err, stdout.String())
	}
	if len(results) == 0 {
		t.Error("search for 'Greet' should return at least one result")
	}
}

func TestCallees_JSON(t *testing.T) {
	dir := writeFixture(t)
	indexedEngine(t, dir)

	var stdout, stderr bytes.Buffer
	code := codecli.RunCode([]string{"callees", "--json", "main"}, dir, &stdout, &stderr, noStdin())
	if code != 0 {
		t.Fatalf("callees exit %d; stderr: %s", code, stderr.String())
	}

	var sg map[string]interface{}
	if err := json.Unmarshal(stdout.Bytes(), &sg); err != nil {
		t.Fatalf("callees --json not valid JSON: %v\noutput: %s", err, stdout.String())
	}
}

func TestCallers_JSON(t *testing.T) {
	dir := writeFixture(t)
	indexedEngine(t, dir)

	var stdout, stderr bytes.Buffer
	code := codecli.RunCode([]string{"callers", "--json", "Greet"}, dir, &stdout, &stderr, noStdin())
	if code != 0 {
		t.Fatalf("callers exit %d; stderr: %s", code, stderr.String())
	}
	var sg map[string]interface{}
	if err := json.Unmarshal(stdout.Bytes(), &sg); err != nil {
		t.Fatalf("callers --json not valid JSON: %v\noutput: %s", err, stdout.String())
	}
}

func TestImpact_JSON(t *testing.T) {
	dir := writeFixture(t)
	indexedEngine(t, dir)

	var stdout, stderr bytes.Buffer
	code := codecli.RunCode([]string{"impact", "--json", "Greet"}, dir, &stdout, &stderr, noStdin())
	if code != 0 {
		t.Fatalf("impact exit %d; stderr: %s", code, stderr.String())
	}
	var sg map[string]interface{}
	if err := json.Unmarshal(stdout.Bytes(), &sg); err != nil {
		t.Fatalf("impact --json not valid JSON: %v\noutput: %s", err, stdout.String())
	}
}

func TestAffected_FindsTestFile(t *testing.T) {
	dir := writeFixtureWithTest(t)
	indexedEngine(t, dir)

	var stdout, stderr bytes.Buffer
	code := codecli.RunCode([]string{"affected", "--depth", "5", "greeter.go"}, dir, &stdout, &stderr, noStdin())
	if code != 0 {
		t.Fatalf("affected exit %d; stderr: %s", code, stderr.String())
	}

	out := stdout.String()
	if !strings.Contains(out, "greeter_test.go") {
		t.Errorf("affected output should contain 'greeter_test.go' (BFS via import-edge);\nstdout: %s\nstderr: %s", out, stderr.String())
	}
}

func TestAffected_Stdin(t *testing.T) {
	dir := writeFixtureWithTest(t)
	indexedEngine(t, dir)

	stdinReader := strings.NewReader("greeter.go\n")
	var stdout, stderr bytes.Buffer
	code := codecli.RunCode([]string{"affected", "--stdin", "--depth", "5"}, dir, &stdout, &stderr, stdinReader)
	if code != 0 {
		t.Fatalf("affected --stdin exit %d; stderr: %s", code, stderr.String())
	}

	out := stdout.String()
	if !strings.Contains(out, "greeter_test.go") {
		t.Errorf("affected --stdin output should contain 'greeter_test.go';\nstdout: %s\nstderr: %s", out, stderr.String())
	}
}

func TestFiles_JSON(t *testing.T) {
	dir := writeFixture(t)
	indexedEngine(t, dir)

	var stdout, stderr bytes.Buffer
	code := codecli.RunCode([]string{"files", "--json"}, dir, &stdout, &stderr, noStdin())
	if code != 0 {
		t.Fatalf("files exit %d; stderr: %s", code, stderr.String())
	}
	var files []interface{}
	if err := json.Unmarshal(stdout.Bytes(), &files); err != nil {
		t.Fatalf("files --json not valid JSON: %v\noutput: %s", err, stdout.String())
	}
	if len(files) == 0 {
		t.Error("files should list at least one file after indexing")
	}
}

func TestExplore_ReturnsContent(t *testing.T) {
	dir := writeFixture(t)
	indexedEngine(t, dir)

	var stdout, stderr bytes.Buffer
	code := codecli.RunCode([]string{"explore", "Greet"}, dir, &stdout, &stderr, noStdin())
	if code != 0 {
		t.Fatalf("explore exit %d; stderr: %s", code, stderr.String())
	}
	if stdout.Len() == 0 {
		t.Error("explore should produce non-empty output")
	}
}

func TestEnsureGitignore_Idempotent(t *testing.T) {
	dir := t.TempDir()
	gitignorePath := filepath.Join(dir, ".gitignore")

	must(t, codecli.EnsureGitignore(dir))
	data, err := os.ReadFile(gitignorePath)
	must(t, err)
	if !strings.Contains(string(data), ".claude/.atomic-index/") {
		t.Fatal("gitignore entry not present after first call")
	}

	must(t, codecli.EnsureGitignore(dir))
	data2, err := os.ReadFile(gitignorePath)
	must(t, err)

	count := strings.Count(string(data2), ".claude/.atomic-index/")
	if count != 1 {
		t.Errorf("expected exactly 1 gitignore entry, found %d:\n%s", count, string(data2))
	}
}

func TestEnsureGitignore_CreatesFile(t *testing.T) {
	dir := t.TempDir()
	gitignorePath := filepath.Join(dir, ".gitignore")

	if _, err := os.Stat(gitignorePath); err == nil {
		t.Fatal("precondition: .gitignore should not exist")
	}

	must(t, codecli.EnsureGitignore(dir))

	data, err := os.ReadFile(gitignorePath)
	must(t, err)
	if !strings.Contains(string(data), ".claude/.atomic-index/") {
		t.Fatalf(".gitignore created but does not contain the entry:\n%s", string(data))
	}
}

// The written rule must track harness.dir, not a hardcoded ".claude" literal.
func TestEnsureGitignore_HarnessAware(t *testing.T) {
	restore := config.SetHarnessDirForTest(".pi")
	defer restore()

	dir := t.TempDir()
	gitignorePath := filepath.Join(dir, ".gitignore")

	must(t, codecli.EnsureGitignore(dir))

	data, err := os.ReadFile(gitignorePath)
	must(t, err)
	if !strings.Contains(string(data), ".pi/.atomic-index/") {
		t.Fatalf("gitignore entry not present for .pi harness dir:\n%s", string(data))
	}
	if strings.Contains(string(data), ".claude/.atomic-index/") {
		t.Fatalf("gitignore must not contain the default-harness literal under a .pi harness:\n%s", string(data))
	}
}

// A countPendingChanges failure must degrade to a stderr note and exit 0.
func TestStatus_PendingChanges_StderrOnError(t *testing.T) {
	dir := writeFixture(t)
	indexedEngine(t, dir)

	// An unreadable file fails ReadFile and so counts as deleted, i.e. pending.
	greeterPath := filepath.Join(dir, "greeter.go")
	must(t, os.Chmod(greeterPath, 0o000))
	t.Cleanup(func() { os.Chmod(greeterPath, 0o644) }) // restore so TempDir cleanup works

	var stdout, stderr bytes.Buffer
	code := codecli.RunCode([]string{"status", "--json"}, dir, &stdout, &stderr, noStdin())
	if code != 0 {
		t.Fatalf("status should succeed even when a file is unreadable (non-fatal); exit %d, stderr: %s", code, stderr.String())
	}

	var s codecli.StatusJSON
	must(t, json.Unmarshal(stdout.Bytes(), &s))
	if s.PendingChanges < 1 {
		t.Errorf("pendingChanges should be >= 1 when a file is unreadable, got %d", s.PendingChanges)
	}
}

func TestStatus_PendingChanges_Success(t *testing.T) {
	dir := writeFixture(t)
	indexedEngine(t, dir)

	var stdout, stderr bytes.Buffer
	code := codecli.RunCode([]string{"status", "--json"}, dir, &stdout, &stderr, noStdin())
	if code != 0 {
		t.Fatalf("status exit %d; stderr: %s", code, stderr.String())
	}

	var s codecli.StatusJSON
	must(t, json.Unmarshal(stdout.Bytes(), &s))
	if s.PendingChanges != 0 {
		t.Errorf("pendingChanges should be 0 immediately after indexing, got %d", s.PendingChanges)
	}
}

// Sync must not silently create an index on a never-indexed project.
func TestSync_NotIndexed_ReturnsError(t *testing.T) {
	dir := t.TempDir()
	var stdout, stderr bytes.Buffer
	code := codecli.RunCode([]string{"sync"}, dir, &stdout, &stderr, noStdin())
	if code == 0 {
		t.Fatal("sync on a never-indexed project should return non-zero exit code")
	}
	errOut := stderr.String()
	if !strings.Contains(errOut, "not initialized") && !strings.Contains(errOut, "atomic code index") {
		t.Errorf("sync on un-indexed project should mention 'not initialized' or 'atomic code index'; got: %s", errOut)
	}
}
