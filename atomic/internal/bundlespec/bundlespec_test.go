package bundlespec_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/bundlespec"
)

func TestMatchesAgent(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{"atomic-builder.md", true},
		{"atomic-reviewer.md", true},
		{"atomic-signals-inferrer.md", true},
		{"builder.md", false},
		{"README.md", false},
		{"atomic-builder.txt", false},
		{"atomic-builder", false},
		{"atomic-builder/", false},
	}
	for _, tc := range cases {
		got := bundlespec.MatchesAgent(tc.name)
		if got != tc.want {
			t.Errorf("MatchesAgent(%q) = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestMatchesSkillDir(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{"atomic-git-discipline", true},
		{"atomic-tdd", true},
		{"atomic-writing", true},
		{"commit", false},
		{"_templates", false},
		{"atomiccommit", false},
		// Name-only predicate: gating on IsDir() is the caller's job.
		{"atomic-foo.md", true},
	}
	for _, tc := range cases {
		got := bundlespec.MatchesSkillDir(tc.name)
		if got != tc.want {
			t.Errorf("MatchesSkillDir(%q) = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestMatchesOutputStyle(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{"atomic.md", true},
		{"atomic-verbose.md", true},
		{"verbose.md", false},
		{"README.md", false},
		{"atomic.txt", false},
		{"atomic", false},
	}
	for _, tc := range cases {
		got := bundlespec.MatchesOutputStyle(tc.name)
		if got != tc.want {
			t.Errorf("MatchesOutputStyle(%q) = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestMatchesCommand(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{"commit-only.md", true},
		{"atomic-plan.md", true},
		{"README.md", true}, // no allowlist — any .md file matches
		{"commit-only.txt", false},
		{"commit-only", false},
	}
	for _, tc := range cases {
		got := bundlespec.MatchesCommand(tc.name)
		if got != tc.want {
			t.Errorf("MatchesCommand(%q) = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestMatchesRule(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"rules/python/style.md", true},
		{"rules/typescript/style.md", true},
		{"rules/go/naming.md", true},
		{"rules/python/style.txt", false},
		{"rules/python/style", false},
	}
	for _, tc := range cases {
		got := bundlespec.MatchesRule(tc.path)
		if got != tc.want {
			t.Errorf("MatchesRule(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

func TestIsGlobalSteeringSource(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{"AGENTS.md", true},
		{"agents.md", false},
		{"CLAUDE.md", false},
		{"path/AGENTS.md", false},
	}
	for _, tc := range cases {
		got := bundlespec.IsGlobalSteeringSource(tc.name)
		if got != tc.want {
			t.Errorf("IsGlobalSteeringSource(%q) = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestLoaderWithoutImport(t *testing.T) {
	loader := string(bundlespec.ScopeSteering.LoaderDocument())
	cases := []struct {
		name  string
		files map[string]string
		want  string
	}{
		{"no loader", nil, ""},
		{"loader imports source", map[string]string{"CLAUDE.md": loader}, ""},
		{"prose without import", map[string]string{"CLAUDE.md": "# Realm\n"}, "CLAUDE.md"},
		{"lowercase loader imports source", map[string]string{"claude.md": loader}, ""},
		{"lowercase prose without import", map[string]string{"claude.md": "# Realm\n"}, "claude.md"},
		{"upper imports, lower does not", map[string]string{"CLAUDE.md": loader, "claude.md": "# Realm\n"}, "claude.md"},
		{"prose import", map[string]string{"CLAUDE.md": "Claude reads @AGENTS.md here.\n"}, ""},
		{"trailing punctuation is not an import", map[string]string{"CLAUDE.md": "See @./AGENTS.md.\n"}, "CLAUDE.md"},
		{"indented code block is not an import", map[string]string{"CLAUDE.md": "# Realm\n\n    @AGENTS.md\n"}, "CLAUDE.md"},
		{"unclosed backtick run stays prose", map[string]string{"CLAUDE.md": "Use ` then @AGENTS.md\n"}, ""},
		{"backup name is not an import", map[string]string{"CLAUDE.md": "@AGENTS.md.bak\n"}, "CLAUDE.md"},
		{"fenced example is not an import", map[string]string{"CLAUDE.md": "# Realm\n\n```\n@AGENTS.md\n```\n"}, "CLAUDE.md"},
		{"code span is not an import", map[string]string{"CLAUDE.md": "Write `@AGENTS.md` to import it.\n"}, "CLAUDE.md"},
		{"tilde line inside a backtick fence", map[string]string{"CLAUDE.md": "```\n~~~\n@AGENTS.md\n```\n"}, "CLAUDE.md"},
		{"longer fence around a shorter one", map[string]string{"CLAUDE.md": "````\n```\n@AGENTS.md\n```\n````\n"}, "CLAUDE.md"},
		{"dot-slash import", map[string]string{"CLAUDE.md": "# Realm\n\n@./AGENTS.md\n"}, ""},
		{"indented import line", map[string]string{"CLAUDE.md": "  @AGENTS.md  \n"}, ""},
	}
	for _, tc := range cases {
		dir := t.TempDir()
		if len(tc.files) == 2 && caseInsensitive(t, dir) {
			continue
		}
		for name, body := range tc.files {
			if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		got := bundlespec.ScopeSteering.LoaderWithoutImport(dir)
		if got == "" && tc.want == "" {
			continue
		}
		if got == "" || tc.want == "" || !strings.EqualFold(filepath.Base(got), tc.want) || filepath.Dir(got) != dir {
			t.Errorf("%s: LoaderWithoutImport = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// caseInsensitive reports whether dir's filesystem folds case, where
// CLAUDE.md and claude.md are one file and cannot both be written.
func caseInsensitive(t *testing.T, dir string) bool {
	t.Helper()
	probe := filepath.Join(dir, "probe")
	if err := os.WriteFile(probe, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(probe)
	_, err := os.Stat(filepath.Join(dir, "PROBE"))
	return err == nil
}

func TestSteeringContract(t *testing.T) {
	if bundlespec.GlobalSteering.Loader {
		t.Error("global steering must render directly, not through a loader")
	}
	if !bundlespec.ScopeSteering.Loader {
		t.Error("repository and realm steering must use an adjacent loader")
	}
	if got := bundlespec.ScopeSteering.LoaderBody(); got != "@AGENTS.md\n" {
		t.Errorf("LoaderBody() = %q, want %q", got, "@AGENTS.md\n")
	}
}

// A loader the wiki pipeline writes must carry the bytes the Claude adapter
// writes, or a later install no longer recognizes the block as its own.
func TestPipelineLoaderProseMatchesLoaderDocument(t *testing.T) {
	doc := strings.TrimSuffix(string(bundlespec.ScopeSteering.LoaderDocument()), "\n")
	for file, indent := range map[string]string{
		"context/commands/setup-wiki.md":                "",
		"context/commands/refresh-wiki.md":              "",
		"context/skills/atomic-wiki/references/repo.md": "    ",
	} {
		raw, err := os.ReadFile(filepath.Join("..", "..", "..", file))
		if err != nil {
			t.Fatal(err)
		}
		lines := strings.Split("```markdown\n"+doc+"\n```", "\n")
		for i, line := range lines {
			if line != "" {
				lines[i] = indent + line
			}
		}
		if !strings.Contains(string(raw), strings.Join(lines, "\n")) {
			t.Errorf("%s has no loader block matching LoaderDocument %q", file, doc)
		}
	}
}
