package languages_test

// EE4 extractor tests — inheritance (heritage) capture.
//
// What these tests prove:
//  1. TS: `class Dog extends Animal implements Speaker {}` → extends ref Dog→Animal
//     AND implements ref Dog→Speaker after full extraction (before resolution).
//  2. TS: multiple implements → multiple refs (one per interface).
//  3. C++: `class Circle : public Shape {}` → extends ref Circle→Shape.
//  4. C++: multiple bases → multiple extends refs.
//  5. Java: `class C extends B implements I {}` → extends C→B + implements C→I.
//  6. Java: multiple implements → multiple refs.
//  7. TS interface methods: `interface I { m(): void }` → method node `m` exists.
//  8. No double-count: class methods still extracted exactly once.
//  9. Node count stable across two extractions.
// 10. Extends→implements promotion fires when a TS class "extends" an interface (resolution
//     layer — tested via pipeline seeding, not extractor alone).
//
// WHY: EE4 is foundational — interface-impl + cpp-override synthesizers (CP16)
// and the query layer's GetTypeHierarchy all depend on extends/implements edges
// that were NEVER created before this checkpoint. Without EE4 the graph contains
// zero inheritance information.

import (
	"context"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction/languages"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

// ee4TSFixture: TypeScript class with single-extend + multiple-implements.
const ee4TSFixture = `
class Animal {
  name: string = "";
}
interface Speaker {
  speak(): void;
}
interface Runner {
  run(): void;
}
class Dog extends Animal implements Speaker, Runner {
  speak(): void {}
  run(): void {}
}
`

// ee4TSInterfaceFixture: TypeScript interface with method signatures.
const ee4TSInterfaceFixture = `
interface IBase {
  m(): void;
  greet(name: string): string;
}
`

const ee4TSFixturePath = "src/ee4.ts"
const ee4TSInterfaceFixturePath = "src/ee4iface.ts"

// ee4CppFixture: C++ class with multiple public/private bases.
const ee4CppFixture = `
class Shape {
public:
  virtual double area() const { return 0; }
};
class Drawable {
public:
  virtual void draw() const {}
};
class Circle : public Shape, public Drawable {
public:
  double area() const override { return 3.14; }
  void draw() const override {}
};
`

const ee4CppFixturePath = "src/ee4.cpp"

// ee4JavaFixture: Java class with extends + implements.
const ee4JavaFixture = `
public interface Speakable {
  void speak();
}
public interface Runnable {
  void run();
}
public class Animal {
  public String name = "";
}
public class Dog extends Animal implements Speakable, Runnable {
  public void speak() {}
  public void run() {}
}
`

const ee4JavaFixturePath = "src/ee4.java"

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func heritageRefs(refs []types.UnresolvedReference) []types.UnresolvedReference {
	var out []types.UnresolvedReference
	for _, r := range refs {
		if r.ReferenceKind == types.EdgeKindExtends || r.ReferenceKind == types.EdgeKindImplements {
			out = append(out, r)
		}
	}
	return out
}

func heritageRefNames(refs []types.UnresolvedReference, kind types.EdgeKind) []string {
	var out []string
	for _, r := range refs {
		if r.ReferenceKind == kind {
			out = append(out, r.ReferenceName)
		}
	}
	return out
}

// heritageRefDesc returns a debug string for a list of heritage refs.
func heritageRefDesc(refs []types.UnresolvedReference) string {
	var sb strings.Builder
	for _, r := range refs {
		sb.WriteString(string(r.ReferenceKind))
		sb.WriteByte(':')
		sb.WriteString(r.ReferenceName)
		sb.WriteByte(' ')
	}
	return sb.String()
}

func tsExtractor(t *testing.T) *extraction.TreeSitterExtractor {
	t.Helper()
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageTypeScript)
	if !ok {
		t.Fatal("LanguageTypeScript not registered")
	}
	return newExtractor(t, extLang, cfg)
}

func tsxExtractor(t *testing.T) *extraction.TreeSitterExtractor {
	t.Helper()
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageTSX)
	if !ok {
		t.Fatal("LanguageTSX not registered")
	}
	return newExtractor(t, extLang, cfg)
}

func cppExtractor(t *testing.T) *extraction.TreeSitterExtractor {
	t.Helper()
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageCpp)
	if !ok {
		t.Fatal("LanguageCpp not registered")
	}
	return newExtractor(t, extLang, cfg)
}

