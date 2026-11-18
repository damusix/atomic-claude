// Package omp adopts the OMP harness to the common adapter contract. Every
// native claim is gated by the capability record; see docs/research/harness-capability-matrix.md.
package omp

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/artifacts"
	"github.com/damusix/atomic-claude/atomic/internal/embeddedcorpus"
	"github.com/damusix/atomic-claude/atomic/internal/harness"
)

// ProfileEnv is the documented named-profile selector OMP reads as `--profile`.
const ProfileEnv = "OMP_PROFILE"

// DefaultAgentDir is the observed default-profile agent root, used only when the
// OMP binary is absent.
const DefaultAgentDir = ".omp/agent"

// Profile is one OMP profile; the empty Name is the default profile. Root comes
// from `omp config path`, never a guessed directory layout.
type Profile struct {
	Name string `json:"name,omitempty"`
	Root string `json:"root"`
}

// Adapter is the OMP adapter.
type Adapter struct {
	// ConfigPath resolves a profile's agent root. It defaults to
	// DefaultConfigPath; tests inject a stub.
	ConfigPath func(home, profile string) (string, error)
	// AmbientProfile returns the ambient OMP_PROFILE name, read through this seam
	// so an injected resolver stays independent of the process. Nil means none.
	AmbientProfile func() string
	// ProfileNames lists extra named profiles to report. OMP has no proven
	// profile-listing surface, so the production value is nil.
	ProfileNames func(home string) []string
	// Corpus loads the canonical corpus the package publishes. It defaults to
	// the selected binary's embedded generation; tests inject a fixture corpus.
	Corpus func() (*artifacts.Catalog, error)
}

// New returns an adapter resolving real roots and the embedded corpus.
func New() *Adapter {
	return &Adapter{
		ConfigPath:     DefaultConfigPath,
		AmbientProfile: func() string { return os.Getenv(ProfileEnv) },
		Corpus:         embeddedcorpus.Load,
	}
}

// Kind reports the harness this adapter serves.
func (a *Adapter) Kind() harness.Kind { return harness.KindOMP }

// Capabilities returns the capability record for OMP 18.1.18.
func (a *Adapter) Capabilities() harness.CapabilityMatrix { return harness.OMPCapabilities() }

// Lifecycle is implemented in lifecycle.go, where the projection, enrollment,
// verification, and removal seams are bound for one home.

// corpus loads the canonical corpus through the adapter's seam.
func (a *Adapter) corpus() (*artifacts.Catalog, error) {
	load := a.Corpus
	if load == nil {
		load = embeddedcorpus.Load
	}
	return load()
}

// resolve returns the root resolver, defaulting to DefaultConfigPath.
func (a *Adapter) resolve() func(home, profile string) (string, error) {
	if a.ConfigPath != nil {
		return a.ConfigPath
	}
	return DefaultConfigPath
}

// Discover reports every OMP profile root visible from home, read-only. An
// unresolvable named profile is skipped so it cannot fail discovery for every harness.
func (a *Adapter) Discover(home string) ([]harness.Instance, error) {
	if strings.TrimSpace(home) == "" {
		return nil, errors.New("omp: discovery requires a home directory")
	}

	resolve := a.resolve()
	profiles := []Profile{{Name: ""}}
	names := make([]string, 0, 1)
	if a.ProfileNames != nil {
		names = append(names, a.ProfileNames(home)...)
	}
	// With no profile-listing surface, the ambient selector is the only named
	// profile Atomic can know without guessing a directory layout.
	if a.AmbientProfile != nil {
		if ambient := strings.TrimSpace(a.AmbientProfile()); ambient != "" {
			names = append(names, ambient)
		}
	}
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		profiles = append(profiles, Profile{Name: name})
	}

	seen := map[string]bool{}
	instances := make([]harness.Instance, 0, len(profiles))
	for _, p := range profiles {
		root, err := resolve(home, p.Name)
		if err != nil {
			if p.Name != "" {
				continue
			}
			return nil, err
		}
		root = filepath.Clean(root)
		if root == "" || root == "." || seen[root] {
			continue
		}
		seen[root] = true
		exists, err := isDir(root)
		if err != nil {
			return nil, err
		}
		instances = append(instances, harness.Instance{
			Kind:       harness.KindOMP,
			ID:         root,
			NativeRoot: root,
			Home:       home,
			Exists:     exists,
		})
	}
	if len(instances) == 0 {
		return nil, fmt.Errorf("omp: no profile root resolved for %s: %w", home, harness.ErrNoInstance)
	}
	return instances, nil
}

