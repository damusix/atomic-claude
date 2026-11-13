package retro

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/harness"
)

// UnsupportedError names a harness whose session history has no reader yet.
type UnsupportedError struct {
	Kind harness.Kind
}

func (e *UnsupportedError) Error() string {
	return fmt.Sprintf("unsupported: no reader for %s session history", e.Kind)
}

// HistoryDir is the detected harness's session history directory, else the
// enrolled harness's when no Claude target is enrolled. Only Claude has one.
func HistoryDir(home string, d harness.Detection) (string, error) {
	if d.Confidence == harness.ConfidenceAmbiguous {
		names := make([]string, len(d.Candidates))
		for i, k := range d.Candidates {
			names[i] = string(k)
		}
		return "", fmt.Errorf("harness detection is ambiguous (%s); cannot choose a session history", strings.Join(names, ", "))
	}
	kind := d.Kind
	if kind == "" {
		enrolled, err := harness.EnrolledKinds(home)
		if err == nil && len(enrolled) > 0 && !slices.Contains(enrolled, harness.KindClaude) {
			kind = enrolled[0]
		}
	}
	switch kind {
	case harness.KindOMP, harness.KindCodex:
		return "", &UnsupportedError{Kind: kind}
	}
	return filepath.Join(config.ClaudeConfigDir(home), "projects"), nil
}
