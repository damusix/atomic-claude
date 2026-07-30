package wiki

// bucketdoc.go — CP4 scaffold placement: the two `atomic wiki bucket`
// authoring verbs (doc, skill) that CP5's CLI actions call into.
//
// Both verbs are pure filesystem scaffolds: validate, refuse a collision,
// render an embedded template, write via writeFileAtomic. Registration
// (is <bucket> a known bucket name?) is the caller's job — ScaffoldBucketDoc
// and ScaffoldBucketSkill take the resolved bucket directory directly and
// never consult the <wiki-buckets> registry themselves.

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

// bucketTemplate reads an embedded scaffold template by name (the filename
// without the .md extension). These templates are compiled into the binary
// like doctemplate's skeletons — they are NOT install artifacts and are
// never wired into the bundle mirror.
func bucketTemplate(name string) (string, error) {
	data, err := bucketTemplatesFS.ReadFile("templates/" + name + ".md")
	if err != nil {
		return "", fmt.Errorf("bucket doc: embedded template %q: %w", name, err)
	}
	return string(data), nil
}

// slugPattern is the kebab-case discipline for bucket doc slugs — same
// pattern discipline as knowledge topic names (docs/spec/wiki-buckets.md).
var slugPattern = regexp.MustCompile(`^[a-z0-9-]+$`)

// validateSlug enforces the kebab-case topic-name pattern. An empty slug is
// rejected (the pattern requires at least one character).
func validateSlug(slug string) error {
	if !slugPattern.MatchString(slug) {
		return fmt.Errorf("bucket doc: invalid slug %q — must match [a-z0-9-]+ (lowercase letters, digits, hyphens)", slug)
	}
	return nil
}

// ScaffoldBucketDoc validates slug, refuses a collision with an existing
// <bucketDir>/<slug>.md, and writes the topic file from the embedded
// bucket-doc scaffold with `created` stamped from now. When router is true,
// it additionally scaffolds the sibling <slug>/ subtree via routerScaffold.
//
// The doc write and the router subtree are independent once the collision
// check passes: a pre-existing subtree does not block the doc write and
// vice versa. But a pre-existing <slug>.md always aborts before either
// write happens — the router subtree is never created on a collision.
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

// routerScaffold creates the sibling <slug>/ subtree directory and its
// CLAUDE.md stub from the embedded bucket-router-claude scaffold. Both are
// skipped when already present — MkdirAll is naturally idempotent for the
// directory; the CLAUDE.md file is checked explicitly so it is never
// overwritten.
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

// ScaffoldBucketSkill writes <realmRoot>/.claude/skills/<bucketName>-management/SKILL.md
// from the embedded bucket-skill scaffold, pre-filled with the bucket name
// and its purpose line (bucketPurposeLine). A pre-existing file is a no-op:
// the path is returned with a nil error and the file is never overwritten.
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

// bucketPurposeLine reads <bucketDir>/index.md and returns its description
// via the same ladder as DeriveMemberDescription (frontmatter description ->
// first prose line), or "" when neither is present or index.md is absent.
func bucketPurposeLine(bucketDir string) string {
	return DeriveMemberDescription(filepath.Join(bucketDir, "index.md"))
}
