package profile_test

import (
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/profile"
)

// --- RenderEnvironmentSection ---

// TestRenderEnvSection_DateInjected verifies the lastcheck attribute uses the
// injected date, not time.Now().
func TestRenderEnvSection_DateInjected(t *testing.T) {
	e := profile.Env{
		GitUserName:  "Test User",
		GitUserEmail: "test@example.com",
		GOOS:         "darwin",
		GOARCH:       "arm64",
		NumCPU:       10,
	}
	tools := []profile.ToolResult{}
	shell := profile.ShellResult{LoginShell: "/bin/zsh"}
	const knownDate = "2026-05-28"

	section := profile.RenderEnvironmentSection(e, tools, shell, knownDate)

	want := "<deterministic lastcheck=2026-05-28>"
	if !strings.Contains(section, want) {
		t.Errorf("section missing %q\ngot:\n%s", want, section)
	}
}

// TestRenderEnvSection_HasHeading verifies the section starts with "## Environment".
func TestRenderEnvSection_HasHeading(t *testing.T) {
	e := profile.Env{GOOS: "linux", GOARCH: "amd64", NumCPU: 4}
	section := profile.RenderEnvironmentSection(e, nil, profile.ShellResult{}, "2026-01-01")

	if !strings.HasPrefix(section, "## Environment\n") {
		t.Errorf("section does not start with '## Environment\\n', got: %q", section[:min(50, len(section))])
	}
}

// TestRenderEnvSection_BaseEnvFields verifies git/OS/arch/CPU appear in the section.
func TestRenderEnvSection_BaseEnvFields(t *testing.T) {
	e := profile.Env{
		GitUserName:  "Alice",
		GitUserEmail: "alice@example.com",
		GOOS:         "linux",
		GOARCH:       "amd64",
		NumCPU:       8,
	}
	section := profile.RenderEnvironmentSection(e, nil, profile.ShellResult{}, "2026-05-28")

	wantParts := []string{
		"Git user.name: Alice",
		"Git user.email: alice@example.com",
		"OS: linux",
		"Arch: amd64",
		"CPU count: 8",
	}
	for _, p := range wantParts {
		if !strings.Contains(section, p) {
			t.Errorf("section missing %q\ngot:\n%s", p, section)
		}
	}
}

// TestRenderEnvSection_ToolProvenance verifies detected tools appear with
// "name: version (source)" format for runtimes with a resolved path.
// v2.1: source labels are the manager name, "brew", or "sys".
func TestRenderEnvSection_ToolProvenance(t *testing.T) {
	tools := []profile.ToolResult{
		{
			Name:         "python",
			Category:     profile.CategoryLanguageRuntime,
			Installed:    true,
			Version:      "Python 3.12.0",
			ResolvedPath: "/home/user/.pyenv/shims/python",
			SourceClass:  "pyenv",
		},
		{
			Name:         "go",
			Category:     profile.CategoryLanguageRuntime,
			Installed:    true,
			Version:      "go version go1.23 darwin/arm64",
			ResolvedPath: "/opt/homebrew/bin/go",
			SourceClass:  profile.SourceBrew,
		},
		{
			Name:         "git",
			Category:     profile.CategoryCLI,
			Installed:    true,
			Version:      "git version 2.39.0",
			ResolvedPath: "/usr/bin/git",
			SourceClass:  profile.SourceSys,
		},
	}
	e := profile.Env{GOOS: "darwin", GOARCH: "arm64", NumCPU: 10}
	section := profile.RenderEnvironmentSection(e, tools, profile.ShellResult{}, "2026-05-28")

	// Runtime provenance: "name: version (source)"
	if !strings.Contains(section, "python: Python 3.12.0 (pyenv)") {
		t.Errorf("section missing python provenance line\ngot:\n%s", section)
	}
	if !strings.Contains(section, "go: go version go1.23 darwin/arm64 (brew)") {
		t.Errorf("section missing go provenance line\ngot:\n%s", section)
	}
	if !strings.Contains(section, "git: git version 2.39.0 (sys)") {
		t.Errorf("section missing git provenance line\ngot:\n%s", section)
	}
}

