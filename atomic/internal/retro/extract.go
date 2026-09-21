// Package retro extracts user-typed text and Skill/Agent tool calls from
// Claude Code session transcripts into a compact, line-numbered form that
// /retrospective-learning can scan without reading raw .jsonl traffic.
package retro

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Kind identifies what an Entry renders as.
type Kind int

const (
	KindUser Kind = iota
	KindCommand
	KindSkill
	KindAgent
)

// Entry is one kept row, reduced to what the retrospective needs to cite.
type Entry struct {
	Kind      Kind
	Timestamp time.Time
	Text      string
}

// Session is one <uuid>.jsonl file with at least one kept entry.
type Session struct {
	ID         string
	ProjectDir string
	First      time.Time
	Last       time.Time
	Entries    []Entry
	// Size is the summed byte length of rendered entry text, used to
	// balance shards; it is not the file size on disk.
	Size int64
}

// Options configures Extract. ProjectsDir is explicit (never defaulted here)
// so tests never touch the real ~/.claude/projects.
type Options struct {
	ProjectsDir string
	Since       time.Time
	ProjectGlob string
}

// Stats summarizes one Extract run for the CLI's stderr report.
type Stats struct {
	ProjectsScanned int
	FilesScanned    int
	TotalBytes      int64
	SessionsKept    int
	UnparsableRows  int
	ReadErrors      int
}

// Result is Extract's return value.
type Result struct {
	Sessions []Session
	Stats    Stats
}

// Extract walks ProjectsDir's direct child directories, reads the *.jsonl
// files that are direct children of each (skipping subagents/ transcripts one
// level deeper without a name check), and returns sessions with at least one
// kept entry, ordered by first timestamp.
func Extract(opts Options) (Result, error) {
	var result Result

	projectDirs, err := os.ReadDir(opts.ProjectsDir)
	if err != nil {
		return result, err
	}

	for _, pd := range projectDirs {
		if !pd.IsDir() {
			continue
		}
		slug := pd.Name()
		if opts.ProjectGlob != "" {
			matched, err := filepath.Match(opts.ProjectGlob, slug)
			if err != nil {
				return result, err
			}
			if !matched {
				continue
			}
		}
		result.Stats.ProjectsScanned++

		projectPath := filepath.Join(opts.ProjectsDir, slug)
		files, err := os.ReadDir(projectPath)
		if err != nil {
			result.Stats.ReadErrors++
			continue
		}
		for _, f := range files {
			if f.IsDir() || filepath.Ext(f.Name()) != ".jsonl" {
				continue
			}
			info, err := f.Info()
			if err != nil {
				result.Stats.ReadErrors++
				continue
			}
			if info.ModTime().Before(opts.Since) {
				continue
			}
			result.Stats.FilesScanned++
			result.Stats.TotalBytes += info.Size()

			id := strings.TrimSuffix(f.Name(), ".jsonl")
			sess, unparsable, err := parseSession(filepath.Join(projectPath, f.Name()), slug, id)
			result.Stats.UnparsableRows += unparsable
			if err != nil || len(sess.Entries) == 0 {
				continue
			}
			result.Stats.SessionsKept++
			result.Sessions = append(result.Sessions, sess)
		}
	}

	sort.Slice(result.Sessions, func(i, j int) bool {
		return result.Sessions[i].First.Before(result.Sessions[j].First)
	})

	return result, nil
}

