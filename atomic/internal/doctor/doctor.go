// Package doctor implements the `atomic doctor` subcommand: a deterministic
// integrity check for atomic-claude install + project state coherence.
package doctor

import (
	"os"
)

// Severity represents the outcome of a single check.
type Severity string

const (
	PASS Severity = "PASS"
	WARN Severity = "WARN"
	FAIL Severity = "FAIL"
	SKIP Severity = "SKIP"
)

// Result is the outcome of running one check category.
type Result struct {
	Index    int
	Name     string
	Severity Severity
	Detail   string

	Findings    []string // per-item lines; printed only when Verbose
	Remediation string   // fix hint; printed on WARN/FAIL regardless of Verbose

	// Scopes names the enrolled native roots this result was produced for. Empty
	// means the check is not scoped to ledger-enrolled roots, so its repair runs
	// the legacy verb; a scoped result is repaired through the ledger-driven
	// converge, which writes to the roots that were inspected.
	Scopes []string
}

// Opts holds the parsed CLI flags passed to Run.
type Opts struct {
	Fix       bool
	JSON      bool
	Only      []int // category indices; nil = all
	Skip      []int // category indices; nil = none
	StaleDays int
	Verbose   bool

	// ClaudeMDPath feeds the code-index check's wiki-realm detection. Empty
	// derives it from $HOME; tests set it to avoid reading the real user's file.
	ClaudeMDPath string

	// RepoRoot is resolved once per Run so no check spawns its own git
	// subprocess. Tests calling RunWith may set it to avoid git entirely.
	RepoRoot string

	// Home is the OS home directory the multi-harness lifecycle state lives
	// under. The CLI resolves it once; an empty value falls back to
	// os.UserHomeDir in the checks that need it.
	Home string

	// cache is per-Run shared state for the multi-harness checks, so several
	// independent categories report the same read-only observation without
	// repeating discovery or ledger reads. RunWith seeds it.
	cache *statusCache
}

// CheckFunc is the signature every check implementation must satisfy.
type CheckFunc func(opts Opts) Result

// Category is one entry in the category registry.
type Category struct {
	Index    int
	Name     string
	Severity Severity // default; an individual Result may override
	Run      CheckFunc

	// RepoDevOnly checks only make sense inside the atomic-claude repo. They
	// are omitted entirely elsewhere — not even a SKIP line — so users never
	// see repo-development noise. An explicit --only runs them anyway.
	RepoDevOnly bool
}

// categories is the single source of truth. Indices are stable: never
// renumber, only append.
var categories = []Category{
	{Index: 1, Name: "install", Severity: WARN, Run: checkInstall},
	{Index: 2, Name: "hooks", Severity: WARN, Run: checkHooks},
	{Index: 3, Name: "signals", Severity: WARN, Run: checkSignals},
	{Index: 4, Name: "refs", Severity: FAIL, Run: checkRefs},
	{Index: 5, Name: "manifest", Severity: FAIL, Run: checkManifest, RepoDevOnly: true},
	{Index: 6, Name: "followups", Severity: WARN, Run: checkFollowups},
	{Index: 7, Name: "memory", Severity: WARN, Run: checkMemory},
	{Index: 8, Name: "binary", Severity: WARN, Run: checkBinary},
	{Index: 9, Name: "config", Severity: WARN, Run: checkConfig},
	{Index: 10, Name: "profile", Severity: WARN, Run: checkProfile},
	{Index: 11, Name: "code-index", Severity: WARN, Run: checkCodeIndex},
	{Index: 12, Name: "migrate", Severity: WARN, Run: checkMigrateDrift},
	{Index: 13, Name: "repo-config", Severity: WARN, Run: checkRepoConfig},
	{Index: 14, Name: "output-style", Severity: WARN, Run: checkOutputStyle},
	{Index: 15, Name: "targets", Severity: WARN, Run: checkTargets},
	{Index: 16, Name: "resources", Severity: WARN, Run: checkResources},
	{Index: 17, Name: "journals", Severity: WARN, Run: checkJournals},
	{Index: 18, Name: "capabilities", Severity: WARN, Run: checkCapabilities},
	{Index: 19, Name: "rules", Severity: WARN, Run: checkRules},
	{Index: 20, Name: "trust", Severity: WARN, Run: checkTrust},
	{Index: 21, Name: "staleness", Severity: WARN, Run: checkStaleness},
	{Index: 22, Name: "conflicts", Severity: WARN, Run: checkConflicts},
	{Index: 23, Name: "shadowing", Severity: WARN, Run: checkShadowing},
	{Index: 24, Name: "codex", Severity: WARN, Run: checkCodex},
}

// Categories returns the registry. Callers must not mutate it.
func Categories() []Category {
	return categories
}

// Run executes the registry, or the subset opts selects, in index order.
func Run(opts Opts) ([]Result, error) {
	// Resolved once for the whole Run so no check spawns its own git.
	if opts.RepoRoot == "" {
		if cwd, err := os.Getwd(); err == nil {
			opts.RepoRoot = gitToplevelFn(cwd)
		}
	}

	repoDev := false
	if opts.RepoRoot != "" {
		ok, err := isRepoDevRoot(opts.RepoRoot)
		if err == nil {
			repoDev = ok
		}
	}

	return RunWith(opts, repoDev)
}

// RunWith is Run with the repo-dev verdict injected, so tests can simulate
// either kind of tree without chdir.
func RunWith(opts Opts, repoDev bool) ([]Result, error) {
	if opts.RepoRoot == "" {
		if cwd, err := os.Getwd(); err == nil {
			opts.RepoRoot = gitToplevelFn(cwd)
		}
	}
	if opts.cache == nil {
		opts.cache = &statusCache{}
	}

	onlySet := indexSet(opts.Only)
	skipSet := indexSet(opts.Skip)

	results := make([]Result, 0, len(categories))
	for _, c := range categories {
		if len(onlySet) > 0 && !onlySet[c.Index] {
			continue
		}
		if skipSet[c.Index] {
			continue
		}
		if c.RepoDevOnly && !repoDev && !onlySet[c.Index] {
			continue
		}
		r := c.Run(opts)
		r.Index = c.Index
		r.Name = c.Name
		results = append(results, r)
	}
	return results, nil
}

// indexSet turns a slice of indices into a presence map.
func indexSet(indices []int) map[int]bool {
	if len(indices) == 0 {
		return nil
	}
	m := make(map[int]bool, len(indices))
	for _, i := range indices {
		m[i] = true
	}
	return m
}
