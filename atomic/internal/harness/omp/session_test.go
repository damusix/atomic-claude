package omp

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/artifacts"
	"github.com/damusix/atomic-claude/atomic/internal/harness"
	"github.com/damusix/atomic-claude/atomic/internal/managedfile"
	"github.com/damusix/atomic-claude/atomic/internal/rules"
)

// runtimeRules is a synthetic projected rule set: one toolchain rule, one
// documentation rule, and a second rule that overlaps the first, so overlap,
// precedence, and nonmatch all have a case.
func runtimeRules(t *testing.T) []harness.RuleSource {
	t.Helper()
	return []harness.RuleSource{
		runtimeRule(t, "rules/all/style.md", []string{"**/*.ts"}, "# All TypeScript\n"),
		runtimeRule(t, "rules/docs/spec.md", []string{"docs/spec/**/*.md"}, "# Specs\n"),
		runtimeRule(t, "rules/ts/style.md", []string{"**/*.{ts,tsx}"}, "# TypeScript\n"),
	}
}

func runtimeRule(t *testing.T, source string, include []string, body string) harness.RuleSource {
	t.Helper()
	bytes := []byte(body)
	record := rules.RuleRecord{
		ID:           string(rules.ProducerShipped) + ":" + source,
		Class:        rules.ClassShipped,
		Producer:     rules.ProducerShipped,
		Source:       source,
		BaseKind:     rules.BaseRepositoryRoot,
		Include:      include,
		Body:         bytes,
		SourceDigest: managedfile.Digest(bytes),
	}
	return harness.RuleSource{Record: record, Bytes: bytes}
}

// TestBuildSessionDeliveryWiresOnlyProvenEvents pins the event set to the CP0
// roles: the session-baseline event supplies the bounded index, the
// pre-operation event matches proven inputs, and the deny result carries exact
// predicates. A record without those rows wires nothing and reports the gap.
func TestBuildSessionDeliveryWiresOnlyProvenEvents(t *testing.T) {
	delivery, err := BuildSessionDelivery(runtimeRules(t), harness.OMPCapabilities(), nil)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	wired := map[string]string{}
	for _, event := range delivery.Events {
		wired[event.Event+"|"+event.Delivery] = string(event.Role)
	}
	for _, want := range []string{
		"session_start|observation only",
		"before_agent_start|bounded session-baseline rule index (bound 8192 bytes)",
		"tool_call|exact-path matching for proven structured inputs; uncovered tools recorded",
		"tool_call|exact predicate deny",
	} {
		if _, ok := wired[want]; !ok {
			t.Errorf("event %q is not wired; wired: %v", want, wired)
		}
	}
	if len(delivery.Index) != 3 {
		t.Fatalf("index has %d entries, want 3", len(delivery.Index))
	}
	if ids := indexIDs(delivery); strings.Join(ids, ",") != "shipped:rules/all/style.md,shipped:rules/docs/spec.md,shipped:rules/ts/style.md" {
		t.Errorf("index order = %v, want canonical identity order", ids)
	}
	entry := delivery.Index[0]
	if entry.SourceDigest != managedfile.Digest([]byte("# All TypeScript\n")) {
		t.Errorf("index entry digest %q does not carry the source digest", entry.SourceDigest)
	}
	if entry.Patterns[0].Regex != `^(?:[^/]+/)*[^/]*\.ts$` {
		t.Errorf("index pattern regex = %q", entry.Patterns[0].Regex)
	}
	if delivery.Suppressed {
		t.Errorf("a three-rule index was reported suppressed: %s", delivery.IndexText)
	}
	if !strings.Contains(delivery.IndexText, RuntimeMarker) {
		t.Errorf("baseline index does not carry the runtime marker: %s", delivery.IndexText)
	}
}

