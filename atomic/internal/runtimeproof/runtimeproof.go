// Package runtimeproof launches a coding harness against an isolated home and
// collects the startup evidence a capability claim needs: process outcome, hook
// events, and native file bytes. It is the reusable seam CP8A drives the full
// scenario set through; it owns no scenario of its own and asserts no native
// behavior.
//
// A scenario is bounded and non-interactive, and it never requires a model
// response. The isolated configuration root carries no credentials, so the
// launch reaches startup and cannot issue a model request; the runner returns
// the startup evidence regardless and reports the process outcome as evidence
// instead of treating it as the point. CP0's Claude probe is the reference: an
// isolated home reached SessionStart and InstructionsLoaded, then ended with
// "Not logged in".
package runtimeproof

import (
	"bytes"
	"context"

	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// DefaultTimeout bounds a scenario that names none. It is generous enough for a
// cold harness start and small enough that a hung launch cannot stall a run.
const DefaultTimeout = 90 * time.Second

// DefaultPrompt is the print-mode prompt a scenario that names none uses. The
// isolated home cannot answer it, so the prompt only drives startup.
const DefaultPrompt = "Reply OK."

// MaxObservedFile is the largest file the runner retains whole. A larger file is
// recorded as truncated rather than read into an observation.
const MaxObservedFile = 1 << 20

// defaultConfigEnv is the configuration-root variable CP0 observed Claude using.
const defaultConfigEnv = "CLAUDE_CONFIG_DIR"

// ErrTimeout reports a scenario that exceeded its bound. The observation is
// still returned: the startup evidence collected before the bound is real, and
// the timeout is what the caller must decide about.
var ErrTimeout = errors.New("runtimeproof: scenario exceeded its bound")

// Scenario is one bounded, non-interactive launch against an isolated home.
// Home, ConfigDir, WorkDir, and EventLog are required; the runner creates no
// state outside them.
type Scenario struct {
	// Name identifies the scenario in its observation.
	Name string
	// Harness is the executable to launch. Empty selects ClaudeCLI.
	Harness string
	// Args overrides the launch arguments. Nil selects print mode:
	// -p --output-format json <Prompt>.
	Args []string
	// Home is the isolated OS home for the launch.
	Home string
	// ConfigDir is the isolated native configuration root.
	ConfigDir string
	// ConfigEnv names the environment variable that points at ConfigDir. Empty
	// selects CLAUDE_CONFIG_DIR.
	ConfigEnv string
	// WorkDir is the launch working directory, isolated like Home.
	WorkDir string
	// EventLog receives raw hook payloads, one JSON object per line.
	EventLog string
	// Prompt is the print-mode prompt. Empty selects DefaultPrompt.
	Prompt string
	// Timeout bounds the launch. Zero selects DefaultTimeout.
	Timeout time.Duration
	// Env holds extra KEY=VALUE entries for the launch, applied last.
	Env []string
	// Template is a directory tree materialized into Home before launch.
	Template string
	// WorkTemplate is a directory tree materialized into WorkDir before launch.
	WorkTemplate string
	// Prepare installs native configuration into ConfigDir before launch. Empty
	// installs the Claude startup observer.
	Prepare func(configDir string) error
}

// Event is one hook payload observed during a launch, with the fields the
// capability matrix reports on. Raw keeps the whole payload.
type Event struct {
	Name       string `json:"name,omitempty"`
	Source     string `json:"source,omitempty"`
	SessionID  string `json:"session_id,omitempty"`
	Cwd        string `json:"cwd,omitempty"`
	FilePath   string `json:"file_path,omitempty"`
	MemoryType string `json:"memory_type,omitempty"`
	LoadReason string `json:"load_reason,omitempty"`
	ParentFile string `json:"parent_file_path,omitempty"`
	ToolName   string `json:"tool_name,omitempty"`
	Raw        string `json:"raw,omitempty"`
}

// Observation is the evidence one launch retained. Files and WorkFiles map each
// file's root-relative slash path to its bytes; Truncated names the files too
// large to retain.
type Observation struct {
	Scenario  string            `json:"scenario"`
	Harness   string            `json:"harness"`
	ExitCode  int               `json:"exit_code"`
	TimedOut  bool              `json:"timed_out,omitempty"`
	Duration  time.Duration     `json:"duration"`
	Stdout    string            `json:"stdout,omitempty"`
	Stderr    string            `json:"stderr,omitempty"`
	Events    []Event           `json:"events,omitempty"`
	Files     map[string]string `json:"files,omitempty"`
	WorkFiles map[string]string `json:"work_files,omitempty"`
	Truncated []string          `json:"truncated,omitempty"`
}

// ClaudeCLI resolves the Claude executable: ATOMIC_RUNTIMEPROOF_CLAUDE when set,
// else claude on PATH.
func ClaudeCLI() (string, error) {
	if override := strings.TrimSpace(os.Getenv("ATOMIC_RUNTIMEPROOF_CLAUDE")); override != "" {
		return override, nil
	}
	found, err := exec.LookPath("claude")
	if err != nil {
		return "", fmt.Errorf("runtimeproof: locate claude: %w", err)
	}
	return found, nil
}

// ClaudeStartupObserver returns a Prepare function that installs the startup
// observation hooks into an isolated Claude configuration root. SessionStart and
// InstructionsLoaded each append their raw payload to eventLog and answer
// nothing, so a launch that reaches startup yields discovery evidence without
// any model response. An existing settings.json is extended, never replaced.
func ClaudeStartupObserver(eventLog string) func(configDir string) error {
	return func(configDir string) error {
		hook := filepath.Join(configDir, ".runtimeproof-hook.sh")
		script := "#!/bin/sh\n# Observe-only runtimeproof hook: record the payload, answer nothing.\ncat >> \"$RUNTIMEPROOF_EVENT_LOG\"\n"
		if err := os.WriteFile(hook, []byte(script), 0o755); err != nil {
			return fmt.Errorf("runtimeproof: write hook: %w", err)
		}

		settingsPath := filepath.Join(configDir, "settings.json")
		settings := map[string]any{}
		data, err := os.ReadFile(settingsPath)
		switch {
		case err == nil && len(bytes.TrimSpace(data)) > 0:
			if err := json.Unmarshal(data, &settings); err != nil {
				return fmt.Errorf("runtimeproof: parse %s: %w", settingsPath, err)
			}
		case err != nil && !errors.Is(err, fs.ErrNotExist):
			return fmt.Errorf("runtimeproof: read %s: %w", settingsPath, err)
		}

		hooks, _ := settings["hooks"].(map[string]any)
		if hooks == nil {
			hooks = map[string]any{}
			settings["hooks"] = hooks
		}
		command := "/bin/sh " + hook
		for _, name := range []string{"SessionStart", "InstructionsLoaded"} {
			hooks[name] = appendObserverHook(hooks[name], command)
		}

		out, err := json.MarshalIndent(settings, "", "  ")
		if err != nil {
			return fmt.Errorf("runtimeproof: encode %s: %w", settingsPath, err)
		}
		return os.WriteFile(settingsPath, append(out, '\n'), 0o644)
	}
}

// appendObserverHook adds command to a hook event's matcher groups when no group
// already invokes it, so re-running the observer never duplicates hooks.
func appendObserverHook(existing any, command string) []any {
	groups, _ := existing.([]any)
	for _, g := range groups {
		group, ok := g.(map[string]any)
		if !ok {
			continue
		}
		inner, _ := group["hooks"].([]any)
		for _, h := range inner {
			entry, ok := h.(map[string]any)
			if ok && entry["command"] == command {
				return groups
			}
		}
	}
	group := map[string]any{
		"hooks": []any{map[string]any{"type": "command", "command": command}},
	}
	return append(groups, group)
}

// Run launches the scenario and returns its observation. A launch that never
// started, or a scenario that exceeded its bound, returns an error alongside the
// evidence collected so far; a nonzero exit status is evidence, not an error.
func Run(ctx context.Context, s Scenario) (Observation, error) {
	if err := s.validate(); err != nil {
		return Observation{}, err
	}
	timeout := s.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	harness := s.Harness
	if harness == "" {
		var err error
		harness, err = ClaudeCLI()
		if err != nil {
			return Observation{}, err
		}
	}
	args := s.Args
	if args == nil {
		prompt := s.Prompt
		if prompt == "" {
			prompt = DefaultPrompt
		}
		args = []string{"-p", "--output-format", "json", prompt}
	}

	if err := os.MkdirAll(s.ConfigDir, 0o755); err != nil {
		return Observation{}, fmt.Errorf("runtimeproof: create config root: %w", err)
	}
	if err := os.MkdirAll(s.WorkDir, 0o755); err != nil {
		return Observation{}, fmt.Errorf("runtimeproof: create work dir: %w", err)
	}
	if err := os.MkdirAll(s.Home, 0o755); err != nil {
		return Observation{}, fmt.Errorf("runtimeproof: create home: %w", err)
	}
	if s.Template != "" {
		if err := materialize(s.Home, s.Template); err != nil {
			return Observation{}, err
		}
	}
	if s.WorkTemplate != "" {
		if err := materialize(s.WorkDir, s.WorkTemplate); err != nil {
			return Observation{}, err
		}
	}
	if err := os.MkdirAll(filepath.Dir(s.EventLog), 0o755); err != nil {
		return Observation{}, fmt.Errorf("runtimeproof: create event log dir: %w", err)
	}
	if err := os.WriteFile(s.EventLog, nil, 0o644); err != nil {
		return Observation{}, fmt.Errorf("runtimeproof: reset event log: %w", err)
	}
	prepare := s.Prepare
	if prepare == nil {
		prepare = ClaudeStartupObserver(s.EventLog)
	}
	if err := prepare(s.ConfigDir); err != nil {
		return Observation{}, err
	}

	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(runCtx, harness, args...)
	cmd.Dir = s.WorkDir
	cmd.Env = launchEnv(s)
	cmd.Stdin = nil

	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr

	start := time.Now()
	runErr := cmd.Run()
	elapsed := time.Since(start)

	obs := Observation{
		Scenario: s.Name,
		Harness:  harness,
		ExitCode: -1,
		Duration: elapsed,
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
	}
	var err error
	if obs.Events, err = readEvents(s.EventLog); err != nil {
		return obs, err
	}
	configTruncated := []string(nil)
	if obs.Files, configTruncated, err = readTreeBounded(s.ConfigDir); err != nil {
		return obs, err
	}
	var workTruncated []string
	if obs.WorkFiles, workTruncated, err = readTreeBounded(s.WorkDir); err != nil {
		return obs, err
	}
	obs.Truncated = append(configTruncated, workTruncated...)

	if runCtx.Err() == context.DeadlineExceeded {
		obs.TimedOut = true
		return obs, fmt.Errorf("%w: %s ended after %s", ErrTimeout, s.Name, timeout)
	}
	var exitErr *exec.ExitError
	switch {
	case errors.As(runErr, &exitErr):
		obs.ExitCode = exitErr.ExitCode()
	case runErr != nil:
		return obs, fmt.Errorf("runtimeproof: launch %s: %w", harness, runErr)
	default:
		obs.ExitCode = 0
	}
	return obs, nil
}

// validate rejects a scenario that cannot be launched in isolation.
func (s Scenario) validate() error {
	for name, value := range map[string]string{
		"Home":      s.Home,
		"ConfigDir": s.ConfigDir,
		"WorkDir":   s.WorkDir,
		"EventLog":  s.EventLog,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("runtimeproof: scenario %q requires %s", s.Name, name)
		}
	}
	return nil
}

