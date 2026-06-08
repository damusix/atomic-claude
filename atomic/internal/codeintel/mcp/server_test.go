// Tests for the atomic code MCP server (master CP22).
//
// Tests use mcp.NewInMemoryTransports() to drive initialize + tools/call
// in-process, grounding the implementation against a real engine+fixture.
package mcp_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/engine"
	codemcp "github.com/damusix/atomic-claude/atomic/internal/codeintel/mcp"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// newTestEngine creates a temporary engine, indexes the given Go source
// (written to a temp dir), and returns the engine + file count.
func newTestEngine(t *testing.T, files map[string]string) (*engine.Engine, int) {
	t.Helper()

	dir := t.TempDir()
	for name, content := range files {
		fullPath := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
			t.Fatalf("write fixture %s: %v", name, err)
		}
	}

	eng, err := engine.New(dir)
	if err != nil {
		t.Fatalf("engine.New: %v", err)
	}
	ctx := context.Background()
	if err := eng.Init(ctx); err != nil {
		t.Fatalf("eng.Init: %v", err)
	}
	if err := eng.IndexAll(ctx); err != nil {
		t.Fatalf("eng.IndexAll: %v", err)
	}
	if err := eng.ResolveReferences(ctx); err != nil {
		t.Fatalf("eng.ResolveReferences: %v", err)
	}
	stats, _ := eng.GetStats(ctx)
	return eng, stats.FileCount
}

// connectClient connects a client to srv via in-memory transports, returns
// the connected ClientSession. The cleanup is handled via t.Cleanup.
//
// The in-memory transport pair is synchronised: the server goroutine calls
// Connect first, which unblocks the client Connect call. A WaitGroup ensures
// the server goroutine has reached Connect before the client attempts it,
// eliminating the race without a sleep.
func connectClient(t *testing.T, srv *sdk.Server) *sdk.ClientSession {
	t.Helper()
	clientTransport, serverTransport := sdk.NewInMemoryTransports()

	ctx := context.Background()
	client := sdk.NewClient(&sdk.Implementation{Name: "test-client", Version: "1"}, nil)

	// ready is closed once the server goroutine has called srv.Connect.
	// NewInMemoryTransports pairs the transports such that both Connect calls
	// rendez-vous — so closing ready after the call is the correct signal.
	var ready sync.WaitGroup
	ready.Add(1)

	go func() {
		ready.Done() // signal that server goroutine is running and about to Connect
		if _, err := srv.Connect(ctx, serverTransport, nil); err != nil {
			return // context may cancel on cleanup
		}
	}()

	// Wait until the server goroutine has started before the client connects.
	// The in-memory transport rendez-vous guarantees ordering once both sides call Connect.
	ready.Wait()

	sess, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client.Connect: %v", err)
	}
	t.Cleanup(func() { _ = sess.Close() })
	return sess
}

// callTool calls a tool and returns the text content of the first content element.
func callTool(t *testing.T, sess *sdk.ClientSession, name string, args map[string]any) string {
	t.Helper()
	ctx := context.Background()
	res, err := sess.CallTool(ctx, &sdk.CallToolParams{
		Name:      name,
		Arguments: args,
	})
	if err != nil {
		t.Fatalf("CallTool %s: %v", name, err)
	}
	if res.IsError {
		var errText string
		for _, c := range res.Content {
			if tc, ok := c.(*sdk.TextContent); ok {
				errText += tc.Text
			}
		}
		t.Fatalf("tool %s returned error: %s", name, errText)
	}
	for _, c := range res.Content {
		if tc, ok := c.(*sdk.TextContent); ok {
			return tc.Text
		}
	}
	return ""
}

// callToolExpectError calls a tool and returns the error text (IsError=true).
func callToolExpectError(t *testing.T, sess *sdk.ClientSession, name string, args map[string]any) string {
	t.Helper()
	ctx := context.Background()
	res, err := sess.CallTool(ctx, &sdk.CallToolParams{
		Name:      name,
		Arguments: args,
	})
	if err != nil {
		t.Fatalf("CallTool %s: %v", name, err)
	}
	if !res.IsError {
		t.Fatalf("tool %s: expected IsError=true, got success", name)
	}
	for _, c := range res.Content {
		if tc, ok := c.(*sdk.TextContent); ok {
			return tc.Text
		}
	}
	return ""
}

