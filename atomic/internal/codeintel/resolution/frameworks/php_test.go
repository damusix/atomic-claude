package frameworks_test

// Failing-first TDD tests for CP15 batch E (PHP): laravel and symfony resolvers.
//
// Per-framework coverage:
//  1. Detect true on a realistic composer.json fixture + false for unrelated project.
//  2. Extract emits ≥1 route node (exact appendix-H id/qn/name via MakeRouteNode) + handler ref.
//  3. Comment stripping: PHP uses stripJSComments — // and /* */ are stripped but # is NOT.
//     Symfony's #[Route(...)] attribute must survive; a //comment-prefixed line must be stripped.
//  4. Laravel fan-out: Route::match(['get','post'], ...) emits two route nodes.
//  5. Laravel @-style handler and array handler both emit correct action last-segment.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/resolution/frameworks"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// ---------------------------------------------------------------------------
// Laravel tests
// ---------------------------------------------------------------------------

func TestLaravelDetect_ComposerJSON(t *testing.T) {
	dir := t.TempDir()
	composer := `{
  "require": {
    "laravel/framework": "^11.0",
    "php": "^8.2"
  }
}`
	if err := os.WriteFile(filepath.Join(dir, "composer.json"), []byte(composer), 0o644); err != nil {
		t.Fatal(err)
	}
	r := frameworks.NewLaravelResolver(dir)
	if !r.Detect(context.Background()) {
		t.Error("Laravel.Detect should return true when composer.json lists laravel/framework")
	}
}

func TestLaravelDetect_False(t *testing.T) {
	dir := t.TempDir()
	// Symfony-only project — no laravel/framework
	composer := `{
  "require": {
    "symfony/framework-bundle": "^7.0"
  }
}`
	if err := os.WriteFile(filepath.Join(dir, "composer.json"), []byte(composer), 0o644); err != nil {
		t.Fatal(err)
	}
	r := frameworks.NewLaravelResolver(dir)
	if r.Detect(context.Background()) {
		t.Error("Laravel.Detect should return false for a non-laravel project")
	}
}

func TestLaravelExtract_ArrayHandler(t *testing.T) {
	dir := t.TempDir()
	src := `<?php
use Illuminate\Support\Facades\Route;
Route::get('/users', [UserController::class, 'index']);
`
	r := frameworks.NewLaravelResolver(dir)
	nodes, refs := r.Extract("routes/web.php", src)

	if len(nodes) != 1 {
		t.Fatalf("expected 1 route node, got %d", len(nodes))
	}
	n := nodes[0]
	// Verify appendix-H id format: route:{filePath}:{line}:{METHOD}:{path}
	wantID := "route:routes/web.php:3:GET:/users"
	if n.ID != wantID {
		t.Errorf("node ID = %q, want %q", n.ID, wantID)
	}
	wantQN := "routes/web.php::METHOD:/users"
	if n.QualifiedName != wantQN {
		t.Errorf("node QN = %q, want %q", n.QualifiedName, wantQN)
	}
	wantName := "GET /users"
	if n.Name != wantName {
		t.Errorf("node Name = %q, want %q", n.Name, wantName)
	}
	if n.Language != types.LanguagePHP {
		t.Errorf("node Language = %v, want PHP", n.Language)
	}
	if n.Kind != types.NodeKindRoute {
		t.Errorf("node Kind = %v, want Route", n.Kind)
	}

	// Handler ref: last segment of array 2nd element = 'index'
	if len(refs) != 1 {
		t.Fatalf("expected 1 ref, got %d", len(refs))
	}
	if refs[0].ReferenceName != "index" {
		t.Errorf("ref name = %q, want %q", refs[0].ReferenceName, "index")
	}
	if refs[0].FromNodeID != n.ID {
		t.Errorf("ref FromNodeID = %q, want %q", refs[0].FromNodeID, n.ID)
	}
}

func TestLaravelExtract_AtHandler(t *testing.T) {
	dir := t.TempDir()
	src := `<?php
Route::post('/orders', 'OrderController@store');
`
	r := frameworks.NewLaravelResolver(dir)
	nodes, refs := r.Extract("routes/web.php", src)

	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}
	if nodes[0].ID != "route:routes/web.php:2:POST:/orders" {
		t.Errorf("unexpected node ID: %s", nodes[0].ID)
	}
	if len(refs) != 1 {
		t.Fatalf("expected 1 ref, got %d", len(refs))
	}
	// Handler last segment after '@' = 'store'
	if refs[0].ReferenceName != "store" {
		t.Errorf("ref name = %q, want %q", refs[0].ReferenceName, "store")
	}
}

