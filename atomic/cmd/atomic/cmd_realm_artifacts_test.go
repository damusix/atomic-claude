package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/installstate"
	"github.com/damusix/atomic-claude/atomic/internal/realmshare"
)

const (
	cliArtifactSkill   = ".agents/skills/realm-review"
	cliArtifactDoc     = cliArtifactSkill + "/SKILL.md"
	cliArtifactCommand = ".agents/commands/realm-triage.md"
	cliArtifactAgent   = ".agents/agents/realm-reviewer.md"

	cliArtifactSkillText   = "---\nname: realm-review\ndescription: Show the realm review marker.\n---\n\nREALM_SKILL_BODY_MARKER\n"
	cliArtifactCommandText = "---\ndescription: Show the realm triage marker.\n---\n\nREALM_COMMAND_BODY_MARKER\n"
	cliArtifactAgentText   = "---\nname: realm-reviewer\ndescription: Show the realm reviewer marker.\n---\n\nREALM_AGENT_BODY_MARKER\n"
)

func TestRealmCLIArtifactsOptInNativeLeaves(t *testing.T) {
	useRealmArtifactClaude(t)
	f := newRealmArtifactCLIFixture(t)
	const (
		sample        = cliArtifactSkill + "/examples/sample.md"
		large         = cliArtifactSkill + "/examples/long.md"
		available     = ".agents/commands/realm-followup.md"
		personal      = ".agents/commands/personal.md"
		omittedMarker = "REALM_UNSELECTED_COMMAND_MARKER"
		longTail      = "REALM_LONG_SUPPORTING_FILE_TAIL"
		privateA      = "# Publisher private marker\n"
	)
	longText := strings.Repeat("A plain example line.\n", 4096) + longTail + "\n"
	files := map[string]string{
		cliArtifactDoc:           cliArtifactSkillText,
		cliArtifactCommand:       cliArtifactCommandText,
		cliArtifactAgent:         cliArtifactAgentText,
		sample:                   "# Shared review example\n",
		large:                    longText,
		available:                "---\ndescription: Show the followup marker.\n---\n\n" + omittedMarker + "\n",
		personal:                 privateA,
		realmPrivateInstructions: privateA,
	}
	for path, text := range files {
		writeSteeringTestFile(t, filepath.Join(f.publisher, path), text)
	}
	privatePublisher := unchangedRealmCLITrees(t, filepath.Join(f.publisher, personal), filepath.Join(f.publisher, realmPrivateInstructions))

	memberRel := "repos/example"
	publisherMember := filepath.Join(f.publisher, memberRel)
	member := filepath.Join(f.joiner, memberRel)
	for _, root := range []string{publisherMember, member} {
		writeSteeringTestFile(t, filepath.Join(root, realmClaudeProjectSettings), `{"env":{"REALM_MEMBER_MARKER":"keep"}}`+"\n")
		runGit(t, root, "init", "--quiet")
	}
	writeSteeringTestFile(t, filepath.Join(f.publisher, "wiki/index.md"),
		"# Portable wiki\n\n<wiki-scan>\n  <repo path=\""+filepath.ToSlash(publisherMember)+"\" status=\"indexed\"/>\n</wiki-scan>\n")
	publisherShare, join := publishRealmArtifactFixture(t, f, cliArtifactSkill+"/", cliArtifactCommand, cliArtifactAgent, available, "wiki/")
	if !strings.Contains(publisherShare, memberRel) {
		t.Errorf("share preview omitted the portable member: %s", publisherShare)
	}
	clone := realmCLIManagedClone(t, f.joinerHome)
	for path, text := range files {
		if path != personal && path != realmPrivateInstructions {
			assertRealmShareCLIFile(t, clone, path, text, false)
		}
	}
	for _, path := range []string{cliArtifactSkill, cliArtifactCommand, cliArtifactAgent, available} {
		if !strings.Contains(join, path) {
			t.Errorf("join did not preview available source %q:\n%s", path, join)
		}
		assertRealmCLIPathAbsent(t, f.joiner, path)
	}
	privatePublisher(t)
	for _, path := range []string{personal, realmPrivateInstructions, ".claude", memberRel} {
		assertRealmCLIPathAbsent(t, clone, path)
	}

	protected := seedRealmArtifactClaude(t, f)
	personalPaths := []string{member, publisherMember, filepath.Join(f.joiner, realmPrivateInstructions)}
	parents := make(map[string]os.FileInfo)
	for _, dir := range []string{".claude/skills", ".claude/commands", ".claude/agents"} {
		sibling := filepath.Join(f.joiner, dir, "personal")
		if dir == ".claude/skills" {
			sibling = filepath.Join(sibling, "SKILL.md")
		} else {
			sibling += ".md"
		}
		writeSteeringTestFile(t, sibling, "# Joiner native sibling\n")
		personalPaths = append(personalPaths, sibling)
		parents[dir] = realmArtifactLstat(t, filepath.Join(f.joiner, dir))
	}
	preserved := unchangedRealmCLITrees(t, personalPaths...)
	owned := []struct {
		logical, native, primary, marker string
	}{
		{cliArtifactSkill, ".claude/skills/realm-review", cliArtifactDoc, "REALM_SKILL_BODY_MARKER"},
		{cliArtifactCommand, ".claude/commands/realm-triage.md", cliArtifactCommand, "REALM_COMMAND_BODY_MARKER"},
		{cliArtifactAgent, ".claude/agents/realm-reviewer.md", cliArtifactAgent, "REALM_AGENT_BODY_MARKER"},
	}
	preLink := unchangedRealmCLITrees(t, f.joiner, f.joinerHome, f.remote)
	for _, unit := range owned {
		assertRealmCLIPathAbsent(t, f.joiner, unit.native)
	}
	assertRealmArtifactSourceStates(t, f.joinerHome, "available", cliArtifactSkill, cliArtifactCommand, cliArtifactAgent, available)
	args := []string{cliArtifactSkill + "/", cliArtifactCommand, cliArtifactAgent}
	preview := mustRunRealmCLI(t, f.joinerHome, "link", append(slices.Clone(args), "--dry-run")...)
	preLink(t)
	for _, unit := range owned {
		for _, want := range []string{unit.primary, unit.native, unit.marker} {
			if !strings.Contains(preview, want) {
				t.Errorf("link preview omitted %q:\n%s", want, preview)
			}
		}
	}
	if len(preview) >= len(longText) || strings.Contains(preview, longTail) || strings.Contains(preview, omittedMarker) {
		t.Errorf("artifact preview was unbounded or exposed unselected text (%d bytes)", len(preview))
	}
	if !realmArtifactMentions(preview, "supported", "verified", "activation") ||
		!realmArtifactMentions(preview, "restart", "new session", "realm root", "realm-root", "repository boundar") {
		t.Errorf("link preview omitted support or launch limits:\n%s", preview)
	}
	mustRunRealmCLI(t, f.joinerHome, "link", append(slices.Clone(args), "--yes", "--review", realmReviewDigest(t, preview))...)

	links := make(map[string]os.FileInfo)
	for _, unit := range owned {
		for _, path := range []string{unit.logical, unit.native} {
			local := filepath.Join(f.joiner, path)
			info, err := os.Lstat(local)
			if err != nil {
				t.Errorf("lstat linked artifact %s: %v", path, err)
				continue
			}
			links[local] = info
			if info.Mode()&os.ModeSymlink == 0 {
				t.Errorf("%s is not a standalone symlink", path)
			}
			assertRealmArtifactTarget(t, local, filepath.Join(clone, unit.logical))
		}
	}
	assertRealmShareCLIFile(t, f.joiner, sample, files[sample], false)
	protected(t)
	preserved(t)
	for _, path := range []string{available, ".claude/commands/realm-followup.md"} {
		assertRealmCLIPathAbsent(t, f.joiner, path)
	}
	statusUnchanged := unchangedRealmCLITrees(t, f.joiner, f.joinerHome, f.remote)
	assertRealmArtifactSourceStates(t, f.joinerHome, "linked", cliArtifactSkill, cliArtifactCommand, cliArtifactAgent)
	assertRealmArtifactSourceStates(t, f.joinerHome, "available", available)
	status := mustRunRealmCLI(t, f.joinerHome, "status")
	for _, unit := range owned {
		assertRealmArtifactNativeStatus(t, status, unit.native, "active", "projected", "activated", "installed", "linked")
	}
	statusUnchanged(t)

	retry := previewRealmReview(t, f.joinerHome, "link", args...)
	mustRunRealmCLI(t, f.joinerHome, "link", append(slices.Clone(args), "--yes", "--review", retry)...)
	for local, before := range links {
		if after := realmArtifactLstat(t, local); !os.SameFile(before, after) {
			t.Errorf("idempotent link replaced %s", local)
		}
	}
	mustRunRealmCLI(t, f.joinerHome, "unlink")
	for _, unit := range owned {
		assertRealmCLIPathAbsent(t, f.joiner, unit.logical)
		assertRealmCLIPathAbsent(t, f.joiner, unit.native)
		assertRealmShareCLIFile(t, clone, unit.primary, files[unit.primary], false)
	}
	assertRealmArtifactSourceStates(t, f.joinerHome, "available", cliArtifactSkill, cliArtifactCommand, cliArtifactAgent, available)
	for dir, before := range parents {
		after := realmArtifactLstat(t, filepath.Join(f.joiner, dir))
		if !after.IsDir() || after.Mode() != before.Mode() || !os.SameFile(before, after) {
			t.Errorf("native directory was replaced or changed mode: %s", dir)
		}
	}
	protected(t)
	preserved(t)
	t.Chdir(f.publisher)
	mustRunRealmCLI(t, f.publisherHome, "unlink")
	for _, unit := range owned {
		assertRealmShareCLIFile(t, f.publisher, unit.primary, files[unit.primary], false)
	}
	preserved(t)
	privatePublisher(t)
}

