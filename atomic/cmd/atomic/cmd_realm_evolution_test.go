package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/realmshare"
)

func TestRealmCLIContinuousLifecycle(t *testing.T) {
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skipf("git unavailable: %v", err)
	}
	bin := t.TempDir()
	linkRealmInstructionTool(t, bin, "git", git)
	t.Setenv("PATH", bin)
	t.Setenv("HOME", t.TempDir())
	for _, entry := range os.Environ() {
		if name, _, _ := strings.Cut(entry, "="); strings.HasPrefix(name, "GIT_") {
			t.Setenv(name, "")
			if err := os.Unsetenv(name); err != nil {
				t.Fatal(err)
			}
		}
	}
	t.Setenv("GIT_ALLOW_PROTOCOL", "file")
	t.Setenv("GIT_TERMINAL_PROMPT", "0")
	for _, name := range []string{"PI_CONFIG_DIR", "PI_CODING_AGENT_DIR", "PI_PROFILE", "XDG_CONFIG_HOME"} {
		t.Setenv(name, "")
	}
	t.Setenv(config.StateDirEnvVar, ".atomic")

	const (
		ref           = "refs/heads/" + shareBranch
		bucket        = "research"
		page          = bucket + "/field/report.md"
		child         = bucket + "/field/followups/checklist.md"
		original      = "# Initial field report\n"
		edited        = "# Joiner's clean edit\n"
		created       = "# New follow-up checklist\n"
		teamUpdate    = "# Team guidance: keep field reports concise\n"
		publisherEdit = "# Publisher's conflicting finding\n"
		joinerEdit    = "# Joiner's conflicting finding\n"
		resolved      = "# Field report: both findings reconciled\n"
		localEdit     = "# Publisher's private edit to the restored report\n"
		replacement   = "# Joiner's local replacement guidance\n"
		member        = "repos/example"
		sibling       = ".agents/skills/personal-review"
		native        = ".claude/commands/personal.md"
		outside       = "new-private-note.md"
		privateA      = "# Publisher private lifecycle marker\n"
		privateB      = "# Joiner private lifecycle marker\n"
	)
	canonical := func(path string) string {
		t.Helper()
		physical, err := filepath.EvalSymlinks(path)
		if err != nil {
			t.Fatal(err)
		}
		return physical
	}
	publisher, aHome, remote := newRealmShareCLIFixture(t)
	publisher = canonical(publisher)
	joiner, bHome := canonical(t.TempDir()), t.TempDir()
	t.Chdir(publisher)
	mustRunWikiBucketCLI(t, aHome, publisher, "add", bucket)
	writeSteeringTestFile(t, filepath.Join(publisher, page), original)
	writeSteeringTestFile(t, filepath.Join(publisher, sharedRealmInstructions), teamRealmGuidance)
	writeSteeringTestFile(t, filepath.Join(publisher, cliArtifactDoc), cliArtifactSkillText)
	writeSteeringTestFile(t, filepath.Join(publisher, sibling, "SKILL.md"), "# Unselected sibling skill\n")
	seedLocal := func(root, home, private string) func(*testing.T) {
		t.Helper()
		memberRoot := filepath.Join(root, member)
		writeSteeringTestFile(t, filepath.Join(memberRoot, "README.md"), private)
		runGit(t, memberRoot, "init", "--quiet")
		runGit(t, memberRoot, "add", "--", "README.md")
		runGit(t, memberRoot, "commit", "--quiet", "-m", "Keep member history local")
		writeSteeringTestFile(t, filepath.Join(memberRoot, "draft.md"), "# Untracked member work\n")
		settings := `{"env":{"REALM_LIFECYCLE":"local only"}}` + "\n"
		for path, data := range map[string]string{
			realmPrivateInstructions:   private,
			realmClaudeLoader:          "# Authored realm loader\n\nPersonal notes only.\n",
			realmClaudeProjectSettings: settings,
			realmClaudeLocalSettings:   settings,
			native:                     "# Authored native workflow\n",
		} {
			writeSteeringTestFile(t, filepath.Join(root, path), data)
		}
		profile := filepath.Join(home, ".claude")
		for _, dir := range []string{config.Dir(home), profile} {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
		}
		// Non-realm CLI startup initializes the legacy state link independently of sharing.
		_, diag, code := runAtomicCLIFull(t, home, []string{"PATH=" + os.Getenv("PATH")}, "root", "where", "--json")
		if code != 0 {
			t.Fatalf("initialize fixture HOME %s: where exit %d\n%s", home, code, diag)
		}
		assertRealmArtifactTarget(t, filepath.Join(profile, ".atomic"), config.Dir(home))
		writeSteeringTestFile(t, filepath.Join(profile, "settings.json"), settings)
		return unchangedRealmCLITrees(t, memberRoot, profile, filepath.Join(root, ".claude"),
			filepath.Join(root, realmPrivateInstructions), filepath.Join(root, realmClaudeLoader))
	}
	aLocal := seedLocal(publisher, aHome, privateA)
	wikiIndex := filepath.Join(publisher, "wiki", "index.md")
	writeSteeringTestFile(t, wikiIndex, readSteeringTestFile(t, wikiIndex)+
		fmt.Sprintf("\n<wiki-scan>\n  <repo path=%q status=\"indexed\"/>\n</wiki-scan>\n", filepath.Join(publisher, member)))

	paths := []string{bucket + "/", sharedRealmInstructions, cliArtifactSkill + "/"}
	shareArgs := append(slices.Clone(paths), "--remote", remote, "--branch", shareBranch)
	publication := mustRunRealmCLI(t, aHome, "share", append(slices.Clone(shareArgs), "--dry-run")...)
	for _, detail := range []string{`Register bucket "research" at "research"`, member, cliArtifactDoc} {
		if !strings.Contains(publication, detail) {
			t.Errorf("publication review lacks %q:\n%s", detail, publication)
		}
	}
	if refs := runGit(t, remote, "for-each-ref"); refs != "" {
		t.Fatalf("publication preview changed the remote: %s", refs)
	}
	mustRunRealmCLI(t, aHome, "share", append(shareArgs, "--yes", "--review", realmReviewDigest(t, publication))...)
	runGit(t, remote, "symbolic-ref", "HEAD", ref)
	aClone := canonical(realmCLIManagedClone(t, aHome))

	writeSteeringTestFile(t, config.RepoConfigPath(joiner), "scope = \"realm\"\n")
	t.Chdir(joiner)
	joined := mustRunRealmCLI(t, bHome, "join", remote)
	for _, path := range []string{bucket, sharedRealmInstructions, cliArtifactSkill} {
		if !strings.Contains(joined, fmt.Sprintf("%q", path)) {
			t.Errorf("join preview omitted %q:\n%s", path, joined)
		}
		assertRealmCLIPathAbsent(t, joiner, path)
	}
	assertRealmCLIPathAbsent(t, joiner, realmPrivateInstructions)
	assertRealmCLIPathAbsent(t, joiner, member)
	bClone := canonical(realmCLIManagedClone(t, bHome))
	bLocal := seedLocal(joiner, bHome, privateB)
	link := func(home string) {
		t.Helper()
		review := previewRealmReview(t, home, "link", paths...)
		mustRunRealmCLI(t, home, "link", append(slices.Clone(paths), "--yes", "--review", review)...)
	}
	approval := mustRunRealmCLI(t, bHome, "link", append(slices.Clone(paths), "--dry-run")...)
	for _, detail := range []string{strings.TrimSpace(teamRealmGuidance), "REALM_SKILL_BODY_MARKER"} {
		if !strings.Contains(approval, detail) {
			t.Errorf("link review lacks %q:\n%s", detail, approval)
		}
	}
	assertRealmCLIPathAbsent(t, joiner, bucket)
	assertRealmCLIPathAbsent(t, joiner, sharedRealmInstructions)
	assertRealmCLIPathAbsent(t, joiner, cliArtifactSkill)
	mustRunRealmCLI(t, bHome, "link", append(slices.Clone(paths), "--yes", "--review", realmReviewDigest(t, approval))...)

	for _, dev := range []struct{ root, home, clone string }{
		{publisher, aHome, aClone}, {joiner, bHome, bClone},
	} {
		for _, path := range paths {
			local := filepath.Join(dev.root, path)
			if realmArtifactLstat(t, local).Mode()&os.ModeSymlink == 0 {
				t.Fatalf("selected path is not a symlink: %s", local)
			}
			assertRealmArtifactTarget(t, local, filepath.Join(dev.clone, path))
		}
		assertRealmShareCLIFile(t, dev.root, page, original, false)
		assertRealmShareCLIFile(t, dev.root, cliArtifactDoc, cliArtifactSkillText, false)
		assertWikiBucketCLIList(t, dev.home, dev.root, bucket+"\t"+filepath.Join(dev.root, bucket))
	}
	assertRealmCLIPathAbsent(t, joiner, sibling)

	logical := filepath.Dir(filepath.Join(joiner, page))
	aStatus := readRealmEvolutionStatus(t, aHome, publisher)
	bStatus := readRealmEvolutionStatus(t, bHome, logical)
	if aStatus.RealmID == "" || aStatus.RealmID != bStatus.RealmID || os.SameFile(realmArtifactLstat(t, aClone), realmArtifactLstat(t, bClone)) {
		t.Fatalf("expected one shared realm in independent checkouts: A = %+v, B = %+v", aStatus, bStatus)
	}
	for _, dev := range []struct {
		status      realmshare.StatusReport
		root, clone string
	}{
		{aStatus, publisher, aClone}, {bStatus, joiner, bClone},
	} {
		if got := dev.status; got.RealmRoot != dev.root || got.StateRoot != filepath.Join(dev.root, ".atomic") || got.ClonePath != dev.clone {
			t.Fatalf("status selected the wrong local realm, state or clone for %s: %+v", dev.root, got)
		}
		assertRealmCLIPathAbsent(t, dev.clone, filepath.Join(bucket, ".atomic"))
		assertRealmCLIPathAbsent(t, dev.clone, filepath.Join(filepath.Dir(page), ".atomic"))
	}

	writeSteeringTestFile(t, filepath.Join(joiner, page), edited)
	writeSteeringTestFile(t, filepath.Join(joiner, child), created)
	writeSteeringTestFile(t, filepath.Join(joiner, outside), privateB)
	t.Chdir(joiner)
	assertRealmCLISyncSummary(t, mustRunRealmCLI(t, bHome, "sync"), true, false, true)
	assertRealmShareCLIFile(t, publisher, page, original, false)
	assertRealmCLIPathAbsent(t, publisher, child)
	t.Chdir(publisher)
	assertRealmCLISyncSummary(t, mustRunRealmCLI(t, aHome, "sync"), false, true, false)
	assertRealmShareCLIFile(t, publisher, page, edited, false)
	assertRealmShareCLIFile(t, publisher, child, created, false)
	writeSteeringTestFile(t, filepath.Join(publisher, sharedRealmInstructions), teamUpdate)
	assertRealmCLISyncSummary(t, mustRunRealmCLI(t, aHome, "sync"), true, false, true)
	assertRealmShareCLIFile(t, joiner, sharedRealmInstructions, teamRealmGuidance, true)
	t.Chdir(joiner)
	assertRealmCLISyncSummary(t, mustRunRealmCLI(t, bHome, "sync"), false, true, false)
	assertRealmShareCLIFile(t, joiner, sharedRealmInstructions, teamUpdate, true)

	writeSteeringTestFile(t, filepath.Join(joiner, page), joinerEdit)
	writeSteeringTestFile(t, filepath.Join(publisher, page), publisherEdit)
	t.Chdir(publisher)
	assertRealmCLISyncSummary(t, mustRunRealmCLI(t, aHome, "sync"), true, false, true)
	published := runGit(t, remote, "rev-parse", ref)
	t.Chdir(joiner)
	out, diag, code := runRealmCLI(t, bHome, "sync")
	if code == 0 || !strings.Contains(diag, "merge conflict") {
		t.Fatalf("expected a real merge conflict: exit %d\nstdout: %s\nstderr: %s", code, out, diag)
	}
	for _, hint := range []string{
		fmt.Sprintf("clone path %q", page),
		fmt.Sprintf("realm path %q", filepath.Join(joiner, page)),
		"git -C " + quotedRealmCLIShellPath(bClone) + " add --",
		"git -C " + quotedRealmCLIShellPath(bClone) + " commit",
	} {
		if !strings.Contains(diag, hint) {
			t.Errorf("conflict recovery diagnostic lacks %q:\n%s", hint, diag)
		}
	}
	if mergeHead := runGit(t, bClone, "rev-parse", "MERGE_HEAD"); mergeHead != published ||
		runGit(t, bClone, "ls-files", "--unmerged", "--", page) == "" || runGit(t, remote, "rev-parse", ref) != published {
		t.Fatal("conflicting sync did not preserve Git's unmerged state and the remote tip")
	}
	conflicted := readSteeringTestFile(t, filepath.Join(joiner, page))
	if !strings.Contains(conflicted, strings.TrimSpace(publisherEdit)) || !strings.Contains(conflicted, strings.TrimSpace(joinerEdit)) {
		t.Fatalf("the logical report does not preserve both conflicting edits:\n%s", conflicted)
	}

	t.Chdir(publisher)
	mustRunRealmCLI(t, aHome, "unlink")
	assertRestored := func() {
		t.Helper()
		if !realmArtifactLstat(t, filepath.Join(publisher, bucket)).IsDir() {
			t.Fatalf("unlink did not restore the original %s directory", bucket)
		}
		assertRealmShareCLIFile(t, publisher, page, original, false)
		assertRealmCLIPathAbsent(t, publisher, child)
		assertRealmShareCLIFile(t, publisher, sharedRealmInstructions, teamRealmGuidance, false)
		if !realmArtifactLstat(t, filepath.Join(publisher, cliArtifactSkill)).IsDir() {
			t.Fatal("unlink did not restore the original skill directory")
		}
		assertRealmShareCLIFile(t, publisher, cliArtifactDoc, cliArtifactSkillText, false)
	}
	assertRestored()

	t.Chdir(joiner)
	writeSteeringTestFile(t, filepath.Join(bClone, page), resolved)
	runGit(t, joiner, "-C", bClone, "add", "--", page)
	runGit(t, joiner, "-C", bClone, "commit", "-m", "Resolve shared field report")
	merge := runGit(t, bClone, "rev-parse", "HEAD")
	assertRealmCLISyncSummary(t, mustRunRealmCLI(t, bHome, "sync"), false, false, true)
	assertRealmShareCLIFile(t, joiner, page, resolved, false)

	t.Chdir(publisher)
	assertRealmCLISyncSummary(t, mustRunRealmCLI(t, aHome, "sync"), false, true, false)
	assertRestored()
	assertRealmShareCLIFile(t, aClone, page, resolved, false)
	link(aHome)
	assertRealmArtifactTarget(t, filepath.Join(publisher, bucket), filepath.Join(aClone, bucket))
	assertRealmShareCLIFile(t, publisher, page, resolved, false)
	assertRealmShareCLIFile(t, publisher, child, created, false)
	assertRealmShareCLIFile(t, publisher, sharedRealmInstructions, teamUpdate, true)
	mustRunRealmCLI(t, aHome, "unlink")
	assertRestored()
	review := previewRealmReview(t, aHome, "link", paths...)
	writeSteeringTestFile(t, filepath.Join(publisher, page), localEdit)
	unchanged := unchangedRealmCLITrees(t, publisher, aHome, remote)
	out, diag, code = runRealmCLI(t, aHome, "link", append(slices.Clone(paths), "--yes", "--review", review)...)
	if code == 0 || !strings.Contains(diag, `restored original "research" changed`) {
		t.Fatalf("relink did not refuse the edited restored original: exit %d\nstdout: %s\nstderr: %s", code, out, diag)
	}
	unchanged(t)
	assertRealmShareCLIFile(t, publisher, page, localEdit, false)
	assertRealmShareCLIFile(t, aClone, page, resolved, false)

	t.Chdir(joiner)
	mustRunRealmCLI(t, bHome, "unlink")
	for _, path := range []string{bucket, sharedRealmInstructions, cliArtifactSkill} {
		assertRealmCLIPathAbsent(t, joiner, path)
	}
	link(bHome)
	assertRealmShareCLIFile(t, joiner, page, resolved, false)
	assertRealmShareCLIFile(t, joiner, sharedRealmInstructions, teamUpdate, true)
	if err := os.Remove(filepath.Join(joiner, sharedRealmInstructions)); err != nil {
		t.Fatal(err)
	}
	writeSteeringTestFile(t, filepath.Join(joiner, sharedRealmInstructions), replacement)
	status := readRealmEvolutionStatus(t, bHome, logical)
	if !slices.ContainsFunc(status.Paths, func(item realmshare.StatusPath) bool {
		return item.Path == sharedRealmInstructions && item.State == "conflicting" && item.Drift
	}) {
		t.Errorf("status did not report drift on the replaced team link: %+v", status.Paths)
	}
	assertRealmShareCLIFile(t, joiner, sharedRealmInstructions, replacement, false)
	assertRealmShareCLIFile(t, bClone, sharedRealmInstructions, teamUpdate, false)

	aLocal(t)
	bLocal(t)
	assertRealmShareCLIFile(t, joiner, outside, privateB, false)
	assertRealmCLIPathAbsent(t, publisher, outside)
	assertRealmShareCLIFile(t, publisher, filepath.Join(sibling, "SKILL.md"), "# Unselected sibling skill\n", false)
	if head := runGit(t, remote, "rev-parse", ref); head != merge {
		t.Errorf("remote head = %q, want the published resolution %q", head, merge)
	}
	for path, want := range map[string]string{page: resolved, child: created, sharedRealmInstructions: teamUpdate, cliArtifactDoc: cliArtifactSkillText} {
		if got := runGit(t, remote, "show", ref+":"+path); got != want {
			t.Errorf("remote %s = %q, want %q", path, got, want)
		}
	}
	if leaked := runGit(t, remote, "ls-tree", "-r", "--name-only", ref, "--", realmPrivateInstructions,
		realmClaudeLoader, "AGENTS.md", ".claude", ".atomic", sharePrivate, outside, sibling, member, "wiki"); leaked != "" {
		t.Errorf("remote tree contains local-only or unselected paths:\n%s", leaked)
	}
}

func readRealmEvolutionStatus(t *testing.T, home, logical string) realmshare.StatusReport {
	t.Helper()
	t.Chdir(logical)
	env := []string{"PATH=" + os.Getenv("PATH"), "PWD=" + logical}
	out, diag, code := runAtomicCLIFull(t, home, env, "root", "realm", "status", "--json")
	if code != 0 {
		t.Fatalf("realm status from %s: exit %d\nstdout: %s\nstderr: %s", logical, code, out, diag)
	}
	var status realmshare.StatusReport
	if err := json.Unmarshal([]byte(out), &status); err != nil {
		t.Fatalf("realm status from %s: %v\n%s", logical, err, out)
	}
	return status
}
