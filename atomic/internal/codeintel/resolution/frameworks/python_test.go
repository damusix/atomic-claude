package frameworks_test

// Failing-first TDD tests for CP15 batch A: Python frameworks (Django, Flask, FastAPI).
//
// Per-framework coverage:
//   1. Detect true on a realistic fixture (dep file / import line) + false on unrelated dir.
//   2. Extract emits ≥1 route node (exact appendix-H id/qn/name via MakeRouteNode) + handler ref.
//   3. Comment-stripped route emits nothing (asserted for Flask; proves stripper runs first).

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
// Flask tests
// ---------------------------------------------------------------------------

func TestFlaskDetect_RequirementsTxt(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "requirements.txt"), []byte("flask==3.0.0\nrequests\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := frameworks.NewFlaskResolver(dir)
	if !r.Detect(context.Background()) {
		t.Error("Flask.Detect should return true when requirements.txt lists flask")
	}
}

func TestFlaskDetect_PyprojectToml(t *testing.T) {
	dir := t.TempDir()
	content := `[project]
name = "myapp"
dependencies = ["flask>=2.0"]
`
	if err := os.WriteFile(filepath.Join(dir, "pyproject.toml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	r := frameworks.NewFlaskResolver(dir)
	if !r.Detect(context.Background()) {
		t.Error("Flask.Detect should return true when pyproject.toml lists flask")
	}
}

func TestFlaskDetect_ContentFallback(t *testing.T) {
	dir := t.TempDir()
	src := `from flask import Flask\napp = Flask(__name__)\n`
	if err := os.WriteFile(filepath.Join(dir, "app.py"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	r := frameworks.NewFlaskResolver(dir)
	if !r.Detect(context.Background()) {
		t.Error("Flask.Detect should return true when a Python file has Flask import")
	}
}

func TestFlaskDetect_False(t *testing.T) {
	dir := t.TempDir()
	// only django in requirements — not flask
	if err := os.WriteFile(filepath.Join(dir, "requirements.txt"), []byte("django==4.2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := frameworks.NewFlaskResolver(dir)
	if r.Detect(context.Background()) {
		t.Error("Flask.Detect should return false when project has no flask")
	}
}

func TestFlaskExtract_AppRoute(t *testing.T) {
	filePath := "app/views.py"
	content := `
from flask import Flask
app = Flask(__name__)

@app.route('/users', methods=['GET', 'POST'])
def list_users():
    pass

@app.get('/users/<int:id>')
def get_user(id):
    pass
`
	r := frameworks.NewFlaskResolver("/project")
	nodes, refs := r.Extract(filePath, content)

	// Expect: GET /users, POST /users, GET /users/<int:id>
	if len(nodes) < 3 {
		t.Fatalf("Flask.Extract: want ≥3 route nodes, got %d: %v", len(nodes), nodeNames(nodes))
	}

	// Verify GET /users node
	getUsers := findNodeByName(nodes, "GET /users")
	if getUsers == nil {
		t.Fatalf("Flask.Extract: missing 'GET /users' node; got: %v", nodeNames(nodes))
	}
	wantID := "route:app/views.py:5:GET:/users"
	if getUsers.ID != wantID {
		t.Errorf("Flask.Extract route node id:\n  got  %q\n  want %q", getUsers.ID, wantID)
	}
	wantQN := "app/views.py::METHOD:/users"
	if getUsers.QualifiedName != wantQN {
		t.Errorf("Flask.Extract route node qualifiedName:\n  got  %q\n  want %q", getUsers.QualifiedName, wantQN)
	}
	if getUsers.Kind != types.NodeKindRoute {
		t.Errorf("Flask.Extract route kind: got %q want %q", getUsers.Kind, types.NodeKindRoute)
	}
	if getUsers.Language != types.LanguagePython {
		t.Errorf("Flask.Extract route language: got %q want %q", getUsers.Language, types.LanguagePython)
	}

	// Verify handler ref for list_users
	ref := findRefByNodeAndName(refs, getUsers.ID, "list_users")
	if ref == nil {
		t.Errorf("Flask.Extract: missing handler ref 'list_users' from %q; refs: %v", getUsers.ID, refNames(refs))
	} else if ref.ReferenceKind != types.EdgeKindReferences {
		t.Errorf("Flask.Extract: handler ref kind got %q want %q", ref.ReferenceKind, types.EdgeKindReferences)
	}

	// Verify POST /users
	postUsers := findNodeByName(nodes, "POST /users")
	if postUsers == nil {
		t.Fatalf("Flask.Extract: missing 'POST /users' node; got: %v", nodeNames(nodes))
	}
}

func TestFlaskExtract_BlueprintRoute(t *testing.T) {
	filePath := "app/blueprints.py"
	content := `
from flask import Blueprint
bp = Blueprint('auth', __name__)

@bp.route('/login', methods=['POST'])
def login():
    pass
`
	r := frameworks.NewFlaskResolver("/project")
	nodes, refs := r.Extract(filePath, content)

	if len(nodes) == 0 {
		t.Fatal("Flask.Extract: expected route node for blueprint @bp.route")
	}
	node := findNodeByName(nodes, "POST /login")
	if node == nil {
		t.Fatalf("Flask.Extract blueprint: missing 'POST /login'; got: %v", nodeNames(nodes))
	}
	ref := findRefByNodeAndName(refs, node.ID, "login")
	if ref == nil {
		t.Errorf("Flask.Extract blueprint: missing handler ref 'login'; refs: %v", refNames(refs))
	}
}

// TestFlaskExtract_CommentedRouteEmitsNothing proves the Python comment stripper
// runs before the route regex. Commented-out routes must emit ZERO nodes.
func TestFlaskExtract_CommentedRouteEmitsNothing(t *testing.T) {
	filePath := "app/views.py"
	content := `
# @app.route('/commented', methods=['GET'])
# def commented_view():
#     pass

@app.route('/real', methods=['GET'])
def real_view():
    pass
`
	r := frameworks.NewFlaskResolver("/project")
	nodes, _ := r.Extract(filePath, content)

	// Only the real route, not the commented-out one
	for _, n := range nodes {
		if n.Name == "GET /commented" {
			t.Errorf("Flask.Extract: commented route 'GET /commented' must not be emitted")
		}
	}
	if findNodeByName(nodes, "GET /real") == nil {
		t.Errorf("Flask.Extract: 'GET /real' should be emitted; got: %v", nodeNames(nodes))
	}
}

func TestFlaskClaimsReference(t *testing.T) {
	r := frameworks.NewFlaskResolver("/project")
	r.Extract("app/views.py", `
@app.route('/x')
def my_view():
    pass
`)
	if !r.ClaimsReference("my_view") {
		t.Error("Flask.ClaimsReference: should return true for extracted handler")
	}
	if r.ClaimsReference("unrelated_fn") {
		t.Error("Flask.ClaimsReference: should return false for unseen name")
	}
}

func TestFlaskResolve_Confidence(t *testing.T) {
	r := frameworks.NewFlaskResolver("/project")
	// Populate claims via Extract so ClaimsReference returns true.
	nodes, _ := r.Extract("app/views.py", `
@app.route('/x')
def my_view():
    pass
`)
	if len(nodes) == 0 {
		t.Fatal("TestFlaskResolve_Confidence: prerequisite Extract must emit ≥1 node")
	}
	ref := types.UnresolvedReference{
		ReferenceName: "my_view",
		ReferenceKind: types.EdgeKindReferences,
		Language:      types.LanguagePython,
		FromNodeID:    nodes[0].ID,
	}
	result, err := r.Resolve(context.Background(), ref)
	if err != nil {
		t.Fatal(err)
	}
	// Flask.Resolve has no DB — TargetNodeID will be empty — but MUST return
	// confidence 0.8–0.9 when the handler is claimed (appendix H contract).
	if result.Confidence < 0.8 || result.Confidence > 0.9 {
		t.Errorf("Flask.Resolve: confidence want 0.8–0.9, got %v", result.Confidence)
	}
}

func TestFastAPIResolve_Confidence(t *testing.T) {
	r := frameworks.NewFastAPIResolver("/project")
	nodes, _ := r.Extract("main.py", `
@app.get('/items')
def list_items():
    return []
`)
	if len(nodes) == 0 {
		t.Fatal("TestFastAPIResolve_Confidence: prerequisite Extract must emit ≥1 node")
	}
	ref := types.UnresolvedReference{
		ReferenceName: "list_items",
		ReferenceKind: types.EdgeKindReferences,
		Language:      types.LanguagePython,
		FromNodeID:    nodes[0].ID,
	}
	result, err := r.Resolve(context.Background(), ref)
	if err != nil {
		t.Fatal(err)
	}
	if result.Confidence < 0.8 || result.Confidence > 0.9 {
		t.Errorf("FastAPI.Resolve: confidence want 0.8–0.9, got %v", result.Confidence)
	}
}

func TestDjangoResolve_Confidence(t *testing.T) {
	r := frameworks.NewDjangoResolver("/project")
	nodes, _ := r.Extract("urls.py", `
urlpatterns = [
    path('users/', views.user_list),
]
`)
	if len(nodes) == 0 {
		t.Fatal("TestDjangoResolve_Confidence: prerequisite Extract must emit ≥1 node")
	}
	ref := types.UnresolvedReference{
		ReferenceName: "user_list",
		ReferenceKind: types.EdgeKindReferences,
		Language:      types.LanguagePython,
		FromNodeID:    nodes[0].ID,
	}
	result, err := r.Resolve(context.Background(), ref)
	if err != nil {
		t.Fatal(err)
	}
	if result.Confidence < 0.8 || result.Confidence > 0.9 {
		t.Errorf("Django.Resolve: confidence want 0.8–0.9, got %v", result.Confidence)
	}
}

// ---------------------------------------------------------------------------
// FastAPI tests
// ---------------------------------------------------------------------------

func TestFastAPIDetect_RequirementsTxt(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "requirements.txt"), []byte("fastapi==0.110.0\nuvicorn\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := frameworks.NewFastAPIResolver(dir)
	if !r.Detect(context.Background()) {
		t.Error("FastAPI.Detect should return true when requirements.txt lists fastapi")
	}
}

func TestFastAPIDetect_ContentFallback(t *testing.T) {
	dir := t.TempDir()
	src := "from fastapi import FastAPI\napp = FastAPI()\n"
	if err := os.WriteFile(filepath.Join(dir, "main.py"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	r := frameworks.NewFastAPIResolver(dir)
	if !r.Detect(context.Background()) {
		t.Error("FastAPI.Detect should return true when a Python file imports FastAPI")
	}
}

func TestFastAPIDetect_False(t *testing.T) {
	dir := t.TempDir()
	r := frameworks.NewFastAPIResolver(dir)
	if r.Detect(context.Background()) {
		t.Error("FastAPI.Detect should return false in an empty directory")
	}
}

func TestFastAPIExtract_AppDecorators(t *testing.T) {
	filePath := "main.py"
	content := `
from fastapi import FastAPI
app = FastAPI()

@app.get('/items')
async def list_items():
    return []

@app.post('/items')
def create_item():
    pass

@app.delete('/items/{id}')
async def delete_item(id: int):
    pass
`
	r := frameworks.NewFastAPIResolver("/project")
	nodes, refs := r.Extract(filePath, content)

	if len(nodes) < 3 {
		t.Fatalf("FastAPI.Extract: want ≥3 route nodes, got %d: %v", len(nodes), nodeNames(nodes))
	}

	// Verify GET /items
	getItems := findNodeByName(nodes, "GET /items")
	if getItems == nil {
		t.Fatalf("FastAPI.Extract: missing 'GET /items'; got: %v", nodeNames(nodes))
	}
	wantID := "route:main.py:5:GET:/items"
	if getItems.ID != wantID {
		t.Errorf("FastAPI.Extract route node id:\n  got  %q\n  want %q", getItems.ID, wantID)
	}
	if getItems.Language != types.LanguagePython {
		t.Errorf("FastAPI.Extract route language: got %q want %q", getItems.Language, types.LanguagePython)
	}

	// Verify handler ref for list_items
	ref := findRefByNodeAndName(refs, getItems.ID, "list_items")
	if ref == nil {
		t.Errorf("FastAPI.Extract: missing handler ref 'list_items' from %q; refs: %v", getItems.ID, refNames(refs))
	}

	// Verify POST /items
	if findNodeByName(nodes, "POST /items") == nil {
		t.Errorf("FastAPI.Extract: missing 'POST /items'; got: %v", nodeNames(nodes))
	}
}

func TestFastAPIExtract_RouterDecorators(t *testing.T) {
	filePath := "routers/users.py"
	content := `
from fastapi import APIRouter
router = APIRouter()

@router.get('/users')
def get_users():
    pass

@router.put('/users/{id}')
async def update_user(id: int):
    pass
`
	r := frameworks.NewFastAPIResolver("/project")
	nodes, refs := r.Extract(filePath, content)

	if len(nodes) < 2 {
		t.Fatalf("FastAPI.Extract router: want ≥2 nodes, got %d: %v", len(nodes), nodeNames(nodes))
	}
	n := findNodeByName(nodes, "GET /users")
	if n == nil {
		t.Fatalf("FastAPI.Extract router: missing 'GET /users'; got: %v", nodeNames(nodes))
	}
	if findRefByNodeAndName(refs, n.ID, "get_users") == nil {
		t.Errorf("FastAPI.Extract router: missing handler ref 'get_users'; refs: %v", refNames(refs))
	}
}

func TestFastAPIClaimsReference(t *testing.T) {
	r := frameworks.NewFastAPIResolver("/project")
	r.Extract("main.py", `
@app.get('/x')
def my_handler():
    pass
`)
	if !r.ClaimsReference("my_handler") {
		t.Error("FastAPI.ClaimsReference: should return true for extracted handler")
	}
	if r.ClaimsReference("unknown") {
		t.Error("FastAPI.ClaimsReference: should return false for unseen name")
	}
}

// ---------------------------------------------------------------------------
// Django tests
// ---------------------------------------------------------------------------

func TestDjangoDetect_ManagePy(t *testing.T) {
	dir := t.TempDir()
	managePy := `#!/usr/bin/env python
import os
from django.core.management import execute_from_command_line
`
	if err := os.WriteFile(filepath.Join(dir, "manage.py"), []byte(managePy), 0o644); err != nil {
		t.Fatal(err)
	}
	r := frameworks.NewDjangoResolver(dir)
	if !r.Detect(context.Background()) {
		t.Error("Django.Detect should return true when manage.py is present")
	}
}

func TestDjangoDetect_ContentFallback(t *testing.T) {
	dir := t.TempDir()
	src := `from django.urls import path\nurlpatterns = []\n`
	if err := os.WriteFile(filepath.Join(dir, "urls.py"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	r := frameworks.NewDjangoResolver(dir)
	if !r.Detect(context.Background()) {
		t.Error("Django.Detect should return true when a file has 'urlpatterns'")
	}
}

func TestDjangoDetect_False(t *testing.T) {
	dir := t.TempDir()
	r := frameworks.NewDjangoResolver(dir)
	if r.Detect(context.Background()) {
		t.Error("Django.Detect should return false in an empty directory")
	}
}

func TestDjangoExtract_PathEntries(t *testing.T) {
	filePath := "myapp/urls.py"
	content := `
from django.urls import path, re_path
from . import views

urlpatterns = [
    path('users/', views.user_list),
    path('users/<int:pk>/', views.user_detail),
    re_path(r'^items/$', views.item_list),
]
`
	r := frameworks.NewDjangoResolver("/project")
	nodes, refs := r.Extract(filePath, content)

	if len(nodes) < 3 {
		t.Fatalf("Django.Extract: want ≥3 route nodes, got %d: %v", len(nodes), nodeNames(nodes))
	}

	// Django routes use method ANY (no HTTP method in URLconf).
	// Django path() patterns do NOT include a leading slash — the path is e.g. "users/", not "/users/".
	usersNode := findNodeByName(nodes, "ANY users/")
	if usersNode == nil {
		t.Fatalf("Django.Extract: missing 'ANY users/'; got: %v", nodeNames(nodes))
	}
	wantID := "route:myapp/urls.py:6:ANY:users/"
	if usersNode.ID != wantID {
		t.Errorf("Django.Extract route node id:\n  got  %q\n  want %q", usersNode.ID, wantID)
	}
	wantQN := "myapp/urls.py::METHOD:users/"
	if usersNode.QualifiedName != wantQN {
		t.Errorf("Django.Extract route node qualifiedName:\n  got  %q\n  want %q", usersNode.QualifiedName, wantQN)
	}
	if usersNode.Language != types.LanguagePython {
		t.Errorf("Django.Extract route language: got %q want %q", usersNode.Language, types.LanguagePython)
	}

	// Handler ref: views.user_list → "user_list" (last segment)
	ref := findRefByNodeAndName(refs, usersNode.ID, "user_list")
	if ref == nil {
		t.Errorf("Django.Extract: missing handler ref 'user_list' for %q; refs: %v", usersNode.ID, refNames(refs))
	} else if ref.ReferenceKind != types.EdgeKindReferences {
		t.Errorf("Django.Extract: handler ref kind got %q want %q", ref.ReferenceKind, types.EdgeKindReferences)
	}

	// Verify re_path entry — the route pattern is the raw regex string from re_path().
	found := false
	for _, n := range nodes {
		if n.Kind == types.NodeKindRoute && strings.Contains(n.Name, "items") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Django.Extract: missing re_path items route; got: %v", nodeNames(nodes))
	}
}

func TestDjangoExtract_StringViewHandler(t *testing.T) {
	// Django allows string-based view references: 'myapp.views.my_view'
	filePath := "urls.py"
	content := `
urlpatterns = [
    path('profile/', 'myapp.views.profile_view'),
]
`
	r := frameworks.NewDjangoResolver("/project")
	nodes, refs := r.Extract(filePath, content)

	if len(nodes) == 0 {
		t.Fatal("Django.Extract: expected a route node for string-based view")
	}
	// Django path() patterns do NOT include a leading slash.
	n := findNodeByName(nodes, "ANY profile/")
	if n == nil {
		t.Fatalf("Django.Extract string handler: missing 'ANY profile/'; got: %v", nodeNames(nodes))
	}
	// String ref 'myapp.views.profile_view' → last segment 'profile_view'
	ref := findRefByNodeAndName(refs, n.ID, "profile_view")
	if ref == nil {
		t.Errorf("Django.Extract string handler: missing ref 'profile_view'; refs: %v", refNames(refs))
	}
}

func TestDjangoClaimsReference(t *testing.T) {
	r := frameworks.NewDjangoResolver("/project")
	r.Extract("urls.py", `
urlpatterns = [
    path('x/', views.my_handler),
]
`)
	if !r.ClaimsReference("my_handler") {
		t.Error("Django.ClaimsReference: should return true for extracted handler")
	}
	if r.ClaimsReference("other_fn") {
		t.Error("Django.ClaimsReference: should return false for unseen name")
	}
}

// ---------------------------------------------------------------------------
// Registry integration: Python resolvers appear in GetApplicableFrameworks
// ---------------------------------------------------------------------------

func TestRegistry_PythonFrameworksRegistered(t *testing.T) {
	dir := t.TempDir()
	reg := frameworks.NewRegistry(dir, nil)

	pyFrameworks := reg.GetApplicableFrameworks(types.LanguagePython)
	names := make(map[string]bool, len(pyFrameworks))
	for _, r := range pyFrameworks {
		names[r.Name()] = true
	}

	for _, want := range []string{"django", "flask", "fastapi"} {
		if !names[want] {
			t.Errorf("Registry: Python framework %q not registered; got: %v", want, pyFrameworks)
		}
	}
}

// ---------------------------------------------------------------------------
// Helpers (shared with main test file via same package _test)
// ---------------------------------------------------------------------------

func nodeNames(nodes []types.Node) []string {
	out := make([]string, len(nodes))
	for i, n := range nodes {
		out[i] = n.Name
	}
	return out
}

func refNames(refs []types.UnresolvedReference) []string {
	out := make([]string, len(refs))
	for i, r := range refs {
		out[i] = r.ReferenceName
	}
	return out
}

func findNodeByName(nodes []types.Node, name string) *types.Node {
	for i := range nodes {
		if nodes[i].Name == name {
			return &nodes[i]
		}
	}
	return nil
}

func findRefByNodeAndName(refs []types.UnresolvedReference, fromNodeID, name string) *types.UnresolvedReference {
	for i := range refs {
		if refs[i].FromNodeID == fromNodeID && refs[i].ReferenceName == name {
			return &refs[i]
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// R1: real-idiom tests (failing pre-fix, green after)
// ---------------------------------------------------------------------------

// TestFlaskExtract_BlueprintTupleMethod tests the real rw-flask idiom:
// methods=('GET',) — tuple form, not list form. Pre-fix the regex only handled
// square-bracket lists; it must also accept parentheses.
func TestFlaskExtract_BlueprintTupleMethod(t *testing.T) {
	filePath := "conduit/articles/views.py"
	content := `
from flask import Blueprint
blueprint = Blueprint('articles', __name__)

@blueprint.route('/api/articles', methods=('GET',))
def get_articles():
    pass

@blueprint.route('/api/articles', methods=('POST',))
def make_article():
    pass
`
	r := frameworks.NewFlaskResolver("/project")
	nodes, refs := r.Extract(filePath, content)

	// Must emit both routes (one per method, tuple form).
	if len(nodes) < 2 {
		t.Fatalf("Flask.Extract tuple methods: want ≥2 nodes, got %d: %v", len(nodes), nodeNames(nodes))
	}
	getNode := findNodeByName(nodes, "GET /api/articles")
	if getNode == nil {
		t.Fatalf("Flask.Extract tuple methods: missing 'GET /api/articles'; got: %v", nodeNames(nodes))
	}
	if findRefByNodeAndName(refs, getNode.ID, "get_articles") == nil {
		t.Errorf("Flask.Extract tuple methods: missing handler ref 'get_articles'; refs: %v", refNames(refs))
	}
	if findNodeByName(nodes, "POST /api/articles") == nil {
		t.Fatalf("Flask.Extract tuple methods: missing 'POST /api/articles'; got: %v", nodeNames(nodes))
	}
}

// TestFlaskDetect_BlueprintAppNoDirectFlaskCall tests that Detect fires for a
// project whose top-level .py file does `from flask.helpers import ...` (the
// rw-flask autoapp.py pattern) — no Flask(...) call at top level.
func TestFlaskDetect_BlueprintApp(t *testing.T) {
	dir := t.TempDir()
	// Simulates rw-flask's autoapp.py: imports from flask sub-module but no Flask()
	src := "from flask.helpers import get_debug_flag\nfrom conduit.app import create_app\n"
	if err := os.WriteFile(filepath.Join(dir, "autoapp.py"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	r := frameworks.NewFlaskResolver(dir)
	if !r.Detect(context.Background()) {
		t.Error("Flask.Detect should return true when autoapp.py imports from flask.helpers")
	}
}

// TestFastAPIExtract_EmptyPathWithKwargs tests the real rw-fastapi idiom:
// @router.get("", response_model=UserInResponse, name="users:get-current-user")
// Two issues pre-fix: (1) empty-string path not matched; (2) trailing kwargs
// caused the regex to miss the match entirely.
func TestFastAPIExtract_EmptyPathWithKwargs(t *testing.T) {
	filePath := "app/api/routes/users.py"
	content := `
from fastapi import APIRouter
router = APIRouter()

@router.get("", response_model=UserInResponse, name="users:get-current-user")
async def retrieve_current_user():
    pass

@router.put("", response_model=UserInResponse, name="users:update-current-user")
async def update_current_user():
    pass

@router.post("/items", response_model=ItemOut, status_code=201)
async def create_item():
    pass
`
	r := frameworks.NewFastAPIResolver("/project")
	nodes, refs := r.Extract(filePath, content)

	// Must emit all 3 routes: 2 empty-path + 1 path-with-kwargs.
	if len(nodes) < 3 {
		t.Fatalf("FastAPI.Extract empty-path kwargs: want ≥3 nodes, got %d: %v", len(nodes), nodeNames(nodes))
	}
	getNode := findNodeByName(nodes, "GET ")
	if getNode == nil {
		t.Fatalf("FastAPI.Extract empty-path: missing 'GET ' (empty path); got: %v", nodeNames(nodes))
	}
	if findRefByNodeAndName(refs, getNode.ID, "retrieve_current_user") == nil {
		t.Errorf("FastAPI.Extract empty-path: missing handler ref 'retrieve_current_user'; refs: %v", refNames(refs))
	}
	if findNodeByName(nodes, "PUT ") == nil {
		t.Fatalf("FastAPI.Extract empty-path: missing 'PUT ' node; got: %v", nodeNames(nodes))
	}
	if findNodeByName(nodes, "POST /items") == nil {
		t.Fatalf("FastAPI.Extract empty-path: missing 'POST /items' node; got: %v", nodeNames(nodes))
	}
}

// strings.Contains is used directly (replaces the former hand-rolled contains helper).
