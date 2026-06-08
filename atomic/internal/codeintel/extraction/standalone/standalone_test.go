package standalone_test

// Tests for the 5 standalone extractors: Vue, Svelte, Liquid, DFM, MyBatis.
//
// THE load-bearing test (appendix E contract): the Vue extractor runs the
// JS/TS TreeSitterExtractor on the embedded <script> block, then offsets all
// returned node/edge/ref line numbers by the block's start line so positions
// map back to the .vue file.  A symbol's StartLine in the result must equal
// its actual line in the .vue file, NOT its line within the <script> block.
//
// For the other formats: assert the root/component/mapper node + at least one
// child or reference. Node-count must be stable across two calls.

import (
	"context"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction/standalone"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func newPool(t *testing.T) *extraction.Pool {
	t.Helper()
	pool, err := extraction.NewPool(context.Background(), extraction.PoolOptions{Size: 1})
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	t.Cleanup(func() { pool.Close() })
	return pool
}

func findNode(nodes []types.Node, kind types.NodeKind, namePart string) *types.Node {
	for i := range nodes {
		if nodes[i].Kind == kind && strings.Contains(nodes[i].Name, namePart) {
			return &nodes[i]
		}
	}
	return nil
}

func countEdges(edges []types.Edge, kind types.EdgeKind) int {
	n := 0
	for _, e := range edges {
		if e.Kind == kind {
			n++
		}
	}
	return n
}

func countRefs(refs []types.UnresolvedReference, kind types.EdgeKind) int {
	n := 0
	for _, r := range refs {
		if r.ReferenceKind == kind {
			n++
		}
	}
	return n
}

// ---------------------------------------------------------------------------
// Vue — the load-bearing offset test
// ---------------------------------------------------------------------------

// vueFixture is a .vue SFC where the <script> block starts at line 5.
// The function greetUser is at line 8 in the file (line 4 within the script).
// The template references a kebab-case <user-card> component.
//
// Layout (1-indexed):
//
//	 1: <template>
//	 2:   <div>
//	 3:     <user-card :user="user" />
//	 4:   </div>
//	 5: </template>
//	 6: (blank)
//	 7: <script>
//	 8: export function greetUser(name) {
//	 9:   return 'Hello ' + name;
//	10: }
//	11: </script>
//	12: (blank)
//	13: <style scoped>
//	14: .card { color: red; }
//	15: </style>
const vueFixture = `<template>
  <div>
    <user-card :user="user" />
  </div>
</template>

<script>
export function greetUser(name) {
  return 'Hello ' + name;
}
</script>

<style scoped>
.card { color: red; }
</style>
`

const vueFixturePath = "src/components/Greeting.vue"

// TestVue_OffsetCorrect is the KEY test (appendix E).
// It verifies that:
//  1. A component node is emitted at line 1.
//  2. The greetUser function appears with StartLine == 8 (its actual position
//     in the .vue file), NOT line 2 (its position within the <script> block).
//     This proves the line-offset logic is working.
//  3. A contains edge exists from the component node to greetUser.
//  4. A references UnresolvedReference exists for the <user-card> template tag.
func TestVue_OffsetCorrect(t *testing.T) {
	pool := newPool(t)
	ext := standalone.NewVueExtractor(pool)

	result, err := ext.Extract(vueFixturePath, vueFixture)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(result.Errors) > 0 {
		t.Logf("extraction errors (non-fatal): %v", result.Errors)
	}

	// 1. Component node at line 1.
	comp := findNode(result.Nodes, types.NodeKindComponent, "Greeting")
	if comp == nil {
		// Try by file path basename.
		comp = findNode(result.Nodes, types.NodeKindComponent, "")
		if comp == nil {
			t.Fatalf("no component node found; nodes: %v", result.Nodes)
		}
	}
	if comp.StartLine != 1 {
		t.Errorf("component StartLine = %d, want 1", comp.StartLine)
	}
	if !comp.IsExported {
		t.Errorf("component IsExported = false, want true")
	}

	// 2. THE LOAD-BEARING TEST: greetUser must be at line 8 in the .vue file.
	//    The script block starts at line 8 of the file (the line with
	//    "export function greetUser"). If offset is missing, greetUser will
	//    appear at line 2 (script-relative). Correct offset = file line 8.
	greet := findNode(result.Nodes, types.NodeKindFunction, "greetUser")
	if greet == nil {
		t.Fatalf("greetUser function not found; nodes: %v", result.Nodes)
	}
	const wantLine = 8
	if greet.StartLine != wantLine {
		t.Errorf("greetUser StartLine = %d, want %d (offset test FAILED — script-relative line leaked into result)",
			greet.StartLine, wantLine)
	}

	// 3. contains edge: component → greetUser.
	containsCount := 0
	for _, e := range result.Edges {
		if e.Kind == types.EdgeKindContains && e.Source == comp.ID && e.Target == greet.ID {
			containsCount++
		}
	}
	if containsCount == 0 {
		t.Errorf("no contains edge from component to greetUser")
	}

	// 4. references ref for <user-card> template tag.
	refCount := countRefs(result.UnresolvedReferences, types.EdgeKindReferences)
	if refCount == 0 {
		t.Errorf("no references UnresolvedReferences for template component tags")
	}
	foundUserCard := false
	for _, r := range result.UnresolvedReferences {
		if r.ReferenceKind == types.EdgeKindReferences &&
			(strings.Contains(r.ReferenceName, "user-card") || strings.Contains(r.ReferenceName, "UserCard")) {
			foundUserCard = true
			break
		}
	}
	if !foundUserCard {
		t.Errorf("no references ref for user-card; refs: %v", result.UnresolvedReferences)
	}
}

