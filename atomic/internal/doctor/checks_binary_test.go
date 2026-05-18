package doctor_test

import (
	"errors"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/doctor"
)

// TestCheckBinary_pass: up-to-date → PASS.
func TestCheckBinary_pass(t *testing.T) {
	r := doctor.RunCheckBinaryWith(func(_ string) (bool, string, error) {
		// not newer, same version
		return false, "v1.0.0", nil
	}, "v1.0.0")

	if r.Severity != doctor.PASS {
		t.Errorf("severity = %q, want PASS; detail: %s", r.Severity, r.Detail)
	}
	if r.Detail == "" {
		t.Error("Detail is empty")
	}
}

// TestCheckBinary_warn_newer: newer version available → WARN.
func TestCheckBinary_warn_newer(t *testing.T) {
	r := doctor.RunCheckBinaryWith(func(_ string) (bool, string, error) {
		return true, "v2.0.0", nil
	}, "v1.0.0")

	if r.Severity != doctor.WARN {
		t.Errorf("severity = %q, want WARN; detail: %s", r.Severity, r.Detail)
	}
}

// TestCheckBinary_warn_network_error: network failure → WARN (not FAIL;
// offline machines must not break doctor).
func TestCheckBinary_warn_network_error(t *testing.T) {
	r := doctor.RunCheckBinaryWith(func(_ string) (bool, string, error) {
		return false, "", errors.New("connection refused")
	}, "v1.0.0")

	if r.Severity != doctor.WARN {
		t.Errorf("severity = %q, want WARN; detail: %s", r.Severity, r.Detail)
	}
	if r.Detail == "" {
		t.Error("Detail is empty on network error")
	}
}
