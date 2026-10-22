package embeddedcorpus

import (
	"io/fs"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/artifacts"
	"github.com/damusix/atomic-claude/atomic/internal/embedded"
	"github.com/damusix/atomic-claude/atomic/internal/managedfile"
)

// The embedded bundle is a usable canonical corpus: every manifest row is
// re-identified by its canonical source, carries the bytes that ship, and keeps
// a digest of exactly those bytes.
func TestLoadEnumeratesTheEmbeddedBundle(t *testing.T) {
	cat, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cat.Artifacts) != len(embedded.Manifest()) {
		t.Fatalf("artifacts = %d, want one per manifest row (%d)", len(cat.Artifacts), len(embedded.Manifest()))
	}
	for _, a := range cat.Artifacts {
		if a.ID != string(a.Kind)+":"+a.Source || a.Source == "" {
			t.Errorf("artifact identity = %q, source = %q", a.ID, a.Source)
		}
		want, err := fs.ReadFile(embedded.FS, "bundle/"+embeddedSource(a.Source))
		if err != nil {
			t.Fatalf("read embedded %s: %v", a.Source, err)
		}
		if string(a.Body) != string(want) {
			t.Errorf("artifact %s does not carry the embedded bytes", a.ID)
		}
		if a.SourceDigest != managedfile.Digest(a.Body) {
			t.Errorf("artifact %s source digest does not match its bytes", a.ID)
		}
	}

	steering, err := artifacts.NewRenderer(cat).Steering()
	if err != nil {
		t.Fatalf("steering: %v", err)
	}
	if steering.Kind != artifacts.KindSteering || steering.Source != "AGENTS.md" {
		t.Errorf("steering = %+v, want the canonical AGENTS.md source", steering)
	}
	if _, err := artifacts.NewRenderer(cat).OMPSteering(); err != nil {
		t.Errorf("embedded corpus cannot compose OMP steering: %v", err)
	}
}

// embeddedSource maps a canonical source back to the manifest's bundle path,
// treating the steering artifact as the Claude-native file the bundle stores.
func embeddedSource(source string) string {
	if source == "AGENTS.md" {
		return "CLAUDE.md"
	}
	return source
}
