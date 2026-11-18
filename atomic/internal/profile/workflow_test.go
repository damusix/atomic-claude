package profile

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/workflow"
)

const secret = "s3cr3t-token-value"

func probeFor(home string, envNames ...string) WorkflowProbe {
	return probeOn("linux", home, nil, envNames...)
}

// probeOn fakes the platform and environment: pathEnv feeds Getenv, envNames
// feeds the token-name check.
func probeOn(goos, home string, pathEnv map[string]string, envNames ...string) WorkflowProbe {
	set := map[string]bool{}
	for _, n := range envNames {
		set[n] = true
	}
	return WorkflowProbe{
		Home:   home,
		GOOS:   goos,
		EnvSet: func(n string) bool { return set[n] },
		Getenv: func(n string) string { return pathEnv[n] },
	}
}

func quiet(home string) refreshOpts {
	opts, _ := quietOpts(probeFor(home))
	return opts
}

// quietOpts is a refresh with no real tool probing and a captured stderr.
func quietOpts(probe WorkflowProbe) (refreshOpts, *bytes.Buffer) {
	var stderr bytes.Buffer
	return refreshOpts{probe: probe, registry: []RegistryEntry{}, stderr: &stderr}, &stderr
}

func writeAt(t *testing.T, home, rel, body string) {
	t.Helper()
	p := filepath.Join(home, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func values(d []WorkflowFinding) []string {
	var out []string
	for _, f := range d {
		out = append(out, f.Value)
	}
	return out
}

const (
	ghBody   = "github.com:\n    user: ada\n    oauth_token: " + secret + "\n"
	glabBody = "hosts:\n    gitlab.com:\n        token: " + secret + "\n"
	teaBody  = "logins:\n  - name: cb\n    url: https://codeberg.org\n    token: " + secret + "\n"
	fjBody   = `{"hosts":{"codeberg.org":{"type":"Application","token":"` + secret + `"}}}`
)

type lookupCase struct {
	name, goos string
	env        map[string]string
	file, body string
	want       string // provider, "" for none
}

// Paths and env values spell the home directory as "~".
func runLookupCases(t *testing.T, cases []lookupCase) {
	t.Helper()
	for _, c := range cases {
		t.Run(c.name+"/"+c.goos, func(t *testing.T) {
			home := t.TempDir()
			env := map[string]string{}
			for k, v := range c.env {
				env[k] = strings.ReplaceAll(v, "~", home)
			}
			rel, err := filepath.Rel(home, strings.ReplaceAll(c.file, "~", home))
			if err != nil {
				t.Fatal(err)
			}
			writeAt(t, home, rel, c.body)

			got := DetectWorkflow(probeOn(c.goos, home, env)).SourceControl
			switch {
			case c.want == "" && len(got) != 0:
				t.Errorf("SourceControl = %+v, want none", got)
			case c.want != "" && (len(got) != 1 || got[0].Value != c.want):
				t.Errorf("SourceControl = %+v, want %s", got, c.want)
			}
		})
	}
}

func TestDetectWorkflow_GhConfigLocation(t *testing.T) {
	var cases []lookupCase
	for _, goos := range []string{"linux", "darwin"} {
		cases = append(cases,
			lookupCase{"default", goos, nil, "~/.config/gh/hosts.yml", ghBody, "github"},
			lookupCase{"GH_CONFIG_DIR", goos, map[string]string{"GH_CONFIG_DIR": "~/alt"},
				"~/alt/hosts.yml", ghBody, "github"},
			lookupCase{"XDG_CONFIG_HOME", goos, map[string]string{"XDG_CONFIG_HOME": "~/xdg"},
				"~/xdg/gh/hosts.yml", ghBody, "github"},
			lookupCase{"GH_CONFIG_DIR replaces the default", goos,
				map[string]string{"GH_CONFIG_DIR": "~/alt"}, "~/.config/gh/hosts.yml", ghBody, ""},
			lookupCase{"XDG_CONFIG_HOME replaces the default", goos,
				map[string]string{"XDG_CONFIG_HOME": "~/xdg"}, "~/.config/gh/hosts.yml", ghBody, ""},
			lookupCase{"no user is no login", goos, nil, "~/.config/gh/hosts.yml",
				"github.com:\n    git_protocol: ssh\n", ""},
			lookupCase{"keyring login has user only", goos, nil, "~/.config/gh/hosts.yml",
				"github.com:\n    user: ada\n", "github"},
		)
	}
	cases = append(cases, lookupCase{"Application Support is not gh's", "darwin", nil,
		"~/Library/Application Support/gh/hosts.yml", ghBody, ""})
	runLookupCases(t, cases)
}

func TestDetectWorkflow_GlabConfigLocation(t *testing.T) {
	const app = "~/Library/Application Support/glab-cli/config.yml"
	const legacy = "~/.config/glab-cli/config.yml"
	var cases []lookupCase
	for _, goos := range []string{"linux", "darwin"} {
		cases = append(cases,
			lookupCase{"legacy", goos, nil, legacy, glabBody, "gitlab"},
			lookupCase{"GLAB_CONFIG_DIR", goos, map[string]string{"GLAB_CONFIG_DIR": "~/g"},
				"~/g/config.yml", glabBody, "gitlab"},
			lookupCase{"XDG_CONFIG_HOME", goos, map[string]string{"XDG_CONFIG_HOME": "~/xdg"},
				"~/xdg/glab-cli/config.yml", glabBody, "gitlab"},
			lookupCase{"GLAB_CONFIG_DIR replaces legacy", goos,
				map[string]string{"GLAB_CONFIG_DIR": "~/g"}, legacy, glabBody, ""},
			lookupCase{"XDG_CONFIG_HOME replaces legacy", goos,
				map[string]string{"XDG_CONFIG_HOME": "~/xdg"}, legacy, glabBody, ""},
			lookupCase{"empty token is no login", goos, nil, legacy,
				"hosts:\n    gitlab.com:\n        token:\n", ""},
			lookupCase{"keyring login", goos, nil, legacy,
				"hosts:\n    gitlab.com:\n        use_keyring: true\n", "gitlab"},
			lookupCase{"keyring off, no token", goos, nil, legacy,
				"hosts:\n    gitlab.com:\n        use_keyring: false\n", ""},
		)
	}
	cases = append(cases,
		lookupCase{"Application Support", "darwin", nil, app, glabBody, "gitlab"},
		lookupCase{"Application Support", "linux", nil, app, glabBody, ""})
	runLookupCases(t, cases)
}

func TestDetectWorkflow_TeaConfigLocation(t *testing.T) {
	const app = "~/Library/Application Support/tea/config.yml"
	const xdg = "~/.config/tea/config.yml"
	const legacy = "~/.tea/tea.yml"
	var cases []lookupCase
	for _, goos := range []string{"linux", "darwin"} {
		cases = append(cases,
			lookupCase{"legacy", goos, nil, legacy, teaBody, "codeberg"},
			lookupCase{"XDG_CONFIG_HOME", goos, map[string]string{"XDG_CONFIG_HOME": "~/x"},
				"~/x/tea/config.yml", teaBody, "codeberg"},
			lookupCase{"not codeberg", goos, nil, legacy,
				"logins:\n  - url: https://gitea.example\n", ""},
			lookupCase{"no logins", goos, nil, legacy, "logins: []\n", ""},
		)
	}
	cases = append(cases,
		lookupCase{"config dir", "linux", nil, xdg, teaBody, "codeberg"},
		lookupCase{"config dir", "darwin", nil, xdg, teaBody, ""},
		lookupCase{"Application Support", "darwin", nil, app, teaBody, "codeberg"},
		lookupCase{"Application Support", "linux", nil, app, teaBody, ""})
	runLookupCases(t, cases)
}

func TestDetectWorkflow_FjKeysLocation(t *testing.T) {
	const keys = "~/.local/share/forgejo-cli/keys.json"
	const xdgKeys = "~/data/forgejo-cli/keys.json"
	xdg := map[string]string{"XDG_DATA_HOME": "~/data"}
	runLookupCases(t, []lookupCase{
		{"default", "linux", nil, keys, fjBody, "codeberg"},
		{"XDG_DATA_HOME", "linux", xdg, xdgKeys, fjBody, "codeberg"},
		{"XDG_DATA_HOME replaces the default", "linux", xdg, keys, fjBody, ""},
		{"not codeberg", "linux", nil, keys, `{"hosts":{"git.example":{}}}`, ""},
		{"no hosts", "linux", nil, keys, `{"hosts":{}}`, ""},
		{"file probe skipped", "darwin", nil, keys, fjBody, ""},
		{"file probe skipped with XDG", "darwin", xdg, xdgKeys, fjBody, ""},
	})
}

func TestDetectWorkflow_ReasonNamesCLIAndHost(t *testing.T) {
	home := t.TempDir()
	writeAt(t, home, ".config/gh/hosts.yml", ghBody)
	got := DetectWorkflow(probeFor(home)).SourceControl
	if len(got) != 1 || got[0].Reason != "gh authed for github.com" {
		t.Errorf("SourceControl = %+v", got)
	}
}

func TestDetectWorkflow_UnauthenticatedConfigIsNotAProvider(t *testing.T) {
	home := t.TempDir()
	writeAt(t, home, ".config/glab-cli/config.yml", "hosts:\n    gitlab.com:\n        token:\n")
	writeAt(t, home, ".config/tea/config.yml",
		"logins:\n  - name: gt\n    url: https://gitea.example\n    token: "+secret+"\n")
	if got := DetectWorkflow(probeFor(home)); len(got.SourceControl) != 0 {
		t.Errorf("SourceControl = %+v, want none", got.SourceControl)
	}
}

func TestDetectWorkflow_EnvTokensCheckedByName(t *testing.T) {
	got := DetectWorkflow(probeFor(t.TempDir(), "GITLAB_TOKEN", "LINEAR_API_KEY"))
	if v := values(got.SourceControl); len(v) != 1 || v[0] != "gitlab" {
		t.Fatalf("SourceControl = %v, want [gitlab]", v)
	}
	if got.SourceControl[0].Reason != "GITLAB_TOKEN set" {
		t.Errorf("reason = %q", got.SourceControl[0].Reason)
	}
	if v := values(got.IssueTracker); len(v) != 1 || v[0] != "linear" {
		t.Errorf("IssueTracker = %v, want [linear]", v)
	}
}

func TestDetectWorkflow_NonHostTrackers(t *testing.T) {
	home := t.TempDir()
	got := DetectWorkflow(probeFor(home, "JIRA_API_TOKEN", "TRELLO_API_KEY"))
	if v := values(got.IssueTracker); len(v) != 1 || v[0] != "jira" {
		t.Errorf("IssueTracker = %v, want [jira]: trello needs key and token", v)
	}
	got = DetectWorkflow(probeFor(home, "JIRA_API_TOKEN", "TRELLO_API_KEY", "TRELLO_TOKEN"))
	if v := values(got.IssueTracker); len(v) != 2 {
		t.Errorf("IssueTracker = %v, want jira and trello", v)
	}
}

// Atlassian documents no acli config location, so no file is a login signal.
func TestDetectWorkflow_AcliConfigFileIsNotProbed(t *testing.T) {
	home := t.TempDir()
	writeAt(t, home, ".config/acli/jira_config.yaml", "current_profile: work\n")
	if got := DetectWorkflow(probeFor(home)); len(got.IssueTracker) != 0 {
		t.Errorf("IssueTracker = %+v, want none", got.IssueTracker)
	}
}

func TestDetectWorkflow_SameProviderTwiceCountsOnce(t *testing.T) {
	home := t.TempDir()
	writeAt(t, home, ".config/gh/hosts.yml",
		"github.com:\n    user: a\nacme.ghe.com:\n    user: b\n")
	got := DetectWorkflow(probeFor(home, "GH_TOKEN"))
	if v := values(got.SourceControl); len(v) != 1 {
		t.Errorf("SourceControl = %v, want one github", v)
	}
}

// A token value must never reach a finding, the written profile, or stderr.
func TestDetectWorkflow_NoTokenValueRead(t *testing.T) {
	home := t.TempDir()
	writeAt(t, home, ".config/gh/hosts.yml", ghBody)
	writeAt(t, home, ".config/glab-cli/config.yml", glabBody)
	writeAt(t, home, ".config/tea/config.yml", teaBody)
	writeAt(t, home, ".local/share/forgejo-cli/keys.json", fjBody)

	d := DetectWorkflow(probeFor(home))
	if len(d.SourceControl) != 3 {
		t.Fatalf("fixtures detected %+v, want github, gitlab, codeberg", d.SourceControl)
	}
	if got := fmt.Sprintf("%+v", d); strings.Contains(got, secret) {
		t.Errorf("finding carries a token value: %s", got)
	}

	// Two providers write nothing, so refresh a one-provider home and a refusal.
	one := t.TempDir()
	writeAt(t, one, ".config/gh/hosts.yml", ghBody)
	opts, stderr := quietOpts(probeFor(one))
	if _, err := refresh(one, "2026-11-10", opts); err != nil {
		t.Fatal(err)
	}
	profile := filepath.Join(one, ".atomic", "profile.md")
	written, _ := os.ReadFile(profile)
	if !strings.Contains(string(written), "source-control: github") {
		t.Fatalf("fixture not detected:\n%s", written)
	}
	refused := string(written) + "</workflow-preferences>\n"
	writeAt(t, one, ".atomic/profile.md", refused)
	if _, err := refresh(one, "2026-11-11", opts); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(profile)
	for name, got := range map[string]string{
		"written profile": string(written), "refused profile": string(after),
		"stderr": stderr.String(),
	} {
		if strings.Contains(got, secret) {
			t.Errorf("%s carries a token value:\n%s", name, got)
		}
	}
	if stderr.Len() == 0 {
		t.Error("the refusal printed nothing, so stderr was not exercised")
	}
}

func stubProfile(blockBody string) string {
	return "# User profile\n\n## Identity\n<stable>\n- Name: Ada\n</stable>\n\n" +
		"## Workflow preferences\n\n<workflow-preferences level='profile'>\n" + blockBody +
		"</workflow-preferences>\n\n## Environment\n<deterministic>\n- OS: linux\n</deterministic>\n"
}

func one(value, reason string) []WorkflowFinding {
	return []WorkflowFinding{{Value: value, Reason: reason}}
}

func blockOf(t *testing.T, content string) *workflow.Block {
	t.Helper()
	b, err := workflow.ParseBlock(workflow.ProfileSection(content))
	if err != nil || b == nil {
		t.Fatalf("no parseable block (%v) in:\n%s", err, content)
	}
	return b
}

func TestApply_OneProviderWritesSoftLine(t *testing.T) {
	d := WorkflowDetection{SourceControl: one("github", "gh authed for github.com")}
	out, err := ApplyWorkflowDetection(stubProfile(""), d)
	if err != nil {
		t.Fatal(err)
	}
	want := "- source-control: github  # detected: gh authed for github.com\n"
	if !strings.Contains(out, want) {
		t.Errorf("missing %q in:\n%s", want, out)
	}
	if e := blockOf(t, out).Values[workflow.KeySourceControl]; e.Value != "github" || !e.Soft {
		t.Errorf("parsed entry = %+v, want soft github", e)
	}
}

func TestApply_TwoProvidersRemoveEarlierDetectedLine(t *testing.T) {
	in := stubProfile("- source-control: github  # detected: gh authed for github.com\n")
	d := WorkflowDetection{SourceControl: []WorkflowFinding{
		{Value: "github", Reason: "a"}, {Value: "gitlab", Reason: "b"}}}
	out, err := ApplyWorkflowDetection(in, d)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "source-control") {
		t.Errorf("detected line survived two providers:\n%s", out)
	}
}

func TestApply_NoProviderLeavesDetectedLine(t *testing.T) {
	in := stubProfile("- source-control: gitlab  # detected: glab authed for gitlab.com\n")
	out, err := ApplyWorkflowDetection(in, WorkflowDetection{})
	if err != nil {
		t.Fatal(err)
	}
	if out != in {
		t.Errorf("none-detected changed the profile:\n%s", out)
	}
}

func TestApply_OneNonHostTrackerWritesIssueTracker(t *testing.T) {
	d := WorkflowDetection{IssueTracker: one("linear", "LINEAR_API_KEY set")}
	out, err := ApplyWorkflowDetection(stubProfile(""), d)
	if err != nil {
		t.Fatal(err)
	}
	if e := blockOf(t, out).Values[workflow.KeyIssueTracker]; e.Value != "linear" || !e.Soft {
		t.Errorf("issue-tracker = %+v, want soft linear", e)
	}
	if !strings.Contains(out, "# detected: LINEAR_API_KEY set\n") {
		t.Errorf("reason missing:\n%s", out)
	}
}

func TestApply_TwoTrackersRemoveEarlierDetectedLine(t *testing.T) {
	in := stubProfile("- issue-tracker: linear  # detected: LINEAR_API_KEY set\n")
	d := WorkflowDetection{IssueTracker: []WorkflowFinding{
		{Value: "linear", Reason: "a"}, {Value: "jira", Reason: "b"}}}
	out, err := ApplyWorkflowDetection(in, d)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "issue-tracker") {
		t.Errorf("detected line survived two trackers:\n%s", out)
	}
}

