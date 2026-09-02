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

// replIdleTimeoutDefault is the display default shown for an unset
// repl.idle_timeout. It mirrors internal/repl.DefaultIdleTimeout, which config
// cannot import (repl already imports config), so the two are synced by hand.
const replIdleTimeoutDefault = "1h"

// runDoctorDefault is the built-in default for update.run_doctor.
const runDoctorDefault = true

// updateCheckDefault is the built-in default for update.check.
const updateCheckDefault = true

// updateStageDefault is the built-in default for update.stage.
const updateStageDefault = true

// signalsMaxDepthDefault is the built-in default for output.signals.max_depth.
const signalsMaxDepthDefault = 3

// harnessDirDefault is the built-in default for harness.dir — the repo-local
// state directory name every repo-local path helper (see harness.go) joins
// onto a project root.
const harnessDirDefault = ".claude"

// knownKeys is the user-settable leaf keys exposed via Get/Set/Unset/Resolved.
// Machine-written sections like [install] are excluded: not user-settable, and
// never shown by `atomic config list`.
var knownKeys = []string{
	"output.signals.max_depth",
	"update.run_doctor",
	"update.check",
	"update.stage",
	"update.channel",
	"harness.dir",
	"repl.idle_timeout",
}

// knownSchemaKeys is every recognized dotted key across all schema versions — a
// superset of knownKeys including machine-written sections. Used only by
// checkUnknownKeys, to avoid false-positive unknown-key warnings for them.
var knownSchemaKeys = func() []string {
	extra := []string{
		"install.version",
		"install.artifacts.agents",
		"install.artifacts.commands",
		"install.artifacts.skills",
		"install.artifacts.output-styles",
		"install.artifacts.rules",
	}
	// knownKeys[:len:len] prevents mutation of the backing array.
	return append(knownKeys[:len(knownKeys):len(knownKeys)], extra...)
}()

// opaqueSections are top-level tables whose child keys are structurally
// arbitrary. checkUnknownKeys accepts any child of one without a structural
// warning; semantic validation is left to Validate / AgentWarnings.
var opaqueSections = map[string]bool{
	"claude": true,
	"pi":     true,
}

// knownSections is the known top-level table names, derived from the full schema
// so machine-written sections do not trigger unknown-section warnings.
// opaqueSections are added so their table names are recognized too.
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
	// Check gates the background detached-child GitHub lookup. User config only,
	// no repo-scoped equivalent.
	Check bool `toml:"check"`
	// Stage gates once-only background staging of a newer release binary. User
	// config only.
	Stage bool `toml:"stage"`
	// Channel selects the release channel every update path reads: the
	// background check, the banner, `atomic update`, and doctor's binary check.
	// Empty means unset and resolves to selfupdate.ChannelStable; `atomic
	// update --pre` overrides it for one invocation without writing here.
	Channel string `toml:"channel,omitempty"`
}

// harnessSection is the [harness] TOML table.
type harnessSection struct {
	Dir string `toml:"dir"`
}

// installArtifactsSection lists, per artifact kind, the file names the last
// `atomic claude install` copied.
type installArtifactsSection struct {
	Agents       []string `toml:"agents"`
	Commands     []string `toml:"commands"`
	Skills       []string `toml:"skills"`
	OutputStyles []string `toml:"output-styles"`
	Rules        []string `toml:"rules"`
}

// installSection is written by `atomic claude install` and read by the migration
// runner and the prune logic. A missing table means the config predates the
// migration framework — valid, treated as version "0.0.0".
type installSection struct {
	Version   string                  `toml:"version"`
	Artifacts installArtifactsSection `toml:"artifacts"`
}

// knownAtomicAgents is the fallback known-agent set when
// [install.artifacts].agents is absent. Keep in sync with agents/ in the repo.
var knownAtomicAgents = map[string]bool{
	"atomic-implementer":   true,
	"atomic-investigator":  true,
	"atomic-reviewer":      true,
	"atomic-strategist":    true,
	"atomic-wiki-inferrer": true,
}

// claudeSection is the [claude] table, namespaced to mirror pi's: both harnesses
// read [<harness>.agents.<name>].
type claudeSection struct {
	// Agents maps a bundled agent filename to its model/effort override.
	// Machine-written by `atomic config agents`, re-applied at install time; not
	// user-settable via `atomic config set`. Nested-table decode only — no
	// scalar form, no migration.
	Agents map[string]AgentOverride `toml:"agents,omitempty"`
}

// Config is the parsed and defaulted configuration. Fields track explicit set
// values; zero values mean "use built-in default".
type Config struct {
	Output  outputSection  `toml:"output"`
	Update  updateSection  `toml:"update"`
	Harness harnessSection `toml:"harness"`
	// Pi preserves the opaque [pi] tree so unrelated writes do not discard Pi
	// agent overrides. ResolvePiAgents does the semantic validation.
	Pi map[string]any `toml:"pi,omitempty"`
	// Install is omitted from TOML when zero-valued (no install manifest yet).
	Install installSection `toml:"install,omitempty"`
	// Claude carries the Claude Code harness's per-agent overrides. Omitted
	// from TOML when zero-valued.
	Claude claudeSection `toml:"claude,omitempty"`
	// Repl is the user-level idle_timeout fallback, consulted by repl only when
	// the repo config has none. Empty means unset — unlike Harness.Dir it needs
	// no backfill, since repl's own DefaultIdleTimeout supplies the concrete
	// value rather than Load.
	Repl replSection `toml:"repl,omitempty"`
}

