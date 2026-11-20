package realmshare

import (
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/cgi"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/config"
)

const joinFixtureBranch = "team/reviewed"

func TestJoinUsesAdvertisedRefAndPreviewsCollisions(t *testing.T) {
	publisher := newJoinPublisher(t, portableManifest)
	publisher.git(t, publisher.realm, "commit", "--quiet", "--allow-empty", "-m", "Unshared main")
	publisher.git(t, publisher.realm, "push", "--quiet", publisher.remote, "HEAD:refs/heads/main")
	revision := publishJoinFixture(t, publisher)
	refs := publisher.git(t, publisher.remote, "show-ref")

	receiver := newJoinReceiver(t, publisher.remote)
	receiver.write(t, "wiki/index.md", "# Private wiki\n")
	receiver.write(t, "guide.md", "# Private guide\n")
	result, err := Join(t.Context(), receiver.realm, receiver.remote)
	if err != nil {
		t.Fatal(err)
	}
	binding := receiver.binding(t)
	if !reflect.DeepEqual(result.Binding, binding) {
		t.Errorf("preview binding = %+v, persisted binding = %+v", result.Binding, binding)
	}
	if binding.RealmID != fixtureRealmID || binding.Remote != publisher.remote || binding.Branch != joinFixtureBranch {
		t.Errorf("unexpected joined binding: %+v", binding)
	}
	actualParent, err := filepath.EvalSymlinks(filepath.Dir(binding.ClonePath))
	if err != nil {
		t.Fatal(err)
	}
	wantParent, err := filepath.EvalSymlinks(filepath.Join(config.Dir(receiver.home), "realms"))
	if err != nil {
		t.Fatal(err)
	}
	if actualParent != wantParent {
		t.Errorf("clone parent = %q, want %q", actualParent, wantParent)
	}
	for _, check := range []struct {
		args []string
		want string
	}{
		{[]string{"remote", "get-url", "origin"}, receiver.remote},
		{[]string{"symbolic-ref", "HEAD"}, "refs/heads/" + joinFixtureBranch},
		{[]string{"rev-parse", "HEAD"}, revision},
	} {
		if got := receiver.git(t, binding.ClonePath, check.args...); got != check.want {
			t.Errorf("joined git %v = %q, want %q", check.args, got, check.want)
		}
	}
	if got := publisher.git(t, publisher.remote, "show-ref"); got != refs {
		t.Errorf("join changed remote refs: %s", got)
	}

	want := map[string]JoinMapping{
		"wiki":                       {Mapping: Mapping{Source: "shared/wiki", Destination: "wiki", Kind: KindDirectory}, Collision: true},
		"guide.md":                   {Mapping: Mapping{Source: "guide.md", Destination: "guide.md", Kind: KindFile}, Collision: true},
		"research":                   {Mapping: Mapping{Source: "research", Destination: "research", Kind: KindDirectory}},
		".agents/realm.md":           {Mapping: Mapping{Source: ".agents/realm.md", Destination: ".agents/realm.md", Kind: KindInstructions}},
		".agents/skills/review":      {Mapping: Mapping{Source: ".agents/skills/review", Destination: ".agents/skills/review", Kind: KindSkill}},
		".agents/commands/triage.md": {Mapping: Mapping{Source: ".agents/commands/triage.md", Destination: ".agents/commands/triage.md", Kind: KindCommand}},
		".agents/agents/reviewer.md": {Mapping: Mapping{Source: ".agents/agents/reviewer.md", Destination: ".agents/agents/reviewer.md", Kind: KindAgent}},
	}
	for _, preview := range result.Mappings {
		dst := preview.Mapping.Destination
		if expected := want[dst]; preview != expected {
			t.Errorf("preview for %q = %+v, want %+v", dst, preview, expected)
		}
		delete(want, dst)
	}
	if len(want) != 0 {
		t.Errorf("missing mapping previews: %v", want)
	}
	if binding.TrustedDigest != "" || len(binding.Links) != 0 || len(binding.Loaders) != 0 || len(binding.Artifacts) != 0 {
		t.Errorf("join recorded trust or installed ownership: %+v", binding)
	}
	receiver.assertOriginal(t, "wiki", "index.md", "# Private wiki\n")
	receiver.assertOriginal(t, "guide.md", "", "# Private guide\n")
	assertRealmshareAbsent(t, filepath.Join(receiver.realm, "research"))
	assertRealmshareAbsent(t, filepath.Join(receiver.realm, ".agents"))
	assertRealmshareAbsent(t, filepath.Join(receiver.realm, "AGENTS.md"))
	assertRealmshareAbsent(t, filepath.Join(receiver.realm, "CLAUDE.md"))
	assertRealmshareAbsent(t, filepath.Join(receiver.realm, "repos"))
	if got := readRealmshareFile(t, filepath.Join(binding.ClonePath, "shared", "wiki", "index.md")); got != "# Team content\n" {
		t.Errorf("joined wiki content = %q", got)
	}
}

