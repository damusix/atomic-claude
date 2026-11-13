package harness

import (
	"reflect"
	"testing"
)

func TestDetectConfidence(t *testing.T) {
	tests := []struct {
		name       string
		env        map[string]string
		wantKind   Kind
		wantConf   Confidence
		wantEvid   []string
		wantCandid []Kind
	}{
		{
			name:     "claude strong marker",
			env:      map[string]string{"CLAUDECODE": "1"},
			wantKind: KindClaude, wantConf: ConfidenceStrong,
			wantEvid: []string{"CLAUDECODE"},
		},
		{
			name:     "claude entrypoint marker",
			env:      map[string]string{"CLAUDE_CODE_ENTRYPOINT": "cli"},
			wantKind: KindClaude, wantConf: ConfidenceStrong,
			wantEvid: []string{"CLAUDE_CODE_ENTRYPOINT"},
		},
		{
			name:     "claude AI_AGENT prefix",
			env:      map[string]string{"AI_AGENT": "claude-code/2.1"},
			wantKind: KindClaude, wantConf: ConfidenceStrong,
			wantEvid: []string{"AI_AGENT"},
		},
		{
			name:     "AI_AGENT of another agent is no marker",
			env:      map[string]string{"AI_AGENT": "other"},
			wantConf: ConfidenceNone,
		},
		{
			name:     "CLAUDECODE other than 1 is no marker",
			env:      map[string]string{"CLAUDECODE": "0"},
			wantConf: ConfidenceNone,
		},
		{
			name:     "codex strong marker",
			env:      map[string]string{"CODEX_THREAD_ID": "t1"},
			wantKind: KindCodex, wantConf: ConfidenceStrong,
			wantEvid: []string{"CODEX_THREAD_ID"},
		},
		{
			name:     "codex CI marker",
			env:      map[string]string{"CODEX_CI": "1"},
			wantKind: KindCodex, wantConf: ConfidenceStrong,
			wantEvid: []string{"CODEX_CI"},
		},
		{
			name:       "claude plus codex is ambiguous",
			env:        map[string]string{"CLAUDECODE": "1", "CODEX_THREAD_ID": "t1"},
			wantConf:   ConfidenceAmbiguous,
			wantEvid:   []string{"CLAUDECODE", "CODEX_THREAD_ID"},
			wantCandid: []Kind{KindClaude, KindCodex},
		},
		{
			name:     "omp AGENT with CI is weak",
			env:      map[string]string{"AGENT": "1", "CI": "true"},
			wantKind: KindOMP, wantConf: ConfidenceWeak,
			wantEvid: []string{"AGENT", "CI"},
		},
		{
			name:       "weak markers of two kinds are ambiguous",
			env:        map[string]string{"CODEX_SANDBOX": "x", "AGENT": "1", "CI": "true"},
			wantConf:   ConfidenceAmbiguous,
			wantEvid:   []string{"CODEX_SANDBOX", "AGENT", "CI"},
			wantCandid: []Kind{KindCodex, KindOMP},
		},
		{
			name:     "AGENT alone is no marker",
			env:      map[string]string{"AGENT": "1"},
			wantConf: ConfidenceNone,
		},
		{
			name:     "codex sandbox is weak",
			env:      map[string]string{"CODEX_SANDBOX": "seatbelt"},
			wantKind: KindCodex, wantConf: ConfidenceWeak,
			wantEvid: []string{"CODEX_SANDBOX"},
		},
		{
			name:     "strong outranks weak",
			env:      map[string]string{"CLAUDECODE": "1", "CODEX_SANDBOX": "x"},
			wantKind: KindClaude, wantConf: ConfidenceStrong,
			wantEvid: []string{"CLAUDECODE"},
		},
		{
			name: "never-marker variables are ignored",
			env: map[string]string{
				"CLAUDE_PLUGIN_ROOT": "/x", "CLAUDE_PLUGIN_DATA": "/x",
				"CLAUDE_CONFIG_DIR": "/x", "CODEX_HOME": "/x",
				"PI_CODING_AGENT": "1", "PI_CODING_AGENT_DIR": "/x",
				"OMP_PROFILE": "p",
			},
			wantConf: ConfidenceNone,
		},
		{
			name:     "empty map is none",
			env:      map[string]string{},
			wantConf: ConfidenceNone,
		},
		{
			name:     "nil map is none",
			wantConf: ConfidenceNone,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Detect(tt.env)
			if got.Kind != tt.wantKind || got.Confidence != tt.wantConf {
				t.Errorf("Detect = %q/%q, want %q/%q", got.Kind, got.Confidence, tt.wantKind, tt.wantConf)
			}
			if !reflect.DeepEqual(got.Evidence, tt.wantEvid) {
				t.Errorf("Evidence = %v, want %v", got.Evidence, tt.wantEvid)
			}
			if !reflect.DeepEqual(got.Candidates, tt.wantCandid) {
				t.Errorf("Candidates = %v, want %v", got.Candidates, tt.wantCandid)
			}
		})
	}
}

func TestMarkerVarsAreMarkers(t *testing.T) {
	for _, name := range MarkerVars {
		env := map[string]string{name: "1"}
		switch name {
		case "AGENT", "CI":
			env = map[string]string{"AGENT": "1", "CI": "true"}
		case "AI_AGENT":
			env[name] = "claude-code"
		}
		if Detect(env).Confidence == ConfidenceNone {
			t.Errorf("%s alone is no marker; drop it from MarkerVars", name)
		}
	}
}

func TestDetectEvidenceCarriesNamesNeverValues(t *testing.T) {
	got := Detect(map[string]string{"CODEX_THREAD_ID": "secret-thread-value"})
	for _, e := range got.Evidence {
		if e == "secret-thread-value" {
			t.Fatalf("evidence leaks a value: %v", got.Evidence)
		}
	}
}

func TestDetectIgnoresAtomicHarness(t *testing.T) {
	for _, base := range []map[string]string{
		{},
		{"CLAUDECODE": "1"},
		{"CLAUDECODE": "1", "CODEX_CI": "1"},
		{"AGENT": "1", "CI": "true"},
	} {
		want := Detect(base)
		for _, v := range []string{"claude", "omp", "codex", "bogus"} {
			env := map[string]string{"ATOMIC_HARNESS": v}
			for k, val := range base {
				env[k] = val
			}
			if got := Detect(env); !reflect.DeepEqual(got, want) {
				t.Errorf("ATOMIC_HARNESS=%s changed %v: got %+v, want %+v", v, base, got, want)
			}
		}
	}
}
