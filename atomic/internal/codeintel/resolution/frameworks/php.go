// Package frameworks — PHP framework resolvers (CP15 batch E).
//
// This file implements two FrameworkResolver + FrameworkExtractor pairs for
// the two major PHP web frameworks: Laravel and Symfony.
//
// # Language
//
// Both resolvers set Language = types.LanguagePHP.
//
// # Comment stripping
//
// PHP uses stripJSComments (defined in frameworks.go) which strips // and
// /* */ comments ONLY. The # character is intentionally NOT stripped.
// This is REQUIRED because Symfony uses PHP 8 attributes of the form:
//
//	#[Route('/path', methods: ['GET'])]
//
// which are attributes, not comments. If # were stripped, Symfony routes
// would be silently lost. The trade-off (a rare `# comment` line in PHP
// not being stripped) is documented here and acceptable.
//
// The old Symfony docblock annotation form `/** @Route(...) */` is block-
// stripped by stripJSComments — support only the #[Route] attribute form.
//
// # Route node format (appendix H — via MakeRouteNode)
//
//	id:            route:{filePath}:{line}:{METHOD}:{path}
//	qualifiedName: {filePath}::METHOD:{path}
//	name:          "METHOD /path"
//
// # Laravel handler conventions
//
// Three handler forms are supported:
//  1. Array: [ControllerClass::class, 'action'] — action is group 2.
//  2. String @-form: 'ControllerName@action' — action is after '@'.
//  3. String-only: 'action' — the full string is the action (rare; used for
//     string controllers like 'HomeController@index').
//
// In all cases the handler ref uses only the action segment.
//
// # Symfony handler conventions
//
// #[Route('/path', methods: ['GET','POST'])] appears above a method:
//
//	public function methodName(...)
//
// The handler = the function name, found by bounded lookahead (skipping blank
// lines and stacked #[...] attributes). If methods: is absent, method is "ANY".
//
// # Method fan-out
//
// Laravel Route::match(['get','post'], ...) and Symfony methods: ['GET','POST']
// emit one route node per method.
//
// # Detect
//
// Both resolvers detect via composer.json. Laravel checks for "laravel/framework";
// Symfony checks for "symfony/framework-bundle" or "symfony/routing".
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
// Shared PHP helper
// ---------------------------------------------------------------------------

// phpHasDep returns true when composer.json in projectRoot contains the given
// vendor/package name as a JSON-key string (e.g. `"laravel/framework":`).
func phpHasDep(projectRoot, pkg string) bool {
	data, err := os.ReadFile(filepath.Join(projectRoot, "composer.json"))
	if err != nil {
		return false
	}
	return strings.Contains(string(data), `"`+pkg+`"`)
}

// phpLineOf returns the 1-based line number for a byte offset in src.
func phpLineOf(src string, offset int) int {
	return strings.Count(src[:offset], "\n") + 1
}

// ---------------------------------------------------------------------------
// Laravel resolver
// ---------------------------------------------------------------------------

// laravelRouteRe matches the general Laravel route registration forms:
//
//	Route::get('/path', handler)
//	Route::post('/path', handler)
//	Route::match(['get','post'], '/path', handler)
//
// Capture groups:
//
//	1 — method name (get|post|put|patch|delete|options|any) OR empty if match
//	2 — methods array text (inside [...]) for Route::match, OR empty
//	3 — route path (single- or double-quoted)
//	4 — handler argument (remainder until ')' or end — trimmed in code)
//
// laravelRouteRe does NOT need line-start anchoring because Laravel routes
// appear after whitespace within a file; line number is derived from the
// byte offset of loc[0] which always starts at 'Route::'.
var laravelRouteRe = regexp.MustCompile(
	`(?i)Route::(get|post|put|patch|delete|options|any|match)\s*\(` +
		`(?:\s*\[([^\]]*)\]\s*,\s*)?` + // optional methods array for match
		`\s*['"]([^'"]+)['"]\s*,\s*` + // route path
		`([^;]+)`, // handler — ends before ; or EOL (trimmed in code)
)

