package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/realmshare"
)

var realmVerbUsage = []struct {
	verb string
	args string
}{
	{"share", " [<path>...]"},
	{"sync", ""},
	{"join", " <git-url>"},
	{"link", " [<path>...]"},
	{"status", ""},
	{"unlink", ""},
}

func TestRealmCLIParentHelp(t *testing.T) {
	out, diag, code := runAtomicCLIFull(t, t.TempDir(), nil, "root", "realm", "--help")
	help := out + "\n" + diag
	if code != 0 || !strings.Contains(help, "Usage:") || !strings.Contains(help, "atomic realm") {
		t.Fatalf("realm --help: exit %d, want 0 with realm usage:\n%s", code, help)
	}
	words := strings.FieldsFunc(help, func(r rune) bool {
		return strings.ContainsRune(" \t\r\n<>[]|", r)
	})
	for _, tt := range realmVerbUsage {
		if !slices.Contains(words, tt.verb) {
			t.Errorf("realm help does not list %q:\n%s", tt.verb, help)
		}
	}
}

func TestRealmCLIVerbHelp(t *testing.T) {
	for _, tt := range realmVerbUsage {
		t.Run(tt.verb, func(t *testing.T) {
			out, diag, code := runAtomicCLIFull(t, t.TempDir(), nil, "root", "realm", tt.verb, "--help")
			help := out + "\n" + diag
			usage := "atomic realm " + tt.verb + tt.args
			if code != 0 || !strings.Contains(help, "Usage:\n  "+usage) {
				t.Errorf("realm %s --help: exit %d, want 0 with usage %q:\n%s", tt.verb, code, usage, help)
			}
			if flags, ok := realmReviewFlags[tt.verb]; ok {
				for _, flag := range flags {
					if !slices.Contains(strings.Fields(help), flag) {
						t.Errorf("%s help does not list %s:\n%s", tt.verb, flag, help)
					}
				}
				if !strings.Contains(help, "--yes --review") {
					t.Errorf("%s help does not describe headless approval:\n%s", tt.verb, help)
				}
			}
		})
	}
}

func TestRealmCLIStatusJSONHelp(t *testing.T) {
	home := t.TempDir()
	for _, args := range [][]string{{"--help"}, {"--json", "--help"}} {
		out, diag, code := runAtomicCLIFull(t, home, nil, append([]string{"root", "realm", "status"}, args...)...)
		help := out + "\n" + diag
		_, usage, _ := strings.Cut(help, "Usage:\n")
		line, _, _ := strings.Cut(usage, "\n")
		if code != 0 || strings.TrimSuffix(strings.TrimSpace(line), " [flags]") != "atomic realm status" ||
			!slices.Contains(strings.Fields(help), "--json") {
			t.Errorf("realm status %v: exit %d, want positional-free usage listing --json:\n%s", args, code, help)
		}
	}
}

func TestRealmCLIStatusDoesNotBindUnsharedRealm(t *testing.T) {
	root, home := t.TempDir(), t.TempDir()
	writeSteeringTestFile(t, config.RepoConfigPath(root), "scope = \"realm\"\n")
	t.Chdir(root)
	unchanged := unchangedRealmCLITrees(t, root, home)

	out, diag, code := runRealmCLI(t, home, "status")
	unchanged(t)
	report := out
	if code != 0 {
		if strings.TrimSpace(out) != "" {
			t.Errorf("unbound status wrote failure details to stdout: %q", out)
		}
		report = diag
	}
	lower := strings.ToLower(report)
	if !regexp.MustCompile(`\b(?:not bound|unbound|no\b[^\n]*\bbinding|not shared)\b`).MatchString(lower) ||
		(!strings.Contains(lower, "atomic realm share") && !strings.Contains(lower, "atomic realm join")) {
		t.Errorf("unbound status did not explain the missing binding and next step: exit %d\nstdout: %s\nstderr: %s", code, out, diag)
	}
}

func TestRealmCLIStatusRejectsArgumentsWithoutMutation(t *testing.T) {
	f := newRealmLinkCLIFixture(t)
	clone := realmCLIManagedClone(t, f.joinerHome)
	writeSteeringTestFile(t, filepath.Join(clone, shareSelected), "# Must remain unpublished\n")
	unchanged := unchangedRealmCLITrees(t, f.joiner, f.joinerHome, f.remote)

	for _, flags := range [][]string{nil, {"--json"}} {
		for _, arg := range []string{shareSelected, "--dry-run"} {
			args := append(slices.Clone(flags), arg)
			out, diag, code := runRealmCLI(t, f.joinerHome, "status", args...)
			unchanged(t)
			if code == 0 || strings.TrimSpace(out) != "" || !strings.Contains(diag, arg) {
				t.Errorf("status %v did not reject %q on stderr: exit %d\nstdout: %s\nstderr: %s", args, arg, code, out, diag)
			}
		}
	}
}

func TestRealmCLIStatusJSONCacheProvenance(t *testing.T) {
	f := newRealmLinkCLIFixture(t)
	head := strings.TrimSpace(runGit(t, f.remote, "rev-parse", "refs/heads/"+shareBranch))
	unchanged := unchangedRealmCLITrees(t, f.publisher, f.publisherHome, f.joiner, f.joinerHome, f.remote)
	for _, tt := range []struct{ name, root, home string }{
		{"publisher before sync", f.publisher, f.publisherHome},
		{"joiner before sync", f.joiner, f.joinerHome},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Chdir(tt.root)
			out, diag, code := runRealmCLI(t, tt.home, "status", "--json")
			unchanged(t)
			var report map[string]json.RawMessage
			if err := json.Unmarshal([]byte(out), &report); code != 0 || err != nil || report == nil {
				t.Fatalf("status --json: exit %d, decode error %v\nstdout: %s\nstderr: %s", code, err, out, diag)
			}
			if diag = strings.TrimSpace(diag); diag != "" && diag != "PASS" {
				t.Errorf("status --json wrote unexpected diagnostics: %s", diag)
			}
			raw, present := report["cached_remote"]
			if !present {
				t.Fatalf("cached_remote missing before sync: %s", out)
			}
			var cached struct {
				Head            string
				DivergenceKnown bool            `json:"divergence_known"`
				LastSuccess     json.RawMessage `json:"last_success"`
			}
			if err := json.Unmarshal(raw, &cached); err != nil || cached.Head != head || !cached.DivergenceKnown || cached.LastSuccess != nil {
				t.Errorf("cached_remote should report the published revision %s with known divergence and no successful sync time: %s (%v)", head, raw, err)
			}
		})
	}
}

