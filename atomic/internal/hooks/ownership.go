package hooks

import (
	"encoding/json"
	"fmt"

	"github.com/damusix/atomic-claude/atomic/internal/managedfile"
)

// ObserveSettingsOwnedInDir digests only the Atomic-owned settings members, so a
// user's unrelated edits never read as drift and an edit to an owned one does.
func ObserveSettingsOwnedInDir(configDir string) (managedfile.Observation, error) {
	return observeOwnedMembers(SettingsPathInDir(configDir), func(settings map[string]any) map[string]any {
		owned := map[string]any{}
		if value, ok := settings[outputStyleKey]; ok {
			if style, ok := value.(string); ok && style == OutputStyleName {
				owned[outputStyleKey] = style
			}
		}
		if entries := ownedEventEntries(settings, "SessionStart", sessionStartCommand); len(entries) > 0 {
			owned["SessionStart"] = entries
		}
		return owned
	})
}

// observeOwnedMembers digests the members ownedOf selects from the JSON file at
// path; no selected member is an absent observation.
func observeOwnedMembers(path string, ownedOf func(map[string]any) map[string]any) (managedfile.Observation, error) {
	obs := managedfile.Observation{Path: path, Kind: managedfile.KindSettings, Ownership: managedfile.OwnershipUnowned}

	settings, _, raw, err := readSettingsHujson(path)
	if err != nil || shapeError(path, settings) != nil {
		obs.Conflict = managedfile.ConflictMalformedSettings
		return obs, nil
	}
	if raw == nil {
		obs.Kind = managedfile.KindAbsent
		return obs, nil
	}

	owned := ownedOf(settings)
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

// ownedEventEntries returns the event entries that register command.
// It reads the parsed map so the digest ignores comments and formatting.
func ownedEventEntries(settings map[string]any, event, command string) []any {
	hooksMap, ok := settings["hooks"].(map[string]any)
	if !ok {
		return nil
	}
	arr, ok := hooksMap[event].([]any)
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
			if commandMatches(command, hm["command"]) {
				out = append(out, entry)
				break
			}
		}
	}
	return out
}
