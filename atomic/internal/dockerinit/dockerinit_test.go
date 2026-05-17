package dockerinit_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/dockerinit"
)

func TestInit_FreshDir(t *testing.T) {
	dir := t.TempDir()
	opts := dockerinit.Options{
		TargetDir:     dir,
		AtomicVersion: "v1.0.0",
		HostUID:       1000,
	}
	actions, err := dockerinit.Init(opts)
	if err != nil {
		t.Fatalf("Init returned error: %v", err)
	}
	if len(actions) != 6 {
		t.Fatalf("expected 6 FileActions, got %d", len(actions))
	}
	for _, a := range actions {
		if a.Kind != dockerinit.ActionCreated {
			t.Errorf("action %s: expected ActionCreated, got %s", a.Path, a.Kind)
		}
		full := filepath.Join(dir, a.Path)
		if _, err := os.Stat(full); os.IsNotExist(err) {
			t.Errorf("file not on disk: %s", full)
		}
	}
}

func TestInit_ExistingFilesNoForce(t *testing.T) {
	dir := t.TempDir()
	// Pre-create two of the output files with known content.
	preExisting := map[string]string{
		"Dockerfile":         "# stub dockerfile\n",
		"docker-compose.yml": "# stub compose\n",
	}
	for name, content := range preExisting {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	opts := dockerinit.Options{
		TargetDir:     dir,
		AtomicVersion: "v1.0.0",
		HostUID:       1000,
	}
	actions, err := dockerinit.Init(opts)
	if err != nil {
		t.Fatalf("Init returned error: %v", err)
	}
	if len(actions) != 6 {
		t.Fatalf("expected 6 FileActions, got %d", len(actions))
	}

	byPath := make(map[string]dockerinit.ActionKind, len(actions))
	for _, a := range actions {
		byPath[a.Path] = a.Kind
	}

	for name := range preExisting {
		if byPath[name] != dockerinit.ActionSkipped {
			t.Errorf("%s: expected ActionSkipped, got %s", name, byPath[name])
		}
		// Pre-existing content must be unchanged.
		data, _ := os.ReadFile(filepath.Join(dir, name))
		if string(data) != preExisting[name] {
			t.Errorf("%s: content was modified without --force", name)
		}
	}

	// All others must be ActionCreated.
	for path, kind := range byPath {
		if _, isPreExisting := preExisting[path]; isPreExisting {
			continue
		}
		if kind != dockerinit.ActionCreated {
			t.Errorf("%s: expected ActionCreated, got %s", path, kind)
		}
	}
}

func TestInit_ExistingFilesForce(t *testing.T) {
	dir := t.TempDir()
	stubContent := "# this will be overwritten\n"
	if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte(stubContent), 0644); err != nil {
		t.Fatal(err)
	}

	opts := dockerinit.Options{
		TargetDir:     dir,
		AtomicVersion: "v1.2.3",
		HostUID:       1000,
		Force:         true,
	}
	actions, err := dockerinit.Init(opts)
	if err != nil {
		t.Fatalf("Init returned error: %v", err)
	}

	var dfAction *dockerinit.FileAction
	for i := range actions {
		if actions[i].Path == "Dockerfile" {
			dfAction = &actions[i]
			break
		}
	}
	if dfAction == nil {
		t.Fatal("no FileAction for Dockerfile")
	}
	if dfAction.Kind != dockerinit.ActionOverwritten {
		t.Errorf("Dockerfile: expected ActionOverwritten, got %s", dfAction.Kind)
	}

	data, err := os.ReadFile(filepath.Join(dir, "Dockerfile"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) == stubContent {
		t.Error("Dockerfile was not overwritten with --force")
	}
	if string(data) == "" {
		t.Error("Dockerfile is empty after overwrite")
	}
}

func TestInit_TargetDoesNotExist(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "new", "nested", "dir")

	opts := dockerinit.Options{
		TargetDir:     dir,
		AtomicVersion: "v1.0.0",
		HostUID:       1000,
	}
	actions, err := dockerinit.Init(opts)
	if err != nil {
		t.Fatalf("Init should create non-existent target dir, got error: %v", err)
	}
	if len(actions) != 6 {
		t.Fatalf("expected 6 FileActions, got %d", len(actions))
	}
	// Verify the dir was actually created and files are in it.
	for _, a := range actions {
		full := filepath.Join(dir, a.Path)
		if _, err := os.Stat(full); os.IsNotExist(err) {
			t.Errorf("file not found after dir creation: %s", full)
		}
	}
}

func TestInit_TemplateRendering(t *testing.T) {
	dir := t.TempDir()
	opts := dockerinit.Options{
		TargetDir:     dir,
		AtomicVersion: "v1.2.3",
		HostUID:       1234,
	}
	if _, err := dockerinit.Init(opts); err != nil {
		t.Fatalf("Init returned error: %v", err)
	}

	// Dockerfile must contain ATOMIC_VERSION=v1.2.3 and reference HOST_UID=1234.
	df, err := os.ReadFile(filepath.Join(dir, "Dockerfile"))
	if err != nil {
		t.Fatal(err)
	}
	dfStr := string(df)
	if !contains(dfStr, "ATOMIC_VERSION=v1.2.3") {
		t.Errorf("Dockerfile missing ATOMIC_VERSION=v1.2.3; got:\n%s", dfStr)
	}
	if !contains(dfStr, "1234") {
		t.Errorf("Dockerfile missing HOST_UID=1234 reference; got:\n%s", dfStr)
	}

	// docker-compose.yml must have both bind mounts.
	compose, err := os.ReadFile(filepath.Join(dir, "docker-compose.yml"))
	if err != nil {
		t.Fatal(err)
	}
	composeStr := string(compose)
	if !contains(composeStr, "./tmp/workspace:/workspace") {
		t.Errorf("docker-compose.yml missing workspace bind mount; got:\n%s", composeStr)
	}
	if !contains(composeStr, "./tmp/claude-home:/home/atomic/.claude") {
		t.Errorf("docker-compose.yml missing claude-home bind mount; got:\n%s", composeStr)
	}
}

func TestInit_EntrypointPermissions(t *testing.T) {
	dir := t.TempDir()
	opts := dockerinit.Options{
		TargetDir:     dir,
		AtomicVersion: "v1.0.0",
		HostUID:       1000,
	}
	if _, err := dockerinit.Init(opts); err != nil {
		t.Fatalf("Init returned error: %v", err)
	}

	info, err := os.Stat(filepath.Join(dir, "docker-entrypoint.sh"))
	if err != nil {
		t.Fatal(err)
	}
	mode := info.Mode()
	// Must be executable by owner, group, and other (0755).
	if mode&0755 != 0755 {
		t.Errorf("docker-entrypoint.sh mode %04o, want at least 0755", mode)
	}
}

// contains is a helper so test bodies stay readable without importing strings.
func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		func() bool {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		}())
}
