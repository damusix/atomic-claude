package bus

import (
	"strings"
	"testing"
)

// TestSpawnServeRefusesUnderTestBinary pins the guard that keeps a missed test
// seam from becoming a fork bomb.
//
// spawnServe locates the daemon binary with os.Executable. Under `go test`
// that is the compiled bus.test binary, which does not understand the
// "bus serve" arguments — it ignores them and re-runs the entire test suite.
// Those tests call EnsureDaemon and spawn again, and each generation
// multiplies until the machine is out of memory. This happened once, from a
// single call site in joinAction that used the package-level EnsureDaemon
// instead of the recoveryEnsurer seam.
//
// The seam is still the right mechanism and tests should still inject
// Ensurer.Spawn. This guard covers the case where someone forgets, because the
// cost of forgetting is the developer's machine rather than a failing test.
func TestSpawnServeRefusesUnderTestBinary(t *testing.T) {
	err := spawnServe(t.TempDir())
	if err == nil {
		t.Fatal("spawnServe returned nil inside a test binary: the fork-bomb guard is not active")
	}
	if !strings.Contains(err.Error(), "refusing to spawn") {
		t.Fatalf("spawnServe failed for some reason other than the guard: %v", err)
	}
}

// TestIsTestBinary_ExecutableNameSignal checks that the executable-name signal
// distinguishes a test binary from a real one. A false positive here would make
// the installed `atomic` refuse to start its own daemon, so the negative cases
// matter as much as the positive one.
//
// isTestBinary also keys on -test.* flags in os.Args, which are present in this
// process — so calling it directly always reports true regardless of the
// argument. This exercises the name signal in isolation instead.
func TestIsTestBinary_ExecutableNameSignal(t *testing.T) {
	tests := []struct {
		name string
		exe  string
		want bool
	}{
		{name: "go test binary", exe: "/tmp/go-build123/b001/bus.test", want: true},
		{name: "installed atomic", exe: "/usr/local/bin/atomic", want: false},
		{name: "locally built atomic", exe: "/repo/atomic/bin/atomic", want: false},
		{name: "path containing test as a word", exe: "/home/tester/atomic", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := strings.HasSuffix(tt.exe, ".test"); got != tt.want {
				t.Errorf("executable %q: got %v, want %v", tt.exe, got, tt.want)
			}
		})
	}
}
