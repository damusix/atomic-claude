// api_bus_transcript.go — EXPERIMENT (bus chat, api_bus.go): session
// transcript endpoints for the /bus rail. A bus member's Session is a
// Claude Code session id; its transcript lives at
// <home>/.claude/projects/<encoded-cwd>/<session>.jsonl. The rail lists
// each room member's session with transcript availability; clicking one
// renders the .jsonl into markdown server-side (RenderMarkdown, same
// pipeline every page uses) and returns HTML-in-JSON.
package serve

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/bus"
)

// sessionIDPattern is deliberately strict: the id is spliced into a glob
// under <home>/.claude/projects, so anything path-like is rejected before
// it can traverse.
var sessionIDPattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`)

// findSessionTranscript globs every Claude Code project dir for
// <session>.jsonl and returns the newest match — the same session id can
// appear under more than one encoded cwd (e.g. a session that moved into a
// worktree), and the newest file is the one still being written.
func findSessionTranscript(home, session string) (string, os.FileInfo, error) {
	if !sessionIDPattern.MatchString(session) || strings.Contains(session, "..") {
		return "", nil, fmt.Errorf("invalid session id")
	}
	matches, err := filepath.Glob(filepath.Join(home, ".claude", "projects", "*", session+".jsonl"))
	if err != nil {
		return "", nil, err
	}
	var newest string
	var newestInfo os.FileInfo
	for _, m := range matches {
		info, statErr := os.Stat(m)
		if statErr != nil {
			continue
		}
		if newestInfo == nil || info.ModTime().After(newestInfo.ModTime()) {
			newest, newestInfo = m, info
		}
	}
	if newest == "" {
		return "", nil, nil
	}
	return newest, newestInfo, nil
}

type busSessionTranscript struct {
	Found     bool   `json:"found"`
	Path      string `json:"path,omitempty"`
	MtimeUnix int64  `json:"mtime,omitempty"`
	SizeBytes int64  `json:"size,omitempty"`
}

type busSessionInfo struct {
	Name       string               `json:"name"`
	Kind       string               `json:"kind"`
	Session    string               `json:"session"`
	Stale      bool                 `json:"stale"`
	Repo       string               `json:"repo,omitempty"`
	Realm      string               `json:"realm,omitempty"`
	Transcript busSessionTranscript `json:"transcript"`
}

type busSessionsResponse struct {
	Sessions []busSessionInfo `json:"sessions"`
}

func (h *busAPIHandler) handleSessions(w http.ResponseWriter, r *http.Request) {
	room := r.URL.Query().Get("room")
	if !requireRoom(w, room) {
		return
	}
	resp, err := h.do(bus.Request{Op: bus.OpWho, Room: room})
	if err != nil {
		writeBusError(w, err)
		return
	}
	var payload struct {
		Members []bus.Member `json:"members"`
	}
	if err := json.Unmarshal(resp.Payload, &payload); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "parse who payload: "+err.Error())
		return
	}

	out := busSessionsResponse{Sessions: make([]busSessionInfo, 0, len(payload.Members))}
	for _, m := range payload.Members {
		info := busSessionInfo{
			Name: m.Name, Kind: m.Kind, Session: m.Session,
			Stale: m.Stale, Repo: m.Repo, Realm: m.Realm,
		}
		if path, stat, ferr := findSessionTranscript(h.home, m.Session); ferr == nil && path != "" {
			info.Transcript = busSessionTranscript{
				Found: true, Path: path,
				MtimeUnix: stat.ModTime().Unix(), SizeBytes: stat.Size(),
			}
		}
		out.Sessions = append(out.Sessions, info)
	}
	sort.Slice(out.Sessions, func(i, j int) bool { return out.Sessions[i].Name < out.Sessions[j].Name })
	writeAPIJSON(w, out)
}

type busTranscriptResponse struct {
	HTML         string `json:"html"`
	Title        string `json:"title"`
	AgentName    string `json:"agentName,omitempty"`
	Path         string `json:"path"`
	ShownEntries int    `json:"shownEntries"`
	TotalEntries int    `json:"totalEntries"`
	Offset       int    `json:"offset"`
	// FirstEntry/LastEntry are the 1-based absolute positions of the shown
	// window within the whole transcript (0/0 when the window is empty) —
	// the same numbers transcriptToMarkdown renders into its own range
	// note, single-sourced via transcriptMeta so the client never
	// recomputes them from shownEntries/totalEntries/offset.
	FirstEntry int `json:"firstEntry"`
	LastEntry  int `json:"lastEntry"`
}

func (h *busAPIHandler) handleTranscript(w http.ResponseWriter, r *http.Request) {
	session := r.URL.Query().Get("session")
	if session == "" {
		writeAPIError(w, http.StatusBadRequest, "missing session")
		return
	}
	n := 100
	if raw := r.URL.Query().Get("n"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 && parsed <= 500 {
			n = parsed
		}
	}
	// offset counts entries skipped from the tail — offset 0 is the latest
	// window, offset n is the one before it, and so on backward.
	offset := 0
	if raw := r.URL.Query().Get("offset"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 && parsed <= 10000 {
			offset = parsed
		}
	}

	path, _, err := findSessionTranscript(h.home, session)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	if path == "" {
		writeAPIError(w, http.StatusNotFound, "no transcript found for session "+session)
		return
	}

	md, meta, err := transcriptToMarkdown(path, n, offset)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "read transcript: "+err.Error())
		return
	}
	html, _, err := RenderMarkdown([]byte(md))
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "render transcript: "+err.Error())
		return
	}

	title := meta.title
	if title == "" {
		title = session
	}
	writeAPIJSON(w, busTranscriptResponse{
		HTML: html, Title: title, AgentName: meta.agentName, Path: path,
		ShownEntries: meta.shown, TotalEntries: meta.total, Offset: offset,
		FirstEntry: meta.first, LastEntry: meta.last,
	})
}

// --- .jsonl → markdown ---

// transcriptLine is the subset of a Claude Code session .jsonl line the
// renderer reads. Meta line types (ai-title, agent-name) share the struct;
// unused fields stay zero.
type transcriptLine struct {
	Type        string          `json:"type"`
	Timestamp   string          `json:"timestamp"`
	IsSidechain bool            `json:"isSidechain"`
	AITitle     string          `json:"aiTitle"`
	AgentName   string          `json:"agentName"`
	Message     json.RawMessage `json:"message"`
}

type transcriptMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"` // string, or []block
}

