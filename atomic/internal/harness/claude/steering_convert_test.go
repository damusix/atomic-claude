package claude

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/bundlespec"
	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/installstate"
	"github.com/damusix/atomic-claude/atomic/internal/managedfile"
)

func loadLedgerAt(t *testing.T, home string) *installstate.Ledger {
	t.Helper()
	ledger, err := installstate.LoadLedger(config.LedgerPath(home))
	if err != nil {
		t.Fatal(err)
	}
	return ledger
}

func rowFor(ledger *installstate.Ledger, path string) (installstate.Row, bool) {
	for _, r := range ledger.Rows {
		if r.Resource == path {
			return r, true
		}
	}
	return installstate.Row{}, false
}

// A converted file is re-owned at its new bytes, and a deleted file stops being
// owned, so the ledger never claims bytes Atomic no longer wrote.
func TestConvertScopeKeepsLedgerInStep(t *testing.T) {
	home, dir := t.TempDir(), t.TempDir()
	migrateMode(t, home, dir, bundlespec.SteeringLoader)
	agentsPath, claudePath := filepath.Join(dir, "AGENTS.md"), filepath.Join(dir, "CLAUDE.md")
	if _, ok := rowFor(loadLedgerAt(t, home), claudePath); !ok {
		t.Fatal("MigrateScope left no ledger row for CLAUDE.md")
	}

	if _, _, err := ConvertScope(home, "", dir, bundlespec.SteeringClaudeMD); err != nil {
		t.Fatal(err)
	}
	ledger := loadLedgerAt(t, home)
	if _, ok := rowFor(ledger, agentsPath); ok {
		t.Error("a ledger row survives for the deleted AGENTS.md")
	}
	row, ok := rowFor(ledger, claudePath)
	if !ok {
		t.Fatal("no ledger row for the rewritten CLAUDE.md")
	}
	data, _ := os.ReadFile(claudePath)
	want, err := managedfile.DigestResourceBytes(data, managedfile.KindBlock)
	if err != nil || row.Applied.Digest != want {
		t.Errorf("row digest = %s, want %s (%v)", row.Applied.Digest, want, err)
	}
}

// With no ledger row, a block-only AGENTS.md is neither deleted nor stripped to
// an empty file: it stays as it is and a note names it.
func TestConvertScopeLeavesUnownedBlockOnlyFileUntouched(t *testing.T) {
	home, dir := t.TempDir(), t.TempDir()
	agentsPath := filepath.Join(dir, "AGENTS.md")
	original := managedfile.BlockDocument([]byte(scopeGuidance))
	if err := os.WriteFile(agentsPath, original, 0o644); err != nil {
		t.Fatal(err)
	}

	_, notes, err := ConvertScope(home, "", dir, bundlespec.SteeringClaudeMD)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(agentsPath); err != nil || string(got) != string(original) {
		t.Errorf("AGENTS.md = %q, %v; want it untouched", got, err)
	}
	if len(notes) != 1 || !strings.Contains(notes[0], "AGENTS.md") {
		t.Errorf("notes = %v, want one naming AGENTS.md", notes)
	}
	claude, _ := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	if !strings.Contains(string(claude), scopeGuidance) {
		t.Errorf("CLAUDE.md = %q, want the guidance", claude)
	}
}

// The global Claude root holds the global steering, so a pair there would be
// read twice; even the default loader mode must leave it alone.
func TestConvertScopeRefusesTheClaudeNativeRoot(t *testing.T) {
	home := t.TempDir()
	native := filepath.Join(home, ".claude")
	if err := os.MkdirAll(native, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := exec.Command("git", "-C", native, "init", "-q").Run(); err != nil {
		t.Skipf("git unavailable: %v", err)
	}
	block := managedfile.BlockDocument([]byte(scopeGuidance))
	if err := os.WriteFile(filepath.Join(native, "AGENTS.md"), block, 0o644); err != nil {
		t.Fatal(err)
	}

	for _, mode := range bundlespec.SteeringModes {
		if _, _, err := ConvertScope(home, native, native, mode); err == nil || !strings.Contains(err.Error(), "native root") {
			t.Errorf("%s: err = %v, want a native-root refusal", mode, err)
		}
	}
	if !IsNativeRoot(home, native) {
		t.Error("IsNativeRoot did not recognise ~/.claude")
	}
	if IsNativeRoot(home, t.TempDir()) {
		t.Error("IsNativeRoot matched an unrelated directory")
	}
	if _, err := os.Stat(filepath.Join(native, "CLAUDE.md")); err == nil {
		t.Error("a CLAUDE.md was written into the native root")
	}
}

func TestConvertScopeRefusesAnEnrolledNativeRoot(t *testing.T) {
	home, enrolled := t.TempDir(), t.TempDir()
	ledger := &installstate.Ledger{Header: installstate.NewHeader()}
	ledger.Targets = []installstate.TargetRecord{{Harness: "claude", Instance: enrolled, NativeRoot: enrolled, Status: "enrolled"}}
	if err := ledger.Save(config.LedgerPath(home)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ConvertScope(home, "", enrolled, bundlespec.SteeringLoader); err == nil {
		t.Error("ConvertScope wrote into an enrolled Claude root")
	}
}

// The global ~/.claude/CLAUDE.md is the same whatever steering mode the user or
// repository sets, because Claude reads no global AGENTS.md.
func TestGlobalClaudeMDIsIdenticalAcrossSteeringModes(t *testing.T) {
	var baseline []byte
	for _, mode := range append([]bundlespec.SteeringMode{""}, bundlespec.SteeringModes...) {
		home, repo := t.TempDir(), t.TempDir()
		if mode != "" {
			cfg := "[steering]\nclaude = \"" + string(mode) + "\"\n"
			if err := os.MkdirAll(filepath.Dir(config.TOMLPath(home)), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(config.TOMLPath(home), []byte(cfg), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(filepath.Dir(config.RepoConfigPath(repo)), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(config.RepoConfigPath(repo), []byte(cfg), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		artifacts, err := GlobalArtifacts(home, filepath.Join(home, ".claude"))
		if err != nil {
			t.Fatalf("mode %q: %v", mode, err)
		}
		var got []byte
		for _, a := range artifacts {
			if filepath.Base(a.Path) == "CLAUDE.md" {
				got = a.Data
			}
		}
		if got == nil {
			t.Fatal("no global CLAUDE.md artifact")
		}
		if baseline == nil {
			baseline = got
		} else if string(got) != string(baseline) {
			t.Errorf("global CLAUDE.md differs under steering mode %q", mode)
		}
	}
}
