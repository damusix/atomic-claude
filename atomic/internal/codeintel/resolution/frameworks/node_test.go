package frameworks_test

// Failing-first TDD tests for CP15 batch C: Node/JS-TS web frameworks.
//
// Frameworks: nestjs, koa, hapi, fastify, sails, adonisjs.
//
// Per-framework coverage:
//  1. Detect true on a realistic package.json fixture + false on unrelated dir.
//  2. Extract emits ≥1 route node (exact appendix-H id/qn/name via MakeRouteNode) + handler ref.
//  3. Commented JS route (stripJSComments) emits nothing.
//  4. ClaimsReference true for extracted handler, false for unseen.
//  5. Resolve returns confidence 0.8–0.9.
//
// NestJS-specific: controller-prefix join, decorator scanning.
// Hapi-specific: method-array fan-out.
// Fastify-specific: object form with `url` field.
// Sails/Adonis: action-string last-segment handler.

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/resolution/frameworks"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// ---------------------------------------------------------------------------
// NestJS tests
// ---------------------------------------------------------------------------

func TestNestJSDetect_PackageJSON(t *testing.T) {
	dir := t.TempDir()
	pkgJSON := `{"dependencies": {"@nestjs/core": "^10.0.0", "@nestjs/common": "^10.0.0"}}`
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(pkgJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	r := frameworks.NewNestJSResolver(dir)
	if !r.Detect(context.Background()) {
		t.Error("NestJS.Detect: should return true when package.json lists @nestjs/core")
	}
}

func TestNestJSDetect_False(t *testing.T) {
	dir := t.TempDir()
	pkgJSON := `{"dependencies": {"express": "^4.0.0"}}`
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(pkgJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	r := frameworks.NewNestJSResolver(dir)
	if r.Detect(context.Background()) {
		t.Error("NestJS.Detect: should return false when package.json does not list @nestjs")
	}
}

func TestNestJSExtract_ControllerPrefixJoin(t *testing.T) {
	// @Controller('users') + @Get(':id') → GET /users/:id
	filePath := "src/users/users.controller.ts"
	content := `
import { Controller, Get, Post, Param } from '@nestjs/common';
import { UsersService } from './users.service';

@Controller('users')
export class UsersController {
  constructor(private readonly usersService: UsersService) {}

  @Get(':id')
  findOne(@Param('id') id: string) {
    return this.usersService.findOne(+id);
  }

  @Post()
  create() {
    return this.usersService.create();
  }
}
`
	r := frameworks.NewNestJSResolver("/project")
	nodes, refs := r.Extract(filePath, content)

	if len(nodes) < 2 {
		t.Fatalf("NestJS.Extract: want ≥2 route nodes, got %d: %v", len(nodes), nodeNames(nodes))
	}

	// @Controller('users') + @Get(':id') → GET /users/:id
	getNode := findNodeByName(nodes, "GET /users/:id")
	if getNode == nil {
		t.Fatalf("NestJS.Extract: missing 'GET /users/:id'; got: %v", nodeNames(nodes))
	}
	if getNode.Kind != types.NodeKindRoute {
		t.Errorf("NestJS.Extract: route kind got %q want %q", getNode.Kind, types.NodeKindRoute)
	}
	if getNode.Language != types.LanguageTypeScript {
		t.Errorf("NestJS.Extract: language got %q want %q", getNode.Language, types.LanguageTypeScript)
	}
	// Handler = the method name "findOne"
	if findRefByNodeAndName(refs, getNode.ID, "findOne") == nil {
		t.Errorf("NestJS.Extract: missing handler ref 'findOne'; refs: %v", refNames(refs))
	}

	// @Post() with no arg → POST /users
	postNode := findNodeByName(nodes, "POST /users")
	if postNode == nil {
		t.Fatalf("NestJS.Extract: missing 'POST /users'; got: %v", nodeNames(nodes))
	}
	if findRefByNodeAndName(refs, postNode.ID, "create") == nil {
		t.Errorf("NestJS.Extract: missing handler ref 'create' for POST /users; refs: %v", refNames(refs))
	}
}

func TestNestJSExtract_EmptyControllerPrefix(t *testing.T) {
	// @Controller() with no prefix → methods get /
	filePath := "src/app.controller.ts"
	content := `
import { Controller, Get } from '@nestjs/common';

@Controller()
export class AppController {
  @Get()
  getHello() { return 'Hello'; }
}
`
	r := frameworks.NewNestJSResolver("/project")
	nodes, refs := r.Extract(filePath, content)

	if len(nodes) == 0 {
		t.Fatal("NestJS.Extract: expected at least one route node for @Controller() + @Get()")
	}
	// Empty prefix + empty method arg → GET /
	getNode := findNodeByName(nodes, "GET /")
	if getNode == nil {
		t.Fatalf("NestJS.Extract: missing 'GET /'; got: %v", nodeNames(nodes))
	}
	if findRefByNodeAndName(refs, getNode.ID, "getHello") == nil {
		t.Errorf("NestJS.Extract: missing handler ref 'getHello'; refs: %v", refNames(refs))
	}
}

func TestNestJSExtract_CommentedDecoratorEmitsNothing(t *testing.T) {
	filePath := "src/app.controller.ts"
	content := `
import { Controller, Get } from '@nestjs/common';

@Controller('items')
export class ItemsController {
  // @Get(':id')
  // findOne() {}

  @Post()
  create() {}
}
`
	r := frameworks.NewNestJSResolver("/project")
	nodes, _ := r.Extract(filePath, content)

	for _, n := range nodes {
		if n.Name == "GET /items/:id" {
			t.Errorf("NestJS.Extract: commented @Get must not emit a route node")
		}
	}
}

func TestNestJSClaimsReference(t *testing.T) {
	r := frameworks.NewNestJSResolver("/project")
	r.Extract("src/app.controller.ts", `
import { Controller, Get } from '@nestjs/common';
@Controller('x')
export class AppController {
  @Get(':id')
  findOne() {}
}
`)
	if !r.ClaimsReference("findOne") {
		t.Error("NestJS.ClaimsReference: should return true for extracted handler method name")
	}
	if r.ClaimsReference("unknown") {
		t.Error("NestJS.ClaimsReference: should return false for unseen name")
	}
}

func TestNestJSResolve_Confidence(t *testing.T) {
	r := frameworks.NewNestJSResolver("/project")
	r.Extract("src/app.controller.ts", `
import { Controller, Get } from '@nestjs/common';
@Controller('x')
export class AppController {
  @Get(':id')
  findOne() {}
}
`)
	result, err := r.Resolve(context.Background(), types.UnresolvedReference{
		ReferenceName: "findOne",
		Language:      types.LanguageTypeScript,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Confidence < 0.8 || result.Confidence > 0.9 {
		t.Errorf("NestJS.Resolve: confidence want 0.8–0.9, got %v", result.Confidence)
	}
}

// ---------------------------------------------------------------------------
// Koa tests
// ---------------------------------------------------------------------------

func TestKoaDetect_PackageJSON(t *testing.T) {
	dir := t.TempDir()
	pkgJSON := `{"dependencies": {"koa": "^2.14.0", "koa-router": "^12.0.0"}}`
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(pkgJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	r := frameworks.NewKoaResolver(dir)
	if !r.Detect(context.Background()) {
		t.Error("Koa.Detect: should return true when package.json lists koa")
	}
}

func TestKoaDetect_KoaRouterPackage(t *testing.T) {
	dir := t.TempDir()
	pkgJSON := `{"dependencies": {"@koa/router": "^12.0.0"}}`
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(pkgJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	r := frameworks.NewKoaResolver(dir)
	if !r.Detect(context.Background()) {
		t.Error("Koa.Detect: should return true when package.json lists @koa/router")
	}
}

func TestKoaDetect_False(t *testing.T) {
	dir := t.TempDir()
	pkgJSON := `{"dependencies": {"express": "^4.0.0"}}`
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(pkgJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	r := frameworks.NewKoaResolver(dir)
	if r.Detect(context.Background()) {
		t.Error("Koa.Detect: should return false when package.json does not list koa")
	}
}

func TestKoaExtract_BasicRoutes(t *testing.T) {
	filePath := "src/routes.js"
	content := `
const Router = require('koa-router');
const router = new Router();

router.get('/users', listUsers);
router.post('/users', createUser);
router.delete('/users/:id', deleteUser);
`
	r := frameworks.NewKoaResolver("/project")
	nodes, refs := r.Extract(filePath, content)

	if len(nodes) < 3 {
		t.Fatalf("Koa.Extract: want ≥3 route nodes, got %d: %v", len(nodes), nodeNames(nodes))
	}

	getNode := findNodeByName(nodes, "GET /users")
	if getNode == nil {
		t.Fatalf("Koa.Extract: missing 'GET /users'; got: %v", nodeNames(nodes))
	}
	if getNode.Kind != types.NodeKindRoute {
		t.Errorf("Koa.Extract: route kind got %q want %q", getNode.Kind, types.NodeKindRoute)
	}
	if getNode.Language != types.LanguageJavaScript {
		t.Errorf("Koa.Extract: language got %q want %q (should infer from .js ext)", getNode.Language, types.LanguageJavaScript)
	}
	if findRefByNodeAndName(refs, getNode.ID, "listUsers") == nil {
		t.Errorf("Koa.Extract: missing handler ref 'listUsers'; refs: %v", refNames(refs))
	}

	if findNodeByName(nodes, "POST /users") == nil {
		t.Errorf("Koa.Extract: missing 'POST /users'")
	}
}

func TestKoaExtract_CommentedRouteEmitsNothing(t *testing.T) {
	filePath := "src/routes.js"
	content := `
// router.get('/commented', commentedHandler);
router.post('/real', realHandler);
`
	r := frameworks.NewKoaResolver("/project")
	nodes, _ := r.Extract(filePath, content)

	for _, n := range nodes {
		if n.Name == "GET /commented" {
			t.Errorf("Koa.Extract: commented route must not be emitted")
		}
	}
	if findNodeByName(nodes, "POST /real") == nil {
		t.Errorf("Koa.Extract: 'POST /real' should be emitted; got: %v", nodeNames(nodes))
	}
}

func TestKoaClaimsReference(t *testing.T) {
	r := frameworks.NewKoaResolver("/project")
	r.Extract("src/routes.js", `router.get('/x', myKoaHandler);`)
	if !r.ClaimsReference("myKoaHandler") {
		t.Error("Koa.ClaimsReference: should return true for extracted handler")
	}
	if r.ClaimsReference("unknown") {
		t.Error("Koa.ClaimsReference: should return false for unseen name")
	}
}

func TestKoaResolve_Confidence(t *testing.T) {
	r := frameworks.NewKoaResolver("/project")
	r.Extract("src/routes.js", `router.get('/x', myKoaHandler);`)
	result, err := r.Resolve(context.Background(), types.UnresolvedReference{
		ReferenceName: "myKoaHandler",
		Language:      types.LanguageJavaScript,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Confidence < 0.8 || result.Confidence > 0.9 {
		t.Errorf("Koa.Resolve: confidence want 0.8–0.9, got %v", result.Confidence)
	}
}

// ---------------------------------------------------------------------------
// Hapi tests
// ---------------------------------------------------------------------------

func TestHapiDetect_PackageJSON(t *testing.T) {
	dir := t.TempDir()
	pkgJSON := `{"dependencies": {"@hapi/hapi": "^21.0.0"}}`
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(pkgJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	r := frameworks.NewHapiResolver(dir)
	if !r.Detect(context.Background()) {
		t.Error("Hapi.Detect: should return true when package.json lists @hapi/hapi")
	}
}

func TestHapiDetect_LegacyPackage(t *testing.T) {
	dir := t.TempDir()
	pkgJSON := `{"dependencies": {"hapi": "^17.0.0"}}`
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(pkgJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	r := frameworks.NewHapiResolver(dir)
	if !r.Detect(context.Background()) {
		t.Error("Hapi.Detect: should return true when package.json lists hapi (legacy)")
	}
}

func TestHapiDetect_False(t *testing.T) {
	dir := t.TempDir()
	pkgJSON := `{"dependencies": {"express": "^4.0.0"}}`
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(pkgJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	r := frameworks.NewHapiResolver(dir)
	if r.Detect(context.Background()) {
		t.Error("Hapi.Detect: should return false when package.json does not list hapi")
	}
}

func TestHapiExtract_ObjectForm(t *testing.T) {
	filePath := "src/server.js"
	content := `
const Hapi = require('@hapi/hapi');
const server = Hapi.server();

server.route({
  method: 'GET',
  path: '/users',
  handler: listUsers,
});

server.route({
  method: 'POST',
  path: '/users',
  handler: createUser,
});
`
	r := frameworks.NewHapiResolver("/project")
	nodes, refs := r.Extract(filePath, content)

	if len(nodes) < 2 {
		t.Fatalf("Hapi.Extract: want ≥2 route nodes, got %d: %v", len(nodes), nodeNames(nodes))
	}

	getNode := findNodeByName(nodes, "GET /users")
	if getNode == nil {
		t.Fatalf("Hapi.Extract: missing 'GET /users'; got: %v", nodeNames(nodes))
	}
	if getNode.Kind != types.NodeKindRoute {
		t.Errorf("Hapi.Extract: route kind got %q want %q", getNode.Kind, types.NodeKindRoute)
	}
	if getNode.Language != types.LanguageJavaScript {
		t.Errorf("Hapi.Extract: language got %q want %q", getNode.Language, types.LanguageJavaScript)
	}
	if findRefByNodeAndName(refs, getNode.ID, "listUsers") == nil {
		t.Errorf("Hapi.Extract: missing handler ref 'listUsers'; refs: %v", refNames(refs))
	}
}

func TestHapiExtract_MethodArrayFanOut(t *testing.T) {
	// method: ['GET', 'POST'] → two route nodes, one per method
	filePath := "src/routes.js"
	content := `
server.route({
  method: ['GET', 'POST'],
  path: '/items',
  handler: handleItems,
});
`
	r := frameworks.NewHapiResolver("/project")
	nodes, refs := r.Extract(filePath, content)

	// Expect 2 nodes: GET /items and POST /items
	if len(nodes) < 2 {
		t.Fatalf("Hapi.Extract method array: want ≥2 nodes, got %d: %v", len(nodes), nodeNames(nodes))
	}

	getNode := findNodeByName(nodes, "GET /items")
	if getNode == nil {
		t.Fatalf("Hapi.Extract method array: missing 'GET /items'; got: %v", nodeNames(nodes))
	}
	postNode := findNodeByName(nodes, "POST /items")
	if postNode == nil {
		t.Fatalf("Hapi.Extract method array: missing 'POST /items'; got: %v", nodeNames(nodes))
	}

	// Both should reference handleItems
	if findRefByNodeAndName(refs, getNode.ID, "handleItems") == nil {
		t.Errorf("Hapi.Extract method array: missing handler ref 'handleItems' for GET; refs: %v", refNames(refs))
	}
	if findRefByNodeAndName(refs, postNode.ID, "handleItems") == nil {
		t.Errorf("Hapi.Extract method array: missing handler ref 'handleItems' for POST; refs: %v", refNames(refs))
	}
}

func TestHapiExtract_MethodWildcardIsAny(t *testing.T) {
	// method: '*' → ANY
	filePath := "src/routes.js"
	content := `
server.route({
  method: '*',
  path: '/wildcard',
  handler: wildcardHandler,
});
`
	r := frameworks.NewHapiResolver("/project")
	nodes, _ := r.Extract(filePath, content)

	if len(nodes) == 0 {
		t.Fatal("Hapi.Extract: expected route node for method '*'")
	}
	anyNode := findNodeByName(nodes, "ANY /wildcard")
	if anyNode == nil {
		t.Fatalf("Hapi.Extract wildcard: missing 'ANY /wildcard'; got: %v", nodeNames(nodes))
	}
}

func TestHapiExtract_CommentedRouteEmitsNothing(t *testing.T) {
	filePath := "src/routes.js"
	content := `
/*
server.route({
  method: 'GET',
  path: '/commented',
  handler: commentedHandler,
});
*/
server.route({
  method: 'DELETE',
  path: '/real',
  handler: realHandler,
});
`
	r := frameworks.NewHapiResolver("/project")
	nodes, _ := r.Extract(filePath, content)

	for _, n := range nodes {
		if n.Name == "GET /commented" {
			t.Errorf("Hapi.Extract: commented route must not be emitted")
		}
	}
	if findNodeByName(nodes, "DELETE /real") == nil {
		t.Errorf("Hapi.Extract: 'DELETE /real' should be emitted; got: %v", nodeNames(nodes))
	}
}

func TestHapiClaimsReference(t *testing.T) {
	r := frameworks.NewHapiResolver("/project")
	r.Extract("src/routes.js", `server.route({ method: 'GET', path: '/x', handler: myHapiHandler });`)
	if !r.ClaimsReference("myHapiHandler") {
		t.Error("Hapi.ClaimsReference: should return true for extracted handler")
	}
	if r.ClaimsReference("unknown") {
		t.Error("Hapi.ClaimsReference: should return false for unseen name")
	}
}

func TestHapiResolve_Confidence(t *testing.T) {
	r := frameworks.NewHapiResolver("/project")
	r.Extract("src/routes.js", `server.route({ method: 'GET', path: '/x', handler: myHapiHandler });`)
	result, err := r.Resolve(context.Background(), types.UnresolvedReference{
		ReferenceName: "myHapiHandler",
		Language:      types.LanguageJavaScript,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Confidence < 0.8 || result.Confidence > 0.9 {
		t.Errorf("Hapi.Resolve: confidence want 0.8–0.9, got %v", result.Confidence)
	}
}

// ---------------------------------------------------------------------------
// Fastify tests
// ---------------------------------------------------------------------------

func TestFastifyDetect_PackageJSON(t *testing.T) {
	dir := t.TempDir()
	pkgJSON := `{"dependencies": {"fastify": "^4.0.0"}}`
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(pkgJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	r := frameworks.NewFastifyResolver(dir)
	if !r.Detect(context.Background()) {
		t.Error("Fastify.Detect: should return true when package.json lists fastify")
	}
}

func TestFastifyDetect_False(t *testing.T) {
	dir := t.TempDir()
	pkgJSON := `{"dependencies": {"express": "^4.0.0"}}`
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(pkgJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	r := frameworks.NewFastifyResolver(dir)
	if r.Detect(context.Background()) {
		t.Error("Fastify.Detect: should return false when package.json does not list fastify")
	}
}

func TestFastifyExtract_Shorthand(t *testing.T) {
	// fastify.get('/path', handler) shorthand form
	filePath := "src/server.js"
	content := `
const fastify = require('fastify')();

fastify.get('/users', listUsers);
fastify.post('/users', createUser);
`
	r := frameworks.NewFastifyResolver("/project")
	nodes, refs := r.Extract(filePath, content)

	if len(nodes) < 2 {
		t.Fatalf("Fastify.Extract shorthand: want ≥2 route nodes, got %d: %v", len(nodes), nodeNames(nodes))
	}

	getNode := findNodeByName(nodes, "GET /users")
	if getNode == nil {
		t.Fatalf("Fastify.Extract: missing 'GET /users'; got: %v", nodeNames(nodes))
	}
	if getNode.Kind != types.NodeKindRoute {
		t.Errorf("Fastify.Extract: route kind got %q want %q", getNode.Kind, types.NodeKindRoute)
	}
	if findRefByNodeAndName(refs, getNode.ID, "listUsers") == nil {
		t.Errorf("Fastify.Extract: missing handler ref 'listUsers'; refs: %v", refNames(refs))
	}
}

func TestFastifyExtract_ObjectFormUrlField(t *testing.T) {
	// fastify.route({ method: 'GET', url: '/path', handler: fn }) — note: url not path
	filePath := "src/routes.js"
	content := `
fastify.route({
  method: 'GET',
  url: '/items',
  handler: listItems,
});

fastify.route({
  method: 'POST',
  url: '/items',
  handler: createItem,
});
`
	r := frameworks.NewFastifyResolver("/project")
	nodes, refs := r.Extract(filePath, content)

	if len(nodes) < 2 {
		t.Fatalf("Fastify.Extract object form: want ≥2 route nodes, got %d: %v", len(nodes), nodeNames(nodes))
	}

	getNode := findNodeByName(nodes, "GET /items")
	if getNode == nil {
		t.Fatalf("Fastify.Extract object form: missing 'GET /items'; got: %v", nodeNames(nodes))
	}
	if findRefByNodeAndName(refs, getNode.ID, "listItems") == nil {
		t.Errorf("Fastify.Extract object form: missing handler ref 'listItems'; refs: %v", refNames(refs))
	}

	postNode := findNodeByName(nodes, "POST /items")
	if postNode == nil {
		t.Errorf("Fastify.Extract object form: missing 'POST /items'; got: %v", nodeNames(nodes))
	}
}

func TestFastifyExtract_CommentedRouteEmitsNothing(t *testing.T) {
	filePath := "src/server.js"
	content := `
// fastify.get('/commented', commentedHandler);
fastify.post('/real', realHandler);
`
	r := frameworks.NewFastifyResolver("/project")
	nodes, _ := r.Extract(filePath, content)

	for _, n := range nodes {
		if n.Name == "GET /commented" {
			t.Errorf("Fastify.Extract: commented route must not be emitted")
		}
	}
	if findNodeByName(nodes, "POST /real") == nil {
		t.Errorf("Fastify.Extract: 'POST /real' should be emitted; got: %v", nodeNames(nodes))
	}
}

func TestFastifyClaimsReference(t *testing.T) {
	r := frameworks.NewFastifyResolver("/project")
	r.Extract("src/server.js", `fastify.get('/x', myFastifyHandler);`)
	if !r.ClaimsReference("myFastifyHandler") {
		t.Error("Fastify.ClaimsReference: should return true for extracted handler")
	}
	if r.ClaimsReference("unknown") {
		t.Error("Fastify.ClaimsReference: should return false for unseen name")
	}
}

func TestFastifyResolve_Confidence(t *testing.T) {
	r := frameworks.NewFastifyResolver("/project")
	r.Extract("src/server.js", `fastify.get('/x', myFastifyHandler);`)
	result, err := r.Resolve(context.Background(), types.UnresolvedReference{
		ReferenceName: "myFastifyHandler",
		Language:      types.LanguageJavaScript,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Confidence < 0.8 || result.Confidence > 0.9 {
		t.Errorf("Fastify.Resolve: confidence want 0.8–0.9, got %v", result.Confidence)
	}
}

// ---------------------------------------------------------------------------
// Sails tests
// ---------------------------------------------------------------------------

func TestSailsDetect_PackageJSON(t *testing.T) {
	dir := t.TempDir()
	pkgJSON := `{"dependencies": {"sails": "^1.5.0"}}`
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(pkgJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	r := frameworks.NewSailsResolver(dir)
	if !r.Detect(context.Background()) {
		t.Error("Sails.Detect: should return true when package.json lists sails")
	}
}

func TestSailsDetect_False(t *testing.T) {
	dir := t.TempDir()
	pkgJSON := `{"dependencies": {"express": "^4.0.0"}}`
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(pkgJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	r := frameworks.NewSailsResolver(dir)
	if r.Detect(context.Background()) {
		t.Error("Sails.Detect: should return false when package.json does not list sails")
	}
}

func TestSailsExtract_ActionStringWithMethod(t *testing.T) {
	// 'GET /users': 'UsersController.index' → GET /users, handler=index
	filePath := "config/routes.js"
	content := `
module.exports.routes = {
  'GET /users': 'UsersController.index',
  'POST /users': 'UsersController.create',
  'DELETE /users/:id': 'UsersController.destroy',
};
`
	r := frameworks.NewSailsResolver("/project")
	nodes, refs := r.Extract(filePath, content)

	if len(nodes) < 3 {
		t.Fatalf("Sails.Extract: want ≥3 route nodes, got %d: %v", len(nodes), nodeNames(nodes))
	}

	getNode := findNodeByName(nodes, "GET /users")
	if getNode == nil {
		t.Fatalf("Sails.Extract: missing 'GET /users'; got: %v", nodeNames(nodes))
	}
	if getNode.Kind != types.NodeKindRoute {
		t.Errorf("Sails.Extract: route kind got %q want %q", getNode.Kind, types.NodeKindRoute)
	}
	// Action last segment: 'UsersController.index' → 'index'
	if findRefByNodeAndName(refs, getNode.ID, "index") == nil {
		t.Errorf("Sails.Extract: missing handler ref 'index' (last segment of UsersController.index); refs: %v", refNames(refs))
	}

	postNode := findNodeByName(nodes, "POST /users")
	if postNode == nil {
		t.Errorf("Sails.Extract: missing 'POST /users'")
	}
}

func TestSailsExtract_NoMethodFallbackAny(t *testing.T) {
	// '/users': 'UsersController.index' — no method → ANY
	filePath := "config/routes.js"
	content := `
module.exports.routes = {
  '/users': 'UsersController.index',
};
`
	r := frameworks.NewSailsResolver("/project")
	nodes, refs := r.Extract(filePath, content)

	if len(nodes) == 0 {
		t.Fatal("Sails.Extract no-method: expected route node")
	}
	anyNode := findNodeByName(nodes, "ANY /users")
	if anyNode == nil {
		t.Fatalf("Sails.Extract no-method: missing 'ANY /users'; got: %v", nodeNames(nodes))
	}
	if findRefByNodeAndName(refs, anyNode.ID, "index") == nil {
		t.Errorf("Sails.Extract no-method: missing handler ref 'index'; refs: %v", refNames(refs))
	}
}

func TestSailsExtract_CommentedRouteEmitsNothing(t *testing.T) {
	filePath := "config/routes.js"
	content := `
module.exports.routes = {
  // 'GET /commented': 'FooController.bar',
  'POST /real': 'FooController.create',
};
`
	r := frameworks.NewSailsResolver("/project")
	nodes, _ := r.Extract(filePath, content)

	for _, n := range nodes {
		if n.Name == "GET /commented" {
			t.Errorf("Sails.Extract: commented route must not be emitted")
		}
	}
	if findNodeByName(nodes, "POST /real") == nil {
		t.Errorf("Sails.Extract: 'POST /real' should be emitted; got: %v", nodeNames(nodes))
	}
}

func TestSailsClaimsReference(t *testing.T) {
	r := frameworks.NewSailsResolver("/project")
	r.Extract("config/routes.js", `module.exports.routes = { 'GET /x': 'FooController.mySailsHandler' };`)
	if !r.ClaimsReference("mySailsHandler") {
		t.Error("Sails.ClaimsReference: should return true for extracted handler last segment")
	}
	if r.ClaimsReference("FooController") {
		t.Error("Sails.ClaimsReference: should not claim the controller prefix, only last segment")
	}
}

func TestSailsResolve_Confidence(t *testing.T) {
	r := frameworks.NewSailsResolver("/project")
	r.Extract("config/routes.js", `module.exports.routes = { 'GET /x': 'FooController.mySailsHandler' };`)
	result, err := r.Resolve(context.Background(), types.UnresolvedReference{
		ReferenceName: "mySailsHandler",
		Language:      types.LanguageJavaScript,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Confidence < 0.8 || result.Confidence > 0.9 {
		t.Errorf("Sails.Resolve: confidence want 0.8–0.9, got %v", result.Confidence)
	}
}

// ---------------------------------------------------------------------------
// AdonisJS tests
// ---------------------------------------------------------------------------

func TestAdonisDetect_PackageJSON(t *testing.T) {
	dir := t.TempDir()
	pkgJSON := `{"dependencies": {"@adonisjs/core": "^6.0.0"}}`
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(pkgJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	r := frameworks.NewAdonisResolver(dir)
	if !r.Detect(context.Background()) {
		t.Error("Adonis.Detect: should return true when package.json lists @adonisjs/core")
	}
}

func TestAdonisDetect_LegacyAdonis(t *testing.T) {
	dir := t.TempDir()
	pkgJSON := `{"dependencies": {"adonis": "^3.0.0"}}`
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(pkgJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	r := frameworks.NewAdonisResolver(dir)
	if !r.Detect(context.Background()) {
		t.Error("Adonis.Detect: should return true when package.json lists adonis (legacy)")
	}
}

func TestAdonisDetect_False(t *testing.T) {
	dir := t.TempDir()
	pkgJSON := `{"dependencies": {"express": "^4.0.0"}}`
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(pkgJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	r := frameworks.NewAdonisResolver(dir)
	if r.Detect(context.Background()) {
		t.Error("Adonis.Detect: should return false when package.json does not list adonis")
	}
}

func TestAdonisExtract_ActionString(t *testing.T) {
	// Route.get('/users', 'UsersController.index') → GET /users, handler=index
	filePath := "start/routes.js"
	content := `
const Route = use('Route');

Route.get('/users', 'UsersController.index');
Route.post('/users', 'UsersController.store');
Route.delete('/users/:id', 'UsersController.destroy');
`
	r := frameworks.NewAdonisResolver("/project")
	nodes, refs := r.Extract(filePath, content)

	if len(nodes) < 3 {
		t.Fatalf("Adonis.Extract: want ≥3 route nodes, got %d: %v", len(nodes), nodeNames(nodes))
	}

	getNode := findNodeByName(nodes, "GET /users")
	if getNode == nil {
		t.Fatalf("Adonis.Extract: missing 'GET /users'; got: %v", nodeNames(nodes))
	}
	if getNode.Kind != types.NodeKindRoute {
		t.Errorf("Adonis.Extract: route kind got %q want %q", getNode.Kind, types.NodeKindRoute)
	}
	// Action last segment: 'UsersController.index' → 'index'
	if findRefByNodeAndName(refs, getNode.ID, "index") == nil {
		t.Errorf("Adonis.Extract: missing handler ref 'index' (last segment of UsersController.index); refs: %v", refNames(refs))
	}

	postNode := findNodeByName(nodes, "POST /users")
	if postNode == nil {
		t.Errorf("Adonis.Extract: missing 'POST /users'; got: %v", nodeNames(nodes))
	}
}

func TestAdonisExtract_CommentedRouteEmitsNothing(t *testing.T) {
	filePath := "start/routes.js"
	content := `
const Route = use('Route');

// Route.get('/commented', 'FooController.bar');
Route.post('/real', 'FooController.create');
`
	r := frameworks.NewAdonisResolver("/project")
	nodes, _ := r.Extract(filePath, content)

	for _, n := range nodes {
		if n.Name == "GET /commented" {
			t.Errorf("Adonis.Extract: commented route must not be emitted")
		}
	}
	if findNodeByName(nodes, "POST /real") == nil {
		t.Errorf("Adonis.Extract: 'POST /real' should be emitted; got: %v", nodeNames(nodes))
	}
}

func TestAdonisClaimsReference(t *testing.T) {
	r := frameworks.NewAdonisResolver("/project")
	r.Extract("start/routes.js", `Route.get('/x', 'FooController.myAdonisHandler');`)
	if !r.ClaimsReference("myAdonisHandler") {
		t.Error("Adonis.ClaimsReference: should return true for extracted handler last segment")
	}
	if r.ClaimsReference("FooController") {
		t.Error("Adonis.ClaimsReference: should not claim the controller prefix, only last segment")
	}
}

func TestAdonisResolve_Confidence(t *testing.T) {
	r := frameworks.NewAdonisResolver("/project")
	r.Extract("start/routes.js", `Route.get('/x', 'FooController.myAdonisHandler');`)
	result, err := r.Resolve(context.Background(), types.UnresolvedReference{
		ReferenceName: "myAdonisHandler",
		Language:      types.LanguageJavaScript,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Confidence < 0.8 || result.Confidence > 0.9 {
		t.Errorf("Adonis.Resolve: confidence want 0.8–0.9, got %v", result.Confidence)
	}
}

// ---------------------------------------------------------------------------
// NestJS: two @Controller classes in one file — document-order prefix isolation
// ---------------------------------------------------------------------------

// TestNestJSExtract_TwoControllersInOneFile asserts that when two @Controller
// classes appear in the same file, methods in the SECOND class use the SECOND
// controller's prefix (last-write-wins in document order).  The first
// controller's prefix must NOT bleed into the second controller's methods.
func TestNestJSExtract_TwoControllersInOneFile(t *testing.T) {
	filePath := "src/app.controller.ts"
	content := `
import { Controller, Get, Post } from '@nestjs/common';

@Controller('alpha')
export class AlphaController {
  @Get('one')
  getOne() {}
}

@Controller('beta')
export class BetaController {
  @Post('two')
  createTwo() {}
}
`
	r := frameworks.NewNestJSResolver("/project")
	nodes, refs := r.Extract(filePath, content)

	// AlphaController: GET /alpha/one
	alphaNode := findNodeByName(nodes, "GET /alpha/one")
	if alphaNode == nil {
		t.Fatalf("TwoControllers: missing 'GET /alpha/one'; got: %v", nodeNames(nodes))
	}
	if findRefByNodeAndName(refs, alphaNode.ID, "getOne") == nil {
		t.Errorf("TwoControllers: missing handler ref 'getOne' for GET /alpha/one; refs: %v", refNames(refs))
	}

	// BetaController: POST /beta/two  (not /alpha/two — second prefix must apply)
	betaNode := findNodeByName(nodes, "POST /beta/two")
	if betaNode == nil {
		t.Fatalf("TwoControllers: missing 'POST /beta/two'; got %v — second controller prefix must apply, not first", nodeNames(nodes))
	}
	if findRefByNodeAndName(refs, betaNode.ID, "createTwo") == nil {
		t.Errorf("TwoControllers: missing handler ref 'createTwo' for POST /beta/two; refs: %v", refNames(refs))
	}

	// Negative assertion: POST /alpha/two must NOT exist.
	if findNodeByName(nodes, "POST /alpha/two") != nil {
		t.Errorf("TwoControllers: 'POST /alpha/two' must not exist — second controller has prefix 'beta', not 'alpha'")
	}
}

// ---------------------------------------------------------------------------
// NestJS: stacked decorators — handler resolves to method, not guard name
// ---------------------------------------------------------------------------

// TestNestJSExtract_StackedDecoratorResolvesHandler asserts that stacked
// decorators between the route decorator and the method definition do NOT
// cause the handler to resolve to the guard/interceptor name.  Given:
//
//	@Get(':id')
//	@UseGuards(AuthGuard)
//	findOne(@Param('id') id: string) {}
//
// the handler must be "findOne", not "UseGuards" or "AuthGuard".
func TestNestJSExtract_StackedDecoratorResolvesHandler(t *testing.T) {
	filePath := "src/users.controller.ts"
	content := `
import { Controller, Get, UseGuards, Param } from '@nestjs/common';
import { AuthGuard } from './auth.guard';

@Controller('users')
export class UsersController {
  @Get(':id')
  @UseGuards(AuthGuard)
  findOne(@Param('id') id: string) {
    return this.usersService.findOne(+id);
  }
}
`
	r := frameworks.NewNestJSResolver("/project")
	nodes, refs := r.Extract(filePath, content)

	if len(nodes) == 0 {
		t.Fatal("StackedDecorator: expected at least one route node")
	}

	getNode := findNodeByName(nodes, "GET /users/:id")
	if getNode == nil {
		t.Fatalf("StackedDecorator: missing 'GET /users/:id'; got: %v", nodeNames(nodes))
	}

	// Handler must be the method name, not the guard decorator name.
	if findRefByNodeAndName(refs, getNode.ID, "findOne") == nil {
		t.Errorf("StackedDecorator: handler ref should be 'findOne', got refs: %v", refNames(refs))
	}
	// UseGuards and AuthGuard must NOT be claimed as handlers.
	for _, badName := range []string{"UseGuards", "AuthGuard"} {
		if findRefByNodeAndName(refs, getNode.ID, badName) != nil {
			t.Errorf("StackedDecorator: ref %q must not be claimed as handler for the route", badName)
		}
	}
}

// TestNestJSExtract_NoBoundaryBleedToNextClass asserts that a route decorator
// on the last method of a class does NOT misattribute a method from the next
// class as the handler.  This locks the bounded-lookahead fix: nestDefRe must
// stop at the class boundary (the closing '}') and not scan into the next class.
//
// Without the fix, @Delete(':id') at the end of ItemsController (where the
// actual handler method follows on the next line) still resolves correctly,
// but if the class has only a decorator and no handler method, the whole-file
// scan would grab processItem from ItemsService — the cross-class misattribution
// the reviewer flagged.
func TestNestJSExtract_NoBoundaryBleedToNextClass(t *testing.T) {
	filePath := "src/items.controller.ts"
	// A route decorator at the end of ItemsController; no method body follows
	// before the class closes.  A service class follows with its own method.
	// The handler name must be empty (not "processItem" from ItemsService).
	content := `
import { Controller, Delete } from '@nestjs/common';

@Controller('items')
export class ItemsController {
  @Delete(':id')
}

export class ItemsService {
  processItem(id: string) { return id; }
}
`
	r := frameworks.NewNestJSResolver("/project")
	nodes, refs := r.Extract(filePath, content)

	// Route node should still be emitted (decorator was found).
	deleteNode := findNodeByName(nodes, "DELETE /items/:id")
	if deleteNode == nil {
		t.Fatalf("NoBoundaryBleed: missing 'DELETE /items/:id'; got: %v", nodeNames(nodes))
	}

	// processItem is from ItemsService, not the route handler — must NOT be claimed.
	if findRefByNodeAndName(refs, deleteNode.ID, "processItem") != nil {
		t.Errorf("NoBoundaryBleed: 'processItem' from ItemsService must NOT be claimed as handler for DELETE /items/:id (cross-class boundary bleed)")
	}
	// No handler ref at all is acceptable when there is no actual method after the decorator.
	if r.ClaimsReference("processItem") {
		t.Errorf("NoBoundaryBleed: 'processItem' must not be in claimed set — it belongs to a different class")
	}
}

// ---------------------------------------------------------------------------
// Registry: Node/JS-TS frameworks appear in GetApplicableFrameworks
// ---------------------------------------------------------------------------

func TestRegistry_NodeFrameworksRegistered(t *testing.T) {
	dir := t.TempDir()
	reg := frameworks.NewRegistry(dir, nil)

	jsFrameworks := reg.GetApplicableFrameworks(types.LanguageJavaScript)
	names := make(map[string]bool, len(jsFrameworks))
	for _, r := range jsFrameworks {
		names[r.Name()] = true
	}

	for _, want := range []string{"nestjs", "koa", "hapi", "fastify", "sails", "adonisjs"} {
		if !names[want] {
			t.Errorf("Registry: Node framework %q not registered; got names: %v", want, names)
		}
	}
}
