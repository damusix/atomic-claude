// Package version exposes the binary version and commit SHA.
// Both values default to sentinel strings and can be overridden at build time
// via -ldflags:
//
//	go build -ldflags "-X github.com/damusix/atomic-claude/atomic/internal/version.Version=v0.1.0 \
//	                   -X github.com/damusix/atomic-claude/atomic/internal/version.Commit=abc1234" \
//	    ./cmd/atomic
package version

// Version is the semver string for this build. Override with -ldflags.
var Version = "dev"

// Commit is the git commit SHA for this build. Override with -ldflags.
var Commit = "unknown"
