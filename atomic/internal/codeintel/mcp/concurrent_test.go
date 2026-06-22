// Tests proving 2+ MCP daemons for different repos run simultaneously without
// socket/lock collision.
//
// Spec criterion (Checkpoint 3 Part A):
//   - Two daemons for distinct repos bind distinct sockets (each under its own
//     db dir).
//   - Both answer an MCP initialize (tools/list returns tools).
//   - Neither daemon's socket or lock lands in the other's db tree.
package mcp_test

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	codemcp "github.com/damusix/atomic-claude/atomic/internal/codeintel/mcp"
)

// TestConcurrentDaemons_DistinctSocketsNoCollision starts two MCP daemons, each
// serving a distinct repo/db, and asserts:
//
//  1. Both daemons bind a socket.
//  2. Each socket lives under its own db directory (not the other's).
//  3. Both daemons answer an MCP initialize handshake (tools/list succeeds).
//  4. Neither daemon's socket lands in the other daemon's db tree.
//
// This is the automated stand-in for the taxgentic live-verification step
// ("2+ daemons for different member repos, no collision").
func TestConcurrentDaemons_DistinctSocketsNoCollision(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// --- Repo A ---
	dbDirA, err := os.MkdirTemp("/tmp", "atmc-concA-")
	if err != nil {
		t.Fatalf("MkdirTemp A: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dbDirA) })
	dbPathA := filepath.Join(dbDirA, "repoA.db")
	sockPathA := codemcp.SocketPathFromDB(dbPathA)

	// --- Repo B ---
	dbDirB, err := os.MkdirTemp("/tmp", "atmc-concB-")
	if err != nil {
		t.Fatalf("MkdirTemp B: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dbDirB) })
	dbPathB := filepath.Join(dbDirB, "repoB.db")
	sockPathB := codemcp.SocketPathFromDB(dbPathB)

	// Sockets must be different paths.
	if sockPathA == sockPathB {
		t.Fatalf("sockets must be distinct: both are %q", sockPathA)
	}

	// Allocate source directories before spawning goroutines so t.Cleanup is
	// registered from the test goroutine (calling t.Cleanup from a non-test
	// goroutine panics on recent Go).
	sourceA, err := os.MkdirTemp("/tmp", "atmc-srcA-")
	if err != nil {
		t.Fatalf("MkdirTemp srcA: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(sourceA) })

	sourceB, err := os.MkdirTemp("/tmp", "atmc-srcB-")
	if err != nil {
		t.Fatalf("MkdirTemp srcB: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(sourceB) })

	// Spin up both daemons concurrently. Errors are sent to a buffered channel
	// and asserted after wg.Wait() from the test goroutine (t.Errorf must not
	// be called from non-test goroutines on recent Go).
	type daemonResult struct {
		name string
		err  error
	}
	results := make(chan daemonResult, 2)

	var wg sync.WaitGroup
	wg.Add(2)

	// Start Daemon A.
	go func() {
		defer wg.Done()
		if err := startConcurrentDaemon(t, ctx, sourceA, dbPathA, sockPathA); err != nil {
			results <- daemonResult{"A", err}
		}
	}()

	// Start Daemon B.
	go func() {
		defer wg.Done()
		if err := startConcurrentDaemon(t, ctx, sourceB, dbPathB, sockPathB); err != nil {
			results <- daemonResult{"B", err}
		}
	}()

	wg.Wait()
	close(results)

	for r := range results {
		t.Errorf("daemon %s: %v", r.name, r.err)
	}

	// Both sockets must be live.
	waitForSocketLive(t, sockPathA, 5*time.Second)
	waitForSocketLive(t, sockPathB, 5*time.Second)

	// --- Assert: socket A is under dbDirA, NOT under dbDirB ---
	if !isUnderPath(sockPathA, dbDirA) {
		t.Errorf("socket A %q should be under %q", sockPathA, dbDirA)
	}
	if isUnderPath(sockPathA, dbDirB) {
		t.Errorf("socket A %q must not be under %q (daemon A pollutes daemon B's tree)", sockPathA, dbDirB)
	}

	// --- Assert: socket B is under dbDirB, NOT under dbDirA ---
	if !isUnderPath(sockPathB, dbDirB) {
		t.Errorf("socket B %q should be under %q", sockPathB, dbDirB)
	}
	if isUnderPath(sockPathB, dbDirA) {
		t.Errorf("socket B %q must not be under %q (daemon B pollutes daemon A's tree)", sockPathB, dbDirA)
	}

	// --- Assert: both daemons answer MCP initialize ---
	toolsA := mcpListTools(t, ctx, sockPathA)
	if len(toolsA) == 0 {
		t.Error("daemon A: tools/list returned 0 tools — initialize failed")
	}

	toolsB := mcpListTools(t, ctx, sockPathB)
	if len(toolsB) == 0 {
		t.Error("daemon B: tools/list returned 0 tools — initialize failed")
	}

	// Lock paths must also be distinct and under the correct db dir.
	lockPathA := codemcp.LockPathFromDB(dbPathA)
	lockPathB := codemcp.LockPathFromDB(dbPathB)

	if lockPathA == lockPathB {
		t.Errorf("lock paths must be distinct: both are %q", lockPathA)
	}
	if isUnderPath(lockPathA, dbDirB) {
		t.Errorf("lock A %q must not be under %q", lockPathA, dbDirB)
	}
	if isUnderPath(lockPathB, dbDirA) {
		t.Errorf("lock B %q must not be under %q", lockPathB, dbDirA)
	}
}

// startConcurrentDaemon initializes an engine at (sourceRoot, dbPath), starts
// an in-process accept loop, and returns nil once the socket file exists.
// The daemon stops when ctx is cancelled (via t.Cleanup).
func startConcurrentDaemon(t *testing.T, ctx context.Context, sourceRoot, dbPath, sockPath string) error {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(sockPath), 0o755); err != nil {
		return err
	}
	_ = os.Remove(sockPath)

	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		return err
	}

	ctx2, cancel := context.WithCancel(ctx)
	t.Cleanup(func() {
		cancel()
		ln.Close()
		_ = os.Remove(sockPath)
	})

	eng := newEmptyEngine(t, sourceRoot)
	t.Cleanup(func() { eng.Close() })

	stats, _ := eng.GetStats(ctx2)
	srv := codemcp.NewServer(eng, stats.FileCount)

	go func() {
		_ = codemcp.RunAcceptLoop(ctx2, ln, srv, sockPath)
	}()

	return nil
}

// mcpListTools connects to the socket, runs tools/list, and returns the tool names.
func mcpListTools(t *testing.T, ctx context.Context, sockPath string) []string {
	t.Helper()
	conn, err := net.DialTimeout("unix", sockPath, 3*time.Second)
	if err != nil {
		t.Fatalf("dial %q: %v", sockPath, err)
	}
	defer conn.Close()

	transport := &sdk.IOTransport{Reader: conn, Writer: conn}
	client := sdk.NewClient(&sdk.Implementation{Name: "conc-test", Version: "1"}, nil)
	sess, err := client.Connect(ctx, transport, nil)
	if err != nil {
		t.Fatalf("client.Connect %q: %v", sockPath, err)
	}
	defer sess.Close()

	res, err := sess.ListTools(ctx, &sdk.ListToolsParams{})
	if err != nil {
		t.Fatalf("ListTools %q: %v", sockPath, err)
	}

	names := make([]string, len(res.Tools))
	for i, tool := range res.Tools {
		names[i] = tool.Name
	}
	return names
}
