package main

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/installstate"
	"github.com/damusix/atomic-claude/atomic/internal/realmshare"
	"github.com/pelletier/go-toml/v2"
)

// The <wikis> projection follows enrollment, never detection, and never
// creates Claude steering on a home that enrolls no Claude target.
func TestWikiSteeringPaths(t *testing.T) {
	type target struct{ harness, root string }
	tests := []struct {
		name         string
		targets      []target
		legacyClaude bool
		want         func(home string) []string
	}{
		{
			name:    "omp only",
			targets: []target{{"omp", ".omp/agent"}},
			want:    func(h string) []string { return []string{filepath.Join(h, ".omp/agent/AGENTS.md")} },
		},
		{
			name:    "claude at a custom root and omp",
			targets: []target{{"claude", "custom-claude"}, {"omp", ".omp/agent"}},
			want: func(h string) []string {
				return []string{filepath.Join(h, "custom-claude/CLAUDE.md"), filepath.Join(h, ".omp/agent/AGENTS.md")}
			},
		},
		{
			name:    "codex projects into its global AGENTS.md",
			targets: []target{{"codex", "codex-home"}},
			want:    func(h string) []string { return []string{filepath.Join(h, "codex-home/AGENTS.md")} },
		},
		{
			name:         "unadopted claude install keeps its existing file",
			legacyClaude: true,
			want:         func(h string) []string { return []string{filepath.Join(h, ".claude/CLAUDE.md")} },
		},
		{
			name:         "an enrolled home ignores an unenrolled claude file",
			targets:      []target{{"omp", ".omp/agent"}},
			legacyClaude: true,
			want:         func(h string) []string { return []string{filepath.Join(h, ".omp/agent/AGENTS.md")} },
		},
		{
			name: "empty home projects nowhere",
			want: func(string) []string { return nil },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := t.TempDir()
			ledger := &installstate.Ledger{}
			for _, tg := range tt.targets {
				root := filepath.Join(home, tg.root)
				ledger.UpsertTarget(installstate.TargetRecord{Harness: tg.harness, Instance: root, NativeRoot: root})
			}
			if err := ledger.Save(config.LedgerPath(home)); err != nil {
				t.Fatal(err)
			}
			if tt.legacyClaude {
				legacy := filepath.Join(home, ".claude", "CLAUDE.md")
				if err := os.MkdirAll(filepath.Dir(legacy), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(legacy, []byte("# mine\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			}

			got, err := wikiSteeringPaths(home)
			if err != nil {
				t.Fatal(err)
			}
			if want := tt.want(home); !reflect.DeepEqual(got, want) {
				t.Errorf("wikiSteeringPaths = %v, want %v", got, want)
			}
		})
	}
}

// An unadopted Claude install under CLAUDE_CONFIG_DIR keeps its <wikis> target.
func TestWikiSteeringPathsFollowsClaudeConfigDir(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, "custom-claude")
	t.Setenv(config.ClaudeConfigDirEnv, root)
	steering := filepath.Join(root, "CLAUDE.md")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(steering, []byte("# mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := wikiSteeringPaths(home)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{steering}; !reflect.DeepEqual(got, want) {
		t.Errorf("wikiSteeringPaths = %v, want %v", got, want)
	}
}

func TestWikiBucketCLIRealmSharingLifecycle(t *testing.T) {
	publisher, publisherHome, remote := newRealmShareCLIFixture(t)
	const (
		wikiIndex   = "wiki/index.md"
		legacy      = "legacy-notes"
		sops        = "sops"
		sopsIndex   = "sops/index.md"
		selected    = "sops/"
		runbook     = "sops/runbook/index.md"
		privateText = "# Receiver's private runbook\n"
		ref         = "refs/heads/" + shareBranch
	)
	legacyPath := filepath.Join(publisher, legacy)
	writeSteeringTestFile(t, filepath.Join(legacyPath, "index.md"), "# Legacy notes\n")
	writeSteeringTestFile(t, filepath.Join(publisher, wikiIndex), "# Shared wiki\n\n<wiki-buckets>\n"+
		`<bucket name="`+legacy+`" path="`+filepath.ToSlash(legacyPath)+`"/>`+"\n</wiki-buckets>\n")
	writeSteeringTestFile(t, filepath.Join(publisher, sharedRealmInstructions), teamRealmGuidance)

	sharedPaths := []string{"wiki/", sharedRealmInstructions}
	args := append(slices.Clone(sharedPaths), "--remote", remote, "--branch", shareBranch)
	review := previewRealmReview(t, publisherHome, "share", args...)
	mustRunRealmCLI(t, publisherHome, "share", append(args, "--yes", "--review", review)...)
	runGit(t, remote, "symbolic-ref", "HEAD", ref)
	base := runGit(t, remote, "rev-parse", ref)

	ownedLinks := func(root string) func() {
		t.Helper()
		links := make(map[string]os.FileInfo)
		for _, rel := range sharedPaths {
			path := filepath.Join(root, rel)
			info, err := os.Lstat(path)
			if err != nil || info.Mode()&os.ModeSymlink == 0 {
				t.Fatalf("expected owned symlink at %s: %v (%v)", path, info, err)
			}
			links[path] = info
		}
		return func() {
			t.Helper()
			for path, before := range links {
				if after, err := os.Lstat(path); err != nil || !os.SameFile(before, after) {
					t.Errorf("existing owned symlink was replaced at %s: %v", path, err)
				}
			}
		}
	}
	publisherLinksUnchanged := ownedLinks(publisher)
	sharedWiki := readSteeringTestFile(t, filepath.Join(publisher, wikiIndex))
	publisherClone := realmCLIManagedClone(t, publisherHome)

	t.Chdir(t.TempDir())
	mustRunWikiBucketCLI(t, publisherHome, publisher, "add", sops)
	if got := readSteeringTestFile(t, filepath.Join(publisher, wikiIndex)); got != sharedWiki {
		t.Errorf("private bucket add changed the shared wiki: got %q, want %q", got, sharedWiki)
	}
	assertRealmShareCLIFile(t, publisher, sharedRealmInstructions, teamRealmGuidance, true)
	if got := runGit(t, remote, "rev-parse", ref); got != base {
		t.Errorf("private bucket add changed remote HEAD: %q -> %q", base, got)
	}
	if t.Failed() {
		return
	}
	if info, err := os.Lstat(filepath.Join(publisher, sops)); err != nil || !info.IsDir() {
		t.Fatalf("private bucket is not a real local directory: %v (%v)", info, err)
	}
	assertWikiBucketCLIList(t, publisherHome, publisher, legacy+"\t"+legacyPath, sops+"\t"+filepath.Join(publisher, sops))
	if got := strings.TrimSpace(mustRunWikiBucketCLI(t, publisherHome, publisher, "doc", sops, "runbook")); got != filepath.Join(publisher, runbook) {
		t.Errorf("private doc path = %q, want %q", got, filepath.Join(publisher, runbook))
	}
	runbookText := readSteeringTestFile(t, filepath.Join(publisher, runbook))
	readSteeringTestFile(t, filepath.Join(publisher, sops, "runbook", "01_raw", "index.md"))
	writeSteeringTestFile(t, filepath.Join(publisher, sops, "checklist.md"), "# Checklist\n")
	mustRunWikiBucketCLI(t, publisherHome, publisher, "index", sops)
	if got := readSteeringTestFile(t, filepath.Join(publisher, sopsIndex)); !strings.Contains(got, "](checklist.md)") {
		t.Errorf("private index did not discover checklist.md:\n%s", got)
	}
	if got := runGit(t, publisherClone, "status", "--porcelain", "--untracked-files=all"); got != "" {
		t.Errorf("private add/doc/index left changes in the shared clone:\n%s", got)
	}

	join := func() (root, home string) {
		t.Helper()
		root, home = t.TempDir(), t.TempDir()
		writeSteeringTestFile(t, config.RepoConfigPath(root), "scope = \"realm\"\n")
		t.Chdir(root)
		mustRunRealmCLI(t, home, "join", remote)
		review := previewRealmReview(t, home, "link", sharedPaths...)
		mustRunRealmCLI(t, home, "link", append(slices.Clone(sharedPaths), "--yes", "--review", review)...)
		return root, home
	}
	receiver, receiverHome := join()
	receiverLinksUnchanged := ownedLinks(receiver)
	collision, collisionHome := join()
	mustRunWikiBucketCLI(t, collisionHome, collision, "add", sops)
	writeSteeringTestFile(t, filepath.Join(collision, sops, "private.md"), privateText)

	t.Chdir(publisher)
	preview := mustRunRealmCLI(t, publisherHome, "share", selected, "--dry-run")
	if !strings.Contains(preview, `Register bucket "sops" at "sops"`) {
		t.Errorf("share review did not show the portable registration:\n%s", preview)
	}
	if _, diag, code := runRealmCLI(t, publisherHome, "share", selected); code == 0 {
		t.Errorf("share without approval succeeded: %s", diag)
	}
	if got := runGit(t, remote, "rev-parse", ref); got != base {
		t.Errorf("unapproved share changed remote HEAD: %q -> %q", base, got)
	}
	if info, err := os.Lstat(filepath.Join(publisher, sops)); err != nil || !info.IsDir() {
		t.Fatalf("unapproved share replaced the private directory: %v (%v)", info, err)
	}
	mustRunRealmCLI(t, publisherHome, "share", selected, "--yes", "--review", realmReviewDigest(t, preview))
	if info, err := os.Lstat(filepath.Join(publisher, sops)); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("approved share did not link the published bucket: %v (%v)", info, err)
	}
	publisherLinksUnchanged()
	assertWikiBucketCLIList(t, publisherHome, publisher, legacy+"\t"+legacyPath, sops+"\t"+filepath.Join(publisher, sops))
	assertRealmShareCLIFile(t, publisher, legacy+"/index.md", "# Legacy notes\n", false)

	published := runGit(t, remote, "rev-parse", ref)
	rawManifest := runGit(t, remote, "show", ref+":realm.toml")
	var manifest realmshare.Manifest
	if err := toml.Unmarshal([]byte(rawManifest), &manifest); err != nil {
		t.Fatalf("parse published realm.toml: %v\n%s", err, rawManifest)
	}
	if want := []realmshare.Bucket{{Name: sops, Path: sops}}; !slices.Equal(manifest.Buckets, want) {
		t.Errorf("published buckets = %+v, want %+v", manifest.Buckets, want)
	}
	var source string
	for _, mapping := range manifest.Mappings {
		if mapping.Destination == sops && mapping.Kind == realmshare.KindDirectory {
			source = mapping.Source
		}
	}
	if source == "" {
		t.Fatalf("published manifest lacks a directory mapping for %q:\n%s", sops, rawManifest)
	}
	if got := runGit(t, remote, "show", ref+":"+source+"/runbook/index.md"); got != runbookText {
		t.Errorf("published runbook = %q, want %q", got, runbookText)
	}
	files := runGit(t, remote, "ls-tree", "-r", "--name-only", ref)
	if strings.Contains(rawManifest, legacy) || strings.Contains(files, legacy) || strings.Contains(files, "private.md") {
		t.Errorf("private bucket data reached the remote:\n%s\n%s", rawManifest, files)
	}
	if got := readSteeringTestFile(t, filepath.Join(publisher, wikiIndex)); strings.Contains(got, legacy) || strings.Contains(got, publisher) {
		t.Errorf("shared wiki contains private registration or an absolute publisher path:\n%s", got)
	}

	t.Chdir(collision)
	assertRealmCLISyncSummary(t, mustRunRealmCLI(t, collisionHome, "sync"), false, true, false)
	if out, diag, code := runRealmCLI(t, collisionHome, "link", selected, "--dry-run"); code == 0 || !strings.Contains(diag, sops) {
		t.Errorf("link review did not reject the private bucket collision: exit %d\nstdout: %s\nstderr: %s", code, out, diag)
	}
	assertWikiBucketCLIList(t, collisionHome, collision, sops+"\t"+filepath.Join(collision, sops))
	assertRealmShareCLIFile(t, collision, sops+"/private.md", privateText, false)
	assertRealmCLIPathAbsent(t, collision, runbook)

	t.Chdir(receiver)
	assertRealmCLISyncSummary(t, mustRunRealmCLI(t, receiverHome, "sync"), false, true, false)
	receiverClone := realmCLIManagedClone(t, receiverHome)
	if got := runGit(t, receiverClone, "rev-parse", "HEAD"); got != published {
		t.Errorf("receiving clone HEAD = %q, want %q", got, published)
	}
	assertRealmCLIPathAbsent(t, receiver, sops)
	assertWikiBucketCLIList(t, receiverHome, receiver)
	if out, diag, code := runWikiBucketCLI(t, receiverHome, receiver, "doc", sops, "unlinked-draft"); code == 0 || !strings.Contains(diag, sops) {
		t.Errorf("doc reached the received but unlinked bucket: exit %d\nstdout: %s\nstderr: %s", code, out, diag)
	}
	assertRealmCLIPathAbsent(t, receiverClone, source+"/unlinked-draft")
	preview = mustRunRealmCLI(t, receiverHome, "link", selected, "--dry-run")
	if !strings.Contains(preview, `Shared bucket "sops" at "sops"`) {
		t.Errorf("link review did not show the received registration:\n%s", preview)
	}
	assertRealmCLIPathAbsent(t, receiver, sops)
	mustRunRealmCLI(t, receiverHome, "link", selected, "--yes", "--review", realmReviewDigest(t, preview))
	if info, err := os.Lstat(filepath.Join(receiver, sops)); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("approved link did not install the receiver's bucket symlink: %v (%v)", info, err)
	}
	receiverLinksUnchanged()
	assertRealmShareCLIFile(t, receiver, runbook, runbookText, false)

	ambient := t.TempDir()
	t.Chdir(ambient)
	assertWikiBucketCLIList(t, receiverHome, receiver, sops+"\t"+filepath.Join(receiver, sops))
	const receiverDoc = "sops/receiver-draft/index.md"
	if got := strings.TrimSpace(mustRunWikiBucketCLI(t, receiverHome, receiver, "doc", sops, "receiver-draft")); got != filepath.Join(receiver, receiverDoc) {
		t.Errorf("shared doc path = %q, want %q", got, filepath.Join(receiver, receiverDoc))
	}
	readSteeringTestFile(t, filepath.Join(receiver, receiverDoc))
	assertRealmCLIPathAbsent(t, publisher, receiverDoc)
	assertRealmCLIPathAbsent(t, ambient, sops)
	writeSteeringTestFile(t, filepath.Join(receiver, sops, "receiver-checklist.md"), "# Receiver checklist\n")
	mustRunWikiBucketCLI(t, receiverHome, receiver, "index", sops)
	if got := readSteeringTestFile(t, filepath.Join(receiver, sopsIndex)); !strings.Contains(got, "](receiver-checklist.md)") {
		t.Errorf("shared index did not follow the receiver's owned bucket root:\n%s", got)
	}
	if got := runGit(t, remote, "rev-parse", ref); got != published {
		t.Errorf("receiving, linking, or local indexing advanced the remote: %q -> %q", published, got)
	}
}

func TestWikiBucketCLIUnboundHome(t *testing.T) {
	home := t.TempDir()
	t.Chdir(t.TempDir())
	const name = "home-notes"
	wikiIndex := filepath.Join(home, "wiki", "index.md")
	writeSteeringTestFile(t, wikiIndex, "# Home wiki\n")

	mustRunWikiBucketCLI(t, home, home, "add", name)
	bucket := filepath.Join(home, name)
	assertWikiBucketCLIList(t, home, home, name+"\t"+bucket)
	if got := readSteeringTestFile(t, wikiIndex); !strings.HasPrefix(got, "# Home wiki\n") || !strings.Contains(got, "<wiki-buckets>") {
		t.Errorf("legacy add did not preserve the home wiki and write its registry:\n%s", got)
	}
	if got := readSteeringTestFile(t, filepath.Join(bucket, "index.md")); !strings.Contains(got, "# "+name+"\n") {
		t.Errorf("legacy add did not create the bucket index:\n%s", got)
	}
}

func TestWikiBucketCLIUnboundAddRequiresOperationLock(t *testing.T) {
	root, home := t.TempDir(), t.TempDir()
	t.Chdir(root)
	writeSteeringTestFile(t, config.RepoConfigPath(root), "scope = \"realm\"\n")
	writeSteeringTestFile(t, filepath.Join(root, "wiki", "index.md"), "# Local wiki\n")
	writeSteeringTestFile(t, filepath.Join(root, "CLAUDE.md"), "# Local guidance\n")
	lockPath := config.OperationLockPath(home)
	if err := os.MkdirAll(lockPath, 0o755); err != nil {
		t.Fatal(err)
	}
	unchanged := unchangedRealmCLITrees(t, root, lockPath)

	out, diag, code := runWikiBucketCLI(t, home, root, "add", "local-notes")
	if code == 0 || !strings.Contains(diag, filepath.Base(lockPath)) {
		t.Errorf("bucket add did not refuse the unavailable operation lock: exit %d\nstdout: %s\nstderr: %s", code, out, diag)
	}
	unchanged(t)
}

func TestWikiInitCLILinkedSharedWiki(t *testing.T) {
	for _, mode := range []string{"explicit root", "implicit root from linked descendant", "explicit root from linked descendant"} {
		t.Run(mode, func(t *testing.T) {
			root, home, remote := newRealmShareCLIFixture(t)
			root, err := filepath.EvalSymlinks(root)
			if err != nil {
				t.Fatal(err)
			}
			wikiDir := filepath.Join(root, "wiki")
			const index = "# Team wiki\n"
			const topic = "topics/runbook.md"
			const runbook = "# Team runbook\n"
			writeSteeringTestFile(t, filepath.Join(wikiDir, "index.md"), index)
			writeSteeringTestFile(t, filepath.Join(wikiDir, topic), runbook)
			share := []string{"wiki/", "--remote", remote, "--branch", shareBranch}
			review := previewRealmReview(t, home, "share", share...)
			mustRunRealmCLI(t, home, "share", append(share, "--yes", "--review", review)...)

			clone := realmCLIManagedClone(t, home)
			cloneWiki := filepath.Join(clone, "wiki")
			assertRealmShareCLIFile(t, cloneWiki, "index.md", index, false)
			assertRealmShareCLIFile(t, cloneWiki, topic, runbook, false)
			link, err := os.Lstat(wikiDir)
			if err != nil || link.Mode()&os.ModeSymlink == 0 {
				t.Fatalf("shared wiki is not linked: %v (%v)", link, err)
			}
			unchanged := unchangedRealmCLITrees(t, root, cloneWiki)

			args := []string{"root", "wiki", "init", "--scope", "realm"}
			env := []string{"PATH=" + os.Getenv("PATH")}
			descendant := filepath.Join(wikiDir, filepath.Dir(topic))
			switch mode {
			case "explicit root":
				args = append(args, "--root", root)
			case "implicit root from linked descendant":
				t.Chdir(descendant)
				env = append(env, "PWD="+descendant)
			case "explicit root from linked descendant":
				args = append(args, "--root", descendant)
			}
			out, diag, code := runAtomicCLIFull(t, home, env, args...)
			if mode == "explicit root from linked descendant" {
				if code == 0 && !strings.Contains(out, "using existing shared wiki "+wikiDir+"\n") {
					t.Errorf("wiki init succeeded without using the consuming realm's wiki %s\nstdout: %s\nstderr: %s", wikiDir, out, diag)
				}
			} else if code != 0 {
				t.Errorf("wiki init: exit %d\nstdout: %s\nstderr: %s", code, out, diag)
			}
			if after, err := os.Lstat(wikiDir); err != nil || !os.SameFile(link, after) {
				t.Errorf("wiki init replaced the owned wiki symlink: %v (%v)", after, err)
			}
			unchanged(t)
			assertRealmCLIPathAbsent(t, cloneWiki, ".git")
			for _, loader := range []string{"AGENTS.md", "CLAUDE.md"} {
				assertRealmCLIPathAbsent(t, cloneWiki, loader)
				assertRealmCLIPathAbsent(t, clone, loader)
			}
		})
	}
}

func TestWikiInitCLIRejectsUnownedWikiLink(t *testing.T) {
	for _, mode := range []string{"marked realm", "no scope marker"} {
		t.Run(mode, func(t *testing.T) {
			root, home, target := t.TempDir(), t.TempDir(), t.TempDir()
			t.Chdir(root)
			if mode == "marked realm" {
				writeSteeringTestFile(t, config.RepoConfigPath(root), "scope = \"realm\"\n")
			}
			writeSteeringTestFile(t, filepath.Join(target, "index.md"), "# Local wiki\n")
			if err := os.Symlink(target, filepath.Join(root, "wiki")); err != nil {
				t.Fatal(err)
			}
			unchanged := unchangedRealmCLITrees(t, root, target)

			out, diag, code := runAtomicCLIFull(t, home, []string{"PATH=" + os.Getenv("PATH")},
				"root", "wiki", "init", "--scope", "realm", "--root", root)
			if code == 0 {
				t.Errorf("wiki init accepted an unowned wiki symlink\nstdout: %s\nstderr: %s", out, diag)
			}
			unchanged(t)
		})
	}
}

func TestWikiInitCLIRejectsUnregisteredLinkedCWD(t *testing.T) {
	root, home, target := t.TempDir(), t.TempDir(), t.TempDir()
	root, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	writeSteeringTestFile(t, filepath.Join(root, "wiki", "index.md"), "# Local wiki\n")
	writeSteeringTestFile(t, filepath.Join(target, "topics", "runbook.md"), "# Runbook\n")
	alias := filepath.Join(root, "notes")
	if err := os.Symlink(target, alias); err != nil {
		t.Fatal(err)
	}
	descendant := filepath.Join(alias, "topics")
	t.Chdir(descendant)
	unchanged := unchangedRealmCLITrees(t, root, target, home)

	out, diag, code := runAtomicCLIFull(t, home, []string{"PATH=" + os.Getenv("PATH"), "PWD=" + descendant},
		"root", "wiki", "init", "--scope", "realm")
	if code == 0 {
		t.Errorf("wiki init accepted a working directory under an unregistered alias\nstdout: %s\nstderr: %s", out, diag)
	}
	unchanged(t)
}

func TestWikiScanCLILinkedDescendant(t *testing.T) {
	root, home, remote := newRealmShareCLIFixture(t)
	root, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	wikiIndex := filepath.Join(root, "wiki", "index.md")
	descendant := filepath.Join(root, "wiki", "topics")
	writeSteeringTestFile(t, wikiIndex, "# Shared wiki\n")
	// The existing index stops an unchecked scan before it can write.
	writeSteeringTestFile(t, filepath.Join(descendant, "wiki", "index.md"), "# Authored topic wiki\n")
	share := []string{"wiki/", "--remote", remote, "--branch", shareBranch}
	review := previewRealmReview(t, home, "share", share...)
	mustRunRealmCLI(t, home, "share", append(share, "--yes", "--review", review)...)

	const member = "repos/service"
	memberDir := filepath.Join(root, member)
	writeSteeringTestFile(t, filepath.Join(memberDir, "README.md"), "# Service\n")
	runGit(t, memberDir, "init", "--quiet")
	env := []string{"PATH=" + os.Getenv("PATH")}
	t.Run("explicit root is refused", func(t *testing.T) {
		unchanged := unchangedRealmCLITrees(t, root, home)
		out, diag, code := runAtomicCLIFull(t, home, env, "root", "wiki", "scan", "--root", descendant)
		if code != 1 || !strings.Contains(diag, "use --root "+strconv.Quote(root)) {
			t.Errorf("wiki scan did not direct the user to the consuming realm: exit %d\nstdout: %s\nstderr: %s", code, out, diag)
		}
		unchanged(t)
	})
	t.Run("implicit root uses the consuming realm", func(t *testing.T) {
		t.Chdir(descendant)
		unchanged := unchangedRealmCLITrees(t, filepath.Join(realmCLIManagedClone(t, home), "wiki"))
		out, diag, code := runAtomicCLIFull(t, home, append(env, "PWD="+descendant), "root", "wiki", "scan")
		if code != 0 || !strings.Contains(out, "pending "+member+"\n") {
			t.Errorf("wiki scan did not discover the consuming realm's member: exit %d\nstdout: %s\nstderr: %s", code, out, diag)
		}
		if got := readSteeringTestFile(t, config.WikisPath(home)); !strings.Contains(got, wikiIndex) || strings.Contains(got, descendant) {
			t.Errorf("wiki scan did not register only the consuming realm's wiki:\n%s", got)
		}
		unchanged(t)
	})
}

func TestWikiScanCLIRejectsUnownedLinkedDescendant(t *testing.T) {
	root, home, target := t.TempDir(), t.TempDir(), t.TempDir()
	root, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	writeSteeringTestFile(t, filepath.Join(target, "topics", "wiki", "index.md"), "# Authored topic wiki\n")
	alias := filepath.Join(root, "notes")
	if err := os.Symlink(target, alias); err != nil {
		t.Fatal(err)
	}
	descendant := filepath.Join(alias, "topics")
	t.Chdir(descendant)
	unchanged := unchangedRealmCLITrees(t, root, home, target)
	env := []string{"PWD=" + descendant}
	for _, explicit := range []bool{false, true} {
		args := []string{"root", "wiki", "scan"}
		if explicit {
			args = append(args, "--root", descendant)
		}
		out, diag, code := runAtomicCLIFull(t, home, env, args...)
		if code != 1 || !strings.Contains(diag, "symlink not owned by the local realm; specify the realm with --root") {
			t.Errorf("wiki scan did not refuse the unowned symlink (explicit root: %t): exit %d\nstdout: %s\nstderr: %s", explicit, code, out, diag)
		}
	}
	unchanged(t)
}

func TestWikiScanCLILinkedSharedWiki(t *testing.T) {
	root, home, remote := newRealmShareCLIFixture(t)
	const (
		sharedBucket  = "team-notes"
		privateBucket = "local-notes"
		wikiBucket    = "wiki-notes"
		member        = "repos/scan-service"
		noteLink      = "](scan-note.md)"
	)
	wikiDir := filepath.Join(root, "wiki")
	wikiIndex := filepath.Join(wikiDir, "index.md")
	writeSteeringTestFile(t, wikiIndex, "# Team wiki\n\n<wiki-scan>\n</wiki-scan>\n")
	writeSteeringTestFile(t, filepath.Join(wikiDir, "scan-note.md"), "# Wiki scan note\n")
	writeSteeringTestFile(t, filepath.Join(root, sharedRealmInstructions), teamRealmGuidance)
	args := []string{"wiki/", sharedRealmInstructions, "--remote", remote, "--branch", shareBranch}
	review := previewRealmReview(t, home, "share", args...)
	mustRunRealmCLI(t, home, "share", append(args, "--yes", "--review", review)...)

	mustRunWikiBucketCLI(t, home, root, "add", sharedBucket)
	review = previewRealmReview(t, home, "share", sharedBucket+"/")
	mustRunRealmCLI(t, home, "share", sharedBucket+"/", "--yes", "--review", review)
	if info, err := os.Lstat(filepath.Join(root, sharedBucket)); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("shared bucket is not linked: %v (%v)", info, err)
	}

	mustRunWikiBucketCLI(t, home, root, "add", privateBucket)
	manifest := filepath.Join(realmCLIManagedClone(t, home), "realm.toml")
	writeSteeringTestFile(t, manifest, readSteeringTestFile(t, manifest)+`
[[buckets]]
name = "`+wikiBucket+`"
path = "wiki"
`)
	assertWikiBucketCLIList(t, home, root,
		privateBucket+"\t"+filepath.Join(root, privateBucket),
		sharedBucket+"\t"+filepath.Join(root, sharedBucket),
		wikiBucket+"\t"+wikiDir,
	)

	buckets := []string{privateBucket, sharedBucket}
	for _, name := range buckets {
		dir := filepath.Join(root, name)
		if got := readSteeringTestFile(t, filepath.Join(dir, "index.md")); got == "" || strings.Contains(got, noteLink) {
			t.Fatalf("%s needs an existing, stale bucket index:\n%s", name, got)
		}
		writeSteeringTestFile(t, filepath.Join(dir, "scan-note.md"), "# Scan note\n")
	}
	descendant := filepath.Join(wikiDir, "topics")
	writeSteeringTestFile(t, filepath.Join(descendant, "scan-note.md"), "# Wiki topic note\n")
	assertRealmCLIPathAbsent(t, descendant, "index.md")
	writeSteeringTestFile(t, filepath.Join(root, member, "README.md"), "# Scan service\n")
	runGit(t, filepath.Join(root, member), "init", "--quiet")
	localSteering := filepath.Join(home, ".claude", "CLAUDE.md")
	writeSteeringTestFile(t, localSteering, "# Personal guidance\n")

	if info, err := os.Lstat(wikiDir); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("shared wiki is not linked: %v (%v)", info, err)
	}
	centralBefore := readSteeringTestFile(t, wikiIndex)
	if !strings.HasPrefix(centralBefore, "# Team wiki\n") || strings.Contains(centralBefore, "<wiki-scan") || strings.Contains(centralBefore, noteLink) {
		t.Fatalf("shared wiki needs its original heading, no scan marker, and a stale bucket index:\n%s", centralBefore)
	}

	out, diag, code := runAtomicCLIFull(t, home, []string{"PATH=" + os.Getenv("PATH")}, "root", "wiki", "scan", "--root", root)
	if code != 0 {
		t.Fatalf("wiki scan: exit %d\nstdout: %s\nstderr: %s", code, out, diag)
	}
	if !strings.Contains(out, "1 repos · 0 indexed · 1 pending") || !strings.Contains(out, "pending "+member+"\n") {
		t.Errorf("wiki scan did not classify the local member:\n%s", out)
	}
	if got := readSteeringTestFile(t, wikiIndex); got != centralBefore {
		t.Errorf("wiki scan changed the shared wiki index:\ngot:  %q\nwant: %q", got, centralBefore)
	}
	if got := readSteeringTestFile(t, filepath.Join(descendant, "index.md")); !strings.Contains(got, noteLink) {
		t.Errorf("wiki scan did not generate the descendant bucket index:\n%s", got)
	}
	for _, name := range buckets {
		if got := readSteeringTestFile(t, filepath.Join(root, name, "index.md")); !strings.Contains(got, noteLink) {
			t.Errorf("wiki scan did not rebuild the %s bucket index:\n%s", name, got)
		}
	}
	for _, path := range []string{filepath.Join(config.Dir(home), "wikis.md"), localSteering} {
		if got := readSteeringTestFile(t, path); !strings.Contains(got, wikiIndex) {
			t.Errorf("wiki scan did not register the consuming realm in %s:\n%s", path, got)
		}
	}
	assertRealmShareCLIFile(t, root, sharedRealmInstructions, teamRealmGuidance, true)
	assertRealmCLIPathAbsent(t, wikiDir, ".git")

	mustRunWikiBucketCLI(t, home, root, "index", wikiBucket)
	if got := readSteeringTestFile(t, wikiIndex); !strings.Contains(got, noteLink) {
		t.Errorf("explicit bucket index did not rebuild the shared wiki index:\n%s", got)
	}
}

func runWikiBucketCLI(t *testing.T, home, root, verb string, args ...string) (string, string, int) {
	t.Helper()
	return runAtomicCLIFull(t, home, []string{"PATH=" + os.Getenv("PATH")},
		append([]string{"root", "wiki", "bucket", verb, "--root", root}, args...)...)
}

func mustRunWikiBucketCLI(t *testing.T, home, root, verb string, args ...string) string {
	t.Helper()
	out, diag, code := runWikiBucketCLI(t, home, root, verb, args...)
	if code != 0 {
		t.Fatalf("wiki bucket %s %v: exit %d\nstdout: %s\nstderr: %s", verb, args, code, out, diag)
	}
	return out
}

func assertWikiBucketCLIList(t *testing.T, home, root string, want ...string) {
	t.Helper()
	out := strings.TrimSpace(mustRunWikiBucketCLI(t, home, root, "list"))
	var got []string
	if out != "" {
		got = strings.Split(out, "\n")
		slices.Sort(got)
	}
	if !slices.Equal(got, want) {
		t.Errorf("wiki bucket list for %s = %q, want %q", root, got, want)
	}
}
