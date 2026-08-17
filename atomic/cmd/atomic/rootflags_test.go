package main

import "testing"

// TestScanRepoOverride covers the pre-scan that makes the global --repo
// override actually take effect (cli-repo-flag-never-parses): every leaf
// command sets DisableFlagParsing:true, so Cobra's own persistent-flag
// parsing of --repo is a no-op regardless of position — this scan is the
// only place --repo is read.
func TestScanRepoOverride(t *testing.T) {
	cases := []struct {
		name      string
		argv      []string
		wantValue string
		wantArgs  []string
		wantErr   bool
	}{
		{
			name:      "before the verb",
			argv:      []string{"atomic", "--repo", "/tmp/other", "signals", "show"},
			wantValue: "/tmp/other",
			wantArgs:  []string{"atomic", "signals", "show"},
		},
		{
			name:      "after the verb",
			argv:      []string{"atomic", "signals", "show", "--repo", "/tmp/other"},
			wantValue: "/tmp/other",
			wantArgs:  []string{"atomic", "signals", "show"},
		},
		{
			name:      "equals form",
			argv:      []string{"atomic", "signals", "show", "--repo=/tmp/other"},
			wantValue: "/tmp/other",
			wantArgs:  []string{"atomic", "signals", "show"},
		},
		{
			name:      "own flags survive alongside --repo",
			argv:      []string{"atomic", "wiki", "scan", "--root", "/tmp/x", "--repo", "/tmp/other"},
			wantValue: "/tmp/other",
			wantArgs:  []string{"atomic", "wiki", "scan", "--root", "/tmp/x"},
		},
		{
			name:      "absent",
			argv:      []string{"atomic", "signals", "show"},
			wantValue: "",
			wantArgs:  []string{"atomic", "signals", "show"},
		},
		{
			name:    "missing value at end of argv",
			argv:    []string{"atomic", "signals", "show", "--repo"},
			wantErr: true,
		},
		{
			name:    "missing value: next token is another flag",
			argv:    []string{"atomic", "--repo", "--json", "signals", "show"},
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			value, cleaned, err := scanRepoOverride(tc.argv)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected an error, got value=%q cleaned=%v", value, cleaned)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if value != tc.wantValue {
				t.Errorf("value = %q, want %q", value, tc.wantValue)
			}
			if len(cleaned) != len(tc.wantArgs) {
				t.Fatalf("cleaned = %v, want %v", cleaned, tc.wantArgs)
			}
			for i, a := range cleaned {
				if a != tc.wantArgs[i] {
					t.Errorf("cleaned[%d] = %q, want %q", i, a, tc.wantArgs[i])
				}
			}
		})
	}
}

// TestRepoFlagExempt verifies that migrate, config resolve, and wiki stamp —
// the three verbs whose own --repo flag already carries different,
// established semantics — are detected as exempt from the global pre-scan,
// while every other verb (including ones that also take their own flags, or
// share a top-level name with an exempt verb's parent) is not.
func TestRepoFlagExempt(t *testing.T) {
	cases := []struct {
		name string
		argv []string
		want bool
	}{
		{"migrate alone", []string{"migrate"}, true},
		{"migrate with its own --repo", []string{"migrate", "--repo", "/x"}, true},
		{"migrate with --realm", []string{"migrate", "--realm", "/x"}, true},
		{"config resolve", []string{"config", "resolve", "--repo", "/x", "--json"}, true},
		{"wiki stamp with positional file before flags", []string{"wiki", "stamp", "f.md", "--repo", "/x"}, true},
		{"config get is not exempt", []string{"config", "get", "some.key"}, false},
		{"wiki scan is not exempt", []string{"wiki", "scan", "--root", "/x"}, false},
		{"signals is not exempt", []string{"signals", "show"}, false},
		{"code is not exempt", []string{"code", "status", "--repo", "/x"}, false},
		{"empty argv", []string{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := repoFlagExempt(tc.argv); got != tc.want {
				t.Errorf("repoFlagExempt(%v) = %v, want %v", tc.argv, got, tc.want)
			}
		})
	}
}
