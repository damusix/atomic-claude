package realmshare

import (
	"errors"
	"net/http"
	"net/http/cgi"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/config"
)

func TestSharePreviewDoesNotMutateRealmOrRemote(t *testing.T) {
	f := newShareFixture(t)
	f.write(t, "notes/guide.md", "# Guide\n")
	f.write(t, "notes/day/log.md", "# Log\n")
	f.write(t, "notes/.claude/settings.json", "{\"private\":true}\n")
	plan := f.preview(t, ShareOptions{Remote: f.remote, Paths: []string{"notes/"}})
	if plan.Remote != f.remote || plan.Branch != "main" || plan.ClonePath == "" {
		t.Fatalf("preview destination = %s %s %s", plan.Remote, plan.Branch, plan.ClonePath)
	}
	if len(plan.Selections) != 1 {
		t.Fatalf("preview selections = %+v", plan.Selections)
	}
	selection := plan.Selections[0]
	if selection.Mapping.Destination != "notes" || selection.Mapping.Kind != KindDirectory || selection.Files != 2 || selection.Bytes != int64(len("# Guide\n# Log\n")) {
		t.Errorf("preview selection = %+v", selection)
	}
	if len(selection.Exclusions) == 0 {
		t.Error("preview did not report excluded harness settings")
	}
	f.assertUnbound(t)
	f.assertOriginal(t, "notes", "guide.md", "# Guide\n")
	f.assertOriginal(t, "notes", "day/log.md", "# Log\n")
	f.assertOriginal(t, "notes", ".claude/settings.json", "{\"private\":true}\n")
}

func TestShareReviewAndPublicationHonorGitInfoExclude(t *testing.T) {
	f := newShareFixture(t)
	f.git(t, f.realm, "init", "--quiet")
	const guide, private = "# Guide\n", "# Unpublished notes\n"
	const exclude = "/notes/draft.md\n"
	f.write(t, "notes/guide.md", guide)
	f.write(t, "notes/draft.md", private)
	f.write(t, ".git/info/exclude", exclude)

	plan := f.preview(t, ShareOptions{Remote: f.remote, Paths: []string{"notes"}})
	if len(plan.Selections) != 1 {
		t.Fatalf("preview selections = %+v", plan.Selections)
	}
	review := plan.Selections[0]
	if review.Files != 1 || review.Bytes != int64(len(guide)) {
		t.Errorf("preview counted ignored content: %+v", review)
	}
	if !slices.ContainsFunc(review.Exclusions, func(exclusion string) bool {
		return strings.Contains(exclusion, "notes/draft.md")
	}) {
		t.Errorf("preview did not report the Git-excluded path: %v", review.Exclusions)
	}
	f.assertUnbound(t)
	f.assertOriginal(t, "notes", "guide.md", guide)
	f.assertOriginal(t, "notes", "draft.md", private)
	f.assertOriginal(t, ".git/info/exclude", "", exclude)
	if t.Failed() {
		return
	}

	requireShareSymlinks(t)
	f.apply(t, plan)
	binding := f.binding(t)
	link := f.assertLink(t, binding, "notes", "")
	ref := "refs/heads/" + binding.Branch
	wantTree := []string{manifestName, link.Source + "/guide.md"}
	slices.Sort(wantTree)
	if got := strings.Fields(f.git(t, f.remote, "ls-tree", "-r", "--name-only", ref)); !slices.Equal(got, wantTree) {
		t.Errorf("published tree = %v, want %v", got, wantTree)
	}
	backup := f.restorationBackup(t, binding, "notes")
	if got := readRealmshareFile(t, filepath.Join(backup, "draft.md")); got != private {
		t.Errorf("ignored file in restoration backup = %q, want %q", got, private)
	}
}

func TestShareHonorsRepositoryLocalGitIgnoreCase(t *testing.T) {
	f := newShareFixture(t)
	f.git(t, f.realm, "init", "--quiet")
	f.git(t, f.realm, "config", "--local", "core.ignoreCase", "true")
	const guide, private = "# Guide\n", "# Private draft\n"
	const draft, exclude = "notes/Draft.md", "/NoTeS/dRaFt.md\n"
	f.write(t, "notes/guide.md", guide)
	f.write(t, draft, private)
	f.write(t, ".git/info/exclude", exclude)
	if got := f.git(t, f.realm, "check-ignore", "--", draft); got != draft {
		t.Fatalf("source Git reported %q as ignored, want %q", got, draft)
	}

	plan := f.preview(t, ShareOptions{Remote: f.remote, Paths: []string{"notes"}})
	f.assertUnbound(t)
	f.assertOriginal(t, draft, "", private)
	if len(plan.Selections) != 1 {
		t.Fatalf("preview selections = %+v", plan.Selections)
	}
	review := plan.Selections[0]
	if review.Files != 1 || review.Bytes != int64(len(guide)) {
		t.Errorf("preview counted case-insensitively ignored content: %+v", review)
	}
	if !slices.ContainsFunc(review.Exclusions, func(exclusion string) bool {
		return strings.Contains(exclusion, draft)
	}) {
		t.Errorf("preview did not report the ignored path %q: %v", draft, review.Exclusions)
	}
	if t.Failed() {
		return
	}

	requireShareSymlinks(t)
	f.apply(t, plan)
	binding := f.binding(t)
	ref := "refs/heads/" + binding.Branch
	if got := f.git(t, f.remote, "ls-tree", "-r", "--name-only", ref, "--", "notes"); got != "notes/guide.md" {
		t.Errorf("published notes tree = %q, want only notes/guide.md", got)
	}
	backup := f.restorationBackup(t, binding, "notes")
	if got := readRealmshareFile(t, filepath.Join(backup, "Draft.md")); got != private {
		t.Errorf("ignored draft in restoration backup = %q, want %q", got, private)
	}
}

func TestSharePreviewShowsPortableMemberDefinitions(t *testing.T) {
	f := newShareFixture(t)
	member := filepath.Join(f.realm, "repos", "service")
	f.write(t, "repos/service/readme.md", "# Service\n")
	f.git(t, member, "init", "--quiet")
	index := "# Realm wiki\n\n<wiki-scan>\n" +
		`  <repo path="` + filepath.ToSlash(member) + `" status="indexed"/>` + "\n" +
		`  <repo path="` + filepath.ToSlash(f.remote) + `" status="indexed"/>` + "\n" +
		"</wiki-scan>\n"
	f.write(t, "wiki/index.md", index)

	plan := f.preview(t, ShareOptions{Remote: f.remote, Paths: []string{"wiki"}})
	want := []Member{{Key: "service", Path: "repos/service"}}
	if !slices.Equal(plan.Members, want) {
		t.Errorf("preview member definitions = %+v, want %+v", plan.Members, want)
	}
	if len(plan.Selections) != 1 || plan.Selections[0].Mapping.Destination != "wiki" {
		t.Errorf("preview selected member content: %+v", plan.Selections)
	}
	f.assertUnbound(t)
	f.assertOriginal(t, "wiki", "index.md", index)
	f.assertOriginal(t, "repos/service", "readme.md", "# Service\n")
}

