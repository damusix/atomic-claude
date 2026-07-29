package repoinit_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/repoinit"
)

func initGitRepo(t *testing.T, dir string) {
	t.Helper()
	cmd := exec.Command("git", "init", "-q", dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
}

func TestInit_ColdRepoScaffold(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)

	actions, err := repoinit.Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if len(actions) != 7 {
		t.Fatalf("expected 7 actions, got %d: %+v", len(actions), actions)
	}
	for _, a := range actions {
		if a.Kind != repoinit.ActionCreated {
			t.Errorf("%s: expected created, got %s", a.Name, a.Kind)
		}
	}

	for _, rel := range []string{filepath.Join(".claude", ".scratchpad"), filepath.Join(".claude", "project")} {
		info, err := os.Stat(filepath.Join(dir, rel))
		if err != nil || !info.IsDir() {
			t.Errorf("%s: not created as a directory", rel)
		}
	}

	nestedIgnore, err := os.ReadFile(filepath.Join(dir, ".claude", ".gitignore"))
	if err != nil {
		t.Fatalf("read nested .gitignore: %v", err)
	}
	nested := string(nestedIgnore)
	if !strings.Contains(nested, "# managed by atomic repo init") {
		t.Errorf("nested .gitignore missing managed header:\n%s", nested)
	}
	if !strings.Contains(nested, "/.scratchpad/") || !strings.Contains(nested, "/.atomic-index/") || !strings.Contains(nested, "/worktrees/") {
		t.Errorf("nested .gitignore missing managed rules:\n%s", nested)
	}

	rootIgnore, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		t.Fatalf("read root .gitignore: %v", err)
	}
	root := string(rootIgnore)
	if !strings.Contains(root, "tmp/") {
		t.Errorf("root .gitignore missing managed rules:\n%s", root)
	}

	scopeMarker, err := os.ReadFile(filepath.Join(dir, ".claude", "atomic.toml"))
	if err != nil {
		t.Fatalf("read .claude/atomic.toml: %v", err)
	}
	if string(scopeMarker) != "scope = \"repo\"\n" {
		t.Errorf(".claude/atomic.toml = %q, want %q", scopeMarker, "scope = \"repo\"\n")
	}
}

// TestInit_ColdRepoScaffold_UnderNonDefaultHarnessDir verifies every
// guarantee (scratchpad, project, nested .gitignore + its harness-aware
// managed header, .atomic-index and worktrees ignore rules) nests under the
// resolved harness dir — under a ".pi" harness dir, Init scaffolds .pi/...
// instead of the default .claude/....
func TestInit_ColdRepoScaffold_UnderNonDefaultHarnessDir(t *testing.T) {
	restore := config.SetHarnessDirForTest(".pi")
	defer restore()

	dir := t.TempDir()
	initGitRepo(t, dir)

	actions, err := repoinit.Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if len(actions) != 7 {
		t.Fatalf("expected 7 actions, got %d: %+v", len(actions), actions)
	}
	for _, a := range actions {
		if a.Kind != repoinit.ActionCreated {
			t.Errorf("%s: expected created, got %s", a.Name, a.Kind)
		}
	}

	for _, rel := range []string{filepath.Join(".pi", ".scratchpad"), filepath.Join(".pi", "project")} {
		info, err := os.Stat(filepath.Join(dir, rel))
		if err != nil || !info.IsDir() {
			t.Errorf("%s: not created as a directory", rel)
		}
	}

	nestedIgnore, err := os.ReadFile(filepath.Join(dir, ".pi", ".gitignore"))
	if err != nil {
		t.Fatalf("read nested .pi/.gitignore: %v", err)
	}
	nested := string(nestedIgnore)
	if !strings.Contains(nested, "# managed by atomic repo init; rules are relative to .pi/") {
		t.Errorf("nested .gitignore missing harness-aware managed header:\n%s", nested)
	}
	if !strings.Contains(nested, "/.scratchpad/") || !strings.Contains(nested, "/.atomic-index/") || !strings.Contains(nested, "/worktrees/") {
		t.Errorf("nested .gitignore missing managed rules:\n%s", nested)
	}

	// The default .claude/ layout must not have been touched.
	if _, err := os.Stat(filepath.Join(dir, ".claude")); !os.IsNotExist(err) {
		t.Errorf(".claude should not exist under a .pi harness, stat err=%v", err)
	}

	rootIgnore, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		t.Fatalf("read root .gitignore: %v", err)
	}
	if !strings.Contains(string(rootIgnore), "tmp/") {
		t.Errorf("root .gitignore missing managed rules:\n%s", rootIgnore)
	}

	scopeMarker, err := os.ReadFile(filepath.Join(dir, ".pi", "atomic.toml"))
	if err != nil {
		t.Fatalf("read .pi/atomic.toml: %v", err)
	}
	if string(scopeMarker) != "scope = \"repo\"\n" {
		t.Errorf(".pi/atomic.toml = %q, want %q", scopeMarker, "scope = \"repo\"\n")
	}
}

