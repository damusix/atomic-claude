package frameworks_test

// Failing-first TDD tests for CP15 batch E (Ruby): rails resolver.
//
// Coverage:
//  1. Detect true on Gemfile with 'rails' gem + false for non-rails project.
//  2. Extract: get/post/put/patch/delete → uppercase method + controller#action last-segment handler ref.
//  3. `root` verb produces path "/" with the action last-segment.
//  4. Hash-rocket form: `get '/p' => 'controller#action'`.
//  5. A `# get '/x'` commented-out route emits NOTHING (# line comment stripping).
//  6. Resolve 0.8–0.9 + ClaimsReference.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/resolution/frameworks"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

func TestRailsDetect_Gemfile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Gemfile"), []byte(`source 'https://rubygems.org'
gem 'rails', '~> 7.1'
gem 'pg'
`), 0o644); err != nil {
		t.Fatal(err)
	}
	r := frameworks.NewRailsResolver(dir)
	if !r.Detect(context.Background()) {
		t.Error("Rails.Detect should return true when Gemfile includes 'rails'")
	}
}

func TestRailsDetect_False(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Gemfile"), []byte(`source 'https://rubygems.org'
gem 'sinatra'
`), 0o644); err != nil {
		t.Fatal(err)
	}
	r := frameworks.NewRailsResolver(dir)
	if r.Detect(context.Background()) {
		t.Error("Rails.Detect should return false when Gemfile does not include 'rails'")
	}
}

func TestRailsExtract_GetRoute(t *testing.T) {
	r := frameworks.NewRailsResolver(t.TempDir())
	src := `Rails.application.routes.draw do
  get '/users', to: 'users#index'
end
`
	nodes, refs := r.Extract("config/routes.rb", src)

	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}
	n := nodes[0]
	if n.ID != "route:config/routes.rb:2:GET:/users" {
		t.Errorf("node ID = %q", n.ID)
	}
	if n.QualifiedName != "config/routes.rb::METHOD:/users" {
		t.Errorf("node QN = %q", n.QualifiedName)
	}
	if n.Name != "GET /users" {
		t.Errorf("node Name = %q", n.Name)
	}
	if n.Language != types.LanguageRuby {
		t.Errorf("node Language = %v, want Ruby", n.Language)
	}
	if n.Kind != types.NodeKindRoute {
		t.Errorf("node Kind = %v, want Route", n.Kind)
	}

	// Handler ref: action last segment = 'index'
	if len(refs) != 1 {
		t.Fatalf("expected 1 ref, got %d", len(refs))
	}
	if refs[0].ReferenceName != "index" {
		t.Errorf("ref name = %q, want index", refs[0].ReferenceName)
	}
}

func TestRailsExtract_PostRoute(t *testing.T) {
	r := frameworks.NewRailsResolver(t.TempDir())
	src := `Rails.application.routes.draw do
  post '/orders', to: 'orders#create'
end
`
	nodes, refs := r.Extract("config/routes.rb", src)
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}
	if nodes[0].ID != "route:config/routes.rb:2:POST:/orders" {
		t.Errorf("node ID = %q", nodes[0].ID)
	}
	if len(refs) != 1 || refs[0].ReferenceName != "create" {
		t.Errorf("expected ref to 'create', got %v", refs)
	}
}

func TestRailsExtract_HashRocket(t *testing.T) {
	r := frameworks.NewRailsResolver(t.TempDir())
	src := `Rails.application.routes.draw do
  get '/products' => 'products#list'
end
`
	nodes, refs := r.Extract("config/routes.rb", src)
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}
	if nodes[0].ID != "route:config/routes.rb:2:GET:/products" {
		t.Errorf("node ID = %q", nodes[0].ID)
	}
	if len(refs) != 1 || refs[0].ReferenceName != "list" {
		t.Errorf("expected ref to 'list', got %v", refs)
	}
}

func TestRailsExtract_RootVerb(t *testing.T) {
	r := frameworks.NewRailsResolver(t.TempDir())
	src := `Rails.application.routes.draw do
  root 'home#index'
end
`
	nodes, refs := r.Extract("config/routes.rb", src)
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node for root verb, got %d", len(nodes))
	}
	n := nodes[0]
	if n.ID != "route:config/routes.rb:2:GET:/" {
		t.Errorf("root verb should produce path '/', node ID = %q", n.ID)
	}
	if len(refs) != 1 || refs[0].ReferenceName != "index" {
		t.Errorf("expected ref to 'index', got %v", refs)
	}
}

func TestRailsExtract_CommentedRouteEmitsNothing(t *testing.T) {
	// A route prefixed with # is a comment in Ruby — must NOT emit a node.
	r := frameworks.NewRailsResolver(t.TempDir())
	src := `Rails.application.routes.draw do
  # get '/secret', to: 'secret#index'
  get '/public', to: 'public#index'