func TestJoinResolvesSameCommitBranchesFromAdvertisedSymref(t *testing.T) {
	publisher := newJoinPublisher(t, portableManifest)
	revision := publishJoinFixture(t, publisher)
	publisher.git(t, publisher.remote, "update-ref", "refs/heads/main", revision)
	publisher.git(t, publisher.remote, "update-ref", "--no-deref", "HEAD", revision)

	t.Run("detached HEAD is ambiguous", func(t *testing.T) {
		receiver := newJoinReceiver(t, publisher.remote)
		if got := receiver.git(t, receiver.home, "ls-remote", "--symref", "--", receiver.remote, "HEAD"); got != revision+"\tHEAD" {
			t.Fatalf("detached remote HEAD advertisement = %q", got)
		}
		if _, err := Join(t.Context(), receiver.realm, receiver.remote); err == nil || !strings.Contains(err.Error(), "multiple branches") {
			t.Fatalf("join with two eligible branches and no advertised symref: %v", err)
		}
		if _, err := LoadBinding(receiver.realm); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("binding lookup after ambiguous join: %v", err)
		}
		assertRealmshareAbsent(t, joinFixtureClonePath(receiver))
	})

	t.Run("advertised symref wins", func(t *testing.T) {
		publisher.git(t, publisher.remote, "symbolic-ref", "HEAD", "refs/heads/"+joinFixtureBranch)
		receiver := newJoinReceiver(t, publisher.remote)
		if _, err := Join(t.Context(), receiver.realm, receiver.remote); err != nil {
			t.Fatal(err)
		}
		if got := receiver.binding(t).Branch; got != joinFixtureBranch {
			t.Errorf("joined branch = %q, want advertised %q", got, joinFixtureBranch)
		}
	})
}

func TestJoinDoesNotBindStaleRemoteHEAD(t *testing.T) {
	publisher := newJoinPublisher(t, portableManifest)
	revision := publishJoinFixture(t, publisher)
	const nextBranch = "next"
	publisher.git(t, publisher.remote, "update-ref", "refs/heads/"+nextBranch, revision)
	refs := publisher.git(t, publisher.remote, "show-ref", "--heads")
	git, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	backend := &cgi.Handler{
		Path: git,
		Args: []string{"http-backend"},
		Env: []string{
			"GIT_PROJECT_ROOT=" + filepath.Dir(publisher.remote),
			"GIT_HTTP_EXPORT_ALL=1",
			"GIT_CONFIG_NOSYSTEM=1",
			"GIT_CONFIG_GLOBAL=" + os.DevNull,
		},
	}
	var advertisements atomic.Int32
	server := newJoinTLSServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// A new transport session sees the new default, without any branch tip moving.
		if r.URL.Path == "/team.git/info/refs" && advertisements.Add(1) == 2 {
			if err := os.WriteFile(filepath.Join(publisher.remote, "HEAD"), []byte("ref: refs/heads/"+nextBranch+"\n"), 0o600); err != nil {
				t.Errorf("changing fixture HEAD: %v", err)
				http.Error(w, "fixture HEAD update failed", http.StatusInternalServerError)
				return
			}
		}
		backend.ServeHTTP(w, r)
	}))
	remote := server.URL + "/team.git"
	receiver := newJoinReceiver(t, remote)

	result, err := Join(t.Context(), receiver.realm, remote)
	count := advertisements.Load()
	if count == 0 {
		t.Fatalf("join never requested the fixture's refs: %v", err)
	}
	if got := publisher.git(t, publisher.remote, "show-ref", "--heads"); got != refs {
		t.Fatalf("branch tips changed during fixture: %s", got)
	}
	wantBranch := joinFixtureBranch
	if count >= 2 {
		wantBranch = nextBranch
	}
	if got := publisher.git(t, publisher.remote, "symbolic-ref", "HEAD"); got != "refs/heads/"+wantBranch {
		t.Fatalf("remote HEAD = %q, want %q", got, "refs/heads/"+wantBranch)
	}
	if err != nil {
		if count == 1 {
			t.Fatalf("join refused a stable advertisement: %v", err)
		}
		if _, err := LoadBinding(receiver.realm); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("binding lookup after refused join: %v", err)
		}
		assertRealmshareAbsent(t, joinFixtureClonePath(receiver))
	} else if binding := receiver.binding(t); binding.Branch != wantBranch || result.Binding.Branch != wantBranch {
		t.Errorf("join reported %q and bound %q, want current advertised branch %q",
			result.Binding.Branch, binding.Branch, wantBranch)
	}
	assertRealmshareAbsent(t, filepath.Join(receiver.realm, "wiki"))
}

