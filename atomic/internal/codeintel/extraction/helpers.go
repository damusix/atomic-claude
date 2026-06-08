package extraction

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	sitter "github.com/malivvan/tree-sitter"
)

// generateNodeID returns the stable identifier for a code node.
//
// Formula (appendix B, COPY exactly):
//
//	id = kind + ":" + hex(sha256("filePath:kind:name:line"))[:32]
//
// Exception for file nodes (kind == "file"):
//
//	id = "file:" + filePath
//
// line is 1-based. Edges reference ids by value — any divergence breaks every
// edge (risk R3). The golden-vector test in helpers_test.go is the CI gate.
func generateNodeID(filePath, kind, name string, line int) string {
	return GenerateNodeID(filePath, kind, name, line)
}

// GenerateNodeID is the exported form of generateNodeID, available to sibling
// packages (e.g. extraction/standalone) that need to generate IDs using the
// same stable formula without reimplementing it.
func GenerateNodeID(filePath, kind, name string, line int) string {
	if kind == "file" {
		return "file:" + filePath
	}
	input := fmt.Sprintf("%s:%s:%s:%d", filePath, kind, name, line)
	sum := sha256.Sum256([]byte(input))
	return kind + ":" + hex.EncodeToString(sum[:])[:32]
}

// GenerateRefID returns a stable, unique identifier for one unresolved reference
// site. Mirroring the node-id scheme (appendix B) but prefixed "ref:" to keep
// the id spaces disjoint.
//
// Formula:
//
//	id = "ref:" + hex(sha256("fromNodeID:referenceName:referenceKind:line:col"))[:32]
//
// Distinct call sites for the same callee name produce distinct ids because
// line and col differ. Genuinely-identical sites (same fromNode + name + kind +
// line + col) hash to the same id — the INSERT OR IGNORE in db/resolution.go
// deduplicates them correctly.
func GenerateRefID(fromNodeID, referenceName, referenceKind string, line, col int) string {
	input := fmt.Sprintf("%s:%s:%s:%d:%d", fromNodeID, referenceName, referenceKind, line, col)
	sum := sha256.Sum256([]byte(input))
	return "ref:" + hex.EncodeToString(sum[:])[:32]
}

// nodeText slices the source string by the node's [startByte, endByte) range.
// Both values are byte offsets into source as returned by sitter.Node.StartByte
// and sitter.Node.EndByte. Returns an empty string when startByte == endByte.
func nodeText(startByte, endByte uint64, source string) string {
	if startByte >= endByte || int(endByte) > len(source) {
		return ""
	}
	return source[startByte:endByte]
}

// childByField returns the named child for a grammar field (e.g. "name",
// "body"). Returns (nil, nil) when the field does not exist for this node —
// callers must not treat a missing field as an error.
//
// The implementation uses the tree-sitter ts_node_child_by_field_name API
// wired in tsbinding/node.go. The WASM exports ts_node_is_null to distinguish
// a valid null-node return from an actual child.
func childByField(ctx context.Context, node sitter.Node, field string) (*sitter.Node, error) {
	child, err := node.ChildByFieldName(ctx, field)
	if err != nil {
		return nil, fmt.Errorf("childByField(%q): %w", field, err)
	}
	isNull, err := child.IsNull(ctx)
	if err != nil {
		return nil, fmt.Errorf("childByField(%q) IsNull: %w", field, err)
	}
	if isNull {
		return nil, nil
	}
	return &child, nil
}

// precedingDocstring scans the source text immediately before nodeStartByte and
// collects the contiguous block of line comments (// …) or block comments
// (/* … */) directly above the declaration. Any blank line between the comment
// and the declaration breaks the chain.
//
// Returns the combined comment text with "//" prefixes stripped and lines
// joined by "\n". Returns an empty string when no such comment exists.
//
// This is a pure byte-scan over the source string — no WASM calls.
func precedingDocstring(nodeStartByte uint64, source string) string {
	if nodeStartByte == 0 || int(nodeStartByte) > len(source) {
		return ""
	}

	// Split source up to the node's start into lines. We'll scan upward.
	before := source[:nodeStartByte]
	// Trim the trailing newline that immediately precedes the declaration line.
	before = strings.TrimRight(before, "\n")
	lines := strings.Split(before, "\n")

	// Walk lines backwards, collecting comment lines. A blank line (or a
	// non-comment, non-blank line) terminates the docstring scan.
	var commentLines []string
	for i := len(lines) - 1; i >= 0; i-- {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "" {
			// Blank line — docstring chain broken.
			break
		}
		if strings.HasPrefix(trimmed, "//") {
			commentLines = append(commentLines, trimmed)
			continue
		}
		if strings.HasPrefix(trimmed, "/*") || strings.HasSuffix(trimmed, "*/") {
			// Block comment line — include it.
			commentLines = append(commentLines, trimmed)
			continue
		}
		// Non-comment line — chain broken.
		break
	}

	if len(commentLines) == 0 {
		return ""
	}

	// Reverse to restore top-down order.
	for l, r := 0, len(commentLines)-1; l < r; l, r = l+1, r-1 {
		commentLines[l], commentLines[r] = commentLines[r], commentLines[l]
	}
	return strings.Join(commentLines, "\n")
}
