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

// Pinned so the expected paths do not follow the built-in default.
const stateSeg = ".state"

func pinState(t *testing.T) {
	t.Helper()
	t.Cleanup(config.SetStateDirForTest(stateSeg))
}

func initGitRepo(t *testing.T, dir string) {
	t.Helper()
	cmd := exec.Command("git", "init", "-q", dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
}

func TestInit_ColdRepoScaffold(t *testing.T) {
	pinState(t)
	dir := t.TempDir()
	initGitRepo(t, dir)

	actions, err := repoinit.Init(dir, false)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if len(actions) != 8 {
		t.Fatalf("expected 8 actions, got %d: %+v", len(actions), actions)
	}
	for _, a := range actions {
		if a.Kind != repoinit.ActionCreated {
			t.Errorf("%s: expected created, got %s", a.Name, a.Kind)
		}
	}

	for _, rel := range []string{filepath.Join(stateSeg, ".scratchpad"), filepath.Join(stateSeg, "project")} {
		info, err := os.Stat(filepath.Join(dir, rel))
		if err != nil || !info.IsDir() {
			t.Errorf("%s: not created as a directory", rel)
		}
	}

	nestedIgnore, err := os.ReadFile(filepath.Join(dir, stateSeg, ".gitignore"))
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

	scopeMarker, err := os.ReadFile(filepath.Join(dir, stateSeg, "atomic.toml"))
	if err != nil {
		t.Fatalf("read %s/atomic.toml: %v", stateSeg, err)
	}
	if string(scopeMarker) != "scope = \"repo\"\n" {
		t.Errorf("%s/atomic.toml = %q, want %q", stateSeg, scopeMarker, "scope = \"repo\"\n")
	}
}

// `atomic docs scan` rewrites the doc-surfaces cache with a fresh timestamp on
// every run, so a tracked copy would churn every commit.
func TestInit_IgnoresDocSurfacesCache(t *testing.T) {
	pinState(t)
	dir := t.TempDir()
	initGitRepo(t, dir)

	if _, err := repoinit.Init(dir, false); err != nil {
		t.Fatalf("Init: %v", err)
	}
	cmd := exec.Command("git", "check-ignore", "-q", filepath.Join(stateSeg, "project", "doc-surfaces.md"))
	cmd.Dir = dir
	if err := cmd.Run(); err != nil {
		t.Errorf("doc-surfaces.md is not ignored after Init: %v", err)
	}
}

// gitIn runs git in dir with a throwaway identity and fails the test on error.
func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@t"}, args...)...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

// A rule cannot untrack a committed cache, so init names the one command that
// does and leaves the index for the user.
func TestInit_ReportsATrackedDocSurfacesCache(t *testing.T) {
	pinState(t)
	dir := t.TempDir()
	initGitRepo(t, dir)
	cache := filepath.Join(stateSeg, "project", "doc-surfaces.md")
	if err := os.MkdirAll(filepath.Join(dir, filepath.Dir(cache)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, cache), []byte("# cache\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", cache)
	gitIn(t, dir, "commit", "-q", "-m", "track cache")

	actions, err := repoinit.Init(dir, false)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	var got repoinit.Action
	for _, a := range actions {
		if a.Name == filepath.ToSlash(cache)+" ignored" {
			got = a
		}
	}
	if got.Kind != repoinit.ActionTracked || !strings.Contains(got.Detail, "git rm --cached "+filepath.ToSlash(cache)) {
		t.Errorf("cache action = %+v, want %s with a `git rm --cached` remediation", got, repoinit.ActionTracked)
	}
	if !strings.Contains(gitIn(t, dir, "ls-files"), filepath.ToSlash(cache)) {
		t.Error("Init changed the index; the cache must stay tracked until the user untracks it")
	}
	if rules, _ := os.ReadFile(filepath.Join(dir, stateSeg, ".gitignore")); !strings.Contains(string(rules), "/project/doc-surfaces.md\n") {
		t.Errorf("rule not written:\n%s", rules)
	}
}

// A rule already present but overridden by a later negation is not something
// appending can fix, so init names the cause instead of claiming it.
func TestInit_ReportsANegatedRule(t *testing.T) {
	pinState(t)
	dir := t.TempDir()
	initGitRepo(t, dir)
	if err := os.MkdirAll(filepath.Join(dir, stateSeg), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, stateSeg, ".gitignore"), []byte("/.scratchpad/\n!/.scratchpad/\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	actions, err := repoinit.Init(dir, false)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	for _, a := range actions {
		if a.Name == stateSeg+"/.scratchpad/ ignored" {
			if a.Kind != repoinit.ActionNegated || !strings.Contains(a.Detail, "negat") {
				t.Errorf("scratchpad action = %+v, want %s naming the negation", a, repoinit.ActionNegated)
			}
			return
		}
	}
	t.Fatalf("no scratchpad action in %+v", actions)
}

// Every guarantee nests under the resolved state root, so a ".pi" segment
// scaffolds .pi/... and never the built-in default's directory.
func TestInit_ColdRepoScaffold_UnderNonDefaultStateDir(t *testing.T) {
	dir := t.TempDir()
	builtinState := filepath.Dir(config.ProjectDir(dir))

	restore := config.SetStateDirForTest(".pi")
	defer restore()

	initGitRepo(t, dir)

	actions, err := repoinit.Init(dir, false)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if len(actions) != 8 {
		t.Fatalf("expected 8 actions, got %d: %+v", len(actions), actions)
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
		t.Errorf("nested .gitignore missing state-root-aware managed header:\n%s", nested)
	}
	if !strings.Contains(nested, "/.scratchpad/") || !strings.Contains(nested, "/.atomic-index/") || !strings.Contains(nested, "/worktrees/") {
		t.Errorf("nested .gitignore missing managed rules:\n%s", nested)
	}

	if _, err := os.Stat(builtinState); !os.IsNotExist(err) {
		t.Errorf("%s should not exist under a .pi state segment, stat err=%v", builtinState, err)
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
	pinState(t)
	dir := t.TempDir()
	initGitRepo(t, dir)

	if _, err := repoinit.Init(dir, false); err != nil {
		t.Fatalf("first Init: %v", err)
	}

	nestedBefore, _ := os.ReadFile(filepath.Join(dir, stateSeg, ".gitignore"))
	rootBefore, _ := os.ReadFile(filepath.Join(dir, ".gitignore"))

	actions, err := repoinit.Init(dir, false)
	if err != nil {
		t.Fatalf("second Init: %v", err)
	}
	for _, a := range actions {
		if a.Kind != repoinit.ActionOK {
			t.Errorf("%s: expected ok on second run, got %s", a.Name, a.Kind)
		}
	}

	nestedAfter, _ := os.ReadFile(filepath.Join(dir, stateSeg, ".gitignore"))
	rootAfter, _ := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if string(nestedBefore) != string(nestedAfter) {
		t.Errorf("nested .gitignore changed on second run:\nbefore:\n%s\nafter:\n%s", nestedBefore, nestedAfter)
	}
	if string(rootBefore) != string(rootAfter) {
		t.Errorf("root .gitignore changed on second run:\nbefore:\n%s\nafter:\n%s", rootBefore, rootAfter)
	}
}

// A rule that ignores by effect — a wildcard, or a bare non-anchored path —
// counts as satisfied; Init must append nothing.
func TestInit_PreExistingEffectiveRulesNoAppend(t *testing.T) {
	pinState(t)
	dir := t.TempDir()
	initGitRepo(t, dir)

	scratchName := stateSeg + "/.scratchpad/"
	rootContent := "tmp/*\n" + scratchName + "\n"
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(rootContent), 0644); err != nil {
		t.Fatal(err)
	}

	actions, err := repoinit.Init(dir, false)
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
	ignoredName := scratchName + " ignored"
	if byName[ignoredName] != repoinit.ActionOK {
		t.Errorf("%s: expected ok (already effective via root rule), got %s", ignoredName, byName[ignoredName])
	}

	// tmp/ is the only root guarantee left, and the wildcard satisfies it.
	rootAfter, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	if string(rootAfter) != rootContent {
		t.Errorf("pre-existing root .gitignore content not preserved:\nbefore:\n%s\nafter:\n%s", rootContent, rootAfter)
	}

	// The nested file may still be created for .atomic-index/, but must never
	// carry a redundant /.scratchpad/ line.
	if data, err := os.ReadFile(filepath.Join(dir, stateSeg, ".gitignore")); err == nil {
		if strings.Contains(string(data), "/.scratchpad/") {
			t.Errorf("nested .gitignore should not carry /.scratchpad/ when root already ignores it:\n%s", data)
		}
	}
}

// Outside a git work tree the effect check degrades to a literal line scan and
// must still scaffold idempotently.
func TestInit_NoGitDegradation(t *testing.T) {
	dir := t.TempDir()

	actions, err := repoinit.Init(dir, false)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if len(actions) != 8 {
		t.Fatalf("expected 8 actions, got %d: %+v", len(actions), actions)
	}
	for _, a := range actions {
		if a.Kind != repoinit.ActionCreated {
			t.Errorf("%s: expected created, got %s", a.Name, a.Kind)
		}
	}

	actions2, err := repoinit.Init(dir, false)
	if err != nil {
		t.Fatalf("second Init: %v", err)
	}
	for _, a := range actions2 {
		if a.Kind != repoinit.ActionOK {
			t.Errorf("%s: expected ok on second (degraded) run, got %s", a.Name, a.Kind)
		}
	}
}

// Appending never rewrites, reorders, or drops a byte, and inserts a newline
// only when the file lacks a trailing one.
func TestInit_AppendPreservesExistingContent(t *testing.T) {
	pinState(t)
	dir := t.TempDir()
	initGitRepo(t, dir)

	if err := os.MkdirAll(filepath.Join(dir, stateSeg), 0755); err != nil {
		t.Fatal(err)
	}
	existing := "# custom user note\ncustom-rule/"
	if err := os.WriteFile(filepath.Join(dir, stateSeg, ".gitignore"), []byte(existing), 0644); err != nil {
		t.Fatal(err)
	}

	if _, err := repoinit.Init(dir, false); err != nil {
		t.Fatalf("Init: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, stateSeg, ".gitignore"))
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

// Action order and names are the CLI output contract: one line per item.
func TestInit_ActionOrderAndNames(t *testing.T) {
	pinState(t)
	dir := t.TempDir()
	initGitRepo(t, dir)

	actions, err := repoinit.Init(dir, false)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	wantNames := []string{
		stateSeg + "/.scratchpad/",
		stateSeg + "/project/",
		stateSeg + "/.scratchpad/ ignored",
		stateSeg + "/.atomic-index/ ignored",
		stateSeg + "/project/doc-surfaces.md ignored",
		"tmp/ ignored",
		stateSeg + "/worktrees/ ignored",
		stateSeg + `/atomic.toml scope="repo"`,
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

	if _, err := repoinit.Init(root, false); err == nil {
		t.Error("expected error creating the state scratchpad under a read-only root, got nil")
	}
}

// An existing scope = "realm" must never be silently rewritten to "repo".
func TestInit_ScopeMarkerConflictErrors(t *testing.T) {
	pinState(t)
	dir := t.TempDir()
	initGitRepo(t, dir)

	if err := os.MkdirAll(filepath.Join(dir, stateSeg), 0755); err != nil {
		t.Fatal(err)
	}
	existing := "scope = \"realm\"\n"
	markerPath := filepath.Join(dir, stateSeg, "atomic.toml")
	if err := os.WriteFile(markerPath, []byte(existing), 0644); err != nil {
		t.Fatal(err)
	}

	if _, err := repoinit.Init(dir, false); err == nil {
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

func TestInit_ScopeMarkerIdempotent(t *testing.T) {
	pinState(t)
	dir := t.TempDir()
	initGitRepo(t, dir)

	if _, err := repoinit.Init(dir, false); err != nil {
		t.Fatalf("first Init: %v", err)
	}

	actions, err := repoinit.Init(dir, false)
	if err != nil {
		t.Fatalf("second Init: %v", err)
	}
	last := actions[len(actions)-1]
	if last.Kind != repoinit.ActionOK {
		t.Errorf("scope marker action on second run: kind = %s, want ok", last.Kind)
	}

	got, err := os.ReadFile(filepath.Join(dir, stateSeg, "atomic.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "scope = \"repo\"\n" {
		t.Errorf("%s/atomic.toml = %q, want %q", stateSeg, got, "scope = \"repo\"\n")
	}
}

func selectStateDir(t *testing.T, repo, stateDir string) {
	t.Helper()
	if _, err := config.PersistStateSelection(repo, stateDir); err != nil {
		t.Fatalf("PersistStateSelection: %v", err)
	}
}

func readFile(t *testing.T, parts ...string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(parts...))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func exists(parts ...string) bool {
	_, err := os.Stat(filepath.Join(parts...))
	return err == nil
}

// A selection record naming a custom root decides where every guarantee lands;
// the built-in default directory is never created beside it.
func TestInit_HonorsSelectionRecord(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)
	selectStateDir(t, dir, "custom/state")

	if _, err := repoinit.Init(dir, false); err != nil {
		t.Fatalf("Init: %v", err)
	}

	nested := readFile(t, dir, "custom", "state", ".gitignore")
	for _, rule := range []string{"/.scratchpad/", "/.atomic-index/", "/worktrees/"} {
		if !strings.Contains(nested, rule) {
			t.Errorf("custom root .gitignore missing %s:\n%s", rule, nested)
		}
	}
	if !strings.Contains(readFile(t, dir, "custom", "state", "atomic.toml"), `scope = "repo"`) {
		t.Error("scope marker not written under the custom root")
	}
	if exists(dir, ".atomic") {
		t.Error(".atomic must not be created when the record names another root")
	}
}

// An OMP-only repository gains no .claude/; an existing one, or an enrolled
// Claude target, gets /worktrees/ because Claude's worktrees live there.
func TestInit_ClaudeWorktreeRuleOnlyWhenClaudeInPlay(t *testing.T) {
	t.Run("omp only", func(t *testing.T) {
		pinState(t)
		dir := t.TempDir()
		initGitRepo(t, dir)
		if _, err := repoinit.Init(dir, false); err != nil {
			t.Fatal(err)
		}
		if exists(dir, ".claude") {
			t.Error("an OMP-only repository must not gain .claude/")
		}
	})

	t.Run("claude directory present", func(t *testing.T) {
		pinState(t)
		dir := t.TempDir()
		initGitRepo(t, dir)
		if err := os.Mkdir(filepath.Join(dir, ".claude"), 0o755); err != nil {
			t.Fatal(err)
		}
		if _, err := repoinit.Init(dir, false); err != nil {
			t.Fatal(err)
		}
		if got := readFile(t, dir, ".claude", ".gitignore"); !strings.Contains(got, "/worktrees/") {
			t.Errorf(".claude/.gitignore missing /worktrees/:\n%s", got)
		}
	})

	t.Run("claude enrolled", func(t *testing.T) {
		pinState(t)
		dir := t.TempDir()
		initGitRepo(t, dir)
		if _, err := repoinit.Init(dir, true); err != nil {
			t.Fatal(err)
		}
		if got := readFile(t, dir, ".claude", ".gitignore"); !strings.Contains(got, "/worktrees/") {
			t.Errorf(".claude/.gitignore missing /worktrees/:\n%s", got)
		}
	})
}

// A linked worktree under either harness location must not surface as an
// untracked directory, which is what makes it safe to create one in place.
func TestInit_LinkedWorktreeNeverUntracked(t *testing.T) {
	run := func(t *testing.T, dir string, args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@t"}, args...)...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return string(out)
	}

	cases := []struct {
		name, state, worktreeIn string
		claudeEnrolled          bool
	}{
		{"atomic state", ".atomic", ".atomic", false},
		{"claude state", ".claude", ".claude", false},
		{"claude worktree under atomic state", ".atomic", ".claude", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			initGitRepo(t, dir)
			run(t, dir, "commit", "--allow-empty", "-q", "-m", "init")
			selectStateDir(t, dir, tc.state)
			if _, err := repoinit.Init(dir, tc.claudeEnrolled); err != nil {
				t.Fatal(err)
			}

			run(t, dir, "worktree", "add", "-q", filepath.Join(dir, tc.worktreeIn, "worktrees", "wt"), "-b", "wt")

			status := run(t, dir, "status", "--porcelain", "--untracked-files=all")
			if strings.Contains(status, "worktrees") {
				t.Errorf("linked worktree shows in status:\n%s", status)
			}
		})
	}
}

// A state root outside the repository is not the repository's to ignore: no
// rule for it reaches the repository .gitignore, and no .claude/ appears.
func TestInit_StateRootOutsideRepositoryGetsNoIgnoreRule(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)
	outside := t.TempDir()
	selectStateDir(t, dir, outside)

	actions, err := repoinit.Init(dir, false)
	if err != nil {
		t.Fatal(err)
	}

	if got := readFile(t, dir, ".gitignore"); got != "tmp/\n" {
		t.Errorf("repository .gitignore = %q, want only tmp/", got)
	}
	if exists(dir, ".claude") {
		t.Error("an outside state root must not create .claude/")
	}
	if exists(outside, ".gitignore") {
		t.Error("no ignore file belongs in a state root outside the repository")
	}
	if !exists(outside, ".scratchpad") || !exists(outside, "project") || !exists(outside, "atomic.toml") {
		t.Errorf("state not scaffolded at the outside root; actions: %+v", actions)
	}
}
