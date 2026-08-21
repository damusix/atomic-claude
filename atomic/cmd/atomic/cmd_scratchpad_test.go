package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/config"
)

func TestScratchpadNewActionCreatesAndExtends(t *testing.T) {
	root := t.TempDir()

	bundleRoot, extended, err := scratchpadNewAction(root, "cli-feature", "plan")
	if err != nil {
		t.Fatalf("scratchpadNewAction: %v", err)
	}
	if extended {
		t.Fatalf("expected extended=false for a fresh bundle")
	}
	want := filepath.Join(config.ScratchpadDir(root), "cli-feature")
	if bundleRoot != want {
		t.Errorf("bundleRoot = %q, want %q", bundleRoot, want)
	}
	if _, err := os.Stat(filepath.Join(bundleRoot, "meta.toml")); err != nil {
		t.Fatalf("expected meta.toml: %v", err)
	}

	// Re-running with a different purpose reports extension.
	_, extended2, err := scratchpadNewAction(root, "cli-feature", "implement")
	if err != nil {
		t.Fatalf("scratchpadNewAction (extend): %v", err)
	}
	if !extended2 {
		t.Errorf("expected extended=true on second call for the same slug")
	}
}

func TestScratchpadNewActionUnknownPurposeErrors(t *testing.T) {
	root := t.TempDir()
	if _, _, err := scratchpadNewAction(root, "x", "bogus"); err == nil {
		t.Fatalf("expected an error for an unknown purpose")
	}
}

func TestScratchpadPathActionResolvesExistingBundle(t *testing.T) {
	root := t.TempDir()
	if _, _, err := scratchpadNewAction(root, "cli-path-feature", "implement"); err != nil {
		t.Fatalf("scratchpadNewAction: %v", err)
	}

	got, err := scratchpadPathAction(root, "cli-path-feature")
	if err != nil {
		t.Fatalf("scratchpadPathAction: %v", err)
	}
	want := filepath.Join(config.ScratchpadDir(root), "cli-path-feature")
	if got != want {
		t.Errorf("scratchpadPathAction = %q, want %q", got, want)
	}
}

// atomic scratchpad new <slug> --purpose <p> is the documented order (spec,
// brief, and this command's own usage string). flag.FlagSet.Parse stops at
// the first non-flag token, so this must go through the real argv path
// (scratchpadDispatch), not the scratchpadNewAction seam that skips parsing.
func TestScratchpadDispatchNewAcceptsSlugBeforePurposeFlag(t *testing.T) {
	root := t.TempDir()
	var stdout, stderr bytes.Buffer

	code := scratchpadDispatch([]string{"new", "argv-order", "--purpose", "plan"}, root, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr:\n%s", code, stderr.String())
	}

	bundleRoot := filepath.Join(config.ScratchpadDir(root), "argv-order")
	if _, err := os.Stat(filepath.Join(bundleRoot, "meta.toml")); err != nil {
		t.Fatalf("expected meta.toml at %s: %v", bundleRoot, err)
	}
}

// Flags-first order must keep working alongside the fix above.
func TestScratchpadDispatchNewAcceptsPurposeFlagBeforeSlug(t *testing.T) {
	root := t.TempDir()
	var stdout, stderr bytes.Buffer

	code := scratchpadDispatch([]string{"new", "--purpose", "plan", "flags-first"}, root, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr:\n%s", code, stderr.String())
	}

	bundleRoot := filepath.Join(config.ScratchpadDir(root), "flags-first")
	if _, err := os.Stat(filepath.Join(bundleRoot, "meta.toml")); err != nil {
		t.Fatalf("expected meta.toml at %s: %v", bundleRoot, err)
	}
}

// A slug is the third path-segment source the spec's validation criterion
// names (alongside a bundle's `created` date and a branch label): a
// traversal payload must never reach BundleRoot's filepath.Join, on any of
// the three verbs that take a slug positionally.
func TestScratchpadDispatchNewRejectsTraversalSlug(t *testing.T) {
	root := t.TempDir()
	var stdout, stderr bytes.Buffer

	code := scratchpadDispatch([]string{"new", "../../../etc/evil", "--purpose", "plan"}, root, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("exit code = 0, want nonzero for a traversal slug")
	}
	if _, err := os.Stat(filepath.Join(root, "..", "..", "etc", "evil")); err == nil {
		t.Fatalf("bundle escaped root via traversal slug")
	}
}

func TestScratchpadDispatchPathRejectsTraversalSlug(t *testing.T) {
	root := t.TempDir()
	var stdout, stderr bytes.Buffer

	code := scratchpadDispatch([]string{"path", "../../../etc/evil"}, root, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("exit code = 0, want nonzero for a traversal slug")
	}
}

func TestScratchpadDispatchArchiveRejectsTraversalSlug(t *testing.T) {
	root := t.TempDir()
	var stdout, stderr bytes.Buffer

	code := scratchpadDispatch([]string{"archive", "../../../etc/evil"}, root, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("exit code = 0, want nonzero for a traversal slug")
	}
}

func TestScratchpadPathActionMissingSlugErrors(t *testing.T) {
	root := t.TempDir()
	if _, err := scratchpadPathAction(root, "does-not-exist"); err == nil {
		t.Fatalf("expected an error for a slug with no bundle")
	}
}

