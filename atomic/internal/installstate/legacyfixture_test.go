package installstate

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/claudeinstall"
	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/embedded"
	"github.com/damusix/atomic-claude/atomic/internal/managedfile"
)

// installLegacy runs the real Claude installer so every fixture carries the
// installer's own output format: the artifact tree, the <atomic> block, the
// [install] record, the write-once pre-install snapshot, settings/style, and
// profile.md. Nothing here fabricates a legacy shape the installer would not
// produce.
func installLegacy(t *testing.T, home string) string {
	t.Helper()
	restore := claudeinstall.ProfileRefresh
	claudeinstall.ProfileRefresh = func(string, string, int) (bool, error) { return false, nil }
	t.Cleanup(func() { claudeinstall.ProfileRefresh = restore })

	root := filepath.Join(home, ".claude")
	// A pre-existing settings.json is what the write-once snapshot records a copy
	// of: everything else the manifest names is absent on a fresh home, so the
	// snapshot's recorded copies would be empty.
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "settings.json"), []byte(`{"model":"sonnet"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := claudeinstall.Install(root, home, false, claudeinstall.RealClock); err != nil {
		t.Fatalf("legacy install: %v", err)
	}
	return root
}

// selectedArtifacts builds the selected-generation adoption artifacts from the
// embedded bundle, exactly as the harness adapter would hand them over.
func selectedArtifacts(t *testing.T, nativeRoot string) []Artifact {
	t.Helper()
	var out []Artifact
	for _, a := range embedded.Manifest() {
		data, err := fs.ReadFile(embedded.FS, a.Source)
		if err != nil {
			t.Fatalf("read embedded %s: %v", a.Source, err)
		}
		kind := managedfile.KindFile
		if a.Kind == "claude-md" {
			kind = managedfile.KindBlock
		}
		out = append(out, Artifact{
			ID:   a.Target,
			Kind: kind,
			Path: filepath.Join(nativeRoot, filepath.FromSlash(a.Target)),
			Data: data,
		})
	}
	return out
}

// ageInstall rewrites every non-block artifact to older-version bytes and the
// CLAUDE.md block to an older body, preserving the user prose around it, and
// records an older [install] version. The result is a structurally complete
// prior-version install whose artifacts the selected generation cannot prove.
func ageInstall(t *testing.T, home, nativeRoot string) {
	t.Helper()
	for _, a := range embedded.Manifest() {
		if a.Kind == "claude-md" {
			continue
		}
		path := filepath.Join(nativeRoot, filepath.FromSlash(a.Target))
		if err := os.WriteFile(path, []byte("older "+a.Target+"\n"), 0o644); err != nil {
			t.Fatalf("age %s: %v", path, err)
		}
	}
	ageSteeringBlock(t, nativeRoot)

	cfgPath := config.TOMLPath(home)
	cfg, _, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Install.Version = "0.1.0"
	if err := config.WritePersist(cfgPath, cfg); err != nil {
		t.Fatalf("record older install version: %v", err)
	}
}

// ageSteeringBlock replaces the managed block but keeps surrounding user-owned
// prose byte-identical, so adoption must splice the block rather than clobber
// the file.
func ageSteeringBlock(t *testing.T, nativeRoot string) {
	t.Helper()
	path := filepath.Join(nativeRoot, "CLAUDE.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(data, []byte("user prose before the block\n")) {
		data = append([]byte("user prose before the block\n"), data...)
	}
	if !bytes.HasSuffix(data, []byte("user prose after the block\n")) {
		data = append(data, []byte("user prose after the block\n")...)
	}
	aged, err := managedfile.ReplaceBlock(data, []byte("<atomic>\nolder atomic body\n</atomic>\n"))
	if err != nil {
		t.Fatalf("age block: %v", err)
	}
	if err := os.WriteFile(path, aged, 0o644); err != nil {
		t.Fatal(err)
	}
}

// deleteListed removes one [install]-listed artifact, producing structural
// drift on an otherwise complete install.
func deleteListed(t *testing.T, nativeRoot, target string) {
	t.Helper()
	if err := os.Remove(filepath.Join(nativeRoot, filepath.FromSlash(target))); err != nil {
		t.Fatal(err)
	}
}

// writeProposal creates the legacy diverged-CLAUDE.md proposal file.
func writeProposal(t *testing.T, home string) {
	t.Helper()
	path := claudeinstall.ProposedPath(home)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("proposed atomic CLAUDE.md\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// corruptSnapshot removes one recorded pre-install copy so the manifest no
// longer matches its own copies.
func corruptSnapshot(t *testing.T, home string) {
	t.Helper()
	dir := config.PreInstallDir(home)
	raw, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m claudeinstall.PreInstallManifest
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	for _, f := range m.Files {
		if !f.Existed {
			continue
		}
		copyPath := filepath.Join(dir, filepath.FromSlash(f.Path))
		if _, err := os.Stat(copyPath); err != nil {
			continue
		}
		if err := os.Remove(copyPath); err != nil {
			t.Fatal(err)
		}
		return
	}
	t.Fatal("no pre-install copy found to corrupt")
}

// writeLedgerFixture persists a real v2 ledger.
func writeLedgerFixture(t *testing.T, home string, targets []TargetRecord, rows []Row) {
	t.Helper()
	led := &Ledger{Header: NewHeader(), Targets: targets, Rows: rows}
	if err := led.Save(config.LedgerPath(home)); err != nil {
		t.Fatal(err)
	}
}

// writeJournalFixture persists a real journal in the v2 layout.
func writeJournalFixture(t *testing.T, home string, j *Journal) string {
	t.Helper()
	path := config.JournalPath(home, j.OperationID)
	if err := WriteJournal(path, j); err != nil {
		t.Fatal(err)
	}
	return path
}

// fileMutationAt builds a journal-ready file mutation with the intended digest
// of data.
func fileMutationAt(t *testing.T, unit, path string, data []byte) Mutation {
	t.Helper()
	digest, err := managedfile.DigestResourceBytes(data, managedfile.KindFile)
	if err != nil {
		t.Fatal(err)
	}
	return Mutation{
		Unit:     unit,
		Resource: unit,
		Target:   "claude:default",
		Kind:     managedfile.KindFile,
		Path:     path,
		Intended: digest,
	}
}

// testTime is a fixed instant for fixture journals.
func testTime() time.Time { return time.Date(2026, 9, 16, 10, 0, 0, 0, time.UTC) }

// journalWith builds an unresolved journal carrying mutations and progress.
func journalWith(id string, mutations []Mutation, progress []Progress) *Journal {
	j := NewJournal(id, testTime())
	j.Mutations = mutations
	j.Progress = progress
	return j
}

// treeDigest hashes every regular file under root, path-qualified and sorted,
// so a byte-identical filesystem has an identical digest.
func treeDigest(t *testing.T, root string) string {
	t.Helper()
	var lines []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			lines = append(lines, "d "+path)
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		lines = append(lines, "f "+path+" "+hex.EncodeToString(sum[:]))
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	sort.Strings(lines)
	sum := sha256.Sum256([]byte(strings.Join(lines, "\n")))
	return hex.EncodeToString(sum[:])
}

// journalCount counts the journal files under ~/.atomic/install/journals.
func journalCount(t *testing.T, home string) int {
	t.Helper()
	entries, err := os.ReadDir(config.JournalsDir(home))
	if err != nil {
		return 0
	}
	return len(entries)
}
