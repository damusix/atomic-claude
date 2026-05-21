package config

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
)

// Run is the CLI entry point for `atomic config <verb> [args]`.
// home is the ~/.claude path (caller resolves it; Run does not call os.UserHomeDir).
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
		if err := writeResolved(home, cfg); err != nil {
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
		if err := writeResolved(home, cfg); err != nil {
			fmt.Fprintf(stderr, "atomic config unset: %v\n", err)
			return 1
		}
		return 0

	case "list":
		fs := flag.NewFlagSet("config-list", flag.ContinueOnError)
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

	default:
		fmt.Fprintf(stderr, "atomic config: unknown verb %q\n", verb)
		printConfigUsage(stderr)
		return 2
	}
}

// writeResolved renders cfg to the config.resolved.md file under home.
// Creates parent directories if needed.
func writeResolved(home string, cfg *Config) error {
	path := ResolvedPath(home)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(path), err)
	}
	return os.WriteFile(path, []byte(Render(cfg)), 0o644)
}

func printConfigUsage(w io.Writer) {
	fmt.Fprintln(w, "Usage: atomic config <get|set|unset|list|path> [args]")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "  get <key>           Print resolved value for key")
	fmt.Fprintln(w, "  set <key> <value>   Set key to value; writes config.toml and re-renders config.resolved.md")
	fmt.Fprintln(w, "  unset <key>         Remove key from config (reverts to built-in default)")
	fmt.Fprintln(w, "  list [--json]       Print all resolved key=value pairs")
	fmt.Fprintln(w, "  path                Print path to config.toml")
}
