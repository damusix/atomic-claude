// Package standalone provides extractors for file formats that are not
// handled by the generic tree-sitter grammar pipeline. Each extractor
// implements the same extract(filePath, source) → (ExtractionResult, error)
// pattern described in appendix E of the spec.
//
// Formats:
//   - Vue SFC (.vue)     — component node + JS/TS script extraction; content pre-padded so sub-extractor line numbers and node IDs are file-absolute
//   - Svelte (.svelte)   — component node + JS script extraction; content pre-padded so sub-extractor line numbers and node IDs are file-absolute
//   - Liquid (.liquid)   — template node + render/include references
//   - Delphi DFM (.dfm)  — form + nested object nodes
//   - MyBatis XML (.xml) — mapper + statement nodes + namespace reference
//
// The Registry (For method) maps file extensions to the correct Extractor,
// for use by the orchestrator (CP10).
package standalone

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction/languages"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// ---------------------------------------------------------------------------
// Extractor interface
// ---------------------------------------------------------------------------

// Extractor is the common interface for all standalone format extractors.
// It mirrors the extract() signature from appendix E.
type Extractor interface {
	Extract(filePath, source string) (types.ExtractionResult, error)
}

// ---------------------------------------------------------------------------
// Registry
// ---------------------------------------------------------------------------

// Registry maps file extensions to Extractor instances.
type Registry struct {
	entries map[string]Extractor
}

// NewRegistry constructs a Registry wired with all 5 standalone extractors.
// pool is required for Vue and Svelte (which run the JS/TS tree-sitter extractor
// on embedded script blocks); the other formats are regex-based and ignore pool.
func NewRegistry(pool *extraction.Pool) *Registry {
	entries := map[string]Extractor{
		".vue":    NewVueExtractor(pool),
		".svelte": NewSvelteExtractor(pool),
		".liquid": NewLiquidExtractor(),
		".dfm":    NewDFMExtractor(),
		".fmx":    NewDFMExtractor(),
		".xml":    NewMyBatisExtractor(),
	}
	// SQL (dialect-agnostic regex extractor; pool not required).
	// Extensions come from the canonical SQLExtensions slice so all consumers
	// stay in sync with a single source of truth.
	sqlExt := NewSQLExtractor()
	for _, ext := range SQLExtensions {
		entries[ext] = sqlExt
	}
	return &Registry{entries: entries}
}

// For returns the Extractor for the given file extension (e.g. ".vue"), or nil
// when the extension is not a registered standalone format.
func (r *Registry) For(ext string) Extractor {
	return r.entries[strings.ToLower(ext)]
}

// ---------------------------------------------------------------------------
// Shared helpers
// ---------------------------------------------------------------------------

// componentNode builds a root component node at line 1 for the given filePath.
func componentNode(filePath string) types.Node {
	name := fileBaseName(filePath)
	id := extraction.GenerateNodeID(filePath, string(types.NodeKindComponent), name, 1)
	return types.Node{
		ID:            id,
		Kind:          types.NodeKindComponent,
		Name:          name,
		QualifiedName: name,
		FilePath:      filePath,
		Language:      types.LanguageUnknown, // overridden per format
		StartLine:     1,
		EndLine:       1,
		IsExported:    true,
	}
}

// fileBaseName returns the file name without extension, used as component name.
func fileBaseName(filePath string) string {
	base := filepath.Base(filePath)
	ext := filepath.Ext(base)
	if ext != "" {
		return base[:len(base)-len(ext)]
	}
	return base
}

// containsEdge builds a contains edge from source to target.
func containsEdge(sourceID, targetID string) types.Edge {
	return types.Edge{
		Source: sourceID,
		Target: targetID,
		Kind:   types.EdgeKindContains,
	}
}

// ---------------------------------------------------------------------------
// Vue SFC extractor
// ---------------------------------------------------------------------------

// scriptTagRE matches a <script ...> block (optionally with "setup" attribute).
// Group 1 = full attribute string (may include "setup"), group 2 = script content.
// It is intentionally simple: HTML comments inside attributes are not supported.
var scriptTagRE = regexp.MustCompile(`(?si)<script([^>]*)>(.*?)</script>`)

