package migrate

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

func init() {
	Registry = append(Registry, Migration{
		TargetVersion: "1.0.0",
		Scope:         "repo",
		Up:            relocateSignalsToWiki,
	})
}

// relocateSignalsToWiki moves signals from .claude/project/ to docs/wiki/ and
// rewires the @-ref. Every move and write is idempotent, and the <wiki-type>
// sentinel is written last, so a crashed run resumes and finishes cleanly.
func relocateSignalsToWiki(ctx *Context) error {
	root := ctx.Root

	newIndex := filepath.Join(root, "docs", "wiki", "index.md")
	oldIndex := filepath.Join(root, ".claude", "project", "signals.md")

	if data, err := os.ReadFile(newIndex); err == nil {
		if strings.Contains(string(data), "<wiki-type>") {
			return nil
		}
	}

	// Neither layout present: not an atomic repo.
	if !fileExists(newIndex) {
		if _, err := os.Stat(oldIndex); os.IsNotExist(err) {
			return nil
		} else if err != nil {
			return fmt.Errorf("migrate signals→wiki: stat old index: %w", err)
		}
	}

	wikiDir := filepath.Join(root, "docs", "wiki")
	if err := os.MkdirAll(wikiDir, 0o755); err != nil {
		return fmt.Errorf("migrate signals→wiki: mkdir docs/wiki: %w", err)
	}

	if err := moveFile(oldIndex, newIndex); err != nil {
		return fmt.Errorf("migrate signals→wiki: move signals.md: %w", err)
	}

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

	detSig := filepath.Join(root, ".claude", "project", "deterministic-signals.md")
	if _, err := os.Stat(detSig); err == nil {
		scanDst := filepath.Join(wikiDir, "scan.md")
		if err := moveFile(detSig, scanDst); err != nil {
			return fmt.Errorf("migrate signals→wiki: move deterministic-signals.md: %w", err)
		}
	}

	if err := addDomainFrontmatter(wikiDir, newIndex); err != nil {
		return fmt.Errorf("migrate signals→wiki: add domain frontmatter: %w", err)
	}

	if err := rewireAtRef(root); err != nil {
		return fmt.Errorf("migrate signals→wiki: rewire @-ref: %w", err)
	}

	if err := prependWikiIndexHeader(newIndex); err != nil {
		return fmt.Errorf("migrate signals→wiki: prepend header to index.md: %w", err)
	}

	return nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// moveFile is a replayable rename: a missing src is a no-op (the move already
// happened), but src and dst both existing is an error rather than a clobber.
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
		return nil
	}
	return os.Rename(src, dst)
}

// writeFileAtomic writes via temp + rename so a concurrent reader never sees a
// partial file.
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

// prependWikiIndexHeader adds OKF frontmatter and the machine control blocks,
// skipping when <wiki-type> is already present.
func prependWikiIndexHeader(indexPath string) error {
	data, err := os.ReadFile(indexPath)
	if err != nil {
		return err
	}
	if strings.Contains(string(data), "<wiki-type>") {
		return nil
	}
	header := "---\ntype: Index\n---\n\n" +
		"<wiki-type>repo</wiki-type>\n" +
		"<scan-sha></scan-sha>\n" +
		"<wiki-schema>1</wiki-schema>\n\n"
	return writeFileAtomic(indexPath, append([]byte(header), data...), 0o644)
}

// addDomainFrontmatter stamps `type: Domain` on every .md in wikiDir except
// indexPath and scan.md, which is raw machine output.
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
		if p == indexPath || e.Name() == "scan.md" {
			continue
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
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

var oldRefRe = regexp.MustCompile(`(?m)^@\.claude/project/signals\.md`)

// rewireAtRef repoints the signals @-ref in the root config files, skipping
// any that are missing.
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
