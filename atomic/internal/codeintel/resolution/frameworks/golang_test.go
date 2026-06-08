package frameworks_test

// Failing-first TDD tests for CP15 batch B: Go web frameworks (gin, echo, fiber, gorilla, chi).
//
// Per-framework coverage:
//   1. Detect true on a realistic go.mod fixture + false on unrelated dir.
//   2. Extract emits ≥1 route node (exact appendix-H id/qn/name via MakeRouteNode) + handler ref.
//   3. Commented Go route (using // stripper) emits nothing.
//   4. Resolve returns confidence 0.8–0.9 + non-empty TargetNodeID (or empty when db=nil is acceptable,
//      but confidence contract must be provable).
//   5. ClaimsReference true for recorded handlers, false for unseen.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/resolution/frameworks"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// ---------------------------------------------------------------------------
// Gin tests
// ---------------------------------------------------------------------------

func TestGinDetect_GoMod(t *testing.T) {
	dir := t.TempDir()
	gomod := `module example.com/myapp

go 1.21

require (
	github.com/gin-gonic/gin v1.9.1
)
`
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o644); err != nil {
		t.Fatal(err)
	}
	r := frameworks.NewGinResolver(dir)
	if !r.Detect(context.Background()) {
		t.Error("Gin.Detect should return true when go.mod requires gin-gonic/gin")
	}
}

func TestGinDetect_False(t *testing.T) {
	dir := t.TempDir()
	gomod := `module example.com/myapp

go 1.21

require (
	github.com/labstack/echo/v4 v4.11.0
)
`
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o644); err != nil {
		t.Fatal(err)
	}
	r := frameworks.NewGinResolver(dir)
	if r.Detect(context.Background()) {
		t.Error("Gin.Detect should return false when go.mod does not list gin")
	}
}

func TestGinExtract_BasicRoutes(t *testing.T) {
	filePath := "main.go"
	content := `package main

import "github.com/gin-gonic/gin"

func main() {
	r := gin.Default()
	r.GET("/users", listUsers)
	r.POST("/users", createUser)
	r.DELETE("/users/:id", deleteUser)
}
`
	r := frameworks.NewGinResolver("/project")
	nodes, refs := r.Extract(filePath, content)

	if len(nodes) < 3 {
		t.Fatalf("Gin.Extract: want ≥3 route nodes, got %d: %v", len(nodes), nodeNames(nodes))
	}

	// Verify GET /users node format (appendix H)
	getNode := findNodeByName(nodes, "GET /users")
	if getNode == nil {
		t.Fatalf("Gin.Extract: missing 'GET /users'; got: %v", nodeNames(nodes))
	}
	wantID := "route:main.go:7:GET:/users"
	if getNode.ID != wantID {
		t.Errorf("Gin.Extract route node id:\n  got  %q\n  want %q", getNode.ID, wantID)
	}
	wantQN := "main.go::METHOD:/users"
	if getNode.QualifiedName != wantQN {
		t.Errorf("Gin.Extract route node qualifiedName:\n  got  %q\n  want %q", getNode.QualifiedName, wantQN)
	}
	if getNode.Kind != types.NodeKindRoute {
		t.Errorf("Gin.Extract route kind: got %q want %q", getNode.Kind, types.NodeKindRoute)
	}
	if getNode.Language != types.LanguageGo {
		t.Errorf("Gin.Extract route language: got %q want %q", getNode.Language, types.LanguageGo)
	}

	// Verify handler ref for listUsers
	ref := findRefByNodeAndName(refs, getNode.ID, "listUsers")
	if ref == nil {
		t.Errorf("Gin.Extract: missing handler ref 'listUsers'; refs: %v", refNames(refs))
	} else if ref.ReferenceKind != types.EdgeKindReferences {
		t.Errorf("Gin.Extract: handler ref kind got %q want %q", ref.ReferenceKind, types.EdgeKindReferences)
	}

	// POST /users
	if findNodeByName(nodes, "POST /users") == nil {
		t.Errorf("Gin.Extract: missing 'POST /users'; got: %v", nodeNames(nodes))
	}
}

