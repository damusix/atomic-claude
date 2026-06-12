package wiki

// bucket_registry.go — CP2 registry primitives for `atomic wiki bucket`.
//
// Two concerns:
//
//  1. <wiki-buckets> block in wiki/index.md: machine-managed, idempotent splice
//     of <bucket name="…" path="…"/> entries. Mirrors the <wiki-scan> pattern.
//
//  2. ## Capture surfaces section in realm CLAUDE.md: written once on the first
//     `bucket add`. Appends a bullet per bucket; heading written once only.

import (
	"fmt"
	"os"
	"strings"
)

// bucketsMarkerOpen / bucketsMarkerClose are the block boundaries in wiki/index.md.
const bucketsMarkerOpen = "<wiki-buckets"
const bucketsMarkerClose = "</wiki-buckets>"

// captureSurfacesHeading is the heading written to realm CLAUDE.md.
const captureSurfacesHeading = "## Capture surfaces"

// spliceBucketEntry adds a `<bucket name="<name>" path="<absPath>"/>` entry
// to the <wiki-buckets> block in indexPath.
//
//   - If the block is absent, one is appended preserving all prior content.
//   - If the entry is already present (same name), the call is idempotent.
//   - If the block carries a declined="true" attribute, that attribute is removed.
//   - If indexPath does not exist, it is created containing only the block.
func spliceBucketEntry(indexPath, name, absPath string) error {
	// Read existing content.
	data, err := os.ReadFile(indexPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("bucket registry: read %s: %w", indexPath, err)
	}

	var content string
	if os.IsNotExist(err) || len(data) == 0 {
		content = ""
	} else {
		content = string(data)
	}

	newContent := rewriteBucketsBlock(content, name, absPath)
	return writeFileAtomic(indexPath, []byte(newContent))
}

// rewriteBucketsBlock rewrites the <wiki-buckets> block in content.
// If no block is present, one is appended.
// If the block has declined="true", that attribute is removed.
// If the entry already exists (same name), the block is returned as-is.
func rewriteBucketsBlock(content, name, absPath string) string {
	entry := fmt.Sprintf(`<bucket name=%q path=%q/>`, name, absPath)

	openIdx := strings.Index(content, bucketsMarkerOpen)
	if openIdx == -1 {
		// No block — append one.
		if content != "" && !strings.HasSuffix(content, "\n") {
			content += "\n"
		}
		return content + "\n" + buildBucketsBlock([]string{entry})
	}

	// Find end of the open tag line (up to ">").
	openTagEnd := strings.Index(content[openIdx:], ">")
	if openTagEnd == -1 {
		// Malformed — append.
		return content + "\n" + buildBucketsBlock([]string{entry})
	}
	openTagEnd += openIdx + 1

	// Find the close tag.
	closeIdx := strings.Index(content[openTagEnd:], bucketsMarkerClose)
	if closeIdx == -1 {
		// No close tag — append new block instead.
		return content + "\n" + buildBucketsBlock([]string{entry})
	}
	blockBodyStart := openTagEnd
	blockBodyEnd := openTagEnd + closeIdx
	blockBody := content[blockBodyStart:blockBodyEnd]

	blockEnd := blockBodyEnd + len(bucketsMarkerClose)

	// Extract the open tag (for attribute inspection).
	openTag := content[openIdx:openTagEnd]

	// Check if entry already present (idempotent).
	if strings.Contains(blockBody, fmt.Sprintf(`name=%q`, name)) {
		return content
	}

	// Remove declined="true" attribute if present.
	newOpenTag := strings.ReplaceAll(openTag, ` declined="true"`, "")

	// Append the new entry to the block body.
	newBody := blockBody
	if !strings.HasSuffix(newBody, "\n") {
		newBody += "\n"
	}
	newBody += entry + "\n"

	before := content[:openIdx]
	after := content[blockEnd:]

	return before + newOpenTag + newBody + bucketsMarkerClose + after
}

// buildBucketsBlock produces a fresh <wiki-buckets>…</wiki-buckets> block.
func buildBucketsBlock(entries []string) string {
	var sb strings.Builder
	sb.WriteString("<wiki-buckets>\n")
	for _, e := range entries {
		sb.WriteString(e)
		sb.WriteString("\n")
	}
	sb.WriteString(bucketsMarkerClose)
	sb.WriteString("\n")
	return sb.String()
}

// readBucketEntries parses the <wiki-buckets> block in indexPath and returns
// a slice of (name, path) pairs. Returns nil when the block is absent or the
// file does not exist.
func readBucketEntries(indexPath string) ([]bucketEntry, error) {
	data, err := os.ReadFile(indexPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("bucket registry: read %s: %w", indexPath, err)
	}
	return parseBucketEntries(string(data)), nil
}

// bucketEntry holds a parsed <bucket> tag from the block.
type bucketEntry struct {
	Name string
	Path string
}

