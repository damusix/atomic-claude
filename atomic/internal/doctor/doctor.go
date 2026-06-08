// Package doctor implements the `atomic doctor` subcommand: a deterministic
// integrity check for atomic-claude install + project state coherence.
package doctor

import "os"

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

	// Rich fields — populated by checks and rendered by FormatHuman.
	Findings    []string // per-item detail lines; printed only when Verbose=true
	Remediation string   // fix hint; printed on Warn/Fail regardless of Verbose
	FixApplied  bool     // set by repair path after a fix succeeds
	FixSummary  string   // human description of what was fixed; printed when FixApplied=true
}

// Opts holds the parsed CLI flags passed to Run.
type Opts struct {
	Fix       bool
	JSON      bool
	Only      []int // resolved category indices; nil = all
	Skip      []int // resolved category indices; nil = none
	StaleDays int
	Verbose   bool
}

// CheckFunc is the signature every check implementation must satisfy.
type CheckFunc func(opts Opts) Result

// Category is one entry in the category registry.
type Category struct {
	Index    int
	Name     string
	Severity Severity // default severity for this category (individual Results may override)
	Run      CheckFunc

	// RepoDevOnly marks a check that only makes sense inside the atomic-claude
	// development repo (e.g. manifest parity against the embedded source
	// snapshot). Outside that repo it is omitted entirely — not even a SKIP
	// line — so end users running atomic doctor in their own projects never see
	// repo-development noise. An explicit `--only <index>` request overrides
	// this and runs the check anyway (it self-reports SKIP).
	RepoDevOnly bool
}

// categories is the single source of truth for all check categories.
// Indices are stable — never renumber; new checks append.
// Severity column: 4=refs→FAIL, 5=manifest→FAIL; all others→WARN.
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
}

// Categories returns the full category registry slice. Callers must not mutate.
func Categories() []Category {
	return categories
}

// Run executes the full registry (or the filtered subset in opts) and returns
// results in index order. Repo-dev-only checks are auto-omitted outside the
// atomic-claude repo (detected from cwd) unless explicitly requested via --only.
func Run(opts Opts) ([]Result, error) {
	return RunWith(opts, isRepoDevCwd())
}

// RunWith is Run with the repo-dev verdict injected, exported for testing so a
// repo-dev / non-repo-dev tree can be simulated without chdir.
func RunWith(opts Opts, repoDev bool) ([]Result, error) {
	onlySet := indexSet(opts.Only)
	skipSet := indexSet(opts.Skip)

	// TODO CP-6: short-circuit with PASS-everything and exit 0 when ResolveTarget(~/.claude/) is absent.
	results := make([]Result, 0, len(categories))
	for _, c := range categories {
		if len(onlySet) > 0 && !onlySet[c.Index] {
			continue
		}
		if skipSet[c.Index] {
			continue
		}
		// Repo-dev-only checks are noise outside the atomic-claude repo. Omit
		// them entirely unless the user explicitly asked via --only.
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

// isRepoDevCwd reports whether the current working directory is inside the
// atomic-claude development repo. Best-effort: a getwd or detection error
// resolves to false (treat as a normal user repo).
func isRepoDevCwd() bool {
	cwd, err := os.Getwd()
	if err != nil {
		return false
	}
	ok, err := IsRepoDev(cwd)
	if err != nil {
		return false
	}
	return ok
}

// indexSet converts a slice of indices to a presence map for O(1) lookup.
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

// All check functions are implemented in their respective files:
// checkInstall   → checks_install.go      (CP-3)
// checkHooks     → checks_hooks.go        (CP-4)
// checkSignals   → checks_signals.go      (CP-5)
// checkRefs      → checks_refs.go         (CP-4)
// checkManifest  → checks_manifest.go     (CP-3)
// checkFollowups → checks_followups.go    (CP-5)
// checkMemory    → checks_memory.go       (CP-5)
// checkBinary    → checks_binary.go       (CP-3)
// checkConfig    → checks_config.go       (CP-7)
// checkProfile   → checks_profile.go      (CP-5)
// checkCodeIndex → checks_code_index.go   (CP7 code-intel-integration)
