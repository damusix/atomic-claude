package wiki

// action_test.go — tests for the shared argument scanner hardening
// (resolveWikiRoot) behind `atomic wiki bucket`.
//
// Covers: -h/-help/--help yield errUsageRequested and print usage without
// mutating state (`bucket add -h` used to silently create a
// bucket named "-h"); an unrecognized single-dash token is rejected with
// the same parity as an unrecognized double-dash token.

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveWikiRoot_HelpTokensYieldSentinel(t *testing.T) {
	cwd := t.TempDir()
	for _, tok := range []string{"-h", "-help", "--help"} {
		t.Run(tok, func(t *testing.T) {
			_, positional, err := resolveWikiRoot([]string{tok}, cwd)
			if !errors.Is(err, errUsageRequested) {
				t.Fatalf("resolveWikiRoot(%q): got err %v, want errUsageRequested", tok, err)
			}
			if positional != nil {
				t.Errorf("resolveWikiRoot(%q): expected nil positional, got %v", tok, positional)
			}
		})
	}
}

func TestResolveWikiRoot_HelpTokenAnyPosition(t *testing.T) {
	cwd := t.TempDir()
	_, _, err := resolveWikiRoot([]string{"--root=" + cwd, "mybucket", "-h"}, cwd)
	if !errors.Is(err, errUsageRequested) {
		t.Fatalf("expected errUsageRequested after --root and a positional, got %v", err)
	}
}

func TestResolveWikiRoot_UnrecognizedSingleDashRejected(t *testing.T) {
	cwd := t.TempDir()
	_, positional, err := resolveWikiRoot([]string{"-x"}, cwd)
	if err == nil {
		t.Fatalf("expected error for unrecognized single-dash token, got positional %v", positional)
	}
	if errors.Is(err, errUsageRequested) {
		t.Fatal("unrecognized token must not be classified as a help request")
	}
	if !strings.Contains(err.Error(), `"-x"`) {
		t.Errorf("expected error to name the token, got: %v", err)
	}
}

func TestResolveWikiRoot_UnrecognizedDoubleDashRejected(t *testing.T) {
	cwd := t.TempDir()
	_, _, err := resolveWikiRoot([]string{"--bogus"}, cwd)
	if err == nil {
		t.Fatal("expected error for unrecognized double-dash token, got nil")
	}
	if !strings.Contains(err.Error(), `"--bogus"`) {
		t.Errorf("expected error to name the token, got: %v", err)
	}
}

func TestResolveWikiRoot_UnrecognizedDashTokenDoesNotLeakIntoPositional(t *testing.T) {
	cwd := t.TempDir()
	// This is the exact shape of the #164 defect: a single-dash token after a
	// positional must be rejected, not appended to positional as a second name.
	_, positional, err := resolveWikiRoot([]string{"mybucket", "-x"}, cwd)
	if err == nil {
		t.Fatalf("expected rejection, got positional %v", positional)
	}
}

func TestBucketVerbs_HelpFlagPrintsUsageAndExitsZero(t *testing.T) {
	root, _, wikiDir := setupBucketCLIRoot(t)
	claudeHome := t.TempDir()
	writeBucketCLIFile(t, filepath.Join(wikiDir, "index.md"), "# Wiki\n")

	verbs := []string{"add", "list", "doc", "skill", "index"}
	helpTokens := []string{"-h", "-help", "--help"}

	for _, verb := range verbs {
		for _, tok := range helpTokens {
			t.Run(verb+"/"+tok, func(t *testing.T) {
				var out bytes.Buffer
				code := wikiAction([]string{"bucket", verb, "--root=" + root, tok}, claudeHome, root, &out)
				if code != 0 {
					t.Fatalf("wikiAction bucket %s %s: got exit %d, want 0; output: %q", verb, tok, code, out.String())
				}
				if !strings.Contains(out.String(), "Usage:") {
					t.Errorf("wikiAction bucket %s %s: expected usage text on out, got: %q", verb, tok, out.String())
				}
			})
		}
	}
}

