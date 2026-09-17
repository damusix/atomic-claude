package where_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/where"
)

// A `<wikis>` mention in the shipped <atomic> prose is not a registration. The
// registry reader matches whole-line tags only, so a bullet inside <atomic> can
// never classify cwd — only the real block can.
func TestResolve_ProseMentionInAtomicBlockIsNotARealm(t *testing.T) {
	home := t.TempDir()
	bogusRoot := filepath.Join(home, "bogus-realm")
	realRoot := filepath.Join(t.TempDir(), "real-realm")
	cwd := filepath.Join(bogusRoot, "project")

	claudeHome := filepath.Join(home, ".claude")
	if err := os.MkdirAll(claudeHome, 0o755); err != nil {
		t.Fatal(err)
	}
	claudeMD := filepath.Join(claudeHome, "CLAUDE.md")
	content := "<atomic>\n" +
		"- `atomic wiki`: wiki paths live in a `<wikis>` block in `~/.claude/CLAUDE.md`, outside `<atomic>`.\n" +
		"- " + filepath.Join(bogusRoot, "wiki", "index.md") + ": named in prose, never a registration.\n" +
		"</atomic>\n\n" +
		"<wikis>\n- " + filepath.Join(realRoot, "wiki", "index.md") + "\n</wikis>\n"
	if err := os.WriteFile(claudeMD, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	report, err := where.Resolve(cwd, claudeMD)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if report.RealmScope.Position != where.RealmNone {
		t.Errorf("position = %v (realm root %q), want none: cwd is outside every registered realm",
			report.RealmScope.Position, report.RealmScope.RealmRoot)
	}
	if report.RealmScope.RealmRoot == bogusRoot {
		t.Errorf("resolved the prose bullet in the <atomic> block as a realm root: %q", bogusRoot)
	}
}
