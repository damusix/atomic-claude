package realmshare

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateSyncHistoryChecksUnpublishedAncestry(t *testing.T) {
	f := newShareFixture(t)
	repo := t.TempDir()
	f.git(t, repo, "init", "--quiet", "--initial-branch=team")
	definition := "version = 1\nid = \"" + fixtureRealmID + "\"\n" +
		manifestMapping("guide.md", "guide.md", string(KindFile))
	manifestPath := filepath.Join(repo, manifestName)
	writeRealmshareFile(t, manifestPath, definition)
	writeRealmshareFile(t, filepath.Join(repo, "guide.md"), "# Fixture guide\n")
	base := syncFixtureCommit(t, f, repo, "Published fixture", manifestName, "guide.md")
	const otherRealm = "other-fixture-realm"
	writeRealmshareFile(t, manifestPath, strings.Replace(definition, fixtureRealmID, otherRealm, 1))
	wrongIdentity := syncFixtureCommit(t, f, repo, "Change fixture identity", manifestName)
	writeRealmshareFile(t, manifestPath, definition)
	repaired := syncFixtureCommit(t, f, repo, "Restore fixture identity", manifestName)

	tree := f.git(t, repo, "rev-parse", base+"^{tree}")
	validParent := f.git(t, repo, "commit-tree", tree, "--no-gpg-sign", "-p", base, "-m", "Valid first parent")
	merged := f.git(t, repo, "commit-tree", tree, "--no-gpg-sign", "-p", validParent, "-p", wrongIdentity, "-m", "Valid merge snapshot")
	afterPublished := f.git(t, repo, "commit-tree", tree, "--no-gpg-sign", "-p", repaired, "-m", "Commit after the published boundary")

	git := localGit(repo)
	for _, tc := range []struct {
		name      string
		head      string
		published string
		wantError bool
	}{
		{name: "linear ancestor", head: repaired, published: base, wantError: true},
		{name: "second parent", head: merged, published: base, wantError: true},
		{name: "known published ancestry", head: afterPublished, published: repaired},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := validateSyncHistory(t.Context(), git, tc.head, tc.published, fixtureRealmID, ".atomic")
			if tc.wantError {
				if err == nil || !strings.Contains(err.Error(), "realm identity") || !strings.Contains(err.Error(), otherRealm) {
					t.Fatalf("history validation = %v, want refusal of the synthetic identity %q", err, otherRealm)
				}
			} else if err != nil {
				t.Fatalf("history validation revisited published ancestry: %v", err)
			}
		})
	}
}
