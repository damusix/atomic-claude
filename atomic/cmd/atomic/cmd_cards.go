package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/harness"
	"github.com/damusix/atomic-claude/atomic/internal/harness/claude"
	"github.com/damusix/atomic-claude/atomic/internal/harness/omp"
	"github.com/damusix/atomic-claude/atomic/internal/installstate"
	"github.com/damusix/atomic-claude/atomic/internal/wiki"
)

// cardWrite selects how far a card convergence goes beyond enrolled targets.
type cardWrite struct {
	// link writes the Claude link even when no Claude target is enrolled,
	// because the writer moved or laid out the state it points to.
	link bool
	// stage adds the Claude link, the OMP links, and the rendered cards to the
	// Git index beside the migration's renames.
	stage bool
	// rendered skips the re-render: wiki init's refresh already wrote canonical cards.
	rendered bool
}

func init() {
	config.AfterRepoStateMigration = func(repoRoot, stateRoot string) error {
		cfg, _, err := config.LoadRepoConfig(filepath.Join(stateRoot, "atomic.toml"))
		if err == nil && cfg.Scope == "realm" {
			return nil
		}
		return convergeRepoCards(config.ProjectHome(), repoRoot, stateRoot, cardWrite{link: true, stage: true}, os.Stderr)
	}
	wiki.ProjectRepoCards = func(repoRoot, stateRoot string) error {
		return convergeRepoCards(config.ProjectHome(), repoRoot, stateRoot, cardWrite{rendered: true}, os.Stderr)
	}
}

// convergeRepoCards makes a repository's wiki cards readable to Claude and,
// when OMP is enrolled, links them into .omp/rules.
func convergeRepoCards(home, repoRoot, stateRoot string, w cardWrite, out io.Writer) error {
	ledger := &installstate.Ledger{}
	if home != "" {
		var err error
		if ledger, err = installstate.LoadLedger(config.LedgerPath(home)); err != nil {
			return err
		}
	}
	var claudeTargets []string
	ompEnrolled := false
	for _, t := range ledger.Targets {
		switch t.Harness {
		case string(harness.KindClaude):
			claudeTargets = append(claudeTargets, t.Key())
		case string(harness.KindOMP):
			ompEnrolled = true
		}
	}
	key := config.ProjectKey(repoRoot)

	var errs []error
	if w.link || len(claudeTargets) > 0 {
		result, err := claude.EnsureCardLink(claude.CardLinkRequest{
			Home: home, RepoRoot: repoRoot, StateRoot: stateRoot, ProjectKey: key,
			Targets: claudeTargets, Stage: w.stage,
		})
		printCardLink(out, result)
		errs = append(errs, err)
	}
	if ompEnrolled {
		// OMP lists a card only through the description older cards lack.
		if !w.rendered {
			if err := wiki.RenderRepoCards(repoRoot, stateRoot); err != nil {
				return errors.Join(append(errs, err)...)
			}
		}
		result, err := omp.New().PublishProjectCards(omp.CardPublishRequest{
			Home: home, StateRoot: stateRoot, ProjectKey: key, Base: repoRoot, Stage: w.stage,
		})
		if result.Applied {
			fmt.Fprintf(out, "omp cards: published %s\n", result.Dir)
		}
		for _, l := range result.Ignored {
			fmt.Fprintf(out, "omp cards: %s is ignored by %s, so Git will not track it and OMP will skip it\n", l.Path, l.Source)
		}
		printRetired(out, "omp cards", result.Retired, result.Kept)
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

func printCardLink(out io.Writer, r claude.CardLinkResult) {
	switch r.Action {
	case claude.CardLinkCreated, claude.CardLinkAdopted, claude.CardLinkCopied:
		fmt.Fprintf(out, "card link: %s %s\n", r.Action, r.Path)
	}
	if r.Negated != "" {
		fmt.Fprintf(out, "card link: appended a negation for it to %s\n", r.Negated)
	}
	if r.IgnoredBy != "" {
		fmt.Fprintf(out, "card link: %s is ignored by %s, so Git will not track it; it was not staged\n", r.Path, r.IgnoredBy)
	}
	printRetired(out, "card link", r.Retired, r.Kept)
}

func printRetired(out io.Writer, label string, retired, kept []string) {
	for _, p := range retired {
		fmt.Fprintf(out, "%s: removed the retired card tree %s\n", label, p)
	}
	for _, p := range kept {
		fmt.Fprintf(out, "%s: kept %s; Atomic cannot prove it wrote it, so delete it yourself if unwanted\n", label, p)
	}
}

// installRepoRoot is the repository install converges cards into.
var installRepoRoot = cwdRepoRoot

// convergeCwdRepoCards converges the working directory's repository cards.
// A failure only warns: the targets themselves already converged.
func convergeCwdRepoCards(home string, repoRoot func() (string, bool), warn io.Writer) {
	root, ok := repoRoot()
	if !ok || home == "" {
		return
	}
	stateRoot, err := config.RepositoryStateDir(root)
	if err == nil {
		err = convergeRepoCards(home, root, stateRoot, cardWrite{}, warn)
	}
	if err != nil {
		fmt.Fprintf(warn, "atomic: repository wiki cards: %v\n", err)
	}
}