// TestRenderEnvSection_VersionManagerPresenceFlag verifies version managers render
// as "name: installed" presence flags (no version when directory-only).
func TestRenderEnvSection_VersionManagerPresenceFlag(t *testing.T) {
	tools := []profile.ToolResult{
		{
			Name:      "nvm",
			Category:  profile.CategoryVersionManager,
			Installed: true,
			// No ResolvedPath or Version — directory-only detection.
		},
		{
			Name:         "pyenv",
			Category:     profile.CategoryVersionManager,
			Installed:    true,
			Version:      "pyenv 2.3.0",
			ResolvedPath: "/home/user/.pyenv/bin/pyenv",
			SourceClass:  "pyenv",
		},
	}
	e := profile.Env{GOOS: "linux", GOARCH: "amd64", NumCPU: 4}
	section := profile.RenderEnvironmentSection(e, tools, profile.ShellResult{}, "2026-05-28")

	// nvm has no resolved path → presence flag only.
	if !strings.Contains(section, "nvm: installed") {
		t.Errorf("section missing 'nvm: installed'\ngot:\n%s", section)
	}
	// pyenv has a resolved path → provenance line with manager name.
	if !strings.Contains(section, "pyenv: pyenv 2.3.0 (pyenv)") {
		t.Errorf("section missing pyenv provenance\ngot:\n%s", section)
	}
}

// TestRenderEnvSection_ShellInfo verifies shell and framework appear.
func TestRenderEnvSection_ShellInfo(t *testing.T) {
	e := profile.Env{GOOS: "darwin", GOARCH: "arm64", NumCPU: 10}
	shell := profile.ShellResult{
		LoginShell:     "/bin/zsh",
		Framework:      "oh-my-zsh",
		OhMyZshPlugins: []string{"git", "zsh-autosuggestions"},
		OhMyZshThemes:  []string{"mytheme.zsh-theme"},
	}
	section := profile.RenderEnvironmentSection(e, nil, shell, "2026-05-28")

	if !strings.Contains(section, "/bin/zsh") {
		t.Errorf("section missing shell\ngot:\n%s", section)
	}
	if !strings.Contains(section, "oh-my-zsh") {
		t.Errorf("section missing framework\ngot:\n%s", section)
	}
	if !strings.Contains(section, "git") || !strings.Contains(section, "zsh-autosuggestions") {
		t.Errorf("section missing oh-my-zsh plugins\ngot:\n%s", section)
	}
}

// TestRenderEnvSection_CustomScripts verifies oh-my-zsh custom scripts render
// as a "custom scripts" line (only when non-empty).
func TestRenderEnvSection_CustomScripts(t *testing.T) {
	e := profile.Env{GOOS: "darwin", GOARCH: "arm64", NumCPU: 10}
	shell := profile.ShellResult{
		LoginShell:    "/bin/zsh",
		Framework:     "oh-my-zsh",
		CustomScripts: []string{"aliases.zsh", "functions.zsh"},
	}
	section := profile.RenderEnvironmentSection(e, nil, shell, "2026-05-28")

	if !strings.Contains(section, "custom scripts: aliases.zsh, functions.zsh") {
		t.Errorf("section missing custom scripts line\ngot:\n%s", section)
	}
}

// TestRenderEnvSection_CustomScriptsOmittedWhenEmpty verifies that when
// CustomScripts is empty, no "custom scripts" line is emitted.
func TestRenderEnvSection_CustomScriptsOmittedWhenEmpty(t *testing.T) {
	e := profile.Env{GOOS: "darwin", GOARCH: "arm64", NumCPU: 10}
	shell := profile.ShellResult{
		LoginShell:    "/bin/zsh",
		Framework:     "oh-my-zsh",
		CustomScripts: nil,
	}
	section := profile.RenderEnvironmentSection(e, nil, shell, "2026-05-28")

	if strings.Contains(section, "custom scripts") {
		t.Errorf("section should not contain 'custom scripts' when none present\ngot:\n%s", section)
	}
}

// TestRenderEnvSection_ClosingTag verifies the section ends with </deterministic>.
func TestRenderEnvSection_ClosingTag(t *testing.T) {
	e := profile.Env{GOOS: "darwin", GOARCH: "arm64", NumCPU: 10}
	section := profile.RenderEnvironmentSection(e, nil, profile.ShellResult{}, "2026-05-28")

	if !strings.Contains(section, "</deterministic>") {
		t.Errorf("section missing </deterministic>\ngot:\n%s", section)
	}
}

// TestRenderEnvSection_OnlyInstalledTools verifies non-installed tools are omitted.
func TestRenderEnvSection_OnlyInstalledTools(t *testing.T) {
	tools := []profile.ToolResult{
		{Name: "docker", Category: profile.CategoryContainer, Installed: true, Version: "Docker 24.0.0", ResolvedPath: "/usr/local/bin/docker", SourceClass: profile.SourceSys},
		{Name: "podman", Category: profile.CategoryContainer, Installed: false},
	}
	e := profile.Env{GOOS: "linux", GOARCH: "amd64", NumCPU: 4}
	section := profile.RenderEnvironmentSection(e, tools, profile.ShellResult{}, "2026-05-28")

	if !strings.Contains(section, "docker") {
		t.Errorf("section missing installed tool 'docker'\ngot:\n%s", section)
	}
	if strings.Contains(section, "podman") {
		t.Errorf("section contains non-installed tool 'podman'\ngot:\n%s", section)
	}
}