// TestSessionDeliveryGolden pins the runtime delivery plan: the wired events, the
// indexed rules with their digests and tiers, the proven tool coverage, the
// duplication contract with its CP0 claim IDs, and every unproven surface. A
// change to what Atomic claims about OMP runtime delivery has to change here.
func TestSessionDeliveryGolden(t *testing.T) {
	delivery, err := BuildSessionDelivery(runtimeRules(t), harness.OMPCapabilities(), nil)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "marker %s bound %d index-bytes %d suppressed %v\n", RuntimeMarker, delivery.Bound, delivery.IndexBytes, delivery.Suppressed)
	for _, event := range delivery.Events {
		fmt.Fprintf(&b, "event %s %s observation=%v %s\n", event.Event, event.Role, event.Observation, event.Delivery)
	}
	for _, entry := range delivery.Index {
		globs := make([]string, 0, len(entry.Patterns))
		for _, pattern := range entry.Patterns {
			globs = append(globs, pattern.Glob+" => "+pattern.Regex)
		}
		fmt.Fprintf(&b, "index %s %s %s %s\n", entry.RecordID, entry.SourceDigest, entry.Tier, strings.Join(globs, " "))
	}
	for _, tool := range delivery.Tools {
		fmt.Fprintf(&b, "tool %s %s\n", tool.Tool, tool.PathField)
	}
	for _, gap := range delivery.Gaps {
		fmt.Fprintf(&b, "gap %s %s\n", gap.Role, gap.Status)
	}
	for _, gap := range delivery.Unproven {
		fmt.Fprintf(&b, "unproven %s\n", gap.Surface)
	}
	fmt.Fprintf(&b, "dedupe factory %s\n", delivery.Dedupe.Factory)
	fmt.Fprintf(&b, "dedupe injection %s\n", delivery.Dedupe.Injection)
	fmt.Fprintf(&b, "dedupe resume %s\n", delivery.Dedupe.Resume)
	fmt.Fprintf(&b, "dedupe child %s\n", delivery.Dedupe.Child)
	fmt.Fprintf(&b, "dedupe lifetime %s\n", delivery.Dedupe.Lifetime)
	checkGolden(t, "runtime-delivery.txt", []byte(b.String()))
}

// TestBuildSessionDeliveryMissingRowsStayUnsupported proves a version whose
// capability record proves nothing wires no delivery event and reports the gap
// instead of registering a handler it cannot justify.
func TestBuildSessionDeliveryMissingRowsStayUnsupported(t *testing.T) {
	rowless := harness.CapabilityMatrix{Harness: harness.KindOMP, Version: "unproven"}
	delivery, err := BuildSessionDelivery(runtimeRules(t), rowless, nil)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	for _, event := range delivery.Events {
		if !event.Observation {
			t.Errorf("event %q was wired without a supported capability row", event.Event)
		}
	}
	if len(delivery.Gaps) != 4 {
		t.Errorf("gaps = %v, want every rule-delivery role reported", delivery.Gaps)
	}
	if len(delivery.Index) != 3 {
		t.Errorf("the index still carries the projected rules, so the rules ship while the delivery reports unsupported")
	}
}

// TestBuildSessionDeliveryRefusesUnprovenDenyForms proves only the exercised
// predicate form is available: any other tool, an empty command, or a missing
// reason refuses the delivery rather than shipping a broader claim.
func TestBuildSessionDeliveryRefusesUnprovenDenyForms(t *testing.T) {
	for name, predicate := range map[string]DenyPredicate{
		"other-tool":     {Tool: "read", Command: "x", Reason: "r"},
		"empty-command":  {Tool: "bash", Reason: "r"},
		"missing-reason": {Tool: "bash", Command: "x"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := BuildSessionDelivery(runtimeRules(t), harness.OMPCapabilities(), []DenyPredicate{predicate}); err == nil {
				t.Fatalf("delivery accepted the unproven predicate form %+v", predicate)
			}
		})
	}
	delivery, err := BuildSessionDelivery(runtimeRules(t), harness.OMPCapabilities(), []DenyPredicate{{Tool: "bash", Command: "touch x", Reason: "blocked"}})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if len(delivery.Deny) != 1 || delivery.Deny[0].Evidence != DenyEvidence {
		t.Errorf("exact predicate = %+v, want the CP0 deny evidence attached", delivery.Deny)
	}
}

