// Package profile captures deterministic environment data and renders the
// profile.md stub written at install time.
package profile

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

// Env's git fields are empty when git is absent or unconfigured.
type Env struct {
	GitUserName  string
	GitUserEmail string
	GOOS         string
	GOARCH       string
	NumCPU       int
}

// CaptureEnv leaves git fields empty on failure rather than aborting install.
func CaptureEnv() Env {
	return Env{
		GitUserName:  gitConfigGlobal("user.name"),
		GitUserEmail: gitConfigGlobal("user.email"),
		GOOS:         runtime.GOOS,
		GOARCH:       runtime.GOARCH,
		NumCPU:       runtime.NumCPU(),
	}
}

// gitConfigGlobal returns "" on any error.
func gitConfigGlobal(key string) string {
	out, err := exec.Command("git", "config", "--global", key).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// RenderStub fills the Environment block from e and leaves the other sections as
// placeholders. Those come from writeStubSections, the single schema definition
// shared with renderStubWithoutEnv.
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
