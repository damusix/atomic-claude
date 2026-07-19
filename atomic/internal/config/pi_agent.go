package config

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

type PiAgentOverride struct {
	Model    string `json:"model,omitempty"`
	Thinking string `json:"thinking,omitempty"`
}

type PiAgentDiagnostic struct {
	Code     string `json:"code"`
	Severity string `json:"severity"`
	Source   string `json:"source"`
	Path     string `json:"path"`
	Message  string `json:"message"`
	Agent    string `json:"agent,omitempty"`
}

type PiAgentResolveEnvelope struct {
	SchemaVersion int                        `json:"schema_version"`
	Valid         bool                       `json:"valid"`
	Agents        map[string]PiAgentOverride `json:"agents"`
	Diagnostics   []PiAgentDiagnostic        `json:"diagnostics"`
}

type piAgentSource struct {
	source  string
	path    string
	agents  map[string]PiAgentOverride
	diags   []PiAgentDiagnostic
	invalid map[string]bool
}

var agentNamePattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,62}[a-z0-9])?$`)

var validThinking = map[string]bool{
	"off": true, "minimal": true, "low": true, "medium": true,
	"high": true, "xhigh": true, "max": true,
}

func ResolvePiAgents(globalPath, repoPath string) PiAgentResolveEnvelope {
	env := PiAgentResolveEnvelope{
		SchemaVersion: 1,
		Valid:         true,
		Agents:        map[string]PiAgentOverride{},
		Diagnostics:   []PiAgentDiagnostic{},
	}
	global, globalOK := loadPiAgentSource("global", globalPath)
	repo, repoOK := loadPiAgentSource("repository", repoPath)
	env.Diagnostics = append(env.Diagnostics, global.diags...)
	env.Diagnostics = append(env.Diagnostics, repo.diags...)
	sortDiagnostics(env.Diagnostics)
	if !globalOK || !repoOK {
		env.Valid = false
		return env
	}
	for _, name := range sortedAgentNames(global.agents) {
		env.Agents[name] = global.agents[name]
	}
	for name := range repo.invalid {
		delete(env.Agents, name)
	}
	for _, name := range sortedAgentNames(repo.agents) {
		local := repo.agents[name]
		merged := env.Agents[name]
		if local.Model != "" {
			merged.Model = local.Model
		}
		if local.Thinking != "" {
			merged.Thinking = local.Thinking
		}
		env.Agents[name] = merged
	}
	return env
}

func loadPiAgentSource(source, path string) (piAgentSource, bool) {
	result := piAgentSource{source: source, path: path, agents: map[string]PiAgentOverride{}, invalid: map[string]bool{}}
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return result, true
		}
		result.diags = append(result.diags, PiAgentDiagnostic{Code: "config-source-unreadable", Severity: "error", Source: source, Path: path, Message: err.Error()})
		return result, false
	}
	var rawMap map[string]any
	if err := toml.Unmarshal(raw, &rawMap); err != nil {
		result.diags = append(result.diags, PiAgentDiagnostic{Code: "config-source-invalid", Severity: "error", Source: source, Path: path, Message: err.Error()})
		return result, false
	}
	piRaw, exists := rawMap["pi"]
	if !exists {
		return result, true
	}
	piTable, ok := piRaw.(map[string]any)
	if !ok {
		result.diags = append(result.diags, PiAgentDiagnostic{Code: "config-source-invalid", Severity: "error", Source: source, Path: path, Message: "pi must be a table"})
		return result, false
	}
	agentRaw, exists := piTable["agent"]
	if !exists {
		return result, true
	}
	agentTable, ok := agentRaw.(map[string]any)
	if !ok {
		result.diags = append(result.diags, PiAgentDiagnostic{Code: "config-source-invalid", Severity: "error", Source: source, Path: path, Message: "pi.agent must be a table"})
		return result, false
	}
	for _, name := range sortedAnyKeys(agentTable) {
		rawAgent := agentTable[name]
		table, ok := rawAgent.(map[string]any)
		if !ok {
			result.invalid[name] = true
			diagnosticAgent := name
			if !agentNamePattern.MatchString(name) {
				diagnosticAgent = ""
			}
			result.diags = append(result.diags, diag(source, path, diagnosticAgent, "config-agent-invalid", "agent entry must be a table"))
			continue
		}
		agent, diags := validatePiAgent(source, path, name, table)
		if len(diags) > 0 {
			result.invalid[name] = true
			result.diags = append(result.diags, diags...)
			continue
		}
		if agent.Model == "" && agent.Thinking == "" {
			continue
		}
		result.agents[name] = agent
	}
	return result, true
}

func validatePiAgent(source, path, name string, table map[string]any) (PiAgentOverride, []PiAgentDiagnostic) {
	var out PiAgentOverride
	var diags []PiAgentDiagnostic
	if !agentNamePattern.MatchString(name) {
		return out, []PiAgentDiagnostic{
			diag(source, path, "", "config-agent-invalid", "agent name must match ^[a-z0-9](?:[a-z0-9-]{0,62}[a-z0-9])?$"),
		}
	}
	for _, key := range sortedAnyKeys(table) {
		value := table[key]
		s, isString := value.(string)
		switch key {
		case "model":
			if !isString || !validModelString(s) {
				diags = append(diags, diag(source, path, name, "config-agent-invalid", "model must be a non-empty exact provider/model string"))
			} else {
				out.Model = s
			}
		case "thinking":
			if !isString || !validThinking[s] {
				diags = append(diags, diag(source, path, name, "config-agent-invalid", "thinking must be one of: off, minimal, low, medium, high, xhigh, max"))
			} else {
				out.Thinking = s
			}
		default:
			diags = append(diags, diag(source, path, name, "config-agent-invalid", fmt.Sprintf("unknown pi.agent field %q", key)))
		}
	}
	return out, diags
}

func validModelString(s string) bool {
	if strings.TrimSpace(s) != s || s == "" || strings.ContainsAny(s, " \t\n\r*?[]") {
		return false
	}
	parts := strings.SplitN(s, "/", 2)
	return len(parts) == 2 && parts[0] != "" && parts[1] != ""
}

func diag(source, path, agent, code, message string) PiAgentDiagnostic {
	return PiAgentDiagnostic{Code: code, Severity: "error", Source: source, Path: path, Agent: agent, Message: message}
}

func sortedAgentNames(m map[string]PiAgentOverride) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sortedAnyKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sortDiagnostics(diags []PiAgentDiagnostic) {
	sort.SliceStable(diags, func(i, j int) bool {
		a, b := diags[i], diags[j]
		for _, pair := range [][2]string{{a.Source, b.Source}, {a.Path, b.Path}, {a.Agent, b.Agent}, {a.Code, b.Code}, {a.Message, b.Message}} {
			if pair[0] != pair[1] {
				return pair[0] < pair[1]
			}
		}
		return false
	})
}