// --- RewriteEnvironmentSection ---

// TestRewrite_CleanReplace verifies the section is replaced in a file that
// already has a clean ## Environment section.
func TestRewrite_CleanReplace(t *testing.T) {
	existing := `# User profile

## Identity
<stable>
- Name: Alice
</stable>

## Environment
<deterministic lastcheck=2026-01-01>
- OS: linux
</deterministic>
`
	newSection := "## Environment\n<deterministic lastcheck=2026-05-28>\n- OS: darwin\n</deterministic>\n"

	got := profile.RewriteEnvironmentSection(existing, newSection)

	// Old OS gone, new OS present.
	if strings.Contains(got, "- OS: linux") {
		t.Errorf("old environment content still present\ngot:\n%s", got)
	}
	if !strings.Contains(got, "- OS: darwin") {
		t.Errorf("new environment content missing\ngot:\n%s", got)
	}
	// Identity preserved.
	if !strings.Contains(got, "- Name: Alice") {
		t.Errorf("Identity section was destroyed\ngot:\n%s", got)
	}
}

// TestRewrite_MalformedSelfHeals verifies a malformed section (tags stripped)
// is replaced wholesale without duplicating the heading.
func TestRewrite_MalformedSelfHeals(t *testing.T) {
	existing := `# User profile

## Identity
<stable>
- Name: Bob
</stable>

## Environment
orphan text with no tags
more garbage
`
	newSection := "## Environment\n<deterministic lastcheck=2026-05-28>\n- OS: darwin\n</deterministic>\n"

	got := profile.RewriteEnvironmentSection(existing, newSection)

	// Exactly one "## Environment" heading.
	count := strings.Count(got, "## Environment")
	if count != 1 {
		t.Errorf("expected exactly 1 '## Environment' heading, got %d\ngot:\n%s", count, got)
	}
	// Old malformed content gone.
	if strings.Contains(got, "orphan text") {
		t.Errorf("malformed content still present\ngot:\n%s", got)
	}
	// New content present.
	if !strings.Contains(got, "- OS: darwin") {
		t.Errorf("new environment content missing\ngot:\n%s", got)
	}
	// Identity preserved.
	if !strings.Contains(got, "- Name: Bob") {
		t.Errorf("Identity section was destroyed\ngot:\n%s", got)
	}
}

// TestRewrite_SectionAbsentAppends verifies a fresh section is appended when
// ## Environment is not in the file.
func TestRewrite_SectionAbsentAppends(t *testing.T) {
	existing := `# User profile

## Identity
<stable>
- Name: Carol
</stable>
`
	newSection := "## Environment\n<deterministic lastcheck=2026-05-28>\n- OS: darwin\n</deterministic>\n"

	got := profile.RewriteEnvironmentSection(existing, newSection)

	if !strings.Contains(got, "## Environment") {
		t.Errorf("section was not appended\ngot:\n%s", got)
	}
	if !strings.Contains(got, "- OS: darwin") {
		t.Errorf("new environment content missing after append\ngot:\n%s", got)
	}
	// Identity preserved.
	if !strings.Contains(got, "- Name: Carol") {
		t.Errorf("Identity section was destroyed\ngot:\n%s", got)
	}
}

// TestRewrite_FileAbsentProducesStub verifies that empty/absent content
// produces the full stub with the new Environment section.
func TestRewrite_FileAbsentProducesStub(t *testing.T) {
	newSection := "## Environment\n<deterministic lastcheck=2026-05-28>\n- OS: darwin\n</deterministic>\n"

	// Empty string simulates absent file content.
	got := profile.RewriteEnvironmentSection("", newSection)

	// Must contain the h1 title (from stub).
	if !strings.Contains(got, "# User profile") {
		t.Errorf("stub h1 missing\ngot:\n%s", got)
	}
	// Must contain the environment section.
	if !strings.Contains(got, "- OS: darwin") {
		t.Errorf("environment content missing in stub\ngot:\n%s", got)
	}
	// Must have exactly 1 ## Environment heading.
	count := strings.Count(got, "## Environment")
	if count != 1 {
		t.Errorf("expected 1 '## Environment', got %d\ngot:\n%s", count, got)
	}
}

