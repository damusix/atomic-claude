package doctor

import (
	"os"
	"path/filepath"
)

// ClaudeHomeMissing is the pre-flight gate: no <home>/.claude means no
// category check can say anything useful.
func ClaudeHomeMissing(home string) bool {
	_, err := os.Stat(filepath.Join(home, ".claude"))
	return os.IsNotExist(err)
}

const missingHomeMessage = "atomic-claude not installed; run `atomic claude install`."

// MissingHomeMessage returns the short-circuit message.
func MissingHomeMessage() string {
	return missingHomeMessage
}
