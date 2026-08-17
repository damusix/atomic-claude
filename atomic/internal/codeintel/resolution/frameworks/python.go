// Python web-framework resolvers: Flask, FastAPI, and Django.
//
// Python's comment syntax differs from JS, so these use stripPyComments.
// Django routes record method ANY because a URLconf entry names no method at
// its declaration site — the absence is Django's, not an omission here.

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

// stripPyComments blanks comments while preserving line count, so a regex
// match still reports the source line. A hash inside a string literal is
// treated as a comment: over-stripping loses a route we might have found,
// while under-stripping would mint a route that does not exist.
func stripPyComments(src string) string {
	lines := strings.Split(src, "\n")
	var out strings.Builder
	inTriple := false
	tripleChar := byte(0) // ' or "

	for _, line := range lines {
		if inTriple {
			triple := string([]byte{tripleChar, tripleChar, tripleChar})
			idx := strings.Index(line, triple)
			if idx >= 0 {
				inTriple = false
				line = strings.Repeat(" ", idx+3) + line[idx+3:]
			} else {
				out.WriteByte('\n')
				continue
			}
		}

		result, newInTriple, newTripleChar := pyStripLine(line, inTriple, tripleChar)
		inTriple = newInTriple
		tripleChar = newTripleChar
		out.WriteString(result)
		out.WriteByte('\n')
	}
	return out.String()
}

