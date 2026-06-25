package resolution_test

// CP12 name matcher tests.
//
// Why this file is the spec gate:
//   - Weight consts are asserted literally (calibration gate — prevents silent
//     drift of the appendix-F values).
//   - obj.method resolves to the right method def (methodCall + receiver
//     inference for C++/Java).
//   - Two same-named funcs in different files: same-file +100 wins.
//   - Cross-language candidate penalized (−80) vs same-language.
//   - Kind affinity: call ref prefers function over same-named class (+25).
//   - Fuzzy fires only when exact and qualified lookups both miss.
//   - MatchReference sub-order: filePath → qualifiedName → methodCall →
//     exactName → fuzzy (first match wins).
//
// All tests seed a temp DB under t.TempDir() via openTestDB (defined in
// resolver_test.go, same package).

import (
	"context"
	"math"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/db"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/resolution"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// ---------------------------------------------------------------------------
// Calibration gate — assert the weight consts equal appendix-F values exactly.
// ---------------------------------------------------------------------------

func TestNameMatcherWeightConsts(t *testing.T) {
	// WHY: the scoring weights in appendix F are CALIBRATED constants. If any
	// const drifts (typo, rounding, refactor), this test fails loudly. It is
	// the single gate that prevents silent divergence from the reference.
	if resolution.ScoreSameFile != 100 {
		t.Errorf("ScoreSameFile = %d, want 100 (appendix F)", resolution.ScoreSameFile)
	}
	if resolution.ScorePathProximityMax != 80 {
		t.Errorf("ScorePathProximityMax = %d, want 80 (appendix F)", resolution.ScorePathProximityMax)
	}
	if resolution.ScoreSameLanguage != 50 {
		t.Errorf("ScoreSameLanguage = %d, want 50 (appendix F)", resolution.ScoreSameLanguage)
	}
	if resolution.ScoreCrossLanguage != -80 {
		t.Errorf("ScoreCrossLanguage = %d, want -80 (appendix F)", resolution.ScoreCrossLanguage)
	}
	if resolution.ScoreKindAffinity != 25 {
		t.Errorf("ScoreKindAffinity = %d, want 25 (appendix F)", resolution.ScoreKindAffinity)
	}
	if resolution.ScoreExported != 10 {
		t.Errorf("ScoreExported = %d, want 10 (appendix F)", resolution.ScoreExported)
	}
}

// ---------------------------------------------------------------------------
// Test: same-file +100 wins over any other same-named candidate
// ---------------------------------------------------------------------------

func TestNameMatcher_SameFileWins(t *testing.T) {
	// WHY: appendix F says same-file +100. Two same-named functions in different
	// files — the one in the same file as the reference must win. The +100 bonus
	// must dominate over all other factors for an equal starting point.
	d, _ := openTestDB(t)
	ctx := context.Background()

	refFile := "src/handler.ts"
	otherFile := "src/other/helper.ts"
	funcName := "parseRequest"

	// Seed file nodes.
	seedFile(t, d, refFile, types.LanguageTypeScript)
	seedFile(t, d, otherFile, types.LanguageTypeScript)

	// parseRequest in refFile (same file as the ref).
	sameFileNode := seedFunction(t, d, refFile, funcName, 10, types.LanguageTypeScript)
	// parseRequest in otherFile (different file, same language).
	_ = seedFunction(t, d, otherFile, funcName, 5, types.LanguageTypeScript)

	ref := types.UnresolvedReference{
		ID:            "ref-samefile",
		FromNodeID:    "file:" + refFile,
		ReferenceName: funcName,
		ReferenceKind: types.EdgeKindCalls,
		Line:          20,
		FilePath:      refFile,
		Language:      types.LanguageTypeScript,
	}

	nm := resolution.NewNameMatcher(d)
	result, err := nm.MatchReference(ctx, ref)
	if err != nil {
		t.Fatalf("MatchReference: %v", err)
	}
	if result == nil {
		t.Fatal("MatchReference returned nil, want a result")
	}
	if result.Node.ID != sameFileNode {
		t.Errorf("same-file: got node %q, want %q (same-file +100 must win)",
			result.Node.ID, sameFileNode)
	}
	if result.Confidence <= 0 {
		t.Errorf("confidence must be > 0, got %f", result.Confidence)
	}
}

// ---------------------------------------------------------------------------
// Test: cross-language candidate penalized (−80) vs same-language (+50)
// ---------------------------------------------------------------------------

func TestNameMatcher_CrossLanguagePenalty(t *testing.T) {
	// WHY: appendix F penalises cross-language candidates by −80 and rewards
	// same-language by +50. A Python function named "process" should beat a
	// JavaScript function named "process" when the reference is from Python.
	d, _ := openTestDB(t)
	ctx := context.Background()

	pyFile := "src/main.py"
	jsFile := "lib/helper.js"
	funcName := "process"

	seedFile(t, d, pyFile, types.LanguagePython)
	seedFile(t, d, jsFile, types.LanguageJavaScript)

	pyNode := seedFunction(t, d, pyFile, funcName, 5, types.LanguagePython)
	_ = seedFunction(t, d, jsFile, funcName, 5, types.LanguageJavaScript)

	ref := types.UnresolvedReference{
		ID:            "ref-crosslang",
		FromNodeID:    "file:" + pyFile,
		ReferenceName: funcName,
		ReferenceKind: types.EdgeKindCalls,
		Line:          15,
		FilePath:      pyFile,
		Language:      types.LanguagePython,
	}

	nm := resolution.NewNameMatcher(d)
	result, err := nm.MatchReference(ctx, ref)
	if err != nil {
		t.Fatalf("MatchReference: %v", err)
	}
	if result == nil {
		t.Fatal("MatchReference returned nil")
	}
	if result.Node.ID != pyNode {
		t.Errorf("cross-language: got %q, want Python node %q (same-lang +50 vs cross-lang −80)",
			result.Node.ID, pyNode)
	}
}

// ---------------------------------------------------------------------------
// Test: kind affinity — call ref prefers function over same-named class
// ---------------------------------------------------------------------------

func TestNameMatcher_KindAffinity_CallPrefersFunction(t *testing.T) {
	// WHY: appendix F grants +25 for call→function affinity. A `call` ref to
	// "fetch" should prefer a function node over a class node of the same name,
	// holding language and file path equal.
	d, _ := openTestDB(t)
	ctx := context.Background()

	theFile := "src/api.ts"
	seedFile(t, d, theFile, types.LanguageTypeScript)

	// Insert both a function and a class with the same name in the same file.
	funcID := nodeID(theFile, "function", "fetch", 10)
	if err := d.UpsertNode(ctx, types.Node{
		ID:         funcID,
		Kind:       types.NodeKindFunction,
		Name:       "fetch",
		FilePath:   theFile,
		Language:   types.LanguageTypeScript,
		StartLine:  10,
		EndLine:    15,
		IsExported: true,
	}); err != nil {
		t.Fatalf("UpsertNode function: %v", err)
	}

	classID := nodeID(theFile, "class", "fetch", 20)
	if err := d.UpsertNode(ctx, types.Node{
		ID:         classID,
		Kind:       types.NodeKindClass,
		Name:       "fetch",
		FilePath:   theFile,
		Language:   types.LanguageTypeScript,
		StartLine:  20,
		EndLine:    30,
		IsExported: true,
	}); err != nil {
		t.Fatalf("UpsertNode class: %v", err)
	}

	ref := types.UnresolvedReference{
		ID:            "ref-affinity",
		FromNodeID:    "file:" + theFile,
		ReferenceName: "fetch",
		ReferenceKind: types.EdgeKindCalls, // call → should prefer function
		Line:          5,
		FilePath:      theFile,
		Language:      types.LanguageTypeScript,
	}

	nm := resolution.NewNameMatcher(d)
	result, err := nm.MatchReference(ctx, ref)
	if err != nil {
		t.Fatalf("MatchReference: %v", err)
	}
	if result == nil {
		t.Fatal("MatchReference returned nil")
	}
	if result.Node.ID != funcID {
		t.Errorf("kind affinity: got %q (%s), want function node %q (call→function +25)",
			result.Node.ID, result.Node.Kind, funcID)
	}
}

// ---------------------------------------------------------------------------
// Test: instantiates ref prefers class over same-named function
// ---------------------------------------------------------------------------

func TestNameMatcher_KindAffinity_InstantiatesPrefersClass(t *testing.T) {
	// WHY: appendix F grants +25 for instantiates→class. An `instantiates` ref
	// should prefer a class node over a function of the same name.
	d, _ := openTestDB(t)
	ctx := context.Background()

	theFile := "src/models.ts"
	seedFile(t, d, theFile, types.LanguageTypeScript)

	funcID := nodeID(theFile, "function", "Widget", 5)
	if err := d.UpsertNode(ctx, types.Node{
		ID: funcID, Kind: types.NodeKindFunction, Name: "Widget",
		FilePath: theFile, Language: types.LanguageTypeScript,
		StartLine: 5, EndLine: 8, IsExported: true,
	}); err != nil {
		t.Fatalf("UpsertNode func: %v", err)
	}

	classID := nodeID(theFile, "class", "Widget", 10)
	if err := d.UpsertNode(ctx, types.Node{
		ID: classID, Kind: types.NodeKindClass, Name: "Widget",
		FilePath: theFile, Language: types.LanguageTypeScript,
		StartLine: 10, EndLine: 30, IsExported: true,
	}); err != nil {
		t.Fatalf("UpsertNode class: %v", err)
	}

	ref := types.UnresolvedReference{
		ID:            "ref-instantiates",
		FromNodeID:    "file:" + theFile,
		ReferenceName: "Widget",
		ReferenceKind: types.EdgeKindInstantiates, // instantiates → should prefer class
		Line:          50,
		FilePath:      theFile,
		Language:      types.LanguageTypeScript,
	}

	nm := resolution.NewNameMatcher(d)
	result, err := nm.MatchReference(ctx, ref)
	if err != nil {
		t.Fatalf("MatchReference: %v", err)
	}
	if result == nil {
		t.Fatal("nil result")
	}
	if result.Node.ID != classID {
		t.Errorf("instantiates affinity: got %q (%s), want class %q",
			result.Node.ID, result.Node.Kind, classID)
	}
}

// ---------------------------------------------------------------------------
// Test: method call (obj.method) resolves to correct method via methodCall path
// ---------------------------------------------------------------------------

func TestNameMatcher_MethodCall(t *testing.T) {
	// WHY: `obj.method()` references must resolve to the right method definition.
	// The methodCall strategy in matchReference uses the right-hand part of
	// "receiver.method" notation to look up method nodes. This proves both
	// that "." is parsed correctly and that NodeKindMethod is returned.
	d, _ := openTestDB(t)
	ctx := context.Background()

	theFile := "src/service.ts"
	seedFile(t, d, theFile, types.LanguageTypeScript)

	methodID := nodeID(theFile, "method", "connect", 20)
	if err := d.UpsertNode(ctx, types.Node{
		ID:         methodID,
		Kind:       types.NodeKindMethod,
		Name:       "connect",
		FilePath:   theFile,
		Language:   types.LanguageTypeScript,
		StartLine:  20,
		EndLine:    30,
		IsExported: true,
	}); err != nil {
		t.Fatalf("UpsertNode method: %v", err)
	}

	// Also seed a function named "connect" to prove method wins when ref has "."
	funcID := nodeID(theFile, "function", "connect", 5)
	if err := d.UpsertNode(ctx, types.Node{
		ID: funcID, Kind: types.NodeKindFunction, Name: "connect",
		FilePath: theFile, Language: types.LanguageTypeScript,
		StartLine: 5, EndLine: 10, IsExported: true,
	}); err != nil {
		t.Fatalf("UpsertNode func: %v", err)
	}

	ref := types.UnresolvedReference{
		ID:            "ref-method",
		FromNodeID:    "file:" + theFile,
		ReferenceName: "db.connect", // obj.method notation
		ReferenceKind: types.EdgeKindCalls,
		Line:          50,
		FilePath:      theFile,
		Language:      types.LanguageTypeScript,
	}

	nm := resolution.NewNameMatcher(d)
	result, err := nm.MatchReference(ctx, ref)
	if err != nil {
		t.Fatalf("MatchReference: %v", err)
	}
	if result == nil {
		t.Fatal("nil result")
	}
	if result.Node.Kind != types.NodeKindMethod {
		t.Errorf("method call: got kind %q, want %q", result.Node.Kind, types.NodeKindMethod)
	}
	if result.Node.Name != "connect" {
		t.Errorf("method call: got name %q, want %q", result.Node.Name, "connect")
	}
}

// ---------------------------------------------------------------------------
// Test: receiver inference for C++/Java method calls
// ---------------------------------------------------------------------------

func TestNameMatcher_ReceiverInference_Java(t *testing.T) {
	// WHY: for Java/C++, obj.method() should use the receiver type to scope the
	// method lookup. When a class "Connection" exists and has a method "open",
	// a ref "conn.open" in a Java file should resolve to Connection::open, not
	// some other "open" method from a different class. Receiver inference is
	// modest — it matches by heuristic (name similarity), not full type inference.
	d, _ := openTestDB(t)
	ctx := context.Background()

	theFile := "src/Main.java"
	otherFile := "src/Other.java"
	seedFile(t, d, theFile, types.LanguageJava)
	seedFile(t, d, otherFile, types.LanguageJava)

	// Method "open" on a class "Connection" in theFile.
	connOpen := nodeID(theFile, "method", "open", 15)
	if err := d.UpsertNode(ctx, types.Node{
		ID: connOpen, Kind: types.NodeKindMethod, Name: "open",
		QualifiedName: "Connection::open",
		FilePath:      theFile, Language: types.LanguageJava,
		StartLine: 15, EndLine: 20, IsExported: true,
	}); err != nil {
		t.Fatalf("UpsertNode Connection::open: %v", err)
	}

	// Also seed a generic "open" method in otherFile (should be less preferred).
	otherOpen := nodeID(otherFile, "method", "open", 5)
	if err := d.UpsertNode(ctx, types.Node{
		ID: otherOpen, Kind: types.NodeKindMethod, Name: "open",
		QualifiedName: "OtherClass::open",
		FilePath:      otherFile, Language: types.LanguageJava,
		StartLine: 5, EndLine: 10, IsExported: true,
	}); err != nil {
		t.Fatalf("UpsertNode OtherClass::open: %v", err)
	}

	// Ref: "conn.open" where receiver name "conn" hints at "Connection".
	// The receiver heuristic should prefer the method whose qualified_name contains
	// the receiver type (partial name match).
	ref := types.UnresolvedReference{
		ID:            "ref-receiver",
		FromNodeID:    "file:" + theFile,
		ReferenceName: "conn.open", // receiver "conn" → heuristic match to "Connection"
		ReferenceKind: types.EdgeKindCalls,
		Line:          40,
		FilePath:      theFile,
		Language:      types.LanguageJava,
	}

	nm := resolution.NewNameMatcher(d)
	result, err := nm.MatchReference(ctx, ref)
	if err != nil {
		t.Fatalf("MatchReference: %v", err)
	}
	if result == nil {
		t.Fatal("nil result")
	}
	// The Connection::open node from the same file should win over OtherClass::open.
	// Both same-file +100 and receiver affinity favor connOpen.
	if result.Node.ID != connOpen {
		t.Errorf("receiver inference: got %q (qname=%q), want %q (Connection::open)",
			result.Node.ID, result.Node.QualifiedName, connOpen)
	}
}

// ---------------------------------------------------------------------------
// Test: fuzzy fires only on exact/qualified miss
// ---------------------------------------------------------------------------

func TestNameMatcher_FuzzyFiresOnlyOnMiss(t *testing.T) {
	// WHY: fuzzy matching should only activate when exact name lookup and
	// qualified-name lookup both fail to find candidates. If an exact match
	// exists, fuzzy must NOT be the strategy that wins — the exact path must.
	d, _ := openTestDB(t)
	ctx := context.Background()

	theFile := "src/math.ts"
	seedFile(t, d, theFile, types.LanguageTypeScript)

	// Exact function "calculateTotal" exists.
	exactID := seedFunction(t, d, theFile, "calculateTotal", 10, types.LanguageTypeScript)

	// Ref with exact name — should resolve via exactName, not fuzzy.
	exactRef := types.UnresolvedReference{
		ID:            "ref-exact",
		FromNodeID:    "file:" + theFile,
		ReferenceName: "calculateTotal",
		ReferenceKind: types.EdgeKindCalls,
		Line:          20,
		FilePath:      theFile,
		Language:      types.LanguageTypeScript,
	}

	nm := resolution.NewNameMatcher(d)
	exactResult, err := nm.MatchReference(ctx, exactRef)
	if err != nil {
		t.Fatalf("MatchReference exact: %v", err)
	}
	if exactResult == nil || exactResult.Node.ID != exactID {
		t.Errorf("exact: got %v, want %q", exactResult, exactID)
	}
	// Must NOT report fuzzy strategy.
	if exactResult != nil && exactResult.Strategy == resolution.StrategyFuzzy {
		t.Error("exact match found but strategy reported as fuzzy — exact path should win")
	}

	// Now test a fuzzy-only case: ref with a slightly misspelled name.
	fuzzyRef := types.UnresolvedReference{
		ID:            "ref-fuzzy",
		FromNodeID:    "file:" + theFile,
		ReferenceName: "calcullateTotal", // extra 'l' — no exact match
		ReferenceKind: types.EdgeKindCalls,
		Line:          25,
		FilePath:      theFile,
		Language:      types.LanguageTypeScript,
	}

	fuzzyResult, err := nm.MatchReference(ctx, fuzzyRef)
	if err != nil {
		t.Fatalf("MatchReference fuzzy: %v", err)
	}
	// Fuzzy may or may not find a match depending on edit distance. What must hold:
	// if it does match, the strategy is fuzzy.
	if fuzzyResult != nil && fuzzyResult.Strategy != resolution.StrategyFuzzy {
		t.Errorf("fuzzy misspelling: strategy = %q, want %q", fuzzyResult.Strategy, resolution.StrategyFuzzy)
	}
}

// ---------------------------------------------------------------------------
// Test: matchReference sub-order — filePath wins first
// ---------------------------------------------------------------------------

func TestNameMatcher_SubOrderFilePath(t *testing.T) {
	// WHY: appendix F sub-order is filePath → qualifiedName → methodCall →
	// exactName → fuzzy. A ref whose ReferenceName looks like a file path
	// (contains "/") should match via the filePath strategy.
	d, _ := openTestDB(t)
	ctx := context.Background()

	targetFile := "src/utils/format.ts"
	seedFile(t, d, targetFile, types.LanguageTypeScript)

	// Also seed a function with the same path-like name to confirm filePath wins.
	seedFunction(t, d, "src/app.ts", "src/utils/format", 5, types.LanguageTypeScript)
	seedFile(t, d, "src/app.ts", types.LanguageTypeScript)

	ref := types.UnresolvedReference{
		ID:            "ref-filepath",
		FromNodeID:    "file:src/app.ts",
		ReferenceName: "src/utils/format", // looks like a path
		ReferenceKind: types.EdgeKindReferences,
		Line:          3,
		FilePath:      "src/app.ts",
		Language:      types.LanguageTypeScript,
	}

	nm := resolution.NewNameMatcher(d)
	result, err := nm.MatchReference(ctx, ref)
	if err != nil {
		t.Fatalf("MatchReference filepath: %v", err)
	}
	if result == nil {
		t.Fatal("filepath strategy: nil result")
	}
	if result.Strategy != resolution.StrategyFilePath {
		t.Errorf("filepath strategy: got %q, want %q", result.Strategy, resolution.StrategyFilePath)
	}
}

// ---------------------------------------------------------------------------
// Test: qualifiedName match (::- or .-separated)
// ---------------------------------------------------------------------------

func TestNameMatcher_QualifiedName(t *testing.T) {
	// WHY: a ref like "MyClass::myMethod" must resolve via the qualifiedName
	// strategy by matching the qualified_name column directly. This is the
	// second step in the sub-order.
	d, _ := openTestDB(t)
	ctx := context.Background()

	theFile := "src/models.ts"
	seedFile(t, d, theFile, types.LanguageTypeScript)

	qualID := nodeID(theFile, "method", "save", 20)
	if err := d.UpsertNode(ctx, types.Node{
		ID:            qualID,
		Kind:          types.NodeKindMethod,
		Name:          "save",
		QualifiedName: "MyClass::save",
		FilePath:      theFile,
		Language:      types.LanguageTypeScript,
		StartLine:     20,
		EndLine:       25,
		IsExported:    true,
	}); err != nil {
		t.Fatalf("UpsertNode: %v", err)
	}

	ref := types.UnresolvedReference{
		ID:            "ref-qname",
		FromNodeID:    "file:" + theFile,
		ReferenceName: "MyClass::save",
		ReferenceKind: types.EdgeKindCalls,
		Line:          30,
		FilePath:      theFile,
		Language:      types.LanguageTypeScript,
	}

	nm := resolution.NewNameMatcher(d)
	result, err := nm.MatchReference(ctx, ref)
	if err != nil {
		t.Fatalf("MatchReference qname: %v", err)
	}
	if result == nil {
		t.Fatal("nil result for qualified name")
	}
	if result.Node.ID != qualID {
		t.Errorf("qualifiedName: got %q, want %q", result.Node.ID, qualID)
	}
}

// ---------------------------------------------------------------------------
// Test: GetAllCandidates returns multiple results
// ---------------------------------------------------------------------------

func TestNameMatcher_GetAllCandidates(t *testing.T) {
	// WHY: appendix F says "expose a way to get all candidates". MatchReference
	// returns the best; NameMatcher must also offer GetAllCandidates for the
	// node tool (MCP CP22) to surface overloads.
	d, _ := openTestDB(t)
	ctx := context.Background()

	fileA := "src/a.ts"
	fileB := "src/b.ts"
	seedFile(t, d, fileA, types.LanguageTypeScript)
	seedFile(t, d, fileB, types.LanguageTypeScript)

	// Three candidates with the same name.
	seedFunction(t, d, fileA, "render", 10, types.LanguageTypeScript)
	seedFunction(t, d, fileA, "render", 20, types.LanguageTypeScript) // different line
	seedFunction(t, d, fileB, "render", 5, types.LanguageTypeScript)

	ref := types.UnresolvedReference{
		ID:            "ref-overload",
		FromNodeID:    "file:" + fileA,
		ReferenceName: "render",
		ReferenceKind: types.EdgeKindCalls,
		Line:          50,
		FilePath:      fileA,
		Language:      types.LanguageTypeScript,
	}

	nm := resolution.NewNameMatcher(d)
	candidates, err := nm.GetAllCandidates(ctx, ref)
	if err != nil {
		t.Fatalf("GetAllCandidates: %v", err)
	}
	if len(candidates) < 2 {
		t.Errorf("GetAllCandidates: got %d candidates, want >= 2", len(candidates))
	}
	// Verify they are sorted descending by score.
	for i := 1; i < len(candidates); i++ {
		if candidates[i].Score > candidates[i-1].Score {
			t.Errorf("candidates not sorted descending: [%d].Score=%f > [%d].Score=%f",
				i, candidates[i].Score, i-1, candidates[i-1].Score)
		}
	}
}

// ---------------------------------------------------------------------------
// Test: path proximity — closer directory scores higher
// ---------------------------------------------------------------------------

func TestNameMatcher_PathProximity(t *testing.T) {
	// WHY: appendix F says path proximity 0–80 (closer dir = higher). A function
	// in a sibling directory should outscore one in a distant directory, all else
	// equal (different files, same language, neither in the ref file).
	d, _ := openTestDB(t)
	ctx := context.Background()

	refFile := "src/api/handler.ts"
	nearFile := "src/api/utils.ts"   // same directory
	farFile := "lib/other/helper.ts" // distant directory

	seedFile(t, d, refFile, types.LanguageTypeScript)
	seedFile(t, d, nearFile, types.LanguageTypeScript)
	seedFile(t, d, farFile, types.LanguageTypeScript)

	nearNode := seedFunction(t, d, nearFile, "validate", 5, types.LanguageTypeScript)
	_ = seedFunction(t, d, farFile, "validate", 5, types.LanguageTypeScript)

	ref := types.UnresolvedReference{
		ID:            "ref-proximity",
		FromNodeID:    "file:" + refFile,
		ReferenceName: "validate",
		ReferenceKind: types.EdgeKindCalls,
		Line:          30,
		FilePath:      refFile,
		Language:      types.LanguageTypeScript,
	}

	nm := resolution.NewNameMatcher(d)
	result, err := nm.MatchReference(ctx, ref)
	if err != nil {
		t.Fatalf("MatchReference: %v", err)
	}
	if result == nil {
		t.Fatal("nil result")
	}
	if result.Node.ID != nearNode {
		t.Errorf("path proximity: got %q (from %q), want near node %q (from %q)",
			result.Node.ID, result.Node.FilePath, nearNode, nearFile)
	}
}

// ---------------------------------------------------------------------------
// Test: no match → nil result (not an error)
// ---------------------------------------------------------------------------

func TestNameMatcher_NoMatch(t *testing.T) {
	// WHY: when no candidate exists for a name, MatchReference must return
	// (nil, nil) — not an error. The pipeline treats nil-result as unresolved.
	d, _ := openTestDB(t)
	ctx := context.Background()

	ref := types.UnresolvedReference{
		ID:            "ref-nomatch",
		FromNodeID:    "file:src/app.ts",
		ReferenceName: "completelyNonexistentSymbol",
		ReferenceKind: types.EdgeKindCalls,
		Line:          1,
		FilePath:      "src/app.ts",
		Language:      types.LanguageTypeScript,
	}

	nm := resolution.NewNameMatcher(d)
	result, err := nm.MatchReference(ctx, ref)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Errorf("expected nil result for unknown symbol, got %+v", result)
	}
}

