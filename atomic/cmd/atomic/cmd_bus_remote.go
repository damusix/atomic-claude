package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/damusix/atomic-claude/atomic/internal/bus"
	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/prompt"
	"github.com/spf13/cobra"
)

// busRemoteFields is what `bus remote add` collects from flags, then the form.
type busRemoteFields struct {
	Name string
	config.BusRemote
}

// busRemoteIsTTY is a variable so tests can force either branch.
var busRemoteIsTTY = prompt.IsInteractive

// busRemoteForm fills the missing fields of f interactively. A variable so
// tests can stand in for the TTY.
var busRemoteForm = func(f *busRemoteFields, home string, taken func(string) bool) error {
	return huh.NewForm(huh.NewGroup(
		huh.NewInput().Title("Name").Description("used as --host <name>").Value(&f.Name).
			Validate(func(s string) error {
				if err := config.ValidateBusRemoteName(s); err != nil {
					return err
				}
				if taken(s) {
					return fmt.Errorf("%q already exists; rerun with --force to replace it", s)
				}
				return nil
			}),
		huh.NewInput().Title("Host").Description("gateway URL, e.g. https://bus.example.com or http://10.0.0.5:7777").
			Value(&f.Host).Validate(config.ValidateBusRemoteHost),
		huh.NewInput().Title("Key").Description("the key `atomic bus gateway enroll` printed").
			EchoMode(huh.EchoModePassword).Value(&f.Key).Validate(config.ValidateBusRemoteKey),
		huh.NewInput().Title("CA file (optional)").Description("PEM for a private certificate; blank uses the system trust store").
			Value(&f.CA).Validate(func(s string) error { return config.ValidateBusRemoteCA(s, home) }),
	)).Run()
}

func buildBusRemoteCmd() *cobra.Command {
	rc := &cobra.Command{
		Use:                "remote",
		Short:              "Manage the gateways this machine can reach with --host (add|list|test|remove)",
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
				return cmd.Help()
			}
			fmt.Fprintf(os.Stderr, "atomic bus remote: unknown verb %q\nUsage: atomic bus remote <add|list|test|remove> [args]\n", args[0])
			os.Exit(int(bus.ExitUsage))
			return nil
		},
	}
	add := &cobra.Command{
		Use:                "add",
		Short:              "Save a gateway under a name; prompts for missing fields on a terminal",
		Annotations:        map[string]string{"args_hint": "[<name>]"},
		DisableFlagParsing: true,
		RunE:               func(cmd *cobra.Command, args []string) error { runBusRemote(busRemoteAddAction, args); return nil },
	}
	add.Flags().String("host", "", "gateway URL; a bare host is dialed as https")
	add.Flags().String("key", "", "the hex key `atomic bus gateway enroll` printed")
	add.Flags().String("ca", "", "PEM file for a private certificate (optional)")
	add.Flags().Bool("force", false, "replace an existing remote of the same name")
	rc.AddCommand(add)

	list := &cobra.Command{
		Use:                "list",
		Short:              "List saved gateways (keys are never printed)",
		DisableFlagParsing: true,
		RunE:               func(cmd *cobra.Command, args []string) error { runBusRemote(busRemoteListAction, args); return nil },
	}
	list.Flags().Bool("json", false, "emit JSON")
	rc.AddCommand(list)

	rc.AddCommand(&cobra.Command{
		Use:                "test",
		Short:              "Send one authenticated ping to a saved gateway, or to every one",
		Annotations:        map[string]string{"args_hint": "[<name>]"},
		DisableFlagParsing: true,
		RunE:               func(cmd *cobra.Command, args []string) error { runBusRemote(busRemoteTestAction, args); return nil },
	})
	rc.AddCommand(&cobra.Command{
		Use:                "remove",
		Short:              "Delete a saved gateway",
		Annotations:        map[string]string{"args_hint": "<name>"},
		DisableFlagParsing: true,
		RunE:               func(cmd *cobra.Command, args []string) error { runBusRemote(busRemoteRemoveAction, args); return nil },
	})
	return rc
}

