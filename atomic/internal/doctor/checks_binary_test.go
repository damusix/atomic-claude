package doctor_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/doctor"
)

// On the prerelease channel the tip is routinely semver-lower than what is
// running, so the stable wording ("current < latest available") would state a
// relationship that is false.
func TestCheckBinary_prereleaseSidewaysMoveAvoidsLessThanWording(t *testing.T) {
	r := doctor.RunCheckBinaryWith(func(_ string) (bool, string, error) {
		return true, "1.7.0-next.4", nil
	}, "1.7.0", "prerelease")

	if r.Severity != doctor.WARN {
		t.Fatalf("severity = %q, want WARN; detail: %s", r.Severity, r.Detail)
	}
	if strings.Contains(r.Detail, "<") {
		t.Errorf("detail must not claim current < latest for a lower tip, got: %s", r.Detail)
	}
	if !strings.Contains(r.Detail, "1.7.0-next.4") || !strings.Contains(r.Detail, "prerelease") {
		t.Errorf("detail should name the tip and the channel, got: %s", r.Detail)
	}
}

// A genuine forward move on the prerelease channel keeps the ordering wording.
func TestCheckBinary_prereleaseForwardMoveKeepsLessThanWording(t *testing.T) {
	r := doctor.RunCheckBinaryWith(func(_ string) (bool, string, error) {
		return true, "1.7.0-next.5", nil
	}, "1.7.0-next.4", "prerelease")

	if r.Severity != doctor.WARN {
		t.Fatalf("severity = %q, want WARN; detail: %s", r.Severity, r.Detail)
	}
	if !strings.Contains(r.Detail, "<") {
		t.Errorf("a forward move should read as an ordering, got: %s", r.Detail)
	}
}

func TestCheckBinary_pass(t *testing.T) {
	r := doctor.RunCheckBinaryWith(func(_ string) (bool, string, error) {
		return false, "v1.0.0", nil
	}, "v1.0.0", "stable")

	if r.Severity != doctor.PASS {
		t.Errorf("severity = %q, want PASS; detail: %s", r.Severity, r.Detail)
	}
	if r.Detail == "" {
		t.Error("Detail is empty")
	}
}

func TestCheckBinary_warn_newer(t *testing.T) {
	r := doctor.RunCheckBinaryWith(func(_ string) (bool, string, error) {
		return true, "v2.0.0", nil
	}, "v1.0.0", "stable")

	if r.Severity != doctor.WARN {
		t.Errorf("severity = %q, want WARN; detail: %s", r.Severity, r.Detail)
	}
}

// An offline machine must not break the doctor run, so this stays WARN.
func TestCheckBinary_warn_network_error(t *testing.T) {
	r := doctor.RunCheckBinaryWith(func(_ string) (bool, string, error) {
		return false, "", errors.New("connection refused")
	}, "v1.0.0", "stable")

	if r.Severity != doctor.WARN {
		t.Errorf("severity = %q, want WARN; detail: %s", r.Severity, r.Detail)
	}
	if r.Detail == "" {
		t.Error("Detail is empty on network error")
	}
}
