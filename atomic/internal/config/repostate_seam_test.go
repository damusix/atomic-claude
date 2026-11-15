package config

import (
	"errors"
	"strings"
	"testing"
)

// The state already moved when the card projection fails, so the error must
// point at the verb that retries only the projection.
func TestMigrateRepositoryStateNamesTheRetryWhenTheSeamFails(t *testing.T) {
	repo := legacyRepo(t)
	prev := AfterRepoStateMigration
	AfterRepoStateMigration = func(string, string) error { return errors.New("link conflict") }
	t.Cleanup(func() { AfterRepoStateMigration = prev })

	_, err := MigrateRepositoryState(repo)
	if err == nil || !strings.Contains(err.Error(), "atomic repo init") || !strings.Contains(err.Error(), "link conflict") {
		t.Fatalf("error = %v, want the cause and `atomic repo init` as the retry", err)
	}
}