func TestApply_ExplicitLinesSurviveEveryCase(t *testing.T) {
	in := stubProfile("- source-control: gitlab\n- issue-tracker: filesystem  # my choice\n")
	cases := map[string]WorkflowDetection{
		"none": {},
		"one":  {SourceControl: one("github", "x"), IssueTracker: one("linear", "y")},
		"two": {SourceControl: []WorkflowFinding{{Value: "github"}, {Value: "codeberg"}},
			IssueTracker: []WorkflowFinding{{Value: "linear"}, {Value: "jira"}}},
	}
	for name, d := range cases {
		out, err := ApplyWorkflowDetection(in, d)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if out != in {
			t.Errorf("%s: explicit lines changed:\n%s", name, out)
		}
	}
}

func TestApply_UserBytesOutsideDetectedLinesPreserved(t *testing.T) {
	body := "# prose a user wrote\n\n- worktrees: off\n- mystery-key: 1\n" +
		"- source-control: gitlab  # detected: old\n  \n"
	in := stubProfile(body)
	d := WorkflowDetection{SourceControl: one("github", "new")}
	out, err := ApplyWorkflowDetection(in, d)
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(in, "- source-control: gitlab  # detected: old\n",
		"- source-control: github  # detected: new\n", 1)
	if out != want {
		t.Errorf("only the detected line may change.\n got:\n%s\nwant:\n%s", out, want)
	}
}