// pyStripLine returns the stripped line and the carried triple-quote state.
func pyStripLine(line string, inTriple bool, tripleChar byte) (result string, newInTriple bool, newTC byte) {
	var b strings.Builder
	i := 0
	for i < len(line) {
		ch := line[i]

		if i+2 < len(line) && (ch == '"' || ch == '\'') &&
			line[i+1] == ch && line[i+2] == ch {
			tripleChar = ch
			triple := string([]byte{ch, ch, ch})
			rest := line[i+3:]
			end := strings.Index(rest, triple)
			if end >= 0 {
				i = i + 3 + end + 3
				continue
			}
			return b.String(), true, tripleChar
		}

		if ch == '#' {
			return b.String(), false, 0
		}

		// Skipped whole, so a # inside it is not read as a comment.
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

func pyHasDep(projectRoot, pkgName string) bool {
	lowerPkg := strings.ToLower(pkgName)

	reqPath := filepath.Join(projectRoot, "requirements.txt")
	if data, err := os.ReadFile(reqPath); err == nil {
		for _, line := range strings.Split(strings.ToLower(string(data)), "\n") {
			line = strings.TrimSpace(line)
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

// pyContentHasPattern scans only top-level .py files: a full walk at detection
// time would cost more than the frameworks it would find.
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

func pyLineOf(src string, offset int) int {
	return strings.Count(src[:offset], "\n") + 1
}

// extractLastSegment reduces "myapp.views.foo" to "foo", the form the name
// matcher looks up, and tolerates surrounding quotes.
func extractLastSegment(s string) string {
	s = strings.Trim(s, `'"`)
	parts := strings.Split(s, ".")
	return parts[len(parts)-1]
}

// flaskRouteRe matches both `@app.route('/p', methods=[...])` and the
// `@app.get('/p')` shorthand, in that alternation order. Groups 1-2 fill for
// the route form, 3-4 for the shorthand; the unused pair is empty.
var flaskRouteRe = regexp.MustCompile(
	`(?m)@(?:[A-Za-z_][A-Za-z0-9_]*)\.route\s*\(\s*['"]([^'"]+)['"]\s*(?:,\s*methods\s*=\s*[\[({]([^\])}]*)[\])}])?\s*\)` +
		`|@(?:[A-Za-z_][A-Za-z0-9_]*)\.(?:(get|post|put|delete|patch|head|options))\s*\(\s*['"]([^'"]+)['"]\s*\)`,
)

var pyDefRe = regexp.MustCompile(`(?m)^\s*(?:async\s+)?def\s+([A-Za-z_][A-Za-z0-9_]*)\s*\(`)

var flaskMethodRe = regexp.MustCompile(`'([A-Z]+)'|"([A-Z]+)"`)

type FlaskResolver struct {
	projectRoot string
	claimed     map[string]bool
}

// flaskImportRe also accepts sub-package imports (flask.helpers,
// flask_jwt_extended), since a top-level module may import only those.
var flaskImportRe = regexp.MustCompile(`(?i)from\s+flask[\w.]*\s+import|Flask\s*\(`)

func NewFlaskResolver(projectRoot string) *FlaskResolver {
	return &FlaskResolver{projectRoot: projectRoot, claimed: make(map[string]bool)}
}

func (r *FlaskResolver) Name() string { return "flask" }

func (r *FlaskResolver) Languages() []types.Language {
	return []types.Language{types.LanguagePython}
}

func (r *FlaskResolver) Detect(ctx context.Context) bool {
	if pyHasDep(r.projectRoot, "flask") {
		return true
	}
	return pyContentHasPattern(r.projectRoot, flaskImportRe, 30)
}

func (r *FlaskResolver) Extract(filePath, content string) ([]types.Node, []types.UnresolvedReference) {
	stripped := stripPyComments(content)
	totalLines := strings.Count(content, "\n") + 1

	var nodes []types.Node
	var refs []types.UnresolvedReference

	matches := flaskRouteRe.FindAllStringSubmatchIndex(stripped, -1)
	for _, loc := range matches {
		if len(loc) < 10 {
			continue
		}

		// Which alternation matched is read off which group pair is set.

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
			path = shorthandPath
			methods = []string{strings.ToUpper(shorthandMethod)}
		}

		if path == "" || len(methods) == 0 {
			continue
		}

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

func (r *FlaskResolver) ClaimsReference(name string) bool { return r.claimed[name] }

// Resolve looks up a claimed handler by name. Confidence 0.85 (midpoint 0.8–0.9).
func (r *FlaskResolver) Resolve(ctx context.Context, ref types.UnresolvedReference) (resolution.ResolvedRef, error) {
	if !r.claimed[ref.ReferenceName] {
		return resolution.ResolvedRef{}, nil
	}
	return resolution.ResolvedRef{Confidence: 0.85}, nil
}

// findNextDefName finds the function a decorator decorates, skipping blank
// lines and stacked decorators. Bounded, so it cannot attribute a distant
// function to this route.
func findNextDefName(src string, offset int) string {
	rest := src[offset:]
	m := pyDefRe.FindStringSubmatch(rest)
	if m == nil {
		return ""
	}
	return m[1]
}

// fastAPIRouteRe matches `@app.get('/p')`. Groups: method, path — the path may
// be empty, and trailing kwargs (response_model=, status_code=) are ignored by
// capturing only the first quoted string.
var fastAPIRouteRe = regexp.MustCompile(
	`(?m)@(?:[A-Za-z_][A-Za-z0-9_]*)\.` +
		`(get|post|put|delete|patch|options|head)` +
		`\s*\(\s*['"]([^'"]*)['"]\s*[,)]`,
)

var fastapiImportRe = regexp.MustCompile(`(?i)from\s+fastapi\s+import|FastAPI\s*\(`)

type FastAPIResolver struct {
	projectRoot string
	claimed     map[string]bool
}

func NewFastAPIResolver(projectRoot string) *FastAPIResolver {
	return &FastAPIResolver{projectRoot: projectRoot, claimed: make(map[string]bool)}
}

func (r *FastAPIResolver) Name() string { return "fastapi" }

func (r *FastAPIResolver) Languages() []types.Language {
	return []types.Language{types.LanguagePython}
}

func (r *FastAPIResolver) Detect(ctx context.Context) bool {
	if pyHasDep(r.projectRoot, "fastapi") {
		return true
	}
	return pyContentHasPattern(r.projectRoot, fastapiImportRe, 30)
}

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

func (r *FastAPIResolver) ClaimsReference(name string) bool { return r.claimed[name] }

// Resolve looks up a claimed handler by name. Confidence 0.85 (midpoint 0.8–0.9).
func (r *FastAPIResolver) Resolve(ctx context.Context, ref types.UnresolvedReference) (resolution.ResolvedRef, error) {
	if !r.claimed[ref.ReferenceName] {
		return resolution.ResolvedRef{}, nil
	}
	return resolution.ResolvedRef{Confidence: 0.85}, nil
}

// findNextDefFastAPI aliases findNextDefName; kept for call-site readability.
func findNextDefFastAPI(src string, offset int) string {
	return findNextDefName(src, offset)
}

// djangoPathRe matches a `path('route/', view)` entry. Groups: path, view.
var djangoPathRe = regexp.MustCompile(
	`(?m)\bpath\s*\(\s*r?['"]([^'"]*)['"]\s*,\s*((?:[A-Za-z_][A-Za-z0-9_.]*)|(?:['"][A-Za-z0-9_.]+['"]))\s*`,
)

// djangoRePathRe matches the regex-pattern form. Groups: pattern, view.
var djangoRePathRe = regexp.MustCompile(
	`(?m)\bre_path\s*\(\s*r?['"]([^'"]*)['"]\s*,\s*((?:[A-Za-z_][A-Za-z0-9_.]*)|(?:['"][A-Za-z0-9_.]+['"]))\s*`,
)

var djangoURLRe = regexp.MustCompile(
	`(?m)\burl\s*\(\s*r?['"]([^'"]*)['"]\s*,\s*((?:[A-Za-z_][A-Za-z0-9_.]*)|(?:['"][A-Za-z0-9_.]+['"]))\s*`,
)

var djangoImportRe = regexp.MustCompile(`(?i)from\s+django|urlpatterns\s*=`)

// Django URLconf entries carry NO HTTP method at the declaration site.
// Method is set to "ANY" (uppercase) consistently.
type DjangoResolver struct {
	projectRoot string
	claimed     map[string]bool
}

func NewDjangoResolver(projectRoot string) *DjangoResolver {
	return &DjangoResolver{projectRoot: projectRoot, claimed: make(map[string]bool)}
}

func (r *DjangoResolver) Name() string { return "django" }

func (r *DjangoResolver) Languages() []types.Language {
	return []types.Language{types.LanguagePython}
}

func (r *DjangoResolver) Detect(ctx context.Context) bool {
	if _, err := os.Stat(filepath.Join(r.projectRoot, "manage.py")); err == nil {
		return true
	}
	if pyHasDep(r.projectRoot, "django") {
		return true
	}
	return pyContentHasPattern(r.projectRoot, djangoImportRe, 50)
}

func (r *DjangoResolver) Extract(filePath, content string) ([]types.Node, []types.UnresolvedReference) {
	stripped := stripPyComments(content)
	totalLines := strings.Count(content, "\n") + 1

	var nodes []types.Node
	var refs []types.UnresolvedReference

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

			node := MakeRouteNode(filePath, line, "ANY", routePattern, types.LanguagePython)
			nodes = append(nodes, node)

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

func (r *DjangoResolver) ClaimsReference(name string) bool { return r.claimed[name] }

// Resolve looks up a claimed handler by name. Confidence 0.85 (midpoint 0.8–0.9).
func (r *DjangoResolver) Resolve(ctx context.Context, ref types.UnresolvedReference) (resolution.ResolvedRef, error) {
	if !r.claimed[ref.ReferenceName] {
		return resolution.ResolvedRef{}, nil
	}
	return resolution.ResolvedRef{Confidence: 0.85}, nil
}