func TestGinExtract_GroupRoutes(t *testing.T) {
	filePath := "routes/api.go"
	content := `package routes

func SetupRoutes(r *gin.Engine) {
	v1 := r.Group("/api/v1")
	v1.GET("/items", handlers.ListItems)
	v1.POST("/items", handlers.CreateItem)
	v1.PUT("/items/:id", handlers.UpdateItem)
}
`
	r := frameworks.NewGinResolver("/project")
	nodes, refs := r.Extract(filePath, content)

	if len(nodes) < 3 {
		t.Fatalf("Gin.Extract group: want ≥3 route nodes, got %d: %v", len(nodes), nodeNames(nodes))
	}

	// Handler is last arg — pkg.Fn → emit last segment
	getNode := findNodeByName(nodes, "GET /items")
	if getNode == nil {
		t.Fatalf("Gin.Extract group: missing 'GET /items'; got: %v", nodeNames(nodes))
	}
	// handlers.ListItems → last segment is "ListItems"
	ref := findRefByNodeAndName(refs, getNode.ID, "ListItems")
	if ref == nil {
		t.Errorf("Gin.Extract group: missing handler ref 'ListItems'; refs: %v", refNames(refs))
	}
}

func TestGinExtract_MiddlewareLastArg(t *testing.T) {
	// The LAST arg is the handler; earlier args are middleware.
	filePath := "main.go"
	content := `package main

func setup(r *gin.Engine) {
	r.GET("/secure", authMiddleware, rateLimitMiddleware, secureHandler)
}
`
	r := frameworks.NewGinResolver("/project")
	nodes, refs := r.Extract(filePath, content)

	if len(nodes) == 0 {
		t.Fatal("Gin.Extract middleware: expected route node")
	}
	n := findNodeByName(nodes, "GET /secure")
	if n == nil {
		t.Fatalf("Gin.Extract middleware: missing 'GET /secure'; got: %v", nodeNames(nodes))
	}
	// Last arg = secureHandler (not authMiddleware)
	if findRefByNodeAndName(refs, n.ID, "secureHandler") == nil {
		t.Errorf("Gin.Extract middleware: expected handler ref 'secureHandler'; refs: %v", refNames(refs))
	}
	if findRefByNodeAndName(refs, n.ID, "authMiddleware") != nil {
		t.Errorf("Gin.Extract middleware: must NOT emit ref for middleware 'authMiddleware'")
	}
}

func TestGinExtract_CommentedRouteEmitsNothing(t *testing.T) {
	filePath := "main.go"
	content := `package main

func setup(r *gin.Engine) {
	// r.GET("/commented", commentedHandler)
	r.POST("/real", realHandler)
}
`
	r := frameworks.NewGinResolver("/project")
	nodes, _ := r.Extract(filePath, content)

	for _, n := range nodes {
		if n.Name == "GET /commented" {
			t.Errorf("Gin.Extract: commented route must not be emitted")
		}
	}
	if findNodeByName(nodes, "POST /real") == nil {
		t.Errorf("Gin.Extract: 'POST /real' should be emitted; got: %v", nodeNames(nodes))
	}
}

func TestGinClaimsReference(t *testing.T) {
	r := frameworks.NewGinResolver("/project")
	r.Extract("main.go", `package main
func setup(r *gin.Engine) {
	r.GET("/x", myGinHandler)
}`)
	if !r.ClaimsReference("myGinHandler") {
		t.Error("Gin.ClaimsReference: should return true for extracted handler")
	}
	if r.ClaimsReference("unknown") {
		t.Error("Gin.ClaimsReference: should return false for unseen name")
	}
}

func TestGinResolve_Confidence(t *testing.T) {
	r := frameworks.NewGinResolver("/project")
	r.Extract("main.go", `package main
func setup(r *gin.Engine) {
	r.GET("/x", myGinHandler)
}`)
	ref := types.UnresolvedReference{
		ReferenceName: "myGinHandler",
		ReferenceKind: types.EdgeKindReferences,
		Language:      types.LanguageGo,
	}
	result, err := r.Resolve(context.Background(), ref)
	if err != nil {
		t.Fatal(err)
	}
	if result.Confidence < 0.8 || result.Confidence > 0.9 {
		t.Errorf("Gin.Resolve: confidence want 0.8–0.9, got %v", result.Confidence)
	}
}

// ---------------------------------------------------------------------------
// Echo tests
// ---------------------------------------------------------------------------

