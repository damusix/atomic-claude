package frameworks_test

// Failing-first TDD tests for CP15 batch D (Rust): actix-web, axum, rocket.
//
// Per-framework coverage:
//   - Detect: Cargo.toml fixture → true; absent/other dep → false.
//   - Extract: realistic source → route nodes (appendix-H format) + handler refs.
//   - Bounded lookahead (actix, rocket): second #[...] attribute is skipped.
//   - Axum method-chain fan-out: get(h).post(h2) → one node per method.
//   - Commented routes emit nothing (stripJSComments reuse).
//   - Resolve: confidence in [0.8, 0.9]; ClaimsReference correct.

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
// Shared helpers (rust + spring test files; same frameworks_test package)
// ---------------------------------------------------------------------------

// findRustRouteNode finds the first node with the given method and path.
// Named differently from other per-file helpers to avoid redeclaration.
func findRustRouteNode(nodes []types.Node, method, path string) *types.Node {
	want := method + " " + path
	for i, n := range nodes {
		if n.Name == want {
			return &nodes[i]
		}
	}
	return nil
}

// assertRouteNodeFormat asserts the appendix-H id/qualifiedName/name format.
func assertRouteNodeFormat(t *testing.T, n types.Node, filePath, method, path string, lang types.Language) {
	t.Helper()
	// id: route:{filePath}:{line}:{METHOD}:{path}
	prefix := "route:" + filePath + ":"
	if !strings.HasPrefix(n.ID, prefix) {
		t.Errorf("node.ID %q: want prefix %q", n.ID, prefix)
	}
	suffix := ":" + method + ":" + path
	if !strings.HasSuffix(n.ID, suffix) {
		t.Errorf("node.ID %q: want suffix %q", n.ID, suffix)
	}
	// qualifiedName: {filePath}::METHOD:{path}
	wantQN := filePath + "::METHOD:" + path
	if n.QualifiedName != wantQN {
		t.Errorf("node.QualifiedName: want %q, got %q", wantQN, n.QualifiedName)
	}
	// name: "METHOD /path"
	wantName := method + " " + path
	if n.Name != wantName {
		t.Errorf("node.Name: want %q, got %q", wantName, n.Name)
	}
	if n.Kind != types.NodeKindRoute {
		t.Errorf("node.Kind: want route, got %v", n.Kind)
	}
	if n.Language != lang {
		t.Errorf("node.Language: want %v, got %v", lang, n.Language)
	}
}

// ---------------------------------------------------------------------------
// Actix-web
// ---------------------------------------------------------------------------

func TestActixDetect_WithCargoToml(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Cargo.toml"), []byte(`[dependencies]
actix-web = "4"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	r := frameworks.NewActixResolver(dir)
	if !r.Detect(context.Background()) {
		t.Fatal("Detect: want true when Cargo.toml has actix-web")
	}
}

func TestActixDetect_NoCargoToml(t *testing.T) {
	dir := t.TempDir()
	r := frameworks.NewActixResolver(dir)
	if r.Detect(context.Background()) {
		t.Fatal("Detect: want false when Cargo.toml absent")
	}
}

func TestActixDetect_OtherDep(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Cargo.toml"), []byte(`[dependencies]
axum = "0.7"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	r := frameworks.NewActixResolver(dir)
	if r.Detect(context.Background()) {
		t.Fatal("Detect: want false when actix-web absent")
	}
}

func TestActixExtract_AttributeMacroAboveAsyncFn(t *testing.T) {
	// Real actix-web pattern: attribute macro directly above async fn
	src := `
use actix_web::{get, post, web, App, HttpResponse};

#[get("/products")]
async fn list_products() -> HttpResponse {
    HttpResponse::Ok().finish()
}

#[post("/products")]
async fn create_product(body: web::Json<Product>) -> HttpResponse {
    HttpResponse::Created().finish()
}
`
	r := frameworks.NewActixResolver(t.TempDir())
	nodes, refs := r.Extract("src/routes.rs", src)

	if len(nodes) < 2 {
		t.Fatalf("Extract: want ≥2 nodes, got %d", len(nodes))
	}

	// GET /products
	getNode := findRustRouteNode(nodes, "GET", "/products")
	if getNode == nil {
		t.Fatal("Extract: missing GET /products node")
	}
	assertRouteNodeFormat(t, *getNode, "src/routes.rs", "GET", "/products", types.LanguageRust)

	// Handler ref: list_products
	getRef := findRefByNodeAndName(refs, getNode.ID, "list_products")
	if getRef == nil {
		t.Fatalf("Extract: missing handler ref 'list_products' from %s", getNode.ID)
	}
	if getRef.ReferenceKind != types.EdgeKindReferences {
		t.Errorf("Extract: handler ref kind want references, got %v", getRef.ReferenceKind)
	}

	// POST node
	postNode := findRustRouteNode(nodes, "POST", "/products")
	if postNode == nil {
		t.Fatal("Extract: missing POST /products node")
	}
	if findRefByNodeAndName(refs, postNode.ID, "create_product") == nil {
		t.Fatal("Extract: missing handler ref 'create_product'")
	}
}

