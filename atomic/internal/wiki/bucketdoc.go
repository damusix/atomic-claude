package wiki

// Scaffolds behind the `atomic wiki bucket doc` and `skill` verbs. Both take
// an already-resolved bucket directory and never consult the <wiki-buckets>
// registry: confirming the bucket is registered is the caller's job.

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

//go:embed templates/*.md
var bucketTemplatesFS embed.FS

// bucketTemplate reads a scaffold by filename stem. These are compiled into
// the binary, not install artifacts, and never enter the bundle mirror.
func bucketTemplate(name string) (string, error) {
	data, err := bucketTemplatesFS.ReadFile("templates/" + name + ".md")
	if err != nil {
		return "", fmt.Errorf("embedded template %q: %w", name, err)
	}
	return string(data), nil
}

// slugPattern is the bucket-doc slug contract in docs/spec/bucket-icm-structure.md.
var slugPattern = regexp.MustCompile(`^[a-z0-9-]+$`)

func validateSlug(slug string) error {
	if !slugPattern.MatchString(slug) {
		return fmt.Errorf("invalid slug %q — must match [a-z0-9-]+ (lowercase letters, digits, hyphens)", slug)
	}
	if skipDirs[slug] {
		return fmt.Errorf("slug %q names a folder the bucket index skips", slug)
	}
	return nil
}

// ScaffoldBucketDoc writes <slug>/index.md and <slug>/01_raw/index.md under
// bucketDir, returning the first. Any existing <slug> path aborts before a write.
func ScaffoldBucketDoc(bucketDir, slug string, now time.Time) (string, error) {
	if err := validateSlug(slug); err != nil {
		return "", err
	}

	slugDir := filepath.Join(bucketDir, slug)
	if err := os.Mkdir(slugDir, 0o755); errors.Is(err, fs.ErrExist) {
		return "", fmt.Errorf("%s already exists — refusing to overwrite", slugDir)
	} else if err != nil {
		return "", err
	}
	// slugDir is this call's own, so removing it loses nothing and keeps a
	// retry from refusing a half-built folder.
	fail := func(err error) (string, error) {
		if rmErr := os.RemoveAll(slugDir); rmErr != nil {
			err = errors.Join(err, fmt.Errorf("remove partial %s: %w", slugDir, rmErr))
		}
		return "", err
	}

	replacer := strings.NewReplacer(
		"{{CREATED}}", now.Format("2006-01-02"),
		"{{TITLE}}", slug,
	)
	files := []struct{ template, path string }{
		{"bucket-doc", filepath.Join(slugDir, "index.md")},
		{"bucket-stage", filepath.Join(slugDir, "01_raw", "index.md")},
	}
	for _, f := range files {
		tmpl, err := bucketTemplate(f.template)
		if err != nil {
			return fail(err)
		}
		if err := writeFileAtomic(f.path, []byte(replacer.Replace(tmpl))); err != nil {
			return fail(err)
		}
	}

	return files[0].path, nil
}

// ScaffoldBucketSkill writes the bucket's per-realm SKILL.md. A pre-existing
// file is a no-op returning (path, nil), never an overwrite.
func ScaffoldBucketSkill(realmRoot, bucketName, bucketDir string) (string, error) {
	target := filepath.Join(realmRoot, ".claude", "skills", bucketName+"-management", "SKILL.md")
	if _, err := os.Lstat(target); err == nil {
		return target, nil // already exists — no-op
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("bucket skill: stat %s: %w", target, err)
	}

	tmpl, err := bucketTemplate("bucket-skill")
	if err != nil {
		return "", err
	}

	content := strings.NewReplacer(
		"{{BUCKET}}", bucketName,
		"{{PURPOSE}}", bucketPurposeLine(bucketDir),
	).Replace(tmpl)

	if err := writeFileAtomic(target, []byte(content)); err != nil {
		return "", err
	}

	return target, nil
}

func bucketPurposeLine(bucketDir string) string {
	return DeriveMemberDescription(filepath.Join(bucketDir, "index.md"))
}