// laravelArrayHandlerRe matches [ControllerClass::class, 'action'] or
// [ControllerClass::class, "action"] inside the handler argument.
// Capture group 1 = action name.
var laravelArrayHandlerRe = regexp.MustCompile(
	`\[\s*[A-Za-z_\\][A-Za-z0-9_\\]*(?:::class)?\s*,\s*['"]([A-Za-z_][A-Za-z0-9_]*)['"]`,
)

// laravelMethodNamesRe extracts individual quoted method names from a
// Route::match methods array like ['get','post'].
var laravelMethodNamesRe = regexp.MustCompile(`'([a-zA-Z]+)'|"([a-zA-Z]+)"`)

// LaravelResolver implements FrameworkResolver + FrameworkExtractor for Laravel.
type LaravelResolver struct {
	projectRoot string
	claimed     map[string]bool
}

// NewLaravelResolver creates a LaravelResolver.
func NewLaravelResolver(projectRoot string) *LaravelResolver {
	return &LaravelResolver{projectRoot: projectRoot, claimed: make(map[string]bool)}
}

// Name returns "laravel".
func (r *LaravelResolver) Name() string { return "laravel" }

// Languages returns [php].
func (r *LaravelResolver) Languages() []types.Language {
	return []types.Language{types.LanguagePHP}
}

// Detect returns true when composer.json contains "laravel/framework".
func (r *LaravelResolver) Detect(ctx context.Context) bool {
	return phpHasDep(r.projectRoot, "laravel/framework")
}

// Extract scans filePath/content for Laravel route registrations and returns
// route nodes + handler refs. Comments (// and /* */) are stripped first.
// The # character is NOT stripped (see package doc; PHP attributes start with #).
func (r *LaravelResolver) Extract(filePath, content string) ([]types.Node, []types.UnresolvedReference) {
	stripped := stripJSComments(content)
	totalLines := strings.Count(content, "\n") + 1

	var nodes []types.Node
	var refs []types.UnresolvedReference

	matches := laravelRouteRe.FindAllStringSubmatchIndex(stripped, -1)
	for _, loc := range matches {
		if len(loc) < 10 {
			continue
		}

		methodOrMatch := strings.ToLower(stripped[loc[2]:loc[3]])
		methodsArrayText := ""
		if loc[4] >= 0 {
			methodsArrayText = stripped[loc[4]:loc[5]]
		}
		routePath := stripped[loc[6]:loc[7]]
		handlerRaw := strings.TrimSpace(stripped[loc[8]:loc[9]])
		// Trim trailing ) and ; from handler capture.
		handlerRaw = strings.TrimRight(handlerRaw, ");, \t\n\r")

		line := phpLineOf(stripped, loc[0])
		if line > totalLines {
			line = totalLines
		}

		// Determine methods list.
		var methods []string
		if methodOrMatch == "match" {
			// Extract methods from [...] array.
			for _, m := range laravelMethodNamesRe.FindAllStringSubmatch(methodsArrayText, -1) {
				if m[1] != "" {
					methods = append(methods, strings.ToUpper(m[1]))
				} else if m[2] != "" {
					methods = append(methods, strings.ToUpper(m[2]))
				}
			}
			if len(methods) == 0 {
				methods = []string{"ANY"}
			}
		} else if methodOrMatch == "any" {
			methods = []string{"ANY"}
		} else {
			methods = []string{strings.ToUpper(methodOrMatch)}
		}

		// Extract handler action.
		handlerName := laravelExtractAction(handlerRaw)

		for _, method := range methods {
			node := MakeRouteNode(filePath, line, method, routePath, types.LanguagePHP)
			nodes = append(nodes, node)

			if handlerName != "" {
				r.claimed[handlerName] = true
				refs = append(refs, types.UnresolvedReference{
					ID:            fmt.Sprintf("ref:%s:%d:%s:%s", filePath, line, method, handlerName),
					FromNodeID:    node.ID,
					ReferenceName: handlerName,
					ReferenceKind: types.EdgeKindReferences,
					Line:          line,
					FilePath:      filePath,
					Language:      types.LanguagePHP,
				})
			}
		}
	}

	return nodes, refs
}