func runBusRemote(action func([]string, string, io.Writer, io.Writer) int, args []string) {
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic bus remote: resolve home dir: %v\n", err)
		os.Exit(2)
	}
	os.Exit(action(args, home, os.Stdout, os.Stderr))
}

func busRemoteAddAction(args []string, home string, out, errOut io.Writer) int {
	const usage = "Usage: atomic bus remote add [<name>] [--host <url>] [--key <hex>] [--ca <file>] [--force]\n"
	fs := flag.NewFlagSet("bus-remote-add", flag.ContinueOnError)
	fs.SetOutput(errOut)
	var f busRemoteFields
	var force bool
	fs.StringVar(&f.Host, "host", "", "gateway URL")
	fs.StringVar(&f.Key, "key", "", "hex key from enroll")
	fs.StringVar(&f.CA, "ca", "", "PEM file for a private certificate")
	fs.BoolVar(&force, "force", false, "replace an existing remote")
	positional, err := bus.ParseFlags(fs, args)
	if err != nil {
		return int(bus.ExitUsage)
	}
	if len(positional) > 1 {
		fmt.Fprint(errOut, usage)
		return int(bus.ExitUsage)
	}
	if len(positional) == 1 {
		f.Name = positional[0]
	}

	path := config.TOMLPath(home)
	cfg, _, err := config.Load(path)
	if err != nil {
		fmt.Fprintf(errOut, "atomic bus remote add: %v\n", err)
		return int(bus.ExitHard)
	}

	if f.Name == "" || f.Host == "" || f.Key == "" {
		if !busRemoteIsTTY() {
			fmt.Fprint(errOut, usage)
			fmt.Fprintln(errOut, "name, --host, and --key are required without a terminal")
			return int(bus.ExitUsage)
		}
		taken := func(name string) bool {
			_, ok := cfg.Bus.Remotes[name]
			return ok && !force
		}
		if err := busRemoteForm(&f, home, taken); err != nil {
			if errors.Is(err, huh.ErrUserAborted) {
				fmt.Fprintln(errOut, "atomic bus remote add: aborted")
			} else {
				fmt.Fprintf(errOut, "atomic bus remote add: %v\n", err)
			}
			return int(bus.ExitHard)
		}
	}

	// The bus dials from any cwd, so a relative CA path must not stay relative.
	if f.CA != "" && !strings.HasPrefix(f.CA, "~") && !filepath.IsAbs(f.CA) {
		abs, err := filepath.Abs(f.CA)
		if err != nil {
			fmt.Fprintf(errOut, "atomic bus remote add: ca: %v\n", err)
			return int(bus.ExitUsage)
		}
		f.CA = abs
	}
	if err := config.ValidateBusRemote(f.Name, f.BusRemote, home); err != nil {
		fmt.Fprintf(errOut, "atomic bus remote add: %v\n", err)
		return int(bus.ExitUsage)
	}
	if err := config.AddBusRemote(cfg, f.Name, f.BusRemote, force); err != nil {
		fmt.Fprintf(errOut, "atomic bus remote add: %v\n", err)
		return int(bus.ExitNameTaken)
	}
	if err := config.WritePersist(path, cfg); err != nil {
		fmt.Fprintf(errOut, "atomic bus remote add: %v\n", err)
		return int(bus.ExitHard)
	}
	fmt.Fprintf(out, "added %s (%s)\ncheck it: atomic bus remote test %s\n", f.Name, f.Host, f.Name)
	return int(bus.ExitOK)
}

