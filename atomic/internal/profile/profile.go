// Package profile captures deterministic environment data and renders the
// initial profile.md stub that is written to ~/.claude/.atomic/profile.md at
// install time.
package profile

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

// Env holds the deterministic environment values captured at install time.
// Git fields may be empty strings if git is not installed or no global config is set.
type Env struct {
	GitUserName  string
	GitUserEmail string
	GOOS         string
	GOARCH       string
	NumCPU       int
}

// CaptureEnv reads runtime constants and git global config.
// Git failures (not installed, no config set) result in empty strings — install is not aborted.
func CaptureEnv() Env {
	return Env{
		GitUserName:  gitConfigGlobal("user.name"),
		GitUserEmail: gitConfigGlobal("user.email"),
		GOOS:         runtime.GOOS,
		GOARCH:       runtime.GOARCH,
		NumCPU:       runtime.NumCPU(),
	}
}

// gitConfigGlobal runs `git config --global <key>` and returns the trimmed output.
// Returns empty string on any error (git absent, config key not set, etc.).
func gitConfigGlobal(key string) string {
	out, err := exec.Command("git", "config", "--global", key).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// RenderStub builds the initial profile.md content from the schema contract.
// The <deterministic> Environment block is populated from e; all other sections
// contain skeletal placeholder bullets matching the spec schema.
func RenderStub(e Env) string {
	var sb strings.Builder

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

	sb.WriteString("\n## Environment\n")
	sb.WriteString("<deterministic>\n")
	fmt.Fprintf(&sb, "- Git user.name: %s\n", e.GitUserName)
	fmt.Fprintf(&sb, "- Git user.email: %s\n", e.GitUserEmail)
	fmt.Fprintf(&sb, "- OS: %s\n", e.GOOS)
	fmt.Fprintf(&sb, "- Arch: %s\n", e.GOARCH)
	fmt.Fprintf(&sb, "- CPU count: %d\n", e.NumCPU)
	sb.WriteString("</deterministic>\n")

	return sb.String()
}
