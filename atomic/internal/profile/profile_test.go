package profile_test

import (
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/profile"
)

// TestCaptureEnv_HasRequiredFields verifies that CaptureEnv returns a non-nil
// Env with the runtime fields always populated (GOOS, GOARCH, NumCPU are
// always available; git fields may be empty but must not panic).
func TestCaptureEnv_HasRequiredFields(t *testing.T) {
	e := profile.CaptureEnv()

	if e.GOOS == "" {
		t.Error("GOOS must not be empty")
	}
	if e.GOARCH == "" {
		t.Error("GOARCH must not be empty")
	}
	if e.NumCPU <= 0 {
		t.Errorf("NumCPU = %d, want > 0", e.NumCPU)
	}
	// GitUserName and GitUserEmail may be empty (git may not be installed in CI),
	// but must not cause a panic — the test reaching this line proves that.
}

// TestRenderStub_ContainsAllSections verifies that the stub markdown contains
// the six required section headings from the schema contract.
func TestRenderStub_ContainsAllSections(t *testing.T) {
	e := profile.CaptureEnv()
	stub := profile.RenderStub(e)

	sections := []string{
		"## Identity",
		"## Work",
		"## Active projects",
		"## Interests",
		"## People mentioned",
		"## Environment",
	}
	for _, s := range sections {
		if !strings.Contains(stub, s) {
			t.Errorf("stub missing section %q", s)
		}
	}
}

// TestRenderStub_ContainsXMLTags verifies the volatility tags are present.
func TestRenderStub_ContainsXMLTags(t *testing.T) {
	e := profile.CaptureEnv()
	stub := profile.RenderStub(e)

	tags := []string{"<stable>", "</stable>", "<volatile>", "</volatile>", "<deterministic>", "</deterministic>"}
	for _, tag := range tags {
		if !strings.Contains(stub, tag) {
			t.Errorf("stub missing tag %q", tag)
		}
	}
}

// TestRenderStub_EnvFieldsPopulated verifies deterministic env fields are
// filled in the Environment section with the values from CaptureEnv.
func TestRenderStub_EnvFieldsPopulated(t *testing.T) {
	e := profile.CaptureEnv()
	// Override with known values so we can assert exact output.
	e.GOOS = "testOS"
	e.GOARCH = "testARCH"
	e.NumCPU = 42
	e.GitUserName = "Test User"
	e.GitUserEmail = "test@example.com"

	stub := profile.RenderStub(e)

	checks := []string{
		"testOS",
		"testARCH",
		"42",
		"Test User",
		"test@example.com",
	}
	for _, c := range checks {
		if !strings.Contains(stub, c) {
			t.Errorf("stub does not contain %q\nstub:\n%s", c, stub)
		}
	}
}

// TestRenderStub_EmptyGitFields verifies that empty git config values produce
// empty field entries rather than panicking or crashing.
func TestRenderStub_EmptyGitFields(t *testing.T) {
	e := profile.Env{
		GOOS:         "linux",
		GOARCH:       "amd64",
		NumCPU:       4,
		GitUserName:  "",
		GitUserEmail: "",
	}
	stub := profile.RenderStub(e)

	// Must contain the field labels even when values are empty.
	if !strings.Contains(stub, "Git user.name:") {
		t.Error("stub missing 'Git user.name:' label")
	}
	if !strings.Contains(stub, "Git user.email:") {
		t.Error("stub missing 'Git user.email:' label")
	}
}

// TestRenderStub_HasH1Title verifies the stub begins with the H1 title.
func TestRenderStub_HasH1Title(t *testing.T) {
	e := profile.CaptureEnv()
	stub := profile.RenderStub(e)

	if !strings.HasPrefix(stub, "# User profile\n") {
		t.Errorf("stub does not start with '# User profile\\n', got: %q", stub[:min(40, len(stub))])
	}
}