func TestApply_MissingSectionInsertedBeforeEnvironment(t *testing.T) {
	in := "# User profile\n\n## Identity\n- Name: Ada\n\n## Environment\n<deterministic>\n- OS: linux\n</deterministic>\n"
	out, err := ApplyWorkflowDetection(in, WorkflowDetection{})
	if err != nil {
		t.Fatal(err)
	}
	sec := strings.Index(out, "## Workflow preferences")
	env := strings.Index(out, "## Environment")
	if sec < 0 || sec > env {
		t.Fatalf("section not before Environment:\n%s", out)
	}
	if b := blockOf(t, out); len(b.Values) != 0 || b.Level != "profile" {
		t.Errorf("block = %+v, want empty level=profile", b)
	}
	if !strings.HasSuffix(out, in[strings.Index(in, "## Environment"):]) {
		t.Errorf("Environment span changed:\n%s", out)
	}
}

func TestApply_MissingSectionWithoutEnvironmentGoesToEnd(t *testing.T) {
	in := "# User profile\n\n## Identity\n- Name: Ada\n"
	out, err := ApplyWorkflowDetection(in, WorkflowDetection{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out, in) || !strings.Contains(out[len(in):], "## Workflow preferences") {
		t.Errorf("section not appended:\n%s", out)
	}
	blockOf(t, out)
}

func TestApply_SectionWithoutBlockGetsOne(t *testing.T) {
	in := "# User profile\n\n## Workflow preferences\n\nsome prose\n\n## Environment\n"
	out, err := ApplyWorkflowDetection(in, WorkflowDetection{SourceControl: one("github", "r")})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "some prose") {
		t.Errorf("prose lost:\n%s", out)
	}
	if e := blockOf(t, out).Values[workflow.KeySourceControl]; e.Value != "github" {
		t.Errorf("entry = %+v", e)
	}
}