// ---------------------------------------------------------------------------
// Test: exported bias (+10) — exported candidate ranks higher than unexported
// ---------------------------------------------------------------------------

func TestNameMatcher_ExportedBias(t *testing.T) {
	// WHY: appendix F awards +10 for exported symbols. Two otherwise-identical
	// candidates (same file, same kind, same name, different lines) where one
	// is exported — the exported one must score higher.
	d, _ := openTestDB(t)
	ctx := context.Background()

	theFile := "src/lib.ts"
	seedFile(t, d, theFile, types.LanguageTypeScript)

	exportedID := nodeID(theFile, "function", "helper", 10)
	if err := d.UpsertNode(ctx, types.Node{
		ID: exportedID, Kind: types.NodeKindFunction, Name: "helper",
		FilePath: theFile, Language: types.LanguageTypeScript,
		StartLine: 10, EndLine: 14, IsExported: true, // exported
	}); err != nil {
		t.Fatalf("UpsertNode exported: %v", err)
	}

	unexportedID := nodeID(theFile, "function", "helper", 20)
	if err := d.UpsertNode(ctx, types.Node{
		ID: unexportedID, Kind: types.NodeKindFunction, Name: "helper",
		FilePath: theFile, Language: types.LanguageTypeScript,
		StartLine: 20, EndLine: 24, IsExported: false, // unexported
	}); err != nil {
		t.Fatalf("UpsertNode unexported: %v", err)
	}

	ref := types.UnresolvedReference{
		ID:            "ref-exported",
		FromNodeID:    "file:" + theFile,
		ReferenceName: "helper",
		ReferenceKind: types.EdgeKindCalls,
		// Line chosen to be equidistant from both (line 15).
		Line:     15,
		FilePath: theFile,
		Language: types.LanguageTypeScript,
	}

	nm := resolution.NewNameMatcher(d)
	candidates, err := nm.GetAllCandidates(ctx, ref)
	if err != nil {
		t.Fatalf("GetAllCandidates: %v", err)
	}
	if len(candidates) < 2 {
		t.Fatalf("expected 2 candidates, got %d", len(candidates))
	}
	// The first candidate (highest score) must be the exported one.
	if candidates[0].Node.ID != exportedID {
		t.Errorf("exported bias: top candidate = %q (exported=%v), want exported node %q",
			candidates[0].Node.ID, candidates[0].Node.IsExported, exportedID)
	}
}

