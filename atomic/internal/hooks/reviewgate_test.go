package hooks_test

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/hooks"
)

// initGitRepo creates a repo with one committed file so diff/status have a
// HEAD to compare against, and sets author identity so commits work in CI
// (no global git config assumed).
func initGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGitT(t, dir, "init", "-q")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("seed\n"), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	runGitT(t, dir, "add", "-A")
	commitGitT(t, dir, "seed")
	return dir
}

func runGitT(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func commitGitT(t *testing.T, dir, msg string) {
	t.Helper()
	cmd := exec.Command("git", "-c", "user.name=test", "-c", "user.email=test@example.com", "commit", "-q", "-m", msg)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git commit: %v\n%s", err, out)
	}
}

func postToolUsePayload(t *testing.T, m map[string]any) []byte {
	t.Helper()
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return b
}

// newSessionID derives a session_id from the test name, sanitized to the
// [A-Za-z0-9_-]+ shape statePath requires — a subtest name contains "/".
func newSessionID(t *testing.T) string {
	t.Helper()
	safe := strings.NewReplacer("/", "-", " ", "-").Replace(t.Name())
	return fmt.Sprintf("sess-%s", safe)
}

// Subagent tool calls carry agent_type; a subagent's own edits (e.g.
// atomic-implementer inside a loop) must not count toward the main agent's
// gate, or every checkpoint diff would re-open it.
func TestPostToolUse_SubagentCallIgnored(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	sid := newSessionID(t)
	dir := initGitRepo(t)

	payload := postToolUsePayload(t, map[string]any{
		"session_id": sid,
		"cwd":        dir,
		"agent_type": "atomic-implementer",
		"tool_name":  "Edit",
		"tool_input": map[string]any{"file_path": filepath.Join(dir, "README.md")},
	})
	hooks.PostToolUse(payload)

	stop := postToolUsePayload(t, map[string]any{"session_id": sid, "cwd": dir})
	if msg := hooks.Stop(stop); msg != "" {
		t.Fatalf("expected no gate for a subagent edit, got message: %q", msg)
	}
}

// Edit/Write/MultiEdit/NotebookEdit all carry tool_input.file_path and all
// must land in state, since any of them is how the main agent changes a file.
func TestPostToolUse_EditToolsRecordPath(t *testing.T) {
	for _, tool := range []string{"Edit", "Write", "MultiEdit", "NotebookEdit"} {
		t.Run(tool, func(t *testing.T) {
			t.Setenv("TMPDIR", t.TempDir())
			sid := newSessionID(t)
			dir := initGitRepo(t)
			target := filepath.Join(dir, "README.md")
			if err := os.WriteFile(target, []byte("changed\n"), 0o644); err != nil {
				t.Fatalf("write: %v", err)
			}

			payload := postToolUsePayload(t, map[string]any{
				"session_id": sid,
				"cwd":        dir,
				"tool_name":  tool,
				"tool_input": map[string]any{"file_path": target},
			})
			hooks.PostToolUse(payload)

			// README.md is a docs path, so it never blocks — swap in a source
			// file to prove the path was actually recorded and gates.
			src := filepath.Join(dir, "main.go")
			if err := os.WriteFile(src, []byte("package main\n"), 0o644); err != nil {
				t.Fatalf("write source: %v", err)
			}
			runGitT(t, dir, "add", "main.go")
			commitGitT(t, dir, "add source")
			if err := os.WriteFile(src, []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
				t.Fatalf("edit source: %v", err)
			}
			payload2 := postToolUsePayload(t, map[string]any{
				"session_id": sid,
				"cwd":        dir,
				"tool_name":  tool,
				"tool_input": map[string]any{"file_path": src},
			})
			hooks.PostToolUse(payload2)

			stop := postToolUsePayload(t, map[string]any{"session_id": sid, "cwd": dir})
			if msg := hooks.Stop(stop); msg == "" {
				t.Fatalf("expected the %s-recorded source edit to gate the stop", tool)
			}
		})
	}
}

