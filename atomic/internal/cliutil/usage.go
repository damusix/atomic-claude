// Package cliutil provides shared utilities for the atomic CLI.
package cliutil

import (
	"flag"
	"fmt"
	"strings"
)

// SetUsage renders flags in double-dash form, matching atomic's documented
// convention rather than Go's single-dash PrintDefaults output.
func SetUsage(fs *flag.FlagSet, usageLine string) {
	fs.Usage = func() {
		w := fs.Output()
		if usageLine != "" {
			fmt.Fprintf(w, "Usage: %s\n\n", usageLine)
		}

		// Count registered flags. A flag with an empty Usage is a same-value
		// alias for another flag (e.g. -o for --out) and is deliberately
		// left out of the listing; only its Usage line names it.
		var count int
		fs.VisitAll(func(f *flag.Flag) {
			if f.Usage != "" {
				count++
			}
		})
		if count == 0 {
			return
		}

		fmt.Fprintf(w, "Options:\n")
		fs.VisitAll(func(f *flag.Flag) {
			if f.Usage == "" {
				return
			}
			line := fmt.Sprintf("  --%s  %s", f.Name, f.Usage)
			if f.DefValue != "" && f.DefValue != "false" && !strings.Contains(f.Usage, "(default") {
				line += fmt.Sprintf(" (default %s)", f.DefValue)
			}
			fmt.Fprintln(w, line)
		})
	}
}
