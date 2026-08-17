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

// The paired transports rendez-vous on Connect, so a WaitGroup orders the two
// sides without a sleep.
func connectClient(t *testing.T, srv *sdk.Server) *sdk.ClientSession {
	t.Helper()
	clientTransport, serverTransport := sdk.NewInMemoryTransports()

	ctx := context.Background()
	client := sdk.NewClient(&sdk.Implementation{Name: "test-client", Version: "1"}, nil)

	var ready sync.WaitGroup
	ready.Add(1)

	go func() {
		ready.Done() // signal that server goroutine is running and about to Connect
		if _, err := srv.Connect(ctx, serverTransport, nil); err != nil {
			return // context may cancel on cleanup
		}
	}()

	ready.Wait()

	sess, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client.Connect: %v", err)
	}
	t.Cleanup(func() { _ = sess.Close() })
	return sess
}

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
	banned := []string{"Sourcegraph", "sourcegraph", "Cody", "cody", "src/mcp"}
	for _, b := range banned {
		if strings.Contains(instructions, b) {
			t.Errorf("instructions must not contain reference product name %q", b)
		}
	}
	if !strings.Contains(instructions, "atomic") {
		t.Errorf("instructions must contain 'atomic': %q", instructions)
	}
	if strings.Contains(instructions, "use Read") || strings.Contains(instructions, "use the Read tool") {
		t.Errorf("instructions must not tell agent to 'use Read'")
	}
}

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
		{0, 13000, 4, 3800, 7, true},
		{149, 13000, 4, 3800, 7, true},
		{150, 18000, 5, 3800, 8, true},
		{499, 18000, 5, 3800, 8, true},
		{500, 24000, 8, 6500, 12, false},
		{4999, 24000, 8, 6500, 12, false},
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

