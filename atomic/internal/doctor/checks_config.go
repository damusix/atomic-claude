package doctor

import (
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/harness"
	"github.com/damusix/atomic-claude/atomic/internal/installstate"
	"github.com/damusix/atomic-claude/atomic/internal/selfupdate"
)

// checkConfig implements category 9: config integrity.
func checkConfig(opts Opts) Result {
	home, err := resolveHome()
	if err != nil {
		return Result{Severity: WARN, Detail: fmt.Sprintf("resolve home dir: %v", err)}
	}
	return RunCheckConfigWith(home)
}

// RunCheckConfigWith runs the config check against an explicit home dir.
// Exported for testing. It merges config.toml validity with a chronic
// background update-check failure; the chronic finding never escalates past
// WARN, since a failing background check is not a config validity problem.
func RunCheckConfigWith(home string) Result {
	base := checkConfigValidity(home)
	chronicDetail := checkChronicUpdateFailure(home)
	if chronicDetail == "" {
		return base
	}
	switch base.Severity {
	case PASS:
		return Result{Severity: WARN, Detail: chronicDetail}
	default:
		return Result{Severity: base.Severity, Detail: base.Detail + "; " + chronicDetail}
	}
}

// checkChronicUpdateFailure reports a detail when the background update
// check's last_result is non-empty, which is how it records a failure.
// A missing state file loads as the zero value and so reports "".
func checkChronicUpdateFailure(home string) string {
	state := selfupdate.LoadState(config.StatePath(home))
	if state.Update.LastResult == "" {
		return ""
	}
	if state.Update.LastCheck.IsZero() {
		return fmt.Sprintf("background update check failing: %s", state.Update.LastResult)
	}
	return fmt.Sprintf(
		"background update check failing since %s: %s",
		state.Update.LastCheck.Format(time.RFC3339),
		state.Update.LastResult,
	)
}

// checkConfigValidity implements the config.toml validity half of category 9.
func checkConfigValidity(home string) Result {
	tomlPath := config.TOMLPath(home)

	if _, err := os.Stat(tomlPath); os.IsNotExist(err) {
		return Result{Severity: PASS, Detail: "config.toml not present (using built-in defaults)"}
	}

	cfg, warns, err := config.Load(tomlPath)
	if err != nil {
		return Result{Severity: FAIL, Detail: fmt.Sprintf("config parse error: %v", err)}
	}

	// An unknown key only WARNs; an invalid value FAILs.
	if err := config.Validate(cfg); err != nil {
		return Result{Severity: FAIL, Detail: err.Error()}
	}

	details := inertOverrides(home, cfg)
	details = append(details, agentWarningDetails(config.AgentWarnings(cfg))...)
	if len(warns) > 0 {
		details = append(details, joinWarnings(warns)+" — run `atomic config unset <key>` to remove")
	}

	if len(details) > 0 {
		return Result{Severity: WARN, Detail: strings.Join(details, "; ")}
	}
	return Result{Severity: PASS, Detail: "config.toml ok"}
}

// agentWarningDetails groups agent warnings per harness, each group pointing at
// that harness's form; a warning naming no known harness keeps the bare hint.
func agentWarningDetails(warns []config.Warning) []string {
	byHarness := map[string][]config.Warning{}
	for _, w := range warns {
		kind := w.Harness
		if !slices.Contains(config.AgentHarnesses, kind) {
			kind = ""
		}
		byHarness[kind] = append(byHarness[kind], w)
	}
	var details []string
	for _, kind := range config.AgentHarnesses {
		if group := byHarness[kind]; len(group) > 0 {
			details = append(details, joinWarnings(group)+" — run `atomic config agents --harness "+kind+"` to edit them")
		}
	}
	if group := byHarness[""]; len(group) > 0 {
		details = append(details, joinWarnings(group)+" — run `atomic config agents` to edit them")
	}
	return details
}

func joinWarnings(warns []config.Warning) string {
	msgs := make([]string, 0, len(warns))
	for _, w := range warns {
		msgs = append(msgs, w.Message)
	}
	return strings.Join(msgs, "; ")
}

// claudeMaskingEnv are the Claude Code variables that mask an agent file's
// model or effort.
var claudeMaskingEnv = []struct{ name, effect string }{
	{"CLAUDE_CODE_EFFORT_LEVEL", "outranks the stored claude.agents effort"},
	{"CLAUDE_CODE_SUBAGENT_MODEL", "sets the model wherever claude.agents leaves it empty"},
}

// probeGatedAgents are the harnesses whose agent delivery waits on a capability
// row. Claude's agents directory is the delivery path Atomic has always shipped.
var probeGatedAgents = map[string]func() harness.CapabilityMatrix{
	"omp":   harness.OMPCapabilities,
	"codex": harness.CodexCapabilities,
}

// inertOverrides names stored overrides that would not take effect. An unadopted legacy
// Claude install still applies [claude.agents]; an unreadable ledger is left to other categories.
func inertOverrides(home string, cfg *config.Config) []string {
	var out []string
	ledger, err := installstate.LoadLedger(config.LedgerPath(home))
	if err == nil {
		enrolled := map[string]bool{}
		for _, t := range ledger.Targets {
			enrolled[t.Harness] = true
		}
		if cfg.Install.Version != "" && !cfg.Install.Adopted {
			enrolled["claude"] = true
		}
		for _, kind := range config.AgentHarnesses {
			if len(cfg.AgentOverrides(kind)) == 0 {
				continue
			}
			switch {
			case !enrolled[kind]:
				out = append(out, fmt.Sprintf("%s.agents overrides are stored but %s is not enrolled", kind, kind))
			case probeGatedAgents[kind] != nil:
				if _, open := probeGatedAgents[kind]().Gap(harness.RoleAgentDelivery); open {
					out = append(out, fmt.Sprintf("%s.agents overrides are stored but %s agent delivery is unsupported until a runtime probe proves it", kind, kind))
				}
			}
		}
	}
	if len(cfg.AgentOverrides("claude")) > 0 {
		for _, v := range claudeMaskingEnv {
			if os.Getenv(v.name) != "" {
				out = append(out, fmt.Sprintf("%s is set and %s", v.name, v.effect))
			}
		}
	}
	return out
}