func TestInit_SecondRunIdempotent(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)

	if _, err := repoinit.Init(dir); err != nil {
		t.Fatalf("first Init: %v", err)
	}

	nestedBefore, _ := os.ReadFile(filepath.Join(dir, ".claude", ".gitignore"))
	rootBefore, _ := os.ReadFile(filepath.Join(dir, ".gitignore"))

	actions, err := repoinit.Init(dir)
	if err != nil {
		t.Fatalf("second Init: %v", err)
	}
	for _, a := range actions {
		if a.Kind != repoinit.ActionOK {
			t.Errorf("%s: expected ok on second run, got %s", a.Name, a.Kind)
		}
	}

	nestedAfter, _ := os.ReadFile(filepath.Join(dir, ".claude", ".gitignore"))
	rootAfter, _ := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if string(nestedBefore) != string(nestedAfter) {
		t.Errorf("nested .gitignore changed on second run:\nbefore:\n%s\nafter:\n%s", nestedBefore, nestedAfter)
	}
	if string(rootBefore) != string(rootAfter) {
		t.Errorf("root .gitignore changed on second run:\nbefore:\n%s\nafter:\n%s", rootBefore, rootAfter)
	}
}

// TestInit_PreExistingEffectiveRulesNoAppend covers repos whose root
// .gitignore already ignores tmp/ (via a wildcard) or .claude/.scratchpad/
// (via a bare, non-anchored rule) by effect — the pre-atomic /atomic-setup
// audit style. Init must recognize these as already effective (via
// git check-ignore) and append nothing anywhere.
func TestInit_PreExistingEffectiveRulesNoAppend(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)

	rootContent := "tmp/*\n.claude/.scratchpad/\n"
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(rootContent), 0644); err != nil {
		t.Fatal(err)
	}

	actions, err := repoinit.Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	byName := make(map[string]repoinit.ActionKind, len(actions))
	for _, a := range actions {
		byName[a.Name] = a.Kind
	}

	if byName["tmp/ ignored"] != repoinit.ActionOK {
		t.Errorf("tmp/ ignored: expected ok (already effective via tmp/*), got %s", byName["tmp/ ignored"])
	}
	if byName[".claude/.scratchpad/ ignored"] != repoinit.ActionOK {
		t.Errorf(".claude/.scratchpad/ ignored: expected ok (already effective via root rule), got %s", byName[".claude/.scratchpad/ ignored"])
	}

	// Both root guarantees (tmp/ via the wildcard, and nothing else lives at
	// root anymore) are satisfied, so the root file must survive byte-for-byte
	// untouched. The worktrees guarantee lives in the nested .claude/.gitignore.
	rootAfter, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	if string(rootAfter) != rootContent {
		t.Errorf("pre-existing root .gitignore content not preserved:\nbefore:\n%s\nafter:\n%s", rootContent, rootAfter)
	}

	// The nested .claude/.gitignore may still be created (the .atomic-index/
	// rule is not covered by the root content above), but it must never carry
	// a redundant /.scratchpad/ line.
	if data, err := os.ReadFile(filepath.Join(dir, ".claude", ".gitignore")); err == nil {
		if strings.Contains(string(data), "/.scratchpad/") {
			t.Errorf("nested .gitignore should not carry /.scratchpad/ when root already ignores it:\n%s", data)
		}
	}
}

// TestInit_NoGitDegradation covers a root that is not a git work tree at
// all: the effect check cannot run git check-ignore, so Init must degrade to
// a literal line-presence scan and still scaffold correctly and idempotently.
func TestInit_NoGitDegradation(t *testing.T) {
	dir := t.TempDir()

	actions, err := repoinit.Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if len(actions) != 7 {
		t.Fatalf("expected 7 actions, got %d: %+v", len(actions), actions)
	}
	for _, a := range actions {
		if a.Kind != repoinit.ActionCreated {
			t.Errorf("%s: expected created, got %s", a.Name, a.Kind)
		}
	}

	actions2, err := repoinit.Init(dir)
	if err != nil {
		t.Fatalf("second Init: %v", err)
	}
	for _, a := range actions2 {
		if a.Kind != repoinit.ActionOK {
			t.Errorf("%s: expected ok on second (degraded) run, got %s", a.Name, a.Kind)
		}
	}
}