func TestSharePublishesDirectoryWithoutPrivateHarnessSettings(t *testing.T) {
	f := newShareFixture(t)
	requireShareSymlinks(t)
	const guide, log, settings = "# Guide\n", "# Log\n", "{\"private\":true}\n"
	f.write(t, "notes/guide.md", guide)
	f.write(t, "notes/day/log.md", log)
	f.write(t, "notes/.claude/settings.json", settings)
	f.apply(t, f.preview(t, ShareOptions{Remote: f.remote, Paths: []string{"notes"}}))

	binding := f.binding(t)
	link := f.assertLink(t, binding, "notes", "")
	ref := "refs/heads/" + binding.Branch
	wantTree := []string{manifestName, link.Source + "/day/log.md", link.Source + "/guide.md"}
	slices.Sort(wantTree)
	if got := strings.Fields(f.git(t, f.remote, "ls-tree", "-r", "--name-only", ref)); !slices.Equal(got, wantTree) {
		t.Errorf("published tree = %v, want %v", got, wantTree)
	}
	backup := f.restorationBackup(t, binding, "notes")
	for name, want := range map[string]string{"guide.md": guide, "day/log.md": log} {
		if got := f.git(t, f.remote, "show", ref+":"+link.Source+"/"+name); got != strings.TrimSpace(want) {
			t.Errorf("published %s = %q, want %q", name, got, strings.TrimSpace(want))
		}
		if got := readRealmshareFile(t, filepath.Join(backup, name)); got != want {
			t.Errorf("backup %s = %q, want %q", name, got, want)
		}
	}
	if got := readRealmshareFile(t, filepath.Join(backup, ".claude", "settings.json")); got != settings {
		t.Errorf("backup harness settings = %q, want %q", got, settings)
	}
}

func TestSharePublishesOnlySelectedFileToEmptyRemote(t *testing.T) {
	f := newShareFixture(t)
	requireShareSymlinks(t)
	f.write(t, "guide.md", "# Shared\n")
	f.write(t, "draft.md", "# Local draft\n")
	f.write(t, "AGENTS.local.md", "# Personal\n")
	f.write(t, "AGENTS.md", "# Local loader\n")
	f.apply(t, f.preview(t, ShareOptions{Remote: f.remote, Branch: "team", Paths: []string{"guide.md"}}))
	binding := f.binding(t)
	if binding.Remote != f.remote || binding.Branch != "team" || binding.PendingShare != nil {
		t.Fatalf("binding = %+v", binding)
	}
	manifest := f.manifest(t, binding)
	if manifest.ID != binding.RealmID || len(manifest.Mappings) != 1 || manifest.Mappings[0].Kind != KindFile {
		t.Fatalf("published manifest = %+v", manifest)
	}
	f.assertLink(t, binding, "guide.md", "# Shared\n")
	if got := readRealmshareFile(t, f.restorationBackup(t, binding, "guide.md")); got != "# Shared\n" {
		t.Errorf("backup guide = %q", got)
	}
	for name, want := range map[string]string{"draft.md": "# Local draft\n", "AGENTS.local.md": "# Personal\n", "AGENTS.md": "# Local loader\n"} {
		f.assertOriginal(t, name, "", want)
	}
	wantTree := []string{"realm.toml", manifest.Mappings[0].Source}
	slices.Sort(wantTree)
	if got := strings.Fields(f.git(t, f.remote, "ls-tree", "-r", "--name-only", "refs/heads/team")); !slices.Equal(got, wantTree) {
		t.Errorf("published tree = %v, want %v", got, wantTree)
	}
	if got := f.git(t, f.remote, "for-each-ref", "--format=%(refname)"); got != "refs/heads/team" {
		t.Errorf("published refs = %q", got)
	}
}

func TestShareSnapshotsWikiWithoutLosingOriginalHistory(t *testing.T) {
	f := newShareFixture(t)
	requireShareSymlinks(t)
	const committed, current, draft = "# Committed wiki\n", "# Current wiki\n", "# New wiki page\n"
	f.write(t, "wiki/index.md", committed)
	wiki := filepath.Join(f.realm, "wiki")
	f.git(t, wiki, "init", "--quiet")
	f.git(t, wiki, "add", "index.md")
	f.git(t, wiki, "commit", "--quiet", "-m", "wiki history")
	wikiHead := f.git(t, wiki, "rev-parse", "HEAD")
	f.write(t, "wiki/index.md", current)
	f.write(t, "wiki/draft.md", draft)

	plan := f.preview(t, ShareOptions{Remote: f.remote, Paths: []string{"wiki"}})
	if len(plan.Selections) != 1 || plan.Selections[0].Files != 2 || len(plan.Selections[0].Exclusions) == 0 {
		t.Fatalf("wiki preview did not distinguish the snapshot from Git history: %+v", plan.Selections)
	}
	f.apply(t, plan)
	binding := f.binding(t)
	receipt := f.assertLink(t, binding, "wiki", "")
	assertRealmshareAbsent(t, filepath.Join(wiki, ".git"))
	ref := "refs/heads/" + binding.Branch
	if got := f.git(t, f.remote, "show", ref+":"+receipt.Source+"/index.md"); got != strings.TrimSpace(current) {
		t.Errorf("published index = %q", got)
	}
	if got := f.git(t, f.remote, "show", ref+":"+receipt.Source+"/draft.md"); got != strings.TrimSpace(draft) {
		t.Errorf("published draft = %q", got)
	}
	if got := f.git(t, f.remote, "rev-list", "--count", ref); got != "1" {
		t.Errorf("shared history has %s commits, want one snapshot", got)
	}
	backup := f.restorationBackup(t, binding, "wiki")
	if got := f.git(t, backup, "rev-parse", "HEAD"); got != wikiHead {
		t.Errorf("original wiki HEAD = %s, want %s", got, wikiHead)
	}
	if got := f.git(t, backup, "show", "HEAD:index.md"); got != strings.TrimSpace(committed) {
		t.Errorf("original history index = %q", got)
	}
	for name, want := range map[string]string{"index.md": current, "draft.md": draft} {
		if got := readRealmshareFile(t, filepath.Join(backup, name)); got != want {
			t.Errorf("backup %s = %q, want %q", name, got, want)
		}
	}
}

func TestSharePublishesIndividualSkillWithoutSiblingArtifacts(t *testing.T) {
	f := newShareFixture(t)
	requireShareSymlinks(t)
	const skill = ".agents/skills/review"
	f.write(t, skill+"/SKILL.md", "# Review\n")
	f.write(t, skill+"/examples/sample.md", "# Example\n")
	f.write(t, skill+"/.gitignore", "scratch.md\n")
	f.write(t, skill+"/scratch.md", "# Ignored\n")
	f.write(t, ".agents/skills/personal/SKILL.md", "# Personal skill\n")
	plan := f.preview(t, ShareOptions{Remote: f.remote, Paths: []string{skill}})
	if len(plan.Selections) != 1 || plan.Selections[0].Mapping.Kind != KindSkill {
		t.Fatalf("skill preview = %+v", plan.Selections)
	}
	f.apply(t, plan)
	binding := f.binding(t)
	receipt := f.assertLink(t, binding, skill, "")
	f.assertOriginal(t, ".agents/skills/personal", "SKILL.md", "# Personal skill\n")
	tree := strings.Fields(f.git(t, f.remote, "ls-tree", "-r", "--name-only", "refs/heads/"+binding.Branch))
	for _, name := range []string{receipt.Source + "/SKILL.md", receipt.Source + "/examples/sample.md"} {
		if !slices.Contains(tree, name) {
			t.Errorf("published skill missing %s: %v", name, tree)
		}
	}
	for _, name := range tree {
		if strings.HasSuffix(name, "/scratch.md") || strings.Contains(name, "/personal/") {
			t.Errorf("unselected or ignored skill material was published: %s", name)
		}
	}
}

