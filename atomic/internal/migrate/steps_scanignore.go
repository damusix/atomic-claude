package migrate

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/pelletier/go-toml/v2"
)

func init() {
	Registry = append(Registry, Migration{
		TargetVersion: "1.1.0",
		Scope:         "repo",
		Up:            scanIgnoreToRepoConfig,
	})
}

// scanIgnoreToRepoConfig folds a repo-root .signalsignore into [scan] in the
// repo config and deletes the file.
//
// Idempotent by absence: with no .signalsignore there is nothing to convert, and
// the file is removed only after the config write succeeds, so a crashed run
// replays from the original rather than losing the rules.
func scanIgnoreToRepoConfig(ctx *Context) error {
	legacy := filepath.Join(ctx.Root, config.LegacyScanIgnoreFile)
	if !fileExists(legacy) {
		return nil
	}

	ignore, generated, err := readLegacyLines(legacy)
	if err != nil {
		return fmt.Errorf("migrate scan-ignore: %w", err)
	}

	// An empty or comment-only file carries no rules; drop it without touching
	// the config, so the migration never writes an empty [scan] table.
	if len(ignore) == 0 && len(generated) == 0 {
		if err := os.Remove(legacy); err != nil {
			return fmt.Errorf("migrate scan-ignore: remove %s: %w", config.LegacyScanIgnoreFile, err)
		}
		return nil
	}

	cfgPath := config.RepoConfigPath(ctx.Root)
	raw, readErr := os.ReadFile(cfgPath)
	if readErr != nil && !os.IsNotExist(readErr) {
		return fmt.Errorf("migrate scan-ignore: read %s: %w", cfgPath, readErr)
	}

	var doc map[string]any
	if len(raw) > 0 {
		if err := toml.Unmarshal(raw, &doc); err != nil {
			// A repo config we cannot parse is one we must not rewrite: the author's
			// own content would be lost re-serializing a partial parse. Leave both
			// files alone; ScanGlobs still honors the legacy file.
			return nil
		}
	}
	if doc == nil {
		doc = map[string]any{}
	}
	// An existing [scan] table already wins over the legacy file at read time, so
	// overwriting it here would silently change behavior.
	if _, exists := doc["scan"]; exists {
		return os.Remove(legacy)
	}

	scan := map[string]any{}
	if len(ignore) > 0 {
		scan["ignore"] = ignore
	}
	if len(generated) > 0 {
		scan["generated"] = generated
	}
	doc["scan"] = scan

	out, err := toml.Marshal(doc)
	if err != nil {
		return fmt.Errorf("migrate scan-ignore: encode %s: %w", cfgPath, err)
	}
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		return fmt.Errorf("migrate scan-ignore: mkdir %s: %w", filepath.Dir(cfgPath), err)
	}
	if err := writeFileAtomic(cfgPath, out, 0o644); err != nil {
		return fmt.Errorf("migrate scan-ignore: write %s: %w", cfgPath, err)
	}

	if err := os.Remove(legacy); err != nil {
		return fmt.Errorf("migrate scan-ignore: remove %s: %w", config.LegacyScanIgnoreFile, err)
	}
	return nil
}

// readLegacyLines splits the legacy file the same way config's reader does.
// Duplicated rather than exported from config because this is a one-time
// transform of a format that is going away, not a shared contract.
func readLegacyLines(path string) (ignore, generated []string, err error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, fmt.Errorf("read %s: %w", path, err)
	}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "+") {
			generated = append(generated, line[1:])
		} else {
			ignore = append(ignore, line)
		}
	}
	return ignore, generated, nil
}
