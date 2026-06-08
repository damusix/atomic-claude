// Package frameworks — Python framework resolvers (CP15 batch A).
//
// This file implements three FrameworkResolver + FrameworkExtractor pairs for
// the three major Python web frameworks: Flask, FastAPI, and Django.
//
// # Language
//
// All three resolvers set Language = types.LanguagePython.
//
// # Comment stripping
//
// Python comments differ from JS/TS:
//   - Single-line: # to end of line.
//   - Block strings used as doc comments: '''...''' or """...""".
//
// stripPyComments handles both before the route regex runs, so commented-out
// decorators never emit route nodes.
//
// # Route node format (appendix H — via MakeRouteNode)
//
//	id:            route:{filePath}:{line}:{METHOD}:{path}
//	qualifiedName: {filePath}::METHOD:{path}
//	name:          "METHOD /path"
//
// # Django method convention
//
// Django URLconf entries carry NO HTTP method at the declaration site.
// Method is set to "ANY" (uppercase) consistently. This is documented here
// and in comments at point-of-use so a future reader can distinguish
// "we forgot the method" from "URLconf has no method at declaration time".

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
// Python comment stripper
// ---------------------------------------------------------------------------

// stripPyComments removes Python single-line (#) and triple-quoted block
// ("""...""" and ”'...”') comments from src, preserving line count so
// that line-number-based regex matches return the correct original line.
//
// The implementation is line-oriented. It handles:
//   - # lines: everything from # to end of line is blanked.
//   - Triple-quoted strings that span multiple lines: the content is
//     replaced with blank lines (preserving line count).
//
// It does NOT try to handle hash signs inside single- or double-quoted
// strings (rare in route files). False positives — a hash inside a string
// literal treated as a comment — are acceptable; false negatives (a real
// comment not stripped) would be a bug.
func stripPyComments(src string) string {
	lines := strings.Split(src, "\n")
	var out strings.Builder
	inTriple := false
	tripleChar := byte(0) // ' or "

	for _, line := range lines {
		if inTriple {
			// Look for the closing triple quote of the same style.
			triple := string([]byte{tripleChar, tripleChar, tripleChar})
			idx := strings.Index(line, triple)
			if idx >= 0 {
				inTriple = false
				// Blank out the closing-quote line up to and including the marker.
				line = strings.Repeat(" ", idx+3) + line[idx+3:]
			} else {
				// Whole line is inside a block comment — blank it.
				out.WriteByte('\n')
				continue
			}
		}

		// Scan for # and triple-quote openers, whichever comes first.
		result, newInTriple, newTripleChar := pyStripLine(line, inTriple, tripleChar)
		inTriple = newInTriple
		tripleChar = newTripleChar
		out.WriteString(result)
		out.WriteByte('\n')
	}
	return out.String()
}

// pyStripLine processes one line of Python source, stripping comments.
// Returns the processed line and the updated inTriple/tripleChar state.
func pyStripLine(line string, inTriple bool, tripleChar byte) (result string, newInTriple bool, newTC byte) {
	var b strings.Builder
	i := 0
	for i < len(line) {
		ch := line[i]

		// Opening triple quote (""" or ''')?
		if i+2 < len(line) && (ch == '"' || ch == '\'') &&
			line[i+1] == ch && line[i+2] == ch {
			// Start of a triple-quoted block.
			tripleChar = ch
			triple := string([]byte{ch, ch, ch})
			rest := line[i+3:]
			end := strings.Index(rest, triple)
			if end >= 0 {
				// Opens and closes on the same line — blank the whole span.
				i = i + 3 + end + 3
				continue
			}
			// Opens but doesn't close on this line — blank to end.
			return b.String(), true, tripleChar
		}

		// Hash outside a string → line comment.
		if ch == '#' {
			return b.String(), false, 0
		}

		// Single-quoted or double-quoted string: skip over it to avoid treating
		// a # inside a string as a comment.
		if ch == '"' || ch == '\'' {
			b.WriteByte(ch)
			i++
			for i < len(line) && line[i] != ch {
				if line[i] == '\\' {
					b.WriteByte(line[i])
					i++
				}
				if i < len(line) {
					b.WriteByte(line[i])
					i++
				}
			}
			if i < len(line) {
				b.WriteByte(line[i]) // closing quote
				i++
			}
			continue
		}

		b.WriteByte(ch)
		i++
	}
	return b.String(), false, 0
}

