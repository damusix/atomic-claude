package config

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// runCheckIgnore runs `git -C dir check-ignore args...`: code 0 ignored, 1 not,
// 128 not a work tree; ran is false when git cannot start.
func runCheckIgnore(dir, stdin string, args ...string) (out []byte, stderr string, code int, ran bool) {
	cmd := exec.Command("git", append([]string{"-C", dir, "check-ignore"}, args...)...)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	out, err := cmd.Output()
	if err == nil {
		return out, "", 0, true
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return out, strings.TrimSpace(string(exit.Stderr)), exit.ExitCode(), true
	}
	return nil, "", 0, false
}

// GitCheckIgnore reports whether git ignores probe under root, with --no-index
// so a tracked file still matches. determined is false when git cannot answer.
func GitCheckIgnore(root, probe string) (ignored, determined bool) {
	_, _, code, ran := runCheckIgnore(root, "", "-q", "--no-index", probe)
	if !ran {
		return false, false
	}
	switch code {
	case 0:
		return true, true
	case 1:
		return false, true
	}
	return false, false
}

// GitIgnoreSource returns the ignore file git reports as hiding path; ok is false
// for no match or a negation, err for any other git failure.
func GitIgnoreSource(repo, path string) (source string, ok bool, err error) {
	out, stderr, code, ran := runCheckIgnore(repo, path+"\x00", "-v", "-z", "--stdin")
	switch {
	case !ran:
		return "", false, errors.New("git check-ignore: git could not be started")
	case code == 1:
		return "", false, nil
	case code != 0:
		return "", false, fmt.Errorf("git check-ignore: exit %d: %s", code, stderr)
	}
	// -z fields: source, line number, pattern, path.
	fields := strings.Split(string(out), "\x00")
	if len(fields) < 3 || strings.HasPrefix(fields[2], "!") {
		return "", false, nil
	}
	return fields[0], true, nil
}

// GitIgnoredSet returns the subset of paths git ignores under dir in one call;
// never nil, and empty when git cannot answer.
func GitIgnoredSet(dir string, paths []string) map[string]bool {
	set := map[string]bool{}
	if len(paths) == 0 {
		return set
	}
	out, _, code, ran := runCheckIgnore(dir, strings.Join(paths, "\n")+"\n", "--stdin")
	if !ran || code != 0 {
		return set
	}
	for _, line := range strings.Split(string(out), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			set[line] = true
		}
	}
	return set
}

// DocSurfacesIgnoreRule is the state-root ignore rule for the cache
// `atomic docs scan` rewrites on every run.
const DocSurfacesIgnoreRule = "/project/doc-surfaces.md"

// AppendIgnoreRule adds rule to the ignore file at path unless a line already
// equals it, creating the file and its directory as needed.
func AppendIgnoreRule(path, rule string) error {
	_, err := AppendIgnoreRuleWithHeader(path, rule, "")
	return err
}

// AppendIgnoreRuleWithHeader is AppendIgnoreRule that writes header ahead of
// the rule when it creates the file, and reports whether it wrote.
func AppendIgnoreRuleWithHeader(path, rule, header string) (bool, error) {
	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return false, fmt.Errorf("config: read %s: %w", path, err)
	}
	if os.IsNotExist(err) {
		existing = []byte(header)
	}
	for _, line := range strings.Split(string(existing), "\n") {
		if strings.TrimSpace(line) == rule {
			return false, nil
		}
	}
	if len(existing) > 0 && existing[len(existing)-1] != '\n' {
		existing = append(existing, '\n')
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, fmt.Errorf("config: mkdir %s: %w", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, append(existing, rule+"\n"...), 0o644); err != nil {
		return false, err
	}
	return true, nil
}