// An Agent call naming atomic-reviewer or atomic-auditor records a review
// fingerprint over the touched set; a subsequent Stop with the same dirty
// content must pass.
func TestPostToolUse_ReviewRecordsFingerprint(t *testing.T) {
	for _, agent := range []string{"atomic-reviewer", "atomic-auditor"} {
		t.Run(agent, func(t *testing.T) {
			t.Setenv("TMPDIR", t.TempDir())
			sid := newSessionID(t)
			dir := initGitRepo(t)
			src := filepath.Join(dir, "main.go")
			if err := os.WriteFile(src, []byte("package main\n"), 0o644); err != nil {
				t.Fatalf("write: %v", err)
			}
			runGitT(t, dir, "add", "main.go")
			commitGitT(t, dir, "add source")
			if err := os.WriteFile(src, []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
				t.Fatalf("edit source: %v", err)
			}

			edit := postToolUsePayload(t, map[string]any{
				"session_id": sid,
				"cwd":        dir,
				"tool_name":  "Edit",
				"tool_input": map[string]any{"file_path": src},
			})
			hooks.PostToolUse(edit)

			review := postToolUsePayload(t, map[string]any{
				"session_id": sid,
				"cwd":        dir,
				"tool_name":  "Agent",
				"tool_input": map[string]any{"subagent_type": agent},
			})
			hooks.PostToolUse(review)

			stop := postToolUsePayload(t, map[string]any{"session_id": sid, "cwd": dir})
			if msg := hooks.Stop(stop); msg != "" {
				t.Fatalf("expected reviewed content to pass, got: %q", msg)
			}
		})
	}
}

