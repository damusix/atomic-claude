package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/damusix/atomic-claude/atomic/internal/version"
)

func main() {
	fs := flag.NewFlagSet("atomic", flag.ContinueOnError)
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: atomic [flags] <command> [args]\n\n")
		fmt.Fprintf(os.Stderr, "Commands:\n")
		fmt.Fprintf(os.Stderr, "  (none implemented yet — coming in CP-2+)\n\n")
		fmt.Fprintf(os.Stderr, "Flags:\n")
		fs.PrintDefaults()
	}

	var showVersion bool
	fs.BoolVar(&showVersion, "version", false, "print version and exit")
	fs.BoolVar(&showVersion, "v", false, "print version and exit (short)")

	if err := fs.Parse(os.Args[1:]); err != nil {
		os.Exit(2)
	}

	if showVersion {
		fmt.Printf("atomic %s (%s)\n", version.Version, version.Commit)
		return
	}

	args := fs.Args()
	if len(args) == 0 {
		fs.Usage()
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "atomic: unknown command %q (not implemented yet)\n", args[0])
	os.Exit(1)
}
