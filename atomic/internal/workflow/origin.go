package workflow

import (
	"os/exec"
	"strings"
)

// Origin is the host of a Git origin URL and the provider it maps to, "" when
// the host maps to none.
type Origin struct {
	Host     string
	Provider string
}

var providerHost = map[string]string{
	"github":    "github.com",
	"gitlab":    "gitlab.com",
	"bitbucket": "bitbucket.org",
	"codeberg":  "codeberg.org",
}

// ProviderForOrigin parses the scp, https, and ssh forms of a remote URL,
// case-insensitively.
func ProviderForOrigin(url string) Origin {
	host := originHost(strings.TrimSpace(url))
	return Origin{Host: host, Provider: providerOf(host)}
}

func originHost(url string) string {
	var host string
	if _, rest, ok := strings.Cut(url, "://"); ok {
		authority, _, _ := strings.Cut(rest, "/")
		authority = authority[strings.LastIndex(authority, "@")+1:]
		host, _, _ = strings.Cut(authority, ":")
	} else if i := strings.Index(url, ":"); i >= 0 {
		host = url[:i]
		host = host[strings.LastIndex(host, "@")+1:]
	}
	return strings.ToLower(host)
}

func providerOf(host string) string {
	switch {
	case host == "github.com", strings.HasSuffix(host, ".ghe.com"):
		return "github"
	case host == "gitlab.com":
		return "gitlab"
	case host == "bitbucket.org":
		return "bitbucket"
	case host == "codeberg.org":
		return "codeberg"
	}
	return ""
}

// OriginURL returns the origin remote of the repository at root, "" when there
// is none.
func OriginURL(root string) string {
	out, err := exec.Command("git", "-C", root, "remote", "get-url", "origin").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
