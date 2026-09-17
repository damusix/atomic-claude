package managedfile

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

const blockDoc = "user prose\n<atomic>\natomic body\n</atomic>\ntrailing prose\n"

func TestBlockBoundariesAcceptsOnlySingleBlock(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
		wantErr bool
	}{
		{name: "single block", content: blockDoc, want: "<atomic>\natomic body\n</atomic>\n"},
		{name: "tags must own their lines", content: "x\n<atomic>b</atomic>", wantErr: true},
		{name: "no block", content: "plain prose\n", wantErr: true},
		{name: "unclosed", content: "<atomic>\nbody\n", wantErr: true},
		{name: "close before open", content: "</atomic>\n<atomic>\n", wantErr: true},
		{name: "two blocks", content: "<atomic>a</atomic>\n<atomic>b</atomic>\n", wantErr: true},
		{name: "inline mention is not a boundary", content: "mentions <atomic> inline\n", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ManagedBlock([]byte(tt.content))
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ManagedBlock(%q) = %q, want error", tt.content, got)
				}
				if HasBlock([]byte(tt.content)) {
					t.Errorf("HasBlock(%q) = true, want false", tt.content)
				}
				return
			}
			if err != nil {
				t.Fatalf("ManagedBlock(%q): %v", tt.content, err)
			}
			if string(got) != tt.want {
				t.Errorf("ManagedBlock(%q) = %q, want %q", tt.content, got, tt.want)
			}
			if !HasBlock([]byte(tt.content)) {
				t.Errorf("HasBlock(%q) = false, want true", tt.content)
			}
		})
	}
}

func TestReplaceBlockPreservesSurroundings(t *testing.T) {
	out, err := ReplaceBlock([]byte(blockDoc), []byte("<atomic>\nnew body\n</atomic>\n"))
	if err != nil {
		t.Fatal(err)
	}
	want := "user prose\n<atomic>\nnew body\n</atomic>\ntrailing prose\n"
	if string(out) != want {
		t.Fatalf("ReplaceBlock = %q, want %q", out, want)
	}
	if _, err := ReplaceBlock([]byte("no block here\n"), []byte("<atomic>x</atomic>")); err == nil {
		t.Error("ReplaceBlock on unowned content: want error")
	}
	if _, err := ReplaceBlock([]byte(blockDoc), []byte("not a block")); err == nil {
		t.Error("ReplaceBlock with unowned replacement: want error")
	}
}

func TestBlocksEqualRequiresParseableIdenticalBlocks(t *testing.T) {
	a := []byte("docs a\n<atomic>\nsame\n</atomic>\n")
	b := []byte("docs b\n<atomic>\nsame\n</atomic>\n")
	c := []byte("docs c\n<atomic>\nother\n</atomic>\n")
	if !BlocksEqual(a, b) {
		t.Error("identical blocks with different surroundings should compare equal")
	}
	if BlocksEqual(a, c) {
		t.Error("different blocks should not compare equal")
	}
	if BlocksEqual(a, []byte("no block\n")) {
		t.Error("unowned content should never compare equal")
	}
}

