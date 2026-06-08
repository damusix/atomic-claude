package indexer_test

import (
	"os/exec"
)

// buildOSCmd creates an *exec.Cmd with the given dir and args.
// This is extracted so both runCmd and runCmdBytes can share the construction.
func buildOSCmd(dir, name string, args ...string) *exec.Cmd {
	c := exec.Command(name, args...)
	c.Dir = dir
	return c
}
