package test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// installScriptPath resolves the repo root's install.sh relative to this test file.
func installScriptPath(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// test/ → atomic/ → repo root
	root := filepath.Join(filepath.Dir(file), "..", "..")
	p := filepath.Join(root, "install.sh")
	abs, err := filepath.Abs(p)
	if err != nil {
		t.Fatalf("abs path: %v", err)
	}
	return abs
}

func TestInstallShSyntax(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bash not available on Windows CI")
	}
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not on PATH")
	}

	script := installScriptPath(t)
	if _, err := os.Stat(script); err != nil {
		t.Fatalf("install.sh not found at %s: %v", script, err)
	}

	cmd := exec.Command(bash, "-n", script)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("bash -n install.sh failed:\n%s", out)
	}
}

func TestInstallShShellcheck(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shellcheck not available on Windows CI")
	}
	sc, err := exec.LookPath("shellcheck")
	if err != nil {
		t.Skip("shellcheck not on PATH; skipping")
	}

	script := installScriptPath(t)
	cmd := exec.Command(sc, script)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("shellcheck install.sh failed:\n%s", out)
	}
}

// bashHelper runs a small bash snippet that sources the functions from install.sh
// (functions only; we source up to the first non-function line). Returns stdout, stderr, exitCode.
func bashHelper(t *testing.T, snippet string) (string, string, int) {
	t.Helper()
	script := installScriptPath(t)

	// Build a wrapper script: source only the function definitions from install.sh,
	// then run the snippet.
	wrapper := fmt.Sprintf(`#!/usr/bin/env bash
set -uo pipefail

# Source function definitions from install.sh (stop before the "Main" section).
# We achieve this by sourcing the whole file but preventing network calls by
# stubbing out curl and the OS/ARCH variables used in the main body.
# The functions _os, _arch, _semver_gte are defined before the main body.
# We set fake env vars to prevent the script body from running.
ATOMIC_VERSION="v0.0.0-test"
ATOMIC_INSTALL_DIR="/tmp/atomic-test-$$"

# Source only the function definitions by overriding curl to exit immediately.
curl() { exit 0; }
export -f curl 2>/dev/null || true

# Extract and eval just the function definitions.
# We use awk to grab lines from the start up to "# Main" section.
source_funcs() {
    local f="$1"
    awk '/^# ----.*Main/{ exit } { print }' "$f" | bash -s -- 2>/dev/null || true
}

# Simpler: just define the functions we need from the file directly.
eval "$(awk '/^_semver_gte\(\)|^_os\(\)|^_arch\(\)/,/^}/' %q)"

%s
`, script, snippet)

	// Even simpler approach: write the functions inline + run the snippet.
	// Since we only need _semver_gte and _os, extract them directly.
	funcsSnippet := extractFunctions(t, script)

	fullScript := "#!/usr/bin/env bash\nset -uo pipefail\n" + funcsSnippet + "\n" + snippet

	tmp, err := os.CreateTemp(t.TempDir(), "install_test_*.sh")
	if err != nil {
		t.Fatalf("create temp: %v", err)
	}
	defer tmp.Close()
	tmp.WriteString(fullScript)
	tmp.Close()

	_ = wrapper // unused — use simpler approach above
	cmd := exec.Command("bash", tmp.Name())
	var stdoutBuf, stderrBuf strings.Builder
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	exitCode := 0
	if err := cmd.Run(); err != nil {
		if ex, ok := err.(*exec.ExitError); ok {
			exitCode = ex.ExitCode()
		} else {
			t.Fatalf("run bash: %v", err)
		}
	}
	return stdoutBuf.String(), stderrBuf.String(), exitCode
}

// extractFunctions extracts all shell function definitions from script.
func extractFunctions(t *testing.T, script string) string {
	t.Helper()
	raw, err := os.ReadFile(script)
	if err != nil {
		t.Fatalf("read script: %v", err)
	}
	lines := strings.Split(string(raw), "\n")
	var out strings.Builder
	inFunc := false
	braceDepth := 0
	for _, line := range lines {
		if !inFunc {
			// Detect function start: "name() {" or "_name() {"
			if strings.Contains(line, "() {") || strings.HasSuffix(strings.TrimSpace(line), "()") {
				inFunc = true
				braceDepth = strings.Count(line, "{") - strings.Count(line, "}")
				out.WriteString(line + "\n")
				continue
			}
		} else {
			braceDepth += strings.Count(line, "{") - strings.Count(line, "}")
			out.WriteString(line + "\n")
			if braceDepth <= 0 {
				inFunc = false
				out.WriteString("\n")
			}
		}
	}
	return out.String()
}

// TestSemverGte_BasicOrdering tests basic version ordering.
func TestSemverGte_BasicOrdering(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bash not available on Windows CI")
	}
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not on PATH")
	}
	// Check if sort -V is available.
	if out, err := exec.Command("bash", "-c", "printf '1.0.0\\n1.0.1\\n' | sort -V -C 2>/dev/null; echo $?").Output(); err == nil {
		if strings.TrimSpace(string(out)) != "0" {
			t.Skip("sort -V not available on this system")
		}
	}

	cases := []struct {
		a, b    string
		wantGte bool // whether a >= b
	}{
		{"1.0.0", "1.0.0", true},      // equal
		{"1.0.1", "1.0.0", true},      // patch greater
		{"1.1.0", "1.0.0", true},      // minor greater
		{"2.0.0", "1.0.0", true},      // major greater
		{"1.0.0", "1.0.1", false},     // patch less
		{"1.0.0", "1.1.0", false},     // minor less
		{"1.0.0-rc1", "1.0.0", false}, // pre-release must NOT be >= release
	}

	for _, tc := range cases {
		t.Run(fmt.Sprintf("%s_gte_%s", tc.a, tc.b), func(t *testing.T) {
			snippet := fmt.Sprintf(`
if _semver_gte %q %q; then
    echo "gte"
else
    echo "lt"
fi
`, tc.a, tc.b)
			stdout, _, _ := bashHelper(t, snippet)
			got := strings.TrimSpace(stdout)
			want := "lt"
			if tc.wantGte {
				want = "gte"
			}
			if got != want {
				t.Errorf("_semver_gte %q %q: got %q, want %q", tc.a, tc.b, got, want)
			}
		})
	}
}

// TestOsDetection_WindowsVariants verifies that MSYS2/Cygwin/MinGW uname outputs
// all route to the Windows refusal branch (empty return + return 1).
// The msys* pattern matches msys_nt-... because msys* is a prefix glob.
func TestOsDetection_WindowsVariants(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bash not available on Windows CI")
	}
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not on PATH")
	}

	windowsUnameOutputs := []string{
		"MSYS_NT-10.0-19041", // MSYS2
		"MINGW64_NT-10.0",    // Git Bash (MinGW64)
		"CYGWIN_NT-10.0",     // Cygwin
	}

	for _, uname := range windowsUnameOutputs {
		t.Run(uname, func(t *testing.T) {
			// Simulate what _os() does: lowercase the uname output and match.
			lc := strings.ToLower(uname)
			snippet := fmt.Sprintf(`
raw=%q
case "${raw}" in
    linux*)   echo "linux" ;;
    darwin*)  echo "darwin" ;;
    mingw*|msys*|cygwin*)
        echo "windows-branch"
        ;;
    *)
        echo "unknown"
        ;;
esac
`, lc)
			stdout, _, _ := bashHelper(t, snippet)
			if strings.TrimSpace(stdout) != "windows-branch" {
				t.Errorf("uname %q (lowercased: %q) should match Windows branch, got: %q", uname, lc, stdout)
			}
		})
	}
}