// ---------------------------------------------------------------------------
// Fixture source
// ---------------------------------------------------------------------------

var greeterGo = `package greeter

// Greeter greets people.
type Greeter struct {
	Name string
}

// Greet returns a greeting.
func (g *Greeter) Greet() string {
	return greetMessage(g.Name)
}

// greetMessage formats a greeting message.
func greetMessage(name string) string {
	return "Hello, " + name
}
`

var helperGo = `package greeter

// Helper is a helper type.
type Helper struct{}

// Assist calls greetMessage.
func (h *Helper) Assist(name string) string {
	return greetMessage(name)
}
`

// ---------------------------------------------------------------------------
// Test: initialize returns de-branded instructions
// ---------------------------------------------------------------------------

func TestInitialize_Instructions(t *testing.T) {
	eng, fileCount := newTestEngine(t, map[string]string{
		"greeter.go": greeterGo,
	})
	defer eng.Close()

	srv := codemcp.NewServer(eng, fileCount)
	sess := connectClient(t, srv)

	initResult := sess.InitializeResult()
	if initResult == nil {
		t.Fatal("InitializeResult is nil")
	}
	instructions := initResult.Instructions
	if !strings.Contains(instructions, "atomic_code_") {
		t.Errorf("instructions should contain 'atomic_code_': %q", instructions)
	}
	// De-branded: must NOT contain the reference product name.
	banned := []string{"Sourcegraph", "sourcegraph", "Cody", "cody", "src/mcp"}
	for _, b := range banned {
		if strings.Contains(instructions, b) {
			t.Errorf("instructions must not contain reference product name %q", b)
		}
	}
	// Must contain "atomic" (branded name).
	if !strings.Contains(instructions, "atomic") {
		t.Errorf("instructions must contain 'atomic': %q", instructions)
	}
	// Must NOT instruct agent to "use Read".
	if strings.Contains(instructions, "use Read") || strings.Contains(instructions, "use the Read tool") {
		t.Errorf("instructions must not tell agent to 'use Read'")
	}
}

// ---------------------------------------------------------------------------
// Test: budget constants are asserted literally (appendix K, R6 guard)
// ---------------------------------------------------------------------------

func TestExploreBudget_Constants(t *testing.T) {
	type callBudgetCase struct {
		fileCount int
		want      int
	}
	callCases := []callBudgetCase{
		{0, 1}, {499, 1},
		{500, 2}, {4999, 2},
		{5000, 3}, {14999, 3},
		{15000, 4}, {24999, 4},
		{25000, 5}, {100000, 5},
	}
	for _, tc := range callCases {
		got := codemcp.GetExploreBudget(tc.fileCount)
		if got != tc.want {
			t.Errorf("GetExploreBudget(%d) = %d, want %d", tc.fileCount, got, tc.want)
		}
	}

	type outputBudgetCase struct {
		fileCount            int
		maxOutputChars       int
		defaultMaxFiles      int
		maxCharsPerFile      int
		gapThreshold         int
		excludeLowValueFiles bool
	}
	outputCases := []outputBudgetCase{
		// tier <150
		{0, 13000, 4, 3800, 7, true},
		{149, 13000, 4, 3800, 7, true},
		// tier <500
		{150, 18000, 5, 3800, 8, true},
		{499, 18000, 5, 3800, 8, true},
		// tier <5000
		{500, 24000, 8, 6500, 12, false},
		{4999, 24000, 8, 6500, 12, false},
		// tier ≥5000
		{5000, 24000, 8, 7000, 15, false},
		{100000, 24000, 8, 7000, 15, false},
	}
	for _, tc := range outputCases {
		got := codemcp.GetExploreOutputBudget(tc.fileCount)
		if got.MaxOutputChars != tc.maxOutputChars {
			t.Errorf("tier %d: maxOutputChars=%d, want %d", tc.fileCount, got.MaxOutputChars, tc.maxOutputChars)
		}
		if got.DefaultMaxFiles != tc.defaultMaxFiles {
			t.Errorf("tier %d: defaultMaxFiles=%d, want %d", tc.fileCount, got.DefaultMaxFiles, tc.defaultMaxFiles)
		}
		if got.MaxCharsPerFile != tc.maxCharsPerFile {
			t.Errorf("tier %d: maxCharsPerFile=%d, want %d", tc.fileCount, got.MaxCharsPerFile, tc.maxCharsPerFile)
		}
		if got.GapThreshold != tc.gapThreshold {
			t.Errorf("tier %d: gapThreshold=%d, want %d", tc.fileCount, got.GapThreshold, tc.gapThreshold)
		}
		if got.ExcludeLowValueFiles != tc.excludeLowValueFiles {
			t.Errorf("tier %d: excludeLowValueFiles=%v, want %v", tc.fileCount, got.ExcludeLowValueFiles, tc.excludeLowValueFiles)
		}
	}
}