func TestEchoDetect_GoMod(t *testing.T) {
	dir := t.TempDir()
	gomod := `module example.com/myapp

go 1.21

require (
	github.com/labstack/echo/v4 v4.11.0
)
`
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o644); err != nil {
		t.Fatal(err)
	}
	r := frameworks.NewEchoResolver(dir)
	if !r.Detect(context.Background()) {
		t.Error("Echo.Detect should return true when go.mod requires labstack/echo")
	}
}

func TestEchoDetect_False(t *testing.T) {
	dir := t.TempDir()
	r := frameworks.NewEchoResolver(dir)
	if r.Detect(context.Background()) {
		t.Error("Echo.Detect should return false in an empty directory")
	}
}

func TestEchoExtract_BasicRoutes(t *testing.T) {
	filePath := "server.go"
	content := `package main

import "github.com/labstack/echo/v4"

func main() {
	e := echo.New()
	e.GET("/users", listUsers)
	e.POST("/users", createUser)
	e.DELETE("/users/:id", deleteUser)
}
`
	r := frameworks.NewEchoResolver("/project")
	nodes, refs := r.Extract(filePath, content)

	if len(nodes) < 3 {
		t.Fatalf("Echo.Extract: want ≥3 route nodes, got %d: %v", len(nodes), nodeNames(nodes))
	}

	getNode := findNodeByName(nodes, "GET /users")
	if getNode == nil {
		t.Fatalf("Echo.Extract: missing 'GET /users'; got: %v", nodeNames(nodes))
	}
	wantID := "route:server.go:7:GET:/users"
	if getNode.ID != wantID {
		t.Errorf("Echo.Extract route node id:\n  got  %q\n  want %q", getNode.ID, wantID)
	}
	if getNode.Language != types.LanguageGo {
		t.Errorf("Echo.Extract route language: got %q want %q", getNode.Language, types.LanguageGo)
	}
	if findRefByNodeAndName(refs, getNode.ID, "listUsers") == nil {
		t.Errorf("Echo.Extract: missing handler ref 'listUsers'; refs: %v", refNames(refs))
	}
}

func TestEchoExtract_CommentedRouteEmitsNothing(t *testing.T) {
	filePath := "server.go"
	content := `package main

func setup(e *echo.Echo) {
	/* e.GET("/commented", commentedHandler) */
	e.PUT("/real", realHandler)
}
`
	r := frameworks.NewEchoResolver("/project")
	nodes, _ := r.Extract(filePath, content)

	for _, n := range nodes {
		if n.Name == "GET /commented" {
			t.Errorf("Echo.Extract: block-commented route must not be emitted")
		}
	}
	if findNodeByName(nodes, "PUT /real") == nil {
		t.Errorf("Echo.Extract: 'PUT /real' should be emitted; got: %v", nodeNames(nodes))
	}
}

func TestEchoClaimsReference(t *testing.T) {
	r := frameworks.NewEchoResolver("/project")
	r.Extract("server.go", `package main
func setup(e *echo.Echo) {
	e.GET("/x", myEchoHandler)
}`)
	if !r.ClaimsReference("myEchoHandler") {
		t.Error("Echo.ClaimsReference: should return true for extracted handler")
	}
}

