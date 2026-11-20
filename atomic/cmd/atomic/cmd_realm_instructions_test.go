package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/installstate"
	"github.com/damusix/atomic-claude/atomic/internal/realmshare"
	"github.com/tailscale/hujson"
)

const (
	realmPrivateInstructions   = "AGENTS.local.md"
	realmClaudeLoader          = "CLAUDE.md"
	realmClaudeProjectSettings = ".claude/settings.json"
	realmClaudeLocalSettings   = ".claude/settings.local.json"
	realmAuthoredHookFile      = ".claude/hooks/authored.sh"

	realmClaudeInstructionCommand = "atomic hooks session-start --harness claude --realm-instructions"
	realmAuthoredLoader           = "# Authored realm loader\n\n<atomic>\nExisting Atomic content.\n</atomic>\n\nPersonal loader notes.\n"
	realmAuthoredHook             = "printf 'authored realm hook marker'\n"
	realmAuthoredSettings         = `{
  // Keep the authored realm settings comment.
  "env": {"REALM_LOCAL_MARKER": "local settings only"},
  "permissions": {"allow": ["Read"], "deny": ["Bash"]},
  "hooks": {
    "SessionStart": [
      {"matcher": "resume", "hooks": [{"type": "command", "command": "printf 'authored resume handler'"}]},
    ],
    "Stop": [{"hooks": [{"type": "command", "command": "printf 'authored stop handler'"}]}],
  },
}
`
)

func TestRealmCLILinkWikiOnlyLeavesClaudeFilesUntouched(t *testing.T) {
	bin, claude := realmInstructionTestPATH(t)
	if claude != "" {
		linkRealmInstructionTool(t, bin, "claude", claude)
	}
	const (
		wiki         = "wiki"
		wikiIndex    = "wiki/index.md"
		wikiContents = "# Shared realm wiki\n"
	)
	publisher, publisherHome, remote := newRealmShareCLIFixture(t)
	writeSteeringTestFile(t, filepath.Join(publisher, wikiIndex), wikiContents)
	writeSteeringTestFile(t, filepath.Join(publisher, sharedRealmInstructions), teamRealmGuidance)
	args := []string{wiki + "/", sharedRealmInstructions, "--remote", remote, "--branch", shareBranch}
	review := previewRealmReview(t, publisherHome, "share", args...)
	mustRunRealmCLI(t, publisherHome, "share", append(args, "--yes", "--review", review)...)
	runGit(t, remote, "symbolic-ref", "HEAD", "refs/heads/"+shareBranch)
	if got := runGit(t, remote, "show", "refs/heads/"+shareBranch+":"+wikiIndex); got != wikiContents {
		t.Fatalf("published wiki index = %q, want %q", got, wikiContents)
	}

	f := realmLinkCLIFixture{publisher, publisherHome, t.TempDir(), t.TempDir(), remote}
	writeSteeringTestFile(t, config.RepoConfigPath(f.joiner), "scope = \"realm\"\n")
	writeSteeringTestFile(t, filepath.Join(f.joiner, realmPrivateInstructions), privateRealmGuidance)
	t.Chdir(f.joiner)
	join := mustRunRealmCLI(t, f.joinerHome, "join", remote)
	for _, path := range []string{wiki, sharedRealmInstructions} {
		if !strings.Contains(join, path) {
			t.Errorf("join preview lacks %q:\n%s", path, join)
		}
		assertRealmCLIPathAbsent(t, f.joiner, path)
	}
	clone := realmCLIManagedClone(t, f.joinerHome)
	assertRealmShareCLIFile(t, clone, wikiIndex, wikiContents, false)
	unchangedClaude := seedRealmInstructionClaude(t, f)
	privatePath := filepath.Join(f.joiner, realmPrivateInstructions)
	unchangedPrivate := unchangedRealmCLITrees(t, privatePath)
	unchanged := unchangedRealmCLITrees(t, f.joiner, f.joinerHome, f.remote)
	unchangedLedger := unchangedRealmCLITrees(t, config.LedgerPath(f.joinerHome))
	preview := mustRunRealmCLI(t, f.joinerHome, "link", wiki+"/", "--dry-run")
	if strings.Contains(preview, sharedRealmInstructions) || strings.Contains(preview, "SessionStart") {
		t.Errorf("wiki-only preview includes instruction activation:\n%s", preview)
	}
	unchanged(t)
	out := mustRunRealmCLI(t, f.joinerHome, "link", wiki+"/", "--yes", "--review", realmReviewDigest(t, preview))
	if strings.Contains(out, "SessionStart") {
		t.Errorf("wiki-only link reports instruction activation:\n%s", out)
	}
	assertRealmShareCLIFile(t, f.joiner, wikiIndex, wikiContents, false)
	unchangedWiki := unchangedRealmLinkWitness(t, f, wiki)
	unchangedWiki(t)
	assertRealmCLIPathAbsent(t, f.joiner, sharedRealmInstructions)
	unchangedPrivate(t)
	unchangedClaude(t)
	unchangedLedger(t)
	if err := os.Remove(privatePath); err != nil {
		t.Fatal(err)
	}
	repeat := previewRealmReview(t, f.joinerHome, "link", wiki+"/")
	mustRunRealmCLI(t, f.joinerHome, "link", wiki+"/", "--yes", "--review", repeat)
	assertRealmCLIPathAbsent(t, f.joiner, realmPrivateInstructions)
	unchangedClaude(t)
	unchangedLedger(t)
	unchangedWiki(t)
	assertRealmCLIPathAbsent(t, f.joiner, sharedRealmInstructions)
	assertRealmInstructionRemote(t, f.remote, teamRealmGuidance)
}

