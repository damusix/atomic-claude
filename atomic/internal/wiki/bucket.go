package wiki

import (
	"fmt"
	"path/filepath"
	"strings"
)

// validateBucketName requires a safe single path segment: the name becomes a
// directory component under the realm root. It backs up the CLI scanner's own
// dash-token rejection and covers programmatic callers that bypass the CLI.
func validateBucketName(name string) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("bucket: name must not be empty")
	}
	if strings.HasPrefix(name, "-") {
		return fmt.Errorf("bucket: name %q must not begin with %q", name, "-")
	}
	if strings.ContainsAny(name, "/\\") {
		return fmt.Errorf("bucket: name %q must not contain a path separator", name)
	}
	if name == "." || name == ".." {
		return fmt.Errorf("bucket: name %q is not a valid path segment", name)
	}
	if name == "wiki" {
		return fmt.Errorf("bucket: name %q is reserved", name)
	}
	return nil
}

// RegisterBucket refuses an unsafe name or one already in wikiDir/index.md's
// <wiki-buckets> registry, writing nothing, so callers gate their mutations on it.
func RegisterBucket(wikiDir, name string) error {
	if err := validateBucketName(name); err != nil {
		return err
	}

	entries, err := readBucketEntries(filepath.Join(wikiDir, "index.md"))
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.Name == name {
			return fmt.Errorf("bucket: %q is already registered", name)
		}
	}
	return nil
}
