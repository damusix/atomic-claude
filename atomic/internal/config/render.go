package config

import "fmt"

// Resolved returns a flat dotted-key to resolved-value map, filling in built-in
// defaults for zero-value fields.
func Resolved(cfg *Config) map[string]string {
	// A zero max_depth means "use default", and doubles as the zero-value-Config
	// signal below.
	maxDepth := cfg.Output.Signals.MaxDepth
	zeroValueConfig := maxDepth <= 0
	if maxDepth <= 0 {
		maxDepth = signalsMaxDepthDefault
	}
	// A bool's zero value is indistinguishable from an explicit false without a
	// sentinel. A literal &Config{} has MaxDepth == 0, while anything from
	// Default()/Load() always has MaxDepth > 0, so MaxDepth <= 0 means
	// "zero-value Config" and the run_doctor default applies too. A loaded Config
	// with RunDoctor == false is intentional and preserved.
	runDoctor := cfg.Update.RunDoctor
	if !runDoctor && zeroValueConfig {
		runDoctor = runDoctorDefault
	}
	updateCheck := cfg.Update.Check
	if !updateCheck && zeroValueConfig {
		updateCheck = updateCheckDefault
	}
	updateStage := cfg.Update.Stage
	if !updateStage && zeroValueConfig {
		updateStage = updateStageDefault
	}
	// An empty harness.dir is never a valid explicit value, so it unambiguously
	// means "use default".
	harnessDirVal := cfg.Harness.Dir
	if harnessDirVal == "" {
		harnessDirVal = harnessDirDefault
	}
	// An empty idle_timeout means unset — display the same default
	// resolveIdleTimeout falls back to.
	idleTimeoutVal := cfg.Repl.IdleTimeout
	if idleTimeoutVal == "" {
		idleTimeoutVal = replIdleTimeoutDefault
	}
	return map[string]string{
		"output.signals.max_depth": fmt.Sprintf("%d", maxDepth),
		"update.run_doctor":        fmt.Sprintf("%t", runDoctor),
		"update.check":             fmt.Sprintf("%t", updateCheck),
		"update.stage":             fmt.Sprintf("%t", updateStage),
		"harness.dir":              harnessDirVal,
		"repl.idle_timeout":        idleTimeoutVal,
	}
}
