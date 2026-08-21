package config

import "testing"

// ValidateDateSegment is the single enforcement point for a Created-date
// value before it becomes a path segment: a real calendar date passes and
// yields its 10-char prefix, while a too-short, malformed, or path-escape
// payload errors rather than sneaking a "../" segment past the date-shape
// check.
func TestValidateDateSegment(t *testing.T) {
	cases := []struct {
		name    string
		value   string
		wantErr bool
		want    string
	}{
		{"valid date-only", "2026-08-20", false, "2026-08-20"},
		{"valid RFC3339 timestamp", "2026-08-20T09:00:00Z", false, "2026-08-20"},
		{"too short", "2026-08", true, ""},
		{"empty", "", true, ""},
		{"invalid calendar date", "2026-13-40", true, ""},
		{"path escape", "../../../evil", true, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := ValidateDateSegment("bundle created date", c.value)
			if c.wantErr {
				if err == nil {
					t.Fatalf("ValidateDateSegment(%q) = (%q, nil), want error", c.value, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ValidateDateSegment(%q) unexpected error: %v", c.value, err)
			}
			if got != c.want {
				t.Errorf("ValidateDateSegment(%q) = %q, want %q", c.value, got, c.want)
			}
		})
	}
}