func TestObserveFileBlockAndAbsent(t *testing.T) {
	dir := t.TempDir()

	plain := filepath.Join(dir, "plain.md")
	if err := os.WriteFile(plain, []byte("hello\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	obs, err := Observe(plain, KindFile)
	if err != nil {
		t.Fatal(err)
	}
	if obs.Kind != KindFile || obs.Digest != Digest([]byte("hello\n")) || obs.Mode != 0o600 {
		t.Errorf("Observe file = %+v", obs)
	}
	verified, ok := obs.Verify(obs.Digest)
	if !ok || verified.Ownership != OwnershipOwned {
		t.Errorf("Verify against own digest failed: %+v", verified)
	}
	if _, ok := obs.Verify(""); ok {
		t.Error("empty expected digest must not prove ownership")
	}

	missing, err := Observe(filepath.Join(dir, "gone.md"), KindFile)
	if err != nil {
		t.Fatal(err)
	}
	if missing.Kind != KindAbsent || missing.Exists() {
		t.Errorf("Observe missing = %+v", missing)
	}

	blocked := filepath.Join(dir, "CLAUDE.md")
	if err := os.WriteFile(blocked, []byte(blockDoc), 0o644); err != nil {
		t.Fatal(err)
	}
	blockObs, err := Observe(blocked, KindBlock)
	if err != nil {
		t.Fatal(err)
	}
	wantDigest := Digest([]byte("<atomic>\natomic body\n</atomic>\n"))
	if blockObs.Digest != wantDigest || blockObs.Conflict != ConflictNone {
		t.Errorf("block observation = %+v, want digest %s", blockObs, wantDigest)
	}

	malformed := filepath.Join(dir, "malformed.md")
	if err := os.WriteFile(malformed, []byte("<atomic>open only\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	badObs, err := Observe(malformed, KindBlock)
	if err != nil {
		t.Fatal(err)
	}
	if badObs.Conflict != ConflictMalformedBlock || badObs.Digest != "" {
		t.Errorf("malformed block observation = %+v", badObs)
	}
}

func TestTreeDigestIsStableAndDetectsDrift(t *testing.T) {
	root := t.TempDir()
	tree := func(name, body string) string {
		dir := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Join(dir, "nested"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "a.md"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "nested", "b.md"), []byte("b\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		return dir
	}
	first := tree("one", "same\n")
	second := tree("two", "same\n")
	digestA, entriesA, err := TreeDigest(first)
	if err != nil {
		t.Fatal(err)
	}
	digestB, _, err := TreeDigest(second)
	if err != nil {
		t.Fatal(err)
	}
	if digestA != digestB {
		t.Errorf("identical trees digested differently: %s vs %s", digestA, digestB)
	}
	if len(entriesA) != 4 {
		t.Fatalf("entries = %d, want 4 (%+v)", len(entriesA), entriesA)
	}
	var paths []string
	for _, e := range entriesA {
		paths = append(paths, e.Path)
	}
	if strings.Join(paths, ",") != ".,a.md,nested,nested/b.md" {
		t.Errorf("tree entries not in stable path order: %v", paths)
	}

	changed := tree("three", "changed\n")
	digestC, _, err := TreeDigest(changed)
	if err != nil {
		t.Fatal(err)
	}
	if digestC == digestA {
		t.Error("changed tree kept its digest")
	}

	if _, _, err := TreeDigest(filepath.Join(root, "absent")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("TreeDigest on missing root: %v", err)
	}
}

// renderModedTree renders one logical tree with every entry's mode set
// explicitly, so a comparison of two such trees isolates the root's own mode
// from the modes a umask would otherwise mask.
func renderModedTree(t *testing.T, root string) {
	t.Helper()
	nested := filepath.Join(root, "nested")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "a.md"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nested, "b.md"), []byte("b\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for path, mode := range map[string]os.FileMode{
		nested:                        0o755,
		filepath.Join(root, "a.md"):   0o644,
		filepath.Join(nested, "b.md"): 0o600,
	} {
		if err := os.Chmod(path, mode); err != nil {
			t.Fatal(err)
		}
	}
}

func TestTreeDigestIgnoresRootModeAcrossUmask(t *testing.T) {
	parent := t.TempDir()

	// Three render roots of the same tree: one MkdirAll root under a permissive
	// umask, one MkdirTemp scratch root, one MkdirAll root under a restrictive
	// umask. Only their own modes differ.
	restore := syscall.Umask(0o022)
	defer syscall.Umask(restore)

	permissive := filepath.Join(parent, "permissive")
	if err := os.MkdirAll(permissive, 0o755); err != nil {
		t.Fatal(err)
	}
	scratch, err := os.MkdirTemp(parent, "scratch-")
	if err != nil {
		t.Fatal(err)
	}

	restrictive := filepath.Join(parent, "restrictive")
	syscall.Umask(0o077)
	if err := os.MkdirAll(restrictive, 0o755); err != nil {
		t.Fatal(err)
	}
	syscall.Umask(0o022)

	var roots []string
	for _, root := range []string{permissive, scratch, restrictive} {
		renderModedTree(t, root)
		roots = append(roots, root)
	}

	modes := make([]os.FileMode, 0, len(roots))
	for _, root := range roots {
		fi, err := os.Stat(root)
		if err != nil {
			t.Fatal(err)
		}
		modes = append(modes, fi.Mode().Perm())
	}
	if modes[0] == modes[2] || modes[0] == modes[1] {
		t.Fatalf("render roots did not vary in mode: %v", modes)
	}

	digest, entries, err := TreeDigest(roots[0])
	if err != nil {
		t.Fatal(err)
	}
	for i, root := range roots[1:] {
		other, _, err := TreeDigest(root)
		if err != nil {
			t.Fatal(err)
		}
		if other != digest {
			t.Errorf("root %s (mode %v) digested %s, want %s from mode %v", root, modes[i+1], other, digest, modes[0])
		}
	}

	// The root entry is still reported with the mode it has, and every other
	// entry's mode remains part of the tree's identity.
	if len(entries) != 4 || entries[0].Path != "." {
		t.Fatalf("tree entries = %+v", entries)
	}
	if entries[0].Mode != fmt.Sprintf("%04o", modes[0]) {
		t.Errorf("root entry mode = %q, want %q", entries[0].Mode, fmt.Sprintf("%04o", modes[0]))
	}
	if err := os.Chmod(filepath.Join(roots[0], "nested"), 0o700); err != nil {
		t.Fatal(err)
	}
	changed, _, err := TreeDigest(roots[0])
	if err != nil {
		t.Fatal(err)
	}
	if changed == digest {
		t.Error("a non-root entry's mode change left the tree identity unchanged")
	}
}

func TestWriteFileAtomicPreservesModeAndFollowsSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(target, []byte("old\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := WriteFileAtomic(target, []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "new\n" {
		t.Errorf("content = %q, want %q", data, "new\n")
	}
	fi, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, want 0600 (existing bits preserved)", fi.Mode().Perm())
	}

	link := filepath.Join(dir, "link.json")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := WriteFileAtomic(link, []byte("through link\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Lstat(link); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Errorf("symlink was replaced by the rename: %v %v", fi, err)
	}
	if data, _ := os.ReadFile(target); string(data) != "through link\n" {
		t.Errorf("symlink target content = %q", data)
	}

	readonly := filepath.Join(dir, "readonly.json")
	if err := os.WriteFile(readonly, []byte("locked\n"), 0o400); err != nil {
		t.Fatal(err)
	}
	if err := WriteFileAtomic(readonly, []byte("nope\n"), 0o644); err == nil {
		t.Error("writing a read-only target: want error")
	}
	if data, _ := os.ReadFile(readonly); string(data) != "locked\n" {
		t.Errorf("read-only target was modified: %q", data)
	}
}

func TestPublishDirFirstPublicationIsOneRename(t *testing.T) {
	parent := t.TempDir()
	stage := filepath.Join(parent, "stage")
	dest := filepath.Join(parent, "packages", "atomic")
	if err := os.MkdirAll(filepath.Join(stage, "agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stage, "agents", "a.md"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	wantDigest, _, err := TreeDigest(stage)
	if err != nil {
		t.Fatal(err)
	}
	stageInfo, err := os.Stat(stage)
	if err != nil {
		t.Fatal(err)
	}

	pub, err := PublishDir(stage, dest, filepath.Join(parent, "backup"))
	if err != nil {
		t.Fatal(err)
	}
	if !pub.First || pub.Backup != "" {
		t.Errorf("publication = %+v, want first publication with no backup", pub)
	}
	if pub.Digest != wantDigest {
		t.Errorf("publication digest = %s, want %s", pub.Digest, wantDigest)
	}
	if _, err := os.Stat(stage); !os.IsNotExist(err) {
		t.Errorf("stage survived publication: %v", err)
	}
	destInfo, err := os.Stat(dest)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(stageInfo, destInfo) {
		t.Error("destination is not the staged tree: publication was not one same-filesystem rename")
	}
}

func TestPublishDirReplacementMovesCurrentToBackup(t *testing.T) {
	parent := t.TempDir()
	dest := filepath.Join(parent, "pkg")
	backupDir := filepath.Join(parent, "tx", "backup")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dest, "old.md"), []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stage := filepath.Join(parent, "stage")
	if err := os.MkdirAll(stage, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stage, "new.md"), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	pub, err := PublishDir(stage, dest, backupDir)
	if err != nil {
		t.Fatal(err)
	}
	if pub.First || pub.Backup != filepath.Join(backupDir, "pkg") {
		t.Fatalf("publication = %+v", pub)
	}
	if _, err := os.Stat(filepath.Join(dest, "new.md")); err != nil {
		t.Errorf("new tree not published: %v", err)
	}
	if data, err := os.ReadFile(filepath.Join(pub.Backup, "old.md")); err != nil || string(data) != "old\n" {
		t.Errorf("displaced tree not in backup: %q %v", data, err)
	}

	if _, err := PublishDir(stage, dest, backupDir); err == nil {
		t.Error("second replacement with a stale backup: want error")
	}
}

func TestPublishDirRollsBackWhenStagedMoveFails(t *testing.T) {
	parent := t.TempDir()
	dest := filepath.Join(parent, "pkg")
	backupDir := filepath.Join(parent, "tx", "backup")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dest, "old.md"), []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stage := filepath.Join(parent, "stage")
	if err := os.MkdirAll(stage, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stage, "new.md"), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	original := renameDir
	defer func() { renameDir = original }()
	calls := 0
	boom := errors.New("staged move failed")
	renameDir = func(oldpath, newpath string) error {
		calls++
		if calls == 2 {
			return boom
		}
		return os.Rename(oldpath, newpath)
	}

	if _, err := PublishDir(stage, dest, backupDir); !errors.Is(err, boom) {
		t.Fatalf("PublishDir error = %v, want %v", err, boom)
	}
	if data, err := os.ReadFile(filepath.Join(dest, "old.md")); err != nil || string(data) != "old\n" {
		t.Errorf("displaced tree was not restored: %q %v", data, err)
	}
	if _, err := os.Stat(filepath.Join(backupDir, "pkg")); !os.IsNotExist(err) {
		t.Errorf("backup lingered after rollback: %v", err)
	}
}

func TestBackupFileRecordsRetentionMetadataAndRestores(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "CLAUDE.md")
	if err := os.WriteFile(src, []byte(blockDoc), 0o600); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(dir, "tx", "backup", "CLAUDE.md")
	now := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	rec, err := BackupFile(src, dest, "op-1", now)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Digest != Digest([]byte(blockDoc)) || rec.Size != int64(len(blockDoc)) || rec.Mode != 0o600 || rec.OperationID != "op-1" {
		t.Errorf("backup record = %+v", rec)
	}
	if !rec.Created.Equal(now) {
		t.Errorf("backup Created = %v, want %v", rec.Created, now)
	}
	if data, err := os.ReadFile(rec.Path); err != nil || string(data) != blockDoc {
		t.Errorf("backup copy = %q (%v)", data, err)
	}

	if err := os.WriteFile(src, []byte("clobbered\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := RestoreBackup(rec); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(src); string(data) != blockDoc {
		t.Errorf("restored content = %q", data)
	}

	if err := os.WriteFile(dest, []byte("tampered\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := RestoreBackup(rec); err == nil {
		t.Error("restoring a tampered backup: want error")
	}
	if _, err := BackupFile(filepath.Join(dir, "missing"), dest, "op-1", now); err == nil {
		t.Error("backing up a missing source: want error")
	}
}

func TestSidecarDigestDetectsDrift(t *testing.T) {
	dir := t.TempDir()
	published := filepath.Join(dir, "atomic")
	if err := os.MkdirAll(published, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(published, "package.json"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	digest, entries, err := TreeDigest(published)
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteSidecar(published, Sidecar{Kind: KindTree, Digest: digest, Generation: "gen-1", Entries: entries}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(published, "package.json")); err != nil {
		t.Fatalf("sidecar must live beside the tree, not inside it: %v", err)
	}
	if got := filepath.Base(SidecarPath(published)); got != "atomic.atomic-digest.json" {
		t.Errorf("sidecar path base = %q", got)
	}

	if ok, err := VerifySidecar(published); err != nil || !ok {
		t.Fatalf("VerifySidecar = %v, %v", ok, err)
	}

	if err := os.WriteFile(filepath.Join(published, "package.json"), []byte("{\"x\":1}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if ok, err := VerifySidecar(published); err != nil || ok {
		t.Errorf("VerifySidecar after drift = %v, %v; want false", ok, err)
	}

	if err := os.Remove(SidecarPath(published)); err != nil {
		t.Fatal(err)
	}
	if _, err := VerifySidecar(published); err == nil {
		t.Error("VerifySidecar without a sidecar: want error")
	}
}

func TestDigestResourceBytesRejectsTreesAndUnmanagedBlocks(t *testing.T) {
	if _, err := DigestResourceBytes([]byte(blockDoc), KindTree); err == nil {
		t.Error("tree bytes accepted without a tree walk")
	}
	if _, err := DigestResourceBytes([]byte("no block\n"), KindBlock); err == nil {
		t.Error("unmanaged content accepted as a block")
	}
	want := Digest([]byte("<atomic>\natomic body\n</atomic>\n"))
	if got, err := DigestResourceBytes([]byte(blockDoc), KindBlock); err != nil || got != want {
		t.Errorf("block digest = %s (%v), want %s", got, err, want)
	}
}

func TestWriteFileAtomicProducesNoPartialFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ledger.json")
	if err := WriteFileAtomic(path, []byte("first\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WriteFileAtomic(path, bytes.Repeat([]byte("x"), 4096), 0o644); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "ledger.json" {
		t.Errorf("temp files left behind: %v", entries)
	}
}
