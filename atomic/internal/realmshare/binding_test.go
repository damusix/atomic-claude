package realmshare

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/pelletier/go-toml/v2"
)

func TestBindingPersistsLocalStateAtResolvedRoot(t *testing.T) {
	for _, stateDir := range []string{".claude/state", "absolute override"} {
		t.Run(stateDir, func(t *testing.T) {
			home, realm, base := bindingFixture(t)
			prepareBindingFixture(t, base)
			writeRealmshareFile(t, config.TOMLPath(home), "[state]\ndir = '.claude/state'\n")
			t.Setenv(config.StateDirEnvVar, "")
			stateRoot := filepath.Join(realm, ".claude", "state")
			if stateDir == "absolute override" {
				stateRoot = t.TempDir()
				t.Setenv(config.StateDirEnvVar, stateRoot)
			}
			privateBucket := filepath.Join(realm, "private-notes")
			backup := filepath.Join(stateRoot, "backups", "wiki")
			writeRealmshareFile(t, filepath.Join(privateBucket, "index.md"), "# Private notes\n")
			writeRealmshareFile(t, filepath.Join(backup, "index.md"), "# Original wiki\n")
			definition := fmt.Sprintf(`realm_id = %q
clone_path = %q
remote = %q
branch = %q
trusted_digest = 'sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef'

[[private_buckets]]
name = 'private-notes'
path = %q

[pending_share]
paths = ['research']
commit = '2d77b159c1b132bd472eeb92731c3a6a6992a227'

[[loaders]]
path = 'AGENTS.md'
digest = 'sha256:abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789'

[[artifacts]]
logical = '.agents/commands/triage.md'

[artifacts.link]
path = '.claude/commands/triage.md'
source = '.agents/commands/triage.md'
witness = 'realm-share-backups/triage-native.link'

[[links]]
path = 'research'
source = 'research'

[[links]]
path = '.agents/commands/triage.md'
source = '.agents/commands/triage.md'
witness = 'realm-share-backups/triage-logical.link'

[[restorations]]
path = 'wiki'
digest = 'sha256:fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210'
backup = %q

[sync]
last_success = 2026-11-18T12:00:00Z
ahead = 1
behind = 2
`, base.RealmID, base.ClonePath, base.Remote, base.Branch, privateBucket, backup)
			var binding Binding
			if err := toml.Unmarshal([]byte(definition), &binding); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(stateRoot, "realm-share.toml")
			if err := SaveBinding(realm, binding); err != nil {
				t.Fatal(err)
			}
			assertBindingRecords(t, path, definition)
			loaded, err := LoadBinding(realm)
			if err != nil {
				t.Fatal(err)
			}
			if loaded.RealmID != base.RealmID || loaded.ClonePath != base.ClonePath || loaded.Remote != base.Remote || loaded.Branch != base.Branch {
				t.Fatalf("binding = %+v, want realm %s, clone %s, remote %s, branch %s", loaded, base.RealmID, base.ClonePath, base.Remote, base.Branch)
			}
			if err := SaveBinding(realm, loaded); err != nil {
				t.Fatal(err)
			}
			assertBindingRecords(t, path, definition)
			assertRealmshareAbsent(t, filepath.Join(realm, ".atomic", "realm-share.toml"))
			assertRealmshareAbsent(t, filepath.Join(base.ClonePath, "realm-share.toml"))
		})
	}
}

func TestBindingOmitsUnsetSyncTimestamp(t *testing.T) {
	_, realm, binding := bindingFixture(t)
	prepareBindingFixture(t, binding)
	binding.Sync = &SyncState{Ahead: 1, Behind: 2}
	if err := SaveBinding(realm, binding); err != nil {
		t.Fatal(err)
	}
	assertBindingRecords(t, filepath.Join(realm, ".atomic", "realm-share.toml"), "[sync]\nahead = 1\nbehind = 2\n")
}

