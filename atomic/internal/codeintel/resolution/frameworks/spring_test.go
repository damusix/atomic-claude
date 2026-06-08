package frameworks_test

// Failing-first TDD tests for CP15 batch D (Java): Spring MVC/Boot.
//
// Coverage:
//   - Detect: pom.xml / build.gradle / import-based detection.
//   - Extract: class @RequestMapping prefix + method annotations → full path.
//   - @GetMapping/@PostMapping/@PutMapping/@DeleteMapping/@PatchMapping fan-out.
//   - @RequestMapping(value=..., method=RequestMethod.GET) explicit method form.
//   - @RequestMapping without method → "ANY".
//   - Class-prefix join: @RequestMapping("/api") + @GetMapping("/users") → GET /api/users.
//   - Commented routes emit nothing.
//   - Resolve: confidence in [0.8, 0.9]; ClaimsReference correct.

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/resolution/frameworks"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// ---------------------------------------------------------------------------
// Detect tests
// ---------------------------------------------------------------------------

func TestSpringDetect_PomXml(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "pom.xml"), []byte(`<project>
  <dependencies>
    <dependency>
      <groupId>org.springframework.boot</groupId>
      <artifactId>spring-boot-starter-web</artifactId>
    </dependency>
  </dependencies>
</project>
`), 0o644); err != nil {
		t.Fatal(err)
	}
	r := frameworks.NewSpringResolver(dir)
	if !r.Detect(context.Background()) {
		t.Fatal("Detect: want true when pom.xml has org.springframework")
	}
}

func TestSpringDetect_BuildGradle(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "build.gradle"), []byte(`dependencies {
    implementation 'org.springframework.boot:spring-boot-starter-web'
}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	r := frameworks.NewSpringResolver(dir)
	if !r.Detect(context.Background()) {
		t.Fatal("Detect: want true when build.gradle has org.springframework")
	}
}

func TestSpringDetect_ImportBased(t *testing.T) {
	// Falls back to scanning .java files for org.springframework import
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "UserController.java"), []byte(`import org.springframework.web.bind.annotation.RestController;

@RestController
public class UserController {}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	r := frameworks.NewSpringResolver(dir)
	if !r.Detect(context.Background()) {
		t.Fatal("Detect: want true when java file has org.springframework import")
	}
}

func TestSpringDetect_NoSpring(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "pom.xml"), []byte(`<project>
  <dependencies>
    <dependency><groupId>com.example</groupId></dependency>
  </dependencies>
</project>
`), 0o644); err != nil {
		t.Fatal(err)
	}
	r := frameworks.NewSpringResolver(dir)
	if r.Detect(context.Background()) {
		t.Fatal("Detect: want false when no spring dependency")
	}
}

// ---------------------------------------------------------------------------
// Extract tests
// ---------------------------------------------------------------------------

func TestSpringExtract_ClassPrefixJoin(t *testing.T) {
	// Class-level @RequestMapping("/api") + @GetMapping("/users") → GET /api/users
	src := `
import org.springframework.web.bind.annotation.*;

@RestController
@RequestMapping("/api")
public class UserController {

    @GetMapping("/users")
    public List<User> listUsers() {
        return userService.findAll();
    }

    @PostMapping("/users")
    public User createUser(@RequestBody User user) {
        return userService.save(user);
    }
}
`
	r := frameworks.NewSpringResolver(t.TempDir())
	nodes, refs := r.Extract("src/UserController.java", src)

	if len(nodes) < 2 {
		t.Fatalf("Extract class-prefix join: want ≥2 nodes, got %d", len(nodes))
	}

	getNode := findRustRouteNode(nodes, "GET", "/api/users")
	if getNode == nil {
		t.Fatalf("Extract: missing GET /api/users; got nodes: %v", springNodeNames(nodes))
	}
	assertRouteNodeFormat(t, *getNode, "src/UserController.java", "GET", "/api/users", types.LanguageJava)

	if findRefByNodeAndName(refs, getNode.ID, "listUsers") == nil {
		t.Fatal("Extract: missing handler ref 'listUsers'")
	}

	postNode := findRustRouteNode(nodes, "POST", "/api/users")
	if postNode == nil {
		t.Fatal("Extract: missing POST /api/users")
	}
	if findRefByNodeAndName(refs, postNode.ID, "createUser") == nil {
		t.Fatal("Extract: missing handler ref 'createUser'")
	}
}

func TestSpringExtract_RequestMappingWithMethod(t *testing.T) {
	// @RequestMapping(value="/sub", method=RequestMethod.GET)
	src := `
@RestController
@RequestMapping("/items")
public class ItemController {

    @RequestMapping(value = "/list", method = RequestMethod.GET)
    public List<Item> getItems() {
        return itemService.list();
    }

    @RequestMapping(value = "/create", method = RequestMethod.POST)
    public Item postItem(@RequestBody Item item) {
        return itemService.create(item);
    }
}
`
	r := frameworks.NewSpringResolver(t.TempDir())
	nodes, refs := r.Extract("src/ItemController.java", src)

	if len(nodes) < 2 {
		t.Fatalf("Extract @RequestMapping explicit method: want ≥2 nodes, got %d", len(nodes))
	}

	getNode := findRustRouteNode(nodes, "GET", "/items/list")
	if getNode == nil {
		t.Fatalf("Extract: missing GET /items/list; got: %v", springNodeNames(nodes))
	}
	if findRefByNodeAndName(refs, getNode.ID, "getItems") == nil {
		t.Fatal("Extract: missing handler ref 'getItems'")
	}

	postNode := findRustRouteNode(nodes, "POST", "/items/create")
	if postNode == nil {
		t.Fatal("Extract: missing POST /items/create")
	}
	if findRefByNodeAndName(refs, postNode.ID, "postItem") == nil {
		t.Fatal("Extract: missing handler ref 'postItem'")
	}
}

