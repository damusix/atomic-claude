package test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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
