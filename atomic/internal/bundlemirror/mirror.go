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
	"strings"
)

// Artifact describes one file in the embedded manifest.
type Artifact struct {
	Kind   string
	Source string // path inside embedded FS, e.g. "bundle/agents/atomic-builder.md"
	Target string // path to write inside the target dir
	SHA256 string
}

// CommandAllowlist is the explicit set of command filenames to include.
// Commands are NOT included by prefix — this is the canonical list.
var CommandAllowlist = map[string]bool{
	"atomic-compress.md":         true,
	"atomic-plan.md":             true,
	"atomic-setup.md":            true,
	"commit-and-merge.md":        true,
	"commit-and-pr.md":           true,
	"commit-and-squash.md":       true,
	"commit-only.md":             true,
	"documentation.md":           true,
	"git-cleanup.md":             true,
	"merge-to-main.md":           true,
	"pr-only.md":                 true,
	"refresh-signals.md":         true,
	"report-issue.md":            true,
	"squash-and-merge.md":        true,
	"squash-only.md":             true,
	"subagent-implementation.md": true,
	"worktree-start.md":          true,
}

// Run walks repoRoot per the inclusion rules, copies matching files into
// outDir/bundle/<target-path>, and returns the artifact list.
func Run(repoRoot, outDir string) ([]Artifact, error) {
	bundleDir := filepath.Join(outDir, "bundle")
	if err := os.MkdirAll(bundleDir, 0o755); err != nil {
		return nil, fmt.Errorf("create bundle dir: %w", err)
	}

	var artifacts []Artifact

	// agents/atomic-*.md
	agentsDir := filepath.Join(repoRoot, "agents")
	entries, err := os.ReadDir(agentsDir)
	if err != nil {
		return nil, fmt.Errorf("read agents dir: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), "atomic-") || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		src := filepath.Join(agentsDir, e.Name())
		target := "agents/" + e.Name()
		a, err := mirrorFile(src, target, "agent", bundleDir)
		if err != nil {
			return nil, err
		}
		artifacts = append(artifacts, a)
	}

	// skills/atomic-*/SKILL.md
	skillsDir := filepath.Join(repoRoot, "skills")
	skillEntries, err := os.ReadDir(skillsDir)
	if err != nil {
		return nil, fmt.Errorf("read skills dir: %w", err)
	}
	for _, e := range skillEntries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), "atomic-") {
			continue
		}
		skillFile := filepath.Join(skillsDir, e.Name(), "SKILL.md")
		if _, err := os.Stat(skillFile); os.IsNotExist(err) {
			continue
		}
		target := "skills/" + e.Name() + "/SKILL.md"
		a, err := mirrorFile(skillFile, target, "skill", bundleDir)
		if err != nil {
			return nil, err
		}
		artifacts = append(artifacts, a)
	}

	// output-styles/atomic*.md
	outputStylesDir := filepath.Join(repoRoot, "output-styles")
	osEntries, err := os.ReadDir(outputStylesDir)
	if err != nil {
		return nil, fmt.Errorf("read output-styles dir: %w", err)
	}
	for _, e := range osEntries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), "atomic") || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		src := filepath.Join(outputStylesDir, e.Name())
		target := "output-styles/" + e.Name()
		a, err := mirrorFile(src, target, "output-style", bundleDir)
		if err != nil {
			return nil, err
		}
		artifacts = append(artifacts, a)
	}

	// commands/ (explicit allowlist)
	commandsDir := filepath.Join(repoRoot, "commands")
	cmdEntries, err := os.ReadDir(commandsDir)
	if err != nil {
		return nil, fmt.Errorf("read commands dir: %w", err)
	}
	for _, e := range cmdEntries {
		if e.IsDir() || !CommandAllowlist[e.Name()] {
			continue
		}
		src := filepath.Join(commandsDir, e.Name())
		target := "commands/" + e.Name()
		a, err := mirrorFile(src, target, "command", bundleDir)
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
		if d.IsDir() || !strings.HasSuffix(path, ".md") {
			return nil
		}
		rel, err := filepath.Rel(repoRoot, path)
		if err != nil {
			return err
		}
		target := filepath.ToSlash(rel) // e.g. rules/python/style.md
		a, err := mirrorFile(path, target, "rule", bundleDir)
		if err != nil {
			return err
		}
		artifacts = append(artifacts, a)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk rules: %w", err)
	}

	// claude.md → CLAUDE.md
	claudeMdSrc := filepath.Join(repoRoot, "claude.md")
	a, err := mirrorFile(claudeMdSrc, "CLAUDE.md", "claude-md", bundleDir)
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
