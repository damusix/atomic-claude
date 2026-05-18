package doctor_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/doctor"
	"github.com/damusix/atomic-claude/atomic/internal/embedded"
)

// TestCheckManifest_notRepoDev: when IsRepoDev returns false → SKIP.
func TestCheckManifest_notRepoDev(t *testing.T) {
	// Use a temp dir with no marker file → not repo-dev.
	cwd := t.TempDir()

	r := doctor.RunCheckManifest(cwd)
	if r.Severity != doctor.SKIP {
		t.Errorf("severity = %q, want SKIP for non-repo-dev dir; detail: %s", r.Severity, r.Detail)
	}
	if r.Detail == "" {
		t.Error("Detail is empty, want informative message")
	}
}

// TestCheckManifest_pass: synthetic repo root with marker + all artifacts
// matching the committed manifest → PASS.
func TestCheckManifest_pass(t *testing.T) {
	root := buildSyntheticRepoDev(t)

	r := doctor.RunCheckManifest(root)
	if r.Severity != doctor.PASS {
		t.Errorf("severity = %q, want PASS; detail: %s", r.Severity, r.Detail)
	}
}

// TestCheckManifest_fail_drift: same synthetic repo but one source artifact
// is mutated on disk → Compare returns OK=false → FAIL.
func TestCheckManifest_fail_drift(t *testing.T) {
	root := buildSyntheticRepoDev(t)

	// Mutate one agent source file to cause drift.
	manifest := embedded.Manifest()
	var driftTarget string
	for _, a := range manifest {
		// Pick any agent artifact source path relative to root.
		// The source path inside embedded FS is e.g. "bundle/agents/atomic-builder.md".
		// The file lives at <root>/<Target> where Target = "agents/atomic-builder.md".
		if a.Kind == "agent" {
			driftTarget = filepath.Join(root, filepath.FromSlash(a.Target))
			break
		}
	}
	if driftTarget == "" {
		t.Fatal("no agent artifact found in manifest")
	}
	if err := os.WriteFile(driftTarget, []byte("mutated"), 0o644); err != nil {
		t.Fatalf("mutate artifact: %v", err)
	}

	r := doctor.RunCheckManifest(root)
	if r.Severity != doctor.FAIL {
		t.Errorf("severity = %q, want FAIL; detail: %s", r.Severity, r.Detail)
	}
}

// buildSyntheticRepoDev creates a temporary directory tree that looks like the
// atomic-claude repo root to IsRepoDev and bundlemirror.Enumerate:
//   - atomic/internal/bundlemirror/mirror.go  (marker file)
//   - all source artifact files from embedded.Manifest() written at their Target paths
//
// Returns the root path.
func buildSyntheticRepoDev(t *testing.T) string {
	t.Helper()
	root := t.TempDir()

	// Create the IsRepoDev marker.
	markerDir := filepath.Join(root, "atomic", "internal", "bundlemirror")
	if err := os.MkdirAll(markerDir, 0o755); err != nil {
		t.Fatalf("mkdir marker: %v", err)
	}
	if err := os.WriteFile(filepath.Join(markerDir, "mirror.go"), []byte("package bundlemirror"), 0o644); err != nil {
		t.Fatalf("write marker: %v", err)
	}

	// Write each embedded artifact at its Target path so Enumerate can hash them.
	for _, a := range embedded.Manifest() {
		data, err := embedded.FS.ReadFile(a.Source)
		if err != nil {
			t.Fatalf("read embedded %s: %v", a.Source, err)
		}
		dst := filepath.Join(root, filepath.FromSlash(a.Target))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(dst), err)
		}
		if err := os.WriteFile(dst, data, 0o644); err != nil {
			t.Fatalf("write %s: %v", dst, err)
		}
	}

	return root
}