// templateTagRE matches PascalCase or kebab-case component tags inside <template>.
// Group 1 = the tag name. Self-closing and opening tags are both matched.
var templateTagRE = regexp.MustCompile(`<([A-Z][a-zA-Z0-9]*|[a-z][a-z0-9]*(?:-[a-z0-9]+)+)[\s/>]`)

// templateBlockRE matches the <template> block content.
var templateBlockRE = regexp.MustCompile(`(?si)<template[^>]*>(.*?)</template>`)

// handlerBindingRE matches Vue event binding attributes in both forms:
//   - @event="handlerName"    (shorthand)
//   - v-on:event="handlerName" (long form)
//
// Group 1 = handler method name. Matches single- and double-quoted values.
// Only simple identifier handlers (method names) are captured; inline
// expressions like @click="count++" are ignored because they contain
// non-identifier characters.
var handlerBindingRE = regexp.MustCompile(`(?:@|v-on:)[a-zA-Z][a-zA-Z0-9\-]*(?:\.[a-zA-Z]+)*=["']([a-zA-Z_$][a-zA-Z0-9_$]*)["']`)

// VueExtractor extracts from .vue Single-File Components.
type VueExtractor struct {
	tsExt *extraction.TreeSitterExtractor
	jsExt *extraction.TreeSitterExtractor
}

// NewVueExtractor returns a VueExtractor backed by the given pool.
// Both TypeScript and JavaScript extractors are wired (script lang="" defaults
// to JS; lang="ts" uses TS).
func NewVueExtractor(pool *extraction.Pool) *VueExtractor {
	return &VueExtractor{
		tsExt: extraction.NewTreeSitterExtractor(pool, extraction.LangTypeScript, languages.TypeScriptExtractor()),
		jsExt: extraction.NewTreeSitterExtractor(pool, extraction.LangJavaScript, languages.JavaScriptExtractor()),
	}
}

// Extract implements Extractor for .vue files.
func (e *VueExtractor) Extract(filePath, source string) (types.ExtractionResult, error) {
	comp := componentNode(filePath)
	comp.Language = types.LanguageVue

	result := types.ExtractionResult{
		Nodes: []types.Node{comp},
	}

	// --- Script block ---
	scriptMatches := scriptTagRE.FindAllStringSubmatchIndex(source, -1)
	for _, m := range scriptMatches {
		// m[0]:m[1] = full match; m[2]:m[3] = attrs; m[4]:m[5] = content
		if len(m) < 6 {
			continue
		}
		attrs := source[m[2]:m[3]]
		content := source[m[4]:m[5]]

		// Pad content with leading newlines so the sub-extractor computes
		// file-absolute line numbers from the start. This ensures that
		// GenerateNodeID hashes the file-absolute line — matching StartLine —
		// rather than the script-relative line. The approach mirrors what
		// ExtractEmbeddedSQL does (embedded_sql.go: "pad with leading newlines
		// so line numbers are file-absolute"). No post-hoc line shift is applied
		// to nodes, edges, or refs: the padding already encodes the offset.
		//
		// contentLineOffset = number of newlines in source before the first
		// byte of script content. Prepending that many newlines makes
		// script-relative line N become file-absolute line N+contentLineOffset.
		contentLineOffset := strings.Count(source[:m[4]], "\n")
		paddedContent := strings.Repeat("\n", contentLineOffset) + content

		// Run the appropriate extractor on the padded script content.
		isTS := strings.Contains(attrs, `lang="ts"`) || strings.Contains(attrs, `lang='ts'`)
		var scriptResult types.ExtractionResult
		if isTS {
			ctx := context.Background()
			scriptResult = e.tsExt.Extract(ctx, filePath, paddedContent, types.LanguageTypeScript)
		} else {
			ctx := context.Background()
			scriptResult = e.jsExt.Extract(ctx, filePath, paddedContent, types.LanguageJavaScript)
		}

		// Strip the file: node that the tree-sitter extractor emits (we have
		// our component node already). No line-offset loop needed: padding
		// makes all positions file-absolute.
		for _, n := range scriptResult.Nodes {
			if n.Kind == types.NodeKindFile {
				continue
			}
			result.Nodes = append(result.Nodes, n)
		}

		// Append edges. No line-offset needed: positions are already file-absolute.
		for _, edge := range scriptResult.Edges {
			// Re-wire: if edge source is the file: node, replace with component ID.
			src := edge.Source
			if src == "file:"+filePath {
				src = comp.ID
			}
			edge.Source = src
			result.Edges = append(result.Edges, edge)
		}

		// Append unresolved refs. No line-offset needed: positions are file-absolute.
		for _, ref := range scriptResult.UnresolvedReferences {
			result.UnresolvedReferences = append(result.UnresolvedReferences, ref)
		}

		result.Errors = append(result.Errors, scriptResult.Errors...)
	}

	// Note: contains edges (component → script children) are already emitted via
	// the re-wiring of the tree-sitter extractor's "file:→child" edges above.
	// No additional contains edges needed here.

	// --- Template block: PascalCase/kebab component tags → references ---
	templateRefs := extractTemplateRefs(filePath, source, comp.ID, types.LanguageVue)
	result.UnresolvedReferences = append(result.UnresolvedReferences, templateRefs...)

	// --- Template block: @event="handler" / v-on:event="handler" → references ---
	handlerRefs := extractHandlerRefs(filePath, source, comp.ID, types.LanguageVue)
	result.UnresolvedReferences = append(result.UnresolvedReferences, handlerRefs...)

	return result, nil
}