// Default returns a Config populated with built-in defaults.
func Default() *Config {
	return &Config{
		Output: outputSection{
			Signals: signalsSubSection{MaxDepth: signalsMaxDepthDefault},
		},
		Update: updateSection{
			RunDoctor: runDoctorDefault,
			Check:     updateCheckDefault,
			Stage:     updateStageDefault,
		},
		Harness: harnessSection{Dir: harnessDirDefault},
	}
}

// Load reads path into a Config leniently: unknown keys produce Warnings, not an
// error. A missing file yields Default() with no warnings.
func Load(path string) (*Config, []Warning, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Default(), nil, nil
		}
		return nil, nil, fmt.Errorf("config: read %s: %w", path, err)
	}

	// Decode into a raw map first so unknown keys can be detected.
	var rawMap map[string]any
	if err := toml.Unmarshal(raw, &rawMap); err != nil {
		return nil, nil, fmt.Errorf("config: parse %s: %w", path, err)
	}

	var warns []Warning
	warns = append(warns, checkUnknownKeys(rawMap, "")...)

	// A bool's zero value is indistinguishable from "absent" after decode, so
	// explicit presence is read from the raw map.
	updateRunDoctorExplicit := false
	updateCheckExplicit := false
	updateStageExplicit := false
	if updateRaw, ok := rawMap["update"]; ok {
		if updateTable, ok := updateRaw.(map[string]any); ok {
			if _, ok := updateTable["run_doctor"]; ok {
				updateRunDoctorExplicit = true
			}
			if _, ok := updateTable["check"]; ok {
				updateCheckExplicit = true
			}
			if _, ok := updateTable["stage"]; ok {
				updateStageExplicit = true
			}
		}
	}

	// Same for the int: 0 is indistinguishable from "absent" after decode.
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

	// Backfill only where the key was absent: an explicit false is already false,
	// and TOML decode of a missing section resets Default()'s true back to zero.
	if !updateRunDoctorExplicit {
		cfg.Update.RunDoctor = runDoctorDefault
	}
	// Same explicit-presence backfill as run_doctor.
	if !updateCheckExplicit {
		cfg.Update.Check = updateCheckDefault
	}
	if !updateStageExplicit {
		cfg.Update.Stage = updateStageDefault
	}
	// An explicit max_depth decodes as-is, even 0 or negative; Validate catches
	// that. Only an absent key is backfilled.
	if !signalsMaxDepthExplicit {
		cfg.Output.Signals.MaxDepth = signalsMaxDepthDefault
	}
	// An explicit empty harness.dir is never valid (Set/Validate reject it), so
	// there is no absent-versus-zero collision — backfill unconditionally.
	if cfg.Harness.Dir == "" {
		cfg.Harness.Dir = harnessDirDefault
	}

	return cfg, warns, nil
}

// knownLeaves is the known dotted leaf keys, from the full schema so [install]
// leaves produce no warnings.
var knownLeaves = func() map[string]bool {
	m := map[string]bool{}
	for _, k := range knownSchemaKeys {
		m[k] = true
	}
	return m
}()

// knownPrefixes is the known intermediate dotted paths — e.g. "output.signals"
// is a prefix of "output.signals.max_depth".
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