end
`
	nodes, _ := r.Extract("config/routes.rb", src)
	if len(nodes) != 1 {
		t.Errorf("commented route must emit nothing; got %d nodes: %v", len(nodes), rubyNodeIDs(nodes))
	}
	if nodes[0].ID != "route:config/routes.rb:3:GET:/public" {
		t.Errorf("unexpected node ID: %s", nodes[0].ID)
	}
}

func TestRailsExtract_AllVerbsUppercase(t *testing.T) {
	r := frameworks.NewRailsResolver(t.TempDir())
	src := `Rails.application.routes.draw do
  get    '/a', to: 'a#index'
  post   '/b', to: 'b#create'
  put    '/c', to: 'c#update'
  patch  '/d', to: 'd#update'
  delete '/e', to: 'e#destroy'
end
`
	nodes, _ := r.Extract("config/routes.rb", src)
	if len(nodes) != 5 {
		t.Fatalf("expected 5 nodes, got %d", len(nodes))
	}
	want := []string{"GET", "POST", "PUT", "PATCH", "DELETE"}
	for i, n := range nodes {
		parts := strings.SplitN(n.Name, " ", 2)
		if parts[0] != want[i] {
			t.Errorf("node[%d] method = %q, want %q", i, parts[0], want[i])
		}
	}
}

func TestRailsExtract_NoMatchUnrelated(t *testing.T) {
	r := frameworks.NewRailsResolver(t.TempDir())
	nodes, refs := r.Extract("app/models/user.rb", `class User < ApplicationRecord
  validates :email, presence: true
end
`)
	if len(nodes) != 0 || len(refs) != 0 {
		t.Errorf("non-route file should emit nothing, got %d nodes %d refs", len(nodes), len(refs))
	}
}

func TestRailsResolve(t *testing.T) {
	r := frameworks.NewRailsResolver(t.TempDir())
	src := `Rails.application.routes.draw do
  get '/ping', to: 'ping#show'
end
`
	r.Extract("config/routes.rb", src)

	ref := types.UnresolvedReference{ReferenceName: "show"}
	resolved, err := r.Resolve(context.Background(), ref)
	if err != nil {
		t.Fatalf("Resolve error: %v", err)
	}
	if resolved.Confidence < 0.8 || resolved.Confidence > 0.9 {
		t.Errorf("Resolve confidence = %v, want 0.8–0.9", resolved.Confidence)
	}
	if !r.ClaimsReference("show") {
		t.Error("ClaimsReference should return true after Extract sees 'show'")
	}
}

// TestRailsDSL_ResourcesExpansion tests resources/resource DSL expansion —
// the primary gap that caused rw-rails to extract 0 routes.
//
// Fixture mirrors the rw-rails idiom:
//   - resource :user, only: [...]           → singular set, no index/no :id
//   - resources :articles, param: :slug, except: [:edit, :new] → 7-2 = 5 actions
//   - get :feed, on: :collection inside articles → GET /articles/feed
//   - resources :tags, only: [:index]       → 1 route
func TestRailsDSL_ResourcesExpansion(t *testing.T) {
	r := frameworks.NewRailsResolver(t.TempDir())
	src := `Rails.application.routes.draw do
  resource :user, only: [:show, :update]

  resources :articles, param: :slug, except: [:edit, :new] do
    get :feed, on: :collection
  end

  resources :tags, only: [:index]
end
`
	nodes, _ := r.Extract("config/routes.rb", src)

	// Build a method+path map for assertion
	routeMap := map[string]bool{}
	for _, n := range nodes {
		routeMap[n.Name] = true
	}

	// resource :user → GET/PATCH/PUT /user (show + update×2); no index, no :id
	wantPresent := []string{
		"GET /user",
		"PATCH /user",
		"PUT /user",
	}
	// resource :user must NOT produce an index (no GET /users) or :id segment
	wantAbsent := []string{
		"GET /users",
		"GET /user/:id",
	}
	// resources :articles, except: [:edit, :new] → 5 actions (6 routes: PATCH+PUT both count)
	wantPresent = append(wantPresent,
		"GET /articles",
		"POST /articles",
		"GET /articles/:slug",
		"PATCH /articles/:slug",
		"PUT /articles/:slug",
		"DELETE /articles/:slug",
	)
	// collection route inside do block
	wantPresent = append(wantPresent, "GET /articles/feed")
	// resources :tags, only: [:index]
	wantPresent = append(wantPresent, "GET /tags")

	for _, want := range wantPresent {
		if !routeMap[want] {
			t.Errorf("missing expected route %q; got: %v", want, rubyNodeIDs(nodes))
		}
	}
	for _, absent := range wantAbsent {
		if routeMap[absent] {
			t.Errorf("unexpected route %q should not be emitted", absent)
		}
	}

	// Total minimum count: 3 (user) + 6 (articles) + 1 (feed) + 1 (tags) = 11
	if len(nodes) < 11 {
		t.Errorf("expected at least 11 route nodes, got %d: %v", len(nodes), rubyNodeIDs(nodes))
	}
}

// TestRailsDSL_UnbalancedEndNoPanic ensures that extra trailing `end` lines in a
// routes.rb file (unbalanced depth) do not cause a panic or corrupt the depth
// counter below zero. Valid routes before the extra end must still be returned.
func TestRailsDSL_UnbalancedEndNoPanic(t *testing.T) {
	r := frameworks.NewRailsResolver(t.TempDir())
	src := `Rails.application.routes.draw do
  resources :posts, only: [:index]
