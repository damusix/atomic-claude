package migrate

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

func init() {
	// Register repo-scope migrations in ascending TargetVersion order.
	Registry = append(Registry, Migration{
		TargetVersion: "1.0.0",
		Scope:         "repo",
		Up:            relocateSignalsToWiki,
	})
}

// relocateSignalsToWiki is the first repo-scope migration step (v1.0.0).
// It moves the signals files from .claude/project/ to docs/wiki/ and rewires
// the @-ref in CLAUDE.md / claude.local.md / CLAUDE.local.md.
//
// Guards (checked in order):
//  1. docs/wiki/index.md exists AND contains <wiki-type> → fully migrated, return nil.
//  2. docs/wiki/index.md does not exist AND .claude/project/signals.md does not exist
//     → no old layout present, return nil.
//
// Partial-failure recovery: when docs/wiki/index.md exists without <wiki-type>,
// the migration is incomplete. The step resumes and finishes it; all moves and
// content writes are idempotent.
//
// The <wiki-type> sentinel is written last so it only appears on full success.
func relocateSignalsToWiki(ctx *Context) error {
	root := ctx.Root

	newIndex := filepath.Join(root, "docs", "wiki", "index.md")
	oldIndex := filepath.Join(root, ".claude", "project", "signals.md")

	// Guard 1: fully migrated — sentinel <wiki-type> present → no-op.
	if data, err := os.ReadFile(newIndex); err == nil {
		if strings.Contains(string(data), "<wiki-type>") {
			return nil
		}
	}

	// Guard 2: no new index AND no old layout → not an atomic repo, no-op.
	if !fileExists(newIndex) {
		if _, err := os.Stat(oldIndex); os.IsNotExist(err) {
			return nil
		} else if err != nil {
			return fmt.Errorf("migrate signals→wiki: stat old index: %w", err)
		}
	}

	// Ensure target directory exists.
	wikiDir := filepath.Join(root, "docs", "wiki")
	if err := os.MkdirAll(wikiDir, 0o755); err != nil {
		return fmt.Errorf("migrate signals→wiki: mkdir docs/wiki: %w", err)
	}

	// Move signals.md → docs/wiki/index.md (idempotent: skips when already moved).
	if err := moveFile(oldIndex, newIndex); err != nil {
		return fmt.Errorf("migrate signals→wiki: move signals.md: %w", err)
	}

	// Move domain files: .claude/project/signals/*.md → docs/wiki/*.md (idempotent).
	domainDir := filepath.Join(root, ".claude", "project", "signals")
	if info, err := os.Stat(domainDir); err == nil && info.IsDir() {
		entries, err := os.ReadDir(domainDir)
		if err != nil {
			return fmt.Errorf("migrate signals→wiki: read domain dir: %w", err)
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			src := filepath.Join(domainDir, e.Name())
			dst := filepath.Join(wikiDir, e.Name())
			if err := moveFile(src, dst); err != nil {
				return fmt.Errorf("migrate signals→wiki: move domain %s: %w", e.Name(), err)
			}
		}
	}

	// Move deterministic-signals.md → docs/wiki/scan.md when present (idempotent).
	detSig := filepath.Join(root, ".claude", "project", "deterministic-signals.md")
	if _, err := os.Stat(detSig); err == nil {
		scanDst := filepath.Join(wikiDir, "scan.md")
		if err := moveFile(detSig, scanDst); err != nil {
			return fmt.Errorf("migrate signals→wiki: move deterministic-signals.md: %w", err)
		}
	}

	// Prepend type: Domain frontmatter to domain files, excluding index.md and
	// scan.md. Idempotent: skips files that already have a frontmatter fence.
	if err := addDomainFrontmatter(wikiDir, newIndex); err != nil {
		return fmt.Errorf("migrate signals→wiki: add domain frontmatter: %w", err)
	}

	// Rewire @.claude/project/signals.md → @docs/wiki/index.md in root config
	// files. Idempotent: skips files where the old ref is already absent.
	if err := rewireAtRef(root); err != nil {
		return fmt.Errorf("migrate signals→wiki: rewire @-ref: %w", err)
	}

	// Write the sentinel LAST: only after all other steps succeed.
	// Guard 1 uses <wiki-type> to confirm a complete migration; writing it last
	// ensures a crash before this point leaves the migration resumable.
	if err := prependWikiIndexHeader(newIndex); err != nil {
		return fmt.Errorf("migrate signals→wiki: prepend header to index.md: %w", err)
	}

	return nil
}