// rawRow is the subset of a transcript row the filter reads.
type rawRow struct {
	Type             string `json:"type"`
	IsSidechain      bool   `json:"isSidechain"`
	IsMeta           bool   `json:"isMeta"`
	IsCompactSummary bool   `json:"isCompactSummary"`
	Timestamp        string `json:"timestamp"`
	CWD              string `json:"cwd"`
	Message          struct {
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

type rawBlock struct {
	Type  string          `json:"type"`
	Text  string          `json:"text"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

// parseSession streams one file's rows through the row rules, tracking the
// session's first/last timestamp over every user/assistant row regardless of
// whether that row produced an entry.
func parseSession(path, slug, id string) (Session, int, error) {
	sess := Session{ID: id, ProjectDir: slug}

	f, err := os.Open(path)
	if err != nil {
		return sess, 0, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	// A tool_result row can carry a large pasted output past bufio.Scanner's
	// 64 KiB default token limit; grow the buffer so those rows still parse.
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)

	unparsable := 0
	haveSpan := false
	haveCWD := false

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}

		var row rawRow
		if err := json.Unmarshal(line, &row); err != nil {
			unparsable++
			continue
		}

		if !haveCWD && row.CWD != "" {
			sess.ProjectDir = row.CWD
			haveCWD = true
		}

		ts, tsOK := parseTimestamp(row.Timestamp)
		if (row.Type == "user" || row.Type == "assistant") && tsOK {
			if !haveSpan || ts.Before(sess.First) {
				sess.First = ts
			}
			if !haveSpan || ts.After(sess.Last) {
				sess.Last = ts
			}
			haveSpan = true
		}

		if row.IsSidechain {
			continue
		}

		var entries []Entry
		switch row.Type {
		case "user":
			entries = processUserRow(&row)
		case "assistant":
			entries = processAssistantRow(&row)
		}

		if len(entries) > 0 {
			if !tsOK {
				unparsable++
			} else {
				for i := range entries {
					entries[i].Timestamp = ts
					sess.Entries = append(sess.Entries, entries[i])
					sess.Size += int64(len(entries[i].Text))
				}
			}
		}
	}

	if scanner.Err() != nil {
		unparsable++
	}

	return sess, unparsable, nil
}

func parseTimestamp(s string) (time.Time, bool) {
	if s == "" {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

var (
	commandNameRe    = regexp.MustCompile(`<command-name>(.*?)</command-name>`)
	commandMessageRe = regexp.MustCompile(`<command-message>(.*?)</command-message>`)
	commandArgsRe    = regexp.MustCompile(`<command-args>(.*?)</command-args>`)
	systemReminderRe = regexp.MustCompile(`(?s)<system-reminder>.*?</system-reminder>`)
)

var machineTagPrefixes = []string{
	"<local-command-stdout>",
	"<local-command-caveat>",
	"<task-notification>",
	"<bash-input>",
	"<bash-stdout>",
	"<bash-stderr>",
	"<agent-message",
	"<cross-session-message",
}

func processUserRow(row *rawRow) []Entry {
	if row.IsMeta || row.IsCompactSummary {
		return nil
	}

	str, blocks, ok := decodeContent(row.Message.Content)
	if !ok {
		return nil
	}
	if blocks != nil {
		for _, b := range blocks {
			if b.Type == "tool_result" {
				return nil
			}
		}
		str = joinTextBlocks(blocks)
	}

	if name, args, isCmd := parseCommand(str); isCmd {
		line := "/" + strings.TrimPrefix(name, "/")
		if args != "" {
			line += " " + args
		}
		return []Entry{{Kind: KindCommand, Text: line}}
	}

	text := strings.TrimSpace(systemReminderRe.ReplaceAllString(str, ""))
	if text == "" {
		return nil
	}
	for _, tag := range machineTagPrefixes {
		if strings.HasPrefix(text, tag) {
			return nil
		}
	}

	return []Entry{{Kind: KindUser, Text: text}}
}

func processAssistantRow(row *rawRow) []Entry {
	_, blocks, ok := decodeContent(row.Message.Content)
	if !ok || blocks == nil {
		return nil
	}

	var entries []Entry
	for _, b := range blocks {
		if b.Type != "tool_use" {
			continue
		}
		switch b.Name {
		case "Skill":
			var in struct {
				Skill string `json:"skill"`
				Args  string `json:"args"`
			}
			if err := json.Unmarshal(b.Input, &in); err != nil {
				continue
			}
			line := in.Skill
			if in.Args != "" {
				line += " " + in.Args
			}
			entries = append(entries, Entry{Kind: KindSkill, Text: line})
		case "Agent":
			var in struct {
				SubagentType string `json:"subagent_type"`
				Description  string `json:"description"`
			}
			if err := json.Unmarshal(b.Input, &in); err != nil {
				continue
			}
			entries = append(entries, Entry{Kind: KindAgent, Text: in.SubagentType + " — " + in.Description})
		}
	}
	return entries
}

// decodeContent reads message.content as either a JSON string or an array of
// blocks; ok is false only when content is absent or neither shape decodes.
func decodeContent(raw json.RawMessage) (str string, blocks []rawBlock, ok bool) {
	if len(raw) == 0 {
		return "", nil, false
	}
	if err := json.Unmarshal(raw, &str); err == nil {
		return str, nil, true
	}
	if err := json.Unmarshal(raw, &blocks); err == nil {
		return "", blocks, true
	}
	return "", nil, false
}

func joinTextBlocks(blocks []rawBlock) string {
	var parts []string
	for _, b := range blocks {
		if b.Type == "text" {
			parts = append(parts, b.Text)
		}
	}
	return strings.Join(parts, "\n")
}

// parseCommand extracts a slash-command line from a row's raw text. Built-ins
// carry <command-name>; custom commands and skills carry <command-message>.
func parseCommand(s string) (name, args string, ok bool) {
	if m := commandNameRe.FindStringSubmatch(s); m != nil {
		name = m[1]
		ok = true
	} else if m := commandMessageRe.FindStringSubmatch(s); m != nil {
		name = m[1]
		ok = true
	}
	if !ok {
		return "", "", false
	}
	if m := commandArgsRe.FindStringSubmatch(s); m != nil {
		args = strings.TrimSpace(m[1])
	}
	return strings.TrimSpace(name), args, true
}
