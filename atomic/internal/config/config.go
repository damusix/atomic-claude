package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/selfupdate"
	"github.com/pelletier/go-toml/v2"
)

// runDoctorDefault is the built-in default for update.run_doctor.
const runDoctorDefault = true

// signalsMaxDepthDefault is the built-in default for output.signals.max_depth.
const signalsMaxDepthDefault = 3

// knownKeys is the list of user-settable leaf keys exposed via Get/Set/Unset/Resolved.
// Machine-written sections (e.g. [install]) are NOT included here — they are not
// user-settable via `atomic config set` and do not appear in `atomic config list`.
var knownKeys = []string{
	"output.signals.max_depth",
	"update.run_doctor",
}

// knownSchemaKeys is the exhaustive set of recognized dotted keys across all
// schema versions. It is a superset of knownKeys: machine-written sections like
// [install] (written by atomic claude install, C3+) are valid TOML but are NOT
// user-settable. knownSchemaKeys is used only by checkUnknownKeys to avoid
// producing false-positive unknown-key warnings for these fields.
var knownSchemaKeys = func() []string {
	extra := []string{
		"install.version",
		"install.artifacts.agents",
		"install.artifacts.commands",
		"install.artifacts.skills",
		"install.artifacts.output-styles",
		"install.artifacts.rules",
	}
	// Safe append: knownKeys[:len:len] prevents mutation of the backing array.
	return append(knownKeys[:len(knownKeys):len(knownKeys)], extra...)
}()

// opaqueSections is the set of top-level TOML table names whose child keys are
// structurally arbitrary (any string key is valid). checkUnknownKeys accepts
// any child key of an opaque section without producing a structural warning;
// semantic validation (value allowlist, known-key check) is left to Validate /
// AgentWarnings.
var opaqueSections = map[string]bool{
	"agents": true,
}

// knownSections is the set of known top-level TOML table names.
// Derived once from knownSchemaKeys (full schema, not just settable keys) so that
// machine-written sections like [install] don't trigger unknown-section warnings.
// opaqueSections are also included so their top-level table names are recognized.
var knownSections = func() map[string]bool {
	m := map[string]bool{}
	for _, k := range knownSchemaKeys {
		if dot := strings.IndexByte(k, '.'); dot > 0 {
			m[k[:dot]] = true
		}
	}
	// Opaque sections have arbitrary child keys; add them explicitly so
	// checkUnknownKeys recognizes the top-level table name without warning.
	for k := range opaqueSections {
		m[k] = true
	}
	return m
}()

// Warning carries a non-fatal diagnostic from Load.
type Warning struct {
	Message string
}

func (w Warning) Error() string { return w.Message }

// signalsSubSection is the [output.signals] TOML sub-table.
type signalsSubSection struct {
	MaxDepth int `toml:"max_depth"`
}

// outputSection is the [output] TOML table.
type outputSection struct {
	Signals signalsSubSection `toml:"signals"`
}

// updateSection is the [update] TOML table.
type updateSection struct {
	RunDoctor bool `toml:"run_doctor"`
}

// installArtifactsSection is the [install.artifacts] TOML sub-table.
// Each field is the list of artifact file names (relative to their kind directory)
// that were copied by the last `atomic claude install` invocation.
type installArtifactsSection struct {
	Agents       []string `toml:"agents"`
	Commands     []string `toml:"commands"`
	Skills       []string `toml:"skills"`
	OutputStyles []string `toml:"output-styles"`
	Rules        []string `toml:"rules"`
}

// installSection is the [install] TOML table (schema v2).
// It is written by atomic claude install (C3) and read by the migration
// runner (C4) and the prune logic (C3). A missing [install] table means the
// config was written before the migration framework existed (pre-framework
// install) — this is valid and treated as version "0.0.0".
type installSection struct {
	Version   string                  `toml:"version"`
	Artifacts installArtifactsSection `toml:"artifacts"`
}

// validTiers is the allowlist of model tier values for [agents] overrides.
// "fable" is forward-reserved and may not yet correspond to a recognized Claude Code
// model tier at runtime, but is allowlisted to avoid validation churn when it lands.
var validTiers = map[string]bool{
	"haiku":  true,
	"sonnet": true,
	"opus":   true,
	"fable":  true, // forward-reserved; may not be a live Claude Code model tier yet
}

// knownAtomicAgents is the static set of bundled atomic agent filenames (no .md suffix).
// Used as the fallback known-agent set when [install.artifacts].agents is absent.
// Must stay in sync with the agent files shipped under agents/ in the repo.
var knownAtomicAgents = map[string]bool{
	"atomic-implementer":   true,
	"atomic-investigator":  true,
	"atomic-reviewer":      true,
	"atomic-strategist":    true,
	"atomic-wiki-inferrer": true,
}

// Config is the parsed + defaulted configuration.
// Fields track explicit set values; zero values mean "use built-in default".
type Config struct {
	Output outputSection `toml:"output"`
	Update updateSection `toml:"update"`
	// Install is omitted from TOML when zero-valued (no install manifest yet).
	Install installSection `toml:"install,omitempty"`
	// Agents maps bundled agent filenames (no .md suffix) to model tier strings.
	// Machine-written by `atomic config agents` (CP3); re-applied at install time (CP4).
	// Omitted from TOML when empty. NOT in knownKeys — not user-settable via `atomic config set`.
	Agents map[string]string `toml:"agents,omitempty"`
}

