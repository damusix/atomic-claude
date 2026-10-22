// Package embeddedcorpus loads the canonical corpus the selected binary
// carries: the embedded bundle's own bytes, with no checkout to read.
//
// It is deliberately not part of artifacts. The bundle-mirror generator imports
// artifacts, and the embedded filesystem carries a go:embed directive that
// cannot compile until the bundle it generates exists — so a loader living in
// artifacts would leave a fresh clone unable to generate its own bundle.
package embeddedcorpus

import (
	"fmt"
	"io/fs"

	"github.com/damusix/atomic-claude/atomic/internal/artifacts"
	"github.com/damusix/atomic-claude/atomic/internal/embedded"
	"github.com/damusix/atomic-claude/atomic/internal/managedfile"
)

// Load enumerates the embedded bundle into the canonical corpus. The bundle is
// already rendered — partials expanded, every artifact at its canonical source
// path — so each file is re-identified from the manifest's canonical source and
// its portable semantics are parsed from the bytes that ship, not from a source
// tree a shipped binary cannot see.
func Load() (*artifacts.Catalog, error) {
	out := make([]artifacts.Artifact, 0, len(embedded.Manifest()))
	for _, m := range embedded.Manifest() {
		kind, err := kindOfInstallerKind(m.Kind)
		if err != nil {
			return nil, err
		}
		if m.Canonical == "" {
			return nil, fmt.Errorf("embeddedcorpus: embedded %s carries no canonical source", m.Source)
		}
		data, err := fs.ReadFile(embedded.FS, m.Source)
		if err != nil {
			return nil, fmt.Errorf("embeddedcorpus: read embedded %s: %w", m.Source, err)
		}
		out = append(out, artifacts.Artifact{
			ID:           string(kind) + ":" + m.Canonical,
			Kind:         kind,
			Source:       m.Canonical,
			Body:         data,
			SourceDigest: managedfile.Digest(data),
		})
	}
	return artifacts.NewCatalog(out)
}

// kindOfInstallerKind maps a bundle manifest kind back to the canonical kind.
// The manifest spells the global steering artifact as its Claude-native target,
// so it is the one row whose kind does not round-trip by name.
func kindOfInstallerKind(kind string) (artifacts.Kind, error) {
	if kind == "claude-md" {
		return artifacts.KindSteering, nil
	}
	k := artifacts.Kind(kind)
	switch k {
	case artifacts.KindSteering, artifacts.KindAgent, artifacts.KindSkill, artifacts.KindOutputStyle, artifacts.KindCommand, artifacts.KindRule:
		return k, nil
	}
	return "", fmt.Errorf("embeddedcorpus: unknown embedded kind %q", kind)
}
