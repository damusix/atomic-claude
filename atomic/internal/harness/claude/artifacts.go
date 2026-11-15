package claude

import (
	"fmt"
	"io/fs"
	"path"
	"path/filepath"
	"strings"

	atomicartifacts "github.com/damusix/atomic-claude/atomic/internal/artifacts"
	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/embedded"
	"github.com/damusix/atomic-claude/atomic/internal/harness"
	"github.com/damusix/atomic-claude/atomic/internal/installstate"
	"github.com/damusix/atomic-claude/atomic/internal/managedfile"
)

// GlobalSteeringPath is Claude's user-level steering file. The global contract
// renders straight into it, with no global AGENTS.md loader.
func GlobalSteeringPath(nativeRoot string) string {
	return filepath.Join(nativeRoot, steeringTarget)
}

// GlobalLoaderPath is the user-level AGENTS.md a Claude install must never
// create, exposed so migration can assert its absence.
func GlobalLoaderPath(nativeRoot string) string {
	return filepath.Join(nativeRoot, "AGENTS.md")
}

// GlobalArtifacts returns the embedded generation's artifacts for a Claude
// native root, each agent carrying home's stored [claude.agents] override.
func GlobalArtifacts(home, nativeRoot string) ([]installstate.Artifact, error) {
	if strings.TrimSpace(nativeRoot) == "" {
		return nil, fmt.Errorf("claude: global artifacts need a native root")
	}
	cfg, _, err := config.Load(config.TOMLPath(home))
	if err != nil {
		return nil, fmt.Errorf("claude: read config: %w", err)
	}
	if err := config.ValidateClaudeAgentOverrides(cfg.Claude.Agents); err != nil {
		return nil, fmt.Errorf("claude: %w", err)
	}

	manifest := embedded.Manifest()
	out := make([]installstate.Artifact, 0, len(manifest))
	steering := false
	for _, a := range manifest {
		if a.Target == "" {
			return nil, fmt.Errorf("claude: embedded artifact %s carries no target", a.Source)
		}
		if a.Target == filepath.ToSlash(filepath.Base(GlobalLoaderPath(nativeRoot))) {
			return nil, fmt.Errorf("claude: embedded artifact %s projects to the forbidden global loader %s", a.Source, a.Target)
		}
		data, err := fs.ReadFile(embedded.FS, a.Source)
		if err != nil {
			return nil, fmt.Errorf("claude: read embedded %s: %w", a.Source, err)
		}
		kind := managedfile.KindFile
		switch a.Kind {
		case steeringKind:
			kind = managedfile.KindBlock
			steering = true
			if data, err = atomicartifacts.ClaudeSteering(data); err != nil {
				return nil, fmt.Errorf("claude: %s: %w", a.Target, err)
			}
		case agentKind:
			name := strings.TrimSuffix(path.Base(a.Target), ".md")
			if data, err = harness.Overlay(harness.KindClaude, data, cfg.Claude.Agents[name]); err != nil {
				return nil, fmt.Errorf("claude: %s: %w", a.Target, err)
			}
		}
		out = append(out, installstate.Artifact{
			ID:   a.Target,
			Kind: kind,
			Path: filepath.Join(nativeRoot, filepath.FromSlash(a.Target)),
			Data: data,
		})
	}
	if !steering {
		return nil, fmt.Errorf("claude: embedded corpus carries no global steering artifact")
	}
	return out, nil
}

// GlobalClaims returns the ownership claims the selected generation makes under
// nativeRoot. It must mirror GlobalArtifacts exactly.
func GlobalClaims(home, nativeRoot string) ([]managedfile.Claim, error) {
	files, err := SelectionClaims(home, nativeRoot)
	if err != nil {
		return nil, err
	}
	return append([]managedfile.Claim{SteeringClaim(nativeRoot)}, files...), nil
}
