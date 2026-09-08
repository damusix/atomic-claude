package hooks

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/tailscale/hujson"
)

// OutputStyleName is the value seeded into outputStyle — the `name:`
// frontmatter of context/output-styles/atomic.md, not its filename slug.
const OutputStyleName = "Atomic"

// OutputStyleRelPath, resolved against targetDir, is the guard: seeding only
// makes sense when the style it names is actually installed. Exported so
// claudeinstall can check the same manifest target without a duplicated
// literal.
const OutputStyleRelPath = "output-styles/atomic.md"

const outputStyleKey = "outputStyle"

// SeedOutputStyle writes "outputStyle": OutputStyleName into
// SettingsPath(scopeRoot), but only when the style file is installed under
// targetDir, output_style.seed is enabled, and the key is absent. Every
// benign skip (flag off, style missing, key already present, read-only
// target) returns wrote == false with a nil error; a malformed settings.json
// is the only error path.
func SeedOutputStyle(scopeRoot, targetDir, home string) (wrote bool, err error) {
	if !StyleInstalled(targetDir) || !SeedEnabled(home) {
		return false, nil
	}

	sfPath := SettingsPath(scopeRoot)
	settings, ast, _, err := readSettingsHujson(sfPath)
	if err != nil {
		return false, err
	}
	if _, present := settings[outputStyleKey]; present {
		return false, nil
	}

	if ast.Value == nil {
		ast, err = hujson.Parse([]byte("{}"))
		if err != nil {
			return false, fmt.Errorf("hooks: build empty settings: %w", err)
		}
	}

	if err := setOutputStyleMember(&ast, OutputStyleName); err != nil {
		return false, err
	}

	skipped, err := writeSettingsHujson(sfPath, ast)
	if err != nil {
		return false, err
	}
	return !skipped, nil
}

// ReadOutputStyle reads outputStyle from one settings file. A missing file
// or absent key is ("", false, nil). A key set to a non-string by some other
// tool is rendered rather than dropped, so a reporting caller shows what is
// actually there instead of an empty string indistinguishable from "".
func ReadOutputStyle(sfPath string) (value string, present bool, err error) {
	settings, _, _, err := readSettingsHujson(sfPath)
	if err != nil {
		return "", false, err
	}
	raw, ok := settings[outputStyleKey]
	if !ok {
		return "", false, nil
	}
	if s, ok := raw.(string); ok {
		return s, true, nil
	}
	return fmt.Sprintf("%v", raw), true, nil
}

// RemoveOutputStyleIfAtomic deletes outputStyle only when its value is
// exactly OutputStyleName and the style file itself is already gone —
// while it's still installed the key names something real. Any other
// value, an absent key, an absent settings file, or an installed style
// file is a no-op that writes nothing.
func RemoveOutputStyleIfAtomic(scopeRoot string) (removed bool, skipped bool, err error) {
	sfPath := SettingsPath(scopeRoot)
	if StyleInstalled(filepath.Dir(sfPath)) {
		return false, false, nil
	}

	settings, ast, _, err := readSettingsHujson(sfPath)
	if err != nil {
		return false, false, err
	}
	raw, ok := settings[outputStyleKey]
	if !ok {
		return false, false, nil
	}
	if s, ok := raw.(string); !ok || s != OutputStyleName {
		return false, false, nil
	}

	removeOutputStyleMember(&ast)

	skipped, err = writeSettingsHujson(sfPath, ast)
	if err != nil {
		return false, false, err
	}
	return !skipped, skipped, nil
}

// StyleInstalled reports whether targetDir carries the shipped Atomic style.
func StyleInstalled(targetDir string) bool {
	_, err := os.Stat(filepath.Join(targetDir, OutputStyleRelPath))
	return err == nil
}

// SeedEnabled reads output_style.seed from home's user config, defaulting to
// true (config.Default's own default) when the file is missing or unreadable.
func SeedEnabled(home string) bool {
	cfg, _, err := config.Load(config.TOMLPath(home))
	if err != nil {
		return true
	}
	return cfg.OutputStyle.Seed
}

// setOutputStyleMember sets or appends the top-level outputStyle key on ast.
func setOutputStyleMember(ast *hujson.Value, value string) error {
	valBytes, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("hooks: marshal outputStyle value: %w", err)
	}
	valAST, err := hujson.Parse(valBytes)
	if err != nil {
		return fmt.Errorf("hooks: parse outputStyle value: %w", err)
	}

	topObj := ensureObject(ast)
	if ptr := findMember(topObj, outputStyleKey); ptr != nil {
		*ptr = valAST
		return nil
	}
	topObj.Members = append(topObj.Members, hujson.ObjectMember{
		Name:  parseJSONString(outputStyleKey),
		Value: valAST,
	})
	return nil
}

// removeOutputStyleMember drops the top-level outputStyle key from ast.
func removeOutputStyleMember(ast *hujson.Value) {
	topObj, ok := ast.Value.(*hujson.Object)
	if !ok {
		return
	}
	removeMember(topObj, outputStyleKey)
}
