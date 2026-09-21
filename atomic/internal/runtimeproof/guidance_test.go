// CP6A scenario matrix: the end-to-end verification of Claude and OMP project
// and nested guidance over the built adapters. Each runtime row launches the
// real harness against an isolated home whose steering the adapters themselves
// generated, and retains the observation it collected; each deterministic row
// drives the same adapters over a real repository shape (a linked Git worktree,
// a non-Git realm) and asserts the write landed where the invoked scope is.
//
// The Claude rows are startup observations: the runner strips every model
// credential, so the launch reaches startup, the instruction loader runs, and
// the run ends at authentication instead of issuing a request. The OMP rows
// follow CP4B2's credential convention: a row that needs the model to choose an
// operation skips loudly when CP0_OMP_AUTH_HOME or CP0_OMP_API_KEY is unset.
package runtimeproof

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/harness"
	"github.com/damusix/atomic-claude/atomic/internal/harness/claude"
	"github.com/damusix/atomic-claude/atomic/internal/harness/omp"
	"github.com/damusix/atomic-claude/atomic/internal/managedfile"
	"github.com/damusix/atomic-claude/atomic/internal/wiki"
)

// The scope guidance the matrix projects. The bodies are markers, not the
// shipped wiki steering: the rows assert delivery paths and counts, and a
// distinctive body keeps a stray copy easy to spot in a retained observation.
const (
	guidanceRepoScope = "## Repository guidance\n\n- the repository root scope\n"
	guidanceWikiScope = "## Nested wiki guidance\n\n- the nested wiki scope\n"
)

// claudeGuidanceHome prepares one isolated Claude home whose global steering is
// the adapter's own projection and whose repository carries the adapter's own
// loader pairs for the repository root and the nested wiki. It returns the
// isolated home, the native configuration root, and the repository root.
func claudeGuidanceHome(t *testing.T) (home, configDir, repo string) {
	t.Helper()
	root := t.TempDir()
	// macOS reports temp roots through the /var symlink while Claude reports the
	// resolved /private/var path, so resolve the root once and build every
	// expected path from the canonical form.
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	home = filepath.Join(root, "home")
	configDir = filepath.Join(home, ".claude")
	repo = filepath.Join(root, "repo")
	writeTree(t, repo, map[string]string{
		"docs/wiki/index.md": "# index\n",
		"nested/target.ts":   "export const x = 1;\n",
	})
	// The authored global contract imports the mutable profile, so the isolated
	// home carries one: the row then proves the global steering and its import
	// both reach the loader.
	writeTree(t, home, map[string]string{
		".atomic/profile.md": "# Runtimeproof profile\n",
	})

	if _, err := claude.Migrate(claude.MigrationRequest{
		Home:        home,
		NativeRoot:  configDir,
		Target:      "claude:" + configDir,
		Consumer:    "claude:" + configDir,
		Generation:  "gen-cp6a",
		Tier:        "instruction-only",
		OperationID: "cp6a-guidance",
		// The isolated home has no legacy pre-install snapshot, which is the
		// absence the acknowledgement accepts.
		AcknowledgeSnapshot: true,
		Scopes: []claude.Scope{
			{Dir: repo, Guidance: []byte(guidanceRepoScope)},
			{Dir: filepath.Join(repo, "docs", "wiki"), Guidance: []byte(guidanceWikiScope)},
		},
	}); err != nil {
		t.Fatalf("adapter migration: %v", err)
	}
	return home, configDir, repo
}

// claudeLaunch builds one startup-only launch against a prepared guidance home.
func claudeLaunch(t *testing.T, binary, home, configDir, workDir, name string) Scenario {
	t.Helper()
	root := filepath.Dir(home)
	eventLog := filepath.Join(root, name+".jsonl")
	return Scenario{
		Name:      name,
		Harness:   binary,
		Home:      home,
		ConfigDir: configDir,
		WorkDir:   workDir,
		EventLog:  eventLog,
		Timeout:   60 * time.Second,
	}
}

