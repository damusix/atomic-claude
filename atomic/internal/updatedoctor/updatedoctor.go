// Package updatedoctor runs a scoped atomic doctor check after a successful
// self-update. It lives above both the selfupdate and doctor packages so
// neither imports the other.
package updatedoctor

import (
	"fmt"
	"io"

	"github.com/damusix/atomic-claude/atomic/internal/doctor"
)

// RunDoctorFn matches doctor.Run's signature. Injected for tests.
type RunDoctorFn func(doctor.Opts) ([]doctor.Result, error)

// Run executes doctor with the post-update skip-set (signals=3, binary=8),
// recovers panics, and prints FAIL-only lines to w.
// WARN and SKIP results are suppressed unconditionally.
// Never returns an error — update success is always preserved.
func Run(runDoctor RunDoctorFn, w io.Writer) {
	results, err := safeRunDoctor(runDoctor)
	if err != nil {
		fmt.Fprintf(w, "doctor self-check failed: %v\n", err)
		return
	}

	for _, r := range results {
		if r.Severity == doctor.FAIL {
			fmt.Fprint(w, doctor.FormatResultLine(r))
		}
	}
}

// safeRunDoctor calls run with the skip set and recovers any panics.
func safeRunDoctor(run RunDoctorFn) (results []doctor.Result, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v", r)
		}
	}()
	return run(doctor.Opts{Skip: []int{3, 8}})
}