// TestBuildSessionDeliveryReportsUndeliverableRule proves a rule whose patterns
// leave the translated dialect never enters the runtime index: an approximated
// pattern would match paths the canonical matcher does not.
func TestBuildSessionDeliveryReportsUndeliverableRule(t *testing.T) {
	sources := append(runtimeRules(t), runtimeRule(t, "rules/odd/style.md", []string{`a/\d.ts`}, "# Odd\n"))
	delivery, err := BuildSessionDelivery(sources, harness.OMPCapabilities(), nil)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if len(delivery.Undeliverable) != 1 || delivery.Undeliverable[0].RecordID != "shipped:rules/odd/style.md" {
		t.Fatalf("undeliverable = %+v, want the out-of-dialect rule reported", delivery.Undeliverable)
	}
	if len(delivery.Index) != 3 {
		t.Errorf("index has %d entries; an out-of-dialect rule must not enter it", len(delivery.Index))
	}
}

// TestBuildSessionDeliverySuppressesAnOverBoundIndex proves the bound is a
// refusal, not a truncation: an index larger than the bound is reported
// suppressed so no session silently receives a partial rule list.
func TestBuildSessionDeliverySuppressesAnOverBoundIndex(t *testing.T) {
	var sources []harness.RuleSource
	for i := 0; i < 120; i++ {
		source := "rules/generated/rule" + strings.Repeat("0", 1+len(string(rune('a'+i%26)))) + string(rune('a'+i%26)) + string(rune('a'+i/26)) + "/style.md"
		sources = append(sources, runtimeRule(t, source, []string{"**/*.ts"}, "# generated "+source+"\n"))
	}
	delivery, err := BuildSessionDelivery(sources, harness.OMPCapabilities(), nil)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if !delivery.Suppressed {
		t.Fatalf("index of %d bytes was not reported suppressed against the %d byte bound", delivery.IndexBytes, delivery.Bound)
	}
	module, err := delivery.RenderExtension()
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(string(module), `"bound": 8192`) {
		t.Error("the rendered module does not carry the bound, so the runtime cannot refuse the block")
	}
}

// TestRenderExtensionIsDeterministicAndCarriesOnlyWiredEvents pins the generated
// module: identical deliveries produce identical bytes, and the module registers
// the CP0-selected events and no others.
func TestRenderExtensionIsDeterministicAndCarriesOnlyWiredEvents(t *testing.T) {
	first, err := BuildSessionDelivery(runtimeRules(t), harness.OMPCapabilities(), nil)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	second, err := BuildSessionDelivery(runtimeRules(t), harness.OMPCapabilities(), nil)
	if err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	firstModule, err := first.RenderExtension()
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	secondModule, err := second.RenderExtension()
	if err != nil {
		t.Fatalf("rerender: %v", err)
	}
	if string(firstModule) != string(secondModule) {
		t.Error("rendering the same delivery twice produced different bytes")
	}
	for _, want := range []string{
		`import { appendFileSync } from "node:fs"`,
		`pi.on("session_start"`,
		`pi.on("before_agent_start"`,
		`pi.on("tool_call"`,
		`pi.on("before_provider_request"`,
		`pi.on("session_shutdown"`,
		`"id": "shipped:rules/ts/style.md"`,
	} {
		if !strings.Contains(string(firstModule), want) {
			t.Errorf("rendered module is missing %q", want)
		}
	}
	for _, forbidden := range []string{"tool_result", `pi.on("context"`, "session_before_switch", "ExtensionAPI } from"} {
		if strings.Contains(string(firstModule), forbidden) {
			t.Errorf("rendered module registers the unselected surface %q", forbidden)
		}
	}
	if strings.Contains(string(firstModule), extensionDataPlaceholder) {
		t.Error("rendered module still carries the payload placeholder")
	}
	// A delivery with no deny predicate still ships an empty array: the module
	// evaluates the list on every tool call, so null would be a runtime error.
	empty, err := BuildSessionDelivery(runtimeRules(t), harness.OMPCapabilities(), nil)
	if err != nil {
		t.Fatalf("build without predicates: %v", err)
	}
	emptyModule, err := empty.RenderExtension()
	if err != nil {
		t.Fatalf("render without predicates: %v", err)
	}
	if strings.Contains(string(emptyModule), ": null") {
		t.Errorf("rendered module carries a null payload field:\n%s", emptyModule)
	}
	if !strings.Contains(string(emptyModule), `"deny": []`) {
		t.Error("rendered module does not carry an empty deny list")
	}
}