func TestScratchpadDispatchArchiveMovesBundle(t *testing.T) {
	root := t.TempDir()
	var stdout, stderr bytes.Buffer
	if code := scratchpadDispatch([]string{"new", "to-archive", "--purpose", "implement"}, root, &stdout, &stderr); code != 0 {
		t.Fatalf("new: exit %d, stderr:\n%s", code, stderr.String())
	}
	stdout.Reset()

	code := scratchpadDispatch([]string{"archive", "to-archive"}, root, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("archive: exit %d, stderr:\n%s", code, stderr.String())
	}
	dest := strings.TrimSpace(stdout.String())
	if _, err := os.Stat(filepath.Join(dest, "meta.toml")); err != nil {
		t.Fatalf("expected archived bundle at %q: %v", dest, err)
	}
	liveDir := filepath.Join(config.ScratchpadDir(root), "to-archive")
	if _, err := os.Stat(liveDir); !os.IsNotExist(err) {
		t.Errorf("expected live bundle dir to be gone, got err=%v", err)
	}
}

func TestScratchpadDispatchArchiveMissingSlugErrors(t *testing.T) {
	root := t.TempDir()
	var stdout, stderr bytes.Buffer
	code := scratchpadDispatch([]string{"archive", "no-such-slug"}, root, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected a nonzero exit archiving a slug with no bundle")
	}
}

// list --json omits any entry without a meta.toml and includes every real
// bundle.
func TestScratchpadDispatchListJSON(t *testing.T) {
	root := t.TempDir()
	var stdout, stderr bytes.Buffer
	for _, slug := range []string{"alpha", "beta"} {
		if code := scratchpadDispatch([]string{"new", slug, "--purpose", "implement"}, root, &stdout, &stderr); code != 0 {
			t.Fatalf("new %s: exit %d, stderr:\n%s", slug, code, stderr.String())
		}
		stdout.Reset()
	}
	// A dir with no meta.toml under the scratchpad root must never appear.
	stray := filepath.Join(config.ScratchpadDir(root), "session-reports")
	if err := os.MkdirAll(stray, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	code := scratchpadDispatch([]string{"list", "--json"}, root, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("list --json: exit %d, stderr:\n%s", code, stderr.String())
	}

	var rows []map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &rows); err != nil {
		t.Fatalf("json.Unmarshal: %v\noutput:\n%s", err, stdout.String())
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2 (session-reports must be skipped): %+v", len(rows), rows)
	}
	got := map[string]bool{}
	for _, r := range rows {
		got[r["slug"].(string)] = true
	}
	if !got["alpha"] || !got["beta"] {
		t.Errorf("rows = %+v, want alpha and beta", rows)
	}
}

// A bundle moved to the archive stops appearing in the live list and starts
// appearing in `list --archived`.
func TestScratchpadDispatchListArchivedVsLive(t *testing.T) {
	root := t.TempDir()
	var stdout, stderr bytes.Buffer
	if code := scratchpadDispatch([]string{"new", "gamma", "--purpose", "implement"}, root, &stdout, &stderr); code != 0 {
		t.Fatalf("new: exit %d, stderr:\n%s", code, stderr.String())
	}
	stdout.Reset()
	if code := scratchpadDispatch([]string{"archive", "gamma"}, root, &stdout, &stderr); code != 0 {
		t.Fatalf("archive: exit %d, stderr:\n%s", code, stderr.String())
	}
	stdout.Reset()

	if code := scratchpadDispatch([]string{"list", "--json"}, root, &stdout, &stderr); code != 0 {
		t.Fatalf("list: exit %d, stderr:\n%s", code, stderr.String())
	}
	var live []map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &live); err != nil {
		t.Fatalf("json.Unmarshal (live): %v", err)
	}
	if len(live) != 0 {
		t.Errorf("live list after archiving = %+v, want empty", live)
	}
	stdout.Reset()

	if code := scratchpadDispatch([]string{"list", "--json", "--archived"}, root, &stdout, &stderr); code != 0 {
		t.Fatalf("list --archived: exit %d, stderr:\n%s", code, stderr.String())
	}
	var archived []map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &archived); err != nil {
		t.Fatalf("json.Unmarshal (archived): %v", err)
	}
	if len(archived) != 1 || archived[0]["slug"] != "gamma" {
		t.Fatalf("archived list = %+v, want one entry for gamma", archived)
	}
}

// `new` on a slug with an existing exact-match archive prints a notice and
// still proceeds to create a fresh bundle.
func TestScratchpadDispatchNewPrintsArchivedNoticeAndProceeds(t *testing.T) {
	root := t.TempDir()
	var stdout, stderr bytes.Buffer
	if code := scratchpadDispatch([]string{"new", "delta", "--purpose", "plan"}, root, &stdout, &stderr); code != 0 {
		t.Fatalf("new: exit %d, stderr:\n%s", code, stderr.String())
	}
	stdout.Reset()
	if code := scratchpadDispatch([]string{"archive", "delta"}, root, &stdout, &stderr); code != 0 {
		t.Fatalf("archive: exit %d, stderr:\n%s", code, stderr.String())
	}
	stdout.Reset()

	code := scratchpadDispatch([]string{"new", "delta", "--purpose", "fix"}, root, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("new (2nd): exit %d, stderr:\n%s", code, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "archiv") {
		t.Errorf("expected an archived-match notice, got:\n%s", out)
	}
	liveDir := filepath.Join(config.ScratchpadDir(root), "delta")
	if _, err := os.Stat(filepath.Join(liveDir, "meta.toml")); err != nil {
		t.Fatalf("expected a fresh bundle to be created despite the archived match: %v", err)
	}
}
