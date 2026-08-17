package doctor_test

import (
	"bytes"
	"errors"
	"flag"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/doctor"
)

func TestParseFlagsHelpReturnsErrHelp(t *testing.T) {
	_, err := doctor.ParseFlags([]string{"-h"})
	if !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("ParseFlags([\"-h\"]) error = %v, want flag.ErrHelp", err)
	}
}

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

	// Space-prefixed so " -verbose" does not also match " --verbose".
	if strings.Contains(usage, " -verbose") && !strings.Contains(usage, " --verbose") {
		t.Errorf("usage output must not use bare single-dash '-verbose':\n%s", usage)
	}
	if strings.Contains(usage, " -fix") && !strings.Contains(usage, " --fix") {
		t.Errorf("usage output must not use bare single-dash '-fix':\n%s", usage)
	}
}