// ---------------------------------------------------------------------------
// Python dep-file detection helpers
// ---------------------------------------------------------------------------

// pyHasDep returns true if the project directory's requirements.txt or
// pyproject.toml mentions the given package name (case-insensitive).
func pyHasDep(projectRoot, pkgName string) bool {
	lowerPkg := strings.ToLower(pkgName)

	// requirements.txt: each line is a package spec, optionally with version.
	reqPath := filepath.Join(projectRoot, "requirements.txt")
	if data, err := os.ReadFile(reqPath); err == nil {
		for _, line := range strings.Split(strings.ToLower(string(data)), "\n") {
			line = strings.TrimSpace(line)
			// Match "flask", "flask==3.0", "flask>=2.0", etc.
			if line == lowerPkg ||
				strings.HasPrefix(line, lowerPkg+"=") ||
				strings.HasPrefix(line, lowerPkg+">") ||
				strings.HasPrefix(line, lowerPkg+"<") ||
				strings.HasPrefix(line, lowerPkg+"~") ||
				strings.HasPrefix(line, lowerPkg+"[") ||
				strings.HasPrefix(line, lowerPkg+" ") {
				return true
			}
		}
	}

	// pyproject.toml: look for the package name anywhere in the file content.
	ppPath := filepath.Join(projectRoot, "pyproject.toml")
	if data, err := os.ReadFile(ppPath); err == nil {
		lower := strings.ToLower(string(data))
		if strings.Contains(lower, `"`+lowerPkg) || strings.Contains(lower, `'`+lowerPkg) ||
			strings.Contains(lower, lowerPkg+">=") || strings.Contains(lower, lowerPkg+"==") {
			return true
		}
	}

	return false
}

// pyContentHasPattern reads up to n lines from each .py file in the top-level
// of projectRoot and returns true if any matches the given regexp.
func pyContentHasPattern(projectRoot string, pattern *regexp.Regexp, maxLines int) bool {
	entries, err := os.ReadDir(projectRoot)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if filepath.Ext(entry.Name()) != ".py" {
			continue
		}
		snippet := readFirstNLines(filepath.Join(projectRoot, entry.Name()), maxLines)
		if pattern.MatchString(snippet) {
			return true
		}
	}
	return false
}

// lineOf returns the 1-based line number of byte offset in src.
func pyLineOf(src string, offset int) int {
	return strings.Count(src[:offset], "\n") + 1
}

// extractLastSegment returns the final dot-separated segment of s.
// "views.user_list" → "user_list"; "user_list" → "user_list";
// "'myapp.views.foo'" → "foo" (strips surrounding quotes first).
func extractLastSegment(s string) string {
	// Strip surrounding quotes.
	s = strings.Trim(s, `'"`)
	parts := strings.Split(s, ".")
	return parts[len(parts)-1]
}

// ---------------------------------------------------------------------------
// Flask resolver
// ---------------------------------------------------------------------------

// flaskRouteRe matches Flask route decorators in the stripped source.
// It handles two forms:
//
//  1. @app.route('/path', methods=['GET','POST']) or @bp.route('/path', methods=('GET',))
//     — captured by the route variant. Both list [...] and tuple (...) forms for methods.
//  2. @app.get('/path'), @app.post('/path'), @bp.delete('/path'), etc.
//     — captured by the shorthand variant.
//
// Capture groups:
//
//	1 — route path (inside quotes)
//	2 — methods container text (e.g. "'GET','POST'"), or "" if absent
//	3 — shorthand method name (e.g. "get", "post"), or "" if route form
//	4 — shorthand path (inside quotes), or "" if route form
//
// The two alternations are in order: route form first, shorthand second.
var flaskRouteRe = regexp.MustCompile(
	`(?m)@(?:[A-Za-z_][A-Za-z0-9_]*)\.route\s*\(\s*['"]([^'"]+)['"]\s*(?:,\s*methods\s*=\s*[\[({]([^\])}]*)[\])}])?\s*\)` +
		`|@(?:[A-Za-z_][A-Za-z0-9_]*)\.(?:(get|post|put|delete|patch|head|options))\s*\(\s*['"]([^'"]+)['"]\s*\)`,
)

