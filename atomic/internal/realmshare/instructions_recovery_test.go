package realmshare

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestInstructionRemovalRestartsAfterCancellationStageDeleted(t *testing.T) {
	assertInstructionRemovalRestart(t, true)
}

func TestInstructionRemovalRestartsAfterReplacementOriginalParked(t *testing.T) {
	assertInstructionRemovalRestart(t, false)
}

func assertInstructionRemovalRestart(t *testing.T, cancel bool) {
	t.Helper()
	f, _ := newLinkFixture(t)
	requireShareSymlinks(t)
	clone := f.binding(t).ClonePath
	manifest := filepath.Join(clone, manifestName)
	writeRealmshareFile(t, manifest, readRealmshareFile(t, manifest)+manifestMapping(sharedInstructionsPath, sharedInstructionsPath, string(KindInstructions)))
	writeRealmshareFile(t, filepath.Join(clone, sharedInstructionsPath), "# Reviewed team instructions\n")
	const private = "# Unrelated private notes\n"
	f.write(t, privateInstructionsPath, private)
	if err := ApplyLink(t.Context(), previewRealmLink(t, f.realm, sharedInstructionsPath)); err != nil {
		t.Fatal(err)
	}

	const authored = "# Authored Claude instructions\n\nKeep this text exactly.\n"
	original, staged := authored+claudeLoaderBlock, authored
	a := InstructionActivation{Source: sharedInstructionsPath, ClaudeLoader: claudeLoaderDigest}
	p := pendingInstructionWrite{Path: claudeRealmLoaderPath, Stage: instructionStagePrefix + "restart"}
	if cancel {
		original, staged = authored, authored+claudeLoaderBlock
		a.ClaudeLoader = ""
		p.Member = claudeLoaderDigest
		p.Phase = instructionCanceled
	}
	f.write(t, p.Path, original)
	f.write(t, p.stagePath(), staged)
	root, err := os.OpenRoot(f.realm)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	_, _, p.Before, err = instructionFile(root, p.Path)
	if err != nil {
		t.Fatal(err)
	}
	_, mode, stageReceipt, err := instructionFile(root, p.stagePath())
	if err != nil {
		t.Fatal(err)
	}
	p.After = instructionFileDigest([]byte(staged), mode)
	var ok bool
	p.Identity, ok = strings.CutPrefix(stageReceipt, p.After+":")
	if !ok || p.Identity == "" {
		t.Fatalf("stage receipt %q does not encode its digest and identity", stageReceipt)
	}
	a.Pending = &p
	b := f.binding(t)
	b.Instructions = &a
	if err := SaveBinding(f.realm, b); err != nil {
		t.Fatal(err)
	}

	wantReceipt := stageReceipt
	if cancel {
		if err := root.Remove(p.stagePath()); err != nil {
			t.Fatal(err)
		}
		wantReceipt = p.Before
	} else if err := root.Rename(p.Path, p.parkedPath()); err != nil {
		t.Fatal(err)
	}

	loc, err := locateBinding(f.realm)
	if err != nil {
		t.Fatal(err)
	}
	for retry := 1; retry <= 2; retry++ {
		l, err := openLocalLinks(loc)
		if err != nil {
			t.Fatal(err)
		}
		err = l.removeInstructions()
		l.close()
		if err != nil {
			t.Fatalf("instruction removal attempt %d: %v", retry, err)
		}
		got := f.binding(t)
		want := InstructionActivation{Source: sharedInstructionsPath}
		if got.Instructions == nil || *got.Instructions != want {
			t.Fatalf("attempt %d: activation = %+v, want %+v", retry, got.Instructions, want)
		}
		if got.TrustedDigest != b.TrustedDigest || !slices.Equal(got.Links, b.Links) {
			t.Fatalf("attempt %d: recovery changed the approved source or its link receipts", retry)
		}
		data, _, receipt, err := instructionFile(root, p.Path)
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != authored || receipt != wantReceipt {
			t.Fatalf("attempt %d: %s = %q (receipt %q), want %q (receipt %q)", retry, p.Path, data, receipt, authored, wantReceipt)
		}
		if got := readRealmshareFile(t, filepath.Join(f.realm, privateInstructionsPath)); got != private {
			t.Fatalf("attempt %d: unrelated private guidance = %q, want %q", retry, got, private)
		}
		assertRealmshareAbsent(t, filepath.Join(f.realm, p.stagePath()))
		assertRealmshareAbsent(t, filepath.Join(f.realm, p.parkedPath()))
	}
}
