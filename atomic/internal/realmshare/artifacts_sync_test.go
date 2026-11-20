package realmshare

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSyncRejectsNativeGrantUpdateToLinkedSkill(t *testing.T) {
	const (
		unit     = ".agents/skills/review"
		skill    = unit + "/SKILL.md"
		personal = ".agents/skills/personal"
		header   = "---\nname: review\ndescription: Review a draft for clarity.\n"
		initial  = header + "---\nSummarize the draft.\n"
		reviewed = header + "---\nSummarize the draft in two sentences.\n"
		grant    = header + "allowed-tools: Read\n---\nSummarize the draft in two sentences.\n"
	)
	f, seed := newLinkedSyncSkillFixture(t, unit, initial)
	f.write(t, personal+"/SKILL.md", "# Personal notes\n")
	binding := f.binding(t)
	clone, ref := binding.ClonePath, "refs/heads/"+binding.Branch
	publish := func(content, message string) string {
		t.Helper()
		writeRealmshareFile(t, filepath.Join(seed, skill), content)
		head := syncFixtureCommit(t, f, seed, message, skill)
		f.git(t, seed, "push", "--quiet", f.remote, "HEAD:"+ref)
		return head
	}
	localUnit, localSkill := filepath.Join(f.realm, unit), filepath.Join(f.realm, skill)
	owned := f.assertLink(t, binding, unit, "")
	link := linkLstat(t, localUnit)

	reviewedHead := publish(reviewed, "Clarify review skill instructions")
	if got, err := Sync(t.Context(), f.realm); err != nil || !got.Received || got.Head != reviewedHead {
		t.Fatalf("receive content-only update = %+v, %v; want %s", got, err, reviewedHead)
	}
	if got := readRealmshareFile(t, localSkill); got != reviewed {
		t.Fatalf("linked skill after safe sync = %q, want %q", got, reviewed)
	}
	if !os.SameFile(link, linkLstat(t, localUnit)) {
		t.Fatal("safe sync replaced the opted-in link")
	}

	remoteHead := publish(grant, "Add read metadata to review skill")
	if got, err := Sync(t.Context(), f.realm); err == nil || !strings.Contains(err.Error(), "allowed-tools") || got.Received {
		t.Errorf("receive native grant update = %+v, %v; want rejection identifying allowed-tools", got, err)
	}
	if got := f.git(t, clone, "rev-parse", "HEAD"); got != reviewedHead {
		t.Errorf("clone HEAD = %s, want %s", got, reviewedHead)
	}
	if got := f.git(t, clone, "status", "--porcelain=v1"); got != "" {
		t.Errorf("clone workspace changed: %q", got)
	}
	for _, path := range []string{filepath.Join(clone, skill), localSkill} {
		if got := readRealmshareFile(t, path); got != reviewed {
			t.Errorf("%s after rejected update = %q, want %q", path, got, reviewed)
		}
	}
	if !os.SameFile(link, linkLstat(t, localUnit)) {
		t.Error("rejected sync replaced the opted-in link")
	}
	if got := f.assertLink(t, f.binding(t), unit, ""); got != owned {
		t.Errorf("link receipt = %+v, want %+v", got, owned)
	}
	assertRealmshareAbsent(t, filepath.Join(clone, ".git", "MERGE_HEAD"))
	f.assertOriginal(t, personal, "SKILL.md", "# Personal notes\n")
	f.assertOriginal(t, "docs/local.md", "", "# Local notes\n")
	if got := f.git(t, f.remote, "rev-parse", ref); got != remoteHead {
		t.Errorf("remote tip = %s, want %s", got, remoteHead)
	}
}

