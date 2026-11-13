package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/harness"
	"github.com/damusix/atomic-claude/atomic/internal/harness/claude"
	"github.com/damusix/atomic-claude/atomic/internal/installstate"
	"github.com/damusix/atomic-claude/atomic/internal/managedfile"
	"github.com/damusix/atomic-claude/atomic/internal/prompt"
)

func stubAgentsForm(t *testing.T, selections map[string]config.AgentOverride) *[]config.AgentForm {
	t.Helper()
	var opened []config.AgentForm
	orig := config.AgentTierSelector
	config.AgentTierSelector = func(form config.AgentForm, _ map[string]config.AgentOverride) (map[string]config.AgentOverride, error) {
		opened = append(opened, form)
		return selections, nil
	}
	t.Cleanup(func() { config.AgentTierSelector = orig })
	return &opened
}

func stubLegacyReapply(t *testing.T) *[]string {
	t.Helper()
	var roots []string
	orig := reapplyLegacyAgents
	reapplyLegacyAgents = func(root, _ string) ([]string, int, error) {
		roots = append(roots, root)
		return nil, 0, nil
	}
	t.Cleanup(func() { reapplyLegacyAgents = orig })
	return &roots
}

func runConfigAgentsCaptured(t *testing.T, home string, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	t.Setenv("HOME", home)
	stdout, stderr = captureOutput(t, func() { code = runConfigAgents(args, home, nil) })
	return code, stdout, stderr
}

func assertLedgerClaimsDisk(t *testing.T, home string, target harness.Target, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := installstate.LoadLedger(config.LedgerPath(home))
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range ledger.Rows {
		if row.Target == target.Key() && row.Applied.Path == path {
			if row.Applied.Digest != managedfile.Digest(data) {
				t.Errorf("ledger digest %s does not claim the bytes on disk at %s", row.Applied.Digest, path)
			}
			return data
		}
	}
	t.Fatalf("ledger has no row for %s", path)
	return nil
}

func installOMP(t *testing.T, home string) string {
	t.Helper()
	root := filepath.Join(home, ".omp", "agent")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if out, code := runAtomicCLIEnv(t, home, []string{"OMP_PROFILE="}, "install", "--harness", "omp", "--yes"); code != 0 {
		t.Fatalf("install --harness omp exited %d:\n%s", code, out)
	}
	return root
}

