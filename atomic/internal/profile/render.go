package profile

import (
	"fmt"
	"strings"
)

// RenderEnvironmentSection produces the full ## Environment section markdown
// from detection results and a caller-injected date.
//
// The date parameter must be a YYYY-MM-DD string; it is written verbatim into
// the <deterministic lastcheck=...> attribute. time.Now() is never called here —
// the caller supplies the date for testability (mirrors hooks.SessionStart(root, now)).
func RenderEnvironmentSection(e Env, tools []ToolResult, shell ShellResult, date string) string {
	var sb strings.Builder

	sb.WriteString("## Environment\n")
	fmt.Fprintf(&sb, "<deterministic lastcheck=%s>\n", date)

	// Base deterministic fields from CaptureEnv.
	fmt.Fprintf(&sb, "- Git user.name: %s\n", e.GitUserName)
	fmt.Fprintf(&sb, "- Git user.email: %s\n", e.GitUserEmail)
	fmt.Fprintf(&sb, "- OS: %s\n", e.GOOS)
	fmt.Fprintf(&sb, "- Arch: %s\n", e.GOARCH)
	fmt.Fprintf(&sb, "- CPU count: %d\n", e.NumCPU)

	// Group installed tools by category, in the canonical category order.
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

	// Collect installed tools per category.
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

	// Shell section.
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

// renderToolLine formats a single ToolResult into a markdown bullet line.
//
// Rules:
//   - Has resolved path → "- name: version (source)" (provenance line)
//   - No resolved path (directory-only detection) → "- name: installed" (presence flag)
func renderToolLine(r ToolResult) string {
	if r.ResolvedPath == "" {
		return fmt.Sprintf("- %s: installed\n", r.Name)
	}
	// Defensive guard: version should always be set by CaptureVersion when a
	// resolved path is present, but fall back to "unknown" if somehow empty.
	version := r.Version
	if version == "" {
		version = "unknown"
	}
	return fmt.Sprintf("- %s: %s (%s)\n", r.Name, version, r.SourceClass)
}

// RewriteEnvironmentSection takes the existing file content and a freshly
// rendered Environment section, and returns the complete new file content.
//
// The four cases from the spec:
//
//   - Section present (clean): replace heading→next-## span wholesale.
//   - Section present but malformed: same wholesale replace — anchor is the
//     heading, not the tags; cannot produce duplicates.
//   - Section absent: append the new section at EOF.
//   - File absent (empty content): produce the full stub then replace/append
//     the Environment section into it.
//
// User-authored sections outside the ## Environment span are byte-preserved.
// A user section after ## Environment is NOT truncated.
func RewriteEnvironmentSection(content, envSection string) string {
	// Case: file absent / empty — produce stub, then recurse once.
	if strings.TrimSpace(content) == "" {
		// Build a stub without the Environment section; we will append below.
		stub := renderStubWithoutEnv()
		return RewriteEnvironmentSection(stub, envSection)
	}

	// Locate "## Environment" heading (must be at line start).
	const heading = "## Environment"
	headingIdx := findHeadingIndex(content, heading)

	if headingIdx == -1 {
		// Case: section absent — append at EOF.
		// Ensure a newline separator before appending.
		result := content
		if !strings.HasSuffix(result, "\n") {
			result += "\n"
		}
		result += "\n" + envSection
		return result
	}

	// Cases: section present (clean or malformed) — find the span end.
	// The span runs from headingIdx to the next "## " heading at line-start, or EOF.
	spanEnd := findNextH2After(content, headingIdx+len(heading))
	if spanEnd == -1 {
		// ## Environment is the last section — replace to EOF.
		before := content[:headingIdx]
		return before + envSection
	}

	// ## Environment has a following section — replace the span, preserve the rest.
	before := content[:headingIdx]
	after := content[spanEnd:]
	// The "\n" is injected here because findNextH2After returns the index of the
	// "##" character, which means the blank line that originally separated the
	// Environment section from the next section was consumed into the replaced
	// span (it was part of the envSection's trailing content). Re-injecting "\n"
	// restores exactly one blank line between the new section and what follows.
	return before + envSection + "\n" + after
}

// findHeadingIndex returns the byte index of "## Environment" at a line start,
// or -1 if not found.
func findHeadingIndex(content, heading string) int {
	// Must be at the start of a line (pos 0, or after a newline).
	idx := 0
	for {
		pos := strings.Index(content[idx:], heading)
		if pos == -1 {
			return -1
		}
		abs := idx + pos
		// Check that this occurrence is at a line start.
		if abs == 0 || content[abs-1] == '\n' {
			return abs
		}
		// Advance past this occurrence and keep looking.
		idx = abs + len(heading)
		if idx >= len(content) {
			return -1
		}
	}
}

// findNextH2After finds the byte index of the next "## " heading at a line start
// that occurs after the given offset. Returns -1 if none exists.
func findNextH2After(content string, after int) int {
	// We scan for "\n## " which guarantees line-start.
	search := "\n## "
	idx := strings.Index(content[after:], search)
	if idx == -1 {
		return -1
	}
	// +1 to skip the leading newline; we want the ## to start right here.
	return after + idx + 1
}

// writeStubSections writes the five non-Environment profile sections into sb.
// Extracted so both RenderStub (profile.go) and renderStubWithoutEnv share a
// single definition of the schema — a schema change only needs to happen here.
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

// renderStubWithoutEnv produces a profile.md stub without the ## Environment
// section. Used by RewriteEnvironmentSection when content is absent so the
// Environment section can be appended cleanly.
func renderStubWithoutEnv() string {
	var sb strings.Builder
	writeStubSections(&sb)
	return sb.String()
}
