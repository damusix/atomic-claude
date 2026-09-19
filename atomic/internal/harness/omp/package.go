package omp

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/artifacts"
	"github.com/damusix/atomic-claude/atomic/internal/frontmatter"
	"github.com/damusix/atomic-claude/atomic/internal/harness"
	"github.com/damusix/atomic-claude/atomic/internal/managedfile"
	"github.com/damusix/atomic-claude/atomic/internal/rules"
)

// SkeletonPath is the extension entry point the package publishes. OMP
// discovers an extension as a TypeScript module under an agent root's
// extensions/ directory (CP0); the package carries the module, while
// registering and driving it are surfaces CP0 left unproven.
const SkeletonPath = "extensions/atomic.ts"

// portableCommandKeys are the command frontmatter keys the package carries as
// its own metadata. Any other canonical key is reported unsupported rather than
// copied with an implied native claim.
var portableCommandKeys = map[string]bool{"description": true}

// unparsedFrontmatter is the unsupported marker for authored frontmatter that is
// not valid YAML: no key could be classified, so the file ships as authored and
// the frontmatter is reported rather than silently rewritten.
const unparsedFrontmatter = "frontmatter (unparsed)"

// PackageFile is one file in the generated package tree.
type PackageFile struct {
	// Path is the package-relative path, slash-separated.
	Path  string `json:"path"`
	Bytes []byte `json:"-"`
	// Source is the canonical artifact identity the file was projected from;
	// empty for the extension entry point.
	Source string `json:"source,omitempty"`
	// Unsupported names the canonical fields the file could not carry natively.
	Unsupported []string `json:"unsupported,omitempty"`
	// Tier is the enforcement tier the CP0 record proves for this file.
	Tier artifacts.EnforcementTier `json:"tier"`
	// Digest is the digest of Bytes.
	Digest string `json:"digest"`
}

// PackageGap is one native package surface CP0 could not prove, with the
// evidence that fixes it.
type PackageGap struct {
	Surface  string `json:"surface"`
	Evidence string `json:"evidence"`
}

// Package is one generated OMP package: the tree the selected generation
// publishes, its deterministic content identity, and every native surface it
// cannot promise.
type Package struct {
	// Generation is the package's content identity: one line per projected file
	// in path order, so two profiles must agree on it before they can share one
	// published package.
	Generation string        `json:"generation"`
	Files      []PackageFile `json:"files"`
	// Gaps are the CP0 capability rows the package cannot promise, in stable
	// role order.
	Gaps []harness.Capability `json:"gaps,omitempty"`
	// Unproven names the native package surfaces with no CP0 observation.
	Unproven []PackageGap `json:"unproven,omitempty"`
}

// BuildPackage projects the canonical corpus into the OMP package tree. It is
// offline and deterministic: the same corpus produces identical bytes and an
// identical generation, and no native write happens here. Unproven metadata
// never enters the tree — a canonical field the CP0 record does not prove is
// dropped and reported — and the report names every native surface the package
// cannot promise.
func BuildPackage(cat *artifacts.Catalog, m harness.CapabilityMatrix) (Package, error) {
	if m.Harness != harness.KindOMP {
		return Package{}, fmt.Errorf("omp: package generation needs the OMP capability record, got %q", m.Harness)
	}

	pkg := Package{
		Gaps:     append(harness.SkillGaps(m), harness.RuleGaps(m)...),
		Unproven: packageGaps(),
	}

	if err := pkg.addCommands(cat); err != nil {
		return Package{}, err
	}
	if err := pkg.addAgents(cat); err != nil {
		return Package{}, err
	}
	if err := pkg.addSkills(cat, m); err != nil {
		return Package{}, err
	}
	if err := pkg.addRules(cat, m); err != nil {
		return Package{}, err
	}
	pkg.Files = append(pkg.Files, PackageFile{
		Path:   SkeletonPath,
		Bytes:  []byte(extensionSkeleton),
		Tier:   artifacts.EnforcementUnsupported,
		Digest: artifacts.ProjectionDigest([]byte(extensionSkeleton)),
	})

	sort.Slice(pkg.Files, func(i, j int) bool { return pkg.Files[i].Path < pkg.Files[j].Path })
	for _, f := range pkg.Files {
		if !ProviderID(string(f.Bytes)) {
			continue
		}
		identity := f.Source
		if identity == "" {
			identity = f.Path
		}
		return Package{}, fmt.Errorf("omp: %s carries a concrete provider or model identifier; Atomic ships semantic preferences only", identity)
	}
	for i := 1; i < len(pkg.Files); i++ {
		if pkg.Files[i].Path == pkg.Files[i-1].Path {
			return Package{}, fmt.Errorf("omp: two artifacts project to the package path %s", pkg.Files[i].Path)
		}
	}
	pkg.Generation = packageGeneration(pkg.Files)
	return pkg, nil
}

