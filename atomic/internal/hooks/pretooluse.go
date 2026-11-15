package hooks

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/repoctx"
	"github.com/damusix/atomic-claude/atomic/internal/rules"
)

const sentRetention = 7 * 24 * time.Hour

type cardsSent struct {
	Sessions map[string]sessionCards `json:"sessions"`
}

type sessionCards struct {
	LastSeen time.Time `json:"last_seen"`
	Cards    []string  `json:"cards"`
}

// PreToolUse returns the hook JSON for the unsent wiki cards an apply_patch
// payload matches; any failure returns "" so a card never fails the edit.
func PreToolUse(in io.Reader, home string, now time.Time) string {
	var payload struct {
		SessionID string `json:"session_id"`
		Cwd       string `json:"cwd"`
		ToolInput struct {
			Command string `json:"command"`
		} `json:"tool_input"`
	}
	if err := json.NewDecoder(io.LimitReader(in, 1<<20)).Decode(&payload); err != nil || payload.SessionID == "" || payload.Cwd == "" {
		return ""
	}
	patched := patchPaths(payload.ToolInput.Command)
	if len(patched) == 0 {
		return ""
	}
	root, _, err := repoctx.ResolveFrom(payload.Cwd, "")
	if err != nil {
		return ""
	}
	cards, err := matchingCards(root, payload.Cwd, patched)
	if err != nil || len(cards) == 0 {
		return ""
	}

	statePath := config.CodexCardsSentPath(home)
	state := readCardsSent(statePath)
	session := state.Sessions[payload.SessionID]
	var fresh []rules.RuleRecord
	for _, card := range cards {
		if !slices.Contains(session.Cards, card.ID) {
			fresh = append(fresh, card)
			session.Cards = append(session.Cards, card.ID)
		}
	}
	if len(fresh) == 0 {
		return ""
	}
	session.LastSeen = now
	for id, s := range state.Sessions {
		if now.Sub(s.LastSeen) > sentRetention {
			delete(state.Sessions, id)
		}
	}
	state.Sessions[payload.SessionID] = session
	if err := writeCardsSent(statePath, state); err != nil {
		return ""
	}

	bodies := make([]string, len(fresh))
	for i, card := range fresh {
		bodies[i] = strings.TrimSpace(string(card.Body))
	}
	out, err := json.Marshal(map[string]any{"hookSpecificOutput": map[string]any{
		"hookEventName":     "PreToolUse",
		"additionalContext": strings.Join(bodies, "\n\n"),
	}})
	if err != nil {
		return ""
	}
	return string(out)
}

func patchPaths(patch string) []string {
	var paths []string
	scanner := bufio.NewScanner(strings.NewReader(patch))
	scanner.Buffer(nil, 1<<20)
	for scanner.Scan() {
		for _, prefix := range []string{"*** Add File: ", "*** Update File: ", "*** Move to: "} {
			if path, ok := strings.CutPrefix(scanner.Text(), prefix); ok {
				paths = append(paths, strings.TrimSpace(path))
			}
		}
	}
	return paths
}

// matchingCards returns the repository's wiki cards whose scope covers any of
// patched, resolved against cwd as Codex resolves them, in card order.
func matchingCards(root, cwd string, patched []string) ([]rules.RuleRecord, error) {
	stateDir, err := config.RepositoryStateDir(root)
	if err != nil {
		return nil, err
	}
	key := config.ProjectKey(root)
	records, err := rules.LoadWiki(filepath.Join(stateDir, "rules"), key)
	if err != nil || len(records) == 0 {
		return nil, err
	}
	instances, err := rules.BindAll(records, key, root)
	if err != nil {
		return nil, err
	}
	matcher, err := rules.NewMatcher(instances)
	if err != nil {
		return nil, err
	}
	// Git reports a real path while Codex may hand over a symlinked cwd.
	if real, err := filepath.EvalSymlinks(cwd); err == nil {
		cwd = real
	}
	if real, err := filepath.EvalSymlinks(root); err == nil {
		root = real
	}
	seen := map[string]bool{}
	var matched []rules.RuleRecord
	for _, path := range patched {
		if filepath.IsAbs(path) {
			path = realPath(path)
		} else {
			path = filepath.Join(cwd, path)
		}
		rel, err := filepath.Rel(root, path)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		matches, err := matcher.Match(filepath.ToSlash(rel))
		if err != nil {
			return nil, err
		}
		for _, m := range matches {
			if !seen[m.Record.ID] {
				seen[m.Record.ID] = true
				matched = append(matched, m.Record)
			}
		}
	}
	slices.SortFunc(matched, func(a, b rules.RuleRecord) int { return strings.Compare(a.ID, b.ID) })
	return matched, nil
}

// realPath resolves symlinks in the deepest existing ancestor of path, which an
// added file's own path does not yet have.
func realPath(path string) string {
	var tail []string
	for dir := path; ; dir = filepath.Dir(dir) {
		if real, err := filepath.EvalSymlinks(dir); err == nil {
			return filepath.Join(append([]string{real}, tail...)...)
		}
		if filepath.Dir(dir) == dir {
			return path
		}
		tail = append([]string{filepath.Base(dir)}, tail...)
	}
}

// readCardsSent treats a missing or corrupt file as empty: the worst outcome is
// a card sent twice.
func readCardsSent(path string) cardsSent {
	var state cardsSent
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &state)
	}
	if state.Sessions == nil {
		state.Sessions = map[string]sessionCards{}
	}
	return state
}

func writeCardsSent(path string, state cardsSent) error {
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".codex-cards-sent-*.tmp")
	if err != nil {
		return err
	}
	_, werr := tmp.Write(data)
	cerr := tmp.Close()
	if err := firstErr(werr, cerr); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return nil
}

func firstErr(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}
