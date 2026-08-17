package standalone_test

// The Vue and Svelte extractors run the JS/TS TreeSitterExtractor over the embedded
// <script> block, so every node/edge/ref position has to map back to the outer file
// rather than to its offset within the block.

import (
	"context"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction/standalone"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

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

// <script> opens at line 7, so greetUser sits at file line 8, script-relative 2.
// The template tag is kebab-case on purpose.
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

// greetUser landing at file line 8 rather than script-relative 2 is the offset proof.
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

	comp := findNode(result.Nodes, types.NodeKindComponent, "Greeting")
	if comp == nil {
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

	greet := findNode(result.Nodes, types.NodeKindFunction, "greetUser")
	if greet == nil {
		t.Fatalf("greetUser function not found; nodes: %v", result.Nodes)
	}
	const wantLine = 8
	if greet.StartLine != wantLine {
		t.Errorf("greetUser StartLine = %d, want %d (offset test FAILED — script-relative line leaked into result)",
			greet.StartLine, wantLine)
	}

	containsCount := 0
	for _, e := range result.Edges {
		if e.Kind == types.EdgeKindContains && e.Source == comp.ID && e.Target == greet.ID {
			containsCount++
		}
	}
	if containsCount == 0 {
		t.Errorf("no contains edge from component to greetUser")
	}

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

	// A top-level <script setup> const is module-scope (scopeDepth 0), outside any
	// FunctionScopeTypes construct, so local-variable suppression must keep it.
	count := findNode(result.Nodes, types.NodeKindVariable, "count")
	if count == nil {
		t.Fatalf("count variable node not found (want: top-level <script setup> const is kept under scope suppression); nodes: %v", result.Nodes)
	}
	// Script content starts at file line 6, so nothing from it may report lower.
	if count.StartLine < 6 {
		t.Errorf("count variable StartLine = %d; expected >= 6 (file-relative)", count.StartLine)
	}
}

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

	childCount := 0
	for _, n := range result.Nodes {
		if n.Kind != types.NodeKindComponent {
			childCount++
		}
	}
	if childCount == 0 {
		t.Errorf("no child nodes from script block")
	}

	// Svelte sibling of the Vue case above: `export let` is module-scope, so
	// local-variable suppression must keep it.
	if n := findNode(result.Nodes, types.NodeKindVariable, "name"); n == nil {
		t.Errorf("name variable node not found (want: top-level <script> let is kept under scope suppression); nodes: %v", result.Nodes)
	}

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

func TestSvelte_NodeCountStable(t *testing.T) {
	pool := newPool(t)
	ext := standalone.NewSvelteExtractor(pool)

	r1, _ := ext.Extract(svelteFixturePath, svelteFixture)
	r2, _ := ext.Extract(svelteFixturePath, svelteFixture)
	if len(r1.Nodes) != len(r2.Nodes) {
		t.Errorf("node count changed: %d → %d", len(r1.Nodes), len(r2.Nodes))
	}
}

// The node ID hashes the line, so hashing a script-relative line while correcting
// StartLine post-hoc leaves the two inconsistent. Padding the content with leading
// newlines gives the sub-extractor file-absolute lines from the start.
func TestVue_NodeIDUsesFileAbsoluteLine(t *testing.T) {
	pool := newPool(t)
	ext := standalone.NewVueExtractor(pool)

	result, err := ext.Extract(vueFixturePath, vueFixture)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	greet := findNode(result.Nodes, types.NodeKindFunction, "greetUser")
	if greet == nil {
		t.Fatalf("greetUser function not found; nodes: %v", result.Nodes)
	}

	const wantLine = 8
	if greet.StartLine != wantLine {
		t.Errorf("greetUser StartLine = %d, want %d", greet.StartLine, wantLine)
	}

	wantID := extraction.GenerateNodeID(vueFixturePath, string(types.NodeKindFunction), "greetUser", wantLine)
	if greet.ID != wantID {
		t.Errorf("greetUser node ID = %q\n\twant (file-absolute line %d) = %q\n\t(node ID embeds script-relative line — followup-hardening-f-4)",
			greet.ID, wantLine, wantID)
	}
}

// A <style> block ahead of <script> forces contentLineOffset > 0; greetSvelte then
// sits at file line 6, script-relative 2.
const svelteOffsetFixture = `<style>
  /* styles */
</style>

<script>
  function greetSvelte(user) {
    return 'Hello ' + user;
  }
</script>
`

const svelteOffsetFixturePath = "src/Greeter.svelte"

// Svelte sibling of TestVue_NodeIDUsesFileAbsoluteLine.
func TestSvelte_NodeIDUsesFileAbsoluteLine(t *testing.T) {
	pool := newPool(t)
	ext := standalone.NewSvelteExtractor(pool)

	result, err := ext.Extract(svelteOffsetFixturePath, svelteOffsetFixture)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	greet := findNode(result.Nodes, types.NodeKindFunction, "greetSvelte")
	if greet == nil {
		t.Fatalf("greetSvelte function not found; nodes: %v", result.Nodes)
	}

	const wantLine = 6
	if greet.StartLine != wantLine {
		t.Errorf("greetSvelte StartLine = %d, want %d", greet.StartLine, wantLine)
	}

	wantID := extraction.GenerateNodeID(svelteOffsetFixturePath, string(types.NodeKindFunction), "greetSvelte", wantLine)
	if greet.ID != wantID {
		t.Errorf("greetSvelte node ID = %q\n\twant (file-absolute line %d) = %q\n\t(node ID embeds script-relative line — followup-hardening-f-4)",
			greet.ID, wantLine, wantID)
	}
}

const liquidFixture = `{% render 'header', title: page.title %}
<div class="content">
  {% include 'product-card' %}
  <p>{{ product.description }}</p>
</div>
{% render 'footer' %}
`

const liquidFixturePath = "templates/product.liquid"

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

	// Both {% render %} and {% include %} are reference-producing forms.
	refCount := countRefs(result.UnresolvedReferences, types.EdgeKindReferences)
	if refCount < 2 {
		t.Errorf("want >= 2 references refs, got %d; refs: %v", refCount, result.UnresolvedReferences)
	}
}

