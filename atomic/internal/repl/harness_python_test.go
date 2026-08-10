package repl

import (
	"strings"
	"testing"
)

// TestPythonHarness drives the embedded Python harness through the shared
// cross-language conformance suite in harness_contract_test.go, spawning
// python3 directly rather than through any Go spawn path — this checkpoint
// tests the script, not the plumbing that will later start it.
func TestPythonHarness(t *testing.T) {
	runHarnessConformance(t, harnessCase{
		lang: LangPython,
		bin:  "python3",

		valueExpr: "6 * 7",
		multiline: "base = 6\nfactor = 7\nbase * factor",
		statement: "unused_probe = 1",
		wantValue: "42",

		stdoutCode: "print('out-here')",
		stderrCode: "import sys\nprint('err-here', file=sys.stderr)",

		failCode:     "print('before-failure')\nraise ValueError('boom-on-line-two')",
		failMessage:  "ValueError: boom-on-line-two",
		failLineText: "raise ValueError('boom-on-line-two')",
		failLineRef:  "line 2",
		// The traceback must start at the eval'd code: the harness's own exec
		// frame is noise an agent would have to learn to ignore.
		forbiddenInError: []string{"python_harness.py", "_run", "linecache"},

		bigOutput:   "print('x' * 100000)",
		smallOutput: "print('short')",
		// A str can hold a lone surrogate (surrogateescape decoding produces
		// them); UTF-8 cannot encode one, so the harness must replace it
		// rather than emit a frame Go will reject.
		surrogate: "import sys\nsys.stdout.write('a\\udcffb')\nNone",
		bigValue:  "'x' * 100000",

		stateSet:         "state_probe = 41",
		stateGet:         "state_probe + 1",
		resetErrorMarker: "NameError",

		slowEval: "import time\ntime.sleep(0.6)\nslow_marker = 40\n'slow-done'",
		fastEval: "slow_marker + 2",
		wantFast: "42",
	})
}

// TestPythonHarness_SyntaxErrorReportsTheOffendingLine covers the one failure
// mode with no user frame to unwind: the code never ran, so the traceback comes
// from the parse itself and must still show what and where.
func TestPythonHarness_SyntaxErrorReportsTheOffendingLine(t *testing.T) {
	h := startHarness(t, pythonCase(), conformanceIdleTimeout)
	resp := h.eval(t, "value = 1\nvalue = (\n")

	if resp.OK {
		t.Fatalf("ok = true for code that does not parse; response = %+v", resp)
	}
	for _, want := range []string{"SyntaxError", "line 2"} {
		if !strings.Contains(resp.Error, want) {
			t.Errorf("error does not contain %q; got:\n%s", want, resp.Error)
		}
	}
	// Still serving afterwards.
	assertOK(t, h.eval(t, "6 * 7"))
}

func pythonCase() harnessCase {
	return harnessCase{lang: LangPython, bin: "python3"}
}
