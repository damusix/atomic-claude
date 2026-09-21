package doctor

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/harness"
	"github.com/damusix/atomic-claude/atomic/internal/installstate"
)

// ClaudeHomeMissing is the pre-flight gate: no <home>/.claude means no
// Claude-scoped category check can say anything useful.
func ClaudeHomeMissing(home string) bool {
	_, err := os.Stat(filepath.Join(home, ".claude"))
	return os.IsNotExist(err)
}

// HasEnrolledTargets reports whether the install ledger records any enrolled
// harness target, Claude or otherwise. An OMP-only home has no ~/.claude but
// still owns lifecycle state the harness categories must report on.
//
// A ledger that cannot be read — permissions, undecodable JSON, or a schema
// this binary refuses — counts as enrolled. That read failure is exactly the
// defect the harness categories exist to surface, so short-circuiting it to
// "atomic-claude not installed" would swallow it; running the categories lets
// them report the unreadable ledger instead.
func HasEnrolledTargets(home string) bool {
	ledger, err := installstate.LoadLedger(config.LedgerPath(home))
	if err != nil {
		return true
	}
	return len(ledger.Targets) > 0
}

// ShortCircuit reports whether doctor should stop before running any category.
// A missing Claude home only short-circuits a machine with nothing enrolled; as
// soon as the ledger holds a target — an OMP-only home included — the harness
// categories run and the Claude-scoped ones skip on their own.
func ShortCircuit(home string) bool {
	return ClaudeHomeMissing(home) && !HasEnrolledTargets(home)
}

// claudeScope is the Claude-scoped surface one category must inspect. The ledger
// is the authority: a machine whose ledger enrols Claude targets is inspected at
// each target's own NativeRoot, and one whose ledger enrols none skips these
// categories rather than failing an un-enrolled ~/.claude. An unresolved ledger —
// an install that predates enrollment — keeps the legacy default root, so an
// existing user's checks are unchanged.
type claudeScope struct {
	// Roots are the Claude config directories to inspect, in stable order.
	Roots []string
	// Enrolled reports that Roots came from the ledger.
	Enrolled bool
	// Skip reports that the categories have nothing to inspect: either a ledger
	// that enrols no Claude target, or no Claude home and no enrollment at all.
	Skip bool
	// Detail names why Skip is set.
	Detail string
}

// claudeNoTargetDetail is the SKIP detail for a machine whose ledger enrols a
// harness other than Claude.
const claudeNoTargetDetail = "no Claude target enrolled; Claude-scoped checks not applicable"

// claudeNoHomeDetail is the SKIP detail for a machine with no Claude home and
// no enrollment at all.
const claudeNoHomeDetail = "no Claude home; not applicable"

// claudeScopeFor resolves the Claude config directories a Claude-scoped category
// reads.
func claudeScopeFor(opts Opts) claudeScope {
	legacy := func() claudeScope {
		home := opts.Home
		if home == "" {
			resolved, err := os.UserHomeDir()
			if err != nil {
				return claudeScope{}
			}
			home = resolved
		}
		root := filepath.Join(home, ".claude")
		if _, err := os.Stat(root); os.IsNotExist(err) {
			return claudeScope{Skip: true, Detail: claudeNoHomeDetail}
		}
		return claudeScope{Roots: []string{root}}
	}

	ledger, err := loadLedger(opts)
	if err != nil || len(ledger.Targets) == 0 {
		return legacy()
	}
	seen := make(map[string]bool, len(ledger.Targets))
	var roots []string
	for _, target := range ledger.Targets {
		if target.Harness != string(harness.KindClaude) || target.NativeRoot == "" {
			continue
		}
		if seen[target.NativeRoot] {
			continue
		}
		seen[target.NativeRoot] = true
		roots = append(roots, target.NativeRoot)
	}
	if len(roots) == 0 {
		return claudeScope{Skip: true, Detail: claudeNoTargetDetail}
	}
	sort.Strings(roots)
	return claudeScope{Roots: roots, Enrolled: true}
}

// combineClaudeRoots merges one per-root result into a single category result:
// the worst severity wins, every root's detail and findings survive, and an
// enrolled root list is carried so a repair targets what was inspected.
func combineClaudeRoots(scope claudeScope, results []Result) Result {
	out := Result{}
	if scope.Enrolled {
		out.Scopes = scope.Roots
	}
	var details []string
	for i, r := range results {
		if len(results) == 1 {
			details = append(details, r.Detail)
		} else {
			details = append(details, scope.Roots[i]+": "+r.Detail)
		}
		out.Findings = append(out.Findings, r.Findings...)
		if severityRank(r.Severity) > severityRank(out.Severity) {
			out.Severity = r.Severity
			out.Remediation = r.Remediation
		}
	}
	if out.Severity == "" {
		out.Severity = SKIP
	}
	if scope.Enrolled && severityRank(out.Severity) >= severityRank(WARN) {
		// The per-root producers name `atomic claude update`, which resolves its
		// own default root and would write outside the ledger on a relocated or
		// adopted target — the very outcome the scoped converge avoids. Name the
		// converge here so the printed hint agrees with the `--fix` plan.
		out.Remediation = claudeConvergePlan
	}
	out.Detail = strings.Join(details, "; ")
	return out
}

// severityRank orders severities by how much attention they demand, so a merge
// reports the worst root's verdict.
func severityRank(s Severity) int {
	switch s {
	case FAIL:
		return 3
	case WARN:
		return 2
	case PASS:
		return 1
	default:
		return 0
	}
}

const missingHomeMessage = "atomic-claude not installed; run `atomic claude install`."

// MissingHomeMessage returns the short-circuit message.
func MissingHomeMessage() string {
	return missingHomeMessage
}
