package migrate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/config"
)

func legacyPath(root string) string {
	return filepath.Join(root, config.LegacyScanIgnoreFile)
}

func readRepoConfig(t *testing.T, root string) string {
	t.Helper()
	raw, err := os.ReadFile(config.RepoConfigPath(root))
	if err != nil {
		t.Fatalf("read repo config: %v", err)
	}
	return string(raw)
}

func TestScanIgnoreMigrationConvertsAndRemoves(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(legacyPath(root), []byte("# c\nvendor/*\n+*.pb.go\n"), 0o644); err != nil {
		t.Fatalf("write legacy: %v", err)
	}

	if err := scanIgnoreToRepoConfig(&Context{Root: root}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	if _, err := os.Stat(legacyPath(root)); !os.IsNotExist(err) {
		t.Error("legacy file should be gone after a successful convert")
	}
	got := readRepoConfig(t, root)
	if !strings.Contains(got, "vendor/*") || !strings.Contains(got, "*.pb.go") {
		t.Errorf("config missing converted globs:\n%s", got)
	}

	// The converted config must be what ScanGlobs actually reads back.
	ignore, generated, legacy, err := config.ScanGlobs(root)
	if err != nil {
		t.Fatalf("ScanGlobs: %v", err)
	}
	if legacy {
		t.Error("want legacy=false after migration")
	}
	if len(ignore) != 1 || ignore[0] != "vendor/*" {
		t.Errorf("ignore = %v", ignore)
	}
	if len(generated) != 1 || generated[0] != "*.pb.go" {
		t.Errorf("generated = %v", generated)
	}
}

// The step runs on every repo on every upgrade; with no legacy file it must do
// nothing at all rather than create an empty config.
func TestScanIgnoreMigrationNoopWithoutLegacyFile(t *testing.T) {
	root := t.TempDir()
	if err := scanIgnoreToRepoConfig(&Context{Root: root}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if _, err := os.Stat(config.RepoConfigPath(root)); !os.IsNotExist(err) {
		t.Error("migration should not create a repo config when there is nothing to convert")
	}
}

func TestScanIgnoreMigrationPreservesExistingConfigKeys(t *testing.T) {
	root := t.TempDir()
	p := config.RepoConfigPath(root)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(p, []byte("scope = \"repo\"\n\n[code]\nignore = [\"dist/**\"]\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := os.WriteFile(legacyPath(root), []byte("vendor/*\n"), 0o644); err != nil {
		t.Fatalf("write legacy: %v", err)
	}

	if err := scanIgnoreToRepoConfig(&Context{Root: root}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	cfg, warns, err := config.LoadRepoConfig(p)
	if err != nil {
		t.Fatalf("LoadRepoConfig: %v", err)
	}
	if len(warns) != 0 {
		t.Errorf("unexpected warnings: %v", warns)
	}
	if cfg.Scope != "repo" {
		t.Errorf("scope lost: %q", cfg.Scope)
	}
	if len(cfg.Code.Ignore) != 1 || cfg.Code.Ignore[0] != "dist/**" {
		t.Errorf("[code] ignore lost: %v", cfg.Code.Ignore)
	}
	if len(cfg.Scan.Ignore) != 1 || cfg.Scan.Ignore[0] != "vendor/*" {
		t.Errorf("[scan] ignore not written: %v", cfg.Scan.Ignore)
	}
}

// Rewriting a config we could not fully parse would drop the author's content,
// so the step declines and leaves the legacy file in place as the live source.
func TestScanIgnoreMigrationLeavesMalformedConfigAlone(t *testing.T) {
	root := t.TempDir()
	p := config.RepoConfigPath(root)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	broken := "not valid = toml [[[\n"
	if err := os.WriteFile(p, []byte(broken), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := os.WriteFile(legacyPath(root), []byte("vendor/*\n"), 0o644); err != nil {
		t.Fatalf("write legacy: %v", err)
	}

	if err := scanIgnoreToRepoConfig(&Context{Root: root}); err != nil {
		t.Fatalf("want silent decline, got %v", err)
	}
	if got := readRepoConfig(t, root); got != broken {
		t.Errorf("malformed config was rewritten:\n%s", got)
	}
	if _, err := os.Stat(legacyPath(root)); err != nil {
		t.Error("legacy file must survive so the rules still apply")
	}
}

// A file of only comments carries no rules; writing an empty [scan] table would
// then permanently suppress any future legacy fallback for that repo.
func TestScanIgnoreMigrationDropsEmptyLegacyFile(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(legacyPath(root), []byte("# nothing here\n\n"), 0o644); err != nil {
		t.Fatalf("write legacy: %v", err)
	}
	if err := scanIgnoreToRepoConfig(&Context{Root: root}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if _, err := os.Stat(legacyPath(root)); !os.IsNotExist(err) {
		t.Error("empty legacy file should be removed")
	}
	if _, err := os.Stat(config.RepoConfigPath(root)); !os.IsNotExist(err) {
		t.Error("no config should be written for a rules-free legacy file")
	}
}
