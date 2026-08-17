package extraction

// Generic config-driven string-literal harvester for embedded-SQL extraction.
// Callers supply the per-language node kinds; the harvester picks a shape.
// Shape 1 (content-child grammars): join ContentKinds descendants in source
// order, interpolations as "?". Shape 2 (inline-content grammars): the node
// carries its delimiters and content inline, so splice "?" over interpolation
// byte ranges and strip the delimiter alphabet off both ends. Node kinds and
// shape assignment come from grammar probing — see
// docs/spec/embedded-sql-language-expansion.md.

import (
	"context"
	"sort"
	"strings"

	sitter "github.com/malivvan/tree-sitter"
)

// EmbeddedSpan mirrors standalone.StringLiteralSpan field-for-field; the
// indexer converts. A separate type exists only because standalone imports this
// package, so the reverse import would cycle.
type EmbeddedSpan struct {
	Text      string // literal content after delimiter stripping / interpolation substitution
	StartLine int    // 1-based file-absolute line of the opening delimiter
	EndLine   int    // 1-based file-absolute line of the closing delimiter
}

// EmbeddedLiteralConfig carries the node-kind sets that vary per language.
type EmbeddedLiteralConfig struct {
	// StringKinds are top-level string literals. The walk stops here, so
	// strings nested inside a harvested literal are not harvested again.
	StringKinds map[string]bool

	// ContentKinds carry raw literal text. One such descendant selects Shape 1.
	ContentKinds map[string]bool

	// InterpKinds are interpolation segments; each becomes "?" in the text.
	InterpKinds map[string]bool
}

// HarvestEmbeddedLiterals returns one span per non-empty string literal in src.
// Lines are 1-based and file-absolute. The caller borrows and returns inst.
func HarvestEmbeddedLiterals(
	ctx context.Context,
	inst Instance,
	src string,
	lang Lang,
	cfg EmbeddedLiteralConfig,
) ([]EmbeddedSpan, error) {
	if err := inst.SetLanguage(ctx, lang); err != nil {
		return nil, err
	}

	tree, err := inst.ParseString(ctx, src)
	if err != nil {
		return nil, err
	}

	root, err := tree.(*tsTree).rootNode(ctx)
	if err != nil {
		return nil, err
	}

	lineOffsets := buildLineOffsets(src)

	var spans []EmbeddedSpan
	embWalkNode(ctx, root, src, lineOffsets, cfg, &spans)
	return spans, nil
}

// embWalkNode is best-effort: a Kind() error skips the subtree silently.
func embWalkNode(
	ctx context.Context,
	node sitter.Node,
	src string,
	lineOffsets []int,
	cfg EmbeddedLiteralConfig,
	out *[]EmbeddedSpan,
) {
	kind, err := node.Kind(ctx)
	if err != nil {
		return // best-effort: skip this subtree
	}

	if cfg.StringKinds[kind] {
		span := embHarvestString(ctx, node, src, lineOffsets, cfg)
		if span != nil {
			*out = append(*out, *span)
		}
		return // do not recurse into string children
	}

	cnt, err := node.NamedChildCount(ctx)
	if err != nil {
		return
	}
	for i := uint64(0); i < cnt; i++ {
		child, err := node.NamedChild(ctx, i)
		if err != nil {
			continue
		}
		embWalkNode(ctx, child, src, lineOffsets, cfg, out)
	}
}

