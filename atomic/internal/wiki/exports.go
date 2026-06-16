package wiki

// exports.go — thin exported wrappers over unexported helpers, provided so
// external packages (e.g. internal/serve) can reuse the EXACT same hashing
// logic that the stamper uses, guaranteeing comparison consistency.
//
// Rule: serve must never reimplement hashing — it must call these wrappers
// so a divergence between the stamper and the reader is impossible.

// FileSHA256 returns the lowercase hex SHA-256 of the file at path.
// This is the same algorithm used by BucketDiff and the bucket manifest
// engine — exported so consumers can hash individual files consistently.
func FileSHA256(path string) (string, error) {
	return sha256File(path)
}

// ResolveFingerprint computes the fingerprint for the cited id under wikiRoot,
// using the same algorithm as StampConcern:
//
//   - id = "knowledge/<topic>.md" → SHA-256 of <wikiRoot>/<id> file content.
//   - <wikiRoot>/<id>/.claude/project/signals.md exists → SHA-256 of its content.
//   - otherwise → git rev-parse HEAD of <wikiRoot>/<id>.
//
// Returns (fingerprint, true) on success, ("", false) when the source is
// unavailable. This is deliberately identical to the internal resolveFingerprint
// so that serve's drift check always agrees with the stamper.
//
// Callers must pass the correct root for the id type:
//   - knowledge/ ids → pass wikiDir (wiki/ directory)
//   - repo ids → pass realm root
//
// See stale.go for the same dispatch pattern used by `atomic wiki stale`.
func ResolveFingerprint(wikiRoot, id string) (string, bool) {
	return resolveFingerprint(wikiRoot, id)
}
