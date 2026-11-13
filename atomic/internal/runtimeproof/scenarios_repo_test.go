// Repository, realm, and rule scenarios: worktree scope writes, non-Git realm
// initialization, and the Claude and OMP rule projections.
package runtimeproof

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/artifacts"
	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/embeddedcorpus"
	"github.com/damusix/atomic-claude/atomic/internal/harness"
	"github.com/damusix/atomic-claude/atomic/internal/harness/claude"
	"github.com/damusix/atomic-claude/atomic/internal/harness/omp"
	"github.com/damusix/atomic-claude/atomic/internal/managedfile"
	"github.com/damusix/atomic-claude/atomic/internal/wiki"
)

// A worktree scope write must land in the worktree while project-keyed state
// stays shared with the main checkout.
func TestWorktreeSharesProjectKey(t *testing.T) {
	root := isolatedHome(t)
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

	stateHome := isolatedHome(t)
	restore := config.SetHomeDirForTest(stateHome)
	defer restore()
	if mainKey, wtKey := config.ProjectStateDir(main), config.ProjectStateDir(worktree); mainKey != wtKey {
		t.Fatalf("project-keyed state diverged: main %q vs worktree %q", mainKey, wtKey)
	}

	home := filepath.Join(root, "atomic-home")
	nativeRoot := filepath.Join(home, ".claude")
	ev := ScenarioEvidence{
		Scenario:  "cp8a/repo/worktree-shares-project-key",
		Group:     "repo",
		Engine:    "harness/claude + config",
		Criterion: "a worktree scope write lands in the invoked worktree while project-keyed state stays shared",
		Command:   "git worktree add → claude.MigrateScope(worktree)",
		Paths:     []string{main, worktree, stateHome},
	}

	if _, err := claude.MigrateScope(claude.ScopeRequest{
		Home:        home,
		NativeRoot:  nativeRoot,
		Target:      "claude:" + nativeRoot,
		OperationID: "cp8a-worktree",
		Scope:       claude.Scope{Dir: worktree, Guidance: []byte(guidanceRepoScope)},
	}); err != nil {
		t.Fatalf("worktree scope migration: %v", err)
	}
	for _, name := range []string{"CLAUDE.md", "AGENTS.md"} {
		if !fileExists(filepath.Join(worktree, name)) {
			t.Errorf("scope pair did not land in the invoked worktree: %s", name)
		}
		if fileExists(filepath.Join(main, name)) {
			t.Errorf("the main checkout gained %s from a worktree invocation", name)
		}
	}

	ev.Outcome = "worktree received its loader pair; main checkout untouched; project key shared"
	recordScenario(t, ev)
}