// pyDefRe finds the next `def name(` or `async def name(` after a match.
// Used by both Flask and FastAPI to locate the decorated function name.
var pyDefRe = regexp.MustCompile(`(?m)^\s*(?:async\s+)?def\s+([A-Za-z_][A-Za-z0-9_]*)\s*\(`)

// flaskMethodRe extracts quoted method strings from a methods=[...] list.
var flaskMethodRe = regexp.MustCompile(`'([A-Z]+)'|"([A-Z]+)"`)

// FlaskResolver implements FrameworkResolver + FrameworkExtractor for Flask.
type FlaskResolver struct {
	projectRoot string
	claimed     map[string]bool
}

// flaskImportRe detects Flask import patterns.
// Matches "from flask import ...", "from flask.helpers import ...", "from flask_foo import ...",
// and "Flask(" instantiation. The sub-module form (flask.helpers, flask_jwt_extended, etc.)
// is needed for projects like rw-flask that import from flask sub-packages at the top level.
var flaskImportRe = regexp.MustCompile(`(?i)from\s+flask[\w.]*\s+import|Flask\s*\(`)

// NewFlaskResolver creates a FlaskResolver without a DB.
func NewFlaskResolver(projectRoot string) *FlaskResolver {
	return &FlaskResolver{projectRoot: projectRoot, claimed: make(map[string]bool)}
}

// Name returns "flask".
func (r *FlaskResolver) Name() string { return "flask" }

// Languages returns [python].
func (r *FlaskResolver) Languages() []types.Language {
	return []types.Language{types.LanguagePython}
}

// Detect returns true if requirements.txt/pyproject.toml lists flask, or a
// top-level .py file imports Flask.
func (r *FlaskResolver) Detect(ctx context.Context) bool {
	if pyHasDep(r.projectRoot, "flask") {
		return true
	}
	return pyContentHasPattern(r.projectRoot, flaskImportRe, 30)
}

// Extract scans filePath/content for Flask route decorators and returns route
// nodes + handler refs. Comments are stripped first.
func (r *FlaskResolver) Extract(filePath, content string) ([]types.Node, []types.UnresolvedReference) {
	stripped := stripPyComments(content)
	totalLines := strings.Count(content, "\n") + 1

	var nodes []types.Node
	var refs []types.UnresolvedReference

	// Walk through all matches.
	matches := flaskRouteRe.FindAllStringSubmatchIndex(stripped, -1)
	for _, loc := range matches {
		if len(loc) < 10 {
			continue
		}

		// Decode the two alternation forms:
		// Form 1 (route): groups 1=path, 2=methods-list, 3="", 4=""
		// Form 2 (shorthand): groups 1="", 2="", 3=method, 4=path
		// loc layout: full, g1s,g1e, g2s,g2e, g3s,g3e, g4s,g4e, g5s,g5e
		// Actually the compiled pattern has 4 capture groups + outer = 5 pairs of indices:
		// loc[0:2]=full, loc[2:4]=g1(route path), loc[4:6]=g2(methods list), loc[6:8]=g3(shorthand method), loc[8:10]=g4(shorthand path)

		routePath := ""
		methodsText := ""
		shorthandMethod := ""
		shorthandPath := ""

		if loc[2] >= 0 {
			routePath = stripped[loc[2]:loc[3]]
		}
		if loc[4] >= 0 {
			methodsText = stripped[loc[4]:loc[5]]
		}
		if loc[6] >= 0 {
			shorthandMethod = stripped[loc[6]:loc[7]]
		}
		if loc[8] >= 0 {
			shorthandPath = stripped[loc[8]:loc[9]]
		}

		matchEnd := loc[1]
		line := pyLineOf(stripped, loc[0])
		if line > totalLines {
			line = totalLines
		}

		var methods []string
		var path string

		if routePath != "" {
			// Form 1: @*.route(path, methods=[...])
			path = routePath
			if methodsText != "" {
				for _, m := range flaskMethodRe.FindAllStringSubmatch(methodsText, -1) {
					if m[1] != "" {
						methods = append(methods, m[1])
					} else if m[2] != "" {
						methods = append(methods, m[2])
					}
				}
			}
			if len(methods) == 0 {
				methods = []string{"GET"} // Flask default
			}
		} else if shorthandMethod != "" {
			// Form 2: @*.get/post/..(path)
			path = shorthandPath
			methods = []string{strings.ToUpper(shorthandMethod)}
		}

		if path == "" || len(methods) == 0 {
			continue
		}

		// Find the handler function name: next non-blank `def name(` after the decorator.
		handlerName := findNextDefName(stripped, matchEnd)

		for _, method := range methods {
			node := MakeRouteNode(filePath, line, method, path, types.LanguagePython)
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
					Language:      types.LanguagePython,
				})
			}
		}
	}

	return nodes, refs
}

