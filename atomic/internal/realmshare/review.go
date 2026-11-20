package realmshare

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
)

func approvalDigest(verb string, snapshot any) (string, error) {
	data, err := json.Marshal(struct {
		Version  int
		Verb     string
		Snapshot any
	}{Version: 1, Verb: verb, Snapshot: snapshot})
	if err != nil {
		return "", fmt.Errorf("encode %s review: %w", verb, err)
	}
	return fmt.Sprintf("sha256:%x", sha256.Sum256(data)), nil
}