func TestApply_RefusalCasesWriteNothing(t *testing.T) {
	block := "<workflow-preferences level='profile'>\n</workflow-preferences>\n"
	cases := map[string]string{
		"duplicate block": stubProfile("") + "\n## Notes\n" + block,
		"unpaired open":   strings.Replace(stubProfile(""), "</workflow-preferences>\n", "", 1),
		"unpaired close":  stubProfile("") + "</workflow-preferences>\n",
		"block in Environment span": "# User profile\n\n## Workflow preferences\n\n" + block +
			"\n## Environment\n<deterministic>\n" + block + "</deterministic>\n",
		"block only in Environment span": "# User profile\n\n## Environment\n<deterministic>\n" +
			block + "</deterministic>\n",
	}
	for name, in := range cases {
		out, err := ApplyWorkflowDetection(in, WorkflowDetection{SourceControl: one("github", "r")})
		if err == nil {
			t.Errorf("%s: want a refusal, got none", name)
		}
		if out != in {
			t.Errorf("%s: refusal returned changed content", name)
		}
	}
}

func TestApply_IdempotentAndUnchangedIsIdentical(t *testing.T) {
	d := WorkflowDetection{
		SourceControl: one("github", "gh authed for github.com"),
		IssueTracker:  one("linear", "LINEAR_API_KEY set"),
	}
	in := "# User profile\n\n## Environment\n<deterministic>\n</deterministic>\n"
	first, err := ApplyWorkflowDetection(in, d)
	if err != nil {
		t.Fatal(err)
	}
	second, err := ApplyWorkflowDetection(first, d)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Errorf("second run changed bytes:\n%s\n---\n%s", first, second)
	}
}

