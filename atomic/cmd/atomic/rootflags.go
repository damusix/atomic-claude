// Root-level argv scanning. Leaf commands set DisableFlagParsing, so the
// persistent flags are recovered from raw argv before dispatch.

package main

import (
	"fmt"
	"strings"
)

// findFirstVerb scans argv (os.Args[1:] after scanNoUpdateCheck) for the
// first positional argument, skipping flags and their values. Only --repo
// takes a value among the root-level flags; all other root flags are booleans.
// Used to gate the background update goroutine (skip when verb == "update").
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

// scanNoUpdateCheck pre-scans argv for --no-update-check (and
// --no-update-check=true/false) in any position. It returns the resolved flag
// value and a cleaned argv with the flag tokens removed so subcommand parsers
// don't trip over an unknown flag.
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

// repoFlagExemptions are verb paths (leading positional-token prefixes)
// whose own --repo flag already carries different, established semantics —
// a required target path, not the global context override — so
// scanRepoOverride must leave their argv untouched entirely:
//
//	migrate --repo <path>            : repo-scope migration target
//	config resolve --repo <root>     : the repo to resolve Pi config for
//	wiki stamp <file> --repo <path>  : summary-mode repo whose HEAD to stamp
var repoFlagExemptions = [][]string{
	{"migrate"},
	{"config", "resolve"},
	{"wiki", "stamp"},
}

// repoFlagExempt reports whether argv's verb path is one of
// repoFlagExemptions (or is prefixed by one — wiki stamp takes a positional
// <file> before its own flags, so the exempt prefix still matches).
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

// verbPrefix returns the leading run of non-flag tokens in argv. Every
// atomic invocation places its full verb path, and any of that verb's own
// positional args, before its flags (see each verb's own usage string), so
// this identifies the target verb without a full argv parse.
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

// scanRepoOverride pre-scans argv for a global "--repo <path>" or
// "--repo=<path>" override in any position and strips it, so no verb — a
// Cobra leaf or a hand-rolled flag.NewFlagSet — ever sees an unrecognized
// flag. DisableFlagParsing:true on every leaf command (see buildRootCmd)
// makes Cobra's own persistent-flag parsing a no-op regardless of --repo's
// position; this scan is the only place --repo is actually read. Not called
// when repoFlagExempt reports the invocation targets a verb with its own,
// differently-scoped --repo flag.
//
// Returns an error when --repo has no value to consume — end of argv, or the
// next token looks like another flag — rather than silently treating an
// unrelated token (e.g. the verb name) as the path.
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