// claudeGuidanceCounts indexes the instruction files one observation reported
// and how many times each loaded, so a duplicate delivery fails a row instead
// of passing unnoticed.
func claudeGuidanceCounts(obs Observation) map[string]int {
	counts := map[string]int{}
	for _, e := range obs.Events {
		if e.Name != "InstructionsLoaded" || e.FilePath == "" {
			continue
		}
		counts[filepath.Clean(e.FilePath)]++
	}
	return counts
}

// claudeSessionID reads the session identifier Claude reports in its JSON print
// output, so a resume row consumes the id its own create launch produced and
// never a hard-coded one.
func claudeSessionID(stdout string) string {
	start := strings.Index(stdout, "{")
	if start < 0 {
		return ""
	}
	var payload struct {
		SessionID string `json:"session_id"`
	}
	if err := json.Unmarshal([]byte(stdout[start:]), &payload); err != nil {
		return ""
	}
	return payload.SessionID
}

// TestClaudeGuidanceRuntimeMatrix drives the real Claude binary against an
// isolated adapter-generated steering tree and retains one observation per row.
// It proves the CP0 loading rows the adapter depends on — global direct
// steering, the repository loader pair, and the nested loader pair all load,
// each exactly once, with the nested pair absent until the working directory
// reaches it — and records the resumed and child rows honestly: a resumed
// session rediscovers the same guidance, while a child session remains
// unsupported because the startup-only runner cannot reach a Task call.
//
// The nested row also records a boundary: an ancestor scope's import resolves
// outside the working directory, so Claude treats it as an external import and
// gates it behind interactive approval. Per-scope delivery is proven; delivery
// of an ancestor scope into a nested non-interactive session is not, and the row
// asserts the observed state so a version that changes it fails loudly.
func TestClaudeGuidanceRuntimeMatrix(t *testing.T) {
	if testing.Short() {
		t.Skip("real harness launch")
	}
	binary, err := ClaudeCLI()
	if err != nil {
		t.Skipf("claude not available: %v", err)
	}

	home, configDir, repo := claudeGuidanceHome(t)
	globalPath := filepath.Join(configDir, "CLAUDE.md")
	repoLoader := filepath.Join(repo, "CLAUDE.md")
	repoShared := filepath.Join(repo, "AGENTS.md")
	wikiLoader := filepath.Join(repo, "docs", "wiki", "CLAUDE.md")
	wikiShared := filepath.Join(repo, "docs", "wiki", "AGENTS.md")
	profilePath := filepath.Join(home, ".atomic", "profile.md")

	for _, path := range []string{globalPath, repoLoader, repoShared, wikiLoader, wikiShared, profilePath} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("adapter did not write %s: %v", path, err)
		}
	}

	// Row claude_global_direct + claude_repo_pair: starting at the repository
	// root loads the direct global steering once, the root loader once, and the
	// root guidance once through the adjacent import. The nested wiki pair stays
	// unloaded: CP0 observed nested steering only once the working directory
	// reached it.
	rootObs, err := Run(context.Background(), claudeLaunch(t, binary, home, configDir, repo, "claude-root"))
	if errors.Is(err, ErrTimeout) {
		t.Fatalf("claude exceeded its bound: %+v", rootObs)
	}
	if err != nil {
		t.Fatalf("root Run: %v", err)
	}
	rootCounts := claudeGuidanceCounts(rootObs)
	for path, want := range map[string]int{globalPath: 1, profilePath: 1, repoLoader: 1, repoShared: 1} {
		if got := rootCounts[path]; got != want {
			t.Errorf("root session loaded %s %d times, want %d; all loads: %v", path, got, want, sortedCounts(rootCounts))
		}
	}
	for _, path := range []string{wikiLoader, wikiShared} {
		if got := rootCounts[path]; got != 0 {
			t.Errorf("starting at the repository root loaded the nested %s %d times, want 0", path, got)
		}
	}
	if parent := claudeParentOf(rootObs, repoShared); parent != repoLoader {
		t.Errorf("root guidance loaded with parent %q, want the adjacent loader %q", parent, repoLoader)
	}
	if parent := claudeParentOf(rootObs, profilePath); parent != globalPath {
		t.Errorf("the profile import loaded with parent %q, want the global steering %q", parent, globalPath)
	}

	// Row claude_nested_pair: starting inside the nested wiki loads the root
	// CLAUDE.md by upward discovery and the nested pair from the working
	// directory, each once.
	//
	// The root shared file stays unloaded: Claude resolves an `@import` against
	// the importing file, and when that resolves outside the working directory
	// it is an external import that Claude gates behind an interactive approval.
	// A non-interactive nested session therefore never sees it. That is Claude's
	// documented guard, not an adapter choice — the loader's bytes are correct
	// and the nested loader that lives in the working directory does deliver.
	nestedObs, err := Run(context.Background(), claudeLaunch(t, binary, home, configDir, filepath.Join(repo, "docs", "wiki"), "claude-nested"))
	if errors.Is(err, ErrTimeout) {
		t.Fatalf("claude exceeded its bound: %+v", nestedObs)
	}
	if err != nil {
		t.Fatalf("nested Run: %v", err)
	}
	nestedCounts := claudeGuidanceCounts(nestedObs)
	for path, want := range map[string]int{globalPath: 1, profilePath: 1, repoLoader: 1, wikiLoader: 1, wikiShared: 1} {
		if got := nestedCounts[path]; got != want {
			t.Errorf("nested session loaded %s %d times, want %d; all loads: %v", path, got, want, sortedCounts(nestedCounts))
		}
	}
	if got := nestedCounts[repoShared]; got != 0 {
		t.Errorf("nested session loaded the ancestor shared file %s %d times, want 0: the import is external to the working directory and Claude gates it; re-check the boundary before claiming delivery", repoShared, got)
	}
	if parent := claudeParentOf(nestedObs, wikiShared); parent != wikiLoader {
		t.Errorf("nested guidance loaded with parent %q, want the adjacent loader %q", parent, wikiLoader)
	}

	// Row claude_resume: resuming the session id the create launch reported
	// rediscovers the same guidance, each file once, and reports a resume start.
	t.Run("claude_resume", func(t *testing.T) {
		sessionID := claudeSessionID(rootObs.Stdout)
		if sessionID == "" {
			t.Skipf("the launch reported no session id, so resume cannot be exercised: exit %d\n%s", rootObs.ExitCode, rootObs.Stderr)
		}
		resumeScenario := claudeLaunch(t, binary, home, configDir, repo, "claude-resume")
		resumeScenario.Args = []string{"-p", "--output-format", "json", "--resume", sessionID, DefaultPrompt}
		resumeObs, err := Run(context.Background(), resumeScenario)
		if errors.Is(err, ErrTimeout) {
			t.Fatalf("claude exceeded its bound: %+v", resumeObs)
		}
		if err != nil {
			t.Fatalf("resume Run: %v", err)
		}
		resumeCounts := claudeGuidanceCounts(resumeObs)
		for path, want := range map[string]int{globalPath: 1, profilePath: 1, repoLoader: 1, repoShared: 1} {
			if got := resumeCounts[path]; got != want {
				t.Errorf("resumed session loaded %s %d times, want %d; all loads: %v", path, got, want, sortedCounts(resumeCounts))
			}
		}
		if !observedResume(resumeObs) {
			t.Errorf("the resume launch reported no SessionStart source=resume: %+v", resumeObs.Events)
		}
		t.Logf("claude %s; resume loads %v (session %s)", binary, sortedCounts(resumeCounts), sessionID)
	})

	// Row claude_child: CP0 records Claude child-session delivery as unsupported
	// because no Task call was reached, and the startup-only runner cannot reach
	// one either — a child exists only after the model chooses the Task tool.
	// The row therefore reports the limitation instead of claiming delivery.
	t.Run("claude_child_unsupported", func(t *testing.T) {
		t.Skip("CP0 lists Claude child-session delivery unsupported: a child needs a model Task call, which the startup-only runner cannot issue")
	})

	t.Logf("claude %s; root loads %v; nested loads %v; resume row reports its own loads", binary,
		sortedCounts(rootCounts), sortedCounts(nestedCounts))
}