func busRemoteListAction(args []string, home string, out, errOut io.Writer) int {
	fs := flag.NewFlagSet("bus-remote-list", flag.ContinueOnError)
	fs.SetOutput(errOut)
	var asJSON bool
	fs.BoolVar(&asJSON, "json", false, "emit JSON")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		fmt.Fprintln(errOut, "Usage: atomic bus remote list [--json]")
		return int(bus.ExitUsage)
	}
	cfg, _, err := config.Load(config.TOMLPath(home))
	if err != nil {
		fmt.Fprintf(errOut, "atomic bus remote list: %v\n", err)
		return int(bus.ExitHard)
	}

	type row struct {
		Name string `json:"name"`
		Host string `json:"host"`
		CA   string `json:"ca,omitempty"`
	}
	rows := make([]row, 0, len(cfg.Bus.Remotes))
	for name, r := range cfg.Bus.Remotes {
		rows = append(rows, row{Name: name, Host: r.Host, CA: r.CA})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Name < rows[j].Name })

	if asJSON {
		data, err := json.Marshal(rows)
		if err != nil {
			fmt.Fprintf(errOut, "atomic bus remote list: %v\n", err)
			return int(bus.ExitHard)
		}
		fmt.Fprintln(out, string(data))
		return int(bus.ExitOK)
	}
	if len(rows) == 0 {
		fmt.Fprintln(out, "no remotes; add one with: atomic bus remote add")
		return int(bus.ExitOK)
	}
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	for _, r := range rows {
		fmt.Fprintf(tw, "%s\t%s\t%s\n", r.Name, r.Host, r.CA)
	}
	tw.Flush()
	return int(bus.ExitOK)
}

// busRemoteTestTimeout bounds each probe so one unresponsive host cannot
// stall a test of every remote.
const busRemoteTestTimeout = 5 * time.Second

func busRemoteTestAction(args []string, home string, out, errOut io.Writer) int {
	fs := flag.NewFlagSet("bus-remote-test", flag.ContinueOnError)
	fs.SetOutput(errOut)
	if err := fs.Parse(args); err != nil || fs.NArg() > 1 {
		fmt.Fprintln(errOut, "Usage: atomic bus remote test [<name>]")
		return int(bus.ExitUsage)
	}
	cfg, _, err := config.Load(config.TOMLPath(home))
	if err != nil {
		fmt.Fprintf(errOut, "atomic bus remote test: %v\n", err)
		return int(bus.ExitHard)
	}

	var names []string
	if fs.NArg() == 1 {
		if _, ok := cfg.Bus.Remotes[fs.Arg(0)]; !ok {
			fmt.Fprintf(errOut, "atomic bus remote test: %q: %v; see atomic bus remote list\n", fs.Arg(0), config.ErrBusRemoteNotFound)
			return int(bus.ExitUsage)
		}
		names = []string{fs.Arg(0)}
	} else {
		for name := range cfg.Bus.Remotes {
			names = append(names, name)
		}
		sort.Strings(names)
	}
	if len(names) == 0 {
		fmt.Fprintln(errOut, "atomic bus remote test: no remotes; add one with: atomic bus remote add")
		return int(bus.ExitUsage)
	}

	code := int(bus.ExitOK)
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	for _, name := range names {
		result := "ok"
		if _, err := bus.DoRemoteTimeout(home, name, bus.Request{Op: bus.OpPing}, busRemoteTestTimeout); err != nil {
			result = "FAIL " + err.Error()
			code = int(bus.ExitUnreachable)
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\n", name, cfg.Bus.Remotes[name].Host, result)
	}
	tw.Flush()
	return code
}

func busRemoteRemoveAction(args []string, home string, out, errOut io.Writer) int {
	fs := flag.NewFlagSet("bus-remote-remove", flag.ContinueOnError)
	fs.SetOutput(errOut)
	if err := fs.Parse(args); err != nil || fs.NArg() != 1 || fs.Arg(0) == "" {
		fmt.Fprintln(errOut, "Usage: atomic bus remote remove <name>")
		return int(bus.ExitUsage)
	}
	args = fs.Args()
	path := config.TOMLPath(home)
	cfg, _, err := config.Load(path)
	if err != nil {
		fmt.Fprintf(errOut, "atomic bus remote remove: %v\n", err)
		return int(bus.ExitHard)
	}
	if err := config.RemoveBusRemote(cfg, args[0]); err != nil {
		fmt.Fprintf(errOut, "atomic bus remote remove: %v\n", err)
		return int(bus.ExitUsage)
	}
	if err := config.WritePersist(path, cfg); err != nil {
		fmt.Fprintf(errOut, "atomic bus remote remove: %v\n", err)
		return int(bus.ExitHard)
	}
	fmt.Fprintf(out, "removed %s\n", args[0])
	return int(bus.ExitOK)
}
