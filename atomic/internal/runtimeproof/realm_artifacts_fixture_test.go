package runtimeproof

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func linkRealmRuntimeArtifacts(t *testing.T, ev ScenarioEvidence, consumer *redesignHome, authored map[string]string, paths []string) string {
	t.Helper()
	root := isolatedHome(t)
	publisher := &redesignHome{
		atomic:    consumer.atomic,
		root:      root,
		home:      filepath.Join(root, "home"),
		codexHome: filepath.Join(root, "codex-home"),
		repo:      filepath.Join(root, "publisher"),
	}
	writeTree(t, publisher.home, map[string]string{".atomic/profile.md": "# Runtimeproof publisher\n"})
	writeTree(t, publisher.repo, authored)
	for _, h := range []*redesignHome{publisher, consumer} {
		writeTree(t, h.repo, map[string]string{".atomic/atomic.toml": "scope = \"realm\"\n"})
	}

	// guidanceGit inherits repository overrides; keep fixture Git settings in child processes instead.
	run := func(h *redesignHome, dir, binary string, args ...string) string {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), binary, args...)
		cmd.Dir = dir
		for _, entry := range h.env() {
			key, _, _ := strings.Cut(entry, "=")
			key = strings.ToUpper(key)
			if !strings.HasPrefix(key, "GIT_") && !strings.HasPrefix(key, "XDG_") {
				cmd.Env = append(cmd.Env, entry)
			}
		}
		cmd.Env = append(cmd.Env,
			"GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1", "GIT_ALLOW_PROTOCOL=file",
			"GIT_AUTHOR_NAME=runtimeproof", "GIT_AUTHOR_EMAIL=runtimeproof@example.invalid",
			"GIT_COMMITTER_NAME=runtimeproof", "GIT_COMMITTER_EMAIL=runtimeproof@example.invalid",
			"XDG_CONFIG_HOME="+filepath.Join(h.root, "config"),
			"XDG_DATA_HOME="+filepath.Join(h.root, "data"),
			"XDG_STATE_HOME="+filepath.Join(h.root, "state"),
			"XDG_CACHE_HOME="+filepath.Join(h.root, "cache"),
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%s: %s %v in %s: %v\n%s", ev.Scenario, binary, args, dir, err, out)
		}
		return string(out)
	}

	remote := filepath.Join(root, "remote.git")
	run(publisher, root, "git", "init", "--bare", "--template=", "-b", "main", remote)
	digestLine := regexp.MustCompile(`^Review digest: (sha256:[[:xdigit:]]{64})$`)
	approve := func(h *redesignHome, verb string, flags ...string) {
		t.Helper()
		args := append([]string{"--no-update-check", "realm", verb}, flags...)
		previewArgs := append(args, "--dry-run", "--")
		preview := run(h, h.repo, h.atomic, append(previewArgs, paths...)...)
		var digest string
		for line := range strings.SplitSeq(preview, "\n") {
			if !strings.HasPrefix(line, "Review digest:") {
				continue
			}
			match := digestLine.FindStringSubmatch(line)
			if len(match) != 2 || digest != "" {
				t.Fatalf("realm %s preview must contain exactly one Review digest: sha256:<hex> line:\n%s", verb, preview)
			}
			digest = match[1]
		}
		if digest == "" {
			t.Fatalf("realm %s preview lacks a Review digest: sha256:<hex> line:\n%s", verb, preview)
		}
		applyArgs := append(args, "--yes", "--review", digest, "--")
		run(h, h.repo, h.atomic, append(applyArgs, paths...)...)
	}
	approve(publisher, "share", "--remote", remote, "--branch", "main")
	run(consumer, consumer.repo, consumer.atomic, "--no-update-check", "realm", "join", remote)
	approve(consumer, "link")
	return consumer.repo
}
