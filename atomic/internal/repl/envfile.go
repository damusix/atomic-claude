package repl

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// ParseEnvFile reads a KEY=VALUE file into entries shaped for exec.Cmd.Env,
// in file order — so a repeated key resolves the way the process environment
// already resolves one, last wins.
//
// The grammar is deliberately the small one: blank lines and lines whose first
// non-blank character is '#' are skipped; the first '=' splits; surrounding
// whitespace is trimmed; a fully single- or double-quoted value is unquoted
// once. There is no variable expansion and no inline-comment stripping — a
// value is the literal rest of the line, because guessing where a value ends is
// how a secret ends up truncated at the '#' inside it.
//
// A line that is neither blank, a comment, nor an assignment is an error naming
// its number. Skipping it would hand back a session silently missing a variable
// the eval'd code needs.
func ParseEnvFile(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("repl: open env file %s: %w", path, err)
	}
	defer file.Close()

	var entries []string
	scanner := bufio.NewScanner(file)
	for lineNo := 1; scanner.Scan(); lineNo++ {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		key, value, found := strings.Cut(line, "=")
		if !found {
			return nil, fmt.Errorf("repl: env file %s line %d: %q is not a KEY=VALUE assignment", path, lineNo, line)
		}
		key = strings.TrimSpace(key)
		if key == "" {
			return nil, fmt.Errorf("repl: env file %s line %d: assignment has no name", path, lineNo)
		}
		if strings.ContainsAny(key, " \t") {
			return nil, fmt.Errorf("repl: env file %s line %d: %q is not a valid variable name", path, lineNo, key)
		}
		entries = append(entries, key+"="+unquote(strings.TrimSpace(value)))
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("repl: read env file %s: %w", path, err)
	}
	return entries, nil
}

// unquote strips one matching pair of surrounding quotes. An unmatched quote is
// left in place: it is more likely part of the value than a typo worth guessing
// at.
func unquote(value string) string {
	if len(value) < 2 {
		return value
	}
	quote := value[0]
	if (quote == '"' || quote == '\'') && value[len(value)-1] == quote {
		return value[1 : len(value)-1]
	}
	return value
}