// extractHandlerRefs scans the template block for @event="handler" and
// v-on:event="handler" bindings and emits an UnresolvedReference for each
// distinct handler method name. The ref has ReferenceKind=references so the
// standard resolution pipeline resolves it to the <script> method node, and
// VueHandlerSynthesizer converts the resulting references edge into a calls
// edge from the component to the handler method.
func extractHandlerRefs(filePath, source, fromNodeID string, lang types.Language) []types.UnresolvedReference {
	templateMatch := templateBlockRE.FindStringSubmatchIndex(source)
	if templateMatch == nil {
		return nil
	}
	templateContent := source[templateMatch[2]:templateMatch[3]]
	templateStartByte := templateMatch[2]

	var refs []types.UnresolvedReference
	seen := map[string]struct{}{}

	for _, m := range handlerBindingRE.FindAllStringSubmatchIndex(templateContent, -1) {
		handlerName := templateContent[m[2]:m[3]]
		if _, dup := seen[handlerName]; dup {
			continue
		}
		seen[handlerName] = struct{}{}

		byteOffset := templateStartByte + m[2]
		line := strings.Count(source[:byteOffset], "\n") + 1

		refs = append(refs, types.UnresolvedReference{
			ID:            extraction.GenerateRefID(fromNodeID, handlerName, string(types.EdgeKindReferences), line, 0),
			FromNodeID:    fromNodeID,
			ReferenceName: handlerName,
			ReferenceKind: types.EdgeKindReferences,
			Line:          line,
			FilePath:      filePath,
			Language:      lang,
		})
	}
	return refs
}

// extractTemplateRefs scans the template block for component tag references.
func extractTemplateRefs(filePath, source, fromNodeID string, lang types.Language) []types.UnresolvedReference {
	templateMatch := templateBlockRE.FindStringSubmatchIndex(source)
	if templateMatch == nil {
		return nil
	}
	templateContent := source[templateMatch[2]:templateMatch[3]]
	templateStartByte := templateMatch[2]

	var refs []types.UnresolvedReference
	seen := map[string]struct{}{}

	tagMatches := templateTagRE.FindAllStringSubmatchIndex(templateContent, -1)
	for _, m := range tagMatches {
		tagName := templateContent[m[2]:m[3]]
		if _, dup := seen[tagName]; dup {
			continue
		}
		seen[tagName] = struct{}{}

		// Skip built-in HTML elements.
		if isHTMLElement(tagName) {
			continue
		}

		byteOffset := templateStartByte + m[2]
		line := strings.Count(source[:byteOffset], "\n") + 1

		refs = append(refs, types.UnresolvedReference{
			ID:            extraction.GenerateRefID(fromNodeID, tagName, string(types.EdgeKindReferences), line, 0),
			FromNodeID:    fromNodeID,
			ReferenceName: tagName,
			ReferenceKind: types.EdgeKindReferences,
			Line:          line,
			FilePath:      filePath,
			Language:      lang,
		})
	}
	return refs
}

