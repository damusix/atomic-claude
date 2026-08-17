package dockerinit_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/dockerinit"
)

// The contributor docker-compose.yml at the repo root and the end-user template
// must keep the same shape: service name, volume mounts, tty/stdin_open,
// HOST_UID. Substring assertions rather than YAML parsing, so whitespace and
// comment drift do not false-fail.
func TestComposeConvergence(t *testing.T) {
	contributorPath := filepath.Join("..", "..", "..", "docker-compose.yml")
	contributorBytes, err := os.ReadFile(contributorPath)
	if err != nil {
		if os.IsNotExist(err) {
			t.Skip("contributor docker-compose.yml not found (running from tarball?); skipping convergence check")
		}
		t.Fatalf("reading contributor docker-compose.yml: %v", err)
	}
	contributor := string(contributorBytes)

	dir := t.TempDir()
	_, err = dockerinit.Init(dockerinit.Options{
		TargetDir:     dir,
		AtomicVersion: "v0.0.0-test",
		HostUID:       1000,
	})
	if err != nil {
		t.Fatalf("Init returned error: %v", err)
	}

	renderedBytes, err := os.ReadFile(filepath.Join(dir, "docker-compose.yml"))
	if err != nil {
		t.Fatalf("reading rendered docker-compose.yml: %v", err)
	}
	rendered := string(renderedBytes)

	sharedKeys := []string{
		"atomic-eval",
		"./tmp/workspace:/workspace",
		"./tmp/claude-home:/home/atomic/.claude",
		"tty: true",
		"stdin_open: true",
		"HOST_UID",
	}

	for _, needle := range sharedKeys {
		if !strings.Contains(contributor, needle) {
			t.Errorf("contributor docker-compose.yml missing shared key %q", needle)
		}
		if !strings.Contains(rendered, needle) {
			t.Errorf("rendered end-user docker-compose.yml missing shared key %q", needle)
		}
	}
}
