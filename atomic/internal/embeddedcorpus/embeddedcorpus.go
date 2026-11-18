// Package embeddedcorpus loads the canonical corpus from the embedded bundle.
// It sits outside artifacts so a fresh clone can still generate that bundle.
package embeddedcorpus

import (
	"fmt"
	"io/fs"

	"github.com/damusix/atomic-claude/atomic/internal/artifacts"
	"github.com/damusix/atomic-claude/atomic/internal/embedded"
	"github.com/damusix/atomic-claude/atomic/internal/managedfile"
)

// Load enumerates the embedded bundle into the canonical corpus, parsing the
// bytes that ship because a shipped binary has no source tree to read.
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

// kindOfInstallerKind maps a manifest kind back to the canonical kind. Steering
// is the one row that does not round-trip by name.
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