// isHTMLElement reports whether name is a standard HTML element name.
// Only kebab names need checking since PascalCase never appears in HTML.
var htmlElements = map[string]struct{}{
	"div": {}, "span": {}, "p": {}, "a": {}, "ul": {}, "ol": {}, "li": {},
	"h1": {}, "h2": {}, "h3": {}, "h4": {}, "h5": {}, "h6": {},
	"table": {}, "thead": {}, "tbody": {}, "tr": {}, "th": {}, "td": {},
	"form": {}, "input": {}, "button": {}, "select": {}, "option": {},
	"textarea": {}, "label": {}, "img": {}, "video": {}, "audio": {},
	"nav": {}, "header": {}, "footer": {}, "main": {}, "section": {},
	"article": {}, "aside": {}, "figure": {}, "figcaption": {},
	"strong": {}, "em": {}, "code": {}, "pre": {}, "blockquote": {},
	"br": {}, "hr": {}, "template": {},
}

func isHTMLElement(name string) bool {
	_, ok := htmlElements[strings.ToLower(name)]
	return ok
}

// ---------------------------------------------------------------------------
// Svelte extractor
// ---------------------------------------------------------------------------

// capitalTagRE matches capitalized component tags (PascalCase) in Svelte markup.
var capitalTagRE = regexp.MustCompile(`<([A-Z][a-zA-Z0-9]*)[\s/>]`)

// SvelteExtractor extracts from .svelte files.
type SvelteExtractor struct {
	jsExt *extraction.TreeSitterExtractor
}

// NewSvelteExtractor returns a SvelteExtractor backed by the given pool.
// Svelte's <script> defaults to JavaScript (no lang= switching supported here).
func NewSvelteExtractor(pool *extraction.Pool) *SvelteExtractor {
	return &SvelteExtractor{
		jsExt: extraction.NewTreeSitterExtractor(pool, extraction.LangJavaScript, languages.JavaScriptExtractor()),
	}
}

// Extract implements Extractor for .svelte files.
func (e *SvelteExtractor) Extract(filePath, source string) (types.ExtractionResult, error) {
	comp := componentNode(filePath)
	comp.Language = types.LanguageSvelte

	result := types.ExtractionResult{
		Nodes: []types.Node{comp},
	}

	// --- Script block ---
	scriptMatches := scriptTagRE.FindAllStringSubmatchIndex(source, -1)
	for _, m := range scriptMatches {
		if len(m) < 6 {
			continue
		}
		content := source[m[4]:m[5]]
		// Pad content with leading newlines so the sub-extractor computes
		// file-absolute line numbers from the start (mirrors Vue and the
		// embedded-SQL approach in embedded_sql.go). No post-hoc line shift
		// is applied to nodes, edges, or refs.
		contentLineOffset := strings.Count(source[:m[4]], "\n")
		paddedContent := strings.Repeat("\n", contentLineOffset) + content

		ctx := context.Background()
		scriptResult := e.jsExt.Extract(ctx, filePath, paddedContent, types.LanguageJavaScript)

		// No line-offset loop: padding makes all positions file-absolute.
		for _, n := range scriptResult.Nodes {
			if n.Kind == types.NodeKindFile {
				continue
			}
			result.Nodes = append(result.Nodes, n)
		}
		for _, edge := range scriptResult.Edges {
			src := edge.Source
			if src == "file:"+filePath {
				src = comp.ID
			}
			edge.Source = src
			result.Edges = append(result.Edges, edge)
		}
		for _, ref := range scriptResult.UnresolvedReferences {
			result.UnresolvedReferences = append(result.UnresolvedReferences, ref)
		}
		result.Errors = append(result.Errors, scriptResult.Errors...)
	}

	// contains edges are already emitted via re-wiring of "file:→child" edges.

	// --- Capitalized component tags → references ---
	// Scan outside <script> and <style> blocks.
	markup := stripScriptAndStyle(source)
	seen := map[string]struct{}{}
	for _, m := range capitalTagRE.FindAllStringSubmatchIndex(markup, -1) {
		tagName := markup[m[2]:m[3]]
		if _, dup := seen[tagName]; dup {
			continue
		}
		seen[tagName] = struct{}{}

		byteOffset := m[2]
		line := strings.Count(markup[:byteOffset], "\n") + 1
		result.UnresolvedReferences = append(result.UnresolvedReferences, types.UnresolvedReference{
			FromNodeID:    comp.ID,
			ReferenceName: tagName,
			ReferenceKind: types.EdgeKindReferences,
			Line:          line,
			FilePath:      filePath,
			Language:      types.LanguageSvelte,
		})
	}

	return result, nil
}