func TestShareRejectsInvalidInitialInputs(t *testing.T) {
	for _, name := range []string{"nonempty remote", "nested repository", "private file", "artifact collection", "unsupported link"} {
		t.Run(name, func(t *testing.T) {
			f := newShareFixture(t)
			f.write(t, "guide.md", "# Guide\n")
			paths := []string{"guide.md"}
			switch name {
			case "nonempty remote":
				seed := t.TempDir()
				f.git(t, seed, "init", "--quiet")
				writeRealmshareFile(t, filepath.Join(seed, "readme.md"), "already here\n")
				f.git(t, seed, "add", "readme.md")
				f.git(t, seed, "commit", "--quiet", "-m", "existing")
				f.git(t, seed, "push", f.remote, "HEAD:refs/heads/other")
			case "nested repository":
				f.write(t, "notes/member/readme.md", "# Member\n")
				f.git(t, filepath.Join(f.realm, "notes", "member"), "init", "--quiet")
				paths = []string{"notes"}
			case "private file":
				f.write(t, "AGENTS.local.md", "# Personal\n")
				paths = []string{"AGENTS.local.md"}
			case "artifact collection":
				f.write(t, ".agents/skills/review/SKILL.md", "# Review\n")
				paths = []string{".agents/skills"}
			case "unsupported link":
				target := filepath.Join(t.TempDir(), "outside.md")
				writeRealmshareFile(t, target, "# Outside\n")
				symlinkRealmshare(t, target, filepath.Join(f.realm, "link.md"))
				paths = []string{"link.md"}
			}
			before := f.git(t, f.remote, "for-each-ref")
			if _, err := PreviewShare(t.Context(), f.realm, ShareOptions{Remote: f.remote, Paths: paths}); err == nil {
				t.Fatalf("accepted initial selection %v", paths)
			}
			assertRealmshareAbsent(t, f.state)
			assertRealmshareAbsent(t, filepath.Join(config.Dir(f.home), "realms"))
			if got := f.git(t, f.remote, "for-each-ref"); got != before {
				t.Errorf("preview changed remote refs: %q -> %q", before, got)
			}
		})
	}
}

func TestSharePreviewRejectsNestedBareRepository(t *testing.T) {
	f := newShareFixture(t)
	f.write(t, "notes/guide.md", "# Guide\n")
	bare := filepath.Join(f.realm, "notes", "archive")
	f.git(t, "", "init", "--quiet", "--bare", bare)
	head := readRealmshareFile(t, filepath.Join(bare, "HEAD"))

	if _, err := PreviewShare(t.Context(), f.realm, ShareOptions{Remote: f.remote, Paths: []string{"notes"}}); err == nil || !strings.Contains(err.Error(), "notes/archive") {
		t.Errorf("preview did not identify the nested bare repository: %v", err)
	}
	f.assertUnbound(t)
	f.assertOriginal(t, "notes", "guide.md", "# Guide\n")
	f.assertOriginal(t, "notes/archive", "HEAD", head)
	if got := f.git(t, bare, "rev-parse", "--is-bare-repository"); got != "true" {
		t.Errorf("nested repository is no longer bare: %q", got)
	}
}

func TestShareAddsBoundBucketUsingRecordedDestination(t *testing.T) {
	f := newShareFixture(t)
	original := f.publishGuide(t)
	f.write(t, "sops/index.md", "# Procedures\n")
	f.write(t, "private/index.md", "# Private bucket\n")
	selected := Bucket{Name: "sops", Path: filepath.Join(f.realm, "sops")}
	private := Bucket{Name: "private", Path: filepath.Join(f.realm, "private")}
	original.PrivateBuckets = []Bucket{selected, private}
	if err := SaveBinding(f.realm, original); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(t.TempDir(), "other.git")
	f.git(t, "", "init", "--quiet", "--bare", other)
	for _, opts := range []ShareOptions{
		{Paths: []string{"sops"}, Remote: other},
		{Paths: []string{"sops"}, Branch: "other"},
	} {
		if _, err := PreviewShare(t.Context(), f.realm, opts); err == nil {
			t.Errorf("accepted conflicting override: %+v", opts)
		}
	}
	before := readRealmshareFile(t, filepath.Join(f.state, bindingName))
	plan := f.preview(t, ShareOptions{Paths: []string{"sops/"}})
	portable := Bucket{Name: "sops", Path: "sops"}
	if plan.Remote != original.Remote || plan.Branch != original.Branch || len(plan.Selections) != 1 || !slices.Equal(plan.Selections[0].Buckets, []Bucket{portable}) {
		t.Fatalf("bound preview = %+v", plan)
	}
	if got := readRealmshareFile(t, filepath.Join(f.state, bindingName)); got != before {
		t.Fatal("bound preview changed the local binding")
	}
	f.apply(t, plan)
	binding := f.binding(t)
	if binding.RealmID != original.RealmID || binding.Remote != original.Remote || binding.Branch != original.Branch || !slices.Equal(binding.PrivateBuckets, []Bucket{private}) {
		t.Fatalf("binding after incremental publication = %+v", binding)
	}
	manifest := f.manifest(t, binding)
	if !slices.Equal(manifest.Buckets, []Bucket{portable}) || len(manifest.Mappings) != 2 {
		t.Fatalf("incremental manifest = %+v", manifest)
	}
	f.assertLink(t, binding, "guide.md", "# Guide\n")
	bucketLink := f.assertLink(t, binding, "sops", "")
	f.assertOriginal(t, "private", "index.md", "# Private bucket\n")
	ref := "refs/heads/" + binding.Branch
	if got := f.git(t, f.remote, "show", ref+":"+bucketLink.Source+"/index.md"); got != "# Procedures" {
		t.Errorf("published bucket content = %q", got)
	}
	if tree := f.git(t, f.remote, "ls-tree", "-r", "--name-only", ref); strings.Contains(tree, "private/") {
		t.Errorf("private bucket leaked to remote: %s", tree)
	}
	published := f.git(t, f.remote, "rev-parse", ref)
	linkPath := filepath.Join(f.realm, "sops")
	beforeLink, err := os.Lstat(linkPath)
	if err != nil {
		t.Fatal(err)
	}
	f.apply(t, f.preview(t, ShareOptions{Paths: []string{"sops", "sops/index.md"}}))
	if got := f.git(t, f.remote, "rev-parse", ref); got != published {
		t.Errorf("repeat and covered descendant created a new publication: %s -> %s", published, got)
	}
	if afterLink, err := os.Lstat(linkPath); err != nil || !os.SameFile(beforeLink, afterLink) {
		t.Errorf("repeat replaced the existing symlink: %v", err)
	}
	if got := f.binding(t); !slices.Equal(got.Links, binding.Links) {
		t.Errorf("repeat changed owned link mappings: %+v -> %+v", binding.Links, got.Links)
	}
}

