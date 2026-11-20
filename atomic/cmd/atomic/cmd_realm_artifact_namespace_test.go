package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/realmshare"
)

func TestRealmCLIArtifactShareRefusesCloneAtCodexHome(t *testing.T) {
	realmInstructionTestPATH(t)
	root, home, remote := newRealmShareCLIFixture(t)
	writeSteeringTestFile(t, filepath.Join(root, cliArtifactDoc), cliArtifactSkillText)

	clone := realmArtifactProposedClone(t, root, home)
	args := []string{cliArtifactSkill + "/", "--remote", remote, "--branch", shareBranch}
	preview := mustRunRealmCLI(t, home, "share", append(args, "--dry-run")...)
	if !strings.Contains(preview, "Managed clone: "+strconv.Quote(clone)) {
		t.Fatalf("share did not propose the expected checkout %q:\n%s", clone, preview)
	}
	assertRealmCLIPathAbsent(t, filepath.Dir(clone), filepath.Base(clone))
	unchanged := unchangedRealmCLITrees(t, root, remote)

	t.Setenv("CODEX_HOME", clone)
	out, diag, code := runRealmCLI(t, home, "share", append(args, "--dry-run")...)
	if code == 0 {
		review := realmReviewDigest(t, out)
		out, diag, code = runRealmCLI(t, home, "share", append(args, "--yes", "--review", review)...)
	}
	if code == 0 || !strings.Contains(out+diag, clone) || !realmArtifactMentions(out+diag, "codex_home", "profile") {
		t.Errorf("share should refuse the checkout at the configured Codex profile: exit %d\nstdout: %s\nstderr: %s", code, out, diag)
	}
	assertRealmCLIPathAbsent(t, filepath.Dir(clone), filepath.Base(clone))
	unchanged(t)
	if refs := runGit(t, remote, "for-each-ref"); refs != "" {
		t.Errorf("refused share published remote refs:\n%s", refs)
	}
}

func TestRealmCLIArtifactJoinRefusesCloneAtCodexHome(t *testing.T) {
	realmInstructionTestPATH(t)
	f := newRealmArtifactCLIFixture(t)
	writeSteeringTestFile(t, filepath.Join(f.publisher, cliArtifactDoc), cliArtifactSkillText)
	args := []string{cliArtifactSkill + "/", "--remote", f.remote, "--branch", shareBranch}
	review := previewRealmReview(t, f.publisherHome, "share", args...)
	mustRunRealmCLI(t, f.publisherHome, "share", append(args, "--yes", "--review", review)...)
	runGit(t, f.remote, "symbolic-ref", "HEAD", "refs/heads/"+shareBranch)

	t.Chdir(f.joiner)
	clone := realmArtifactProposedClone(t, f.joiner, f.joinerHome)
	t.Setenv("CODEX_HOME", clone)
	assertRealmCLIPathAbsent(t, filepath.Dir(clone), filepath.Base(clone))
	unchanged := unchangedRealmCLITrees(t, f.joiner, f.remote)

	out, diag, code := runRealmCLI(t, f.joinerHome, "join", f.remote)
	if code == 0 || !strings.Contains(out+diag, clone) || !realmArtifactMentions(out+diag, "codex_home", "profile") {
		t.Errorf("join should refuse the checkout at the configured Codex profile: exit %d\nstdout: %s\nstderr: %s", code, out, diag)
	}
	assertRealmCLIPathAbsent(t, filepath.Dir(clone), filepath.Base(clone))
	assertRealmCLIPathAbsent(t, filepath.Dir(config.RepoConfigPath(f.joiner)), "realm-share.toml")
	unchanged(t)
}

