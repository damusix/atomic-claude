package selfupdate

import (
	"fmt"
	"strconv"
	"strings"
)

// semver holds the parsed components of a semantic version string.
type semver struct {
	major      int
	minor      int
	patch      int
	prerelease string // non-empty if prerelease
}

// parseSemver parses "vX.Y.Z", "X.Y.Z", "vX.Y.Z-pre", "X.Y.Z-pre".
// Build metadata ("+...") is stripped and ignored.
func parseSemver(s string) (semver, error) {
	s = strings.TrimPrefix(s, "v")
	// strip build metadata
	if idx := strings.IndexByte(s, '+'); idx >= 0 {
		s = s[:idx]
	}
	pre := ""
	if idx := strings.IndexByte(s, '-'); idx >= 0 {
		pre = s[idx+1:]
		s = s[:idx]
	}
	parts := strings.SplitN(s, ".", 3)
	if len(parts) != 3 {
		return semver{}, fmt.Errorf("semver: invalid version %q", s)
	}
	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return semver{}, fmt.Errorf("semver: invalid major in %q: %w", s, err)
	}
	minor, err := strconv.Atoi(parts[1])
	if err != nil {
		return semver{}, fmt.Errorf("semver: invalid minor in %q: %w", s, err)
	}
	patch, err := strconv.Atoi(parts[2])
	if err != nil {
		return semver{}, fmt.Errorf("semver: invalid patch in %q: %w", s, err)
	}
	return semver{major: major, minor: minor, patch: patch, prerelease: pre}, nil
}

// compare returns -1, 0, or 1 (a < b, a == b, a > b).
// Follows semver 2.0: prerelease has lower precedence than the release.
func (a semver) compare(b semver) int {
	if a.major != b.major {
		return cmpInt(a.major, b.major)
	}
	if a.minor != b.minor {
		return cmpInt(a.minor, b.minor)
	}
	if a.patch != b.patch {
		return cmpInt(a.patch, b.patch)
	}
	// both have same numeric core — prerelease sorts lower
	switch {
	case a.prerelease == "" && b.prerelease == "":
		return 0
	case a.prerelease != "" && b.prerelease == "":
		return -1 // a is prerelease, b is release
	case a.prerelease == "" && b.prerelease != "":
		return 1
	default:
		return strings.Compare(a.prerelease, b.prerelease)
	}
}

func cmpInt(a, b int) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}

// CompareSemver compares two semver strings a and b.
// Returns -1 if a < b, 0 if a == b, 1 if a > b.
//
// Malformed inputs (unparseable by parseSemver) are treated as "0.0.0" (the
// floor), so CompareSemver("bad", "1.0.0") returns -1 and
// CompareSemver("bad", "0.0.0") returns 0. This is the canonical exported
// compare for consumers that cannot access the unexported semver type.
func CompareSemver(a, b string) int {
	sv := func(s string) semver {
		v, err := parseSemver(s)
		if err != nil {
			return semver{} // zero value == 0.0.0
		}
		return v
	}
	return sv(a).compare(sv(b))
}

// newerThan returns true when b.TagName is newer than aVersion string.
func newerThan(current string, latest string) (bool, error) {
	a, err := parseSemver(current)
	if err != nil {
		return false, err
	}
	b, err := parseSemver(latest)
	if err != nil {
		return false, err
	}
	return b.compare(a) > 0, nil
}
