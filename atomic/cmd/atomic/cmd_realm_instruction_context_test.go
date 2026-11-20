package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/harness/codex"
	"github.com/damusix/atomic-claude/atomic/internal/hooks"
	"github.com/damusix/atomic-claude/atomic/internal/installstate"
)

const updatedRealmStartupGuidance = "# Current team guidance\n\nUse `make check` before review.\n"

func TestRealmClaudeInstructionSessionStart(t *testing.T) {
	claude, err := exec.LookPath("claude")
	if err != nil {
		t.Skipf("Claude activation requires a real claude on PATH: %v", err)
	}
	f := newRealmLinkCLIFixture(t)
	setRealmStartupPath(t, claude)
	enrollRealmStartupHarness(t, f.joinerHome, "claude", ".claude")
	const personalLoader = "# Authored local Claude guidance\n\n" + loaderBlockText
	loader := filepath.Join(f.joiner, "CLAUDE.md")
	writeSteeringTestFile(t, loader, personalLoader)
	input := realmStartupInput(t, f.joiner)
	approveRealmStartupLink(t, f.joinerHome, shareSelected)
	assertRealmStartupSilent(t, runRealmClaudeInstructionStartup(t, f, input))
	approveRealmStartupLink(t, f.joinerHome, sharedRealmInstructions)
	assertRealmShareCLIFile(t, f.joiner, sharedRealmInstructions, teamRealmGuidance, true)
	assertRealmShareCLIFile(t, f.joiner, "AGENTS.local.md", privateRealmGuidance, false)

	loaded := readSteeringTestFile(t, loader)
	if !strings.Contains(loaded, personalLoader) {
		t.Fatalf("CLAUDE.md lost the authored loader: %q", loaded)
	}
	managed := strings.Replace(loaded, personalLoader, "", 1)
	if strings.Count("\n"+managed+"\n", "\n@AGENTS.local.md\n") != 1 {
		t.Errorf("CLAUDE.md must import private guidance once: %q", managed)
	}
	for _, forbidden := range []string{sharedRealmInstructions, strings.TrimSpace(teamRealmGuidance), strings.TrimSpace(privateRealmGuidance)} {
		if strings.Contains(managed, forbidden) {
			t.Errorf("CLAUDE.md managed addition includes %q: %q", forbidden, managed)
		}
	}

	clone := realmCLIManagedClone(t, f.joinerHome)
	source := filepath.Join(clone, sharedRealmInstructions)
	t.Chdir(t.TempDir())
	assertTeamContext := func(t *testing.T, out, want string) {
		t.Helper()
		if len(strings.TrimSpace(out)) > 10_000 {
			t.Errorf("SessionStart envelope is %d bytes, over the 10,000-character limit", len(strings.TrimSpace(out)))
		}
		context := realmStartupAdditionalContext(t, out)
		assertRealmStartupContext(t, context, []string{want}, []string{strings.TrimSpace(privateRealmGuidance), "@AGENTS.local.md"})
		if want != teamRealmGuidance {
			assertRealmStartupContext(t, context, nil, []string{strings.TrimSpace(teamRealmGuidance)})
		}
	}
	assertTeamContext(t, runRealmClaudeInstructionStartup(t, f, input), teamRealmGuidance)
	writeSteeringTestFile(t, source, updatedRealmStartupGuidance)
	unchanged := unchangedRealmCLIStartupWithReminder(t, f.joiner, f.joinerHome, f.remote)
	dirtyOut, _ := runReminderStartup(t, f.joinerHome, "claude", input, "--realm-instructions")
	unchanged(t)
	assertTeamContext(t, dirtyOut, updatedRealmStartupGuidance)
	if got := readReminderSystemMessage(t, dirtyOut); got != wantRealmSyncReminder {
		t.Errorf("realm-local SessionStart notice = %q, want %q", got, wantRealmSyncReminder)
	}

	outside := filepath.Join(t.TempDir(), "outside.md")
	writeSteeringTestFile(t, outside, "# Unexpected outside guidance\n")
	for _, tt := range []struct {
		name, contents, target string
	}{
		{name: "malformed UTF-8", contents: updatedRealmStartupGuidance + "\xff"},
		{name: "oversized source", contents: updatedRealmStartupGuidance + strings.Repeat("x", 10_000)},
		{name: "oversized JSON envelope", contents: updatedRealmStartupGuidance + strings.Repeat("\\", 6_000)},
		{name: "unsafe clone symlink", target: outside},
		{name: "missing source"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			moveRealmStartupFixtureAside(t, source)
			switch {
			case tt.target != "":
				if err := os.Symlink(tt.target, source); err != nil {
					t.Fatal(err)
				}
			case tt.contents != "":
				writeSteeringTestFile(t, source, tt.contents)
			}
			assertRealmStartupSilent(t, runRealmClaudeInstructionStartup(t, f, input))
		})
	}

	t.Run("replaced local witness", func(t *testing.T) {
		link := filepath.Join(f.joiner, sharedRealmInstructions)
		target, err := os.Readlink(link)
		if err != nil {
			t.Fatal(err)
		}
		moveRealmStartupFixtureAside(t, link)
		if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}
		assertRealmStartupSilent(t, runRealmClaudeInstructionStartup(t, f, input))
	})
	t.Run("malformed hook stdin", func(t *testing.T) {
		assertRealmStartupSilent(t, runRealmClaudeInstructionStartup(t, f, `{"cwd":`))
	})
	t.Run("after unlink", func(t *testing.T) {
		t.Chdir(f.joiner)
		mustRunRealmCLI(t, f.joinerHome, "unlink")
		assertRealmStartupSilent(t, runRealmClaudeInstructionStartup(t, f, input))
	})
}