// TestInit_AppendPreservesExistingContent asserts that appending a managed
// rule to a pre-existing .claude/.gitignore never rewrites, reorders, or
// drops any existing byte, and only inserts a separating newline when the
// file lacks a trailing one.
func TestInit_AppendPreservesExistingContent(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)

	if err := os.MkdirAll(filepath.Join(dir, ".claude"), 0755); err != nil {
		t.Fatal(err)
	}
	existing := "# custom user note\ncustom-rule/"
	if err := os.WriteFile(filepath.Join(dir, ".claude", ".gitignore"), []byte(existing), 0644); err != nil {
		t.Fatal(err)
	}

	if _, err := repoinit.Init(dir); err != nil {
		t.Fatalf("Init: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, ".claude", ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	if !strings.HasPrefix(got, existing+"\n") {
		t.Errorf("existing bytes not preserved byte-for-byte:\n%s", got)
	}
	if strings.Contains(got, "# managed by atomic repo init") {
		t.Errorf("managed header must not be added when file already existed:\n%s", got)
	}
	if !strings.Contains(got, "/.scratchpad/") || !strings.Contains(got, "/.atomic-index/") || !strings.Contains(got, "/worktrees/") {
		t.Errorf("managed rules not appended:\n%s", got)
	}
}

// TestInit_ActionOrderAndNames locks in the seven guarantees' order and
// names (the CLI output contract: one line per item).
func TestInit_ActionOrderAndNames(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)

	actions, err := repoinit.Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	wantNames := []string{
		".claude/.scratchpad/",
		".claude/project/",
		".claude/.scratchpad/ ignored",
		".claude/.atomic-index/ ignored",
		"tmp/ ignored",
		".claude/worktrees/ ignored",
		`.claude/atomic.toml scope="repo"`,
	}
	if len(actions) != len(wantNames) {
		t.Fatalf("expected %d actions, got %d: %+v", len(wantNames), len(actions), actions)
	}
	for i, name := range wantNames {
		if actions[i].Name != name {
			t.Errorf("action %d: got name %q, want %q", i, actions[i].Name, name)
		}
	}
}

// TestInit_MkdirErrorPropagates asserts I/O failures surface as an error
// rather than being silently swallowed.
func TestInit_MkdirErrorPropagates(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("running as root bypasses permission checks")
	}
	parent := t.TempDir()
	root := filepath.Join(parent, "locked")
	if err := os.Mkdir(root, 0555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(root, 0755) })

	if _, err := repoinit.Init(root); err == nil {
		t.Error("expected error creating .claude/.scratchpad under a read-only root, got nil")
	}
}

// TestInit_ScopeMarkerConflictErrors: a root already declaring
// scope = "realm" must never be silently rewritten to "repo" — Init errors
// and the marker file is left untouched.
func TestInit_ScopeMarkerConflictErrors(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)

	if err := os.MkdirAll(filepath.Join(dir, ".claude"), 0755); err != nil {
		t.Fatal(err)
	}
	existing := "scope = \"realm\"\n"
	markerPath := filepath.Join(dir, ".claude", "atomic.toml")
	if err := os.WriteFile(markerPath, []byte(existing), 0644); err != nil {
		t.Fatal(err)
	}

	if _, err := repoinit.Init(dir); err == nil {
		t.Fatal("expected error when root already declares scope=\"realm\"")
	}

	got, err := os.ReadFile(markerPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != existing {
		t.Errorf("conflicting marker must be left untouched:\ngot:\n%s\nwant:\n%s", got, existing)
	}
}

// TestInit_ScopeMarkerIdempotent: a second run reports the scope marker
// action as ok and writes nothing further to the marker file.
func TestInit_ScopeMarkerIdempotent(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)

	if _, err := repoinit.Init(dir); err != nil {
		t.Fatalf("first Init: %v", err)
	}

	actions, err := repoinit.Init(dir)
	if err != nil {
		t.Fatalf("second Init: %v", err)
	}
	last := actions[len(actions)-1]
	if last.Kind != repoinit.ActionOK {
		t.Errorf("scope marker action on second run: kind = %s, want ok", last.Kind)
	}

	got, err := os.ReadFile(filepath.Join(dir, ".claude", "atomic.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "scope = \"repo\"\n" {
		t.Errorf(".claude/atomic.toml = %q, want %q", got, "scope = \"repo\"\n")
	}
}