func TestConfigAgentsOnAHarnessThatIsNotEnrolledSavesConfigOnly(t *testing.T) {
	home := t.TempDir()
	codexHome := filepath.Join(home, ".codex")
	if err := os.MkdirAll(codexHome, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_HOME", codexHome)
	stubAgentsForm(t, map[string]config.AgentOverride{"atomic-reviewer": {Model: "gpt-a", Effort: "ultra"}})
	reapplied := stubLegacyReapply(t)

	code, stdout, stderr := runConfigAgentsCaptured(t, home, "--harness", "codex")
	if code != 0 {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	if !strings.Contains(stdout, "applies when codex is enrolled") {
		t.Errorf("stdout = %q, want the change reported as applying when codex is enrolled", stdout)
	}
	cfg, _, err := config.Load(config.TOMLPath(home))
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Codex.Agents["atomic-reviewer"]; got != (config.AgentOverride{Model: "gpt-a", Effort: "ultra"}) {
		t.Errorf("codex override = %+v", got)
	}
	if entries, _ := os.ReadDir(codexHome); len(entries) != 0 {
		t.Errorf("a harness that is not enrolled got files: %v", entries)
	}
	if len(*reapplied) != 0 {
		t.Errorf("ReapplyAgents ran for %v", *reapplied)
	}
}

func TestConfigAgentsConvergesAnEnrolledOMPAgentThroughTheLedger(t *testing.T) {
	home := t.TempDir()
	root := installOMP(t, home)
	forms := stubAgentsForm(t, map[string]config.AgentOverride{"atomic-investigator": {Model: "@smol", Effort: "high"}})

	code, stdout, stderr := runConfigAgentsCaptured(t, home, "--harness", "omp")
	if code != 0 {
		t.Fatalf("exit %d, stdout %q, stderr %q", code, stdout, stderr)
	}
	if len(*forms) != 1 || (*forms)[0].Harness != "omp" {
		t.Fatalf("forms opened = %+v, want one omp form", *forms)
	}
	path := filepath.Join(root, "agents", "atomic-investigator.md")
	got := string(assertLedgerClaimsDisk(t, home, harness.Target{Kind: harness.KindOMP, Instance: root}, path))
	if !strings.Contains(got, "thinking: high") || !strings.Contains(got, "@smol") {
		t.Errorf("agent file lacks the saved override:\n%s", got)
	}
	if !strings.Contains(stdout, "Restart omp sessions") {
		t.Errorf("stdout = %q, want omp's restart note", stdout)
	}
}

// Convergence goes through the ledger, so a file the user edited by hand is a
// conflict that keeps its bytes rather than something a save overwrites.
func TestConfigAgentsLeavesAHandEditedAgentAsAConflict(t *testing.T) {
	home := t.TempDir()
	root := installOMP(t, home)
	path := filepath.Join(root, "agents", "atomic-investigator.md")
	edited := []byte("---\nname: atomic-investigator\n---\nmine\n")
	if err := os.WriteFile(path, edited, 0o644); err != nil {
		t.Fatal(err)
	}
	stubAgentsForm(t, map[string]config.AgentOverride{"atomic-investigator": {Effort: "low"}})

	code, stdout, _ := runConfigAgentsCaptured(t, home, "--harness", "omp")
	if code == 0 {
		t.Fatalf("a conflicting save exited 0:\n%s", stdout)
	}
	if data, _ := os.ReadFile(path); string(data) != string(edited) {
		t.Errorf("the hand-edited agent was overwritten:\n%s", data)
	}
	cfg, _, err := config.Load(config.TOMLPath(home))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.OMP.Agents["atomic-investigator"].Effort != "low" {
		t.Error("the override was not saved before convergence refused")
	}
}

func TestConfigAgentsConvergesAnEnrolledClaudeRootUnderClaudeConfigDir(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, "claude-config")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if out, code := runAtomicCLIEnv(t, home, []string{claude.ConfigDirEnv + "=" + root}, "install", "--harness", "claude", "--yes"); code != 0 {
		t.Fatalf("install --harness claude exited %d:\n%s", code, out)
	}
	t.Setenv(claude.ConfigDirEnv, root)
	stubAgentsForm(t, map[string]config.AgentOverride{"atomic-implementer": {Effort: "high"}})
	reapplied := stubLegacyReapply(t)

	code, stdout, stderr := runConfigAgentsCaptured(t, home, "--harness", "claude")
	if code != 0 {
		t.Fatalf("exit %d, stdout %q, stderr %q", code, stdout, stderr)
	}
	path := filepath.Join(root, "agents", "atomic-implementer.md")
	if got := string(assertLedgerClaimsDisk(t, home, harness.Target{Kind: harness.KindClaude, Instance: root}, path)); !strings.Contains(got, "effort: high") {
		t.Errorf("agent file lacks the saved effort:\n%s", got)
	}
	if len(*reapplied) != 0 {
		t.Errorf("ReapplyAgents ran for an enrolled root: %v", *reapplied)
	}
	if !strings.Contains(stdout, "Restart Claude Code sessions") {
		t.Errorf("stdout = %q, want Claude's restart note", stdout)
	}
}

func TestConfigAgentsReappliesOnlyAnUnadoptedLegacyClaudeRoot(t *testing.T) {
	home := t.TempDir()
	writeLegacyInstallRecord(t, home)
	stubAgentsForm(t, map[string]config.AgentOverride{"atomic-implementer": {Model: "opus"}})
	reapplied := stubLegacyReapply(t)

	if code, _, stderr := runConfigAgentsCaptured(t, home, "--harness", "claude"); code != 0 {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	if want := claude.DefaultConfigDir(home); len(*reapplied) != 1 || (*reapplied)[0] != want {
		t.Errorf("ReapplyAgents roots = %v, want [%s]", *reapplied, want)
	}

	adopted := t.TempDir()
	cfg := config.Default()
	cfg.Install.Version, cfg.Install.Adopted = "6.0.0", true
	if err := config.WritePersist(config.TOMLPath(adopted), cfg); err != nil {
		t.Fatal(err)
	}
	*reapplied = nil
	code, stdout, _ := runConfigAgentsCaptured(t, adopted, "--harness", "claude")
	if code != 0 || len(*reapplied) != 0 {
		t.Fatalf("an adopted record: exit %d, ReapplyAgents roots %v", code, *reapplied)
	}
	if !strings.Contains(stdout, "applies when claude is enrolled") {
		t.Errorf("stdout = %q, want the not-enrolled report", stdout)
	}
}

func TestConfigAgentsAllRunsOneFormPerEnrolledHarness(t *testing.T) {
	home := t.TempDir()
	claudeRoot := filepath.Join(home, "claude-config")
	if err := os.MkdirAll(claudeRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if out, code := runAtomicCLIEnv(t, home, []string{claude.ConfigDirEnv + "=" + claudeRoot}, "install", "--harness", "claude", "--yes"); code != 0 {
		t.Fatalf("install --harness claude exited %d:\n%s", code, out)
	}
	installOMP(t, home)
	t.Setenv(claude.ConfigDirEnv, claudeRoot)
	forms := stubAgentsForm(t, map[string]config.AgentOverride{})

	if code, stdout, stderr := runConfigAgentsCaptured(t, home, "--harness", "all"); code != 0 {
		t.Fatalf("exit %d, stdout %q, stderr %q", code, stdout, stderr)
	}
	var got []string
	for _, f := range *forms {
		got = append(got, f.Harness)
	}
	if strings.Join(got, ",") != "claude,omp" {
		t.Errorf("forms opened = %v, want claude then omp", got)
	}
}

// Ctrl+C on one form means the user wants out, so `all` must not open the next.
func TestConfigAgentsAllStopsAtAnAbortedForm(t *testing.T) {
	home := t.TempDir()
	enrollLedgerKinds(t, home, "claude", "omp")
	opened := 0
	orig := config.AgentTierSelector
	config.AgentTierSelector = func(config.AgentForm, map[string]config.AgentOverride) (map[string]config.AgentOverride, error) {
		opened++
		return nil, config.ErrAgentsAborted
	}
	t.Cleanup(func() { config.AgentTierSelector = orig })

	code, _, stderr := runConfigAgentsCaptured(t, home, "--harness", "all")
	if code != 1 || opened != 1 {
		t.Fatalf("exit %d after %d forms, stderr %q; want exit 1 after one form", code, opened, stderr)
	}
}

func TestConfigAgentsReportsEachFormSentinel(t *testing.T) {
	for sentinel, want := range map[error]string{
		config.ErrNonInteractiveAgents: "atomic config agents --harness codex: requires an interactive terminal",
		config.ErrAgentsAborted:        "atomic config agents --harness codex: aborted",
	} {
		orig := config.AgentTierSelector
		t.Cleanup(func() { config.AgentTierSelector = orig })
		config.AgentTierSelector = func(config.AgentForm, map[string]config.AgentOverride) (map[string]config.AgentOverride, error) {
			return nil, sentinel
		}
		code, _, stderr := runConfigAgentsCaptured(t, t.TempDir(), "--harness", "codex")
		if code != 1 || !strings.Contains(stderr, want) {
			t.Errorf("%v: exit %d, stderr %q; want exit 1 with %q", sentinel, code, stderr, want)
		}
	}
}

func TestConfigAgentsUnknownHarnessIsAUsageError(t *testing.T) {
	if code, _, stderr := runConfigAgentsCaptured(t, t.TempDir(), "--harness", "pi"); code != 2 {
		t.Fatalf("exit %d, stderr %q; want usage exit 2", code, stderr)
	}
}

func TestConfigAgentsHarnessChoiceErrors(t *testing.T) {
	for _, tt := range []struct {
		chooseErr error
		want      string
	}{
		{prompt.ErrAborted, "atomic config agents: aborted"},
		{errors.New("terminal went away"), "terminal went away"},
	} {
		home := t.TempDir()
		enrollLedgerKinds(t, home, "claude", "omp")
		orig := chooseAgentsHarness
		t.Cleanup(func() { chooseAgentsHarness = orig })
		chooseAgentsHarness = func([]harness.Kind) (harness.Kind, error) { return "", tt.chooseErr }
		code, _, stderr := runConfigAgentsCaptured(t, home)
		if code != 1 || !strings.Contains(stderr, tt.want) {
			t.Errorf("choice error %v: exit %d, stderr %q; want %q", tt.chooseErr, code, stderr, tt.want)
		}
	}
}