func TestLiquid_NodeCountStable(t *testing.T) {
	ext := standalone.NewLiquidExtractor()
	r1, _ := ext.Extract(liquidFixturePath, liquidFixture)
	r2, _ := ext.Extract(liquidFixturePath, liquidFixture)
	if len(r1.Nodes) != len(r2.Nodes) {
		t.Errorf("node count changed: %d → %d", len(r1.Nodes), len(r2.Nodes))
	}
}

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

func TestDFM_RootFormAndChildren(t *testing.T) {
	ext := standalone.NewDFMExtractor()

	result, err := ext.Extract(dfmFixturePath, dfmFixture)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	form := findNode(result.Nodes, types.NodeKindComponent, "LoginForm")
	if form == nil {
		t.Fatalf("no LoginForm component; nodes: %v", result.Nodes)
	}

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

	containsCount := countEdges(result.Edges, types.EdgeKindContains)
	if containsCount == 0 {
		t.Errorf("no contains edges")
	}
}

func TestDFM_NodeCountStable(t *testing.T) {
	ext := standalone.NewDFMExtractor()
	r1, _ := ext.Extract(dfmFixturePath, dfmFixture)
	r2, _ := ext.Extract(dfmFixturePath, dfmFixture)
	if len(r1.Nodes) != len(r2.Nodes) {
		t.Errorf("node count changed: %d → %d", len(r1.Nodes), len(r2.Nodes))
	}
}

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

func TestMyBatis_MapperAndStatements(t *testing.T) {
	ext := standalone.NewMyBatisExtractor()

	result, err := ext.Extract(mybatisFixturePath, mybatisFixture)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

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

	// Statements land as function or method nodes, one per <select>/<insert>/etc.
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

	containsCount := countEdges(result.Edges, types.EdgeKindContains)
	if containsCount == 0 {
		t.Errorf("no contains edges")
	}
}

func TestMyBatis_NodeCountStable(t *testing.T) {
	ext := standalone.NewMyBatisExtractor()
	r1, _ := ext.Extract(mybatisFixturePath, mybatisFixture)
	r2, _ := ext.Extract(mybatisFixturePath, mybatisFixture)
	if len(r1.Nodes) != len(r2.Nodes) {
		t.Errorf("node count changed: %d → %d", len(r1.Nodes), len(r2.Nodes))
	}
}

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

