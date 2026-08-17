// Package standalone provides extractors for file formats the tree-sitter
// pipeline does not handle: Vue SFC, Svelte, Liquid, Delphi DFM, MyBatis XML,
// and SQL. Registry maps a file extension to its Extractor for the orchestrator.
// Vue and Svelte pre-pad embedded script content with newlines so the
// sub-extractor produces file-absolute line numbers and node IDs.
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

// --- Extractor interface ---

// Extractor is the common interface for all standalone format extractors.
type Extractor interface {
	Extract(filePath, source string) (types.ExtractionResult, error)
}

// --- Registry ---

// Registry maps file extensions to Extractor instances.
type Registry struct {
	entries map[string]Extractor
}

// NewRegistry wires every standalone extractor. pool is used only by Vue and
// Svelte, which run the JS/TS tree-sitter extractor on embedded script blocks.
func NewRegistry(pool *extraction.Pool) *Registry {
	entries := map[string]Extractor{
		".vue":    NewVueExtractor(pool),
		".svelte": NewSvelteExtractor(pool),
		".liquid": NewLiquidExtractor(),
		".dfm":    NewDFMExtractor(),
		".fmx":    NewDFMExtractor(),
		".xml":    NewMyBatisExtractor(),
	}
	// Extensions come from SQLExtensions so every consumer stays in sync.
	sqlExt := NewSQLExtractor()
	for _, ext := range SQLExtensions {
		entries[ext] = sqlExt
	}
	return &Registry{entries: entries}
}

// For returns the Extractor for a file extension, or nil when unregistered.
func (r *Registry) For(ext string) Extractor {
	return r.entries[strings.ToLower(ext)]
}

// --- Shared helpers ---

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

func fileBaseName(filePath string) string {
	base := filepath.Base(filePath)
	ext := filepath.Ext(base)
	if ext != "" {
		return base[:len(base)-len(ext)]
	}
	return base
}

func containsEdge(sourceID, targetID string) types.Edge {
	return types.Edge{
		Source: sourceID,
		Target: targetID,
		Kind:   types.EdgeKindContains,
	}
}

// --- Vue SFC extractor ---

// scriptTagRE matches a <script ...> block. Deliberately naive: HTML comments
// inside attributes are not handled.
var scriptTagRE = regexp.MustCompile(`(?si)<script([^>]*)>(.*?)</script>`)

// templateTagRE matches PascalCase or kebab-case component tags, self-closing
// or not.
var templateTagRE = regexp.MustCompile(`<([A-Z][a-zA-Z0-9]*|[a-z][a-z0-9]*(?:-[a-z0-9]+)+)[\s/>]`)

// templateBlockRE matches the <template> block content.
var templateBlockRE = regexp.MustCompile(`(?si)<template[^>]*>(.*?)</template>`)

// handlerBindingRE matches @event="handler" and v-on:event="handler". Only bare
// identifier handlers match; inline expressions like @click="count++" do not.
var handlerBindingRE = regexp.MustCompile(`(?:@|v-on:)[a-zA-Z][a-zA-Z0-9\-]*(?:\.[a-zA-Z]+)*=["']([a-zA-Z_$][a-zA-Z0-9_$]*)["']`)

// VueExtractor extracts from .vue Single-File Components.
type VueExtractor struct {
	tsExt *extraction.TreeSitterExtractor
	jsExt *extraction.TreeSitterExtractor
}

// NewVueExtractor wires both sub-extractors; script lang defaults to JS.
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
		if len(m) < 6 {
			continue
		}
		attrs := source[m[2]:m[3]]
		content := source[m[4]:m[5]]

		// Pad with leading newlines so the sub-extractor computes file-absolute
		// lines from the start: GenerateNodeID hashes the line, so nodes, edges,
		// and refs get no post-hoc shift.
		contentLineOffset := strings.Count(source[:m[4]], "\n")
		paddedContent := strings.Repeat("\n", contentLineOffset) + content

		isTS := strings.Contains(attrs, `lang="ts"`) || strings.Contains(attrs, `lang='ts'`)
		var scriptResult types.ExtractionResult
		if isTS {
			ctx := context.Background()
			scriptResult = e.tsExt.Extract(ctx, filePath, paddedContent, types.LanguageTypeScript)
		} else {
			ctx := context.Background()
			scriptResult = e.jsExt.Extract(ctx, filePath, paddedContent, types.LanguageJavaScript)
		}

		// The component node replaces the file: node the sub-extractor emits.
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

		// Top-level calls (onMounted in <script setup>) are attributed to the
		// stripped file: node; left un-rewired their owner is absent from the
		// file's stored nodes and the from_node_id FK fails on insert.
		for _, ref := range scriptResult.UnresolvedReferences {
			if ref.FromNodeID == "file:"+filePath {
				ref.FromNodeID = comp.ID
			}
			result.UnresolvedReferences = append(result.UnresolvedReferences, ref)
		}

		result.Errors = append(result.Errors, scriptResult.Errors...)
	}

	// Contains edges come from the re-wired file:→child edges above.

	templateRefs := extractTemplateRefs(filePath, source, comp.ID, types.LanguageVue)
	result.UnresolvedReferences = append(result.UnresolvedReferences, templateRefs...)

	handlerRefs := extractHandlerRefs(filePath, source, comp.ID, types.LanguageVue)
	result.UnresolvedReferences = append(result.UnresolvedReferences, handlerRefs...)

	return result, nil
}