func TestRealmClaudeInstructionSessionStartWithAuthoredImportExamples(t *testing.T) {
	claude, err := exec.LookPath("claude")
	if err != nil {
		t.Skipf("Claude activation requires a real claude on PATH: %v", err)
	}
	for _, tt := range []struct{ name, example string }{
		{"fenced", "```markdown\n@AGENTS.local.md\n```\n"},
		{"commented", "<!--\n@AGENTS.local.md\n-->\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := newRealmLinkCLIFixture(t)
			setRealmStartupPath(t, claude)
			enrollRealmStartupHarness(t, f.joinerHome, "claude", ".claude")
			authored := "# Authored local Claude guidance\n\n" + tt.example + "\n" + loaderBlockText
			loader := filepath.Join(f.joiner, "CLAUDE.md")
			writeSteeringTestFile(t, loader, authored)
			settings := filepath.Join(f.joiner, realmClaudeLocalSettings)
			writeSteeringTestFile(t, settings, realmAuthoredSettings)
			preview := mustRunRealmCLI(t, f.joinerHome, "link", sharedRealmInstructions, "--dry-run")
			out := mustRunRealmCLI(t, f.joinerHome, "link", sharedRealmInstructions, "--yes", "--review", realmReviewDigest(t, preview))
			loaded := readSteeringTestFile(t, loader)
			added, preserved := strings.CutPrefix(loaded, authored)
			if !preserved {
				t.Fatalf("CLAUDE.md lost the authored example: %q", loaded)
			}
			startup := runRealmClaudeInstructionStartup(t, f, realmStartupInput(t, f.joiner))
			if added == "" {
				if got := readSteeringTestFile(t, settings); got != realmAuthoredSettings {
					t.Errorf("Claude settings changed without an effective private import: %q", got)
				}
				assertRealmStartupSilent(t, startup)
				assertRealmStartupContext(t, preview+"\n"+out, nil, []string{
					"Register the Atomic SessionStart hook", "Claude realm startup already configured", realmClaudeInstructionCommand,
				})
				return
			}
			if strings.Count("\n"+added+"\n", "\n@AGENTS.local.md\n") != 1 {
				t.Fatalf("CLAUDE.md did not add a standalone private import outside the authored example: %q", added)
			}
			assertRealmInstructionSettings(t, f.joiner, 1)
			assertRealmStartupContext(t, realmStartupAdditionalContext(t, startup), []string{teamRealmGuidance}, []string{strings.TrimSpace(privateRealmGuidance)})
		})
	}
}

func TestRealmClaudeInstructionSessionStartWithoutPrerequisites(t *testing.T) {
	f := newRealmLinkCLIFixture(t)
	setRealmStartupPath(t, "")
	enrollRealmStartupHarness(t, f.joinerHome, "claude", ".claude")
	input := realmStartupInput(t, f.joiner)
	approveRealmStartupLink(t, f.joinerHome, shareSelected)
	assertRealmStartupSilent(t, runRealmClaudeInstructionStartup(t, f, input))
	approveRealmStartupLink(t, f.joinerHome, sharedRealmInstructions)
	assertRealmShareCLIFile(t, f.joiner, sharedRealmInstructions, teamRealmGuidance, true)
	assertRealmStartupSilent(t, runRealmClaudeInstructionStartup(t, f, input))
}

