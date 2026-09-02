package doctor

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/selfupdate"
	"github.com/damusix/atomic-claude/atomic/internal/version"
)

const binaryCheckTimeout = 5 * time.Second

// binaryLookupFn reports (isNewer, latestTag, err). Tests override it to keep
// the check off the network.
var binaryLookupFn = defaultBinaryLookup

// binaryChannelFn resolves the channel the check reports against. Pinned to the
// configured channel so a machine tracking prereleases is not told it is on the
// latest stable release. Tests override it to avoid reading the real config.
var binaryChannelFn = defaultBinaryChannel

func defaultBinaryLookup(channel string) (bool, string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), binaryCheckTimeout)
	defer cancel()
	c := &selfupdate.Client{}
	return c.Check(ctx, channel, version.Version)
}

func defaultBinaryChannel() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return selfupdate.ChannelStable
	}
	cfg, _, err := config.Load(config.TOMLPath(home))
	if err != nil || !selfupdate.ValidChannel(cfg.Update.Channel) {
		return selfupdate.ChannelStable
	}
	return cfg.Update.Channel
}

// checkBinary implements category 8: binary self-check. An available update
// WARNs, and so does a failed lookup — an offline machine must not break the
// doctor run, so this never FAILs.
func checkBinary(_ Opts) Result {
	return RunCheckBinaryWith(binaryLookupFn, version.Version, binaryChannelFn())
}

// RunCheckBinaryWith runs the binary check using the provided lookup function,
// current version string, and channel. Exported for testing.
func RunCheckBinaryWith(lookup func(channel string) (bool, string, error), current, channel string) Result {
	available, latest, err := lookup(channel)
	if err != nil {
		return Result{
			Severity: WARN,
			Detail:   fmt.Sprintf("update check failed: %v", err),
		}
	}
	if available {
		// On the prerelease channel the tip can be semver-lower than what is
		// running, so "<" would misstate the relationship.
		if channel == selfupdate.ChannelPrerelease && !selfupdate.IsNewer(current, latest) {
			return Result{
				Severity: WARN,
				Detail:   fmt.Sprintf("%s available on %s (current: %s)", latest, channel, current),
			}
		}
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
