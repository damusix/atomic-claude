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
//
// The five non-Environment sections are written by writeStubSections (render.go)
// so both RenderStub and renderStubWithoutEnv share a single schema definition.
func RenderStub(e Env) string {
	var sb strings.Builder

	writeStubSections(&sb)

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