func TestSaveBindingRejectsRealmAndCloneReassignment(t *testing.T) {
	for _, name := range []string{"second clone for same realm", "second realm for same clone"} {
		t.Run(name, func(t *testing.T) {
			home, realm, binding := bindingFixture(t)
			prepareBindingFixture(t, binding)
			if err := SaveBinding(realm, binding); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(realm, ".atomic", "realm-share.toml")
			before := readRealmshareFile(t, path)
			colleague := t.TempDir()
			another := binding
			another.ClonePath = filepath.Join(config.Dir(home), "realms", "another-team")
			prepareBindingFixture(t, another)
			candidateRealm, candidateBinding := realm, another
			if name == "second realm for same clone" {
				candidateRealm, candidateBinding = colleague, binding
			}
			if err := SaveBinding(candidateRealm, candidateBinding); err == nil {
				t.Fatal("reassigned a bound realm or clone")
			}
			if got := readRealmshareFile(t, path); got != before {
				t.Errorf("rejected reassignment changed %s", path)
			}
			assertRealmshareAbsent(t, filepath.Join(colleague, ".atomic", "realm-share.toml"))
			if err := SaveBinding(colleague, another); err != nil {
				t.Fatalf("rejected save claimed the separate clone or realm: %v", err)
			}
		})
	}
}

func TestSaveBindingRejectsCloneNormalizationAlias(t *testing.T) {
	home, realm, binding := bindingFixture(t)
	clones := filepath.Join(config.Dir(home), "realms")
	binding.ClonePath = filepath.Join(clones, "caf\u00e9")
	prepareBindingFixture(t, binding)

	candidate := binding
	candidate.ClonePath = filepath.Join(clones, "cafe\u0301")
	originalInfo, err := os.Stat(binding.ClonePath)
	if err != nil {
		t.Fatal(err)
	}
	aliasInfo, err := os.Stat(candidate.ClonePath)
	if errors.Is(err, os.ErrNotExist) {
		t.Skip("filesystem does not resolve Unicode normalization aliases")
	}
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(originalInfo, aliasInfo) {
		t.Fatal("alias resolved to a different checkout")
	}

	colleague := t.TempDir()
	ownerLocation, err := locateBinding(realm)
	if err != nil {
		t.Fatal(err)
	}
	candidateLocation, err := locateBinding(colleague)
	if err != nil {
		t.Fatal(err)
	}
	ownerClaim, err := ownerLocation.validate(binding)
	if err != nil {
		t.Fatal(err)
	}
	candidateClaim, err := candidateLocation.validate(candidate)
	if err != nil {
		t.Fatalf("alias must pass checkout verification: %v", err)
	}
	if ownerClaim.Realm == candidateClaim.Realm || ownerClaim.CloneID != candidateClaim.CloneID {
		t.Fatal("expected distinct realms claiming the same verified checkout")
	}

	if err := SaveBinding(realm, binding); err != nil {
		t.Fatal(err)
	}
	if err := SaveBinding(colleague, candidate); err == nil || !strings.Contains(err.Error(), "already has a different owner") {
		t.Fatalf("expected an ownership conflict for the clone alias, got %v", err)
	}
	assertRealmshareAbsent(t, filepath.Join(colleague, ".atomic", bindingName))
}

func TestSaveBindingRejectsUnrelatedClone(t *testing.T) {
	_, realm, binding := bindingFixture(t)
	if err := os.MkdirAll(binding.ClonePath, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"init", "--quiet", binding.ClonePath},
		{"-C", binding.ClonePath, "config", "--local", "remote.origin.url", "https://example.com/unrelated.git"},
	} {
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	configPath := filepath.Join(binding.ClonePath, ".git", "config")
	before := readRealmshareFile(t, configPath)
	localEdit := filepath.Join(binding.ClonePath, "unpublished.md")
	writeRealmshareFile(t, localEdit, "unrelated developer work")
	if err := SaveBinding(realm, binding); err == nil {
		t.Fatal("adopted an unrelated Git repository at the requested clone name")
	}
	if got := readRealmshareFile(t, configPath); got != before {
		t.Error("rejected binding changed unrelated Git configuration")
	}
	if got := readRealmshareFile(t, localEdit); got != "unrelated developer work" {
		t.Errorf("rejected binding changed unrelated work: %q", got)
	}
	assertRealmshareAbsent(t, filepath.Join(realm, ".atomic", "realm-share.toml"))
}

