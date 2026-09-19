// Package omp adopts the OMP harness to the common adapter contract: it
// discovers native profile roots through the CP0-proven `omp config path`
// command, generates the shared Atomic package and the profile-owned steering
// block, and enrolls a profile through the shared transaction engine.
//
// Every native claim is gated by the CP0 capability record. OMP 18.1.18 proved
// extension discovery under an agent root, the steering files a profile loads,
// a session-baseline event, a pre-operation event with structured targets, and
// one exact deny result. It never registered a package, so package
// installation, shared-package visibility, project scope, and cleanup remain
// unsupported and are reported rather than assumed.
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
// It names isolated agent state, so a named profile's root is resolved by the
// same command with this variable set.
const ProfileEnv = "OMP_PROFILE"

// DefaultAgentDir is the agent root CP0 observed for a default-profile home. It
// is used only when the OMP binary is absent, so a machine that has never run
// OMP still reports the root its default profile would use.
const DefaultAgentDir = ".omp/agent"

// Profile is one OMP profile Atomic operates on. The empty Name is the default
// profile; a named profile is isolated agent state. Root is the profile's
// agent root — the directory OMP discovers AGENTS.md, extensions/, and rules/
// under — and a caller resolves it from the `omp config path` evidence rather
// than guessing a directory layout.
type Profile struct {
	Name string `json:"name,omitempty"`
	Root string `json:"root"`
}

// Adapter is the OMP adapter.
type Adapter struct {
	// ConfigPath resolves a profile's agent root. It defaults to
	// DefaultConfigPath; tests inject a stub so discovery never depends on the
	// process environment or an installed OMP binary.
	ConfigPath func(home, profile string) (string, error)
	// ProfileNames lists the named profiles discovery should report. CP0 proved
	// no profile-listing surface, so the production value is nil and only the
	// default profile is enumerated: a caller that knows a profile name supplies
	// it, and the root still comes from ConfigPath.
	ProfileNames func(home string) []string
	// Corpus loads the canonical corpus the package publishes. It defaults to
	// the selected binary's embedded generation; tests inject a fixture corpus.
	Corpus func() (*artifacts.Catalog, error)
}

// New returns an adapter resolving real roots and the embedded corpus.
func New() *Adapter {
	return &Adapter{ConfigPath: DefaultConfigPath, Corpus: embeddedcorpus.Load}
}

// Kind reports the harness this adapter serves.
func (a *Adapter) Kind() harness.Kind { return harness.KindOMP }

// Capabilities returns the CP0 record for OMP 18.1.18.
func (a *Adapter) Capabilities() harness.CapabilityMatrix { return harness.OMPCapabilities() }

// Lifecycle returns the generic lifecycle hook points. This checkpoint wires
// none: enrollment is driven through the adapter's own entry points, and the
// CLI cutover is where the hooks are bound, so an unwired hook reports
// ErrUnsupported rather than pretending to have run.
func (a *Adapter) Lifecycle() harness.Lifecycle { return harness.Lifecycle{} }

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

// Discover reports every OMP profile root visible from home. Discovery reads
// the filesystem and runs the read-only path query; it never enrolls, so it
// creates no ledger, journal, or package.
func (a *Adapter) Discover(home string) ([]harness.Instance, error) {
	if strings.TrimSpace(home) == "" {
		return nil, errors.New("omp: discovery requires a home directory")
	}

	resolve := a.resolve()
	profiles := []Profile{{Name: ""}}
	if a.ProfileNames != nil {
		for _, name := range a.ProfileNames(home) {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			profiles = append(profiles, Profile{Name: name})
		}
	}

	seen := map[string]bool{}
	instances := make([]harness.Instance, 0, len(profiles))
	for _, p := range profiles {
		root, err := resolve(home, p.Name)
		if err != nil {
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

// DefaultConfigPath resolves a profile's agent root by running the CP0-proven
// `omp config path` command. A named profile is selected through ProfileEnv,
// the documented profile selector, on that same command. When the binary is not
// installed the CP0-recorded default root for the home is reported, because a
// read-only path query has no side effect to fear; a binary that runs and fails
// is an error, never a guess. A named profile has no recorded fallback — CP0
// exercised only the default profile — so its absence is an error.
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

// parseConfigPath reads the agent root out of the command's output. A missing
// or ambiguous answer is refused: two lines could be two roots, and picking one
// would be a guess about where Atomic writes.
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

// profileEnv returns env with HOME pointing at home and ProfileEnv selecting
// profile. Both variables are replaced, never appended, so an ambient value
// cannot shadow the requested profile.
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

// SettingsPath is a profile's native settings file. The CP0 setup wrote a
// defaultModel into it, but no exercised session resolved that value, so model
// policy stays with the user and the adapter never writes it.
func SettingsPath(root string) string {
	return filepath.Join(root, "config.yml")
}

// SteeringPath is the profile-owned steering file the adapter manages one block
// inside. OMP discovers it as AGENTS.md under the agent root.
func SteeringPath(root string) string {
	return filepath.Join(root, "AGENTS.md")
}
