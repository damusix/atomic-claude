// Package templaterender implements the template rendering logic used by
// cmd/render-templates. It loads shared partials from <repoRoot>/templates/shared/,
// then for each rendered kind (commands, agents) walks <repoRoot>/templates/<kind>/,
// renders each file via text/template, and writes the output to <outDir>/<kind>/<name>.md.
// All kinds share one shared-partial pool, so a partial defined once is callable
// from any command or agent template.
//
// The orphan rule halts with a non-zero exit when any <outDir>/<kind>/<name>.md
// lacks a corresponding <repoRoot>/templates/<kind>/<name>.md.
package templaterender

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/template"
)

// renderedKinds are the artifact kinds rendered from templates/<kind>/ into
// <outDir>/<kind>/. Each kind shares the same shared-partial pool. The slice
// order is the render order; it must stay deterministic.
var renderedKinds = []string{"commands", "agents"}

// Run is the main entry point. repoRoot is the path to the repository root
// (parent of atomic/); outDir is the directory where rendered outputs are
// written (a <kind>/ subdirectory is created inside it per rendered kind).
func Run(repoRoot, outDir string) error {
	templatesDir := filepath.Join(repoRoot, "templates")
	sharedDir := filepath.Join(templatesDir, "shared")

	// Load all shared partials once; every kind clones from this pool.
	sharedTmpl, err := loadSharedPartials(sharedDir)
	if err != nil {
		return fmt.Errorf("load shared partials: %w", err)
	}

	for _, kind := range renderedKinds {
		if err := renderKind(sharedTmpl, kind, templatesDir, outDir); err != nil {
			return err
		}
	}

	return nil
}

// renderKind renders one artifact kind: templates/<kind>/**/*.md → <outDir>/<kind>/**/*.md.
// It enforces the orphan rule for that kind before rendering.
func renderKind(sharedTmpl *template.Template, kind, templatesDir, outDir string) error {
	kindTemplDir := filepath.Join(templatesDir, kind)
	kindOutDir := filepath.Join(outDir, kind)

	// Enumerate source templates (recursive — includes subdirs like _templates/).
	srcTemplates, err := listMDFilesRecursive(kindTemplDir)
	if err != nil {
		return fmt.Errorf("list templates/%s: %w", kind, err)
	}

	// Orphan detection: enumerate existing output files, check each has a template.
	if err := checkOrphans(kind, kindOutDir, srcTemplates); err != nil {
		return err
	}

	if len(srcTemplates) == 0 {
		return nil
	}

	for _, relPath := range srcTemplates {
		src := filepath.Join(kindTemplDir, relPath)
		dst := filepath.Join(kindOutDir, relPath)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return fmt.Errorf("create output dir for %s/%s: %w", kind, relPath, err)
		}
		if err := renderFile(sharedTmpl, src, dst); err != nil {
			return fmt.Errorf("render %s/%s: %w", kind, relPath, err)
		}
	}

	return nil
}

// loadSharedPartials reads all *.md files in sharedDir and registers them as
// named templates in a base template set. Each file may define named templates
// via {{- define "name" -}} ... {{- end -}}. Returns an empty template set if
// sharedDir does not exist.
func loadSharedPartials(sharedDir string) (*template.Template, error) {
	base := template.New("base")

	entries, err := os.ReadDir(sharedDir)
	if os.IsNotExist(err) {
		return base, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read shared dir: %w", err)
	}

	// Sort for determinism.
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	for _, name := range names {
		data, err := os.ReadFile(filepath.Join(sharedDir, name))
		if err != nil {
			return nil, fmt.Errorf("read shared/%s: %w", name, err)
		}
		// Parse into the same template set so all defines are available.
		if _, err := base.Parse(string(data)); err != nil {
			return nil, fmt.Errorf("parse shared/%s: %w", name, err)
		}
	}

	return base, nil
}

// listMDFiles returns sorted *.md file names (base names only) in dir.
// Returns nil (not an error) if dir does not exist. Non-recursive.
func listMDFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read dir %s: %w", dir, err)
	}

	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}

// listMDFilesRecursive returns sorted *.md file paths (relative to dir) in dir
// and all subdirectories. Returns nil (not an error) if dir does not exist.
func listMDFilesRecursive(dir string) ([]string, error) {
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return nil, nil
	}

	var paths []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".md") {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		paths = append(paths, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk %s: %w", dir, err)
	}
	sort.Strings(paths)
	return paths, nil
}

// checkOrphans enumerates outDir/<kind>/**/*.md and returns an error for any
// file that has no corresponding source template. The error message names both
// remediation paths: create the template OR rm the output file.
func checkOrphans(kind, kindOutDir string, srcTemplates []string) error {
	existing, err := listMDFilesRecursive(kindOutDir)
	if err != nil {
		return fmt.Errorf("list output %s: %w", kind, err)
	}

	// Build a set of template paths for O(1) lookup.
	tmplSet := make(map[string]bool, len(srcTemplates))
	for _, name := range srcTemplates {
		tmplSet[name] = true
	}

	var orphans []string
	for _, name := range existing {
		if !tmplSet[name] {
			orphans = append(orphans, name)
		}
	}

	if len(orphans) == 0 {
		return nil
	}

	// Build a multi-orphan error message naming both remediation paths.
	var sb strings.Builder
	sb.WriteString("render-templates: orphan output file(s) found — ")
	sb.WriteString(fmt.Sprintf("every %s/ file must have a matching template.\n", kind))
	sb.WriteString("Remediation: for each orphan, either\n")
	sb.WriteString("  (a) create the missing template, or\n")
	sb.WriteString("  (b) rm the orphan output file.\n\n")
	for _, name := range orphans {
		sb.WriteString(fmt.Sprintf("  orphan: %s/%s\n", kind, name))
		sb.WriteString(fmt.Sprintf("    create: templates/%s/%s\n", kind, name))
		sb.WriteString(fmt.Sprintf("    rm:     %s/%s\n", kind, name))
	}

	return errors.New(strings.TrimRight(sb.String(), "\n"))
}

// renderFile parses src as a text/template (cloned from the shared base so all
// partials are available), executes it with nil data, and writes the result to dst.
func renderFile(sharedTmpl *template.Template, src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("read template: %w", err)
	}

	// Clone the shared base so each file gets its own parse tree without
	// accumulating definitions across files.
	t, err := sharedTmpl.Clone()
	if err != nil {
		return fmt.Errorf("clone shared template: %w", err)
	}

	// Parse the source file as a new template named after the file.
	name := filepath.Base(src)
	t, err = t.New(name).Parse(string(data))
	if err != nil {
		return fmt.Errorf("parse: %w", err)
	}

	// Execute the named template (not "base").
	var sb strings.Builder
	if err := t.ExecuteTemplate(&sb, name, nil); err != nil {
		return fmt.Errorf("execute: %w", err)
	}

	if err := os.WriteFile(dst, []byte(sb.String()), 0o644); err != nil {
		return fmt.Errorf("write output: %w", err)
	}

	return nil
}
