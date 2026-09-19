// OMP scenario support for the runtime proof runner. The generated Atomic
// extension is the artifact under proof, so the scenarios materialize an
// isolated OMP home carrying that module under the CP0-proven agent root and
// launch OMP against it. Nothing here asserts native behavior: the runner
// retains the observation, and the capability record stays the source of truth.
package runtimeproof

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/harness/omp"
)

// NoConfigEnv tells the runner that a harness resolves its configuration root
// from HOME and reads no configuration-root variable, which is what CP0 observed
// for OMP: the agent root is the probe's `omp config path` answer under the
// isolated home, not an environment variable.
const NoConfigEnv = "-"

// OMPLogName is the file the generated extension writes its observation log to
// beside the extension under the isolated agent root, which is the root the
// runner retains, so every scenario's records stay with its observation.
const OMPLogName = "atomic-runtime.jsonl"

// ompDefaultModel is the placeholder provider selection a launch uses when the
// caller names none. The isolated home carries no credential, so the model
// request is expected to fail; the selection exists only to get the harness
// through provider resolution to the events under proof. It is never a shipped
// artifact value.
const ompDefaultModel = "openai/gpt-5.2"

// ompPlaceholderKey is the API key a credential-free launch passes. It is not a
// credential and authorizes nothing: OMP rejects the request, which is what keeps
// the observation a startup observation.
const ompPlaceholderKey = "atomic-runtimeproof-placeholder"

// OMPCLI resolves the OMP executable: ATOMIC_RUNTIMEPROOF_OMP when set, else omp
// on PATH.
func OMPCLI() (string, error) {
	if override := strings.TrimSpace(os.Getenv("ATOMIC_RUNTIMEPROOF_OMP")); override != "" {
		return override, nil
	}
	found, err := exec.LookPath("omp")
	if err != nil {
		return "", fmt.Errorf("runtimeproof: locate omp: %w", err)
	}
	return found, nil
}

// OMPHome is an isolated OMP home prepared for one or more launches.
type OMPHome struct {
	// Home is the isolated OS home the launch runs with.
	Home string
	// AgentRoot is the CP0-observed agent root under that home. OMP discovers
	// AGENTS.md, RULES.md, rules/, and extensions/ beneath it.
	AgentRoot string
	// Module is the extension entry point OMP auto-discovers.
	Module string
	// Log receives this home's runtime records unless a launch names its own.
	Log string
}

// NewOMPHome materializes an isolated OMP home carrying module as the discovered
// extension. The home holds no credential, no session, and no provider
// selection beyond the placeholder a launch passes.
func NewOMPHome(home string, module []byte) (OMPHome, error) {
	if strings.TrimSpace(home) == "" {
		return OMPHome{}, fmt.Errorf("runtimeproof: OMP home requires a directory")
	}
	agentRoot := filepath.Join(home, ".omp", "agent")
	prepared := OMPHome{
		Home:      home,
		AgentRoot: agentRoot,
		Module:    filepath.Join(agentRoot, "extensions", "atomic.ts"),
		Log:       filepath.Join(agentRoot, OMPLogName),
	}
	if err := os.MkdirAll(filepath.Dir(prepared.Module), 0o755); err != nil {
		return OMPHome{}, fmt.Errorf("runtimeproof: create OMP agent root: %w", err)
	}
	if err := os.WriteFile(prepared.Module, module, 0o644); err != nil {
		return OMPHome{}, fmt.Errorf("runtimeproof: write OMP extension: %w", err)
	}
	return prepared, nil
}

// LogPath names a second log inside the prepared home's agent root, so a
// multi-launch scenario separates the records of its runs.
func (h OMPHome) LogPath(name string) string {
	return filepath.Join(h.AgentRoot, name)
}

// OMPLaunch is one OMP invocation against a prepared home.
type OMPLaunch struct {
	Name    string
	WorkDir string
	Prompt  string
	// Log receives this launch's runtime records. Empty uses the home's log, so a
	// multi-launch scenario can separate its runs.
	Log string
	// Session, when set, resumes that session.
	Session string
	// Save keeps the session, so a later launch can resume it. An ephemeral
	// launch saves nothing.
	Save bool
	// Args are extra harness arguments this launch exercises, inserted before the
	// prompt.
	Args []string
}

// Launch builds the bounded, non-interactive scenario for one OMP invocation.
// A model credential is used only when the CP0 probe convention supplies one;
// otherwise the placeholder stands in and the request is expected to fail after
// the events under proof.
func (h OMPHome) Launch(launch OMPLaunch) (Scenario, error) {
	log := launch.Log
	if log == "" {
		log = h.Log
	}
	model := ompModel()
	key := ompPlaceholderKey
	if token, ok := OMPModelToken(); ok {
		// The transient token belongs to the CP0 probe provider, so the model must
		// resolve to that provider instead of the placeholder selection.
		key = token
		model = ompCredentialModel()
	}
	args := []string{
		"-p", "--mode", "json", "--cwd", launch.WorkDir,
		"--auto-approve", "--max-time", "20",
		"--model", model, "--api-key", key,
	}
	switch {
	case launch.Session != "":
		args = append(args, "--resume", launch.Session)
	case !launch.Save:
		args = append(args, "--no-session")
	}
	args = append(args, launch.Args...)
	args = append(args, launch.Prompt)
	// The module appends to its log, so a launch starts from an empty one: a
	// scenario reads exactly the records its own run produced.
	hookLog := filepath.Join(h.Home, "runtimeproof-events.jsonl")
	if err := resetFile(log); err != nil {
		return Scenario{}, err
	}
	if err := resetFile(hookLog); err != nil {
		return Scenario{}, err
	}
	return Scenario{
		Name:      launch.Name,
		Harness:   ompExecutable(),
		Args:      args,
		Home:      h.Home,
		ConfigDir: h.AgentRoot,
		ConfigEnv: NoConfigEnv,
		WorkDir:   launch.WorkDir,
		EventLog:  hookLog,
		Timeout:   DefaultTimeout,
		Env: []string{
			"OMP_NO_UPDATE_CHECK=1",
			omp.RuntimeLogEnv + "=" + log,
		},
	}, nil
}