func TestRealmCLIStatusTextAndJSONReportSameObservedState(t *testing.T) {
	f := newRealmLinkCLIFixture(t)
	clone := realmCLIManagedClone(t, f.joinerHome)
	mustRunRealmCLI(t, f.joinerHome, "sync")
	t.Cleanup(config.SetHomeDirForTest(f.joinerHome))
	binding, err := realmshare.LoadBinding(f.joiner)
	if err != nil || binding.Sync == nil || binding.Sync.LastSuccess.IsZero() {
		t.Fatalf("fixture sync did not record a successful observation: %+v (%v)", binding.Sync, err)
	}
	review := previewRealmReview(t, f.joinerHome, "link", shareSelected)
	mustRunRealmCLI(t, f.joinerHome, "link", shareSelected, "--yes", "--review", review)

	t.Chdir(f.publisher)
	writeSteeringTestFile(t, filepath.Join(f.publisher, shareSelected), "# Publisher version\n")
	mustRunRealmCLI(t, f.publisherHome, "sync")
	observedHead := strings.TrimSpace(runGit(t, f.remote, "rev-parse", "refs/heads/"+shareBranch))

	t.Chdir(f.joiner)
	writeSteeringTestFile(t, filepath.Join(clone, shareSelected), "# Joiner version\n")
	_, diag, code := runRealmCLI(t, f.joinerHome, "sync")
	if code == 0 || runGit(t, clone, "ls-files", "--unmerged", "--", shareSelected) == "" {
		t.Fatalf("fixture did not retain an unmerged shared path: exit %d\n%s", code, diag)
	}
	quotedClone := quotedRealmCLIShellPath(binding.ClonePath)
	for _, command := range []string{
		"git -C " + quotedClone + " add -- <paths>",
		"git -C " + quotedClone + " commit and retry",
	} {
		if !strings.Contains(diag, command) {
			t.Errorf("sync diagnostic lacks %q:\n%s", command, diag)
		}
	}
	head := strings.TrimSpace(runGit(t, clone, "rev-parse", "HEAD"))
	drifted := filepath.Join(f.joiner, shareSelected)
	if err := os.Remove(drifted); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(f.joiner, "AGENTS.local.md"), drifted); err != nil {
		t.Fatal(err)
	}
	dirty := filepath.Join(clone, sharedRealmInstructions)
	writeSteeringTestFile(t, dirty, "# Staged team guidance\n")
	runGit(t, clone, "add", "--", sharedRealmInstructions)
	writeSteeringTestFile(t, dirty, "# Unstaged team guidance\n")

	t.Chdir(f.publisher)
	writeSteeringTestFile(t, filepath.Join(f.publisher, shareSelected), "# New remote version not observed by the joiner\n")
	mustRunRealmCLI(t, f.publisherHome, "sync")
	t.Chdir(f.joiner)

	unchanged := unchangedRealmCLITrees(t, f.joiner, f.joinerHome, f.publisher, f.publisherHome, f.remote)
	text, textDiag, textCode := runRealmCLI(t, f.joinerHome, "status")
	unchanged(t)
	raw, jsonDiag, jsonCode := runRealmCLI(t, f.joinerHome, "status", "--json")
	unchanged(t)
	if textCode != 0 || jsonCode != 0 {
		t.Fatalf("status: exit %d\nstdout: %s\nstderr: %s\nstatus --json: exit %d\nstdout: %s\nstderr: %s",
			textCode, text, textDiag, jsonCode, raw, jsonDiag)
	}
	for mode, diag := range map[string]string{"text": textDiag, "JSON": jsonDiag} {
		if diag = strings.TrimSpace(diag); diag != "" && diag != "PASS" {
			t.Errorf("%s status wrote unexpected diagnostics: %s", mode, diag)
		}
	}
	if json.Valid([]byte(text)) {
		t.Errorf("status without --json emitted JSON: %s", text)
	}

	type statusPath struct {
		Path, Source, State string
		Drift               bool
	}

	var report struct {
		RealmRoot string `json:"realm_root"`
		StateRoot string `json:"state_root"`
		ClonePath string `json:"clone_path"`
		RealmID   string `json:"realm_id"`
		Remote    string `json:"remote"`
		Branch    string `json:"branch"`
		Head      string `json:"head"`
		Paths     []statusPath
		Changes   []struct {
			Path, Index, Worktree string
		}
		Conflicts []struct {
			Source, Path string
		}
		CachedRemote *struct {
			Head            string
			Ahead           int
			Behind          int
			DivergenceKnown bool      `json:"divergence_known"`
			LastSuccess     time.Time `json:"last_success"`
		} `json:"cached_remote"`
	}
	if err := json.Unmarshal([]byte(raw), &report); err != nil {
		t.Fatalf("status --json stdout is not a JSON report: %v\n%s", err, raw)
	}
	if report.RealmID != binding.RealmID || report.Remote != binding.Remote || report.Branch != shareBranch || report.Head != head {
		t.Errorf("status --json does not identify the bound checkout: %s", raw)
	}
	for _, path := range []struct{ got, want string }{
		{report.RealmRoot, f.joiner},
		{report.StateRoot, filepath.Dir(config.RepoConfigPath(f.joiner))},
		{report.ClonePath, clone},
	} {
		got, gotErr := os.Stat(path.got)
		want, wantErr := os.Stat(path.want)
		if gotErr != nil || wantErr != nil || !os.SameFile(got, want) {
			t.Errorf("status --json path %q does not identify %q", path.got, path.want)
		}
	}
	if !slices.ContainsFunc(report.Paths, func(path statusPath) bool {
		return path.Path == shareSelected && path.Source == shareSelected && path.Drift
	}) || !slices.ContainsFunc(report.Paths, func(path statusPath) bool {
		return path.Path == sharedRealmInstructions && path.Source == sharedRealmInstructions && path.State == "available" && !path.Drift
	}) {
		t.Errorf("status --json does not distinguish the drifted link and available path: %s", raw)
	}
	if !slices.ContainsFunc(report.Conflicts, func(conflict struct{ Source, Path string }) bool {
		return conflict.Source == shareSelected && conflict.Path == shareSelected
	}) {
		t.Errorf("status --json does not report the mapped merge conflict: %s", raw)
	}
	if !slices.ContainsFunc(report.Changes, func(change struct{ Path, Index, Worktree string }) bool {
		return change.Path == sharedRealmInstructions && strings.TrimSpace(change.Index) != "" && strings.TrimSpace(change.Worktree) != ""
	}) {
		t.Errorf("status --json does not report staged and unstaged changes: %s", raw)
	}
	cached := report.CachedRemote
	if cached == nil || cached.Head != observedHead || !cached.DivergenceKnown || cached.Ahead != 1 || cached.Behind != 1 ||
		!cached.LastSuccess.Equal(binding.Sync.LastSuccess) {
		t.Errorf("status --json does not report cached 1/1 divergence and the prior successful sync: %s", raw)
	}
	for _, fact := range []string{binding.RealmID, binding.Remote, shareBranch, head[:7], observedHead[:7],
		shareSelected, sharedRealmInstructions, binding.Sync.LastSuccess.UTC().Format("2006-01-02T15:04:05")} {
		if !strings.Contains(text, fact) {
			t.Errorf("text status omits %q:\n%s", fact, text)
		}
	}
	lines := strings.Split(text, "\n")
	for _, want := range []struct{ path, state string }{
		{shareSelected, "drift"},
		{sharedRealmInstructions, "available"},
	} {
		if !slices.ContainsFunc(lines, func(line string) bool {
			return strings.Contains(line, want.path) && strings.Contains(strings.ToLower(line), want.state)
		}) {
			t.Errorf("text status does not identify %q as %s:\n%s", want.path, want.state, text)
		}
	}
	lower := strings.ToLower(text)
	for _, pattern := range []string{
		`\bcached\b`, `\bconflict\w*\b`, `\bchanges\b`,
		`\b(?:ahead\W+1|1\W+ahead)\b`, `\b(?:behind\W+1|1\W+behind)\b`,
	} {
		if !regexp.MustCompile(pattern).MatchString(lower) {
			t.Errorf("text status does not convey %q:\n%s", pattern, text)
		}
	}
}

func TestRealmCLIUnknownVerbFails(t *testing.T) {
	const verb = "not-a-realm-verb"
	_, diag, code := runAtomicCLIFull(t, t.TempDir(), nil, "root", "realm", verb)
	if code == 0 || !strings.Contains(diag, verb) {
		t.Errorf("realm %s: exit %d, want nonzero with a diagnostic naming the unknown verb:\n%s", verb, code, diag)
	}
}

var realmReviewFlags = map[string][]string{
	"share": {"--branch", "--dry-run", "--remote", "--review", "--yes"},
	"link":  {"--dry-run", "--review", "--yes"},
}

func TestRealmCLIShareHelp(t *testing.T) {
	out, diag, code := runAtomicCLIFull(t, t.TempDir(), nil, "root", "realm", "share", "--help")
	help := out + "\n" + diag
	if code != 0 {
		t.Fatalf("realm share --help: exit %d:\n%s", code, help)
	}
	for _, guidance := range []string{
		"--dry-run",
		"local linking is interrupted",
		"rerun share with the same paths to resume",
	} {
		if !strings.Contains(help, guidance) {
			t.Errorf("share help lacks guidance %q:\n%s", guidance, help)
		}
	}
}

const (
	shareSelected = "notes/selected.md"
	sharePrivate  = "notes/private.md"
	shareContents = "# Reviewed notes\n"
	shareBranch   = "shared-docs"
)

var reviewDigestPattern = regexp.MustCompile(`^sha256:[[:xdigit:]]{64}$`)

const wrongReviewDigest = "sha256:0000000000000000000000000000000000000000000000000000000000000000"

func realmReviewDigest(t *testing.T, preview string) string {
	t.Helper()
	var digest string
	for _, line := range strings.Split(preview, "\n") {
		if !strings.HasPrefix(line, "Review digest:") {
			continue
		}
		got, ok := strings.CutPrefix(line, "Review digest: ")
		if digest != "" || !ok || !reviewDigestPattern.MatchString(got) {
			t.Fatalf("want exactly one Review digest: sha256:<hex> line:\n%s", preview)
		}
		digest = got
	}
	if digest == "" {
		t.Fatalf("preview lacks a Review digest: sha256:<hex> line:\n%s", preview)
	}
	return digest
}

func previewRealmReview(t *testing.T, home, verb string, args ...string) string {
	t.Helper()
	return realmReviewDigest(t, mustRunRealmCLI(t, home, verb, append(slices.Clone(args), "--dry-run")...))
}

