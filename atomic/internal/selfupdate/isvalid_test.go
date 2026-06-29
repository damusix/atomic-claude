package selfupdate_test

import (
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/selfupdate"
)

func TestIsValidSemver(t *testing.T) {
	valid := []string{
		"1.0.0",
		"v1.0.0",
		"1.2.3",
		"0.0.0",
		"1.0.0-rc.1",
		"1.0.0+build.1",
	}
	for _, s := range valid {
		if !selfupdate.IsValidSemver(s) {
			t.Errorf("IsValidSemver(%q) = false, want true", s)
		}
	}

	malformed := []string{
		"",
		"bad",
		"1.0",
		"1",
		"abc.def.ghi",
		"1.0.x",
	}
	for _, s := range malformed {
		if selfupdate.IsValidSemver(s) {
			t.Errorf("IsValidSemver(%q) = true, want false", s)
		}
	}
}