// TestBucketAdd_HelpProbeCreatesNothing reproduces that directly:
// `atomic wiki bucket add -h` must not create a bucket named "-h" (or
// anything else).
func TestBucketAdd_HelpProbeCreatesNothing(t *testing.T) {
	root := t.TempDir()
	claudeHome := t.TempDir()

	var out bytes.Buffer
	code := wikiAction([]string{"bucket", "add", "-h"}, claudeHome, root, &out)
	if code != 0 {
		t.Fatalf("expected exit 0 for -h, got %d; output: %q", code, out.String())
	}
	if !strings.Contains(out.String(), "Usage:") {
		t.Errorf("expected usage text on out, got: %q", out.String())
	}

	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("expected no side effects from -h probe, found: %v", names)
	}
}

// TestBucketAdd_DoubleDashHelpProbeCreatesNothing is the --help counterpart.
func TestBucketAdd_DoubleDashHelpProbeCreatesNothing(t *testing.T) {
	root := t.TempDir()
	claudeHome := t.TempDir()

	var out bytes.Buffer
	code := wikiAction([]string{"bucket", "add", "--help"}, claudeHome, root, &out)
	if code != 0 {
		t.Fatalf("expected exit 0 for --help, got %d; output: %q", code, out.String())
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("expected no side effects from --help probe, found %d entries", len(entries))
	}
}

// TestBucketVerbs_UnrecognizedSingleDashTokenExits2 verifies parity with the
// existing double-dash rejection: an unrecognized single-dash token is
// rejected with exit 2 across every bucket sub-verb.
func TestBucketVerbs_UnrecognizedSingleDashTokenExits2(t *testing.T) {
	root, _, wikiDir := setupBucketCLIRoot(t)
	claudeHome := t.TempDir()
	writeBucketCLIFile(t, filepath.Join(wikiDir, "index.md"), "# Wiki\n")

	verbs := []string{"add", "list", "doc", "skill", "index"}
	for _, verb := range verbs {
		t.Run(verb, func(t *testing.T) {
			var out bytes.Buffer
			code := wikiAction([]string{"bucket", verb, "--root=" + root, "-x"}, claudeHome, root, &out)
			if code != 2 {
				t.Errorf("wikiAction bucket %s -x: got exit %d, want 2; output: %q", verb, code, out.String())
			}
		})
	}
}

func TestWikiAction_RemovedVerbsAreUnknown(t *testing.T) {
	root := t.TempDir()
	for _, args := range [][]string{
		{"stamp", "f.md", "--repo", root},
		{"mark-dirty", "--root", root},
		{"bucket", "diff", "notes"},
		{"bucket", "promote", "notes"},
	} {
		oldStderr := os.Stderr
		r, w, _ := os.Pipe()
		os.Stderr = w

		code := wikiAction(args, t.TempDir(), root, &bytes.Buffer{})

		w.Close()
		os.Stderr = oldStderr
		buf := make([]byte, 4096)
		n, _ := r.Read(buf)
		stderr := string(buf[:n])

		if code != 1 {
			t.Errorf("wikiAction %v: got exit %d, want 1", args, code)
		}
		if !strings.Contains(stderr, "unknown verb") {
			t.Errorf("wikiAction %v: stderr %q, want it to report an unknown verb", args, stderr)
		}
	}
}

// A reader grepping stderr by verb must see the prefix on every error line,
// and an unpaired region alone must not fail the command.
func TestReportTreeErrors_PrefixesEveryLineAndOnlyUnpairedIsSoft(t *testing.T) {
	oldStderr := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w
	t.Cleanup(func() { os.Stderr = oldStderr })

	soft := reportTreeErrors("atomic wiki scan", []error{fmt.Errorf("a: %w", errUnpairedRegion)})
	hard := reportTreeErrors("atomic wiki scan", []error{errors.New("second")})

	w.Close()
	os.Stderr = oldStderr
	captured, _ := io.ReadAll(r)

	want := fmt.Sprintf("atomic wiki scan: a: %v\natomic wiki scan: second\n", errUnpairedRegion)
	if got := string(captured); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if soft {
		t.Error("an unpaired region alone reported as a hard failure")
	}
	if !hard {
		t.Error("a non-region error not reported as a hard failure")
	}
}