func TestRealmCLIArtifactNativeCollisionPreservesAuthoredCommand(t *testing.T) {
	useRealmArtifactClaude(t)
	f := newRealmArtifactCLIFixture(t)
	writeSteeringTestFile(t, filepath.Join(f.publisher, cliArtifactCommand), cliArtifactCommandText)
	publishRealmArtifactFixture(t, f, cliArtifactCommand)
	protected := seedRealmArtifactClaude(t, f)
	const native = ".claude/commands/realm-triage.md"
	path := filepath.Join(f.joiner, native)
	writeSteeringTestFile(t, path, "---\ndescription: Authored command.\n---\n\nPERSONAL_NATIVE_COMMAND_MARKER\n")
	before := realmArtifactLstat(t, path)
	personal := unchangedRealmCLITrees(t, path)
	preview, diag, code := runRealmCLI(t, f.joinerHome, "link", cliArtifactCommand, "--dry-run")
	if !strings.Contains(preview+diag, native) || !realmArtifactMentions(preview+diag, "collision", "occupied", "existing", "user-owned") {
		t.Errorf("review did not identify the authored native collision: exit %d\n%s%s", code, preview, diag)
	}
	if code == 0 {
		out, diag, code := runRealmCLI(t, f.joinerHome, "link", cliArtifactCommand, "--yes", "--review", realmReviewDigest(t, preview))
		if code == 0 {
			assertRealmArtifactSourceStates(t, f.joinerHome, "linked", cliArtifactCommand)
			status := mustRunRealmCLI(t, f.joinerHome, "status")
			assertRealmArtifactNativeStatus(t, status, native, "collision", "occupied", "blocked", "user-owned")
		} else {
			if !strings.Contains(diag, native) || !realmArtifactMentions(diag, "collision", "occupied", "existing", "user-owned") {
				t.Errorf("link failed without identifying the native collision: exit %d\n%s%s", code, out, diag)
			}
			assertRealmArtifactSourceStates(t, f.joinerHome, "available", cliArtifactCommand)
		}
	} else {
		assertRealmArtifactSourceStates(t, f.joinerHome, "available", cliArtifactCommand)
	}
	personal(t)
	protected(t)
	mustRunRealmCLI(t, f.joinerHome, "unlink")
	personal(t)
	if after := realmArtifactLstat(t, path); !os.SameFile(before, after) {
		t.Error("unlink replaced the authored command")
	}
	protected(t)
}

