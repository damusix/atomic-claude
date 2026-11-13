package doctor

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/bundlespec"
	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/harness"
)

// agentsMDMinClaude is the first Claude Code release that reads AGENTS.md when
// no CLAUDE.md is present.
var agentsMDMinClaude = [3]int{2, 1, 277}

// claudeCLIVersionFn returns `claude --version` output; a seam for tests.
var claudeCLIVersionFn = func() (string, error) {
	out, err := exec.Command("claude", "--version").Output()
	return string(out), err
}

var claudeVersionRE = regexp.MustCompile(`(\d+)\.(\d+)\.(\d+)`)

// claudeSiblingFiles are the files whose presence makes Claude skip AGENTS.md.
var claudeSiblingFiles = []string{"CLAUDE.md", filepath.Join(".claude", "CLAUDE.md"), "CLAUDE.local.md"}

// checkSteering is category 26: whether the repository's Claude steering mode
// reaches the harnesses that read it. The mode is the `[steering] claude` key.
func checkSteering(opts Opts) Result {
	root := opts.RepoRoot
	if root == "" {
		if cwd, err := os.Getwd(); err == nil {
			if root = gitToplevelFn(cwd); root == "" {
				root, _ = config.FindScopeRoot(cwd, "realm")
			}
		}
	}
	home := opts.Home
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	return RunCheckSteeringWith(root, home)
}

// RunCheckSteeringWith checks root's steering mode against the files beside it,
// the installed Claude, and the enrolled harnesses. Exported for testing.
func RunCheckSteeringWith(root, home string) Result {
	if root == "" {
		return Result{Severity: PASS, Detail: "no repository"}
	}
	mode, err := config.ResolveSteeringMode(home, root)
	if err != nil {
		return Result{Severity: WARN, Detail: err.Error(), Remediation: "set steering.claude to loader, agents-md, or claude-md"}
	}

	var problems []string
	var info string
	switch mode {
	case bundlespec.SteeringAgentsMD:
		for _, dir := range steeringPairDirs(root) {
			for _, name := range claudeSiblingFiles {
				if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
					problems = append(problems, fmt.Sprintf("agents-md mode, but %s exists, so Claude skips %s", relTo(root, filepath.Join(dir, name)), relTo(root, filepath.Join(dir, "AGENTS.md"))))
				}
			}
		}
		out, err := claudeCLIVersionFn()
		version := claudeVersionRE.FindStringSubmatch(out)
		switch {
		case err != nil || version == nil:
			info = "info: Claude version unknown (`claude --version` missing or unparseable); agents-md needs Claude 2.1.277 or newer"
		case versionBefore(version[1:], agentsMDMinClaude):
			problems = append(problems, fmt.Sprintf("agents-md mode, but Claude %s predates 2.1.277 and ignores AGENTS.md", version[0]))
		}
	case bundlespec.SteeringClaudeMD:
		for _, dir := range steeringPairDirs(root) {
			claude, err := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
			if _, aerr := os.Stat(filepath.Join(dir, "AGENTS.md")); err == nil && aerr == nil && bundlespec.ScopeSteering.OwnsLoader(claude) {
				problems = append(problems, fmt.Sprintf("%s stays in loader shape because its AGENTS.md holds user text", relTo(root, dir)))
			}
		}
		if home != "" {
			kinds, err := harness.EnrolledKinds(home)
			if err == nil {
				for _, k := range []harness.Kind{harness.KindOMP, harness.KindCodex} {
					if slices.Contains(kinds, k) {
						problems = append(problems, fmt.Sprintf("claude-md mode writes no AGENTS.md, but %s is enrolled and reads it", k))
					}
				}
			}
		}
	}

	if len(problems) > 0 {
		return Result{
			Severity:    WARN,
			Detail:      strings.Join(problems, "; "),
			Remediation: "set steering.claude to loader in <state-root>/atomic.toml, or remove the conflict",
			ManualOnly:  true,
		}
	}
	detail := "steering.claude = " + string(mode)
	if info != "" {
		detail += "; " + info
	}
	return Result{Severity: PASS, Detail: detail}
}

// steeringPairDirs lists the directories that can hold a steering pair: the
// repository or realm root, plus docs/wiki and wiki when they have an AGENTS.md.
func steeringPairDirs(root string) []string {
	dirs := []string{root}
	for _, sub := range []string{filepath.Join("docs", "wiki"), "wiki"} {
		dir := filepath.Join(root, sub)
		if _, err := os.Stat(filepath.Join(dir, "AGENTS.md")); err == nil {
			dirs = append(dirs, dir)
		}
	}
	return dirs
}

func relTo(root, path string) string {
	if rel, err := filepath.Rel(root, path); err == nil {
		return filepath.ToSlash(rel)
	}
	return path
}

// versionBefore reports whether the dotted numbers in got sort before want.
func versionBefore(got []string, want [3]int) bool {
	for i, w := range want {
		n, err := strconv.Atoi(got[i])
		if err != nil || n == w {
			continue
		}
		return n < w
	}
	return false
}