// TestRewrite_UserSectionAfterEnvPreserved verifies that a user-authored section
// AFTER ## Environment is NOT truncated by the rewrite.
// This is the load-bearing boundary-detection test from the spec.
func TestRewrite_UserSectionAfterEnvPreserved(t *testing.T) {
	existing := `# User profile

## Identity
<stable>
- Name: Dave
</stable>

## Work
<volatile>
- Employer: ACME
</volatile>

## Environment
<deterministic lastcheck=2026-01-01>
- OS: linux
</deterministic>

## Active projects
<volatile>
- my-awesome-project
</volatile>

## People mentioned
<volatile>
- Eve (coworker) — owns the payments service
</volatile>
`
	newSection := "## Environment\n<deterministic lastcheck=2026-05-28>\n- OS: darwin\n</deterministic>\n"

	got := profile.RewriteEnvironmentSection(existing, newSection)

	// Sections before Environment preserved.
	if !strings.Contains(got, "- Name: Dave") {
		t.Errorf("Identity section destroyed\ngot:\n%s", got)
	}
	if !strings.Contains(got, "- Employer: ACME") {
		t.Errorf("Work section destroyed\ngot:\n%s", got)
	}

	// Environment updated.
	if !strings.Contains(got, "lastcheck=2026-05-28") {
		t.Errorf("new lastcheck missing\ngot:\n%s", got)
	}
	if strings.Contains(got, "lastcheck=2026-01-01") {
		t.Errorf("old lastcheck still present\ngot:\n%s", got)
	}

	// Sections AFTER Environment preserved — the critical boundary test.
	if !strings.Contains(got, "## Active projects") {
		t.Errorf("'## Active projects' after Environment was truncated\ngot:\n%s", got)
	}
	if !strings.Contains(got, "my-awesome-project") {
		t.Errorf("Active projects content after Environment was truncated\ngot:\n%s", got)
	}
	if !strings.Contains(got, "## People mentioned") {
		t.Errorf("'## People mentioned' after Environment was truncated\ngot:\n%s", got)
	}
	if !strings.Contains(got, "Eve (coworker)") {
		t.Errorf("People mentioned content after Environment was truncated\ngot:\n%s", got)
	}

	// Exactly one ## Environment heading.
	count := strings.Count(got, "## Environment")
	if count != 1 {
		t.Errorf("expected 1 '## Environment', got %d\ngot:\n%s", count, got)
	}
}

// TestRewrite_EnvAtEOF verifies correct behavior when ## Environment is the last
// section (no following ## heading).
func TestRewrite_EnvAtEOF(t *testing.T) {
	existing := `# User profile

## Identity
<stable>
- Name: Frank
</stable>

## Environment
<deterministic lastcheck=2026-01-01>
- OS: linux
</deterministic>
`
	newSection := "## Environment\n<deterministic lastcheck=2026-05-28>\n- OS: darwin\n</deterministic>\n"

	got := profile.RewriteEnvironmentSection(existing, newSection)

	if !strings.Contains(got, "lastcheck=2026-05-28") {
		t.Errorf("new lastcheck missing\ngot:\n%s", got)
	}
	if strings.Contains(got, "lastcheck=2026-01-01") {
		t.Errorf("old lastcheck still present\ngot:\n%s", got)
	}
	if !strings.Contains(got, "- Name: Frank") {
		t.Errorf("Identity section destroyed\ngot:\n%s", got)
	}
}

// --- LookPath seam for starship (F-1) ---

// TestShellEnumeration_NoFramework_Isolated verifies the no-framework path
// does not depend on the runner's real PATH by injecting a LookPath seam
// that always returns "not found".
func TestShellEnumeration_NoFramework_Isolated(t *testing.T) {
	home := t.TempDir() // empty — no .oh-my-zsh, no .zprezto

	// LookPath seam: always returns not-found, regardless of real PATH.
	notFound := func(string) (string, error) {
		return "", &notFoundError{}
	}

	opts := profile.ShellEnvOptions{
		Shell:    "/bin/bash",
		Home:     home,
		LookPath: notFound,
	}
	result := profile.DetectShell(opts)
	if result.Framework != "" {
		t.Errorf("Framework = %q, want empty (seam prevents starship detection)", result.Framework)
	}
}

// TestShellEnumeration_StarshipViaLookPath verifies that starship is detected
// when the LookPath seam succeeds.
func TestShellEnumeration_StarshipViaLookPath(t *testing.T) {
	home := t.TempDir() // empty — no .oh-my-zsh, no .zprezto

	// LookPath seam: starship "found".
	found := func(name string) (string, error) {
		if name == "starship" {
			return "/usr/local/bin/starship", nil
		}
		return "", &notFoundError{}
	}

	opts := profile.ShellEnvOptions{
		Shell:    "/bin/zsh",
		Home:     home,
		LookPath: found,
	}
	result := profile.DetectShell(opts)
	if result.Framework != "starship" {
		t.Errorf("Framework = %q, want %q", result.Framework, "starship")
	}
}

// notFoundError satisfies the error interface for the LookPath seam.
type notFoundError struct{}

func (e *notFoundError) Error() string { return "not found" }
