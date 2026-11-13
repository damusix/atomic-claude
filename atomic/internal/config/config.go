package config

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
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

// outputStyleSeedDefault is the built-in default for output_style.seed.
const outputStyleSeedDefault = true

// updateCheckDefault is the built-in default for update.check.
const updateCheckDefault = true

// updateStageDefault is the built-in default for update.stage.
const updateStageDefault = true

// signalsMaxDepthDefault is the built-in default for output.signals.max_depth.
const signalsMaxDepthDefault = 3

// knownKeys is the user-settable leaf keys exposed via Get/Set/Unset/Resolved.
// Machine-written sections like [install] are excluded: not user-settable, and
// never shown by `atomic config list`.
var knownKeys = []string{
	"output.signals.max_depth",
	"update.run_doctor",
	"update.check",
	"update.stage",
	"update.channel",
	"state.dir",
	"repl.idle_timeout",
	"output_style.seed",
	"steering.claude",
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
// warning; semantic validation is left to the section's reader.
var opaqueSections = map[string]bool{
	"bus": true,
	// harness and pi are retired tables: a stored one loads silently and the
	// next write drops it.
	"harness": true,
	"pi":      true,
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
	for _, k := range AgentHarnesses {
		m[k] = true
	}
	return m
}()

// agentTables are the `<harness>.agents` tables: their children are agent
// names, which AgentWarnings checks instead of the schema.
var agentTables = func() map[string]bool {
	m := map[string]bool{}
	for _, k := range AgentHarnesses {
		m[k+".agents"] = true
	}
	return m
}()

// Warning carries a non-fatal diagnostic from Load.
type Warning struct {
	Message string
	// Key is the dotted config key an unknown-key warning names; empty otherwise.
	Key string
	// Harness is the kind whose agent override an AgentWarnings entry names.
	Harness string
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

// stateSection is the [state] TOML table.
type stateSection struct {
	// Dir is a relative path of safe segments under each repository root.
	// Empty means unset: the ladder falls back to `.atomic`.
	Dir string `toml:"dir,omitempty"`
}

// outputStyleSection is the [output_style] TOML table.
type outputStyleSection struct {
	Seed bool `toml:"seed"`
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
	// Adopted records that a ledger adoption took over this legacy record.
	Adopted bool `toml:"adopted,omitempty"`
}

// BusRemote is one [bus.remotes.<name>] entry: a gateway reachable with
// `--host <name>`. CA may start with "~/".
type BusRemote struct {
	Host string `toml:"host"`
	Key  string `toml:"key"`
	CA   string `toml:"ca,omitempty"`
}

type busSection struct {
	Remotes map[string]BusRemote `toml:"remotes,omitempty"`
}

// Config is the parsed and defaulted configuration. Fields track explicit set
// values; zero values mean "use built-in default".
type Config struct {
	Output      outputSection      `toml:"output"`
	Update      updateSection      `toml:"update"`
	State       stateSection       `toml:"state,omitempty"`
	OutputStyle outputStyleSection `toml:"output_style"`
	// Install is omitted from TOML when zero-valued (no install manifest yet).
	Install installSection `toml:"install,omitempty"`
	// Claude, OMP, and Codex carry each harness's per-agent overrides, written
	// by `atomic config agents` and never by `atomic config set`.
	Claude agentsSection `toml:"claude,omitempty"`
	OMP    agentsSection `toml:"omp,omitempty"`
	Codex  agentsSection `toml:"codex,omitempty"`
	// Repl is the user-level idle_timeout fallback, consulted by repl only when
	// the repo config has none. Empty means unset; no backfill, since repl's
	// DefaultIdleTimeout supplies the value.
	Repl replSection `toml:"repl,omitempty"`
	// Steering.Claude is the user-level default Claude steering mode; empty means
	// unset and resolves to loader.
	Steering steeringSection `toml:"steering,omitempty"`
	// Bus is modeled so writes that round-trip the file keep [bus.remotes].
	Bus busSection `toml:"bus,omitempty"`
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
		OutputStyle: outputStyleSection{Seed: outputStyleSeedDefault},
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

	if err := checkAgentTableShapes(rawMap); err != nil {
		return nil, nil, fmt.Errorf("%w (in %s)", err, path)
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

	// Same explicit-presence backfill as run_doctor.
	outputStyleSeedExplicit := false
	if outputStyleRaw, ok := rawMap["output_style"]; ok {
		if outputStyleTable, ok := outputStyleRaw.(map[string]any); ok {
			if _, ok := outputStyleTable["seed"]; ok {
				outputStyleSeedExplicit = true
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
	if !outputStyleSeedExplicit {
		cfg.OutputStyle.Seed = outputStyleSeedDefault
	}
	// An explicit max_depth decodes as-is, even 0 or negative; Validate catches
	// that. Only an absent key is backfilled.
	if !signalsMaxDepthExplicit {
		cfg.Output.Signals.MaxDepth = signalsMaxDepthDefault
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
					Key:     dotted,
				})
				continue
			}
			// Opaque sections accept arbitrary child keys, so structural checking
			// stops here; semantics are Validate / AgentWarnings' job.
			if opaqueSections[k] {
				continue
			}
		} else {
			if agentTables[dotted] {
				continue
			}
			// Nested keys accept both leaves and known intermediate prefixes: a
			// sub-table like "output.signals" must not warn.
			if !knownLeaves[dotted] && !knownPrefixes[dotted] {
				warns = append(warns, Warning{
					Message: fmt.Sprintf("config: unknown key %q (ignored)", dotted),
					Key:     dotted,
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
	// Empty state.dir means unset — the neutral ladder falls through.
	if cfg.State.Dir != "" {
		if err := ValidateStateDirPath(cfg.State.Dir); err != nil {
			return err
		}
	}
	// An empty install.version is valid — it means no [install] table yet.
	if cfg.Install.Version != "" && !selfupdate.IsValidSemver(cfg.Install.Version) {
		return fmt.Errorf("config: install.version %q is not a valid semver string (e.g. \"1.2.0\")", cfg.Install.Version)
	}
	if err := ValidateClaudeAgentOverrides(cfg.Claude.Agents); err != nil {
		return err
	}
	// Empty idle_timeout means unset; a present value must be a positive duration.
	if cfg.Repl.IdleTimeout != "" {
		if _, err := ValidateIdleTimeout(cfg.Repl.IdleTimeout); err != nil {
			return err
		}
	}
	if err := ValidateSteeringMode(cfg.Steering.Claude); err != nil {
		return err
	}
	// Empty channel means unset and resolves to stable.
	if cfg.Update.Channel != "" && !selfupdate.ValidChannel(cfg.Update.Channel) {
		return fmt.Errorf("config: update.channel %q is not one of: prerelease, stable", cfg.Update.Channel)
	}
	return nil
}

// AgentWarnings flags unknown agents, malformed models, and unrecognized OMP or Codex efforts.
// Claude's known agents come from the install manifest when recorded, else the embedded bundle.
func AgentWarnings(cfg *Config) []Warning {
	bundled := map[string]bool{}
	for _, name := range bundledAgents() {
		bundled[name] = true
	}

	var warns []Warning
	for _, kind := range AgentHarnesses {
		known := bundled
		if kind == "claude" && len(cfg.Install.Artifacts.Agents) > 0 {
			known = map[string]bool{}
			for _, fname := range cfg.Install.Artifacts.Agents {
				known[strings.TrimSuffix(path.Base(fname), ".md")] = true
			}
		}
		overrides := cfg.AgentOverrides(kind)
		for _, agentName := range slices.Sorted(maps.Keys(overrides)) {
			ov := overrides[agentName]
			if !known[agentName] {
				warns = append(warns, Warning{
					Message: fmt.Sprintf("config: %s.agents.%s: unknown agent (not in installed set); override stored but agent must exist at apply time", kind, agentName),
					Harness: kind,
				})
			}
			if ov.Model != "" && !validModelFormat(ov.Model) {
				warns = append(warns, Warning{
					Message: fmt.Sprintf("config: %s.agents.%s.model: questionable value %q; passed through as-is", kind, agentName, ov.Model),
					Harness: kind,
				})
			}
			if levels, ok := advisoryEfforts[kind]; ok && ov.Effort != "" && !slices.Contains(levels, ov.Effort) {
				warns = append(warns, Warning{
					Message: fmt.Sprintf("config: %s.agents.%s.effort: unrecognized effort %q; passed through as-is", kind, agentName, ov.Effort),
					Harness: kind,
				})
			}
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
		return unknownKeyError(dottedKey)
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
	case "output_style.seed":
		switch value {
		case "true":
			cfg.OutputStyle.Seed = true
		case "false":
			cfg.OutputStyle.Seed = false
		default:
			return fmt.Errorf("config: output_style.seed %q is not one of: false, true", value)
		}
	case "state.dir":
		if err := ValidateStateDirPath(value); err != nil {
			return err
		}
		cfg.State.Dir = value
	case "repl.idle_timeout":
		if _, err := ValidateIdleTimeout(value); err != nil {
			return err
		}
		cfg.Repl.IdleTimeout = value
	case "steering.claude":
		if value == "" {
			return fmt.Errorf("config: steering.claude must not be empty; use `atomic config unset steering.claude`")
		}
		if err := ValidateSteeringMode(value); err != nil {
			return err
		}
		cfg.Steering.Claude = value
	case "update.channel":
		if !selfupdate.ValidChannel(value) {
			return fmt.Errorf("config: update.channel %q is not one of: prerelease, stable", value)
		}
		cfg.Update.Channel = value
	}
	return nil
}

// Unset reverts a key to its built-in default, erroring on an unknown one with a
// near-match suggestion.
func Unset(cfg *Config, dottedKey string) error {
	if !isKnownKey(dottedKey) {
		return unknownKeyError(dottedKey)
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
	case "output_style.seed":
		cfg.OutputStyle.Seed = outputStyleSeedDefault
	case "state.dir":
		cfg.State.Dir = ""
	case "repl.idle_timeout":
		cfg.Repl.IdleTimeout = ""
	case "steering.claude":
		cfg.Steering.Claude = ""
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

// unknownKeyError refuses a key set and unset cannot write, naming
// `atomic config agents` for agent overrides and a near match otherwise.
func unknownKeyError(dottedKey string) error {
	for _, harness := range AgentHarnesses {
		if strings.HasPrefix(dottedKey, harness+".agents.") {
			return fmt.Errorf("config: %s.agents.* keys are written by `atomic config agents --harness %s`", harness, harness)
		}
	}
	if suggestion := nearMatch(dottedKey, knownKeys); suggestion != "" {
		return fmt.Errorf("config: unknown key %q; did you mean %q?", dottedKey, suggestion)
	}
	return fmt.Errorf("config: unknown key %q", dottedKey)
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