func javaExtractor(t *testing.T) *extraction.TreeSitterExtractor {
	t.Helper()
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageJava)
	if !ok {
		t.Fatal("LanguageJava not registered")
	}
	return newExtractor(t, extLang, cfg)
}

// ---------------------------------------------------------------------------
// TypeScript EE4 tests
// ---------------------------------------------------------------------------

// TestEE4_TS_ExtendsRefEmitted proves that `class Dog extends Animal …` emits
// an UnresolvedReference with ReferenceKind=extends and ReferenceName="Animal".
// WHY: The extends ref is how the resolution layer creates the extends edge
// Dog→Animal. Without it, GetTypeHierarchy returns no ancestors.
func TestEE4_TS_ExtendsRefEmitted(t *testing.T) {
	e := tsExtractor(t)
	result := e.Extract(context.Background(), ee4TSFixturePath, ee4TSFixture, types.LanguageTypeScript)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	extendsNames := heritageRefNames(result.UnresolvedReferences, types.EdgeKindExtends)
	found := false
	for _, n := range extendsNames {
		if n == "Animal" {
			found = true
		}
	}
	if !found {
		t.Errorf("no extends ref to 'Animal'; all heritage refs: %s",
			heritageRefDesc(heritageRefs(result.UnresolvedReferences)))
	}
}

// TestEE4_TS_ImplementsRefsEmitted proves that `class Dog … implements Speaker, Runner`
// emits two implements refs: one to "Speaker" and one to "Runner".
// WHY: Each implemented interface needs its own implements edge — a synthesizer
// checking "does Dog implement Speaker?" needs that edge to exist.
func TestEE4_TS_ImplementsRefsEmitted(t *testing.T) {
	e := tsExtractor(t)
	result := e.Extract(context.Background(), ee4TSFixturePath, ee4TSFixture, types.LanguageTypeScript)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	implNames := heritageRefNames(result.UnresolvedReferences, types.EdgeKindImplements)
	nameSet := make(map[string]bool, len(implNames))
	for _, n := range implNames {
		nameSet[n] = true
	}

	for _, want := range []string{"Speaker", "Runner"} {
		if !nameSet[want] {
			t.Errorf("no implements ref to %q; implements refs: %v", want, implNames)
		}
	}
}

// TestEE4_TS_HeritageRefFromClassNode proves that heritage refs are emitted FROM
// the class node (not the file node).
// WHY: The extends/implements edge must have the class as source — anchoring at
// the file node would be semantically wrong and useless for type-hierarchy queries.
func TestEE4_TS_HeritageRefFromClassNode(t *testing.T) {
	e := tsExtractor(t)
	result := e.Extract(context.Background(), ee4TSFixturePath, ee4TSFixture, types.LanguageTypeScript)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	fileID := "file:" + ee4TSFixturePath
	hrefs := heritageRefs(result.UnresolvedReferences)
	if len(hrefs) == 0 {
		t.Fatal("no heritage refs found")
	}
	for _, r := range hrefs {
		if r.FromNodeID == fileID || r.FromNodeID == "" {
			t.Errorf("heritage ref %s:%s has wrong FromNodeID %q (must be class node)",
				r.ReferenceKind, r.ReferenceName, r.FromNodeID)
		}
	}
}

// TestEE4_TS_InterfaceMethodNode proves that `interface I { m(): void }` produces
// a method node for `m`.
// WHY: interface-impl synthesis (CP16) needs to match concrete method Dog.speak()
// against interface method Speaker.speak() — if Speaker.speak() has no node, the
// synthesizer has nothing to link against.
func TestEE4_TS_InterfaceMethodNode(t *testing.T) {
	e := tsExtractor(t)
	result := e.Extract(context.Background(), ee4TSInterfaceFixturePath, ee4TSInterfaceFixture, types.LanguageTypeScript)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	node := findNode(result.Nodes, types.NodeKindMethod, "m")
	if node == nil {
		t.Errorf("method node 'm' not found; nodes: %s", nodeKindList(result.Nodes))
	}
}

// TestEE4_TS_InterfaceMethodNode_MultipleSignatures proves that multiple
// method_signature nodes in an interface all become method nodes.
func TestEE4_TS_InterfaceMethodNode_MultipleSignatures(t *testing.T) {
	e := tsExtractor(t)
	result := e.Extract(context.Background(), ee4TSInterfaceFixturePath, ee4TSInterfaceFixture, types.LanguageTypeScript)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	mNode := findNode(result.Nodes, types.NodeKindMethod, "m")
	greetNode := findNode(result.Nodes, types.NodeKindMethod, "greet")
	if mNode == nil {
		t.Error("method node 'm' not found")
	}
	if greetNode == nil {
		t.Error("method node 'greet' not found")
	}
}

