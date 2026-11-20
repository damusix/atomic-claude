package realmshare

import (
	"crypto/rand"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"syscall"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/config"
)

func TestShareCutoverPreservesUnownedReplacement(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("post-receive fixture requires a POSIX shell")
	}
	for _, kind := range []string{"same-target symlink", "regular file"} {
		t.Run(kind, func(t *testing.T) {
			f := newShareFixture(t)
			requireShareSymlinks(t)
			const name, reviewed, replacement = "guide.md", "# Reviewed guide\n", "new local work\n"
			f.write(t, name, reviewed)
			original := filepath.Join(f.realm, name)
			stash := filepath.Join(f.realm, "stashed.md")
			hook := f.hook(t, "post-receive", "mv "+quoteShareShell(original)+" "+quoteShareShell(stash)+"\n")
			opts := ShareOptions{Remote: f.remote, Paths: []string{name}}
			if err := f.share(t, opts); err == nil || !strings.Contains(err.Error(), "is published") {
				t.Fatalf("expected interrupted local cutover after publication, got %v", err)
			}
			if err := os.Remove(hook); err != nil {
				t.Fatal(err)
			}
			binding := f.binding(t)
			pending := binding.PendingShare
			if pending == nil || !pending.Published || len(pending.Selections) != 1 {
				t.Fatalf("expected one pending publication selection: %+v", pending)
			}
			selected := pending.Selections[0]
			backup := selected.Backup
			if !filepath.IsAbs(backup) {
				backup = filepath.Join(f.state, backup)
			}
			if err := os.MkdirAll(filepath.Dir(backup), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(stash, backup); err != nil {
				t.Fatal(err)
			}
			target := filepath.Join(binding.ClonePath, filepath.FromSlash(selected.Mapping.Source))
			if kind == "same-target symlink" {
				symlinkRealmshare(t, target, original)
			} else {
				f.write(t, name, replacement)
			}
			before, err := os.Lstat(original)
			if err != nil {
				t.Fatal(err)
			}

			retryErr := f.share(t, opts)
			if after, err := os.Lstat(original); err != nil || !os.SameFile(before, after) {
				t.Errorf("retry removed or replaced the user's entry: %v", err)
			}
			if kind == "same-target symlink" {
				if got, err := os.Readlink(original); err != nil || got != target {
					t.Errorf("user symlink changed: target %q, err %v", got, err)
				}
			} else {
				f.assertOriginal(t, name, "", replacement)
			}
			if got := readRealmshareFile(t, backup); got != reviewed {
				t.Errorf("retained original changed: %q", got)
			}
			after := f.binding(t)
			if after.PendingShare == nil || !after.PendingShare.Published || after.PendingShare.Commit != pending.Commit ||
				len(after.Links) != 0 || len(after.Restorations) != 0 {
				t.Errorf("retry completed ownership receipts over the user's entry: %+v", after)
			}
			if retryErr == nil {
				t.Fatal("retry accepted an occupied destination not owned by Atomic")
			}

			if err := os.Rename(original, filepath.Join(f.realm, "replacement-kept")); err != nil {
				t.Fatal(err)
			}
			f.apply(t, f.preview(t, opts))
			binding = f.binding(t)
			if binding.PendingShare != nil {
				t.Errorf("finished cutover still has a pending receipt: %+v", binding.PendingShare)
			}
			f.assertLink(t, binding, name, reviewed)
			if got := readRealmshareFile(t, f.restorationBackup(t, binding, name)); got != reviewed {
				t.Errorf("finished cutover lost the original: %q", got)
			}
			if got := f.git(t, f.remote, "rev-parse", "refs/heads/"+binding.Branch); got != pending.Commit {
				t.Errorf("retry republished the snapshot: %s -> %s", pending.Commit, got)
			}
		})
	}
}

