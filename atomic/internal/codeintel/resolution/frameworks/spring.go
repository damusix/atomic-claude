// Package frameworks — Java Spring framework resolver (CP15 batch D).
//
// This file implements one FrameworkResolver + FrameworkExtractor pair for
// Spring MVC / Spring Boot.
//
// # Language
//
// The resolver sets Language = types.LanguageJava.
//
// # Comment stripping
//
// Java comments use the same delimiters as JS/TS:
//   - Single-line: // to end of line.
//   - Block: /* ... */ (can span lines).
//
// stripJSComments (defined in frameworks.go) handles both — no Java-specific
// stripper needed. Comments are stripped before route regexes run, so
// commented-out annotations never emit route nodes.
//
// # Route node format (appendix H — via MakeRouteNode)
//
//	id:            route:{filePath}:{line}:{METHOD}:{path}
//	qualifiedName: {filePath}::METHOD:{path}
//	name:          "METHOD /path"
//
// # Spring MVC annotation model
//
// Class-level annotations establish a route prefix:
//   - @RequestMapping("/prefix") on a class sets the prefix.
//   - @RestController alone contributes no prefix (empty string).
//
// Method-level annotations define sub-paths and HTTP methods:
//   - @GetMapping("/sub") → GET prefix+sub
//   - @PostMapping("/sub") → POST prefix+sub
//   - @PutMapping("/sub") → PUT prefix+sub
//   - @DeleteMapping("/sub") → DELETE prefix+sub
//   - @PatchMapping("/sub") → PATCH prefix+sub
//   - @RequestMapping(value="/sub", method=RequestMethod.GET) → GET prefix+sub
//   - @RequestMapping("/sub") (no method) → ANY prefix+sub
//
// The bounded-handler-lookahead is the same pattern as nestHandlerName
// in node.go: scan forward line-by-line, skip blank lines and @annotation
// lines, stop at '}' or a class/field declaration, return the method name
// from the first public/private/protected method definition.
//
// # Prefix tracking
//
// Collect all @RequestMapping positions + prefixes on the class (outside any
// method body, at indentation ≤ 4 spaces to distinguish class-level from
// method-level). For each method annotation, find the most-recently-declared
// class-level prefix (same strategy as NestJS controller tracking).
//
// # Detect
//
// Primary: pom.xml or build.gradle containing "org.springframework".
// Fallback: any .java file in projectRoot containing an org.springframework import.
package frameworks

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/resolution"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// ---------------------------------------------------------------------------
// Spring regexes
// ---------------------------------------------------------------------------

// springClassMappingRe matches a class-level @RequestMapping with a path arg:
//
//	@RequestMapping("/prefix")
//	@RequestMapping(value = "/prefix")
//
// Capture group 1 = prefix path.
var springClassMappingRe = regexp.MustCompile(
	`@RequestMapping\s*\(\s*(?:value\s*=\s*)?["']([^"']+)["']\s*\)`,
)

// springMethodAnnotationRe matches method-level mapping annotations:
//
//	@GetMapping("/sub")
//	@PostMapping
//	@PutMapping("/sub")
//	@DeleteMapping("/sub")
//	@PatchMapping("/sub")
//
// Capture groups: 1=verb (Get|Post|Put|Delete|Patch), 2=sub-path (may be empty string).
var springMethodAnnotationRe = regexp.MustCompile(
	`@(Get|Post|Put|Delete|Patch)Mapping\s*(?:\(\s*(?:value\s*=\s*)?["']([^"']*)["']\s*\))?`,
)

// springRequestMappingMethodRe matches @RequestMapping with explicit method:
//
//	@RequestMapping(value = "/sub", method = RequestMethod.GET)
//	@RequestMapping(value = "/sub", method = RequestMethod.POST)
//
// Capture groups: 1=value path, 2=HTTP method verb (GET|POST|...).
var springRequestMappingMethodRe = regexp.MustCompile(
	`@RequestMapping\s*\(\s*(?:value\s*=\s*)?["']([^"']+)["']\s*,\s*method\s*=\s*RequestMethod\.([A-Z]+)\s*\)`,
)