// Review and stop fingerprints cover the same gated set; see
// docs/spec/review-gate-hook.md change log.
func TestPostToolUse_ReviewFingerprintMatchesGatedSet(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	sid := newSessionID(t)
	dir := initGitRepo(t)

	docPath := filepath.Join(dir, "docs", "x.md")
	if err := os.MkdirAll(filepath.Dir(docPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(docPath, []byte("seed\n"), 0o644); err != nil {
		t.Fatalf("write docs: %v", err)
	}
	src := filepath.Join(dir, "main.go")
	if err := os.WriteFile(src, []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	runGitT(t, dir, "add", "-A")
	commitGitT(t, dir, "seed docs and source")

	if err := os.WriteFile(docPath, []byte("changed\n"), 0o644); err != nil {
		t.Fatalf("edit docs: %v", err)
	}
	if err := os.WriteFile(src, []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatalf("edit source: %v", err)
	}

	for _, path := range []string{docPath, src} {
		edit := postToolUsePayload(t, map[string]any{
			"session_id": sid, "cwd": dir, "tool_name": "Edit",
			"tool_input": map[string]any{"file_path": path},
		})
		hooks.PostToolUse(edit)
	}

	review := postToolUsePayload(t, map[string]any{
		"session_id": sid, "cwd": dir, "tool_name": "Agent",
		"tool_input": map[string]any{"subagent_type": "atomic-auditor"},
	})
	hooks.PostToolUse(review)

	stop := postToolUsePayload(t, map[string]any{"session_id": sid, "cwd": dir})
	if msg := hooks.Stop(stop); msg != "" {
		t.Fatalf("expected reviewed docs+source touch to pass, got msg=%q", msg)
	}

	if err := os.WriteFile(src, []byte("package main\n\nfunc main() { println(1) }\n"), 0o644); err != nil {
		t.Fatalf("second source edit: %v", err)
	}
	edit2 := postToolUsePayload(t, map[string]any{
		"session_id": sid, "cwd": dir, "tool_name": "Edit",
		"tool_input": map[string]any{"file_path": src},
	})
	hooks.PostToolUse(edit2)

	if msg := hooks.Stop(stop); msg == "" {
		t.Fatal("expected the post-review source edit to re-open the gate")
	}
}

// An Agent call for a subagent that is not a review agent (e.g. an
// investigator) must not close the gate.
func TestPostToolUse_NonReviewAgentDoesNotCloseTheGate(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	sid := newSessionID(t)
	dir := initGitRepo(t)
	src := filepath.Join(dir, "main.go")
	if err := os.WriteFile(src, []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	runGitT(t, dir, "add", "main.go")
	commitGitT(t, dir, "add source")
	if err := os.WriteFile(src, []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatalf("edit source: %v", err)
	}

	edit := postToolUsePayload(t, map[string]any{
		"session_id": sid,
		"cwd":        dir,
		"tool_name":  "Edit",
		"tool_input": map[string]any{"file_path": src},
	})
	hooks.PostToolUse(edit)

	explore := postToolUsePayload(t, map[string]any{
		"session_id": sid,
		"cwd":        dir,
		"tool_name":  "Agent",
		"tool_input": map[string]any{"subagent_type": "atomic-investigator"},
	})
	hooks.PostToolUse(explore)

	stop := postToolUsePayload(t, map[string]any{"session_id": sid, "cwd": dir})
	if msg := hooks.Stop(stop); msg == "" {
		t.Fatal("expected atomic-investigator to leave the gate open")
	}
}

// The signals-gate documentation test: docs/ at any depth and root-level
// README/CHANGELOG/etc are exempt; a bundled-artifact .md is source.
func TestIsDocsPath(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	sid := newSessionID(t)
	dir := initGitRepo(t)

	docPath := filepath.Join(dir, "docs", "guides", "install.md")
	if err := os.MkdirAll(filepath.Dir(docPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(docPath, []byte("seed\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	runGitT(t, dir, "add", "-A")
	commitGitT(t, dir, "add docs")
	if err := os.WriteFile(docPath, []byte("changed\n"), 0o644); err != nil {
		t.Fatalf("edit docs: %v", err)
	}

	edit := postToolUsePayload(t, map[string]any{
		"session_id": sid,
		"cwd":        dir,
		"tool_name":  "Edit",
		"tool_input": map[string]any{"file_path": docPath},
	})
	hooks.PostToolUse(edit)

	stop := postToolUsePayload(t, map[string]any{"session_id": sid, "cwd": dir})
	if msg := hooks.Stop(stop); msg != "" {
		t.Fatalf("expected docs/ edit to be exempt, got: %q", msg)
	}

	// A bundled-artifact .md is source, not documentation.
	sid2 := sid + "-artifact"
	artifactPath := filepath.Join(dir, "skills", "atomic-verify", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(artifactPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(artifactPath, []byte("seed\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	runGitT(t, dir, "add", "-A")
	commitGitT(t, dir, "add artifact")
	if err := os.WriteFile(artifactPath, []byte("changed\n"), 0o644); err != nil {
		t.Fatalf("edit artifact: %v", err)
	}

	edit2 := postToolUsePayload(t, map[string]any{
		"session_id": sid2,
		"cwd":        dir,
		"tool_name":  "Edit",
		"tool_input": map[string]any{"file_path": artifactPath},
	})
	hooks.PostToolUse(edit2)
	stop2 := postToolUsePayload(t, map[string]any{"session_id": sid2, "cwd": dir})
	if msg := hooks.Stop(stop2); msg == "" {
		t.Fatal("expected a bundled-artifact .md edit to gate as source")
	}
}

// The core positive case: an unreviewed dirty edit blocks and names the path.
func TestStop_BlocksUnreviewedDirtyEdit(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	sid := newSessionID(t)
	dir := initGitRepo(t)
	src := filepath.Join(dir, "main.go")
	if err := os.WriteFile(src, []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	runGitT(t, dir, "add", "main.go")
	commitGitT(t, dir, "add source")
	if err := os.WriteFile(src, []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatalf("edit source: %v", err)
	}

	edit := postToolUsePayload(t, map[string]any{
		"session_id": sid,
		"cwd":        dir,
		"tool_name":  "Edit",
		"tool_input": map[string]any{"file_path": src},
	})
	hooks.PostToolUse(edit)

	stop := postToolUsePayload(t, map[string]any{"session_id": sid, "cwd": dir})
	msg := hooks.Stop(stop)
	if msg == "" {
		t.Fatal("expected the unreviewed dirty edit to block")
	}
	if !strings.Contains(msg, "main.go") {
		t.Fatalf("expected block message to name main.go, got: %q", msg)
	}
}

// A path outside the repository (e.g. a global profile or memory file) must
// not disable the gate for the paths that ARE in the repo: dropping it, not
// aborting the whole git call, is what keeps main.go gated.
func TestStop_IgnoresPathOutsideRepo(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	sid := newSessionID(t)
	dir := initGitRepo(t)
	outside := t.TempDir()

	src := filepath.Join(dir, "main.go")
	if err := os.WriteFile(src, []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	runGitT(t, dir, "add", "main.go")
	commitGitT(t, dir, "add source")
	if err := os.WriteFile(src, []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatalf("edit source: %v", err)
	}

	outsideFile := filepath.Join(outside, "profile.md")
	if err := os.WriteFile(outsideFile, []byte("notes\n"), 0o644); err != nil {
		t.Fatalf("write outside file: %v", err)
	}

	for _, p := range []string{outsideFile, src} {
		edit := postToolUsePayload(t, map[string]any{
			"session_id": sid, "cwd": dir, "tool_name": "Edit",
			"tool_input": map[string]any{"file_path": p},
		})
		hooks.PostToolUse(edit)
	}

	stop := postToolUsePayload(t, map[string]any{"session_id": sid, "cwd": dir})
	msg := hooks.Stop(stop)
	if msg == "" {
		t.Fatal("expected the in-repo edit to block despite an out-of-repo path recorded")
	}
	if !strings.Contains(msg, "main.go") {
		t.Fatalf("expected block message to name main.go, got: %q", msg)
	}
}

// SC4's re-open property must hold when the session's cwd is a repo
// subdirectory: all git runs at the repo root, not cwd, so pathspecs
// resolve correctly either way.
func TestStop_ReopensGateFromSubdirectoryCwd(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	sid := newSessionID(t)
	dir := initGitRepo(t)
	sub := filepath.Join(dir, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	src := filepath.Join(sub, "a.go")
	if err := os.WriteFile(src, []byte("package sub\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	runGitT(t, dir, "add", "-A")
	commitGitT(t, dir, "add sub")

	if err := os.WriteFile(src, []byte("package sub\n\nfunc F() {}\n"), 0o644); err != nil {
		t.Fatalf("edit: %v", err)
	}
	edit := postToolUsePayload(t, map[string]any{
		"session_id": sid, "cwd": sub, "tool_name": "Edit",
		"tool_input": map[string]any{"file_path": src},
	})
	hooks.PostToolUse(edit)

	review := postToolUsePayload(t, map[string]any{
		"session_id": sid, "cwd": sub, "tool_name": "Agent",
		"tool_input": map[string]any{"subagent_type": "atomic-auditor"},
	})
	hooks.PostToolUse(review)

	stop := postToolUsePayload(t, map[string]any{"session_id": sid, "cwd": sub})
	if msg := hooks.Stop(stop); msg != "" {
		t.Fatalf("expected clean stop after review from a subdirectory cwd, got: %q", msg)
	}

	if err := os.WriteFile(src, []byte("package sub\n\nfunc F() { println(1) }\n"), 0o644); err != nil {
		t.Fatalf("second edit: %v", err)
	}
	edit2 := postToolUsePayload(t, map[string]any{
		"session_id": sid, "cwd": sub, "tool_name": "Edit",
		"tool_input": map[string]any{"file_path": src},
	})
	hooks.PostToolUse(edit2)

	if msg := hooks.Stop(stop); msg == "" {
		t.Fatal("expected the post-review edit to re-open the gate from a subdirectory cwd")
	}
}

// Parallel tool calls in one message fire concurrent post-tool-use
// processes; append-only events must all survive the fold.
func TestPostToolUse_ConcurrentEventsAllFold(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	sid := newSessionID(t)
	dir := initGitRepo(t)

	const n = 20
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			path := filepath.Join(dir, fmt.Sprintf("f%02d.go", i))
			if err := os.WriteFile(path, []byte("package p\n"), 0o644); err != nil {
				t.Errorf("write %d: %v", i, err)
				return
			}
			edit := postToolUsePayload(t, map[string]any{
				"session_id": sid, "cwd": dir, "tool_name": "Edit",
				"tool_input": map[string]any{"file_path": path},
			})
			hooks.PostToolUse(edit)
		}(i)
	}
	wg.Wait()

	stop := postToolUsePayload(t, map[string]any{"session_id": sid, "cwd": dir})
	msg := hooks.Stop(stop)
	if msg == "" {
		t.Fatal("expected all 20 concurrently touched files to gate the stop")
	}
	if !strings.Contains(msg, fmt.Sprintf("%d files you edited", n)) {
		t.Fatalf("expected block message to report all %d touched files, got: %q", n, msg)
	}
}

// git status --porcelain -z reports non-ASCII names unquoted and
// NUL-separated; a quoted name would fail isDocsPath's plain-string match
// and fail the fingerprint's file read, both failing open.
func TestStop_HandlesNonASCIIFilename(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	sid := newSessionID(t)
	dir := initGitRepo(t)
	src := filepath.Join(dir, "café.go")
	if err := os.WriteFile(src, []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	edit := postToolUsePayload(t, map[string]any{
		"session_id": sid, "cwd": dir, "tool_name": "Edit",
		"tool_input": map[string]any{"file_path": src},
	})
	hooks.PostToolUse(edit)

	stop := postToolUsePayload(t, map[string]any{"session_id": sid, "cwd": dir})
	msg := hooks.Stop(stop)
	if msg == "" {
		t.Fatal("expected the untracked café.go to block")
	}
	if !strings.Contains(msg, "café.go") {
		t.Fatalf("expected block message to name café.go unquoted, got: %q", msg)
	}
}

// A post-review edit moves the fingerprint, so the gate re-opens even though
// a review already happened once this session.
func TestStop_BlocksAgainAfterPostReviewEdit(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	sid := newSessionID(t)
	dir := initGitRepo(t)
	src := filepath.Join(dir, "main.go")
	if err := os.WriteFile(src, []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	runGitT(t, dir, "add", "main.go")
	commitGitT(t, dir, "add source")
	if err := os.WriteFile(src, []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatalf("edit source: %v", err)
	}

	edit := postToolUsePayload(t, map[string]any{
		"session_id": sid, "cwd": dir, "tool_name": "Edit",
		"tool_input": map[string]any{"file_path": src},
	})
	hooks.PostToolUse(edit)
	review := postToolUsePayload(t, map[string]any{
		"session_id": sid, "cwd": dir, "tool_name": "Agent",
		"tool_input": map[string]any{"subagent_type": "atomic-auditor"},
	})
	hooks.PostToolUse(review)

	// Confirm the gate is closed before the second edit.
	stop := postToolUsePayload(t, map[string]any{"session_id": sid, "cwd": dir})
	if msg := hooks.Stop(stop); msg != "" {
		t.Fatalf("expected clean stop after review, got msg=%q", msg)
	}

	if err := os.WriteFile(src, []byte("package main\n\nfunc main() { println(1) }\n"), 0o644); err != nil {
		t.Fatalf("second edit: %v", err)
	}
	edit2 := postToolUsePayload(t, map[string]any{
		"session_id": sid, "cwd": dir, "tool_name": "Edit",
		"tool_input": map[string]any{"file_path": src},
	})
	hooks.PostToolUse(edit2)

	if msg := hooks.Stop(stop); msg == "" {
		t.Fatal("expected the post-review edit to re-open the gate")
	}
}

// A commit that cleans every recorded path closes the gate: nothing is dirty.
func TestStop_PassesAfterCommit(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	sid := newSessionID(t)
	dir := initGitRepo(t)
	src := filepath.Join(dir, "main.go")
	if err := os.WriteFile(src, []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	runGitT(t, dir, "add", "main.go")
	commitGitT(t, dir, "add source")
	if err := os.WriteFile(src, []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatalf("edit source: %v", err)
	}

	edit := postToolUsePayload(t, map[string]any{
		"session_id": sid, "cwd": dir, "tool_name": "Edit",
		"tool_input": map[string]any{"file_path": src},
	})
	hooks.PostToolUse(edit)

	runGitT(t, dir, "add", "main.go")
	commitGitT(t, dir, "unreviewed edit")

	stop := postToolUsePayload(t, map[string]any{"session_id": sid, "cwd": dir})
	if msg := hooks.Stop(stop); msg != "" {
		t.Fatalf("expected a clean commit to close the gate, got: %q", msg)
	}
}

// Every fail-open path returns "" rather than erroring or blocking.
func TestStop_FailsOpen(t *testing.T) {
	t.Run("stop_hook_active", func(t *testing.T) {
		t.Setenv("TMPDIR", t.TempDir())
		sid := newSessionID(t)
		dir := initGitRepo(t)
		src := filepath.Join(dir, "main.go")
		os.WriteFile(src, []byte("package main\n"), 0o644)
		runGitT(t, dir, "add", "main.go")
		commitGitT(t, dir, "add source")
		os.WriteFile(src, []byte("package main\n\nfunc main() {}\n"), 0o644)

		edit := postToolUsePayload(t, map[string]any{
			"session_id": sid, "cwd": dir, "tool_name": "Edit",
			"tool_input": map[string]any{"file_path": src},
		})
		hooks.PostToolUse(edit)

		stop := postToolUsePayload(t, map[string]any{
			"session_id": sid, "cwd": dir, "stop_hook_active": true,
		})
		if msg := hooks.Stop(stop); msg != "" {
			t.Fatalf("expected no gate on stop_hook_active, got msg=%q", msg)
		}
	})

	t.Run("no state for session", func(t *testing.T) {
		t.Setenv("TMPDIR", t.TempDir())
		stop := postToolUsePayload(t, map[string]any{
			"session_id": newSessionID(t), "cwd": initGitRepo(t),
		})
		if msg := hooks.Stop(stop); msg != "" {
			t.Fatalf("expected no gate with no recorded state, got msg=%q", msg)
		}
	})

	t.Run("no git repo", func(t *testing.T) {
		t.Setenv("TMPDIR", t.TempDir())
		sid := newSessionID(t)
		notARepo := t.TempDir()
		src := filepath.Join(notARepo, "main.go")
		if err := os.WriteFile(src, []byte("package main\n"), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
		edit := postToolUsePayload(t, map[string]any{
			"session_id": sid, "cwd": notARepo, "tool_name": "Edit",
			"tool_input": map[string]any{"file_path": src},
		})
		hooks.PostToolUse(edit)
		stop := postToolUsePayload(t, map[string]any{"session_id": sid, "cwd": notARepo})
		if msg := hooks.Stop(stop); msg != "" {
			t.Fatalf("expected no gate with no git repo, got msg=%q", msg)
		}
	})

	t.Run("malformed input", func(t *testing.T) {
		t.Setenv("TMPDIR", t.TempDir())
		if msg := hooks.Stop([]byte("not json")); msg != "" {
			t.Fatalf("expected no gate on malformed input, got msg=%q", msg)
		}
		hooks.PostToolUse([]byte("not json"))
	})

	t.Run("unsafe session_id", func(t *testing.T) {
		t.Setenv("TMPDIR", t.TempDir())
		dir := initGitRepo(t)
		unsafe := "../escape"
		edit := postToolUsePayload(t, map[string]any{
			"session_id": unsafe, "cwd": dir, "tool_name": "Edit",
			"tool_input": map[string]any{"file_path": filepath.Join(dir, "README.md")},
		})
		hooks.PostToolUse(edit)
		stop := postToolUsePayload(t, map[string]any{"session_id": unsafe, "cwd": dir})
		if msg := hooks.Stop(stop); msg != "" {
			t.Fatalf("expected an unsafe session_id to no-op, got msg=%q", msg)
		}
	})
}

// A process killed mid-write leaves a partial line; the fold must keep every
// well-formed event around it instead of dropping the session's state.
func TestStop_FoldSkipsUnparseableStateLine(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	sid := newSessionID(t)
	dir := initGitRepo(t)

	path := filepath.Join(dir, "main.go")
	if err := os.WriteFile(path, []byte("package main\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	hooks.PostToolUse(postToolUsePayload(t, map[string]any{
		"session_id": sid, "cwd": dir, "tool_name": "Edit",
		"tool_input": map[string]any{"file_path": path},
	}))

	stateFile := filepath.Join(os.TempDir(), "atomic-hooks", sid+".jsonl")
	f, err := os.OpenFile(stateFile, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("open state file: %v", err)
	}
	if _, err := f.WriteString(`{"touched":"/trunc`); err != nil {
		t.Fatal(err)
	}
	f.Close()

	stop := postToolUsePayload(t, map[string]any{"session_id": sid, "cwd": dir})
	msg := hooks.Stop(stop)
	if !strings.Contains(msg, "main.go") {
		t.Fatalf("expected the well-formed event to survive a truncated line, got msg=%q", msg)
	}
}
