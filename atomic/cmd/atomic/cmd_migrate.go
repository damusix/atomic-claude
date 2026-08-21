package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/cliutil"
	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/migrate"
	"github.com/damusix/atomic-claude/atomic/internal/prompt"
	"github.com/spf13/cobra"
)

// --repo here is a target repo path, not the root's persistent context
// override. The two never collide: DisableFlagParsing blocks flag merging at
// execute time, and the FlagSet predates AddCommand so lazy-init never merges.
func buildMigrateCmd() *cobra.Command {
	c := &cobra.Command{
		Use:                "migrate",
		Short:              "Run versioned atomic migrations",
		Annotations:        map[string]string{"args_hint": ""},
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			runMigrate(args)
			return nil
		},
	}
	c.Flags().String("repo", "", "run repo-scope migrations on this path")
	c.Flags().String("realm", "", "run install-scope + repo fan-out under this realm root")
	c.Flags().Bool("show-log", false, "print dated migration log entries, optionally since a version or date")
	return c
}

// runMigrate routes by flag:
//
//	atomic migrate                          → install-scope steps against ~/.claude
//	atomic migrate --repo <path>            → repo-scope steps on that repo
//	atomic migrate --realm <path>           → install-scope + fan-out to member repos
//	atomic migrate --show-log [<since>]     → print the migration log, newest first
func runMigrate(args []string) {
	fs := flag.NewFlagSet("migrate", flag.ContinueOnError)
	cliutil.SetUsage(fs, "atomic migrate [--repo <path>] [--realm <path>] [--show-log [<since>]]")
	var repoPath string
	var realmPath string
	var showLog bool
	fs.StringVar(&repoPath, "repo", "", "run repo-scope migrations on this path")
	fs.StringVar(&realmPath, "realm", "", "run install-scope + repo fan-out under this realm root")
	fs.BoolVar(&showLog, "show-log", false, "print dated migration log entries, optionally since a version or date")
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}
	// The optional <since> is a bare positional following the bool flag
	// (`--show-log 1.2.0`), not a `--show-log=1.2.0` value — Go's flag
	// package leaves it in fs.Args() rather than attaching it to the bool.
	// fs.Parse also stops consuming at the first positional, so a flag typed
	// after <since> (`--show-log 1.2.0 --repo x`) never reaches repoPath —
	// it lands unconsumed in fs.Args() instead of being silently dropped.
	remaining := fs.Args()
	var showLogSince string
	if showLog && len(remaining) > 0 {
		showLogSince = remaining[0]
		remaining = remaining[1:]
	}
	if showLog {
		// --show-log lists global log entries; a target repo or realm has no
		// bearing on that, so the combination is rejected rather than one
		// side winning silently.
		if repoPath != "" || realmPath != "" || len(remaining) > 0 {
			fmt.Fprintln(os.Stderr, "atomic migrate: --show-log cannot be combined with --repo or --realm")
			os.Exit(2)
		}
		out, err := migrate.FormatLog(migrate.Registry, showLogSince)
		if err != nil {
			fmt.Fprintf(os.Stderr, "atomic migrate: %v\n", err)
			os.Exit(1)
		}
		if out == "" {
			fmt.Println("atomic migrate: no log entries")
			return
		}
		fmt.Print(out)
		return
	}

	switch {
	case repoPath != "":
		absRepo, err := filepath.Abs(repoPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "atomic migrate: resolve --repo path: %v\n", err)
			os.Exit(1)
		}
		if err := migrateRepoAction(absRepo); err != nil {
			fmt.Fprintf(os.Stderr, "atomic migrate: %v\n", err)
			os.Exit(1)
		}

	case realmPath != "":
		absRealm, err := filepath.Abs(realmPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "atomic migrate: resolve --realm path: %v\n", err)
			os.Exit(1)
		}
		home, herr := os.UserHomeDir()
		if herr != nil {
			fmt.Fprintf(os.Stderr, "atomic migrate: resolve home dir: %v\n", herr)
			os.Exit(1)
		}
		if err := runMigrateInstall(home); err != nil {
			fmt.Fprintf(os.Stderr, "atomic migrate: install-scope: %v\n", err)
			os.Exit(1)
		}
		if err := runMigrateRealm(absRealm); err != nil {
			fmt.Fprintf(os.Stderr, "atomic migrate: realm: %v\n", err)
			os.Exit(1)
		}

	default:
		home, herr := os.UserHomeDir()
		if herr != nil {
			fmt.Fprintf(os.Stderr, "atomic migrate: resolve home dir: %v\n", herr)
			os.Exit(1)
		}
		if err := runMigrateInstall(home); err != nil {
			fmt.Fprintf(os.Stderr, "atomic migrate: %v\n", err)
			os.Exit(1)
		}
	}
}

