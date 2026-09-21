package validate_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/validate"
)

// writeProjectionCorpus materializes a minimal canonical corpus under a temp
// root so the projection gate runs end-to-end without the real context/ tree.
// files are context/-relative paths; the standard artifact directories are
// always created because artifacts.Load requires them.
func writeProjectionCorpus(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for _, dir := range []string{"agents", "commands", "skills/atomic-sample", "output-styles", "rules"} {
		if err := os.MkdirAll(filepath.Join(root, "context", dir), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	files["AGENTS.md"] = "# Global\n\nBody.\n"
	for rel, content := range files {
		p := filepath.Join(root, "context", filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("mkdir for %s: %v", rel, err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	return root
}

func projectionHasRule(findings []validate.Finding, rule string) bool {
	for _, f := range findings {
		if f.Rule == rule {
			return true
		}
	}
	return false
}

// A clean corpus renders for every target with no findings.
func TestProjection_CleanCorpusPasses(t *testing.T) {
	root := writeProjectionCorpus(t, map[string]string{
		"agents/atomic-sample.md":        "---\nname: atomic-sample\ndescription: Sample agent.\n---\nAgent body.\n",
		"commands/sample.md":             "---\ndescription: Sample command.\n---\nRun `atomic code search --json`.\n",
		"skills/atomic-sample/SKILL.md":  "---\nname: atomic-sample\ndescription: Sample skill.\n---\nSkill body.\n",
		"output-styles/atomic-sample.md": "Sample style.\n",
		"rules/sample.md":                "---\npaths:\n  - \"**/*.go\"\n---\nRule body.\n",
	})

	findings, err := validate.RunProjectionRules(root)
	if err != nil {
		t.Fatalf("RunProjectionRules: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("clean corpus produced findings: %+v", findings)
	}

	// The gate is deterministic: two passes over one corpus agree.
	again, err := validate.RunProjectionRules(root)
	if err != nil {
		t.Fatalf("RunProjectionRules (second pass): %v", err)
	}
	if len(again) != len(findings) {
		t.Fatalf("gate output is not deterministic: %d != %d findings", len(again), len(findings))
	}
}

// P1: a declared dependency that names nothing in the corpus fails instead of
// projecting with a silent hole.
func TestProjection_MissingDependencyFails(t *testing.T) {
	root := writeProjectionCorpus(t, map[string]string{
		"agents/atomic-sample.md": "---\nname: atomic-sample\ndescription: Sample agent.\nskills: [atomic-missing]\n---\nAgent body.\n",
	})

	findings, err := validate.RunProjectionRules(root)
	if err != nil {
		t.Fatalf("RunProjectionRules: %v", err)
	}
	if !projectionHasRule(findings, "P1") {
		t.Fatalf("expected P1 dependency finding, got %+v", findings)
	}
	if !strings.Contains(findings[0].Message, "atomic-missing") {
		t.Errorf("finding should name the unresolved dependency: %+v", findings[0])
	}
}

// P3: a model/effort/tool restriction key that reaches a projected native
// document is a leak, never a default.
func TestProjection_MetadataLeakFails(t *testing.T) {
	root := writeProjectionCorpus(t, map[string]string{
		"commands/sample.md": "---\ndescription: Sample command.\nmodel: opus\n---\nBody.\n",
	})

	findings, err := validate.RunProjectionRules(root)
	if err != nil {
		t.Fatalf("RunProjectionRules: %v", err)
	}
	if !projectionHasRule(findings, "P3") {
		t.Fatalf("expected P3 metadata finding, got %+v", findings)
	}
}

// P3: a Codex skill is Markdown with frontmatter, so a user-policy key in a
// shipped skill file is a leak. A parser keyed on the target read the Markdown as
// TOML, failed, and reported nothing — the whole Codex skill surface went
// unchecked.
func TestProjection_CodexSkillMetadataLeakFails(t *testing.T) {
	root := writeProjectionCorpus(t, map[string]string{
		"skills/atomic-sample/SKILL.md":            "---\nname: atomic-sample\ndescription: Sample skill.\n---\nSkill body.\n",
		"skills/atomic-sample/references/notes.md": "---\nmodel: opus\n---\nNotes shipped beside the manifest.\n",
	})

	findings, err := validate.RunProjectionRules(root)
	if err != nil {
		t.Fatalf("RunProjectionRules: %v", err)
	}
	found := false
	for _, f := range findings {
		if f.Rule == "P3" && f.Severity == "FAIL" && strings.Contains(f.Message, "model") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a P3 finding for the Codex skill metadata leak, got %+v", findings)
	}
}

// P6: a Claude-only wire token that reaches another target's projected bytes
// unclassified is a leak.
func TestProjection_WireTokenLeakFails(t *testing.T) {
	root := writeProjectionCorpus(t, map[string]string{
		"agents/atomic-sample.md": "---\nname: atomic-sample\ndescription: Sample agent.\n---\nThen run `Bash` directly.\n",
	})

	findings, err := validate.RunProjectionRules(root)
	if err != nil {
		t.Fatalf("RunProjectionRules: %v", err)
	}
	if !projectionHasRule(findings, "P6") {
		t.Fatalf("expected P6 wire-token finding, got %+v", findings)
	}
}

// The gate renders the real canonical corpus and reports no FAIL.
func TestProjection_RealCorpusPasses(t *testing.T) {
	var buf strings.Builder
	code := validate.RunWithOutput([]string{"projections"}, &buf)
	if code != 0 {
		t.Fatalf("projections on real corpus: exit %d, want 0; output:\n%s", code, buf.String())
	}
	if !strings.Contains(buf.String(), "0 FAIL") {
		t.Errorf("expected a clean summary, got:\n%s", buf.String())
	}
}
