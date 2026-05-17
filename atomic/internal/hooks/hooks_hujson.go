package hooks

// hujsonSettings provides JWCC-aware read/write for settings.json using
// github.com/tailscale/hujson. Comments and trailing commas in the original
// file are preserved on round-trip.
//
// Strategy:
//  1. Parse with hujson.Parse to validate and get the AST.
//  2. Standardize a copy and json.Unmarshal to read current state.
//  3. If a mutation is needed, locate the relevant AST node and mutate in-place.
//  4. Pack the AST back to bytes and write.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/tailscale/hujson"
)

// registerInSettings adds the hook entry to settings.json if not already present.
// Uses hujson to preserve comments and trailing commas in the existing file.
func registerInSettings(sfPath, scriptAbsPath string) error {
	settings, ast, _, err := readSettingsHujson(sfPath)
	if err != nil {
		return malformedErrorWithScript(sfPath, scriptAbsPath)
	}

	// Check idempotency: look for existing entry with the same command.
	if hasRegistration(settings, scriptAbsPath) {
		return nil
	}

	// If the file didn't exist, ast.Value is nil — start from an empty object.
	if ast.Value == nil {
		ast, err = hujson.Parse([]byte("{}"))
		if err != nil {
			return fmt.Errorf("hooks: build empty settings: %w", err)
		}
	}

	if err := astRegisterSessionStart(&ast, scriptAbsPath); err != nil {
		return err
	}

	return writeSettingsHujson(sfPath, ast)
}

// unregisterFromSettings removes the entry matching scriptAbsPath from settings.json.
// Uses hujson to preserve comments and trailing commas.
func unregisterFromSettings(sfPath, scriptAbsPath string) error {
	settings, ast, _, err := readSettingsHujson(sfPath)
	if err != nil {
		return malformedErrorWithScript(sfPath, scriptAbsPath)
	}
	if ast.Value == nil {
		return nil
	}

	// Quick check: if nothing to remove, skip writing.
	hooksMap, ok := settings["hooks"].(map[string]any)
	if !ok {
		return nil
	}
	if _, ok := hooksMap["SessionStart"]; !ok {
		return nil
	}

	if err := astUnregisterSessionStart(&ast, scriptAbsPath); err != nil {
		return err
	}

	return writeSettingsHujson(sfPath, ast)
}

// readSettingsHujson reads settings.json preserving JWCC syntax (comments,
// trailing commas). Returns (goMap, rawAST, rawBytes, error).
// If the file does not exist, returns (empty map, zero Value, nil bytes, nil).
// If the file is not JWCC-parseable, returns (nil, zero, raw, error).
func readSettingsHujson(sfPath string) (map[string]any, hujson.Value, []byte, error) {
	raw, err := os.ReadFile(sfPath)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]any{}, hujson.Value{}, nil, nil
		}
		return nil, hujson.Value{}, nil, fmt.Errorf("hooks: read settings.json: %w", err)
	}

	// Parse as JWCC (validates comments + trailing commas).
	ast, err := hujson.Parse(raw)
	if err != nil {
		return nil, hujson.Value{}, raw, fmt.Errorf("JWCC parse error: %w", err)
	}

	// Standardize a copy to get plain JSON for Go-level reads.
	// IMPORTANT: do NOT standardize `raw` in-place — ast aliases raw's bytes,
	// so in-place mutation would corrupt the comment/whitespace extras.
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

// writeSettingsHujson writes settings.json from an updated hujson AST.
func writeSettingsHujson(sfPath string, ast hujson.Value) error {
	if err := os.MkdirAll(filepath.Dir(sfPath), 0o755); err != nil {
		return fmt.Errorf("hooks: mkdir for settings.json: %w", err)
	}
	out := ast.Pack()
	// Ensure trailing newline.
	if len(out) > 0 && out[len(out)-1] != '\n' {
		out = append(out, '\n')
	}
	if err := os.WriteFile(sfPath, out, 0o644); err != nil {
		return fmt.Errorf("hooks: write settings.json: %w", err)
	}
	return nil
}

// astRegisterSessionStart appends a new SessionStart entry to the hujson AST.
// It creates the hooks key and SessionStart array if they don't exist.
func astRegisterSessionStart(ast *hujson.Value, scriptAbsPath string) error {
	entryBytes, err := buildEntryJSON(scriptAbsPath)
	if err != nil {
		return fmt.Errorf("hooks: build entry JSON: %w", err)
	}
	entryVal, err := hujson.Parse(entryBytes)
	if err != nil {
		return fmt.Errorf("hooks: parse entry JSON: %w", err)
	}

	// Get or create the top-level object.
	topObj := ensureObject(ast)

	// Find or create the "hooks" member.
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

	// Find or create the "SessionStart" member.
	ssValPtr := findMember(hooksObj, "SessionStart")
	if ssValPtr == nil {
		emptyArr, _ := hujson.Parse([]byte("[]"))
		hooksObj.Members = append(hooksObj.Members, hujson.ObjectMember{
			Name:  parseJSONString("SessionStart"),
			Value: emptyArr,
		})
		ssValPtr = &hooksObj.Members[len(hooksObj.Members)-1].Value
	}

	// Append the new entry.
	arr := ssValPtr.Value.(*hujson.Array)
	arr.Elements = append(arr.Elements, entryVal)

	return nil
}

// astUnregisterSessionStart removes the SessionStart entry whose inner
// hooks[].command equals scriptAbsPath. Drops SessionStart if empty;
// drops hooks if empty.
func astUnregisterSessionStart(ast *hujson.Value, scriptAbsPath string) error {
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

	// Filter out entries that reference scriptAbsPath.
	filtered := arr.Elements[:0]
	for _, elem := range arr.Elements {
		if !sessionStartEntryMatchesScript(elem, scriptAbsPath) {
			filtered = append(filtered, elem)
		}
	}
	arr.Elements = filtered

	// Drop SessionStart if empty.
	if len(arr.Elements) == 0 {
		removeMember(hooksObj, "SessionStart")
	}

	// Drop hooks if empty.
	if len(hooksObj.Members) == 0 {
		removeMember(topObj, "hooks")
	}

	return nil
}

// buildEntryJSON returns the JSON bytes for a new SessionStart hook entry.
func buildEntryJSON(scriptAbsPath string) ([]byte, error) {
	entry := map[string]any{
		"matcher": ".*",
		"hooks": []any{
			map[string]any{
				"type":    "command",
				"command": scriptAbsPath,
			},
		},
	}
	return json.MarshalIndent(entry, "", "  ")
}

// sessionStartEntryMatchesScript returns true if the hujson element represents
// a SessionStart entry whose hooks[].command matches scriptAbsPath.
func sessionStartEntryMatchesScript(elem hujson.Value, scriptAbsPath string) bool {
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
		if hm["command"] == scriptAbsPath {
			return true
		}
	}
	return false
}

// ensureObject returns the *hujson.Object from a Value, coercing it to an
// empty object if it's nil or not an object.
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

// findMember finds the Value pointer for a named member in an Object.
// Returns nil if not found.
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

// removeMember removes a named member from an Object.
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

// parseJSONString creates a hujson Value representing a JSON string literal.
func parseJSONString(s string) hujson.Value {
	b, _ := json.Marshal(s)
	v, _ := hujson.Parse(b)
	return v
}
