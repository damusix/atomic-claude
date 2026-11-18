package doctor

import (
	"fmt"
	"os"
	"slices"
	"sort"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/harness"
	"github.com/damusix/atomic-claude/atomic/internal/installstate"
)

// ClaudeHomeMissing is the pre-flight gate: no Claude configuration root means
// no Claude-scoped category check can say anything useful.
func ClaudeHomeMissing(home string) bool {
	_, err := os.Stat(config.ClaudeConfigDir(home))
	return os.IsNotExist(err)
}

// HasEnrolledTargets reports whether the ledger records any harness target. An
// unreadable ledger counts as enrolled so the harness categories report it.
func HasEnrolledTargets(home string) bool {
	ledger, err := installstate.LoadLedger(config.LedgerPath(home))
	if err != nil {
		return true
	}
	return len(ledger.Targets) > 0
}

// ShortCircuit reports whether doctor should stop before running any category:
// only when the Claude home is missing and nothing is enrolled.
func ShortCircuit(home string) bool {
	return ClaudeHomeMissing(home) && !HasEnrolledTargets(home)
}

// claudeScope is the Claude surface one category inspects. The ledger decides;
// an install that predates enrollment keeps the legacy default root.
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

// claudeNoTargetDetail is the SKIP detail for a machine whose ledger enrols no
// Claude target, naming the harnesses it does enrol.
func claudeNoTargetDetail(enrolled []string) string {
	var also string
	if len(enrolled) > 0 {
		also = fmt.Sprintf(" (%s enrolled)", strings.Join(enrolled, ", "))
	}
	return "no Claude target enrolled" + also + "; Claude-scoped checks not applicable"
}

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
		root := config.ClaudeConfigDir(home)
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
	var roots, others []string
	for _, target := range ledger.Targets {
		if target.Harness != string(harness.KindClaude) {
			if !slices.Contains(others, target.Harness) {
				others = append(others, target.Harness)
			}
			continue
		}
		if target.NativeRoot == "" {
			continue
		}
		if seen[target.NativeRoot] {
			continue
		}
		seen[target.NativeRoot] = true
		roots = append(roots, target.NativeRoot)
	}
	if len(roots) == 0 {
		if opts.Harness == harness.KindClaude {
			return legacy()
		}
		sort.Strings(others)
		return claudeScope{Skip: true, Detail: claudeNoTargetDetail(others)}
	}
	sort.Strings(roots)
	return claudeScope{Roots: roots, Enrolled: true}
}

// combineClaudeRoots merges per-root results; the worst severity wins and the
// enrolled roots are carried so a repair targets what was inspected.
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
		// The per-root hint names `atomic claude update`, which misses a relocated
		// root; this keeps the printed hint in step with the `--fix` plan.
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

// MissingHomeMessage returns the short-circuit message for the detected
// harness; an empty kind is the Claude default.
func MissingHomeMessage(kind harness.Kind) string {
	switch kind {
	case harness.KindOMP, harness.KindCodex:
		return fmt.Sprintf("atomic not installed for %s; run `atomic install --harness %s`.", kind, kind)
	}
	return missingHomeMessage
}