// TestEE4_TS_ClassMethodNotDoubleExtracted proves class method_definition nodes
// are extracted exactly once. The fixture has "speak" as a method_signature in
// the Speaker interface AND as a method_definition in the Dog class — these must
// produce two separate method nodes (one per parent), not three or more.
// WHY: adding method_signature to MethodTypes must not cause method_definition
// to also fire for the same node — both grammar types exist in the grammar
// but a class body only contains method_definition, not method_signature.
func TestEE4_TS_ClassMethodNotDoubleExtracted(t *testing.T) {
	e := tsExtractor(t)
	result := e.Extract(context.Background(), ee4TSFixturePath, ee4TSFixture, types.LanguageTypeScript)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	// Count method nodes named "speak" — must be exactly 2:
	// one from Speaker interface (method_signature) and one from Dog class (method_definition).
	// If it were 3+, method_definition fired twice; if 0, method_signature broke extraction.
	count := 0
	for _, n := range result.Nodes {
		if n.Kind == types.NodeKindMethod && n.Name == "speak" {
			count++
		}
	}
	if count != 2 {
		t.Errorf("expected exactly 2 method nodes for 'speak' (interface + class), got %d; nodes: %s",
			count, nodeKindList(result.Nodes))
	}
}

// TestEE4_TS_NodeCountStable proves extraction is deterministic after EE4.
func TestEE4_TS_NodeCountStable(t *testing.T) {
	e := tsExtractor(t)
	ctx := context.Background()
	r1 := e.Extract(ctx, ee4TSFixturePath, ee4TSFixture, types.LanguageTypeScript)
	r2 := e.Extract(ctx, ee4TSFixturePath, ee4TSFixture, types.LanguageTypeScript)
	if len(r1.Nodes) != len(r2.Nodes) {
		t.Errorf("node count unstable: run1=%d run2=%d", len(r1.Nodes), len(r2.Nodes))
	}
	if len(r1.UnresolvedReferences) != len(r2.UnresolvedReferences) {
		t.Errorf("ref count unstable: run1=%d run2=%d",
			len(r1.UnresolvedReferences), len(r2.UnresolvedReferences))
	}
}

// ---------------------------------------------------------------------------
// TSX EE4 tests — verify heritage also works on TSX (mirrors TS config).
// ---------------------------------------------------------------------------

// TestEE4_TSX_ExtendsRefEmitted proves TSX files also emit extends refs.
// WHY: TSXExtractor mirrors TypeScriptExtractor config — it must inherit
// the heritage mechanism since .tsx files contain class declarations too.
func TestEE4_TSX_ExtendsRefEmitted(t *testing.T) {
	e := tsxExtractor(t)
	result := e.Extract(context.Background(), "src/ee4.tsx", ee4TSFixture, types.LanguageTSX)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	extendsNames := heritageRefNames(result.UnresolvedReferences, types.EdgeKindExtends)
	found := false
	for _, n := range extendsNames {
		if n == "Animal" {
			found = true
		}
	}
	if !found {
		t.Errorf("TSX: no extends ref to 'Animal'; heritage refs: %s",
			heritageRefDesc(heritageRefs(result.UnresolvedReferences)))
	}
}

// ---------------------------------------------------------------------------
// C++ EE4 tests
// ---------------------------------------------------------------------------

// TestEE4_Cpp_ExtendsRefEmitted proves `class Circle : public Shape {}` emits
// an extends ref to "Shape".
// WHY: C++ has no "implements" keyword — all bases are listed in the same
// base_class_clause. The resolution layer handles extends→implements promotion
// if Shape turns out to be an abstract class / pure virtual interface.
func TestEE4_Cpp_ExtendsRefEmitted(t *testing.T) {
	e := cppExtractor(t)
	result := e.Extract(context.Background(), ee4CppFixturePath, ee4CppFixture, types.LanguageCpp)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	extendsNames := heritageRefNames(result.UnresolvedReferences, types.EdgeKindExtends)
	nameSet := make(map[string]bool, len(extendsNames))
	for _, n := range extendsNames {
		nameSet[n] = true
	}

	// Circle : public Shape, public Drawable → two extends refs.
	for _, want := range []string{"Shape", "Drawable"} {
		if !nameSet[want] {
			t.Errorf("C++: no extends ref to %q; extends refs: %v", want, extendsNames)
		}
	}
}