func TestEchoResolve_Confidence(t *testing.T) {
	r := frameworks.NewEchoResolver("/project")
	r.Extract("server.go", `package main
func setup(e *echo.Echo) {
	e.GET("/x", myEchoHandler)
}`)
	result, err := r.Resolve(context.Background(), types.UnresolvedReference{
		ReferenceName: "myEchoHandler",
		Language:      types.LanguageGo,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Confidence < 0.8 || result.Confidence > 0.9 {
		t.Errorf("Echo.Resolve: confidence want 0.8–0.9, got %v", result.Confidence)
	}
}

// ---------------------------------------------------------------------------
// Fiber tests
// ---------------------------------------------------------------------------

func TestFiberDetect_GoMod(t *testing.T) {
	dir := t.TempDir()
	gomod := `module example.com/myapp

go 1.21

require (
	github.com/gofiber/fiber/v2 v2.52.0
)
`
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o644); err != nil {
		t.Fatal(err)
	}
	r := frameworks.NewFiberResolver(dir)
	if !r.Detect(context.Background()) {
		t.Error("Fiber.Detect should return true when go.mod requires gofiber/fiber")
	}
}

func TestFiberDetect_False(t *testing.T) {
	dir := t.TempDir()
	r := frameworks.NewFiberResolver(dir)
	if r.Detect(context.Background()) {
		t.Error("Fiber.Detect should return false in an empty directory")
	}
}

func TestFiberExtract_TitleCaseMethods(t *testing.T) {
	// Fiber uses Title-case method names: Get, Post, Put, Delete, Patch, Head, Options, All
	filePath := "app.go"
	content := `package main

import "github.com/gofiber/fiber/v2"

func main() {
	app := fiber.New()
	app.Get("/users", listUsers)
	app.Post("/users", createUser)
	app.Delete("/users/:id", deleteUser)
}
`
	r := frameworks.NewFiberResolver("/project")
	nodes, refs := r.Extract(filePath, content)

	if len(nodes) < 3 {
		t.Fatalf("Fiber.Extract: want ≥3 route nodes, got %d: %v", len(nodes), nodeNames(nodes))
	}

	// Methods are emitted uppercase in route node name
	getNode := findNodeByName(nodes, "GET /users")
	if getNode == nil {
		t.Fatalf("Fiber.Extract: missing 'GET /users'; got: %v", nodeNames(nodes))
	}
	wantID := "route:app.go:7:GET:/users"
	if getNode.ID != wantID {
		t.Errorf("Fiber.Extract route node id:\n  got  %q\n  want %q", getNode.ID, wantID)
	}
	if getNode.Language != types.LanguageGo {
		t.Errorf("Fiber.Extract route language: got %q want %q", getNode.Language, types.LanguageGo)
	}
	if findRefByNodeAndName(refs, getNode.ID, "listUsers") == nil {
		t.Errorf("Fiber.Extract: missing handler ref 'listUsers'; refs: %v", refNames(refs))
	}
}

func TestFiberExtract_CommentedRouteEmitsNothing(t *testing.T) {
	filePath := "app.go"
	content := `package main

func setup(app *fiber.App) {
	// app.Get("/commented", commentedHandler)
	app.Post("/real", realHandler)
}
`
	r := frameworks.NewFiberResolver("/project")
	nodes, _ := r.Extract(filePath, content)

	for _, n := range nodes {
		if n.Name == "GET /commented" {
			t.Errorf("Fiber.Extract: commented route must not be emitted")
		}
	}
	if findNodeByName(nodes, "POST /real") == nil {
		t.Errorf("Fiber.Extract: 'POST /real' should be emitted; got: %v", nodeNames(nodes))
	}
}

func TestFiberClaimsReference(t *testing.T) {
	r := frameworks.NewFiberResolver("/project")
	r.Extract("app.go", `package main
func setup(app *fiber.App) {
	app.Get("/x", myFiberHandler)
}`)
	if !r.ClaimsReference("myFiberHandler") {
		t.Error("Fiber.ClaimsReference: should return true for extracted handler")
	}
}

func TestFiberResolve_Confidence(t *testing.T) {
	r := frameworks.NewFiberResolver("/project")
	r.Extract("app.go", `package main
func setup(app *fiber.App) {
	app.Get("/x", myFiberHandler)
}`)
	result, err := r.Resolve(context.Background(), types.UnresolvedReference{
		ReferenceName: "myFiberHandler",
		Language:      types.LanguageGo,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Confidence < 0.8 || result.Confidence > 0.9 {
		t.Errorf("Fiber.Resolve: confidence want 0.8–0.9, got %v", result.Confidence)
	}
}

// ---------------------------------------------------------------------------
// Gorilla tests
// ---------------------------------------------------------------------------

func TestGorillaDetect_GoMod(t *testing.T) {
	dir := t.TempDir()
	gomod := `module example.com/myapp

go 1.21

require (
	github.com/gorilla/mux v1.8.1
)
`
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o644); err != nil {
		t.Fatal(err)
	}
	r := frameworks.NewGorillaResolver(dir)
	if !r.Detect(context.Background()) {
		t.Error("Gorilla.Detect should return true when go.mod requires gorilla/mux")
	}
}

func TestGorillaDetect_False(t *testing.T) {
	dir := t.TempDir()
	r := frameworks.NewGorillaResolver(dir)
	if r.Detect(context.Background()) {
		t.Error("Gorilla.Detect should return false in an empty directory")
	}
}

func TestGorillaExtract_HandleFuncWithMethods(t *testing.T) {
	// .Methods("GET","POST") → two route nodes (one per method)
	filePath := "router.go"
	content := `package main

import "github.com/gorilla/mux"

func NewRouter() *mux.Router {
	r := mux.NewRouter()
	r.HandleFunc("/users", listUsers).Methods("GET", "POST")
	r.HandleFunc("/users/{id}", getUser).Methods("GET")
}
`
	r := frameworks.NewGorillaResolver("/project")
	nodes, refs := r.Extract(filePath, content)

	// /users with Methods("GET","POST") → 2 nodes; /users/{id} with Methods("GET") → 1 node = 3 total
	if len(nodes) < 3 {
		t.Fatalf("Gorilla.Extract: want ≥3 route nodes for Methods fan-out, got %d: %v", len(nodes), nodeNames(nodes))
	}

	// Both GET /users and POST /users must exist
	if findNodeByName(nodes, "GET /users") == nil {
		t.Errorf("Gorilla.Extract: missing 'GET /users'; got: %v", nodeNames(nodes))
	}
	if findNodeByName(nodes, "POST /users") == nil {
		t.Errorf("Gorilla.Extract: missing 'POST /users' from Methods fan-out; got: %v", nodeNames(nodes))
	}

	// Check appendix-H format for GET /users
	getNode := findNodeByName(nodes, "GET /users")
	if getNode != nil {
		wantID := "route:router.go:7:GET:/users"
		if getNode.ID != wantID {
			t.Errorf("Gorilla.Extract route node id:\n  got  %q\n  want %q", getNode.ID, wantID)
		}
		if getNode.Language != types.LanguageGo {
			t.Errorf("Gorilla.Extract route language: got %q want %q", getNode.Language, types.LanguageGo)
		}
		if findRefByNodeAndName(refs, getNode.ID, "listUsers") == nil {
			t.Errorf("Gorilla.Extract: missing handler ref 'listUsers' from GET /users; refs: %v", refNames(refs))
		}
	}

	// POST /users must also reference listUsers
	postNode := findNodeByName(nodes, "POST /users")
	if postNode != nil {
		if findRefByNodeAndName(refs, postNode.ID, "listUsers") == nil {
			t.Errorf("Gorilla.Extract: missing handler ref 'listUsers' from POST /users; refs: %v", refNames(refs))
		}
	}
}

func TestGorillaExtract_HandleFuncNoMethods(t *testing.T) {
	// No .Methods(...) chain → method is ANY
	filePath := "router.go"
	content := `package main

func setup(r *mux.Router) {
	r.HandleFunc("/catch-all", catchAllHandler)
}
`
	r := frameworks.NewGorillaResolver("/project")
	nodes, refs := r.Extract(filePath, content)

	if len(nodes) == 0 {
		t.Fatal("Gorilla.Extract: expected route node for HandleFunc without .Methods()")
	}
	anyNode := findNodeByName(nodes, "ANY /catch-all")
	if anyNode == nil {
		t.Fatalf("Gorilla.Extract no-methods: missing 'ANY /catch-all'; got: %v", nodeNames(nodes))
	}
	if findRefByNodeAndName(refs, anyNode.ID, "catchAllHandler") == nil {
		t.Errorf("Gorilla.Extract no-methods: missing handler ref 'catchAllHandler'; refs: %v", refNames(refs))
	}
}

func TestGorillaExtract_CommentedRouteEmitsNothing(t *testing.T) {
	filePath := "router.go"
	content := `package main

func setup(r *mux.Router) {
	// r.HandleFunc("/commented", commentedHandler).Methods("GET")
	r.HandleFunc("/real", realHandler).Methods("POST")
}
`
	r := frameworks.NewGorillaResolver("/project")
	nodes, _ := r.Extract(filePath, content)

	for _, n := range nodes {
		if strings.Contains(n.Name, "/commented") {
			t.Errorf("Gorilla.Extract: commented route must not be emitted")
		}
	}
	if findNodeByName(nodes, "POST /real") == nil {
		t.Errorf("Gorilla.Extract: 'POST /real' should be emitted; got: %v", nodeNames(nodes))
	}
}

func TestGorillaClaimsReference(t *testing.T) {
	r := frameworks.NewGorillaResolver("/project")
	r.Extract("router.go", `package main
func setup(r *mux.Router) {
	r.HandleFunc("/x", myGorillaHandler).Methods("GET")
}`)
	if !r.ClaimsReference("myGorillaHandler") {
		t.Error("Gorilla.ClaimsReference: should return true for extracted handler")
	}
	if r.ClaimsReference("unknown") {
		t.Error("Gorilla.ClaimsReference: should return false for unseen name")
	}
}

func TestGorillaResolve_Confidence(t *testing.T) {
	r := frameworks.NewGorillaResolver("/project")
	r.Extract("router.go", `package main
func setup(r *mux.Router) {
	r.HandleFunc("/x", myGorillaHandler).Methods("GET")
}`)
	result, err := r.Resolve(context.Background(), types.UnresolvedReference{
		ReferenceName: "myGorillaHandler",
		Language:      types.LanguageGo,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Confidence < 0.8 || result.Confidence > 0.9 {
		t.Errorf("Gorilla.Resolve: confidence want 0.8–0.9, got %v", result.Confidence)
	}
}

// ---------------------------------------------------------------------------
// Chi tests
// ---------------------------------------------------------------------------

func TestChiDetect_GoMod(t *testing.T) {
	dir := t.TempDir()
	gomod := `module example.com/myapp

go 1.21

require (
	github.com/go-chi/chi/v5 v5.0.10
)
`
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o644); err != nil {
		t.Fatal(err)
	}
	r := frameworks.NewChiResolver(dir)
	if !r.Detect(context.Background()) {
		t.Error("Chi.Detect should return true when go.mod requires go-chi/chi")
	}
}

func TestChiDetect_False(t *testing.T) {
	dir := t.TempDir()
	r := frameworks.NewChiResolver(dir)
	if r.Detect(context.Background()) {
		t.Error("Chi.Detect should return false in an empty directory")
	}
}

func TestChiExtract_TitleCaseMethods(t *testing.T) {
	// Chi uses Title-case method names: Get, Post, Put, Delete, Patch, Head, Options
	filePath := "routes.go"
	content := `package routes

import "github.com/go-chi/chi/v5"

func Router() chi.Router {
	r := chi.NewRouter()
	r.Get("/users", listUsers)
	r.Post("/users", createUser)
	r.Delete("/users/{id}", deleteUser)
	return r
}
`
	r := frameworks.NewChiResolver("/project")
	nodes, refs := r.Extract(filePath, content)

	if len(nodes) < 3 {
		t.Fatalf("Chi.Extract: want ≥3 route nodes, got %d: %v", len(nodes), nodeNames(nodes))
	}

	getNode := findNodeByName(nodes, "GET /users")
	if getNode == nil {
		t.Fatalf("Chi.Extract: missing 'GET /users'; got: %v", nodeNames(nodes))
	}
	wantID := "route:routes.go:7:GET:/users"
	if getNode.ID != wantID {
		t.Errorf("Chi.Extract route node id:\n  got  %q\n  want %q", getNode.ID, wantID)
	}
	if getNode.Language != types.LanguageGo {
		t.Errorf("Chi.Extract route language: got %q want %q", getNode.Language, types.LanguageGo)
	}
	if findRefByNodeAndName(refs, getNode.ID, "listUsers") == nil {
		t.Errorf("Chi.Extract: missing handler ref 'listUsers'; refs: %v", refNames(refs))
	}
}

func TestChiExtract_MethodFunc(t *testing.T) {
	// r.Method("GET", "/path", handler) form
	filePath := "routes.go"
	content := `package routes

func setup(r chi.Router) {
	r.Method("GET", "/special", specialHandler)
}
`
	r := frameworks.NewChiResolver("/project")
	nodes, refs := r.Extract(filePath, content)

	if len(nodes) == 0 {
		t.Fatal("Chi.Extract Method(): expected route node")
	}
	n := findNodeByName(nodes, "GET /special")
	if n == nil {
		t.Fatalf("Chi.Extract Method(): missing 'GET /special'; got: %v", nodeNames(nodes))
	}
	if findRefByNodeAndName(refs, n.ID, "specialHandler") == nil {
		t.Errorf("Chi.Extract Method(): missing handler ref 'specialHandler'; refs: %v", refNames(refs))
	}
}

func TestChiExtract_CommentedRouteEmitsNothing(t *testing.T) {
	filePath := "routes.go"
	content := `package routes

func setup(r chi.Router) {
	// r.Get("/commented", commentedHandler)
	r.Post("/real", realHandler)
}
`
	r := frameworks.NewChiResolver("/project")
	nodes, _ := r.Extract(filePath, content)

	for _, n := range nodes {
		if n.Name == "GET /commented" {
			t.Errorf("Chi.Extract: commented route must not be emitted")
		}
	}
	if findNodeByName(nodes, "POST /real") == nil {
		t.Errorf("Chi.Extract: 'POST /real' should be emitted; got: %v", nodeNames(nodes))
	}
}

func TestChiClaimsReference(t *testing.T) {
	r := frameworks.NewChiResolver("/project")
	r.Extract("routes.go", `package routes
func setup(r chi.Router) {
	r.Get("/x", myChiHandler)
}`)
	if !r.ClaimsReference("myChiHandler") {
		t.Error("Chi.ClaimsReference: should return true for extracted handler")
	}
}

func TestChiResolve_Confidence(t *testing.T) {
	r := frameworks.NewChiResolver("/project")
	r.Extract("routes.go", `package routes
func setup(r chi.Router) {
	r.Get("/x", myChiHandler)
}`)
	result, err := r.Resolve(context.Background(), types.UnresolvedReference{
		ReferenceName: "myChiHandler",
		Language:      types.LanguageGo,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Confidence < 0.8 || result.Confidence > 0.9 {
		t.Errorf("Chi.Resolve: confidence want 0.8–0.9, got %v", result.Confidence)
	}
}

// ---------------------------------------------------------------------------
// Registry: Go frameworks appear in GetApplicableFrameworks
// ---------------------------------------------------------------------------

func TestRegistry_GoFrameworksRegistered(t *testing.T) {
	dir := t.TempDir()
	reg := frameworks.NewRegistry(dir, nil)

	goFrameworks := reg.GetApplicableFrameworks(types.LanguageGo)
	names := make(map[string]bool, len(goFrameworks))
	for _, r := range goFrameworks {
		names[r.Name()] = true
	}

	for _, want := range []string{"gin", "echo", "fiber", "gorilla", "chi"} {
		if !names[want] {
			t.Errorf("Registry: Go framework %q not registered; got names: %v", want, names)
		}
	}
}

// ---------------------------------------------------------------------------
// F-26: Gorilla multi-line .Methods() chain (carry-along fix)
// ---------------------------------------------------------------------------

func TestGorillaExtract_MultiLineMethods(t *testing.T) {
	// Idiomatic multi-line chaining: r.HandleFunc("/p", h).\n\t.Methods("GET")
	// extractGorillaMethods was rejecting this because it checked for "\n" before
	// ".Methods" in the window. Fix: allow newlines in the window (the window is
	// already bounded to 200 chars — no need to restrict by newline).
	filePath := "router.go"
	content := `package main

func NewRouter() *mux.Router {
	r := mux.NewRouter()
	r.HandleFunc("/multiline", multilineHandler).
		Methods("GET")
}
`
	r := frameworks.NewGorillaResolver("/project")
	nodes, refs := r.Extract(filePath, content)

	if len(nodes) == 0 {
		t.Fatal("Gorilla.Extract multi-line .Methods(): expected route node, got none")
	}
	// Must be GET /multiline, not ANY /multiline
	getNode := findNodeByName(nodes, "GET /multiline")
	if getNode == nil {
		t.Fatalf("Gorilla.Extract multi-line: missing 'GET /multiline' (got ANY or nothing); got: %v", nodeNames(nodes))
	}
	if findRefByNodeAndName(refs, getNode.ID, "multilineHandler") == nil {
		t.Errorf("Gorilla.Extract multi-line: missing handler ref 'multilineHandler'; refs: %v", refNames(refs))
	}
	// Must NOT also emit ANY /multiline
	if anyNode := findNodeByName(nodes, "ANY /multiline"); anyNode != nil {
		t.Errorf("Gorilla.Extract multi-line: should NOT emit 'ANY /multiline' when .Methods('GET') is present")
	}
}
