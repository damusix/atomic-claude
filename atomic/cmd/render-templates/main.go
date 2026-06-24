// render-templates renders the artifact templates from <repoRoot>/templates/
// into the corresponding output directories (commands/ etc.) under <outDir>.
//
// Run via: go run ./cmd/render-templates -repo <path> -outdir <path>
// Or from atomic/Makefile: make render
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/templaterender"
)

func main() {
	var repoRoot string
	var outDir string
	flag.StringVar(&repoRoot, "repo", "", "path to repo root (parent of atomic/); defaults to ../ relative to cwd")
	flag.StringVar(&outDir, "outdir", "", "path to write rendered outputs into; defaults to repo root")
	flag.Parse()

	wd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "render-templates: get cwd: %v\n", err)
		os.Exit(1)
	}

	if repoRoot == "" {
		// Default: if cwd ends in "atomic", repo root is one level up.
		if strings.HasSuffix(filepath.ToSlash(wd), "/atomic") {
			repoRoot = filepath.Join(wd, "..")
		} else {
			repoRoot = wd
		}
	}

	repoRoot, err = filepath.Abs(repoRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "render-templates: resolve repo root: %v\n", err)
		os.Exit(1)
	}

	if outDir == "" {
		outDir = repoRoot
	}

	outDir, err = filepath.Abs(outDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "render-templates: resolve outdir: %v\n", err)
		os.Exit(1)
	}

	if err := templaterender.Run(repoRoot, outDir); err != nil {
		fmt.Fprintf(os.Stderr, "render-templates: %v\n", err)
		os.Exit(1)
	}

	fmt.Fprintln(os.Stdout, "render-templates: done")
}
