package profile_test

import (
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/profile"
)

// lastcheck uses the injected date, never time.Now.
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

func TestRenderEnvSection_HasHeading(t *testing.T) {
	e := profile.Env{GOOS: "linux", GOARCH: "amd64", NumCPU: 4}
	section := profile.RenderEnvironmentSection(e, nil, profile.ShellResult{}, "2026-01-01")

	if !strings.HasPrefix(section, "## Environment\n") {
		t.Errorf("section does not start with '## Environment\\n', got: %q", section[:min(50, len(section))])
	}
}

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

// A resolved path renders as "name: version (source)", source being the manager
// name, "brew", or "sys".
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

// Directory-only detection renders "name: installed" with no version.
func TestRenderEnvSection_VersionManagerPresenceFlag(t *testing.T) {
	tools := []profile.ToolResult{
		{
			Name:      "nvm",
			Category:  profile.CategoryVersionManager,
			Installed: true,
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

	if !strings.Contains(section, "nvm: installed") {
		t.Errorf("section missing 'nvm: installed'\ngot:\n%s", section)
	}
	if !strings.Contains(section, "pyenv: pyenv 2.3.0 (pyenv)") {
		t.Errorf("section missing pyenv provenance\ngot:\n%s", section)
	}
}

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

func TestRenderEnvSection_ClosingTag(t *testing.T) {
	e := profile.Env{GOOS: "darwin", GOARCH: "arm64", NumCPU: 10}
	section := profile.RenderEnvironmentSection(e, nil, profile.ShellResult{}, "2026-05-28")

	if !strings.Contains(section, "</deterministic>") {
		t.Errorf("section missing </deterministic>\ngot:\n%s", section)
	}
}

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

	if strings.Contains(got, "- OS: linux") {
		t.Errorf("old environment content still present\ngot:\n%s", got)
	}
	if !strings.Contains(got, "- OS: darwin") {
		t.Errorf("new environment content missing\ngot:\n%s", got)
	}
	if !strings.Contains(got, "- Name: Alice") {
		t.Errorf("Identity section was destroyed\ngot:\n%s", got)
	}
}

// A section with its tags stripped is replaced wholesale, heading not duplicated.
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

	count := strings.Count(got, "## Environment")
	if count != 1 {
		t.Errorf("expected exactly 1 '## Environment' heading, got %d\ngot:\n%s", count, got)
	}
	if strings.Contains(got, "orphan text") {
		t.Errorf("malformed content still present\ngot:\n%s", got)
	}
	if !strings.Contains(got, "- OS: darwin") {
		t.Errorf("new environment content missing\ngot:\n%s", got)
	}
	// Identity preserved.
	if !strings.Contains(got, "- Name: Bob") {
		t.Errorf("Identity section was destroyed\ngot:\n%s", got)
	}
}

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

func TestRewrite_FileAbsentProducesStub(t *testing.T) {
	newSection := "## Environment\n<deterministic lastcheck=2026-05-28>\n- OS: darwin\n</deterministic>\n"

	got := profile.RewriteEnvironmentSection("", newSection)

	if !strings.Contains(got, "# User profile") {
		t.Errorf("stub h1 missing\ngot:\n%s", got)
	}
	if !strings.Contains(got, "- OS: darwin") {
		t.Errorf("environment content missing in stub\ngot:\n%s", got)
	}
	count := strings.Count(got, "## Environment")
	if count != 1 {
		t.Errorf("expected 1 '## Environment', got %d\ngot:\n%s", count, got)
	}
}

// The boundary test: a user section after ## Environment must survive.
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

	if !strings.Contains(got, "- Name: Dave") {
		t.Errorf("Identity section destroyed\ngot:\n%s", got)
	}
	if !strings.Contains(got, "- Employer: ACME") {
		t.Errorf("Work section destroyed\ngot:\n%s", got)
	}

	if !strings.Contains(got, "lastcheck=2026-05-28") {
		t.Errorf("new lastcheck missing\ngot:\n%s", got)
	}
	if strings.Contains(got, "lastcheck=2026-01-01") {
		t.Errorf("old lastcheck still present\ngot:\n%s", got)
	}

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

// The seam keeps the no-framework path off the runner's real PATH.
func TestShellEnumeration_NoFramework_Isolated(t *testing.T) {
	home := t.TempDir() // empty — no .oh-my-zsh, no .zprezto

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

func TestShellEnumeration_StarshipViaLookPath(t *testing.T) {
	home := t.TempDir() // empty — no .oh-my-zsh, no .zprezto

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

type notFoundError struct{}

func (e *notFoundError) Error() string { return "not found" }
