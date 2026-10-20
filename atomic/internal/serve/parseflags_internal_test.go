package serve

import (
	"io"
	"path/filepath"
	"strings"
	"testing"
)

// Handlers filepath.Join TargetDir with request paths, so a relative TargetDir
// turns every /page/ and /rail/ lookup into a 404. Normalizing here is the one
// fix that covers all of them.
func TestParseFlagsRelativeTargetDirBecomesAbsolute(t *testing.T) {
	cases := []struct {
		name string
		args []string // positional args; first is the target dir
	}{
		{name: "dot", args: []string{"."}},
		{name: "relative subdir", args: []string{"some/relative/path"}},
		{name: "no arg (cwd fallback)", args: []string{}},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			opts, err := parseFlags(tc.args, io.Discard, io.Discard)
			if err != nil {
				// filepath.Abs does not require the path to exist, so a missing
				// relative path is not a reason to error.
				t.Fatalf("parseFlags(%v): unexpected error: %v", tc.args, err)
			}
			if !filepath.IsAbs(opts.TargetDir) {
				t.Errorf("parseFlags(%v): TargetDir = %q, want absolute path", tc.args, opts.TargetDir)
			}
		})
	}
}

func TestParseFlagsTrailingPortHonored(t *testing.T) {
	opts, err := parseFlags([]string{"some/dir", "--port", "4711"}, io.Discard, io.Discard)
	if err != nil {
		t.Fatalf("parseFlags: unexpected error: %v", err)
	}
	if opts.Port != 4711 {
		t.Errorf("Port = %d, want 4711: a flag after the dir must not be dropped", opts.Port)
	}
	if filepath.Base(opts.TargetDir) != "dir" {
		t.Errorf("TargetDir = %q, want it to end in dir", opts.TargetDir)
	}
}

func TestParseFlagsExtraPositionalRejected(t *testing.T) {
	var stderr strings.Builder
	if _, err := parseFlags([]string{"a", "b"}, io.Discard, &stderr); err == nil {
		t.Fatal("parseFlags(a b): want error, serve takes at most one dir")
	}
	if !strings.Contains(stderr.String(), `["b"]`) {
		t.Errorf("stderr = %q, want it to name the extra argument", stderr.String())
	}
}

func TestParseFlagsTerminatorKeepsFlagPositional(t *testing.T) {
	var stderr strings.Builder
	if _, err := parseFlags([]string{"--", "dir", "--port", "4711"}, io.Discard, &stderr); err == nil {
		t.Fatal("want error: after --, --port is a second positional, not a flag")
	}
	if want := `["--port" "4711"]`; !strings.Contains(stderr.String(), want) {
		t.Errorf("stderr=%q, want it to name %s as the stray arguments", stderr.String(), want)
	}
}