func newRealmShareCLIFixture(t *testing.T) (root, home, remote string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git unavailable: %v", err)
	}
	root, home, remote = t.TempDir(), t.TempDir(), t.TempDir()
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_AUTHOR_NAME", "Realm Share Test")
	t.Setenv("GIT_AUTHOR_EMAIL", "realm@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "Realm Share Test")
	t.Setenv("GIT_COMMITTER_EMAIL", "realm@example.com")
	runGit(t, remote, "init", "--bare")
	writeRealmShareCLIFiles(t, root, shareContents)
	t.Chdir(root)
	return root, home, remote
}

func writeRealmShareCLIFiles(t *testing.T, root, contents string) {
	t.Helper()
	writeSteeringTestFile(t, config.RepoConfigPath(root), "scope = \"realm\"\n")
	writeSteeringTestFile(t, filepath.Join(root, shareSelected), contents)
	writeSteeringTestFile(t, filepath.Join(root, sharePrivate), "# Local only\n")
}

func runRealmShareCLI(t *testing.T, home string, args ...string) (string, string, int) {
	t.Helper()
	return runRealmCLI(t, home, "share", args...)
}

func runRealmCLI(t *testing.T, home, verb string, args ...string) (string, string, int) {
	t.Helper()
	return runAtomicCLIFull(t, home, []string{"PATH=" + os.Getenv("PATH"), "ATOMIC_TEST_CLI_IN=" + os.DevNull},
		append([]string{"root", "realm", verb}, args...)...)
}

func assertRealmShareCLINotPublished(t *testing.T, root, home, remote string) {
	t.Helper()
	if refs := runGit(t, remote, "for-each-ref"); refs != "" {
		t.Errorf("unapproved operation published refs:\n%s", refs)
	}
	if entries, err := os.ReadDir(home); err != nil || len(entries) != 0 {
		t.Errorf("unapproved operation changed home: entries %v, error %v", entries, err)
	}
	assertRealmShareCLIRealmUnchanged(t, root, shareContents)
}

func assertRealmShareCLIRealmUnchanged(t *testing.T, root, selectedContents string) {
	t.Helper()
	var paths []string
	err := filepath.WalkDir(root, func(path string, _ os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path != root {
			paths = append(paths, path)
		}
		return nil
	})
	marker := config.RepoConfigPath(root)
	want := []string{filepath.Dir(marker), marker, filepath.Join(root, "notes"), filepath.Join(root, shareSelected), filepath.Join(root, sharePrivate)}
	slices.Sort(want)
	if err != nil || !slices.Equal(paths, want) {
		t.Errorf("realm paths = %v, want %v; walk error: %v", paths, want, err)
	}
	if contents := readSteeringTestFile(t, marker); contents != "scope = \"realm\"\n" {
		t.Errorf("realm scope marker changed: %q", contents)
	}
	assertRealmShareCLIFile(t, root, shareSelected, selectedContents, false)
	assertRealmShareCLIFile(t, root, sharePrivate, "# Local only\n", false)
}

func assertRealmShareCLIFile(t *testing.T, root, rel, want string, linked bool) {
	t.Helper()
	path := filepath.Join(root, rel)
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatalf("lstat %s: %v", rel, err)
	}
	if got := info.Mode()&os.ModeSymlink != 0; got != linked {
		t.Errorf("%s is a symlink: %t, want %t", rel, got, linked)
	}
	if contents := readSteeringTestFile(t, path); contents != want {
		t.Errorf("%s contains %q, want %q", rel, contents, want)
	}
}

func TestRealmCLIShareRejectsIncompleteNoninteractiveInput(t *testing.T) {
	root, home, remote := newRealmShareCLIFixture(t)
	unchanged := unchangedRealmCLITrees(t, root, home, remote)
	for _, tt := range []struct {
		name string
		args []string
		want []string
	}{
		{"missing approval", []string{shareSelected, "--remote", remote}, []string{"--yes"}},
		{"missing review", []string{shareSelected, "--remote", remote, "--yes"}, []string{"--review"}},
		{"missing selection", []string{"--remote", remote, "--yes", "--review", wrongReviewDigest}, []string{"path"}},
		{"missing selection in dry run", []string{"--remote", remote, "--dry-run"}, []string{"path"}},
		{"missing initial remote", []string{shareSelected, "--yes", "--review", wrongReviewDigest}, []string{"--remote"}},
		{"conflicting approval flags", []string{shareSelected, "--remote", remote, "--dry-run", "--yes"}, []string{"--dry-run", "--yes"}},
		{"review without yes", []string{shareSelected, "--remote", remote, "--review", wrongReviewDigest}, []string{"--review", "--yes"}},
		{"review with dry run", []string{shareSelected, "--remote", remote, "--dry-run", "--review", wrongReviewDigest}, []string{"--review"}},
		{"empty review with dry run", []string{shareSelected, "--remote", remote, "--dry-run", "--review="}, []string{"--review"}},
		{"wrong review", []string{shareSelected, "--remote", remote, "--yes", "--review", wrongReviewDigest}, []string{"review", "--dry-run"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			out, diag, code := runRealmShareCLI(t, home, tt.args...)
			if code == 0 {
				t.Errorf("share exited 0 without valid inputs:\nstdout: %s\nstderr: %s", out, diag)
			}
			for _, hint := range tt.want {
				if !strings.Contains(strings.ToLower(diag), hint) {
					t.Errorf("share diagnostic lacks %q:\n%s", hint, diag)
				}
			}
			assertRealmShareCLINotPublished(t, root, home, remote)
			unchanged(t)
		})
	}
}

