package doctor

import (
	"flag"
	"fmt"
	"strconv"
	"strings"
)

// ParseFlags parses the args slice (not including the subcommand name) and
// returns a validated Opts. Returns a non-nil error for usage violations;
// callers should exit 2 on error.
func ParseFlags(args []string) (Opts, error) {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)

	var fix bool
	var jsonOut bool
	var onlyStr string
	var skipStr string
	var staleDays int
	var verbose bool

	fs.BoolVar(&fix, "fix", false, "Per-item confirm prompt before applying any repair")
	fs.BoolVar(&jsonOut, "json", false, "Emit machine-readable JSON result to stdout")
	fs.StringVar(&onlyStr, "only", "", "Comma-separated category indices or names to run")
	fs.StringVar(&skipStr, "skip", "", "Comma-separated category indices or names to skip")
	fs.IntVar(&staleDays, "stale-days", 7, "Stale-signals threshold in days (positive int)")
	fs.BoolVar(&verbose, "verbose", false, "Print per-file detail for install integrity and manifest parity")

	if err := fs.Parse(args); err != nil {
		return Opts{}, fmt.Errorf("doctor: %w", err)
	}

	// Mutual exclusion: --fix and --json cannot be combined.
	if fix && jsonOut {
		return Opts{}, fmt.Errorf("doctor: --fix and --json are mutually exclusive")
	}

	// Validate --stale-days.
	if staleDays <= 0 {
		return Opts{}, fmt.Errorf("doctor: --stale-days must be a positive integer, got %d", staleDays)
	}

	// Resolve --only.
	only, err := resolveCategories(onlyStr)
	if err != nil {
		return Opts{}, fmt.Errorf("doctor: --only: %w", err)
	}

	// Resolve --skip.
	skip, err := resolveCategories(skipStr)
	if err != nil {
		return Opts{}, fmt.Errorf("doctor: --skip: %w", err)
	}

	return Opts{
		Fix:       fix,
		JSON:      jsonOut,
		Only:      only,
		Skip:      skip,
		StaleDays: staleDays,
		Verbose:   verbose,
	}, nil
}

// resolveCategories parses a comma-separated list of category indices or names
// and returns the resolved indices. Empty input returns nil.
func resolveCategories(input string) ([]int, error) {
	if input == "" {
		return nil, nil
	}

	// Build name→index lookup.
	byName := make(map[string]int, len(categories))
	byIndex := make(map[int]bool, len(categories))
	for _, c := range categories {
		byName[c.Name] = c.Index
		byIndex[c.Index] = true
	}

	tokens := strings.Split(input, ",")
	seen := make(map[int]bool, len(tokens))
	result := make([]int, 0, len(tokens))

	for _, raw := range tokens {
		tok := strings.TrimSpace(raw)
		if tok == "" {
			continue
		}

		// Try parsing as integer first.
		if n, err := strconv.Atoi(tok); err == nil {
			if !byIndex[n] {
				return nil, fmt.Errorf("index %d out of range (valid: 1-%d)", n, len(categories))
			}
			if !seen[n] {
				seen[n] = true
				result = append(result, n)
			}
			continue
		}

		// Try as canonical name.
		idx, ok := byName[tok]
		if !ok {
			return nil, fmt.Errorf("unknown category %q (valid names: %s)", tok, validNames())
		}
		if !seen[idx] {
			seen[idx] = true
			result = append(result, idx)
		}
	}

	return result, nil
}

// validNames returns a comma-separated list of canonical category names.
func validNames() string {
	names := make([]string, len(categories))
	for i, c := range categories {
		names[i] = c.Name
	}
	return strings.Join(names, ", ")
}