func TestShareRetryPreservesUnownedParkedReplacement(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("post-receive fixture requires a POSIX shell")
	}
	f := newShareFixture(t)
	requireShareSymlinks(t)
	const name, reviewed, replacement = "guide.md", "# Reviewed guide\n", "new local work\n"
	f.write(t, name, reviewed)
	original := filepath.Join(f.realm, name)
	stash := filepath.Join(f.realm, "stashed.md")
	hook := f.hook(t, "post-receive", "mv "+quoteShareShell(original)+" "+quoteShareShell(stash)+"\n")
	opts := ShareOptions{Remote: f.remote, Paths: []string{name}}
	if err := f.share(t, opts); err == nil || !strings.Contains(err.Error(), "is published") {
		t.Fatalf("expected interrupted local cutover after publication, got %v", err)
	}
	if err := os.Remove(hook); err != nil {
		t.Fatal(err)
	}

	binding := f.binding(t)
	pending := binding.PendingShare
	if pending == nil || !pending.Published || len(pending.Selections) != 1 {
		t.Fatalf("expected one pending publication selection: %+v", pending)
	}
	selected := &pending.Selections[0]
	backup := filepath.Join(f.state, selected.Backup)
	if err := os.MkdirAll(filepath.Dir(backup), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(stash, backup); err != nil {
		t.Fatal(err)
	}
	symlinkRealmshare(t, filepath.Join(binding.ClonePath, filepath.FromSlash(selected.Mapping.Source)), backup+".link")
	selected.Parking = ".rollback-" + rand.Text()
	if err := SaveBinding(f.realm, binding); err != nil {
		t.Fatal(err)
	}
	parked := filepath.Join(filepath.Dir(backup), selected.Parking)
	writeRealmshareFile(t, parked, replacement)
	before, err := os.Lstat(parked)
	if err != nil {
		t.Fatal(err)
	}

	retryErr := f.share(t, opts)
	if retryErr == nil {
		t.Error("retry accepted an unresolved parked replacement")
	} else if message := retryErr.Error(); !strings.Contains(message, name) || !strings.Contains(message, selected.Parking) {
		t.Errorf("retry did not identify both the original and parked paths: %v", retryErr)
	}
	if _, err := os.Lstat(original); !os.IsNotExist(err) {
		t.Errorf("retry modified the missing destination: %v", err)
	}
	if after, err := os.Lstat(parked); err != nil || !os.SameFile(before, after) {
		t.Errorf("retry removed or replaced the parked entry: %v", err)
	} else if got := readRealmshareFile(t, parked); got != replacement {
		t.Errorf("parked content = %q, want %q", got, replacement)
	}
	if got := readRealmshareFile(t, backup); got != reviewed {
		t.Errorf("retained original content = %q, want %q", got, reviewed)
	}
	after := f.binding(t)
	if after.PendingShare == nil || !after.PendingShare.Published || after.PendingShare.Commit != pending.Commit ||
		len(after.PendingShare.Selections) != 1 || after.PendingShare.Selections[0].Parking != selected.Parking ||
		len(after.Links) != 0 || len(after.Restorations) != 0 {
		t.Errorf("retry lost the parking receipt or recorded ownership: %+v", after)
	}
}

func TestShareWikiCaseAliasKeepsPrivateRegistryLocal(t *testing.T) {
	for _, fixture := range []struct{ selection, index string }{
		{"Wiki", "wiki/index.md"},
		{"wiki", "wiki/Index.md"},
	} {
		t.Run(fixture.selection+"-"+filepath.Base(fixture.index), func(t *testing.T) {
			f := newShareFixture(t)
			requireShareSymlinks(t)
			const public = "# Public wiki"
			private := filepath.Join(f.realm, "private-notes")
			content := public + "\n\n<wiki-buckets>\n<bucket name=\"private-notes\" path=\"" + private + "\"/>\n</wiki-buckets>\n"
			f.write(t, fixture.index, content)
			actual, err := os.Stat(filepath.Join(f.realm, filepath.FromSlash(fixture.index)))
			if err != nil {
				t.Fatal(err)
			}
			lookup := filepath.Join(f.realm, fixture.selection, "index.md")
			alias, err := os.Stat(lookup)
			if errors.Is(err, os.ErrNotExist) {
				t.Skip("fixture filesystem does not resolve case aliases")
			}
			if err != nil || !os.SameFile(actual, alias) {
				t.Fatalf("fixture case alias does not refer to its wiki index: %v", err)
			}

			if err := f.share(t, ShareOptions{Remote: f.remote, Paths: []string{fixture.selection}}); err != nil {
				if refs := f.git(t, f.remote, "for-each-ref"); refs != "" {
					t.Fatalf("case alias rejected only after publication: %v; refs: %s", err, refs)
				}
				f.assertOriginal(t, "wiki", filepath.Base(fixture.index), content)
				return
			}
			binding := f.binding(t)
			receipt := slices.IndexFunc(binding.Restorations, func(receipt RestorationReceipt) bool {
				return strings.EqualFold(receipt.Path, "wiki")
			})
			if receipt < 0 {
				t.Fatal("local wiki is missing its restoration receipt")
			}
			backup := f.restorationBackup(t, binding, binding.Restorations[receipt].Path)
			if got := readRealmshareFile(t, filepath.Join(backup, filepath.Base(fixture.index))); got != content {
				t.Errorf("local wiki backup lost the original registry: %q", got)
			}
			ref := "refs/heads/" + binding.Branch
			found := false
			for _, path := range strings.Split(f.git(t, f.remote, "ls-tree", "-r", "--name-only", ref), "\n") {
				got := f.git(t, f.remote, "show", ref+":"+path)
				if strings.Contains(got, "private-notes") || strings.Contains(got, "<wiki-buckets") {
					t.Errorf("published %s contains private wiki registry data", path)
				}
				if strings.EqualFold(path, "wiki/index.md") {
					found = strings.Contains(got, public)
				}
			}
			if !found {
				t.Error("public wiki index content missing from the approved publication")
			}
		})
	}
}