// addCommands projects every canonical command into the package's command tree.
// OMP's command discovery is unproven, so only the portable description travels
// as native metadata; the body ships verbatim. The shipped corpus tolerates
// authored frontmatter that is not valid YAML, so such a file ships as authored
// with the unparsed frontmatter reported rather than failing the package.
func (p *Package) addCommands(cat *artifacts.Catalog) error {
	for _, a := range cat.OfKind(artifacts.KindCommand) {
		kvs, body, err := frontmatter.ParseOrdered(string(a.Body))
		if err != nil {
			p.Files = append(p.Files, PackageFile{
				Path:        a.Source,
				Bytes:       a.Body,
				Source:      a.ID,
				Unsupported: []string{unparsedFrontmatter},
				Tier:        artifacts.EnforcementUnsupported,
				Digest:      artifacts.ProjectionDigest(a.Body),
			})
			continue
		}
		var fields []frontmatter.KV
		var unsupported []string
		for _, kv := range kvs {
			if portableCommandKeys[kv.Key] {
				fields = append(fields, kv)
				continue
			}
			unsupported = append(unsupported, kv.Key)
		}
		doc, err := frontmatter.EmitOrdered(fields, body)
		if err != nil {
			return fmt.Errorf("omp: project %s: %w", a.ID, err)
		}
		p.Files = append(p.Files, PackageFile{
			Path:        a.Source,
			Bytes:       []byte(doc),
			Source:      a.ID,
			Unsupported: unsupported,
			Tier:        artifacts.EnforcementUnsupported,
			Digest:      artifacts.ProjectionDigest([]byte(doc)),
		})
	}
	return nil
}

// addAgents projects every canonical agent through the shared OMP agent
// projection, so the package and the offline projection gate agree byte for
// byte on what OMP receives.
func (p *Package) addAgents(cat *artifacts.Catalog) error {
	for _, a := range cat.OfKind(artifacts.KindAgent) {
		projection, err := harness.OMPAgent(cat, a)
		if err != nil {
			return err
		}
		p.Files = append(p.Files, PackageFile{
			Path:        projection.Path,
			Bytes:       projection.Bytes,
			Source:      a.ID,
			Unsupported: projection.Unsupported,
			Tier:        projection.Enforcement,
			Digest:      projection.Digest,
		})
	}
	return nil
}

// addSkills projects the canonical skill corpus through the shared OMP skill
// projection. A collision or an unresolvable relative reference refuses the
// package: a skill tree that cannot resolve its own files must not ship.
func (p *Package) addSkills(cat *artifacts.Catalog, m harness.CapabilityMatrix) error {
	report, err := harness.ProjectSkills(cat, artifacts.TargetOMP, harness.SkillPolicy{}, m)
	if err != nil {
		return err
	}
	if len(report.Collisions) > 0 {
		return fmt.Errorf("omp: skill projections collide on %s", report.Collisions[0].Path)
	}
	if len(report.Reference) > 0 {
		gap := report.Reference[0]
		return fmt.Errorf("omp: skill %s file %s references %s, which the shipped skill tree does not carry", gap.Skill, gap.File, gap.Ref)
	}
	for _, file := range report.Files {
		p.Files = append(p.Files, PackageFile{
			Path:        file.Projection.Path,
			Bytes:       file.Projection.Bytes,
			Source:      file.Artifact,
			Unsupported: file.Projection.Unsupported,
			Tier:        file.Projection.Enforcement,
			Digest:      file.Projection.Digest,
		})
	}
	return nil
}