func TestRealmCLIArtifactOutsideDependencyRequiresExplicitSelection(t *testing.T) {
	realmInstructionTestPATH(t)
	f := newRealmArtifactCLIFixture(t)
	const (
		dependency = "handouts/review-reference.md"
		reference  = cliArtifactSkill + "/reference.md"
		text       = "# Shared dependency marker\n"
	)
	writeSteeringTestFile(t, filepath.Join(f.publisher, cliArtifactDoc), cliArtifactSkillText+"\nSee [reference](reference.md).\n")
	writeSteeringTestFile(t, filepath.Join(f.publisher, dependency), text)
	if err := os.Symlink("../../../"+dependency, filepath.Join(f.publisher, reference)); err != nil {
		t.Fatal(err)
	}
	unpublished := unchangedRealmCLITrees(t, f.publisher, f.publisherHome, f.remote)
	out, diag, code := runRealmCLI(t, f.publisherHome, "share", cliArtifactSkill+"/", "--remote", f.remote, "--branch", shareBranch, "--dry-run")
	if code == 0 || !strings.Contains(diag, dependency) || !realmArtifactMentions(diag, "select", "include") {
		t.Errorf("share did not require explicit selection of the outside dependency: exit %d\n%s%s", code, out, diag)
	}
	unpublished(t)
	publishRealmArtifactFixture(t, f, cliArtifactSkill+"/", dependency)
	clone := realmCLIManagedClone(t, f.joinerHome)
	assertRealmShareCLIFile(t, clone, reference, text, true)
	unchanged := unchangedRealmCLITrees(t, f.joiner, f.joinerHome, f.remote)
	out, diag, code = runRealmCLI(t, f.joinerHome, "link", cliArtifactSkill+"/", "--dry-run")
	if code == 0 || !strings.Contains(diag, cliArtifactSkill) || !strings.Contains(diag, dependency) || !realmArtifactMentions(diag, "select", "include") {
		t.Errorf("link did not name the missing selection and how to resolve it: exit %d\n%s%s", code, out, diag)
	}
	unchanged(t)
	args := []string{cliArtifactSkill + "/", dependency}
	preview := mustRunRealmCLI(t, f.joinerHome, "link", append(slices.Clone(args), "--dry-run")...)
	if !strings.Contains(preview, dependency) {
		t.Errorf("review omitted the explicitly selected dependency:\n%s", preview)
	}
	mustRunRealmCLI(t, f.joinerHome, "link", append(args, "--yes", "--review", realmReviewDigest(t, preview))...)
	assertRealmShareCLIFile(t, f.joiner, reference, text, true)
	assertRealmShareCLIFile(t, f.joiner, dependency, text, true)
}