// A realm may start from a non-Git root; its nested wiki becomes its own Git
// repository and both get loader pairs.
func TestRealmInitialization(t *testing.T) {
	realm := filepath.Join(isolatedHome(t), "realm")
	if err := os.MkdirAll(realm, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := materialize(realm, filepath.Join("testdata", "guidance", "realm")); err != nil {
		t.Fatal(err)
	}
	if _, err := wiki.Scan(realm, wiki.Options{Clock: func() time.Time { return time.Unix(0, 0).UTC() }}); err != nil {
		t.Fatalf("realm scan: %v", err)
	}
	if _, err := wiki.InitRealmScope(realm); err != nil {
		t.Fatalf("realm scaffold: %v", err)
	}
	wikiDir := filepath.Join(realm, "wiki")
	guidanceGit(t, wikiDir, "init", "-q", "-b", "main")

	home := filepath.Join(isolatedHome(t), "atomic-home")
	nativeRoot := filepath.Join(home, ".claude")
	ev := ScenarioEvidence{
		Scenario:  "cp8a/repo/realm-initialization",
		Group:     "repo",
		Engine:    "wiki + harness/claude",
		Criterion: "realm init starts non-Git, keeps the realm root outside Git, and makes the nested wiki its own repository",
		Command:   "wiki.Scan → wiki.InitRealmScope → claude.MigrateScope(realm, wiki)",
		Paths:     []string{realm, wikiDir},
	}

	for _, scope := range []claude.Scope{
		{Dir: realm, Guidance: []byte("## Realm capture\n")},
		{Dir: wikiDir, Guidance: []byte(guidanceWikiScope)},
	} {
		if _, err := claude.MigrateScope(claude.ScopeRequest{
			Home:        home,
			NativeRoot:  nativeRoot,
			Target:      "claude:" + nativeRoot,
			OperationID: "cp8a-realm",
			Scope:       scope,
		}); err != nil {
			t.Fatalf("realm scope %s: %v", scope.Dir, err)
		}
	}
	if _, err := os.Stat(filepath.Join(realm, ".git")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("realm initialization created a Git repository at the realm root")
	}
	if _, err := os.Stat(filepath.Join(wikiDir, ".git")); err != nil {
		t.Errorf("the nested wiki is not its own Git repository: %v", err)
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

	ev.Outcome = "realm root stayed non-Git; nested wiki is its own repository; both loader pairs present"
	recordScenario(t, ev)
}

// No native scoped-rule role was proven for the tested Claude version, so every
// projected rule must report unsupported.
func TestClaudeRuleTierHonesty(t *testing.T) {
	cat, err := embeddedcorpus.Load()
	if err != nil {
		t.Fatalf("load corpus: %v", err)
	}
	sources, err := omp.ShippedRuleSources(cat)
	if err != nil {
		t.Fatalf("shipped rule sources: %v", err)
	}
	if len(sources) == 0 {
		t.Fatalf("no shipped rules to project")
	}

	ev := ScenarioEvidence{
		Scenario:  "cp8a/repo/claude-rule-tier-honesty",
		Group:     "repo",
		Engine:    "harness (Claude rule projection)",
		Criterion: "the Claude rule projection ships authored bytes but reports unsupported and names unproven rule-delivery surfaces",
		Command:   "harness.ProjectClaudeRules(shipped sources, Claude capabilities)",
	}

	report, err := harness.ProjectClaudeRules(sources, claude.New().Capabilities())
	if err != nil {
		t.Fatalf("project claude rules: %v", err)
	}
	if len(report.Rules) != len(sources) {
		t.Fatalf("projected %d rules, want %d", len(report.Rules), len(sources))
	}
	for _, r := range report.Rules {
		if !strings.HasPrefix(r.NativeResource, "rules/") {
			t.Errorf("rule %s projected to a non-rule native path %s", r.RecordID, r.NativeResource)
		}
		if r.Tier != artifacts.EnforcementUnsupported {
			t.Errorf("rule %s tier = %s, want unsupported", r.RecordID, r.Tier)
		}
	}
	if len(report.Gaps) == 0 {
		t.Errorf("the projection reported no unproven rule-delivery surfaces")
	}

	ev.Outcome = "authored bytes projected; every rule reported unsupported; gaps named"
	recordScenario(t, ev)
}

// OMP supplies matched bodies for covered tools only, so the launched index must
// carry stable identities in canonical order and report uncovered tools.
func TestOMPRuleIdentityIndex(t *testing.T) {
	cat, err := embeddedcorpus.Load()
	if err != nil {
		t.Fatalf("load corpus: %v", err)
	}
	sources, err := omp.ShippedRuleSources(cat)
	if err != nil {
		t.Fatalf("shipped rule sources: %v", err)
	}
	delivery, err := omp.BuildSessionDelivery(sources, harness.OMPCapabilities(), nil)
	if err != nil {
		t.Fatalf("build session delivery: %v", err)
	}

	ev := ScenarioEvidence{
		Scenario:  "cp8a/repo/omp-rule-identity-index",
		Group:     "repo",
		Engine:    "harness/omp",
		Criterion: "the OMP session index carries stable rule identities and digests, and covered tools deliver while uncovered ones do not",
		Command:   "omp.BuildSessionDelivery → RenderExtension → real omp launch",
	}

	// The offline index is deterministic: identities in canonical order, each
	// carrying its own source digest.
	ids := make([]string, 0, len(delivery.Index))
	for _, e := range delivery.Index {
		if e.RecordID == "" || e.SourceDigest == "" {
			t.Fatalf("index entry carries no identity or digest: %+v", e)
		}
		ids = append(ids, e.RecordID)
	}
	for i := 1; i < len(ids); i++ {
		if ids[i-1] >= ids[i] {
			t.Fatalf("index identities are not in canonical ascending order: %v", ids)
		}
	}

	if testing.Short() {
		recordScenarioSkip(t, ev, "short mode: the real OMP launch is skipped")
		return
	}
	if _, err := OMPCLI(); err != nil {
		recordScenarioSkip(t, ev, "omp binary unavailable: the runtime index delivery cannot be observed")
		return
	}
	dir := t.TempDir()
	// The shipped adapter enrolls the home, so OMP launches the module an
	// install publishes.
	home, enroll := prepareOMPHome(t, filepath.Join(dir, "home"), OMPHomeRequest{Adapter: omp.New()})
	ev.Paths = []string{home.AgentRoot, enroll.PackageRoot}
	repo := filepath.Join(dir, "repo")
	writeTree(t, repo, map[string]string{"src/a.ts": "export const a = 1;\n", "docs/spec/x.md": "# spec\n"})
	launch := OMPLaunch{Name: "cp8a-omp-index", WorkDir: repo, Prompt: "Reply OK.", Log: home.LogPath("atomic-runtime-index.jsonl")}
	scenario, err := home.Launch(launch)
	if err != nil {
		t.Fatalf("build launch: %v", err)
	}
	scenario.Args = append([]string{"--tools", "read,bash"}, scenario.Args...)
	obs, err := Run(context.Background(), scenario)
	if errors.Is(err, ErrTimeout) {
		t.Fatalf("omp exceeded its bound: %+v", obs)
	}
	if err != nil {
		t.Fatalf("run omp: %v", err)
	}
	log, err := OMPRuntimeLog(obs, "atomic-runtime-index.jsonl")
	if err != nil {
		t.Fatalf("parse runtime log: %v", err)
	}
	if len(log.Index) != len(ids) {
		t.Fatalf("session index = %v, want the projected identities %v", log.Index, ids)
	}
	for i := range ids {
		if log.Index[i] != ids[i] {
			t.Fatalf("session index = %v, want %v", log.Index, ids)
		}
	}
	if !contains(log.DeclaredUncovered, "bash") {
		t.Errorf("declared uncovered tools = %v, want bash reported (no structured target was proven)", log.DeclaredUncovered)
	}
	var covered []string
	for _, record := range log.Records {
		if record.Kind == "session_start" {
			covered = record.Covered
		}
	}
	if !contains(covered, "read") {
		t.Errorf("covered tools = %v, want read (its path input was proven)", covered)
	}
	ev.Paths = append(ev.Paths, home.AgentRoot, repo)

	ev.Outcome = "identity index reached the session in canonical order; covered tools known, bash reported uncovered"
	recordScenario(t, ev)
}

// Wiki initialization must emit the docs/wiki AGENTS.md and CLAUDE.md loader
// pair, and leave an existing CLAUDE.md-direct install byte-identical.
func TestRepoScopeInitLoaderPair(t *testing.T) {
	root := filepath.Join(isolatedHome(t), "repo")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}

	ev := ScenarioEvidence{
		Scenario:  "cp8a/repo/init-loader-pair",
		Group:     "repo",
		Engine:    "wiki",
		Criterion: "repo wiki init writes the docs/wiki/AGENTS.md steering file and its thin CLAUDE.md loader, leaving a CLAUDE.md-direct install untouched",
		Command:   "wiki.InitRepoScope",
		Paths:     []string{root},
	}

	created, err := wiki.InitRepoScope(root)
	if err != nil {
		t.Fatalf("InitRepoScope: %v", err)
	}
	agentsPath := filepath.Join(root, "docs", "wiki", "AGENTS.md")
	claudePath := filepath.Join(root, "docs", "wiki", "CLAUDE.md")
	if len(created) != 2 || created[0] != agentsPath || created[1] != claudePath {
		t.Fatalf("created = %v, want the pair %s, %s", created, agentsPath, claudePath)
	}
	agents, err := os.ReadFile(agentsPath)
	if err != nil {
		t.Fatalf("read shared steering: %v", err)
	}
	if len(agents) == 0 {
		t.Errorf("the shared AGENTS.md steering file is empty")
	}
	loader, err := os.ReadFile(claudePath)
	if err != nil {
		t.Fatalf("read loader: %v", err)
	}
	// An `@` line directly inside the block tags is swallowed by the HTML block
	// they open, so the pair would deliver nothing.
	if !strings.Contains(string(loader), managedfile.BlockOpen+"\n\n@AGENTS.md\n\n"+managedfile.BlockClose) {
		t.Errorf("the loader is not the blank-bracketed @AGENTS.md import:\n%s", loader)
	}
	if !fileExists(agentsPath) {
		t.Errorf("the loader imports an AGENTS.md that does not exist")
	}

	// A repo initialized before the pair existed keeps its CLAUDE.md bytes and
	// gains no second steering file to duplicate them.
	legacy := filepath.Join(isolatedHome(t), "legacy")
	legacyClaude := filepath.Join(legacy, "docs", "wiki", "CLAUDE.md")
	if err := os.MkdirAll(filepath.Dir(legacyClaude), 0o755); err != nil {
		t.Fatal(err)
	}
	prior := "---\ntype: Steering\ndescription: pre-pair install.\n---\n\nmaintainer notes\n"
	if err := os.WriteFile(legacyClaude, []byte(prior), 0o644); err != nil {
		t.Fatal(err)
	}
	kept, err := wiki.InitRepoScope(legacy)
	if err != nil {
		t.Fatalf("InitRepoScope on a CLAUDE.md-direct install: %v", err)
	}
	if len(kept) != 0 {
		t.Errorf("created = %v, want nothing for a CLAUDE.md-direct install", kept)
	}
	if got, err := os.ReadFile(legacyClaude); err != nil {
		t.Fatal(err)
	} else if string(got) != prior {
		t.Errorf("init rewrote an existing CLAUDE.md-direct install:\n%s", got)
	}
	if fileExists(filepath.Join(legacy, "docs", "wiki", "AGENTS.md")) {
		t.Errorf("init added an AGENTS.md the existing install does not import")
	}

	ev.Outcome = "init emitted the docs/wiki AGENTS.md + CLAUDE.md loader pair; the pre-pair CLAUDE.md install stayed byte-identical"
	recordScenario(t, ev)
}