func TestSharePreviewRejectsOverlapWithRemappedBoundSource(t *testing.T) {
	f := newShareFixture(t)
	requireShareSymlinks(t)
	binding := Binding{
		RealmID:   fixtureRealmID,
		ClonePath: filepath.Join(config.Dir(f.home), "realms", "team"),
		Remote:    f.remote,
		Branch:    "team",
		Links:     []LinkReceipt{{Path: "guide.md", Source: "notes/guide.md"}},
	}
	ref := "refs/heads/" + binding.Branch
	definition := "version = 1\nid = \"" + fixtureRealmID + "\"\n" + manifestMapping("notes/guide.md", "guide.md", "file")
	if err := PrepareManagedClone(binding.ClonePath, func(clone string) error {
		f.git(t, clone, "init", "--quiet", "--initial-branch", binding.Branch)
		f.git(t, clone, "remote", "add", "origin", f.remote)
		writeRealmshareFile(t, filepath.Join(clone, manifestName), definition)
		writeRealmshareFile(t, filepath.Join(clone, "notes", "guide.md"), "# Shared\n")
		f.git(t, clone, "add", "--all")
		f.git(t, clone, "commit", "--quiet", "-m", "publish remapped guide")
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	f.git(t, binding.ClonePath, "push", "origin", "HEAD:"+ref)
	symlinkRealmshare(t, filepath.Join(binding.ClonePath, "notes", "guide.md"), filepath.Join(f.realm, "guide.md"))
	if err := SaveBinding(f.realm, binding); err != nil {
		t.Fatal(err)
	}
	f.write(t, "notes/fresh.md", "# Unshared\n")
	f.write(t, "disjoint.md", "# Disjoint\n")
	f.preview(t, ShareOptions{Paths: []string{"disjoint.md"}})
	before := f.git(t, f.remote, "rev-parse", ref)
	bindingPath := filepath.Join(f.state, bindingName)
	beforeBinding := readRealmshareFile(t, bindingPath)

	if _, err := PreviewShare(t.Context(), f.realm, ShareOptions{Paths: []string{"notes"}}); err == nil || !strings.Contains(strings.ToLower(err.Error()), "overlap") {
		t.Errorf("preview did not reject overlap with bound source notes/guide.md: %v", err)
	}
	f.assertOriginal(t, "notes", "fresh.md", "# Unshared\n")
	f.assertLink(t, f.binding(t), "guide.md", "# Shared\n")
	if got := readRealmshareFile(t, bindingPath); got != beforeBinding {
		t.Error("preview changed the local binding")
	}
	if got := readRealmshareFile(t, filepath.Join(binding.ClonePath, manifestName)); got != definition {
		t.Error("preview changed the clone definition")
	}
	if got := f.git(t, binding.ClonePath, "--no-optional-locks", "status", "--porcelain=v1", "--untracked-files=all", "--ignored=matching"); got != "" {
		t.Errorf("preview dirtied the managed clone: %s", got)
	}
	if got := f.git(t, binding.ClonePath, "rev-parse", "HEAD"); got != before {
		t.Errorf("preview changed clone HEAD: %s -> %s", before, got)
	}
	if got := f.git(t, f.remote, "rev-parse", ref); got != before {
		t.Errorf("preview changed the remote branch: %s -> %s", before, got)
	}
}

func TestSharePreviewRejectsOverlapWithUnmappedPublishedPaths(t *testing.T) {
	f := newShareFixture(t)
	binding := f.publishGuide(t)
	const published, local = "# Already published\n", "# Local draft\n"
	for _, path := range []string{"notes/standalone.md", "notes/section/unmapped.md"} {
		writeRealmshareFile(t, filepath.Join(binding.ClonePath, path), published)
	}
	f.git(t, binding.ClonePath, "add", "--", "notes")
	f.git(t, binding.ClonePath, "commit", "--quiet", "-m", "publish unmapped notes")
	ref := "refs/heads/" + binding.Branch
	f.git(t, binding.ClonePath, "push", "origin", "HEAD:"+ref)
	before := f.git(t, f.remote, "rev-parse", ref)
	bindingPath := filepath.Join(f.state, bindingName)
	beforeBinding := readRealmshareFile(t, bindingPath)
	manifestPath := filepath.Join(binding.ClonePath, manifestName)
	beforeManifest := readRealmshareFile(t, manifestPath)

	f.write(t, "notes/standalone.md", local)
	f.write(t, "notes/section/fresh.md", local)
	f.write(t, "notes/disjoint.md", local)
	f.preview(t, ShareOptions{Paths: []string{"notes/disjoint.md"}})
	for _, path := range []string{"notes/standalone.md", "notes/section"} {
		t.Run(path, func(t *testing.T) {
			if _, err := PreviewShare(t.Context(), f.realm, ShareOptions{Paths: []string{path}}); err == nil {
				t.Errorf("preview accepted %q over an unmapped path in the published tree", path)
			}
		})
	}

	f.assertOriginal(t, "notes/standalone.md", "", local)
	f.assertOriginal(t, "notes/section", "fresh.md", local)
	assertRealmshareAbsent(t, filepath.Join(f.realm, "notes", "section", "unmapped.md"))
	if got := readRealmshareFile(t, bindingPath); got != beforeBinding {
		t.Error("preview changed the local binding")
	}
	if got := readRealmshareFile(t, manifestPath); got != beforeManifest {
		t.Error("preview changed the clone definition")
	}
	if got := f.git(t, binding.ClonePath, "--no-optional-locks", "status", "--porcelain=v1", "--untracked-files=all", "--ignored=matching"); got != "" {
		t.Errorf("preview dirtied the managed clone: %s", got)
	}
	if got := f.git(t, binding.ClonePath, "rev-parse", "HEAD"); got != before {
		t.Errorf("preview changed clone HEAD: %s -> %s", before, got)
	}
	if got := f.git(t, f.remote, "rev-parse", ref); got != before {
		t.Errorf("preview changed the remote branch: %s -> %s", before, got)
	}
}

func TestShareRefusesUnrelatedCloneWork(t *testing.T) {
	for _, state := range []string{"unstaged", "staged", "unpublished commit"} {
		t.Run(state, func(t *testing.T) {
			f := newShareFixture(t)
			binding := f.publishGuide(t)
			f.write(t, "addition.md", "# Addition\n")
			f.write(t, "guide.md", "# Unrelated edit\n")
			if state != "unstaged" {
				f.git(t, binding.ClonePath, "add", "--all")
			}
			if state == "unpublished commit" {
				f.git(t, binding.ClonePath, "commit", "--quiet", "-m", "unrelated commit")
			}
			status := f.git(t, binding.ClonePath, "status", "--porcelain=v1")
			local := f.git(t, binding.ClonePath, "rev-parse", "HEAD")
			remote := f.git(t, f.remote, "rev-parse", "refs/heads/"+binding.Branch)
			if err := f.share(t, ShareOptions{Paths: []string{"addition.md"}}); err == nil {
				t.Fatal("accepted an addition with unrelated clone work")
			}
			f.assertOriginal(t, "addition.md", "", "# Addition\n")
			f.assertLink(t, binding, "guide.md", "# Unrelated edit\n")
			if got := f.git(t, binding.ClonePath, "status", "--porcelain=v1"); got != status {
				t.Errorf("clone work changed: %q -> %q", status, got)
			}
			if got := f.git(t, binding.ClonePath, "rev-parse", "HEAD"); got != local {
				t.Errorf("local commit changed: %s -> %s", local, got)
			}
			if got := f.git(t, f.remote, "rev-parse", "refs/heads/"+binding.Branch); got != remote {
				t.Errorf("remote commit changed: %s -> %s", remote, got)
			}
		})
	}
}

func TestShareHTTPSRefusesServerRedirects(t *testing.T) {
	for _, service := range []string{"git-upload-pack", "git-receive-pack"} {
		for _, config := range []string{"default", "url-scoped"} {
			t.Run(service+"/"+config, func(t *testing.T) {
				f := newShareFixture(t)
				if service == "git-receive-pack" {
					requireShareSymlinks(t)
				}
				const original = "# Approved fixture\n"
				f.write(t, "guide.md", original)
				other := filepath.Join(filepath.Dir(f.remote), "other.git")
				f.git(t, f.home, "init", "--quiet", "--bare", other)
				f.git(t, other, "config", "--local", "http.receivepack", "true")
				git, err := exec.LookPath("git")
				if err != nil {
					t.Fatal(err)
				}
				backend := &cgi.Handler{
					Path: git,
					Args: []string{"http-backend"},
					Env: []string{
						"GIT_PROJECT_ROOT=" + filepath.Dir(f.remote),
						"GIT_HTTP_EXPORT_ALL=1",
						"GIT_CONFIG_NOSYSTEM=1",
						"GIT_CONFIG_GLOBAL=" + os.DevNull,
					},
				}
				var uploadRequested, redirectOffered, alternateContacted atomic.Bool
				mux := http.NewServeMux()
				mux.HandleFunc("/team.git/", func(w http.ResponseWriter, r *http.Request) {
					if r.Method == http.MethodGet && r.URL.Path == "/team.git/info/refs" {
						requested := r.URL.Query().Get("service")
						if requested == "git-upload-pack" {
							uploadRequested.Store(true)
						}
						if requested == service {
							redirectOffered.Store(true)
							http.Redirect(w, r, "/other.git/"+strings.TrimPrefix(r.URL.RequestURI(), "/team.git/"), http.StatusFound)
							return
						}
					}
					backend.ServeHTTP(w, r)
				})
				mux.HandleFunc("/other.git/", func(w http.ResponseWriter, r *http.Request) {
					alternateContacted.Store(true)
					backend.ServeHTTP(w, r)
				})
				server := newJoinTLSServer(t, mux)
				remote := server.URL + "/team.git"
				if config == "url-scoped" {
					f.git(t, f.home, "config", "--global", "http."+remote+".followRedirects", "true")
				}
				opts := ShareOptions{Remote: remote, Paths: []string{"guide.md"}}

				if service == "git-upload-pack" {
					if _, err := PreviewShare(t.Context(), f.realm, opts); err == nil {
						t.Error("preview accepted a redirected publication destination")
					}
					f.assertUnbound(t)
				} else {
					plan := f.preview(t, opts)
					if !uploadRequested.Load() || redirectOffered.Load() || alternateContacted.Load() {
						t.Fatal("preview did not discover only the approved repository")
					}
					if err := ApplyShare(t.Context(), plan); err == nil {
						t.Error("apply accepted a redirected publication destination")
					}
					if got := f.git(t, f.remote, "for-each-ref"); got != "" {
						t.Errorf("rejected publication changed approved remote refs: %s", got)
					}
				}
				if !redirectOffered.Load() {
					t.Errorf("the approved URL did not offer the fixture's %s redirect", service)
				}
				if alternateContacted.Load() {
					t.Error("share contacted the alternate URL")
				}
				if got := f.git(t, other, "for-each-ref"); got != "" {
					t.Errorf("share changed alternate remote refs: %s", got)
				}
				f.assertOriginal(t, "guide.md", "", original)
			})
		}
	}
}

func TestTransportHTTPConfigExcludesAuthorityHeaders(t *testing.T) {
	for _, key := range []string{"http.extraHeader", "http.https://approved.example.test/team.git.extraHeader"} {
		t.Run(key, func(t *testing.T) {
			f := newShareFixture(t)
			const authority = "Host: unused.example.test"
			const authorization = "Authorization: Bearer realm-share-fixture"
			f.git(t, f.home, "config", "--global", "--add", key, authority)
			f.git(t, f.home, "config", "--global", "--add", key, authorization)

			env, err := transportHTTPConfig(t.Context(), f.home)
			if err != nil {
				t.Fatal(err)
			}
			var keptAuthorization bool
			for _, entry := range env {
				name, value, _ := strings.Cut(entry, "=")
				if !strings.HasPrefix(name, "GIT_CONFIG_VALUE_") {
					continue
				}
				switch value {
				case authority:
					t.Error("transport config retained an authority override")
				case authorization:
					keptAuthorization = true
				}
			}
			if !keptAuthorization {
				t.Error("transport config dropped Authorization")
			}
		})
	}
}

func TestShareHTTPSPreservesAuthorizationExtraHeader(t *testing.T) {
	f := newShareFixture(t)
	requireShareSymlinks(t)
	const original = "# Authenticated fixture\n"
	const authorization = "Bearer realm-share-fixture"
	f.write(t, "guide.md", original)
	f.git(t, f.remote, "config", "--local", "http.receivepack", "true")
	git, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	backend := &cgi.Handler{
		Path: git,
		Args: []string{"http-backend"},
		Env: []string{
			"GIT_PROJECT_ROOT=" + filepath.Dir(f.remote),
			"GIT_HTTP_EXPORT_ALL=1",
			"GIT_CONFIG_NOSYSTEM=1",
			"GIT_CONFIG_GLOBAL=" + os.DevNull,
		},
	}
	server := newJoinTLSServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != authorization {
			http.Error(w, "fixture authorization required", http.StatusForbidden)
			return
		}
		backend.ServeHTTP(w, r)
	}))
	remote := server.URL + "/team.git"
	f.git(t, f.home, "config", "--global", "http."+remote+".extraHeader", "Authorization: "+authorization)

	if err := f.share(t, ShareOptions{Remote: remote, Paths: []string{"guide.md"}}); err != nil {
		t.Fatal(err)
	}
	binding := f.binding(t)
	f.assertLink(t, binding, "guide.md", original)
	if got := f.git(t, f.remote, "show", "refs/heads/"+binding.Branch+":guide.md"); got != strings.TrimSpace(original) {
		t.Errorf("published guide = %q", got)
	}
}

