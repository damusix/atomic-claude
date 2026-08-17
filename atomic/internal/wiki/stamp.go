package wiki

// Fingerprint-stamp helpers. Every write goes through internal/frontmatter, so
// the rest of the file survives byte-for-byte, and code writes the values while
// the model supplies which sources to cite.

import (
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/frontmatter"
)

// StampSummary records repoPath's current HEAD as the summary's reflects_rev.
func StampSummary(path, repoPath string) error {
	sha, err := gitRevParseHead(repoPath)
	if err != nil {
		return fmt.Errorf("stamp summary: %w", err)
	}
	return updateFrontmatterKey(path, "reflects_rev", sha)
}

// StampConcern writes the concern's reflects: list as "<id>@<fingerprint>"
// entries, skipping any id it cannot resolve rather than failing the stamp.
func StampConcern(path, wikiRoot string, citedIDs []string) error {
	entries := []any{}
	for _, id := range citedIDs {
		fp, ok := resolveFingerprint(wikiRoot, id)
		if !ok {
			continue
		}
		entries = append(entries, fmt.Sprintf("%s@%s", id, fp))
	}

	return updateFrontmatterKey(path, "reflects", entries)
}

// StampKnowledge replaces a knowledge page's sources: list with the caller's
// opaque entries. A missing file is an error: the inferrer authors pages, stamp
// only updates them.
func StampKnowledge(path string, sources []string) error {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return fmt.Errorf("stamp knowledge: %s does not exist", path)
	}
	entries := make([]any, len(sources))
	for i, s := range sources {
		entries[i] = s
	}
	return updateFrontmatterKey(path, "sources", entries)
}

// resolveFingerprint picks the fingerprint that fits the cited id: a content
// hash for a knowledge page or an indexed repo's signals.md, git HEAD for a
// summarized repo. An unavailable source returns ("", false).
func resolveFingerprint(wikiRoot, id string) (string, bool) {
	if strings.HasPrefix(id, "knowledge/") && strings.HasSuffix(id, ".md") {
		knowledgePath := filepath.Join(wikiRoot, id)
		data, err := os.ReadFile(knowledgePath)
		if err != nil {
			return "", false
		}
		h := sha256.Sum256(data)
		return fmt.Sprintf("%x", h), true
	}

	repoDir := filepath.Join(wikiRoot, id)

	signalsMD := filepath.Join(repoDir, ".claude", "project", "signals.md")
	if data, err := os.ReadFile(signalsMD); err == nil {
		h := sha256.Sum256(data)
		return fmt.Sprintf("%x", h), true
	}

	sha, err := gitRevParseHead(repoDir)
	if err != nil {
		return "", false
	}
	return sha, true
}

func gitRevParseHead(dir string) (string, error) {
	out, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		return "", fmt.Errorf("git rev-parse HEAD at %s: %w", dir, err)
	}
	return strings.TrimSpace(string(out)), nil
}

// updateFrontmatterKey rewrites one key, leaving the other keys and the body
// untouched.
func updateFrontmatterKey(path, key string, value any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("stamp: read %s: %w", path, err)
	}

	meta, body, err := frontmatter.Parse(string(data))
	if err != nil {
		return fmt.Errorf("stamp: parse frontmatter of %s: %w", path, err)
	}

	if meta == nil {
		meta = map[string]any{}
	}
	meta[key] = value

	doc, err := frontmatter.Emit(meta, body)
	if err != nil {
		return fmt.Errorf("stamp: emit frontmatter for %s: %w", path, err)
	}

	return os.WriteFile(path, []byte(doc), 0o644)
}