func TestRealmCLIShareDryRun(t *testing.T) {
	root, home, remote := newRealmShareCLIFixture(t)
	unchanged := unchangedRealmCLITrees(t, root, home, remote)
	out, diag, code := runRealmShareCLI(t, home, shareSelected, "--remote", remote, "--dry-run")
	if code != 0 {
		t.Fatalf("share --dry-run: exit %d:\nstdout: %s\nstderr: %s", code, out, diag)
	}
	for _, want := range []string{shareSelected, remote, "main", "symlink", "bytes"} {
		if !strings.Contains(out, want) {
			t.Errorf("preview lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, sharePrivate) {
		t.Errorf("preview includes unselected path %q:\n%s", sharePrivate, out)
	}
	review := realmReviewDigest(t, out)
	if again := previewRealmReview(t, home, "share", shareSelected, "--remote", remote); again != review {
		t.Errorf("unchanged share previews returned different digests: %s and %s", review, again)
	}
	unchanged(t)
	assertRealmShareCLINotPublished(t, root, home, remote)
}

func TestRealmCLIShareRejectsReviewAfterSourceEdit(t *testing.T) {
	root, home, remote := newRealmShareCLIFixture(t)
	original := filepath.Join(root, shareSelected)
	stamp := time.Unix(1_700_000_000, 0)
	if err := os.Chtimes(original, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	args := []string{shareSelected, "--remote", remote, "--branch", shareBranch}
	preview := mustRunRealmCLI(t, home, "share", append(args, "--dry-run")...)
	review := realmReviewDigest(t, preview)

	writeSteeringTestFile(t, original, strings.Replace(shareContents, "notes", "Notes", 1))
	if err := os.Chtimes(original, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	unchanged := unchangedRealmCLITrees(t, root, home, remote)
	out, diag, code := runRealmShareCLI(t, home, append(args, "--yes", "--review", review)...)
	failure := strings.ToLower(diag)
	if code == 0 || !strings.Contains(failure, "review") || !strings.Contains(failure, "--dry-run") {
		t.Errorf("share did not reject a stale review after a same-length source edit: exit %d\nstdout: %s\nstderr: %s", code, out, diag)
	}
	unchanged(t)
	current := mustRunRealmCLI(t, home, "share", append(args, "--dry-run")...)
	if digest := realmReviewDigest(t, current); digest == review {
		t.Errorf("same-length source edit did not change the review digest: %s", digest)
	}
}

func TestRealmCLIShareLeavesLegacyHomeUnchangedWithoutApproval(t *testing.T) {
	for _, tt := range []struct {
		name   string
		dryRun bool
	}{
		{"dry run", true},
		{"missing approval", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, home, remote := newRealmShareCLIFixture(t)
			legacy := filepath.Join(home, ".claude", ".atomic")
			const profile = "# Legacy user profile\n"
			writeSteeringTestFile(t, filepath.Join(legacy, "profile.md"), profile)

			args := []string{shareSelected, "--remote", remote}
			if tt.dryRun {
				args = append(args, "--dry-run")
			}
			out, diag, code := runRealmShareCLI(t, home, args...)
			if tt.dryRun {
				if code != 0 || !strings.Contains(out, shareSelected) {
					t.Errorf("dry run did not preview selection: exit %d\nstdout: %s\nstderr: %s", code, out, diag)
				}
			} else if code == 0 || !strings.Contains(diag, "--yes") {
				t.Errorf("share did not reject missing approval: exit %d\nstdout: %s\nstderr: %s", code, out, diag)
			}

			if _, err := os.Lstat(config.Dir(home)); !os.IsNotExist(err) {
				t.Errorf("expected no migrated state at %s, got %v", config.Dir(home), err)
			}
			info, err := os.Lstat(legacy)
			if err != nil || !info.IsDir() {
				t.Fatalf("legacy state is no longer a real directory: %v (%v)", info, err)
			}
			assertRealmShareCLIFile(t, legacy, "profile.md", profile, false)
		})
	}
}

func TestRealmCLIShareDoesNotIgnoreRepoOverride(t *testing.T) {
	cwdRealm, home, remote := newRealmShareCLIFixture(t)
	overrideRealm := t.TempDir()
	const overrideContents = "# Override realm notes\n"
	writeRealmShareCLIFiles(t, overrideRealm, overrideContents)

	run := func(flags ...string) (string, string, int) {
		t.Helper()
		args := []string{"root", "--repo", overrideRealm, "realm", "share", shareSelected, "--remote", remote}
		return runAtomicCLIFull(t, home, []string{"PATH=" + os.Getenv("PATH"), "ATOMIC_TEST_CLI_IN=" + os.DevNull},
			append(args, flags...)...)
	}
	out, diag, code := run("--dry-run")
	if code == 0 {
		out, diag, code = run("--yes", "--review", realmReviewDigest(t, out))
	}
	if code != 0 {
		if !strings.Contains(diag, "--repo") {
			t.Errorf("share rejected the operation without explaining the override: exit %d\nstdout: %s\nstderr: %s", code, out, diag)
		}
		assertRealmShareCLINotPublished(t, cwdRealm, home, remote)
		assertRealmShareCLIRealmUnchanged(t, overrideRealm, overrideContents)
		return
	}
	assertRealmShareCLIRealmUnchanged(t, cwdRealm, shareContents)
	assertRealmShareCLIFile(t, overrideRealm, shareSelected, overrideContents, true)
	assertRealmShareCLIFile(t, overrideRealm, sharePrivate, "# Local only\n", false)
}

func TestRealmCLIShareTreatsFlagsAfterTerminatorAsPaths(t *testing.T) {
	cwdRealm, home, remote := newRealmShareCLIFixture(t)
	overrideRealm := t.TempDir()
	const overrideContents = "# Override realm notes\n"
	const literalRepo = "--repo=notes"
	const literalContents = "# Literal flag filename\n"
	writeRealmShareCLIFiles(t, overrideRealm, overrideContents)
	literals := []string{literalRepo, "--repo", "--version", "--no-update-check", "--dry-run", "--yes", "--review"}
	for _, name := range literals {
		writeSteeringTestFile(t, filepath.Join(overrideRealm, name), literalContents)
	}
	run := func(flags, paths []string) (string, string, int) {
		t.Helper()
		args := []string{"root", "--repo", overrideRealm, "realm", "share", "--remote", remote}
		args = append(append(args, flags...), "--")
		return runAtomicCLIFull(t, home, []string{"PATH=" + os.Getenv("PATH"), "ATOMIC_TEST_CLI_IN=" + os.DevNull},
			append(args, paths...)...)
	}

	for _, name := range literals {
		t.Run(name, func(t *testing.T) {
			out, diag, code := run([]string{"--dry-run"}, []string{name})
			if code != 0 || !strings.Contains(out, `Local symlink: "`+name+`"`) {
				t.Errorf("share did not preview literal path %q: exit %d\nstdout: %s\nstderr: %s", name, code, out, diag)
			}
			assertRealmShareCLINotPublished(t, cwdRealm, home, remote)
			assertRealmShareCLIFile(t, overrideRealm, name, literalContents, false)
		})
	}

	t.Run("approved share stays in override realm", func(t *testing.T) {
		paths := []string{literalRepo, shareSelected}
		preview, diag, code := run([]string{"--dry-run"}, paths)
		if code != 0 {
			t.Fatalf("preview share with literal path %q: exit %d\nstdout: %s\nstderr: %s", literalRepo, code, preview, diag)
		}
		out, diag, code := run([]string{"--yes", "--review", realmReviewDigest(t, preview)}, paths)
		assertRealmShareCLIRealmUnchanged(t, cwdRealm, shareContents)
		if code != 0 {
			t.Fatalf("share with literal path %q: exit %d\nstdout: %s\nstderr: %s", literalRepo, code, out, diag)
		}
		assertRealmShareCLIFile(t, overrideRealm, shareSelected, overrideContents, true)
		assertRealmShareCLIFile(t, overrideRealm, sharePrivate, "# Local only\n", false)
		for _, name := range literals {
			assertRealmShareCLIFile(t, overrideRealm, name, literalContents, name == literalRepo)
		}
		ref := "refs/heads/main"
		if got := runGit(t, remote, "show", ref+":"+literalRepo); got != literalContents {
			t.Errorf("published literal path contains %q, want %q", got, literalContents)
		}
		if got := runGit(t, remote, "show", ref+":"+shareSelected); got != overrideContents {
			t.Errorf("published selection contains %q, want override realm content %q", got, overrideContents)
		}
	})
}

func TestRealmCLISharePublishesOnlyApprovedPaths(t *testing.T) {
	root, home, remote := newRealmShareCLIFixture(t)
	args := []string{shareSelected, "--remote", remote, "--branch", shareBranch}
	review := previewRealmReview(t, home, "share", args...)
	out, diag, code := runRealmShareCLI(t, home, append(args, "--yes", "--review", review)...)
	if code != 0 {
		t.Fatalf("share --yes: exit %d:\nstdout: %s\nstderr: %s", code, out, diag)
	}
	ref := "refs/heads/" + shareBranch
	if refs := strings.TrimSpace(runGit(t, remote, "for-each-ref", "--format=%(refname)")); refs != ref {
		t.Fatalf("published refs = %q, want %q", refs, ref)
	}
	files := strings.Fields(runGit(t, remote, "ls-tree", "-r", "--name-only", ref))
	if !slices.Contains(files, "realm.toml") {
		t.Errorf("published tree lacks realm.toml: %v", files)
	}
	var selected string
	for _, path := range files {
		switch filepath.Base(path) {
		case filepath.Base(shareSelected):
			selected = path
		case filepath.Base(sharePrivate):
			t.Errorf("published unapproved file %q", path)
		}
	}
	if selected == "" {
		t.Errorf("published tree lacks %s: %v", shareSelected, files)
	} else if got := runGit(t, remote, "show", ref+":"+selected); got != shareContents {
		t.Errorf("published %s contains %q, want %q", selected, got, shareContents)
	}
	assertRealmShareCLIFile(t, root, shareSelected, shareContents, true)
	assertRealmShareCLIFile(t, root, sharePrivate, "# Local only\n", false)
}

func TestRealmCLIShareRetryPreviewsOnlyLocalCutover(t *testing.T) {
	root, home, remote := newRealmShareCLIFixture(t)
	const changed = "# Changed after push"
	original := filepath.Join(root, shareSelected)
	quotedOriginal := "'" + strings.ReplaceAll(original, "'", "'\"'\"'") + "'"
	hook := filepath.Join(remote, "hooks", "post-receive")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\nprintf '%s\\n' '"+changed+"' > "+quotedOriginal+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	args := []string{shareSelected, "--remote", remote, "--branch", shareBranch}
	review := previewRealmReview(t, home, "share", args...)
	out, diag, code := runRealmShareCLI(t, home, append(args, "--yes", "--review", review)...)
	failure := strings.ToLower(diag)
	if code == 0 || !strings.Contains(failure, "is published") || !strings.Contains(failure, "cutover") {
		t.Fatalf("expected failure after publication: exit %d\nstdout: %s\nstderr: %s", code, out, diag)
	}
	assertRealmShareCLIFile(t, root, shareSelected, changed+"\n", false)
	ref := "refs/heads/" + shareBranch
	published := runGit(t, remote, "rev-parse", ref)
	if got := runGit(t, remote, "show", ref+":"+shareSelected); got != shareContents {
		t.Fatalf("published snapshot contains %q, want %q", got, shareContents)
	}
	if err := os.Remove(hook); err != nil {
		t.Fatal(err)
	}
	writeSteeringTestFile(t, original, shareContents)

	out, diag, code = runRealmShareCLI(t, home, append(args, "--dry-run")...)
	if code != 0 {
		t.Fatalf("share retry --dry-run: exit %d\nstdout: %s\nstderr: %s", code, out, diag)
	}
	if cutoverReview := realmReviewDigest(t, out); cutoverReview == review {
		t.Errorf("retry preview reused publication digest: %s", review)
	}
	preview := strings.ToLower(out)
	for _, want := range []string{"published", "cutover", "local symlink", shareSelected} {
		if !strings.Contains(preview, want) {
			t.Errorf("retry preview lacks %q:\n%s", want, out)
		}
	}
	for _, unwanted := range []string{"publication preview", "publish a snapshot"} {
		if strings.Contains(preview, unwanted) {
			t.Errorf("retry preview still proposes publication (%q):\n%s", unwanted, out)
		}
	}
	assertRealmShareCLIFile(t, root, shareSelected, shareContents, false)
	assertRealmShareCLIFile(t, root, sharePrivate, "# Local only\n", false)
	if got := runGit(t, remote, "rev-parse", ref); got != published {
		t.Errorf("retry preview changed the published revision: %q -> %q", published, got)
	}
}

func TestRealmCLIShareChecklistCandidates(t *testing.T) {
	root, home, remote := newRealmShareCLIFixture(t)
	t.Setenv("HOME", home)
	t.Cleanup(config.SetHomeDirForTest(home))

	args := []string{shareSelected, "--remote", remote}
	review := previewRealmReview(t, home, "share", args...)
	out, diag, code := runRealmShareCLI(t, home, append(args, "--yes", "--review", review)...)
	if code != 0 {
		t.Fatalf("publish checklist fixture: exit %d:\nstdout: %s\nstderr: %s", code, out, diag)
	}
	assertRealmShareCLIFile(t, root, shareSelected, shareContents, true)

	for _, rel := range []string{
		"overview.md",
		".env.local",
		"reports/draft.md",
		"wiki-only/draft.md",
		".agents/realm.md",
		".agents/skills/review/SKILL.md",
		".agents/skills/incomplete/README.md",
		".agents/commands/review.md",
		".agents/agents/reviewer.md",
		".atomic/private.md",
		".claude/settings.local.json",
		"AGENTS.md",
		"AGENTS.local.md",
		"CLAUDE.md",
	} {
		writeSteeringTestFile(t, filepath.Join(root, rel), "local fixture\n")
	}
	writeSteeringTestFile(t, filepath.Join(root, "wiki", "index.md"),
		"<wiki-buckets>\n<bucket name=\"wiki-only\" path=\"wiki-only\"/>\n</wiki-buckets>\n")
	mustRunWikiBucketCLI(t, home, root, "add", "reports")
	member := filepath.Join(root, "repos", "member")
	writeSteeringTestFile(t, filepath.Join(member, "notes.md"), "# Member notes\n")
	runGit(t, member, "init")

	candidates, err := realmShareCandidates(root)
	if err != nil {
		t.Fatalf("list share checklist candidates: %v", err)
	}
	labels := make(map[string]string, len(candidates))
	for _, candidate := range candidates {
		if _, exists := labels[candidate.Path]; exists {
			t.Errorf("duplicate checklist candidate %q", candidate.Path)
		}
		labels[candidate.Path] = candidate.Label
	}
	for _, want := range []struct {
		path   string
		shared bool
	}{
		{"reports", false},
		{"wiki-only", false},
		{"overview.md", false},
		{".agents/realm.md", false},
		{".agents/skills/review", false},
		{".agents/commands/review.md", false},
		{".agents/agents/reviewer.md", false},
		{shareSelected, true},
	} {
		label, ok := labels[want.path]
		if !ok || !strings.Contains(label, want.path) {
			t.Errorf("checklist does not display %q: %v", want.path, labels)
			continue
		}
		if shared := strings.Contains(strings.ToLower(label), "already shared"); shared != want.shared {
			t.Errorf("checklist label %q marks already shared = %t, want %t", label, shared, want.shared)
		}
		if want.path == "reports" && !strings.Contains(label, "bucket") {
			t.Errorf("checklist label %q does not identify the registered bucket", label)
		}
		if want.path == "wiki-only" && label != "wiki-only (directory)" {
			t.Errorf("wiki-only tag changed the ordinary directory label: %q", label)
		}
	}
	preview := mustRunRealmCLI(t, home, "share", "reports/", "wiki-only/", "--dry-run")
	var registrations []string
	for line := range strings.SplitSeq(preview, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Register bucket ") {
			registrations = append(registrations, line)
		}
	}
	if want := []string{`Register bucket "reports" at "reports"`}; !slices.Equal(registrations, want) {
		t.Errorf("share preview registrations = %q, want %q:\n%s", registrations, want, preview)
	}
	if !strings.Contains(preview, `"wiki-only" (directory):`) {
		t.Errorf("share preview does not select wiki-only as ordinary content:\n%s", preview)
	}
	for path, label := range labels {
		switch {
		case path == ".agents", path == ".agents/skills", path == ".agents/commands", path == ".agents/agents",
			path == ".agents/skills/review/SKILL.md", strings.HasPrefix(path, ".agents/skills/incomplete"),
			path == ".env.local", path == "notes",
			path == "repos", path == "repos/member", strings.HasPrefix(path, "repos/member/"),
			path == ".atomic", strings.HasPrefix(path, ".atomic/"),
			path == ".claude", strings.HasPrefix(path, ".claude/"),
			path == "AGENTS.md", path == "AGENTS.local.md", path == "CLAUDE.md":
			t.Errorf("checklist offers a private path or non-selectable artifact path: %q (%q)", path, label)
		}
	}
}

const (
	sharedRealmInstructions = ".agents/realm.md"
	teamRealmGuidance       = "# Reviewed team guidance\n"
	privateRealmGuidance    = "# Personal joiner guidance\n"
)

type realmLinkCLIFixture struct {
	publisher, publisherHome string
	joiner, joinerHome       string
	remote                   string
}

func newRealmLinkCLIFixture(t *testing.T) realmLinkCLIFixture {
	t.Helper()
	publisher, publisherHome, remote := newRealmShareCLIFixture(t)
	writeSteeringTestFile(t, filepath.Join(publisher, sharedRealmInstructions), teamRealmGuidance)
	args := []string{shareSelected, sharedRealmInstructions, "--remote", remote, "--branch", shareBranch}
	review := previewRealmReview(t, publisherHome, "share", args...)
	mustRunRealmCLI(t, publisherHome, "share", append(args, "--yes", "--review", review)...)
	runGit(t, remote, "symbolic-ref", "HEAD", "refs/heads/"+shareBranch)

	joiner, joinerHome := t.TempDir(), filepath.Join(t.TempDir(), "joiner's home")
	if err := os.Mkdir(joinerHome, 0o700); err != nil {
		t.Fatal(err)
	}
	writeSteeringTestFile(t, config.RepoConfigPath(joiner), "scope = \"realm\"\n")
	writeSteeringTestFile(t, filepath.Join(joiner, "AGENTS.local.md"), privateRealmGuidance)
	t.Chdir(joiner)
	preview := mustRunRealmCLI(t, joinerHome, "join", remote)
	for _, path := range []string{shareSelected, sharedRealmInstructions} {
		if !strings.Contains(preview, path) {
			t.Errorf("join preview lacks %q:\n%s", path, preview)
		}
		assertRealmCLIPathAbsent(t, joiner, path)
	}
	assertRealmShareCLIFile(t, joiner, "AGENTS.local.md", privateRealmGuidance, false)
	return realmLinkCLIFixture{publisher, publisherHome, joiner, joinerHome, remote}
}

func mustRunRealmCLI(t *testing.T, home, verb string, args ...string) string {
	t.Helper()
	out, diag, code := runRealmCLI(t, home, verb, args...)
	if code != 0 {
		t.Fatalf("realm %s %v: exit %d\nstdout: %s\nstderr: %s", verb, args, code, out, diag)
	}
	return out
}

func assertRealmCLIPathAbsent(t *testing.T, root, rel string) {
	t.Helper()
	if _, err := os.Lstat(filepath.Join(root, rel)); !os.IsNotExist(err) {
		t.Errorf("expected %q to be absent in %s, lstat error: %v", rel, root, err)
	}
}

func realmCLIManagedClone(t *testing.T, home string) string {
	t.Helper()
	root := filepath.Join(config.Dir(home), "realms")
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 1 || !entries[0].IsDir() {
		t.Fatalf("expected one managed clone in %s: %v (%v)", root, entries, err)
	}
	return filepath.Join(root, entries[0].Name())
}

func quotedRealmCLIShellPath(path string) string {
	return "'" + strings.ReplaceAll(path, "'", `'\''`) + "'"
}

type realmCLITreeEntry struct {
	mode     os.FileMode
	modified int64
	data     string
}

func snapshotRealmCLITree(t *testing.T, root string) map[string]realmCLITreeEntry {
	t.Helper()
	tree := make(map[string]realmCLITreeEntry)
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		value := realmCLITreeEntry{mode: info.Mode(), modified: info.ModTime().UnixNano()}
		switch {
		case info.Mode()&os.ModeSymlink != 0:
			value.data, err = os.Readlink(path)
		case info.Mode().IsRegular():
			var contents []byte
			contents, err = os.ReadFile(path)
			value.data = string(contents)
		}
		tree[path] = value
		return err
	})
	if err != nil {
		t.Fatalf("snapshot %s: %v", root, err)
	}
	return tree
}