func TestLaravelExtract_MatchFanOut(t *testing.T) {
	dir := t.TempDir()
	src := `<?php
Route::match(['get', 'post'], '/search', [SearchController::class, 'handle']);
`
	r := frameworks.NewLaravelResolver(dir)
	nodes, refs := r.Extract("routes/web.php", src)

	if len(nodes) != 2 {
		t.Fatalf("Route::match with 2 methods should emit 2 nodes, got %d", len(nodes))
	}
	methods := map[string]bool{}
	for _, n := range nodes {
		parts := strings.SplitN(n.Name, " ", 2)
		methods[parts[0]] = true
	}
	if !methods["GET"] || !methods["POST"] {
		t.Errorf("fan-out nodes missing GET or POST: %v", methods)
	}
	// Each method emits its own ref to 'handle'
	if len(refs) != 2 {
		t.Fatalf("expected 2 refs for fan-out, got %d", len(refs))
	}
	for _, ref := range refs {
		if ref.ReferenceName != "handle" {
			t.Errorf("ref name = %q, want %q", ref.ReferenceName, "handle")
		}
	}
}

func TestLaravelExtract_CommentStripped(t *testing.T) {
	dir := t.TempDir()
	// JS-style comment should be stripped; the route inside never fires
	src := `<?php
// Route::get('/hidden', 'Foo@bar');
/* Route::post('/also-hidden', 'Foo@baz'); */
Route::get('/visible', [HomeController::class, 'index']);
`
	r := frameworks.NewLaravelResolver(dir)
	nodes, _ := r.Extract("routes/web.php", src)

	if len(nodes) != 1 {
		t.Errorf("expected 1 route node (commented-out routes stripped), got %d: %v",
			len(nodes), nodesIDs(nodes))
	}
	if nodes[0].ID != "route:routes/web.php:4:GET:/visible" {
		t.Errorf("unexpected node ID: %s", nodes[0].ID)
	}
}

func TestLaravelExtract_NoMatchForUnrelated(t *testing.T) {
	r := frameworks.NewLaravelResolver(t.TempDir())
	nodes, refs := r.Extract("app/Http/Controller.php", `<?php
class HomeController extends Controller {
    public function index() { return view('home'); }
}
`)
	if len(nodes) != 0 || len(refs) != 0 {
		t.Errorf("non-route PHP file should emit nothing, got %d nodes %d refs", len(nodes), len(refs))
	}
}

func TestLaravelResolve(t *testing.T) {
	dir := t.TempDir()
	src := `<?php
Route::get('/ping', [PingController::class, 'ping']);
`
	r := frameworks.NewLaravelResolver(dir)
	r.Extract("routes/web.php", src)

	ref := types.UnresolvedReference{ReferenceName: "ping"}
	resolved, err := r.Resolve(context.Background(), ref)
	if err != nil {
		t.Fatalf("Resolve error: %v", err)
	}
	if resolved.Confidence < 0.8 || resolved.Confidence > 0.9 {
		t.Errorf("Resolve confidence = %v, want 0.8–0.9", resolved.Confidence)
	}
	if !r.ClaimsReference("ping") {
		t.Error("ClaimsReference should return true after Extract sees handler 'ping'")
	}
}

// ---------------------------------------------------------------------------
// Symfony tests
// ---------------------------------------------------------------------------

func TestSymfonyDetect_ComposerJSON(t *testing.T) {
	dir := t.TempDir()
	composer := `{
  "require": {
    "symfony/framework-bundle": "^7.0"
  }
}`
	if err := os.WriteFile(filepath.Join(dir, "composer.json"), []byte(composer), 0o644); err != nil {
		t.Fatal(err)
	}
	r := frameworks.NewSymfonyResolver(dir)
	if !r.Detect(context.Background()) {
		t.Error("Symfony.Detect should return true when composer.json lists symfony/framework-bundle")
	}
}

func TestSymfonyDetect_RoutingBundle(t *testing.T) {
	dir := t.TempDir()
	composer := `{
  "require": {
    "symfony/routing": "^7.0"
  }
}`
	if err := os.WriteFile(filepath.Join(dir, "composer.json"), []byte(composer), 0o644); err != nil {
		t.Fatal(err)
	}
	r := frameworks.NewSymfonyResolver(dir)
	if !r.Detect(context.Background()) {
		t.Error("Symfony.Detect should return true when composer.json lists symfony/routing")
	}
}

func TestSymfonyDetect_False(t *testing.T) {
	dir := t.TempDir()
	// Laravel-only project
	composer := `{
  "require": {
    "laravel/framework": "^11.0"
  }
}`
	if err := os.WriteFile(filepath.Join(dir, "composer.json"), []byte(composer), 0o644); err != nil {
		t.Fatal(err)
	}
	r := frameworks.NewSymfonyResolver(dir)
	if r.Detect(context.Background()) {
		t.Error("Symfony.Detect should return false for a non-symfony project")
	}
}

