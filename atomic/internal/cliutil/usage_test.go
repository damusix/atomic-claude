package cliutil_test

import (
	"bytes"
	"flag"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/cliutil"
)

func TestSetUsage_DoubleDash(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	var buf bytes.Buffer
	fs.SetOutput(&buf)
	fs.Bool("check", false, "only check for updates")

	cliutil.SetUsage(fs, "atomic update [options]")
	fs.Usage()

	out := buf.String()
	if !strings.Contains(out, "--check") {
		t.Errorf("expected --check in output, got:\n%s", out)
	}
	// "  -check" is what Go's own PrintDefaults would emit.
	if strings.Contains(out, "  -check") {
		t.Errorf("found single-dash '  -check' in output, got:\n%s", out)
	}
}

func TestSetUsage_UsageLineHeader(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	var buf bytes.Buffer
	fs.SetOutput(&buf)
	fs.Bool("check", false, "check flag")

	cliutil.SetUsage(fs, "atomic update [--check]")
	fs.Usage()

	out := buf.String()
	if !strings.Contains(out, "atomic update [--check]") {
		t.Errorf("usage line not in output, got:\n%s", out)
	}
}

func TestSetUsage_StringFlagWithDefault(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	var buf bytes.Buffer
	fs.SetOutput(&buf)
	fs.String("channel", "stable", "release channel")

	cliutil.SetUsage(fs, "atomic update [options]")
	fs.Usage()

	out := buf.String()
	if !strings.Contains(out, "(default stable)") {
		t.Errorf("expected '(default stable)' in output, got:\n%s", out)
	}
}

func TestSetUsage_BoolFlagFalseNoDefault(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	var buf bytes.Buffer
	fs.SetOutput(&buf)
	fs.Bool("check", false, "only check")

	cliutil.SetUsage(fs, "atomic update [options]")
	fs.Usage()

	out := buf.String()
	if strings.Contains(out, "(default false)") {
		t.Errorf("bool false default must not appear in output, got:\n%s", out)
	}
}

// A flag like --target that embeds "(default …)" in its own Usage text must not
// get a second one appended.
func TestSetUsage_NoDoubleDefault(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	var buf bytes.Buffer
	fs.SetOutput(&buf)
	fs.String("target", "~/.claude", "target directory (default ~/.claude)")

	cliutil.SetUsage(fs, "atomic claude install [options]")
	fs.Usage()

	out := buf.String()
	if count := strings.Count(out, "(default ~/.claude)"); count != 1 {
		t.Errorf("expected exactly 1 occurrence of \"(default ~/.claude)\", got %d:\n%s", count, out)
	}
}

// The counterpart: a flag with no "(default" in its Usage still gets one.
func TestSetUsage_StringFlagWithDefaultNoExisting(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	var buf bytes.Buffer
	fs.SetOutput(&buf)
	fs.String("channel", "stable", "release channel")

	cliutil.SetUsage(fs, "atomic update [options]")
	fs.Usage()

	out := buf.String()
	if !strings.Contains(out, "(default stable)") {
		t.Errorf("expected appended \"(default stable)\" suffix, got:\n%s", out)
	}
}

func TestSetUsage_ZeroFlags(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	var buf bytes.Buffer
	fs.SetOutput(&buf)

	cliutil.SetUsage(fs, "atomic list")
	fs.Usage()

	out := buf.String()
	if !strings.Contains(out, "atomic list") {
		t.Errorf("usage line missing, got:\n%s", out)
	}
	if strings.Contains(out, "Options:") {
		t.Errorf("Options: section must not appear when no flags, got:\n%s", out)
	}
}
