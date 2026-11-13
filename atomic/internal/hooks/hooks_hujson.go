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

// registerInSettings is idempotent. skipped mirrors writeSettingsHujson's own
// signal: a read-only target left untouched rather than an error.
func registerInSettings(sfPath, command string) (skipped bool, err error) {
	settings, ast, err := readWellShaped(sfPath, command)
	if err != nil {
		return false, err
	}

	rewrote := command == sessionStartCommand &&
		astRewriteCommand(&ast, unflaggedSessionStartCommand, command)
	if hasRegistration(settings, command) {
		if rewrote {
			return writeSettingsHujson(sfPath, ast)
		}
		return false, nil
	}

	// A missing file leaves ast.Value nil.
	if ast.Value == nil {
		ast, err = hujson.Parse([]byte("{}"))
		if err != nil {
			return false, fmt.Errorf("hooks: build empty settings: %w", err)
		}
	}

	if err := astRegisterEvent(&ast, "SessionStart", sessionStartMatcher(command), command); err != nil {
		return false, err
	}

	return writeSettingsHujson(sfPath, ast)
}

func unregisterFromSettings(sfPath, command string) (skipped bool, err error) {
	settings, ast, _, err := readSettingsHujson(sfPath)
	if err != nil {
		return false, malformedSettingsError(sfPath, command)
	}
	if ast.Value == nil {
		return false, nil
	}

	hooksMap, ok := settings["hooks"].(map[string]any)
	if !ok {
		return false, nil
	}
	if _, ok := hooksMap["SessionStart"]; !ok {
		return false, nil
	}

	if err := astUnregisterSessionStart(&ast, command); err != nil {
		return false, err
	}

	return writeSettingsHujson(sfPath, ast)
}

// readWellShaped is readSettingsHujson that refuses a file it cannot safely
// edit: unparseable JSON, or a hooks shape a writer would have to coerce.
func readWellShaped(path, command string) (map[string]any, hujson.Value, error) {
	settings, ast, _, err := readSettingsHujson(path)
	if err != nil {
		return nil, hujson.Value{}, malformedSettingsError(path, command)
	}
	if err := shapeError(path, settings); err != nil {
		return nil, hujson.Value{}, err
	}
	return settings, ast, nil
}

// SettingsShapeError reports a hooks or SessionStart value a writer would have
// to coerce, or a file that does not parse; an absent file reports nil.
func SettingsShapeError(path string) error {
	settings, _, _, err := readSettingsHujson(path)
	if err != nil {
		return fmt.Errorf("%s: %w; fix the file, then rerun", path, err)
	}
	return shapeError(path, settings)
}

func shapeError(path string, settings map[string]any) error {
	raw, ok := settings["hooks"]
	if !ok {
		return nil
	}
	hooksMap, ok := raw.(map[string]any)
	if !ok {
		return fmt.Errorf(`%s: "hooks" must be an object; found %s. Fix the file, then rerun.`, path, jsonType(raw))
	}
	events := []string{"SessionStart"}
	if isCodexHooks(path) {
		events = append(events, "PreToolUse")
	}
	for _, event := range events {
		value, ok := hooksMap[event]
		if !ok {
			continue
		}
		if _, ok := value.([]any); !ok {
			return fmt.Errorf(`%s: "hooks".%q must be an array; found %s. Fix the file, then rerun.`, path, event, jsonType(value))
		}
	}
	return nil
}