func TestSaveBindingRejectsUnmanagedMatchingClone(t *testing.T) {
	_, realm, binding := bindingFixture(t)
	initBindingClone(t, binding.ClonePath, binding.Remote, portableManifest)
	if err := SaveBinding(realm, binding); err == nil {
		t.Fatal("adopted an unmanaged repository with the same Git origin and manifest identity")
	}
	if err := PrepareManagedClone(binding.ClonePath, func(string) error {
		t.Fatal("prepared over an existing checkout")
		return nil
	}); err == nil {
		t.Fatal("prepared an existing checkout")
	}
	assertRealmshareAbsent(t, filepath.Join(realm, ".atomic", "realm-share.toml"))
}

func TestBindingRecoversInterruptedCloneWitness(t *testing.T) {
	home, realm, binding := bindingFixture(t)
	const retained = "work in the prepared checkout"
	var staging string
	if err := PrepareManagedClone(binding.ClonePath, func(dir string) error {
		staging = dir
		initBindingClone(t, dir, binding.Remote, portableManifest)
		writeRealmshareFile(t, filepath.Join(dir, "retained.md"), retained)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if staging == binding.ClonePath {
		t.Fatal("preparation did not use a separate staging path")
	}
	if err := os.Rename(binding.ClonePath, staging); err != nil {
		t.Fatal(err)
	}
	witness := filepath.Join(config.Dir(home), cloneProofDir, filepath.Base(binding.ClonePath))
	witnessInfo, err := os.Lstat(witness)
	if err != nil {
		t.Fatal(err)
	}
	markerInfo, err := os.Lstat(filepath.Join(staging, cloneProof))
	if err != nil || !os.SameFile(witnessInfo, markerInfo) {
		t.Fatalf("interrupted witness does not identify the staged checkout: %v", err)
	}

	repopulate := func(string) error {
		t.Fatal("retry invoked population instead of recovering the prepared checkout")
		return nil
	}
	initBindingClone(t, binding.ClonePath, binding.Remote, portableManifest)
	const unrelated = "work in the colliding checkout"
	writeRealmshareFile(t, filepath.Join(binding.ClonePath, "retained.md"), unrelated)
	if err := PrepareManagedClone(binding.ClonePath, repopulate); err == nil {
		t.Fatal("retry adopted an unrelated checkout matching the staged Git origin and realm identity")
	}
	if got := readRealmshareFile(t, filepath.Join(binding.ClonePath, "retained.md")); got != unrelated {
		t.Errorf("colliding checkout changed: %q", got)
	}
	if got := readRealmshareFile(t, filepath.Join(staging, "retained.md")); got != retained {
		t.Errorf("staged checkout changed: %q", got)
	}
	if err := os.Rename(binding.ClonePath, filepath.Join(t.TempDir(), "unrelated")); err != nil {
		t.Fatal(err)
	}

	if err := PrepareManagedClone(binding.ClonePath, repopulate); err != nil {
		t.Fatalf("retry did not recover the staged checkout after witness publication: %v", err)
	}
	if got := readRealmshareFile(t, filepath.Join(binding.ClonePath, "retained.md")); got != retained {
		t.Errorf("recovered checkout lost staged work: %q", got)
	}
	if err := SaveBinding(realm, binding); err != nil {
		t.Fatalf("recovered checkout cannot be bound: %v", err)
	}
}

func TestSaveBindingRejectsCloneWithDifferentIdentity(t *testing.T) {
	_, realm, binding := bindingFixture(t)
	prepareBindingFixture(t, binding)
	definition := filepath.Join(binding.ClonePath, "realm.toml")
	writeRealmshareFile(t, definition, "version = 1\nid = 'another-realm'\n")
	if err := SaveBinding(realm, binding); err == nil {
		t.Fatal("accepted a managed checkout with a different realm identity")
	}
	assertRealmshareAbsent(t, filepath.Join(realm, ".atomic", "realm-share.toml"))
}

func TestLoadBindingRejectsCopiedBinding(t *testing.T) {
	_, realm, binding := bindingFixture(t)
	prepareBindingFixture(t, binding)
	if err := SaveBinding(realm, binding); err != nil {
		t.Fatal(err)
	}
	other := t.TempDir()
	copy := readRealmshareFile(t, filepath.Join(realm, ".atomic", "realm-share.toml"))
	writeRealmshareFile(t, filepath.Join(other, ".atomic", "realm-share.toml"), copy)
	if _, err := LoadBinding(other); err == nil {
		t.Fatal("accepted a copied binding in another local realm")
	}
}

func TestLoadBindingRejectsCopiedCheckoutProof(t *testing.T) {
	_, realm, binding := bindingFixture(t)
	prepareBindingFixture(t, binding)
	if err := SaveBinding(realm, binding); err != nil {
		t.Fatal(err)
	}
	proof := readRealmshareFile(t, filepath.Join(binding.ClonePath, filepath.FromSlash(cloneProof)))
	if err := os.Rename(binding.ClonePath, binding.ClonePath+"-old"); err != nil {
		t.Fatal(err)
	}
	initBindingClone(t, binding.ClonePath, binding.Remote, portableManifest)
	writeRealmshareFile(t, filepath.Join(binding.ClonePath, filepath.FromSlash(cloneProof)), proof)
	if _, err := LoadBinding(realm); err == nil {
		t.Fatal("accepted a replacement checkout with a copy of stale ownership proof")
	}
}

func TestSaveBindingRejectsReassignmentAfterStateRootChanges(t *testing.T) {
	_, realm, binding := bindingFixture(t)
	prepareBindingFixture(t, binding)
	if err := SaveBinding(realm, binding); err != nil {
		t.Fatal(err)
	}
	t.Setenv(config.StateDirEnvVar, ".alternate")
	binding.ClonePath += "-other"
	prepareBindingFixture(t, binding)
	if err := SaveBinding(realm, binding); err == nil {
		t.Fatal("reassigned a bound realm through a different state root")
	}
	assertRealmshareAbsent(t, filepath.Join(realm, ".alternate", "realm-share.toml"))
}

func TestSaveBindingRejectsStateInsideManagedClone(t *testing.T) {
	_, realm, binding := bindingFixture(t)
	t.Setenv(config.StateDirEnvVar, filepath.Join(binding.ClonePath, "state"))
	if err := SaveBinding(realm, binding); err == nil {
		t.Fatal("stored private state inside the shared clone")
	}
	assertRealmshareAbsent(t, binding.ClonePath)
}

func TestSaveBindingRejectsSymlinkedStateParent(t *testing.T) {
	_, realm, binding := bindingFixture(t)
	prepareBindingFixture(t, binding)
	outside := t.TempDir()
	symlinkRealmshare(t, outside, filepath.Join(realm, ".atomic"))
	if err := SaveBinding(realm, binding); err == nil {
		t.Fatal("wrote state through a symlinked state directory")
	}
	assertRealmshareAbsent(t, filepath.Join(outside, "realm-share.toml"))
}

func TestSaveBindingRejectsCloneOutsideUserRealms(t *testing.T) {
	for _, name := range []string{"outside", "symlinked parent"} {
		t.Run(name, func(t *testing.T) {
			home, realm, binding := bindingFixture(t)
			outside := t.TempDir()
			if name == "outside" {
				binding.ClonePath = filepath.Join(outside, "team")
			} else {
				realms := filepath.Join(config.Dir(home), "realms")
				if err := os.MkdirAll(filepath.Dir(realms), 0o755); err != nil {
					t.Fatal(err)
				}
				symlinkRealmshare(t, outside, realms)
			}
			if err := SaveBinding(realm, binding); err == nil {
				t.Fatal("allowed clone outside the user state root's realms directory")
			}
			assertRealmshareAbsent(t, filepath.Join(outside, "team"))
			assertRealmshareAbsent(t, filepath.Join(realm, ".atomic", "realm-share.toml"))
		})
	}
}

func TestBindingRejectsSymlinkedLocalState(t *testing.T) {
	_, realm, binding := bindingFixture(t)
	prepareBindingFixture(t, binding)
	if err := SaveBinding(realm, binding); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(realm, ".atomic", "realm-share.toml")
	outside := filepath.Join(t.TempDir(), "realm-share.toml")
	before := readRealmshareFile(t, path)
	if err := os.Rename(path, outside); err != nil {
		t.Fatal(err)
	}
	symlinkRealmshare(t, outside, path)
	if _, err := LoadBinding(realm); err == nil {
		t.Error("loaded local binding through a symlink")
	}
	if err := SaveBinding(realm, binding); err == nil {
		t.Error("saved local binding over a symlink")
	}
	if target, err := os.Readlink(path); err != nil || target != outside {
		t.Errorf("rejected save replaced the symlink: target %q, error %v", target, err)
	}
	if got := readRealmshareFile(t, outside); got != before {
		t.Error("rejected save changed the symlink target")
	}
}

func TestSaveBindingRejectsUnsafeReceipts(t *testing.T) {
	_, realm, binding := bindingFixture(t)
	prepareBindingFixture(t, binding)
	binding.Loaders = []FileReceipt{{Path: ".git/config", Digest: "digest"}}
	if err := SaveBinding(realm, binding); err == nil {
		t.Fatal("accepted Git metadata as an owned loader")
	}
	binding.Loaders = nil
	binding.Restorations = []RestorationReceipt{{Path: "wiki", Digest: "digest", Backup: filepath.Join(t.TempDir(), "wiki")}}
	if err := SaveBinding(realm, binding); err == nil {
		t.Fatal("accepted a backup outside local state")
	}
	assertRealmshareAbsent(t, filepath.Join(realm, ".atomic", "realm-share.toml"))
}

func bindingFixture(t *testing.T) (home, realm string, binding Binding) {
	t.Helper()
	home, realm = t.TempDir(), t.TempDir()
	t.Cleanup(config.SetHomeDirForTest(home))
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv(config.StateDirEnvVar, ".atomic")
	return home, realm, Binding{
		RealmID:   fixtureRealmID,
		ClonePath: filepath.Join(config.Dir(home), "realms", "team"),
		Remote:    "https://example.com/team.git",
		Branch:    "main",
	}
}

func prepareBindingFixture(t *testing.T, binding Binding) {
	t.Helper()
	if err := PrepareManagedClone(binding.ClonePath, func(path string) error {
		initBindingClone(t, path, binding.Remote, portableManifest)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func initBindingClone(t *testing.T, path, remote, definition string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"init", "--quiet", path},
		{"-C", path, "config", "--local", "remote.origin.url", remote},
	} {
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	writeManifestFixture(t, path, definition)
}

func assertBindingRecords(t *testing.T, path, definition string) {
	t.Helper()
	var got, want map[string]any
	if err := toml.Unmarshal([]byte(readRealmshareFile(t, path)), &got); err != nil {
		t.Fatal(err)
	}
	if err := toml.Unmarshal([]byte(definition), &want); err != nil {
		t.Fatal(err)
	}
	for key, expected := range want {
		if !reflect.DeepEqual(got[key], expected) {
			t.Errorf("%s: %s = %#v, want %#v", path, key, got[key], expected)
		}
	}
}

func readRealmshareFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func assertRealmshareAbsent(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("%s: want no entry, got %v", path, err)
	}
}
