package hooks

// JWCC-aware read/write for settings.json: mutations happen on the parsed AST
// and are packed back out, so the user's comments and trailing commas survive.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/tailscale/hujson"
)

// registerInSettings is idempotent.
func registerInSettings(sfPath, command string) error {
	settings, ast, _, err := readSettingsHujson(sfPath)
	if err != nil {
		return malformedSettingsError(sfPath, command)
	}

	if hasRegistration(settings, command) {
		return nil
	}

	// A missing file leaves ast.Value nil.
	if ast.Value == nil {
		ast, err = hujson.Parse([]byte("{}"))
		if err != nil {
			return fmt.Errorf("hooks: build empty settings: %w", err)
		}
	}

	if err := astRegisterSessionStart(&ast, command); err != nil {
		return err
	}

	return writeSettingsHujson(sfPath, ast)
}

func unregisterFromSettings(sfPath, command string) error {
	settings, ast, _, err := readSettingsHujson(sfPath)
	if err != nil {
		return malformedSettingsError(sfPath, command)
	}
	if ast.Value == nil {
		return nil
	}

	hooksMap, ok := settings["hooks"].(map[string]any)
	if !ok {
		return nil
	}
	if _, ok := hooksMap["SessionStart"]; !ok {
		return nil
	}

	if err := astUnregisterSessionStart(&ast, command); err != nil {
		return err
	}

	return writeSettingsHujson(sfPath, ast)
}

// readSettingsHujson returns an empty map for a missing file, and (nil, zero,
// raw, err) when the file will not parse as JWCC.
func readSettingsHujson(sfPath string) (map[string]any, hujson.Value, []byte, error) {
	raw, err := os.ReadFile(sfPath)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]any{}, hujson.Value{}, nil, nil
		}
		return nil, hujson.Value{}, nil, fmt.Errorf("hooks: read settings.json: %w", err)
	}

	ast, err := hujson.Parse(raw)
	if err != nil {
		return nil, hujson.Value{}, raw, fmt.Errorf("JWCC parse error: %w", err)
	}

	// Standardize a copy, never raw in place: ast aliases raw's bytes, so mutating
	// them would corrupt the comment and whitespace extras.
	rawCopy := make([]byte, len(raw))
	copy(rawCopy, raw)
	stdBytes, err := hujson.Standardize(rawCopy)
	if err != nil {
		return nil, hujson.Value{}, raw, fmt.Errorf("JWCC standardize error: %w", err)
	}

	var settings map[string]any
	if err := json.Unmarshal(stdBytes, &settings); err != nil {
		return nil, hujson.Value{}, raw, fmt.Errorf("JSON unmarshal error: %w", err)
	}
	if settings == nil {
		settings = map[string]any{}
	}
	return settings, ast, raw, nil
}

func writeSettingsHujson(sfPath string, ast hujson.Value) error {
	if err := os.MkdirAll(filepath.Dir(sfPath), 0o755); err != nil {
		return fmt.Errorf("hooks: mkdir for settings.json: %w", err)
	}
	out := ast.Pack()
	if len(out) > 0 && out[len(out)-1] != '\n' {
		out = append(out, '\n')
	}
	if err := os.WriteFile(sfPath, out, 0o644); err != nil {
		return fmt.Errorf("hooks: write settings.json: %w", err)
	}
	return nil
}

