package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestRealmCLILinkClaudePreservesDarwinAccessMetadata(t *testing.T) {
	bin, claude := realmInstructionTestPATH(t)
	if claude == "" {
		t.Skip("installed Claude executable not found on the original PATH")
	}
	for _, protected := range []string{realmClaudeLoader, realmClaudeLocalSettings} {
		t.Run(protected, func(t *testing.T) {
			f := newRealmLinkCLIFixture(t)
			seedRealmInstructionClaude(t, f)
			writeSteeringTestFile(t, filepath.Join(f.joiner, realmPrivateInstructions), privateRealmGuidance)
			review := previewRealmReview(t, f.joinerHome, "link", sharedRealmInstructions)
			mustRunRealmCLI(t, f.joinerHome, "link", sharedRealmInstructions, "--yes", "--review", review)
			unchangedLink := unchangedRealmLinkWitness(t, f, sharedRealmInstructions)

			path := filepath.Join(f.joiner, protected)
			if out, err := exec.Command("/bin/chmod", "+a", "everyone allow read", path).CombinedOutput(); err != nil {
				t.Fatalf("add fixture ACL to %s: %v\n%s", path, err, out)
			}
			before := readRealmDarwinAccess(t, path)
			if !strings.Contains(before.acl, "everyone allow read") {
				t.Fatalf("missing authored ACL on %s: %q", path, before.acl)
			}
			linkRealmInstructionTool(t, bin, "claude", claude)
			t.Cleanup(func() {
				if err := os.Remove(filepath.Join(bin, "claude")); err != nil {
					t.Error(err)
				}
			})
			review = previewRealmReview(t, f.joinerHome, "link", sharedRealmInstructions)
			out, diag, code := runRealmCLI(t, f.joinerHome, "link", sharedRealmInstructions, "--yes", "--review", review)
			unchangedLink(t)
			if after := readRealmDarwinAccess(t, path); after != before {
				t.Errorf("reviewed link changed access metadata on %s:\nbefore: %+v\nafter:  %+v", protected, before, after)
			}

			loader := readSteeringTestFile(t, filepath.Join(f.joiner, realmClaudeLoader))
			settings := readSteeringTestFile(t, filepath.Join(f.joiner, realmClaudeLocalSettings))
			if code == 0 && (loader != realmAuthoredLoader || settings != realmAuthoredSettings) {
				added, ok := strings.CutPrefix(loader, realmAuthoredLoader)
				if !ok || strings.Count(added, "@"+realmPrivateInstructions) != 1 {
					t.Errorf("activation did not preserve the authored loader and add one private import:\n%s", loader)
				}
				assertRealmInstructionSettings(t, f.joiner, 1)
				return
			}

			if loader != realmAuthoredLoader || settings != realmAuthoredSettings {
				t.Error("refused activation changed authored Claude files")
			}
			report := strings.ToLower(out + "\n" + diag)
			refusal := false
			for _, line := range strings.Split(report, "\n") {
				if strings.Contains(line, strings.ToLower(protected)) &&
					(strings.Contains(line, "acl") || strings.Contains(line, "access metadata")) &&
					(code != 0 || strings.Contains(line, "unchanged") || strings.Contains(line, "refus")) {
					refusal = true
				}
			}
			if !refusal || strings.Contains(report, "claude realm startup already configured") ||
				strings.Contains(report, "claude realm startup activated") {
				t.Errorf("want explicit Claude activation refusal without a success claim: exit %d\nstdout: %s\nstderr: %s", code, out, diag)
			}
		})
	}
}

type realmDarwinAccess struct {
	mode     os.FileMode
	uid, gid uint32
	acl      string
}

func readRealmDarwinAccess(t *testing.T, path string) realmDarwinAccess {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !info.Mode().IsRegular() {
		t.Fatalf("%s is not a regular file: %v", path, info.Mode())
	}
	out, err := exec.Command("/bin/ls", "-le", path).CombinedOutput()
	if err != nil {
		t.Fatalf("read fixture ACL on %s: %v\n%s", path, err, out)
	}
	_, acl, _ := strings.Cut(string(out), "\n")
	stat := info.Sys().(*syscall.Stat_t)
	return realmDarwinAccess{mode: info.Mode(), uid: stat.Uid, gid: stat.Gid, acl: acl}
}