// ClaimsReference returns true if a handler with this name was seen in Extract.
func (r *FlaskResolver) ClaimsReference(name string) bool { return r.claimed[name] }

// Resolve looks up a claimed handler by name. Confidence 0.85 (midpoint 0.8–0.9).
func (r *FlaskResolver) Resolve(ctx context.Context, ref types.UnresolvedReference) (resolution.ResolvedRef, error) {
	if !r.claimed[ref.ReferenceName] {
		return resolution.ResolvedRef{}, nil
	}
	return resolution.ResolvedRef{Confidence: 0.85}, nil
}

// findNextDefName scans src forward from offset, returning the name of the
// first `def <name>(` or `async def <name>(` encountered (skipping blank lines
// and decorator lines). Returns "" if none found within a reasonable window.
// Shared by Flask and FastAPI (both use the same def-regex pyDefRe).
func findNextDefName(src string, offset int) string {
	rest := src[offset:]
	m := pyDefRe.FindStringSubmatch(rest)
	if m == nil {
		return ""
	}
	return m[1]
}

// ---------------------------------------------------------------------------
// FastAPI resolver
// ---------------------------------------------------------------------------

// fastAPIRouteRe matches FastAPI route decorators of the form:
//
//	@app.get('/path')
//	@router.post('/path')
//	@app.delete('/path/{id}')
//	@router.get("", response_model=Foo, name="bar")  — empty path, trailing kwargs
//	@router.put("", ...)                              — empty path
//
// Capture groups:
//
//	1 — HTTP method (lowercase: get, post, put, delete, patch, options, head)
//	2 — route path (may be empty string)
//
// The path is the first positional argument; trailing kwargs (response_model=,
// name=, status_code=, etc.) are intentionally ignored — the regex captures
// only the first quoted string and stops, regardless of what follows.
var fastAPIRouteRe = regexp.MustCompile(
	`(?m)@(?:[A-Za-z_][A-Za-z0-9_]*)\.` +
		`(get|post|put|delete|patch|options|head)` +
		`\s*\(\s*['"]([^'"]*)['"]\s*[,)]`,
)

// pyDefRe (defined above near Flask) is shared for FastAPI def-finding as well.

// fastapiImportRe detects FastAPI import patterns.
var fastapiImportRe = regexp.MustCompile(`(?i)from\s+fastapi\s+import|FastAPI\s*\(`)

// FastAPIResolver implements FrameworkResolver + FrameworkExtractor for FastAPI.
type FastAPIResolver struct {
	projectRoot string
	claimed     map[string]bool
}

// NewFastAPIResolver creates a FastAPIResolver without a DB.
func NewFastAPIResolver(projectRoot string) *FastAPIResolver {
	return &FastAPIResolver{projectRoot: projectRoot, claimed: make(map[string]bool)}
}

// Name returns "fastapi".
func (r *FastAPIResolver) Name() string { return "fastapi" }

// Languages returns [python].
func (r *FastAPIResolver) Languages() []types.Language {
	return []types.Language{types.LanguagePython}
}

