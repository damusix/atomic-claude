package repoinit_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

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
	if len(actions) != 6 {
		t.Fatalf("expected 6 actions, got %d: %+v", len(actions), actions)
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
	if !strings.Contains(nested, "/.scratchpad/") || !strings.Contains(nested, "/.atomic-index/") {
		t.Errorf("nested .gitignore missing managed rules:\n%s", nested)
	}

	rootIgnore, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		t.Fatalf("read root .gitignore: %v", err)
	}
	root := string(rootIgnore)
	if !strings.Contains(root, "tmp/") || !strings.Contains(root, ".worktrees/") {
		t.Errorf("root .gitignore missing managed rules:\n%s", root)
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

	// The pre-existing lines must survive byte-for-byte at the head of the
	// file; .worktrees/ is a separate, still-unsatisfied guarantee and is
	// expected to be appended after them.
	rootAfter, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(rootAfter), rootContent) {
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
	if len(actions) != 6 {
		t.Fatalf("expected 6 actions, got %d: %+v", len(actions), actions)
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
	if !strings.Contains(got, "/.scratchpad/") || !strings.Contains(got, "/.atomic-index/") {
		t.Errorf("managed rules not appended:\n%s", got)
	}
}

// TestInit_ActionOrderAndNames locks in the six guarantees' order and names
// (the CLI output contract: one line per item).
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
		".worktrees/ ignored",
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