// laravelExtractAction extracts the action name from a Laravel handler argument.
//
//   - Array form [Ctrl::class, 'action'] → 'action'
//   - @-form 'ControllerName@action' → 'action'
//   - Plain string 'action' → 'action'
func laravelExtractAction(handler string) string {
	// Array form: [ControllerClass::class, 'action'] or [ControllerClass::class, "action"]
	if m := laravelArrayHandlerRe.FindStringSubmatch(handler); m != nil {
		return m[1]
	}
	// @-form: 'ControllerName@action' or ControllerName@action (unquoted)
	handler = strings.Trim(handler, `'"`)
	if idx := strings.LastIndex(handler, "@"); idx >= 0 {
		return handler[idx+1:]
	}
	// Plain identifier: trim to identifier characters.
	return extractIdentifier(handler)
}

// ClaimsReference returns true if a handler action with this name was seen.
func (r *LaravelResolver) ClaimsReference(name string) bool { return r.claimed[name] }

// Resolve returns confidence 0.85 for claimed handler names.
func (r *LaravelResolver) Resolve(ctx context.Context, ref types.UnresolvedReference) (resolution.ResolvedRef, error) {
	if !r.claimed[ref.ReferenceName] {
		return resolution.ResolvedRef{}, nil
	}
	return resolution.ResolvedRef{Confidence: 0.85}, nil
}

// ---------------------------------------------------------------------------
// Symfony resolver
// ---------------------------------------------------------------------------

// symfonyAttributeRe matches PHP 8 #[Route(...)] attribute lines.
//
// Capture groups:
//
//	1 — route path (single- or double-quoted)
//	2 — methods array text (inside [...]), or "" if absent
var symfonyAttributeRe = regexp.MustCompile(
	`#\[Route\s*\(\s*['"]([^'"]+)['"]` + // path
		`(?:[^)]*?methods\s*:\s*\[([^\]]*)\])?` + // optional methods:[...]
		`[^)]*\)\s*\]`, // closing )]
)

// symfonyMethodNamesRe extracts quoted method names from a Symfony methods: [...] array.
var symfonyMethodNamesRe = regexp.MustCompile(`'([A-Za-z]+)'|"([A-Za-z]+)"`)

// symfonyPublicFuncRe matches `public function name(` (the handler below the attribute).
// Also matches bare `function name(` for completeness.
var symfonyPublicFuncRe = regexp.MustCompile(
	`(?m)^\s*(?:public|protected|private|static|\s)*function\s+([A-Za-z_][A-Za-z0-9_]*)\s*\(`,
)

// SymfonyResolver implements FrameworkResolver + FrameworkExtractor for Symfony.
type SymfonyResolver struct {
	projectRoot string
	claimed     map[string]bool
}

// NewSymfonyResolver creates a SymfonyResolver.
func NewSymfonyResolver(projectRoot string) *SymfonyResolver {
	return &SymfonyResolver{projectRoot: projectRoot, claimed: make(map[string]bool)}
}

// Name returns "symfony".
func (r *SymfonyResolver) Name() string { return "symfony" }

// Languages returns [php].
func (r *SymfonyResolver) Languages() []types.Language {
	return []types.Language{types.LanguagePHP}
}

// Detect returns true when composer.json contains "symfony/framework-bundle"
// or "symfony/routing".
func (r *SymfonyResolver) Detect(ctx context.Context) bool {
	return phpHasDep(r.projectRoot, "symfony/framework-bundle") ||
		phpHasDep(r.projectRoot, "symfony/routing")
}

