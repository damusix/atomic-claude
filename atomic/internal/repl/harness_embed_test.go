package repl

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func TestHarnessScript_ReturnsEmbeddedSource(t *testing.T) {
	tests := []struct {
		lang       string
		wantMarker string
	}{
		{LangPython, "atomic repl Python harness"},
		{LangNode, "atomic repl Node harness"},
	}
	for _, tt := range tests {
		t.Run(tt.lang, func(t *testing.T) {
			script, err := HarnessScript(tt.lang)
			if err != nil {
				t.Fatalf("HarnessScript(%q): %v", tt.lang, err)
			}
			if len(script) == 0 {
				t.Fatalf("HarnessScript(%q) returned no bytes", tt.lang)
			}
			if !strings.Contains(string(script), tt.wantMarker) {
				t.Errorf("HarnessScript(%q) does not contain %q — wrong script embedded?", tt.lang, tt.wantMarker)
			}
		})
	}
}

func TestHarnessScript_UnknownLangNamesValidOnes(t *testing.T) {
	_, err := HarnessScript("ruby")
	if err == nil {
		t.Fatal("HarnessScript(\"ruby\") returned no error")
	}
	for _, want := range []string{"ruby", LangPython, LangNode} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name %q", err, want)
		}
	}
}

// The materialized script sits under ~/.atomic/repl with no package.json of its
// own, so a ".js" extension would inherit the "type" field of whichever
// unrelated package.json Node finds by walking up.
func TestHarnessFilename_NodeIsMJS(t *testing.T) {
	tests := []struct {
		lang string
		want string
	}{
		{LangPython, "python_harness.py"},
		{LangNode, "node_harness.mjs"},
	}
	for _, tt := range tests {
		got, err := HarnessFilename(tt.lang)
		if err != nil {
			t.Fatalf("HarnessFilename(%q): %v", tt.lang, err)
		}
		if got != tt.want {
			t.Errorf("HarnessFilename(%q) = %q, want %q", tt.lang, got, tt.want)
		}
		if filepath.Ext(got) == "" {
			t.Errorf("HarnessFilename(%q) = %q has no extension", tt.lang, got)
		}
	}

	if _, err := HarnessFilename("ruby"); err == nil {
		t.Error("HarnessFilename(\"ruby\") returned no error")
	}
}

// The drift guard between Go and the two hand-written harnesses. Each script
// hardcodes the protocol version and the output cap because it reads no Go and
// no config; nothing but this notices when one side moves.
func TestHarnessScripts_PinProtocolConstants(t *testing.T) {
	tests := []struct {
		lang       string
		versionRE  *regexp.Regexp
		maxBytesRE *regexp.Regexp
	}{
		{
			lang:       LangPython,
			versionRE:  regexp.MustCompile(`(?m)^PROTOCOL_VERSION = (\d+)$`),
			maxBytesRE: regexp.MustCompile(`(?m)^MAX_STREAM_BYTES = (\d+) \* 1024$`),
		},
		{
			lang:       LangNode,
			versionRE:  regexp.MustCompile(`(?m)^const PROTOCOL_VERSION = (\d+);$`),
			maxBytesRE: regexp.MustCompile(`(?m)^const MAX_STREAM_BYTES = (\d+) \* 1024;$`),
		},
	}
	for _, tt := range tests {
		t.Run(tt.lang, func(t *testing.T) {
			script, err := HarnessScript(tt.lang)
			if err != nil {
				t.Fatalf("HarnessScript(%q): %v", tt.lang, err)
			}
			if got := captureInt(t, tt.versionRE, script); got != ProtocolVersion {
				t.Errorf("%s harness PROTOCOL_VERSION = %d, want %d", tt.lang, got, ProtocolVersion)
			}
			if got := captureInt(t, tt.maxBytesRE, script) * 1024; got != MaxStreamBytes {
				t.Errorf("%s harness MAX_STREAM_BYTES = %d, want %d", tt.lang, got, MaxStreamBytes)
			}
		})
	}
}

// Each harness must have a dispatch arm for every op in AllOps, so adding one to
// the Go list without teaching the harnesses fails here rather than at runtime.
// The patterns match the dispatch statement, not a mention in a comment.
func TestHarnessScripts_DispatchEveryOp(t *testing.T) {
	tests := []struct {
		lang string
		arm  func(op string) string
	}{
		{LangPython, func(op string) string { return fmt.Sprintf("if op == %q:", op) }},
		{LangNode, func(op string) string { return fmt.Sprintf("case '%s':", op) }},
	}
	for _, tt := range tests {
		script, err := HarnessScript(tt.lang)
		if err != nil {
			t.Fatalf("HarnessScript(%q): %v", tt.lang, err)
		}
		for _, op := range AllOps {
			if !strings.Contains(string(script), tt.arm(op)) {
				t.Errorf("%s harness has no dispatch arm %q", tt.lang, tt.arm(op))
			}
		}
	}
}

func captureInt(t *testing.T, re *regexp.Regexp, script []byte) int {
	t.Helper()
	match := re.FindSubmatch(script)
	if match == nil {
		t.Fatalf("harness does not declare a constant matching %s", re)
	}
	value, err := strconv.Atoi(string(match[1]))
	if err != nil {
		t.Fatalf("parse %q: %v", match[1], err)
	}
	return value
}