func TestSpringExtract_RequestMappingNoMethod_IsANY(t *testing.T) {
	// @RequestMapping without explicit method → "ANY"
	src := `
@RestController
public class PingController {

    @RequestMapping("/ping")
    public String ping() {
        return "pong";
    }
}
`
	r := frameworks.NewSpringResolver(t.TempDir())
	nodes, refs := r.Extract("src/PingController.java", src)

	if len(nodes) == 0 {
		t.Fatal("Extract: want ≥1 node for @RequestMapping without method")
	}

	anyNode := findRustRouteNode(nodes, "ANY", "/ping")
	if anyNode == nil {
		t.Fatalf("Extract: @RequestMapping no method should emit ANY /ping; got: %v", springNodeNames(nodes))
	}
	if findRefByNodeAndName(refs, anyNode.ID, "ping") == nil {
		t.Fatal("Extract: missing handler ref 'ping'")
	}
}

func TestSpringExtract_AllMappingAnnotations(t *testing.T) {
	// @GetMapping, @PostMapping, @PutMapping, @DeleteMapping, @PatchMapping
	src := `
@RestController
@RequestMapping("/v1")
public class OrderController {

    @GetMapping("/orders")
    public List<Order> list() { return null; }

    @PostMapping("/orders")
    public Order create() { return null; }

    @PutMapping("/orders/{id}")
    public Order update() { return null; }

    @DeleteMapping("/orders/{id}")
    public void delete() {}

    @PatchMapping("/orders/{id}")
    public Order patch() { return null; }
}
`
	r := frameworks.NewSpringResolver(t.TempDir())
	nodes, _ := r.Extract("src/OrderController.java", src)

	wantRoutes := []struct{ method, path string }{
		{"GET", "/v1/orders"},
		{"POST", "/v1/orders"},
		{"PUT", "/v1/orders/{id}"},
		{"DELETE", "/v1/orders/{id}"},
		{"PATCH", "/v1/orders/{id}"},
	}
	for _, w := range wantRoutes {
		if findRustRouteNode(nodes, w.method, w.path) == nil {
			t.Errorf("Extract: missing %s %s; got: %v", w.method, w.path, springNodeNames(nodes))
		}
	}
}

func TestSpringExtract_NoPrefixController(t *testing.T) {
	// No @RequestMapping on class → sub-paths stand alone
	src := `
@RestController
public class HealthController {

    @GetMapping("/health")
    public String health() {
        return "UP";
    }
}
`
	r := frameworks.NewSpringResolver(t.TempDir())
	nodes, refs := r.Extract("src/HealthController.java", src)

	if len(nodes) == 0 {
		t.Fatal("Extract: want ≥1 node when no class prefix")
	}
	n := findRustRouteNode(nodes, "GET", "/health")
	if n == nil {
		t.Fatalf("Extract: missing GET /health; got: %v", springNodeNames(nodes))
	}
	if findRefByNodeAndName(refs, n.ID, "health") == nil {
		t.Fatal("Extract: missing handler ref 'health'")
	}
}

func TestSpringExtract_CommentedRouteEmitsNothing(t *testing.T) {
	src := `
// @GetMapping("/secret")
// public String secret() { return "hidden"; }
`
	r := frameworks.NewSpringResolver(t.TempDir())
	nodes, _ := r.Extract("src/Ctrl.java", src)
	if len(nodes) != 0 {
		t.Fatalf("Extract: commented route should emit 0 nodes, got %d", len(nodes))
	}
}

// ---------------------------------------------------------------------------
// Resolve / ClaimsReference
// ---------------------------------------------------------------------------

func TestSpringResolve_ConfidenceRange(t *testing.T) {
	src := `
@RestController
public class TestCtrl {
    @GetMapping("/test")
    public String test() { return "ok"; }
}
`
	r := frameworks.NewSpringResolver(t.TempDir())
	_, _ = r.Extract("src/TestCtrl.java", src)

	if !r.ClaimsReference("test") {
		t.Fatal("ClaimsReference: want true for 'test'")
	}
	ref := types.UnresolvedReference{
		ReferenceName: "test",
		Language:      types.LanguageJava,
	}
	resolved, err := r.Resolve(context.Background(), ref)
	if err != nil {
		t.Fatalf("Resolve: unexpected error: %v", err)
	}
	if resolved.Confidence < 0.8 || resolved.Confidence > 0.9 {
		t.Errorf("Resolve: confidence %v not in [0.8, 0.9]", resolved.Confidence)
	}
}

func TestSpringClaimsReference_False(t *testing.T) {
	r := frameworks.NewSpringResolver(t.TempDir())
	if r.ClaimsReference("nonexistent") {
		t.Fatal("ClaimsReference: want false for name never seen in Extract")
	}
}

// ---------------------------------------------------------------------------
// Local helper
// ---------------------------------------------------------------------------

func springNodeNames(nodes []types.Node) []string {
	names := make([]string, len(nodes))
	for i, n := range nodes {
		names[i] = n.Name
	}
	return names
}
