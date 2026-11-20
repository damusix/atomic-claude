package realmshare

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

const instructionsWriterPath = ".agents/realm.md"

func TestUpdateSharedInstructionsTransformsOnlyOwnedSource(t *testing.T) {
	f, clone, link, source := joinedInstructionsWriterFixture(t)
	localFiles := map[string]string{
		"AGENTS.md":       "# Local OMP loader\n",
		"CLAUDE.md":       "# Local Claude loader\n",
		"AGENTS.local.md": "# Private OMP instructions\n",
		"CLAUDE.local.md": "# Private Claude instructions\n",
	}
	localBefore := make(map[string]os.FileInfo, len(localFiles))
	for name, content := range localFiles {
		f.write(t, name, content)
		localBefore[name] = linkLstat(t, filepath.Join(f.realm, name))
	}
	const input, output = "# Uncommitted team draft\n", "# Updated team instructions\n"
	writeRealmshareFile(t, source, input)
	if err := os.Chmod(source, 0o640); err != nil {
		t.Fatal(err)
	}
	sourceBefore := linkLstat(t, source)
	linkBefore := linkLstat(t, link)
	targetBefore, err := os.Readlink(link)
	if err != nil {
		t.Fatal(err)
	}
	binding := f.binding(t)
	if len(binding.Links) != 1 || binding.Links[0].Path != instructionsWriterPath || binding.Links[0].Source != instructionsWriterPath {
		t.Fatalf("expected an exact instruction receipt, got %+v", binding.Links)
	}
	proof := filepath.Join(f.state, localWitnessPath(binding, binding.Links[0]))
	proofBefore := linkLstat(t, proof)
	if !os.SameFile(linkBefore, proofBefore) {
		t.Fatal("fixture link does not match its ownership witness")
	}

	calls := 0
	if err := UpdateSharedInstructions(f.realm, func(data []byte) ([]byte, error) {
		calls++
		if string(data) != input {
			t.Errorf("transform input = %q, want clone worktree bytes %q", data, input)
		}
		return []byte(output), nil
	}); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Errorf("transform called %d times, want 1", calls)
	}
	for _, path := range []string{source, link} {
		if got := readRealmshareFile(t, path); got != output {
			t.Errorf("%s = %q, want %q", path, got, output)
		}
	}
	sourceAfter := linkLstat(t, source)
	if got := sourceAfter.Mode(); !got.IsRegular() || got != sourceBefore.Mode() {
		t.Errorf("source mode = %v, want %v", got, sourceBefore.Mode())
	}
	if os.SameFile(sourceBefore, sourceAfter) {
		t.Error("clone source was modified in place instead of replaced")
	}
	if target, err := os.Readlink(link); err != nil || target != targetBefore || !os.SameFile(linkBefore, linkLstat(t, link)) {
		t.Errorf("local symlink changed: target = %q, error = %v", target, err)
	}
	if !os.SameFile(proofBefore, linkLstat(t, proof)) {
		t.Error("ownership witness changed")
	}
	for name, want := range localFiles {
		path := filepath.Join(f.realm, name)
		if got := readRealmshareFile(t, path); got != want || !os.SameFile(localBefore[name], linkLstat(t, path)) {
			t.Errorf("local %s changed: %q", name, got)
		}
		assertRealmshareAbsent(t, filepath.Join(clone, name))
	}
	report, err := InspectStatus(t.Context(), f.realm)
	if err != nil {
		t.Fatal(err)
	}
	if got := statusPath(t, report, instructionsWriterPath); got.Source != instructionsWriterPath || got.Kind != KindInstructions || got.State != "linked" || got.Drift {
		t.Errorf("status should still report the owned instruction link: %+v", got)
	}
	if want := []StatusChange{{Path: instructionsWriterPath, Worktree: "M"}}; !slices.Equal(report.Changes, want) {
		t.Errorf("clone changes = %+v, want only the unstaged instruction edit %+v", report.Changes, want)
	}
}

func TestUpdateSharedInstructionsDoesNotPublishUnchangedOrRejectedBytes(t *testing.T) {
	reject := errors.New("reject instruction edit")
	for _, tc := range []struct {
		name string
		edit func([]byte) ([]byte, error)
		want error
	}{
		{name: "equal bytes", edit: func(data []byte) ([]byte, error) { return slices.Clone(data), nil }},
		{name: "callback error", edit: func(data []byte) ([]byte, error) {
			data[0] = '!'
			return data, reject
		}, want: reject},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, _, link, source := joinedInstructionsWriterFixture(t)
			const input = "# Current instructions\n"
			writeRealmshareFile(t, source, input)
			stamp := time.Unix(1234567890, 0)
			if err := os.Chtimes(source, stamp, stamp); err != nil {
				t.Fatal(err)
			}
			sourceBefore, linkBefore := linkLstat(t, source), linkLstat(t, link)
			calls := 0
			err := UpdateSharedInstructions(f.realm, func(data []byte) ([]byte, error) {
				calls++
				if string(data) != input {
					t.Fatalf("transform input = %q, want %q", data, input)
				}
				return tc.edit(data)
			})
			if !errors.Is(err, tc.want) || calls != 1 {
				t.Errorf("update = %v, %d transform calls; want %v, 1 call", err, calls, tc.want)
			}
			after := linkLstat(t, source)
			if !os.SameFile(sourceBefore, after) || !sourceBefore.ModTime().Equal(after.ModTime()) || readRealmshareFile(t, source) != input {
				t.Error("update rewrote the source")
			}
			if !os.SameFile(linkBefore, linkLstat(t, link)) {
				t.Error("update replaced the local symlink")
			}
		})
	}
}

