package wiki

// action_test.go — CP1 tests for the shared argument scanner hardening
// (resolveWikiRoot / parseBucketDocArgs) behind `atomic wiki bucket`.
//
// Covers: -h/-help/--help yield errUsageRequested and print usage without
// mutating state (issue #164 — `bucket add -h` used to silently create a
// bucket named "-h"); an unrecognized single-dash token is rejected with
// the same parity as an unrecognized double-dash token; parseBucketDocArgs'
// collapse to delegate at resolveWikiRoot still honors --router in any
// position.

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---- resolveWikiRoot: help sentinel ----

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

// ---- resolveWikiRoot: unrecognized dash tokens ----

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

// ---- parseBucketDocArgs: delegates to resolveWikiRoot ----

func TestParseBucketDocArgs_HelpSentinel(t *testing.T) {
	cwd := t.TempDir()
	for _, tok := range []string{"-h", "-help", "--help"} {
		t.Run(tok, func(t *testing.T) {
			_, _, router, err := parseBucketDocArgs([]string{tok}, cwd)
			if !errors.Is(err, errUsageRequested) {
				t.Fatalf("parseBucketDocArgs(%q): got err %v, want errUsageRequested", tok, err)
			}
			if router {
				t.Errorf("parseBucketDocArgs(%q): router should be false on help sentinel", tok)
			}
		})
	}
}

func TestParseBucketDocArgs_UnrecognizedSingleDashRejected(t *testing.T) {
	cwd := t.TempDir()
	_, _, _, err := parseBucketDocArgs([]string{"-x"}, cwd)
	if err == nil || errors.Is(err, errUsageRequested) {
		t.Fatalf("expected non-sentinel rejection for -x, got %v", err)
	}
}

func TestParseBucketDocArgs_RouterAnyPosition(t *testing.T) {
	cwd := t.TempDir()
	cases := [][]string{
		{"--router", "mybucket", "slug"},
		{"mybucket", "--router", "slug"},
		{"mybucket", "slug", "--router"},
		{"--root=" + cwd, "mybucket", "slug", "--router"},
		{"mybucket", "--router", "slug", "--root=" + cwd},
	}
	for _, args := range cases {
		absRoot, positional, router, err := parseBucketDocArgs(args, cwd)
		if err != nil {
			t.Fatalf("args %v: unexpected error: %v", args, err)
		}
		if !router {
			t.Errorf("args %v: expected --router honored, got false", args)
		}
		if len(positional) != 2 || positional[0] != "mybucket" || positional[1] != "slug" {
			t.Errorf("args %v: expected positional [mybucket slug], got %v", args, positional)
		}
		if absRoot == "" {
			t.Errorf("args %v: expected resolved root", args)
		}
	}
}

// TestParseBucketDocArgs_RouterConsumedBeforeRootValue pins a deliberate
// behavior delta flagged in CP1 review: parseBucketDocArgs strips every
// literal "--router" token before delegating to resolveWikiRoot, so
// `--root --router` reaches resolveWikiRoot as bare "--root" and errors
// "flag --root requires a value" rather than silently taking "--router" as
// the root path. A root path is never legitimately named "--router", so
// erroring is correct — this is a pinned decision, not a regression.
func TestParseBucketDocArgs_RouterConsumedBeforeRootValue(t *testing.T) {
	cwd := t.TempDir()
	_, _, _, err := parseBucketDocArgs([]string{"--root", "--router"}, cwd)
	if err == nil {
		t.Fatal("expected error for --root immediately followed by --router, got nil")
	}
	if errors.Is(err, errUsageRequested) {
		t.Fatal("expected a value-required error, not the help sentinel")
	}
	if !strings.Contains(err.Error(), "flag --root requires a value") {
		t.Errorf("expected \"flag --root requires a value\", got: %v", err)
	}
}

func TestParseBucketDocArgs_RouterAbsentDefaultsFalse(t *testing.T) {
	cwd := t.TempDir()
	_, positional, router, err := parseBucketDocArgs([]string{"mybucket", "slug"}, cwd)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if router {
		t.Error("expected router false when --router absent")
	}
	if len(positional) != 2 {
		t.Errorf("expected 2 positionals, got %v", positional)
	}
}

// ---- wikiAction integration: help probe across all seven bucket verbs ----

// TestBucketVerbs_HelpFlagPrintsUsageAndExitsZero verifies -h, -help, and
// --help all print usage to out and exit 0 for every bucket sub-verb.
func TestBucketVerbs_HelpFlagPrintsUsageAndExitsZero(t *testing.T) {
	root, _, wikiDir := setupBucketCLIRoot(t)
	claudeHome := t.TempDir()
	writeBucketCLIFile(t, filepath.Join(wikiDir, "index.md"), "# Wiki\n")

	verbs := []string{"add", "list", "diff", "promote", "doc", "skill", "index"}
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

// TestBucketAdd_HelpProbeCreatesNothing reproduces issue #164 directly:
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

	verbs := []string{"add", "list", "diff", "promote", "doc", "skill", "index"}
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