// TestRenderExtensionRegistersOnlyPlannedEvents pins the module's registered set
// to the delivery plan: a capability record that proves no delivering role
// renders a module with zero gated events, and a full record registers exactly
// the events the delivery names. The unproven template this replaces registered
// every handler unconditionally, so the artifact contradicted the plan.
func TestRenderExtensionRegistersOnlyPlannedEvents(t *testing.T) {
	rowless := harness.CapabilityMatrix{Harness: harness.KindOMP, Version: "unproven"}
	unproven, err := BuildSessionDelivery(runtimeRules(t), rowless, nil)
	if err != nil {
		t.Fatalf("build rowless delivery: %v", err)
	}
	module, err := unproven.RenderExtension()
	if err != nil {
		t.Fatalf("render rowless module: %v", err)
	}
	if got := sortedJoin(RegisteredEvents(module)); got != "before_provider_request,session_shutdown,session_start" {
		t.Errorf("rowless module registers %s, want the observation events only", got)
	}
	for _, gated := range []string{"before_agent_start", "tool_call"} {
		if strings.Contains(string(module), `pi.on("`+gated+`"`) {
			t.Errorf("a rowless delivery registered the gated event %q", gated)
		}
	}

	proven, err := BuildSessionDelivery(runtimeRules(t), harness.OMPCapabilities(), nil)
	if err != nil {
		t.Fatalf("build proven delivery: %v", err)
	}
	provenModule, err := proven.RenderExtension()
	if err != nil {
		t.Fatalf("render proven module: %v", err)
	}
	if want, got := sortedJoin(proven.WiredEvents()), sortedJoin(RegisteredEvents(provenModule)); want != got {
		t.Errorf("proven module registers %s, but the delivery plans %s", got, want)
	}
	if !strings.Contains(string(provenModule), `"events": [`) {
		t.Error("the rendered module does not carry the wired event set in its payload")
	}
}

// sortedJoin renders a string list as a sorted, comma-joined key, so two sets of
// event names compare without depending on order.
func sortedJoin(values []string) string {
	sorted := append([]string(nil), values...)
	sort.Strings(sorted)
	return strings.Join(sorted, ",")
}