// addRules ships every actual path-scoped rule into the package. Steering and
// output styles are never rules, so only context/rules/** enters this surface.
// The record is parsed and validated for its canonical identity and digest, and
// the tier comes from the shared selector: OMP proves a pre-operation event but
// not a context return, so a shipped rule carries no native scope claim.
func (p *Package) addRules(cat *artifacts.Catalog, m harness.CapabilityMatrix) error {
	evidence := harness.RuleEvidence(m)
	var records []rules.RuleRecord
	for _, a := range cat.OfKind(artifacts.KindRule) {
		record, err := rules.ParseShipped(a.Source, a.Body)
		if err != nil {
			return fmt.Errorf("omp: parse %s: %w", a.ID, err)
		}
		projection := rules.Select(record, artifacts.TargetOMP, evidence)
		p.Files = append(p.Files, PackageFile{
			Path:   a.Source,
			Bytes:  a.Body,
			Source: a.ID,
			Tier:   projection.Tier,
			Digest: artifacts.ProjectionDigest(a.Body),
		})
		records = append(records, record)
	}
	if len(records) > 0 {
		if err := rules.Validate(records); err != nil {
			return err
		}
	}
	return nil
}

// Render writes the package tree into dir. The caller owns dir; every file is
// written with the mode the generated-tree identity records.
func (p Package) Render(dir string) error {
	for _, f := range p.Files {
		if err := safePackagePath(f.Path); err != nil {
			return err
		}
		path := filepath.Join(dir, filepath.FromSlash(f.Path))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return fmt.Errorf("omp: mkdir for %s: %w", f.Path, err)
		}
		if err := os.WriteFile(path, f.Bytes, 0o644); err != nil {
			return fmt.Errorf("omp: write %s: %w", f.Path, err)
		}
	}
	return nil
}

// TreeDigest renders the package into a scratch directory and digests the
// result with the same generated-tree identity the publication path checks, so
// a plan-time digest cannot drift from the published tree.
func (p Package) TreeDigest() (string, error) {
	dir, err := os.MkdirTemp("", "atomic-omp-package-")
	if err != nil {
		return "", fmt.Errorf("omp: stage package for digest: %w", err)
	}
	defer os.RemoveAll(dir)
	if err := p.Render(dir); err != nil {
		return "", err
	}
	digest, _, err := managedfile.TreeDigest(dir)
	return digest, err
}

// Paths returns the package's file paths in order.
func (p Package) Paths() []string {
	out := make([]string, 0, len(p.Files))
	for _, f := range p.Files {
		out = append(out, f.Path)
	}
	return out
}

// packageGeneration is the package's content identity: each projected file's
// path and digest in path order, so identical content always produces an
// identical generation and a change anywhere changes it.
func packageGeneration(files []PackageFile) string {
	h := sha256.New()
	for _, f := range files {
		h.Write([]byte(f.Path))
		h.Write([]byte{0})
		h.Write([]byte(f.Digest))
		h.Write([]byte{'\n'})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// safePackagePath rejects a package path that is absolute or escapes the tree.
func safePackagePath(path string) error {
	if path == "" || strings.HasPrefix(path, "/") || strings.Contains(path, "\\") {
		return fmt.Errorf("omp: package path %q is not a relative slash path", path)
	}
	clean := filepath.ToSlash(filepath.Clean(path))
	if clean != path || strings.HasPrefix(clean, "../") || clean == ".." {
		return fmt.Errorf("omp: package path %q escapes the package tree", path)
	}
	return nil
}

// packageGaps are the OMP package surfaces CP0 left unsupported: no package was
// registered, so installation, shared visibility, project scope, uninstall,
// and cleanup have no observation.
func packageGaps() []PackageGap {
	return []PackageGap{
		{Surface: "package install and lifecycle", Evidence: "omp plugin list returned empty npm and marketplace arrays; no package was registered"},
		{Surface: "shared-package visibility", Evidence: "no package was registered, so visibility to an unenrolled profile is unobserved"},
		{Surface: "project installation scope", Evidence: "no package was registered"},
		{Surface: "package uninstall and cleanup", Evidence: "no package was registered"},
		{Surface: "extension disablement", Evidence: "--no-extensions was never run; replay target absent"},
		{Surface: "runtime delivery", Evidence: "no matched-body context return was observed; runtime delivery lands with its own checkpoint"},
	}
}

// extensionSkeleton is the extension module the package publishes. It is the
// CP0-proven module shape — a default-exported factory receiving the
// ExtensionAPI — and nothing else: no event is registered, because OMP
// registered no package in CP0 and runtime delivery is a separate checkpoint.
const extensionSkeleton = `// Atomic's OMP extension entry point. OMP discovers an extension as a
// TypeScript module whose default export receives the ExtensionAPI.
import type { ExtensionAPI } from "@oh-my-pi/pi-coding-agent";

export default function atomic(_pi: ExtensionAPI): void {}
`