func TestJoinHTTPSRefusesServerRedirects(t *testing.T) {
	publisher := newJoinPublisher(t, portableManifest)
	publishJoinFixture(t, publisher)
	otherRepo := filepath.Join(t.TempDir(), "other.git")
	publisher.git(t, publisher.home, "clone", "--quiet", "--bare", publisher.remote, otherRepo)
	publisher.git(t, otherRepo, "update-server-info")

	var requested, redirected atomic.Bool
	mux := http.NewServeMux()
	mux.HandleFunc("/team.git/", func(w http.ResponseWriter, r *http.Request) {
		requested.Store(true)
		http.Redirect(w, r, "/other.git/"+strings.TrimPrefix(r.URL.RequestURI(), "/team.git/"), http.StatusFound)
	})
	files := http.StripPrefix("/other.git/", http.FileServer(http.Dir(otherRepo)))
	mux.HandleFunc("/other.git/", func(w http.ResponseWriter, r *http.Request) {
		redirected.Store(true)
		files.ServeHTTP(w, r)
	})
	server := newJoinTLSServer(t, mux)
	remote := server.URL + "/team.git"

	for _, tc := range []struct {
		name, configKey string
	}{
		{"default", ""},
		{"global redirects enabled", "http.followRedirects"},
		{"URL-scoped redirects enabled", "http." + remote + ".followRedirects"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requested.Store(false)
			redirected.Store(false)
			receiver := newJoinReceiver(t, remote)
			if tc.configKey != "" {
				receiver.git(t, receiver.home, "config", "--global", tc.configKey, "true")
			}

			_, err := Join(t.Context(), receiver.realm, remote)
			if !requested.Load() {
				t.Fatalf("join never requested the approved URL: %v", err)
			}
			if redirected.Load() {
				t.Errorf("join followed the redirect to %s/other.git", server.URL)
			}
			if err == nil {
				t.Error("join accepted a redirected repository")
			}
			if _, err := LoadBinding(receiver.realm); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("binding lookup after redirect: %v", err)
			}
			assertRealmshareAbsent(t, joinFixtureClonePath(receiver))
			assertRealmshareAbsent(t, filepath.Join(receiver.realm, "wiki"))
		})
	}
}

