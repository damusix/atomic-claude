package serve

import (
	"io"
	"path/filepath"
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