// sortedCounts renders a count map deterministically for a retained log.
func sortedCounts(counts map[string]int) []string {
	out := make([]string, 0, len(counts))
	for path, count := range counts {
		out = append(out, path+"="+strconv.Itoa(count))
	}
	sort.Strings(out)
	return out
}

// claudeParentOf returns the parent_file_path the observation reported for one
// loaded file, or "" when the file loaded without a parent.
func claudeParentOf(obs Observation, path string) string {
	for _, e := range obs.Events {
		if e.Name == "InstructionsLoaded" && filepath.Clean(e.FilePath) == path {
			return filepath.Clean(e.ParentFile)
		}
	}
	return ""
}

// observedResume reports whether any SessionStart event named a resume source.
func observedResume(obs Observation) bool {
	for _, e := range obs.Events {
		if e.Name == "SessionStart" && e.Source == "resume" {
			return true
		}
	}
	return false
}

// guidanceGit runs git in dir with a synthetic identity, so a fixture commit
// never depends on the developer's global git configuration.
func guidanceGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=runtimeproof", "GIT_AUTHOR_EMAIL=runtimeproof@example.invalid",
		"GIT_COMMITTER_NAME=runtimeproof", "GIT_COMMITTER_EMAIL=runtimeproof@example.invalid",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v in %s: %v: %s", args, dir, err, out)
	}
}