// embHarvestString applies Shape 1 or Shape 2; nil when the text comes out empty.
func embHarvestString(
	ctx context.Context,
	node sitter.Node,
	src string,
	lineOffsets []int,
	cfg EmbeddedLiteralConfig,
) *EmbeddedSpan {
	startByte, err := node.StartByte(ctx)
	if err != nil {
		return nil
	}
	endByte, err := node.EndByte(ctx)
	if err != nil {
		return nil
	}

	startLine := pyByteToLine(lineOffsets, startByte)
	endLine := pyByteToLine(lineOffsets, endByte)

	// One pass collects both descendant sets; whether contentDescs is empty
	// picks the shape.
	type descNode struct {
		kind      string
		startByte uint64
		endByte   uint64
		node      sitter.Node
	}

	var contentDescs []descNode
	var interpDescs []descNode

	// Stops at nested StringKinds nodes so a harvested literal is not re-entered.
	var collectDescendants func(n sitter.Node)
	collectDescendants = func(n sitter.Node) {
		cnt, err := n.NamedChildCount(ctx)
		if err != nil {
			return
		}
		for i := uint64(0); i < cnt; i++ {
			child, err := n.NamedChild(ctx, i)
			if err != nil {
				continue
			}
			childKind, err := child.Kind(ctx)
			if err != nil {
				continue
			}
			if cfg.StringKinds[childKind] {
				continue
			}
			csb, err := child.StartByte(ctx)
			if err != nil {
				continue
			}
			ceb, err := child.EndByte(ctx)
			if err != nil {
				continue
			}
			if cfg.ContentKinds[childKind] {
				contentDescs = append(contentDescs, descNode{childKind, csb, ceb, child})
				continue // stop-at-leaf: do not recurse into content nodes
			}
			if cfg.InterpKinds[childKind] {
				interpDescs = append(interpDescs, descNode{childKind, csb, ceb, child})
				continue // stop-at-leaf: do not recurse into interp nodes
			}
			// Content/interp can hide under wrapper nodes (PHP/Ruby heredocs).
			collectDescendants(child)
		}
	}
	collectDescendants(node)

	var text string

	if len(contentDescs) > 0 {
		// Shape 1: content + interp descendants merged in source order.
		type seg struct {
			startByte uint64
			endByte   uint64
			isInterp  bool
		}
		segs := make([]seg, 0, len(contentDescs)+len(interpDescs))
		for _, d := range contentDescs {
			segs = append(segs, seg{d.startByte, d.endByte, false})
		}
		for _, d := range interpDescs {
			segs = append(segs, seg{d.startByte, d.endByte, true})
		}
		sort.Slice(segs, func(i, j int) bool {
			return segs[i].startByte < segs[j].startByte
		})

		var parts []string
		for _, s := range segs {
			if s.isInterp {
				parts = append(parts, "?")
			} else {
				if int(s.endByte) <= len(src) && s.startByte < s.endByte {
					parts = append(parts, src[s.startByte:s.endByte])
				}
			}
		}
		text = strings.Join(parts, "")
	} else {
		// Shape 2: splice interps into the node's own text, descending so the
		// remaining offsets stay valid, then strip delimiters.
		if int(endByte) > len(src) || startByte >= endByte {
			return nil
		}
		nodeSrc := src[startByte:endByte]

		type interp struct {
			relStart int
			relEnd   int
		}
		interps := make([]interp, 0, len(interpDescs))
		for _, d := range interpDescs {
			rs := int(d.startByte) - int(startByte)
			re := int(d.endByte) - int(startByte)
			if rs >= 0 && re <= len(nodeSrc) && rs < re {
				interps = append(interps, interp{rs, re})
			}
		}
		sort.Slice(interps, func(i, j int) bool {
			return interps[i].relStart > interps[j].relStart // descending
		})

		result := nodeSrc
		for _, ip := range interps {
			result = result[:ip.relStart] + "?" + result[ip.relEnd:]
		}

		result = embStripDelimiters(result)
		text = result
	}

	if text == "" {
		return nil
	}

	return &EmbeddedSpan{
		Text:      text,
		StartLine: startLine,
		EndLine:   endLine,
	}
}

// delimAlphabet is the delimiter set for inline-content (Shape 2) grammars.
const delimAlphabet = "\"'`@[]="

// embStripDelimiters strips delimiter runs off both ends of s. Safe because SQL
// always begins with a letter, so the leading strip cannot eat content.
func embStripDelimiters(s string) string {
	start := 0
	for start < len(s) && strings.ContainsRune(delimAlphabet, rune(s[start])) {
		start++
	}
	s = s[start:]

	end := len(s)
	for end > 0 && strings.ContainsRune(delimAlphabet, rune(s[end-1])) {
		end--
	}
	return s[:end]
}