// springRequestMappingNoMethodRe matches @RequestMapping with a path but NO method= clause.
// We use a negative approach: match path-only forms that don't contain "method=".
//
// Capture group 1 = path.
var springRequestMappingNoMethodRe = regexp.MustCompile(
	`@RequestMapping\s*\(\s*(?:value\s*=\s*)?["']([^"']+)["']\s*\)`,
)

// springHandlerDefRe matches a Java method definition line after skipping annotations.
// Captures the method name.
var springHandlerDefRe = regexp.MustCompile(
	`^\s*(?:public|private|protected)(?:\s+static)?(?:\s+final)?(?:\s+\w[\w<>[\], ]*)\s+([A-Za-z_$][A-Za-z0-9_$]*)\s*\(`,
)

// springClassDeclRe matches a Java class or interface declaration line.
var springClassDeclRe = regexp.MustCompile(`(?m)^\s*(?:public\s+|private\s+|protected\s+|abstract\s+|final\s+)*(?:class|interface|enum)\s+[A-Za-z_$]`)

// ---------------------------------------------------------------------------
// Spring helper: handler name lookup
// ---------------------------------------------------------------------------

// springHandlerName scans forward line-by-line from rest (text after a method
// annotation) to find the Java method definition that follows. It skips:
//   - blank lines
//   - lines starting with '@' (stacked annotations like @ResponseBody, @Valid)
//
// Returns "" if a class/interface boundary (line starts with '}') is reached.
func springHandlerName(rest string) string {
	for _, line := range strings.Split(rest, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue // blank line — keep scanning
		}
		if strings.HasPrefix(trimmed, "@") {
			continue // stacked annotation — keep scanning
		}
		if strings.HasPrefix(trimmed, "}") || strings.HasPrefix(trimmed, "{") {
			return "" // class boundary — stop
		}
		if m := springHandlerDefRe.FindStringSubmatch(line); m != nil {
			return m[1]
		}
		// Non-annotation, non-blank line that isn't a method def — stop.
		return ""
	}
	return ""
}

// springPrefixEntry holds a class-level @RequestMapping position and its prefix.
type springPrefixEntry struct {
	offset int
	prefix string
}

// isClassLevelOffset returns true if the given match offset corresponds to one
// of the collected class-level @RequestMapping prefix entries. Used by sections
// 2 and 3 of Extract to skip class-level @RequestMapping annotations that were
// already captured as prefix entries.
func isClassLevelOffset(classPrefixes []springPrefixEntry, offset int) bool {
	for _, p := range classPrefixes {
		if p.offset == offset {
			return true
		}
	}
	return false
}

// springJoinPaths joins a class-level prefix with a method-level sub-path.
// Both may or may not have leading slashes; the result always has exactly one
// leading slash.
func springJoinPaths(prefix, sub string) string {
	prefix = strings.TrimPrefix(prefix, "/")
	sub = strings.TrimPrefix(sub, "/")
	switch {
	case prefix == "" && sub == "":
		return "/"
	case prefix == "":
		return "/" + sub
	case sub == "":
		return "/" + prefix
	default:
		return "/" + prefix + "/" + sub
	}
}

// ---------------------------------------------------------------------------
// Spring resolver
// ---------------------------------------------------------------------------

// SpringResolver implements FrameworkResolver + FrameworkExtractor for Spring MVC.
type SpringResolver struct {
	projectRoot string
	claimed     map[string]bool
}

// NewSpringResolver creates a SpringResolver.
func NewSpringResolver(projectRoot string) *SpringResolver {
	return &SpringResolver{projectRoot: projectRoot, claimed: make(map[string]bool)}
}

// Name returns "spring".
func (r *SpringResolver) Name() string { return "spring" }

// Languages returns [LanguageJava].
func (r *SpringResolver) Languages() []types.Language {
	return []types.Language{types.LanguageJava}
}