func TestRealmCLIArtifactNativeGrantMetadataStaysInert(t *testing.T) {
	useRealmArtifactClaude(t)
	f := newRealmArtifactCLIFixture(t)
	writeSteeringTestFile(t, filepath.Join(f.publisher, cliArtifactDoc), cliArtifactSkillText)
	publishRealmArtifactFixture(t, f, cliArtifactSkill+"/")
	protected := seedRealmArtifactClaude(t, f)
	clone := realmCLIManagedClone(t, f.joinerHome)
	grantText := strings.Replace(cliArtifactSkillText, "description:", "allowed-tools: Read\ndescription:", 1)
	writeSteeringTestFile(t, filepath.Join(clone, cliArtifactDoc), grantText)
	unchanged := unchangedRealmCLITrees(t, f.joiner, f.joinerHome, f.remote)
	out, diag, code := runRealmCLI(t, f.joinerHome, "link", cliArtifactSkill+"/", "--dry-run")
	if !strings.Contains(out+diag, "allowed-tools") || !realmArtifactMentions(out+diag, "permission", "grant", "prohibited", "unsupported", "blocked", "refus") {
		t.Errorf("review did not explain why the native grant stays inert: exit %d\n%s%s", code, out, diag)
	}
	unchanged(t)
	if code == 0 {
		review := realmReviewDigest(t, out)
		applyOut, applyDiag, applyCode := runRealmCLI(t, f.joinerHome, "link", cliArtifactSkill+"/", "--yes", "--review", review)
		if applyCode != 0 && !strings.Contains(applyOut+applyDiag, "allowed-tools") {
			t.Errorf("apply rejected the reviewed artifact for an unrelated reason: exit %d\n%s%s", applyCode, applyOut, applyDiag)
		}
	}
	assertRealmCLIPathAbsent(t, f.joiner, cliArtifactSkill)
	assertRealmCLIPathAbsent(t, f.joiner, ".claude/skills/realm-review")
	assertRealmShareCLIFile(t, clone, cliArtifactDoc, grantText, false)
	protected(t)
}