func TestUpdateSharedInstructionsRefusesDrift(t *testing.T) {
	for _, tc := range []struct {
		name    string
		local   bool
		replace func(*testing.T, string)
	}{
		{name: "local regular replacement", local: true, replace: func(t *testing.T, path string) {
			writeRealmshareFile(t, path, "# Local replacement\n")
		}},
		{name: "missing local link", local: true},
		{name: "missing clone source"},
		{name: "clone source directory", replace: func(t *testing.T, path string) {
			writeRealmshareFile(t, filepath.Join(path, "child.md"), "# Not the instruction source\n")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, clone, link, source := joinedInstructionsWriterFixture(t)
			path, other := source, link
			if tc.local {
				path, other = link, source
			}
			otherBefore := linkLstat(t, other)
			original := readRealmshareFile(t, source)
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			var replacement os.FileInfo
			if tc.replace != nil {
				tc.replace(t, path)
				replacement = linkLstat(t, path)
			}
			err := UpdateSharedInstructions(f.realm, func([]byte) ([]byte, error) {
				t.Error("transform ran despite instruction mapping drift")
				return []byte("# Unexpected edit\n"), nil
			})
			if err == nil {
				t.Error("update accepted a drifted instruction mapping")
			}
			report, err := InspectStatus(t.Context(), f.realm)
			if err != nil {
				t.Fatal(err)
			}
			if got := statusPath(t, report, instructionsWriterPath); got.Source != instructionsWriterPath || got.Kind != KindInstructions || got.State != "conflicting" || !got.Drift || got.Detail == "" {
				t.Errorf("status should explain instruction mapping drift: %+v", got)
			}
			if replacement == nil {
				assertRealmshareAbsent(t, path)
			} else if !os.SameFile(replacement, linkLstat(t, path)) {
				t.Error("update removed the replacement")
			}
			if !os.SameFile(otherBefore, linkLstat(t, other)) {
				t.Error("update replaced the other end of the mapping")
			}
			if tc.local && readRealmshareFile(t, source) != original {
				t.Error("update changed the clone source despite local drift")
			}
			if tc.local && replacement != nil && readRealmshareFile(t, link) != "# Local replacement\n" {
				t.Error("update changed the local replacement")
			}
			if replacement != nil && replacement.IsDir() && readRealmshareFile(t, filepath.Join(path, "child.md")) != "# Not the instruction source\n" {
				t.Error("update changed the replacement directory contents")
			}
			if got := readRealmshareFile(t, filepath.Join(clone, "guide.md")); got != "# Team content\n" {
				t.Errorf("unrelated shared guide changed: %q", got)
			}
		})
	}
}

func TestUpdateSharedInstructionsRequiresInstructionReceipt(t *testing.T) {
	requireShareSymlinks(t)
	f, _, _ := joinedStatusFixture(t)
	if err := ApplyLink(t.Context(), previewRealmLink(t, f.realm, "wiki")); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(f.binding(t).ClonePath, instructionsWriterPath)
	original := readRealmshareFile(t, source)
	if err := UpdateSharedInstructions(f.realm, func([]byte) ([]byte, error) {
		t.Error("transform ran without an instruction receipt")
		return []byte("# Unexpected edit\n"), nil
	}); err == nil {
		t.Error("a wiki link authorized an instruction edit")
	}
	assertRealmshareAbsent(t, filepath.Join(f.realm, instructionsWriterPath))
	if readRealmshareFile(t, source) != original {
		t.Error("update changed the clone source without a receipt")
	}
	report, err := InspectStatus(t.Context(), f.realm)
	if err != nil {
		t.Fatal(err)
	}
	if got := statusPath(t, report, instructionsWriterPath); got.State != "available" || got.Drift {
		t.Errorf("status should show an available but unowned mapping: %+v", got)
	}
}

func joinedInstructionsWriterFixture(t *testing.T) (f shareFixture, clone, link, source string) {
	t.Helper()
	requireShareSymlinks(t)
	f, _, _ = joinedStatusFixture(t)
	if err := ApplyLink(t.Context(), previewRealmLink(t, f.realm, instructionsWriterPath)); err != nil {
		t.Fatal(err)
	}
	clone = f.binding(t).ClonePath
	return f, clone, filepath.Join(f.realm, instructionsWriterPath), filepath.Join(clone, instructionsWriterPath)
}