// extractHandlerRefs emits one reference per distinct event-binding handler
// name. Resolution points it at the <script> method node, and
// VueHandlerSynthesizer then turns that references edge into a calls edge.
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

// htmlElements holds the built-in tags to skip. Only kebab names need checking:
// PascalCase never appears in HTML.
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

// --- Svelte extractor ---

// capitalTagRE matches capitalized component tags (PascalCase) in Svelte markup.
var capitalTagRE = regexp.MustCompile(`<([A-Z][a-zA-Z0-9]*)[\s/>]`)

// SvelteExtractor extracts from .svelte files.
type SvelteExtractor struct {
	jsExt *extraction.TreeSitterExtractor
}

// NewSvelteExtractor wires the JS sub-extractor; lang= switching is not
// supported.
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
		// Pad so the sub-extractor computes file-absolute lines; no post-hoc
		// shift is applied to nodes, edges, or refs.
		contentLineOffset := strings.Count(source[:m[4]], "\n")
		paddedContent := strings.Repeat("\n", contentLineOffset) + content

		ctx := context.Background()
		scriptResult := e.jsExt.Extract(ctx, filePath, paddedContent, types.LanguageJavaScript)

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
		// Same file:→component rewire as the edges above: an un-rewired owner is
		// absent from the file's stored nodes and fails the from_node_id FK.
		for _, ref := range scriptResult.UnresolvedReferences {
			if ref.FromNodeID == "file:"+filePath {
				ref.FromNodeID = comp.ID
			}
			result.UnresolvedReferences = append(result.UnresolvedReferences, ref)
		}
		result.Errors = append(result.Errors, scriptResult.Errors...)
	}

	// Contains edges come from the re-wired file:→child edges above.

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

// stripScriptAndStyle blanks script and style blocks, keeping their newlines so
// line numbers stay stable.
func stripScriptAndStyle(source string) string {
	reBlock := regexp.MustCompile(`(?si)<(script|style)[^>]*>.*?</(script|style)>`)
	return reBlock.ReplaceAllStringFunc(source, func(match string) string {
		return strings.Repeat("\n", strings.Count(match, "\n"))
	})
}

// --- Liquid extractor ---

// liquidRenderRE matches {% render 'name' %} or {% render "name" %}.
var liquidRenderRE = regexp.MustCompile(`{%-?\s*render\s+['"]([^'"]+)['"]`)

// liquidIncludeRE matches {% include 'name' %} or {% include "name" %}.
var liquidIncludeRE = regexp.MustCompile(`{%-?\s*include\s+['"]([^'"]+)['"]`)

// LiquidExtractor extracts from .liquid Shopify/Liquid template files.
type LiquidExtractor struct{}

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

// --- Delphi DFM extractor ---

// dfmObjectRE matches "object Name: TType" lines.
var dfmObjectRE = regexp.MustCompile(`(?im)^\s*object\s+(\w+)\s*:\s*(\w+)`)

// DFMExtractor extracts from Delphi Form Definition (.dfm, .fmx) files.
type DFMExtractor struct{}

func NewDFMExtractor() *DFMExtractor {
	return &DFMExtractor{}
}

// Extract emits a component node per object block; the first is the root form.
func (e *DFMExtractor) Extract(filePath, source string) (types.ExtractionResult, error) {
	result := types.ExtractionResult{}

	matches := dfmObjectRE.FindAllStringSubmatchIndex(source, -1)
	if len(matches) == 0 {
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

// --- MyBatis XML extractor ---

// mybatisMapperRE matches <mapper namespace="..."> or <mapper namespace='...'>
var mybatisMapperRE = regexp.MustCompile(`(?i)<mapper\s[^>]*namespace\s*=\s*['"]([^'"]+)['"]`)

// mybatisStatementRE matches <select|insert|update|delete id="...">
var mybatisStatementRE = regexp.MustCompile(`(?i)<(select|insert|update|delete)\s[^>]*\bid\s*=\s*['"]([^'"]+)['"]`)

// MyBatisExtractor extracts from MyBatis XML mapper files.
type MyBatisExtractor struct{}

func NewMyBatisExtractor() *MyBatisExtractor {
	return &MyBatisExtractor{}
}

// Extract emits a module node for <mapper>, a function node per statement, and
// a reference from the mapper to its namespace Java class.
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
