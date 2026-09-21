package hooks

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// sessionIDPattern guards the state-file path: session_id is attacker-shaped
// input (it comes from the harness's JSON payload), and it becomes a filename
// component.
var sessionIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

var reviewAgents = map[string]bool{
	"atomic-reviewer": true,
	"atomic-auditor":  true,
}

var editTools = map[string]bool{
	"Edit":         true,
	"Write":        true,
	"MultiEdit":    true,
	"NotebookEdit": true,
}

// gateState is the folded view of a session's append-only event log: the
// touched-path set (deduped, first-seen order) and the last review
// fingerprint.
type gateState struct {
	Touched  []string `json:"touched"`
	Reviewed string   `json:"reviewed"`
}

func (s *gateState) addTouched(path string) {
	for _, p := range s.Touched {
		if p == path {
			return
		}
	}
	s.Touched = append(s.Touched, path)
}

// gateEvent is one line of the state file: exactly one of Touched or
// Reviewed is set.
type gateEvent struct {
	Touched  string `json:"touched,omitempty"`
	Reviewed string `json:"reviewed,omitempty"`
}

type postToolUsePayload struct {
	SessionID string `json:"session_id"`
	Cwd       string `json:"cwd"`
	AgentType string `json:"agent_type"`
	ToolName  string `json:"tool_name"`
	ToolInput struct {
		FilePath     string `json:"file_path"`
		SubagentType string `json:"subagent_type"`
	} `json:"tool_input"`
}

type stopPayload struct {
	SessionID      string `json:"session_id"`
	Cwd            string `json:"cwd"`
	StopHookActive bool   `json:"stop_hook_active"`
}

// PostToolUse records main-agent file edits and review-agent dispatches under
// the calling session, for Stop to gate against later. Every error — bad
// JSON, an unsafe session_id, a state-file write failure — is a silent no-op:
// this hook must never block the tool call it observes.
func PostToolUse(stdin []byte) {
	var payload postToolUsePayload
	if err := json.Unmarshal(stdin, &payload); err != nil {
		return
	}
	// A tool call made inside a subagent carries agent_type; only the main
	// agent's own edits are gated.
	if payload.AgentType != "" {
		return
	}

	sp, ok := statePath(payload.SessionID)
	if !ok {
		return
	}

	switch {
	case editTools[payload.ToolName]:
		if payload.ToolInput.FilePath == "" {
			return
		}
		appendEvent(sp, gateEvent{Touched: payload.ToolInput.FilePath})

	case payload.ToolName == "Agent":
		if !reviewAgents[payload.ToolInput.SubagentType] {
			return
		}
		st, err := foldState(sp)
		if err != nil {
			return
		}
		root, gated, err := gatedPaths(payload.Cwd, st.Touched)
		if err != nil {
			return
		}
		sum, err := fingerprint(root, gated)
		if err != nil {
			return
		}
		appendEvent(sp, gateEvent{Reviewed: sum})
	}
}

// Stop returns a non-empty block message only when at least one path
// recorded this session is inside the repo at cwd, dirty, not documentation,
// and its fingerprint has moved past the last recorded review. Every other
// outcome — stop_hook_active, no state, no git repo, malformed input, any
// git error — returns ""; the CLI layer maps a non-empty message to exit 2.
func Stop(stdin []byte) string {
	var payload stopPayload
	if err := json.Unmarshal(stdin, &payload); err != nil {
		return ""
	}
	if payload.StopHookActive {
		return ""
	}

	sp, ok := statePath(payload.SessionID)
	if !ok {
		return ""
	}
	st, err := foldState(sp)
	if err != nil || len(st.Touched) == 0 {
		return ""
	}

	root, gated, err := gatedPaths(payload.Cwd, st.Touched)
	if err != nil || len(gated) == 0 {
		return ""
	}

	sum, err := fingerprint(root, gated)
	if err != nil {
		return ""
	}
	if sum == st.Reviewed {
		return ""
	}

	return blockMessage(gated)
}

func blockMessage(paths []string) string {
	var sb strings.Builder
	n := len(paths)
	if n == 1 {
		sb.WriteString("atomic review gate: 1 file you edited this session is uncommitted and no review agent has read it since:\n")
	} else {
		fmt.Fprintf(&sb, "atomic review gate: %d files you edited this session are uncommitted and no review agent has read them since:\n", n)
	}

	shown := paths
	const maxShown = 10
	overflow := 0
	if n > maxShown {
		shown = paths[:maxShown]
		overflow = n - maxShown
	}
	for _, p := range shown {
		fmt.Fprintf(&sb, "%s\n", p)
	}
	if overflow > 0 {
		fmt.Fprintf(&sb, "(and %d more)\n", overflow)
	}

	sb.WriteString("Run the atomic-verify review gate before claiming done: dispatch atomic-auditor with diff: working and intent: <what the change was for>, act on its verdict, and report \"audit: <verdict> (atomic-auditor on <model>)\". If this is not a completion claim, say what is still in progress and continue.")

	return sb.String()
}

// statePath resolves session_id to its state-file path, rejecting anything
// that isn't a safe filename component.
func statePath(sessionID string) (string, bool) {
	if !sessionIDPattern.MatchString(sessionID) {
		return "", false
	}
	return filepath.Join(os.TempDir(), "atomic-hooks", sessionID+".jsonl"), true
}

// appendEvent adds one event line to the session's state file. Append-only
// so concurrent PostToolUse calls (parallel tool calls in one message) never
// race on a read-modify-write of the whole file.
func appendEvent(path string, ev gateEvent) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()

	data, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	_, err = f.Write(append(data, '\n'))
	return err
}