func TestJoinHTTPSDoesNotFollowConfiguredRemoteRedirection(t *testing.T) {
	publisher := newJoinPublisher(t, portableManifest)
	publishJoinFixture(t, publisher)
	publisher.git(t, publisher.remote, "update-server-info")

	files := http.FileServer(http.Dir(publisher.remote))
	var redirected atomic.Bool
	mux := http.NewServeMux()
	mux.Handle("/team.git/", http.StripPrefix("/team.git/", files))
	mux.HandleFunc("/other.git/", func(w http.ResponseWriter, r *http.Request) {
		redirected.Store(true)
		http.StripPrefix("/other.git/", files).ServeHTTP(w, r)
	})
	server := newJoinTLSServer(t, mux)
	remote, other := server.URL+"/team.git", server.URL+"/other.git"

	for _, name := range []string{"literal URL", "global origin", "checkout include"} {
		t.Run(name, func(t *testing.T) {
			redirected.Store(false)
			receiver := newJoinReceiver(t, remote)
			switch name {
			case "global origin":
				receiver.git(t, receiver.home, "config", "--global", "remote.origin.url", other)
			case "checkout include":
				include := filepath.Join(receiver.home, "checkout.gitconfig")
				writeRealmshareFile(t, include, "[url \""+other+"\"]\n\tinsteadOf = "+remote+"\n")
				receiver.git(t, receiver.home, "config", "--global", "includeIf.gitdir:**/.git.path", filepath.ToSlash(include))
			}

			if got := receiver.git(t, receiver.home, "ls-remote", "--get-url", "--", remote); got != remote {
				t.Fatalf("literal URL preflight = %q, want %q", got, remote)
			}
			result, err := Join(t.Context(), receiver.realm, remote)
			if redirected.Load() {
				t.Fatalf("join contacted unapproved URL %q", other)
			}
			if err != nil {
				if name == "literal URL" {
					t.Fatalf("join of loopback HTTPS fixture: %v", err)
				}
				if _, err := LoadBinding(receiver.realm); !errors.Is(err, os.ErrNotExist) {
					t.Errorf("binding lookup after refused redirection: %v", err)
				}
				assertRealmshareAbsent(t, joinFixtureClonePath(receiver))
			} else if result.Binding.Remote != remote || result.Binding.RealmID != fixtureRealmID {
				t.Errorf("joined binding = %+v, want realm %q from %q", result.Binding, fixtureRealmID, remote)
			}
			assertRealmshareAbsent(t, filepath.Join(receiver.realm, "wiki"))
		})
	}
}

func TestJoinRejectsInvalidRemoteDefinition(t *testing.T) {
	for _, tc := range []struct {
		name, definition string
		unsafeLink       bool
	}{
		{"unsupported version", strings.Replace(portableManifest, "version = 1", "version = 999", 1), false},
		{"escaping destination", strings.Replace(portableManifest, `destination = "wiki"`, `destination = "../join-escape"`, 1), false},
		{"private destination", strings.Replace(portableManifest, `destination = "guide.md"`, `destination = "AGENTS.local.md"`, 1), false},
		{"escaping source symlink", portableManifest, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			publisher := newJoinPublisher(t, tc.definition)
			if tc.unsafeLink {
				symlinkRealmshare(t, "../../../outside.md", filepath.Join(publisher.realm, "shared", "wiki", "escape.md"))
				publisher.git(t, publisher.realm, "config", "core.symlinks", "true")
			}
			publishJoinFixture(t, publisher)
			receiver := newJoinReceiver(t, publisher.remote)
			receiver.write(t, "AGENTS.local.md", "# Private instructions\n")
			if _, err := Join(t.Context(), receiver.realm, receiver.remote); err == nil {
				t.Fatal("joined with an invalid remote definition")
			}
			if _, err := LoadBinding(receiver.realm); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("binding lookup after rejected join: %v", err)
			}
			assertRealmshareAbsent(t, joinFixtureClonePath(receiver))
			assertRealmshareAbsent(t, filepath.Join(receiver.realm, "wiki"))
			assertRealmshareAbsent(t, filepath.Join(receiver.realm, "..", "join-escape"))
			receiver.assertOriginal(t, "AGENTS.local.md", "", "# Private instructions\n")
		})
	}
}

func TestJoinRejectsUndeclaredTrackedFile(t *testing.T) {
	publisher := newJoinPublisher(t, portableManifest)
	publisher.write(t, "undeclared.md", "# Ordinary extra file\n")
	publishJoinFixture(t, publisher)
	receiver := newJoinReceiver(t, publisher.remote)
	receiver.write(t, "guide.md", "# Private guide\n")

	_, err := Join(t.Context(), receiver.realm, receiver.remote)
	if err == nil || !strings.Contains(err.Error(), `undeclared Git tree path "undeclared.md"`) {
		t.Errorf("join error = %v, want undeclared Git tree path", err)
	}
	if _, err := LoadBinding(receiver.realm); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("binding lookup after rejected join: %v", err)
	}
	assertRealmshareAbsent(t, joinFixtureClonePath(receiver))
	assertRealmshareAbsent(t, filepath.Join(receiver.realm, "wiki"))
	assertRealmshareAbsent(t, filepath.Join(receiver.realm, "undeclared.md"))
	receiver.assertOriginal(t, "guide.md", "", "# Private guide\n")
}

