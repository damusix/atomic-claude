package config

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

	"github.com/damusix/atomic-claude/atomic/internal/cliutil"
)

// ApplyAgentsHook re-patches already-installed agent files with the current
// [claude.agents] overrides after `atomic config agents` saves. nil in production
// until cmd/atomic wires it to claudeinstall.ReapplyAgents at startup — this
// seam exists because internal/config must not import internal/claudeinstall
// (claudeinstall already imports config, which would be a cycle). changed is
// the list of agent basenames rewritten; installed is how many configured
// agents were already present on disk.
var ApplyAgentsHook func(home string) (changed []string, installed int, err error)

// Run is the CLI entry point for `atomic config <verb> [args]`.
// home is the user's home directory (caller resolves it; Run does not call os.UserHomeDir).
// Returns an exit code: 0 success, 1 error, 2 usage error.
func Run(args []string, home string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		printConfigUsage(stderr)
		return 2
	}

	verb := args[0]
	rest := args[1:]

	switch verb {
	case "path":
		fmt.Fprintln(stdout, TOMLPath(home))
		return 0

	case "get":
		if len(rest) < 1 {
			fmt.Fprintf(stderr, "Usage: atomic config get <key>\n")
			return 2
		}
		key := rest[0]
		cfg, _, err := Load(TOMLPath(home))
		if err != nil {
			fmt.Fprintf(stderr, "atomic config get: %v\n", err)
			return 1
		}
		val, err := Get(cfg, key)
		if err != nil {
			fmt.Fprintf(stderr, "atomic config get: %v\n", err)
			return 1
		}
		fmt.Fprintln(stdout, val)
		return 0

	case "set":
		if len(rest) < 2 {
			fmt.Fprintf(stderr, "Usage: atomic config set <key> <value>\n")
			return 2
		}
		key, value := rest[0], rest[1]
		cfg, _, err := Load(TOMLPath(home))
		if err != nil {
			fmt.Fprintf(stderr, "atomic config set: %v\n", err)
			return 1
		}
		if err := Set(cfg, key, value); err != nil {
			fmt.Fprintf(stderr, "atomic config set: %v\n", err)
			return 1
		}
		if err := WritePersist(TOMLPath(home), cfg); err != nil {
			fmt.Fprintf(stderr, "atomic config set: %v\n", err)
			return 1
		}
		return 0

	case "unset":
		if len(rest) < 1 {
			fmt.Fprintf(stderr, "Usage: atomic config unset <key>\n")
			return 2
		}
		key := rest[0]
		cfg, _, err := Load(TOMLPath(home))
		if err != nil {
			fmt.Fprintf(stderr, "atomic config unset: %v\n", err)
			return 1
		}
		if err := Unset(cfg, key); err != nil {
			fmt.Fprintf(stderr, "atomic config unset: %v\n", err)
			return 1
		}
		if err := WritePersist(TOMLPath(home), cfg); err != nil {
			fmt.Fprintf(stderr, "atomic config unset: %v\n", err)
			return 1
		}
		return 0

	case "list":
		fs := flag.NewFlagSet("config-list", flag.ContinueOnError)
		cliutil.SetUsage(fs, "atomic config list [--json]")
		fs.SetOutput(stderr)
		var asJSON bool
		fs.BoolVar(&asJSON, "json", false, "print as JSON object")
		if err := fs.Parse(rest); err != nil {
			return 2
		}

		cfg, _, err := Load(TOMLPath(home))
		if err != nil {
			fmt.Fprintf(stderr, "atomic config list: %v\n", err)
			return 1
		}
		m := Resolved(cfg)

		if asJSON {
			data, err := json.Marshal(m)
			if err != nil {
				fmt.Fprintf(stderr, "atomic config list: marshal json: %v\n", err)
				return 1
			}
			fmt.Fprintln(stdout, string(data))
			return 0
		}

		// Human-readable: sorted key=value lines.
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Fprintf(stdout, "%s=%s\n", k, m[k])
		}
		return 0

	case "resolve":
		fs := flag.NewFlagSet("config-resolve", flag.ContinueOnError)
		cliutil.SetUsage(fs, "atomic config resolve --repo <root> --json")
		fs.SetOutput(stderr)
		var repoRoot string
		var asJSON bool
		fs.StringVar(&repoRoot, "repo", "", "repository root")
		fs.BoolVar(&asJSON, "json", false, "print as JSON object")
		if err := fs.Parse(rest); err != nil {
			return 2
		}
		if repoRoot == "" || !asJSON || fs.NArg() != 0 {
			fmt.Fprintln(stderr, "Usage: atomic config resolve --repo <root> --json")
			return 2
		}
		repoAbs, err := filepath.Abs(repoRoot)
		if err != nil {
			fmt.Fprintf(stderr, "atomic config resolve: repo path: %v\n", err)
			return 1
		}
		info, err := os.Stat(repoAbs)
		if err != nil {
			fmt.Fprintf(stderr, "atomic config resolve: repo path: %v\n", err)
			return 1
		}
		if !info.IsDir() {
			fmt.Fprintf(stderr, "atomic config resolve: repo path is not a directory: %s\n", repoAbs)
			return 1
		}
		env := ResolvePiAgents(TOMLPath(home), filepath.Join(repoAbs, ".pi", "atomic.toml"))
		data, err := json.Marshal(env)
		if err != nil {
			fmt.Fprintf(stderr, "atomic config resolve: marshal json: %v\n", err)
			return 1
		}
		fmt.Fprintln(stdout, string(data))
		if !env.Valid {
			return 1
		}
		return 0

	case "agents":
		cfg, _, err := Load(TOMLPath(home))
		if err != nil {
			fmt.Fprintf(stderr, "atomic config agents: load config: %v\n", err)
			return 1
		}
		selections, err := AgentTierSelector(cfg)
		if err != nil {
			if errors.Is(err, ErrNonInteractiveAgents) {
				fmt.Fprintln(stderr, "atomic config agents: requires an interactive terminal")
				fmt.Fprintln(stderr, "Run `atomic config agents` in a terminal with stdin and stdout attached to a TTY.")
				return 1
			}
			if errors.Is(err, ErrAgentsAborted) {
				fmt.Fprintln(stderr, "atomic config agents: aborted")
				return 1
			}
			fmt.Fprintf(stderr, "atomic config agents: %v\n", err)
			return 1
		}
		if err := applyAgentOverrides(cfg, selections); err != nil {
			fmt.Fprintf(stderr, "atomic config agents: %v\n", err)
			return 1
		}
		if err := WritePersist(TOMLPath(home), cfg); err != nil {
			fmt.Fprintf(stderr, "atomic config agents: write config: %v\n", err)
			return 1
		}
		if ApplyAgentsHook == nil {
			fmt.Fprintln(stdout, "Agent tier overrides saved.")
			return 0
		}
		changed, installed, applyErr := ApplyAgentsHook(home)
		switch {
		case applyErr != nil:
			fmt.Fprintf(stderr, "atomic config agents: saved, but could not auto-apply to installed agents: %v\n", applyErr)
			fmt.Fprintln(stderr, "Run 'atomic claude install' to apply.")
		case installed == 0:
			fmt.Fprintln(stdout, "Saved. No installed agents found — will apply on the next 'atomic claude install'.")
		case len(changed) == 0:
			fmt.Fprintln(stdout, "Saved. Installed agents already up to date.")
		default:
			fmt.Fprintf(stdout, "Saved and applied to %d installed agent file(s): %s.\n", len(changed), strings.Join(changed, ", "))
			fmt.Fprintln(stdout, "Restart Claude Code sessions to pick up the change.")
		}
		return 0

	default:
		fmt.Fprintf(stderr, "atomic config: unknown verb %q\n", verb)
		printConfigUsage(stderr)
		return 2
	}
}

func printConfigUsage(w io.Writer) {
	fmt.Fprintln(w, "Usage: atomic config <get|set|unset|list|path|agents|resolve> [args]")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "  get <key>           Print resolved value for key")
	fmt.Fprintln(w, "  set <key> <value>   Set key to value; writes config.toml")
	fmt.Fprintln(w, "  unset <key>         Remove key from config (reverts to built-in default)")
	fmt.Fprintln(w, "  list [--json]       Print all resolved key=value pairs")
	fmt.Fprintln(w, "  path                Print path to config.toml")
	fmt.Fprintln(w, "  agents              Set per-agent model tiers interactively")
	fmt.Fprintln(w, "  resolve --repo <root> --json  Print resolved Pi agent overrides")
}