func TestRealmCLILinkReconcilesClaudeAfterItAppearsOnPATH(t *testing.T) {
	bin, claude := realmInstructionTestPATH(t)
	f := newRealmLinkCLIFixture(t)
	unchangedClaude := seedRealmInstructionClaude(t, f)
	privatePath := filepath.Join(f.joiner, realmPrivateInstructions)
	if err := os.Remove(privatePath); err != nil {
		t.Fatal(err)
	}

	unchanged := unchangedRealmCLITrees(t, f.joiner, f.joinerHome, f.remote)
	preview, diag, code := runRealmCLI(t, f.joinerHome, "link", sharedRealmInstructions, "--dry-run")
	if code != 0 {
		t.Fatalf("review with Claude missing: exit %d\nstdout: %s\nstderr: %s", code, preview, diag)
	}
	if !strings.Contains(preview, strings.TrimSpace(teamRealmGuidance)) {
		t.Errorf("instruction review omitted the selected team text:\n%s", preview)
	}
	if !slices.ContainsFunc(strings.Split(preview+"\n"+diag, "\n"), func(line string) bool {
		return strings.Contains(line, " PATH") && strings.Contains(strings.ToLower(line), "claude")
	}) {
		t.Errorf("instruction review omitted the missing Claude PATH limitation:\n%s%s", preview, diag)
	}
	unchanged(t)
	review := realmReviewDigest(t, preview)
	out := mustRunRealmCLI(t, f.joinerHome, "link", sharedRealmInstructions, "--yes", "--review", review)
	if strings.Contains(out, realmClaudeInstructionCommand) {
		t.Errorf("link with Claude missing reports a hook command:\n%s", out)
	}
	assertRealmShareCLIFile(t, f.joiner, sharedRealmInstructions, teamRealmGuidance, true)
	unchangedClaude(t)
	if info, err := os.Lstat(privatePath); err != nil || !info.Mode().IsRegular() {
		t.Errorf("approved instruction link did not create a regular private file: %v (%v)", info, err)
	}
	t.Run("late registration", func(t *testing.T) {
		if claude == "" {
			t.Skip("installed Claude executable not found on the original PATH")
		}
		writeSteeringTestFile(t, privatePath, privateRealmGuidance)
		withoutClaude := previewRealmReview(t, f.joinerHome, "link", sharedRealmInstructions)
		linkRealmInstructionTool(t, bin, "claude", claude)
		unchangedAuthored := unchangedRealmCLITrees(t, privatePath,
			filepath.Join(f.joinerHome, ".claude"),
			filepath.Join(f.joiner, realmClaudeProjectSettings), filepath.Join(f.joiner, ".claude/hooks"))
		unchangedLink := unchangedRealmLinkWitness(t, f, sharedRealmInstructions)
		clone := realmCLIManagedClone(t, f.joinerHome)
		unchangedCheckout := unchangedRealmCLITrees(t, clone, f.remote)
		unchanged := unchangedRealmCLITrees(t, f.joiner, f.joinerHome, f.remote)
		preview := mustRunRealmCLI(t, f.joinerHome, "link", sharedRealmInstructions, "--dry-run")
		if !strings.Contains(preview, "SessionStart") || !strings.Contains(preview, realmClaudeLocalSettings) {
			t.Errorf("late link review does not describe the local Claude hook:\n%s", preview)
		}
		lateReview := realmReviewDigest(t, preview)
		if lateReview == withoutClaude {
			t.Error("late registration did not change the approved effect digest")
		}
		unchanged(t)
		out, diag, code := runRealmCLI(t, f.joinerHome, "link", sharedRealmInstructions, "--yes", "--review", withoutClaude)
		if code == 0 || !strings.Contains(diag, "--dry-run") {
			t.Errorf("late link accepted a review from before Claude was on PATH: exit %d\nstdout: %s\nstderr: %s", code, out, diag)
		}
		unchanged(t)
		mustRunRealmCLI(t, f.joinerHome, "link", sharedRealmInstructions, "--yes", "--review", lateReview)
		unchangedLink(t)
		unchangedAuthored(t)
		unchangedCheckout(t)

		loader := readSteeringTestFile(t, filepath.Join(f.joiner, realmClaudeLoader))
		added, ok := strings.CutPrefix(loader, realmAuthoredLoader)
		if !ok || strings.Count(added, "@"+realmPrivateInstructions) != 1 ||
			strings.Contains(added, sharedRealmInstructions) || strings.Contains(added, strings.TrimSpace(teamRealmGuidance)) ||
			strings.Contains(added, strings.TrimSpace(privateRealmGuidance)) {
			t.Errorf("Claude loader must preserve its authored prefix and add only the private import:\n%s", loader)
		}
		assertRealmInstructionSettings(t, f.joiner, 1)

		unchanged = unchangedRealmCLITrees(t, f.joiner, f.remote)
		repeat := previewRealmReview(t, f.joinerHome, "link", sharedRealmInstructions)
		mustRunRealmCLI(t, f.joinerHome, "link", sharedRealmInstructions, "--yes", "--review", repeat)
		unchanged(t)
		unchangedLink(t)

		const sharedEdit = "# Team guidance updated while Claude is registered\n"
		writeSteeringTestFile(t, filepath.Join(f.joiner, sharedRealmInstructions), sharedEdit)
		assertRealmCLISyncSummary(t, mustRunRealmCLI(t, f.joinerHome, "sync"), true, false, true)
		assertRealmInstructionRemote(t, f.remote, sharedEdit)

		mustRunRealmCLI(t, f.joinerHome, "unlink")
		assertRealmCLIPathAbsent(t, f.joiner, sharedRealmInstructions)
		assertRealmShareCLIFile(t, clone, sharedRealmInstructions, sharedEdit, false)
		assertRealmShareCLIFile(t, f.joiner, realmClaudeLoader, realmAuthoredLoader, false)
		assertRealmInstructionSettings(t, f.joiner, 0)
		unchangedAuthored(t)
		assertRealmInstructionRemote(t, f.remote, sharedEdit)
		unchanged = unchangedRealmCLITrees(t, f.joiner, f.remote)
		mustRunRealmCLI(t, f.joinerHome, "unlink")
		unchanged(t)
	})
}

