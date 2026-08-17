// Spring MVC / Spring Boot resolver.
//
// A route's path is split across two annotations: a class-level
// @RequestMapping supplies the prefix, a method-level mapping supplies the
// sub-path and the HTTP method. Since the same @RequestMapping annotation can
// appear in either position, the class-level ones are identified first (their
// next meaningful line is a class declaration) and each method annotation then
// takes the nearest preceding one as its prefix.
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

// springClassMappingRe captures the prefix from `@RequestMapping("/p")`, in
// both the bare and value= forms.
var springClassMappingRe = regexp.MustCompile(
	`@RequestMapping\s*\(\s*(?:value\s*=\s*)?["']([^"']+)["']\s*\)`,
)

// springMethodAnnotationRe matches `@GetMapping("/sub")` and its siblings.
// Groups: verb, sub-path — the path is optional.
var springMethodAnnotationRe = regexp.MustCompile(
	`@(Get|Post|Put|Delete|Patch)Mapping\s*(?:\(\s*(?:value\s*=\s*)?["']([^"']*)["']\s*\))?`,
)

// springRequestMappingMethodRe matches `@RequestMapping(value = "/sub",
// method = RequestMethod.GET)`. Groups: path, verb.
var springRequestMappingMethodRe = regexp.MustCompile(
	`@RequestMapping\s*\(\s*(?:value\s*=\s*)?["']([^"']+)["']\s*,\s*method\s*=\s*RequestMethod\.([A-Z]+)\s*\)`,
)

// springRequestMappingNoMethodRe matches only the path-only form; a match here
// means the route accepts any method.
var springRequestMappingNoMethodRe = regexp.MustCompile(
	`@RequestMapping\s*\(\s*(?:value\s*=\s*)?["']([^"']+)["']\s*\)`,
)

var springHandlerDefRe = regexp.MustCompile(
	`^\s*(?:public|private|protected)(?:\s+static)?(?:\s+final)?(?:\s+\w[\w<>[\], ]*)\s+([A-Za-z_$][A-Za-z0-9_$]*)\s*\(`,
)

var springClassDeclRe = regexp.MustCompile(`(?m)^\s*(?:public\s+|private\s+|protected\s+|abstract\s+|final\s+)*(?:class|interface|enum)\s+[A-Za-z_$]`)

// springHandlerName finds the method an annotation decorates, skipping blank
// lines and stacked annotations. It gives up at a class boundary rather than
// attribute a method from the next class.
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
		return ""
	}
	return ""
}

type springPrefixEntry struct {
	offset int
	prefix string
}

// isClassLevelOffset keeps a class-level @RequestMapping from being re-read as
// a route of its own; it has already contributed a prefix.
func isClassLevelOffset(classPrefixes []springPrefixEntry, offset int) bool {
	for _, p := range classPrefixes {
		if p.offset == offset {
			return true
		}
	}
	return false
}

// springJoinPaths normalises to exactly one leading slash, whether or not
// either half carries its own.
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

type SpringResolver struct {
	projectRoot string
	claimed     map[string]bool
}

func NewSpringResolver(projectRoot string) *SpringResolver {
	return &SpringResolver{projectRoot: projectRoot, claimed: make(map[string]bool)}
}

func (r *SpringResolver) Name() string { return "spring" }

func (r *SpringResolver) Languages() []types.Language {
	return []types.Language{types.LanguageJava}
}

// Detect reads the build file first, then falls back to source imports.
func (r *SpringResolver) Detect(_ context.Context) bool {
	if data, err := os.ReadFile(filepath.Join(r.projectRoot, "pom.xml")); err == nil {
		if strings.Contains(string(data), "org.springframework") {
			return true
		}
	}
	if data, err := os.ReadFile(filepath.Join(r.projectRoot, "build.gradle")); err == nil {
		if strings.Contains(string(data), "org.springframework") {
			return true
		}
	}
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

func (r *SpringResolver) Extract(filePath, content string) ([]types.Node, []types.UnresolvedReference) {
	stripped := stripJSComments(content)
	totalLines := strings.Count(content, "\n") + 1

	var nodes []types.Node
	var refs []types.UnresolvedReference

	// An annotation is class-level when its next meaningful line declares a
	// class — cheaper and more robust than tracking class-body brace spans.
	var classPrefixes []springPrefixEntry

	for _, loc := range springClassMappingRe.FindAllStringSubmatchIndex(stripped, -1) {
		if len(loc) < 4 {
			continue
		}
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

	activePrefix := func(offset int) string {
		prefix := ""
		for _, p := range classPrefixes {
			if p.offset < offset {
				prefix = p.prefix
			}
		}
		return prefix
	}

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

		if isClassLevelOffset(classPrefixes, matchOffset) {
			continue
		}

		prefix := activePrefix(matchOffset)
		fullPath := springJoinPaths(prefix, subPath)

		handlerName := springHandlerName(stripped[loc[1]:])

		r.emitSpringRoute(filePath, line, method, fullPath, handlerName, &nodes, &refs)
	}

	// The path-only form overlaps the method= form's matches, so those offsets
	// are collected first and skipped below.
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

		if isClassLevelOffset(classPrefixes, matchOffset) {
			continue
		}

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

// emitSpringRoute appends the route node and its handler ref.
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

func (r *SpringResolver) ClaimsReference(name string) bool { return r.claimed[name] }

func (r *SpringResolver) Resolve(ctx context.Context, ref types.UnresolvedReference) (resolution.ResolvedRef, error) {
	if !r.claimed[ref.ReferenceName] {
		return resolution.ResolvedRef{}, nil
	}
	return resolution.ResolvedRef{Confidence: 0.85}, nil
}