func TestShareRefusesDisjointPendingLocalMutation(t *testing.T) {
	for _, operation := range []string{"link", "unlink"} {
		t.Run(operation, func(t *testing.T) {
			f, _ := newLinkFixture(t)
			if operation == "unlink" {
				requireShareSymlinks(t)
				if err := ApplyLink(t.Context(), previewRealmLink(t, f.realm, "guide.md")); err != nil {
					t.Fatal(err)
				}
			}
			const original = "# Disjoint addition\n"
			f.write(t, "addition.md", original)
			opts := ShareOptions{Paths: []string{"addition.md"}}
			approved := f.preview(t, opts)
			binding := f.binding(t)
			if operation == "link" {
				recordPendingLink(t, f, previewRealmLink(t, f.realm, "guide.md"))
			} else {
				if len(binding.Links) != 1 || binding.Links[0].Path != "guide.md" {
					t.Fatalf("fixture lacks the owned guide link: %+v", binding.Links)
				}
				binding.PendingLocal = &pendingLocal{Unlink: true, Changes: []localChange{
					{Token: "unlink-0", Link: binding.Links[0]},
				}}
				if err := SaveBinding(f.realm, binding); err != nil {
					t.Fatal(err)
				}
			}
			// Creating and removing the capability probe still changes the realm directory's mtime.
			stamp := time.Unix(1_000_000_000, 0)
			if err := os.Chtimes(f.realm, stamp, stamp); err != nil {
				t.Fatal(err)
			}
			mtime := linkLstat(t, f.realm).ModTime()
			before := f.linkState(t, binding.ClonePath)
			assertRefused := func(stage string, err error) {
				t.Helper()
				if err == nil || !strings.Contains(strings.ToLower(err.Error()), "pending") ||
					!strings.Contains(strings.ToLower(err.Error()), operation) {
					t.Errorf("%s did not refuse the pending %s: %v", stage, operation, err)
				}
				if !linkLstat(t, f.realm).ModTime().Equal(mtime) {
					t.Errorf("%s changed the realm directory despite the pending %s", stage, operation)
				}
				after := f.linkState(t, binding.ClonePath)
				for i, tree := range []string{"realm", "managed clone", "remote"} {
					if after[i] != before[i] {
						t.Errorf("%s changed the %s despite the pending %s", stage, tree, operation)
					}
				}
			}
			_, err := PreviewShare(t.Context(), f.realm, opts)
			assertRefused("preview", err)
			assertRefused("apply", ApplyShare(t.Context(), approved))
			f.assertOriginal(t, "addition.md", "", original)
		})
	}
}