// TestReadRuntimeLogReportsObservedFacts parses the record stream the generated
// module writes and reports it as evidence, never as the plan's expectations.
func TestReadRuntimeLogReportsObservedFacts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.jsonl")
	writeFile(t, path, strings.Join([]string{
		`{"kind":"factory","pid":1}`,
		`{"kind":"session_start","cwd":"/repo","tools":["read","bash"],"covered":["read"],"uncovered":["bash"],"index":["shipped:rules/ts/style.md"]}`,
		`{"kind":"baseline","deliveries":1,"bytes":200,"bound":8192,"replaced":0}`,
		`{"kind":"provider_request","hasBaseline":true}`,
		`{"kind":"operation","tool":"read","candidate":"src/a.ts","matched":["shipped:rules/all/style.md","shipped:rules/ts/style.md"]}`,
		`{"kind":"uncovered_tool","tool":"bash","reason":"no CP0-proven structured input path"}`,
		`{"kind":"uncovered_operation","tool":"read","reason":"the proven field carried no usable exact path"}`,
		`{"kind":"deny","tool":"bash","reason":"ATOMIC_DENY_MARKER"}`,
		`{"kind":"baseline","deliveries":2,"bytes":220,"bound":8192,"replaced":1}`,
	}, "\n")+"\n")

	log, err := ReadRuntimeLog(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if log.Factories != 1 || log.Sessions != 1 || log.BaselineDeliveries != 2 {
		t.Errorf("counts = %+v", log)
	}
	if !log.BaselineConsumed {
		t.Error("a delivered block reaching the provider payload was not reported")
	}
	if got := strings.Join(log.Matched, ","); got != "shipped:rules/all/style.md,shipped:rules/ts/style.md" {
		t.Errorf("matched = %s", got)
	}
	if got := strings.Join(log.UncoveredTools, ","); got != "bash" {
		t.Errorf("uncovered tools = %s", got)
	}
	if log.Operations != 1 || log.Denials != 1 || log.UncoveredOperations != 1 {
		t.Errorf("operations/denials/uncovered = %d/%d/%d", log.Operations, log.Denials, log.UncoveredOperations)
	}

	missing, err := ReadRuntimeLog(filepath.Join(t.TempDir(), "absent.jsonl"))
	if err != nil || missing.Sessions != 0 {
		t.Errorf("a missing log = %+v, %v; want an empty log and no error", missing, err)
	}
}

// TestRuntimeStatusIsReadOnlyAndReportsTheDelivery is the doctor seam: the
// status carries the delivery plan plus the last runtime proof, and it writes
// nothing.
func TestRuntimeStatusIsReadOnlyAndReportsTheDelivery(t *testing.T) {
	home := newHome(t)
	before := treeEntries(t, home)
	a := newAdapter(t, map[string]string{"": filepath.Join(home, "agent")})
	proofPath := filepath.Join(t.TempDir(), "runtime.jsonl")
	writeFile(t, proofPath, `{"kind":"session_start","tools":["read"],"covered":["read"],"index":["shipped:rules/ts/style.md"]}`+"\n"+
		`{"kind":"operation","tool":"read","candidate":"src/a.ts","matched":["shipped:rules/ts/style.md"]}`+"\n")
	log, err := ReadRuntimeLog(proofPath)
	if err != nil {
		t.Fatalf("read proof: %v", err)
	}
	status, err := a.RuntimeStatus(runtimeRules(t), nil, &RuntimeProof{
		Scenario: "omp-primary",
		Harness:  "omp/18.1.18",
		Observed: "2026-09-19",
		Events:   []string{"factory", "session_start", "baseline", "provider_request"},
		Log:      &log,
	})
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if status.Proof == nil || status.Proof.Scenario != "omp-primary" || status.Proof.Log.Matched[0] != "shipped:rules/ts/style.md" {
		t.Errorf("status proof = %+v", status.Proof)
	}
	if status.Delivery.Bound != BaselineByteBound || len(status.Delivery.Tools) != 1 || status.Delivery.Tools[0].Tool != "read" {
		t.Errorf("status delivery = %+v", status.Delivery)
	}
	if got := treeEntries(t, home); got != before {
		t.Errorf("runtime status wrote to the home: %v -> %v", before, got)
	}
}

func indexIDs(delivery SessionDelivery) []string {
	ids := make([]string, 0, len(delivery.Index))
	for _, entry := range delivery.Index {
		ids = append(ids, entry.RecordID)
	}
	return ids
}

// treeEntries names every path under root, so a read-only claim can be checked.
func treeEntries(t *testing.T, root string) string {
	t.Helper()
	var names []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		names = append(names, path)
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	return strings.Join(names, "\n")
}