func unchangedRealmCLITrees(t *testing.T, roots ...string) func(*testing.T) {
	t.Helper()
	return compareRealmCLITrees(t, "", roots...)
}

func unchangedRealmCLISyncWorkspace(t *testing.T, root string) func(*testing.T) {
	t.Helper()
	receipt := filepath.Join(filepath.Dir(config.RepoConfigPath(root)), "realm-share.toml")
	return compareRealmCLITrees(t, receipt, root)
}

func compareRealmCLITrees(t *testing.T, syncReceipt string, roots ...string) func(*testing.T) {
	t.Helper()
	before := make(map[string]realmCLITreeEntry)
	for _, root := range roots {
		for path, entry := range snapshotRealmCLITree(t, root) {
			before[path] = entry
		}
	}
	return func(t *testing.T) {
		t.Helper()
		var count int
		for _, root := range roots {
			for path, entry := range snapshotRealmCLITree(t, root) {
				count++
				old, ok := before[path]
				if syncReceipt != "" {
					switch path {
					case syncReceipt:
						entry.data, entry.modified = old.data, old.modified
					case filepath.Dir(syncReceipt):
						entry.modified = old.modified
					}
				}
				if !ok || old != entry {
					t.Errorf("path was created or changed: %s", path)
				}
			}
		}
		if count != len(before) {
			t.Errorf("tree entries: got %d, want %d", count, len(before))
		}
	}
}