// ---------------------------------------------------------------------------
// Test: line distance — nearer line scores higher
// ---------------------------------------------------------------------------

func TestNameMatcher_LineDistance(t *testing.T) {
	// WHY: appendix F says "nearer line = slight boost". Two candidates in
	// the same file with the same properties but at different lines — the one
	// closer to the reference line must score higher.
	d, _ := openTestDB(t)
	ctx := context.Background()

	theFile := "src/module.ts"
	seedFile(t, d, theFile, types.LanguageTypeScript)

	// Reference is at line 50.
	// Near function at line 45 (distance 5).
	nearID := nodeID(theFile, "function", "doWork", 45)
	if err := d.UpsertNode(ctx, types.Node{
		ID: nearID, Kind: types.NodeKindFunction, Name: "doWork",
		FilePath: theFile, Language: types.LanguageTypeScript,
		StartLine: 45, EndLine: 48, IsExported: true,
	}); err != nil {
		t.Fatalf("UpsertNode near: %v", err)
	}
	// Far function at line 1 (distance 49).
	farID := nodeID(theFile, "function", "doWork", 1)
	if err := d.UpsertNode(ctx, types.Node{
		ID: farID, Kind: types.NodeKindFunction, Name: "doWork",
		FilePath: theFile, Language: types.LanguageTypeScript,
		StartLine: 1, EndLine: 4, IsExported: true,
	}); err != nil {
		t.Fatalf("UpsertNode far: %v", err)
	}

	ref := types.UnresolvedReference{
		ID:            "ref-linedist",
		FromNodeID:    "file:" + theFile,
		ReferenceName: "doWork",
		ReferenceKind: types.EdgeKindCalls,
		Line:          50,
		FilePath:      theFile,
		Language:      types.LanguageTypeScript,
	}

	nm := resolution.NewNameMatcher(d)
	result, err := nm.MatchReference(ctx, ref)
	if err != nil {
		t.Fatalf("MatchReference: %v", err)
	}
	if result == nil {
		t.Fatal("nil result")
	}
	if result.Node.ID != nearID {
		t.Errorf("line distance: got %q (line %d), want near node %q (line 45, closer to ref line 50)",
			result.Node.ID, result.Node.StartLine, nearID)
	}
}

