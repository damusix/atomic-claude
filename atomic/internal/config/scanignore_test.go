package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeRepoConfig puts a repo config at the harness-resolved path under root.
func writeRepoConfig(t *testing.T, root, body string) {
	t.Helper()
	p := RepoConfigPath(root)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatalf("write repo config: %v", err)
	}
}

func writeLegacy(t *testing.T, root, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, LegacyScanIgnoreFile), []byte(body), 0o644); err != nil {
		t.Fatalf("write legacy: %v", err)
	}
}

// Neither source present is the common case and must not error, because a scan
// runs on every repo whether or not it has exclusions.
func TestScanGlobsNoSourceIsNotAnError(t *testing.T) {
	root := t.TempDir()
	ignore, generated, legacy, err := ScanGlobs(root)
	if err != nil {
		t.Fatalf("want no error, got %v", err)
	}
	if ignore != nil || generated != nil || legacy {
		t.Fatalf("want empty non-legacy result, got ignore=%v generated=%v legacy=%v", ignore, generated, legacy)
	}
}

// The '+' prefix is the legacy file's only way to say "keep it in the tree but
// skip its content", so it must land in generated, not ignore.
func TestScanGlobsLegacySplitsGeneratedFromIgnore(t *testing.T) {
	root := t.TempDir()
	writeLegacy(t, root, "# comment\n\nvendor/*\n+*.pb.go\n")

	ignore, generated, legacy, err := ScanGlobs(root)
	if err != nil {
		t.Fatalf("ScanGlobs: %v", err)
	}
	if !legacy {
		t.Error("want legacy=true when globs came from .signalsignore")
	}
	if len(ignore) != 1 || ignore[0] != "vendor/*" {
		t.Errorf("ignore = %v, want [vendor/*]", ignore)
	}
	if len(generated) != 1 || generated[0] != "*.pb.go" {
		t.Errorf("generated = %v, want [*.pb.go]", generated)
	}
}

func TestScanGlobsRepoConfigWins(t *testing.T) {
	root := t.TempDir()
	writeLegacy(t, root, "from-legacy/*\n")
	writeRepoConfig(t, root, "[scan]\nignore = [\"from-config/**\"]\n")

	ignore, _, legacy, err := ScanGlobs(root)
	if err != nil {
		t.Fatalf("ScanGlobs: %v", err)
	}
	if legacy {
		t.Error("want legacy=false when [scan] is present")
	}
	if len(ignore) != 1 || ignore[0] != "from-config/**" {
		t.Errorf("ignore = %v, want [from-config/**]", ignore)
	}
}

// [scan] wins as a whole table. A config declaring only ignore must suppress a
// legacy file's '+' lines too, otherwise the effective rules depend on a file
// the author may not know is still there.
func TestScanGlobsRepoConfigSuppressesLegacyGenerated(t *testing.T) {
	root := t.TempDir()
	writeLegacy(t, root, "+*.pb.go\n")
	writeRepoConfig(t, root, "[scan]\nignore = [\"vendor/**\"]\n")

	_, generated, _, err := ScanGlobs(root)
	if err != nil {
		t.Fatalf("ScanGlobs: %v", err)
	}
	if len(generated) != 0 {
		t.Errorf("generated = %v, want none: [scan] should win whole", generated)
	}
}

// A repo config that fails to parse must not take the scan down with it; the
// legacy file is still a usable source.
func TestScanGlobsFallsBackWhenRepoConfigMalformed(t *testing.T) {
	root := t.TempDir()
	writeLegacy(t, root, "vendor/*\n")
	writeRepoConfig(t, root, "this is not = valid toml [[[\n")

	ignore, _, legacy, err := ScanGlobs(root)
	if err != nil {
		t.Fatalf("want degradation, got error %v", err)
	}
	if !legacy || len(ignore) != 1 || ignore[0] != "vendor/*" {
		t.Errorf("want legacy fallback [vendor/*], got ignore=%v legacy=%v", ignore, legacy)
	}
}

// scan.ignore and scan.generated must be known keys, or every repo declaring
// them gets an unknown-key warning.
func TestRepoConfigScanKeysAreKnown(t *testing.T) {
	root := t.TempDir()
	writeRepoConfig(t, root, "[scan]\nignore = [\"a/**\"]\ngenerated = [\"*.pb.go\"]\n")

	cfg, warns, err := LoadRepoConfig(RepoConfigPath(root))
	if err != nil {
		t.Fatalf("LoadRepoConfig: %v", err)
	}
	if len(warns) != 0 {
		t.Errorf("want no warnings, got %v", warns)
	}
	if len(cfg.Scan.Ignore) != 1 || len(cfg.Scan.Generated) != 1 {
		t.Errorf("scan not decoded: %+v", cfg.Scan)
	}
}

// A typo inside [scan] must still be caught. Registering the section without
// registering its leaves would make the whole table a silent free-for-all.
func TestRepoConfigScanUnknownLeafWarns(t *testing.T) {
	root := t.TempDir()
	writeRepoConfig(t, root, "[scan]\nignore = [\"a/**\"]\nbogus = 1\n")

	_, warns, err := LoadRepoConfig(RepoConfigPath(root))
	if err != nil {
		t.Fatalf("LoadRepoConfig: %v", err)
	}
	if len(warns) != 1 || !strings.Contains(warns[0].Message, "scan.bogus") {
		t.Errorf("want one warning naming scan.bogus, got %v", warns)
	}
}