func TestActixExtract_BoundedLookahead_SkipsAttributeLines(t *testing.T) {
	// Macro followed by another attribute before the fn — must skip to fn def
	src := `
#[get("/items")]
#[actix_web::main]
async fn list_items() -> impl Responder {
    "ok"
}
`
	r := frameworks.NewActixResolver(t.TempDir())
	nodes, refs := r.Extract("src/main.rs", src)

	if len(nodes) != 1 {
		t.Fatalf("Extract: want 1 node, got %d", len(nodes))
	}
	if findRefByNodeAndName(refs, nodes[0].ID, "list_items") == nil {
		t.Fatal("Extract: bounded lookahead should find list_items past second attribute")
	}
}

func TestActixExtract_RouteCallForm(t *testing.T) {
	// .route("/path", web::get().to(handler)) form
	src := `
web::resource("/users")
    .route(web::get().to(list_users))
    .route(web::post().to(create_user));
`
	r := frameworks.NewActixResolver(t.TempDir())
	nodes, refs := r.Extract("src/app.rs", src)

	if len(nodes) < 2 {
		t.Fatalf("Extract: want ≥2 nodes for .route() form, got %d", len(nodes))
	}
	getUsersNode := findRustRouteNode(nodes, "GET", "/users")
	if getUsersNode == nil {
		t.Fatal("Extract: missing GET /users from .route().to() form")
	}
	if findRefByNodeAndName(refs, getUsersNode.ID, "list_users") == nil {
		t.Fatal("Extract: missing handler ref 'list_users' from .route().to() form")
	}
}

func TestActixExtract_DirectRouteForm(t *testing.T) {
	// App::new().route("/users", web::get().to(list_users)) — direct-route form.
	// Path is the FIRST arg to .route(); method is the second.
	src := `
App::new()
    .route("/users", web::get().to(list_users))
    .route("/users", web::post().to(create_user));
`
	r := frameworks.NewActixResolver(t.TempDir())
	nodes, refs := r.Extract("src/main.rs", src)

	if len(nodes) < 2 {
		t.Fatalf("Extract direct-route: want ≥2 nodes, got %d", len(nodes))
	}
	getUsersNode := findRustRouteNode(nodes, "GET", "/users")
	if getUsersNode == nil {
		t.Fatal("Extract direct-route: missing GET /users node")
	}
	assertRouteNodeFormat(t, *getUsersNode, "src/main.rs", "GET", "/users", types.LanguageRust)
	if findRefByNodeAndName(refs, getUsersNode.ID, "list_users") == nil {
		t.Fatal("Extract direct-route: missing handler ref 'list_users'")
	}

	postNode := findRustRouteNode(nodes, "POST", "/users")
	if postNode == nil {
		t.Fatal("Extract direct-route: missing POST /users node")
	}
	if findRefByNodeAndName(refs, postNode.ID, "create_user") == nil {
		t.Fatal("Extract direct-route: missing handler ref 'create_user'")
	}
}

func TestActixExtract_DirectRouteForm_CfgStyle(t *testing.T) {
	// cfg.route("/path", web::get().to(handler)) — service-config style
	src := `
pub fn config(cfg: &mut web::ServiceConfig) {
    cfg.route("/products", web::get().to(list_products));
}
`
	r := frameworks.NewActixResolver(t.TempDir())
	nodes, refs := r.Extract("src/config.rs", src)

	if len(nodes) < 1 {
		t.Fatalf("Extract direct-route cfg: want ≥1 node, got %d", len(nodes))
	}
	n := findRustRouteNode(nodes, "GET", "/products")
	if n == nil {
		t.Fatal("Extract direct-route cfg: missing GET /products")
	}
	if findRefByNodeAndName(refs, n.ID, "list_products") == nil {
		t.Fatal("Extract direct-route cfg: missing handler ref 'list_products'")
	}
}