// checkUnknownKeys walks a raw decoded TOML map and warns for each key not in
// the schema. prefix is the dotted path so far.
func checkUnknownKeys(m map[string]any, prefix string) []Warning {
	var warns []Warning
	for k, v := range m {
		dotted := k
		if prefix != "" {
			dotted = prefix + "." + k
		}

		// Top level: the key must name a known section.
		if prefix == "" {
			if !knownSections[k] {
				warns = append(warns, Warning{
					Message: fmt.Sprintf("config: unknown key %q (ignored)", dotted),
				})
				continue
			}
			// Opaque sections accept arbitrary child keys, so structural checking
			// stops here; semantics are Validate / AgentWarnings' job.
			if opaqueSections[k] {
				continue
			}
		} else {
			// Nested keys accept both leaves and known intermediate prefixes: a
			// sub-table like "output.signals" must not warn.
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

// Validate returns an error if cfg holds values outside the allowed schema.
func Validate(cfg *Config) error {
	if cfg.Output.Signals.MaxDepth <= 0 {
		return fmt.Errorf("config: output.signals.max_depth must be a positive integer, got %d", cfg.Output.Signals.MaxDepth)
	}
	if err := validateHarnessDir(cfg.Harness.Dir); err != nil {
		return err
	}
	// An empty install.version is valid — it means no [install] table yet.
	if cfg.Install.Version != "" && !selfupdate.IsValidSemver(cfg.Install.Version) {
		return fmt.Errorf("config: install.version %q is not a valid semver string (e.g. \"1.2.0\")", cfg.Install.Version)
	}
	// effort is strict: any non-empty value outside the enum fails validation.
	// model is lenient and never blocks loading, and an unknown agent name is
	// only a warning — both live in AgentWarnings.
	for agentName, ov := range cfg.Claude.Agents {
		if ov.Effort != "" && !validEfforts[ov.Effort] {
			return fmt.Errorf("config: claude.agents.%s.effort: invalid effort %q; must be one of: low, medium, high, xhigh, max", agentName, ov.Effort)
		}
	}
	// Empty idle_timeout means unset; a present value must be a positive duration.
	if cfg.Repl.IdleTimeout != "" {
		if _, err := ValidateIdleTimeout(cfg.Repl.IdleTimeout); err != nil {
			return err
		}
	}
	// Empty channel means unset and resolves to stable.
	if cfg.Update.Channel != "" && !selfupdate.ValidChannel(cfg.Update.Channel) {
		return fmt.Errorf("config: update.channel %q is not one of: prerelease, stable", cfg.Update.Channel)
	}
	return nil
}

// AgentWarnings returns non-fatal warnings for [claude.agents] keys outside the
// known bundled-agent set. An unknown key does not prevent loading — the user
// may have a custom agent, or removed a bundled one. The known set comes from
// the install manifest when available, else knownAtomicAgents.
func AgentWarnings(cfg *Config) []Warning {
	if len(cfg.Claude.Agents) == 0 {
		return nil
	}

	// Prefer the install manifest over the static fallback.
	known := knownAtomicAgents
	if len(cfg.Install.Artifacts.Agents) > 0 {
		known = make(map[string]bool, len(cfg.Install.Artifacts.Agents))
		for _, fname := range cfg.Install.Artifacts.Agents {
			known[strings.TrimSuffix(fname, ".md")] = true
		}
	}

	var warns []Warning
	for agentName, ov := range cfg.Claude.Agents {
		if !known[agentName] {
			warns = append(warns, Warning{
				Message: fmt.Sprintf("config: claude.agents.%s: unknown agent (not in installed set); override stored but agent must exist at apply time", agentName),
			})
		}
		if ov.Model != "" && !validModelFormat(ov.Model) {
			warns = append(warns, Warning{
				Message: fmt.Sprintf("config: claude.agents.%s.model: questionable value %q; passed through as-is", agentName, ov.Model),
			})
		}
	}
	return warns
}

// Get returns the resolved value for a dotted key, erroring on an unknown one
// with a near-match suggestion.
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

// Set updates cfg in memory for a dotted key/value pair. Errors on an unknown
// key (with a near-match suggestion) or a value outside the allowed enum.
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
	case "update.check":
		switch value {
		case "true":
			cfg.Update.Check = true
		case "false":
			cfg.Update.Check = false
		default:
			return fmt.Errorf("config: update.check %q is not one of: false, true", value)
		}
	case "update.stage":
		switch value {
		case "true":
			cfg.Update.Stage = true
		case "false":
			cfg.Update.Stage = false
		default:
			return fmt.Errorf("config: update.stage %q is not one of: false, true", value)
		}
	case "harness.dir":
		if err := validateHarnessDir(value); err != nil {
			return err
		}
		cfg.Harness.Dir = value
	case "repl.idle_timeout":
		if _, err := ValidateIdleTimeout(value); err != nil {
			return err
		}
		cfg.Repl.IdleTimeout = value
	case "update.channel":
		if !selfupdate.ValidChannel(value) {
			return fmt.Errorf("config: update.channel %q is not one of: prerelease, stable", value)
		}
		cfg.Update.Channel = value
	}
	return nil
}

// validateHarnessDir requires a single non-empty path segment, never "." or
// "..", never containing "/".
func validateHarnessDir(value string) error {
	if value == "" || value == "." || value == ".." || strings.Contains(value, "/") {
		return fmt.Errorf("config: harness.dir must be a single non-empty path segment (not \".\", \"..\", and not containing \"/\"), got %q", value)
	}
	return nil
}

// Unset reverts a key to its built-in default, erroring on an unknown one with a
// near-match suggestion.
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
	case "update.check":
		cfg.Update.Check = updateCheckDefault
	case "update.stage":
		cfg.Update.Stage = updateStageDefault
	case "harness.dir":
		cfg.Harness.Dir = harnessDirDefault
	case "repl.idle_timeout":
		cfg.Repl.IdleTimeout = ""
	case "update.channel":
		cfg.Update.Channel = ""
	}
	return nil
}

// WritePersist writes cfg to path as TOML through write-to-tmp + rename, creating
// the parent directory if needed.
func WritePersist(path string, cfg *Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("config: mkdir %s: %w", filepath.Dir(path), err)
	}

	data, err := toml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("config: marshal: %w", err)
	}

	// Same directory so the rename stays on one filesystem.
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

// nearMatch returns the closest candidate within Levenshtein distance 2, or "".
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