// TestClaudeGuidanceWorktreeWrites proves an invocation inside a linked Git
// worktree writes its scope guidance into that worktree while the
// project-keyed state home stays shared with the main checkout. The worktree is
// a real `git worktree add`, so the `.git` file and the project-key resolution
// follow the shape production sees rather than a fabricated one.
func TestClaudeGuidanceWorktreeWrites(t *testing.T) {
	root := t.TempDir()
	main := filepath.Join(root, "main")
	worktree := filepath.Join(root, "linked")
	if err := os.MkdirAll(main, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTree(t, main, map[string]string{"README.md": "main checkout\n"})
	guidanceGit(t, main, "init", "-q", "-b", "main")
	guidanceGit(t, main, "add", "-A")
	guidanceGit(t, main, "commit", "-q", "-m", "seed")
	guidanceGit(t, main, "worktree", "add", "-q", "-b", "linked-branch", worktree)
	// The invoked worktree carries pre-existing maintainer prose with a relative
	// reference; the migration must keep it byte-identical and only add its block.
	if err := materialize(worktree, filepath.Join("testdata", "guidance", "worktree")); err != nil {
		t.Fatal(err)
	}
	priorClaude := readFileString(t, filepath.Join(worktree, "CLAUDE.md"))

	stateHome := t.TempDir()
	restore := config.SetHomeDirForTest(stateHome)
	defer restore()
	if mainKey, wtKey := config.ProjectStateDir(main), config.ProjectStateDir(worktree); mainKey != wtKey {
		t.Fatalf("project-keyed state diverged: main %q vs worktree %q", mainKey, wtKey)
	}

	home := filepath.Join(root, "atomic-home")
	nativeRoot := filepath.Join(home, ".claude")
	if _, err := claude.MigrateScope(claude.ScopeRequest{
		Home:        home,
		NativeRoot:  nativeRoot,
		Target:      "claude:" + nativeRoot,
		OperationID: "cp6a-worktree",
		Scope:       claude.Scope{Dir: worktree, Guidance: []byte(guidanceRepoScope)},
	}); err != nil {
		t.Fatalf("worktree scope migration: %v", err)
	}

	for _, name := range []string{"CLAUDE.md", "AGENTS.md"} {
		if _, err := os.Stat(filepath.Join(worktree, name)); err != nil {
			t.Errorf("scope pair did not land in the invoked worktree: %s: %v", name, err)
		}
		if _, err := os.Stat(filepath.Join(main, name)); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("the main checkout gained %s from a worktree invocation: %v", name, err)
		}
	}
	if got := readFileString(t, filepath.Join(worktree, "CLAUDE.md")); !strings.HasPrefix(got, priorClaude) {
		t.Errorf("the worktree migration rewrote unowned prose:\n%s", got)
	}
	if !strings.Contains(readFileString(t, filepath.Join(worktree, "CLAUDE.md")), "@NOTES.md") {
		t.Error("the worktree migration dropped the unowned relative reference")
	}
}