// fileExists reports whether path exists (any type).
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// moveFile moves src to dst, creating parent directories as needed.
// Idempotency contract:
//   - dst exists AND src exists  → error (refuse to clobber an unrelated file)
//   - dst exists AND src absent  → nil (move already completed on a prior run)
//   - src exists AND dst absent  → rename (normal move)
//   - src absent AND dst absent  → nil (nothing to do)
func moveFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	_, dstErr := os.Stat(dst)
	_, srcErr := os.Stat(src)
	dstExists := dstErr == nil
	srcExists := srcErr == nil

	if dstExists && srcExists {
		return fmt.Errorf("moveFile: %s already exists and %s also exists; refusing to overwrite", dst, src)
	}
	if !srcExists {
		// dst exists (move already done) or neither exists (nothing to do).
		return nil
	}
	// src exists, dst absent: normal rename.
	return os.Rename(src, dst)
}

// writeFileAtomic writes data to path via write-to-temp + rename, so concurrent
// readers always see a complete file rather than a partial write.
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".migrate-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	_, werr := tmp.Write(data)
	cerr := tmp.Close()
	if werr != nil {
		os.Remove(tmpName)
		return werr
	}
	if cerr != nil {
		os.Remove(tmpName)
		return cerr
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return err
	}
	return nil
}

// prependWikiIndexHeader prepends OKF frontmatter and machine control blocks
// to index.md. Skips silently when <wiki-type> is already present (idempotent).
//
// Written header:
//
//	---
//	type: Index
//	---
//
//	<wiki-type>repo</wiki-type>
//	<scan-sha></scan-sha>
//	<wiki-schema>1</wiki-schema>
func prependWikiIndexHeader(indexPath string) error {
	data, err := os.ReadFile(indexPath)
	if err != nil {
		return err
	}
	// Already has a wiki-type block: skip to stay idempotent.
	if strings.Contains(string(data), "<wiki-type>") {
		return nil
	}
	header := "---\ntype: Index\n---\n\n" +
		"<wiki-type>repo</wiki-type>\n" +
		"<scan-sha></scan-sha>\n" +
		"<wiki-schema>1</wiki-schema>\n\n"
	return writeFileAtomic(indexPath, append([]byte(header), data...), 0o644)
}

// addDomainFrontmatter prepends `type: Domain` OKF frontmatter to every .md
// file in wikiDir, excluding indexPath and scan.md (raw machine output).
// Skips files that already start with a YAML frontmatter fence (---).
func addDomainFrontmatter(wikiDir, indexPath string) error {
	entries, err := os.ReadDir(wikiDir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		p := filepath.Join(wikiDir, e.Name())
		// Exclude index.md and scan.md.
		if p == indexPath || e.Name() == "scan.md" {
			continue
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		// Skip if already has frontmatter.
		if strings.HasPrefix(strings.TrimSpace(string(data)), "---") {
			continue
		}
		fm := "---\ntype: Domain\n---\n\n"
		if err := writeFileAtomic(p, append([]byte(fm), data...), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// oldRefRe matches @.claude/project/signals.md lines in config files.
var oldRefRe = regexp.MustCompile(`(?m)^@\.claude/project/signals\.md`)

// rewireAtRef replaces `@.claude/project/signals.md` with
// `@docs/wiki/index.md` in the root config files:
// CLAUDE.md, claude.local.md, and CLAUDE.local.md.
// Missing files are silently skipped.
func rewireAtRef(root string) error {
	candidates := []string{
		filepath.Join(root, "CLAUDE.md"),
		filepath.Join(root, "claude.local.md"),
		filepath.Join(root, "CLAUDE.local.md"),
	}
	for _, path := range candidates {
		data, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		if !oldRefRe.Match(data) {
			continue
		}
		updated := oldRefRe.ReplaceAll(data, []byte("@docs/wiki/index.md"))
		if err := writeFileAtomic(path, updated, 0o644); err != nil {
			return err
		}
	}
	return nil
}