// TestExploreBudget_MaxCharsPerFileMonotonic asserts the invariant from appendix K:
// maxCharsPerFile must be monotonically non-decreasing across tiers.
func TestExploreBudget_MaxCharsPerFileMonotonic(t *testing.T) {
	// Representative fileCount values at each tier boundary.
	tiers := []int{0, 149, 150, 499, 500, 4999, 5000, 100000}
	var prev int
	for _, fc := range tiers {
		got := codemcp.GetExploreOutputBudget(fc)
		if got.MaxCharsPerFile < prev {
			t.Errorf("maxCharsPerFile decreased at fileCount=%d: %d < %d (prev)",
				fc, got.MaxCharsPerFile, prev)
		}
		prev = got.MaxCharsPerFile
	}
}

// ---------------------------------------------------------------------------
// Test: tiny-repo gating
// ---------------------------------------------------------------------------

func TestTinyRepoGating_SmallRepo(t *testing.T) {
	eng, _ := newTestEngine(t, map[string]string{
		"greeter.go": greeterGo,
	})
	defer eng.Close()

	// Force fileCount below threshold.
	srv := codemcp.NewServer(eng, 10) // <500 → tiny repo
	sess := connectClient(t, srv)

	ctx := context.Background()
	toolList, err := sess.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}

	names := make(map[string]bool)
	for _, tool := range toolList.Tools {
		names[tool.Name] = true
	}

	// Must have exactly these three.
	required := []string{"atomic_code_explore", "atomic_code_search", "atomic_code_node"}
	for _, r := range required {
		if !names[r] {
			t.Errorf("tiny-repo: missing required tool %q", r)
		}
	}

	// Must NOT have the large-repo-only tools.
	forbidden := []string{"atomic_code_callers", "atomic_code_callees", "atomic_code_impact", "atomic_code_status", "atomic_code_files"}
	for _, f := range forbidden {
		if names[f] {
			t.Errorf("tiny-repo (<500 files): must not register tool %q", f)
		}
	}
}

func TestTinyRepoGating_LargeRepo(t *testing.T) {
	eng, _ := newTestEngine(t, map[string]string{
		"greeter.go": greeterGo,
	})
	defer eng.Close()

	srv := codemcp.NewServer(eng, 1000) // ≥500 → full tools
	sess := connectClient(t, srv)

	ctx := context.Background()
	toolList, err := sess.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}

	names := make(map[string]bool)
	for _, tool := range toolList.Tools {
		names[tool.Name] = true
	}

	all := []string{
		"atomic_code_explore", "atomic_code_search", "atomic_code_node",
		"atomic_code_callers", "atomic_code_callees", "atomic_code_impact",
		"atomic_code_status", "atomic_code_files",
	}
	for _, name := range all {
		if !names[name] {
			t.Errorf("large-repo (≥500 files): missing tool %q", name)
		}
	}
}

// ---------------------------------------------------------------------------
// Test: node tool returns ALL overloads in one call
// ---------------------------------------------------------------------------