// Detect returns true if requirements.txt/pyproject.toml lists fastapi, or a
// top-level .py file imports FastAPI.
func (r *FastAPIResolver) Detect(ctx context.Context) bool {
	if pyHasDep(r.projectRoot, "fastapi") {
		return true
	}
	return pyContentHasPattern(r.projectRoot, fastapiImportRe, 30)
}

// Extract scans filePath/content for FastAPI route decorators and returns route
// nodes + handler refs. Comments are stripped first.
func (r *FastAPIResolver) Extract(filePath, content string) ([]types.Node, []types.UnresolvedReference) {
	stripped := stripPyComments(content)
	totalLines := strings.Count(content, "\n") + 1

	var nodes []types.Node
	var refs []types.UnresolvedReference

	matches := fastAPIRouteRe.FindAllStringSubmatchIndex(stripped, -1)
	for _, loc := range matches {
		if len(loc) < 6 {
			continue
		}
		method := strings.ToUpper(stripped[loc[2]:loc[3]])
		path := stripped[loc[4]:loc[5]]
		matchEnd := loc[1]

		line := pyLineOf(stripped, loc[0])
		if line > totalLines {
			line = totalLines
		}

		node := MakeRouteNode(filePath, line, method, path, types.LanguagePython)
		nodes = append(nodes, node)

		handlerName := findNextDefFastAPI(stripped, matchEnd)
		if handlerName != "" {
			r.claimed[handlerName] = true
			refs = append(refs, types.UnresolvedReference{
				ID:            fmt.Sprintf("ref:%s:%d:%s:%s", filePath, line, method, handlerName),
				FromNodeID:    node.ID,
				ReferenceName: handlerName,
				ReferenceKind: types.EdgeKindReferences,
				Line:          line,
				FilePath:      filePath,
				Language:      types.LanguagePython,
			})
		}
	}

	return nodes, refs
}

// ClaimsReference returns true if a handler with this name was seen in Extract.
func (r *FastAPIResolver) ClaimsReference(name string) bool { return r.claimed[name] }

// Resolve looks up a claimed handler by name. Confidence 0.85 (midpoint 0.8–0.9).
func (r *FastAPIResolver) Resolve(ctx context.Context, ref types.UnresolvedReference) (resolution.ResolvedRef, error) {
	if !r.claimed[ref.ReferenceName] {
		return resolution.ResolvedRef{}, nil
	}
	return resolution.ResolvedRef{Confidence: 0.85}, nil
}

// findNextDefFastAPI is an alias for findNextDefName — both use pyDefRe, which
// is byte-identical. The alias is kept so callers remain readable at their
// call site without a signature change.
func findNextDefFastAPI(src string, offset int) string {
	return findNextDefName(src, offset)
}

// ---------------------------------------------------------------------------
// Django resolver
// ---------------------------------------------------------------------------

// djangoPathRe matches `path('route/', view)` entries inside urlpatterns.
// Capture groups:
//
//	1 — route path (inside quotes)
//	2 — view argument (identifier like `views.user_list` or string `'app.views.foo'`)
var djangoPathRe = regexp.MustCompile(
	`(?m)\bpath\s*\(\s*r?['"]([^'"]*)['"]\s*,\s*((?:[A-Za-z_][A-Za-z0-9_.]*)|(?:['"][A-Za-z0-9_.]+['"]))\s*`,
)

// djangoRePathRe matches `re_path(r'^route$', view)` entries.
// Capture groups:
//
//	1 — route pattern (inside quotes, may start with r prefix)
//	2 — view argument
var djangoRePathRe = regexp.MustCompile(
	`(?m)\bre_path\s*\(\s*r?['"]([^'"]*)['"]\s*,\s*((?:[A-Za-z_][A-Za-z0-9_.]*)|(?:['"][A-Za-z0-9_.]+['"]))\s*`,
)

// djangoURLRe matches legacy `url(r'...', view)` entries.
var djangoURLRe = regexp.MustCompile(
	`(?m)\burl\s*\(\s*r?['"]([^'"]*)['"]\s*,\s*((?:[A-Za-z_][A-Za-z0-9_.]*)|(?:['"][A-Za-z0-9_.]+['"]))\s*`,
)

