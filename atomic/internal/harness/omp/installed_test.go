package omp

import (
	"errors"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/harness"
)

// An unresolvable default profile is a soft "not installed" the caller words
// and prints; the adapter itself never writes to stderr.
func TestInstalledReportsUnresolvableRootAsNotInstalled(t *testing.T) {
	a := New()
	a.AmbientProfile = func() string { return "" }
	a.ConfigPath = func(string, string) (string, error) { return "", errors.New("omp exploded") }

	instances, err := a.Installed(t.TempDir())
	if !errors.Is(err, harness.ErrNotInstalled) {
		t.Fatalf("err = %v, want harness.ErrNotInstalled", err)
	}
	if len(instances) != 0 {
		t.Errorf("instances = %v, want none", instances)
	}
}
