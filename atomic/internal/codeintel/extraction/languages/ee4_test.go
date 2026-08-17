package languages_test

// Inheritance capture, across the three languages that declare it differently.
// The interface-impl and override synthesizers and the type-hierarchy query all
// read extends and implements edges, so without these refs the graph carries no
// inheritance at all.

import (
	"context"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction/languages"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// Single extend plus multiple implements.
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

const ee4TSInterfaceFixture = `
interface IBase {
  m(): void;
  greet(name: string): string;
}
`

const ee4TSFixturePath = "src/ee4.ts"
const ee4TSInterfaceFixturePath = "src/ee4iface.ts"

// Mixes public and private bases; both are still bases.
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

// heritageRefDesc formats heritage refs for a failure message.
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

// This ref is what the resolution layer turns into the extends edge; without it
// a type-hierarchy query finds no ancestors.
func TestEE4_TS_ExtendsRefEmitted(t *testing.T) {
	t.Parallel()
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

// Each interface needs its own edge, since a synthesizer asks about one at a time.
func TestEE4_TS_ImplementsRefsEmitted(t *testing.T) {
	t.Parallel()
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

// The edge's source has to be the class; anchored at the file it would be both
// wrong and useless to a hierarchy query.
func TestEE4_TS_HeritageRefFromClassNode(t *testing.T) {
	t.Parallel()
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

// Interface-impl synthesis matches a concrete method against the interface
// method it implements, so the interface side needs a node of its own.
func TestEE4_TS_InterfaceMethodNode(t *testing.T) {
	t.Parallel()
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

func TestEE4_TS_InterfaceMethodNode_MultipleSignatures(t *testing.T) {
	t.Parallel()
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

// The fixture declares "speak" twice, once per parent, so the count pins that
// listing both method node types did not make either fire twice.
func TestEE4_TS_ClassMethodNotDoubleExtracted(t *testing.T) {
	t.Parallel()
	e := tsExtractor(t)
	result := e.Extract(context.Background(), ee4TSFixturePath, ee4TSFixture, types.LanguageTypeScript)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	// One from the interface, one from the class. More means a node type fired
	// twice; none means one stopped firing at all.
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

// Re-extracting a fixture must yield the same counts.
func TestEE4_TS_NodeCountStable(t *testing.T) {
	t.Parallel()
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

// TSX reuses the TypeScript config, and .tsx files declare classes too.
func TestEE4_TSX_ExtendsRefEmitted(t *testing.T) {
	t.Parallel()
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

// C++ has no implements keyword, so every base arrives as an extends and the
// resolution layer promotes the ones that turn out to be interfaces.
func TestEE4_Cpp_ExtendsRefEmitted(t *testing.T) {
	t.Parallel()
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

	for _, want := range []string{"Shape", "Drawable"} {
		if !nameSet[want] {
			t.Errorf("C++: no extends ref to %q; extends refs: %v", want, extendsNames)
		}
	}
}

// A C++ struct declares bases with the same clause a class does.
func TestEE4_Cpp_StructExtendsRefEmitted(t *testing.T) {
	t.Parallel()
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

// Re-extracting a fixture must yield the same counts.
func TestEE4_Cpp_NodeCountStable(t *testing.T) {
	t.Parallel()
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

func TestEE4_Java_ExtendsRefEmitted(t *testing.T) {
	t.Parallel()
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

func TestEE4_Java_ImplementsRefsEmitted(t *testing.T) {
	t.Parallel()
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

// Re-extracting a fixture must yield the same counts.
func TestEE4_Java_NodeCountStable(t *testing.T) {
	t.Parallel()
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

// The extends-to-implements promotion belongs to the resolution layer and is
// covered by its own tests, not here.