func TestSyncMergesSafeDivergenceWithLinkedSkill(t *testing.T) {
	const (
		unit         = ".agents/skills/summarize"
		skill        = unit + "/SKILL.md"
		header       = "---\nname: summarize\ndescription: Summarize meeting notes.\n---\n"
		initial      = header + "Summarize the notes.\n"
		incoming     = header + "Summarize the notes in two paragraphs.\n"
		guide        = "shared/guide.md"
		localGuide   = "# Team guide\n\nKeep meeting notes concise.\n"
		personal     = ".agents/skills/personal"
		personalText = "# Personal notes\n"
	)
	f, seed := newLinkedSyncSkillFixture(t, unit, initial)
	f.write(t, personal+"/SKILL.md", personalText)
	binding := f.binding(t)
	clone, ref := binding.ClonePath, "refs/heads/"+binding.Branch
	owned := f.assertLink(t, binding, unit, "")
	if owned.Witness == "" {
		t.Fatal("opted-in skill has no link witness")
	}
	localUnit := filepath.Join(f.realm, unit)
	link := linkLstat(t, localUnit)

	writeRealmshareFile(t, filepath.Join(clone, guide), localGuide)
	localHead := syncFixtureCommit(t, f, clone, "Clarify the team guide", guide)
	writeRealmshareFile(t, filepath.Join(seed, skill), incoming)
	remoteHead := syncFixtureCommit(t, f, seed, "Clarify the summary skill", skill)
	f.git(t, seed, "push", "--quiet", f.remote, "HEAD:"+ref)

	result, err := Sync(t.Context(), f.realm)
	if err != nil || !result.Received || !result.Published {
		t.Fatalf("sync divergent changes = %+v, %v; want a received and published merge", result, err)
	}
	if got, want := f.git(t, clone, "rev-list", "--parents", "-1", "HEAD"), result.Head+" "+localHead+" "+remoteHead; got != want {
		t.Errorf("merge commit and parents = %q, want %q", got, want)
	}
	if got := f.git(t, f.remote, "rev-parse", ref); got != result.Head {
		t.Errorf("remote tip = %s, want merge %s", got, result.Head)
	}
	for path, want := range map[string]string{
		filepath.Join(clone, guide):   localGuide,
		filepath.Join(clone, skill):   incoming,
		filepath.Join(f.realm, skill): incoming,
	} {
		if got := readRealmshareFile(t, path); got != want {
			t.Errorf("%s after merge = %q, want %q", path, got, want)
		}
	}
	if got := f.git(t, clone, "status", "--porcelain=v1"); got != "" {
		t.Errorf("clone workspace after merge = %q, want clean", got)
	}
	if got := f.assertLink(t, f.binding(t), unit, ""); got != owned {
		t.Errorf("link receipt = %+v, want %+v", got, owned)
	}
	if !os.SameFile(link, linkLstat(t, localUnit)) || !os.SameFile(link, linkLstat(t, filepath.Join(f.state, owned.Witness))) {
		t.Error("merge changed the opted-in link or its witness")
	}
	f.assertOriginal(t, personal, "SKILL.md", personalText)
	f.assertOriginal(t, "docs/local.md", "", "# Local notes\n")
}

func TestSyncRejectsNewDependencyOnUnlinkedSharedFile(t *testing.T) {
	const (
		unit       = ".agents/skills/prepare"
		skill      = unit + "/SKILL.md"
		header     = "---\nname: prepare\ndescription: Prepare meeting notes.\n---\n"
		initial    = header + "Prepare the meeting notes.\n"
		incoming   = header + "Read guide.md before preparing the meeting notes.\n"
		dependency = unit + "/guide.md"
		source     = "shared/guide.md"
		dest       = "guide.md"
		guide      = "# Team guide\n"
	)
	f, seed := newLinkedSyncSkillFixture(t, unit, initial)
	binding := f.binding(t)
	clone := binding.ClonePath
	localUnit := filepath.Join(f.realm, unit)
	link := linkLstat(t, localUnit)
	owned := f.assertLink(t, binding, unit, "")
	before := f.git(t, clone, "rev-parse", "HEAD")
	assertRealmshareAbsent(t, filepath.Join(f.realm, dest))
	if got := readRealmshareFile(t, filepath.Join(clone, source)); got != guide {
		t.Fatalf("unlinked shared guide = %q, want %q", got, guide)
	}

	writeRealmshareFile(t, filepath.Join(seed, skill), incoming)
	symlinkRealmshare(t, "../../../"+source, filepath.Join(seed, dependency))
	remote := syncFixtureCommit(t, f, seed, "Reference the shared team guide", skill, dependency)
	f.git(t, seed, "push", "--quiet", f.remote, "HEAD:refs/heads/"+binding.Branch)

	result, err := Sync(t.Context(), f.realm)
	if err == nil || result.Received {
		t.Errorf("sync with unlinked dependency = %+v, %v; want refusal", result, err)
	} else {
		for _, want := range []string{source, "unlink", "sync", "review", " link"} {
			if !strings.Contains(strings.ToLower(err.Error()), want) {
				t.Errorf("sync error = %q; missing dependency or recovery guidance %q", err, want)
			}
		}
	}
	assertSyncSkillUnchanged(t, f, binding, owned, link, unit, initial, before)
	assertRealmshareAbsent(t, filepath.Join(clone, dependency))
	assertRealmshareAbsent(t, filepath.Join(f.realm, dependency))
	assertRealmshareAbsent(t, filepath.Join(f.realm, dest))

	if err := Unlink(t.Context(), f.realm); err != nil {
		t.Fatal(err)
	}
	if got, err := Sync(t.Context(), f.realm); err != nil || !got.Received || got.Head != remote {
		t.Fatalf("sync after unlink = %+v, %v; want %s", got, err, remote)
	}
	assertRealmshareAbsent(t, localUnit)
	assertRealmshareAbsent(t, filepath.Join(f.realm, dest))
	if _, err := PreviewLink(t.Context(), f.realm, []string{unit}); err == nil || !strings.Contains(err.Error(), source) {
		t.Fatalf("preview of the skill alone = %v; want refusal identifying %s", err, source)
	}
	if err := ApplyLink(t.Context(), previewRealmLink(t, f.realm, unit, dest)); err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]string{skill: incoming, dependency: guide, dest: guide} {
		if got := readRealmshareFile(t, filepath.Join(f.realm, path)); got != want {
			t.Errorf("%s after reviewing both selections = %q, want %q", path, got, want)
		}
	}
}

