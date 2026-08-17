package doctor_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/bundlespec"
	"github.com/damusix/atomic-claude/atomic/internal/doctor"
	"github.com/damusix/atomic-claude/atomic/internal/embedded"
)

func TestCheckManifest_notRepoDev(t *testing.T) {
	// No marker file, so this dir is not repo-dev.
	cwd := t.TempDir()

	r := doctor.RunCheckManifest(cwd)
	if r.Severity != doctor.SKIP {
		t.Errorf("severity = %q, want SKIP for non-repo-dev dir; detail: %s", r.Severity, r.Detail)
	}
	if r.Detail == "" {
		t.Error("Detail is empty, want informative message")
	}
}

func TestCheckManifest_pass(t *testing.T) {
	root := buildSyntheticRepoDev(t)

	r := doctor.RunCheckManifest(root)
	if r.Severity != doctor.PASS {
		t.Errorf("severity = %q, want PASS; detail: %s", r.Severity, r.Detail)
	}
}

func TestCheckManifest_fail_drift(t *testing.T) {
	root := buildSyntheticRepoDev(t)

	manifest := embedded.Manifest()
	var driftTarget string
	for _, a := range manifest {
		// Sources live at <root>/context/<Target>, not at the embedded path.
		if a.Kind == "agent" {
			driftTarget = filepath.Join(bundlespec.SourceRoot(root), filepath.FromSlash(a.Target))
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

func TestCheckManifest_pass_no_findings(t *testing.T) {
	root := buildSyntheticRepoDev(t)

	r := doctor.RunCheckManifest(root)
	if r.Severity != doctor.PASS {
		t.Fatalf("severity = %q, want PASS; detail: %s", r.Severity, r.Detail)
	}
	if len(r.Findings) != 0 {
		t.Errorf("Findings = %v, want empty", r.Findings)
	}
	if r.Remediation != "" {
		t.Errorf("Remediation = %q, want empty", r.Remediation)
	}
}

func TestCheckManifest_fail_findings(t *testing.T) {
	root := buildSyntheticRepoDev(t)

	manifest := embedded.Manifest()
	var driftTarget string
	var driftRelPath string
	for _, a := range manifest {
		if a.Kind == "agent" {
			driftTarget = filepath.Join(bundlespec.SourceRoot(root), filepath.FromSlash(a.Target))
			driftRelPath = a.Target
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
		t.Fatalf("severity = %q, want FAIL; detail: %s", r.Severity, r.Detail)
	}

	want := "drifted: " + driftRelPath
	found := false
	for _, f := range r.Findings {
		if f == want {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Findings = %v; want entry %q", r.Findings, want)
	}
	if r.Remediation != "make -C atomic bundle" {
		t.Errorf("Remediation = %q, want %q", r.Remediation, "make -C atomic bundle")
	}
}

func TestCheckManifest_fail_findings_missing(t *testing.T) {
	root := buildSyntheticRepoDev(t)

	manifest := embedded.Manifest()
	var missingRelPath string
	for _, a := range manifest {
		if a.Kind == "agent" {
			missingRelPath = a.Target
			break
		}
	}
	if missingRelPath == "" {
		t.Fatal("no agent artifact found in manifest")
	}
	if err := os.Remove(filepath.Join(bundlespec.SourceRoot(root), filepath.FromSlash(missingRelPath))); err != nil {
		t.Fatalf("remove artifact: %v", err)
	}

	r := doctor.RunCheckManifestWith(root)
	if r.Severity != doctor.FAIL {
		t.Fatalf("severity = %q, want FAIL; detail: %s", r.Severity, r.Detail)
	}

	want := "missing: " + missingRelPath
	found := false
	for _, f := range r.Findings {
		if f == want {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Findings = %v; want entry %q", r.Findings, want)
	}
	if r.Remediation != "make -C atomic bundle" {
		t.Errorf("Remediation = %q, want %q", r.Remediation, "make -C atomic bundle")
	}
}

func TestCheckManifest_fail_findings_extra(t *testing.T) {
	root := buildSyntheticRepoDev(t)

	// Matches bundlespec's atomic-*.md shape but is absent from the manifest,
	// and named so it cannot collide with a real artifact.
	extraRelPath := "agents/atomic-zzz-extra-fixture-test.md"
	extraDst := filepath.Join(bundlespec.SourceRoot(root), filepath.FromSlash(extraRelPath))
	if err := os.WriteFile(extraDst, []byte("# extra fixture\n"), 0o644); err != nil {
		t.Fatalf("write extra artifact: %v", err)
	}

	r := doctor.RunCheckManifestWith(root)
	if r.Severity != doctor.FAIL {
		t.Fatalf("severity = %q, want FAIL; detail: %s", r.Severity, r.Detail)
	}

	want := "extra: " + extraRelPath
	found := false
	for _, f := range r.Findings {
		if f == want {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Findings = %v; want entry %q", r.Findings, want)
	}
	if r.Remediation != "make -C atomic bundle" {
		t.Errorf("Remediation = %q, want %q", r.Remediation, "make -C atomic bundle")
	}
}

// buildSyntheticRepoDev returns a tree that looks like the atomic-claude repo
// root to IsRepoDev and bundlemirror.Enumerate: the marker file, plus every
// embedded artifact written under the source root at its Target path.
func buildSyntheticRepoDev(t *testing.T) string {
	t.Helper()
	root := t.TempDir()

	markerDir := filepath.Join(root, "atomic", "internal", "bundlemirror")
	if err := os.MkdirAll(markerDir, 0o755); err != nil {
		t.Fatalf("mkdir marker: %v", err)
	}
	if err := os.WriteFile(filepath.Join(markerDir, "mirror.go"), []byte("package bundlemirror"), 0o644); err != nil {
		t.Fatalf("write marker: %v", err)
	}

	for _, a := range embedded.Manifest() {
		data, err := embedded.FS.ReadFile(a.Source)
		if err != nil {
			t.Fatalf("read embedded %s: %v", a.Source, err)
		}
		dst := filepath.Join(bundlespec.SourceRoot(root), filepath.FromSlash(a.Target))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(dst), err)
		}
		if err := os.WriteFile(dst, data, 0o644); err != nil {
			t.Fatalf("write %s: %v", dst, err)
		}
	}

	return root
}
