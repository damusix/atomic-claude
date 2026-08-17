package validate

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// isPathArg reports whether arg looks like a path rather than a bare verb: it
// carries a separator or an extension, which no verb does.
func isPathArg(arg string) bool {
	return strings.ContainsAny(arg, "/\\") || strings.Contains(filepath.Base(arg), ".")
}

// runPathDispatch handles `atomic validate <path>...` with no subcommand. The
// header still names a subcommand so the user can tell which validator spoke;
// it says "spec" because every routable path dispatches there.
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
		printHeader(w, "spec", "path-aware routing")
		printHuman(w, findings, s, suggest)
	}
	return exitCode(s)
}

// dispatchPaths routes docs/spec/*.md to the spec validator and WARNs on
// anything else. The config and bundle validators are whole-repo, never
// per-file, so a path cannot reach them — only their explicit subcommands can.
func dispatchPaths(paths []string, repoRoot string, w io.Writer) ([]Finding, int) {
	var all []Finding

	for _, p := range paths {
		// Absolute paths go straight to Rel: Join(repoRoot, absPath) would
		// concatenate into a double-rooted path and skew the result.
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

// isSpecPath reports whether rel is a docs/spec/*.md path.
func isSpecPath(slashRel string) bool {
	return strings.HasPrefix(slashRel, "docs/spec/") && strings.HasSuffix(slashRel, ".md")
}

// runWholeRepo runs every validator in sequence, printing a header+findings
// block each so the user can attribute findings, then one aggregate summary.
// Spec runs first: it is the fastest and the likeliest to fail.
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

	configFindings, err := RunConfigRules(root)
	if err != nil {
		fmt.Fprintf(w, "atomic validate: config error: %v\n", err)
		return 2
	}
	configSummary := summarize(configFindings)

	// Skipped silently outside the atomic-claude repo: a user's own project has
	// no embedded source snapshot to compare against.
	var bundleFindings []Finding
	var bundleSummary summary
	includeBundle := repoDev(root)
	if includeBundle {
		var bundleErr int
		bundleFindings, bundleSummary, bundleErr = runBundleCollect(root)
		if bundleErr != 0 {
			fmt.Fprintf(w, "atomic validate: bundle check failed: internal error (exit %d)\n", bundleErr)
			return 2
		}
	}

	artifactsFindings, artifactsSummary, artifactsErr := runArtifactsCollect(root)
	if artifactsErr != 0 {
		fmt.Fprintf(w, "atomic validate: artifacts check failed: internal error (exit %d)\n", artifactsErr)
		return 2
	}

	var allFindings []Finding
	allFindings = append(allFindings, specFindings...)
	allFindings = append(allFindings, configFindings...)
	allFindings = append(allFindings, bundleFindings...)
	allFindings = append(allFindings, artifactsFindings...)

	aggSummary := summary{
		Pass: specSummary.Pass + configSummary.Pass + bundleSummary.Pass + artifactsSummary.Pass,
		Warn: specSummary.Warn + configSummary.Warn + bundleSummary.Warn + artifactsSummary.Warn,
		Fail: specSummary.Fail + configSummary.Fail + bundleSummary.Fail + artifactsSummary.Fail,
	}

	if jsonOut {
		printJSON(w, allFindings, aggSummary)
		return exitCode(aggSummary)
	}

	printHeader(w, "spec", "structural integrity")
	printHuman(w, specFindings, specSummary, suggest)

	fmt.Fprintln(w)

	printHeader(w, "config", "referential integrity")
	printHuman(w, configFindings, configSummary, suggest)

	if includeBundle {
		fmt.Fprintln(w)
		printHeader(w, "bundle", "manifest parity")
		printHuman(w, bundleFindings, bundleSummary, suggest)
	}

	fmt.Fprintln(w)
	printHeader(w, "artifacts", "CLI-flag citation integrity")
	printHuman(w, artifactsFindings, artifactsSummary, suggest)

	return exitCode(aggSummary)
}
