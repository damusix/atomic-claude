package config

import (
	"fmt"
	"regexp"
	"time"
)

// segmentPattern is the allow-list every value must satisfy before it may
// become a single filesystem path segment: letters, digits, dot, underscore,
// hyphen — and nothing else. This is an allow-list, not a deny-list of
// dangerous substrings: a value is rejected because it isn't on the list,
// never because it "looks" hostile.
var segmentPattern = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// ValidateSegment rejects value unless it is a single safe path segment —
// non-empty, matching segmentPattern, and not "." or "..". source names the
// origin of the value (a bundle's meta.toml `created` date, a branch label,
// a slug) so a failing caller can trace the untrusted input back to its
// source. A value that fails validation is always an error: never a
// substituted default, never a sanitized-in-place rewrite.
func ValidateSegment(source, value string) error {
	if value == "" || value == "." || value == ".." || !segmentPattern.MatchString(value) {
		return fmt.Errorf("config: invalid %s %q: not a safe path segment", source, value)
	}
	return nil
}

// ValidateDateSegment validates that value's first 10 characters are a
// genuine YYYY-MM-DD calendar date, then routes that prefix through
// ValidateSegment before returning it. A calendar-date check is strictly
// narrower than ValidateSegment's generic allow-list — it also rejects a
// well-formed-looking but impossible date like "2026-13-40" — so it stays a
// separate step; the path-segment-safety property itself has exactly one
// enforcement point, shared with every other path-segment source.
func ValidateDateSegment(source, value string) (string, error) {
	if len(value) < 10 {
		return "", fmt.Errorf("config: invalid %s %q: too short", source, value)
	}
	prefix := value[:10]
	if _, err := time.Parse("2006-01-02", prefix); err != nil {
		return "", fmt.Errorf("config: invalid %s %q: %w", source, value, err)
	}
	if err := ValidateSegment(source, prefix); err != nil {
		return "", err
	}
	return prefix, nil
}