func TestRealmCLILinkRequiresExplicitApproval(t *testing.T) {
	f := newRealmLinkCLIFixture(t)
	unchanged := unchangedRealmCLITrees(t, f.joiner, f.joinerHome, f.remote)
	preview := mustRunRealmCLI(t, f.joinerHome, "link", sharedRealmInstructions, "--dry-run")
	if !strings.Contains(preview, sharedRealmInstructions) || !strings.Contains(strings.ToLower(preview), "symlink") {
		t.Errorf("link preview does not describe the instruction symlink:\n%s", preview)
	}
	if strings.Contains(preview, shareSelected) {
		t.Errorf("link preview includes unselected %q:\n%s", shareSelected, preview)
	}
	review := realmReviewDigest(t, preview)
	if again := previewRealmReview(t, f.joinerHome, "link", sharedRealmInstructions); again != review {
		t.Errorf("unchanged link previews returned different digests: %s and %s", review, again)
	}
	unchanged(t)

	for _, tt := range []struct {
		name string
		args []string
		want []string
	}{
		{"missing approval", []string{sharedRealmInstructions}, []string{"--yes"}},
		{"missing review", []string{sharedRealmInstructions, "--yes"}, []string{"--review"}},
		{"missing selection", []string{"--yes", "--review", review}, []string{"path"}},
		{"missing dry-run selection", []string{"--dry-run"}, []string{"path"}},
		{"conflicting flags", []string{sharedRealmInstructions, "--dry-run", "--yes"}, []string{"--dry-run", "--yes"}},
		{"review without yes", []string{sharedRealmInstructions, "--review", review}, []string{"--review", "--yes"}},
		{"review with dry run", []string{sharedRealmInstructions, "--dry-run", "--review", review}, []string{"--review"}},
		{"empty review with dry run", []string{sharedRealmInstructions, "--dry-run", "--review="}, []string{"--review"}},
		{"review for another selection", []string{shareSelected, "--yes", "--review", review}, []string{"review", "--dry-run"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			out, diag, code := runRealmCLI(t, f.joinerHome, "link", tt.args...)
			if code == 0 {
				t.Errorf("link exited 0 without valid approval:\nstdout: %s\nstderr: %s", out, diag)
			}
			for _, hint := range tt.want {
				if !strings.Contains(strings.ToLower(diag), hint) {
					t.Errorf("link diagnostic lacks %q:\n%s", hint, diag)
				}
			}
			unchanged(t)
		})
	}

	mustRunRealmCLI(t, f.joinerHome, "link", sharedRealmInstructions, "--yes", "--review", review)
	assertRealmShareCLIFile(t, f.joiner, sharedRealmInstructions, teamRealmGuidance, true)
	assertRealmCLIPathAbsent(t, f.joiner, shareSelected)
	assertRealmShareCLIFile(t, f.joiner, "AGENTS.local.md", privateRealmGuidance, false)
}

func TestRealmCLILinkDryRunPreviewsSharedWriteAccess(t *testing.T) {
	const directory = "handbook"
	publisher, publisherHome, remote := newRealmShareCLIFixture(t)
	writeSteeringTestFile(t, filepath.Join(publisher, directory, "index.md"), "# Team handbook\n")
	writeSteeringTestFile(t, filepath.Join(publisher, sharedRealmInstructions), teamRealmGuidance)
	writeSteeringTestFile(t, filepath.Join(publisher, cliArtifactCommand), cliArtifactCommandText)
	selections := []string{directory, shareSelected, sharedRealmInstructions, cliArtifactCommand}
	args := append(slices.Clone(selections), "--remote", remote, "--branch", shareBranch)
	review := previewRealmReview(t, publisherHome, "share", args...)
	mustRunRealmCLI(t, publisherHome, "share", append(args, "--yes", "--review", review)...)
	runGit(t, remote, "symbolic-ref", "HEAD", "refs/heads/"+shareBranch)

	joiner, joinerHome := t.TempDir(), t.TempDir()
	writeSteeringTestFile(t, config.RepoConfigPath(joiner), "scope = \"realm\"\n")
	writeSteeringTestFile(t, filepath.Join(joiner, ".claude", "settings.local.json"),
		`{"permissions":{"additionalDirectories":["/sentinel/claude"]}}`+"\n")
	writeSteeringTestFile(t, filepath.Join(joiner, ".codex", "config.toml"),
		"sandbox_mode = \"workspace-write\"\n[sandbox_workspace_write]\nwritable_roots = [\"/sentinel/codex\"]\n")
	t.Chdir(joiner)
	mustRunRealmCLI(t, joinerHome, "join", remote)
	for _, selection := range selections {
		assertRealmCLIPathAbsent(t, joiner, selection)
	}
	clone, err := filepath.EvalSymlinks(realmCLIManagedClone(t, joinerHome))
	if err != nil {
		t.Fatalf("resolve managed clone: %v", err)
	}
	unchanged := unchangedRealmCLITrees(t, joiner, joinerHome, remote)

	for _, selection := range selections {
		t.Run(selection, func(t *testing.T) {
			out := mustRunRealmCLI(t, joinerHome, "link", selection, "--dry-run")
			realmReviewDigest(t, out)
			for _, want := range []string{
				selection,
				clone,
				"permissions.additionalDirectories",
				"sandbox_workspace_write.writable_roots",
				"workspace-write",
			} {
				if !strings.Contains(out, want) {
					t.Errorf("link preview lacks %q:\n%s", want, out)
				}
			}
			if selection != directory {
				target := filepath.Join(clone, filepath.FromSlash(selection))
				if !strings.Contains(out, target) {
					t.Errorf("link preview does not identify the checked-out file %q:\n%s", target, out)
				}
			}
			writeAccess := regexp.MustCompile(`(?m)^  Shared write access:[^\n]*(?:\n    [^\n]*)*`).FindString(out)
			if !regexp.MustCompile(`(?i)\b(?:new|fresh) session\b`).MatchString(writeAccess) {
				t.Errorf("shared write access does not advise starting a fresh session:\n%s", out)
			}
			unchanged(t)
		})
	}
}