func TestActixExtract_CommentedRouteEmitsNothing(t *testing.T) {
	src := `
// #[get("/secret")]
// async fn secret_handler() -> HttpResponse { HttpResponse::Ok().finish() }
`
	r := frameworks.NewActixResolver(t.TempDir())
	nodes, _ := r.Extract("src/routes.rs", src)
	if len(nodes) != 0 {
		t.Fatalf("Extract: commented route should emit 0 nodes, got %d", len(nodes))
	}
}

func TestActixResolve_ConfidenceRange(t *testing.T) {
	src := `
#[get("/ping")]
async fn ping() -> &'static str { "pong" }
`
	r := frameworks.NewActixResolver(t.TempDir())
	_, _ = r.Extract("src/main.rs", src)

	if !r.ClaimsReference("ping") {
		t.Fatal("ClaimsReference: want true for 'ping'")
	}
	ref := types.UnresolvedReference{
		ReferenceName: "ping",
		Language:      types.LanguageRust,
	}
	resolved, err := r.Resolve(context.Background(), ref)
	if err != nil {
		t.Fatalf("Resolve: unexpected error: %v", err)
	}
	if resolved.Confidence < 0.8 || resolved.Confidence > 0.9 {
		t.Errorf("Resolve: confidence %v not in [0.8, 0.9]", resolved.Confidence)
	}
}

// ---------------------------------------------------------------------------
// Axum
// ---------------------------------------------------------------------------

func TestAxumDetect_WithCargoToml(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Cargo.toml"), []byte(`[dependencies]
axum = "0.7"
tokio = { version = "1", features = ["full"] }
`), 0o644); err != nil {
		t.Fatal(err)
	}
	r := frameworks.NewAxumResolver(dir)
	if !r.Detect(context.Background()) {
		t.Fatal("Detect: want true when Cargo.toml has axum")
	}
}

func TestAxumDetect_NoAxum(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Cargo.toml"), []byte(`[dependencies]
actix-web = "4"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	r := frameworks.NewAxumResolver(dir)
	if r.Detect(context.Background()) {
		t.Fatal("Detect: want false when axum absent")
	}
}

func TestAxumExtract_SimpleRoutes(t *testing.T) {
	src := `
use axum::{Router, routing::{get, post}};

let app = Router::new()
    .route("/users", get(list_users))
    .route("/users", post(create_user));
`
	r := frameworks.NewAxumResolver(t.TempDir())
	nodes, refs := r.Extract("src/main.rs", src)

	if len(nodes) < 2 {
		t.Fatalf("Extract: want ≥2 nodes, got %d", len(nodes))
	}

	getUsersNode := findRustRouteNode(nodes, "GET", "/users")
	if getUsersNode == nil {
		t.Fatal("Extract: missing GET /users")
	}
	assertRouteNodeFormat(t, *getUsersNode, "src/main.rs", "GET", "/users", types.LanguageRust)

	postUsersNode := findRustRouteNode(nodes, "POST", "/users")
	if postUsersNode == nil {
		t.Fatal("Extract: missing POST /users")
	}
	if findRefByNodeAndName(refs, getUsersNode.ID, "list_users") == nil {
		t.Fatal("Extract: missing handler ref 'list_users'")
	}
	if findRefByNodeAndName(refs, postUsersNode.ID, "create_user") == nil {
		t.Fatal("Extract: missing handler ref 'create_user'")
	}
}

func TestAxumExtract_MethodChainFanOut(t *testing.T) {
	// get(h).post(h2) on the same .route() call → two nodes
	src := `
let app = Router::new()
    .route("/orders", get(list_orders).post(create_order));
`
	r := frameworks.NewAxumResolver(t.TempDir())
	nodes, refs := r.Extract("src/router.rs", src)

	if len(nodes) < 2 {
		t.Fatalf("Extract method-chain fan-out: want ≥2 nodes, got %d", len(nodes))
	}

	getNode := findRustRouteNode(nodes, "GET", "/orders")
	if getNode == nil {
		t.Fatal("Extract: missing GET /orders from method-chain")
	}
	postNode := findRustRouteNode(nodes, "POST", "/orders")
	if postNode == nil {
		t.Fatal("Extract: missing POST /orders from method-chain")
	}
	if findRefByNodeAndName(refs, getNode.ID, "list_orders") == nil {
		t.Fatal("Extract: missing handler ref 'list_orders'")
	}
	if findRefByNodeAndName(refs, postNode.ID, "create_order") == nil {
		t.Fatal("Extract: missing handler ref 'create_order'")
	}
}