// TestEE4_Cpp_StructExtendsRefEmitted proves struct bases also emit extends refs.
// WHY: C++ struct and class have the same base_class_clause grammar — both must work.
func TestEE4_Cpp_StructExtendsRefEmitted(t *testing.T) {
	e := cppExtractor(t)
	cppStructFixture := `
class Shape { public: virtual double area() const { return 0; } };
struct ColoredShape : public Shape {
  double area() const override { return 0; }
};
`
	result := e.Extract(context.Background(), ee4CppFixturePath, cppStructFixture, types.LanguageCpp)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	extendsNames := heritageRefNames(result.UnresolvedReferences, types.EdgeKindExtends)
	found := false
	for _, n := range extendsNames {
		if n == "Shape" {
			found = true
		}
	}
	if !found {
		t.Errorf("C++ struct: no extends ref to 'Shape'; extends refs: %v", extendsNames)
	}
}

// TestEE4_Cpp_NodeCountStable proves extraction is deterministic after EE4.
func TestEE4_Cpp_NodeCountStable(t *testing.T) {
	e := cppExtractor(t)
	ctx := context.Background()
	r1 := e.Extract(ctx, ee4CppFixturePath, ee4CppFixture, types.LanguageCpp)
	r2 := e.Extract(ctx, ee4CppFixturePath, ee4CppFixture, types.LanguageCpp)
	if len(r1.Nodes) != len(r2.Nodes) {
		t.Errorf("C++ node count unstable: run1=%d run2=%d", len(r1.Nodes), len(r2.Nodes))
	}
	if len(r1.UnresolvedReferences) != len(r2.UnresolvedReferences) {
		t.Errorf("C++ ref count unstable: run1=%d run2=%d",
			len(r1.UnresolvedReferences), len(r2.UnresolvedReferences))
	}
}

// ---------------------------------------------------------------------------
// Java EE4 tests
// ---------------------------------------------------------------------------

// TestEE4_Java_ExtendsRefEmitted proves `class Dog extends Animal …` emits an
// extends ref to "Animal".
func TestEE4_Java_ExtendsRefEmitted(t *testing.T) {
	e := javaExtractor(t)
	result := e.Extract(context.Background(), ee4JavaFixturePath, ee4JavaFixture, types.LanguageJava)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	extendsNames := heritageRefNames(result.UnresolvedReferences, types.EdgeKindExtends)
	found := false
	for _, n := range extendsNames {
		if n == "Animal" {
			found = true
		}
	}
	if !found {
		t.Errorf("Java: no extends ref to 'Animal'; extends refs: %v", extendsNames)
	}
}

// TestEE4_Java_ImplementsRefsEmitted proves Java `implements Speakable, Runnable`
// emits two implements refs.
func TestEE4_Java_ImplementsRefsEmitted(t *testing.T) {
	e := javaExtractor(t)
	result := e.Extract(context.Background(), ee4JavaFixturePath, ee4JavaFixture, types.LanguageJava)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	implNames := heritageRefNames(result.UnresolvedReferences, types.EdgeKindImplements)
	nameSet := make(map[string]bool, len(implNames))
	for _, n := range implNames {
		nameSet[n] = true
	}

	for _, want := range []string{"Speakable", "Runnable"} {
		if !nameSet[want] {
			t.Errorf("Java: no implements ref to %q; implements refs: %v", want, implNames)
		}
	}
}

// TestEE4_Java_NodeCountStable proves extraction is deterministic after EE4.
func TestEE4_Java_NodeCountStable(t *testing.T) {
	e := javaExtractor(t)
	ctx := context.Background()
	r1 := e.Extract(ctx, ee4JavaFixturePath, ee4JavaFixture, types.LanguageJava)
	r2 := e.Extract(ctx, ee4JavaFixturePath, ee4JavaFixture, types.LanguageJava)
	if len(r1.Nodes) != len(r2.Nodes) {
		t.Errorf("Java node count unstable: run1=%d run2=%d", len(r1.Nodes), len(r2.Nodes))
	}
	if len(r1.UnresolvedReferences) != len(r2.UnresolvedReferences) {
		t.Errorf("Java ref count unstable: run1=%d run2=%d",
			len(r1.UnresolvedReferences), len(r2.UnresolvedReferences))
	}
}

// Resolution promotion (extends→implements when target is interface) is proven
// end-to-end in resolution/synthesis/ee4_e2e_test.go (TestEE4_E2E_TS_ImplementsEdge)
// and via seeded-data in resolution/pipeline_test.go (TestExtendsEdgePromotedToImplements).
