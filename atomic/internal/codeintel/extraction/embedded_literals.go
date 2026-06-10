package extraction

// embedded_literals.go — generic config-driven string-literal harvester for
// embedded-SQL extraction across the 16 remaining tree-sitter languages.
//
// HarvestEmbeddedLiterals is the single entry point. Callers supply an
// EmbeddedLiteralConfig that encodes which tree-sitter node kinds are string
// literals, raw-text content children, and interpolation children for the
// target language. The algorithm then applies one of two extraction shapes:
//
//   Shape 1 (content-child grammars): the string node has descendant nodes
//   whose kind ∈ ContentKinds. Walk descendants in source order; content →
//   append its text; interpolation → append "?"; join.
//
//   Shape 2 (inline-content grammars): the string node carries delimiters and
//   content inline (no raw-text child). Take the node's own source text;
//   for each interpolation descendant in descending byte order splice "?" over
//   its byte range; then strip a leading and trailing run of delimiter
//   alphabet chars: " ' ` @ [ ] =
//
// Node types and shape assignment verified by grammar probing — see
// docs/spec/embedded-sql-language-expansion.md § Grammar node-kind config.
//
// Return type note: this package cannot import extraction/standalone (cycle:
// standalone/sql.go imports extraction). EmbeddedSpan mirrors
// standalone.StringLiteralSpan field-for-field; the indexer (CP2) converts.

import (
	"context"
	"sort"
	"strings"

	sitter "github.com/malivvan/tree-sitter"
)

// EmbeddedSpan holds one harvested string literal span. It mirrors
// standalone.StringLiteralSpan field-for-field; the indexer (CP2) converts
// between them. A separate type is required because extraction/standalone
// imports extraction (for GenerateNodeID), so extraction cannot import
// standalone without a cycle.
type EmbeddedSpan struct {
	Text      string // literal content after delimiter stripping / interpolation substitution
	StartLine int    // 1-based file-absolute line of the opening delimiter
	EndLine   int    // 1-based file-absolute line of the closing delimiter
}

// EmbeddedLiteralConfig carries the three node-kind sets that vary per
// language. All three are map[string]bool for O(1) membership (per project
// preference over slice.contains).
type EmbeddedLiteralConfig struct {
	// StringKinds: node kinds that represent top-level string literals.
	// DFS stops recursing at a StringKinds node (avoids harvesting nested
	// string nodes inside an already-harvested literal).
	StringKinds map[string]bool

	// ContentKinds: node kinds that carry raw literal text (no delimiters).
	// Presence of ≥1 ContentKinds descendant inside a StringKinds node
	// selects Shape 1.
	ContentKinds map[string]bool

	// InterpKinds: node kinds that represent interpolation segments (e.g.
	// ${…}, #{…}, \(…)). These become "?" in the harvested text.
	InterpKinds map[string]bool
}

// HarvestEmbeddedLiterals parses src in the given language, walks the AST,
// and returns one EmbeddedSpan per string literal whose harvested text is
// non-empty. Line numbers are 1-based and file-absolute.
//
// The caller is responsible for borrowing inst from a pool and returning it
// after this call.
//
// Returns (nil, nil) when the source has no string literals.
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

// embWalkNode recursively walks the AST. At a StringKinds node it harvests and
// stops recursing. Best-effort: Kind() errors skip the subtree silently.
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

	// General descent.
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

// embHarvestString harvests one string-literal node using Shape 1 or Shape 2.
// Returns nil when the resulting text is empty.
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

	// Determine shape by collecting descendants. A single pass collects both
	// content and interpolation descendants.
	type descNode struct {
		kind      string
		startByte uint64
		endByte   uint64
		node      sitter.Node
	}

	var contentDescs []descNode
	var interpDescs []descNode

	// collectDescendants walks descendants of n (excluding n itself), stopping
	// at nested StringKinds nodes (do not harvest inside a harvested literal).
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
				// Nested string node — do not descend into it.
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
			// Recurse into non-string, non-content, non-interp nodes to find
			// deeper content/interp descendants (e.g. PHP/Ruby heredoc bodies).
			collectDescendants(child)
		}
	}
	collectDescendants(node)

	var text string

	if len(contentDescs) > 0 {
		// Shape 1: walk all content + interp descendants in source order.
		// Build a unified sorted list tagged with type.
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
		// Shape 2: node's own text with interpolations spliced in descending
		// byte order, then delimiter alphabet stripped.
		if int(endByte) > len(src) || startByte >= endByte {
			return nil
		}
		nodeSrc := src[startByte:endByte]

		// Sort interp descendants in descending byte order for in-place splicing.
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

		// Strip leading and trailing delimiter-alphabet characters.
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

// delimAlphabet is the set of characters that may appear as delimiters around
// string content in inline-content (Shape 2) grammars.
// Characters: double-quote, single-quote, backtick, at-sign, open-bracket,
// close-bracket, equals.
const delimAlphabet = "\"'`@[]="

// embStripDelimiters strips a leading run and a trailing run of delimiter
// alphabet characters from s. SQL always begins with a letter (A-Z/a-z), so
// the leading strip cannot eat SQL content.
func embStripDelimiters(s string) string {
	// Strip leading delimiters.
	start := 0
	for start < len(s) && strings.ContainsRune(delimAlphabet, rune(s[start])) {
		start++
	}
	s = s[start:]

	// Strip trailing delimiters.
	end := len(s)
	for end > 0 && strings.ContainsRune(delimAlphabet, rune(s[end-1])) {
		end--
	}
	return s[:end]
}
