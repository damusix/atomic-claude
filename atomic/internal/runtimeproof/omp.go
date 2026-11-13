// OMP scenarios enroll an isolated home through the production adapter and never
// hand-write the module, so each observation is evidence for the shipped layout.
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

// NoConfigEnv marks a harness that resolves its configuration root from HOME, as
// OMP does: its agent root is `omp config path` under the isolated home.
const NoConfigEnv = "-"

// OMPLogName is the extension's observation log, written under the agent root
// the runner retains.
const OMPLogName = "atomic-runtime.jsonl"

// ompDefaultModel is a placeholder that only gets OMP through provider resolution
// to the events under proof; the request then fails for lack of a credential.
const ompDefaultModel = "openai/gpt-5.2"

// ompPlaceholderKey authorizes nothing: OMP rejects the request, which keeps the
// observation a startup observation.
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
	// AgentRoot is the profile's agent root. OMP discovers AGENTS.md, RULES.md,
	// rules/, and extensions/ beneath it.
	AgentRoot string
	// Profile is the enrolled profile's name, empty for the default profile. A
	// named profile is launched with the documented selector set.
	Profile string
	// Module is the extension entry point OMP auto-discovers, as enrollment
	// published it.
	Module string
	// Log receives this home's runtime records unless a launch names its own.
	Log string
}

// OMPHomeRequest is the enrollment one isolated home is prepared with.
type OMPHomeRequest struct {
	// Adapter enrolls the profile. Nil uses the shipped adapter over the
	// embedded corpus, which is the layout an install produces.
	Adapter *omp.Adapter
	// Profile is the profile to enroll. An empty Root is resolved by the same
	// path query production uses.
	Profile omp.Profile
	// Deny carries the exact machine predicates the delivered module blocks.
	Deny []omp.DenyPredicate
}

// NewOMPHome materializes an isolated OMP home by enrolling one profile through
// the production adapter, and returns what that enrollment reported.
func NewOMPHome(home string, req OMPHomeRequest) (OMPHome, omp.EnrollResult, error) {
	var result omp.EnrollResult
	if strings.TrimSpace(home) == "" {
		return OMPHome{}, result, fmt.Errorf("runtimeproof: OMP home requires a directory")
	}
	adapter := req.Adapter
	if adapter == nil {
		adapter = omp.New()
	}
	profile := req.Profile
	if profile.Root == "" {
		root, err := omp.DefaultConfigPath(home, profile.Name)
		if err != nil {
			return OMPHome{}, result, fmt.Errorf("runtimeproof: resolve OMP profile root: %w", err)
		}
		profile.Root = root
	}
	if err := os.MkdirAll(profile.Root, 0o755); err != nil {
		return OMPHome{}, result, fmt.Errorf("runtimeproof: create OMP agent root: %w", err)
	}

	result, err := adapter.Enroll(omp.EnrollRequest{
		Home:        home,
		Profile:     profile,
		Deny:        req.Deny,
		OperationID: "runtimeproof-omp",
	})
	if err != nil {
		return OMPHome{}, result, fmt.Errorf("runtimeproof: enroll OMP profile %q: %w", profile.Name, err)
	}
	if result.ExtensionPath == "" {
		return OMPHome{}, result, fmt.Errorf("runtimeproof: enrollment reported no extension module for profile %q", profile.Name)
	}
	prepared := OMPHome{
		Home:      home,
		AgentRoot: profile.Root,
		Profile:   profile.Name,
		Module:    result.ExtensionPath,
		Log:       filepath.Join(profile.Root, OMPLogName),
	}
	if _, err := os.Stat(prepared.Module); err != nil {
		return OMPHome{}, result, fmt.Errorf("runtimeproof: the enrolled module %s is not readable: %w", prepared.Module, err)
	}
	return prepared, result, nil
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

// Launch builds the bounded, non-interactive scenario for one OMP invocation,
// with the placeholder model unless OMPModelToken supplies a credential.
func (h OMPHome) Launch(launch OMPLaunch) (Scenario, error) {
	log := launch.Log
	if log == "" {
		log = h.Log
	}
	model := ompModel()
	key := ompPlaceholderKey
	if token, ok := OMPModelToken(); ok {
		// The transient token belongs to the probe provider, so the model must
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
	stubDir, err := noopAtomic(h.Home)
	if err != nil {
		return Scenario{}, err
	}
	env := []string{
		"OMP_NO_UPDATE_CHECK=1",
		omp.RuntimeLogEnv + "=" + log,
		"PATH=" + stubDir + string(os.PathListSeparator) + os.Getenv("PATH"),
	}
	if h.Profile != "" {
		// The documented selector must reach the launched process, or OMP would
		// load the default profile while this home enrolled a named one.
		env = append(env, omp.ProfileEnv+"="+h.Profile)
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
		Env:       env,
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

// noopAtomic shadows the installed binary the extension's session-start hook
// runs, which writes under HOME after omp exits and races temp-dir cleanup.
func noopAtomic(home string) (string, error) {
	dir := filepath.Join(home, "runtimeproof-bin")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("runtimeproof: create stub directory: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "atomic"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		return "", fmt.Errorf("runtimeproof: write atomic stub: %w", err)
	}
	return dir, nil
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

// ompCredentialModel is the model a credentialed launch selects: the probe
// provider's model, so the transient token it obtained resolves.
func ompCredentialModel() string {
	if override := strings.TrimSpace(os.Getenv("CP0_OMP_MODEL")); override != "" {
		return override
	}
	return "openai-codex/gpt-5.6-sol"
}

// OMPRuntimeLog parses the runtime log a launch retained, read from the
// observation's retained files by name.
func OMPRuntimeLog(obs Observation, name string) (omp.RuntimeLog, error) {
	data, ok := obs.Files[name]
	if !ok {
		return omp.RuntimeLog{Path: name}, nil
	}
	return omp.ParseRuntimeLog(name, []byte(data))
}

// Proof builds the runtime proof record for one observed launch, which the
// read-only runtime status reads.
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

// OMPModelToken returns CP0_OMP_API_KEY, else a transient token from
// CP0_OMP_AUTH_HOME, else ok=false so the caller skips a credentialed scenario.
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
