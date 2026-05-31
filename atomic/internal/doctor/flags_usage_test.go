package doctor_test

import (
	"bytes"
	"errors"
	"flag"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/doctor"
)

// TestParseFlagsHelpReturnsErrHelp verifies that -h returns flag.ErrHelp.
func TestParseFlagsHelpReturnsErrHelp(t *testing.T) {
	_, err := doctor.ParseFlags([]string{"-h"})
	if !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("ParseFlags([\"-h\"]) error = %v, want flag.ErrHelp", err)
	}
}

// TestParseFlagsUsageContainsDoubleDash verifies the custom usage output uses
// double-dash forms (--verbose, --fix) and not bare single-dash tokens.
func TestParseFlagsUsageContainsDoubleDash(t *testing.T) {
	var buf bytes.Buffer
	_, _ = doctor.ParseFlagsWithOutput([]string{"-h"}, &buf)
	usage := buf.String()

	if !strings.Contains(usage, "--verbose") {
		t.Errorf("usage output must contain '--verbose':\n%s", usage)
	}
	if !strings.Contains(usage, "--fix") {
		t.Errorf("usage output must contain '--fix':\n%s", usage)
	}
	if !strings.Contains(usage, "--json") {
		t.Errorf("usage output must contain '--json':\n%s", usage)
	}
	if !strings.Contains(usage, "--only") {
		t.Errorf("usage output must contain '--only':\n%s", usage)
	}
	if !strings.Contains(usage, "--skip") {
		t.Errorf("usage output must contain '--skip':\n%s", usage)
	}
	if !strings.Contains(usage, "--stale-days") {
		t.Errorf("usage output must contain '--stale-days':\n%s", usage)
	}

	// Must NOT contain bare single-dash forms of the flags.
	// We check for " -verbose" and " -fix" (space-prefixed to avoid matching "--verbose").
	if strings.Contains(usage, " -verbose") && !strings.Contains(usage, " --verbose") {
		t.Errorf("usage output must not use bare single-dash '-verbose':\n%s", usage)
	}
	if strings.Contains(usage, " -fix") && !strings.Contains(usage, " --fix") {
		t.Errorf("usage output must not use bare single-dash '-fix':\n%s", usage)
	}
}
