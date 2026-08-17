package wiki

// Read-only freshness comparator for `atomic wiki stale`. It re-walks the root
// and compares against the recorded <wiki-scan> block, reporting membership
// drift and per-artifact content drift.
//
// Everything unverifiable is fail-safe: a missing, unreadable, or garbled
// fingerprint reports stale rather than fresh, because a page whose baseline
// cannot be read cannot be proven current.

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

// Stale writes DRIFT/STALE lines to out and returns 0 fresh / 1 stale /
// 2 hard error. Only data lines reach out; a hard error comes back as an error
// value so the caller can route diagnostics to stderr without polluting the
// stream. It modifies nothing.
func Stale(root string, out io.Writer) (int, error) {
	wikiDir := filepath.Join(root, "wiki")
	indexPath := filepath.Join(wikiDir, "index.md")

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

	block := extractBlockContent(string(data))
	if block == "" {
		return StaleCodeError, fmt.Errorf("no <wiki-scan> block in %s", indexPath)
	}

	recorded := parseBlockMembers(block)

	current, err := discoverMembers(root, wikiDir)
	if err != nil {
		return StaleCodeError, fmt.Errorf("discover members: %w", err)
	}

	// Nil prior map: we want live classification, not preserved summarized
	// state — the block already records what was claimed.
	classified := classifyMembers(root, wikiDir, current, nil)

	currentByPath := map[string]Member{}
	for _, m := range classified {
		currentByPath[m.Path] = m
	}

	var lines []string
	stale := false

	// Membership and status drift.
	for path := range recorded {
		if _, ok := currentByPath[path]; !ok {
			lines = append(lines, fmt.Sprintf("DRIFT removed %s", path))
			stale = true
		}
	}

	for _, m := range classified {
		rec, inBlock := recorded[m.Path]
		if !inBlock {
			lines = append(lines, fmt.Sprintf("DRIFT added %s", m.Path))
			stale = true
			continue
		}
		if rec.status != m.Status {
			lines = append(lines, fmt.Sprintf("DRIFT status %s %s→%s", m.Path, rec.status, m.Status))
			stale = true
		}
	}

	// Summary drift: each repos/*.md against its repo's git HEAD.
	reposDir := filepath.Join(wikiDir, "repos")
	repoFiles, _ := filepath.Glob(filepath.Join(reposDir, "*.md"))
	// Domain-split repos live at repos/<repo>/<domain>.md.
	subRepoFiles, _ := filepath.Glob(filepath.Join(reposDir, "*", "*.md"))
	repoFiles = append(repoFiles, subRepoFiles...)

	for _, fp := range repoFiles {
		wikiRel, err := filepath.Rel(root, fp)
		if err != nil {
			wikiRel = fp
		}
		wikiPath := wikiRel

		// Relative to wikiDir — the form Member.SummaryPath is recorded in.
		summaryRel, err := filepath.Rel(wikiDir, fp)
		if err != nil {
			lines = append(lines, fmt.Sprintf("STALE summary %s", wikiPath))
			stale = true
			continue
		}

		memberPath, resolved := resolveSummaryMember(summaryRel, classified)
		if !resolved {
			lines = append(lines, fmt.Sprintf("STALE summary %s", wikiPath))
			stale = true
			continue
		}
		repoDir := filepath.Join(root, memberPath)

		doc, readErr := os.ReadFile(fp)
		if readErr != nil {
			lines = append(lines, fmt.Sprintf("STALE summary %s", wikiPath))
			stale = true
			continue
		}

		meta, _, parseErr := frontmatter.Parse(string(doc))
		if parseErr != nil || meta == nil {
			lines = append(lines, fmt.Sprintf("STALE summary %s", wikiPath))
			stale = true
			continue
		}

		reflectsRev, ok := meta["reflects_rev"]
		if !ok {
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

		// A repo with no HEAD is stale, never a hard error.
		currentSHA, gitErr := gitRevParseHead(repoDir)
		if gitErr != nil {
			lines = append(lines, fmt.Sprintf("STALE summary %s", wikiPath))
			stale = true
			continue
		}

		if currentSHA != revStr {
			lines = append(lines, fmt.Sprintf("STALE summary %s", wikiPath))
			stale = true
		}
	}

	// Concern drift: each reflects: entry against its source's live fingerprint.
	concernsDir := filepath.Join(wikiDir, "concerns")
	concernFiles, _ := filepath.Glob(filepath.Join(concernsDir, "*.md"))

	for _, fp := range concernFiles {
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
			lines = append(lines, fmt.Sprintf("STALE concern %s", wikiPath))
			stale = true
			continue
		}

		rawReflects, ok := meta["reflects"]
		if !ok {
			lines = append(lines, fmt.Sprintf("STALE concern %s", wikiPath))
			stale = true
			continue
		}

		entries, ok := rawReflects.([]any)
		if !ok {
			lines = append(lines, fmt.Sprintf("STALE concern %s", wikiPath))
			stale = true
			continue
		}

		for _, entry := range entries {
			entryStr, ok := entry.(string)
			if !ok {
				lines = append(lines, fmt.Sprintf("STALE concern %s", wikiPath))
				stale = true
				break
			}

			// Entries are "<id>@<fingerprint>".
			at := strings.LastIndex(entryStr, "@")
			if at == -1 || at == 0 || at == len(entryStr)-1 {
				lines = append(lines, fmt.Sprintf("STALE concern %s", wikiPath))
				stale = true
				break
			}

			id := entryStr[:at]
			recordedFP := entryStr[at+1:]

			// Knowledge-page ids resolve against wikiDir; repo ids against root.
			resolveRoot := root
			if strings.HasPrefix(id, "knowledge/") && strings.HasSuffix(id, ".md") {
				resolveRoot = wikiDir
			}
			currentFP, ok := resolveFingerprint(resolveRoot, id)
			if !ok {
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

	// Bucket drift. A walk error escalates to exit 2 instead of a STALE line:
	// freshness is undetermined, and a STALE line would misstate the cause.
	bucketEntries := parseBucketEntries(string(data))
	var bucketLines []string
	for _, be := range bucketEntries {
		diff, diffErr := bucketDiffReadOnly(wikiDir, be.Name, be.Path)
		if diffErr != nil {
			return StaleCodeError, fmt.Errorf("bucket %q: %w", be.Name, diffErr)
		}
		if len(diff.Added)+len(diff.Changed)+len(diff.Removed) > 0 {
			bucketLines = append(bucketLines, fmt.Sprintf("STALE bucket %s", be.Name))
			stale = true
		}
	}

	// Bucket lines sort among themselves and always follow the rest, so the
	// two sections stay distinct in the output stream.
	sort.Strings(lines)
	sort.Strings(bucketLines)
	for _, l := range lines {
		fmt.Fprintln(out, l)
	}
	for _, l := range bucketLines {
		fmt.Fprintln(out, l)
	}

	if stale {
		return StaleCodeStale, nil
	}
	return StaleCodeFresh, nil
}

// resolveSummaryMember maps a wikiDir-relative summary path to its owning
// Member.Path. See docs/spec/wiki-stale-summary-resolution.md.
func resolveSummaryMember(summaryRel string, classified []Member) (string, bool) {
	// A claimed SummaryPath is authoritative: it is what scan wrote.
	for _, m := range classified {
		if m.SummaryPath == "" {
			continue
		}
		if m.SummaryPath == summaryRel {
			return m.Path, true
		}
		if strings.HasSuffix(m.SummaryPath, "/") && strings.HasPrefix(summaryRel, m.SummaryPath) {
			return m.Path, true
		}
	}

	// Fall back to discoverSummary's naming convention, inverted: a graduated
	// member carries an empty SummaryPath even with a leftover summary on
	// disk, and without this that summary would report a false STALE.
	stem := summaryStem(summaryRel)
	if stem == "" {
		return "", false
	}
	match, matches := "", 0
	for _, m := range classified {
		if filepath.Base(m.Path) == stem {
			match = m.Path
			matches++
		}
	}
	if matches == 1 {
		return match, true
	}

	// Zero or ambiguous matches: report stale rather than guess.
	return "", false
}

// summaryStem recovers the repo base name from a summary path: both
// "repos/<name>.md" and "repos/<name>/<domain>.md" yield "<name>".
func summaryStem(summaryRel string) string {
	rel := strings.TrimPrefix(summaryRel, "repos/")
	if rel == summaryRel {
		return ""
	}
	if idx := strings.Index(rel, "/"); idx != -1 {
		return rel[:idx]
	}
	return strings.TrimSuffix(rel, ".md")
}

// parseBlockMembers is the read counterpart to wiki.go's block writer.
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
