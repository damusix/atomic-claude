// Guards that the framework route-extraction seam stays wired into the index
// pipeline. Gin is the fixture because its detection falls back to import
// scanning when go.mod is absent, so a bare t.TempDir() suffices.
package engine_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/engine"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// The gin import is what GinResolver keys on, so no go.mod is needed.
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

// No git init: walkDirFallback handles non-git directories.
func writeGinFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "routes.go"), []byte(ginFixtureRoutes), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

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

	routes, err := e.GetNodesByKind(ctx, types.NodeKindRoute)
	if err != nil {
		t.Fatal("GetNodesByKind(route):", err)
	}
	if len(routes) == 0 {
		t.Fatalf("expected ≥1 NodeKindRoute node after framework extraction, got 0 (routeCount=%d)", routeCount)
	}

	if routeCount != len(routes) {
		t.Errorf("ExtractFrameworkNodes returned %d routes but DB has %d NodeKindRoute nodes", routeCount, len(routes))
	}

	t.Logf("framework extraction: %d route nodes (fixture has 2 routes: GET /users, POST /users)", len(routes))
}
