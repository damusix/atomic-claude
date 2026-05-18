// Package bundlemirror implements the artifact mirror logic used by cmd/bundle-mirror.
// Separated so it can be tested without the main() entrypoint.
package bundlemirror

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"github.com/damusix/atomic-claude/atomic/internal/bundlespec"
	"github.com/damusix/atomic-claude/atomic/internal/embedded"
)

// Artifact describes one file in the embedded manifest.
// Kept for backward compatibility with cmd/bundle-mirror; consumers outside
// this package should prefer embedded.Artifact.
type Artifact struct {
	Kind   string
	Source string // path inside embedded FS, e.g. "bundle/agents/atomic-builder.md"
	Target string // path to write inside the target dir
	SHA256 string
}

// Enumerate walks repoRoot per the bundle inclusion rules and returns the
// artifact list without writing anything to disk. Callers outside this package
// (e.g. manifestcheck) should use this instead of Run when no disk write is
// needed.
func Enumerate(repoRoot string) ([]embedded.Artifact, error) {
	return enumerate(repoRoot)
}

// enumerate is the internal no-write walker shared by Enumerate and Run.
func enumerate(repoRoot string) ([]embedded.Artifact, error) {
	var artifacts []embedded.Artifact

	// agents/atomic-*.md
	agentsDir := filepath.Join(repoRoot, "agents")
	entries, err := os.ReadDir(agentsDir)
	if err != nil {
		return nil, fmt.Errorf("read agents dir: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() || !bundlespec.MatchesAgent(e.Name()) {
			continue
		}
		src := filepath.Join(agentsDir, e.Name())
		target := "agents/" + e.Name()
		a, err := readArtifact(src, target, "agent")
		if err != nil {
			return nil, err
		}
		artifacts = append(artifacts, a)
	}

	// skills/atomic-*/** — full directory tree per matching skill.
	skillsDir := filepath.Join(repoRoot, "skills")
	skillEntries, err := os.ReadDir(skillsDir)
	if err != nil {
		return nil, fmt.Errorf("read skills dir: %w", err)
	}
	for _, e := range skillEntries {
		if !e.IsDir() || !bundlespec.MatchesSkillDir(e.Name()) {
			continue
		}
		skillRoot := filepath.Join(skillsDir, e.Name())
		if _, err := os.Stat(filepath.Join(skillRoot, "SKILL.md")); os.IsNotExist(err) {
			continue
		}
		err = filepath.WalkDir(skillRoot, func(path string, d fs.DirEntry, werr error) error {
			if werr != nil {
				return werr
			}
			if d.IsDir() {
				return nil
			}
			rel, err := filepath.Rel(repoRoot, path)
			if err != nil {
				return err
			}
			target := filepath.ToSlash(rel)
			a, err := readArtifact(path, target, "skill")
			if err != nil {
				return err
			}
			artifacts = append(artifacts, a)
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("walk skill %s: %w", e.Name(), err)
		}
	}

	// output-styles/atomic*.md
	outputStylesDir := filepath.Join(repoRoot, "output-styles")
	osEntries, err := os.ReadDir(outputStylesDir)
	if err != nil {
		return nil, fmt.Errorf("read output-styles dir: %w", err)
	}
	for _, e := range osEntries {
		if e.IsDir() || !bundlespec.MatchesOutputStyle(e.Name()) {
			continue
		}
		src := filepath.Join(outputStylesDir, e.Name())
		target := "output-styles/" + e.Name()
		a, err := readArtifact(src, target, "output-style")
		if err != nil {
			return nil, err
		}
		artifacts = append(artifacts, a)
	}

	// commands/*.md — every top-level markdown file ships. Subdirectories skipped.
	commandsDir := filepath.Join(repoRoot, "commands")
	cmdEntries, err := os.ReadDir(commandsDir)
	if err != nil {
		return nil, fmt.Errorf("read commands dir: %w", err)
	}
	for _, e := range cmdEntries {
		if e.IsDir() || !bundlespec.MatchesCommand(e.Name()) {
			continue
		}
		src := filepath.Join(commandsDir, e.Name())
		target := "commands/" + e.Name()
		a, err := readArtifact(src, target, "command")
		if err != nil {
			return nil, err
		}
		artifacts = append(artifacts, a)
	}

	// rules/**/*.md
	rulesDir := filepath.Join(repoRoot, "rules")
	err = filepath.WalkDir(rulesDir, func(path string, d fs.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		if d.IsDir() || !bundlespec.MatchesRule(path) {
			return nil
		}
		rel, err := filepath.Rel(repoRoot, path)
		if err != nil {
			return err
		}
		target := filepath.ToSlash(rel)
		a, err := readArtifact(path, target, "rule")
		if err != nil {
			return err
		}
		artifacts = append(artifacts, a)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk rules: %w", err)
	}

	// CLAUDE.md
	claudeMdSrc := filepath.Join(repoRoot, "CLAUDE.md")
	a, err := readArtifact(claudeMdSrc, "CLAUDE.md", "claude-md")
	if err != nil {
		return nil, err
	}
	artifacts = append(artifacts, a)

	// Stable sort: kind asc, then target asc.
	sort.Slice(artifacts, func(i, j int) bool {
		if artifacts[i].Kind != artifacts[j].Kind {
			return artifacts[i].Kind < artifacts[j].Kind
		}
		return artifacts[i].Target < artifacts[j].Target
	})

	return artifacts, nil
}

// readArtifact reads src, computes its SHA256, and returns an embedded.Artifact
// without writing anything to disk.
func readArtifact(src, target, kind string) (embedded.Artifact, error) {
	data, err := os.ReadFile(src)
	if err != nil {
		return embedded.Artifact{}, fmt.Errorf("read %s: %w", src, err)
	}
	return embedded.Artifact{
		Kind:   kind,
		Source: "bundle/" + target,
		Target: target,
		SHA256: SHA256Hex(data),
	}, nil
}

// Run walks repoRoot per the inclusion rules, copies matching files into
// outDir/bundle/<target-path>, and returns the artifact list.
func Run(repoRoot, outDir string) ([]Artifact, error) {
	bundleDir := filepath.Join(outDir, "bundle")
	if err := os.MkdirAll(bundleDir, 0o755); err != nil {
		return nil, fmt.Errorf("create bundle dir: %w", err)
	}

	embeds, err := enumerate(repoRoot)
	if err != nil {
		return nil, err
	}

	artifacts := make([]Artifact, 0, len(embeds))
	for _, ea := range embeds {
		a, err := mirrorFile(filepath.Join(repoRoot, filepath.FromSlash(ea.Target)), ea.Target, ea.Kind, bundleDir)
		if err != nil {
			return nil, err
		}
		artifacts = append(artifacts, a)
	}

	return artifacts, nil
}

// mirrorFile copies src into bundleDir/<target>, computes sha256, returns Artifact.
func mirrorFile(src, target, kind, bundleDir string) (Artifact, error) {
	dst := filepath.Join(bundleDir, filepath.FromSlash(target))
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return Artifact{}, fmt.Errorf("mkdir for %s: %w", target, err)
	}

	data, err := os.ReadFile(src)
	if err != nil {
		return Artifact{}, fmt.Errorf("read %s: %w", src, err)
	}

	if err := os.WriteFile(dst, data, 0o644); err != nil {
		return Artifact{}, fmt.Errorf("write %s: %w", dst, err)
	}

	return Artifact{
		Kind:   kind,
		Source: "bundle/" + target,
		Target: target,
		SHA256: SHA256Hex(data),
	}, nil
}

// SHA256Hex returns the hex-encoded SHA256 of data. Exported for tests.
func SHA256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