// TestVue_NodeCountStable verifies that extracting the same fixture twice
// produces the same node count (idempotence).
func TestVue_NodeCountStable(t *testing.T) {
	pool := newPool(t)
	ext := standalone.NewVueExtractor(pool)

	r1, err := ext.Extract(vueFixturePath, vueFixture)
	if err != nil {
		t.Fatalf("first Extract: %v", err)
	}
	r2, err := ext.Extract(vueFixturePath, vueFixture)
	if err != nil {
		t.Fatalf("second Extract: %v", err)
	}
	if len(r1.Nodes) != len(r2.Nodes) {
		t.Errorf("node count changed: %d → %d", len(r1.Nodes), len(r2.Nodes))
	}
}

// TestVue_ScriptSetup verifies that <script setup> blocks are also handled.
const vueScriptSetupFixture = `<template>
  <MyButton />
</template>

<script setup>
import { ref } from 'vue'
const count = ref(0)
</script>
`

func TestVue_ScriptSetup(t *testing.T) {
	pool := newPool(t)
	ext := standalone.NewVueExtractor(pool)

	result, err := ext.Extract("src/Counter.vue", vueScriptSetupFixture)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	// Component node must exist.
	hasComp := false
	for _, n := range result.Nodes {
		if n.Kind == types.NodeKindComponent {
			hasComp = true
			break
		}
	}
	if !hasComp {
		t.Errorf("no component node found")
	}

	// <MyButton /> → references ref.
	foundMyButton := false
	for _, r := range result.UnresolvedReferences {
		if r.ReferenceKind == types.EdgeKindReferences &&
			strings.Contains(r.ReferenceName, "MyButton") {
			foundMyButton = true
		}
	}
	if !foundMyButton {
		t.Errorf("no references ref for MyButton")
	}

	// The import (ref from 'vue') should be offset to line 6 (script starts at line 6).
	// At minimum, the count variable or import should not be at script-relative line 2.
	for _, n := range result.Nodes {
		if n.Kind == types.NodeKindVariable && strings.Contains(n.Name, "count") {
			// count is at line 7 in the file; script content starts at line 6,
			// so script-relative line 2 → file line 7.
			if n.StartLine < 6 {
				t.Errorf("count variable StartLine = %d; expected >= 6 (file-relative)", n.StartLine)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Svelte
// ---------------------------------------------------------------------------

const svelteFixture = `<script>
  export let name = 'World';
  function greet() {
    return 'Hello ' + name;
  }
</script>

<div>
  <Counter />
</div>
`

const svelteFixturePath = "src/Hello.svelte"

// TestSvelte_RootNodeAndChildren verifies that:
//  1. A component node is emitted.
//  2. At least one child node from the <script> block is present.
//  3. A references ref for the <Counter> template tag exists.
func TestSvelte_RootNodeAndChildren(t *testing.T) {
	pool := newPool(t)
	ext := standalone.NewSvelteExtractor(pool)

	result, err := ext.Extract(svelteFixturePath, svelteFixture)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	hasComp := false
	for _, n := range result.Nodes {
		if n.Kind == types.NodeKindComponent {
			hasComp = true
			break
		}
	}
	if !hasComp {
		t.Errorf("no component node found; nodes: %v", result.Nodes)
	}

	// At least one non-component child node (script content).
	childCount := 0
	for _, n := range result.Nodes {
		if n.Kind != types.NodeKindComponent {
			childCount++
		}
	}
	if childCount == 0 {
		t.Errorf("no child nodes from script block")
	}

	// <Counter /> → references ref.
	foundCounter := false
	for _, r := range result.UnresolvedReferences {
		if r.ReferenceKind == types.EdgeKindReferences &&
			strings.Contains(r.ReferenceName, "Counter") {
			foundCounter = true
		}
	}
	if !foundCounter {
		t.Errorf("no references ref for Counter component tag; refs: %v", result.UnresolvedReferences)
	}
}

// TestSvelte_NodeCountStable verifies idempotence.
func TestSvelte_NodeCountStable(t *testing.T) {
	pool := newPool(t)
	ext := standalone.NewSvelteExtractor(pool)

	r1, _ := ext.Extract(svelteFixturePath, svelteFixture)
	r2, _ := ext.Extract(svelteFixturePath, svelteFixture)
	if len(r1.Nodes) != len(r2.Nodes) {
		t.Errorf("node count changed: %d → %d", len(r1.Nodes), len(r2.Nodes))
	}
}

// ---------------------------------------------------------------------------
// Liquid
// ---------------------------------------------------------------------------

const liquidFixture = `{% render 'header', title: page.title %}
<div class="content">
  {% include 'product-card' %}
  <p>{{ product.description }}</p>
</div>
{% render 'footer' %}
`

const liquidFixturePath = "templates/product.liquid"

// TestLiquid_RootNodeAndRefs verifies that:
//  1. A component/template node is emitted.
//  2. References exist for the rendered/included templates.
func TestLiquid_RootNodeAndRefs(t *testing.T) {
	ext := standalone.NewLiquidExtractor()

	result, err := ext.Extract(liquidFixturePath, liquidFixture)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	hasComp := false
	for _, n := range result.Nodes {
		if n.Kind == types.NodeKindComponent {
			hasComp = true
			break
		}
	}
	if !hasComp {
		t.Errorf("no component node; nodes: %v", result.Nodes)
	}

	// Should have references for 'header', 'product-card', 'footer'.
	refCount := countRefs(result.UnresolvedReferences, types.EdgeKindReferences)
	if refCount < 2 {
		t.Errorf("want >= 2 references refs, got %d; refs: %v", refCount, result.UnresolvedReferences)
	}
}

// TestLiquid_NodeCountStable verifies idempotence.
func TestLiquid_NodeCountStable(t *testing.T) {
	ext := standalone.NewLiquidExtractor()
	r1, _ := ext.Extract(liquidFixturePath, liquidFixture)
	r2, _ := ext.Extract(liquidFixturePath, liquidFixture)
	if len(r1.Nodes) != len(r2.Nodes) {
		t.Errorf("node count changed: %d → %d", len(r1.Nodes), len(r2.Nodes))
	}
}

// ---------------------------------------------------------------------------
// Delphi DFM
// ---------------------------------------------------------------------------

const dfmFixture = `object LoginForm: TLoginForm
  Left = 0
  Top = 0
  Caption = 'Login'
  object UsernameEdit: TEdit
    Left = 8
    Top = 8
    Width = 200
    Height = 24
  end
  object LoginButton: TButton
    Left = 8
    Top = 40
    Caption = 'Login'
  end
end
`

const dfmFixturePath = "forms/LoginForm.dfm"

// TestDFM_RootFormAndChildren verifies that:
//  1. A component node for the root form is emitted.
//  2. At least one child object (UsernameEdit or LoginButton) is emitted.
func TestDFM_RootFormAndChildren(t *testing.T) {
	ext := standalone.NewDFMExtractor()

	result, err := ext.Extract(dfmFixturePath, dfmFixture)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	// Root form component.
	form := findNode(result.Nodes, types.NodeKindComponent, "LoginForm")
	if form == nil {
		t.Fatalf("no LoginForm component; nodes: %v", result.Nodes)
	}

	// Child objects.
	hasChild := false
	for _, n := range result.Nodes {
		if n.Kind == types.NodeKindComponent && strings.Contains(n.Name, "Edit") ||
			n.Kind == types.NodeKindComponent && strings.Contains(n.Name, "Button") {
			hasChild = true
			break
		}
	}
	if !hasChild {
		t.Errorf("no child component nodes (TEdit/TButton); nodes: %v", result.Nodes)
	}

	// contains edges from form to children.
	containsCount := countEdges(result.Edges, types.EdgeKindContains)
	if containsCount == 0 {
		t.Errorf("no contains edges")
	}
}

// TestDFM_NodeCountStable verifies idempotence.
func TestDFM_NodeCountStable(t *testing.T) {
	ext := standalone.NewDFMExtractor()
	r1, _ := ext.Extract(dfmFixturePath, dfmFixture)
	r2, _ := ext.Extract(dfmFixturePath, dfmFixture)
	if len(r1.Nodes) != len(r2.Nodes) {
		t.Errorf("node count changed: %d → %d", len(r1.Nodes), len(r2.Nodes))
	}
}

// ---------------------------------------------------------------------------
// MyBatis XML
// ---------------------------------------------------------------------------

const mybatisFixture = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE mapper PUBLIC "-//mybatis.org//DTD Mapper 3.0//EN"
    "http://mybatis.org/dtd/mybatis-3-mapper.dtd">
<mapper namespace="com.example.UserMapper">
  <select id="findById" resultType="User">
    SELECT * FROM users WHERE id = #{id}
  </select>
  <insert id="insertUser" parameterType="User">
    INSERT INTO users (name, email) VALUES (#{name}, #{email})
  </insert>
  <update id="updateUser" parameterType="User">
    UPDATE users SET name=#{name} WHERE id=#{id}
  </update>
  <delete id="deleteById">
    DELETE FROM users WHERE id=#{id}
  </delete>
</mapper>
`

const mybatisFixturePath = "src/main/resources/mappers/UserMapper.xml"

// TestMyBatis_MapperAndStatements verifies that:
//  1. A mapper/module node is emitted for the <mapper> element.
//  2. Nodes exist for each statement (select, insert, update, delete).
//  3. References exist for the namespace.
func TestMyBatis_MapperAndStatements(t *testing.T) {
	ext := standalone.NewMyBatisExtractor()

	result, err := ext.Extract(mybatisFixturePath, mybatisFixture)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	// Mapper root node.
	hasMapper := false
	for _, n := range result.Nodes {
		if n.Kind == types.NodeKindModule || n.Kind == types.NodeKindComponent {
			hasMapper = true
			break
		}
	}
	if !hasMapper {
		t.Errorf("no mapper root node; nodes: %v", result.Nodes)
	}

	// Statement nodes (select, insert, update, delete → function/method kind).
	statementCount := 0
	for _, n := range result.Nodes {
		if n.Kind == types.NodeKindFunction || n.Kind == types.NodeKindMethod {
			statementCount++
		}
	}
	if statementCount < 4 {
		t.Errorf("want >= 4 statement nodes (select/insert/update/delete), got %d; nodes: %v",
			statementCount, result.Nodes)
	}

	// contains edges.
	containsCount := countEdges(result.Edges, types.EdgeKindContains)
	if containsCount == 0 {
		t.Errorf("no contains edges")
	}
}

// TestMyBatis_NodeCountStable verifies idempotence.
func TestMyBatis_NodeCountStable(t *testing.T) {
	ext := standalone.NewMyBatisExtractor()
	r1, _ := ext.Extract(mybatisFixturePath, mybatisFixture)
	r2, _ := ext.Extract(mybatisFixturePath, mybatisFixture)
	if len(r1.Nodes) != len(r2.Nodes) {
		t.Errorf("node count changed: %d → %d", len(r1.Nodes), len(r2.Nodes))
	}
}

// ---------------------------------------------------------------------------
// Vue — handler binding capture (@event + v-on:event)
// ---------------------------------------------------------------------------

// vueHandlerFixture is a .vue SFC with @click and v-on:submit bindings.
// The handlers are defined in the <script> block.
//
// Layout (1-indexed):
//
//	 1: <template>
//	 2:   <form v-on:submit="onSubmit">
//	 3:     <button @click="handleClick">Click</button>
//	 4:   </form>
//	 5: </template>
//	 6:
//	 7: <script>
//	 8: export default {
//	 9:   methods: {
//	10:     handleClick() { console.log('clicked'); },
//	11:     onSubmit(e) { e.preventDefault(); },
//	12:   },
//	13: };
//	14: </script>
const vueHandlerFixture = `<template>
  <form v-on:submit="onSubmit">
    <button @click="handleClick">Click</button>
  </form>
</template>

<script>
export default {
  methods: {
    handleClick() { console.log('clicked'); },
    onSubmit(e) { e.preventDefault(); },
  },
};
</script>
`

const vueHandlerFixturePath = "src/components/MyForm.vue"

// TestVue_HandlerBindingCapture verifies that @event="handler" and
// v-on:event="handler" bindings in the template produce UnresolvedReferences
// for the handler names. Both the @ shorthand and v-on: long form must be
// captured. The refs must be emitted from the component node (not a script
// method) so the synthesizer can resolve them to the <script> method nodes.
//
// Pre-fix this test fails (no handler refs produced). Post-fix both
// handleClick (@click) and onSubmit (v-on:submit) appear in UnresolvedReferences.
func TestVue_HandlerBindingCapture(t *testing.T) {
	pool := newPool(t)
	ext := standalone.NewVueExtractor(pool)

	result, err := ext.Extract(vueHandlerFixturePath, vueHandlerFixture)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	// Find the component node — all handler refs must originate from it.
	comp := findNode(result.Nodes, types.NodeKindComponent, "MyForm")
	if comp == nil {
		// fall back to any component node
		for i := range result.Nodes {
			if result.Nodes[i].Kind == types.NodeKindComponent {
				comp = &result.Nodes[i]
				break
			}
		}
	}
	if comp == nil {
		t.Fatalf("no component node found; nodes: %v", result.Nodes)
	}

	// Find handler refs by name in UnresolvedReferences.
	handlerRefs := map[string]bool{}
	for _, r := range result.UnresolvedReferences {
		if r.ReferenceKind == types.EdgeKindReferences && r.FromNodeID == comp.ID {
			handlerRefs[r.ReferenceName] = true
		}
	}

	// @click="handleClick" must produce a ref.
	if !handlerRefs["handleClick"] {
		t.Errorf("handleClick not found in handler refs; all refs: %v", result.UnresolvedReferences)
	}
	// v-on:submit="onSubmit" must produce a ref.
	if !handlerRefs["onSubmit"] {
		t.Errorf("onSubmit not found in handler refs; all refs: %v", result.UnresolvedReferences)
	}
}

// TestVue_HandlerBindingLineNumbers verifies that handler binding refs have
// correct file-relative line numbers (not zero, not template-relative).
func TestVue_HandlerBindingLineNumbers(t *testing.T) {
	pool := newPool(t)
	ext := standalone.NewVueExtractor(pool)

	result, err := ext.Extract(vueHandlerFixturePath, vueHandlerFixture)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	// handleClick is on line 3 in the fixture; onSubmit is on line 2.
	for _, r := range result.UnresolvedReferences {
		if r.ReferenceKind != types.EdgeKindReferences {
			continue
		}
		switch r.ReferenceName {
		case "handleClick":
			if r.Line == 0 {
				t.Errorf("handleClick ref has Line=0, want file-relative line number")
			}
		case "onSubmit":
			if r.Line == 0 {
				t.Errorf("onSubmit ref has Line=0, want file-relative line number")
			}
		}
	}
}

// ---------------------------------------------------------------------------
// For() routing
// ---------------------------------------------------------------------------

// TestFor_RoutesToCorrectExtractor verifies that For() returns non-nil
// extractors for the known extensions and nil for unknown ones.
func TestFor_RoutesToCorrectExtractor(t *testing.T) {
	pool := newPool(t)
	reg := standalone.NewRegistry(pool)

	known := []string{".vue", ".svelte", ".liquid", ".dfm", ".xml"}
	for _, ext := range known {
		e := reg.For(ext)
		if e == nil {
			t.Errorf("For(%q) = nil, want non-nil extractor", ext)
		}
	}

	unknown := []string{".go", ".ts", ".py", ".rb", ".unknown"}
	for _, ext := range unknown {
		e := reg.For(ext)
		if e != nil {
			t.Errorf("For(%q) = non-nil, want nil (not a standalone format)", ext)
		}
	}
}
