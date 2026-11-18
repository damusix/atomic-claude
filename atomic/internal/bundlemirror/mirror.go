// Package bundlemirror implements the artifact mirror logic behind
// internal/tools/bundle-mirror, split out so it is testable without main().
package bundlemirror

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/damusix/atomic-claude/atomic/internal/artifacts"
	"github.com/damusix/atomic-claude/atomic/internal/bundlespec"
)

// Artifact duplicates embedded.Artifact deliberately: internal/embedded carries
// the go:embed for bundle/, so importing it would make this generator
// unbuildable until the directory it exists to create already exists.
type Artifact struct {
	Kind string
	// Source is the path inside the embedded FS, e.g. "bundle/agents/atomic-builder.md".
	Source string
	// Target is the path to write inside the target dir.
	Target string
	// Canonical is the context/-relative authored source. It differs from
	// Target only for the global steering source.
	Canonical string
	SHA256    string
}

// enumeratedArtifact retains Data from the enumeration read so Run can write
// the file without a second read.
type enumeratedArtifact struct {
	Artifact
	Data []byte // projected bytes; reused by Run to avoid a second render
}

// Enumerate is Run without the disk write — what manifestcheck uses.
func Enumerate(repoRoot string) ([]Artifact, error) {
	items, err := enumerate(repoRoot)
	if err != nil {
		return nil, err
	}
	out := make([]Artifact, len(items))
	for i, it := range items {
		out[i] = it.Artifact
	}
	return out, nil
}

// enumerate renders the canonical corpus once and maps it to the Claude-native
// files the embedded bundle carries.
func enumerate(repoRoot string) ([]enumeratedArtifact, error) {
	catalog, err := artifacts.Load(repoRoot)
	if err != nil {
		return nil, err
	}

	artifactsOut := make([]enumeratedArtifact, 0, len(catalog.Artifacts))
	for _, a := range catalog.Artifacts {
		target := claudeTarget(a)
		artifactsOut = append(artifactsOut, enumeratedArtifact{
			Artifact: Artifact{
				Kind:      installerKind(a.Kind),
				Source:    "bundle/" + target,
				Target:    target,
				Canonical: a.Source,
				SHA256:    SHA256Hex(a.Body),
			},
			Data: a.Body,
		})
	}

	sort.Slice(artifactsOut, func(i, j int) bool {
		if artifactsOut[i].Kind != artifactsOut[j].Kind {
			return artifactsOut[i].Kind < artifactsOut[j].Kind
		}
		return artifactsOut[i].Target < artifactsOut[j].Target
	})

	return artifactsOut, nil
}

// claudeTarget maps a canonical artifact to its Claude-native path; only the global
// steering source leaves its authored layout, becoming CLAUDE.md.
func claudeTarget(a artifacts.Artifact) string {
	if a.Kind == artifacts.KindSteering {
		return bundlespec.GlobalSteering.ClaudeTarget
	}
	return a.Source
}

// installerKind maps a canonical kind to the kind claudeinstall switches on.
func installerKind(kind artifacts.Kind) string {
	if kind == artifacts.KindSteering {
		return "claude-md"
	}
	return string(kind)
}

// Run replaces outDir/bundle with every matching artifact at bundle/<target>.
func Run(repoRoot, outDir string) ([]Artifact, error) {
	if outDir == "" {
		return nil, fmt.Errorf("empty output dir")
	}
	bundleDir := filepath.Join(outDir, "bundle")

	embeds, err := enumerate(repoRoot)
	if err != nil {
		return nil, err
	}

	if err := os.RemoveAll(bundleDir); err != nil {
		return nil, fmt.Errorf("clear bundle dir: %w", err)
	}
	if err := os.MkdirAll(bundleDir, 0o755); err != nil {
		return nil, fmt.Errorf("create bundle dir: %w", err)
	}

	out := make([]Artifact, 0, len(embeds))
	for _, ea := range embeds {
		dst := filepath.Join(bundleDir, filepath.FromSlash(ea.Target))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return nil, fmt.Errorf("mkdir for %s: %w", ea.Target, err)
		}
		if err := os.WriteFile(dst, ea.Data, 0o644); err != nil {
			return nil, fmt.Errorf("write %s: %w", dst, err)
		}
		out = append(out, ea.Artifact)
	}

	return out, nil
}

// SHA256Hex is the manifest's checksum form.
func SHA256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