// djangoImportRe detects Django import patterns.
var djangoImportRe = regexp.MustCompile(`(?i)from\s+django|urlpatterns\s*=`)

// DjangoResolver implements FrameworkResolver + FrameworkExtractor for Django.
//
// Django URLconf entries carry NO HTTP method at the declaration site.
// Method is set to "ANY" (uppercase) consistently.
type DjangoResolver struct {
	projectRoot string
	claimed     map[string]bool
}

// NewDjangoResolver creates a DjangoResolver without a DB.
func NewDjangoResolver(projectRoot string) *DjangoResolver {
	return &DjangoResolver{projectRoot: projectRoot, claimed: make(map[string]bool)}
}

// Name returns "django".
func (r *DjangoResolver) Name() string { return "django" }

// Languages returns [python].
func (r *DjangoResolver) Languages() []types.Language {
	return []types.Language{types.LanguagePython}
}

// Detect returns true if manage.py exists, or a .py file in the top-level
// directory imports django or declares urlpatterns.
func (r *DjangoResolver) Detect(ctx context.Context) bool {
	// Presence of manage.py is the canonical Django indicator.
	if _, err := os.Stat(filepath.Join(r.projectRoot, "manage.py")); err == nil {
		return true
	}
	// Check requirements / pyproject.
	if pyHasDep(r.projectRoot, "django") {
		return true
	}
	// Content fallback: any .py file with `from django` or `urlpatterns`.
	return pyContentHasPattern(r.projectRoot, djangoImportRe, 50)
}

// Extract scans filePath/content for Django URLconf entries and returns route
// nodes + handler refs. Comments are stripped first.
//
// Django routes use method "ANY" — URLconf carries no HTTP method.
func (r *DjangoResolver) Extract(filePath, content string) ([]types.Node, []types.UnresolvedReference) {
	stripped := stripPyComments(content)
	totalLines := strings.Count(content, "\n") + 1

	var nodes []types.Node
	var refs []types.UnresolvedReference

	// Process matches from all three URL function forms.
	for _, rePair := range []struct {
		re   *regexp.Regexp
		name string
	}{
		{djangoPathRe, "path"},
		{djangoRePathRe, "re_path"},
		{djangoURLRe, "url"},
	} {
		for _, loc := range rePair.re.FindAllStringSubmatchIndex(stripped, -1) {
			if len(loc) < 6 {
				continue
			}
			routePattern := stripped[loc[2]:loc[3]]
			viewArg := strings.TrimSpace(stripped[loc[4]:loc[5]])

			line := pyLineOf(stripped, loc[0])
			if line > totalLines {
				line = totalLines
			}

			// Method is ANY for all Django URLconf entries (documented above).
			node := MakeRouteNode(filePath, line, "ANY", routePattern, types.LanguagePython)
			nodes = append(nodes, node)

			// Handler = last dot-segment of the view argument.
			// Strip any surrounding quotes first (string-based views).
			handlerName := extractLastSegment(viewArg)
			if handlerName != "" {
				r.claimed[handlerName] = true
				refs = append(refs, types.UnresolvedReference{
					ID:            fmt.Sprintf("ref:%s:%d:ANY:%s", filePath, line, handlerName),
					FromNodeID:    node.ID,
					ReferenceName: handlerName,
					ReferenceKind: types.EdgeKindReferences,
					Line:          line,
					FilePath:      filePath,
					Language:      types.LanguagePython,
				})
			}
		}
	}

	return nodes, refs
}

// ClaimsReference returns true if a handler with this name was seen in Extract.
func (r *DjangoResolver) ClaimsReference(name string) bool { return r.claimed[name] }

// Resolve looks up a claimed handler by name. Confidence 0.85 (midpoint 0.8–0.9).
func (r *DjangoResolver) Resolve(ctx context.Context, ref types.UnresolvedReference) (resolution.ResolvedRef, error) {
	if !r.claimed[ref.ReferenceName] {
		return resolution.ResolvedRef{}, nil
	}
	return resolution.ResolvedRef{Confidence: 0.85}, nil
}

// readFirstNLines for Python Detect flows is defined in frameworks.go and
// shared across all resolvers in this package.
