package realm

// The <code-index> block sits in the realm file the steering mode makes Claude
// load, so a session in any member repo sees the realm through the upward walk.
//
// The block carries no timestamp: one would diff on every index run, defeating
// the regen-only-on-change rule.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/bundlespec"
	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/wiki"
)

// guidanceFiles returns the file that carries the block in the steering mode and
// the other one; under agents-md a CLAUDE.md would hide AGENTS.md.
func guidanceFiles(realmRoot string) (target, other string, err error) {
	mode, err := config.ResolveSteeringMode(config.ProjectHome(), realmRoot)
	if err != nil {
		return "", "", err
	}
	steering := bundlespec.ScopeSteering
	target, other = steering.ClaudeTarget, steering.Source
	if mode == bundlespec.SteeringAgentsMD {
		target, other = other, target
	}
	return filepath.Join(realmRoot, target), filepath.Join(realmRoot, other), nil
}

// WriteCodeIndexBlock rewrites the block in the mode's realm file, creating it.
// Idempotent. members must arrive already filtered to the non-excluded set.
func WriteCodeIndexBlock(realmRoot string, members []MemberEntry) error {
	guidancePath, _, err := guidanceFiles(realmRoot)
	if err != nil {
		return err
	}
	block := buildCodeIndexBlock(members)

	existing, err := os.ReadFile(guidancePath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("realm: read %s: %w", guidancePath, err)
	}

	var newContent string
	if os.IsNotExist(err) || len(existing) == 0 {
		// Add a stub alongside the block so a fresh file is usable as-is.
		newContent = block + "\n" + wiki.CodeIndexStub
	} else {
		newContent = rewriteCodeIndexBlock(string(existing), block)
	}

	// Skip the write when nothing changed, so mtime stays stable.
	if string(existing) == newContent {
		return nil
	}

	return os.WriteFile(guidancePath, []byte(newContent), 0o644)
}

// MoveCodeIndexBlock moves the block and its stub to the mode's realm file after
// a mode change, removing the file it empties. It returns the paths it changed.
func MoveCodeIndexBlock(realmRoot string) (changed []string, err error) {
	target, other, err := guidanceFiles(realmRoot)
	if err != nil {
		return nil, err
	}
	otherData, err := os.ReadFile(other)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("realm: read %s: %w", other, err)
	}
	content := string(otherData)
	start, end, ok := wiki.FindCodeIndexBlock(content)
	if !ok {
		return nil, nil
	}
	block := content[start:end]
	rest := wiki.StripCodeIndex(content)

	existing, err := os.ReadFile(target)
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("realm: read %s: %w", target, err)
	}
	merged := block + "\n" + wiki.CodeIndexStub
	if len(existing) > 0 {
		merged = rewriteCodeIndexBlock(string(existing), block)
	}
	if string(existing) != merged {
		if err := os.WriteFile(target, []byte(merged), 0o644); err != nil {
			return nil, err
		}
		changed = append(changed, target)
	}

	if strings.TrimSpace(rest) == "" {
		err = os.Remove(other)
	} else {
		err = os.WriteFile(other, []byte(rest), 0o644)
	}
	if err != nil {
		return changed, err
	}
	return append(changed, other), nil
}

// buildCodeIndexBlock must stay byte-identical across runs with the same
// membership, so nothing time- or order-varying may enter it.
func buildCodeIndexBlock(members []MemberEntry) string {
	var sb strings.Builder
	sb.WriteString(wiki.CodeIndexOpen)
	sb.WriteString("\n")
	for _, m := range members {
		fmt.Fprintf(&sb, "<member key=%q path=%q />\n", m.Key, m.Path)
	}
	sb.WriteString(wiki.CodeIndexClose)
	return sb.String()
}

// rewriteCodeIndexBlock preserves everything outside the block byte-for-byte,
// appending when no block exists yet.
func rewriteCodeIndexBlock(content, newBlock string) string {
	start, end, ok := wiki.FindCodeIndexBlock(content)
	if !ok {
		result := content
		if !strings.HasSuffix(result, "\n") {
			result += "\n"
		}
		return result + "\n" + newBlock
	}
	return content[:start] + newBlock + content[end:]
}
