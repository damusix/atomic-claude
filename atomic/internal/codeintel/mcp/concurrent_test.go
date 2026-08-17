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

// Two daemons on distinct repos must bind distinct sockets, keep their socket
// and lock inside their own db tree, and both answer an initialize handshake.
func TestConcurrentDaemons_DistinctSocketsNoCollision(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	dbDirA, err := os.MkdirTemp("/tmp", "atmc-concA-")
	if err != nil {
		t.Fatalf("MkdirTemp A: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dbDirA) })
	dbPathA := filepath.Join(dbDirA, "repoA.db")
	sockPathA := codemcp.SocketPathFromDB(dbPathA)

	dbDirB, err := os.MkdirTemp("/tmp", "atmc-concB-")
	if err != nil {
		t.Fatalf("MkdirTemp B: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dbDirB) })
	dbPathB := filepath.Join(dbDirB, "repoB.db")
	sockPathB := codemcp.SocketPathFromDB(dbPathB)

	if sockPathA == sockPathB {
		t.Fatalf("sockets must be distinct: both are %q", sockPathA)
	}

	// Allocated up front: t.Cleanup panics if called off the test goroutine.
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

	// Errors travel by channel because t.Errorf must not run off the test goroutine.
	type daemonResult struct {
		name string
		err  error
	}
	results := make(chan daemonResult, 2)

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		if err := startConcurrentDaemon(t, ctx, sourceA, dbPathA, sockPathA); err != nil {
			results <- daemonResult{"A", err}
		}
	}()

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

	waitForSocketLive(t, sockPathA, 5*time.Second)
	waitForSocketLive(t, sockPathB, 5*time.Second)

	if !isUnderPath(sockPathA, dbDirA) {
		t.Errorf("socket A %q should be under %q", sockPathA, dbDirA)
	}
	if isUnderPath(sockPathA, dbDirB) {
		t.Errorf("socket A %q must not be under %q (daemon A pollutes daemon B's tree)", sockPathA, dbDirB)
	}

	if !isUnderPath(sockPathB, dbDirB) {
		t.Errorf("socket B %q should be under %q", sockPathB, dbDirB)
	}
	if isUnderPath(sockPathB, dbDirA) {
		t.Errorf("socket B %q must not be under %q (daemon B pollutes daemon A's tree)", sockPathB, dbDirA)
	}

	toolsA := mcpListTools(t, ctx, sockPathA)
	if len(toolsA) == 0 {
		t.Error("daemon A: tools/list returned 0 tools — initialize failed")
	}

	toolsB := mcpListTools(t, ctx, sockPathB)
	if len(toolsB) == 0 {
		t.Error("daemon B: tools/list returned 0 tools — initialize failed")
	}

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

// startConcurrentDaemon returns only once the socket exists, and stops with ctx.
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