func TestAxumExtract_CommentedRouteEmitsNothing(t *testing.T) {
	src := `
// .route("/secret", get(secret_handler))
`
	r := frameworks.NewAxumResolver(t.TempDir())
	nodes, _ := r.Extract("src/main.rs", src)
	if len(nodes) != 0 {
		t.Fatalf("Extract: commented route should emit 0 nodes, got %d", len(nodes))
	}
}

func TestAxumResolve_ConfidenceRange(t *testing.T) {
	src := `
let app = Router::new().route("/health", get(health_check));
`
	r := frameworks.NewAxumResolver(t.TempDir())
	_, _ = r.Extract("src/main.rs", src)

	if !r.ClaimsReference("health_check") {
		t.Fatal("ClaimsReference: want true for 'health_check'")
	}
	ref := types.UnresolvedReference{
		ReferenceName: "health_check",
		Language:      types.LanguageRust,
	}
	resolved, err := r.Resolve(context.Background(), ref)
	if err != nil {
		t.Fatalf("Resolve: unexpected error: %v", err)
	}
	if resolved.Confidence < 0.8 || resolved.Confidence > 0.9 {
		t.Errorf("Resolve: confidence %v not in [0.8, 0.9]", resolved.Confidence)
	}
}

// ---------------------------------------------------------------------------
// Rocket
// ---------------------------------------------------------------------------

func TestRocketDetect_WithCargoToml(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Cargo.toml"), []byte(`[dependencies]
rocket = "0.5"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	r := frameworks.NewRocketResolver(dir)
	if !r.Detect(context.Background()) {
		t.Fatal("Detect: want true when Cargo.toml has rocket")
	}
}

func TestRocketDetect_NoRocket(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Cargo.toml"), []byte(`[dependencies]
axum = "0.7"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	r := frameworks.NewRocketResolver(dir)
	if r.Detect(context.Background()) {
		t.Fatal("Detect: want false when rocket absent")
	}
}

func TestRocketExtract_AttributeMacro(t *testing.T) {
	src := `
use rocket::get;

#[get("/health")]
fn health_check() -> &'static str {
    "ok"
}

#[post("/data", data = "<input>")]
fn submit_data(input: Json<Data>) -> Status {
    Status::Created
}
`
	r := frameworks.NewRocketResolver(t.TempDir())
	nodes, refs := r.Extract("src/routes.rs", src)

	if len(nodes) < 2 {
		t.Fatalf("Extract: want ≥2 nodes, got %d", len(nodes))
	}

	getNode := findRustRouteNode(nodes, "GET", "/health")
	if getNode == nil {
		t.Fatal("Extract: missing GET /health")
	}
	assertRouteNodeFormat(t, *getNode, "src/routes.rs", "GET", "/health", types.LanguageRust)
	if findRefByNodeAndName(refs, getNode.ID, "health_check") == nil {
		t.Fatal("Extract: missing handler ref 'health_check'")
	}

	postNode := findRustRouteNode(nodes, "POST", "/data")
	if postNode == nil {
		t.Fatal("Extract: missing POST /data")
	}
	if findRefByNodeAndName(refs, postNode.ID, "submit_data") == nil {
		t.Fatal("Extract: missing handler ref 'submit_data'")
	}
}

func TestRocketExtract_BoundedLookahead_SkipsSecondAttribute(t *testing.T) {
	// A second #[...] attribute between route macro and fn definition
	src := `
#[get("/items")]
#[allow(dead_code)]
fn list_items() -> Vec<Item> {
    vec![]
}
`
	r := frameworks.NewRocketResolver(t.TempDir())
	nodes, refs := r.Extract("src/main.rs", src)

	if len(nodes) != 1 {
		t.Fatalf("Extract bounded lookahead: want 1 node, got %d", len(nodes))
	}
	if findRefByNodeAndName(refs, nodes[0].ID, "list_items") == nil {
		t.Fatal("Extract: bounded lookahead should skip second #[...] and find list_items")
	}
}

