package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// intensityDefault is the built-in default for output.intensity.
const intensityDefault = "full"

// runDoctorDefault is the built-in default for update.run_doctor.
const runDoctorDefault = true

// validIntensity is the set of allowed output.intensity values.
var validIntensity = map[string]bool{
	"lite":  true,
	"full":  true,
	"ultra": true,
}

// knownKeys is the exhaustive list of v1 dotted keys.
var knownKeys = []string{
	"output.intensity",
	"update.run_doctor",
}

// knownSections is the set of known top-level TOML table names.
// Derived once from knownKeys rather than rebuilt on every checkUnknownKeys call.
var knownSections = func() map[string]bool {
	m := map[string]bool{}
	for _, k := range knownKeys {
		if dot := strings.IndexByte(k, '.'); dot > 0 {
			m[k[:dot]] = true
		}
	}
	return m
}()

// Warning carries a non-fatal diagnostic from Load.
type Warning struct {
	Message string
}

func (w Warning) Error() string { return w.Message }

// outputSection is the [output] TOML table.
type outputSection struct {
	Intensity string `toml:"intensity"`
}

// updateSection is the [update] TOML table.
type updateSection struct {
	RunDoctor bool `toml:"run_doctor"`
}

// Config is the parsed + defaulted configuration.
// Fields track explicit set values; zero values mean "use built-in default".
type Config struct {
	Output outputSection `toml:"output"`
	Update updateSection `toml:"update"`
}

// Default returns a Config populated with built-in defaults.
func Default() *Config {
	return &Config{
		Output: outputSection{Intensity: intensityDefault},
		Update: updateSection{RunDoctor: runDoctorDefault},
	}
}

// Load reads path into a Config leniently: unknown keys produce Warnings but
// no error. If path does not exist, Load returns Default() with no warnings.
func Load(path string) (*Config, []Warning, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Default(), nil, nil
		}
		return nil, nil, fmt.Errorf("config: read %s: %w", path, err)
	}

	// Decode into a raw map first so we can detect unknown keys.
	var rawMap map[string]any
	if err := toml.Unmarshal(raw, &rawMap); err != nil {
		return nil, nil, fmt.Errorf("config: parse %s: %w", path, err)
	}

	var warns []Warning
	warns = append(warns, checkUnknownKeys(rawMap, "")...)

	// Detect explicit presence of update.run_doctor before decoding into the
	// typed struct. The bool zero-value (false) is indistinguishable from
	// "absent" after decode, so we check the raw map here.
	updateRunDoctorExplicit := false
	if updateRaw, ok := rawMap["update"]; ok {
		if updateTable, ok := updateRaw.(map[string]any); ok {
			if _, ok := updateTable["run_doctor"]; ok {
				updateRunDoctorExplicit = true
			}
		}
	}

	// Decode into the typed struct (strict fields only).
	cfg := Default()
	if err := toml.Unmarshal(raw, cfg); err != nil {
		return nil, warns, fmt.Errorf("config: decode %s: %w", path, err)
	}

	// Backfill defaults for any zero-value fields.
	if cfg.Output.Intensity == "" {
		cfg.Output.Intensity = intensityDefault
	}
	// update.run_doctor: only backfill default when the key was absent.
	// When explicitly set to false, the decoded value is already false — correct.
	// When absent, Default() already set it to true; TOML decode of a missing
	// section resets the struct to zero (false), so we must restore the default.
	if !updateRunDoctorExplicit {
		cfg.Update.RunDoctor = runDoctorDefault
	}

	return cfg, warns, nil
}

// knownLeaves is the set of known dotted leaf keys, computed once.
var knownLeaves = func() map[string]bool {
	m := map[string]bool{}
	for _, k := range knownKeys {
		m[k] = true
	}
	return m
}()

// checkUnknownKeys walks a raw decoded TOML map and returns a Warning for each
// key that is not in knownKeys. prefix is the dotted path so far.
func checkUnknownKeys(m map[string]any, prefix string) []Warning {
	var warns []Warning
	for k, v := range m {
		dotted := k
		if prefix != "" {
			dotted = prefix + "." + k
		}

		// Check if this is a known section at the top level.
		if prefix == "" {
			if !knownSections[k] {
				warns = append(warns, Warning{
					Message: fmt.Sprintf("config: unknown key %q (ignored)", dotted),
				})
				continue
			}
		} else {
			// For nested keys, check against the full dotted path.
			if !knownLeaves[dotted] {
				warns = append(warns, Warning{
					Message: fmt.Sprintf("config: unknown key %q (ignored)", dotted),
				})
				continue
			}
		}

		// Recurse into tables.
		if sub, ok := v.(map[string]any); ok {
			warns = append(warns, checkUnknownKeys(sub, dotted)...)
		}
	}
	return warns
}

// Validate returns an error if cfg contains values outside the allowed schema.
// update.run_doctor is a bool and has no invalid state at the Config level.
func Validate(cfg *Config) error {
	if cfg.Output.Intensity != "" && !validIntensity[cfg.Output.Intensity] {
		allowed := strings.Join(sortedKeys(validIntensity), ", ")
		return fmt.Errorf("config: output.intensity %q is not one of: %s", cfg.Output.Intensity, allowed)
	}
	return nil
}

