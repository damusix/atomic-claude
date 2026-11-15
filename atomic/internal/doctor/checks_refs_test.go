package doctor_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/doctor"
)

const signalsRef = "@docs/wiki/index.md"

func refBlock() string {
	return "\n## Project signals (auto-loaded)\n\n" + signalsRef + "\n"
}

func writeRefsFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCheckRefs_RefInClaudeLocalMd_Pass(t *testing.T) {
	root := t.TempDir()
	writeRefsFile(t, filepath.Join(root, "claude.local.md"), refBlock())

	r := doctor.RunCheckRefsWith(root)
	if r.Severity != doctor.PASS {
		t.Errorf("severity = %q, want PASS; detail: %q", r.Severity, r.Detail)
	}
}

func TestCheckRefs_RefInCLAUDEMd_Pass(t *testing.T) {
	root := t.TempDir()
	writeRefsFile(t, filepath.Join(root, "CLAUDE.md"), refBlock())

	r := doctor.RunCheckRefsWith(root)
	if r.Severity != doctor.PASS {
		t.Errorf("severity = %q, want PASS; detail: %q", r.Severity, r.Detail)
	}
}

// The wiki pipeline wires the ref into the shared AGENTS.md beside a thin
// CLAUDE.md loader, so a ref there is wired.
func TestCheckRefs_RefInAgentsMdBesideLoader_Pass(t *testing.T) {
	root := t.TempDir()
	writeRefsFile(t, filepath.Join(root, "AGENTS.md"), refBlock())
	writeRefsFile(t, filepath.Join(root, "CLAUDE.md"), "@AGENTS.md\n")

	r := doctor.RunCheckRefsWith(root)
	if r.Severity != doctor.PASS {
		t.Errorf("severity = %q, want PASS; detail: %q", r.Severity, r.Detail)
	}
}

// Claude reads AGENTS.md only through a loader import when CLAUDE.md exists, so
// a ref there beside a loader that never imports it is not wired.
func TestCheckRefs_RefInAgentsMdBesideNonImportingClaudeMd_Fail(t *testing.T) {
	root := t.TempDir()
	writeRefsFile(t, filepath.Join(root, "AGENTS.md"), refBlock())
	writeRefsFile(t, filepath.Join(root, "CLAUDE.md"), "# project\n")

	r := doctor.RunCheckRefsWith(root)
	if r.Severity != doctor.FAIL {
		t.Errorf("severity = %q, want FAIL; detail: %q", r.Severity, r.Detail)
	}
}

func TestCheckRefs_RefInLoneAgentsMd_Pass(t *testing.T) {
	root := t.TempDir()
	writeRefsFile(t, filepath.Join(root, "AGENTS.md"), refBlock())

	r := doctor.RunCheckRefsWith(root)
	if r.Severity != doctor.PASS {
		t.Errorf("severity = %q, want PASS; detail: %q", r.Severity, r.Detail)
	}
}

func TestCheckRefs_NoRefsAnywhere_Fail(t *testing.T) {
	root := t.TempDir()
	r := doctor.RunCheckRefsWith(root)
	if r.Severity != doctor.FAIL {
		t.Errorf("severity = %q, want FAIL; detail: %q", r.Severity, r.Detail)
	}
}

func TestCheckRefs_OnlyDeterministicRef_Fail(t *testing.T) {
	root := t.TempDir()
	writeRefsFile(t, filepath.Join(root, "CLAUDE.md"), "@.claude/project/deterministic-signals.md\n")

	r := doctor.RunCheckRefsWith(root)
	if r.Severity != doctor.FAIL {
		t.Errorf("severity = %q, want FAIL; detail: %q", r.Severity, r.Detail)
	}
}

func TestCheckRefs_SignalsRefPresent_Pass(t *testing.T) {
	root := t.TempDir()
	writeRefsFile(t, filepath.Join(root, "CLAUDE.md"), signalsRef+"\n")

	r := doctor.RunCheckRefsWith(root)
	if r.Severity != doctor.PASS {
		t.Errorf("severity = %q, want PASS; detail: %q", r.Severity, r.Detail)
	}
}

func TestCheckRefs_SearchOrder_FirstFileWins(t *testing.T) {
	root := t.TempDir()
	writeRefsFile(t, filepath.Join(root, "claude.local.md"), refBlock())
	writeRefsFile(t, filepath.Join(root, "CLAUDE.md"), refBlock())

	r := doctor.RunCheckRefsWith(root)
	if r.Severity != doctor.PASS {
		t.Errorf("severity = %q, want PASS; detail: %q", r.Severity, r.Detail)
	}
	want := "ref wired in claude.local.md"
	if r.Detail != want {
		t.Errorf("detail = %q, want %q", r.Detail, want)
	}
}

func TestCheckRefs_CLAUDELocalMd_Variant_Pass(t *testing.T) {
	root := t.TempDir()
	writeRefsFile(t, filepath.Join(root, "CLAUDE.local.md"), refBlock())

	r := doctor.RunCheckRefsWith(root)
	if r.Severity != doctor.PASS {
		t.Errorf("severity = %q, want PASS; detail: %q", r.Severity, r.Detail)
	}
}