// Both the @ shorthand and the v-on: long form must be captured, and the refs must
// come from the component node so the synthesizer can resolve them to script methods.
func TestVue_HandlerBindingCapture(t *testing.T) {
	pool := newPool(t)
	ext := standalone.NewVueExtractor(pool)

	result, err := ext.Extract(vueHandlerFixturePath, vueHandlerFixture)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	comp := findNode(result.Nodes, types.NodeKindComponent, "MyForm")
	if comp == nil {
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

	handlerRefs := map[string]bool{}
	for _, r := range result.UnresolvedReferences {
		if r.ReferenceKind == types.EdgeKindReferences && r.FromNodeID == comp.ID {
			handlerRefs[r.ReferenceName] = true
		}
	}

	if !handlerRefs["handleClick"] {
		t.Errorf("handleClick not found in handler refs; all refs: %v", result.UnresolvedReferences)
	}
	if !handlerRefs["onSubmit"] {
		t.Errorf("onSubmit not found in handler refs; all refs: %v", result.UnresolvedReferences)
	}
}

// Handler-binding refs carry file-relative lines, never zero or template-relative.
func TestVue_HandlerBindingLineNumbers(t *testing.T) {
	pool := newPool(t)
	ext := standalone.NewVueExtractor(pool)

	result, err := ext.Extract(vueHandlerFixturePath, vueHandlerFixture)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

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

// A call at the top level of <script setup> gets the enclosing scope as its owner,
// which there is the file: node. The Vue extractor drops file: nodes in favor of the
// component node, so a ref still pointing at one has no owner in result.Nodes and
// FK-fails the unresolved_refs insert (from_node_id REFERENCES nodes(id)).
const vueTopLevelCallFixture = `<script setup lang="ts">
import { onMounted } from 'vue'

function boot() {
  console.log('boot')
}

onMounted(() => {
  boot()
})
</script>
<template><div /></template>
`

const vueTopLevelCallFixturePath = "src/AtomCore.vue"

// Refs need the same file:-node-to-component rewrite that edges already get.
func TestVue_TopLevelScriptSetupCallOwnerIsComponent(t *testing.T) {
	pool := newPool(t)
	ext := standalone.NewVueExtractor(pool)

	result, err := ext.Extract(vueTopLevelCallFixturePath, vueTopLevelCallFixture)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	comp := findNode(result.Nodes, types.NodeKindComponent, "")
	if comp == nil {
		t.Fatalf("no component node found; nodes: %v", result.Nodes)
	}

	present := map[string]bool{}
	for _, n := range result.Nodes {
		present[n.ID] = true
	}

	sawOnMounted, sawBoot := false, false
	for _, r := range result.UnresolvedReferences {
		if !present[r.FromNodeID] {
			t.Errorf("ref %s (%s) has dangling owner %q — not present in result.Nodes; would FK-fail InsertUnresolvedRef",
				r.ID, r.ReferenceName, r.FromNodeID)
		}
		switch r.ReferenceName {
		case "onMounted":
			sawOnMounted = true
			if r.FromNodeID != comp.ID {
				t.Errorf("onMounted ref owner = %q, want component id %q", r.FromNodeID, comp.ID)
			}
		case "boot":
			sawBoot = true
			if r.FromNodeID != comp.ID {
				t.Errorf("boot ref owner = %q, want component id %q", r.FromNodeID, comp.ID)
			}
		}
	}
	if !sawOnMounted {
		t.Errorf("no ref for top-level onMounted() call; refs: %v", result.UnresolvedReferences)
	}
	if !sawBoot {
		t.Errorf("no ref for boot() call inside the onMounted callback; refs: %v", result.UnresolvedReferences)
	}
}

// Svelte sibling of vueTopLevelCallFixture — same file:-node owner gap.
const svelteTopLevelCallFixture = `<script>
import { onMount } from 'svelte'

function boot() {
  console.log('boot')
}

onMount(() => {
  boot()
})
</script>
<div />
`

const svelteTopLevelCallFixturePath = "src/AtomWidget.svelte"

func TestSvelte_TopLevelScriptCallOwnerIsComponent(t *testing.T) {
	pool := newPool(t)
	ext := standalone.NewSvelteExtractor(pool)

	result, err := ext.Extract(svelteTopLevelCallFixturePath, svelteTopLevelCallFixture)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	comp := findNode(result.Nodes, types.NodeKindComponent, "")
	if comp == nil {
		t.Fatalf("no component node found; nodes: %v", result.Nodes)
	}

	present := map[string]bool{}
	for _, n := range result.Nodes {
		present[n.ID] = true
	}

	sawOnMount, sawBoot := false, false
	for _, r := range result.UnresolvedReferences {
		if !present[r.FromNodeID] {
			t.Errorf("ref %s (%s) has dangling owner %q — not present in result.Nodes; would FK-fail InsertUnresolvedRef",
				r.ID, r.ReferenceName, r.FromNodeID)
		}
		switch r.ReferenceName {
		case "onMount":
			sawOnMount = true
			if r.FromNodeID != comp.ID {
				t.Errorf("onMount ref owner = %q, want component id %q", r.FromNodeID, comp.ID)
			}
		case "boot":
			sawBoot = true
			if r.FromNodeID != comp.ID {
				t.Errorf("boot ref owner = %q, want component id %q", r.FromNodeID, comp.ID)
			}
		}
	}
	if !sawOnMount {
		t.Errorf("no ref for top-level onMount() call; refs: %v", result.UnresolvedReferences)
	}
	if !sawBoot {
		t.Errorf("no ref for boot() call inside the onMount callback; refs: %v", result.UnresolvedReferences)
	}
}

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
