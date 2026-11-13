package workflow

import (
	"os/exec"
	"testing"
)

func TestProviderForOrigin(t *testing.T) {
	cases := []struct {
		url, host, provider string
	}{
		{"git@github.com:acme/app.git", "github.com", "github"},
		{"https://github.com/acme/app", "github.com", "github"},
		{"https://user@github.com/acme/app.git", "github.com", "github"},
		{"https://user:tok@gitlab.com/acme/app.git", "gitlab.com", "gitlab"},
		{"ssh://git@bitbucket.org/acme/app.git", "bitbucket.org", "bitbucket"},
		{"ssh://git@codeberg.org:2222/acme/app.git", "codeberg.org", "codeberg"},
		{"git@Acme.GHE.com:acme/app.git", "acme.ghe.com", "github"},
		{"HTTPS://GitHub.COM/acme/app", "github.com", "github"},
		{"git@git.acme.example:acme/app.git", "git.acme.example", ""},
		{"https://notgithub.com/acme/app", "notgithub.com", ""},
		{"https://ghe.com/acme/app", "ghe.com", ""},
		{"", "", ""},
	}
	for _, tc := range cases {
		o := ProviderForOrigin(tc.url)
		if o.Host != tc.host || o.Provider != tc.provider {
			t.Errorf("ProviderForOrigin(%q) = %+v, want host %q provider %q", tc.url, o, tc.host, tc.provider)
		}
	}
}

func TestOriginURL_ReadsTheRepoOrigin(t *testing.T) {
	repo := t.TempDir()
	run(t, "git", "-C", repo, "init", "-q")
	run(t, "git", "-C", repo, "remote", "add", "origin", "git@gitlab.com:acme/app.git")
	if got := OriginURL(repo); got != "git@gitlab.com:acme/app.git" {
		t.Errorf("OriginURL = %q", got)
	}
}

func TestOriginURL_NoOriginIsEmpty(t *testing.T) {
	repo := t.TempDir()
	run(t, "git", "-C", repo, "init", "-q")
	if got := OriginURL(repo); got != "" {
		t.Errorf("OriginURL = %q, want empty", got)
	}
}

func run(t *testing.T, name string, args ...string) {
	t.Helper()
	if out, err := exec.Command(name, args...).CombinedOutput(); err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, out)
	}
}
