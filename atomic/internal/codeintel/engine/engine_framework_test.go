// Package engine tests — framework route extraction (P4 regression guard).
//
// TestFrameworkRouteExtraction_EndToEnd proves that ExtractFrameworkNodes wires
// the framework route-extraction seam into the index pipeline: after a full
// IndexAll → ExtractFrameworkNodes → ResolveReferences run, ≥1 NodeKindRoute
// node exists in the DB.
//
// The fixture uses Gin (Go) because:
//   - Gin detection falls back to import scanning when go.mod is absent (no git
//     repo needed, compatible with t.TempDir()).
//   - The GinResolver regex is straightforward and well-tested.
package engine_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/engine"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// ginFixtureRoutes is a minimal .go file with a Gin import and two route
// registrations. The GinResolver scans for the gin import pattern as a
// fallback when go.mod is absent, so no go.mod is needed in the fixture.
const ginFixtureRoutes = `package main

import (
	"github.com/gin-gonic/gin"
)

func main() {
	r := gin.Default()
	r.GET("/users", GetUsers)
	r.POST("/users", CreateUser)
	r.Run()
}

func GetUsers(c *gin.Context)   {}
func CreateUser(c *gin.Context) {}
`

// writeGinFixture writes the fixture .go file into a temp dir and returns
// the dir path. No git init — walkDirFallback handles non-git directories.
func writeGinFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "routes.go"), []byte(ginFixtureRoutes), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// TestFrameworkRouteExtraction_EndToEnd is the P4 regression guard:
//
//   - MUST fail on pre-fix code (no ExtractFrameworkNodes call → 0 routes).
//   - MUST pass after the fix (ExtractFrameworkNodes wired → ≥1 route node).
func TestFrameworkRouteExtraction_EndToEnd(t *testing.T) {
	dir := writeGinFixture(t)
	ctx := context.Background()

	e, err := engine.New(dir)
	if err != nil {
		t.Fatal("New:", err)
	}
	defer e.Close()

	if err := e.Init(ctx); err != nil {
		t.Fatal("Init:", err)
	}

	// Full pipeline as the CLI does: extract → framework routes → resolve.
	if err := e.IndexAll(ctx); err != nil {
		t.Fatal("IndexAll:", err)
	}

	routeCount, err := e.ExtractFrameworkNodes(ctx)
	if err != nil {
		t.Fatal("ExtractFrameworkNodes:", err)
	}

	if err := e.ResolveReferences(ctx); err != nil {
		t.Fatal("ResolveReferences:", err)
	}

	// Primary assertion: route nodes must exist in the DB.
	routes, err := e.GetNodesByKind(ctx, types.NodeKindRoute)
	if err != nil {
		t.Fatal("GetNodesByKind(route):", err)
	}
	if len(routes) == 0 {
		t.Fatalf("expected ≥1 NodeKindRoute node after framework extraction, got 0 (routeCount=%d)", routeCount)
	}

	// ExtractFrameworkNodes return value must agree with the DB count.
	if routeCount != len(routes) {
		t.Errorf("ExtractFrameworkNodes returned %d routes but DB has %d NodeKindRoute nodes", routeCount, len(routes))
	}

	t.Logf("framework extraction: %d route nodes (fixture has 2 routes: GET /users, POST /users)", len(routes))
}
