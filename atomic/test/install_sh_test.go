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

	funcsSnippet := extractFunctions(t, script)

	fullScript := "#!/usr/bin/env bash\nset -uo pipefail\n" + funcsSnippet + "\n" + snippet

	tmp, err := os.CreateTemp(t.TempDir(), "install_test_*.sh")
	if err != nil {
		t.Fatalf("create temp: %v", err)
	}
	defer tmp.Close()
	tmp.WriteString(fullScript)
	tmp.Close()

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
// Note: the implementation uses explicit numeric field comparison and a
// pre-release suffix check — NOT sort -V. The test runs on any system with bash.
func TestSemverGte_BasicOrdering(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bash not available on Windows CI")
	}
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not on PATH")
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
// all cause _os() from install.sh to return an empty string (exit 1).
// We invoke the real _os() function (extracted from install.sh via bashHelper)
// with a stubbed uname so the test stays hermetic.
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
			// Stub uname to return the desired string, then call the real _os().
			snippet := fmt.Sprintf(`
uname() { echo %q; }
export -f uname 2>/dev/null || true
result="$(_os 2>/dev/null)" && exit_code=0 || exit_code=$?
echo "result:${result}"
echo "exit:${exit_code}"
`, uname)
			stdout, _, _ := bashHelper(t, snippet)
			lines := strings.Split(strings.TrimSpace(stdout), "\n")
			resultLine := ""
			exitLine := ""
			for _, l := range lines {
				if strings.HasPrefix(l, "result:") {
					resultLine = strings.TrimPrefix(l, "result:")
				}
				if strings.HasPrefix(l, "exit:") {
					exitLine = strings.TrimPrefix(l, "exit:")
				}
			}
			// _os() must return empty string and non-zero exit for Windows variants.
			if resultLine != "" {
				t.Errorf("_os() with uname %q: expected empty result, got %q", uname, resultLine)
			}
			if exitLine == "0" {
				t.Errorf("_os() with uname %q: expected non-zero exit, got 0", uname)
			}
		})
	}
}
