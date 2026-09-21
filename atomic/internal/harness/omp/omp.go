// Package omp adopts the OMP harness to the common adapter contract: it
// discovers native profile roots through the CP0-proven `omp config path`
// command, generates the shared Atomic package and the profile-owned steering
// block, publishes repository wiki cards into the native project rule
// directory, and enrolls a profile through the shared transaction engine.
//
// What OMP loads is the profile's AGENTS.md block and the generated extension
// module under the profile's agent root; the package tree under
// ~/.atomic/packages/omp is Atomic's corpus store, and no OMP surface discovers
// it. Package installation, registration, and artifact discovery are therefore
// reported unsupported rather than assumed.
//
// Every native claim is gated by the CP0 capability record. OMP 18.1.18 proved
// extension discovery under an agent root, the steering files a profile loads,
// a session-baseline event, a pre-operation event with structured targets, and
// one exact deny result. It never registered a package, so package
// installation, shared-package visibility, project installation scope, and
// cleanup remain unsupported and are reported rather than assumed. A rule in
// the native `.omp/rules/` project directory never delivered its scoped body, so
// rule scope is reported unsupported while the bodies still ship.
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
	// AmbientProfile returns the process's ambient profile name — the documented
	// OMP_PROFILE selector — or "" when none is set. Discovery reads the ambient
	// name here rather than from the environment, so a caller that injected its
	// own resolver stays independent of the process it runs in. A nil field
	// means no ambient profile; New wires the production read.
	AmbientProfile func() string
	// ProfileNames lists extra named profiles discovery should report beyond the
	// default profile and the ambient one. CP0 proved no profile-listing
	// surface, so the production value is nil: a caller that knows a profile
	// name supplies it, and the root still comes from ConfigPath rather than a
	// guessed directory layout.
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

// Capabilities returns the CP0 record for OMP 18.1.18.
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

// Discover reports every OMP profile root visible from home. Discovery reads
// the filesystem and runs the read-only path query; it never enrolls, so it
// creates no ledger, journal, or package.
//
// A named profile is speculative: the ambient selector or a caller-supplied
// name. One this machine cannot resolve — a name with no installed binary and no
// CP0-recorded root — is skipped rather than returned, so one unanswerable name
// cannot fail discovery for every harness. The default profile is the harness's
// own root, so its refusal is still reported.
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
	// The documented selector names one isolated profile, and CP0 proved no
	// profile-listing surface, so the ambient name is the only one Atomic can
	// know without guessing a directory layout. A user who runs a named profile
	// already has it set, so discovery reports that profile's root too —
	// through the same CP0-proven `omp config path` query.
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

// ExtensionPath is the module OMP auto-discovers under a profile's agent root.
// CP0 observed discovery of an agent root's `extensions/*.ts` files, so this is
// the one native location a generated Atomic artifact reaches OMP from: the
// package tree Atomic also publishes is a corpus store no OMP surface reads.
func ExtensionPath(root string) string {
	return filepath.Join(root, filepath.FromSlash(SkeletonPath))
}
