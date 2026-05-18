package validate

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// isPathArg reports whether arg looks like a file path rather than a bare
// subcommand verb. A path arg contains a path separator or has a file
// extension (contains a dot after the last slash). Pure verbs ("spec",
// "config", "bundle") contain neither.
func isPathArg(arg string) bool {
	return strings.ContainsAny(arg, "/\\") || strings.Contains(filepath.Base(arg), ".")
}

// runPathDispatch handles `atomic validate <path> [<path>...]` — paths but no
// subcommand. Finds repo root, then delegates to dispatchPaths for per-path
// routing and output.
//
// Header: emits "atomic validate spec — path-aware routing" so the spec
// subcommand label appears in output, matching the spec contract that "header
// line per subcommand still appears so user can tell which validator emitted
// which findings." Using "spec" because all routable paths dispatch to the
// spec validator in v1.
func runPathDispatch(paths []string, jsonOut, suggest bool, w io.Writer) int {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(w, "atomic validate: cannot get working directory: %v\n", err)
		return 2
	}
	root := findRepoRoot(cwd)
	if root == "" {
		fmt.Fprintf(w, "atomic validate: no .git found from %s\n", cwd)
		return 2
	}

	findings, errCode := dispatchPaths(paths, root, w)
	if errCode != 0 {
		return errCode
	}

	s := summarize(findings)
	if jsonOut {
		printJSON(w, findings, s)
	} else {
		// Use "spec" label so the subcommand is identifiable in output.
		printHeader(w, "spec", "path-aware routing")
		printHuman(w, findings, s, suggest)
	}
	return exitCode(s)
}

// dispatchPaths implements path-aware routing for `atomic validate <paths...>`.
//
// Routing table (CP-8):
//   - docs/spec/*.md  → spec validator (RunSpecRules on each file)
//   - all other paths → WARN finding: "no validator applicable; supported: docs/spec/*.md"
//
// Config and bundle validators are whole-repo (not per-file), so they are not
// routable from individual paths in v1. They remain accessible via
// `atomic validate config` / `atomic validate bundle` explicitly.
//
// Returns the combined findings and a max exit code (0 or 1). Returns 2 on
// internal error (file unreadable, parse crash).
func dispatchPaths(paths []string, repoRoot string, w io.Writer) ([]Finding, int) {
	var all []Finding

	for _, p := range paths {
		// Normalize: strip repoRoot prefix for display; use clean slashes for
		// matching so the routing table works on both Unix and Windows.
		//
		// For absolute paths, call Rel directly — Join(repoRoot, absPath) on
		// Unix produces a double-rooted path (repoRoot + absPath concatenated),
		// so Rel's result would be wrong.
		rel := p
		if filepath.IsAbs(p) {
			if r, err := filepath.Rel(repoRoot, p); err == nil {
				rel = r
			}
		} else if r, err := filepath.Rel(repoRoot, filepath.Join(repoRoot, p)); err == nil {
			rel = r
		}
		cleanRel := filepath.ToSlash(rel)

		if isSpecPath(cleanRel) {
			// Route to spec validator.
			abs := p
			if !filepath.IsAbs(p) {
				abs = filepath.Join(repoRoot, p)
			}
			src, err := os.ReadFile(abs)
			if err != nil {
				fmt.Fprintf(w, "atomic validate: cannot read %s: %v\n", p, err)
				return nil, 2
			}
			findings, err := RunSpecRules(rel, src)
			if err != nil {
				fmt.Fprintf(w, "atomic validate: %v\n", err)
				return nil, 2
			}
			all = append(all, findings...)
		} else {
			// Unknown path: emit WARN.
			all = append(all, Finding{
				Severity: "WARN",
				Rule:     "dispatch",
				Path:     rel,
				Line:     0,
				Message:  fmt.Sprintf("path %s: no validator applicable; supported: docs/spec/*.md", rel),
			})
		}
	}

	sortFindings(all)
	return all, 0
}

// isSpecPath reports whether the slash-normalized relative path falls under
// docs/spec/ and ends with .md.
func isSpecPath(slashRel string) bool {
	return strings.HasPrefix(slashRel, "docs/spec/") && strings.HasSuffix(slashRel, ".md")
}

// runWholeRepo runs spec + config + bundle in sequence, emitting one header
// per subcommand. Findings from all three validators are aggregated for the
// summary, but each validator's header+findings block is printed separately
// so the user can see which validator produced which findings.
//
// JSON mode suppresses headers and emits a single envelope over all findings.
//
// Sequential ordering is intentional for v1: spec first (fastest, most likely
// to fail on modified files), then config, then bundle. Parallelization is a
// v1.1 consideration.
func runWholeRepo(jsonOut, suggest bool, w io.Writer) int {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(w, "atomic validate: cannot get working directory: %v\n", err)
		return 2
	}
	root := findRepoRoot(cwd)
	if root == "" {
		fmt.Fprintf(w, "atomic validate: no .git found from %s\n", cwd)
		return 2
	}

	// --- Spec ---
	specPaths, err := filepath.Glob(filepath.Join(root, "docs", "spec", "*.md"))
	if err != nil {
		fmt.Fprintf(w, "atomic validate: spec glob error: %v\n", err)
		return 2
	}
	var specFindings []Finding
	for _, p := range specPaths {
		src, err := os.ReadFile(p)
		if err != nil {
			fmt.Fprintf(w, "atomic validate: cannot read %s: %v\n", p, err)
			return 2
		}
		rel, _ := filepath.Rel(root, p)
		ff, err := RunSpecRules(rel, src)
		if err != nil {
			fmt.Fprintf(w, "atomic validate: %v\n", err)
			return 2
		}
		specFindings = append(specFindings, ff...)
	}
	sortFindings(specFindings)
	specSummary := summarize(specFindings)

	// --- Config ---
	configFindings, err := RunConfigRules(root)
	if err != nil {
		fmt.Fprintf(w, "atomic validate: config error: %v\n", err)
		return 2
	}
	configSummary := summarize(configFindings)

	// --- Bundle ---
	// Capture bundle output separately so we can aggregate findings.
	bundleFindings, bundleSummary, bundleErr := runBundleCollect(root)
	if bundleErr != 0 {
		fmt.Fprintf(w, "atomic validate: bundle check failed: internal error (exit %d)\n", bundleErr)
		return 2
	}

	// --- Aggregate ---
	var allFindings []Finding
	allFindings = append(allFindings, specFindings...)
	allFindings = append(allFindings, configFindings...)
	allFindings = append(allFindings, bundleFindings...)

	aggSummary := summary{
		Pass: specSummary.Pass + configSummary.Pass + bundleSummary.Pass,
		Warn: specSummary.Warn + configSummary.Warn + bundleSummary.Warn,
		Fail: specSummary.Fail + configSummary.Fail + bundleSummary.Fail,
	}

	if jsonOut {
		// Single JSON envelope over all findings, no headers.
		printJSON(w, allFindings, aggSummary)
		return exitCode(aggSummary)
	}

	// Human mode: one header+block per subcommand, then aggregate summary.
	printHeader(w, "spec", "structural integrity")
	printHuman(w, specFindings, specSummary, suggest)

	fmt.Fprintln(w)

	printHeader(w, "config", "referential integrity")
	printHuman(w, configFindings, configSummary, suggest)

	fmt.Fprintln(w)

	printHeader(w, "bundle", "manifest parity")
	printHuman(w, bundleFindings, bundleSummary, suggest)

	return exitCode(aggSummary)
}
