// Package cliutil provides shared utilities for the atomic CLI.
package cliutil

import (
	"flag"
	"fmt"
	"strings"
)

// SetUsage installs a custom Usage on fs that renders every registered flag in
// double-dash (--name) form, matching atomic's documented convention, instead
// of Go's default single-dash PrintDefaults output. usageLine is the one-line
// invocation summary printed after "Usage: " (e.g. "atomic update [options]").
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
