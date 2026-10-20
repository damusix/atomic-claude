package wiki

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/config"
)

// isGitMember only needs a .git entry, so no git init is required.
func discoverFixture(t *testing.T, repos ...string) string {
	t.Helper()
	root := t.TempDir()
	for _, r := range repos {
		if err := os.MkdirAll(filepath.Join(root, r, ".git"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func writeRealmTOML(t *testing.T, root, body string) {
	t.Helper()
	restore := config.SetHarnessDirForTest(".claude")
	t.Cleanup(restore)
	p := config.RepoConfigPath(root)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func discover(t *testing.T, root string) ([]string, []string) {
	t.Helper()
	members, warns, err := discoverMembers(root, filepath.Join(root, "wiki"))
	if err != nil {
		t.Fatalf("discoverMembers: %v", err)
	}
	return members, warns
}

func assertMembers(t *testing.T, got []string, want ...string) {
	t.Helper()
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("members = %v, want %v", got, want)
	}
}

// The warning names the config file so the user knows what to edit.
func assertOneWarning(t *testing.T, warns []string, root, entry, phrase string) {
	t.Helper()
	if len(warns) != 1 {
		t.Fatalf("warnings = %v, want exactly 1", warns)
	}
	w := warns[0]
	for _, want := range []string{entry, root, config.RepoConfigPath(root), phrase} {
		if !strings.Contains(w, want) {
			t.Errorf("warning %q does not contain %q", w, want)
		}
	}
}

func symlinkWarnings(warns []string) []string {
	var out []string
	for _, w := range warns {
		if strings.Contains(w, "symbolic link") {
			out = append(out, w)
		}
	}
	return out
}

// Realms that never opt in must discover exactly what they did before the key
// existed.
func TestDiscover_UnsetWalksWholeRoot(t *testing.T) {
	root := discoverFixture(t, "agency/a", "experiments/b", "loose", "node_modules/c")
	members, warns := discover(t, root)
	assertMembers(t, members, "agency/a", "experiments/b", "loose")
	if len(warns) != 0 {
		t.Errorf("unexpected warnings: %v", warns)
	}
}

// The point of the key: folders the user did not list (scratch, clones of
// third-party code) stay out of the realm.
func TestDiscover_DeclaredRootsExcludeUndeclaredFolders(t *testing.T) {
	root := discoverFixture(t, "agency/a", "agency/sub/b", "experiments/c", "scratch/d")
	writeRealmTOML(t, root, "[realm]\nrepos = [\"agency\", \"experiments\"]\n")
	members, warns := discover(t, root)
	assertMembers(t, members, "agency/a", "agency/sub/b", "experiments/c")
	if len(warns) != 0 {
		t.Errorf("unexpected warnings: %v", warns)
	}
}

func TestDiscover_DeclaredRootThatIsARepo(t *testing.T) {
	root := discoverFixture(t, "solo", "solo/inner", "other")
	writeRealmTOML(t, root, "[realm]\nrepos = [\"solo\"]\n")
	members, _ := discover(t, root)
	assertMembers(t, members, "solo")
}

func TestDiscover_MissingPathWarns(t *testing.T) {
	root := discoverFixture(t, "agency/a")
	writeRealmTOML(t, root, "[realm]\nrepos = [\"agency\", \"gone\"]\n")
	members, warns := discover(t, root)
	assertMembers(t, members, "agency/a")
	assertOneWarning(t, warns, root, `"gone"`, "matches nothing")
}

func TestDiscover_BracePattern(t *testing.T) {
	root := discoverFixture(t, "companya/x", "companyb/y", "companyc/z")
	writeRealmTOML(t, root, "[realm]\nrepos = [\"{companya,companyb}\"]\n")
	members, warns := discover(t, root)
	assertMembers(t, members, "companya/x", "companyb/y")
	if len(warns) != 0 {
		t.Errorf("unexpected warnings: %v", warns)
	}
}

// A brace list is one entry: it is fine for some alternatives to be absent, the
// user only made a mistake when none of them exists.
func TestDiscover_BraceWithOneMissingAlternativeIsSilent(t *testing.T) {
	root := discoverFixture(t, "companya/x")
	writeRealmTOML(t, root, "[realm]\nrepos = [\"{companya,companyb}\"]\n")
	members, warns := discover(t, root)
	assertMembers(t, members, "companya/x")
	if len(warns) != 0 {
		t.Errorf("unexpected warnings: %v", warns)
	}
}

func TestDiscover_BraceMatchingNothingWarns(t *testing.T) {
	root := discoverFixture(t, "agency/a")
	writeRealmTOML(t, root, "[realm]\nrepos = [\"agency\", \"{companya,companyb}\"]\n")
	_, warns := discover(t, root)
	assertOneWarning(t, warns, root, `"{companya,companyb}"`, "matches nothing")
}

func TestDiscover_FileMatchWarns(t *testing.T) {
	root := discoverFixture(t, "agency/a")
	if err := os.WriteFile(filepath.Join(root, "notes.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeRealmTOML(t, root, "[realm]\nrepos = [\"agency\", \"notes.md\"]\n")
	members, warns := discover(t, root)
	assertMembers(t, members, "agency/a")
	assertOneWarning(t, warns, root, `"notes.md"`, "is not a directory")
	if strings.Contains(warns[0], `end the pattern with "/"`) {
		t.Errorf("literal entry got the pattern hint: %q", warns[0])
	}
}

// An entry escaping the realm would pull another realm's repos into this one.
func TestDiscover_OutOfRootWarns(t *testing.T) {
	for _, entry := range []string{"..", "../elsewhere", "agency/../../x", "/abs/path"} {
		t.Run(entry, func(t *testing.T) {
			root := discoverFixture(t, "agency/a")
			writeRealmTOML(t, root, "[realm]\nrepos = [\"agency\", \""+entry+"\"]\n")
			members, warns := discover(t, root)
			assertMembers(t, members, "agency/a")
			assertOneWarning(t, warns, root, `"`+entry+`"`, "must stay inside the realm root")
		})
	}
}

func TestDiscover_InvalidPatternWarns(t *testing.T) {
	root := discoverFixture(t, "agency/a")
	writeRealmTOML(t, root, "[realm]\nrepos = [\"agency\", \"bad[\"]\n")
	members, warns := discover(t, root)
	assertMembers(t, members, "agency/a")
	assertOneWarning(t, warns, root, `"bad["`, "is not a valid pattern")
}

// An empty list most likely means "not configured yet"; scanning zero repos
// would make stale report every recorded member as removed.
func TestDiscover_EmptyListWalksWholeRootAndWarns(t *testing.T) {
	root := discoverFixture(t, "agency/a", "scratch/b")
	writeRealmTOML(t, root, "[realm]\nrepos = []\n")
	members, warns := discover(t, root)
	assertMembers(t, members, "agency/a", "scratch/b")
	if len(warns) != 1 || !strings.Contains(warns[0], "empty; scanning all of "+root+"; list repo paths") || !strings.Contains(warns[0], config.RepoConfigPath(root)) {
		t.Errorf("warnings = %v, want one naming the empty list and %s", warns, config.RepoConfigPath(root))
	}
}

// Lenient-load contract: a broken atomic.toml never stops the scan.
func TestDiscover_MalformedTOMLWalksWholeRootAndWarns(t *testing.T) {
	root := discoverFixture(t, "agency/a", "scratch/b")
	writeRealmTOML(t, root, "[realm\nrepos = [\n")
	members, warns := discover(t, root)
	assertMembers(t, members, "agency/a", "scratch/b")
	if len(warns) != 1 || !strings.Contains(warns[0], config.RepoConfigPath(root)) {
		t.Errorf("warnings = %v, want one naming %s", warns, config.RepoConfigPath(root))
	}
}

func TestDiscover_NestedRootsDeduplicate(t *testing.T) {
	for _, tc := range []struct {
		repos       string
		fixture     []string
		wantMembers []string
	}{
		{`"agency/team", "agency", "agency/*"`, []string{"agency/a", "agency/team/b"}, []string{"agency/a", "agency/team/b"}},
		{`"agency/", "agency/team"`, []string{"agency/a", "agency/team/b"}, []string{"agency/a", "agency/team/b"}},
		{`"solo/", "solo/inner"`, []string{"solo", "solo/inner"}, []string{"solo"}},
		{`"solo", "solo/inner"`, []string{"solo", "solo/inner"}, []string{"solo"}},
	} {
		t.Run(tc.repos, func(t *testing.T) {
			root := discoverFixture(t, tc.fixture...)
			writeRealmTOML(t, root, "[realm]\nrepos = ["+tc.repos+"]\n")
			members, warns := discover(t, root)
			assertMembers(t, members, tc.wantMembers...)
			if len(warns) != 0 {
				t.Errorf("unexpected warnings: %v", warns)
			}
		})
	}
}

// The whole-root walk never enters these, so a glob reaching into them must not
// complain about what it finds there either.
func TestDiscover_GlobHitsUnderSkippedDirsNeverWarn(t *testing.T) {
	root := discoverFixture(t, "agency/a")
	if err := os.MkdirAll(filepath.Join(root, "agency/a/node_modules"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(root, "agency/a/node_modules/pkg")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "agency/a/.git/HEAD"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeRealmTOML(t, root, "[realm]\nrepos = [\"**\"]\n")
	members, warns := discover(t, root)
	assertMembers(t, members, "agency/a")
	for _, w := range warns {
		if strings.Contains(w, "node_modules") || strings.Contains(w, ".git") || strings.Contains(w, "symbolic link") {
			t.Errorf("unexpected warning: %q", w)
		}
	}
}

// The wiki is never a member, and junk dirs stay out even when an entry or a
// glob names them.
func TestDiscover_GlobSkipsWikiAndJunkDirs(t *testing.T) {
	root := discoverFixture(t, "agency/a", "wiki/inner", "node_modules/c", "agency/vendor/d")
	writeRealmTOML(t, root, "[realm]\nrepos = [\"*\", \"agency/vendor\"]\n")
	members, warns := discover(t, root)
	assertMembers(t, members, "agency/a")
	if len(warns) != 0 {
		t.Errorf("unexpected warnings: %v", warns)
	}
}

// "." names the realm root; the root itself is still never a member.
func TestDiscover_DotEntryWalksWholeRoot(t *testing.T) {
	root := discoverFixture(t, ".", "agency/a", "scratch/b")
	writeRealmTOML(t, root, "[realm]\nrepos = [\".\"]\n")
	members, _ := discover(t, root)
	assertMembers(t, members, "agency/a", "scratch/b")
}

func TestDiscover_PatternFileMatchWarnsWithTrailingSlashHint(t *testing.T) {
	root := discoverFixture(t, "agency/a")
	if err := os.WriteFile(filepath.Join(root, "notes.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeRealmTOML(t, root, "[realm]\nrepos = [\"{agency,notes.md}\"]\n")
	members, warns := discover(t, root)
	assertMembers(t, members, "agency/a")
	assertOneWarning(t, warns, root, `"{agency,notes.md}"`, `matches notes.md, which is not a directory; end the pattern with "/"`)
}

func TestDiscover_TrailingSlashMatchesDirectoriesOnly(t *testing.T) {
	for _, entry := range []string{"*/", "agency/"} {
		t.Run(entry, func(t *testing.T) {
			root := discoverFixture(t, "agency/a")
			if err := os.WriteFile(filepath.Join(root, "notes.md"), []byte("x"), 0o644); err != nil {
				t.Fatal(err)
			}
			writeRealmTOML(t, root, "[realm]\nrepos = [\""+entry+"\"]\n")
			members, warns := discover(t, root)
			assertMembers(t, members, "agency/a")
			if len(warns) != 0 {
				t.Errorf("unexpected warnings: %v", warns)
			}
		})
	}
}

// A path through a symlink never becomes a root, so a link cannot pull an
// outside directory into the realm.
func TestDiscover_SymlinkMatchWarns(t *testing.T) {
	for _, tc := range []struct {
		entry       string
		phrase      string
		wantMembers []string
	}{
		{"*", "matches linked, which is a symbolic link", []string{"agency/a"}},
		{"linked/elsewhere", "matches linked/elsewhere, which passes through the symbolic link linked", nil},
		{"*/*", "", []string{"agency/a"}},
	} {
		t.Run(tc.entry, func(t *testing.T) {
			root := discoverFixture(t, "agency/a")
			outside := discoverFixture(t, "elsewhere/x")
			if err := os.Symlink(outside, filepath.Join(root, "linked")); err != nil {
				t.Fatal(err)
			}
			writeRealmTOML(t, root, "[realm]\nrepos = [\""+tc.entry+"\"]\n")
			members, warns := discover(t, root)
			assertMembers(t, members, tc.wantMembers...)
			// "*/*" also matches .claude/atomic.toml, which warns as a file.
			linkWarns := symlinkWarnings(warns)
			// A glob does not descend into a link, so "*/*" never reaches linked/elsewhere.
			if tc.phrase == "" {
				if len(linkWarns) != 0 {
					t.Errorf("unexpected symlink warnings: %v", linkWarns)
				}
				return
			}
			assertOneWarning(t, linkWarns, root, `"`+tc.entry+`"`, tc.phrase)
			if !strings.Contains(linkWarns[0], "declare the real path inside the realm root") {
				t.Errorf("warning %q does not say how to fix it", linkWarns[0])
			}
		})
	}
}

func TestDiscover_ManySymlinkHitsWarnOnce(t *testing.T) {
	root := discoverFixture(t, "agency/a")
	outside := discoverFixture(t, "x")
	for _, l := range []string{"link1", "link2"} {
		if err := os.Symlink(outside, filepath.Join(root, l)); err != nil {
			t.Fatal(err)
		}
	}
	writeRealmTOML(t, root, "[realm]\nrepos = [\"*\"]\n")
	members, warns := discover(t, root)
	assertMembers(t, members, "agency/a")
	assertOneWarning(t, warns, root, `"*"`, "matches 2 paths through symbolic links (first: link1);")
}

func TestDiscover_ManyHitsThroughOneSymlinkWarnOnce(t *testing.T) {
	root := discoverFixture(t, "agency/a")
	outside := discoverFixture(t, "x", "y")
	if err := os.Symlink(outside, filepath.Join(root, "linked")); err != nil {
		t.Fatal(err)
	}
	writeRealmTOML(t, root, "[realm]\nrepos = [\"agency\", \"linked/*\"]\n")
	members, warns := discover(t, root)
	assertMembers(t, members, "agency/a")
	assertOneWarning(t, warns, root, `"linked/*"`, "matches 2 paths through symbolic links (first: linked/x, through linked);")
}

// A link back up the tree must not make a "**" glob expand through it and
// count the realm again under b1/up/.
func TestDiscover_SymlinkLoopTerminates(t *testing.T) {
	for _, tc := range []struct {
		entry  string
		phrase string
	}{
		// With a trailing "/" the glob matches directories only, and a link is not one.
		{"**/", ""},
		{"**", "matches b1/up, which is a symbolic link"},
	} {
		t.Run(tc.entry, func(t *testing.T) {
			root := discoverFixture(t, "b1/repo", "b2")
			if err := os.Symlink("..", filepath.Join(root, "b1", "up")); err != nil {
				t.Fatal(err)
			}
			writeRealmTOML(t, root, "[realm]\nrepos = [\""+tc.entry+"\"]\n")
			members, warns := discover(t, root)
			assertMembers(t, members, "b1/repo", "b2")
			if tc.phrase == "" {
				if len(warns) != 0 {
					t.Errorf("unexpected warnings: %v", warns)
				}
				return
			}
			assertOneWarning(t, symlinkWarnings(warns), root, `"`+tc.entry+`"`, tc.phrase)
		})
	}
}

func TestDiscover_UnreadableHitsWarnOnce(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("chmod does not block root")
	}
	root := discoverFixture(t, "agency/a", "locked/x", "locked/y")
	locked := filepath.Join(root, "locked")
	if err := os.Chmod(locked, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(locked, 0o755) })
	writeRealmTOML(t, root, "[realm]\nrepos = [\"agency\", \"locked/*\"]\n")
	members, warns := discover(t, root)
	assertMembers(t, members, "agency/a")
	assertOneWarning(t, warns, root, `"locked/*"`, "matches 2 paths that could not be read (first: locked/x:")
}

// A root reached through a symlink is still the realm the user named.
func TestDiscover_DotEntryUnderSymlinkedRootWalksWholeRoot(t *testing.T) {
	real := discoverFixture(t, "agency/a", "scratch/b")
	root := filepath.Join(t.TempDir(), "realm")
	if err := os.Symlink(real, root); err != nil {
		t.Fatal(err)
	}
	writeRealmTOML(t, root, "[realm]\nrepos = [\".\"]\n")
	members, warns := discover(t, root)
	assertMembers(t, members, "agency/a", "scratch/b")
	if len(warns) != 0 {
		t.Errorf("unexpected warnings: %v", warns)
	}
}

func TestDiscover_ManyFileMatchesWarnOnce(t *testing.T) {
	root := discoverFixture(t, "agency/a")
	for _, f := range []string{"CLAUDE.md", "notes.md", "agency/readme.md"} {
		if err := os.WriteFile(filepath.Join(root, f), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeRealmTOML(t, root, "[realm]\nrepos = [\"*\", \"agency/*\"]\n")
	members, warns := discover(t, root)
	assertMembers(t, members, "agency/a")
	if len(warns) != 2 {
		t.Fatalf("warnings = %v, want one per entry", warns)
	}
	for _, want := range []string{`"*" matches 2 files, not directories (first: CLAUDE.md); end the pattern with "/"`, `"agency/*" matches agency/readme.md, which is not a directory`} {
		if !slices.ContainsFunc(warns, func(w string) bool { return strings.Contains(w, want) }) {
			t.Errorf("warnings = %v, want one containing %q", warns, want)
		}
	}
}

// "" cleans to "." and would silently widen discovery to the whole root.
func TestDiscover_EmptyEntryWarns(t *testing.T) {
	root := discoverFixture(t, "agency/a", "scratch/b")
	writeRealmTOML(t, root, "[realm]\nrepos = [\"agency\", \"\"]\n")
	members, warns := discover(t, root)
	assertMembers(t, members, "agency/a")
	assertOneWarning(t, warns, root, `""`, "is empty")
}

// A misspelled [realm] key would otherwise fall back to the whole root with no
// sign the setting was ignored.
func TestDiscover_UnknownRealmKeyWarns(t *testing.T) {
	root := discoverFixture(t, "agency/a", "scratch/b")
	writeRealmTOML(t, root, "[realm]\nrepo = [\"agency\"]\n\n[scan]\nunrelated = 1\n")
	members, warns := discover(t, root)
	assertMembers(t, members, "agency/a", "scratch/b")
	if len(warns) != 1 || !strings.Contains(warns[0], `"realm.repo"`) || !strings.Contains(warns[0], config.RepoConfigPath(root)) {
		t.Errorf("warnings = %v, want one naming realm.repo and %s", warns, config.RepoConfigPath(root))
	}
}
