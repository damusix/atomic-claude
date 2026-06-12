package wiki

// stamp.go — CP3/CP4 fingerprint-stamp helpers.
//
// StampSummary writes/updates reflects_rev in a summary file's YAML frontmatter
// from git rev-parse HEAD of the repo at repoPath.
//
// StampConcern writes/updates the reflects: YAML list in a concern file's
// frontmatter.  For each cited repo id the fingerprint is:
//   - sha256 of <wikiRoot>/knowledge/<topic>.md content   (knowledge page)
//   - sha256 of <wikiRoot>/<id>/.claude/project/signals.md (indexed)
//   - git rev-parse HEAD of <wikiRoot>/<id>               (summarized)
//
// An unresolvable cited id is silently skipped; the command never crashes.
//
// StampKnowledge writes/updates the sources: YAML list in a knowledge page's
// frontmatter. Each entry is an opaque "<bucket>/<relpath>@<sha256>" string
// supplied by the caller; code writes the value, model supplies the entries.
//
// All functions use internal/frontmatter for read/write so the rest of
// the file is preserved byte-for-byte.

import (
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/frontmatter"
)

// StampSummary reads the summary file at path, runs git rev-parse HEAD at
// repoPath, and writes/updates the reflects_rev key in the frontmatter.
// All other frontmatter keys and the body are preserved.
func StampSummary(path, repoPath string) error {
	sha, err := gitRevParseHead(repoPath)
	if err != nil {
		return fmt.Errorf("stamp summary: %w", err)
	}
	return updateFrontmatterKey(path, "reflects_rev", sha)
}

// StampConcern reads the concern file at path, resolves a fingerprint for
// each element of citedIDs under wikiRoot, and writes/updates the reflects:
// YAML list key in the frontmatter. Each entry is formatted as "<id>@<fp>".
// An unresolvable id is silently skipped.
func StampConcern(path, wikiRoot string, citedIDs []string) error {
	entries := []any{}
	for _, id := range citedIDs {
		fp, ok := resolveFingerprint(wikiRoot, id)
		if !ok {
			// Unresolvable — skip without error.
			continue
		}
		entries = append(entries, fmt.Sprintf("%s@%s", id, fp))
	}

	return updateFrontmatterKey(path, "reflects", entries)
}

// StampKnowledge reads the knowledge page at path, sets or replaces the sources:
// YAML list key to the provided entries, and writes the result back.
// All other frontmatter keys and the body are preserved.
// Returns an error if the file does not exist — the inferrer authors pages;
// stamp only updates them (consistent with StampSummary/StampConcern).
// The caller supplies the entries as opaque "<bucket>/<relpath>@<sha256hex>"
// strings; code writes the value verbatim.
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

// resolveFingerprint computes the fingerprint for the cited id under wikiRoot.
//
//   - If id matches "knowledge/<topic>.md" → sha256 of wiki/knowledge/<topic>.md
//     file content (knowledge page citation).
//   - If <wikiRoot>/<id>/.claude/project/signals.md exists → sha256 of its content
//     (indexed repo).
//   - Otherwise → git rev-parse HEAD of <wikiRoot>/<id> (summarized repo).
//
// Returns (fingerprint, true) on success, ("", false) when the source is
// unavailable (file missing, no HEAD, etc.).
func resolveFingerprint(wikiRoot, id string) (string, bool) {
	// Knowledge page citation: id = "knowledge/<topic>.md".
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

	// Check for signals.md (indexed repo).
	signalsMD := filepath.Join(repoDir, ".claude", "project", "signals.md")
	if data, err := os.ReadFile(signalsMD); err == nil {
		h := sha256.Sum256(data)
		return fmt.Sprintf("%x", h), true
	}

	// Fall back to git HEAD (summarized repo).
	sha, err := gitRevParseHead(repoDir)
	if err != nil {
		return "", false
	}
	return sha, true
}

// gitRevParseHead runs git rev-parse HEAD at dir and returns the trimmed SHA.
func gitRevParseHead(dir string) (string, error) {
	out, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		return "", fmt.Errorf("git rev-parse HEAD at %s: %w", dir, err)
	}
	return strings.TrimSpace(string(out)), nil
}

// updateFrontmatterKey reads the file at path, sets or replaces the given key
// in the YAML frontmatter to value, and writes the result back.
// All other keys and the body are preserved.
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