func TestRocketExtract_CommentedRouteEmitsNothing(t *testing.T) {
	src := `
// #[get("/secret")]
// fn secret() -> String { "hidden".to_string() }
`
	r := frameworks.NewRocketResolver(t.TempDir())
	nodes, _ := r.Extract("src/routes.rs", src)
	if len(nodes) != 0 {
		t.Fatalf("Extract: commented route should emit 0 nodes, got %d", len(nodes))
	}
}

func TestRocketResolve_ConfidenceRange(t *testing.T) {
	src := `
#[get("/ping")]
fn ping() -> &'static str { "pong" }
`
	r := frameworks.NewRocketResolver(t.TempDir())
	_, _ = r.Extract("src/main.rs", src)

	if !r.ClaimsReference("ping") {
		t.Fatal("ClaimsReference: want true for 'ping'")
	}
	ref := types.UnresolvedReference{
		ReferenceName: "ping",
		Language:      types.LanguageRust,
	}
	resolved, err := r.Resolve(context.Background(), ref)
	if err != nil {
		t.Fatalf("Resolve: unexpected error: %v", err)
	}
	if resolved.Confidence < 0.8 || resolved.Confidence > 0.9 {
		t.Errorf("Resolve: confidence %v not in [0.8, 0.9]", resolved.Confidence)
	}
}

// ---------------------------------------------------------------------------
// Actix-web — bare method form (no web:: prefix), real RealWorld app idiom
// ---------------------------------------------------------------------------

func TestActixExtract_BareMethodChainForm(t *testing.T) {
	// Real rw-actix idiom: method functions imported directly (no web:: prefix),
	// empty-string paths inside web::scope("/user"), and fully qualified handler paths.
	// After F-79 fix: paths are prefixed with the enclosing scope.
	//
	// .route("", get().to(get_current_user))   inside /user → GET /user
	// .route("/login", web::post().to(login))  inside /user → POST /user/login
	src := `
use actix_web::web::{delete, get, post, put};

pub fn api(cfg: &mut web::ServiceConfig) {
    cfg.service(
        web::scope("/user")
            .route("", get().to(get_current_user))
            .route("/login", web::post().to(login)),
    );
}
`
	r := frameworks.NewActixResolver(t.TempDir())
	nodes, refs := r.Extract("src/routes.rs", src)

	if len(nodes) < 2 {
		t.Fatalf("Extract bare-method: want ≥2 nodes, got %d (routes.rs uses bare get()/post() without web:: prefix)", len(nodes))
	}

	// .route("", get().to(get_current_user)) inside /user → GET /user
	getNode := findRustRouteNode(nodes, "GET", "/user")
	if getNode == nil {
		t.Fatalf("Extract bare-method: missing GET /user node (scope prefix applied); got: %v", rustNodeNames(nodes))
	}
	assertRouteNodeFormat(t, *getNode, "src/routes.rs", "GET", "/user", types.LanguageRust)
	if findRefByNodeAndName(refs, getNode.ID, "get_current_user") == nil {
		t.Fatal("Extract bare-method: missing handler ref 'get_current_user'")
	}

	// .route("/login", web::post().to(login)) inside /user → POST /user/login
	postNode := findRustRouteNode(nodes, "POST", "/user/login")
	if postNode == nil {
		t.Fatalf("Extract bare-method: missing POST /user/login node (scope prefix applied); got: %v", rustNodeNames(nodes))
	}
	assertRouteNodeFormat(t, *postNode, "src/routes.rs", "POST", "/user/login", types.LanguageRust)
	if findRefByNodeAndName(refs, postNode.ID, "login") == nil {
		t.Fatal("Extract bare-method: missing handler ref 'login'")
	}
}