func TestRealmCLIArtifactShareRefusesHomeRealm(t *testing.T) {
	realmInstructionTestPATH(t)
	home, _, remote := newRealmShareCLIFixture(t)
	t.Setenv("HOME", home)
	skill := filepath.Join(home, cliArtifactSkill)
	doc := filepath.Join(home, cliArtifactDoc)
	writeSteeringTestFile(t, doc, cliArtifactSkillText)
	skillBefore, docBefore := realmArtifactLstat(t, skill), realmArtifactLstat(t, doc)
	unchanged := unchangedRealmCLITrees(t, skill, remote)

	out, diag, code := runRealmCLI(t, home, "share", cliArtifactSkill+"/", "--remote", remote, "--branch", shareBranch, "--dry-run")
	if code == 0 || !realmArtifactMentions(diag, "home", "global", "profile") {
		t.Errorf("expected share to refuse an artifact in the home realm: exit %d\nstdout: %s\nstderr: %s", code, out, diag)
	}
	unchanged(t)
	if !os.SameFile(skillBefore, realmArtifactLstat(t, skill)) || !os.SameFile(docBefore, realmArtifactLstat(t, doc)) {
		t.Error("share replaced the authored skill directory or SKILL.md")
	}
}

func newRealmArtifactCLIFixture(t *testing.T) realmLinkCLIFixture {
	t.Helper()
	publisher, publisherHome, remote := newRealmShareCLIFixture(t)
	f := realmLinkCLIFixture{publisher, publisherHome, t.TempDir(), t.TempDir(), remote}
	writeSteeringTestFile(t, config.RepoConfigPath(f.joiner), "scope = \"realm\"\n")
	writeSteeringTestFile(t, filepath.Join(f.joiner, realmPrivateInstructions), privateRealmGuidance)
	return f
}