// launchEnv returns the launch environment: the process environment minus every
// variable that could carry a model credential or point at another native root,
// plus the isolation variables the scenario requires. Stripping credentials is
// what makes the observation a startup observation.
func launchEnv(s Scenario) []string {
	configEnv := s.ConfigEnv
	if configEnv == "" {
		configEnv = defaultConfigEnv
	}
	env := make([]string, 0, len(os.Environ())+len(s.Env)+4)
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if key == configEnv || key == "HOME" || key == "RUNTIMEPROOF_EVENT_LOG" {
			continue
		}
		if strings.HasPrefix(key, "ANTHROPIC_") || strings.HasPrefix(key, "CLAUDE_") {
			continue
		}
		env = append(env, entry)
	}
	env = append(env,
		"HOME="+s.Home,
		configEnv+"="+s.ConfigDir,
		"RUNTIMEPROOF_EVENT_LOG="+s.EventLog,
		"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1",
	)
	return append(env, s.Env...)
}

// readEvents parses the event log, if any. A missing log means no hook ran.
func readEvents(path string) ([]Event, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("runtimeproof: read event log: %w", err)
	}
	var events []Event
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(line), &payload); err != nil {
			return nil, fmt.Errorf("runtimeproof: parse event line: %w", err)
		}
		events = append(events, Event{
			Name:       stringField(payload, "hook_event_name"),
			Source:     stringField(payload, "source"),
			SessionID:  stringField(payload, "session_id"),
			Cwd:        stringField(payload, "cwd"),
			FilePath:   stringField(payload, "file_path"),
			MemoryType: stringField(payload, "memory_type"),
			LoadReason: stringField(payload, "load_reason"),
			ParentFile: stringField(payload, "parent_file_path"),
			ToolName:   stringField(payload, "tool_name"),
			Raw:        line,
		})
	}
	return events, nil
}

func stringField(payload map[string]any, key string) string {
	value, _ := payload[key].(string)
	return value
}

// readTreeBounded maps every file under root to its bytes, keyed by
// root-relative slash path. A file larger than MaxObservedFile is named in the
// truncated list instead of read.
func readTreeBounded(root string) (map[string]string, []string, error) {
	files := map[string]string{}
	var truncated []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.Size() > MaxObservedFile {
			truncated = append(truncated, filepath.ToSlash(rel))
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(rel)] = string(data)
		return nil
	})
	if err != nil {
		return nil, nil, fmt.Errorf("runtimeproof: walk %s: %w", root, err)
	}
	return files, truncated, nil
}

// materialize copies a template tree into dst, preserving the relative layout.
func materialize(dst, template string) error {
	err := filepath.WalkDir(template, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(template, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, info.Mode().Perm())
	})
	if err != nil {
		return fmt.Errorf("runtimeproof: materialize %s: %w", template, err)
	}
	return nil
}
