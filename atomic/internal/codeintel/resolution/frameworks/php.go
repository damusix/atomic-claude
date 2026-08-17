// PHP web-framework resolvers: Laravel and Symfony.
//
// These use stripJSComments, which leaves `#` alone. That is load-bearing:
// Symfony routes are PHP 8 attributes (`#[Route(...)]`), not comments, and
// stripping `#` would silently lose every one of them. The cost is that a
// genuine `# comment` line survives stripping, which is rare in PHP.
//
// The older `/** @Route(...) */` docblock form is a block comment and is
// therefore stripped; only the attribute form is supported.
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

// phpHasDep matches the JSON-key form so a longer package sharing the prefix
// does not register as a hit.
func phpHasDep(projectRoot, pkg string) bool {
	data, err := os.ReadFile(filepath.Join(projectRoot, "composer.json"))
	if err != nil {
		return false
	}
	return strings.Contains(string(data), `"`+pkg+`"`)
}

func phpLineOf(src string, offset int) int {
	return strings.Count(src[:offset], "\n") + 1
}

// laravelRouteRe matches `Route::get('/p', h)` and `Route::match(['get'],
// '/p', h)`. Groups: single method, methods array, path, handler argument —
// the first two are mutually exclusive.
var laravelRouteRe = regexp.MustCompile(
	`(?i)Route::(get|post|put|patch|delete|options|any|match)\s*\(` +
		`(?:\s*\[([^\]]*)\]\s*,\s*)?` + // optional methods array for match
		`\s*['"]([^'"]+)['"]\s*,\s*` + // route path
		`([^;]+)`, // handler — ends before ; or EOL (trimmed in code)
)

// laravelArrayHandlerRe captures the action from `[Ctrl::class, 'action']`.
var laravelArrayHandlerRe = regexp.MustCompile(
	`\[\s*[A-Za-z_\\][A-Za-z0-9_\\]*(?:::class)?\s*,\s*['"]([A-Za-z_][A-Za-z0-9_]*)['"]`,
)

var laravelMethodNamesRe = regexp.MustCompile(`'([a-zA-Z]+)'|"([a-zA-Z]+)"`)

type LaravelResolver struct {
	projectRoot string
	claimed     map[string]bool
}

func NewLaravelResolver(projectRoot string) *LaravelResolver {
	return &LaravelResolver{projectRoot: projectRoot, claimed: make(map[string]bool)}
}

func (r *LaravelResolver) Name() string { return "laravel" }

func (r *LaravelResolver) Languages() []types.Language {
	return []types.Language{types.LanguagePHP}
}

func (r *LaravelResolver) Detect(ctx context.Context) bool {
	return phpHasDep(r.projectRoot, "laravel/framework")
}

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
		handlerRaw = strings.TrimRight(handlerRaw, ");, \t\n\r")

		line := phpLineOf(stripped, loc[0])
		if line > totalLines {
			line = totalLines
		}

		var methods []string
		if methodOrMatch == "match" {
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

// laravelExtractAction reduces any of Laravel's three handler spellings —
// `[Ctrl::class, 'act']`, `'Ctrl@act'`, or a bare `'act'` — to just the action.
func laravelExtractAction(handler string) string {
	if m := laravelArrayHandlerRe.FindStringSubmatch(handler); m != nil {
		return m[1]
	}
	handler = strings.Trim(handler, `'"`)
	if idx := strings.LastIndex(handler, "@"); idx >= 0 {
		return handler[idx+1:]
	}
	return extractIdentifier(handler)
}

func (r *LaravelResolver) ClaimsReference(name string) bool { return r.claimed[name] }

func (r *LaravelResolver) Resolve(ctx context.Context, ref types.UnresolvedReference) (resolution.ResolvedRef, error) {
	if !r.claimed[ref.ReferenceName] {
		return resolution.ResolvedRef{}, nil
	}
	return resolution.ResolvedRef{Confidence: 0.85}, nil
}

// symfonyAttributeRe matches `#[Route('/p', methods: ['GET'])]`. Groups: path,
// methods array.
var symfonyAttributeRe = regexp.MustCompile(
	`#\[Route\s*\(\s*['"]([^'"]+)['"]` + // path
		`(?:[^)]*?methods\s*:\s*\[([^\]]*)\])?` + // optional methods:[...]
		`[^)]*\)\s*\]`, // closing )]
)

var symfonyMethodNamesRe = regexp.MustCompile(`'([A-Za-z]+)'|"([A-Za-z]+)"`)

// symfonyPublicFuncRe matches `public function name(`, and bare `function
// name(` too.
var symfonyPublicFuncRe = regexp.MustCompile(
	`(?m)^\s*(?:public|protected|private|static|\s)*function\s+([A-Za-z_][A-Za-z0-9_]*)\s*\(`,
)

type SymfonyResolver struct {
	projectRoot string
	claimed     map[string]bool
}

func NewSymfonyResolver(projectRoot string) *SymfonyResolver {
	return &SymfonyResolver{projectRoot: projectRoot, claimed: make(map[string]bool)}
}

func (r *SymfonyResolver) Name() string { return "symfony" }

func (r *SymfonyResolver) Languages() []types.Language {
	return []types.Language{types.LanguagePHP}
}

func (r *SymfonyResolver) Detect(ctx context.Context) bool {
	return phpHasDep(r.projectRoot, "symfony/framework-bundle") ||
		phpHasDep(r.projectRoot, "symfony/routing")
}

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
			// Symfony's own default when methods: is omitted.
			methods = []string{"ANY"}
		}

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

// symfonyFindHandler finds the function a #[Route] attribute decorates,
// skipping blank lines, stacked attributes, and lone modifier keywords. It
// gives up at a class boundary rather than attribute a distant function.
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
		if strings.HasPrefix(trimmed, "#[") {
			continue
		}
		if strings.HasPrefix(trimmed, "}") || strings.HasPrefix(trimmed, "{") {
			return ""
		}
		if m := symfonyPublicFuncRe.FindStringSubmatch(line); m != nil {
			return m[1]
		}
		if isPhpModifierLine(trimmed) {
			continue
		}
		return ""
	}
	return ""
}

// phpModifierWords covers modifiers written on their own line above the
// function declaration.
var phpModifierWords = map[string]bool{
	"public": true, "protected": true, "private": true,
	"static": true, "abstract": true, "final": true, "readonly": true,
}

func isPhpModifierLine(trimmed string) bool {
	return phpModifierWords[trimmed]
}

func (r *SymfonyResolver) ClaimsReference(name string) bool { return r.claimed[name] }

func (r *SymfonyResolver) Resolve(ctx context.Context, ref types.UnresolvedReference) (resolution.ResolvedRef, error) {
	if !r.claimed[ref.ReferenceName] {
		return resolution.ResolvedRef{}, nil
	}
	return resolution.ResolvedRef{Confidence: 0.85}, nil
}
