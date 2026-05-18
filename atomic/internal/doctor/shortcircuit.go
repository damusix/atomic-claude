package doctor

import (
	"os"
	"path/filepath"
)

// ClaudeHomeMissing returns true when <home>/.claude does not exist.
// Used as a pre-flight check before running any category checks.
func ClaudeHomeMissing(home string) bool {
	_, err := os.Stat(filepath.Join(home, ".claude"))
	return os.IsNotExist(err)
}

// missingHomeMessage is the canonical message for the short-circuit case.
const missingHomeMessage = "atomic-claude not installed; run `atomic claude install`."

// MissingHomeMessage returns the canonical short-circuit message string.
func MissingHomeMessage() string {
	return missingHomeMessage
}
