package harness

import (
	"fmt"
	"os"
	"strings"
)

// Confidence grades a Detect result; the policy per grade is in
// docs/spec/omp-plugin-compatibility.md.
type Confidence string

const (
	ConfidenceStrong    Confidence = "strong"
	ConfidenceWeak      Confidence = "weak"
	ConfidenceAmbiguous Confidence = "ambiguous"
	ConfidenceNone      Confidence = "none"
)

// Detection is the outcome of Detect. Evidence holds variable names, never
// values; Candidates lists the kinds when more than one matched.
type Detection struct {
	Kind       Kind
	Confidence Confidence
	Evidence   []string
	Candidates []Kind
}

// marker is one row of the detection table: it is present when every variable
// it names matches, and those names become the evidence.
type marker struct {
	kind  Kind
	tier  Confidence
	conds []cond
}

type cond struct {
	name  string
	match func(value string) bool
}

func equals(want string) func(string) bool { return func(v string) bool { return v == want } }

func nonEmpty(v string) bool { return v != "" }

var markers = []marker{
	{KindClaude, ConfidenceStrong, []cond{{"CLAUDECODE", equals("1")}}},
	{KindClaude, ConfidenceStrong, []cond{{"CLAUDE_CODE_ENTRYPOINT", nonEmpty}}},
	{KindClaude, ConfidenceStrong, []cond{{"CLAUDE_CODE_SESSION_ID", nonEmpty}}},
	{KindClaude, ConfidenceStrong, []cond{{"AI_AGENT", func(v string) bool { return strings.HasPrefix(v, "claude-code") }}}},
	{KindCodex, ConfidenceStrong, []cond{{"CODEX_THREAD_ID", nonEmpty}}},
	{KindCodex, ConfidenceStrong, []cond{{"CODEX_CI", equals("1")}}},
	{KindCodex, ConfidenceWeak, []cond{{"CODEX_SANDBOX", nonEmpty}}},
	{KindOMP, ConfidenceWeak, []cond{{"AGENT", equals("1")}, {"CI", equals("true")}}},
}

// MarkerVars names every variable Detect reads, so a test can clear the
// markers of the harness session running it.
var MarkerVars = func() []string {
	var names []string
	for _, m := range markers {
		for _, c := range m.conds {
			names = append(names, c.name)
		}
	}
	return names
}()

// Detect reads only env, never process state.
func Detect(env map[string]string) Detection {
	found := map[Confidence]map[Kind][]string{ConfidenceStrong: {}, ConfidenceWeak: {}}
	for _, m := range markers {
		var names []string
		for _, c := range m.conds {
			if !c.match(env[c.name]) {
				names = nil
				break
			}
			names = append(names, c.name)
		}
		found[m.tier][m.kind] = append(found[m.tier][m.kind], names...)
	}
	if d, ok := classify(found[ConfidenceStrong], ConfidenceStrong); ok {
		return d
	}
	if d, ok := classify(found[ConfidenceWeak], ConfidenceWeak); ok {
		return d
	}
	return Detection{Confidence: ConfidenceNone}
}

// Choose never calls detect when flag is set.
func Choose(flag string, detect func() Detection) (Detection, error) {
	if flag == "" {
		return detect(), nil
	}
	k, err := ParseKind(flag)
	if err != nil {
		return Detection{}, err
	}
	return Detection{Kind: k, Confidence: ConfidenceStrong}, nil
}

// UnresolvedHarnessError is SelectAgentsHarness's refusal: no flag, no strong
// detection, and not exactly one enrolled harness.
type UnresolvedHarnessError struct {
	Detection Detection
	Enrolled  []Kind
}

func (e *UnresolvedHarnessError) Error() string {
	enrolled := "none"
	if len(e.Enrolled) > 0 {
		names := make([]string, len(e.Enrolled))
		for i, k := range e.Enrolled {
			names[i] = string(k)
		}
		enrolled = strings.Join(names, ", ")
	}
	return fmt.Sprintf("harness: cannot choose a harness (detection %s; enrolled: %s)", e.Detection.Confidence, enrolled)
}

// SelectAgentsHarness picks the harness `atomic config agents` acts on; no
// other verb may fall back to the single enrolled harness.
func SelectAgentsHarness(flag string, detect func() Detection, enrolled []Kind) (Kind, error) {
	d, err := Choose(flag, detect)
	if err != nil {
		return "", err
	}
	if d.Confidence == ConfidenceStrong {
		return d.Kind, nil
	}
	if len(enrolled) == 1 {
		return enrolled[0], nil
	}
	return "", &UnresolvedHarnessError{Detection: d, Enrolled: enrolled}
}

// Environ is the process environment as the map Detect reads.
func Environ() map[string]string {
	env := map[string]string{}
	for _, kv := range os.Environ() {
		if k, v, ok := strings.Cut(kv, "="); ok {
			env[k] = v
		}
	}
	return env
}

// Fixed kind order keeps Evidence and Candidates deterministic.
func classify(markers map[Kind][]string, tier Confidence) (Detection, bool) {
	var kinds []Kind
	var evidence []string
	for _, k := range []Kind{KindClaude, KindCodex, KindOMP} {
		if names := markers[k]; len(names) > 0 {
			kinds = append(kinds, k)
			evidence = append(evidence, names...)
		}
	}
	switch {
	case len(kinds) == 0:
		return Detection{}, false
	case len(kinds) == 1:
		return Detection{Kind: kinds[0], Confidence: tier, Evidence: evidence}, true
	default:
		return Detection{Confidence: ConfidenceAmbiguous, Evidence: evidence, Candidates: kinds}, true
	}
}
