package main

import (
	"fmt"
	"io"
	"os"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/realm"
	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/harness/claude"
	"github.com/damusix/atomic-claude/atomic/internal/wiki"
)

// convergeSteeringMode reshapes the pairs present at root to its steering mode;
// an unresolvable mode skips conversion and is returned.
func convergeSteeringMode(home, root string, out io.Writer) error {
	mode, err := config.ResolveSteeringMode(home, root)
	if err != nil {
		return err
	}
	if home != "" && claude.IsNativeRoot(home, root) {
		fmt.Fprintf(out, "steering: skipped %s, the Claude native root\n", root)
		return nil
	}
	scope := "repo"
	if cfg, _, err := config.LoadRepoConfig(config.RepoConfigPath(root)); err == nil && cfg.Scope == "realm" {
		scope = "realm"
	}

	// The code-index block moves first so the pair converters see the realm root
	// files without it.
	var touched, notes []string
	if scope == "realm" {
		if touched, err = realm.MoveCodeIndexBlock(root); err != nil {
			return err
		}
	}
	if home != "" {
		var scoped []string
		if scoped, notes, err = claude.ConvertScope(home, claude.DefaultConfigDir(home), root, mode); err != nil {
			return err
		}
		touched = append(touched, scoped...)
	}
	wikiTouched, wikiNotes, err := wiki.ConvertSteeringPair(root, scope, mode)
	touched, notes = append(touched, wikiTouched...), append(notes, wikiNotes...)
	for _, path := range touched {
		fmt.Fprintf(out, "steering (%s): updated %s\n", mode, path)
	}
	for _, note := range notes {
		fmt.Fprintf(out, "steering (%s): %s\n", mode, note)
	}
	return err
}

// cwdSteeringRoot is the repository holding the working directory, else the
// realm root above it, which need not be a Git repository.
func cwdSteeringRoot(repoRoot string, isRepo bool) (string, bool) {
	if isRepo {
		return repoRoot, true
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", false
	}
	return config.FindScopeRoot(cwd, "realm")
}