// runMigrateInstall works across two roots: config.toml lives under
// <home>/.atomic, while migrate.Context.Root stays <home>/.claude, because
// install-scope steps operate on the Claude artifact tree, not the state root.
func runMigrateInstall(home string) error {
	cfgPath := config.TOMLPath(home)
	cfg, _, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	ctx := &migrate.Context{Root: filepath.Join(home, ".claude")}
	installSteps := scopedMigrations("install", migrate.Registry)
	newVer, err := migrate.Run(cfg.Install.Version, installSteps, ctx)
	if err != nil {
		return err
	}
	if newVer == cfg.Install.Version {
		return nil // no steps applied
	}
	cfg.Install.Version = newVer
	return config.WritePersist(cfgPath, cfg)
}

// migrateRepoAction is the testable seam for `atomic migrate --repo <path>`.
func migrateRepoAction(repoPath string) error {
	schema := migrate.ReadWikiSchema(repoPath)
	recorded := schemaToSemver(schema)
	ctx := &migrate.Context{Root: repoPath}
	repoSteps := scopedMigrations("repo", migrate.Registry)
	newVer, err := migrate.Run(recorded, repoSteps, ctx)
	if err != nil {
		return fmt.Errorf("repo %s: %w", repoPath, err)
	}
	newSchema := semverToSchema(newVer)
	if newSchema == schema {
		return nil // nothing changed
	}
	// No-op when docs/wiki/index.md does not exist, as in a no-signals repo
	// where the step ran but created no file.
	return migrate.WriteWikiSchema(repoPath, newSchema)
}

// realmConfirmFn is a seam so tests avoid spawning a real TTY.
var realmConfirmFn = prompt.Confirm

// runMigrateRealm fans out across immediate subdirectories, treating
// .claude/project/signals.md (old) or docs/wiki/index.md (new) as the marker of
// a member repo. One explicit confirm per repo is required, so a non-TTY
// context migrates nothing; an abort skips that member and the loop continues.
func runMigrateRealm(realmPath string) error {
	entries, err := os.ReadDir(realmPath)
	if err != nil {
		return fmt.Errorf("read realm dir: %w", err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		memberPath := filepath.Join(realmPath, e.Name())

		hasNew := fileExistsAt(filepath.Join(memberPath, "docs", "wiki", "index.md"))
		hasOld := fileExistsAt(filepath.Join(memberPath, ".claude", "project", "signals.md"))
		if !hasNew && !hasOld {
			continue // not an atomic'd repo
		}

		schema := migrate.ReadWikiSchema(memberPath)
		if schema >= 1 {
			fmt.Printf("migrate: %s already at schema %d, skipping\n", e.Name(), schema)
			continue
		}

		ok, perr := realmConfirmFn(
			fmt.Sprintf("Migrate repo %s?", e.Name()),
			"Move .claude/project/signals.md → docs/wiki/index.md",
			true,
		)
		if perr != nil {
			if errors.Is(perr, prompt.ErrNonInteractive) {
				fmt.Printf("migrate: %s skipped (non-interactive)\n", e.Name())
				continue
			} else if errors.Is(perr, prompt.ErrAborted) {
				fmt.Printf("migrate: %s skipped (aborted)\n", e.Name())
				continue
			} else {
				return fmt.Errorf("prompt for %s: %w", e.Name(), perr)
			}
		}
		if !ok {
			fmt.Printf("migrate: skipping %s\n", e.Name())
			continue
		}

		if err := migrateRepoAction(memberPath); err != nil {
			fmt.Fprintf(os.Stderr, "migrate: %s: %v (skipping)\n", e.Name(), err)
			continue
		}
		fmt.Printf("migrate: %s migrated\n", e.Name())
	}
	return nil
}

func scopedMigrations(scope string, registry []migrate.Migration) []migrate.Migration {
	var out []migrate.Migration
	for _, m := range registry {
		if m.Scope == scope {
			out = append(out, m)
		}
	}
	return out
}

// schemaToSemver maps N to "N.0.0"; 0 maps to "", which Run floors to "0.0.0".
func schemaToSemver(n int) string {
	if n == 0 {
		return ""
	}
	return strconv.Itoa(n) + ".0.0"
}

// semverToSchema parses only the major component.
func semverToSchema(v string) int {
	if v == "" || v == "0.0.0" {
		return 0
	}
	idx := strings.IndexByte(v, '.')
	if idx < 0 {
		return 0
	}
	n, _ := strconv.Atoi(v[:idx])
	return n
}

func fileExistsAt(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
