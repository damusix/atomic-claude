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

// repoFlagExemptions are verb paths whose own --repo is a required target path,
// not the global context override, so scanRepoOverride must not consume it:
//
//	migrate --repo <path>            : repo-scope migration target
//	config resolve --repo <root>     : the repo to resolve Pi config for
//	wiki stamp <file> --repo <path>  : summary-mode repo whose HEAD to stamp
var repoFlagExemptions = [][]string{
	{"migrate"},
	{"config", "resolve"},
	{"wiki", "stamp"},
}

// repoFlagExempt matches on prefix, not equality: `wiki stamp` takes a
// positional <file> before its flags, and must still match.
func repoFlagExempt(argv []string) bool {
	prefix := verbPrefix(argv)
	for _, exempt := range repoFlagExemptions {
		if len(prefix) < len(exempt) {
			continue
		}
		matched := true
		for i, tok := range exempt {
			if prefix[i] != tok {
				matched = false
				break
			}
		}
		if matched {
			return true
		}
	}
	return false
}

// verbPrefix returns the leading run of non-flag tokens. Every invocation puts
// the verb path and its positionals before any flag, so this identifies the
// target verb without a full argv parse.
func verbPrefix(argv []string) []string {
	var out []string
	for _, a := range argv {
		if strings.HasPrefix(a, "-") {
			break
		}
		out = append(out, a)
	}
	return out
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
