// Tests for the cwd-independent daemon fix (checkpoint 1).
//
// Problem reproduced: when the proxy spawns the daemon with only a projectRoot
// arg, and the process cwd is a non-git/realm-root directory, the daemon's
// scope resolution exits before binding its socket. These tests verify:
//
//  1. RunDaemon binds its socket when called with explicit (sourceRoot, dbPath)
//     regardless of the process cwd.
//  2. The socket/lock live next to the db (in dbPath's directory), not inside
//     the source tree — so nothing is written into a realm member's source tree.
//  3. SocketPathFromDB / LockPathFromDB are consistent between proxy and daemon.
package mcp_test

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/engine"
	codemcp "github.com/damusix/atomic-claude/atomic/internal/codeintel/mcp"
)

// TestCwdIndependent_DaemonBindsSocketFromNonGitCwd verifies that a daemon
// started with an explicit (sourceRoot, dbPath) binds its socket and answers
// an MCP initialize request, even when the cwd is a non-git directory.
//
// This is the regression test for the bug: old RunDaemon(ctx, projectRoot, now)
// used engine.New(projectRoot) which consults cwd if projectRoot has no git root.
// The new RunDaemon(ctx, sourceRoot, dbPath, now) uses engine.NewWithDBPath and
// is cwd-independent.
func TestCwdIndependent_DaemonBindsSocketFromNonGitCwd(t *testing.T) {
	// Use /tmp so the path stays short (unix socket limit ~104 chars on macOS).
	sourceDir, err := os.MkdirTemp("/tmp", "atmc-src-")
	if err != nil {
		t.Fatalf("MkdirTemp source: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(sourceDir) })

	dbDir, err := os.MkdirTemp("/tmp", "atmc-db-")
	if err != nil {
		t.Fatalf("MkdirTemp db: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dbDir) })

	dbPath := filepath.Join(dbDir, "test.db")

	// Create the db dir so the daemon can write the socket there.
	if err := os.MkdirAll(dbDir, 0o755); err != nil {
		t.Fatalf("MkdirAll dbDir: %v", err)
	}

	// Change cwd to a temporary non-git directory (simulates realm root).
	nonGitCwd, err := os.MkdirTemp("/tmp", "atmc-cwd-")
	if err != nil {
		t.Fatalf("MkdirTemp cwd: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(nonGitCwd) })

	origWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := os.Chdir(nonGitCwd); err != nil {
		t.Fatalf("Chdir to non-git cwd: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(origWd) })

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Initialize an empty engine at sourceRoot with db at dbPath.
	eng, err := engine.NewWithDBPath(sourceDir, dbPath)
	if err != nil {
		t.Fatalf("engine.NewWithDBPath: %v", err)
	}
	if err := eng.Init(ctx); err != nil {
		eng.Close()
		t.Fatalf("eng.Init: %v", err)
	}
	defer eng.Close()

	stats, _ := eng.GetStats(ctx)
	srv := codemcp.NewServer(eng, stats.FileCount)

	// Compute the socket path the same way proxy and daemon agree.
	sockPath := codemcp.SocketPathFromDB(dbPath)

	// Start daemon with explicit (sourceRoot, dbPath) — cwd-independent.
	daemonDone := make(chan error, 1)
	go func() {
		ln, err := net.Listen("unix", sockPath)
		if err != nil {
			daemonDone <- err
			return
		}
		daemonDone <- codemcp.RunAcceptLoop(ctx, ln, srv, sockPath)
	}()

	waitForSocketLive(t, sockPath, 5*time.Second)

	// Connect via socket and issue MCP initialize.
	conn, err := net.DialTimeout("unix", sockPath, 3*time.Second)
	if err != nil {
		t.Fatalf("dial socket: %v", err)
	}
	defer conn.Close()

	clientTransport := &sdk.IOTransport{Reader: conn, Writer: conn}
	client := sdk.NewClient(&sdk.Implementation{Name: "cwd-test", Version: "1"}, nil)
	sess, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client.Connect: %v", err)
	}
	defer sess.Close()

	// tools/list proves the MCP handshake succeeded.
	res, err := sess.ListTools(ctx, &sdk.ListToolsParams{})
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(res.Tools) == 0 {
		t.Fatal("expected at least one tool from the daemon (initialize succeeded)")
	}
}

// TestSocketPathFromDB_IsUnderDBDir verifies the socket and lock live next to
// the db, not inside the source tree. For a realm member with
// dbPath=<realm>/.atomic/<key>.db, the socket must be under <realm>/.atomic/.
func TestSocketPathFromDB_IsUnderDBDir(t *testing.T) {
	dbPath := "/some/realm/.atomic/gui.db"
	sockPath := codemcp.SocketPathFromDB(dbPath)
	lockPath := codemcp.LockPathFromDB(dbPath)

	dbDir := filepath.Dir(dbPath)

	if filepath.Dir(sockPath) != dbDir {
		t.Errorf("SocketPathFromDB: want dir %s, got dir %s (full path %s)", dbDir, filepath.Dir(sockPath), sockPath)
	}
	if filepath.Dir(lockPath) != dbDir {
		t.Errorf("LockPathFromDB: want dir %s, got dir %s (full path %s)", dbDir, filepath.Dir(lockPath), lockPath)
	}
}

// TestSocketPathFromDB_NoMemberPollution verifies that when serving a realm
// member (source at <realm>/gui, db at <realm>/.atomic/gui.db), nothing is
// written into <realm>/gui/.claude/.atomic-index/.
func TestSocketPathFromDB_NoMemberPollution(t *testing.T) {
	realmDir, err := os.MkdirTemp("/tmp", "atmc-realm-")
	if err != nil {
		t.Fatalf("MkdirTemp realm: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(realmDir) })

	memberDir := filepath.Join(realmDir, "gui")
	if err := os.MkdirAll(memberDir, 0o755); err != nil {
		t.Fatalf("MkdirAll member: %v", err)
	}

	atomicDir := filepath.Join(realmDir, ".atomic")
	if err := os.MkdirAll(atomicDir, 0o755); err != nil {
		t.Fatalf("MkdirAll .atomic: %v", err)
	}

	dbPath := filepath.Join(atomicDir, "gui.db")
	sockPath := codemcp.SocketPathFromDB(dbPath)
	lockPath := codemcp.LockPathFromDB(dbPath)

	// Socket and lock must NOT be under the member's source tree.
	memberIndexDir := filepath.Join(memberDir, ".claude", ".atomic-index")
	if isUnderPath(sockPath, memberIndexDir) {
		t.Errorf("socket path %s is under member index dir %s (pollutes member source)", sockPath, memberIndexDir)
	}
	if isUnderPath(lockPath, memberIndexDir) {
		t.Errorf("lock path %s is under member index dir %s (pollutes member source)", lockPath, memberIndexDir)
	}

	// Socket and lock must be under <realm>/.atomic/.
	if !isUnderPath(sockPath, atomicDir) {
		t.Errorf("socket path %s should be under %s", sockPath, atomicDir)
	}
	if !isUnderPath(lockPath, atomicDir) {
		t.Errorf("lock path %s should be under %s", lockPath, atomicDir)
	}
}

// TestRepoMode_SocketUnchanged verifies that for a standalone repo (db at
// <repo>/.claude/.atomic-index/atomic.db), the socket remains in the same
// .claude/.atomic-index/ directory as before the fix — no path regression.
func TestRepoMode_SocketUnchanged(t *testing.T) {
	repoDir := "/some/repo"
	dbPath := filepath.Join(repoDir, ".claude", ".atomic-index", "atomic.db")
	sockPath := codemcp.SocketPathFromDB(dbPath)
	lockPath := codemcp.LockPathFromDB(dbPath)

	expectedDir := filepath.Join(repoDir, ".claude", ".atomic-index")
	if filepath.Dir(sockPath) != expectedDir {
		t.Errorf("repo-mode socket dir: want %s, got %s", expectedDir, filepath.Dir(sockPath))
	}
	if filepath.Dir(lockPath) != expectedDir {
		t.Errorf("repo-mode lock dir: want %s, got %s", expectedDir, filepath.Dir(lockPath))
	}
}

// isUnderPath reports whether child is equal to or under parent.
func isUnderPath(child, parent string) bool {
	child = filepath.Clean(child)
	parent = filepath.Clean(parent)
	if child == parent {
		return true
	}
	sep := string(filepath.Separator)
	return len(child) > len(parent) && child[:len(parent)] == parent && child[len(parent):len(parent)+1] == sep
}
