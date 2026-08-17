package wiki

// Wrappers letting other packages reuse the stamper's exact hashing, so a
// reader's drift check can never disagree with what the stamper wrote.
// Reimplementing either of these elsewhere defeats the point.

// FileSHA256 is the hash the bucket manifest engine uses.
func FileSHA256(path string) (string, error) {
	return sha256File(path)
}

// ResolveFingerprint must be given the root matching the id: wikiDir for a
// knowledge/ id, the realm root for a repo id.
func ResolveFingerprint(wikiRoot, id string) (string, bool) {
	return resolveFingerprint(wikiRoot, id)
}