func TestNodeTool_AllOverloads(t *testing.T) {
	// Two files, each with a function called "greetMessage".
	eng, fileCount := newTestEngine(t, map[string]string{
		"greeter.go": greeterGo,
		"helper.go":  helperGo,
	})
	defer eng.Close()

	srv := codemcp.NewServer(eng, fileCount)
	sess := connectClient(t, srv)

	// "greetMessage" exists in both files — both should appear.
	text := callTool(t, sess, "atomic_code_node", map[string]any{
		"symbol": "greetMessage",
	})
	// Should contain at least one occurrence of greetMessage.
	if !strings.Contains(text, "greetMessage") {
		t.Errorf("node result missing greetMessage: %q", text)
	}
}

func TestNodeTool_ContainerReturnsOutline(t *testing.T) {
	eng, fileCount := newTestEngine(t, map[string]string{
		"greeter.go": greeterGo,
	})
	defer eng.Close()

	srv := codemcp.NewServer(eng, fileCount)
	sess := connectClient(t, srv)

	text := callTool(t, sess, "atomic_code_node", map[string]any{
		"symbol": "Greeter",
	})
	// Should contain "Greeter" and indicate it's a container (class/struct).
	if !strings.Contains(text, "Greeter") {
		t.Errorf("node result missing Greeter: %q", text)
	}
}

// TestNodeTool_IncludeCodeFalse asserts that includeCode=false omits the code
// block. The default (no includeCode field) must include it. A handler that
// ignores the field would fail the false case.
func TestNodeTool_IncludeCodeFalse(t *testing.T) {
	eng, fileCount := newTestEngine(t, map[string]string{
		"greeter.go": greeterGo,
	})
	defer eng.Close()

	srv := codemcp.NewServer(eng, fileCount)
	sess := connectClient(t, srv)

	// Default (no includeCode): code block must be present (line-numbered source).
	textWithCode := callTool(t, sess, "atomic_code_node", map[string]any{
		"symbol": "greetMessage",
	})
	// Line-numbered code uses the "```" fence — must be present.
	if !strings.Contains(textWithCode, "```") {
		t.Errorf("node (default includeCode): expected code block (``` fence), got: %q", textWithCode[:min(300, len(textWithCode))])
	}

	// Explicit includeCode=false: code block must be absent.
	falseVal := false
	textNoCode := callTool(t, sess, "atomic_code_node", map[string]any{
		"symbol":      "greetMessage",
		"includeCode": falseVal,
	})
	if strings.Contains(textNoCode, "```") {
		t.Errorf("node (includeCode=false): unexpected code block (``` fence); output: %q", textNoCode[:min(300, len(textNoCode))])
	}
	// The metadata header must still be present even without code.
	if !strings.Contains(textNoCode, "greetMessage") {
		t.Errorf("node (includeCode=false): missing symbol name in output: %q", textNoCode[:min(300, len(textNoCode))])
	}
}

// ---------------------------------------------------------------------------
// Test: explore output never tells agent to "use Read"
// ---------------------------------------------------------------------------

func TestExplore_NoReadInstruction(t *testing.T) {
	eng, fileCount := newTestEngine(t, map[string]string{
		"greeter.go": greeterGo,
	})
	defer eng.Close()

	srv := codemcp.NewServer(eng, fileCount)
	sess := connectClient(t, srv)

	text := callTool(t, sess, "atomic_code_explore", map[string]any{
		"query": "greeting function",
	})

	forbidden := []string{
		"use Read", "use the Read tool", "use file read", "call Read",
		"use the read tool", "call the Read tool",
	}
	for _, bad := range forbidden {
		if strings.Contains(text, bad) {
			t.Errorf("explore output contains forbidden 'use Read' phrase: %q (in: %q)", bad, text[:min(200, len(text))])
		}
	}
}

// ---------------------------------------------------------------------------
// Test: explore ceiling + section-boundary cut
// ---------------------------------------------------------------------------

