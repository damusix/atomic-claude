package rules

import (
	"fmt"
	"strings"
)

// CompileGlob translates one canonical glob into an anchored regex source that
// Go and JavaScript read alike; anything it cannot translate exactly is refused.
func CompileGlob(glob string) (string, error) {
	if glob == "" {
		return "", fmt.Errorf("rules: compile glob: empty pattern")
	}
	if strings.HasPrefix(glob, "/") || strings.HasPrefix(glob, "~") {
		return "", fmt.Errorf("rules: compile glob %q: absolute patterns are not allowed", glob)
	}
	segments := strings.Split(glob, "/")
	for _, segment := range segments {
		if segment == "" {
			return "", fmt.Errorf("rules: compile glob %q: empty path segment", glob)
		}
		if segment == ".." {
			return "", fmt.Errorf("rules: compile glob %q: parent-escaping patterns are not allowed", glob)
		}
	}

	var body strings.Builder
	for i, segment := range segments {
		if strings.Contains(segment, "**") && segment != "**" {
			return "", fmt.Errorf("rules: compile glob %q: ** is only supported as a whole path segment", glob)
		}
		if segment == "**" {
			switch {
			case len(segments) == 1:
				body.WriteString(".*")
			case i == 0:
				body.WriteString("(?:[^/]+/)*")
			case i == len(segments)-1:
				body.WriteString("(?:/[^/]+)*")
			default:
				body.WriteString("/(?:[^/]+/)*")
			}
			continue
		}
		if i > 0 && segments[i-1] != "**" {
			body.WriteString("/")
		}
		translated, err := compileSegment(segment, glob)
		if err != nil {
			return "", err
		}
		body.WriteString(translated)
	}
	return "^" + body.String() + "$", nil
}

// compileSegment translates one glob segment, which never contains `/`.
func compileSegment(segment, glob string) (string, error) {
	var out strings.Builder
	for i := 0; i < len(segment); i++ {
		switch char := segment[i]; char {
		case '*':
			out.WriteString("[^/]*")
		case '?':
			out.WriteString("[^/]")
		case '[':
			end := strings.IndexByte(segment[i:], ']')
			if end < 0 {
				return "", fmt.Errorf("rules: compile glob %q: unterminated character class", glob)
			}
			class := segment[i+1 : i+end]
			if class == "" || strings.ContainsAny(class, `[\`) {
				return "", fmt.Errorf("rules: compile glob %q: unsupported character class %q", glob, "["+class+"]")
			}
			// A leading `!` negates the class for doublestar but is a literal
			// member in the emitted regex, so the two would disagree.
			if class[0] == '!' || class[0] == '^' {
				return "", fmt.Errorf("rules: compile glob %q: negated character class %q is not translatable", glob, "["+class+"]")
			}
			out.WriteString("[" + class + "]")
			i += end
		case '{':
			end := strings.IndexByte(segment[i:], '}')
			if end < 0 {
				return "", fmt.Errorf("rules: compile glob %q: unterminated alternative group", glob)
			}
			group := segment[i+1 : i+end]
			if strings.ContainsAny(group, "{}") {
				return "", fmt.Errorf("rules: compile glob %q: nested alternative groups are unsupported", glob)
			}
			alternatives := strings.Split(group, ",")
			if len(alternatives) < 2 {
				return "", fmt.Errorf("rules: compile glob %q: alternative group %q needs at least two members", glob, "{"+group+"}")
			}
			translated := make([]string, 0, len(alternatives))
			for _, alternative := range alternatives {
				if alternative == "" {
					return "", fmt.Errorf("rules: compile glob %q: empty alternative in %q", glob, "{"+group+"}")
				}
				member, err := compileSegment(alternative, glob)
				if err != nil {
					return "", err
				}
				translated = append(translated, member)
			}
			out.WriteString("(?:" + strings.Join(translated, "|") + ")")
			i += end
		case '\\':
			return "", fmt.Errorf("rules: compile glob %q: escapes are unsupported", glob)
		case ']', '+', '(', ')', '|', '.', '^', '$':
			out.WriteByte('\\')
			out.WriteByte(char)
		default:
			out.WriteByte(char)
		}
	}
	return out.String(), nil
}