func TestJoinDoesNotExecuteCheckoutHooksOrTouchMembers(t *testing.T) {
	publisher := newJoinPublisher(t, strings.Replace(portableManifest, "exclude = true\n", "", 1)+`
[[members]]
key = "absent"
path = "repos/absent"
`)
	const hook = "shared/wiki/git-hooks/post-checkout"
	publisher.write(t, hook, "#!/bin/sh\nprintf 'checkout hook ran\\n' > \"$ATOMIC_REALM_JOIN_MARKER\"\n")
	if err := os.Chmod(filepath.Join(publisher.realm, hook), 0o700); err != nil {
		t.Fatal(err)
	}
	publisher.git(t, publisher.realm, "add", "--chmod=+x", "--", hook)
	publishJoinFixture(t, publisher)

	receiver := newJoinReceiver(t, publisher.remote)
	marker := filepath.Join(t.TempDir(), "checkout-hook-ran")
	t.Setenv("ATOMIC_REALM_JOIN_MARKER", marker)
	receiver.git(t, receiver.home, "config", "--global", "core.hooksPath", "shared/wiki/git-hooks")
	control := filepath.Join(t.TempDir(), "normal-checkout")
	receiver.git(t, receiver.home, "clone", "--quiet", receiver.remote, control)
	if got := readRealmshareFile(t, marker); got != "checkout hook ran\n" {
		t.Fatalf("ordinary checkout hook marker = %q", got)
	}
	if err := os.Remove(marker); err != nil {
		t.Fatal(err)
	}

	member := filepath.Join(receiver.realm, "repos", "service")
	const memberPath = "repos/service/readme.md"
	receiver.write(t, memberPath, "committed\n")
	receiver.git(t, member, "init", "--quiet")
	receiver.git(t, member, "add", "readme.md")
	receiver.git(t, member, "commit", "--quiet", "-m", "Member work")
	receiver.git(t, member, "remote", "add", "origin", receiver.remote)
	receiver.write(t, memberPath, "staged\n")
	receiver.git(t, member, "add", "readme.md")
	receiver.write(t, memberPath, "unstaged\n")
	receiver.write(t, ".claude/settings.local.json", "{\"fixture\":\"personal\"}\n")
	receiver.write(t, "AGENTS.local.md", "# Personal instructions\n")
	memberFiles := []string{"HEAD", "config", "index"}
	before := make([]string, len(memberFiles))
	for i, name := range memberFiles {
		before[i] = readRealmshareFile(t, filepath.Join(member, ".git", name))
	}
	refs := receiver.git(t, member, "show-ref")

	if _, err := Join(t.Context(), receiver.realm, receiver.remote); err != nil {
		t.Fatal(err)
	}
	assertRealmshareAbsent(t, marker)
	assertRealmshareAbsent(t, filepath.Join(receiver.realm, "repos", "absent"))
	for i, name := range memberFiles {
		if got := readRealmshareFile(t, filepath.Join(member, ".git", name)); got != before[i] {
			t.Errorf("join changed member .git/%s", name)
		}
	}
	if got := receiver.git(t, member, "show-ref"); got != refs {
		t.Errorf("join changed member refs: %s", got)
	}
	receiver.assertOriginal(t, memberPath, "", "unstaged\n")
	receiver.assertOriginal(t, ".claude/settings.local.json", "", "{\"fixture\":\"personal\"}\n")
	receiver.assertOriginal(t, "AGENTS.local.md", "", "# Personal instructions\n")
}

