package serve

import (
	"io"
	"path/filepath"
	"testing"
)

// TestParseFlagsRelativeTargetDirBecomesAbsolute verifies that parseFlags
// normalizes a relative target-directory argument to an absolute path.
//
// Why: downstream handlers (page, rail, file, link graph) join TargetDir with
// request-relative paths using filepath.Join. When TargetDir is relative (e.g.
// "."), the joined paths don't match the OS's absolute working directory, so
// every /page/ and /rail/ lookup returns 404. filepath.Abs in parseFlags is
// the single fix that makes all handlers work regardless of how the user invoked
// "atomic serve".
func TestParseFlagsRelativeTargetDirBecomesAbsolute(t *testing.T) {
	cases := []struct {
		name string
		args []string // args passed after flag parsing (positional = target dir)
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
				// "some/relative/path" won't exist but that's fine — parseFlags
				// calls filepath.Abs which does not require the path to exist.
				// If the error is from getwd/home we'd catch it above; for a
				// non-existent relative path filepath.Abs still succeeds.
				t.Fatalf("parseFlags(%v): unexpected error: %v", tc.args, err)
			}
			if !filepath.IsAbs(opts.TargetDir) {
				t.Errorf("parseFlags(%v): TargetDir = %q, want absolute path", tc.args, opts.TargetDir)
			}
		})
	}
}