func TestSyncPreservesExactLinkedSkillBytesWithGitTextNormalization(t *testing.T) {
	const (
		unit       = ".agents/skills/outline"
		skill      = unit + "/SKILL.md"
		attributes = unit + "/.gitattributes"
		header     = "---\nname: outline\ndescription: Outline meeting notes.\n---\n"
		initial    = header + "Outline the meeting notes.\n"
		incoming   = header + "Outline the decisions and next steps.\n"
	)
	f, seed := newLinkedSyncSkillFixture(t, unit, initial)
	binding := f.binding(t)
	clone := binding.ClonePath
	link := linkLstat(t, filepath.Join(f.realm, unit))
	owned := f.assertLink(t, binding, unit, "")
	before := f.git(t, clone, "rev-parse", "HEAD")

	writeRealmshareFile(t, filepath.Join(seed, attributes), "SKILL.md text eol=crlf\n")
	writeRealmshareFile(t, filepath.Join(seed, skill), strings.ReplaceAll(incoming, "\n", "\r\n"))
	remote := syncFixtureCommit(t, f, seed, "Normalize outline skill line endings", attributes, skill)
	if got := f.git(t, seed, "cat-file", "blob", remote+":"+skill); got != strings.TrimSpace(incoming) {
		t.Fatalf("committed skill text = %q; want normalized LF", got)
	}
	f.git(t, seed, "push", "--quiet", f.remote, "HEAD:refs/heads/"+binding.Branch)

	result, err := Sync(t.Context(), f.realm)
	if err != nil {
		detail := strings.ToLower(err.Error())
		if result.Received || (!strings.Contains(detail, "conversion") && !strings.Contains(detail, "eol") &&
			!strings.Contains(detail, "attributes") && !strings.Contains(detail, "materializ") &&
			!strings.Contains(detail, "normaliz")) {
			t.Errorf("sync with text normalization = %+v, %v; want explicit conversion refusal", result, err)
		}
		assertSyncSkillUnchanged(t, f, binding, owned, link, unit, initial, before)
		assertRealmshareAbsent(t, filepath.Join(clone, attributes))
		return
	}
	if !result.Received || result.Head != remote {
		t.Errorf("sync with text normalization = %+v; want exact update at %s", result, remote)
	}
	assertSyncSkillUnchanged(t, f, binding, owned, link, unit, incoming, remote)
}

func assertSyncSkillUnchanged(t *testing.T, f shareFixture, binding Binding, owned LinkReceipt, link os.FileInfo, unit, content, head string) {
	t.Helper()
	clone := binding.ClonePath
	if got := f.git(t, clone, "rev-parse", "HEAD"); got != head {
		t.Errorf("clone HEAD = %s, want %s", got, head)
	}
	if got := f.git(t, clone, "status", "--porcelain=v1"); got != "" {
		t.Errorf("clone workspace = %q, want clean", got)
	}
	for _, root := range []string{clone, f.realm} {
		path := filepath.Join(root, unit, "SKILL.md")
		if got := readRealmshareFile(t, path); got != content {
			t.Errorf("%s = %q, want exact bytes %q", path, got, content)
		}
	}
	if got := f.assertLink(t, f.binding(t), unit, ""); got != owned {
		t.Errorf("link receipt = %+v, want %+v", got, owned)
	}
	if !os.SameFile(link, linkLstat(t, filepath.Join(f.realm, unit))) ||
		!os.SameFile(link, linkLstat(t, filepath.Join(f.state, owned.Witness))) {
		t.Error("sync replaced the opted-in link or its witness")
	}
	assertRealmshareAbsent(t, filepath.Join(clone, ".git", "MERGE_HEAD"))
	f.assertOriginal(t, "docs/local.md", "", "# Local notes\n")
}

func newLinkedSyncSkillFixture(t *testing.T, unit, initial string) (shareFixture, string) {
	t.Helper()
	f, seed := newLinkFixture(t)
	requireShareSymlinks(t)
	skill := unit + "/SKILL.md"
	definition := filepath.Join(seed, manifestName)
	writeRealmshareFile(t, definition, readRealmshareFile(t, definition)+manifestMapping(unit, unit, string(KindSkill)))
	writeRealmshareFile(t, filepath.Join(seed, skill), initial)
	initialHead := syncFixtureCommit(t, f, seed, "Publish fixture skill", manifestName, skill)
	f.git(t, seed, "push", "--quiet", f.remote, "HEAD:refs/heads/"+f.binding(t).Branch)
	if got, err := Sync(t.Context(), f.realm); err != nil || !got.Received || got.Head != initialHead {
		t.Fatalf("receive initial skill = %+v, %v; want %s", got, err, initialHead)
	}
	assertRealmshareAbsent(t, filepath.Join(f.realm, unit))
	if err := ApplyLink(t.Context(), previewRealmLink(t, f.realm, unit)); err != nil {
		t.Fatal(err)
	}
	if got := readRealmshareFile(t, filepath.Join(f.realm, skill)); got != initial {
		t.Fatalf("linked skill before sync = %q, want %q", got, initial)
	}
	return f, seed
}
