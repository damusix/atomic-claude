package wiki

// Scaffolds behind the `atomic wiki bucket doc` and `skill` verbs. Both take
// an already-resolved bucket directory and never consult the <wiki-buckets>
// registry: confirming the bucket is registered is the caller's job.

import (
	"embed"
	"fmt"
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
		return "", fmt.Errorf("bucket doc: embedded template %q: %w", name, err)
	}
	return string(data), nil
}

// slugPattern matches knowledge topic naming — see docs/spec/wiki-buckets.md.
var slugPattern = regexp.MustCompile(`^[a-z0-9-]+$`)

func validateSlug(slug string) error {
	if !slugPattern.MatchString(slug) {
		return fmt.Errorf("bucket doc: invalid slug %q — must match [a-z0-9-]+ (lowercase letters, digits, hyphens)", slug)
	}
	return nil
}

// ScaffoldBucketDoc writes <bucketDir>/<slug>.md, and with router also its
// sibling subtree. An existing <slug>.md aborts before any write, so a
// collision never leaves a half-built router behind.
func ScaffoldBucketDoc(bucketDir, slug string, router bool, now time.Time) (string, error) {
	if err := validateSlug(slug); err != nil {
		return "", err
	}

	target := filepath.Join(bucketDir, slug+".md")
	if _, err := os.Lstat(target); err == nil {
		return "", fmt.Errorf("bucket doc: %s already exists — refusing to overwrite", target)
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("bucket doc: stat %s: %w", target, err)
	}

	tmpl, err := bucketTemplate("bucket-doc")
	if err != nil {
		return "", err
	}

	content := strings.NewReplacer(
		"{{CREATED}}", now.Format("2006-01-02"),
		"{{TITLE}}", slug,
	).Replace(tmpl)

	if err := writeFileAtomic(target, []byte(content)); err != nil {
		return "", err
	}

	if router {
		if err := routerScaffold(bucketDir, slug); err != nil {
			return "", err
		}
	}

	return target, nil
}

// routerScaffold creates the <slug>/ subtree and its CLAUDE.md stub, never
// overwriting either.
func routerScaffold(bucketDir, slug string) error {
	subtreeDir := filepath.Join(bucketDir, slug)
	if err := os.MkdirAll(subtreeDir, 0o755); err != nil {
		return fmt.Errorf("bucket doc: mkdir %s: %w", subtreeDir, err)
	}

	claudePath := filepath.Join(subtreeDir, "CLAUDE.md")
	if _, err := os.Lstat(claudePath); err == nil {
		return nil // already present — never overwrite
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("bucket doc: stat %s: %w", claudePath, err)
	}

	tmpl, err := bucketTemplate("bucket-router-claude")
	if err != nil {
		return err
	}
	content := strings.ReplaceAll(tmpl, "{{SLUG}}", slug)

	return writeFileAtomic(claudePath, []byte(content))
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