// Extract scans filePath/content for Symfony #[Route] attribute annotations
// and returns route nodes + handler refs.
//
// Comment stripping: stripJSComments is used (strips // and /* */ only).
// The # character is NOT stripped — it is needed for PHP 8 attributes.
// See package-level comment for the rationale.
func (r *SymfonyResolver) Extract(filePath, content string) ([]types.Node, []types.UnresolvedReference) {
	stripped := stripJSComments(content)
	totalLines := strings.Count(content, "\n") + 1

	var nodes []types.Node
	var refs []types.UnresolvedReference

	matches := symfonyAttributeRe.FindAllStringSubmatchIndex(stripped, -1)
	for _, loc := range matches {
		if len(loc) < 6 {
			continue
		}

		routePath := stripped[loc[2]:loc[3]]
		methodsText := ""
		if loc[4] >= 0 {
			methodsText = stripped[loc[4]:loc[5]]
		}

		line := phpLineOf(stripped, loc[0])
		if line > totalLines {
			line = totalLines
		}

		// Determine methods list.
		var methods []string
		if methodsText != "" {
			for _, m := range symfonyMethodNamesRe.FindAllStringSubmatch(methodsText, -1) {
				if m[1] != "" {
					methods = append(methods, strings.ToUpper(m[1]))
				} else if m[2] != "" {
					methods = append(methods, strings.ToUpper(m[2]))
				}
			}
		}
		if len(methods) == 0 {
			// No methods: attribute → ANY (documented; Symfony default is all methods)
			methods = []string{"ANY"}
		}

		// Find handler: bounded lookahead after the attribute end.
		// Skip blank lines and stacked #[...] attribute lines.
		handlerName := symfonyFindHandler(stripped, loc[1])

		for _, method := range methods {
			node := MakeRouteNode(filePath, line, method, routePath, types.LanguagePHP)
			nodes = append(nodes, node)

			if handlerName != "" {
				r.claimed[handlerName] = true
				refs = append(refs, types.UnresolvedReference{
					ID:            fmt.Sprintf("ref:%s:%d:%s:%s", filePath, line, method, handlerName),
					FromNodeID:    node.ID,
					ReferenceName: handlerName,
					ReferenceKind: types.EdgeKindReferences,
					Line:          line,
					FilePath:      filePath,
					Language:      types.LanguagePHP,
				})
			}
		}
	}

	return nodes, refs
}

// symfonyFindHandler scans forward from offset in src, returning the name of
// the first PHP function found after the #[Route] attribute. It skips:
//   - blank lines
//   - additional #[...] attribute lines (stacked attributes)
//
// It stops and returns "" at any line starting with '}' or '{' (class boundary).
// This mirrors nestHandlerName in node.go.
func symfonyFindHandler(src string, offset int) string {
	if offset >= len(src) {
		return ""
	}
	rest := src[offset:]
	for _, line := range strings.Split(rest, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		// Stacked PHP attribute: #[SomeThing(...)] — skip
		if strings.HasPrefix(trimmed, "#[") {
			continue
		}
		if strings.HasPrefix(trimmed, "}") || strings.HasPrefix(trimmed, "{") {
			return ""
		}
		if m := symfonyPublicFuncRe.FindStringSubmatch(line); m != nil {
			return m[1]
		}
		// Non-attribute, non-blank line that is not a function def → stop.
		// (Handles access modifiers on separate line like `public\nfunction foo`)
		// Allow lines that are purely modifier keywords.
		if isPhpModifierLine(trimmed) {
			continue
		}
		return ""
	}
	return ""
}

// phpModifierWords is the set of pure PHP access/modifier keywords that can
// appear on lines between the attribute and the function declaration.
var phpModifierWords = map[string]bool{
	"public": true, "protected": true, "private": true,
	"static": true, "abstract": true, "final": true, "readonly": true,
}

// isPhpModifierLine returns true if the line is a lone PHP modifier keyword.
func isPhpModifierLine(trimmed string) bool {
	return phpModifierWords[trimmed]
}

// ClaimsReference returns true if a handler with this name was seen in Extract.
func (r *SymfonyResolver) ClaimsReference(name string) bool { return r.claimed[name] }

// Resolve returns confidence 0.85 for claimed handler names.
func (r *SymfonyResolver) Resolve(ctx context.Context, ref types.UnresolvedReference) (resolution.ResolvedRef, error) {
	if !r.claimed[ref.ReferenceName] {
		return resolution.ResolvedRef{}, nil
	}
	return resolution.ResolvedRef{Confidence: 0.85}, nil
}