func TestRealmOMPInstructionSessionStartRequiresPrimaryOptIn(t *testing.T) {
	t.Setenv("PI_CODING_AGENT_DIR", "")
	f := newRealmLinkCLIFixture(t)
	withoutRealmGuidance := prepareRealmAdapterStartup(t, f)
	run := func(t *testing.T, dir string, primary bool) string {
		t.Helper()
		args := []string{"--format", "text", "--cwd", dir}
		if primary {
			args = append(args, "--realm-primary")
		}
		return runRealmAdapterStartup(t, f.joinerHome, "omp", "", args...)
	}
	baseline := run(t, f.joiner, false)
	assertRealmStartupContext(t, baseline, []string{"## Workflow preferences"}, withoutRealmGuidance)

	enrollRealmStartupHarness(t, f.joinerHome, "omp", ".omp/agent")
	t.Run("generic cwd for child switch or move", func(t *testing.T) {
		assertRealmStartupContext(t, run(t, f.joiner, false), []string{strings.TrimSpace(baseline)}, withoutRealmGuidance)
	})
	t.Run("primary startup", func(t *testing.T) {
		assertRealmStartupContext(t, run(t, f.joiner, true), []string{strings.TrimSpace(baseline), updatedRealmStartupGuidance, strings.TrimSpace(privateRealmGuidance)}, []string{strings.TrimSpace(teamRealmGuidance)})
	})
	t.Run("primary startup with alternate native root", func(t *testing.T) {
		t.Setenv("PI_CODING_AGENT_DIR", t.TempDir())
		assertRealmStartupContext(t, run(t, f.joiner, true), []string{strings.TrimSpace(baseline)}, withoutRealmGuidance)
	})
	assertRealmStartupContext(t, run(t, unboundRealmStartupDir(t), true), []string{"## Workflow preferences"}, withoutRealmGuidance)
	mustRunRealmCLI(t, f.joinerHome, "unlink")
	assertRealmStartupContext(t, run(t, f.joiner, true), []string{"## Workflow preferences"}, withoutRealmGuidance)
}

func TestRealmOMPInstructionSessionStartFromLinkedDescendant(t *testing.T) {
	t.Setenv("PI_CODING_AGENT_DIR", "")
	publisher, publisherHome, remote := newRealmShareCLIFixture(t)
	const shared = "runbooks"
	const nested = "procedures"
	writeSteeringTestFile(t, filepath.Join(publisher, shared, nested, "review.md"), "# Review checklist\n")
	writeSteeringTestFile(t, filepath.Join(publisher, sharedRealmInstructions), teamRealmGuidance)
	args := []string{shared + "/", sharedRealmInstructions, "--remote", remote, "--branch", shareBranch}
	review := previewRealmReview(t, publisherHome, "share", args...)
	mustRunRealmCLI(t, publisherHome, "share", append(args, "--yes", "--review", review)...)
	runGit(t, remote, "symbolic-ref", "HEAD", "refs/heads/"+shareBranch)

	joiner, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	writeSteeringTestFile(t, config.RepoConfigPath(joiner), "scope = \"realm\"\n")
	writeSteeringTestFile(t, filepath.Join(joiner, "AGENTS.local.md"), privateRealmGuidance)
	t.Chdir(joiner)
	mustRunRealmCLI(t, home, "join", remote)
	setRealmStartupPath(t, "")
	enrollRealmStartupHarness(t, home, "omp", ".omp/agent")
	approveRealmStartupLink(t, home, shared+"/")
	approveRealmStartupLink(t, home, sharedRealmInstructions)

	link := filepath.Join(joiner, shared)
	dir := filepath.Join(link, nested)
	clone := realmCLIManagedClone(t, home)
	physicalClone, err := filepath.EvalSymlinks(clone)
	if err != nil {
		t.Fatal(err)
	}
	withoutGuidance := []string{strings.TrimSpace(teamRealmGuidance), strings.TrimSpace(privateRealmGuidance)}
	run := func(t *testing.T, cwd string, flags ...string) string {
		t.Helper()
		return runRealmAdapterStartup(t, home, "omp", "", append([]string{"--format", "text", "--cwd", cwd}, flags...)...)
	}
	t.Run("baseline", func(t *testing.T) {
		out := run(t, dir)
		assertRealmStartupContext(t, out, []string{"realm root (" + joiner + ")", "- Linked discovery root: " + link}, append(withoutGuidance, clone, physicalClone))
	})
	t.Run("primary startup", func(t *testing.T) {
		out := run(t, dir, "--realm-primary")
		assertRealmStartupContext(t, out, withoutGuidance, []string{clone, physicalClone})
	})
	t.Run("explicit repository override", func(t *testing.T) {
		env := []string{"PATH=" + os.Getenv("PATH"), "ATOMIC_TEST_CLI_IN=" + os.DevNull}
		out, diag, code := runAtomicCLIFull(t, home, env, "root", "--repo", physicalClone,
			"hooks", "session-start", "--harness", "omp", "--format", "text", "--cwd", dir, "--realm-primary")
		if code != 0 {
			t.Fatalf("session-start with --repo: exit %d\nstdout: %s\nstderr: %s", code, out, diag)
		}
		assertRealmStartupContext(t, out, []string{"## Workflow preferences"}, append(withoutGuidance, "realm root ("+joiner+")", link))
	})
	t.Run("unowned alias", func(t *testing.T) {
		alias := filepath.Join(joiner, "shortcut")
		if err := os.Symlink(filepath.Join(physicalClone, shared), alias); err != nil {
			t.Fatal(err)
		}
		out := run(t, filepath.Join(alias, nested), "--realm-primary")
		assertRealmStartupContext(t, out, []string{"## Workflow preferences"}, append(withoutGuidance, link))
	})
	t.Run("nested repository", func(t *testing.T) {
		nestedRepo := filepath.Join(physicalClone, shared, nested)
		runGit(t, nestedRepo, "init")
		out := run(t, dir, "--realm-primary")
		assertRealmStartupContext(t, out, []string{"## Workflow preferences"}, append(withoutGuidance, link))
	})
}