// Get returns the resolved value for a dotted key.
// Returns error for unknown keys (with a near-match suggestion when
// Levenshtein distance ≤ 2).
func Get(cfg *Config, dottedKey string) (string, error) {
	m := Resolved(cfg)
	v, ok := m[dottedKey]
	if !ok {
		suggestion := nearMatch(dottedKey, knownKeys)
		if suggestion != "" {
			return "", fmt.Errorf("config: unknown key %q; did you mean %q?", dottedKey, suggestion)
		}
		return "", fmt.Errorf("config: unknown key %q", dottedKey)
	}
	return v, nil
}

// Set updates cfg in memory for the given dotted key/value pair.
// Returns an error for unknown keys (with a near-match suggestion when
// Levenshtein distance ≤ 2) or values outside the allowed enum.
func Set(cfg *Config, dottedKey, value string) error {
	if !isKnownKey(dottedKey) {
		suggestion := nearMatch(dottedKey, knownKeys)
		if suggestion != "" {
			return fmt.Errorf("config: unknown key %q; did you mean %q?", dottedKey, suggestion)
		}
		return fmt.Errorf("config: unknown key %q", dottedKey)
	}

	switch dottedKey {
	case "output.intensity":
		if !validIntensity[value] {
			allowed := strings.Join(sortedKeys(validIntensity), ", ")
			return fmt.Errorf("config: output.intensity %q is not one of: %s", value, allowed)
		}
		cfg.Output.Intensity = value
	case "update.run_doctor":
		switch value {
		case "true":
			cfg.Update.RunDoctor = true
		case "false":
			cfg.Update.RunDoctor = false
		default:
			return fmt.Errorf("config: update.run_doctor %q is not one of: false, true", value)
		}
	}
	return nil
}

// Unset reverts the given key to its built-in default.
// Returns an error for unknown keys (with a near-match suggestion when
// Levenshtein distance ≤ 2).
func Unset(cfg *Config, dottedKey string) error {
	if !isKnownKey(dottedKey) {
		suggestion := nearMatch(dottedKey, knownKeys)
		if suggestion != "" {
			return fmt.Errorf("config: unknown key %q; did you mean %q?", dottedKey, suggestion)
		}
		return fmt.Errorf("config: unknown key %q", dottedKey)
	}
	switch dottedKey {
	case "output.intensity":
		cfg.Output.Intensity = intensityDefault
	case "update.run_doctor":
		cfg.Update.RunDoctor = runDoctorDefault
	}
	return nil
}

// WritePersist atomically writes cfg to path as TOML.
// Creates the parent directory if it does not exist.
// Uses write-to-tmp + rename for interrupt safety.
func WritePersist(path string, cfg *Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("config: mkdir %s: %w", filepath.Dir(path), err)
	}

	data, err := toml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("config: marshal: %w", err)
	}

	// Write to a temp file in the same directory to ensure same filesystem.
	tmp, err := os.CreateTemp(filepath.Dir(path), ".config-*.toml.tmp")
	if err != nil {
		return fmt.Errorf("config: create temp: %w", err)
	}
	tmpName := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("config: write temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("config: close temp: %w", err)
	}

	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("config: rename to %s: %w", path, err)
	}
	return nil
}

// isKnownKey reports whether dottedKey is in the known-keys list.
func isKnownKey(dottedKey string) bool {
	for _, k := range knownKeys {
		if k == dottedKey {
			return true
		}
	}
	return false
}

// nearMatch returns the element from candidates with Levenshtein distance ≤ 2
// to target, or "" if none qualify. When multiple qualify, returns the closest.
func nearMatch(target string, candidates []string) string {
	best := ""
	bestDist := 3 // threshold: only return if dist ≤ 2
	for _, c := range candidates {
		d := levenshtein(target, c)
		if d < bestDist {
			bestDist = d
			best = c
		}
	}
	return best
}

// levenshtein computes the edit distance between two strings.
func levenshtein(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	la, lb := len(ra), len(rb)
	if la == 0 {
		return lb
	}
	if lb == 0 {
		return la
	}

	prev := make([]int, lb+1)
	curr := make([]int, lb+1)
	for j := 0; j <= lb; j++ {
		prev[j] = j
	}
	for i := 1; i <= la; i++ {
		curr[0] = i
		for j := 1; j <= lb; j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			curr[j] = min3(
				curr[j-1]+1,
				prev[j]+1,
				prev[j-1]+cost,
			)
		}
		prev, curr = curr, prev
	}
	return prev[lb]
}

func min3(a, b, c int) int {
	if a < b {
		if a < c {
			return a
		}
		return c
	}
	if b < c {
		return b
	}
	return c
}

// sortedKeys returns the keys of a map[string]bool in sorted order.
func sortedKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	// Simple insertion sort; small slice.
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j] < keys[j-1]; j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	return keys
}
