package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// LegacyScanIgnoreFile is the repo-root file that [scan] in the repo config
// replaced. It is still read when [scan] declares nothing, so a repo that has
// not migrated keeps working.
const LegacyScanIgnoreFile = ".signalsignore"

// ScanGlobs resolves the two exclusion lists a repo scan honors.
//
// Ignore drops a path from the scan entirely; Generated leaves it in the tree
// marked so the inferrer skips it for domain content.
//
// [scan] in the repo config wins whole, not per-list: a config that declares
// only scan.ignore still suppresses a legacy .signalsignore's '+' lines. Merging
// the two would make the effective rule set depend on a file the author may not
// know is still there, and there would be no way to drop a legacy line from the
// config alone.
//
// legacy reports that the returned globs came from the deprecated file, so a
// caller can nudge toward migrating. A repo with neither source yields nil
// slices and no error.
func ScanGlobs(root string) (ignore, generated []string, legacy bool, err error) {
	cfg, _, cfgErr := LoadRepoConfig(RepoConfigPath(root))
	// Lenient: a malformed repo config must not stop a scan. Fall through to the
	// legacy file, matching how every other repo-config consumer degrades.
	if cfgErr == nil && cfg != nil && (len(cfg.Scan.Ignore) > 0 || len(cfg.Scan.Generated) > 0) {
		return cfg.Scan.Ignore, cfg.Scan.Generated, false, nil
	}

	ignore, generated, err = readLegacyScanIgnore(root)
	if err != nil {
		return nil, nil, false, err
	}
	return ignore, generated, len(ignore) > 0 || len(generated) > 0, nil
}

// readLegacyScanIgnore splits .signalsignore into plain excludes and
// '+'-prefixed generated globs. An absent file yields nil slices and no error.
func readLegacyScanIgnore(root string) (ignore, generated []string, err error) {
	path := filepath.Join(root, LegacyScanIgnoreFile)
	f, ferr := os.Open(path)
	if ferr != nil {
		if os.IsNotExist(ferr) {
			return nil, nil, nil
		}
		return nil, nil, fmt.Errorf("read %s: %w", LegacyScanIgnoreFile, ferr)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "+") {
			generated = append(generated, line[1:])
		} else {
			ignore = append(ignore, line)
		}
	}
	return ignore, generated, scanner.Err()
}