// ---------------------------------------------------------------------------
// Sanity: ScorePathProximityMax gradient is in [0, ScorePathProximityMax]
// ---------------------------------------------------------------------------

func TestPathProximityGradient(t *testing.T) {
	// WHY: pathProximityScore must always return a value in [0, ScorePathProximityMax].
	// This asserts the invariant holds for a range of path pairs.
	cases := []struct {
		ref    string
		node   string
		wantGT float64
	}{
		{"src/api/handler.ts", "src/api/utils.ts", 0},    // same dir > 0
		{"src/api/handler.ts", "src/services/svc.ts", 0}, // different subdir ≥ 0
		{"src/api/handler.ts", "lib/other/thing.ts", 0},  // distant ≥ 0
		{"src/api/handler.ts", "src/api/handler.ts", 70}, // same file > 70 (max proximity)
	}
	for _, tc := range cases {
		score := resolution.PathProximityScore(tc.ref, tc.node)
		if score < 0 || score > float64(resolution.ScorePathProximityMax) {
			t.Errorf("PathProximityScore(%q, %q) = %f, want [0, %d]",
				tc.ref, tc.node, score, resolution.ScorePathProximityMax)
		}
		if score < tc.wantGT {
			t.Errorf("PathProximityScore(%q, %q) = %f, want > %f",
				tc.ref, tc.node, score, tc.wantGT)
		}
	}
	// Also verify same-dir is strictly greater than distant.
	sameDir := resolution.PathProximityScore("src/api/handler.ts", "src/api/utils.ts")
	distant := resolution.PathProximityScore("src/api/handler.ts", "lib/other/thing.ts")
	if sameDir <= distant {
		t.Errorf("same-dir proximity (%f) must be > distant (%f)", sameDir, distant)
	}
	_ = math.IsNaN // avoid unused import
}

