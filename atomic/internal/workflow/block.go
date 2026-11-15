// Package workflow resolves source-control, issue-tracker, and worktree
// preferences from the profile, realm, and repository levels.
package workflow

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// Key names one preference line.
type Key string

const (
	KeySourceControl       Key = "source-control"
	KeySourceControlHost   Key = "source-control-host"
	KeyIssueTracker        Key = "issue-tracker"
	KeyIssueTrackerProject Key = "issue-tracker-project"
	KeyWorktrees           Key = "worktrees"
)

// Keys lists every key in rendering order.
var Keys = []Key{
	KeySourceControl,
	KeySourceControlHost,
	KeyIssueTracker,
	KeyIssueTrackerProject,
	KeyWorktrees,
}

var (
	sourceControls = set("github", "gitlab", "bitbucket", "codeberg", "none")
	hostTrackers   = set("github", "gitlab", "codeberg")
	issueTrackers  = set("github", "gitlab", "codeberg", "linear", "jira",
		"trello", "filesystem")

	dnsLabel = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?$`)
	openTag  = regexp.MustCompile(
		`^<workflow-preferences(?:\s+level=(?:'([^']*)'|"([^"]*)"|([^\s'">]*)))?\s*>\s*$`)
)

const closeTag = "</workflow-preferences>"

func set(vals ...string) map[string]bool {
	m := make(map[string]bool, len(vals))
	for _, v := range vals {
		m[v] = true
	}
	return m
}

// Entry is one set line; Soft marks a `# detected` value.
type Entry struct {
	Value string
	Soft  bool
}

// Block is one parsed preference block. Lines maps each known key to its first
// line, whether or not its value was valid.
type Block struct {
	Level    string
	Values   map[Key]Entry
	Warnings []string
	Close    int
	Lines    map[Key]Line
}

// Line locates one key's first line in the parsed content, zero-based.
type Line struct {
	N    int
	Soft bool
}

// ParseBlock parses the one block outside fenced code: (nil, nil) when there
// is none, an error for two blocks or an unpaired tag.
func ParseBlock(content string) (*Block, error) {
	var b *Block
	var open bool
	var fence string
	seen := map[string]bool{}

	for i, line := range strings.Split(content, "\n") {
		line = strings.TrimRight(line, "\r")
		var delimiter bool
		if fence, delimiter = fenceStep(fence, line); delimiter || fence != "" {
			continue
		}
		if m := openTag.FindStringSubmatch(line); m != nil {
			if b != nil {
				return nil, errors.New("more than one preference block")
			}
			b = &Block{Level: m[1] + m[2] + m[3], Values: map[Key]Entry{},
				Lines: map[Key]Line{}}
			open = true
			continue
		}
		if strings.TrimRight(line, " \t") == closeTag {
			if !open {
				return nil, errors.New("closing tag without an opening tag")
			}
			open = false
			b.Close = i
			continue
		}
		if open {
			b.parseLine(i, line, seen)
		}
	}
	if open {
		return nil, errors.New("opening tag without a closing tag")
	}
	return b, nil
}

// fenceStep returns the open fence marker after line and whether line is a
// fence delimiter. Only the opening marker closes a fence.
func fenceStep(fence, line string) (string, bool) {
	t := strings.TrimLeft(line, " ")
	for _, marker := range []string{"```", "~~~"} {
		if !strings.HasPrefix(t, marker) {
			continue
		}
		switch fence {
		case "":
			return marker, true
		case marker:
			return "", true
		}
	}
	return fence, false
}

func (b *Block) parseLine(n int, line string, seen map[string]bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return
	}
	body, ok := strings.CutPrefix(trimmed, "- ")
	name, rest, hasColon := strings.Cut(body, ":")
	if !ok || !hasColon {
		b.warn("malformed line ignored: %q", trimmed)
		return
	}
	name = strings.TrimSpace(name)
	value, note, _ := strings.Cut(rest, "#")
	value, note = strings.TrimSpace(value), strings.TrimSpace(note)
	soft := note == "detected" || strings.HasPrefix(note, "detected:")

	key := Key(name)
	if !knownKey(key) {
		b.warn("unknown key %q ignored", name)
		return
	}
	if seen[name] {
		b.warn("%s repeated; the first line wins", name)
		return
	}
	seen[name] = true
	b.Lines[key] = Line{N: n, Soft: soft}
	if value == "" {
		return
	}
	if err := validate(key, value); err != nil {
		b.warn("%s: %v; unset at this level", name, err)
		return
	}
	if soft && key == KeyIssueTracker && hostTrackers[value] {
		b.warn("%s: detected host tracker %q is invalid; ignored", name, value)
		return
	}
	b.Values[key] = Entry{Value: value, Soft: soft}
}

// SetLine replaces the key's line or inserts it before the close tag, keeping
// the neighbor's CRLF, and updates b.Lines and b.Close so later calls see it.
func (b *Block) SetLine(lines []string, key Key, text string) []string {
	if l, ok := b.Lines[key]; ok {
		lines[l.N] = text + lineEnding(lines[l.N])
		return lines
	}
	at := b.Close
	lines = slices.Insert(lines, at, text+lineEnding(lines[at]))
	b.Lines[key] = Line{N: at}
	b.Close++
	return lines
}

func lineEnding(line string) string {
	if strings.HasSuffix(line, "\r") {
		return "\r"
	}
	return ""
}

func (b *Block) warn(format string, args ...any) {
	b.Warnings = append(b.Warnings, fmt.Sprintf(format, args...))
}

func knownKey(k Key) bool {
	for _, known := range Keys {
		if k == known {
			return true
		}
	}
	return false
}

func validate(key Key, value string) error {
	bad := func() error { return fmt.Errorf("invalid value %q", value) }
	switch key {
	case KeySourceControl:
		if !sourceControls[value] {
			return bad()
		}
	case KeyIssueTracker:
		if !issueTrackers[value] {
			return bad()
		}
	case KeySourceControlHost:
		if !validHostname(value) {
			return bad()
		}
	case KeyIssueTrackerProject:
		if strings.ContainsAny(value, " \t") {
			return bad()
		}
	case KeyWorktrees:
		if value == "auto" || value == "off" {
			return nil
		}
		if filepath.IsAbs(value) {
			return bad()
		}
		for _, seg := range strings.Split(value, "/") {
			if seg == ".." {
				return bad()
			}
		}
	}
	return nil
}

func validHostname(value string) bool {
	if len(value) > 253 {
		return false
	}
	for _, label := range strings.Split(value, ".") {
		if !dnsLabel.MatchString(label) {
			return false
		}
	}
	return true
}

func isProfileHeading(line string) bool {
	return strings.TrimRight(line, " \t\r\n") == "## Workflow preferences"
}

// ProfileSectionStart returns the byte offset just past the first
// `## Workflow preferences` heading line outside fenced code, -1 when none.
func ProfileSectionStart(content string) int {
	var fence string
	offset := 0
	for _, line := range strings.SplitAfter(content, "\n") {
		offset += len(line)
		fence, _ = fenceStep(fence, strings.TrimRight(line, "\n"))
		if fence == "" && isProfileHeading(line) {
			return offset
		}
	}
	return -1
}

// ProfileSection returns the text of the `## Workflow preferences` section of
// a profile, or "" when it has none.
func ProfileSection(content string) string {
	var out []string
	var in bool
	var fence string
	for _, line := range strings.Split(content, "\n") {
		fence, _ = fenceStep(fence, line)
		if fence == "" && strings.HasPrefix(line, "## ") {
			in = isProfileHeading(line)
			continue
		}
		if in {
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}