// Detect returns true when the project uses Spring:
//  1. pom.xml or build.gradle contains "org.springframework".
//  2. Fallback: any .java file in projectRoot contains an org.springframework import.
func (r *SpringResolver) Detect(_ context.Context) bool {
	// Primary: pom.xml
	if data, err := os.ReadFile(filepath.Join(r.projectRoot, "pom.xml")); err == nil {
		if strings.Contains(string(data), "org.springframework") {
			return true
		}
	}
	// Primary: build.gradle
	if data, err := os.ReadFile(filepath.Join(r.projectRoot, "build.gradle")); err == nil {
		if strings.Contains(string(data), "org.springframework") {
			return true
		}
	}
	// Fallback: scan top-level .java files
	entries, err := os.ReadDir(r.projectRoot)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if !strings.HasSuffix(entry.Name(), ".java") {
			continue
		}
		snippet := readFirstNLines(filepath.Join(r.projectRoot, entry.Name()), 30)
		if strings.Contains(snippet, "org.springframework") {
			return true
		}
	}
	return false
}

// Extract scans filePath/content for Spring MVC route annotations and returns
// route nodes + handler references.
//
// Strategy:
//  1. Strip comments.
//  2. Collect class-level @RequestMapping positions and their prefixes.
//  3. For each method annotation, find the active class-level prefix
//     (the last class-level @RequestMapping before this offset).
//  4. Determine sub-path + HTTP method from the annotation form.
//  5. Bounded-lookahead to find the Java method name.
//  6. Build route node via MakeRouteNode + emit handler ref.
func (r *SpringResolver) Extract(filePath, content string) ([]types.Node, []types.UnresolvedReference) {
	stripped := stripJSComments(content)
	totalLines := strings.Count(content, "\n") + 1

	var nodes []types.Node
	var refs []types.UnresolvedReference

	// Collect class-level @RequestMapping positions and prefixes.
	//
	// Strategy: a class-level @RequestMapping appears BEFORE the class body
	// opening brace '{'. We track class declaration positions by finding
	// "class " or "interface " keywords, and for each class declaration we
	// look for @RequestMapping annotations that precede the class body '{'.
	//
	// Simpler robust approach: mark positions of class-body opening braces
	// (lines containing exactly "public class Foo" / "class Foo" followed
	// eventually by '{'), then for each @RequestMapping match determine whether
	// it sits between a class keyword and its opening '{'.
	//
	// Even simpler: look for @RequestMapping annotations whose NEXT
	// non-blank, non-annotation line contains "class " or "interface ".
	var classPrefixes []springPrefixEntry

	for _, loc := range springClassMappingRe.FindAllStringSubmatchIndex(stripped, -1) {
		if len(loc) < 4 {
			continue
		}
		// Check if this is followed by a class declaration (possibly with other
		// annotations between them). Scan forward from loc[1].
		isClassLevel := false
		rest := stripped[loc[1]:]
		for _, line := range strings.Split(rest, "\n") {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" {
				continue
			}
			if strings.HasPrefix(trimmed, "@") {
				continue // stacked annotations
			}
			// If the next meaningful line is a class/interface declaration → class-level
			if springClassDeclRe.MatchString(line) {
				isClassLevel = true
			}
			break
		}
		if !isClassLevel {
			continue
		}
		prefix := stripped[loc[2]:loc[3]]
		classPrefixes = append(classPrefixes, springPrefixEntry{offset: loc[0], prefix: prefix})
	}

	// Helper: active prefix at the given offset.
	activePrefix := func(offset int) string {
		prefix := ""
		for _, p := range classPrefixes {
			if p.offset < offset {
				prefix = p.prefix
			}
		}
		return prefix
	}

	// 1. Shorthand mapping annotations: @GetMapping, @PostMapping, etc.
	for _, loc := range springMethodAnnotationRe.FindAllStringSubmatchIndex(stripped, -1) {
		if len(loc) < 4 {
			continue
		}
		verb := stripped[loc[2]:loc[3]] // Get, Post, Put, Delete, Patch
		method := strings.ToUpper(verb)

		subPath := ""
		if loc[4] >= 0 {
			subPath = stripped[loc[4]:loc[5]]
		}

		matchOffset := loc[0]
		line := lineOf(stripped, matchOffset)
		if line > totalLines {
			line = totalLines
		}

		prefix := activePrefix(matchOffset)
		fullPath := springJoinPaths(prefix, subPath)

		handlerName := springHandlerName(stripped[loc[1]:])

		r.emitSpringRoute(filePath, line, method, fullPath, handlerName, &nodes, &refs)
	}

	// 2. @RequestMapping with explicit method=RequestMethod.VERB
	for _, loc := range springRequestMappingMethodRe.FindAllStringSubmatchIndex(stripped, -1) {
		if len(loc) < 6 {
			continue
		}
		subPath := stripped[loc[2]:loc[3]]
		method := stripped[loc[4]:loc[5]] // already uppercase

		matchOffset := loc[0]
		line := lineOf(stripped, matchOffset)
		if line > totalLines {
			line = totalLines
		}

		// Skip class-level matches (those captured in classPrefixes).
		if isClassLevelOffset(classPrefixes, matchOffset) {
			continue
		}

		prefix := activePrefix(matchOffset)
		fullPath := springJoinPaths(prefix, subPath)

		handlerName := springHandlerName(stripped[loc[1]:])

		r.emitSpringRoute(filePath, line, method, fullPath, handlerName, &nodes, &refs)
	}

	// 3. @RequestMapping with path but NO method= → ANY
	// Skip matches that were already captured as class-level prefixes or as
	// method= forms (which springRequestMappingMethodRe already consumed).
	//
	// Build a set of offsets matched by the method= form to deduplicate.
	methodFormOffsets := make(map[int]bool)
	for _, mloc := range springRequestMappingMethodRe.FindAllStringIndex(stripped, -1) {
		methodFormOffsets[mloc[0]] = true
	}

	for _, loc := range springRequestMappingNoMethodRe.FindAllStringSubmatchIndex(stripped, -1) {
		if len(loc) < 4 {
			continue
		}
		subPath := stripped[loc[2]:loc[3]]
		matchOffset := loc[0]

		// Skip class-level prefixes.
		if isClassLevelOffset(classPrefixes, matchOffset) {
			continue
		}

		// Skip if already handled by method= form.
		if methodFormOffsets[matchOffset] {
			continue
		}

		line := lineOf(stripped, matchOffset)
		if line > totalLines {
			line = totalLines
		}

		prefix := activePrefix(matchOffset)
		fullPath := springJoinPaths(prefix, subPath)

		handlerName := springHandlerName(stripped[loc[1]:])

		r.emitSpringRoute(filePath, line, "ANY", fullPath, handlerName, &nodes, &refs)
	}

	return nodes, refs
}

