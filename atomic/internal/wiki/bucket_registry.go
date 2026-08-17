package wiki

// Registry primitives for `atomic wiki bucket`, covering two surfaces: the
// machine-managed <wiki-buckets> block in wiki/index.md (idempotent splice,
// mirroring <wiki-scan>) and the ## Capture surfaces section in realm
// CLAUDE.md, whose heading is written once and gains a bullet per bucket.

import (
	"fmt"
	"os"
	"strings"
)

const bucketsMarkerOpen = "<wiki-buckets"
const bucketsMarkerClose = "</wiki-buckets>"

const captureSurfacesHeading = "## Capture surfaces"

// spliceBucketEntry registers a bucket in indexPath's <wiki-buckets> block,
// creating the file or block when absent and preserving prior content. Adding
// a name that is already registered is a no-op; any declined="true" attribute
// is dropped, since the realm clearly is using buckets now.
func spliceBucketEntry(indexPath, name, absPath string) error {
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

// rewriteBucketsBlock appends a fresh block whenever the existing one is
// absent or malformed, rather than trying to repair it.
func rewriteBucketsBlock(content, name, absPath string) string {
	entry := fmt.Sprintf(`<bucket name=%q path=%q/>`, name, absPath)

	openIdx := strings.Index(content, bucketsMarkerOpen)
	if openIdx == -1 {
		if content != "" && !strings.HasSuffix(content, "\n") {
			content += "\n"
		}
		return content + "\n" + buildBucketsBlock([]string{entry})
	}

	openTagEnd := strings.Index(content[openIdx:], ">")
	if openTagEnd == -1 {
		return content + "\n" + buildBucketsBlock([]string{entry})
	}
	openTagEnd += openIdx + 1

	closeIdx := strings.Index(content[openTagEnd:], bucketsMarkerClose)
	if closeIdx == -1 {
		return content + "\n" + buildBucketsBlock([]string{entry})
	}
	blockBodyStart := openTagEnd
	blockBodyEnd := openTagEnd + closeIdx
	blockBody := content[blockBodyStart:blockBodyEnd]

	blockEnd := blockBodyEnd + len(bucketsMarkerClose)

	openTag := content[openIdx:openTagEnd]

	if strings.Contains(blockBody, fmt.Sprintf(`name=%q`, name)) {
		return content
	}

	newOpenTag := strings.ReplaceAll(openTag, ` declined="true"`, "")

	newBody := blockBody
	if !strings.HasSuffix(newBody, "\n") {
		newBody += "\n"
	}
	newBody += entry + "\n"

	before := content[:openIdx]
	after := content[blockEnd:]

	return before + newOpenTag + newBody + bucketsMarkerClose + after
}

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

// readBucketEntries returns nil, no error, when the file or block is absent.
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

// BucketEntry is one parsed <bucket> tag. Exported for consumers such as
// atomic serve's nav tree.
type BucketEntry struct {
	Name string
	Path string
}

type bucketEntry = BucketEntry

// ReadBucketEntries returns nil, no error, when the file or block is absent;
// only a genuine read failure errors.
func ReadBucketEntries(indexPath string) ([]BucketEntry, error) {
	return readBucketEntries(indexPath)
}

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

// findCaptureSurfacesHeading matches only at a line start, so the heading text
// inside a paragraph, backtick span, or code fence cannot false-match.
func findCaptureSurfacesHeading(content string) int {
	if strings.HasPrefix(content, captureSurfacesHeading+"\n") ||
		content == captureSurfacesHeading {
		return 0
	}
	needle := "\n" + captureSurfacesHeading
	idx := strings.Index(content, needle)
	if idx == -1 {
		return -1
	}
	return idx + 1 // skip the anchoring newline
}

// writeCaptureSurfacesSection appends one bullet per bucket to realm
// CLAUDE.md, creating the file or the section when absent and never
// duplicating the heading. Prior content survives byte-for-byte.
func writeCaptureSurfacesSection(claudeMDPath, name, absPath string) error {
	data, err := os.ReadFile(claudeMDPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("capture surfaces: read %s: %w", claudeMDPath, err)
	}

	bullet := fmt.Sprintf("- `%s` <!-- describe what this bucket is for -->", absPath)

	var newContent string
	if os.IsNotExist(err) || len(data) == 0 {
		newContent = captureSurfacesHeading + "\n\n" + bullet + "\n"
	} else {
		content := string(data)
		idx := findCaptureSurfacesHeading(content)
		if idx == -1 {
			// EOF-append only, so nothing lands inside an existing <...> block.
			if !strings.HasSuffix(content, "\n") {
				content += "\n"
			}
			newContent = content + "\n" + captureSurfacesHeading + "\n\n" + bullet + "\n"
		} else {
			// The section ends at the next line-anchored ## heading, or EOF.
			// Searching "\n## " rather than "## " makes it structurally
			// impossible to match our own heading at offset 0 of after.
			after := content[idx+len(captureSurfacesHeading):]
			nextH2 := strings.Index(after, "\n## ")
			var insertPos int
			if nextH2 == -1 {
				insertPos = len(content)
			} else {
				insertPos = idx + len(captureSurfacesHeading) + nextH2
			}
			before := strings.TrimRight(content[:insertPos], "\n")
			afterInsert := content[insertPos:]
			if strings.Contains(before, absPath) {
				return nil
			}
			newContent = before + "\n" + bullet + "\n" + afterInsert
		}
	}

	return writeFileAtomic(claudeMDPath, []byte(newContent))
}

// createBucketIndexStub writes <bucketDir>/index.md, or nothing when one
// already exists. The stub includes an empty `<bucket-docs>` region so the
// first RebuildBucketIndex fills a well-formed region in place instead of
// appending one at EOF.
func createBucketIndexStub(bucketDir, name string) error {
	indexPath := fmt.Sprintf("%s/index.md", bucketDir)
	if _, err := os.Lstat(indexPath); err == nil {
		return nil
	}

	var sb strings.Builder
	sb.WriteString("---\n")
	fmt.Fprintf(&sb, "title: %s\n", name)
	sb.WriteString("type: Bucket\n")
	sb.WriteString("description: <Describe what this bucket is for>\n")
	sb.WriteString("---\n\n")
	fmt.Fprintf(&sb, "# %s\n\n", name)
	sb.WriteString("<!-- Describe what this bucket is for -->\n\n")
	sb.WriteString("## Conventions\n\n")
	sb.WriteString("<!-- Add conventions for files in this bucket -->\n\n")
	sb.WriteString(renderManagedRegion(managedRegion{tag: "bucket-docs"}))
	sb.WriteString("\n")

	return writeFileAtomic(indexPath, []byte(sb.String()))
}
