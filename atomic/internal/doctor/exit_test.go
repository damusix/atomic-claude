package doctor_test

import (
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/doctor"
)

// TestExitCodeTable covers the spec table:
// 0 = all PASS, 0 = mix PASS/WARN, 0 = all SKIP, 1 = any FAIL.
func TestExitCodeTable(t *testing.T) {
	cases := []struct {
		name     string
		results  []doctor.Result
		wantCode int
	}{
		{
			name:     "all PASS → 0",
			results:  []doctor.Result{{Severity: doctor.PASS}, {Severity: doctor.PASS}},
			wantCode: 0,
		},
		{
			name:     "mix PASS/WARN → 0",
			results:  []doctor.Result{{Severity: doctor.PASS}, {Severity: doctor.WARN}},
			wantCode: 0,
		},
		{
			name:     "all SKIP → 0",
			results:  []doctor.Result{{Severity: doctor.SKIP}},
			wantCode: 0,
		},
		{
			name:     "one FAIL → 1",
			results:  []doctor.Result{{Severity: doctor.PASS}, {Severity: doctor.FAIL}},
			wantCode: 1,
		},
		{
			name:     "multiple FAIL → 1",
			results:  []doctor.Result{{Severity: doctor.FAIL}, {Severity: doctor.FAIL}},
			wantCode: 1,
		},
		{
			name:     "WARN + FAIL → 1",
			results:  []doctor.Result{{Severity: doctor.WARN}, {Severity: doctor.FAIL}},
			wantCode: 1,
		},
		{
			name:     "empty results → 0",
			results:  []doctor.Result{},
			wantCode: 0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := doctor.ExitCode(tc.results)
			if got != tc.wantCode {
				t.Errorf("ExitCode = %d, want %d", got, tc.wantCode)
			}
		})
	}
}
