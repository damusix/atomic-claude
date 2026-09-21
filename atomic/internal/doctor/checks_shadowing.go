package doctor

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/damusix/atomic-claude/atomic/internal/harness"
)

// checkShadowing implements category 23: projected resources a native copy
// duplicates. Shadowing is degraded or deliberately disabled delivery, so the
// check reports it and never proposes overwriting the native copy.
//
// Two shapes are reported: the same owned digest delivered at two paths with
// one file name, and a Claude-native global AGENTS.md beside the projected
// CLAUDE.md, which the adapter is contractually forbidden to create or
// reference.
func checkShadowing(opts Opts) Result {
	report, err := loadStatus(opts)
	if err != nil {
		return unavailability(err)
	}
	if len(report.Targets) == 0 {
		return Result{Severity: PASS, Detail: "no enrolled targets to check for shadowing"}
	}

	var findings, problems []string
	for _, status := range report.Targets {
		key := status.Target.Key()

		byName := map[string][]harness.ResourceStatus{}
		for _, r := range status.Resources {
			if r.Observed == "" {
				continue
			}
			name := filepath.Base(r.Path)
			if name == "." || name == "/" {
				continue
			}
			byName[name] = append(byName[name], r)
		}
		for name, group := range byName {
			if len(group) < 2 {
				continue
			}
			first := group[0]
			for _, other := range group[1:] {
				if other.Path == first.Path || other.Observed != first.Observed {
					continue
				}
				problems = append(problems, fmt.Sprintf("%s: %s is delivered at both %s and %s", key, name, first.Path, other.Path))
			}
		}

		if status.Target.Kind == harness.KindClaude && status.Target.NativeRoot != "" {
			agents := filepath.Join(status.Target.NativeRoot, "AGENTS.md")
			if _, err := os.Stat(agents); err == nil {
				problems = append(problems, fmt.Sprintf("%s: native %s duplicates the projected CLAUDE.md guidance", key, agents))
			}
			findings = append(findings, fmt.Sprintf("%s native root %s inspected", key, status.Target.NativeRoot))
		}
	}

	detail := fmt.Sprintf("%d target(s) inspected for shadowing", len(report.Targets))
	// byName is a map, so problems arrive in randomized order; sort so two runs
	// over identical state produce byte-identical Detail.
	sort.Strings(problems)
	return resultFor(detail, problems, findings)
}