func TestRealmCLILinkRejectsReviewAfterSharedEdit(t *testing.T) {
	for _, tt := range []struct {
		name string
		path string
		edit func(string) string
	}{
		{"definition", "realm.toml", func(contents string) string {
			return contents + "\n[[members]]\nkey = \"additional\"\npath = \"repos/additional\"\n"
		}},
		{"selected instructions", sharedRealmInstructions, func(contents string) string {
			return strings.Replace(contents, "team guidance", "team Guidance", 1)
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := newRealmLinkCLIFixture(t)
			clone := realmCLIManagedClone(t, f.joinerHome)
			preview := mustRunRealmCLI(t, f.joinerHome, "link", sharedRealmInstructions, "--dry-run")
			review := realmReviewDigest(t, preview)

			path := filepath.Join(clone, tt.path)
			writeSteeringTestFile(t, path, tt.edit(readSteeringTestFile(t, path)))
			unchanged := unchangedRealmCLITrees(t, f.joiner, f.joinerHome, f.remote)
			out, diag, code := runRealmCLI(t, f.joinerHome, "link", sharedRealmInstructions, "--yes", "--review", review)
			failure := strings.ToLower(diag)
			if code == 0 || !strings.Contains(failure, "review") || !strings.Contains(failure, "--dry-run") {
				t.Errorf("link did not reject a stale review: exit %d\nstdout: %s\nstderr: %s", code, out, diag)
			}
			unchanged(t)
			current := mustRunRealmCLI(t, f.joinerHome, "link", sharedRealmInstructions, "--dry-run")
			if digest := realmReviewDigest(t, current); digest == review {
				t.Errorf("edited %s did not change the review digest: %s", tt.path, digest)
			}
		})
	}
}

func TestRealmCLIJoinLinkUnlinkTwoDevelopers(t *testing.T) {
	f := newRealmLinkCLIFixture(t)
	aClone, bClone := realmCLIManagedClone(t, f.publisherHome), realmCLIManagedClone(t, f.joinerHome)
	ref := "refs/heads/" + shareBranch
	published := runGit(t, f.remote, "rev-parse", ref)
	if got := runGit(t, bClone, "rev-parse", "HEAD"); got != published {
		t.Errorf("joined revision = %q, want %q", got, published)
	}
	review := previewRealmReview(t, f.joinerHome, "link", shareSelected)
	mustRunRealmCLI(t, f.joinerHome, "link", shareSelected, "--yes", "--review", review)
	assertRealmShareCLIFile(t, f.joiner, shareSelected, shareContents, true)
	assertRealmShareCLIFile(t, f.publisher, shareSelected, shareContents, true)
	const edited = "# Unpublished joiner edit\n"
	writeSteeringTestFile(t, filepath.Join(f.joiner, shareSelected), edited)
	assertRealmShareCLIFile(t, f.publisher, shareSelected, shareContents, true)

	mustRunRealmCLI(t, f.joinerHome, "unlink")
	assertRealmCLIPathAbsent(t, f.joiner, shareSelected)
	assertRealmShareCLIFile(t, bClone, shareSelected, edited, false)
	assertRealmShareCLIFile(t, f.joiner, "AGENTS.local.md", privateRealmGuidance, false)

	t.Chdir(f.publisher)
	mustRunRealmCLI(t, f.publisherHome, "unlink")
	assertRealmShareCLIFile(t, f.publisher, shareSelected, shareContents, false)
	assertRealmShareCLIFile(t, f.publisher, sharedRealmInstructions, teamRealmGuidance, false)
	assertRealmShareCLIFile(t, f.publisher, sharePrivate, "# Local only\n", false)
	assertRealmShareCLIFile(t, aClone, shareSelected, shareContents, false)
	if got := runGit(t, f.remote, "rev-parse", ref); got != published {
		t.Errorf("link or unlink published a change: %q -> %q", published, got)
	}

	t.Chdir(f.joiner)
	review = previewRealmReview(t, f.joinerHome, "link", shareSelected)
	mustRunRealmCLI(t, f.joinerHome, "link", shareSelected, "--yes", "--review", review)
	assertRealmShareCLIFile(t, f.joiner, shareSelected, edited, true)
}

func TestRealmCLILinkReviewsReceivedPathsWithoutReplacingPrivateContent(t *testing.T) {
	f := newRealmLinkCLIFixture(t)
	aClone, bClone := realmCLIManagedClone(t, f.publisherHome), realmCLIManagedClone(t, f.joinerHome)
	const (
		procedure  = "sops/runbook.md"
		private    = "research/private.md"
		shared     = "research/shared.md"
		runbook    = "# Shared runbook\n"
		privateDoc = "# Publisher's private research\n"
	)
	writeSteeringTestFile(t, filepath.Join(bClone, procedure), runbook)
	writeSteeringTestFile(t, filepath.Join(bClone, shared), "# Shared research\n")
	manifest := filepath.Join(bClone, "realm.toml")
	writeSteeringTestFile(t, manifest, readSteeringTestFile(t, manifest)+`
[[mappings]]
source = "sops"
destination = "sops"
kind = "directory"

[[mappings]]
source = "research"
destination = "research"
kind = "directory"

[[buckets]]
name = "sops"
path = "sops"
`)
	runGit(t, bClone, "add", "--", "realm.toml", "sops", "research")
	runGit(t, bClone, "commit", "-m", "Add shared directories")
	runGit(t, bClone, "push", f.remote, "HEAD:refs/heads/"+shareBranch)
	runGit(t, aClone, "pull", "--ff-only", f.remote, shareBranch)

	t.Chdir(f.publisher)
	both := previewRealmReview(t, f.publisherHome, "link", "sops/", "research/")
	writeSteeringTestFile(t, filepath.Join(f.publisher, private), privateDoc)
	ownedLink := filepath.Join(f.publisher, shareSelected)
	before, err := os.Lstat(ownedLink)
	if err != nil {
		t.Fatal(err)
	}
	assertRealmCLIPathAbsent(t, f.publisher, "sops")
	unchanged := unchangedRealmCLITrees(t, f.publisher, f.publisherHome, f.remote)
	preview := mustRunRealmCLI(t, f.publisherHome, "link", "sops/", "--dry-run")
	if !strings.Contains(preview, "sops") || !strings.Contains(strings.ToLower(preview), "symlink") {
		t.Errorf("link preview does not show the received sops symlink:\n%s", preview)
	}
	unchanged(t)

	out, diag, code := runRealmCLI(t, f.publisherHome, "link", "sops/")
	if code == 0 || !strings.Contains(diag, "--yes") {
		t.Errorf("link accepted a received path without approval: exit %d\nstdout: %s\nstderr: %s", code, out, diag)
	}
	unchanged(t)

	out, diag, code = runRealmCLI(t, f.publisherHome, "link", "sops/", "research/", "--yes", "--review", both)
	if code == 0 || !strings.Contains(diag, "research") {
		t.Errorf("link did not reject the occupied research path: exit %d\nstdout: %s\nstderr: %s", code, out, diag)
	}
	assertRealmCLIPathAbsent(t, f.publisher, "sops")
	assertRealmShareCLIFile(t, f.publisher, private, privateDoc, false)
	assertRealmCLIPathAbsent(t, f.publisher, shared)
	unchanged(t)

	mustRunRealmCLI(t, f.publisherHome, "link", "sops/", "--yes", "--review", realmReviewDigest(t, preview))
	info, err := os.Lstat(filepath.Join(f.publisher, "sops"))
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Errorf("sops is not a symlink: %v (%v)", info, err)
	}
	assertRealmShareCLIFile(t, f.publisher, procedure, runbook, false)
	assertRealmShareCLIFile(t, f.publisher, private, privateDoc, false)
	assertRealmCLIPathAbsent(t, f.publisher, shared)
	if after, err := os.Lstat(ownedLink); err != nil || !os.SameFile(before, after) {
		t.Errorf("existing link was replaced while linking a received path: %v", err)
	}
}

func assertRealmCLISyncSummary(t *testing.T, out string, committed, received, published bool) {
	t.Helper()
	for _, outcome := range []struct {
		text string
		want bool
	}{
		{"Committed local shared changes.", committed},
		{"Received and integrated remote changes.", received},
		{"Published changes to the shared realm remote.", published},
		{"Shared realm is up to date.", !committed && !received && !published},
	} {
		if got := strings.Contains(out, outcome.text); got != outcome.want {
			t.Errorf("sync output contains %q = %t, want %t:\n%s", outcome.text, got, outcome.want, out)
		}
	}
}