func TestStub_CarriesEmptyWorkflowBlockBeforeEnvironment(t *testing.T) {
	stub := RenderStub(Env{})
	sec := strings.Index(stub, "## Workflow preferences")
	if sec < 0 || sec > strings.Index(stub, "## Environment") {
		t.Fatalf("section missing or after Environment:\n%s", stub)
	}
	if b := blockOf(t, stub); len(b.Values) != 0 || b.Level != "profile" {
		t.Errorf("stub block = %+v, want empty level=profile", b)
	}
}

func TestRefresh_AutodetectWritesProfileOnly(t *testing.T) {
	home := t.TempDir()
	writeAt(t, home, ".config/gh/hosts.yml", "github.com:\n    user: ada\n")
	opts, _ := quietOpts(probeFor(home))

	wrote, err := refresh(home, "2026-11-10", opts)
	if err != nil || !wrote {
		t.Fatalf("refresh = %v, %v", wrote, err)
	}
	got, _ := os.ReadFile(filepath.Join(home, ".atomic", "profile.md"))
	if !strings.Contains(string(got), "- source-control: github  # detected: gh authed for github.com") {
		t.Errorf("detection not written:\n%s", got)
	}

	var files []string
	err = filepath.WalkDir(home, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			rel, _ := filepath.Rel(home, p)
			files = append(files, rel)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{filepath.Join(".atomic", "profile.lock"), filepath.Join(".atomic", "profile.md"),
		filepath.Join(".config", "gh", "hosts.yml")}
	if !reflect.DeepEqual(files, want) {
		t.Errorf("files under home = %v, want exactly %v", files, want)
	}
}

func TestRefresh_RefusalWritesNothingAndExplainsOnStderr(t *testing.T) {
	home := t.TempDir()
	in := stubProfile("") + "</workflow-preferences>\n"
	writeAt(t, home, ".atomic/profile.md", in)
	opts, stderr := quietOpts(probeFor(home))

	wrote, err := refresh(home, "2026-11-10", opts)
	if err != nil || wrote {
		t.Fatalf("refresh = %v, %v; want false, nil", wrote, err)
	}
	got, _ := os.ReadFile(filepath.Join(home, ".atomic", "profile.md"))
	if string(got) != in {
		t.Errorf("refusal modified the profile:\n%s", got)
	}
	if !strings.Contains(stderr.String(), "closing tag without an opening tag") {
		t.Errorf("stderr = %q, want the refusal reason", stderr.String())
	}
}

func TestRefresh_UnchangedResultWritesNothing(t *testing.T) {
	home := t.TempDir()
	opts, _ := quietOpts(probeFor(home, "LINEAR_API_KEY"))
	if _, err := refresh(home, "2026-11-10", opts); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(home, ".atomic", "profile.md")
	before, _ := os.Stat(p)

	wrote, err := refresh(home, "2026-11-10", opts)
	if err != nil || wrote {
		t.Fatalf("second refresh = %v, %v; want false, nil", wrote, err)
	}
	after, _ := os.Stat(p)
	if !before.ModTime().Equal(after.ModTime()) {
		t.Error("an unchanged result rewrote the file")
	}
}

// TestMain clears these, so a developer's or CI's real login never reaches a
// test that goes through Refresh.
func TestWorkflowEnvVarsAreClearedForTests(t *testing.T) {
	for _, name := range WorkflowEnvVars() {
		if _, ok := os.LookupEnv(name); ok {
			t.Errorf("%s leaked into the test process", name)
		}
	}
}

func TestRefresh_IgnoresAmbientCredentials(t *testing.T) {
	home := t.TempDir()
	opts, _ := quietOpts(WorkflowProbe{Home: home})
	if _, err := refresh(home, "2026-11-10", opts); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(home, ".atomic", "profile.md"))
	if strings.Contains(string(got), "# detected") {
		t.Errorf("the ambient environment reached detection:\n%s", got)
	}
}

