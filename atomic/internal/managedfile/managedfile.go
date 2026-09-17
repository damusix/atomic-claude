// Package managedfile owns the write primitives every Atomic target shares:
// line-anchored block ownership, current-byte observations with digests,
// durable backups with retention metadata, atomic file publication, and
// journaled generated-directory publication.
package managedfile

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// BlockOpen and BlockClose bound Atomic-owned content inside a user file.
// Detection is line-anchored — only a line whose trimmed content is exactly
// the tag counts — so inline mentions never match. Everything outside the
// block stays user-owned and byte-preserved.
const (
	BlockOpen  = "<atomic>"
	BlockClose = "</atomic>"
)

// BlockBoundaries returns the [start, end) byte offsets of content's single
// managed block, tags included. end covers the close-tag line's trailing
// newline when present. A missing, unclosed, repeated, or out-of-order tag
// shape is rejected rather than guessed: an ambiguous boundary is never
// treated as a boundary, so callers fall back to unowned handling.
func BlockBoundaries(content []byte) (start, end int, err error) {
	start, end = -1, -1
	offset := 0
	for _, line := range bytes.SplitAfter(content, []byte("\n")) {
		switch string(bytes.TrimSpace(line)) {
		case BlockOpen:
			if start != -1 || end != -1 {
				return 0, 0, fmt.Errorf("managedfile: second %s block", BlockOpen)
			}
			start = offset
		case BlockClose:
			if start == -1 || end != -1 {
				return 0, 0, fmt.Errorf("managedfile: %s without a preceding %s", BlockClose, BlockOpen)
			}
			end = offset + len(line)
		}
		offset += len(line)
	}
	if start == -1 {
		return 0, 0, fmt.Errorf("managedfile: no %s block", BlockOpen)
	}
	if end == -1 {
		return 0, 0, fmt.Errorf("managedfile: unclosed %s block", BlockOpen)
	}
	return start, end, nil
}

// HasBlock reports whether content carries exactly one parseable block.
func HasBlock(content []byte) bool {
	_, _, err := BlockBoundaries(content)
	return err == nil
}

// ManagedBlock returns just the block bytes, tags included.
func ManagedBlock(content []byte) ([]byte, error) {
	start, end, err := BlockBoundaries(content)
	if err != nil {
		return nil, err
	}
	return content[start:end], nil
}

// ReplaceBlock swaps content's single managed block for replacement,
// preserving every byte outside it. An unparseable block on either side is an
// error, never a best-effort splice.
func ReplaceBlock(content, replacement []byte) ([]byte, error) {
	start, end, err := BlockBoundaries(content)
	if err != nil {
		return nil, err
	}
	if !HasBlock(replacement) {
		return nil, fmt.Errorf("managedfile: replacement carries no single %s block", BlockOpen)
	}
	out := make([]byte, 0, len(content)-(end-start)+len(replacement))
	out = append(out, content[:start]...)
	out = append(out, replacement...)
	out = append(out, content[end:]...)
	return out, nil
}

// BlocksEqual reports whether both contents carry a parseable block and the
// two blocks are byte-identical. Unowned or malformed content is never equal.
func BlocksEqual(a, b []byte) bool {
	blockA, errA := ManagedBlock(a)
	if errA != nil {
		return false
	}
	blockB, errB := ManagedBlock(b)
	if errB != nil {
		return false
	}
	return bytes.Equal(blockA, blockB)
}

// Digest is the hex-encoded SHA256 of data — the one checksum form every
// observation, backup, journal, and sidecar records.
func Digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