// OMPVersion reports the version of the resolved binary, so a proof records the
// version it observed rather than assuming the recorded capability row.
func OMPVersion() (string, error) {
	binary, err := OMPCLI()
	if err != nil {
		return "", err
	}
	cmd := exec.Command(binary, "--version")
	cmd.Env = append(os.Environ(), "OMP_NO_UPDATE_CHECK=1")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("runtimeproof: omp --version: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// resetFile truncates path, creating it, so a launch's records start empty.
func resetFile(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("runtimeproof: create log directory: %w", err)
	}
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		return fmt.Errorf("runtimeproof: reset log %s: %w", path, err)
	}
	return nil
}

// ompExecutable resolves the OMP binary for the scenario, falling back to the
// bare name so a missing binary surfaces as a launch error the caller can skip.
func ompExecutable() string {
	found, err := OMPCLI()
	if err != nil {
		return "omp"
	}
	return found
}

func ompModel() string {
	if override := strings.TrimSpace(os.Getenv("ATOMIC_RUNTIMEPROOF_OMP_MODEL")); override != "" {
		return override
	}
	return ompDefaultModel
}

// ompCredentialModel is the model a credentialed launch selects: the CP0 probe
// provider's model, so the transient token it obtained resolves.
func ompCredentialModel() string {
	if override := strings.TrimSpace(os.Getenv("CP0_OMP_MODEL")); override != "" {
		return override
	}
	return "openai-codex/gpt-5.6-sol"
}

// OMPRuntimeLog parses the runtime log a launch retained. The scenarios write the
// log beside the home's own log file unless they name another, so a caller reads
// it from the observation's retained files by name.
func OMPRuntimeLog(obs Observation, name string) (omp.RuntimeLog, error) {
	data, ok := obs.Files[name]
	if !ok {
		return omp.RuntimeLog{Path: name}, nil
	}
	return omp.ParseRuntimeLog(name, []byte(data))
}

// Proof builds the runtime proof record for one observed launch: the parsed
// runtime log and the record kinds the run retained. It is what a caller retains
// as the last runtime proof and hands to the read-only runtime status.
func (h OMPHome) Proof(launch OMPLaunch, version string, obs Observation) (omp.RuntimeProof, error) {
	log := launch.Log
	if log == "" {
		log = h.Log
	}
	return OMPProof(launch.Name, version, obs, filepath.Base(log))
}

// OMPProof builds the runtime proof record for one observed scenario run.
func OMPProof(name, version string, obs Observation, logName string) (omp.RuntimeProof, error) {
	log, err := OMPRuntimeLog(obs, logName)
	if err != nil {
		return omp.RuntimeProof{}, err
	}
	events := make([]string, 0, len(log.Records))
	for _, record := range log.Records {
		events = append(events, record.Kind)
	}
	return omp.RuntimeProof{
		Scenario: name,
		Harness:  "omp",
		Version:  version,
		Observed: time.Now().UTC().Format("2006-01-02"),
		Events:   events,
		Log:      &log,
	}, nil
}

// SessionID reads the session identifier OMP reports before any model request,
// so a scenario can resume the session it created without a provider answer.
func SessionID(stdout string) (string, error) {
	for _, line := range strings.Split(stdout, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "{") {
			continue
		}
		var event struct {
			Type string `json:"type"`
			ID   string `json:"id"`
		}
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			continue
		}
		if event.Type == "session" && event.ID != "" {
			return event.ID, nil
		}
	}
	return "", fmt.Errorf("runtimeproof: no session event in the OMP output")
}

// OMPModelToken returns a provider credential through the CP0 probe convention:
// CP0_OMP_API_KEY when set, else a transient token resolved from
// CP0_OMP_AUTH_HOME. It returns ok=false when neither is configured, so a caller
// skips a credentialed scenario rather than launching with a placeholder.
func OMPModelToken() (string, bool) {
	if key := strings.TrimSpace(os.Getenv("CP0_OMP_API_KEY")); key != "" {
		return key, true
	}
	authHome := strings.TrimSpace(os.Getenv("CP0_OMP_AUTH_HOME"))
	if authHome == "" {
		return "", false
	}
	provider := strings.TrimSpace(os.Getenv("CP0_OMP_AUTH_PROVIDER"))
	if provider == "" {
		provider = "openai-codex"
	}
	binary, err := OMPCLI()
	if err != nil {
		return "", false
	}
	cmd := exec.Command(binary, "token", provider)
	cmd.Env = append(os.Environ(), "HOME="+authHome, "OMP_NO_UPDATE_CHECK=1")
	out, err := cmd.Output()
	if err != nil {
		return "", false
	}
	token := strings.TrimSpace(string(out))
	return token, token != ""
}