// TestClaudeGuidanceNonGitRealm proves realm initialization starts from a
// non-Git root: the realm root stays outside version control while the nested
// wiki becomes its own repository, and both loader pairs are created without
// touching the realm's existing scaffold.
func TestClaudeGuidanceNonGitRealm(t *testing.T) {
	realm := filepath.Join(t.TempDir(), "realm")
	if err := os.MkdirAll(realm, 0o755); err != nil {
		t.Fatal(err)
	}
	// The committed realm fixture carries the pre-existing wiki scaffold and the
	// realm root's own shared guidance, so the scenario proves the migration
	// splices around them instead of overwriting.
	if err := materialize(realm, filepath.Join("testdata", "guidance", "realm")); err != nil {
		t.Fatal(err)
	}
	realmAgentsBefore := readFileString(t, filepath.Join(realm, "AGENTS.md"))
	if _, err := wiki.Scan(realm, wiki.Options{Clock: func() time.Time { return time.Unix(0, 0).UTC() }}); err != nil {
		t.Fatalf("realm scan: %v", err)
	}
	if _, err := wiki.InitRealmScope(realm); err != nil {
		t.Fatalf("realm scaffold: %v", err)
	}
	wikiDir := filepath.Join(realm, "wiki")
	guidanceGit(t, wikiDir, "init", "-q", "-b", "main")

	home := filepath.Join(t.TempDir(), "atomic-home")
	nativeRoot := filepath.Join(home, ".claude")
	for _, scope := range []claude.Scope{
		{Dir: realm, Guidance: []byte("## Realm capture\n")},
		{Dir: wikiDir, Guidance: []byte(guidanceWikiScope)},
	} {
		if _, err := claude.MigrateScope(claude.ScopeRequest{
			Home:        home,
			NativeRoot:  nativeRoot,
			Target:      "claude:" + nativeRoot,
			OperationID: "cp6a-realm",
			Scope:       scope,
		}); err != nil {
			t.Fatalf("realm scope %s: %v", scope.Dir, err)
		}
	}

	if _, err := os.Stat(filepath.Join(realm, ".git")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("realm initialization created a Git repository at the realm root: %v", err)
	}
	for _, dir := range []string{realm, wikiDir} {
		for _, name := range []string{"CLAUDE.md", "AGENTS.md"} {
			path := filepath.Join(dir, name)
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("realm pair %s: %v", path, err)
			}
			if !managedfile.HasBlock(data) {
				t.Errorf("%s carries no Atomic block", path)
			}
		}
	}
	if got := readFileString(t, filepath.Join(realm, "AGENTS.md")); !strings.HasPrefix(got, realmAgentsBefore) {
		t.Errorf("the realm migration rewrote the realm root's shared guidance:\n%s", got)
	}
	// The realm wiki pair keeps its pre-existing relative import in the shared
	// AGENTS.md the loader imports, so cd'ing into wiki/ still auto-loads index.md.
	if data, err := os.ReadFile(filepath.Join(wikiDir, "AGENTS.md")); err != nil {
		t.Fatal(err)
	} else if !strings.Contains(string(data), "@index.md") {
		t.Errorf("the wiki steering dropped its pre-existing relative import: %q", data)
	}
	if data, err := os.ReadFile(filepath.Join(wikiDir, "CLAUDE.md")); err != nil {
		t.Fatal(err)
	} else if !strings.Contains(string(data), "@AGENTS.md") {
		t.Errorf("the wiki loader does not import the adjacent AGENTS.md: %q", data)
	}
}

