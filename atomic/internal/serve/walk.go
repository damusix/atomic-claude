// The directory-skip predicate every walker in this package shares.
package serve

// shouldSkipDir skips dot-directories and the usual build dumps, with one
// exception: .claude itself holds servable project docs that wiki links cite
// across members, so skipping it would render valid links broken. Dotdirs
// nested inside it are still skipped by the leading-dot rule.
//
// Callers apply this only to sub-directories, never to the root.
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

// hiddenFile marks the backups, caches, and machinery no walker enumerates.
// Only files reach this; .claude is a directory, handled by shouldSkipDir.
func hiddenFile(name string) bool {
	return len(name) > 0 && name[0] == '.'
}