func jsonType(v any) string {
	switch v.(type) {
	case map[string]any:
		return "object"
	case []any:
		return "array"
	case string:
		return "string"
	case float64:
		return "number"
	case bool:
		return "boolean"
	default:
		return "null"
	}
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

// writeSettingsHujson writes ast to sfPath via temp file + rename, so a
// concurrent reader (Claude Code's own settings watcher) never observes a
// partial file. skipped reports a read-only target left untouched rather
// than an error, so callers can stay silent instead of failing the caller's
// own operation.
func writeSettingsHujson(sfPath string, ast hujson.Value) (skipped bool, err error) {
	if err := os.MkdirAll(filepath.Dir(sfPath), 0o755); err != nil {
		return false, fmt.Errorf("hooks: mkdir for settings.json: %w", err)
	}

	resolved, mode, writable, err := resolveSettingsTarget(sfPath)
	if err != nil {
		return false, err
	}
	if !writable {
		return true, nil
	}

	out := ast.Pack()
	if len(out) > 0 && out[len(out)-1] != '\n' {
		out = append(out, '\n')
	}

	// Same directory as the resolved target (not sfPath) so a symlinked
	// settings.json is followed rather than replaced by the rename.
	tmp, err := os.CreateTemp(filepath.Dir(resolved), ".settings-*.json.tmp")
	if err != nil {
		return false, fmt.Errorf("hooks: create temp settings file: %w", err)
	}
	tmpName := tmp.Name()

	if _, err := tmp.Write(out); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return false, fmt.Errorf("hooks: write temp settings file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return false, fmt.Errorf("hooks: close temp settings file: %w", err)
	}
	if err := os.Chmod(tmpName, mode); err != nil {
		os.Remove(tmpName)
		return false, fmt.Errorf("hooks: chmod temp settings file: %w", err)
	}
	if err := os.Rename(tmpName, resolved); err != nil {
		os.Remove(tmpName)
		return false, fmt.Errorf("hooks: rename to settings.json: %w", err)
	}
	return false, nil
}

// resolveSettingsTarget follows sfPath through any symlink (a dotfiles-repo
// settings.json is common), reports the existing file's mode for
// preservation, and probes writability so a read-only target is skipped
// rather than clobbered by a rename that only needs directory permission.
func resolveSettingsTarget(sfPath string) (resolved string, mode os.FileMode, writable bool, err error) {
	resolved = sfPath
	if r, evalErr := filepath.EvalSymlinks(sfPath); evalErr == nil {
		resolved = r
	} else if !os.IsNotExist(evalErr) {
		return "", 0, false, fmt.Errorf("hooks: resolve settings.json path: %w", evalErr)
	}

	mode = os.FileMode(0o644)
	if fi, statErr := os.Stat(resolved); statErr == nil {
		mode = fi.Mode().Perm()
	}

	f, openErr := os.OpenFile(resolved, os.O_WRONLY, 0)
	if openErr != nil {
		if os.IsNotExist(openErr) {
			return resolved, mode, true, nil
		}
		if os.IsPermission(openErr) {
			return resolved, mode, false, nil
		}
		return "", 0, false, fmt.Errorf("hooks: probe settings.json writability: %w", openErr)
	}
	f.Close()
	return resolved, mode, true, nil
}

// astRegisterEvent creates the hooks key and the event array as needed.
func astRegisterEvent(ast *hujson.Value, event, matcher, command string) error {
	entryBytes, err := buildEntryJSON(matcher, command)
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

	eventValPtr := findMember(hooksObj, event)
	if eventValPtr == nil {
		emptyArr, _ := hujson.Parse([]byte("[]"))
		hooksObj.Members = append(hooksObj.Members, hujson.ObjectMember{
			Name:  parseJSONString(event),
			Value: emptyArr,
		})
		eventValPtr = &hooksObj.Members[len(hooksObj.Members)-1].Value
	}

	arr, ok := eventValPtr.Value.(*hujson.Array)
	if !ok {
		return fmt.Errorf("hooks: %s is not an array", event)
	}
	arr.Elements = append(arr.Elements, entryVal)

	return nil
}

// astUnregisterSessionStart drops SessionStart, then hooks, once each empties.
func astUnregisterSessionStart(ast *hujson.Value, command string) error {
	arr := eventArray(ast, "SessionStart")
	if arr == nil {
		return nil
	}

	filtered := arr.Elements[:0]
	for _, elem := range arr.Elements {
		if !sessionStartEntryMatchesCommand(elem, command) {
			filtered = append(filtered, elem)
		}
	}
	arr.Elements = filtered

	dropEmptyEvent(ast, "SessionStart")
	return nil
}

// astStripHandlers removes every handler running command, then any group or
// container that removal emptied, and reports whether anything changed.
func astStripHandlers(ast *hujson.Value, event, command string) bool {
	arr := eventArray(ast, event)
	if arr == nil {
		return false
	}
	changed := false
	kept := arr.Elements[:0]
	for _, elem := range arr.Elements {
		inner := entryHooks(elem)
		if inner == nil {
			kept = append(kept, elem)
			continue
		}
		before := len(inner.Elements)
		handlers := inner.Elements[:0]
		for _, h := range inner.Elements {
			if _, name := commandValue(h); name == command {
				changed = true
				continue
			}
			handlers = append(handlers, h)
		}
		inner.Elements = handlers
		if len(handlers) > 0 || before == 0 {
			kept = append(kept, elem)
		}
	}
	arr.Elements = kept
	dropEmptyEvent(ast, event)
	return changed
}

func dropEmptyEvent(ast *hujson.Value, event string) {
	topObj, hooksObj, arr := eventParts(ast, event)
	if arr == nil || len(arr.Elements) > 0 {
		return
	}
	removeMember(hooksObj, event)
	if len(hooksObj.Members) == 0 {
		removeMember(topObj, "hooks")
	}
}

func eventArray(ast *hujson.Value, event string) *hujson.Array {
	_, _, arr := eventParts(ast, event)
	return arr
}

// eventParts walks the top object, hooks object and event array;
// all are nil when a step is absent or mis-shaped.
func eventParts(ast *hujson.Value, event string) (topObj, hooksObj *hujson.Object, arr *hujson.Array) {
	topObj, ok := ast.Value.(*hujson.Object)
	if !ok {
		return nil, nil, nil
	}
	hooksVal := findMember(topObj, "hooks")
	if hooksVal == nil {
		return nil, nil, nil
	}
	hooksObj, ok = hooksVal.Value.(*hujson.Object)
	if !ok {
		return nil, nil, nil
	}
	ssVal := findMember(hooksObj, event)
	if ssVal == nil {
		return nil, nil, nil
	}
	arr, _ = ssVal.Value.(*hujson.Array)
	return topObj, hooksObj, arr
}

func buildEntryJSON(matcher, command string) ([]byte, error) {
	entry := map[string]any{
		"matcher": matcher,
		"hooks": []any{
			map[string]any{
				"type":    "command",
				"command": command,
			},
		},
	}
	return json.MarshalIndent(entry, "", "  ")
}

func sessionStartMatcher(command string) string {
	if command == CodexSessionStartCommand {
		return CodexSessionStartMatcher
	}
	return ".*"
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
		if commandMatches(command, hm["command"]) {
			return true
		}
	}
	return false
}