// TestOMPGuidanceRuntimeMatrix drives the real OMP binary against an isolated
// home carrying the generated extension and proves the native equivalents the
// OMP project/nested guidance rests on: the bounded session-baseline block
// (marked with the CP0-proven runtime marker) reaches the provider payload,
// its index carries rule identity in canonical order rather than bodies, and
// the session reports covered versus uncovered tools.
//
// The one model-gated row — exact-path matching — follows CP4B2's credential
// convention and skips loudly without one.
func TestOMPGuidanceRuntimeMatrix(t *testing.T) {
	if testing.Short() {
		t.Skip("real harness launch")
	}
	binary, err := OMPCLI()
	if err != nil {
		t.Skipf("omp is unavailable: %v", err)
	}

	dir := t.TempDir()
	home, _ := prepareOMPHome(t, filepath.Join(dir, "home"), OMPHomeRequest{})
	repo := filepath.Join(dir, "repo")
	writeTree(t, repo, map[string]string{
		"src/a.ts":       "export const a = 1;\n",
		"docs/spec/x.md": "# spec\n",
	})

	scenario, err := home.Launch(OMPLaunch{Name: "omp-guidance", WorkDir: repo, Prompt: "Reply OK.", Log: home.LogPath("atomic-runtime-guidance.jsonl")})
	if err != nil {
		t.Fatalf("launch: %v", err)
	}
	scenario.Args = append([]string{"--tools", "read,bash"}, scenario.Args...)
	obs, err := Run(context.Background(), scenario)
	if errors.Is(err, ErrTimeout) {
		t.Fatalf("omp exceeded its bound: %+v", obs)
	}
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	log, err := OMPRuntimeLog(obs, "atomic-runtime-guidance.jsonl")
	if err != nil {
		t.Fatalf("parse runtime log: %v", err)
	}

	// The session-baseline block is the OMP native steering equivalent. The
	// module records whether the block it returned reached the provider payload,
	// which is the marker CP0 proved for `before_agent_start`.
	if !log.BaselineConsumed {
		t.Fatalf("the Atomic steering block did not reach the provider payload: %+v\n%s", log.Records, obs.Stderr)
	}
	if strings.Join(log.Index, ",") != "shipped:rules/docs/spec.md,shipped:rules/ts/style.md" {
		t.Errorf("the session index = %v, want the projected rule identities in canonical order", log.Index)
	}

	// The delivered index is an identity index: bodies never ride the baseline,
	// because CP0 proved no context-return result for the current operation. The
	// generated module carries no rule body at all, so no handler could return
	// one before an operation.
	delivery := runtimeDelivery(t, nil)
	module, err := delivery.RenderExtension()
	if err != nil {
		t.Fatalf("render extension: %v", err)
	}
	if !strings.Contains(string(module), omp.RuntimeMarker) {
		t.Errorf("the delivered block does not carry the CP0-proven runtime marker %q", omp.RuntimeMarker)
	}
	for _, body := range []string{"# TypeScript", "# Specs"} {
		if strings.Contains(delivery.IndexText, body) {
			t.Errorf("the session baseline carries rule body %q, want identity, digest, and globs only", body)
		}
		if strings.Contains(string(module), body) {
			t.Errorf("the generated module carries rule body %q, so a handler could return it before an operation", body)
		}
	}

	// Tool coverage is an observed session fact, not a plan expectation: an
	// exact structured input is covered, and every tool CP0 did not exercise is
	// recorded uncovered and receives no matched body.
	var covered, uncovered []string
	for _, record := range log.Records {
		if record.Kind == "session_start" {
			covered, uncovered = record.Covered, record.Uncovered
		}
	}
	if !contains(covered, "read") {
		t.Errorf("covered tools = %v, want the tool CP0 proved an exact structured input for", covered)
	}
	if !contains(uncovered, "bash") {
		t.Errorf("uncovered tools = %v, want every tool without a proven structured input", uncovered)
	}

	// Row omp_exact_path: proving a match for a scoped path needs the model to
	// choose the read, so the row skips loudly without a credential while the
	// credential-free observation above still passes.
	t.Run("omp_exact_path", func(t *testing.T) {
		if _, ok := OMPModelToken(); !ok {
			t.Skipf("no provider credential: set CP0_OMP_AUTH_HOME or CP0_OMP_API_KEY to exercise exact-path matching; the credential-free observation is retained")
		}
		scoped, err := home.Launch(OMPLaunch{
			Name:    "omp-scope-match",
			WorkDir: repo,
			Args:    []string{"--tools", "read"},
			Prompt:  "Read docs/spec/x.md with the read tool, then reply exactly DONE.",
			Log:     home.LogPath("atomic-runtime-scope.jsonl"),
		})
		if err != nil {
			t.Fatalf("scope launch: %v", err)
		}
		scopedObs, err := Run(context.Background(), scoped)
		if err != nil {
			t.Fatalf("scope Run: %v", err)
		}
		scopedLog, err := OMPRuntimeLog(scopedObs, "atomic-runtime-scope.jsonl")
		if err != nil {
			t.Fatalf("parse scope log: %v", err)
		}
		matched := false
		for _, record := range scopedLog.Records {
			if record.Kind == "operation" && record.Candidate == "docs/spec/x.md" {
				matched = contains(record.Matched, "shipped:rules/docs/spec.md")
			}
		}
		if !matched {
			t.Skipf("the model did not read the scoped path verbatim, so exact-path matching is not exercised by this run: exit %d\n%s", scopedObs.ExitCode, scopedObs.Stderr)
		}
	})

	t.Logf("omp %s; baseline consumed %v; index %v; covered %v; uncovered %v", binary, log.BaselineConsumed, log.Index, covered, uncovered)
}

