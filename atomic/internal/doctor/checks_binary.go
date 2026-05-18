package doctor

import (
	"context"
	"fmt"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/selfupdate"
	"github.com/damusix/atomic-claude/atomic/internal/version"
)

const (
	binaryCheckChannel = "stable"
	binaryCheckTimeout = 5 * time.Second
)

// binaryLookupFn is the injectable function for check 8. Defaults to the real
// selfupdate.Client.Check call. Tests override it to avoid network calls.
//
// Signature: func(channel string) (isNewer bool, latestTag string, err error)
var binaryLookupFn = defaultBinaryLookup

func defaultBinaryLookup(channel string) (bool, string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), binaryCheckTimeout)
	defer cancel()
	c := &selfupdate.Client{}
	return c.Check(ctx, channel, version.Version)
}

// checkBinary implements category 8: binary self-check.
//
// Calls binaryLookupFn (default: selfupdate.Client.Check with a 5s timeout).
// Maps result:
//   - up-to-date  → PASS with "v<tag> (latest)"
//   - newer avail → WARN with "v<current> < v<latest> available"
//   - error       → WARN with "update check failed: <err>"  (never FAIL;
//     offline machines must not break doctor)
func checkBinary(_ Opts) Result {
	return RunCheckBinaryWith(binaryLookupFn, version.Version)
}

// RunCheckBinaryWith runs the binary check using the provided lookup function
// and current version string. Exported for testing.
func RunCheckBinaryWith(lookup func(channel string) (bool, string, error), current string) Result {
	newer, latest, err := lookup(binaryCheckChannel)
	if err != nil {
		return Result{
			Severity: WARN,
			Detail:   fmt.Sprintf("update check failed: %v", err),
		}
	}
	if newer {
		return Result{
			Severity: WARN,
			Detail:   fmt.Sprintf("%s < %s available", current, latest),
		}
	}
	return Result{
		Severity: PASS,
		Detail:   fmt.Sprintf("%s (latest)", latest),
	}
}