// The generated module is the artifact that runs inside OMP, so its behavior is
// proved by driving it under the JavaScript runtime the harness uses. Every
// CP0-mapped claim lands here: match, nonmatch, overlap, canonical precedence,
// lifetime across turns, idempotent replacement, exact deny with a near miss,
// uncovered tools, and bound suppression.
const extensionDriver = `import { readFileSync, writeFileSync } from "node:fs";
import atomic from "./atomic.ts";

const spec = JSON.parse(readFileSync(process.argv[2], "utf8"));
const handlers: Record<string, (event: unknown, ctx?: unknown) => unknown> = {};
const pi = {
	on(event: string, handler: (event: unknown, ctx?: unknown) => unknown) {
		handlers[event] = handler;
	},
	getAllTools: () => spec.tools.map((name: string) => ({ name })),
};
atomic(pi as unknown as Parameters<typeof atomic>[0]);
const results: unknown[] = [];
for (const step of spec.steps) {
	const handler = handlers[step.event];
	results.push(handler ? await handler(step.payload, step.ctx) : { missing: step.event });
}
writeFileSync(process.argv[3], JSON.stringify(results, null, 2));
`

func TestExtensionModuleDrivesASyntheticSession(t *testing.T) {
	bun, err := exec.LookPath("bun")
	if err != nil {
		t.Skipf("bun is unavailable: %v", err)
	}
	dir := t.TempDir()
	delivery, err := BuildSessionDelivery(runtimeRules(t), harness.OMPCapabilities(), []DenyPredicate{{
		Tool:    "bash",
		Command: "touch /tmp/atomic-denied # BLOCK_ME",
		Reason:  "ATOMIC_DENY_MARKER",
	}})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	module, err := delivery.RenderExtension()
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	writeFile(t, filepath.Join(dir, "atomic.ts"), string(module))
	writeFile(t, filepath.Join(dir, "driver.ts"), extensionDriver)

	spec := map[string]any{
		"tools": []string{"read", "bash", "write"},
		"steps": []map[string]any{
			{"event": "session_start", "payload": map[string]any{"type": "session_start"}, "ctx": map[string]any{"cwd": "/repo"}},
			{"event": "before_agent_start", "payload": map[string]any{"prompt": "p", "systemPrompt": []string{"base"}}},
			{"event": "tool_call", "payload": map[string]any{"toolName": "read", "input": map[string]any{"path": "src/a.ts"}}},
			{"event": "tool_call", "payload": map[string]any{"toolName": "read", "input": map[string]any{"path": "docs/spec/x.md"}}},
			{"event": "tool_call", "payload": map[string]any{"toolName": "read", "input": map[string]any{"path": "docs/other.md"}}},
			{"event": "tool_call", "payload": map[string]any{"toolName": "read", "input": map[string]any{"path": "/etc/passwd"}}},
			{"event": "tool_call", "payload": map[string]any{"toolName": "write", "input": map[string]any{"path": "src/b.ts"}}},
			{"event": "tool_call", "payload": map[string]any{"toolName": "bash", "input": map[string]any{"command": "touch /tmp/atomic-denied"}}},
			{"event": "tool_call", "payload": map[string]any{"toolName": "bash", "input": map[string]any{"command": "touch /tmp/atomic-denied # BLOCK_ME"}}},
			{"event": "before_agent_start", "payload": map[string]any{"prompt": "p2", "systemPrompt": []string{"base", "<atomic-rules marker=\"atomic-omp-runtime\">stale</atomic-rules>"}}},
			{"event": "before_provider_request", "payload": map[string]any{"payload": map[string]any{"system": "<atomic-rules marker=\"atomic-omp-runtime\">"}}},
			{"event": "before_provider_request", "payload": map[string]any{"payload": map[string]any{"system": "plain"}}},
			{"event": "session_shutdown", "payload": map[string]any{}},
		},
	}
	specBytes, err := json.Marshal(spec)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "spec.json"), string(specBytes))

	logPath := filepath.Join(dir, "runtime.jsonl")
	cmd := exec.Command(bun, "run", "driver.ts", "spec.json", "out.json")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), RuntimeLogEnv+"="+logPath)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("bun driver failed: %v\n%s", err, out)
	}

	var results []map[string]any
	if err := json.Unmarshal([]byte(readFile(t, filepath.Join(dir, "out.json"))), &results); err != nil {
		t.Fatalf("parse driver output: %v", err)
	}
	if len(results) != 13 {
		t.Fatalf("driver returned %d results, want 13", len(results))
	}

	baseline := results[1]
	prompt, _ := baseline["systemPrompt"].([]any)
	if len(prompt) != 2 || !strings.Contains(asString(prompt[1]), RuntimeMarker) {
		t.Fatalf("first baseline = %v, want the base prompt plus one Atomic block", baseline)
	}
	block := asString(prompt[1])
	for _, want := range []string{"shipped:rules/all/style.md", "shipped:rules/docs/spec.md", "shipped:rules/ts/style.md", "</atomic-rules>"} {
		if !strings.Contains(block, want) {
			t.Errorf("baseline block is missing %q:\n%s", want, block)
		}
	}
	deny := results[8]
	if deny["block"] != true || deny["reason"] != "ATOMIC_DENY_MARKER" {
		t.Errorf("exact predicate deny = %v, want a block result with the predicate reason", deny)
	}
	if results[7] != nil {
		t.Errorf("the near-miss command was denied: %v", results[7])
	}
	laterPrompt, _ := results[9]["systemPrompt"].([]any)
	if len(laterPrompt) != 2 {
		t.Fatalf("a turn carrying a stale block = %v, want the stale block replaced", results[9])
	}
	if count := strings.Count(asString(laterPrompt[1]), RuntimeMarker); count != 1 || strings.Contains(asString(laterPrompt[1]), "stale") {
		t.Errorf("duplicate or stale block after replacement: %q", laterPrompt[1])
	}
	if !strings.Contains(asString(laterPrompt[1]), "shipped:rules/all/style.md") {
		t.Errorf("the later baseline does not report the session's matched rules:\n%s", laterPrompt[1])
	}

	log, err := ReadRuntimeLog(logPath)
	if err != nil {
		t.Fatalf("read runtime log: %v", err)
	}
	if log.Factories != 1 || log.Sessions != 1 || log.BaselineDeliveries != 2 || !log.BaselineConsumed {
		t.Errorf("log = %+v", log)
	}
	if got := strings.Join(log.Matched, ","); got != "shipped:rules/all/style.md,shipped:rules/docs/spec.md,shipped:rules/ts/style.md" {
		t.Errorf("matched = %s, want canonical identity order", got)
	}
	if got := strings.Join(log.UncoveredTools, ","); got != "bash,write" {
		t.Errorf("uncovered tools = %s, want every tool without a proven structured input", got)
	}
	if log.UncoveredOperations != 1 {
		t.Errorf("uncovered operations = %d, want the absolute path recorded instead of matched", log.UncoveredOperations)
	}
	if log.Operations != 3 || log.Denials != 1 {
		t.Errorf("operations/denials = %d/%d", log.Operations, log.Denials)
	}
	for _, record := range log.Records {
		if record.Kind == "operation" && record.Candidate == "docs/other.md" && len(record.Matched) != 0 {
			t.Errorf("a nonmatching path matched rules: %+v", record)
		}
	}
}