func TestJoinMatchingRetryPreservesManagedCheckout(t *testing.T) {
	publisher := newJoinPublisher(t, portableManifest)
	revision := publishJoinFixture(t, publisher)
	receiver := newJoinReceiver(t, publisher.remote)
	first, err := Join(t.Context(), receiver.realm, receiver.remote)
	if err != nil {
		t.Fatal(err)
	}
	clone := first.Binding.ClonePath
	before, err := os.Stat(clone)
	if err != nil {
		t.Fatal(err)
	}
	draft := filepath.Join(clone, "guide.md")
	writeRealmshareFile(t, draft, "# Unpublished work\n")

	retry, err := Join(t.Context(), receiver.realm, receiver.remote)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(retry, first) || !reflect.DeepEqual(receiver.binding(t), first.Binding) {
		t.Errorf("matching join changed its binding or preview: before %+v, after %+v", first, retry)
	}
	after, err := os.Stat(clone)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(before, after) {
		t.Error("matching join replaced the managed checkout")
	}
	if got := readRealmshareFile(t, draft); got != "# Unpublished work\n" {
		t.Errorf("matching join overwrote the draft: %q", got)
	}
	if got := receiver.git(t, clone, "rev-parse", "HEAD"); got != revision {
		t.Errorf("matching join changed HEAD to %q, want %q", got, revision)
	}
	assertRealmshareAbsent(t, filepath.Join(receiver.realm, "guide.md"))
}

func TestJoinRefusesUnownedCloneEvenWithMatchingOrigin(t *testing.T) {
	publisher := newJoinPublisher(t, portableManifest)
	revision := publishJoinFixture(t, publisher)
	receiver := newJoinReceiver(t, publisher.remote)
	clone := joinFixtureClonePath(receiver)
	receiver.git(t, receiver.home, "clone", "--quiet", receiver.remote, clone)
	draft := filepath.Join(clone, "guide.md")
	writeRealmshareFile(t, draft, "# Unrelated work\n")

	if _, err := Join(t.Context(), receiver.realm, receiver.remote); err == nil {
		t.Fatal("join adopted or bypassed an unrelated checkout")
	}
	if _, err := LoadBinding(receiver.realm); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("binding lookup after clone collision: %v", err)
	}
	assertRealmshareAbsent(t, filepath.Join(clone, cloneProof))
	assertRealmshareAbsent(t, filepath.Join(receiver.realm, "guide.md"))
	if got := readRealmshareFile(t, draft); got != "# Unrelated work\n" {
		t.Errorf("join changed unrelated work: %q", got)
	}
	if got := receiver.git(t, clone, "rev-parse", "HEAD"); got != revision {
		t.Errorf("join changed the unrelated checkout's HEAD to %q, want %q", got, revision)
	}
}

func newJoinTLSServer(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()
	server := httptest.NewTLSServer(handler)
	t.Cleanup(server.Close)
	ca := filepath.Join(t.TempDir(), "git-ca.pem")
	writeRealmshareFile(t, ca, string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})))
	t.Setenv("GIT_SSL_CAINFO", ca)
	t.Setenv("GIT_SSL_NO_VERIFY", "false")
	t.Setenv("NO_PROXY", "127.0.0.1")
	t.Setenv("no_proxy", "127.0.0.1")
	return server
}

func newJoinPublisher(t *testing.T, definition string) shareFixture {
	t.Helper()
	publisher := newShareFixture(t)
	publisher.git(t, publisher.realm, "init", "--quiet")
	writeManifestFixture(t, publisher.realm, definition)
	return publisher
}

func publishJoinFixture(t *testing.T, publisher shareFixture) string {
	t.Helper()
	ref := "refs/heads/" + joinFixtureBranch
	publisher.git(t, publisher.realm, "add", "--all")
	publisher.git(t, publisher.realm, "commit", "--quiet", "-m", "Publish shared realm")
	publisher.git(t, publisher.realm, "push", "--quiet", publisher.remote, "HEAD:"+ref)
	publisher.git(t, publisher.remote, "symbolic-ref", "HEAD", ref)
	return publisher.git(t, publisher.realm, "rev-parse", "HEAD")
}

func newJoinReceiver(t *testing.T, remote string) shareFixture {
	t.Helper()
	home, realm, _ := bindingFixture(t)
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, ".gitconfig"))
	return shareFixture{home: home, realm: realm, state: filepath.Join(realm, ".atomic"), remote: remote}
}

func joinFixtureClonePath(receiver shareFixture) string {
	return filepath.Join(config.Dir(receiver.home), "realms", filepath.Base(receiver.realm))
}