// foldState reads the event log and folds it into the current touched set
// and last review fingerprint. A missing file is empty state; an unparseable
// line is skipped rather than failing the whole fold.
func foldState(path string) (gateState, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return gateState{}, nil
		}
		return gateState{}, err
	}

	var st gateState
	for _, line := range strings.Split(string(data), "\n") {
		if line == "" {
			continue
		}
		var ev gateEvent
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			continue
		}
		if ev.Touched != "" {
			st.addTouched(ev.Touched)
		}
		if ev.Reviewed != "" {
			st.Reviewed = ev.Reviewed
		}
	}
	return st, nil
}

// docDirComponent matches "docs" as a path component at any depth.
var docDirComponent = regexp.MustCompile(`(^|/)docs(/|$)`)

var docTopLevelPrefixes = []string{
	"README", "CHANGELOG", "CONTRIBUTING", "CODE_OF_CONDUCT", "SECURITY", "LICENSE",
}

// isDocsPath applies the signals-gate documentation test to a repo-relative
// path. Kept in lockstep with context/_partials/signals-gate.md step 0.
func isDocsPath(relPath string) bool {
	relPath = filepath.ToSlash(relPath)
	if docDirComponent.MatchString(relPath) {
		return true
	}
	if strings.Contains(relPath, "/") {
		return false
	}
	upper := strings.ToUpper(relPath)
	for _, prefix := range docTopLevelPrefixes {
		if strings.HasPrefix(upper, prefix) {
			return true
		}
	}
	return false
}

// runGit runs git in cwd, returning stdout. Any non-zero exit — no repo, a
// bad pathspec, git missing — is an error for the caller to fail open on.
func runGit(cwd string, args ...string) ([]byte, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = cwd
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return out, nil
}

// repoRoot resolves the repository root for cwd, symlink-resolved so it
// compares cleanly against symlink-resolved touched paths (macOS puts
// /tmp and /var behind /private, so an unresolved comparison mismatches).
func repoRoot(cwd string) (string, error) {
	out, err := runGit(cwd, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", err
	}
	root := strings.TrimSpace(string(out))
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		return resolved, nil
	}
	return root, nil
}

// pathsInRepo drops any path that resolves outside root, so a single
// recorded path from another repo or user directory never aborts git for
// the whole set.
func pathsInRepo(root string, paths []string) []string {
	var in []string
	for _, p := range paths {
		resolved, err := filepath.EvalSymlinks(p)
		if err != nil {
			resolved = filepath.Clean(p)
		}
		rel, err := filepath.Rel(root, resolved)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		in = append(in, resolved)
	}
	return in
}

type statusRecord struct {
	Status string
	Path   string
}

// statusRecords runs git status scoped to paths and parses the NUL-separated
// porcelain records. -z avoids core.quotePath escaping non-ASCII names, and
// a rename record's second NUL field (the original path) is skipped — on a
// path-scoped status over a small recorded set, the new path is the one that
// matters.
func statusRecords(root string, paths []string) ([]statusRecord, error) {
	args := append([]string{"status", "--porcelain", "-z", "--untracked-files=all", "--"}, paths...)
	out, err := runGit(root, args...)
	if err != nil {
		return nil, err
	}

	var records []statusRecord
	fields := strings.Split(string(out), "\x00")
	for i := 0; i < len(fields); i++ {
		f := fields[i]
		if len(f) < 4 {
			continue
		}
		status := f[:2]
		records = append(records, statusRecord{Status: status, Path: f[3:]})
		if status[0] == 'R' || status[0] == 'C' {
			i++ // skip the rename/copy record's original-path field
		}
	}
	return records, nil
}

// dirtyPaths reports which of paths git considers changed, as repo-relative
// paths — git status prints paths relative to the repo root, which is what
// makes this comparable to isDocsPath's repo-relative contract.
func dirtyPaths(root string, paths []string) ([]string, error) {
	if len(paths) == 0 {
		return nil, nil
	}
	records, err := statusRecords(root, paths)
	if err != nil {
		return nil, err
	}
	var dirty []string
	for _, r := range records {
		dirty = append(dirty, r.Path)
	}
	return dirty, nil
}

// gatedPaths is the one set both fingerprints must cover; a review
// fingerprint taken over a wider set never matches Stop's and re-blocks
// every later stop. Returns the resolved repo root alongside the set, since
// every subsequent git call must run there rather than at the payload cwd.
func gatedPaths(cwd string, touched []string) (string, []string, error) {
	root, err := repoRoot(cwd)
	if err != nil {
		return "", nil, err
	}
	inRepo := pathsInRepo(root, touched)
	if len(inRepo) == 0 {
		return root, nil, nil
	}
	dirty, err := dirtyPaths(root, inRepo)
	if err != nil {
		return "", nil, err
	}
	var gated []string
	for _, p := range dirty {
		if !isDocsPath(p) {
			gated = append(gated, p)
		}
	}
	return root, gated, nil
}

// fingerprint hashes git diff HEAD over paths plus, for each untracked path
// among them, its path and file bytes.
func fingerprint(root string, paths []string) (string, error) {
	sorted := append([]string(nil), paths...)
	sort.Strings(sorted)

	h := sha256.New()

	if len(sorted) > 0 {
		diffArgs := append([]string{"diff", "HEAD", "--"}, sorted...)
		diffOut, err := runGit(root, diffArgs...)
		if err != nil {
			return "", err
		}
		h.Write(diffOut)

		records, err := statusRecords(root, sorted)
		if err != nil {
			return "", err
		}
		for _, r := range records {
			if r.Status != "??" {
				continue
			}
			h.Write([]byte(r.Path))
			data, err := os.ReadFile(filepath.Join(root, r.Path))
			if err != nil {
				return "", err
			}
			h.Write(data)
		}
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}
