package workflow

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/config"
)

var (
	// ErrInvalidSetting marks an unknown key or an invalid value.
	ErrInvalidSetting = errors.New("invalid setting")
	// ErrNoLocation means no repository or realm holds the level's file.
	ErrNoLocation = errors.New("no location")
)

// ValidateSetting accepts what a block line can carry and the parser reads
// back as an explicit value.
func ValidateSetting(key, value string) error {
	k := Key(key)
	if !knownKey(k) {
		return fmt.Errorf("%w: unknown key %q", ErrInvalidSetting, key)
	}
	if value == "" || strings.TrimSpace(value) != value || strings.ContainsAny(value, "\r\n#") {
		return fmt.Errorf("%w: %s: invalid value %q", ErrInvalidSetting, key, value)
	}
	if err := validate(k, value); err != nil {
		return fmt.Errorf("%w: %s: %v", ErrInvalidSetting, key, err)
	}
	return nil
}

// SetValue writes an explicit line for key, dropping later lines for it. In a
// shared file, a block of the other level is a conflict.
func SetValue(content string, level Level, key Key, value string, shared bool) (string, error) {
	if err := ValidateSetting(string(key), value); err != nil {
		return "", err
	}
	b, err := ParseBlock(content)
	if err != nil {
		return "", err
	}
	line := fmt.Sprintf("- %s: %s", key, value)
	if b == nil {
		block := fmt.Sprintf("<workflow-preferences level='%s'>\n%s\n%s\n", level, line, closeTag)
		switch {
		case content == "":
			return block, nil
		case strings.HasSuffix(content, "\n"):
			return content + "\n" + block, nil
		}
		return content + "\n\n" + block, nil
	}

	if shared {
		existing := Level(b.Level)
		if existing != LevelRealm {
			existing = LevelRepo
		}
		if existing != level {
			return "", fmt.Errorf("the repo and realm levels share this file: its block is level %q, requested %q", existing, level)
		}
	}

	lines := strings.Split(content, "\n")
	first, had := b.Lines[key]
	closeAt := b.Close
	lines = b.SetLine(lines, key, line)
	if !had {
		return strings.Join(lines, "\n"), nil
	}
	out := slices.Clone(lines[:first.N+1])
	for _, l := range lines[first.N+1 : closeAt] {
		if lineKey(l) != key {
			out = append(out, l)
		}
	}
	return strings.Join(append(out, lines[closeAt:]...), "\n"), nil
}

func lineKey(line string) Key {
	body, ok := strings.CutPrefix(strings.TrimSpace(line), "- ")
	name, _, hasColon := strings.Cut(body, ":")
	if !ok || !hasColon {
		return ""
	}
	return Key(strings.TrimSpace(name))
}

// TargetPath is the file for level; shared means repo and realm use one file.
// A populated legacy state root is used in place. The repository is the one
// holding dir, found as Resolve finds it.
func TargetPath(level Level, dir, home string) (path string, shared bool, err error) {
	if level == LevelProfile {
		if home == "" {
			return "", false, fmt.Errorf("%w: no home directory", ErrNoLocation)
		}
		return config.ProfilePath(home), false, nil
	}
	_, repoPath, repoErr := repoPrefsPath(dir)
	realmPath, realmErr := realmPrefsPath(dir, home)
	shared = repoErr == nil && realmErr == nil && repoPath == realmPath

	switch level {
	case LevelRepo:
		return repoPath, shared, repoErr
	case LevelRealm:
		return realmPath, shared, realmErr
	}
	return "", false, fmt.Errorf("no file for level %q", level)
}