// TestExtensionModuleRefusesAnOverBoundBlock proves the runtime bound: an index
// over the bound is never delivered, not truncated.
func TestExtensionModuleRefusesAnOverBoundBlock(t *testing.T) {
	bun, err := exec.LookPath("bun")
	if err != nil {
		t.Skipf("bun is unavailable: %v", err)
	}
	dir := t.TempDir()
	delivery, err := BuildSessionDelivery(runtimeRules(t), harness.OMPCapabilities(), nil)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	delivery.Bound = 16
	module, err := delivery.RenderExtension()
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	writeFile(t, filepath.Join(dir, "atomic.ts"), string(module))
	writeFile(t, filepath.Join(dir, "driver.ts"), extensionDriver)
	specBytes, err := json.Marshal(map[string]any{
		"tools": []string{},
		"steps": []map[string]any{
			{"event": "before_agent_start", "payload": map[string]any{"prompt": "p", "systemPrompt": []string{"base"}}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "spec.json"), string(specBytes))

	logPath := filepath.Join(dir, "runtime.jsonl")
	cmd := exec.Command(bun, "run", "driver.ts", "spec.json", "out.json")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), RuntimeLogEnv+"="+logPath)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("bun driver failed: %v\n%s", err, out)
	}
	if got := strings.TrimSpace(readFile(t, filepath.Join(dir, "out.json"))); !strings.Contains(got, "null") && !strings.Contains(got, "{}") {
		t.Errorf("an over-bound delivery returned %s, want no system prompt", got)
	}
	log, err := ReadRuntimeLog(logPath)
	if err != nil {
		t.Fatalf("read runtime log: %v", err)
	}
	if log.Suppressions != 1 || log.BaselineDeliveries != 0 {
		t.Errorf("log = %+v, want one suppression and no delivery", log)
	}
}