// stripScriptAndStyle removes <script>...</script> and <style>...</style>
// blocks from source, replacing them with whitespace to preserve line numbers.
func stripScriptAndStyle(source string) string {
	reBlock := regexp.MustCompile(`(?si)<(script|style)[^>]*>.*?</(script|style)>`)
	return reBlock.ReplaceAllStringFunc(source, func(match string) string {
		// Replace content with same number of newlines to keep line numbers stable.
		return strings.Repeat("\n", strings.Count(match, "\n"))
	})
}

// ---------------------------------------------------------------------------
// Liquid extractor
// ---------------------------------------------------------------------------

// liquidRenderRE matches {% render 'name' %} or {% render "name" %}.
var liquidRenderRE = regexp.MustCompile(`{%-?\s*render\s+['"]([^'"]+)['"]`)

// liquidIncludeRE matches {% include 'name' %} or {% include "name" %}.
var liquidIncludeRE = regexp.MustCompile(`{%-?\s*include\s+['"]([^'"]+)['"]`)

// LiquidExtractor extracts from .liquid Shopify/Liquid template files.
type LiquidExtractor struct{}

// NewLiquidExtractor returns a LiquidExtractor (no pool required).
func NewLiquidExtractor() *LiquidExtractor {
	return &LiquidExtractor{}
}

// Extract implements Extractor for .liquid files.
func (e *LiquidExtractor) Extract(filePath, source string) (types.ExtractionResult, error) {
	comp := componentNode(filePath)
	comp.Language = types.LanguageLiquid

	result := types.ExtractionResult{
		Nodes: []types.Node{comp},
	}

	seen := map[string]struct{}{}
	addRef := func(name string, line int) {
		if _, dup := seen[name]; dup {
			return
		}
		seen[name] = struct{}{}
		result.UnresolvedReferences = append(result.UnresolvedReferences, types.UnresolvedReference{
			FromNodeID:    comp.ID,
			ReferenceName: name,
			ReferenceKind: types.EdgeKindReferences,
			Line:          line,
			FilePath:      filePath,
			Language:      types.LanguageLiquid,
		})
	}

	for _, m := range liquidRenderRE.FindAllStringSubmatchIndex(source, -1) {
		name := source[m[2]:m[3]]
		line := strings.Count(source[:m[0]], "\n") + 1
		addRef(name, line)
	}
	for _, m := range liquidIncludeRE.FindAllStringSubmatchIndex(source, -1) {
		name := source[m[2]:m[3]]
		line := strings.Count(source[:m[0]], "\n") + 1
		addRef(name, line)
	}

	return result, nil
}

// ---------------------------------------------------------------------------
// Delphi DFM extractor
// ---------------------------------------------------------------------------

// dfmObjectRE matches "object Name: TType" lines.
// Group 1 = object name, group 2 = type name.
var dfmObjectRE = regexp.MustCompile(`(?im)^\s*object\s+(\w+)\s*:\s*(\w+)`)

// DFMExtractor extracts from Delphi Form Definition (.dfm, .fmx) files.
type DFMExtractor struct{}

// NewDFMExtractor returns a DFMExtractor (no pool required).
func NewDFMExtractor() *DFMExtractor {
	return &DFMExtractor{}
}

// Extract implements Extractor for .dfm files.
// It emits a component node for each "object Name: TType" block.
// The first object encountered is treated as the root form.
func (e *DFMExtractor) Extract(filePath, source string) (types.ExtractionResult, error) {
	result := types.ExtractionResult{}

	matches := dfmObjectRE.FindAllStringSubmatchIndex(source, -1)
	if len(matches) == 0 {
		// No objects found: emit a single placeholder component node.
		comp := componentNode(filePath)
		comp.Language = types.LanguagePascal
		result.Nodes = append(result.Nodes, comp)
		return result, nil
	}

	var rootID string
	for i, m := range matches {
		objName := source[m[2]:m[3]]
		line := strings.Count(source[:m[0]], "\n") + 1
		id := extraction.GenerateNodeID(filePath, string(types.NodeKindComponent), objName, line)
		node := types.Node{
			ID:            id,
			Kind:          types.NodeKindComponent,
			Name:          objName,
			QualifiedName: objName,
			FilePath:      filePath,
			Language:      types.LanguagePascal,
			StartLine:     line,
			EndLine:       line,
			IsExported:    i == 0, // root form is exported
		}
		result.Nodes = append(result.Nodes, node)

		if i == 0 {
			rootID = id
		} else {
			result.Edges = append(result.Edges, containsEdge(rootID, id))
		}
	}

	return result, nil
}

