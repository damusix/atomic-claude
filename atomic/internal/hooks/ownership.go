package hooks

import (
	"encoding/json"
	"fmt"

	"github.com/damusix/atomic-claude/atomic/internal/managedfile"
)

// ObserveSettingsOwnedInDir reports the Atomic-owned members of an explicit
// Claude config directory's settings file as one managed observation: the
// inline SessionStart registration under the Atomic command, and the
// outputStyle key when it names the shipped style. Ownership covers those
// members alone, so a user's unrelated settings edits never read as drift and
// an edit to an owned member always does. The digest is over a canonical
// projection of the owned members, never the whole file.
//
// A missing file, or a file carrying neither owned member, is an absent
// observation; a file that will not parse is a malformed conflict. Path and
// kind are always reported so the caller can name the resource it observed.
func ObserveSettingsOwnedInDir(configDir string) (managedfile.Observation, error) {
	sfPath := SettingsPathInDir(configDir)
	obs := managedfile.Observation{Path: sfPath, Kind: managedfile.KindSettings, Ownership: managedfile.OwnershipUnowned}

	settings, _, raw, err := readSettingsHujson(sfPath)
	if err != nil {
		obs.Conflict = managedfile.ConflictMalformedSettings
		return obs, nil
	}
	if raw == nil {
		obs.Kind = managedfile.KindAbsent
		return obs, nil
	}

	owned := map[string]any{}
	if value, ok := settings[outputStyleKey]; ok {
		if style, ok := value.(string); ok && style == OutputStyleName {
			owned[outputStyleKey] = style
		}
	}
	if entries := ownedSessionStartEntries(settings, sessionStartCommand); len(entries) > 0 {
		owned["SessionStart"] = entries
	}
	if len(owned) == 0 {
		obs.Kind = managedfile.KindAbsent
		return obs, nil
	}

	canonical, err := json.Marshal(owned)
	if err != nil {
		return managedfile.Observation{}, fmt.Errorf("hooks: marshal settings ownership: %w", err)
	}
	obs.Digest = managedfile.Digest(canonical)
	return obs, nil
}

// ownedSessionStartEntries returns the SessionStart entries whose inner hooks
// register command, in file order. It reads the parsed map rather than the AST
// so the projection's digest is independent of the file's comments and
// formatting.
func ownedSessionStartEntries(settings map[string]any, command string) []any {
	hooksMap, ok := settings["hooks"].(map[string]any)
	if !ok {
		return nil
	}
	arr, ok := hooksMap["SessionStart"].([]any)
	if !ok {
		return nil
	}
	var out []any
	for _, elem := range arr {
		entry, ok := elem.(map[string]any)
		if !ok {
			continue
		}
		inner, ok := entry["hooks"].([]any)
		if !ok {
			continue
		}
		for _, h := range inner {
			hm, ok := h.(map[string]any)
			if !ok {
				continue
			}
			if hm["command"] == command {
				out = append(out, entry)
				break
			}
		}
	}
	return out
}
