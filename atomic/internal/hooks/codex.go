package hooks

import (
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/tailscale/hujson"

	"github.com/damusix/atomic-claude/atomic/internal/managedfile"
)

const (
	// CodexSessionStartCommand is the command of the SessionStart member Atomic
	// owns in $CODEX_HOME/hooks.json.
	CodexSessionStartCommand = "atomic hooks session-start --harness codex"
	// CodexSessionStartMatcher leaves out the clear, compact, and fork sources
	// listed at https://developers.openai.com/codex/hooks.
	CodexSessionStartMatcher = "startup|resume"
	// CodexPreToolUseCommand is the command of the PreToolUse member Atomic owns,
	// which delivers the wiki cards matching an edit's paths.
	CodexPreToolUseCommand = "atomic hooks pre-tool-use"
	// CodexPreToolUseMatcher is Codex's tool name for an edit; Write and Edit are
	// its matcher aliases for the same tool.
	CodexPreToolUseMatcher = "apply_patch"

	codexHooksFileName = "hooks.json"
)

type codexMember struct{ event, matcher, command string }

var (
	codexSessionStart = codexMember{"SessionStart", CodexSessionStartMatcher, CodexSessionStartCommand}
	codexPreToolUse   = codexMember{"PreToolUse", CodexPreToolUseMatcher, CodexPreToolUseCommand}
	codexMembers      = []codexMember{codexSessionStart, codexPreToolUse}
)

// CodexHooksPath is the user-layer hooks file of a CODEX_HOME root.
func CodexHooksPath(root string) string {
	return filepath.Join(root, codexHooksFileName)
}

func (m codexMember) register(ast *hujson.Value) error {
	return astRegisterEvent(ast, m.event, m.matcher, m.command)
}

// InstallCodex adds each owned member root's hooks.json lacks, creating the file
// when absent; skipped reports a read-only file left untouched.
func InstallCodex(root string) (skipped bool, err error) {
	path := CodexHooksPath(root)
	settings, ast, err := readWellShaped(path, CodexSessionStartCommand)
	if err != nil {
		return false, err
	}
	if ast.Value == nil {
		if ast, err = hujson.Parse([]byte("{}")); err != nil {
			return false, fmt.Errorf("hooks: build empty settings: %w", err)
		}
	}
	added := false
	for _, m := range codexMembers {
		if hasEventHandler(settings, m.event, m.command) {
			continue
		}
		if err := m.register(&ast); err != nil {
			return false, err
		}
		added = true
	}
	if !added {
		return false, nil
	}
	return writeSettingsHujson(path, ast)
}

// ReplaceCodex moves Atomic's handlers out of whatever groups hold them into one
// canonical group each, keeping every user handler beside them.
func ReplaceCodex(root string) (skipped bool, err error) {
	path := CodexHooksPath(root)
	_, ast, err := readWellShaped(path, CodexSessionStartCommand)
	if err != nil {
		return false, err
	}
	if ast.Value == nil {
		return InstallCodex(root)
	}
	for _, m := range codexMembers {
		astStripHandlers(&ast, m.event, m.command)
		if err := m.register(&ast); err != nil {
			return false, err
		}
	}
	return writeSettingsHujson(path, ast)
}

// UninstallCodex strips Atomic's handlers, never a user handler beside them, and
// leaves the rest of hooks.json, including a file the removal emptied.
func UninstallCodex(root string) (skipped bool, err error) {
	path := CodexHooksPath(root)
	_, ast, err := readWellShaped(path, CodexSessionStartCommand)
	if err != nil || ast.Value == nil {
		return false, err
	}
	changed := false
	for _, m := range codexMembers {
		if astStripHandlers(&ast, m.event, m.command) {
			changed = true
		}
	}
	if !changed {
		return false, nil
	}
	return writeSettingsHujson(path, ast)
}

func ownedDigest(members []codexMember) string {
	owned := map[string]any{}
	for _, m := range members {
		owned[m.event] = []any{map[string]any{
			"matcher": m.matcher,
			"hooks":   []any{map[string]any{"type": "command", "command": m.command}},
		}}
	}
	data, _ := json.Marshal(owned)
	return managedfile.Digest(data)
}

// CodexCanonicalDigest is the owned-member digest of a fresh install, the only
// form Atomic adopts without a ledger record.
func CodexCanonicalDigest() string { return ownedDigest(codexMembers) }

// CodexLegacyDigest is the owned-member digest of a file written before the
// PreToolUse member existed.
func CodexLegacyDigest() string { return ownedDigest([]codexMember{codexSessionStart}) }

// ObserveCodexHooksOwned reports the owned members of root's hooks.json as one
// managed observation, digested apart from the user's hooks.
func ObserveCodexHooksOwned(root string) (managedfile.Observation, error) {
	return observeOwnedMembers(CodexHooksPath(root), func(settings map[string]any) map[string]any {
		owned := map[string]any{}
		for _, m := range codexMembers {
			if entries := ownedEventEntries(settings, m.event, m.command); len(entries) > 0 {
				owned[m.event] = entries
			}
		}
		return owned
	})
}

// ObserveOwnedMembers observes a settings resource by its path: Codex's
// hooks.json or Claude's settings.json.
func ObserveOwnedMembers(path string) (managedfile.Observation, error) {
	if isCodexHooks(path) {
		return ObserveCodexHooksOwned(filepath.Dir(path))
	}
	return ObserveSettingsOwnedInDir(filepath.Dir(path))
}

// RemoveOwnedMembers removes the members a settings resource owns, dispatching
// on the file as ObserveOwnedMembers does.
func RemoveOwnedMembers(path string) (skipped bool, err error) {
	if isCodexHooks(path) {
		return UninstallCodex(filepath.Dir(path))
	}
	return UninstallInDir(filepath.Dir(path))
}

func isCodexHooks(path string) bool {
	return filepath.Base(path) == codexHooksFileName
}

// CodexSessionStartPosition returns the SessionStart group and handler indexes
// of the owned member, which Codex uses to key the hook's trust record.
func CodexSessionStartPosition(root string) (group, handler int, found bool, err error) {
	return codexPosition(root, codexSessionStart)
}

// CodexPreToolUsePosition is CodexSessionStartPosition for the PreToolUse member.
func CodexPreToolUsePosition(root string) (group, handler int, found bool, err error) {
	return codexPosition(root, codexPreToolUse)
}

func codexPosition(root string, m codexMember) (group, handler int, found bool, err error) {
	settings, _, _, err := readSettingsHujson(CodexHooksPath(root))
	if err != nil {
		return 0, 0, false, err
	}
	hooksMap, _ := settings["hooks"].(map[string]any)
	groups, _ := hooksMap[m.event].([]any)
	for g, elem := range groups {
		entry, _ := elem.(map[string]any)
		inner, _ := entry["hooks"].([]any)
		for h, hook := range inner {
			hm, _ := hook.(map[string]any)
			if hm["command"] == m.command {
				return g, h, true, nil
			}
		}
	}
	return 0, 0, false, nil
}