end
end
end
`
	// Must not panic; must still return the valid route.
	nodes, _ := r.Extract("config/routes.rb", src)

	routeMap := map[string]bool{}
	for _, n := range nodes {
		routeMap[n.Name] = true
	}
	if !routeMap["GET /posts"] {
		t.Errorf("expected GET /posts in nodes; got: %v", rubyNodeIDs(nodes))
	}
}

// TestRailsScope_NamespacePrefix verifies that routes inside a namespace block
// carry the namespace path prefix. F-76: namespace :api means all nested routes
// get /api as a prefix.
func TestRailsScope_NamespacePrefix(t *testing.T) {
	r := frameworks.NewRailsResolver(t.TempDir())
	src := `Rails.application.routes.draw do
  namespace :api do
    resources :articles, only: [:index]
  end
end
`
	nodes, _ := r.Extract("config/routes.rb", src)

	routeMap := map[string]bool{}
	for _, n := range nodes {
		routeMap[n.Name] = true
	}

	// namespace :api → resources :articles → GET /api/articles (not /articles)
	if !routeMap["GET /api/articles"] {
		t.Errorf("namespace :api prefix missing: want GET /api/articles; got: %v", rubyNodeIDs(nodes))
	}
	// must NOT emit the non-prefixed path
	if routeMap["GET /articles"] {
		t.Errorf("scope-relative path emitted without prefix: got GET /articles, want GET /api/articles")
	}
}

// TestRailsScope_ScopeSymPrefix verifies scope :api do ... end (symbol form).
func TestRailsScope_ScopeSymPrefix(t *testing.T) {
	r := frameworks.NewRailsResolver(t.TempDir())
	src := `Rails.application.routes.draw do
  scope :api do
    resources :tags, only: [:index]
  end
end
`
	nodes, _ := r.Extract("config/routes.rb", src)

	routeMap := map[string]bool{}
	for _, n := range nodes {
		routeMap[n.Name] = true
	}

	if !routeMap["GET /api/tags"] {
		t.Errorf("scope :api prefix missing: want GET /api/tags; got: %v", rubyNodeIDs(nodes))
	}
	if routeMap["GET /tags"] {
		t.Errorf("scope-relative path emitted without prefix: got GET /tags, want GET /api/tags")
	}
}

// TestRailsScope_NonScopedRoutesUnchanged verifies that routes outside a
// scope/namespace block keep their plain path.
func TestRailsScope_NonScopedRoutesUnchanged(t *testing.T) {
	r := frameworks.NewRailsResolver(t.TempDir())
	src := `Rails.application.routes.draw do
  get '/health', to: 'health#show'
  resources :posts, only: [:index]
end
`
	nodes, _ := r.Extract("config/routes.rb", src)

	routeMap := map[string]bool{}
	for _, n := range nodes {
		routeMap[n.Name] = true
	}

	if !routeMap["GET /health"] {
		t.Errorf("plain route lost: want GET /health; got: %v", rubyNodeIDs(nodes))
	}
	if !routeMap["GET /posts"] {
		t.Errorf("plain resources route lost: want GET /posts; got: %v", rubyNodeIDs(nodes))
	}
}

// TestRailsScope_NestedNamespaces verifies nested namespace blocks compose
// their prefixes: namespace :api { namespace :v1 { resources :x } } → /api/v1/x.
func TestRailsScope_NestedNamespaces(t *testing.T) {
	r := frameworks.NewRailsResolver(t.TempDir())
	src := `Rails.application.routes.draw do
  namespace :api do
    namespace :v1 do
      resources :users, only: [:index]
    end
  end
end
`
	nodes, _ := r.Extract("config/routes.rb", src)

	routeMap := map[string]bool{}
	for _, n := range nodes {
		routeMap[n.Name] = true
	}

	if !routeMap["GET /api/v1/users"] {
		t.Errorf("nested namespaces: want GET /api/v1/users; got: %v", rubyNodeIDs(nodes))
	}
}

// rubyNodeIDs is a test helper.
func rubyNodeIDs(nodes []types.Node) []string {
	ids := make([]string, len(nodes))
	for i, n := range nodes {
		ids[i] = n.ID
	}
	return ids
}
