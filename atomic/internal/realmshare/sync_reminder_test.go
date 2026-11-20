package realmshare

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestClaimSyncReminderOncePerLocalDay(t *testing.T) {
	f, _ := newLinkFixture(t)
	clone := f.binding(t).ClonePath
	writeRealmshareFile(t, filepath.Join(clone, "shared/guide.md"), "# Pending guide\n")

	loc := time.FixedZone("UTC-08", -8*60*60)
	first := time.Date(2026, 11, 20, 15, 0, 0, 0, loc)
	const startups = 4
	claims := make(chan bool, startups)
	for range startups {
		go func() { claims <- ClaimSyncReminder(t.Context(), f.realm, first) }()
	}
	count := 0
	for range startups {
		if <-claims {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("%d of %d concurrent startups claimed the reminder, want one", count, startups)
	}
	if ClaimSyncReminder(t.Context(), f.realm, first.Add(2*time.Hour)) {
		t.Error("UTC midnight reset the reminder before local midnight")
	}
	if !ClaimSyncReminder(t.Context(), f.realm, first.Add(10*time.Hour)) {
		t.Error("local midnight did not reset the reminder")
	}
}

func TestClaimSyncReminderRejectsUncommittedMapping(t *testing.T) {
	f, _ := newLinkFixture(t)
	clone := f.binding(t).ClonePath
	manifest := filepath.Join(clone, manifestName)
	committed := readRealmshareFile(t, manifest)
	writeRealmshareFile(t, manifest, committed+manifestMapping("shared/draft.md", "draft.md", string(KindFile)))
	writeRealmshareFile(t, filepath.Join(clone, "shared/draft.md"), "# Unapproved mapping\n")

	now := time.Now()
	if ClaimSyncReminder(t.Context(), f.realm, now) {
		t.Error("a live mapping not present in HEAD claimed the reminder")
	}
	writeRealmshareFile(t, manifest, committed)
	writeRealmshareFile(t, filepath.Join(clone, "shared/guide.md"), "# Approved mapping\n")
	if !ClaimSyncReminder(t.Context(), f.realm, now) {
		t.Error("the approved edit could not claim the reminder on the same day")
	}
}

func TestClaimSyncReminderRejectsDroppedMapping(t *testing.T) {
	f, _ := newLinkFixture(t)
	clone := f.binding(t).ClonePath
	manifest := filepath.Join(clone, manifestName)
	committed := readRealmshareFile(t, manifest)
	dropped := manifestMapping("shared/guide.md", "guide.md", string(KindFile))
	writeRealmshareFile(t, manifest, strings.Replace(committed, dropped, "", 1))
	writeRealmshareFile(t, filepath.Join(clone, "shared/notes/index.md"), "# Pending notes\n")

	now := time.Now()
	if ClaimSyncReminder(t.Context(), f.realm, now) {
		t.Error("a dropped committed mapping claimed the reminder")
	}
	writeRealmshareFile(t, manifest, committed)
	if !ClaimSyncReminder(t.Context(), f.realm, now) {
		t.Error("the remaining edit could not claim the reminder after restoring the mapping")
	}
}

func TestClaimSyncReminderRejectsDirectoryWithOnlyIgnoredFiles(t *testing.T) {
	f, _ := newLinkFixture(t)
	clone := f.binding(t).ClonePath
	const replacement = "shared/notes/replacement.md"
	writeRealmshareFile(t, filepath.Join(clone, ".git/info/exclude"), "/"+replacement+"\n")
	writeRealmshareFile(t, filepath.Join(clone, replacement), "# Replacement notes\n")
	if err := os.Remove(filepath.Join(clone, "shared/notes/index.md")); err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	if ClaimSyncReminder(t.Context(), f.realm, now) {
		t.Error("withdrawing the directory's last Git-visible file claimed the reminder")
	}
	f.git(t, clone, "add", "-f", "--", replacement)
	if !ClaimSyncReminder(t.Context(), f.realm, now) {
		t.Error("the staged replacement could not claim the reminder on the same day")
	}
}

func TestClaimSyncReminderSkipsActiveFilter(t *testing.T) {
	for _, driver := range []string{"clean", "process"} {
		t.Run(driver, func(t *testing.T) {
			f, _ := newLinkFixture(t)
			clone := f.binding(t).ClonePath
			sentinel := filepath.Join(t.TempDir(), "filter-ran")
			t.Setenv("ATOMIC_REMINDER_FILTER_SENTINEL", sentinel)
			f.git(t, clone, "config", "filter.reminder."+driver, `echo ran > "$ATOMIC_REMINDER_FILTER_SENTINEL"; exit 1`)
			writeRealmshareFile(t, filepath.Join(clone, ".git/info/attributes"), "/shared/guide.md filter=reminder\n")
			writeRealmshareFile(t, filepath.Join(clone, "shared/guide.md"), "# Filtered edit\n")

			now := time.Now()
			if ClaimSyncReminder(t.Context(), f.realm, now) {
				t.Error("the filtered edit claimed the reminder")
			}
			writeRealmshareFile(t, filepath.Join(clone, "shared/notes/index.md"), "# Unfiltered edit\n")
			if !ClaimSyncReminder(t.Context(), f.realm, now) {
				t.Error("the unfiltered edit could not claim the reminder on the same day")
			}
			if _, err := os.Stat(sentinel); !os.IsNotExist(err) {
				t.Errorf("filter sentinel should be absent: %v", err)
			}
		})
	}
}

func TestClaimSyncReminderRejectsMissingMappedFile(t *testing.T) {
	f, _ := newLinkFixture(t)
	path := filepath.Join(f.binding(t).ClonePath, "shared/guide.md")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	if ClaimSyncReminder(t.Context(), f.realm, now) {
		t.Error("a missing mapped file claimed the reminder")
	}
	writeRealmshareFile(t, path, "# Restored guide with pending edits\n")
	if !ClaimSyncReminder(t.Context(), f.realm, now) {
		t.Error("the restored file could not claim the reminder on the same day")
	}
}

func TestClaimSyncReminderUsesWorktreeAfterRestaging(t *testing.T) {
	f, _ := newLinkFixture(t)
	clone := f.binding(t).ClonePath
	const source = "shared/guide.md"
	path := filepath.Join(clone, source)
	committed := readRealmshareFile(t, path)
	writeRealmshareFile(t, path, "# Staged edit\n")
	f.git(t, clone, "add", "--", source)
	writeRealmshareFile(t, path, committed)

	trace := filepath.Join(t.TempDir(), "packets.log")
	t.Setenv("GIT_TRACE_PACKET", trace)
	now := time.Now()
	before := linkTreeDigest(t, clone)
	if ClaimSyncReminder(t.Context(), f.realm, now) {
		t.Error("a staged edit canceled by the worktree claimed the reminder")
	}
	if after := linkTreeDigest(t, clone); after != before {
		t.Error("the net-clean probe modified the managed checkout")
	}

	writeRealmshareFile(t, path, "# Pending worktree edit\n")
	before = linkTreeDigest(t, clone)
	if !ClaimSyncReminder(t.Context(), f.realm, now) {
		t.Error("the net worktree edit could not claim the reminder on the same day")
	}
	if after := linkTreeDigest(t, clone); after != before {
		t.Error("the net-dirty probe modified the managed checkout")
	}
	if packets, err := os.ReadFile(trace); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	} else if len(packets) != 0 {
		t.Errorf("reminder used Git transport:\n%s", packets)
	}
}

