// Package bundlemirror implements the artifact mirror logic behind
// internal/tools/bundle-mirror, split out so it is testable without main().
package bundlemirror

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"github.com/damusix/atomic-claude/atomic/internal/bundlespec"
	"github.com/damusix/atomic-claude/atomic/internal/templaterender"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"text/template"
)

// Artifact duplicates embedded.Artifact deliberately: internal/embedded carries
// the go:embed for bundle/, so importing it would make this generator
// unbuildable until the directory it exists to create already exists.
type Artifact struct {
	Kind   string
	Source string // path inside embedded FS, e.g. "bundle/agents/atomic-builder.md"
	Target string // path to write inside the target dir
	SHA256 string
}

// enumeratedArtifact retains Data from the enumeration read so Run can write
// the file without a second os.ReadFile.
type enumeratedArtifact struct {
	Artifact
	SrcPath string // absolute path of the source file on disk
	Data    []byte // file bytes read during enumeration; reused by Run to avoid a second read
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

// enumerate resolves every path under repoRoot/context/ and makes every Target
// relative to it, so the install tree is independent of the repo layout.
func enumerate(repoRoot string) ([]enumeratedArtifact, error) {
	var artifacts []enumeratedArtifact

	contextRoot := bundlespec.SourceRoot(repoRoot)

	// One pool for the whole walk; every templated artifact clones from it.
	partials, err := templaterender.LoadPartials(filepath.Join(contextRoot, templaterender.PartialsDir))
	if err != nil {
		return nil, err
	}

	agentsDir := filepath.Join(contextRoot, "agents")
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
		a, err := readArtifact(partials, src, target, "agent")
		if err != nil {
			return nil, err
		}
		artifacts = append(artifacts, a)
	}

	skillsDir := filepath.Join(contextRoot, "skills")
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
			rel, err := filepath.Rel(contextRoot, path)
			if err != nil {
				return err
			}
			target := filepath.ToSlash(rel)
			a, err := readArtifact(partials, path, target, "skill")
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

	outputStylesDir := filepath.Join(contextRoot, "output-styles")
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
		a, err := readArtifact(partials, src, target, "output-style")
		if err != nil {
			return nil, err
		}
		artifacts = append(artifacts, a)
	}

	commandsDir := filepath.Join(contextRoot, "commands")
	err = filepath.WalkDir(commandsDir, func(path string, d fs.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		if d.IsDir() || !bundlespec.MatchesCommand(d.Name()) {
			return nil
		}
		rel, err := filepath.Rel(contextRoot, path)
		if err != nil {
			return err
		}
		target := filepath.ToSlash(rel)
		a, err := readArtifact(partials, path, target, "command")
		if err != nil {
			return err
		}
		artifacts = append(artifacts, a)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk commands: %w", err)
	}

	rulesDir := filepath.Join(contextRoot, "rules")
	err = filepath.WalkDir(rulesDir, func(path string, d fs.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		if d.IsDir() || !bundlespec.MatchesRule(path) {
			return nil
		}
		rel, err := filepath.Rel(contextRoot, path)
		if err != nil {
			return err
		}
		target := filepath.ToSlash(rel)
		a, err := readArtifact(partials, path, target, "rule")
		if err != nil {
			return err
		}
		artifacts = append(artifacts, a)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk rules: %w", err)
	}

	claudeMdSrc := filepath.Join(contextRoot, "CLAUDE.md")
	a, err := readArtifact(partials, claudeMdSrc, "CLAUDE.md", "claude-md")
	if err != nil {
		return nil, err
	}
	artifacts = append(artifacts, a)

	sort.Slice(artifacts, func(i, j int) bool {
		if artifacts[i].Kind != artifacts[j].Kind {
			return artifacts[i].Kind < artifacts[j].Kind
		}
		return artifacts[i].Target < artifacts[j].Target
	})

	return artifacts, nil
}

// expandedKinds may compose a shared partial. Everything else is copied
// byte-for-byte: running a skill or rule through the engine would read a
// literal {{ in its prose as a directive.
var expandedKinds = map[string]bool{"command": true, "agent": true}

// readArtifact hashes the expanded bytes, not the source, because that is what
// installs — a parity check has to agree with the file the user ends up with.
func readArtifact(partials *template.Template, src, target, kind string) (enumeratedArtifact, error) {
	data, err := os.ReadFile(src)
	if err != nil {
		return enumeratedArtifact{}, fmt.Errorf("read %s: %w", src, err)
	}
	if expandedKinds[kind] {
		data, err = templaterender.Expand(partials, filepath.Base(src), data)
		if err != nil {
			return enumeratedArtifact{}, err
		}
	}
	return enumeratedArtifact{
		Artifact: Artifact{
			Kind:   kind,
			Source: "bundle/" + target,
			Target: target,
			SHA256: SHA256Hex(data),
		},
		SrcPath: src,
		Data:    data,
	}, nil
}

// Run mirrors every matching artifact into outDir/bundle/<target>.
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
		a, err := mirrorFile(ea.Data, ea.Target, ea.Kind, bundleDir)
		if err != nil {
			return nil, err
		}
		artifacts = append(artifacts, a)
	}

	return artifacts, nil
}

func mirrorFile(data []byte, target, kind, bundleDir string) (Artifact, error) {
	dst := filepath.Join(bundleDir, filepath.FromSlash(target))
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return Artifact{}, fmt.Errorf("mkdir for %s: %w", target, err)
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

// SHA256Hex is the manifest's checksum form.
func SHA256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
