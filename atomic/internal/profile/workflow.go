package profile

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/damusix/atomic-claude/atomic/internal/workflow"
)

// WorkflowProbe holds the seams DetectWorkflow reads through.
type WorkflowProbe struct {
	Home string
	GOOS string
	// Getenv reads only the config-path variables; tokens go through EnvSet.
	Getenv func(name string) string
	// EnvSet reports whether a variable is set. It takes a name and returns no
	// value, so a token can never be read through it.
	EnvSet func(name string) bool
}

// WorkflowFinding is one detected provider or tracker and why it was found.
type WorkflowFinding struct{ Value, Reason string }

// WorkflowDetection holds one finding per distinct source-control provider and
// per non-host issue tracker that has credentials.
type WorkflowDetection struct {
	SourceControl []WorkflowFinding
	IssueTracker  []WorkflowFinding
}

func (d *WorkflowDetection) addSourceControl(value, reason string) {
	d.SourceControl = addFinding(d.SourceControl, value, reason)
}

func (d *WorkflowDetection) addIssueTracker(value, reason string) {
	d.IssueTracker = addFinding(d.IssueTracker, value, reason)
}

func addFinding(list []WorkflowFinding, value, reason string) []WorkflowFinding {
	for _, f := range list {
		if f.Value == value {
			return list
		}
	}
	return append(list, WorkflowFinding{Value: value, Reason: reason})
}

// present records whether a YAML field is non-empty and drops the value.
type present bool

func (p *present) UnmarshalYAML(n *yaml.Node) error {
	*p = present(n.Kind == yaml.ScalarNode && n.Tag != "!!null" && n.Value != "")
	return nil
}

func (p WorkflowProbe) goos() string {
	if p.GOOS != "" {
		return p.GOOS
	}
	return runtime.GOOS
}

func (p WorkflowProbe) env(name string) string {
	if p.Getenv != nil {
		return p.Getenv(name)
	}
	return os.Getenv(name)
}

// configPaths lists a CLI's host files in lookup order; the first existing one
// is read, and a set path variable replaces the defaults.
func (p WorkflowProbe) configPaths(cli string) []string {
	xdg := p.env(xdgConfigHome)
	home := func(rel ...string) string { return filepath.Join(append([]string{p.Home}, rel...)...) }
	appSupport := func(name string) string {
		return home("Library", "Application Support", name)
	}
	switch cli {
	case "gh":
		if d := p.env(ghConfigDir); d != "" {
			return []string{filepath.Join(d, "hosts.yml")}
		}
		if xdg != "" {
			return []string{filepath.Join(xdg, "gh", "hosts.yml")}
		}
		return []string{home(".config", "gh", "hosts.yml")}
	case "glab":
		if d := p.env(glabConfigDir); d != "" {
			return []string{filepath.Join(d, "config.yml")}
		}
		if xdg != "" {
			return []string{filepath.Join(xdg, "glab-cli", "config.yml")}
		}
		paths := []string{home(".config", "glab-cli", "config.yml")}
		if p.goos() == "darwin" {
			paths = append(paths, filepath.Join(appSupport("glab-cli"), "config.yml"))
		}
		return paths
	case "tea":
		var paths []string
		switch {
		case xdg != "":
			paths = []string{filepath.Join(xdg, "tea", "config.yml")}
		case p.goos() == "darwin":
			paths = []string{filepath.Join(appSupport("tea"), "config.yml")}
		default:
			paths = []string{home(".config", "tea", "config.yml")}
		}
		return append(paths, home(".tea", "tea.yml"))
	case "fj":
		// fj's macOS data directory is unverified, so macOS is not probed.
		if p.goos() == "darwin" {
			return nil
		}
		if d := p.env(xdgDataHome); d != "" {
			return []string{filepath.Join(d, "forgejo-cli", "keys.json")}
		}
		return []string{home(".local", "share", "forgejo-cli", "keys.json")}
	}
	return nil
}

// DetectWorkflow reads each CLI's host configuration file and checks
// credential variable names. A token value never enters a finding.
func DetectWorkflow(p WorkflowProbe) WorkflowDetection {
	envSet := p.EnvSet
	if envSet == nil {
		envSet = func(name string) bool { _, ok := os.LookupEnv(name); return ok }
	}
	var d WorkflowDetection

	var gh map[string]struct {
		User present `yaml:"user"`
	}
	if readFirst(p.configPaths("gh"), yaml.Unmarshal, &gh) {
		for _, host := range sortedKeys(gh) {
			if gh[host].User {
				d.addSourceControl("github", "gh authed for "+host)
				break
			}
		}
	}

	var glab struct {
		Hosts map[string]struct {
			Token      present `yaml:"token"`
			UseKeyring bool    `yaml:"use_keyring"`
		} `yaml:"hosts"`
	}
	if readFirst(p.configPaths("glab"), yaml.Unmarshal, &glab) {
		for _, host := range sortedKeys(glab.Hosts) {
			if h := glab.Hosts[host]; bool(h.Token) || h.UseKeyring {
				d.addSourceControl("gitlab", "glab authed for "+host)
				break
			}
		}
	}

	var tea struct {
		Logins []struct {
			URL string `yaml:"url"`
		} `yaml:"logins"`
	}
	if readFirst(p.configPaths("tea"), yaml.Unmarshal, &tea) {
		for _, l := range tea.Logins {
			if o := workflow.ProviderForOrigin(l.URL); o.Provider == "codeberg" {
				d.addSourceControl("codeberg", "tea authed for "+o.Host)
				break
			}
		}
	}

	var fj struct {
		Hosts map[string]struct{} `json:"hosts"`
	}
	if readFirst(p.configPaths("fj"), json.Unmarshal, &fj) {
		for _, host := range sortedKeys(fj.Hosts) {
			if workflow.ProviderForOrigin("https://"+host).Provider == "codeberg" {
				d.addSourceControl("codeberg", "fj authed for "+host)
				break
			}
		}
	}

	for _, v := range providerTokens {
		if envSet(v.name) {
			d.addSourceControl(v.value, v.name+" set")
		}
	}
	for _, v := range trackerTokens {
		if envSet(v.name) {
			d.addIssueTracker(v.value, v.name+" set")
		}
	}
	if envSet(trelloKey) && envSet(trelloToken) {
		d.addIssueTracker("trello", trelloKey+" and "+trelloToken+" set")
	}
	return d
}

