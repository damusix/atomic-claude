package claude

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/bundlespec"
	"github.com/damusix/atomic-claude/atomic/internal/managedfile"
)

func migrateMode(t *testing.T, home, dir string, mode bundlespec.SteeringMode) ScopeResult {
	t.Helper()
	result, err := MigrateScope(ScopeRequest{
		Home:   home,
		Target: "claude:" + filepath.Join(home, ".claude"),
		Scope:  Scope{Dir: dir, Guidance: []byte(scopeGuidance)},
		Mode:   mode,
	})
	if err != nil {
		t.Fatalf("MigrateScope %s: %v", mode, err)
	}
	return result
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// agents-md must leave no CLAUDE.md at all: even an empty one makes Claude skip AGENTS.md.
func TestMigrateScopeAgentsMDWritesAgentsOnly(t *testing.T) {
	home, dir := t.TempDir(), t.TempDir()
	migrateMode(t, home, dir, bundlespec.SteeringAgentsMD)

	if !strings.Contains(readScopeFile(t, filepath.Join(dir, "AGENTS.md")), scopeGuidance) {
		t.Error("AGENTS.md lacks the guidance")
	}
	if fileExists(filepath.Join(dir, "CLAUDE.md")) {
		t.Error("agents-md wrote a CLAUDE.md")
	}
}

func TestMigrateScopeClaudeMDWritesInlineBlockOnly(t *testing.T) {
	home, dir := t.TempDir(), t.TempDir()
	migrateMode(t, home, dir, bundlespec.SteeringClaudeMD)

	claude := readScopeFile(t, filepath.Join(dir, "CLAUDE.md"))
	if !managedfile.BlocksEqual([]byte(claude), managedfile.BlockDocument([]byte(scopeGuidance))) {
		t.Errorf("CLAUDE.md block is not the inline guidance: %q", claude)
	}
	if fileExists(filepath.Join(dir, "AGENTS.md")) {
		t.Error("claude-md wrote an AGENTS.md")
	}
}

// Switching modes moves only Atomic's blocks: user text in either file stays
// byte-for-byte, and an AGENTS.md left holding only the block is deleted.
func TestConvertScopeMovesBlocksAndKeepsUserText(t *testing.T) {
	home, dir := t.TempDir(), t.TempDir()
	migrateMode(t, home, dir, bundlespec.SteeringLoader)

	agentsPath, claudePath := filepath.Join(dir, "AGENTS.md"), filepath.Join(dir, "CLAUDE.md")
	userAgents := "# Mine in AGENTS\n\nkeep agents text\n"
	userClaude := "# Mine in CLAUDE\n\nkeep claude text\n"
	agents := readScopeFile(t, agentsPath)
	claude := readScopeFile(t, claudePath)
	if err := os.WriteFile(agentsPath, []byte(userAgents+agents), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(claudePath, []byte(userClaude+claude), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, _, err := ConvertScope(home, "", dir, bundlespec.SteeringClaudeMD); err != nil {
		t.Fatalf("ConvertScope claude-md: %v", err)
	}
	if got := readScopeFile(t, agentsPath); got != userAgents {
		t.Errorf("AGENTS.md = %q, want only the user text %q", got, userAgents)
	}
	gotClaude := readScopeFile(t, claudePath)
	if !strings.HasPrefix(gotClaude, userClaude) || !strings.Contains(gotClaude, scopeGuidance) {
		t.Errorf("CLAUDE.md = %q, want user text then guidance", gotClaude)
	}
	if strings.Contains(gotClaude, "@AGENTS.md") {
		t.Error("CLAUDE.md still carries the loader")
	}

	if _, _, err := ConvertScope(home, "", dir, bundlespec.SteeringAgentsMD); err != nil {
		t.Fatalf("ConvertScope agents-md: %v", err)
	}
	if got := readScopeFile(t, claudePath); got != userClaude {
		t.Errorf("CLAUDE.md = %q, want only the user text %q", got, userClaude)
	}
	if !strings.Contains(readScopeFile(t, agentsPath), scopeGuidance) {
		t.Error("AGENTS.md lost the guidance")
	}

	if _, _, err := ConvertScope(home, "", dir, bundlespec.SteeringLoader); err != nil {
		t.Fatalf("ConvertScope loader: %v", err)
	}
	if got := readScopeFile(t, claudePath); !strings.HasPrefix(got, userClaude) || !strings.Contains(got, "@AGENTS.md") {
		t.Errorf("CLAUDE.md = %q, want user text then the loader", got)
	}
}

// An AGENTS.md Atomic owns whole is deleted on claude-md; and agents-md deletes
// a CLAUDE.md that held only the loader, since a leftover file would hide AGENTS.md.
func TestConvertScopeDeletesFilesAtomicOwnsWhole(t *testing.T) {
	home, dir := t.TempDir(), t.TempDir()
	migrateMode(t, home, dir, bundlespec.SteeringLoader)

	if _, _, err := ConvertScope(home, "", dir, bundlespec.SteeringAgentsMD); err != nil {
		t.Fatal(err)
	}
	if fileExists(filepath.Join(dir, "CLAUDE.md")) {
		t.Error("loader-only CLAUDE.md survived agents-md")
	}

	if _, _, err := ConvertScope(home, "", dir, bundlespec.SteeringClaudeMD); err != nil {
		t.Fatal(err)
	}
	if fileExists(filepath.Join(dir, "AGENTS.md")) {
		t.Error("block-only AGENTS.md survived claude-md")
	}
	if !fileExists(filepath.Join(dir, "CLAUDE.md")) {
		t.Error("claude-md has no CLAUDE.md")
	}
}

func TestConvertScopeLeavesDirectoryWithoutAPairAlone(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("# only mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	touched, _, err := ConvertScope(t.TempDir(), "", dir, bundlespec.SteeringClaudeMD)
	if err != nil || len(touched) != 0 {
		t.Errorf("ConvertScope = %v, %v; want no change", touched, err)
	}
	if got := readScopeFile(t, filepath.Join(dir, "CLAUDE.md")); got != "# only mine\n" {
		t.Errorf("CLAUDE.md changed to %q", got)
	}
}

func TestConvertScopeIsIdempotent(t *testing.T) {
	home, dir := t.TempDir(), t.TempDir()
	migrateMode(t, home, dir, bundlespec.SteeringClaudeMD)
	touched, _, err := ConvertScope(home, "", dir, bundlespec.SteeringClaudeMD)
	if err != nil || len(touched) != 0 {
		t.Errorf("second ConvertScope = %v, %v; want no change", touched, err)
	}
}