func TestShareCrossDeviceBackupSupportedOrRejectedBeforePublish(t *testing.T) {
	f := newShareFixture(t)
	requireShareSymlinks(t)
	state, err := os.MkdirTemp("/dev/shm", "atomic-realm-share-")
	if err != nil {
		t.Skipf("no writable alternate filesystem at /dev/shm: %v", err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(state); err != nil {
			t.Error(err)
		}
	})
	probe := filepath.Join(f.realm, "rename-probe")
	if err := os.Mkdir(probe, 0o700); err != nil {
		t.Fatal(err)
	}
	err = os.Rename(probe, filepath.Join(state, "rename-probe"))
	if err == nil {
		t.Skip("/dev/shm shares the realm filesystem")
	}
	if !errors.Is(err, syscall.EXDEV) {
		t.Fatalf("could not establish a cross-device layout: %v", err)
	}
	if err := os.Remove(probe); err != nil {
		t.Fatal(err)
	}

	f.state = state
	t.Setenv(config.StateDirEnvVar, state)
	const name, content = "guide.md", "# Guide on the realm filesystem\n"
	f.write(t, name, content)
	if err := f.share(t, ShareOptions{Remote: f.remote, Paths: []string{name}}); err != nil {
		if refs := f.git(t, f.remote, "for-each-ref"); refs != "" {
			t.Errorf("incompatible backup layout failed after publication: %v; refs: %s", err, refs)
		}
		message := strings.ToLower(err.Error())
		if !strings.Contains(message, "device") && !strings.Contains(message, "filesystem") {
			t.Errorf("incompatibility must identify the filesystem boundary: %v", err)
		}
		f.assertOriginal(t, name, "", content)
		return
	}
	binding := f.binding(t)
	if binding.PendingShare != nil {
		t.Errorf("successful cross-device cutover still has a pending receipt: %+v", binding.PendingShare)
	}
	link := f.assertLink(t, binding, name, content)
	if got := readRealmshareFile(t, f.restorationBackup(t, binding, name)); got != content {
		t.Errorf("cross-device backup lost the original: %q", got)
	}
	if got := f.git(t, f.remote, "show", "refs/heads/"+binding.Branch+":"+link.Source); got != strings.TrimSpace(content) {
		t.Errorf("published content = %q", got)
	}
}

func TestShareSelectedMountPointRejectedBeforePublish(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("bind mount fixture requires Linux")
	}
	const (
		fixtureEnv = "ATOMIC_TEST_SHARE_MOUNT_REALM"
		name       = "selected"
		child      = "guide.md"
		content    = "# Guide on the bind mount\n"
	)
	if realm := os.Getenv(fixtureEnv); realm != "" {
		source, err := os.Stat(filepath.Join(realm, "source"))
		if err != nil {
			t.Fatal(err)
		}
		selected, err := os.Stat(filepath.Join(realm, name))
		if err != nil {
			t.Fatal(err)
		}
		if !os.SameFile(source, selected) {
			t.Fatal("selected directory is not the bind mount of the fixture source")
		}
		f := newShareFixture(t)
		f.realm, f.state = realm, filepath.Join(realm, ".atomic")
		err = f.share(t, ShareOptions{Remote: f.remote, Paths: []string{name}})
		if refs := f.git(t, f.remote, "for-each-ref"); refs != "" {
			t.Errorf("selected mount point changed remote refs: %s", refs)
		}
		if err == nil {
			t.Error("share accepted a selected mount point")
		} else if message := strings.ToLower(err.Error()); !strings.Contains(message, "mount point") && !strings.Contains(message, "mountpoint") {
			t.Errorf("share did not identify the selected mount point: %v", err)
		}
		f.assertOriginal(t, name, child, content)
		return
	}

	for _, tool := range []string{"git", "unshare", "mount"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("bind mount fixture requires %s: %v", tool, err)
		}
	}
	realm := t.TempDir()
	source, target := filepath.Join(realm, "source"), filepath.Join(realm, name)
	writeRealmshareFile(t, filepath.Join(source, child), content)
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	// A same-filesystem bind mount defeats a parent-device check and disappears with the private namespace.
	cmd := exec.CommandContext(t.Context(), "unshare", "--mount", "--propagation", "private", "--", "/bin/sh", "-c", `
if output=$(mount --bind "$1" "$2" 2>&1); then
	exec "$3" "-test.run=^TestShareSelectedMountPointRejectedBeforePublish$"
fi
printf '%s\n' "$output" >&2
case "$output" in
	*"permission denied"*|*"Operation not permitted"*|*"must be superuser"*) exit 77 ;;
	*) exit 1 ;;
esac
`, "share-mount-fixture", source, target, binary)
	cmd.Env = append(os.Environ(), fixtureEnv+"="+realm, "LC_ALL=C")
	out, err := cmd.CombinedOutput()
	if err == nil {
		return
	}
	var exit *exec.ExitError
	if (errors.As(err, &exit) && exit.ExitCode() == 77) ||
		strings.HasPrefix(string(out), "unshare: unshare failed: Operation not permitted") ||
		strings.HasPrefix(string(out), "unshare: mount failed: Operation not permitted") {
		t.Skipf("sandbox does not permit a private bind mount: %s", out)
	}
	t.Fatalf("private bind mount fixture: %v\n%s", err, out)
}