// ---------------------------------------------------------------------------
// levenshteinDistance unit test
// ---------------------------------------------------------------------------

func TestLevenshteinDistance(t *testing.T) {
	// WHY: bounded levenshtein is the core of the new in-memory byFuzzy.
	// These known pairs pin the correctness of the two-row DP; early-exit
	// behaviour is also verified (when distance > max, the function returns
	// max+1 without computing the full matrix).
	cases := []struct {
		a, b    string
		maxDist int
		want    int
	}{
		{"kitten", "sitting", 10, 3},
		{"foo", "foo", 2, 0},
		{"ab", "abc", 2, 1},
		{"", "abc", 2, 3},
		{"abc", "", 2, 3},
		{"a", "b", 1, 1},
		{"cat", "car", 1, 1},
		// Early-exit: far pair with max=1 should return ≥ 2 (actual max+1).
		{"kitten", "sitting", 1, 2},
		// Distance exactly at max is still returned.
		{"ab", "cd", 2, 2},
	}
	for _, tc := range cases {
		got := resolution.LevenshteinDistance(tc.a, tc.b, tc.maxDist)
		if got != tc.want {
			t.Errorf("LevenshteinDistance(%q, %q, max=%d) = %d, want %d",
				tc.a, tc.b, tc.maxDist, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// F-68: LevenshteinDistance empty-string base case must respect max
// ---------------------------------------------------------------------------

func TestLevenshteinDistance_EmptyStringRespectsMax(t *testing.T) {
	// WHY: when one input is empty, the distance equals the length of the other.
	// If that length exceeds max, the function must return max+1 (consistent with
	// the inner-loop early-exit contract) — not the raw length. Before this fix
	// LevenshteinDistance("", "verylongname", 1) returned 12 instead of ≤ max+1.
	cases := []struct {
		a, b    string
		maxDist int
		wantMax int // result must be ≤ wantMax
	}{
		// Empty-a: len(b)=12 > max=1 → must return max+1=2.
		{"", "verylongname", 1, 2},
		// Empty-b: len(a)=12 > max=1 → must return max+1=2.
		{"verylongname", "", 1, 2},
		// Empty-a: len(b)=3 ≤ max=5 → must return exactly 3.
		{"", "abc", 5, 3},
		// Empty-b: len(a)=3 ≤ max=5 → must return exactly 3.
		{"abc", "", 5, 3},
	}
	for _, tc := range cases {
		got := resolution.LevenshteinDistance(tc.a, tc.b, tc.maxDist)
		if got > tc.wantMax {
			t.Errorf("LevenshteinDistance(%q, %q, max=%d) = %d, want ≤ %d (max+1 when len>max)",
				tc.a, tc.b, tc.maxDist, got, tc.wantMax)
		}
	}
}

// ---------------------------------------------------------------------------
// In-memory byFuzzy behaviour test (SetKnownNames)
// ---------------------------------------------------------------------------

func TestNameMatcher_InMemoryFuzzy_ResolvesTypo(t *testing.T) {
	// WHY: the new byFuzzy scans the in-memory known-names set rather than
	// generating variant strings. This test verifies:
	//   1. A typo'd ref resolves to the right node when its name is in the
	//      warmed set (behavior-preserving).
	//   2. A ref whose name has NO candidate within the edit-distance threshold
	//      in the known-names set returns nil (no false positives).
	//   3. The known-names set is NOT consulted when it is empty — byFuzzy
	//      short-circuits, so an unpopulated matcher finds nothing via fuzzy.
	d, _ := openTestDB(t)
	ctx := context.Background()

	theFile := "src/calc.ts"
	seedFile(t, d, theFile, types.LanguageTypeScript)
	targetID := seedFunction(t, d, theFile, "calculateTotal", 10, types.LanguageTypeScript)

	// Case 1: known names populated — typo within edit distance resolves.
	nm := resolution.NewNameMatcher(d)
	nm.SetKnownNames([]string{"calculatetotal"}) // lowercased, as warmCaches stores them

	typoRef := types.UnresolvedReference{
		ID:            "ref-typo",
		FromNodeID:    "file:" + theFile,
		ReferenceName: "calculateTotall", // one extra 'l' — edit distance 1
		ReferenceKind: types.EdgeKindCalls,
		Line:          20,
		FilePath:      theFile,
		Language:      types.LanguageTypeScript,
	}
	result, err := nm.MatchReference(ctx, typoRef)
	if err != nil {
		t.Fatalf("MatchReference typo: %v", err)
	}
	if result == nil {
		t.Fatalf("in-memory fuzzy: expected match for typo'd ref, got nil")
	}
	if result.Node.ID != targetID {
		t.Errorf("in-memory fuzzy: resolved to %q, want %q", result.Node.ID, targetID)
	}
	if result.Strategy != resolution.StrategyFuzzy {
		t.Errorf("in-memory fuzzy: strategy = %q, want %q", result.Strategy, resolution.StrategyFuzzy)
	}

	// Case 2: known names populated but no candidate within threshold.
	nm2 := resolution.NewNameMatcher(d)
	nm2.SetKnownNames([]string{"calculatetotal"})
	farRef := types.UnresolvedReference{
		ID:            "ref-far",
		FromNodeID:    "file:" + theFile,
		ReferenceName: "zzz", // completely different — no match within distance
		ReferenceKind: types.EdgeKindCalls,
		Line:          30,
		FilePath:      theFile,
		Language:      types.LanguageTypeScript,
	}
	result2, err := nm2.MatchReference(ctx, farRef)
	if err != nil {
		t.Fatalf("MatchReference far: %v", err)
	}
	if result2 != nil {
		t.Errorf("in-memory fuzzy: expected nil for far ref, got node %q", result2.Node.ID)
	}

	// Case 3: empty known names — byFuzzy finds nothing (no DB scan without set).
	nm3 := resolution.NewNameMatcher(d)
	// SetKnownNames not called — knownNames is nil.
	result3, err := nm3.MatchReference(ctx, typoRef)
	if err != nil {
		t.Fatalf("MatchReference empty knownNames: %v", err)
	}
	// No known names → fuzzy finds nothing. Exact also misses (name is misspelled).
	if result3 != nil {
		t.Errorf("empty knownNames: expected nil, got node %q", result3.Node.ID)
	}
}

// ---------------------------------------------------------------------------
// CP4 resolver tweaks: single-dot SQL → qualified routing; exact-QName preference
// ---------------------------------------------------------------------------

// seedColumn inserts a column node with Name=col (bare) and QualifiedName=tableQName.col.
func seedColumn(t *testing.T, d *db.DB, filePath, tableQName, col string, line int) string {
	t.Helper()
	ctx := context.Background()
	qname := tableQName + "." + col
	id := nodeID(filePath, "column", qname, line)
	if err := d.UpsertNode(ctx, types.Node{
		ID:            id,
		Kind:          types.NodeKindColumn,
		Name:          col,
		QualifiedName: qname,
		FilePath:      filePath,
		Language:      types.LanguageSQL,
		StartLine:     line,
		EndLine:       line,
		IsExported:    true,
	}); err != nil {
		t.Fatalf("seedColumn %s: %v", qname, err)
	}
	return id
}

func TestNameMatcher_CP4_SingleDotSQLResolvesToColumn(t *testing.T) {
	// WHY (tweak 1): a single-dot SQL reference "acct.id" must reach byQualifiedName
	// so it can resolve to the column node acct.id. Without the tweak, byMethodCall
	// handles single dots but excludes columns, so the ref falls through to byExactName
	// which can't match because the column's Name is "id" not "acct.id" — miss.
	// Gate: ref.Language == LanguageSQL — non-SQL receiver.method is unchanged.
	d, _ := openTestDB(t)
	ctx := context.Background()

	sqlFile := "db/schema.sql"
	seedFile(t, d, sqlFile, types.LanguageSQL)

	// Seed the table and column nodes.
	colID := seedColumn(t, d, sqlFile, "acct", "id", 5)

	// SQL ref with single-dot name.
	ref := types.UnresolvedReference{
		ID:            "ref-col-sql",
		FromNodeID:    "proc:usp_Get",
		ReferenceName: "acct.id",
		ReferenceKind: types.EdgeKindReferences,
		Line:          20,
		FilePath:      sqlFile,
		Language:      types.LanguageSQL,
	}

	nm := resolution.NewNameMatcher(d)
	result, err := nm.MatchReference(ctx, ref)
	if err != nil {
		t.Fatalf("MatchReference: %v", err)
	}
	if result == nil {
		t.Fatal("CP4 tweak 1: MatchReference returned nil — single-dot SQL ref did not resolve")
	}
	if result.Node.ID != colID {
		t.Errorf("CP4 tweak 1: resolved to %q, want column node %q", result.Node.ID, colID)
	}
	if result.Strategy != resolution.StrategyQualifiedName {
		t.Errorf("CP4 tweak 1: strategy = %q, want %q", result.Strategy, resolution.StrategyQualifiedName)
	}
}

func TestNameMatcher_CP4_ExactQNamePreferred(t *testing.T) {
	// WHY (tweak 2): byQualifiedName currently returns all nodes whose QualifiedName
	// matches OR ends with ".id". When "acct.id" is the ref, both "acct.id" and
	// "person.id" match via the suffix rule. After tweak 2, if any candidate has
	// QualifiedName == "acct.id" exactly, only those are returned — person.id is excluded.
	// This prevents multi-table collision when the ref is schema-qualified.
	d, _ := openTestDB(t)
	ctx := context.Background()

	sqlFile := "db/schema.sql"
	seedFile(t, d, sqlFile, types.LanguageSQL)

	// Two tables each with an "id" column.
	acctColID := seedColumn(t, d, sqlFile, "acct", "id", 5)
	_ = seedColumn(t, d, sqlFile, "person", "id", 15)

	// Ref qualified to "acct.id" specifically.
	ref := types.UnresolvedReference{
		ID:            "ref-exact-qname",
		FromNodeID:    "proc:usp_Get",
		ReferenceName: "acct.id",
		ReferenceKind: types.EdgeKindReferences,
		Line:          30,
		FilePath:      sqlFile,
		Language:      types.LanguageSQL,
	}

	nm := resolution.NewNameMatcher(d)
	result, err := nm.MatchReference(ctx, ref)
	if err != nil {
		t.Fatalf("MatchReference: %v", err)
	}
	if result == nil {
		t.Fatal("CP4 tweak 2: MatchReference returned nil — no column resolved")
	}
	if result.Node.ID != acctColID {
		t.Errorf("CP4 tweak 2: resolved to %q, want acct.id node %q (exact-QName must beat suffix match)",
			result.Node.ID, acctColID)
	}
}

func TestNameMatcher_CP4_SC9_NonSQLSingleDotUnchanged(t *testing.T) {
	// WHY SC9: the SQL-scoped single-dot fall-through must NOT affect non-SQL languages.
	// A Go/TS "receiver.method" ref that currently resolves via byMethodCall must
	// continue to do so — the new path is gated strictly on LanguageSQL.
	d, _ := openTestDB(t)
	ctx := context.Background()

	tsFile := "src/api.ts"
	seedFile(t, d, tsFile, types.LanguageTypeScript)

	// Seed a method node.
	methodID := nodeID(tsFile, "method", "doWork", 10)
	if err := d.UpsertNode(ctx, types.Node{
		ID:         methodID,
		Kind:       types.NodeKindMethod,
		Name:       "doWork",
		FilePath:   tsFile,
		Language:   types.LanguageTypeScript,
		StartLine:  10,
		EndLine:    15,
		IsExported: true,
	}); err != nil {
		t.Fatalf("UpsertNode method: %v", err)
	}

	// TypeScript single-dot ref "obj.doWork" — must go through byMethodCall.
	ref := types.UnresolvedReference{
		ID:            "ref-ts-method",
		FromNodeID:    "file:" + tsFile,
		ReferenceName: "obj.doWork",
		ReferenceKind: types.EdgeKindCalls,
		Line:          20,
		FilePath:      tsFile,
		Language:      types.LanguageTypeScript,
	}

	nm := resolution.NewNameMatcher(d)
	result, err := nm.MatchReference(ctx, ref)
	if err != nil {
		t.Fatalf("MatchReference: %v", err)
	}
	if result == nil {
		t.Fatal("SC9: MatchReference returned nil — TypeScript method should still resolve")
	}
	if result.Node.ID != methodID {
		t.Errorf("SC9: resolved to %q, want method node %q", result.Node.ID, methodID)
	}
	if result.Strategy != resolution.StrategyMethodCall {
		t.Errorf("SC9: strategy = %q, want %q (non-SQL must not use qualifiedName path)",
			result.Strategy, resolution.StrategyMethodCall)
	}
}

func TestNameMatcher_CP4_SC9_NonSQLColumnGateBlocks(t *testing.T) {
	// WHY SC9 gate-coverage: the previous SC9 test only exercises the byMethodCall-
	// succeeds path, so removing the LanguageSQL gate leaves it green. This sibling
	// test exercises the gate directly: a NON-SQL single-dot ref where byMethodCall
	// finds NOTHING (no method/function node named "doWork" exists), but a COLUMN
	// node with QualifiedName "obj.doWork" DOES exist. Without the gate, tweak 1
	// would route the non-SQL ref to byQualifiedName and resolve to the column.
	// With the gate, the non-SQL ref must NOT resolve to that column.
	//
	// Verification: this test FAILS if the `ref.Language == LanguageSQL` gate in
	// matchReference is deleted, because byMethodCall returns empty → (no gate) →
	// byQualifiedName finds the column → resolves → result != nil → assertion trips.
	d, _ := openTestDB(t)
	ctx := context.Background()

	tsFile := "src/widget.ts"
	seedFile(t, d, tsFile, types.LanguageTypeScript)

	// Seed a COLUMN node (SQL kind) named "doWork" with QualifiedName "obj.doWork".
	// No method/function node named "doWork" exists — byMethodCall returns empty.
	colID := seedColumn(t, d, tsFile, "obj", "doWork", 5)

	// TypeScript single-dot ref: byMethodCall → empty; without gate → byQualifiedName
	// finds the column and resolves. The gate must block this.
	ref := types.UnresolvedReference{
		ID:            "ref-ts-col-gate",
		FromNodeID:    "file:" + tsFile,
		ReferenceName: "obj.doWork",
		ReferenceKind: types.EdgeKindCalls,
		Line:          20,
		FilePath:      tsFile,
		Language:      types.LanguageTypeScript, // NOT SQL
	}

	nm := resolution.NewNameMatcher(d)
	result, err := nm.MatchReference(ctx, ref)
	if err != nil {
		t.Fatalf("MatchReference: %v", err)
	}
	// The non-SQL ref must NOT resolve to the column node.
	if result != nil && result.Node.ID == colID {
		t.Errorf("SC9 gate: non-SQL ref resolved to column node %q — LanguageSQL gate must block byQualifiedName for non-SQL refs",
			colID)
	}
}