func TestShareRejectedInitialPushPreservesOriginals(t *testing.T) {
	f := newShareFixture(t)
	requireShareSymlinks(t)
	f.write(t, "notes/guide.md", "# Guide\n")
	f.write(t, "second.md", "# Second\n")
	plan := f.preview(t, ShareOptions{Remote: f.remote, Paths: []string{"notes", "second.md"}})
	f.hook(t, "pre-receive", "echo realm-fixture-rejected >&2\nexit 1\n")
	if err := ApplyShare(t.Context(), plan); err == nil || !strings.Contains(err.Error(), "realm-fixture-rejected") {
		t.Fatalf("rejected push error = %v", err)
	}
	f.assertOriginal(t, "notes", "guide.md", "# Guide\n")
	f.assertOriginal(t, "second.md", "", "# Second\n")
	if refs := f.git(t, f.remote, "for-each-ref"); refs != "" {
		t.Errorf("rejected push left remote refs: %s", refs)
	}
}

func TestShareDoesNotRecreateRemoteBranchDeletedBeforePush(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Git executable fixture requires POSIX shebang support")
	}
	f := newShareFixture(t)
	binding := f.publishGuide(t)
	f.write(t, "addition.md", "# Addition\n")
	plan := f.preview(t, ShareOptions{Paths: []string{"addition.md"}})
	ref := "refs/heads/" + binding.Branch
	base := f.git(t, f.remote, "rev-parse", ref)

	git, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	shim := t.TempDir()
	wrapper := filepath.Join(shim, "git")
	marker := filepath.Join(shim, "branch-deleted")
	writeRealmshareFile(t, wrapper, "#!/bin/sh\n"+
		"for arg in \"$@\"; do\n"+
		"  if [ \"$arg\" = push ] && [ ! -f "+quoteShareShell(marker)+" ]; then\n"+
		"    "+quoteShareShell(git)+" -C "+quoteShareShell(f.remote)+" update-ref -d "+quoteShareShell(ref)+" "+quoteShareShell(base)+" || exit 1\n"+
		"    printf 'deleted\\n' > "+quoteShareShell(marker)+" || exit 1\n"+
		"    break\n"+
		"  fi\n"+
		"done\n"+
		"exec "+quoteShareShell(git)+" \"$@\"\n")
	if err := os.Chmod(wrapper, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", shim+string(os.PathListSeparator)+os.Getenv("PATH"))

	err = ApplyShare(t.Context(), plan)
	if got, markerErr := os.ReadFile(marker); markerErr != nil || string(got) != "deleted\n" {
		t.Fatalf("Git fixture did not delete the approved remote branch: %q, %v (apply error: %v)", got, markerErr, err)
	}
	if err == nil {
		t.Error("share succeeded after the approved remote branch was deleted")
	}
	if got := f.git(t, f.remote, "for-each-ref", "--format=%(refname)", ref); got != "" {
		t.Errorf("share recreated the deleted remote branch: %s", got)
	}
	f.assertOriginal(t, "addition.md", "", "# Addition\n")
	after := f.binding(t)
	f.assertLink(t, after, "guide.md", "# Guide\n")
	if pending := after.PendingShare; pending != nil && pending.Published {
		t.Errorf("failed push was recorded as published: %+v", pending)
	}
}

func TestShareResumesOnlyRecordedAddition(t *testing.T) {
	f := newShareFixture(t)
	binding := f.publishGuide(t)
	f.write(t, "sops/index.md", "# Approved\n")
	f.write(t, "draft.md", "# Not approved\n")
	bucket := Bucket{Name: "sops", Path: filepath.Join(f.realm, "sops")}
	binding.PrivateBuckets = []Bucket{bucket}
	if err := SaveBinding(f.realm, binding); err != nil {
		t.Fatal(err)
	}
	ref := "refs/heads/" + binding.Branch
	before := f.git(t, f.remote, "rev-parse", ref)
	opts := ShareOptions{Paths: []string{"sops"}}
	plan := f.preview(t, opts)
	hook := f.hook(t, "pre-receive", "echo realm-fixture-rejected >&2\nexit 1\n")
	if err := ApplyShare(t.Context(), plan); err == nil || !strings.Contains(err.Error(), "realm-fixture-rejected") {
		t.Fatalf("rejected addition error = %v", err)
	}
	pending := f.binding(t)
	if pending.PendingShare == nil || pending.PendingShare.Published || !slices.Equal(pending.PendingShare.Paths, opts.Paths) || pending.PendingShare.Commit == "" || !slices.Equal(pending.PrivateBuckets, []Bucket{bucket}) {
		t.Fatalf("binding lost pending publication or private registration: %+v", pending)
	}
	f.assertOriginal(t, "sops", "index.md", "# Approved\n")
	if got := f.git(t, f.remote, "rev-parse", ref); got != before {
		t.Fatalf("rejected addition changed remote: %s -> %s", before, got)
	}
	if err := os.Remove(hook); err != nil {
		t.Fatal(err)
	}
	if err := f.share(t, ShareOptions{Paths: []string{"sops", "draft.md"}}); err == nil {
		t.Fatal("resumed a pending publication with an expanded selection")
	}
	if got := f.git(t, f.remote, "rev-parse", ref); got != before {
		t.Fatalf("expanded selection published work: %s -> %s", before, got)
	}
	f.assertOriginal(t, "draft.md", "", "# Not approved\n")
	f.assertOriginal(t, "sops", "index.md", "# Approved\n")
	if got := f.git(t, binding.ClonePath, "rev-parse", "HEAD"); got != pending.PendingShare.Commit {
		t.Fatalf("expanded retry changed pending commit: %s -> %s", pending.PendingShare.Commit, got)
	}
	f.write(t, "guide.md", "# Unrelated draft\n")
	if err := f.share(t, opts); err == nil {
		t.Fatal("pending publication accepted unrelated work in the clone")
	}
	f.assertLink(t, pending, "guide.md", "# Unrelated draft\n")
	f.assertOriginal(t, "sops", "index.md", "# Approved\n")
	if got := f.git(t, f.remote, "rev-parse", ref); got != before {
		t.Fatalf("dirty retry changed remote: %s -> %s", before, got)
	}
	f.write(t, "guide.md", "# Guide\n")
	if got := f.git(t, binding.ClonePath, "status", "--porcelain=v1"); got != "" {
		t.Fatalf("clone is not clean before retrying an edited original: %s", got)
	}
	f.write(t, "sops/index.md", "# Edited after approval\n")
	if err := f.share(t, opts); err == nil {
		t.Fatal("exact retry accepted an edited original despite the recorded approval")
	}
	f.assertOriginal(t, "sops", "index.md", "# Edited after approval\n")
	if got := f.git(t, f.remote, "rev-parse", ref); got != before {
		t.Errorf("edited original retry changed remote: %s -> %s", before, got)
	}
	if got := f.git(t, binding.ClonePath, "rev-parse", "HEAD"); got != pending.PendingShare.Commit {
		t.Errorf("edited original retry changed pending commit: %s -> %s", pending.PendingShare.Commit, got)
	}
	if got := f.binding(t); got.PendingShare == nil || got.PendingShare.Published || got.PendingShare.Commit != pending.PendingShare.Commit || !slices.Equal(got.PendingShare.Paths, opts.Paths) || !slices.Equal(got.PrivateBuckets, []Bucket{bucket}) {
		t.Fatalf("edited original retry changed pending publication or private registration: %+v", got)
	}
	f.write(t, "sops/index.md", "# Approved\n")
	f.apply(t, f.preview(t, opts))
	if got := f.git(t, f.remote, "rev-parse", ref); got != pending.PendingShare.Commit {
		t.Errorf("resumed commit = %s, want %s", got, pending.PendingShare.Commit)
	}
	finished := f.binding(t)
	if finished.PendingShare != nil {
		t.Errorf("pending publication not cleared: %+v", finished.PendingShare)
	}
	f.assertLink(t, finished, "sops", "")
	f.assertOriginal(t, "draft.md", "", "# Not approved\n")
}

