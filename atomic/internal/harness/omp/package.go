package omp

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
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

// SkeletonPath is the extension entry point the package publishes. OMP discovers
// an extension as a TypeScript module under an agent root's extensions/ directory.
const SkeletonPath = "extensions/atomic.ts"

// portableCommandKeys are the command frontmatter keys the package carries. Any
// other key is reported unsupported rather than copied with an implied native claim.
var portableCommandKeys = map[string]bool{"description": true}

// unparsedFrontmatter marks authored frontmatter that is not valid YAML; the
// file ships as authored rather than being silently rewritten.
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
	// Tier is the enforcement tier the capability record proves for this file.
	Tier artifacts.EnforcementTier `json:"tier"`
	// Scope is the selected native scope mapping. It is empty whenever no
	// proven role maps the file, which is every shipped rule for OMP 18.1.18.
	Scope []rules.ScopeField `json:"scope,omitempty"`
	// Delivery is how OMP would supply a matched rule body at runtime. It is
	// empty for every non-rule file.
	Delivery rules.RuntimeDelivery `json:"delivery,omitempty"`
	// Digest is the digest of Bytes.
	Digest string `json:"digest"`
}

// PackageGap is one native package surface no probe could prove, with the
// evidence that fixes it.
type PackageGap struct {
	Surface  string `json:"surface"`
	Evidence string `json:"evidence"`
}

// Package is one generated OMP package: the tree the selected generation
// publishes and every native surface it cannot promise.
type Package struct {
	// Generation is the package's content identity; two profiles must agree on
	// it before they can share one published package.
	Generation string        `json:"generation"`
	Files      []PackageFile `json:"files"`
	// Gaps are the capability rows the package cannot promise, in stable
	// role order.
	Gaps []harness.Capability `json:"gaps,omitempty"`
	// Unproven names the native package surfaces with no runtime observation.
	Unproven []PackageGap `json:"unproven,omitempty"`
	// Runtime is the runtime delivery the package's extension module carries.
	Runtime SessionDelivery `json:"runtime"`
}

// BuildPackage projects the canonical corpus into the OMP package tree, offline
// and deterministically. deny is the extension module's exact deny predicates.
func BuildPackage(cat *artifacts.Catalog, m harness.CapabilityMatrix, deny []DenyPredicate) (Package, error) {
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
	if err := pkg.addSkills(cat, m); err != nil {
		return Package{}, err
	}
	sources, err := ShippedRuleSources(cat)
	if err != nil {
		return Package{}, err
	}
	report, err := projectShippedRules(sources, m)
	if err != nil {
		return Package{}, err
	}
	if err := pkg.addRules(cat, report); err != nil {
		return Package{}, err
	}
	delivery, err := BuildSessionDelivery(sources, m, deny)
	if err != nil {
		return Package{}, err
	}
	module, err := delivery.RenderExtension()
	if err != nil {
		return Package{}, err
	}
	pkg.Runtime = delivery
	pkg.Files = append(pkg.Files, PackageFile{
		Path:   SkeletonPath,
		Bytes:  module,
		Tier:   Tier,
		Digest: artifacts.ProjectionDigest(module),
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
// Invalid authored YAML ships as authored and is reported, not a package failure.
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

// addSkills projects the skill corpus into the package. A collision or an
// unresolvable relative reference refuses it: such a tree must not ship.
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

// addRules ships every path-scoped rule through the shared OMP projection, so the
// package and the offline projection gate agree byte for byte.
func (p *Package) addRules(cat *artifacts.Catalog, report ShippedRuleReport) error {
	identity := make(map[string]string)
	for _, a := range cat.OfKind(artifacts.KindRule) {
		identity[a.Source] = a.ID
	}
	for _, rule := range report.Rules {
		p.Files = append(p.Files, PackageFile{
			Path:     rule.Source,
			Bytes:    rule.Bytes,
			Source:   identity[rule.Source],
			Tier:     rule.Tier,
			Scope:    rule.Scope,
			Delivery: rule.Delivery,
			Digest:   rule.Digest,
		})
	}
	return nil
}

// Render writes the package tree into dir. Modes are pinned after writing because
// the umask masks os.WriteFile, and a different umask would read the tree as stale.
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
	return chmodTree(dir)
}

// chmodTree pins every node's permission bits — directories to 0755, files to
// 0644 — so the tree digest is umask-independent.
func chmodTree(dir string) error {
	return filepath.WalkDir(dir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == dir {
			return nil
		}
		mode := os.FileMode(0o644)
		if d.IsDir() {
			mode = 0o755
		}
		if err := os.Chmod(path, mode); err != nil {
			return fmt.Errorf("omp: chmod %s: %w", path, err)
		}
		return nil
	})
}

// TreeDigest renders the package into a scratch directory and digests it the way
// publication does, so a plan-time digest cannot drift from the published tree.
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

// ExtensionModule returns the package's runtime delivery module, the same bytes
// a profile's agent root receives.
func (p Package) ExtensionModule() ([]byte, error) {
	for _, f := range p.Files {
		if f.Path == SkeletonPath {
			return f.Bytes, nil
		}
	}
	return nil, fmt.Errorf("omp: package carries no %s module", SkeletonPath)
}

// Paths returns the package's file paths in order.
func (p Package) Paths() []string {
	out := make([]string, 0, len(p.Files))
	for _, f := range p.Files {
		out = append(out, f.Path)
	}
	return out
}

// packageGeneration is the package's content identity over each file's path and
// digest in path order.
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

// packageGaps are the OMP package surfaces left unsupported. Runtime-delivery
// gaps belong to SessionDelivery.Unproven alone and are not repeated here.
func packageGaps() []PackageGap {
	return []PackageGap{
		{Surface: "package install, registration, and lifecycle", Evidence: "omp plugin list --json returned empty npm and marketplace arrays; no package was registered"},
		{Surface: "package command and skill discovery", Evidence: "CP0 proved extension discovery under the agent root only; no command or skill was observed loading from a package"},
		{Surface: "shared-package visibility", Evidence: "no package was registered, so visibility to an unenrolled profile is unobserved"},
		{Surface: "project installation scope", Evidence: "no package was registered"},
		{Surface: "package uninstall and cleanup", Evidence: "no package was registered"},
	}
}