func TestRealmCodexInstructionSessionStartRequiresEnabledNativeTrust(t *testing.T) {
	f := newRealmLinkCLIFixture(t)
	withoutRealmGuidance := prepareRealmAdapterStartup(t, f)
	root := filepath.Join(f.joinerHome, ".codex")
	t.Setenv("CODEX_HOME", root)
	run := func(t *testing.T, dir, source string) string {
		t.Helper()
		return runRealmAdapterStartup(t, f.joinerHome, "codex", realmStartupInputForSource(t, dir, source))
	}
	baseline := run(t, f.joiner, "startup")
	assertRealmStartupContext(t, baseline, []string{"## Workflow preferences"}, withoutRealmGuidance)

	if _, err := codex.New().Enroll(codex.EnrollRequest{Home: f.joinerHome, Root: root}); err != nil {
		t.Fatalf("install native Codex hook: %v", err)
	}
	path := hooks.CodexHooksPath(root)
	trustKey := func(t *testing.T, root string) string {
		t.Helper()
		group, handler, found, err := hooks.CodexSessionStartPosition(root)
		if err != nil || !found {
			t.Fatalf("installed SessionStart member not found in %s: %v", hooks.CodexHooksPath(root), err)
		}
		return fmt.Sprintf("%s:session_start:%d:%d", hooks.CodexHooksPath(root), group, handler)
	}
	key := trustKey(t, root)
	identity := `{"event_name":"session_start","hooks":[{"async":false,"command":"atomic hooks session-start --harness codex","timeout":600,"type":"command"}],"matcher":"startup|resume"}`
	trustedHash := fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(identity)))
	for _, tt := range []struct {
		name, hash      string
		trust           codex.HookTrust
		absent          bool
		disabled        bool
		featureDisabled bool
		otherHome       bool
		wantRealm       bool
	}{
		{name: "absent hook", hash: trustedHash, trust: codex.HookTrustUnknown, absent: true},
		{name: "installed but untrusted", trust: codex.HookUntrusted},
		{name: "stale trust", hash: "sha256:" + strings.Repeat("0", 64), trust: codex.HookTrustModified},
		{name: "trusted but disabled", hash: trustedHash, trust: codex.HookTrusted, disabled: true},
		{name: "trusted but hooks feature disabled", hash: trustedHash, trust: codex.HookTrusted, featureDisabled: true},
		{name: "trusted in a different effective home", hash: trustedHash, trust: codex.HookTrusted, otherHome: true},
		{name: "exact trust and enabled", hash: trustedHash, trust: codex.HookTrusted, wantRealm: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if tt.absent {
				moveRealmStartupFixtureAside(t, path)
			}
			effectiveRoot, effectiveKey := root, key
			if tt.otherHome {
				effectiveRoot = t.TempDir()
				t.Setenv("CODEX_HOME", effectiveRoot)
				if skipped, err := hooks.InstallCodex(effectiveRoot); err != nil || skipped {
					t.Fatalf("install native hook without enrolling effective home: skipped=%t, err=%v", skipped, err)
				}
				effectiveKey = trustKey(t, effectiveRoot)
			}
			writeTrust := func(root, key string) {
				configPath := codex.ConfigPath(root)
				if _, err := os.Lstat(configPath); !os.IsNotExist(err) {
					t.Fatalf("expected no Codex config before adding fixture trust: %v", err)
				}
				state := fmt.Sprintf("[hooks.state.%q]\ntrusted_hash = %q\nenabled = %t\n", key, tt.hash, !tt.disabled)
				if tt.featureDisabled {
					state = "[features]\nhooks = false\n\n" + state
				}
				writeSteeringTestFile(t, configPath, state)
				t.Cleanup(func() {
					if err := os.Remove(configPath); err != nil {
						t.Error(err)
					}
				})
			}
			writeTrust(effectiveRoot, effectiveKey)
			if tt.otherHome {
				writeTrust(root, key)
			}
			trust, evidence := codex.ObserveSessionHookTrust(f.joinerHome, effectiveRoot)
			if trust != tt.trust || strings.Contains(evidence, "the hook is disabled") != tt.disabled {
				t.Fatalf("native trust = %s (%s), want %s (disabled: %t)", trust, evidence, tt.trust, tt.disabled)
			}
			startup := run(t, f.joiner, "startup")
			if tt.wantRealm {
				assertRealmStartupContext(t, startup, []string{strings.TrimSpace(baseline), updatedRealmStartupGuidance, strings.TrimSpace(privateRealmGuidance)}, []string{strings.TrimSpace(teamRealmGuidance)})
				input := realmStartupInput(t, f.joiner)
				for _, trailing := range []struct{ name, suffix string }{
					{"trailing garbage", "\nnot JSON"},
					{"second JSON object", "\n{}"},
				} {
					t.Run(trailing.name, func(t *testing.T) {
						t.Chdir(t.TempDir())
						out := runRealmAdapterStartup(t, f.joinerHome, "codex", input+trailing.suffix)
						assertRealmStartupContext(t, out, []string{strings.TrimSpace(baseline)}, withoutRealmGuidance)
					})
				}
			} else {
				assertRealmStartupContext(t, startup, []string{strings.TrimSpace(baseline)}, withoutRealmGuidance)
			}
			assertRealmStartupContext(t, run(t, f.joiner, "resume"), []string{strings.TrimSpace(baseline)}, withoutRealmGuidance)
			if tt.wantRealm {
				assertRealmStartupContext(t, run(t, unboundRealmStartupDir(t), "startup"), []string{"## Workflow preferences"}, withoutRealmGuidance)
				mustRunRealmCLI(t, f.joinerHome, "unlink")
				assertRealmStartupContext(t, run(t, f.joiner, "startup"), []string{"## Workflow preferences"}, withoutRealmGuidance)
			}
		})
	}
}