func realmInstructionTestPATH(t *testing.T) (bin, claude string) {
	t.Helper()
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skipf("git unavailable: %v", err)
	}
	claude, err = exec.LookPath("claude")
	if err != nil {
		claude = ""
	} else if claude, err = filepath.Abs(claude); err != nil {
		t.Fatal(err)
	}
	bin = t.TempDir()
	linkRealmInstructionTool(t, bin, "git", git)
	t.Setenv("PATH", bin)
	if path, err := exec.LookPath("claude"); err == nil {
		t.Fatalf("Claude unexpectedly on the fixture PATH: %s", path)
	}
	return bin, claude
}

func linkRealmInstructionTool(t *testing.T, bin, name, executable string) {
	t.Helper()
	target, err := filepath.Abs(executable)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(bin, name)); err != nil {
		t.Fatal(err)
	}
}

func seedRealmInstructionClaude(t *testing.T, f realmLinkCLIFixture) func(*testing.T) {
	t.Helper()
	root := filepath.Join(f.joinerHome, ".claude")
	ledger := &installstate.Ledger{
		Header:  installstate.NewHeader(),
		Targets: []installstate.TargetRecord{{Harness: "claude", Instance: root}},
	}
	if err := ledger.Save(config.LedgerPath(f.joinerHome)); err != nil {
		t.Fatal(err)
	}
	writeSteeringTestFile(t, filepath.Join(root, "settings.json"), "{\"env\":{\"GLOBAL_REALM_MARKER\":\"keep global settings\"}}\n")
	writeSteeringTestFile(t, filepath.Join(f.joiner, realmClaudeLoader), realmAuthoredLoader)
	writeSteeringTestFile(t, filepath.Join(f.joiner, realmClaudeProjectSettings), "{\"env\":{\"PROJECT_REALM_MARKER\":\"keep shared project settings local\"}}\n")
	writeSteeringTestFile(t, filepath.Join(f.joiner, realmClaudeLocalSettings), realmAuthoredSettings)
	writeSteeringTestFile(t, filepath.Join(f.joiner, realmAuthoredHookFile), realmAuthoredHook)
	return unchangedRealmCLITrees(t, root, filepath.Join(f.joiner, realmClaudeLoader), filepath.Join(f.joiner, ".claude"))
}