// astRegisterSessionStart creates the hooks key and SessionStart array as needed.
func astRegisterSessionStart(ast *hujson.Value, command string) error {
	entryBytes, err := buildEntryJSON(command)
	if err != nil {
		return fmt.Errorf("hooks: build entry JSON: %w", err)
	}
	entryVal, err := hujson.Parse(entryBytes)
	if err != nil {
		return fmt.Errorf("hooks: parse entry JSON: %w", err)
	}

	topObj := ensureObject(ast)

	hooksValPtr := findMember(topObj, "hooks")
	if hooksValPtr == nil {
		emptyHooks, _ := hujson.Parse([]byte("{}"))
		topObj.Members = append(topObj.Members, hujson.ObjectMember{
			Name:  parseJSONString("hooks"),
			Value: emptyHooks,
		})
		hooksValPtr = &topObj.Members[len(topObj.Members)-1].Value
	}

	hooksObj := ensureObject(hooksValPtr)

	ssValPtr := findMember(hooksObj, "SessionStart")
	if ssValPtr == nil {
		emptyArr, _ := hujson.Parse([]byte("[]"))
		hooksObj.Members = append(hooksObj.Members, hujson.ObjectMember{
			Name:  parseJSONString("SessionStart"),
			Value: emptyArr,
		})
		ssValPtr = &hooksObj.Members[len(hooksObj.Members)-1].Value
	}

	arr := ssValPtr.Value.(*hujson.Array)
	arr.Elements = append(arr.Elements, entryVal)

	return nil
}

// astUnregisterSessionStart drops SessionStart, then hooks, once each empties.
func astUnregisterSessionStart(ast *hujson.Value, command string) error {
	topObj, ok := ast.Value.(*hujson.Object)
	if !ok {
		return nil
	}
	hooksValPtr := findMember(topObj, "hooks")
	if hooksValPtr == nil {
		return nil
	}
	hooksObj, ok := hooksValPtr.Value.(*hujson.Object)
	if !ok {
		return nil
	}
	ssValPtr := findMember(hooksObj, "SessionStart")
	if ssValPtr == nil {
		return nil
	}

	arr, ok := ssValPtr.Value.(*hujson.Array)
	if !ok {
		return nil
	}

	filtered := arr.Elements[:0]
	for _, elem := range arr.Elements {
		if !sessionStartEntryMatchesCommand(elem, command) {
			filtered = append(filtered, elem)
		}
	}
	arr.Elements = filtered

	if len(arr.Elements) == 0 {
		removeMember(hooksObj, "SessionStart")
	}

	if len(hooksObj.Members) == 0 {
		removeMember(topObj, "hooks")
	}

	return nil
}

func buildEntryJSON(command string) ([]byte, error) {
	entry := map[string]any{
		"matcher": ".*",
		"hooks": []any{
			map[string]any{
				"type":    "command",
				"command": command,
			},
		},
	}
	return json.MarshalIndent(entry, "", "  ")
}

func sessionStartEntryMatchesCommand(elem hujson.Value, command string) bool {
	std, err := hujson.Standardize(elem.Pack())
	if err != nil {
		return false
	}
	var entry map[string]any
	if err := json.Unmarshal(std, &entry); err != nil {
		return false
	}
	inner, ok := entry["hooks"].([]any)
	if !ok {
		return false
	}
	for _, h := range inner {
		hm, ok := h.(map[string]any)
		if !ok {
			continue
		}
		if hm["command"] == command {
			return true
		}
	}
	return false
}

// ensureObject coerces a nil or non-object Value into an empty object.
func ensureObject(v *hujson.Value) *hujson.Object {
	if v.Value != nil {
		if obj, ok := v.Value.(*hujson.Object); ok {
			return obj
		}
	}
	obj := &hujson.Object{}
	v.Value = obj
	return obj
}

func findMember(obj *hujson.Object, key string) *hujson.Value {
	for i := range obj.Members {
		lit, ok := obj.Members[i].Name.Value.(hujson.Literal)
		if !ok {
			continue
		}
		if lit.String() == key {
			return &obj.Members[i].Value
		}
	}
	return nil
}

func removeMember(obj *hujson.Object, key string) {
	kept := obj.Members[:0]
	for _, m := range obj.Members {
		lit, ok := m.Name.Value.(hujson.Literal)
		if ok && lit.String() == key {
			continue
		}
		kept = append(kept, m)
	}
	obj.Members = kept
}

func parseJSONString(s string) hujson.Value {
	b, _ := json.Marshal(s)
	v, _ := hujson.Parse(b)
	return v
}