// astRewriteCommand keeps the first from-or-to hook in place (renamed to to),
// so its position and matcher survive, and drops every later one.
func astRewriteCommand(ast *hujson.Value, from, to string) bool {
	arr := eventArray(ast, "SessionStart")
	if arr == nil {
		return false
	}
	changed, haveTo := false, false
	kept := arr.Elements[:0]
	for _, elem := range arr.Elements {
		inner := entryHooks(elem)
		if inner == nil {
			kept = append(kept, elem)
			continue
		}
		before := len(inner.Elements)
		hooksKept := inner.Elements[:0]
		for _, h := range inner.Elements {
			cmd, name := commandValue(h)
			switch {
			case cmd == nil || (name != from && name != to):
			case !haveTo:
				haveTo = true
				if name == from {
					cmd.Value = parseJSONString(to).Value
					changed = true
				}
			default:
				changed = true
				continue
			}
			hooksKept = append(hooksKept, h)
		}
		inner.Elements = hooksKept
		if len(hooksKept) > 0 || before == 0 {
			kept = append(kept, elem)
		}
	}
	arr.Elements = kept
	return changed
}

func entryHooks(entry hujson.Value) *hujson.Array {
	obj, ok := entry.Value.(*hujson.Object)
	if !ok {
		return nil
	}
	v := findMember(obj, "hooks")
	if v == nil {
		return nil
	}
	arr, _ := v.Value.(*hujson.Array)
	return arr
}

func commandValue(hook hujson.Value) (*hujson.Value, string) {
	obj, ok := hook.Value.(*hujson.Object)
	if !ok {
		return nil, ""
	}
	v := findMember(obj, "command")
	if v == nil {
		return nil, ""
	}
	lit, ok := v.Value.(hujson.Literal)
	if !ok {
		return nil, ""
	}
	return v, lit.String()
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