func TestApply_EmptyContentStartsFromTheStub(t *testing.T) {
	out, err := ApplyWorkflowDetection("", WorkflowDetection{SourceControl: one("github", "r")})
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(renderStubWithoutEnv(), "</workflow-preferences>",
		"- source-control: github  # detected: r\n</workflow-preferences>", 1)
	if out != want {
		t.Errorf("empty content did not start from the stub:\n%s", out)
	}
}

func TestApply_DroppedSoftLinesAreStillSoft(t *testing.T) {
	cases := map[string]string{
		"detected host tracker": "- issue-tracker: github  # detected\n",
		"detected bad value":    "- issue-tracker: nonsense  # detected: old\n",
	}
	d := WorkflowDetection{IssueTracker: one("linear", "LINEAR_API_KEY set")}
	for name, body := range cases {
		out, err := ApplyWorkflowDetection(stubProfile(body), d)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		want := stubProfile("- issue-tracker: linear  # detected: LINEAR_API_KEY set\n")
		if out != want {
			t.Errorf("%s: want the soft line replaced by linear, got:\n%s", name, out)
		}
	}
}

func TestApply_CRLFProfileKeepsItsLineEndings(t *testing.T) {
	crlf := func(s string) string { return strings.ReplaceAll(s, "\n", "\r\n") }
	d := WorkflowDetection{SourceControl: one("github", "new")}
	want := crlf(stubProfile("- source-control: github  # detected: new\n"))

	for name, in := range map[string]string{
		"rewritten line": crlf(stubProfile("- source-control: gitlab  # detected: old\n")),
		"added line":     crlf(stubProfile("")),
	} {
		out, err := ApplyWorkflowDetection(in, d)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if out != want {
			t.Errorf("%s: line endings changed:\n%q", name, out)
		}
	}
}