// parseBucketEntries extracts bucket entries from wiki/index.md content.
func parseBucketEntries(content string) []bucketEntry {
	openIdx := strings.Index(content, bucketsMarkerOpen)
	if openIdx == -1 {
		return nil
	}
	openTagEnd := strings.Index(content[openIdx:], ">")
	if openTagEnd == -1 {
		return nil
	}
	bodyStart := openIdx + openTagEnd + 1
	closeIdx := strings.Index(content[bodyStart:], bucketsMarkerClose)
	if closeIdx == -1 {
		return nil
	}
	body := content[bodyStart : bodyStart+closeIdx]

	var entries []bucketEntry
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "<bucket ") {
			continue
		}
		n := attrValue(line, "name")
		p := attrValue(line, "path")
		if n != "" && p != "" {
			entries = append(entries, bucketEntry{Name: n, Path: p})
		}
	}
	return entries
}

// findCaptureSurfacesHeading returns the byte offset of the first line-anchored
// "## Capture surfaces" heading in content, or -1 if absent.
//
// Line-anchored means the heading must appear at the start of a line:
// either at offset 0 or immediately after a '\n'. This mirrors the discipline
// used by the <wiki-scan> and <wikis> block parsers and prevents false-positive
// matches when the text "## Capture surfaces" appears inside a prose paragraph,
// backtick span, or code block.
func findCaptureSurfacesHeading(content string) int {
	// Check if the file starts with the heading.
	if strings.HasPrefix(content, captureSurfacesHeading+"\n") ||
		content == captureSurfacesHeading {
		return 0
	}
	// Search for the heading preceded by a newline (line-anchored).
	needle := "\n" + captureSurfacesHeading
	idx := strings.Index(content, needle)
	if idx == -1 {
		return -1
	}
	// Return offset of the heading itself, not the preceding newline.
	return idx + 1
}

// writeCaptureSurfacesSection writes (or appends to) the ## Capture surfaces
// section in the realm CLAUDE.md file at claudeMDPath.
//
//   - File absent: created with only the section.
//   - Section absent: appended at EOF; all prior content preserved byte-for-byte.
//     No content is written inside any <...> block — the section is always EOF-appended
//     when absent from a real line-anchored heading.
//   - Section present: a new bullet for the bucket is appended; heading not duplicated.
func writeCaptureSurfacesSection(claudeMDPath, name, absPath string) error {
	data, err := os.ReadFile(claudeMDPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("capture surfaces: read %s: %w", claudeMDPath, err)
	}

	bullet := fmt.Sprintf("- `%s` <!-- describe what this bucket is for -->", absPath)

	var newContent string
	if os.IsNotExist(err) || len(data) == 0 {
		// Create with section only.
		newContent = captureSurfacesHeading + "\n\n" + bullet + "\n"
	} else {
		content := string(data)
		idx := findCaptureSurfacesHeading(content)
		if idx == -1 {
			// Section absent — append at EOF. No insertion inside any <...> block.
			if !strings.HasSuffix(content, "\n") {
				content += "\n"
			}
			newContent = content + "\n" + captureSurfacesHeading + "\n\n" + bullet + "\n"
		} else {
			// Section present — append bullet after last line of section.
			// Find the end of the section: next line-anchored ## heading or EOF.
			// We search for "\n## " in the text after the heading so that a
			// heading at the very start of `after` (offset 0) can never match
			// — the leading "\n" is part of the delimiter and would require a
			// preceding newline that does not exist at offset 0.  In practice
			// captureSurfacesHeading is always followed by at least "\n\n",
			// so this edge case cannot arise in well-formed content, but the
			// search pattern makes it structurally impossible regardless.
			after := content[idx+len(captureSurfacesHeading):]
			nextH2 := strings.Index(after, "\n## ")
			var insertPos int
			if nextH2 == -1 {
				insertPos = len(content)
			} else {
				// Insert before the newline that precedes the next heading.
				insertPos = idx + len(captureSurfacesHeading) + nextH2
			}
			before := strings.TrimRight(content[:insertPos], "\n")
			afterInsert := content[insertPos:]
			// Avoid duplicating the bullet if already present.
			if strings.Contains(before, absPath) {
				return nil
			}
			newContent = before + "\n" + bullet + "\n" + afterInsert
		}
	}

	return writeFileAtomic(claudeMDPath, []byte(newContent))
}

// createBucketIndexStub writes <bucketDir>/index.md if absent.
// The stub contains the bucket name as a heading and a ## Conventions placeholder.
// If index.md already exists, this is a no-op.
func createBucketIndexStub(bucketDir, name string) error {
	indexPath := fmt.Sprintf("%s/index.md", bucketDir)
	if _, err := os.Lstat(indexPath); err == nil {
		// Already exists — preserve it.
		return nil
	}

	content := fmt.Sprintf("# %s\n\n<!-- Describe what this bucket is for -->\n\n## Conventions\n\n<!-- Add conventions for files in this bucket -->\n", name)
	return writeFileAtomic(indexPath, []byte(content))
}
