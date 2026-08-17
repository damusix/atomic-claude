package extraction

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	sitter "github.com/malivvan/tree-sitter"
)

// generateNodeID returns the stable identifier for a code node. line is 1-based.
// Edges store ids by value, so any drift in the formula orphans every edge —
// the golden-vector test in helpers_test.go is the gate.
func generateNodeID(filePath, kind, name string, line int) string {
	return GenerateNodeID(filePath, kind, name, line)
}

// GenerateNodeID is the exported form, for sibling packages (extraction/
// standalone) that must produce identical ids. Package nodes key off ecosystem
// + name because a package has no file or line; the npm segment is hardcoded —
// JS-family only, see docs/design/code-intel-package-nodes.md.
func GenerateNodeID(filePath, kind, name string, line int) string {
	if kind == "file" {
		return "file:" + filePath
	}
	if kind == "package" {
		return "package:npm/" + name
	}
	input := fmt.Sprintf("%s:%s:%s:%d", filePath, kind, name, line)
	sum := sha256.Sum256([]byte(input))
	return kind + ":" + hex.EncodeToString(sum[:])[:32]
}

// GenerateRefID identifies one unresolved reference site. The "ref:" prefix
// keeps the id space disjoint from node ids. Line and col separate distinct call
// sites for the same callee; sites identical in all five inputs collide by
// design, and INSERT OR IGNORE in db/resolution.go dedupes them.
func GenerateRefID(fromNodeID, referenceName, referenceKind string, line, col int) string {
	input := fmt.Sprintf("%s:%s:%s:%d:%d", fromNodeID, referenceName, referenceKind, line, col)
	sum := sha256.Sum256([]byte(input))
	return "ref:" + hex.EncodeToString(sum[:])[:32]
}

// nodeText slices source by a node's half-open range. Byte offsets, not rune
// indexes — they come straight from sitter.Node.
func nodeText(startByte, endByte uint64, source string) string {
	if startByte >= endByte || int(endByte) > len(source) {
		return ""
	}
	return source[startByte:endByte]
}

// childByField returns the named child for a grammar field. A missing field is
// (nil, nil), not an error — tree-sitter returns a null node, which the WASM
// ts_node_is_null export distinguishes from a real child.
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

// precedingDocstring returns the contiguous comment block directly above
// nodeStartByte, markers intact, joined by newlines; a blank or non-comment line
// breaks the chain. Pure byte scan over source, no WASM round-trips.
func precedingDocstring(nodeStartByte uint64, source string) string {
	if nodeStartByte == 0 || int(nodeStartByte) > len(source) {
		return ""
	}

	before := source[:nodeStartByte]
	before = strings.TrimRight(before, "\n")
	lines := strings.Split(before, "\n")

	var commentLines []string
	for i := len(lines) - 1; i >= 0; i-- {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "" {
			break
		}
		if strings.HasPrefix(trimmed, "//") {
			commentLines = append(commentLines, trimmed)
			continue
		}
		if strings.HasPrefix(trimmed, "/*") || strings.HasSuffix(trimmed, "*/") {
			commentLines = append(commentLines, trimmed)
			continue
		}
		break
	}

	if len(commentLines) == 0 {
		return ""
	}

	// Collected bottom-up; restore source order.
	for l, r := 0, len(commentLines)-1; l < r; l, r = l+1, r-1 {
		commentLines[l], commentLines[r] = commentLines[r], commentLines[l]
	}
	return strings.Join(commentLines, "\n")
}
