package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/cliutil"
	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/retro"
	"github.com/spf13/cobra"
)

func buildRetroCmd() *cobra.Command {
	parent := &cobra.Command{
		Use:   "retro",
		Short: "Session-history extraction for /retrospective-learning",
		Args:  cobra.ArbitraryArgs,
		RunE:  func(cmd *cobra.Command, args []string) error { runRetro(args); return nil },
	}
	extractCmd := &cobra.Command{
		Use:                "extract",
		Short:              "Extract session history to compact, line-numbered markdown",
		Annotations:        map[string]string{"args_hint": ""},
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			runRetro(append([]string{"extract"}, args...))
			return nil
		},
	}
	extractCmd.Flags().String("since", "", "keep sessions modified at/after this date (YYYY-MM-DD or RFC 3339)")
	extractCmd.Flags().String("project", "", "keep project dirs whose slug matches this glob")
	extractCmd.Flags().Int("shards", 0, "split output into N size-balanced files (requires -o)")
	extractCmd.Flags().StringP("out", "o", "", "write output to this file instead of stdout")
	parent.AddCommand(extractCmd)
	return parent
}

// retroAction is split out of runRetro so tests reach dispatch without
// os.Exit. home is the real home dir and now is injected, never time.Now.
func retroAction(args []string, home string, now time.Time) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Usage: atomic retro <extract> [flags]")
		return 2
	}

	switch args[0] {
	case "extract":
		return retroExtractAction(args[1:], home, now)
	default:
		fmt.Fprintf(os.Stderr, "atomic retro: unknown verb %q\n", args[0])
		fmt.Fprintln(os.Stderr, "Usage: atomic retro <extract> [flags]")
		return 2
	}
}

func retroExtractAction(args []string, home string, now time.Time) int {
	fs := flag.NewFlagSet("retro-extract", flag.ContinueOnError)
	cliutil.SetUsage(fs, "atomic retro extract [--since <date>] [--project <glob>] [--shards N] [-o <file>]")
	fs.SetOutput(os.Stderr)

	var sinceFlag, projectGlob, out string
	var shards int
	fs.StringVar(&sinceFlag, "since", "", "keep sessions modified at/after this date (YYYY-MM-DD or RFC 3339)")
	fs.StringVar(&projectGlob, "project", "", "keep project dirs whose slug matches this glob")
	fs.IntVar(&shards, "shards", 0, "split output into N size-balanced files (requires -o)")
	fs.StringVar(&out, "out", "", "write output to this file instead of stdout")
	fs.StringVar(&out, "o", "", "")

	if err := fs.Parse(args); err != nil {
		return 2
	}

	shardsSet := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "shards" {
			shardsSet = true
		}
	})
	if shardsSet {
		if shards < 1 {
			fmt.Fprintln(os.Stderr, "atomic retro extract: --shards must be >= 1")
			return 2
		}
		if out == "" {
			fmt.Fprintln(os.Stderr, "atomic retro extract: --shards requires -o")
			return 2
		}
	}

	since, err := resolveSince(sinceFlag, home, now)
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic retro extract: %v\n", err)
		return 2
	}

	opts := retro.Options{
		ProjectsDir: filepath.Join(home, ".claude", "projects"),
		Since:       since,
		ProjectGlob: projectGlob,
	}
	result, err := retro.Extract(opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic retro extract: %v\n", err)
		return 1
	}

	baseTitle := fmt.Sprintf("retro extract — since %s — %d sessions, %d projects",
		since.Format("2006-01-02"), result.Stats.SessionsKept, result.Stats.ProjectsScanned)

	var written []writtenFile

	if shardsSet {
		ext := filepath.Ext(out)
		stem := strings.TrimSuffix(out, ext)
		for _, sf := range retro.Shard(result.Sessions, shards) {
			title := fmt.Sprintf("%s — shard %d/%d", baseTitle, sf.Index, sf.Count)
			body := retro.Render(sf.Sessions, title)
			path := fmt.Sprintf("%s.%d%s", stem, sf.Index, ext)
			wf, err := writeOutputFile(path, body)
			if err != nil {
				fmt.Fprintf(os.Stderr, "atomic retro extract: write %s: %v\n", path, err)
				return 1
			}
			written = append(written, wf)
		}
	} else {
		body := retro.Render(result.Sessions, baseTitle)
		if out == "" {
			fmt.Fprint(os.Stdout, body)
		} else {
			wf, err := writeOutputFile(out, body)
			if err != nil {
				fmt.Fprintf(os.Stderr, "atomic retro extract: write %s: %v\n", out, err)
				return 1
			}
			written = append(written, wf)
		}
	}

	fmt.Fprintf(os.Stderr, "files scanned: %d (%d bytes)\n", result.Stats.FilesScanned, result.Stats.TotalBytes)
	fmt.Fprintf(os.Stderr, "projects scanned: %d\n", result.Stats.ProjectsScanned)
	fmt.Fprintf(os.Stderr, "sessions kept: %d\n", result.Stats.SessionsKept)
	fmt.Fprintf(os.Stderr, "unparsable rows: %d\n", result.Stats.UnparsableRows)
	fmt.Fprintf(os.Stderr, "read errors: %d\n", result.Stats.ReadErrors)
	for _, w := range written {
		fmt.Fprintf(os.Stderr, "%s: %d bytes, %d lines\n", w.path, w.bytes, w.lines)
	}

	return 0
}

// writtenFile records one output file's summary line for the stderr report.
type writtenFile struct {
	path  string
	bytes int
	lines int
}

// writeOutputFile writes body to path and records its size, shared by both
// the single-file and sharded write paths so they report identically.
func writeOutputFile(path, body string) (writtenFile, error) {
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		return writtenFile{}, err
	}
	return writtenFile{path: path, bytes: len(body), lines: strings.Count(body, "\n")}, nil
}

// resolveSince applies --since when given, else falls back to the newest
// ~/.atomic/retro-runs/*.json's run_ts, else 30 days before now — and always
// tells stderr which one it picked so a stale default doesn't go unnoticed.
func resolveSince(flagVal, home string, now time.Time) (time.Time, error) {
	if flagVal != "" {
		if t, err := time.ParseInLocation("2006-01-02", flagVal, time.Local); err == nil {
			return t, nil
		}
		if t, err := time.Parse(time.RFC3339, flagVal); err == nil {
			return t, nil
		}
		return time.Time{}, fmt.Errorf("invalid --since %q: want YYYY-MM-DD or RFC 3339", flagVal)
	}

	runsDir := filepath.Join(config.Dir(home), "retro-runs")
	if ts, ok, err := retro.LastRunSince(runsDir); err == nil && ok {
		fmt.Fprintf(os.Stderr, "since: %s (last retro run)\n", ts.Format("2006-01-02"))
		return ts, nil
	}
	fallback := now.AddDate(0, 0, -30)
	fmt.Fprintf(os.Stderr, "since: %s (no retro run log, 30-day default)\n", fallback.Format("2006-01-02"))
	return fallback, nil
}

func runRetro(args []string) {
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic retro: resolve home dir: %v\n", err)
		os.Exit(1)
	}
	os.Exit(retroAction(args, home, time.Now()))
}