type transcriptBlock struct {
	Type     string          `json:"type"`
	Text     string          `json:"text"`
	Thinking string          `json:"thinking"`
	Name     string          `json:"name"`    // tool_use
	Input    json.RawMessage `json:"input"`   // tool_use
	Content  json.RawMessage `json:"content"` // tool_result: string or []block
}

type transcriptEntry struct {
	role      string
	ts        string
	sidechain bool
	blocks    []transcriptBlock
	text      string // string-content form
}

type transcriptMeta struct {
	title     string
	agentName string
	shown     int
	total     int
	// first/last are the 1-based absolute positions of the shown window
	// within the whole transcript; 0/0 when the window is empty.
	first int
	last  int
}

// maxTranscriptLineBytes bounds one .jsonl line; a tool result can carry
// megabytes, and a line beyond this is skipped rather than failing the read.
const maxTranscriptLineBytes = 8 * 1024 * 1024

// transcriptToMarkdown reads a session .jsonl and produces markdown for a
// window of maxEntries user/assistant entries, offset entries back from
// the tail (offset 0 = latest window). Per-block truncation keeps one
// giant tool result from swamping the page; the sliding ring bounds
// memory at offset+maxEntries entries regardless of transcript size.
func transcriptToMarkdown(path string, maxEntries, offset int) (string, transcriptMeta, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", transcriptMeta{}, err
	}
	defer f.Close()

	var meta transcriptMeta
	keep := maxEntries + offset
	entries := make([]transcriptEntry, 0, keep)

	reader := bufio.NewReaderSize(f, 256*1024)
	for {
		line, readErr := readBoundedLine(reader)
		if line != nil {
			if entry, lineMeta, ok := parseTranscriptLine(line); ok {
				meta.total++
				if len(entries) == keep {
					copy(entries, entries[1:])
					entries = entries[:keep-1]
				}
				entries = append(entries, entry)
			} else {
				if lineMeta.title != "" {
					meta.title = lineMeta.title
				}
				if lineMeta.agentName != "" {
					meta.agentName = lineMeta.agentName
				}
			}
		}
		if readErr != nil {
			if readErr == io.EOF {
				break
			}
			return "", transcriptMeta{}, readErr
		}
	}

	end := len(entries) - offset
	if end < 0 {
		end = 0
	}
	start := end - maxEntries
	if start < 0 {
		start = 0
	}
	window := entries[start:end]
	meta.shown = len(window)
	if meta.shown > 0 {
		// 1-based chronological positions of the window within the whole
		// transcript, for the range note below and the API response.
		meta.first = meta.total - len(entries) + start + 1
		meta.last = meta.total - len(entries) + end
	}

	var b strings.Builder
	if meta.total > meta.shown && meta.shown > 0 {
		fmt.Fprintf(&b, "*(entries %d–%d of %d)*\n\n", meta.first, meta.last, meta.total)
	}
	if meta.shown == 0 && meta.total > 0 {
		fmt.Fprintf(&b, "*(no entries this far back — the transcript has %d)*\n", meta.total)
	}
	for i, entry := range window {
		if i > 0 {
			b.WriteString("\n---\n\n")
		}
		writeEntryMarkdown(&b, entry)
	}
	return b.String(), meta, nil
}