func TestRealmCLIArtifactSyncRefusesCloneAtUnicodeCodexHome(t *testing.T) {
	realmInstructionTestPATH(t)
	_, home, remote := newRealmShareCLIFixture(t)
	const composed, decomposed = "caf\u00e9", "cafe\u0301"
	root := filepath.Join(t.TempDir(), composed)
	writeSteeringTestFile(t, config.RepoConfigPath(root), "scope = \"realm\"\n")
	writeSteeringTestFile(t, filepath.Join(root, cliArtifactDoc), cliArtifactSkillText)
	t.Chdir(root)
	args := []string{cliArtifactSkill + "/", "--remote", remote, "--branch", shareBranch}
	review := previewRealmReview(t, home, "share", args...)
	mustRunRealmCLI(t, home, "share", append(args, "--yes", "--review", review)...)

	restoreHome := config.SetHomeDirForTest(home)
	binding, err := realmshare.LoadBinding(root)
	restoreHome()
	if err != nil {
		t.Fatal(err)
	}
	clone := binding.ClonePath
	var aliasName string
	switch filepath.Base(clone) {
	case composed:
		aliasName = decomposed
	case decomposed:
		aliasName = composed
	default:
		t.Fatalf("managed checkout does not have the expected Unicode name: %q", clone)
	}
	profile := filepath.Join(filepath.Dir(clone), aliasName)
	cloneInfo, err := os.Stat(clone)
	if err != nil {
		t.Fatal(err)
	}
	profileInfo, err := os.Stat(profile)
	if os.IsNotExist(err) {
		t.Skip("filesystem does not resolve Unicode normalization aliases")
	}
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(cloneInfo, profileInfo) {
		t.Skip("filesystem does not identify the two spellings as the same checkout")
	}

	writeSteeringTestFile(t, filepath.Join(clone, cliArtifactDoc), cliArtifactSkillText+"\nAn ordinary local review update.\n")
	unchanged := unchangedRealmCLITrees(t, clone, remote)
	t.Setenv("CODEX_HOME", profile)
	out, diag, code := runRealmCLI(t, home, "sync")
	if code == 0 || !realmArtifactMentions(out+diag, "clone", "checkout") || !realmArtifactMentions(out+diag, "codex_home", "profile") {
		t.Errorf("sync should refuse the checkout at the Unicode alias of its Codex profile: exit %d\nstdout: %s\nstderr: %s", code, out, diag)
	}
	unchanged(t)
}

func TestRealmCLIArtifactLinkPreviewShowsCodexImplicitInvocation(t *testing.T) {
	realmInstructionTestPATH(t)
	f := newRealmArtifactCLIFixture(t)
	const (
		sidecar = cliArtifactSkill + "/agents/openai.yaml"
		tail    = "SIDECAR_COMMENT_NOT_NEEDED_IN_PREVIEW"
	)
	sidecarText := "policy:\n  allow_implicit_invocation: false\n" +
		strings.Repeat("# Optional project guidance.\n", 512) + "# " + tail + "\n"
	writeSteeringTestFile(t, filepath.Join(f.publisher, cliArtifactDoc), cliArtifactSkillText)
	writeSteeringTestFile(t, filepath.Join(f.publisher, sidecar), sidecarText)
	publishRealmArtifactFixture(t, f, cliArtifactSkill+"/")
	clone := realmCLIManagedClone(t, f.joinerHome)
	assertRealmShareCLIFile(t, clone, sidecar, sidecarText, false)

	profile := filepath.Join(f.joinerHome, "codex-profile")
	t.Setenv("CODEX_HOME", profile)
	const permissions = "sandbox_mode = \"read-only\"\n"
	writeSteeringTestFile(t, filepath.Join(profile, "config.toml"), permissions)
	writeSteeringTestFile(t, filepath.Join(f.joiner, ".codex", "config.toml"), permissions)
	unchanged := unchangedRealmCLITrees(t, f.joiner, f.joinerHome, f.remote)

	out := mustRunRealmCLI(t, f.joinerHome, "link", cliArtifactSkill+"/", "--dry-run")
	realmReviewDigest(t, out)
	value := regexp.MustCompile(`\ballow_implicit_invocation\b[\\"']*[ \t]*[:=][ \t]*false\b`)
	if !strings.Contains(out, sidecar) || !value.MatchString(out) {
		t.Errorf("link preview did not show the Codex sidecar's effective allow_implicit_invocation: false:\n%s", out)
	}
	if len(out) >= len(sidecarText) || strings.Contains(out, tail) {
		t.Errorf("link preview should summarize sidecar values without dumping the %d-byte sidecar (got %d bytes)", len(sidecarText), len(out))
	}
	assertRealmCLIPathAbsent(t, f.joiner, cliArtifactSkill)
	unchanged(t)
}

func realmArtifactProposedClone(t *testing.T, root, home string) string {
	t.Helper()
	physicalHome, err := filepath.EvalSymlinks(home)
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(config.Dir(physicalHome), "realms", filepath.Base(root))
}
