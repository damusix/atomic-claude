package config

import "fmt"

// Resolved returns a flat dotted-key → resolved-value map.
// Built-in defaults are filled in for any zero-value fields.
func Resolved(cfg *Config) map[string]string {
	// output.signals.max_depth: int zero-value (0) means "use default".
	// It also doubles as the zero-value-Config signal below.
	maxDepth := cfg.Output.Signals.MaxDepth
	zeroValueConfig := maxDepth <= 0
	if maxDepth <= 0 {
		maxDepth = signalsMaxDepthDefault
	}
	// For update.run_doctor: bool's zero value (false) is indistinguishable
	// from an explicit false without a sentinel. A literal zero-value Config
	// (e.g. &Config{}) has MaxDepth == 0; one produced by Default()/Load()
	// always has MaxDepth > 0 (absent → backfilled to the default, explicit
	// non-positive → rejected by Validate). So MaxDepth <= 0 means "zero-value
	// Config", in which case apply the run_doctor default too. A Default()/Load()
	// Config with RunDoctor == false is intentional and preserved.
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
	// harness.dir: an empty string is never a valid explicit value (see
	// Load's backfill comment), so it unambiguously means "use default".
	harnessDirVal := cfg.Harness.Dir
	if harnessDirVal == "" {
		harnessDirVal = harnessDirDefault
	}
	// repl.idle_timeout: an empty string means unset — display the same
	// default resolveIdleTimeout falls back to.
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