func unchangedRealmLinkWitness(t *testing.T, f realmLinkCLIFixture, selected string) func(*testing.T) {
	t.Helper()
	readBinding := func(t *testing.T) realmshare.Binding {
		t.Helper()
		restore := config.SetHomeDirForTest(f.joinerHome)
		defer restore()
		b, err := realmshare.LoadBinding(f.joiner)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	before := readBinding(t).Links
	if len(before) != 1 || before[0].Path != selected || before[0].Source != selected || before[0].Witness == "" {
		t.Fatalf("expected a recorded link and witness for %q: %+v", selected, before)
	}
	link := filepath.Join(f.joiner, selected)
	witness := filepath.Join(filepath.Dir(config.RepoConfigPath(f.joiner)), filepath.FromSlash(before[0].Witness))
	info, err := os.Lstat(witness)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("witness for %q is not a symlink: %s", selected, witness)
	}
	source := filepath.Join(realmCLIManagedClone(t, f.joinerHome), filepath.FromSlash(before[0].Source))
	target, err := os.Stat(source)
	if err != nil {
		t.Fatal(err)
	}
	unchanged := unchangedRealmCLITrees(t, link, witness)
	return func(t *testing.T) {
		t.Helper()
		unchanged(t)
		for _, path := range []string{link, witness} {
			if after, err := os.Lstat(path); err != nil || !os.SameFile(info, after) {
				t.Errorf("original witness does not own %s: %v", path, err)
			}
		}
		if after, err := os.Stat(link); err != nil || !os.SameFile(target, after) {
			t.Errorf("%s does not resolve to the original managed source %s: %v", link, source, err)
		}
		if after := readBinding(t).Links; !slices.Equal(before, after) {
			t.Errorf("link receipt changed: %+v -> %+v", before, after)
		}
	}
}

func assertRealmInstructionSettings(t *testing.T, root string, want int) {
	t.Helper()
	parse := func(raw string) map[string]any {
		t.Helper()
		standard, err := hujson.Standardize([]byte(raw))
		if err != nil {
			t.Fatal(err)
		}
		var result map[string]any
		if err := json.Unmarshal(standard, &result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	raw := readSteeringTestFile(t, filepath.Join(root, realmClaudeLocalSettings))
	if !strings.Contains(raw, "// Keep the authored realm settings comment.") {
		t.Errorf("authored settings comment disappeared:\n%s", raw)
	}
	got := parse(raw)
	hooks, ok := got["hooks"].(map[string]any)
	if !ok {
		t.Fatalf("settings lack a hooks object:\n%s", raw)
	}
	starts, ok := hooks["SessionStart"].([]any)
	if !ok {
		t.Fatalf("settings lack the SessionStart list:\n%s", raw)
	}
	kept := make([]any, 0, len(starts))
	var count int
	for _, start := range starts {
		group, ok := start.(map[string]any)
		if !ok {
			t.Fatalf("invalid SessionStart group: %#v", start)
		}
		handlers, ok := group["hooks"].([]any)
		if !ok || len(handlers) == 0 {
			t.Fatalf("invalid SessionStart handlers: %#v", group)
		}
		remaining := slices.DeleteFunc(handlers, func(value any) bool {
			handler, ok := value.(map[string]any)
			if !ok || handler["command"] != realmClaudeInstructionCommand {
				return false
			}
			if handler["type"] != "command" {
				t.Errorf("realm SessionStart handler is not a command: %#v", handler)
			}
			count++
			return true
		})
		if len(remaining) != 0 {
			group["hooks"] = remaining
			kept = append(kept, group)
		}
	}
	if count != want {
		t.Errorf("owned Claude SessionStart handlers = %d, want %d:\n%s", count, want, raw)
	}
	hooks["SessionStart"] = kept
	if !reflect.DeepEqual(got, parse(realmAuthoredSettings)) {
		t.Errorf("registration changed unrelated Claude settings or hooks:\n%s", raw)
	}
}

func assertRealmInstructionRemote(t *testing.T, remote, team string) {
	t.Helper()
	ref := "refs/heads/" + shareBranch
	if got := runGit(t, remote, "show", ref+":"+sharedRealmInstructions); got != team {
		t.Errorf("remote team guidance = %q, want %q", got, team)
	}
	for _, path := range strings.Split(strings.TrimSpace(runGit(t, remote, "ls-tree", "-r", "--name-only", ref)), "\n") {
		if path == realmPrivateInstructions || path == realmClaudeLoader || strings.HasPrefix(path, ".claude/") {
			t.Errorf("remote tree contains private startup path %q", path)
		}
		contents := runGit(t, remote, "show", ref+":"+path)
		for _, marker := range []string{strings.TrimSpace(privateRealmGuidance), "Authored realm loader", "authored realm hook marker", "local settings only", realmClaudeInstructionCommand} {
			if strings.Contains(contents, marker) {
				t.Errorf("remote blob %q contains local marker %q", path, marker)
			}
		}
	}
}