func TestRealmCLISyncExchangesChangesWithoutLinkingJoiner(t *testing.T) {
	f := newRealmLinkCLIFixture(t)
	aClone, bClone := realmCLIManagedClone(t, f.publisherHome), realmCLIManagedClone(t, f.joinerHome)
	ref := "refs/heads/" + shareBranch
	base := runGit(t, f.remote, "rev-parse", ref)
	unchanged := unchangedRealmCLISyncWorkspace(t, f.joiner)

	const edited = "# Publisher edit\n"
	t.Chdir(f.publisher)
	writeSteeringTestFile(t, filepath.Join(f.publisher, shareSelected), edited)
	publisherUnchanged := unchangedRealmCLISyncWorkspace(t, f.publisher)
	out := mustRunRealmCLI(t, f.publisherHome, "sync")
	assertRealmCLISyncSummary(t, out, true, false, true)
	publisherUnchanged(t)
	published := runGit(t, f.remote, "rev-parse", ref)
	if published == base || published != runGit(t, aClone, "rev-parse", "HEAD") {
		t.Fatalf("sync did not publish the publisher's commit: base %q, remote %q", base, published)
	}
	if got := runGit(t, f.remote, "show", ref+":"+shareSelected); got != edited {
		t.Errorf("published contents = %q, want %q", got, edited)
	}
	if got := runGit(t, f.remote, "ls-tree", "-r", "--name-only", ref, "--", sharePrivate); got != "" {
		t.Errorf("sync published a private workspace path: %q", got)
	}
	assertRealmShareCLIFile(t, f.publisher, shareSelected, edited, true)
	assertRealmShareCLIFile(t, bClone, shareSelected, shareContents, false)

	t.Chdir(f.joiner)
	out = mustRunRealmCLI(t, f.joinerHome, "sync")
	assertRealmCLISyncSummary(t, out, false, true, false)
	if got := runGit(t, bClone, "rev-parse", "HEAD"); got != published {
		t.Errorf("joiner sync did not fast-forward: got %q, want %q", got, published)
	}
	assertRealmShareCLIFile(t, bClone, shareSelected, edited, false)
	unchanged(t)

	out = mustRunRealmCLI(t, f.joinerHome, "sync")
	assertRealmCLISyncSummary(t, out, false, false, false)
	if got := runGit(t, bClone, "rev-parse", "HEAD"); got != published {
		t.Errorf("no-op sync changed joiner HEAD: got %q, want %q", got, published)
	}
	if got := runGit(t, f.remote, "rev-parse", ref); got != published {
		t.Errorf("receiving or no-op sync changed the remote: got %q, want %q", got, published)
	}
	unchanged(t)
}

func TestRealmCLISyncDoesNotReportSuccessWithUnpublishedWork(t *testing.T) {
	f := newRealmLinkCLIFixture(t)
	clone := realmCLIManagedClone(t, f.joinerHome)
	ref := "refs/heads/" + shareBranch
	base := runGit(t, f.remote, "rev-parse", ref)
	runGit(t, f.remote, "config", "receive.hideRefs", ref)
	unchanged := unchangedRealmCLITrees(t, f.joiner)
	const edited = "# Edit retained after push refusal\n"
	writeSteeringTestFile(t, filepath.Join(clone, shareSelected), edited)

	out, diag, code := runRealmCLI(t, f.joinerHome, "sync")
	if code == 0 || !strings.Contains(strings.ToLower(diag), "hidden ref") {
		t.Errorf("sync did not report the remote's refusal: exit %d\nstdout: %s\nstderr: %s", code, out, diag)
	}
	if got := runGit(t, f.remote, "rev-parse", ref); got != base {
		t.Errorf("refused push changed the remote: got %q, want %q", got, base)
	}
	retained := runGit(t, clone, "rev-parse", "HEAD")
	if retained == base || runGit(t, clone, "show", "HEAD:"+shareSelected) != edited {
		t.Fatalf("sync did not retain the unpublished edit in a local commit: HEAD %q, base %q", retained, base)
	}
	assertRealmShareCLIFile(t, clone, shareSelected, edited, false)
	unchanged(t)

	runGit(t, f.remote, "config", "--unset", "receive.hideRefs")
	unchanged = unchangedRealmCLISyncWorkspace(t, f.joiner)
	assertRealmCLISyncSummary(t, mustRunRealmCLI(t, f.joinerHome, "sync"), false, false, true)
	if got := runGit(t, f.remote, "rev-parse", ref); got != retained {
		t.Errorf("retry published %q, want retained commit %q", got, retained)
	}
	unchanged(t)
}

func TestRealmCLISyncGuidesConflictRecovery(t *testing.T) {
	f := newRealmLinkCLIFixture(t)
	aClone := realmCLIManagedClone(t, f.publisherHome)
	bClone, err := filepath.EvalSymlinks(realmCLIManagedClone(t, f.joinerHome))
	if err != nil {
		t.Fatal(err)
	}
	joiner, err := filepath.EvalSymlinks(f.joiner)
	if err != nil {
		t.Fatal(err)
	}
	ref := "refs/heads/" + shareBranch
	writeSteeringTestFile(t, filepath.Join(aClone, shareSelected), "# Publisher version\n")
	runGit(t, aClone, "add", "--", shareSelected)
	runGit(t, aClone, "commit", "-m", "Edit shared notes")
	runGit(t, aClone, "push", f.remote, "HEAD:"+ref)
	published := runGit(t, f.remote, "rev-parse", ref)
	writeSteeringTestFile(t, filepath.Join(bClone, shareSelected), "# Joiner version\n")
	unchanged := unchangedRealmCLISyncWorkspace(t, f.joiner)

	out, diag, code := runRealmCLI(t, f.joinerHome, "sync")
	if code == 0 || !strings.Contains(strings.ToLower(diag), "conflict") {
		t.Fatalf("sync did not report the merge conflict: exit %d\nstdout: %s\nstderr: %s", code, out, diag)
	}
	localPath := filepath.Join(joiner, shareSelected)
	for _, hint := range []string{
		"git -C " + quotedRealmCLIShellPath(bClone) + " add -- ",
		"git -C " + quotedRealmCLIShellPath(bClone) + " commit",
		localPath,
	} {
		if !strings.Contains(diag, hint) {
			t.Errorf("conflict recovery diagnostic lacks %q:\n%s", hint, diag)
		}
	}
	relativeDiagnostic := strings.NewReplacer(localPath, "", filepath.Join(bClone, shareSelected), "").Replace(diag)
	if !strings.Contains(relativeDiagnostic, shareSelected) {
		t.Errorf("conflict recovery diagnostic lacks clone-relative path %q:\n%s", shareSelected, diag)
	}
	if got := runGit(t, bClone, "rev-parse", "MERGE_HEAD"); got != published {
		t.Errorf("conflict MERGE_HEAD = %q, want %q", got, published)
	}
	if got := runGit(t, bClone, "ls-files", "--unmerged", "--", shareSelected); got == "" {
		t.Errorf("conflicted path %q is not unmerged in the index", shareSelected)
	}
	if got := runGit(t, f.remote, "rev-parse", ref); got != published {
		t.Errorf("conflicting sync changed the remote: got %q, want %q", got, published)
	}
	unchanged(t)

	const resolved = "# Resolved shared notes\n"
	writeSteeringTestFile(t, filepath.Join(bClone, shareSelected), resolved)
	runGit(t, joiner, "-C", bClone, "add", "--", shareSelected)
	runGit(t, joiner, "-C", bClone, "commit", "-m", "Resolve shared notes conflict")
	merge := runGit(t, bClone, "rev-parse", "HEAD")
	assertRealmCLISyncSummary(t, mustRunRealmCLI(t, f.joinerHome, "sync"), false, false, true)
	if got := runGit(t, f.remote, "rev-parse", ref); got != merge {
		t.Errorf("sync after manual resolution published %q, want merge commit %q", got, merge)
	}
	if got := runGit(t, f.remote, "show", ref+":"+shareSelected); got != resolved {
		t.Errorf("published resolution = %q, want %q", got, resolved)
	}
	unchanged(t)
}

func TestRealmCLISyncRejectsArgumentsWithoutMutation(t *testing.T) {
	f := newRealmLinkCLIFixture(t)
	clone := realmCLIManagedClone(t, f.joinerHome)
	writeSteeringTestFile(t, filepath.Join(clone, shareSelected), "# Must remain unpublished\n")
	unchanged := unchangedRealmCLITrees(t, f.joiner, f.joinerHome, f.remote)

	for _, arg := range []string{shareSelected, "--dry-run"} {
		t.Run(arg, func(t *testing.T) {
			out, diag, code := runRealmCLI(t, f.joinerHome, "sync", arg)
			if code == 0 || !strings.Contains(diag, arg) {
				t.Errorf("sync did not reject %q: exit %d\nstdout: %s\nstderr: %s", arg, code, out, diag)
			}
			unchanged(t)
		})
	}
}