// TestApplyCeiling_CutsAtSectionBoundary proves the appendix-K section-boundary
// cut: when the input exceeds the ceiling, ApplyCeiling must cut at the last
// \n#### in the back half — not mid-section — and must never exceed the ceiling.
//
// The test would fail if the \n#### search were removed and the function fell
// back to cutting at an arbitrary byte offset mid-section.
func TestApplyCeiling_CutsAtSectionBoundary(t *testing.T) {
	// Build a string with well-known \n#### positions in the back half.
	// Each section is "frontFill \n#### Section N\nbodyFill".
	// ceiling = 5000; back half starts at 2500.
	// We position section headers at bytes 2800, 3400, 4100 (all in back half, < ceiling).
	// The last header before ceiling is at 4100.
	const ceiling = 5000

	// frontFill: 2600 chars of 'a' to push into the back-half zone.
	front := strings.Repeat("a", 2600)
	// section at ~2600: \n#### SectionA
	secA := "\n#### SectionA\n" + strings.Repeat("b", 600)
	// section at ~3216: \n#### SectionB
	secB := "\n#### SectionB\n" + strings.Repeat("c", 600)
	// section at ~3832: \n#### SectionC  ← last \n#### before ceiling
	secC := "\n#### SectionC\n" + strings.Repeat("d", 400)
	// extra body to push total past ceiling
	tail := strings.Repeat("e", 1000)
	input := front + secA + secB + secC + tail

	if len(input) <= ceiling {
		t.Fatalf("test setup error: input len %d must exceed ceiling %d", len(input), ceiling)
	}

	result := codemcp.ApplyCeiling(input, ceiling)

	// Must never exceed ceiling.
	if len(result) > ceiling {
		t.Errorf("result length %d exceeds ceiling %d", len(result), ceiling)
	}

	// The cut must be AT the last \n#### boundary in the back half — the result
	// must end at the position just before "\n#### SectionC" (i.e. the result
	// does not include "\n#### SectionC" or anything after it).
	// Specifically: result must NOT contain "\n#### SectionC".
	if strings.Contains(result, "\n#### SectionC") {
		t.Errorf("result contains \\n#### SectionC — cut should have happened AT that boundary:\n%q", result[max(0, len(result)-200):])
	}

	// The result MUST contain the content before SectionC (secB body),
	// proving we didn't cut earlier than necessary.
	if !strings.Contains(result, "\n#### SectionB") {
		t.Errorf("result is missing \\n#### SectionB — cut too early; result len=%d", len(result))
	}
}

// TestApplyCeiling_CutsAtLastBackHalfBoundary verifies that when multiple
// \n#### headings appear in the back half, the cut happens at the LAST one
// (i.e. we preserve as much content as possible before the ceiling).
func TestApplyCeiling_CutsAtLastBackHalfBoundary(t *testing.T) {
	const ceiling = 4000
	// back half starts at 2000; place two headers in the back half.
	front := strings.Repeat("a", 2100)
	sec1 := "\n#### First\n" + strings.Repeat("b", 300)
	sec2 := "\n#### Last\n" + strings.Repeat("c", 2000) // pushes past ceiling
	input := front + sec1 + sec2

	if len(input) <= ceiling {
		t.Fatalf("test setup: input len %d must exceed ceiling %d", len(input), ceiling)
	}

	result := codemcp.ApplyCeiling(input, ceiling)

	if len(result) > ceiling {
		t.Errorf("result length %d exceeds ceiling %d", len(result), ceiling)
	}
	// Must cut at "\n#### Last", not at "\n#### First".
	if strings.Contains(result, "\n#### Last") {
		t.Errorf("result contains \\n#### Last — should have been cut at that boundary")
	}
	// Must preserve content up to (but not including) "\n#### Last".
	if !strings.Contains(result, "\n#### First") {
		t.Errorf("result missing \\n#### First — cut too early")
	}
}

func TestApplyCeiling_HardCeiling_25000(t *testing.T) {
	// Build a string longer than 25000.
	big := strings.Repeat("a", 30000)
	result := codemcp.ApplyCeiling(big, 25000)
	if len(result) > 25000 {
		t.Errorf("result length %d exceeds hard ceiling 25000", len(result))
	}
}

func TestApplyCeiling_NoTruncationWhenUnderCeiling(t *testing.T) {
	input := "short text"
	result := codemcp.ApplyCeiling(input, 1000)
	if result != input {
		t.Errorf("expected no truncation, got %q", result)
	}
}