func TestClaimSyncReminderIgnoresStagedDeletionRestoredToHEAD(t *testing.T) {
	f, _ := newLinkFixture(t)
	clone := f.binding(t).ClonePath
	const source = "shared/guide.md"
	path := filepath.Join(clone, source)
	committed := readRealmshareFile(t, path)
	f.git(t, clone, "rm", "--", source)
	writeRealmshareFile(t, path, committed)

	now := time.Now()
	if ClaimSyncReminder(t.Context(), f.realm, now) {
		t.Error("a staged deletion canceled by the worktree claimed the reminder")
	}
	writeRealmshareFile(t, path, "# Pending edit after restoration\n")
	if !ClaimSyncReminder(t.Context(), f.realm, now) {
		t.Error("the restored edit could not claim the reminder on the same day")
	}
}

func TestClaimSyncReminderIncludesIgnoredStagedAddition(t *testing.T) {
	f, _ := newLinkFixture(t)
	clone := f.binding(t).ClonePath
	const source = "shared/notes/draft.md"
	writeRealmshareFile(t, filepath.Join(clone, ".git/info/exclude"), "/"+source+"\n")
	writeRealmshareFile(t, filepath.Join(clone, source), "# Pending draft\n")

	now := time.Now()
	if ClaimSyncReminder(t.Context(), f.realm, now) {
		t.Error("an ignored untracked addition claimed the reminder")
	}
	f.git(t, clone, "add", "-f", "--", source)
	if !ClaimSyncReminder(t.Context(), f.realm, now) {
		t.Error("the ignored staged addition could not claim the reminder on the same day")
	}
}