func TestApply_HeadingMatchIsExactAndFenceAware(t *testing.T) {
	const env = "## Environment\n<deterministic>\n</deterministic>\n"
	const section = "## Workflow preferences\n\n<workflow-preferences level='profile'>\n" +
		"- source-control: github  # detected: r\n</workflow-preferences>\n\n"
	for name, head := range map[string]string{
		"fenced copy":  "# P\n\n```\n## Workflow preferences\n```\n\n",
		"longer title": "# P\n\n## Workflow preferences and more\n\ntext\n\n",
	} {
		out, err := ApplyWorkflowDetection(head+env, WorkflowDetection{SourceControl: one("github", "r")})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if want := head + section + env; out != want {
			t.Errorf("%s: want a fresh section before Environment, got:\n%s", name, out)
		}
	}
}

// A refusal leaves lastcheck stale, so the tool probes must not run each time.
func TestRefresh_RefusalSkipsToolProbing(t *testing.T) {
	bin := t.TempDir()
	marker := filepath.Join(t.TempDir(), "ran")
	script := "#!/bin/sh\ntouch " + marker + "\n"
	if err := os.WriteFile(filepath.Join(bin, "fakecli"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	reg := []RegistryEntry{{
		Name: "fakecli", Binaries: []string{"fakecli"}, VersionArgs: []string{"--version"},
		Category: CategoryCLI, Strategy: StrategyBinary,
	}}

	home := t.TempDir()
	writeAt(t, home, ".atomic/profile.md", stubProfile("")+"</workflow-preferences>\n")
	opts, _ := quietOpts(probeFor(home))
	opts.registry = reg
	if _, err := refresh(home, "2026-11-10", opts); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("a refused refresh still ran the tool probes")
	}

	healthy := t.TempDir()
	opts.probe = probeFor(healthy)
	if _, err := refresh(healthy, "2026-11-10", opts); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatal("the registry seam never reached DetectAll")
	}
}
