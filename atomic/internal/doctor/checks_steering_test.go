package doctor

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/installstate"
)

func steeringRepo(t *testing.T, mode string) string {
	t.Helper()
	root := t.TempDir()
	path := config.RepoConfigPath(root)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("[steering]\nclaude = \""+mode+"\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func stubClaudeVersion(t *testing.T, out string, err error) {
	t.Helper()
	old := claudeCLIVersionFn
	claudeCLIVersionFn = func() (string, error) { return out, err }
	t.Cleanup(func() { claudeCLIVersionFn = old })
}

func enroll(t *testing.T, home string, harnesses ...string) {
	t.Helper()
	ledger := &installstate.Ledger{Header: installstate.NewHeader()}
	for _, h := range harnesses {
		ledger.Targets = append(ledger.Targets, installstate.TargetRecord{Harness: h, Instance: "x", NativeRoot: "/x", Status: "enrolled"})
	}
	if err := ledger.Save(config.LedgerPath(home)); err != nil {
		t.Fatal(err)
	}
}

// Claude skips AGENTS.md when any of its own memory files exists, so agents-md
// beside one delivers nothing.
func TestSteeringCheck_AgentsMDBesideClaudeFileWarns(t *testing.T) {
	stubClaudeVersion(t, "2.1.322 (Claude Code)\n", nil)
	for _, sibling := range []string{"CLAUDE.md", ".claude/CLAUDE.md", "CLAUDE.local.md"} {
		t.Run(sibling, func(t *testing.T) {
			root := steeringRepo(t, "agents-md")
			path := filepath.Join(root, filepath.FromSlash(sibling))
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte("x\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			r := RunCheckSteeringWith(root, t.TempDir())
			if r.Severity != WARN || !strings.Contains(r.Detail, sibling) {
				t.Errorf("result = %+v, want a WARN naming %s", r, sibling)
			}
		})
	}
}

func TestSteeringCheck_AgentsMDWithCleanRepoPasses(t *testing.T) {
	stubClaudeVersion(t, "2.1.322 (Claude Code)\n", nil)
	if r := RunCheckSteeringWith(steeringRepo(t, "agents-md"), t.TempDir()); r.Severity != PASS {
		t.Errorf("result = %+v, want PASS", r)
	}
}

func TestSteeringCheck_OldClaudeWarns(t *testing.T) {
	stubClaudeVersion(t, "2.1.276 (Claude Code)\n", nil)
	r := RunCheckSteeringWith(steeringRepo(t, "agents-md"), t.TempDir())
	if r.Severity != WARN || !strings.Contains(r.Detail, "2.1.276") {
		t.Errorf("result = %+v, want a WARN naming 2.1.276", r)
	}
	stubClaudeVersion(t, "2.1.277 (Claude Code)\n", nil)
	if r := RunCheckSteeringWith(steeringRepo(t, "agents-md"), t.TempDir()); r.Severity != PASS {
		t.Errorf("2.1.277 is the first version that reads AGENTS.md; result = %+v", r)
	}
}

// Not knowing the version is not evidence of an old Claude, so it is one info line.
func TestSteeringCheck_UnknownVersionIsInfoNotWarning(t *testing.T) {
	for name, stub := range map[string]func(){
		"missing":     func() { stubClaudeVersion(t, "", errors.New("not found")) },
		"unparseable": func() { stubClaudeVersion(t, "claude dev build", nil) },
	} {
		t.Run(name, func(t *testing.T) {
			stub()
			r := RunCheckSteeringWith(steeringRepo(t, "agents-md"), t.TempDir())
			if r.Severity != PASS || strings.Count(r.Detail, "info:") != 1 {
				t.Errorf("result = %+v, want PASS with one info line", r)
			}
		})
	}
}

func TestSteeringCheck_ClaudeMDWarnsWhenOMPOrCodexEnrolled(t *testing.T) {
	for _, h := range []string{"omp", "codex"} {
		t.Run(h, func(t *testing.T) {
			home := t.TempDir()
			enroll(t, home, "claude", h)
			r := RunCheckSteeringWith(steeringRepo(t, "claude-md"), home)
			if r.Severity != WARN || !strings.Contains(r.Detail, h) {
				t.Errorf("result = %+v, want a WARN naming %s", r, h)
			}
		})
	}
	home := t.TempDir()
	enroll(t, home, "claude")
	if r := RunCheckSteeringWith(steeringRepo(t, "claude-md"), home); r.Severity != PASS {
		t.Errorf("Claude-only claude-md result = %+v, want PASS", r)
	}
}

func TestSteeringCheck_LoaderNeverWarnsAndBadValueDoes(t *testing.T) {
	if r := RunCheckSteeringWith(steeringRepo(t, "loader"), t.TempDir()); r.Severity != PASS {
		t.Errorf("loader result = %+v, want PASS", r)
	}
	r := RunCheckSteeringWith(steeringRepo(t, "bogus"), t.TempDir())
	if r.Severity != WARN || !strings.Contains(r.Detail, `"bogus"`) {
		t.Errorf("bad value result = %+v, want a WARN naming it", r)
	}
}

// docs/wiki and the realm wiki are pairs too: a CLAUDE.md beside their AGENTS.md
// hides it the same way.
func TestSteeringCheck_AgentsMDChecksEveryPairDir(t *testing.T) {
	stubClaudeVersion(t, "2.1.322 (Claude Code)\n", nil)
	for _, sub := range []string{"docs/wiki", "wiki"} {
		t.Run(sub, func(t *testing.T) {
			root := steeringRepo(t, "agents-md")
			dir := filepath.Join(root, filepath.FromSlash(sub))
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"AGENTS.md", "CLAUDE.md"} {
				if err := os.WriteFile(filepath.Join(dir, name), []byte("x\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			r := RunCheckSteeringWith(root, t.TempDir())
			if r.Severity != WARN || !strings.Contains(r.Detail, sub+"/CLAUDE.md") {
				t.Errorf("result = %+v, want a WARN naming %s/CLAUDE.md", r, sub)
			}
		})
	}
}

// claude-md cannot move user text out of AGENTS.md, so a loader pair that stays
// is reported rather than silently left.
func TestSteeringCheck_ClaudeMDWarnsWhenAPairStaysInLoaderShape(t *testing.T) {
	root := steeringRepo(t, "claude-md")
	for name, data := range map[string]string{"AGENTS.md": "my text\n", "CLAUDE.md": "<atomic>\n\n@AGENTS.md\n\n</atomic>\n"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	r := RunCheckSteeringWith(root, t.TempDir())
	if r.Severity != WARN || !strings.Contains(r.Detail, "loader shape") {
		t.Errorf("result = %+v, want a loader-shape WARN", r)
	}
}

// A bad steering value is a repo-config finding, not a load failure that would
// hide every other key in the file.
func TestRepoConfigCheckWarnsOnBadSteeringValueWithoutBreakingTheLoad(t *testing.T) {
	root := steeringRepo(t, "bogus")
	cfg, _, err := config.LoadRepoConfig(config.RepoConfigPath(root))
	if err != nil || cfg.Steering.Claude != "bogus" {
		t.Fatalf("LoadRepoConfig = %+v, %v; want the value loaded for the callers that ignore it", cfg, err)
	}
	r := RunCheckRepoConfigWith(root)
	if r.Severity != WARN || !strings.Contains(r.Detail, `steering.claude "bogus"`) {
		t.Errorf("result = %+v, want a WARN naming the value", r)
	}
}

// A realm root need not be a Git repository, so with no Git toplevel the check
// looks for the realm marker above the working directory.
func TestSteeringCheck_FindsANonGitRealmRoot(t *testing.T) {
	stubClaudeVersion(t, "2.1.322 (Claude Code)\n", nil)
	realm, err := filepath.EvalSymlinks(steeringRepo(t, "agents-md"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config.RepoConfigPath(realm), []byte("scope = \"realm\"\n[steering]\nclaude = \"agents-md\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(realm, "CLAUDE.md"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := gitToplevelFn
	gitToplevelFn = func(string) string { return "" }
	t.Cleanup(func() { gitToplevelFn = old })
	t.Chdir(realm)

	r := checkSteering(Opts{Home: t.TempDir()})
	if r.Severity != WARN || !strings.Contains(r.Detail, "CLAUDE.md") {
		t.Errorf("result = %+v, want the realm root checked", r)
	}
}

// The steering remedy is a config choice, so the repair can only print it.
func TestRepairPlanPrintsTheSteeringRemedy(t *testing.T) {
	stubClaudeVersion(t, "2.1.322 (Claude Code)\n", nil)
	root := steeringRepo(t, "agents-md")
	if err := os.WriteFile(filepath.Join(root, "CLAUDE.md"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := RunCheckSteeringWith(root, t.TempDir())
	if r.Severity != WARN || r.Remediation == "" {
		t.Fatalf("result = %+v, want a WARN carrying a remediation", r)
	}
	r.Name = "steering"
	var out strings.Builder
	Repair([]Result{r}, Opts{}, nil, &out)
	got := out.String()
	if strings.Contains(got, "unknown category") || !strings.Contains(got, "cannot auto-fix") || !strings.Contains(got, r.Remediation) {
		t.Errorf("printed plan:\n%s", got)
	}
}
