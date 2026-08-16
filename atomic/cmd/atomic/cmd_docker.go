package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/damusix/atomic-claude/atomic/internal/cliutil"
	"github.com/damusix/atomic-claude/atomic/internal/dockerinit"
	"github.com/damusix/atomic-claude/atomic/internal/version"
	"github.com/spf13/cobra"
)

// buildDockerCmd builds the "docker" parent + init child.
func buildDockerCmd() *cobra.Command {
	parent := &cobra.Command{
		Use:   "docker",
		Short: "Docker eval environment scaffolding (init)",
		Args:  cobra.ArbitraryArgs,
		RunE:  func(cmd *cobra.Command, args []string) error { runDocker(args); return nil },
	}
	initCmd := &cobra.Command{
		Use:                "init",
		Short:              "Scaffold Docker eval environment",
		Annotations:        map[string]string{"args_hint": ""},
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			runDocker(append([]string{"init"}, args...))
			return nil
		},
	}
	initCmd.Flags().String("target", "", "target directory for scaffolded files")
	initCmd.Flags().Bool("force", false, "overwrite existing files")
	parent.AddCommand(initCmd)
	return parent
}

func runDocker(args []string) {
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "Usage: atomic docker <init> [flags]\n")
		os.Exit(2)
	}

	verb := args[0]
	switch verb {
	case "init":
		fs := flag.NewFlagSet("docker init", flag.ContinueOnError)
		cliutil.SetUsage(fs, "atomic docker init [--target <dir>] [--force]")
		var target string
		var force bool
		fs.StringVar(&target, "target", "./atomic-docker", "target directory for scaffolded files")
		fs.BoolVar(&force, "force", false, "overwrite existing files")
		if err := fs.Parse(args[1:]); err != nil {
			os.Exit(2)
		}

		absTarget, err := filepath.Abs(target)
		if err != nil {
			fmt.Fprintf(os.Stderr, "atomic docker init: resolve target: %v\n", err)
			os.Exit(1)
		}

		if version.Version == "dev" {
			fmt.Fprintf(os.Stderr, "warning: atomic version is \"dev\" — generated Dockerfile pins ATOMIC_VERSION=dev which will fail at docker build. Use a released atomic binary or override with --version later.\n")
		}

		opts := dockerinit.Options{
			TargetDir:     absTarget,
			Force:         force,
			AtomicVersion: version.Version,
			HostUID:       os.Getuid(),
		}

		actions, err := dockerinit.Init(opts)
		if err != nil {
			fmt.Fprintf(os.Stderr, "atomic docker init: %v\n", err)
			os.Exit(1)
		}

		for _, a := range actions {
			fmt.Printf("%-12s %s\n", string(a.Kind), a.Path)
		}

	default:
		fmt.Fprintf(os.Stderr, "atomic docker: unknown subcommand %q\n", verb)
		fmt.Fprintf(os.Stderr, "Usage: atomic docker <init> [flags]\n")
		os.Exit(2)
	}
}
