// Root-level argv scanning. Leaf commands set DisableFlagParsing, so the
// persistent flags are recovered from raw argv before dispatch.

package main

import (
	"fmt"
	"strings"
)

// findFirstVerb returns the first positional argument, skipping flags. --repo
// is the only root flag that takes a value; the rest are booleans.
func findFirstVerb(argv []string) string {
	for i := 0; i < len(argv); i++ {
		a := argv[i]
		if a == "--repo" {
			i++ // skip the value token
			continue
		}
		if strings.HasPrefix(a, "--repo=") {
			continue
		}
		if strings.HasPrefix(a, "-") {
			continue
		}
		return a
	}
	return ""
}

// scanNoUpdateCheck resolves --no-update-check from any position and returns an
// argv with its tokens removed, so no subcommand parser sees an unknown flag.
func scanNoUpdateCheck(argv []string) (found bool, cleaned []string) {
	cleaned = make([]string, 0, len(argv))
	for _, a := range argv {
		switch {
		case a == "--no-update-check" || a == "--no-update-check=true":
			found = true
		case a == "--no-update-check=false":
			// explicit false — leave found as-is, strip the token
		default:
			cleaned = append(cleaned, a)
		}
	}
	return found, cleaned
}

// repoFlagExempt: `migrate --repo <path>` is a target path, not the global
// context override, so scanRepoOverride must not consume it.
func repoFlagExempt(argv []string) bool {
	return len(argv) > 0 && argv[0] == "migrate"
}

// scanRepoOverride is the only place the global --repo is read, since every
// leaf sets DisableFlagParsing. It strips the tokens so no verb sees an
// unrecognized flag. Errors rather than consuming an unrelated token (the verb
// name, say) when --repo has no value to take.
func scanRepoOverride(argv []string) (value string, cleaned []string, err error) {
	cleaned = make([]string, 0, len(argv))
	for i := 0; i < len(argv); i++ {
		a := argv[i]
		switch {
		case a == "--repo":
			if i+1 >= len(argv) || strings.HasPrefix(argv[i+1], "-") {
				return "", nil, fmt.Errorf("--repo requires a value")
			}
			value = argv[i+1]
			i++
		case strings.HasPrefix(a, "--repo="):
			value = strings.TrimPrefix(a, "--repo=")
		default:
			cleaned = append(cleaned, a)
		}
	}
	return value, cleaned, nil
}
