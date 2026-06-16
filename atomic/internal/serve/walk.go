// walk.go — shared directory-skip predicate for all file walkers in the serve package.
package serve

// shouldSkipDir reports whether a directory encountered during a WalkDir
// traversal should be skipped entirely.
//
// Rules:
//   - .claude is NOT skipped: it holds servable project docs (project/signals.md
//     and other markdown) that `atomic wiki linkify` cites across realm members.
//     The page handler serves those files via safeResolve, so the link graph,
//     nav, search, and external walkers must see them too — otherwise valid links
//     into .claude render as broken and their rail 404s. Nested dotdirs inside
//     .claude (.scratchpad, .atomic-index) remain skipped by the leading-dot rule.
//   - Any other dir whose base name starts with '.' (.git, .worktrees, .obsidian,
//     .scratchpad, .atomic-index, …) → skip.
//   - The literal names node_modules, vendor, tmp → skip.
//
// The root itself is never skipped by callers; this function is applied only to
// sub-directories discovered during the walk (d.IsDir() && path != root).
func shouldSkipDir(name string) bool {
	if name == ".claude" {
		return false
	}
	if len(name) > 0 && name[0] == '.' {
		return true
	}
	switch name {
	case "node_modules", "vendor", "tmp":
		return true
	}
	return false
}

// hiddenFile reports whether a file's base name marks it as hidden (leading dot,
// e.g. .DS_Store or .deterministic-signals.prev.md). Hidden files are never
// enumerated by the walkers (graph nodes, nav, search, external registry): they
// are backups, caches, and machinery, not navigable content. Note that .claude
// itself is a directory and is intentionally NOT hidden here (see shouldSkipDir).
func hiddenFile(name string) bool {
	return len(name) > 0 && name[0] == '.'
}