func prepareRealmAdapterStartup(t *testing.T, f realmLinkCLIFixture) []string {
	t.Helper()
	setRealmStartupPath(t, "")
	approveRealmStartupLink(t, f.joinerHome, sharedRealmInstructions)
	clone := realmCLIManagedClone(t, f.joinerHome)
	writeSteeringTestFile(t, filepath.Join(clone, sharedRealmInstructions), updatedRealmStartupGuidance)
	return []string{strings.TrimSpace(updatedRealmStartupGuidance), strings.TrimSpace(teamRealmGuidance), strings.TrimSpace(privateRealmGuidance)}
}

func unboundRealmStartupDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeSteeringTestFile(t, config.RepoConfigPath(dir), "scope = \"realm\"\n")
	writeSteeringTestFile(t, filepath.Join(dir, sharedRealmInstructions), updatedRealmStartupGuidance)
	writeSteeringTestFile(t, filepath.Join(dir, "AGENTS.local.md"), privateRealmGuidance)
	return dir
}

func setRealmStartupPath(t *testing.T, claude string) {
	t.Helper()
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skipf("git unavailable: %v", err)
	}
	bin := t.TempDir()
	for _, executable := range []string{git, claude} {
		if executable == "" {
			continue
		}
		executable, err = filepath.Abs(executable)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(executable, filepath.Join(bin, filepath.Base(executable))); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin)
}