// TestGuidanceUnsafeProjectionPreserved proves, for both harnesses, that a
// failed re-projection preserves the last valid native output: after a valid
// projection, an ambiguous or changed derivative makes the next projection
// refuse, and every byte the prior projection wrote is still exactly where it
// was. Nothing is overwritten and no partial write is left behind.
func TestGuidanceUnsafeProjectionPreserved(t *testing.T) {
	t.Run("claude_scope", func(t *testing.T) {
		home := t.TempDir()
		repo := filepath.Join(t.TempDir(), "repo")
		if err := os.MkdirAll(repo, 0o755); err != nil {
			t.Fatal(err)
		}
		if _, err := claude.MigrateScope(claude.ScopeRequest{
			Home:        home,
			NativeRoot:  filepath.Join(home, ".claude"),
			Target:      "claude:" + filepath.Join(home, ".claude"),
			OperationID: "cp6a-preserve-first",
			Scope:       claude.Scope{Dir: repo, Guidance: []byte(guidanceRepoScope)},
		}); err != nil {
			t.Fatalf("first scope migration: %v", err)
		}
		agentsPath := filepath.Join(repo, "AGENTS.md")
		claudePath := filepath.Join(repo, "CLAUDE.md")
		agentsBefore := readFileString(t, agentsPath)
		claudeBefore := readFileString(t, claudePath)

		// An ambiguous managed block is an invalid re-projection the adapter
		// cannot splice without guessing a boundary.
		corrupted := agentsBefore + "\n" + managedfile.BlockOpen + "\nunclosed\n"
		if err := os.WriteFile(agentsPath, []byte(corrupted), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := claude.MigrateScope(claude.ScopeRequest{
			Home:        home,
			NativeRoot:  filepath.Join(home, ".claude"),
			Target:      "claude:" + filepath.Join(home, ".claude"),
			OperationID: "cp6a-preserve-second",
			Scope:       claude.Scope{Dir: repo, Guidance: []byte("## Changed guidance\n")},
		}); err == nil {
			t.Fatal("the adapter accepted an ambiguous managed block")
		}
		if got := readFileString(t, agentsPath); got != corrupted {
			t.Errorf("the refused re-projection rewrote AGENTS.md:\n%s", got)
		}
		if got := readFileString(t, claudePath); got != claudeBefore {
			t.Errorf("the refused re-projection rewrote CLAUDE.md:\n%s", got)
		}
	})

	t.Run("omp_steering", func(t *testing.T) {
		root := t.TempDir()
		home := t.TempDir()
		profileRoot := filepath.Join(root, ".omp", "agent")
		if err := os.MkdirAll(profileRoot, 0o755); err != nil {
			t.Fatal(err)
		}
		a := omp.New()
		if _, err := a.Enroll(omp.EnrollRequest{Home: home, Profile: omp.Profile{Root: profileRoot}, OperationID: "cp6a-enroll-first"}); err != nil {
			t.Fatalf("first enroll: %v", err)
		}
		steeringPath := omp.SteeringPath(profileRoot)
		prior := readFileString(t, steeringPath)
		corrupted := prior + "\n" + managedfile.BlockOpen + "\nunclosed\n"
		if err := os.WriteFile(steeringPath, []byte(corrupted), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := a.Enroll(omp.EnrollRequest{Home: home, Profile: omp.Profile{Root: profileRoot}, OperationID: "cp6a-enroll-second"}); err == nil {
			t.Fatal("the adapter accepted an ambiguous managed block")
		}
		if got := readFileString(t, steeringPath); got != corrupted {
			t.Errorf("the refused re-enrollment rewrote the profile steering:\n%s", got)
		}
	})

	t.Run("omp_project_cards", func(t *testing.T) {
		home := t.TempDir()
		stateRoot := filepath.Join(t.TempDir(), ".claude")
		base := t.TempDir()
		if _, err := wiki.Refresh(stateRoot, "cp6a-key", wiki.RefreshRepo, []wiki.PointerCard{{
			Domain:  "typescript",
			Include: []string{"**/*.ts"},
			Body:    "Domain: typescript.\n\nMap:\n  - src/\n",
		}}); err != nil {
			t.Fatalf("refresh cards: %v", err)
		}
		a := omp.New()
		request := omp.CardPublishRequest{Home: home, StateRoot: stateRoot, ProjectKey: "cp6a-key", Base: base}
		if _, err := a.PublishProjectCards(request); err != nil {
			t.Fatalf("first publish: %v", err)
		}
		cardPath := filepath.Join(base, ".omp", "rules", "atomic-wiki", "typescript.md")
		prior := readFileString(t, cardPath)
		if !strings.Contains(prior, "typescript") {
			t.Fatalf("the first publication did not write the projected card: %q", prior)
		}

		// A hand edit inside the pipeline-owned tree is a changed derivative the
		// adapter cannot prove it wrote, so the next publish refuses.
		tampered := "# hand edited\n"
		if err := os.WriteFile(cardPath, []byte(tampered), 0o644); err != nil {
			t.Fatal(err)
		}
		result, err := a.PublishProjectCards(request)
		if err == nil {
			t.Fatal("a changed derivative was overwritten")
		}
		if result.Status != harness.StatusConflicted {
			t.Errorf("status = %q, want conflicted", result.Status)
		}
		if got := readFileString(t, cardPath); got != tampered {
			t.Errorf("the conflicting card was modified:\n%s", got)
		}

		// A malformed canonical card is refused before any native write, leaving
		// the last valid publication intact.
		if _, err := wiki.Refresh(stateRoot, "cp6a-key", wiki.RefreshRepo, []wiki.PointerCard{{Domain: "typescript", Body: "no globs\n"}}); err == nil {
			t.Fatal("the canonical producer accepted a card with no include globs")
		}
		if got := readFileString(t, cardPath); got != tampered {
			t.Errorf("a refused canonical refresh changed the native card:\n%s", got)
		}
	})
}

// readFileString reads a file the assertions compare byte-for-byte.
func readFileString(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}