// TestExtensionModuleRowlessRunsWithoutGatedHandlers proves the rowless artifact
// still loads and runs under the harness runtime: the observation handlers fire,
// a gated event has no handler at all, and no baseline, operation, or deny record
// is ever written. The module that registered every handler unconditionally
// would pass this only by running behavior the delivery never planned.
func TestExtensionModuleRowlessRunsWithoutGatedHandlers(t *testing.T) {
	bun, err := exec.LookPath("bun")
	if err != nil {
		t.Skipf("bun is unavailable: %v", err)
	}
	dir := t.TempDir()
	rowless := harness.CapabilityMatrix{Harness: harness.KindOMP, Version: "unproven"}
	delivery, err := BuildSessionDelivery(runtimeRules(t), rowless, nil)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	module, err := delivery.RenderExtension()
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	writeFile(t, filepath.Join(dir, "atomic.ts"), string(module))
	writeFile(t, filepath.Join(dir, "driver.ts"), extensionDriver)
	specBytes, err := json.Marshal(map[string]any{
		"tools": []string{"read"},
		"steps": []map[string]any{
			{"event": "session_start", "payload": map[string]any{}, "ctx": map[string]any{"cwd": "/repo"}},
			{"event": "before_agent_start", "payload": map[string]any{"systemPrompt": []string{"base"}}},
			{"event": "tool_call", "payload": map[string]any{"toolName": "read", "input": map[string]any{"path": "src/a.ts"}}},
			{"event": "before_provider_request", "payload": map[string]any{"payload": map[string]any{}}},
			{"event": "session_shutdown", "payload": map[string]any{}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "spec.json"), string(specBytes))

	logPath := filepath.Join(dir, "runtime.jsonl")
	cmd := exec.Command(bun, "run", "driver.ts", "spec.json", "out.json")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), RuntimeLogEnv+"="+logPath)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("bun driver failed: %v\n%s", err, out)
	}
	var results []map[string]any
	if err := json.Unmarshal([]byte(readFile(t, filepath.Join(dir, "out.json"))), &results); err != nil {
		t.Fatalf("parse driver output: %v", err)
	}
	if len(results) != 5 {
		t.Fatalf("driver returned %d results, want 5", len(results))
	}
	for _, gated := range []struct {
		index int
		event string
	}{
		{1, "before_agent_start"},
		{2, "tool_call"},
	} {
		if got := asString(results[gated.index]["missing"]); got != gated.event {
			t.Errorf("a rowless module handled %s: %v", gated.event, results[gated.index])
		}
	}
	if results[0] != nil || results[3] != nil || results[4] != nil {
		t.Errorf("observation handlers returned values: %v", results)
	}
	log, err := ReadRuntimeLog(logPath)
	if err != nil {
		t.Fatalf("read runtime log: %v", err)
	}
	if log.Sessions != 1 || log.BaselineDeliveries != 0 || log.Operations != 0 || log.Denials != 0 {
		t.Errorf("log = %+v, want one session and no gated record", log)
	}
}

func asString(value any) string {
	text, _ := value.(string)
	return text
}

var _ = artifacts.EnforcementUnsupported
