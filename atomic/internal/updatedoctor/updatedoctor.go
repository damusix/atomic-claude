// Package updatedoctor runs a scoped doctor check after a successful
// self-update. It sits above selfupdate and doctor so neither imports the other.
package updatedoctor

import (
	"fmt"
	"io"

	"github.com/damusix/atomic-claude/atomic/internal/doctor"
)

type RunDoctorFn func(doctor.Opts) ([]doctor.Result, error)

// Run prints FAIL lines only, suppressing WARN and SKIP, and never returns an
// error or panics out: a doctor problem must not turn a successful update into
// a failed one.
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

func safeRunDoctor(run RunDoctorFn) (results []doctor.Result, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v", r)
		}
	}()
	return run(doctor.Opts{Skip: []int{3, 8}})
}