// readBoundedLine reads one \n-terminated line, returning nil (skipping)
// for lines beyond maxTranscriptLineBytes instead of erroring out.
func readBoundedLine(r *bufio.Reader) ([]byte, error) {
	var buf []byte
	overflowed := false
	for {
		chunk, err := r.ReadSlice('\n')
		if !overflowed {
			buf = append(buf, chunk...)
		}
		if err == bufio.ErrBufferFull {
			if len(buf) > maxTranscriptLineBytes {
				overflowed = true
				buf = nil
			}
			continue
		}
		if overflowed {
			return nil, err
		}
		return buf, err
	}
}

func parseTranscriptLine(raw []byte) (transcriptEntry, transcriptMeta, bool) {
	var line transcriptLine
	if json.Unmarshal(raw, &line) != nil {
		return transcriptEntry{}, transcriptMeta{}, false
	}
	switch line.Type {
	case "ai-title":
		return transcriptEntry{}, transcriptMeta{title: line.AITitle}, false
	case "agent-name":
		return transcriptEntry{}, transcriptMeta{agentName: line.AgentName}, false
	case "user", "assistant":
	default:
		return transcriptEntry{}, transcriptMeta{}, false
	}

	var msg transcriptMessage
	if json.Unmarshal(line.Message, &msg) != nil {
		return transcriptEntry{}, transcriptMeta{}, false
	}
	entry := transcriptEntry{role: msg.Role, ts: line.Timestamp, sidechain: line.IsSidechain}
	if entry.role == "" {
		entry.role = line.Type
	}

	// content is either one string or a block list.
	var asString string
	if json.Unmarshal(msg.Content, &asString) == nil {
		entry.text = asString
		return entry, transcriptMeta{}, true
	}
	var blocks []transcriptBlock
	if json.Unmarshal(msg.Content, &blocks) != nil {
		return transcriptEntry{}, transcriptMeta{}, false
	}
	entry.blocks = blocks
	return entry, transcriptMeta{}, true
}

func writeEntryMarkdown(b *strings.Builder, entry transcriptEntry) {
	label := entry.role
	if entry.text == "" && len(entry.blocks) > 0 && onlyToolResults(entry.blocks) {
		label = "tool output"
	}
	if entry.sidechain {
		label += " (sidechain)"
	}
	stamp := ""
	if ts, err := time.Parse(time.RFC3339, entry.ts); err == nil {
		stamp = " · " + ts.Local().Format("15:04:05")
	}
	fmt.Fprintf(b, "#### %s%s\n\n", label, stamp)

	if entry.text != "" {
		b.WriteString(truncateBlock(entry.text, 4000))
		b.WriteString("\n")
		return
	}
	for _, block := range entry.blocks {
		switch block.Type {
		case "text":
			b.WriteString(truncateBlock(block.Text, 4000))
			b.WriteString("\n\n")
		case "thinking":
			fmt.Fprintf(b, "*thinking:* %s\n\n", truncateBlock(oneLine(block.Thinking), 280))
		case "tool_use":
			fmt.Fprintf(b, "````tool\n%s %s\n````\n\n", block.Name, truncateBlock(string(block.Input), 400))
		case "tool_result":
			fmt.Fprintf(b, "````result\n%s\n````\n\n", truncateBlock(toolResultText(block.Content), 600))
		}
	}
}

func onlyToolResults(blocks []transcriptBlock) bool {
	sawResult := false
	for _, block := range blocks {
		switch block.Type {
		case "tool_result":
			sawResult = true
		case "text", "thinking", "tool_use":
			return false
		}
	}
	return sawResult
}

// toolResultText flattens a tool_result's content (string, or a list of
// text blocks) into plain text.
func toolResultText(raw json.RawMessage) string {
	var asString string
	if json.Unmarshal(raw, &asString) == nil {
		return asString
	}
	var blocks []transcriptBlock
	if json.Unmarshal(raw, &blocks) == nil {
		var parts []string
		for _, block := range blocks {
			if block.Text != "" {
				parts = append(parts, block.Text)
			}
		}
		return strings.Join(parts, "\n")
	}
	return string(raw)
}

func truncateBlock(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	cut := n
	for cut > 0 && !isUTF8Boundary(s[cut]) {
		cut--
	}
	return fmt.Sprintf("%s… *(+%d chars)*", s[:cut], len(s)-cut)
}

func isUTF8Boundary(b byte) bool { return b&0xC0 != 0x80 }

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
