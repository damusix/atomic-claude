package realmshare

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/harness"
)

type artifactAttestation struct {
	Logical string          `toml:"logical"`
	Harness harness.Kind    `toml:"harness"`
	Link    LinkReceipt     `toml:"link"`
	Version artifactVersion `toml:"version"`
}

type artifactExecutable struct {
	Launcher string `toml:"launcher"`
	Path     string `toml:"path"`
	Identity string `toml:"identity"`
}

type artifactVersion struct {
	Root         string             `toml:"root"`
	PhysicalRoot string             `toml:"physical_root"`
	Executable   artifactExecutable `toml:"executable"`
	Version      string             `toml:"version"`
}

func artifactExecutableEvidence(loc bindingLocation, kind harness.Kind) (artifactExecutable, error) {
	launcher, err := exec.LookPath(string(kind))
	if err != nil || !filepath.IsAbs(launcher) {
		return artifactExecutable{}, fmt.Errorf("the enrolled harness executable is not available on an absolute PATH entry")
	}
	launcher = filepath.Clean(launcher)
	parent, err := filepath.EvalSymlinks(filepath.Dir(launcher))
	if err != nil {
		return artifactExecutable{}, fmt.Errorf("cannot resolve the harness executable directory")
	}
	physical, err := filepath.EvalSymlinks(launcher)
	if err != nil {
		return artifactExecutable{}, fmt.Errorf("cannot resolve the harness executable")
	}
	roots := [6]string{loc.realm, loc.physicalRealm}
	for i, namespace := range [...]string{"realms", cloneProofDir} {
		roots[2+2*i] = filepath.Join(config.Dir(loc.home), namespace)
		roots[3+2*i] = filepath.Join(config.Dir(loc.physicalHome), namespace)
	}
	var forbidden [len(roots)]artifactNamespace
	for i, root := range roots {
		forbidden[i], err = readArtifactNamespace(root)
		if err != nil {
			return artifactExecutable{}, fmt.Errorf("cannot resolve executable exclusion namespace %q: %w", root, err)
		}
	}
	for _, name := range [...]string{launcher, filepath.Join(parent, filepath.Base(launcher)), physical} {
		namespace, err := readArtifactNamespace(name)
		if err != nil {
			return artifactExecutable{}, fmt.Errorf("cannot resolve harness executable namespace %q: %w", name, err)
		}
		for _, root := range forbidden {
			if root.contains(namespace, "") || namespace.contains(root, "") {
				return artifactExecutable{}, fmt.Errorf("the effective harness executable is inside a realm or managed checkout; use an independently installed executable")
			}
		}
	}
	info, err := os.Lstat(physical)
	if err != nil || !info.Mode().IsRegular() {
		return artifactExecutable{}, fmt.Errorf("cannot identify the regular harness executable")
	}
	file, err := os.Open(physical)
	if err != nil {
		return artifactExecutable{}, fmt.Errorf("cannot open the harness executable for identification")
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) || info.Size() != opened.Size() || !info.ModTime().Equal(opened.ModTime()) || info.Mode() != opened.Mode() {
		return artifactExecutable{}, fmt.Errorf("the harness executable changed while checking its identity")
	}
	id, err := restorationIdentity(file)
	if err != nil {
		return artifactExecutable{}, fmt.Errorf("cannot establish a durable harness executable identity: %w", err)
	}
	identity := localDigest(fmt.Appendf(nil, "%s %d %d %d", id, opened.Size(), opened.ModTime().UnixNano(), opened.Mode()))
	return artifactExecutable{Launcher: launcher, Path: physical, Identity: identity}, nil
}

