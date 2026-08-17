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

// buildMigrateCmd returns the "migrate" top-level command with flag metadata.
// Note: --repo here is a migrate-specific flag (target repo path) distinct from
// the root's persistent --repo (context override); no Cobra conflict occurs
// because DisableFlagParsing:true prevents flag merging at execute time, and
// the FlagSet is created before AddCommand so the lazy-init merge never runs.
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
	return c
}

// runMigrate is the os.Exit-aware entry point for the migrate top-level verb.
//
//	atomic migrate                  → install-scope steps against ~/.claude
//	atomic migrate --repo <path>    → repo-scope steps on that repo
//	atomic migrate --realm <path>   → install-scope + fan-out to member repos
func runMigrate(args []string) {
	fs := flag.NewFlagSet("migrate", flag.ContinueOnError)
	cliutil.SetUsage(fs, "atomic migrate [--repo <path>] [--realm <path>]")
	var repoPath string
	var realmPath string
	fs.StringVar(&repoPath, "repo", "", "run repo-scope migrations on this path")
	fs.StringVar(&realmPath, "realm", "", "run install-scope + repo fan-out under this realm root")
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
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
		// Install-scope first.
		home, herr := os.UserHomeDir()
		if herr != nil {
			fmt.Fprintf(os.Stderr, "atomic migrate: resolve home dir: %v\n", herr)
			os.Exit(1)
		}
		if err := runMigrateInstall(home); err != nil {
			fmt.Fprintf(os.Stderr, "atomic migrate: install-scope: %v\n", err)
			os.Exit(1)
		}
		// Fan-out to member repos.
		if err := runMigrateRealm(absRealm); err != nil {
			fmt.Fprintf(os.Stderr, "atomic migrate: realm: %v\n", err)
			os.Exit(1)
		}

	default:
		// Install-scope only.
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

// runMigrateInstall runs install-scope migrations against home.
// Reads the recorded version from config.toml [install].version, applies any
// pending install-scope steps, and writes the new version back on success.
//
// Two-root split: config.toml lives under <home>/.atomic (config helpers get
// home), while migrate.Context.Root keeps its install-scope meaning of
// <home>/.claude — install-scope migration steps operate on the Claude
// artifact install target, not the atomic state root.
//
// Returns an error; the caller decides whether it is fatal.
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
// It reads the repo's wiki schema, runs repo-scope migrations, and stamps the
// new schema back into docs/wiki/index.md on success.
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
	// WriteWikiSchema is a no-op when docs/wiki/index.md does not exist
	// (e.g. no-signals repo where the step ran but created no file).
	return migrate.WriteWikiSchema(repoPath, newSchema)
}

// realmConfirmFn is the testable seam for runMigrateRealm's per-repo confirm
// prompt. Tests replace it to avoid spawning a real TTY.
var realmConfirmFn = prompt.Confirm

// runMigrateRealm fans out repo-scope migrations across immediate subdirectory
// member repos under realmPath. A member repo is detected by the presence of
// .claude/project/signals.md (old layout) or docs/wiki/index.md (new layout).
// Each detected member prompts for confirmation before migrating.
//
// Non-interactive context (ErrNonInteractive): the member is skipped. The spec
// requires one explicit confirm per repo; a non-TTY context must migrate nothing.
// ErrAborted: the member is skipped; the realm loop continues.
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
				// Non-TTY context: the spec requires explicit confirm per repo.
				// Skip silently rather than auto-migrating.
				fmt.Printf("migrate: %s skipped (non-interactive)\n", e.Name())
				continue
			} else if errors.Is(perr, prompt.ErrAborted) {
				// User aborted this member's prompt: skip it, continue the loop.
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

// scopedMigrations filters migrate.Registry to those with the given Scope.
func scopedMigrations(scope string, registry []migrate.Migration) []migrate.Migration {
	var out []migrate.Migration
	for _, m := range registry {
		if m.Scope == scope {
			out = append(out, m)
		}
	}
	return out
}

// schemaToSemver converts a wiki schema integer to a semver string for
// migrate.Run. 0 returns "" (normalised to floor "0.0.0" by Run); N > 0
// returns "N.0.0".
func schemaToSemver(n int) string {
	if n == 0 {
		return ""
	}
	return strconv.Itoa(n) + ".0.0"
}

// semverToSchema converts the semver string returned by migrate.Run back to an
// integer schema version for WriteWikiSchema. Parses only the major component.
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

// fileExistsAt returns true when the file at path exists (any type).
func fileExistsAt(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