// TestActixExtract_ScopePrefix verifies that routes inside web::scope(...)
// carry the scope path as prefix. F-79: web::scope("/users").route("", ...) →
// GET /users; web::scope("/users").route("/login", ...) → POST /users/login.
func TestActixExtract_ScopePrefix(t *testing.T) {
	src := `
use actix_web::web::{get, post};

pub fn api(cfg: &mut web::ServiceConfig) {
    cfg.service(
        web::scope("/users")
            .route("", get().to(me))
            .route("/login", post().to(login)),
    );
}
`
	r := frameworks.NewActixResolver(t.TempDir())
	nodes, refs := r.Extract("src/routes.rs", src)

	if len(nodes) < 2 {
		t.Fatalf("Extract scope-prefix: want ≥2 nodes, got %d", len(nodes))
	}

	// .route("", get().to(me)) inside /users → GET /users (empty relative → just prefix)
	getNode := findRustRouteNode(nodes, "GET", "/users")
	if getNode == nil {
		t.Fatalf("Extract scope-prefix: missing GET /users (got nodes: %v)", rustNodeNames(nodes))
	}
	if findRefByNodeAndName(refs, getNode.ID, "me") == nil {
		t.Fatal("Extract scope-prefix: missing handler ref 'me'")
	}

	// .route("/login", post().to(login)) inside /users → POST /users/login
	postNode := findRustRouteNode(nodes, "POST", "/users/login")
	if postNode == nil {
		t.Fatalf("Extract scope-prefix: missing POST /users/login (got nodes: %v)", rustNodeNames(nodes))
	}
	if findRefByNodeAndName(refs, postNode.ID, "login") == nil {
		t.Fatal("Extract scope-prefix: missing handler ref 'login'")
	}

	// must NOT emit the scope-relative paths
	if findRustRouteNode(nodes, "GET", "") != nil {
		t.Error("Extract scope-prefix: scope-relative GET \"\" should not be emitted; prefix must be applied")
	}
	if findRustRouteNode(nodes, "POST", "/login") != nil {
		t.Error("Extract scope-prefix: scope-relative POST /login should not be emitted; prefix must be applied")
	}
}

// TestActixExtract_ScopePrefix_NonScopedUnchanged verifies that direct .route()
// calls outside any web::scope keep their literal path.
func TestActixExtract_ScopePrefix_NonScopedUnchanged(t *testing.T) {
	src := `
App::new()
    .route("/health", web::get().to(health_check));
`
	r := frameworks.NewActixResolver(t.TempDir())
	nodes, refs := r.Extract("src/main.rs", src)

	if len(nodes) != 1 {
		t.Fatalf("Extract non-scoped: want 1 node, got %d", len(nodes))
	}
	if findRustRouteNode(nodes, "GET", "/health") == nil {
		t.Fatalf("Extract non-scoped: want GET /health, got nodes: %v", rustNodeNames(nodes))
	}
	if findRefByNodeAndName(refs, nodes[0].ID, "health_check") == nil {
		t.Fatal("Extract non-scoped: missing handler ref 'health_check'")
	}
}

// rustNodeNames is a test helper that returns node names for diagnostics.
func rustNodeNames(nodes []types.Node) []string {
	names := make([]string, len(nodes))
	for i, n := range nodes {
		names[i] = n.Name
	}
	return names
}

func TestActixExtract_BareMethodChainForm_QualifiedHandler(t *testing.T) {
	// Real rw-actix uses fully qualified handler paths like
	// app::features::user::controllers::me — rustLastSegment must reduce to "me".
	// After F-79 fix: paths carry the enclosing web::scope("/user") prefix.
	src := `
use actix_web::web::{delete, get, post, put};

pub fn api(cfg: &mut web::ServiceConfig) {
    cfg.service(
        web::scope("/user")
            .route("", get().to(app::features::user::controllers::me))
            .route("", put().to(app::features::user::controllers::update)),
    );
}
`
	r := frameworks.NewActixResolver(t.TempDir())
	nodes, refs := r.Extract("src/routes.rs", src)

	if len(nodes) < 2 {
		t.Fatalf("Extract bare-method qualified: want ≥2 nodes, got %d", len(nodes))
	}

	// .route("", ...) inside /user → GET /user (scope prefix applied)
	getNode := findRustRouteNode(nodes, "GET", "/user")
	if getNode == nil {
		t.Fatalf("Extract bare-method qualified: missing GET /user node; got: %v", rustNodeNames(nodes))
	}
	// handler ref uses last segment of the qualified path
	if findRefByNodeAndName(refs, getNode.ID, "me") == nil {
		t.Fatal("Extract bare-method qualified: handler ref should be 'me' (last segment)")
	}

	// .route("", put()...) inside /user → PUT /user
	putNode := findRustRouteNode(nodes, "PUT", "/user")
	if putNode == nil {
		t.Fatalf("Extract bare-method qualified: missing PUT /user node; got: %v", rustNodeNames(nodes))
	}
	if findRefByNodeAndName(refs, putNode.ID, "update") == nil {
		t.Fatal("Extract bare-method qualified: handler ref should be 'update'")
	}
}