func probeArtifactVersion(ctx context.Context, loc bindingLocation, kind harness.Kind, before artifactVersion, version string) (artifactVersion, error) {
	expected := version
	switch kind {
	case harness.KindClaude:
		expected += " (Claude Code)"
	case harness.KindCodex:
		expected = "codex-cli " + expected
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	var output artifactVersionOutput
	cmd := exec.CommandContext(ctx, before.Executable.Path, "--version")
	cmd.Dir = filepath.VolumeName(before.Executable.Path) + string(filepath.Separator)
	// Keep project settings, inherited loader options, and PATH helpers out of the probe.
	cmd.Env = []string{"PATH=", "OMP_NO_UPDATE_CHECK=1"}
	cmd.Stdout = &output
	cmd.WaitDelay = 250 * time.Millisecond
	if err := cmd.Run(); err != nil || strings.TrimSpace(string(output.data[:output.n])) != expected {
		return artifactVersion{}, fmt.Errorf("the installed executable did not report the exact tested version %s", version)
	}
	after, err := artifactExecutableEvidence(loc, kind)
	physicalRoot, rootErr := artifactClonePath(before.Root, 0)
	if err != nil || rootErr != nil || after != before.Executable || physicalRoot != before.PhysicalRoot {
		return artifactVersion{}, fmt.Errorf("the effective harness executable or profile changed during version review")
	}
	before.Version = version
	return before, nil
}

type artifactVersionOutput struct {
	data [1024]byte
	n    int
}

func (out *artifactVersionOutput) Write(p []byte) (int, error) {
	if len(p) > len(out.data)-out.n {
		return 0, fmt.Errorf("harness version output exceeds review limit")
	}
	out.n += copy(out.data[out.n:], p)
	return len(p), nil
}

func (l *localLinks) checkArtifactExecutable(instance *artifactInstance, kind harness.Kind) {
	if instance.checked || instance.unavailable != "" {
		return
	}
	instance.checked = true
	executable, err := artifactExecutableEvidence(l.loc, kind)
	if err != nil {
		instance.unavailable = err.Error() + "."
		return
	}
	physicalRoot, err := artifactClonePath(instance.root, 0)
	if err != nil {
		instance.unavailable = "Cannot resolve the effective harness profile: " + err.Error()
		return
	}
	instance.version = artifactVersion{Root: instance.root, PhysicalRoot: physicalRoot, Executable: executable}
}

func (l *localLinks) reviewArtifactVersion(ctx context.Context, instance *artifactInstance, effect *artifactEffect) {
	l.checkArtifactExecutable(instance, effect.Harness)
	if instance.unavailable != "" {
		effect.State, effect.Detail = "unavailable", instance.unavailable+" "+effect.Detail
		return
	}
	if !instance.probed {
		instance.probed = true
		instance.version, instance.probeErr = probeArtifactVersion(ctx, l.loc, effect.Harness, instance.version, effect.Version)
	}
	if instance.probeErr != nil {
		effect.State, effect.Detail = "unavailable", instance.probeErr.Error()+". "+effect.Detail
		return
	}
	if instance.version.Version != effect.Version {
		effect.State, effect.Detail = "unverified", "The installed version does not match this native capability. "+effect.Detail
		return
	}
	effect.VersionEvidence = instance.version
	effect.Detail = fmt.Sprintf("Verified executable %q. %s", instance.version.Executable.Path, effect.Detail)
}

func (l *localLinks) reportOwnedArtifact(mapping Mapping, link LinkReceipt, preview bool, instance *artifactInstance, effect *artifactEffect) {
	index := slices.IndexFunc(l.binding.NativeVersions, func(receipt artifactAttestation) bool {
		return receipt.Logical == mapping.Destination && receipt.Harness == effect.Harness && receipt.Link == link
	})
	if preview {
		if index >= 0 && l.binding.NativeVersions[index].Version == effect.VersionEvidence {
			effect.State = "active"
		}
		return
	}
	if index < 0 {
		effect.State, effect.Detail = "unverified", "No native version evidence was recorded for this link. Review and link to verify native delivery."
		return
	}
	l.checkArtifactExecutable(instance, effect.Harness)
	if instance.unavailable != "" {
		effect.State, effect.Detail = "unavailable", instance.unavailable+" The owned link is retained."
		return
	}
	current := instance.version
	current.Version = effect.Version
	if current != l.binding.NativeVersions[index].Version {
		effect.State, effect.Detail = "drifted", "The installed executable, profile, or tested version changed after approval. The owned link is retained; review and link again."
		return
	}
	effect.State = "active"
}

func approvedArtifactVersions(b Binding, changes []localChange, reviewed []linkSelectionReview) []artifactAttestation {
	var out []artifactAttestation
	for _, selected := range reviewed {
		for _, effect := range selected.Effects {
			if effect.VersionEvidence.Version == "" || (effect.State != "ready" && effect.State != "active" && effect.State != "pending") {
				continue
			}
			logical := selected.Mapping.Destination
			link := effect.Owned
			if effect.Path == logical {
				link = findArtifactBacking(b, changes, logical, selected.Mapping.Source)
			} else if link.Path == "" {
				i := slices.IndexFunc(changes, func(change localChange) bool {
					return change.Artifact == logical && change.Link.Path == effect.Path
				})
				if i >= 0 {
					link = changes[i].Link
				}
			}
			if link.Path != "" {
				out = append(out, artifactAttestation{Logical: logical, Harness: effect.Harness, Link: link, Version: effect.VersionEvidence})
			}
		}
	}
	return out
}

func (l *localLinks) recordArtifactVersions(b *Binding, approved []artifactAttestation) (bool, error) {
	changed := false
	for _, receipt := range approved {
		logical := findArtifactBacking(*b, nil, receipt.Logical, receipt.Link.Source)
		if _, err := localLinkWitness(l.realm, l.state, *b, logical); err != nil {
			return false, err
		}
		if logical != receipt.Link {
			if _, err := localLinkWitness(l.realm, l.state, *b, receipt.Link); err != nil {
				return false, err
			}
		}
		i := slices.IndexFunc(b.NativeVersions, func(previous artifactAttestation) bool {
			return previous.Logical == receipt.Logical && previous.Harness == receipt.Harness
		})
		if i >= 0 && b.NativeVersions[i] == receipt {
			continue
		}
		if !changed {
			b.NativeVersions = slices.Clone(b.NativeVersions)
			changed = true
		}
		if i < 0 {
			b.NativeVersions = append(b.NativeVersions, receipt)
		} else {
			b.NativeVersions[i] = receipt
		}
	}
	return changed, nil
}

func validateArtifactAttestation(b Binding, receipt artifactAttestation) error {
	logical, link, version := receipt.Logical, receipt.Link, receipt.Version
	mapping := Mapping{Kind: artifactKind(logical), Destination: logical, Source: link.Source}
	backing := findArtifactBacking(b, nil, logical, link.Source)
	if backing.Path != "" && version.Version != "" && strings.HasPrefix(version.Executable.Identity, "sha256:") &&
		filepath.IsAbs(version.Root) && filepath.IsAbs(version.PhysicalRoot) &&
		filepath.IsAbs(version.Executable.Launcher) && filepath.IsAbs(version.Executable.Path) {
		for _, matrix := range realmArtifactMatrices {
			row := matrix.Capability(realmArtifactRole(mapping.Kind))
			if matrix.Harness != receipt.Harness || row.Status != harness.StatusSupported || link.Path != artifactNativePath(row, mapping) {
				continue
			}
			if link == backing || slices.Contains(b.Artifacts, artifactReceipt{Logical: logical, Link: link}) {
				return nil
			}
		}
	}
	return fmt.Errorf("invalid native artifact version evidence for %q", link.Path)
}