func TestShareCutoverRevalidatesOriginalAfterPush(t *testing.T) {
	f := newShareFixture(t)
	requireShareSymlinks(t)
	f.write(t, "guide.md", "# Reviewed\n")
	f.write(t, "second.md", "# Second\n")
	original := quoteShareShell(filepath.Join(f.realm, "guide.md"))
	hook := f.hook(t, "post-receive", "printf '# Changed!\\n' > "+original+"\n")
	opts := ShareOptions{Remote: f.remote, Paths: []string{"guide.md", "second.md"}}
	plan := f.preview(t, opts)
	err := ApplyShare(t.Context(), plan)
	if err == nil {
		t.Fatal("cutover succeeded after an original changed")
	}
	if message := strings.ToLower(err.Error()); !strings.Contains(message, "is published") || !strings.Contains(message, "cutover") {
		t.Fatalf("cutover error does not report the already published snapshot: %v", err)
	}
	f.assertOriginal(t, "guide.md", "", "# Changed!\n")
	f.assertOriginal(t, "second.md", "", "# Second\n")
	var guide Mapping
	for _, selection := range plan.Selections {
		if selection.Mapping.Destination == "guide.md" {
			guide = selection.Mapping
		}
	}
	if got := f.git(t, f.remote, "show", "refs/heads/main:"+guide.Source); got != "# Reviewed" {
		t.Errorf("remote snapshot = %q", got)
	}
	published := f.git(t, f.remote, "rev-parse", "refs/heads/main")
	if pending := f.binding(t).PendingShare; pending == nil || !pending.Published || pending.Commit != published {
		t.Fatalf("unfinished cutover did not retain a receipt for published commit %s: %+v", published, pending)
	}
	if err := os.Remove(hook); err != nil {
		t.Fatal(err)
	}
	f.write(t, "guide.md", "# Reviewed\n")
	f.apply(t, f.preview(t, opts))
	if got := f.git(t, f.remote, "rev-parse", "refs/heads/main"); got != published {
		t.Errorf("resuming cutover changed the published commit: %s -> %s", published, got)
	}
	binding := f.binding(t)
	f.assertLink(t, binding, "guide.md", "# Reviewed\n")
	f.assertLink(t, binding, "second.md", "# Second\n")
	if binding.PendingShare != nil {
		t.Errorf("finished cutover retained a pending receipt: %+v", binding.PendingShare)
	}
}

func TestShareRejectsSourceChangeAfterApprovalPreview(t *testing.T) {
	f := newShareFixture(t)
	requireShareSymlinks(t)
	f.write(t, "guide.md", "# Reviewed\n")
	plan := f.preview(t, ShareOptions{Remote: f.remote, Paths: []string{"guide.md"}})
	f.write(t, "guide.md", "# Changed!\n")
	if err := ApplyShare(t.Context(), plan); err == nil {
		t.Fatal("applied an approved plan after the selected content changed")
	}
	f.assertOriginal(t, "guide.md", "", "# Changed!\n")
	if refs := f.git(t, f.remote, "for-each-ref"); refs != "" {
		t.Errorf("stale approval published refs: %s", refs)
	}
}

func TestShareResumesAfterFailedClonePopulation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Git executable fixture requires POSIX shebang support")
	}
	f := newShareFixture(t)
	requireShareSymlinks(t)
	f.write(t, "guide.md", "# Guide\n")
	opts := ShareOptions{Remote: f.remote, Paths: []string{"guide.md"}}
	plan := f.preview(t, opts)
	git, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	shim := t.TempDir()
	wrapper := filepath.Join(shim, "git")
	writeRealmshareFile(t, wrapper, "#!/bin/sh\n"+
		"for arg in \"$@\"; do\n"+
		"  if [ \"$arg\" = init ]; then\n"+
		"    echo realm-fixture-init-failed >&2\n"+
		"    exit 1\n"+
		"  fi\n"+
		"done\n"+
		"exec "+quoteShareShell(git)+" \"$@\"\n")
	if err := os.Chmod(wrapper, 0o700); err != nil {
		t.Fatal(err)
	}
	path := os.Getenv("PATH")
	t.Setenv("PATH", shim+string(os.PathListSeparator)+path)
	if err := ApplyShare(t.Context(), plan); err == nil || !strings.Contains(err.Error(), "realm-fixture-init-failed") {
		t.Fatalf("expected clone population failure from Git init: %v", err)
	}
	f.assertOriginal(t, "guide.md", "", "# Guide\n")
	if refs := f.git(t, f.remote, "for-each-ref"); refs != "" {
		t.Fatalf("failed population changed remote refs: %s", refs)
	}
	t.Setenv("PATH", path)
	f.apply(t, f.preview(t, opts))
	binding := f.binding(t)
	if binding.PendingShare != nil {
		t.Errorf("incomplete population receipt not cleared: %+v", binding.PendingShare)
	}
	link := f.assertLink(t, binding, "guide.md", "# Guide\n")
	if got := f.git(t, f.remote, "show", "refs/heads/"+binding.Branch+":"+link.Source); got != "# Guide" {
		t.Errorf("retried population published guide = %q", got)
	}
}