// emitSpringRoute builds a route node + references ref and appends them.
func (r *SpringResolver) emitSpringRoute(
	filePath string,
	line int,
	method, path, handlerName string,
	nodes *[]types.Node,
	refs *[]types.UnresolvedReference,
) {
	if method == "" {
		method = "ANY"
	}
	node := MakeRouteNode(filePath, line, method, path, types.LanguageJava)
	*nodes = append(*nodes, node)

	if handlerName != "" {
		r.claimed[handlerName] = true
		*refs = append(*refs, types.UnresolvedReference{
			ID:            fmt.Sprintf("ref:%s:%d:%s:%s", filePath, line, method, handlerName),
			FromNodeID:    node.ID,
			ReferenceName: handlerName,
			ReferenceKind: types.EdgeKindReferences,
			Line:          line,
			FilePath:      filePath,
			Language:      types.LanguageJava,
		})
	}
}

// ClaimsReference returns true if a handler with this name was seen in Extract.
func (r *SpringResolver) ClaimsReference(name string) bool { return r.claimed[name] }

// Resolve returns confidence 0.85 for claimed handlers.
func (r *SpringResolver) Resolve(ctx context.Context, ref types.UnresolvedReference) (resolution.ResolvedRef, error) {
	if !r.claimed[ref.ReferenceName] {
		return resolution.ResolvedRef{}, nil
	}
	return resolution.ResolvedRef{Confidence: 0.85}, nil
}