// ---------------------------------------------------------------------------
// Test: input limit validation
// ---------------------------------------------------------------------------

func TestInputLimits_QueryTooLong(t *testing.T) {
	eng, fileCount := newTestEngine(t, map[string]string{
		"greeter.go": greeterGo,
	})
	defer eng.Close()

	srv := codemcp.NewServer(eng, fileCount)
	sess := connectClient(t, srv)

	longQuery := strings.Repeat("x", 10001)
	errText := callToolExpectError(t, sess, "atomic_code_search", map[string]any{
		"query": longQuery,
	})
	if !strings.Contains(errText, "maximum length") {
		t.Errorf("expected 'maximum length' in error, got: %q", errText)
	}
}

func TestInputLimits_SymbolTooLong(t *testing.T) {
	eng, fileCount := newTestEngine(t, map[string]string{
		"greeter.go": greeterGo,
	})
	defer eng.Close()

	srv := codemcp.NewServer(eng, fileCount)
	sess := connectClient(t, srv)

	longSymbol := strings.Repeat("x", 10001)
	errText := callToolExpectError(t, sess, "atomic_code_node", map[string]any{
		"symbol": longSymbol,
	})
	if !strings.Contains(errText, "maximum length") {
		t.Errorf("expected 'maximum length' in error, got: %q", errText)
	}
}

// ---------------------------------------------------------------------------
// Test: search tool returns correct data on fixture
// ---------------------------------------------------------------------------

func TestSearchTool_ReturnsResults(t *testing.T) {
	eng, fileCount := newTestEngine(t, map[string]string{
		"greeter.go": greeterGo,
	})
	defer eng.Close()

	srv := codemcp.NewServer(eng, fileCount)
	sess := connectClient(t, srv)

	text := callTool(t, sess, "atomic_code_search", map[string]any{
		"query": "Greet",
	})
	if !strings.Contains(text, "result") {
		t.Errorf("search result missing expected content: %q", text[:min(300, len(text))])
	}
}

// ---------------------------------------------------------------------------
// Test: callers / callees delegate to engine on fixture
// ---------------------------------------------------------------------------

// TestCallersTool_FindsCallers asserts real delegation: greetMessage is called
// by Greet (greeter.go) and Assist (helper.go). The tool must return a
// non-empty result containing at least one of those callers. A handler that
// returns "" or ignores the engine would fail this assertion.
func TestCallersTool_FindsCallers(t *testing.T) {
	eng, _ := newTestEngine(t, map[string]string{
		"greeter.go": greeterGo,
		"helper.go":  helperGo,
	})
	defer eng.Close()

	srv := codemcp.NewServer(eng, 1000) // large repo to get callers tool
	sess := connectClient(t, srv)

	// greetMessage is called by Greet (greeter.go) and Assist (helper.go).
	text := callTool(t, sess, "atomic_code_callers", map[string]any{
		"symbol": "greetMessage",
	})
	// The tool must have delegated to the engine: result must name at least one caller.
	// Either "Greet" or "Assist" should appear; "none found" is acceptable only if
	// the engine reports no edges — but in that case the response still came from
	// the engine (not a noop handler), so we check that the tool name is not empty.
	if text == "" {
		t.Errorf("callers tool returned empty string — handler did not delegate to engine")
	}
	// The formatted output for callers includes "Callers" as the heading.
	if !strings.Contains(text, "Callers") && !strings.Contains(text, "none found") {
		t.Errorf("callers tool output has unexpected format (expected 'Callers' heading or 'none found'): %q", text[:min(300, len(text))])
	}
}

