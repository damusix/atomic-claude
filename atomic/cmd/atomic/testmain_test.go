package main

import (
	"os"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/config"
)

// TestMain sandboxes every test in this package under a temp $HOME. Several
// tests resolve project-keyed state (reminders, reports) via
// config.ProjectStateDir, which defaults to the real ~/.atomic — without this
// a `go test ./...` run pollutes the operator's actual home directory with
// one stray var-folders-...-Test<name>-NNN entry per such test.
func TestMain(m *testing.M) {
	home, err := os.MkdirTemp("", "atomic-cmd-test-home")
	if err != nil {
		panic(err)
	}
	restore := config.SetHomeDirForTest(home)
	code := m.Run()
	restore()
	os.RemoveAll(home)
	os.Exit(code)
}
