// Package dockerinit scaffolds the Docker eval environment for end users.
// It renders four templates into a target directory and creates the bind-mount
// placeholder directories expected by docker-compose.
package dockerinit

import (
	"bytes"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"text/template"
)

//go:embed templates/Dockerfile.tmpl
var dockerfileTmpl string

//go:embed templates/docker-compose.yml.tmpl
var composeTmpl string

//go:embed templates/entrypoint.sh.tmpl
var entrypointTmpl string

//go:embed templates/dockerignore.tmpl
var dockerignoreTmpl string

// ActionKind classifies what Init did (or would do) to a file.
type ActionKind string

const (
	ActionCreated     ActionKind = "created"
	ActionOverwritten ActionKind = "overwritten"
	ActionSkipped     ActionKind = "skipped" // pre-existing + no --force
)

// FileAction describes what Init did for one output file.
type FileAction struct {
	Path string // relative to TargetDir
	Kind ActionKind
}

// Options controls Init behaviour.
type Options struct {
	TargetDir     string // absolute path; caller resolves ~
	Force         bool   // overwrite existing files
	AtomicVersion string // e.g. version.Version
	HostUID       int    // host user UID; defaults to 1000 if 0
}

type templateEntry struct {
	src  string // template source string
	out  string // output path relative to TargetDir
	mode os.FileMode
}

var templateEntries = []templateEntry{
	{src: dockerfileTmpl, out: "Dockerfile", mode: 0644},
	{src: composeTmpl, out: "docker-compose.yml", mode: 0644},
	{src: entrypointTmpl, out: "docker-entrypoint.sh", mode: 0755},
	{src: dockerignoreTmpl, out: ".dockerignore", mode: 0644},
}

// gitkeepPaths are plain empty files scaffolded so docker-compose bind mounts
// have a target directory on first run.
var gitkeepPaths = []string{
	"tmp/workspace/.gitkeep",
	"tmp/claude-home/.gitkeep",
}

// Init writes the templated files into opts.TargetDir.
// It creates opts.TargetDir (and any parents) if it does not exist.
// Pre-existing files are reported as ActionSkipped unless opts.Force is true.
// Returns FileActions in deterministic (alphabetical) order.
// Returns an error only on irrecoverable failure (cannot create dir, template
// render failure, write failure).
func Init(opts Options) ([]FileAction, error) {
	if opts.HostUID == 0 {
		opts.HostUID = 1000
	}

	if err := os.MkdirAll(opts.TargetDir, 0755); err != nil {
		return nil, fmt.Errorf("dockerinit: create target dir: %w", err)
	}

	var actions []FileAction

	for _, entry := range templateEntries {
		a, err := writeTemplate(opts, entry)
		if err != nil {
			return nil, err
		}
		actions = append(actions, a)
	}

	for _, rel := range gitkeepPaths {
		a, err := writeGitkeep(opts, rel)
		if err != nil {
			return nil, err
		}
		actions = append(actions, a)
	}

	sort.Slice(actions, func(i, j int) bool {
		return actions[i].Path < actions[j].Path
	})

	return actions, nil
}

func writeTemplate(opts Options, entry templateEntry) (FileAction, error) {
	dest := filepath.Join(opts.TargetDir, filepath.FromSlash(entry.out))

	_, err := os.Stat(dest)
	exists := err == nil

	if exists && !opts.Force {
		return FileAction{Path: entry.out, Kind: ActionSkipped}, nil
	}

	tmpl, err := template.New(entry.out).Parse(entry.src)
	if err != nil {
		return FileAction{}, fmt.Errorf("dockerinit: parse template %s: %w", entry.out, err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, opts); err != nil {
		return FileAction{}, fmt.Errorf("dockerinit: render template %s: %w", entry.out, err)
	}

	if err := os.WriteFile(dest, buf.Bytes(), entry.mode); err != nil {
		return FileAction{}, fmt.Errorf("dockerinit: write %s: %w", entry.out, err)
	}

	kind := ActionCreated
	if exists {
		kind = ActionOverwritten
	}
	return FileAction{Path: entry.out, Kind: kind}, nil
}

func writeGitkeep(opts Options, rel string) (FileAction, error) {
	dest := filepath.Join(opts.TargetDir, filepath.FromSlash(rel))

	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		return FileAction{}, fmt.Errorf("dockerinit: create dir for %s: %w", rel, err)
	}

	_, err := os.Stat(dest)
	exists := err == nil

	if exists && !opts.Force {
		return FileAction{Path: rel, Kind: ActionSkipped}, nil
	}

	if err := os.WriteFile(dest, []byte{}, 0644); err != nil {
		return FileAction{}, fmt.Errorf("dockerinit: write %s: %w", rel, err)
	}

	kind := ActionCreated
	if exists {
		kind = ActionOverwritten
	}
	return FileAction{Path: rel, Kind: kind}, nil
}