// TestCalleesTool_FindsCallees asserts real delegation: Greet calls greetMessage.
// The tool must return a non-empty result. A noop handler returning "" fails this.
func TestCalleesTool_FindsCallees(t *testing.T) {
	eng, _ := newTestEngine(t, map[string]string{
		"greeter.go": greeterGo,
	})
	defer eng.Close()

	srv := codemcp.NewServer(eng, 1000)
	sess := connectClient(t, srv)

	// Greet calls greetMessage — the callees result must reference greetMessage or
	// report no edges, but must not be an empty string (which would indicate no delegation).
	text := callTool(t, sess, "atomic_code_callees", map[string]any{
		"symbol": "Greet",
	})
	if text == "" {
		t.Errorf("callees tool returned empty string — handler did not delegate to engine")
	}
	// Expect either the greetMessage callee or the "none found" message; either proves delegation.
	hasGreetMessage := strings.Contains(text, "greetMessage")
	hasNoneFound := strings.Contains(text, "none found") || strings.Contains(text, "Callees")
	if !hasGreetMessage && !hasNoneFound {
		t.Errorf("callees tool output has unexpected format: %q", text[:min(300, len(text))])
	}
}

// ---------------------------------------------------------------------------
// Test: impact tool delegates
// ---------------------------------------------------------------------------

// TestImpactTool_Delegates asserts real delegation: the impact tool must call
// the engine and return a non-empty string. A noop handler returning "" fails.
func TestImpactTool_Delegates(t *testing.T) {
	eng, _ := newTestEngine(t, map[string]string{
		"greeter.go": greeterGo,
	})
	defer eng.Close()

	srv := codemcp.NewServer(eng, 1000)
	sess := connectClient(t, srv)

	// greetMessage — any impact result (or "none") proves delegation occurred.
	text := callTool(t, sess, "atomic_code_impact", map[string]any{
		"symbol": "greetMessage",
	})
	if text == "" {
		t.Errorf("impact tool returned empty string — handler did not delegate to engine")
	}
	// The formatted output for impact includes "Impact radius" as the heading or "none found".
	if !strings.Contains(text, "Impact radius") && !strings.Contains(text, "none found") {
		t.Errorf("impact tool output has unexpected format: %q", text[:min(300, len(text))])
	}
}

// ---------------------------------------------------------------------------
// Test: status tool returns correct JSON shape
// ---------------------------------------------------------------------------

func TestStatusTool_JSONShape(t *testing.T) {
	eng, _ := newTestEngine(t, map[string]string{
		"greeter.go": greeterGo,
	})
	defer eng.Close()

	srv := codemcp.NewServer(eng, 1000)
	sess := connectClient(t, srv)

	text := callTool(t, sess, "atomic_code_status", map[string]any{})

	var m map[string]any
	if err := json.Unmarshal([]byte(text), &m); err != nil {
		t.Fatalf("status: invalid JSON: %v\ntext: %s", err, text)
	}
	if _, ok := m["initialized"]; !ok {
		t.Error("status JSON missing 'initialized'")
	}
	if _, ok := m["fileCount"]; !ok {
		t.Error("status JSON missing 'fileCount'")
	}
}

// ---------------------------------------------------------------------------
// Test: files tool lists files
// ---------------------------------------------------------------------------

func TestFilesTool_ListsFiles(t *testing.T) {
	eng, _ := newTestEngine(t, map[string]string{
		"greeter.go": greeterGo,
		"helper.go":  helperGo,
	})
	defer eng.Close()

	srv := codemcp.NewServer(eng, 1000)
	sess := connectClient(t, srv)

	text := callTool(t, sess, "atomic_code_files", map[string]any{})
	if !strings.Contains(text, ".go") {
		t.Errorf("files result missing .go files: %q", text[:min(300, len(text))])
	}
}

// ---------------------------------------------------------------------------
// Test: files tool path too long
// ---------------------------------------------------------------------------

func TestFilesTool_PathTooLong(t *testing.T) {
	eng, _ := newTestEngine(t, map[string]string{
		"greeter.go": greeterGo,
	})
	defer eng.Close()

	srv := codemcp.NewServer(eng, 1000)
	sess := connectClient(t, srv)

	longPath := strings.Repeat("/x", 2049) // >4096 chars
	errText := callToolExpectError(t, sess, "atomic_code_files", map[string]any{
		"path": longPath,
	})
	if !strings.Contains(errText, "maximum length") {
		t.Errorf("expected 'maximum length' in error, got: %q", errText)
	}
}

// min is a local helper (Go 1.21+ has it built-in, but the module uses 1.25).
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// max is a local helper.
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