func enrollRealmStartupHarness(t *testing.T, home, kind, nativeDir string) {
	t.Helper()
	root := filepath.Join(home, nativeDir)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	path := config.LedgerPath(home)
	ledger, err := installstate.LoadLedger(path)
	if err != nil {
		t.Fatal(err)
	}
	ledger.UpsertTarget(installstate.TargetRecord{Harness: kind, Instance: root, NativeRoot: root, Status: "enrolled"})
	if err := ledger.Save(path); err != nil {
		t.Fatal(err)
	}
}

func approveRealmStartupLink(t *testing.T, home, path string) {
	t.Helper()
	review := previewRealmReview(t, home, "link", path)
	mustRunRealmCLI(t, home, "link", path, "--yes", "--review", review)
}

func moveRealmStartupFixtureAside(t *testing.T, path string) {
	t.Helper()
	original := path + ".test-original"
	if err := os.Rename(path, original); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Rename(original, path); err != nil {
			t.Error(err)
		}
	})
}

func realmStartupInput(t *testing.T, cwd string) string {
	t.Helper()
	return realmStartupInputForSource(t, cwd, "startup")
}

func realmStartupInputForSource(t *testing.T, cwd, source string) string {
	t.Helper()
	payload, err := json.Marshal(map[string]string{"cwd": cwd, "hook_event_name": "SessionStart", "source": source})
	if err != nil {
		t.Fatal(err)
	}
	return string(payload)
}

func runRealmClaudeInstructionStartup(t *testing.T, f realmLinkCLIFixture, input string) string {
	t.Helper()
	inPath := filepath.Join(t.TempDir(), "stdin.json")
	writeSteeringTestFile(t, inPath, input)
	unchanged := unchangedRealmCLITrees(t, f.joiner, f.joinerHome, f.remote)
	out, diag, code := runAtomicCLIFull(t, f.joinerHome, []string{"ATOMIC_TEST_CLI_IN=" + inPath, "PATH=" + os.Getenv("PATH")},
		"root", "hooks", "session-start", "--harness", "claude", "--realm-instructions")
	unchanged(t)
	if code != 0 {
		t.Fatalf("Claude realm SessionStart: exit %d\nstdout: %s\nstderr: %s", code, out, diag)
	}
	return out
}

func runRealmAdapterStartup(t *testing.T, home, kind, input string, flags ...string) string {
	t.Helper()
	args := append([]string{"root", "hooks", "session-start", "--harness", kind}, flags...)
	env := []string{"PATH=" + os.Getenv("PATH")}
	inPath := os.DevNull
	if input != "" {
		inPath = filepath.Join(t.TempDir(), "stdin.json")
		writeSteeringTestFile(t, inPath, input)
	}
	env = append(env, "ATOMIC_TEST_CLI_IN="+inPath)
	out, diag, code := runAtomicCLIFull(t, home, env, args...)
	if code != 0 {
		t.Fatalf("%s SessionStart: exit %d\nstdout: %s\nstderr: %s", kind, code, out, diag)
	}
	if kind == "omp" {
		return out
	}
	return realmStartupAdditionalContext(t, out)
}

func realmStartupAdditionalContext(t *testing.T, out string) string {
	t.Helper()
	var payload struct {
		HookSpecificOutput struct {
			Event   string `json:"hookEventName"`
			Context string `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("invalid SessionStart JSON: %v\n%s", err, out)
	}
	if payload.HookSpecificOutput.Event != "SessionStart" {
		t.Fatalf("unexpected hookEventName: %q", payload.HookSpecificOutput.Event)
	}
	return payload.HookSpecificOutput.Context
}

func assertRealmStartupContext(t *testing.T, context string, want, forbidden []string) {
	t.Helper()
	for _, text := range want {
		if !strings.Contains(context, text) {
			t.Errorf("startup context does not contain %q:\n%s", text, context)
		}
	}
	for _, text := range forbidden {
		if strings.Contains(context, text) {
			t.Errorf("startup context unexpectedly contains %q:\n%s", text, context)
		}
	}
}

func assertRealmStartupSilent(t *testing.T, out string) {
	t.Helper()
	if strings.TrimSpace(out) != "" {
		t.Errorf("Claude realm SessionStart returned %d bytes, want no payload", len(out))
	}
}
