package signals

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/frontmatter"
	"github.com/damusix/atomic-claude/atomic/internal/version"
)

const (
	signalsFile = ".claude/project/deterministic-signals.md"
	prevFile    = ".claude/project/.deterministic-signals.prev.md"
)

// SignalsPath returns the absolute path to the signals file for the given repo root.
func SignalsPath(root string) string {
	return filepath.Join(root, signalsFile)
}

// PrevPath returns the absolute path to the prev signals file for the given repo root.
func PrevPath(root string) string {
	return filepath.Join(root, prevFile)
}

// Scan walks the repo at root, assembles the signals document, and writes it.
// Idempotency: if the body is unchanged, the existing generated_at is kept
// and the file is NOT rewritten (so mtime stays stable).
func Scan(root string) error {
	body, err := assembleBody(root)
	if err != nil {
		return fmt.Errorf("signals scan: %w", err)
	}

	outPath := SignalsPath(root)
	prevPath := PrevPath(root)

	// Read existing file (if any) to check idempotency.
	existingRaw, readErr := os.ReadFile(outPath)

	var genAt string
	rewrite := true

	if readErr == nil {
		oldMeta, oldBody, parseErr := frontmatter.Parse(string(existingRaw))
		if parseErr == nil && oldBody == body {
			// Body unchanged — keep existing generated_at and skip rewrite.
			if oldMeta != nil {
				if v, ok := oldMeta["generated_at"]; ok {
					genAt, _ = v.(string)
				}
			}
			rewrite = false
		}
	}

	if rewrite {
		// Back up the existing file before overwriting.
		if readErr == nil {
			if err := os.MkdirAll(filepath.Dir(prevPath), 0o755); err != nil {
				return fmt.Errorf("signals scan: create prev dir: %w", err)
			}
			if err := os.WriteFile(prevPath, existingRaw, 0o644); err != nil {
				return fmt.Errorf("signals scan: write prev file: %w", err)
			}
		}

		genAt = time.Now().UTC().Format(time.RFC3339)

		meta := map[string]any{
			"atomic_version": version.Version,
			"generated_at":   genAt,
		}
		doc, err := frontmatter.Emit(meta, body)
		if err != nil {
			return fmt.Errorf("signals scan: emit: %w", err)
		}

		if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
			return fmt.Errorf("signals scan: create output dir: %w", err)
		}
		if err := os.WriteFile(outPath, []byte(doc), 0o644); err != nil {
			return fmt.Errorf("signals scan: write output: %w", err)
		}
	}

	return nil
}

// assembleBody builds the body of the signals document (without frontmatter).
func assembleBody(root string) (string, error) {
	tree, err := ScanTree(root)
	if err != nil {
		return "", fmt.Errorf("tree scanner: %w", err)
	}

	manifests, err := ScanManifests(root)
	if err != nil {
		return "", fmt.Errorf("manifests scanner: %w", err)
	}

	langs, err := ScanLanguages(root)
	if err != nil {
		return "", fmt.Errorf("languages scanner: %w", err)
	}

	var sb strings.Builder
	sb.WriteString("# Deterministic signals\n")
	sb.WriteString("\n## Tree\n\n")
	sb.WriteString(tree)
	sb.WriteString("\n\n## Manifests\n\n")
	sb.WriteString(manifests)
	sb.WriteString("\n\n## Languages\n\n")
	sb.WriteString(langs)
	sb.WriteString("\n")

	return sb.String(), nil
}

// Show prints the signals file content to stdout.
func Show(root string) error {
	path := SignalsPath(root)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("signals show: file not found at %s — run 'atomic signals scan' first", path)
		}
		return fmt.Errorf("signals show: %w", err)
	}
	_, err = os.Stdout.Write(data)
	return err
}

// Stale exits 0 if the signals file is newer than the most recent source tree change,
// or returns an error (caller exits 1) if stale or file missing.
// Returns a sentinel ErrStale when stale.
func Stale(root string) error {
	path := SignalsPath(root)
	fi, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("signals stale: file not found at %s — run 'atomic signals scan' first", path)
		}
		return fmt.Errorf("signals stale: %w", err)
	}
	signalsMtime := fi.ModTime()

	newestSrc, err := newestSourceMtime(root)
	if err != nil {
		return fmt.Errorf("signals stale: %w", err)
	}

	if newestSrc.After(signalsMtime) {
		return ErrStale
	}
	return nil
}

// ErrStale is returned by Stale when the signals file is out of date.
var ErrStale = fmt.Errorf("signals stale: source tree is newer than signals file")

// newestSourceMtime returns the mtime of the most recently modified file
// in the source tree (excluding skipDirs).
func newestSourceMtime(root string) (time.Time, error) {
	var newest time.Time
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		fi, err := d.Info()
		if err != nil {
			return nil
		}
		if fi.ModTime().After(newest) {
			newest = fi.ModTime()
		}
		return nil
	})
	return newest, err
}

// Diff prints a unified diff between the previous and current signals files.
// Exit codes (returned as special errors):
//   - nil → exit 0 (no diff)
//   - ErrDiffPresent → exit 1 (diff present, stdout has content)
//   - ErrNoPrior → exit 2 (no prior version)
func Diff(root string) error {
	currentPath := SignalsPath(root)
	prevPath := PrevPath(root)

	if _, err := os.Stat(currentPath); os.IsNotExist(err) {
		return fmt.Errorf("signals diff: signals file not found at %s — run 'atomic signals scan' first", currentPath)
	}

	// Try git diff first.
	if isGitRepo(root) {
		return diffGit(root, currentPath)
	}
	return diffFallback(prevPath, currentPath)
}

// ErrDiffPresent signals that diff found changes (caller should exit 1).
var ErrDiffPresent = fmt.Errorf("signals diff: diff present")

// ErrNoPrior signals that no prior version is available (caller should exit 2).
var ErrNoPrior = fmt.Errorf("signals diff: no prior version available")

func isGitRepo(root string) bool {
	_, err := exec.Command("git", "-C", root, "rev-parse", "--is-inside-work-tree").Output()
	return err == nil
}

func diffGit(root, currentPath string) error {
	// Make the path relative to root for git.
	rel, err := filepath.Rel(root, currentPath)
	if err != nil {
		rel = currentPath
	}

	// --exit-code makes git diff exit 1 when differences are found.
	cmd := exec.Command("git", "-C", root, "diff", "--exit-code", "--", rel)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			if exit.ExitCode() == 1 {
				return ErrDiffPresent
			}
		}
		return fmt.Errorf("signals diff: git diff failed: %w", err)
	}
	return nil
}

func diffFallback(prevPath, currentPath string) error {
	if _, err := os.Stat(prevPath); os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "signals diff: no prior version available at %s\n", prevPath)
		return ErrNoPrior
	}

	cmd := exec.Command("diff", "-u", prevPath, currentPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			if exit.ExitCode() == 1 {
				return ErrDiffPresent
			}
		}
		return fmt.Errorf("signals diff: diff failed: %w", err)
	}
	return nil
}