type tokenVar struct{ name, value string }

var (
	pathVars       = []string{ghConfigDir, glabConfigDir, xdgConfigHome, xdgDataHome}
	providerTokens = []tokenVar{
		{"GH_TOKEN", "github"}, {"GITHUB_TOKEN", "github"},
		{"GH_ENTERPRISE_TOKEN", "github"}, {"GITLAB_TOKEN", "gitlab"},
	}
	trackerTokens = []tokenVar{{"LINEAR_API_KEY", "linear"}, {"JIRA_API_TOKEN", "jira"}}
)

const (
	trelloKey   = "TRELLO_API_KEY"
	trelloToken = "TRELLO_TOKEN"

	ghConfigDir   = "GH_CONFIG_DIR"
	glabConfigDir = "GLAB_CONFIG_DIR"
	xdgConfigHome = "XDG_CONFIG_HOME"
	xdgDataHome   = "XDG_DATA_HOME"
)

// WorkflowEnvVars lists every variable DetectWorkflow reads through the
// process environment, so tests can clear them before reaching Refresh.
func WorkflowEnvVars() []string {
	names := append([]string{trelloKey, trelloToken}, pathVars...)
	for _, v := range append(append([]tokenVar{}, providerTokens...), trackerTokens...) {
		names = append(names, v.name)
	}
	return names
}

// readFirst decodes the first path that exists; it reports whether that one
// decoded.
func readFirst(paths []string, unmarshal func([]byte, any) error, out any) bool {
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err == nil {
			return unmarshal(data, out) == nil
		}
	}
	return false
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

const (
	workflowHeading = "## Workflow preferences"
	emptyBlock      = "<workflow-preferences level='profile'>\n</workflow-preferences>\n"
)

// ApplyWorkflowDetection rewrites only absent or `# detected` lines; empty
// content starts from the stub. A bad block returns the content and an error.
func ApplyWorkflowDetection(content string, d WorkflowDetection) (string, error) {
	content, whole, err := ensureBlock(content)
	if err != nil {
		return content, err
	}

	lines := strings.Split(content, "\n")
	drop := map[int]bool{}
	for _, k := range []struct {
		key      workflow.Key
		findings []WorkflowFinding
	}{
		{workflow.KeySourceControl, d.SourceControl},
		{workflow.KeyIssueTracker, d.IssueTracker},
	} {
		ln, hasLine := whole.Lines[k.key]
		if hasLine && !ln.Soft {
			continue
		}
		switch {
		case len(k.findings) == 1:
			lines = whole.SetLine(lines, k.key, softLine(k.key, k.findings[0]))
		case len(k.findings) > 1 && hasLine:
			drop[ln.N] = true
		}
	}

	var out []string
	for i, line := range lines {
		if !drop[i] {
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n"), nil
}

// EnsureWorkflowBlock places a profile block under `## Workflow preferences`.
// A duplicate, unpaired, or misplaced block is an error.
func EnsureWorkflowBlock(content string) (string, error) {
	out, _, err := ensureBlock(content)
	return out, err
}

func ensureBlock(content string) (string, *workflow.Block, error) {
	orig := content
	if strings.TrimSpace(content) == "" {
		content = renderStubWithoutEnv()
	}
	whole, err := locateBlock(content)
	if err == nil && whole == nil {
		content = insertBlock(content)
		if whole, err = locateBlock(content); err == nil && whole == nil {
			err = fmt.Errorf("could not place a block under %s", workflowHeading)
		}
	}
	if err != nil {
		return orig, nil, err
	}
	return content, whole, nil
}

// locateBlock returns the profile block, nil when there is none, and an error
// for a duplicate, unpaired, or misplaced one.
func locateBlock(content string) (*workflow.Block, error) {
	whole, err := workflow.ParseBlock(content)
	if err != nil {
		return nil, err
	}
	inSection, err := workflow.ParseBlock(workflow.ProfileSection(content))
	if err != nil {
		return nil, err
	}
	if whole != nil && inSection == nil {
		return nil, fmt.Errorf("preference block sits outside %s", workflowHeading)
	}
	return whole, nil
}

func softLine(key workflow.Key, f WorkflowFinding) string {
	return fmt.Sprintf("- %s: %s  # detected: %s", key, f.Value, f.Reason)
}

// insertBlock adds an empty profile block: inside an existing section, else in
// a new section placed before ## Environment, else at the end of the file.
func insertBlock(content string) string {
	if at := workflow.ProfileSectionStart(content); at != -1 {
		if !strings.HasSuffix(content[:at], "\n") {
			return content + "\n\n" + emptyBlock
		}
		return content[:at] + "\n" + emptyBlock + content[at:]
	}

	section := workflowHeading + "\n\n" + emptyBlock
	env := findHeadingIndex(content, "## Environment")
	if env == -1 {
		if content != "" && !strings.HasSuffix(content, "\n") {
			content += "\n"
		}
		if content != "" {
			content += "\n"
		}
		return content + section
	}
	before := content[:env]
	if before != "" && !strings.HasSuffix(before, "\n\n") {
		before += "\n"
	}
	return before + section + "\n" + content[env:]
}