// Installed reports the ambient profile's root, else the default profile's.
// An unresolvable default root wraps harness.ErrNotInstalled.
func (a *Adapter) Installed(home string) ([]harness.Instance, error) {
	profile := ""
	if a.AmbientProfile != nil {
		profile = strings.TrimSpace(a.AmbientProfile())
	}
	root, err := a.resolve()(home, profile)
	if err != nil {
		if profile == "" {
			return nil, fmt.Errorf("%w: %w", harness.ErrNotInstalled, err)
		}
		return nil, nil
	}
	root = filepath.Clean(root)
	exists, err := isDir(root)
	if err != nil {
		return nil, err
	}
	return []harness.Instance{{Kind: harness.KindOMP, ID: root, NativeRoot: root, Home: home, Exists: exists}}, nil
}

// DefaultConfigPath resolves a profile's agent root with `omp config path`. With
// no binary, only the default profile falls back to its observed root.
func DefaultConfigPath(home, profile string) (string, error) {
	if strings.TrimSpace(home) == "" {
		return "", errors.New("omp: profile root needs a home directory")
	}

	cmd := exec.Command("omp", "config", "path", "--json")
	cmd.Env = profileEnv(os.Environ(), home, profile)
	out, err := cmd.Output()
	if err != nil {
		var execErr *exec.Error
		if errors.As(err, &execErr) {
			if profile == "" {
				return filepath.Join(home, filepath.FromSlash(DefaultAgentDir)), nil
			}
			return "", fmt.Errorf("omp: resolve profile %q: the omp binary is not installed and CP0 recorded no default root for a named profile", profile)
		}
		return "", fmt.Errorf("omp: omp config path: %w", err)
	}
	return parseConfigPath(out, profile)
}

// parseConfigPath reads the agent root out of the command's output. Two lines
// could be two roots, so anything but exactly one is refused.
func parseConfigPath(out []byte, profile string) (string, error) {
	var lines []string
	for _, line := range strings.Split(string(out), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	if len(lines) != 1 {
		return "", fmt.Errorf("omp: omp config path for profile %q reported %d lines, want exactly one agent root", profile, len(lines))
	}
	return filepath.Clean(lines[0]), nil
}

// profileEnv returns env with HOME and ProfileEnv replaced, never appended, so
// an ambient value cannot shadow the requested profile.
func profileEnv(env []string, home, profile string) []string {
	out := make([]string, 0, len(env)+2)
	for _, kv := range env {
		if strings.HasPrefix(kv, "HOME=") || strings.HasPrefix(kv, ProfileEnv+"=") {
			continue
		}
		out = append(out, kv)
	}
	out = append(out, "HOME="+home)
	if profile != "" {
		out = append(out, ProfileEnv+"="+profile)
	}
	return out
}

// isDir reports whether path is an existing directory.
func isDir(path string) (bool, error) {
	fi, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("omp: stat %s: %w", path, err)
	}
	return fi.IsDir(), nil
}

// SettingsPath is a profile's native settings file. No probed session resolved a
// defaultModel from it, so the adapter never writes it.
func SettingsPath(root string) string {
	return filepath.Join(root, "config.yml")
}

// SteeringPath is the profile-owned steering file the adapter manages one block
// inside. OMP discovers it as AGENTS.md under the agent root.
func SteeringPath(root string) string {
	return filepath.Join(root, "AGENTS.md")
}

// ExtensionPath is the module OMP auto-discovers under a profile's agent root,
// the one native location a generated artifact reaches OMP from.
func ExtensionPath(root string) string {
	return filepath.Join(root, filepath.FromSlash(SkeletonPath))
}