// ---------------------------------------------------------------------------
// MyBatis XML extractor
// ---------------------------------------------------------------------------

// mybatisMapperRE matches <mapper namespace="..."> or <mapper namespace='...'>
var mybatisMapperRE = regexp.MustCompile(`(?i)<mapper\s[^>]*namespace\s*=\s*['"]([^'"]+)['"]`)

// mybatisStatementRE matches <select|insert|update|delete id="...">
var mybatisStatementRE = regexp.MustCompile(`(?i)<(select|insert|update|delete)\s[^>]*\bid\s*=\s*['"]([^'"]+)['"]`)

// MyBatisExtractor extracts from MyBatis XML mapper files.
type MyBatisExtractor struct{}

// NewMyBatisExtractor returns a MyBatisExtractor (no pool required).
func NewMyBatisExtractor() *MyBatisExtractor {
	return &MyBatisExtractor{}
}

// Extract implements Extractor for MyBatis XML files.
// It emits a module node for the <mapper> element and function nodes for each
// statement (select/insert/update/delete), plus a reference from the mapper to
// its namespace Java class.
func (e *MyBatisExtractor) Extract(filePath, source string) (types.ExtractionResult, error) {
	result := types.ExtractionResult{}

	// --- Mapper root node ---
	namespace := ""
	mapperLine := 1
	if m := mybatisMapperRE.FindStringSubmatchIndex(source); m != nil {
		namespace = source[m[2]:m[3]]
		mapperLine = strings.Count(source[:m[0]], "\n") + 1
	}

	mapperName := namespace
	if mapperName == "" {
		mapperName = fileBaseName(filePath)
	}
	mapperID := extraction.GenerateNodeID(filePath, string(types.NodeKindModule), mapperName, mapperLine)
	mapperNode := types.Node{
		ID:            mapperID,
		Kind:          types.NodeKindModule,
		Name:          mapperName,
		QualifiedName: mapperName,
		FilePath:      filePath,
		Language:      types.LanguageXML,
		StartLine:     mapperLine,
		EndLine:       mapperLine,
		IsExported:    true,
	}
	result.Nodes = append(result.Nodes, mapperNode)

	// Namespace → references the Java interface.
	if namespace != "" {
		result.UnresolvedReferences = append(result.UnresolvedReferences, types.UnresolvedReference{
			FromNodeID:    mapperID,
			ReferenceName: namespace,
			ReferenceKind: types.EdgeKindReferences,
			Line:          mapperLine,
			FilePath:      filePath,
			Language:      types.LanguageXML,
		})
	}

	// --- Statement nodes ---
	for _, m := range mybatisStatementRE.FindAllStringSubmatchIndex(source, -1) {
		stmtKind := strings.ToLower(source[m[2]:m[3]])
		stmtID := source[m[4]:m[5]]
		line := strings.Count(source[:m[0]], "\n") + 1

		qualName := fmt.Sprintf("%s.%s", mapperName, stmtID)
		nodeID := extraction.GenerateNodeID(filePath, string(types.NodeKindFunction), qualName, line)
		node := types.Node{
			ID:            nodeID,
			Kind:          types.NodeKindFunction,
			Name:          stmtID,
			QualifiedName: qualName,
			FilePath:      filePath,
			Language:      types.LanguageXML,
			StartLine:     line,
			EndLine:       line,
			IsExported:    true,
			Metadata:      []byte(fmt.Sprintf(`{"statement_kind":%q}`, stmtKind)),
		}
		result.Nodes = append(result.Nodes, node)
		result.Edges = append(result.Edges, containsEdge(mapperID, nodeID))
	}

	return result, nil
}