func publishRealmArtifactFixture(t *testing.T, f realmLinkCLIFixture, paths ...string) (preview, join string) {
	t.Helper()
	args := append(slices.Clone(paths), "--remote", f.remote, "--branch", shareBranch)
	preview = mustRunRealmCLI(t, f.publisherHome, "share", append(slices.Clone(args), "--dry-run")...)
	mustRunRealmCLI(t, f.publisherHome, "share", append(args, "--yes", "--review", realmReviewDigest(t, preview))...)
	runGit(t, f.remote, "symbolic-ref", "HEAD", "refs/heads/"+shareBranch)
	t.Chdir(f.joiner)
	join = mustRunRealmCLI(t, f.joinerHome, "join", f.remote)
	return preview, join
}

func useRealmArtifactClaude(t *testing.T) {
	t.Helper()
	bin, claude := realmInstructionTestPATH(t)
	if claude == "" {
		t.Skip("Claude binary unavailable")
	}
	linkRealmInstructionTool(t, bin, "claude", claude)
}

func seedRealmArtifactClaude(t *testing.T, f realmLinkCLIFixture) func(*testing.T) {
	t.Helper()
	profile := filepath.Join(f.joinerHome, ".claude")
	ledger := &installstate.Ledger{
		Header:  installstate.NewHeader(),
		Targets: []installstate.TargetRecord{{Harness: "claude", Instance: profile}},
	}
	ledgerPath := config.LedgerPath(f.joinerHome)
	if err := ledger.Save(ledgerPath); err != nil {
		t.Fatal(err)
	}
	project := filepath.Join(f.joiner, realmClaudeProjectSettings)
	local := filepath.Join(f.joiner, realmClaudeLocalSettings)
	writeSteeringTestFile(t, filepath.Join(profile, "settings.json"), `{"env":{"REALM_GLOBAL_MARKER":"keep"}}`+"\n")
	writeSteeringTestFile(t, project, `{"env":{"REALM_PROJECT_MARKER":"keep"}}`+"\n")
	writeSteeringTestFile(t, local, `{"env":{"REALM_LOCAL_MARKER":"keep"}}`+"\n")
	return unchangedRealmCLITrees(t, profile, ledgerPath, project, local)
}

func realmArtifactLstat(t *testing.T, path string) os.FileInfo {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info
}

func assertRealmArtifactTarget(t *testing.T, local, source string) {
	t.Helper()
	got, gotErr := os.Stat(local)
	want, wantErr := os.Stat(source)
	if gotErr != nil || wantErr != nil || !os.SameFile(got, want) {
		t.Errorf("%s does not link to the shared unit %s (%v, %v)", local, source, gotErr, wantErr)
	}
}

func assertRealmArtifactSourceStates(t *testing.T, home, state string, paths ...string) {
	t.Helper()
	raw := mustRunRealmCLI(t, home, "status", "--json")
	var status struct {
		Paths []realmshare.StatusPath `json:"paths"`
	}
	if err := json.Unmarshal([]byte(raw), &status); err != nil {
		t.Fatalf("decode status: %v\n%s", err, raw)
	}
	for _, path := range paths {
		if !slices.ContainsFunc(status.Paths, func(p realmshare.StatusPath) bool {
			return p.Path == path && p.Source == path && p.State == state && !p.Drift
		}) {
			t.Errorf("status did not report logical source %s as %s:\n%s", path, state, raw)
		}
	}
}

func assertRealmArtifactNativeStatus(t *testing.T, status, destination string, states ...string) {
	t.Helper()
	for line := range strings.SplitSeq(status, "\n") {
		if !strings.Contains(line, destination) {
			continue
		}
		fields := strings.FieldsFunc(strings.ToLower(line), func(r rune) bool {
			return (r < 'a' || r > 'z') && r != '-'
		})
		if slices.ContainsFunc(states, func(state string) bool { return slices.Contains(fields, state) }) {
			return
		}
	}
	t.Errorf("status did not report native destination %s with a matching state %q:\n%s", destination, states, status)
}

func realmArtifactMentions(text string, words ...string) bool {
	text = strings.ToLower(text)
	return slices.ContainsFunc(words, func(word string) bool { return strings.Contains(text, word) })
}
