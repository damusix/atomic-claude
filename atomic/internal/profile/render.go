package profile

import (
	"fmt"
	"strings"
)

// RenderEnvironmentSection writes date verbatim into the <deterministic
// lastcheck=...> attribute. The caller supplies it; time.Now is never called here.
func RenderEnvironmentSection(e Env, tools []ToolResult, shell ShellResult, date string) string {
	var sb strings.Builder

	sb.WriteString("## Environment\n")
	fmt.Fprintf(&sb, "<deterministic lastcheck=%s>\n", date)

	fmt.Fprintf(&sb, "- Git user.name: %s\n", e.GitUserName)
	fmt.Fprintf(&sb, "- Git user.email: %s\n", e.GitUserEmail)
	fmt.Fprintf(&sb, "- OS: %s\n", e.GOOS)
	fmt.Fprintf(&sb, "- Arch: %s\n", e.GOARCH)
	fmt.Fprintf(&sb, "- CPU count: %d\n", e.NumCPU)

	catOrder := []ToolCategory{
		CategoryLanguageRuntime,
		CategoryVersionManager,
		CategoryPackageManager,
		CategoryContainer,
		CategoryMonorepo,
		CategoryCLI,
		CategoryCloud,
	}

	catLabels := map[ToolCategory]string{
		CategoryLanguageRuntime: "Language runtimes",
		CategoryVersionManager:  "Version managers",
		CategoryPackageManager:  "Package/build managers",
		CategoryContainer:       "Containers/orchestration",
		CategoryMonorepo:        "Monorepo/build",
		CategoryCLI:             "CLI tools",
		CategoryCloud:           "Cloud",
	}

	byCategory := map[ToolCategory][]ToolResult{}
	for _, r := range tools {
		if !r.Installed {
			continue
		}
		byCategory[r.Category] = append(byCategory[r.Category], r)
	}

	for _, cat := range catOrder {
		entries := byCategory[cat]
		if len(entries) == 0 {
			continue
		}
		fmt.Fprintf(&sb, "\n### %s\n", catLabels[cat])
		for _, r := range entries {
			sb.WriteString(renderToolLine(r))
		}
	}

	if shell.LoginShell != "" || shell.Framework != "" {
		sb.WriteString("\n### Shell\n")
		if shell.LoginShell != "" {
			fmt.Fprintf(&sb, "- Login shell: %s\n", shell.LoginShell)
		}
		if shell.Framework != "" {
			fmt.Fprintf(&sb, "- Framework: %s\n", shell.Framework)
		}
		if len(shell.OhMyZshPlugins) > 0 {
			fmt.Fprintf(&sb, "- oh-my-zsh custom plugins: %s\n", strings.Join(shell.OhMyZshPlugins, ", "))
		}
		if len(shell.OhMyZshThemes) > 0 {
			fmt.Fprintf(&sb, "- oh-my-zsh custom themes: %s\n", strings.Join(shell.OhMyZshThemes, ", "))
		}
		if len(shell.CustomScripts) > 0 {
			fmt.Fprintf(&sb, "- custom scripts: %s\n", strings.Join(shell.CustomScripts, ", "))
		}
	}

	sb.WriteString("</deterministic>\n")

	return sb.String()
}

// renderToolLine emits "- name: version (source)", or "- name: installed" when
// directory-only detection left no resolved path.
func renderToolLine(r ToolResult) string {
	if r.ResolvedPath == "" {
		return fmt.Sprintf("- %s: installed\n", r.Name)
	}
	// CaptureVersion always sets a version alongside a resolved path; guard anyway.
	version := r.Version
	if version == "" {
		version = "unknown"
	}
	return fmt.Sprintf("- %s: %s (%s)\n", r.Name, version, r.SourceClass)
}

// RewriteEnvironmentSection replaces the heading→next-## span wholesale, appends
// when the section is absent, and builds a stub when content is empty. Anchoring
// on the heading rather than the tags means a malformed section cannot duplicate.
// Everything outside the span is byte-preserved, including sections after it.
func RewriteEnvironmentSection(content, envSection string) string {
	if strings.TrimSpace(content) == "" {
		stub := renderStubWithoutEnv()
		return RewriteEnvironmentSection(stub, envSection)
	}

	const heading = "## Environment"
	headingIdx := findHeadingIndex(content, heading)

	if headingIdx == -1 {
		result := content
		if !strings.HasSuffix(result, "\n") {
			result += "\n"
		}
		result += "\n" + envSection
		return result
	}

	spanEnd := findNextH2After(content, headingIdx+len(heading))
	if spanEnd == -1 {
		before := content[:headingIdx]
		return before + envSection
	}

	before := content[:headingIdx]
	after := content[spanEnd:]
	// findNextH2After points at the "##" itself, so the blank line that separated
	// the sections was swallowed by the replaced span; "\n" puts it back.
	return before + envSection + "\n" + after
}

// findHeadingIndex requires the heading to sit at a line start.
func findHeadingIndex(content, heading string) int {
	idx := 0
	for {
		pos := strings.Index(content[idx:], heading)
		if pos == -1 {
			return -1
		}
		abs := idx + pos
		if abs == 0 || content[abs-1] == '\n' {
			return abs
		}
		idx = abs + len(heading)
		if idx >= len(content) {
			return -1
		}
	}
}

// findNextH2After returns -1 when no further line-start "## " heading exists.
func findNextH2After(content string, after int) int {
	search := "\n## "
	idx := strings.Index(content[after:], search)
	if idx == -1 {
		return -1
	}
	// +1 skips the newline so the index lands on the "##".
	return after + idx + 1
}

// writeStubSections is the single definition of the non-Environment schema,
// shared by RenderStub and renderStubWithoutEnv.
func writeStubSections(sb *strings.Builder) {
	sb.WriteString("# User profile\n")

	sb.WriteString("\n## Identity\n")
	sb.WriteString("<stable>\n")
	sb.WriteString("- Name: ...\n")
	sb.WriteString("- Location: ...\n")
	sb.WriteString("- Native language: ...\n")
	sb.WriteString("</stable>\n")

	sb.WriteString("\n## Work\n")
	sb.WriteString("<volatile>\n")
	sb.WriteString("- Employer: ...\n")
	sb.WriteString("- Role: ...\n")
	sb.WriteString("- Team: ...\n")
	sb.WriteString("</volatile>\n")

	sb.WriteString("\n## Active projects\n")
	sb.WriteString("<volatile>\n")
	sb.WriteString("- ...\n")
	sb.WriteString("</volatile>\n")

	sb.WriteString("\n## Interests\n")
	sb.WriteString("<stable>\n")
	sb.WriteString("- ...\n")
	sb.WriteString("- Communication style: ...\n")
	sb.WriteString("</stable>\n")

	sb.WriteString("\n## People mentioned\n")
	sb.WriteString("<volatile>\n")
	sb.WriteString("- Alice (coworker) — owns billing service\n")
	sb.WriteString("</volatile>\n")
}

// renderStubWithoutEnv leaves out ## Environment so the caller can append it.
func renderStubWithoutEnv() string {
	var sb strings.Builder
	writeStubSections(&sb)
	return sb.String()
}