func TestExploreBudget_MaxCharsPerFileMonotonic(t *testing.T) {
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

func TestTinyRepoGating_SmallRepo(t *testing.T) {
	eng, _ := newTestEngine(t, map[string]string{
		"greeter.go": greeterGo,
	})
	defer eng.Close()

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

	required := []string{"atomic_code_explore", "atomic_code_search", "atomic_code_node"}
	for _, r := range required {
		if !names[r] {
			t.Errorf("tiny-repo: missing required tool %q", r)
		}
	}

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

func TestNodeTool_AllOverloads(t *testing.T) {
	eng, fileCount := newTestEngine(t, map[string]string{
		"greeter.go": greeterGo,
		"helper.go":  helperGo,
	})
	defer eng.Close()

	srv := codemcp.NewServer(eng, fileCount)
	sess := connectClient(t, srv)

	text := callTool(t, sess, "atomic_code_node", map[string]any{
		"symbol": "greetMessage",
	})
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
	if !strings.Contains(text, "Greeter") {
		t.Errorf("node result missing Greeter: %q", text)
	}
}

func TestNodeTool_IncludeCodeFalse(t *testing.T) {
	eng, fileCount := newTestEngine(t, map[string]string{
		"greeter.go": greeterGo,
	})
	defer eng.Close()

	srv := codemcp.NewServer(eng, fileCount)
	sess := connectClient(t, srv)

	textWithCode := callTool(t, sess, "atomic_code_node", map[string]any{
		"symbol": "greetMessage",
	})
	if !strings.Contains(textWithCode, "```") {
		t.Errorf("node (default includeCode): expected code block (``` fence), got: %q", textWithCode[:min(300, len(textWithCode))])
	}

	falseVal := false
	textNoCode := callTool(t, sess, "atomic_code_node", map[string]any{
		"symbol":      "greetMessage",
		"includeCode": falseVal,
	})
	if strings.Contains(textNoCode, "```") {
		t.Errorf("node (includeCode=false): unexpected code block (``` fence); output: %q", textNoCode[:min(300, len(textNoCode))])
	}
	if !strings.Contains(textNoCode, "greetMessage") {
		t.Errorf("node (includeCode=false): missing symbol name in output: %q", textNoCode[:min(300, len(textNoCode))])
	}
}

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

// Over-length input must cut at the last section boundary in the back half,
// never mid-section and never past the ceiling.
func TestApplyCeiling_CutsAtSectionBoundary(t *testing.T) {
	// Headers are placed at known offsets in the back half so the expected cut
	// point is unambiguous.
	const ceiling = 5000

	front := strings.Repeat("a", 2600)
	secA := "\n#### SectionA\n" + strings.Repeat("b", 600)
	secB := "\n#### SectionB\n" + strings.Repeat("c", 600)
	secC := "\n#### SectionC\n" + strings.Repeat("d", 400)
	tail := strings.Repeat("e", 1000)
	input := front + secA + secB + secC + tail

	if len(input) <= ceiling {
		t.Fatalf("test setup error: input len %d must exceed ceiling %d", len(input), ceiling)
	}

	result := codemcp.ApplyCeiling(input, ceiling)

	if len(result) > ceiling {
		t.Errorf("result length %d exceeds ceiling %d", len(result), ceiling)
	}

	if strings.Contains(result, "\n#### SectionC") {
		t.Errorf("result contains \\n#### SectionC — cut should have happened AT that boundary:\n%q", result[max(0, len(result)-200):])
	}

	// Proves the cut was not earlier than necessary.
	if !strings.Contains(result, "\n#### SectionB") {
		t.Errorf("result is missing \\n#### SectionB — cut too early; result len=%d", len(result))
	}
}

func TestApplyCeiling_CutsAtLastBackHalfBoundary(t *testing.T) {
	const ceiling = 4000
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
	if strings.Contains(result, "\n#### Last") {
		t.Errorf("result contains \\n#### Last — should have been cut at that boundary")
	}
	if !strings.Contains(result, "\n#### First") {
		t.Errorf("result missing \\n#### First — cut too early")
	}
}

func TestApplyCeiling_HardCeiling_25000(t *testing.T) {
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

func TestCallersTool_FindsCallers(t *testing.T) {
	eng, _ := newTestEngine(t, map[string]string{
		"greeter.go": greeterGo,
		"helper.go":  helperGo,
	})
	defer eng.Close()

	srv := codemcp.NewServer(eng, 1000) // large repo to get callers tool
	sess := connectClient(t, srv)

	text := callTool(t, sess, "atomic_code_callers", map[string]any{
		"symbol": "greetMessage",
	})
	// Either a named caller or "none found" proves the tool reached the engine
	// rather than returning from a stub.
	if text == "" {
		t.Errorf("callers tool returned empty string — handler did not delegate to engine")
	}
	if !strings.Contains(text, "Callers") && !strings.Contains(text, "none found") {
		t.Errorf("callers tool output has unexpected format (expected 'Callers' heading or 'none found'): %q", text[:min(300, len(text))])
	}
}

func TestCalleesTool_FindsCallees(t *testing.T) {
	eng, _ := newTestEngine(t, map[string]string{
		"greeter.go": greeterGo,
	})
	defer eng.Close()

	srv := codemcp.NewServer(eng, 1000)
	sess := connectClient(t, srv)

	text := callTool(t, sess, "atomic_code_callees", map[string]any{
		"symbol": "Greet",
	})
	if text == "" {
		t.Errorf("callees tool returned empty string — handler did not delegate to engine")
	}
	// Either outcome proves delegation; an empty string would not.
	hasGreetMessage := strings.Contains(text, "greetMessage")
	hasNoneFound := strings.Contains(text, "none found") || strings.Contains(text, "Callees")
	if !hasGreetMessage && !hasNoneFound {
		t.Errorf("callees tool output has unexpected format: %q", text[:min(300, len(text))])
	}
}

func TestImpactTool_Delegates(t *testing.T) {
	eng, _ := newTestEngine(t, map[string]string{
		"greeter.go": greeterGo,
	})
	defer eng.Close()

	srv := codemcp.NewServer(eng, 1000)
	sess := connectClient(t, srv)

	text := callTool(t, sess, "atomic_code_impact", map[string]any{
		"symbol": "greetMessage",
	})
	if text == "" {
		t.Errorf("impact tool returned empty string — handler did not delegate to engine")
	}
	if !strings.Contains(text, "Impact radius") && !strings.Contains(text, "none found") {
		t.Errorf("impact tool output has unexpected format: %q", text[:min(300, len(text))])
	}
}

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

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
