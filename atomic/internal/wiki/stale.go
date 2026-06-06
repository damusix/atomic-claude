package wiki

// stale.go — CP4 read-only freshness comparator for `atomic wiki stale`.
//
// Stale re-walks the root (reusing discoverMembers + classifyMembers), parses
// the recorded <wiki-scan> block from wiki/index.md, then reports:
//
//   - Membership drift: added (in tree, not in block), removed (in block, not
//     in tree), status flip (recorded status ≠ current status).
//
//   - Per-artifact content drift:
//     • repos/<repo>.md: reads reflects_rev, compares to current git HEAD of
//       that repo. Missing/unparseable → stale. No HEAD → stale (not error).
//     • concerns/<concern>.md: reads reflects: list; for each <id>@<fp> entry
//       recomputes the current fingerprint (HEAD SHA for summarized, signals.md
//       sha256 for indexed) and compares. Garbled entry → stale.
//
// Output: one line per finding, literal prefixes as required by the spec.
// Exit codes (returned by Stale): 0 fresh, 1 stale, 2 hard error.

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/frontmatter"
)

// StaleResult codes mirror the signals stale convention.
const (
	StaleCodeFresh = 0
	StaleCodeStale = 1
	StaleCodeError = 2
)

// Stale is the entry point for `atomic wiki stale`.
// It writes DRIFT/STALE data lines to out and returns an exit code plus any
// hard error. Hard errors (wiki/ absent, unreadable index, git missing, etc.)
// are returned as a non-nil error so the caller can route them to stderr
// separately from the structured data stream. Only DRIFT/STALE lines are
// written to out — diagnostic error text is never mixed into the data stream.
// It is read-only — it never modifies any file.
//
// Return values:
//
//	(0, nil)   — fresh; no DRIFT/STALE lines emitted
//	(1, nil)   — stale; one or more DRIFT/STALE lines emitted
//	(2, error) — hard error; no DRIFT/STALE lines emitted; caller prints error
func Stale(root string, out io.Writer) (int, error) {
	wikiDir := filepath.Join(root, "wiki")
	indexPath := filepath.Join(wikiDir, "index.md")

	// Hard-error guard: wiki/ or index.md absent means we can't operate.
	if _, err := os.Lstat(wikiDir); err != nil {
		return StaleCodeError, fmt.Errorf("wiki/ not found at %s", wikiDir)
	}

	data, err := os.ReadFile(indexPath)
	if err != nil {
		if os.IsNotExist(err) {
			return StaleCodeError, fmt.Errorf("index.md not found at %s", indexPath)
		}
		return StaleCodeError, fmt.Errorf("read index.md: %w", err)
	}

	// Parse the <wiki-scan> block.
	block := extractBlockContent(string(data))
	if block == "" {
		return StaleCodeError, fmt.Errorf("no <wiki-scan> block in %s", indexPath)
	}

	// Build a map of recorded members from the block.
	recorded := parseBlockMembers(block)

	// Re-walk the root to get current membership.
	current, err := discoverMembers(root, wikiDir)
	if err != nil {
		return StaleCodeError, fmt.Errorf("discover members: %w", err)
	}

	// Classify current members (read-only; Scan writes — we only read here).
	// Use an empty prior map because we want the live classification, not a
	// preservation of prior summarized state — the block already records that.
	classified := classifyMembers(root, wikiDir, current, nil)

	// Build current set indexed by path.
	currentByPath := map[string]Member{}
	for _, m := range classified {
		currentByPath[m.Path] = m
	}

	var lines []string
	stale := false

	// --- 1. Membership / status drift ---

	// Removed: in block but not in current tree.
	for path := range recorded {
		if _, ok := currentByPath[path]; !ok {
			lines = append(lines, fmt.Sprintf("DRIFT removed %s", path))
			stale = true
		}
	}

	// Added + status drift: in current tree.
	for _, m := range classified {
		rec, inBlock := recorded[m.Path]
		if !inBlock {
			lines = append(lines, fmt.Sprintf("DRIFT added %s", m.Path))
			stale = true
			continue
		}
		// Status drift: only compare when neither is summarized (summarized is
		// an extra state written by /refresh-wiki, not by scan).
		if rec.status != m.Status {
			lines = append(lines, fmt.Sprintf("DRIFT status %s %s→%s", m.Path, rec.status, m.Status))
			stale = true
		}
	}

	// --- 2. Per-artifact content drift ---

	// 2a. Summary files (repos/<name>.md).
	reposDir := filepath.Join(wikiDir, "repos")
	repoFiles, _ := filepath.Glob(filepath.Join(reposDir, "*.md"))
	// Also check sub-dirs (domain-split repos use repos/<repo>/<domain>.md).
	subRepoFiles, _ := filepath.Glob(filepath.Join(reposDir, "*", "*.md"))
	repoFiles = append(repoFiles, subRepoFiles...)

	for _, fp := range repoFiles {
		// Derive the repo path from the summary file location.
		// repos/<name>.md  → name is the repo path fragment relative to root.
		// repos/<dir>/<domain>.md → dir is the repo path.
		rel, err := filepath.Rel(reposDir, fp)
		if err != nil {
			continue
		}
		// The repo dir name is the first component of the relative path under repos/.
		repoName := strings.SplitN(rel, string(filepath.Separator), 2)[0]
		repoDir := filepath.Join(root, repoName)

		// wikiRel is relative to root (e.g. "wiki/repos/repoA.md").
		wikiRel, err := filepath.Rel(root, fp)
		if err != nil {
			wikiRel = fp
		}
		wikiPath := wikiRel

		doc, readErr := os.ReadFile(fp)
		if readErr != nil {
			// Unreadable → stale (fail-safe).
			lines = append(lines, fmt.Sprintf("STALE summary %s", wikiPath))
			stale = true
			continue
		}

		meta, _, parseErr := frontmatter.Parse(string(doc))
		if parseErr != nil || meta == nil {
			// No frontmatter / parse error → stale (fail-safe).
			lines = append(lines, fmt.Sprintf("STALE summary %s", wikiPath))
			stale = true
			continue
		}

		reflectsRev, ok := meta["reflects_rev"]
		if !ok {
			// Missing reflects_rev → stale.
			lines = append(lines, fmt.Sprintf("STALE summary %s", wikiPath))
			stale = true
			continue
		}

		revStr, ok := reflectsRev.(string)
		if !ok || revStr == "" {
			lines = append(lines, fmt.Sprintf("STALE summary %s", wikiPath))
			stale = true
			continue
		}

		// Compare to current HEAD. No HEAD → stale (not error).
		currentSHA, gitErr := gitRevParseHead(repoDir)
		if gitErr != nil {
			// No HEAD or git error → always-needs-summary.
			lines = append(lines, fmt.Sprintf("STALE summary %s", wikiPath))
			stale = true
			continue
		}

		if currentSHA != revStr {
			lines = append(lines, fmt.Sprintf("STALE summary %s", wikiPath))
			stale = true
		}
	}

	// 2b. Concern files (concerns/<name>.md).
	concernsDir := filepath.Join(wikiDir, "concerns")
	concernFiles, _ := filepath.Glob(filepath.Join(concernsDir, "*.md"))

	for _, fp := range concernFiles {
		// wikiRel is relative to root (e.g. "wiki/concerns/foo.md").
		wikiRel, err := filepath.Rel(root, fp)
		if err != nil {
			wikiRel = fp
		}
		wikiPath := wikiRel

		doc, readErr := os.ReadFile(fp)
		if readErr != nil {
			lines = append(lines, fmt.Sprintf("STALE concern %s", wikiPath))
			stale = true
			continue
		}

		meta, _, parseErr := frontmatter.Parse(string(doc))
		if parseErr != nil || meta == nil {
			// No frontmatter or unparseable frontmatter → stale (fail-safe).
			// A concern with no recorded fingerprint baseline can't be proven
			// fresh — re-author it.
			lines = append(lines, fmt.Sprintf("STALE concern %s", wikiPath))
			stale = true
			continue
		}

		rawReflects, ok := meta["reflects"]
		if !ok {
			// No reflects: key → stale (fail-safe). Same rationale: no baseline
			// means freshness can't be verified.
			lines = append(lines, fmt.Sprintf("STALE concern %s", wikiPath))
			stale = true
			continue
		}

		entries, ok := rawReflects.([]any)
		if !ok {
			// Garbled → stale.
			lines = append(lines, fmt.Sprintf("STALE concern %s", wikiPath))
			stale = true
			continue
		}

		for _, entry := range entries {
			entryStr, ok := entry.(string)
			if !ok {
				// Garbled entry → stale.
				lines = append(lines, fmt.Sprintf("STALE concern %s", wikiPath))
				stale = true
				break
			}

			// Format: "<id>@<fingerprint>"
			at := strings.LastIndex(entryStr, "@")
			if at == -1 || at == 0 || at == len(entryStr)-1 {
				// Malformed entry → stale.
				lines = append(lines, fmt.Sprintf("STALE concern %s", wikiPath))
				stale = true
				break
			}

			id := entryStr[:at]
			recordedFP := entryStr[at+1:]

			// Resolve current fingerprint for this repo.
			currentFP, ok := resolveFingerprint(root, id)
			if !ok {
				// Can't resolve → stale (fail-safe).
				lines = append(lines, fmt.Sprintf("STALE concern %s (%s)", wikiPath, id))
				stale = true
				break
			}

			if currentFP != recordedFP {
				lines = append(lines, fmt.Sprintf("STALE concern %s (%s)", wikiPath, id))
				stale = true
				break
			}
		}
	}

	// Sort lines for deterministic output.
	sort.Strings(lines)
	for _, l := range lines {
		fmt.Fprintln(out, l)
	}

	if stale {
		return StaleCodeStale, nil
	}
	return StaleCodeFresh, nil
}

// parseBlockMembers parses the content inside a <wiki-scan> block and returns
// a map of relative repo path → priorEntry.
// This is the block-READER counterpart to the block WRITER in wiki.go.
func parseBlockMembers(blockContent string) map[string]priorEntry {
	entries := map[string]priorEntry{}
	for _, line := range strings.Split(blockContent, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "<repo ") {
			continue
		}
		path := attrValue(line, "path")
		status := attrValue(line, "status")
		summary := attrValue(line, "summary")
		if path != "" && status != "" {
			entries[path] = priorEntry{status: status, summaryAttr: summary}
		}
	}
	return entries
}