// Default returns a Config populated with built-in defaults.
func Default() *Config {
	return &Config{
		Output: outputSection{
			Signals: signalsSubSection{MaxDepth: signalsMaxDepthDefault},
		},
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

	// Detect explicit presence of output.signals.max_depth before decoding into
	// the typed struct. The int zero-value (0) is indistinguishable from
	// "absent" after decode, so we check the raw map here.
	signalsMaxDepthExplicit := false
	if outputRaw, ok := rawMap["output"]; ok {
		if outputTable, ok := outputRaw.(map[string]any); ok {
			if signalsRaw, ok := outputTable["signals"]; ok {
				if signalsTable, ok := signalsRaw.(map[string]any); ok {
					if _, ok := signalsTable["max_depth"]; ok {
						signalsMaxDepthExplicit = true
					}
				}
			}
		}
	}

	// Decode into the typed struct (strict fields only).
	cfg := Default()
	if err := toml.Unmarshal(raw, cfg); err != nil {
		return nil, warns, fmt.Errorf("config: decode %s: %w", path, err)
	}

	// Backfill defaults for any zero-value fields.
	// update.run_doctor: only backfill default when the key was absent.
	// When explicitly set to false, the decoded value is already false — correct.
	// When absent, Default() already set it to true; TOML decode of a missing
	// section resets the struct to zero (false), so we must restore the default.
	if !updateRunDoctorExplicit {
		cfg.Update.RunDoctor = runDoctorDefault
	}
	// output.signals.max_depth: only backfill default when the key was absent.
	// When explicitly set, it is decoded as-is (even 0 or negative); Validate
	// will catch non-positive values. When absent, restore the default.
	if !signalsMaxDepthExplicit {
		cfg.Output.Signals.MaxDepth = signalsMaxDepthDefault
	}

	return cfg, warns, nil
}

// knownLeaves is the set of known dotted leaf keys, computed once from the full
// schema (knownSchemaKeys) so that [install] leaf keys don't produce warnings.
var knownLeaves = func() map[string]bool {
	m := map[string]bool{}
	for _, k := range knownSchemaKeys {
		m[k] = true
	}
	return m
}()

// knownPrefixes is the set of known intermediate dotted paths (non-leaf sections),
// computed once from the full schema. Example: "output.signals" is a prefix of
// "output.signals.max_depth"; "install.artifacts" is a prefix of "install.artifacts.agents".
var knownPrefixes = func() map[string]bool {
	m := map[string]bool{}
	for _, k := range knownSchemaKeys {
		for i := 0; i < len(k); i++ {
			if k[i] == '.' {
				prefix := k[:i]
				if !m[prefix] {
					m[prefix] = true
				}
			}
		}
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
			// Opaque sections (e.g. [agents]) accept arbitrary child keys.
			// Do not recurse — structural checking is skipped for their children.
			// Semantic validation (value allowlist, known-key check) is in Validate / AgentWarnings.
			if opaqueSections[k] {
				continue
			}
		} else {
			// For nested keys, accept both leaf keys and known intermediate prefixes.
			// knownPrefixes covers cases like "output.signals" which is a sub-table,
			// not a leaf, but must not produce a false-positive warning.
			if !knownLeaves[dotted] && !knownPrefixes[dotted] {
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
	if cfg.Output.Signals.MaxDepth <= 0 {
		return fmt.Errorf("config: output.signals.max_depth must be a positive integer, got %d", cfg.Output.Signals.MaxDepth)
	}
	// install.version must be a parseable semver when present.
	// An empty string is valid — it means no [install] table yet (pre-framework install).
	if cfg.Install.Version != "" && !selfupdate.IsValidSemver(cfg.Install.Version) {
		return fmt.Errorf("config: install.version %q is not a valid semver string (e.g. \"1.2.0\")", cfg.Install.Version)
	}
	// [agents]: any value outside the tier allowlist is a hard validation failure.
	// A key that is not a known agent name is a non-fatal warning (see AgentWarnings).
	for agentName, tier := range cfg.Agents {
		if !validTiers[tier] {
			return fmt.Errorf("config: agents.%s: invalid tier %q; must be one of: haiku, sonnet, opus, fable", agentName, tier)
		}
	}
	return nil
}

// AgentWarnings returns non-fatal warnings for [agents] keys that are not in the
// known bundled-agent set. An unknown key does not prevent loading or rendering —
// the user may have a custom agent or may have removed a bundled one.
//
// The known-agent set is derived from cfg.Install.Artifacts.Agents (the install
// manifest, filenames including .md suffix) when available; otherwise falls back
// to knownAtomicAgents (the static set of the 5 shipped atomic-* agents).
func AgentWarnings(cfg *Config) []Warning {
	if len(cfg.Agents) == 0 {
		return nil
	}

	// Derive the known-agent set: prefer the install manifest, fall back to static.
	known := knownAtomicAgents
	if len(cfg.Install.Artifacts.Agents) > 0 {
		known = make(map[string]bool, len(cfg.Install.Artifacts.Agents))
		for _, fname := range cfg.Install.Artifacts.Agents {
			known[strings.TrimSuffix(fname, ".md")] = true
		}
	}

	var warns []Warning
	for agentName := range cfg.Agents {
		if !known[agentName] {
			warns = append(warns, Warning{
				Message: fmt.Sprintf("config: agents.%s: unknown agent (not in installed set); tier override stored but agent must exist at apply time", agentName),
			})
		}
	}
	return warns
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
	case "output.signals.max_depth":
		var n int
		if _, err := fmt.Sscanf(value, "%d", &n); err != nil || n <= 0 {
			return fmt.Errorf("config: output.signals.max_depth must be a positive integer, got %q", value)
		}
		cfg.Output.Signals.MaxDepth = n
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
	case "output.signals.max_depth":
		cfg.Output.Signals.MaxDepth = signalsMaxDepthDefault
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