func TestShareDoesNotAdoptOrDeleteUnrelatedCheckout(t *testing.T) {
	f := newShareFixture(t)
	requireShareSymlinks(t)
	f.write(t, "guide.md", "# Guide\n")
	opts := ShareOptions{Remote: f.remote, Paths: []string{"guide.md"}}
	plan := f.preview(t, opts)
	initBindingClone(t, plan.ClonePath, f.remote, portableManifest)
	marker := filepath.Join(plan.ClonePath, "unrelated.md")
	writeRealmshareFile(t, marker, "# Recoverable work\n")
	if err := ApplyShare(t.Context(), plan); err == nil {
		t.Fatal("adopted an unowned checkout created after preview")
	}
	if got := readRealmshareFile(t, marker); got != "# Recoverable work\n" {
		t.Errorf("unrelated checkout content = %q", got)
	}
	if got := f.git(t, plan.ClonePath, "remote", "get-url", "origin"); got != f.remote {
		t.Errorf("unrelated checkout origin = %q", got)
	}
	f.assertOriginal(t, "guide.md", "", "# Guide\n")
	if refs := f.git(t, f.remote, "for-each-ref"); refs != "" {
		t.Errorf("checkout collision published refs: %s", refs)
	}
}

type shareFixture struct {
	home, realm, state, remote string
}

func newShareFixture(t *testing.T) shareFixture {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("Git unavailable: %v", err)
	}
	home, realm, _ := bindingFixture(t)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, ".gitconfig"))
	t.Setenv("GIT_CONFIG_COUNT", "0")
	t.Setenv("GIT_TEMPLATE_DIR", "")
	t.Setenv("GIT_AUTHOR_NAME", "Realm fixture")
	t.Setenv("GIT_AUTHOR_EMAIL", "realm@example.test")
	t.Setenv("GIT_COMMITTER_NAME", "Realm fixture")
	t.Setenv("GIT_COMMITTER_EMAIL", "realm@example.test")
	f := shareFixture{home: home, realm: realm, state: filepath.Join(realm, ".atomic"), remote: filepath.Join(t.TempDir(), "team.git")}
	f.git(t, "", "init", "--quiet", "--bare", f.remote)
	return f
}

func (f shareFixture) write(t *testing.T, path, content string) {
	t.Helper()
	writeRealmshareFile(t, filepath.Join(f.realm, path), content)
}

func (f shareFixture) git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v (in %s): %v\n%s", args, dir, err, out)
	}
	return strings.TrimSpace(string(out))
}

func (f shareFixture) hook(t *testing.T, name, body string) string {
	t.Helper()
	path := filepath.Join(f.remote, "hooks", name)
	writeRealmshareFile(t, path, "#!/bin/sh\n"+body)
	if err := os.Chmod(path, 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func (f shareFixture) preview(t *testing.T, opts ShareOptions) SharePlan {
	t.Helper()
	plan, err := PreviewShare(t.Context(), f.realm, opts)
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func (f shareFixture) apply(t *testing.T, plan SharePlan) {
	t.Helper()
	if err := ApplyShare(t.Context(), plan); err != nil {
		t.Fatal(err)
	}
}

func (f shareFixture) share(t *testing.T, opts ShareOptions) error {
	t.Helper()
	plan, err := PreviewShare(t.Context(), f.realm, opts)
	if err != nil {
		return err
	}
	return ApplyShare(t.Context(), plan)
}

func (f shareFixture) binding(t *testing.T) Binding {
	t.Helper()
	binding, err := LoadBinding(f.realm)
	if err != nil {
		t.Fatal(err)
	}
	return binding
}

func (f shareFixture) manifest(t *testing.T, binding Binding) Manifest {
	t.Helper()
	manifest, err := LoadManifest(binding.ClonePath, f.realm)
	if err != nil {
		t.Fatal(err)
	}
	published := f.git(t, f.remote, "show", "refs/heads/"+binding.Branch+":"+manifestName)
	if local := readRealmshareFile(t, filepath.Join(binding.ClonePath, manifestName)); strings.TrimSpace(local) != published {
		t.Fatalf("remote manifest differs from the validated local definition: %s", published)
	}
	return manifest
}

func (f shareFixture) publishGuide(t *testing.T) Binding {
	t.Helper()
	requireShareSymlinks(t)
	f.write(t, "guide.md", "# Guide\n")
	f.apply(t, f.preview(t, ShareOptions{Remote: f.remote, Branch: "team", Paths: []string{"guide.md"}}))
	return f.binding(t)
}

func (f shareFixture) assertUnbound(t *testing.T) {
	t.Helper()
	assertRealmshareAbsent(t, config.Dir(f.home))
	assertRealmshareAbsent(t, f.state)
	if _, err := LoadBinding(f.realm); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("binding lookup after preview: %v", err)
	}
	if got := f.git(t, f.remote, "for-each-ref"); got != "" {
		t.Errorf("remote refs after preview = %q", got)
	}
}

func (f shareFixture) assertOriginal(t *testing.T, path, child, want string) {
	t.Helper()
	root := filepath.Join(f.realm, path)
	info, err := os.Lstat(root)
	if err != nil {
		t.Fatal(err)
	}
	if child == "" && !info.Mode().IsRegular() || child != "" && !info.IsDir() {
		t.Fatalf("original %s changed type: %v", root, info.Mode())
	}
	if got := readRealmshareFile(t, filepath.Join(root, child)); got != want {
		t.Errorf("original %s content = %q, want %q", filepath.Join(root, child), got, want)
	}
}

func (f shareFixture) assertLink(t *testing.T, binding Binding, path, want string) LinkReceipt {
	t.Helper()
	local := filepath.Join(f.realm, path)
	if _, err := os.Readlink(local); err != nil {
		t.Fatal(err)
	}
	for _, receipt := range binding.Links {
		if receipt.Path != path {
			continue
		}
		target := filepath.Join(binding.ClonePath, filepath.FromSlash(receipt.Source))
		got, err := os.Stat(local)
		if err != nil {
			t.Fatal(err)
		}
		expected, err := os.Stat(target)
		if err != nil {
			t.Fatal(err)
		}
		if !os.SameFile(got, expected) {
			t.Fatalf("owned symlink %s does not point at %s", local, target)
		}
		if want != "" {
			if content := readRealmshareFile(t, local); content != want {
				t.Errorf("linked content at %s = %q, want %q", local, content, want)
			}
		}
		return receipt
	}
	t.Fatalf("missing ownership receipt for %s", path)
	return LinkReceipt{}
}

func (f shareFixture) restorationBackup(t *testing.T, binding Binding, path string) string {
	t.Helper()
	for _, receipt := range binding.Restorations {
		if receipt.Path != path {
			continue
		}
		if receipt.Digest == "" || receipt.Backup == "" {
			t.Fatalf("incomplete restoration receipt for %s: %+v", path, receipt)
		}
		if filepath.IsAbs(receipt.Backup) {
			return receipt.Backup
		}
		return filepath.Join(f.state, receipt.Backup)
	}
	t.Fatalf("missing restoration receipt for %s", path)
	return ""
}

func quoteShareShell(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func requireShareSymlinks(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	writeRealmshareFile(t, target, "")
	symlinkRealmshare(t, target, filepath.Join(dir, "link"))
}
