package repl

import (
	"strings"
	"testing"
)

// Drives the embedded Node harness through the shared conformance suite, spawning
// node directly rather than through any Go spawn path: this tests the script, not
// the plumbing that starts it.
func TestNodeHarness(t *testing.T) {
	runHarnessConformance(t, harnessCase{
		lang: LangNode,
		bin:  "node",

		valueExpr: "6 * 7",
		multiline: "const base = 6;\nconst factor = 7;\nbase * factor",
		statement: "var unusedProbe = 1;",
		wantValue: "42",

		stdoutCode: "console.log('out-here')",
		stderrCode: "console.error('err-here')",

		failCode:     "console.log('before-failure');\nthrow new Error('boom-on-line-two');",
		failMessage:  "Error: boom-on-line-two",
		failLineText: "throw new Error('boom-on-line-two');",
		failLineRef:  "repl.js:2",
		// V8 stacks run straight through the vm bridge into the harness's own
		// frames; those are noise an agent would have to learn to ignore.
		forbiddenInError: []string{"node_harness", "node:vm", "node:internal"},

		bigOutput:   "console.log('x'.repeat(100000))",
		smallOutput: "console.log('short')",
		// A JS string can hold a lone surrogate; UTF-8 cannot encode one, so
		// the harness must replace it rather than emit a frame Go will reject.
		surrogate: "process.stdout.write('a\\uD800b'); undefined;",
		bigValue:  "Array.from({ length: 200 }, () => 'x'.repeat(1000))",

		stateSet:         "const stateProbe = 41;",
		stateGet:         "stateProbe + 1",
		resetErrorMarker: "ReferenceError",

		slowEval: "(() => { const until = Date.now() + 600; while (Date.now() < until) {} globalThis.slowMarker = 40; return 'slow-done'; })()",
		fastEval: "slowMarker + 2",
		wantFast: "42",
	})
}

// The one failure mode with no user frame to unwind: the script never compiled,
// so the trace comes from the parse itself and must still show what and where.
func TestNodeHarness_SyntaxErrorReportsTheOffendingLine(t *testing.T) {
	h := startHarness(t, nodeCase(), conformanceIdleTimeout)
	resp := h.eval(t, "const value = 1;\nconst broken = (;\n")

	if resp.OK {
		t.Fatalf("ok = true for code that does not parse; response = %+v", resp)
	}
	for _, want := range []string{"SyntaxError", "repl.js:2"} {
		if !strings.Contains(resp.Error, want) {
			t.Errorf("error does not contain %q; got:\n%s", want, resp.Error)
		}
	}
	// Still serving afterwards.
	assertOK(t, h.eval(t, "6 * 7"))
}

// A deliberate choice in the harness's newContext: eval'd code's bare specifiers
// resolve against the session's working directory, not the throwaway directory
// the script was materialized into.
func TestNodeHarness_RequireResolvesAgainstCWD(t *testing.T) {
	h := startHarness(t, nodeCase(), conformanceIdleTimeout)
	resp := h.eval(t, "require('node:path').basename('/a/b/c.txt')")
	assertOK(t, resp)
	if resp.Value != "'c.txt'" {
		t.Errorf("value = %q, want %q", resp.Value, "'c.txt'")
	}
}

func nodeCase() harnessCase {
	return harnessCase{lang: LangNode, bin: "node"}
}
