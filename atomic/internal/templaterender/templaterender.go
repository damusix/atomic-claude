// Package templaterender implements the template rendering logic used by
// cmd/render-templates. It loads shared partials from <repoRoot>/templates/shared/,
// walks <repoRoot>/templates/commands/, renders each via text/template, and writes
// the output to <outDir>/commands/<name>.md.
//
// The orphan rule halts with a non-zero exit when any <outDir>/commands/<name>.md
// lacks a corresponding <repoRoot>/templates/commands/<name>.md.
package templaterender

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/template"
)

// Run is the main entry point. repoRoot is the path to the repository root
// (parent of atomic/); outDir is the directory where rendered outputs are
// written (commands/ subdirectory will be created inside it).
func Run(repoRoot, outDir string) error {
	templatesDir := filepath.Join(repoRoot, "templates")
	sharedDir := filepath.Join(templatesDir, "shared")
	commandsTemplDir := filepath.Join(templatesDir, "commands")
	commandsOutDir := filepath.Join(outDir, "commands")

	// Load all shared partials.
	sharedTmpl, err := loadSharedPartials(sharedDir)
	if err != nil {
		return fmt.Errorf("load shared partials: %w", err)
	}

	// Enumerate source templates.
	srcTemplates, err := listMDFiles(commandsTemplDir)
	if err != nil {
		return fmt.Errorf("list templates/commands: %w", err)
	}

	// Orphan detection: enumerate existing output files, check each has a template.
	if err := checkOrphans(commandsOutDir, commandsTemplDir, srcTemplates); err != nil {
		return err
	}

	if len(srcTemplates) == 0 {
		return nil
	}

	if err := os.MkdirAll(commandsOutDir, 0o755); err != nil {
		return fmt.Errorf("create commands output dir: %w", err)
	}

	for _, name := range srcTemplates {
		src := filepath.Join(commandsTemplDir, name)
		dst := filepath.Join(commandsOutDir, name)
		if err := renderFile(sharedTmpl, src, dst); err != nil {
			return fmt.Errorf("render %s: %w", name, err)
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
// Returns nil (not an error) if dir does not exist.
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

// checkOrphans enumerates outDir/commands/*.md and returns an error for any
// file that has no corresponding source template. The error message names both
// remediation paths: create the template OR rm the output file.
func checkOrphans(commandsOutDir, commandsTemplDir string, srcTemplates []string) error {
	existing, err := listMDFiles(commandsOutDir)
	if err != nil {
		return fmt.Errorf("list output commands: %w", err)
	}

	// Build a set of template names for O(1) lookup.
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
	sb.WriteString("every commands/ file must have a matching template.\n")
	sb.WriteString("Remediation: for each orphan, either\n")
	sb.WriteString("  (a) create the missing template, or\n")
	sb.WriteString("  (b) rm the orphan output file.\n\n")
	for _, name := range orphans {
		sb.WriteString(fmt.Sprintf("  orphan: commands/%s\n", name))
		sb.WriteString(fmt.Sprintf("    create: templates/commands/%s\n", name))
		sb.WriteString(fmt.Sprintf("    rm:     commands/%s\n", name))
	}

	return fmt.Errorf("%s", strings.TrimRight(sb.String(), "\n"))
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