func TestClaimSyncReminderIncludesIgnoredStagedDeletion(t *testing.T) {
	f, _ := newLinkFixture(t)
	clone := f.binding(t).ClonePath
	const source = "shared/notes/draft.md"
	path := filepath.Join(clone, source)
	const original = "# Committed draft\n"
	writeRealmshareFile(t, path, original)
	syncFixtureCommit(t, f, clone, "Add draft to selected directory", source)
	f.git(t, clone, "rm", "--", source)
	writeRealmshareFile(t, path, original)

	now := time.Now()
	if ClaimSyncReminder(t.Context(), f.realm, now) {
		t.Error("a staged deletion restored from HEAD claimed the reminder")
	}
	writeRealmshareFile(t, filepath.Join(clone, ".git/info/exclude"), "/"+source+"\n")
	if !ClaimSyncReminder(t.Context(), f.realm, now) {
		t.Error("the ignored restoration did not leave a publishable deletion")
	}
}

func TestClaimSyncReminderRejectsNestedBareRepository(t *testing.T) {
	f, _ := newLinkFixture(t)
	clone := f.binding(t).ClonePath
	nested := filepath.Join(clone, "shared/notes/nested")
	f.git(t, clone, "init", "--quiet", "--bare", nested)
	writeRealmshareFile(t, filepath.Join(clone, "shared/guide.md"), "# Pending edit\n")

	now := time.Now()
	if ClaimSyncReminder(t.Context(), f.realm, now) {
		t.Error("a nested bare repository did not block the reminder")
	}
	if err := os.RemoveAll(nested); err != nil {
		t.Fatal(err)
	}
	if !ClaimSyncReminder(t.Context(), f.realm, now) {
		t.Error("the pending edit could not claim the reminder after removing the repository")
	}
}

func TestClaimSyncReminderRejectsLinkedSkillConversion(t *testing.T) {
	const unit = ".agents/skills/outline"
	f, _ := newLinkedSyncSkillFixture(t, unit, "---\nname: outline\ndescription: Outline meeting notes.\n---\nOutline the meeting notes.\n")
	clone := f.binding(t).ClonePath
	attributes := filepath.Join(clone, unit, ".gitattributes")
	writeRealmshareFile(t, attributes, "SKILL.md text eol=crlf\n")
	writeRealmshareFile(t, filepath.Join(clone, "shared/guide.md"), "# Pending edit\n")

	now := time.Now()
	if ClaimSyncReminder(t.Context(), f.realm, now) {
		t.Error("linked skill conversion attributes did not block the reminder")
	}
	if err := os.Remove(attributes); err != nil {
		t.Fatal(err)
	}
	if !ClaimSyncReminder(t.Context(), f.realm, now) {
		t.Error("the pending edit could not claim the reminder after removing the attributes")
	}
}