func TestCheckRefs_ClaudeMdLower_Pass(t *testing.T) {
	root := t.TempDir()
	writeRefsFile(t, filepath.Join(root, "claude.md"), refBlock())

	r := doctor.RunCheckRefsWith(root)
	if r.Severity != doctor.PASS {
		t.Errorf("severity = %q, want PASS; detail: %q", r.Severity, r.Detail)
	}
}

func TestCheckRefs_PassDetailMentionsFilename(t *testing.T) {
	root := t.TempDir()
	writeRefsFile(t, filepath.Join(root, "CLAUDE.md"), refBlock())

	r := doctor.RunCheckRefsWith(root)
	if r.Severity != doctor.PASS {
		t.Fatalf("severity = %q, want PASS", r.Severity)
	}
	want := "ref wired in CLAUDE.md"
	if r.Detail != want {
		t.Errorf("detail = %q, want %q", r.Detail, want)
	}
}

func TestCheckRefs_NewWikiRef_Pass(t *testing.T) {
	root := t.TempDir()
	writeRefsFile(t, filepath.Join(root, "claude.local.md"), "@docs/wiki/index.md\n")

	r := doctor.RunCheckRefsWith(root)
	if r.Severity != doctor.PASS {
		t.Errorf("severity = %q, want PASS (new wiki ref); detail: %q", r.Severity, r.Detail)
	}
}

// The pre-wiki ref no longer satisfies the check on its own.
func TestCheckRefs_OldSignalsRef_Fail(t *testing.T) {
	root := t.TempDir()
	writeRefsFile(t, filepath.Join(root, "claude.local.md"), "@.claude/project/signals.md\n")

	r := doctor.RunCheckRefsWith(root)
	if r.Severity != doctor.FAIL {
		t.Errorf("severity = %q, want FAIL (old ref no longer satisfies new requirement); detail: %q", r.Severity, r.Detail)
	}
}

// Read from the shipped command so the test cannot drift from a copy.
func setupWikiWiringSnippet(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "context", "commands", "setup-wiki.md"))
	if err != nil {
		t.Fatal(err)
	}
	_, after, ok := strings.Cut(string(raw), "**Project steering file missing `@-ref`**")
	if !ok {
		t.Fatal("setup-wiki.md lost its ref-wiring step")
	}
	_, after, ok = strings.Cut(after, "```bash\n")
	if !ok {
		t.Fatal("setup-wiki.md ref-wiring step has no bash block")
	}
	snippet, _, ok := strings.Cut(after, "\n```\n")
	if !ok {
		t.Fatal("setup-wiki.md ref-wiring bash block is unterminated")
	}
	return snippet
}

func runSetupWikiWiring(t *testing.T, root string) {
	t.Helper()
	snippet := setupWikiWiringSnippet(t)
	for range 2 {
		cmd := exec.Command("bash", "-c", snippet)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("snippet: %v\n%s", err, out)
		}
	}
}

// A user's CLAUDE.md beside AGENTS.md stops Claude reading AGENTS.md alone, so
// the wiring must leave a ref doctor counts without rewriting the user's file.
func TestSetupWikiWiring_BesideUserClaudeMd_Pass(t *testing.T) {
	for name, agents := range map[string]string{
		"ref absent":            "# shared\n",
		"ref already in AGENTS": "# shared\n" + refBlock(),
	} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			writeRefsFile(t, filepath.Join(root, "AGENTS.md"), agents)
			writeRefsFile(t, filepath.Join(root, "CLAUDE.md"), "# project\n")

			runSetupWikiWiring(t, root)

			if r := doctor.RunCheckRefsWith(root); r.Severity != doctor.PASS {
				t.Errorf("severity = %q, want PASS; detail: %q", r.Severity, r.Detail)
			}
			loader, err := os.ReadFile(filepath.Join(root, "CLAUDE.md"))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.HasPrefix(string(loader), "# project\n") || strings.Count(string(loader), "@AGENTS.md") != 1 {
				t.Errorf("CLAUDE.md = %q, want the user content kept and one @AGENTS.md import", loader)
			}
		})
	}
}

// A ref Claude already loads through an override or the loader needs no
// import, and a loader linked to AGENTS.md would make AGENTS.md import itself.
func TestSetupWikiWiring_AlreadyLoadedRef_Unchanged(t *testing.T) {
	shared := "# shared\n" + refBlock()
	for name, files := range map[string]map[string]string{
		"ref in user CLAUDE.md": {
			"AGENTS.md": shared,
			"CLAUDE.md": "# project\n" + refBlock(),
		},
		"ref in CLAUDE.local.md": {
			"AGENTS.md":       shared,
			"CLAUDE.md":       "# project\n",
			"CLAUDE.local.md": refBlock(),
		},
		"CLAUDE.md links to AGENTS.md": {
			"AGENTS.md": shared,
		},
	} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			for file, content := range files {
				writeRefsFile(t, filepath.Join(root, file), content)
			}
			if _, ok := files["CLAUDE.md"]; !ok {
				if err := os.Symlink("AGENTS.md", filepath.Join(root, "CLAUDE.md")); err != nil {
					t.Fatal(err)
				}
			}

			runSetupWikiWiring(t, root)

			for file, want := range files {
				got, err := os.ReadFile(filepath.Join(root, file))
				if err != nil {
					t.Fatal(err)
				}
				if string(got) != want {
					t.Errorf("%s = %q, want it unchanged: %q", file, got, want)
				}
			}
		})
	}
}
