package doctor

import (
	"errors"
	"sort"
	"strings"
	"sync"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/harness"
	"github.com/damusix/atomic-claude/atomic/internal/install"
	"github.com/damusix/atomic-claude/atomic/internal/installstate"
)

// errNoHome marks a multi-harness check that cannot resolve the home directory
// the lifecycle state lives under.
var errNoHome = errors.New("no home directory resolved")

// statusCache memoizes the read-only multi-harness observations one Run
// gathers. The categories report independently but read the same state, so a
// shared load keeps every category from repeating discovery, ledger, and
// projection work. Every seam it caches is read-only by contract.
type statusCache struct {
	statusOnce sync.Once
	status     install.StatusReport
	statusErr  error

	rulesOnce sync.Once
	rules     []install.RulesTargetStatus
	rulesErr  error

	registryOnce sync.Once
	registry     *harness.Registry
	registryErr  error

	ledgerOnce sync.Once
	ledger     *installstate.Ledger
	ledgerErr  error
}

// get returns the per-Run cache, tolerating the nil a bare Opts carries so a
// directly-invoked check still works.
func (c *statusCache) get() *statusCache {
	if c == nil {
		return &statusCache{}
	}
	return c
}

// installHome resolves the home the multi-harness checks read. The CLI resolves
// it once; an empty value falls back to the OS home so a directly-invoked check
// still finds the real state.
func installHome(opts Opts) string {
	if opts.Home != "" {
		return opts.Home
	}
	if home, err := resolveHome(); err == nil {
		return home
	}
	return ""
}

// statusStepsFn builds the read-only reporting steps the multi-harness
// categories read through. Tests swap it to inject a registry with deterministic
// discovery, so a category test never shells out to a real harness binary.
var statusStepsFn = install.DefaultSteps

// loadStatus reads the home's discovered instances, enrolled targets, shared
// resources, and retention in one read-only pass.
func loadStatus(opts Opts) (install.StatusReport, error) {
	c := opts.cache.get()
	c.statusOnce.Do(func() {
		home := installHome(opts)
		if home == "" {
			c.statusErr = errNoHome
			return
		}
		c.status, c.statusErr = statusStepsFn(home).Status(install.Selection{})
	})
	return c.status, c.statusErr
}

// loadRules reads every enrolled target's rule tier, gaps, and owned rule
// resources against the selected generation.
func loadRules(opts Opts) ([]install.RulesTargetStatus, error) {
	c := opts.cache.get()
	c.rulesOnce.Do(func() {
		home := installHome(opts)
		if home == "" {
			c.rulesErr = errNoHome
			return
		}
		c.rules, c.rulesErr = statusStepsFn(home).RulesStatus()
	})
	return c.rules, c.rulesErr
}

// loadRegistry builds the adapter registry the capability and disablement
// checks report on. It reads no enrollment.
func loadRegistry(opts Opts) (*harness.Registry, error) {
	c := opts.cache.get()
	c.registryOnce.Do(func() {
		c.registry, c.registryErr = install.DefaultRegistry(installHome(opts))
	})
	return c.registry, c.registryErr
}

// loadLedger reads the enrollment ledger once per Run.
func loadLedger(opts Opts) (*installstate.Ledger, error) {
	c := opts.cache.get()
	c.ledgerOnce.Do(func() {
		home := installHome(opts)
		if home == "" {
			c.ledgerErr = errNoHome
			return
		}
		c.ledger, c.ledgerErr = installstate.LoadLedger(config.LedgerPath(home))
	})
	return c.ledger, c.ledgerErr
}

// unavailability renders the load error every multi-harness category shares
// when the lifecycle state cannot be read at all.
func unavailability(err error) Result {
	return Result{Severity: WARN, Detail: "multi-harness state unavailable: " + err.Error()}
}

// sortedFindings orders finding lines so a report is byte-stable whatever order
// the underlying state was walked in.
func sortedFindings(findings []string) []string {
	out := append([]string(nil), findings...)
	sort.Strings(out)
	return out
}

// resultFor builds a WARN from problem lines, or a PASS with the given detail.
func resultFor(detail string, problems, findings []string) Result {
	findings = sortedFindings(findings)
	if len(problems) > 0 {
		return Result{Severity: WARN, Detail: strings.Join(problems, "; "), Findings: findings}
	}
	return Result{Severity: PASS, Detail: detail, Findings: findings}
}

// shortDigest truncates a content digest for display, matching the converge
// prompt's convention.
func shortDigest(digest string) string {
	if len(digest) <= 12 {
		return digest
	}
	return digest[:12]
}
