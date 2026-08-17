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

		// Count registered flags.
		var count int
		fs.VisitAll(func(*flag.Flag) { count++ })
		if count == 0 {
			return
		}

		fmt.Fprintf(w, "Options:\n")
		fs.VisitAll(func(f *flag.Flag) {
			line := fmt.Sprintf("  --%s  %s", f.Name, f.Usage)
			if f.DefValue != "" && f.DefValue != "false" && !strings.Contains(f.Usage, "(default") {
				line += fmt.Sprintf(" (default %s)", f.DefValue)
			}
			fmt.Fprintln(w, line)
		})
	}
}