func TestSymfonyExtract_AttributeRouteGET(t *testing.T) {
	dir := t.TempDir()
	src := `<?php
namespace App\Controller;

use Symfony\Component\Routing\Annotation\Route;

class HomeController
{
    #[Route('/home', methods: ['GET'])]
    public function index(): Response
    {
        return new Response('home');
    }
}
`
	r := frameworks.NewSymfonyResolver(dir)
	nodes, refs := r.Extract("src/Controller/HomeController.php", src)

	if len(nodes) != 1 {
		t.Fatalf("expected 1 route node, got %d", len(nodes))
	}
	n := nodes[0]
	wantID := fmt.Sprintf("route:src/Controller/HomeController.php:%d:GET:/home", n.StartLine)
	if n.ID != wantID {
		t.Errorf("node ID = %q, want %q", n.ID, wantID)
	}
	if n.Language != types.LanguagePHP {
		t.Errorf("node Language = %v, want PHP", n.Language)
	}

	// Handler ref = function name 'index'
	if len(refs) != 1 {
		t.Fatalf("expected 1 ref, got %d", len(refs))
	}
	if refs[0].ReferenceName != "index" {
		t.Errorf("ref name = %q, want %q", refs[0].ReferenceName, "index")
	}
}

func TestSymfonyExtract_AttributeRouteFanOut(t *testing.T) {
	src := `<?php
    #[Route('/submit', methods: ['GET', 'POST'])]
    public function submit(): Response
    {
        return new Response('ok');
    }
`
	r := frameworks.NewSymfonyResolver(t.TempDir())
	nodes, refs := r.Extract("src/Controller/FormController.php", src)

	if len(nodes) != 2 {
		t.Fatalf("methods:['GET','POST'] should fan-out to 2 nodes, got %d", len(nodes))
	}
	methods := map[string]bool{}
	for _, n := range nodes {
		parts := strings.SplitN(n.Name, " ", 2)
		methods[parts[0]] = true
	}
	if !methods["GET"] || !methods["POST"] {
		t.Errorf("fan-out missing GET or POST: %v", methods)
	}
	// Both refs point to 'submit'
	if len(refs) != 2 {
		t.Fatalf("expected 2 refs, got %d", len(refs))
	}
	for _, ref := range refs {
		if ref.ReferenceName != "submit" {
			t.Errorf("ref name = %q, want submit", ref.ReferenceName)
		}
	}
}

func TestSymfonyExtract_NoMethodsIsANY(t *testing.T) {
	src := `<?php
    #[Route('/api/data')]
    public function data(): Response { }
`
	r := frameworks.NewSymfonyResolver(t.TempDir())
	nodes, _ := r.Extract("src/Controller/ApiController.php", src)

	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}
	if !strings.HasPrefix(nodes[0].Name, "ANY ") {
		t.Errorf("no methods should produce ANY, got %q", nodes[0].Name)
	}
}

// THE KEY TRAP: #[Route] is a PHP 8 attribute (not a comment) and must survive
// even though stripJSComments is used for PHP.
func TestSymfonyExtract_AttributeSurvivesCommentStrip(t *testing.T) {
	// The #[Route] line should NOT be stripped by stripJSComments (which only
	// strips // and /* */; # is left untouched in PHP mode).
	src := `<?php
    #[Route('/products', methods: ['GET'])]
    public function list(): Response { }
`
	r := frameworks.NewSymfonyResolver(t.TempDir())
	nodes, _ := r.Extract("src/Controller/ProductController.php", src)

	if len(nodes) == 0 {
		t.Error("#[Route] PHP attribute must NOT be stripped by comment stripping — got 0 nodes")
	}
}

// Companion: a real // comment IS stripped and emits nothing.
func TestSymfonyExtract_JSCommentStripped(t *testing.T) {
	src := `<?php
    // #[Route('/hidden', methods: ['GET'])]
    // public function hidden(): Response { }
    #[Route('/visible', methods: ['GET'])]
    public function visible(): Response { }
`
	r := frameworks.NewSymfonyResolver(t.TempDir())
	nodes, _ := r.Extract("src/Controller/MixedController.php", src)

	// The // commented route should be stripped; only '/visible' survives
	if len(nodes) != 1 {
		t.Errorf("// commented route should be stripped, got %d nodes: %v", len(nodes), nodesIDs(nodes))
	}
}

func TestSymfonyResolve(t *testing.T) {
	src := `<?php
    #[Route('/hello', methods: ['GET'])]
    public function hello(): Response { }
`
	r := frameworks.NewSymfonyResolver(t.TempDir())
	r.Extract("src/Controller/HelloController.php", src)

	ref := types.UnresolvedReference{ReferenceName: "hello"}
	resolved, err := r.Resolve(context.Background(), ref)
	if err != nil {
		t.Fatalf("Resolve error: %v", err)
	}
	if resolved.Confidence < 0.8 || resolved.Confidence > 0.9 {
		t.Errorf("Resolve confidence = %v, want 0.8–0.9", resolved.Confidence)
	}
	if !r.ClaimsReference("hello") {
		t.Error("ClaimsReference should return true after Extract sees 'hello'")
	}
}

// nodesIDs is a test helper shared across PHP tests.
func nodesIDs(nodes []types.Node) []string {
	ids := make([]string, len(nodes))
	for i, n := range nodes {
		ids[i] = n.ID
	}
	return ids
}
