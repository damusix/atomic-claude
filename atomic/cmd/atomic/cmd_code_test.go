package main

import (
	"fmt"
	codemcp "github.com/damusix/atomic-claude/atomic/internal/codeintel/mcp"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestCodeMCPDaemonArgv_MatchesCobraTree is the regression
// test: DefaultSpawn's argv (codemcp.DaemonArgv) must always name a
// Cobra-registered command, or the spawned daemon subprocess dies on
// "unknown flag" before its handler ever runs — the only symptom being
// "daemon did not start within 10s" at the proxy. Driving DaemonArgv's own
// output through the real root command (rather than hand-writing the argv
// here) means a future drift between DefaultSpawn and the Cobra tree fails
// this test instead of silently reintroducing the bug.
//
// runCode calls os.Exit, so the dispatch is exercised in a subprocess (the
// standard Go idiom for os.Exit-calling code), matching
// TestRunBus_DispatchUsesRealHomeFromEnv's established pattern.
func TestCodeMCPDaemonArgv_MatchesCobraTree(t *testing.T) {
	if os.Getenv("ATOMIC_TEST_CODE_MCP_DAEMON_ARGV_HELPER") == "1" {
		var repoOverride string
		root := buildRootCmd(&repoOverride)
		root.SetArgs(codemcp.DaemonArgv(os.Getenv("ATOMIC_TEST_MCP_SRC"), os.Getenv("ATOMIC_TEST_MCP_DB"), codemcp.WatchOptions{}))
		if err := root.Execute(); err != nil {
			// Mirrors main()'s own Execute() error handling so the subprocess's
			// stderr/exit code match what a real invocation would produce.
			fmt.Fprintf(os.Stderr, "atomic: %v\n", err)
			os.Exit(1)
		}
		return
	}

	// dbPath's parent path component is a regular file, not a directory: the
	// daemon handler's own MkdirAll fails fast and deterministically (no
	// accept loop ever starts, no socket ever binds), giving a handler-level
	// error to assert against instead of racing a real daemon's lifetime.
	tmp := t.TempDir()
	blocker := filepath.Join(tmp, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatalf("seed blocker file: %v", err)
	}
	dbPath := filepath.Join(blocker, "sub", "atomic.db")
	srcDir := t.TempDir()

	cmd := exec.Command(os.Args[0], "-test.run=TestCodeMCPDaemonArgv_MatchesCobraTree")
	cmd.Env = append(os.Environ(),
		"ATOMIC_TEST_CODE_MCP_DAEMON_ARGV_HELPER=1",
		"ATOMIC_TEST_MCP_SRC="+srcDir,
		"ATOMIC_TEST_MCP_DB="+dbPath,
	)
	out, err := cmd.CombinedOutput()
	output := string(out)

	if strings.Contains(output, "unknown flag") {
		t.Fatalf("DaemonArgv produced args Cobra could not parse (spawn/cobra-tree drift):\n%s", output)
	}
	if err == nil {
		t.Fatalf("expected the daemon handler to fail on an unusable db path, got success:\n%s", output)
	}
	if !strings.Contains(output, "atomic code mcp:") {
		t.Errorf("expected the handler's own \"atomic code mcp:\" error prefix, got:\n%s", output)
	}
}
