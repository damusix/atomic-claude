package migrate_test

import (
	"errors"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/migrate"
)

// TestRunAppliesOnlyStepsAboveRecorded: out-of-order registry, non-empty
// recorded — only steps with TargetVersion > recorded run, in ascending order.
func TestRunAppliesOnlyStepsAboveRecorded(t *testing.T) {
	var order []string
	registry := []migrate.Migration{
		{TargetVersion: "2.0.0", Scope: "install", Up: func(*migrate.Context) error { order = append(order, "2.0.0"); return nil }},
		{TargetVersion: "1.0.0", Scope: "install", Up: func(*migrate.Context) error { order = append(order, "1.0.0"); return nil }},
		{TargetVersion: "3.0.0", Scope: "install", Up: func(*migrate.Context) error { order = append(order, "3.0.0"); return nil }},
	}
	ctx := &migrate.Context{Root: t.TempDir()}

	// recorded = "1.5.0" → only 2.0.0 and 3.0.0 run, in that order.
	got, err := migrate.Run("1.5.0", registry, ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "3.0.0" {
		t.Errorf("returned version: got %q, want %q", got, "3.0.0")
	}
	want := []string{"2.0.0", "3.0.0"}
	if len(order) != len(want) || order[0] != want[0] || order[1] != want[1] {
		t.Errorf("call order: got %v, want %v", order, want)
	}
}

// TestRunIdempotent: re-running with the new recorded version applies nothing.
func TestRunIdempotent(t *testing.T) {
	called := 0
	registry := []migrate.Migration{
		{TargetVersion: "1.0.0", Up: func(*migrate.Context) error { called++; return nil }},
	}
	ctx := &migrate.Context{Root: t.TempDir()}

	got, err := migrate.Run("1.0.0", registry, ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "1.0.0" {
		t.Errorf("returned version: got %q, want %q unchanged", got, "1.0.0")
	}
	if called != 0 {
		t.Errorf("Up called %d times on idempotent re-run, want 0", called)
	}
}

// TestRunFloorRunsAll: empty or "0.0.0" recorded is the floor — all steps run.
func TestRunFloorRunsAll(t *testing.T) {
	cases := []string{"", "0.0.0"}
	for _, rec := range cases {
		t.Run("recorded="+rec, func(t *testing.T) {
			var order []string
			registry := []migrate.Migration{
				{TargetVersion: "1.0.0", Up: func(*migrate.Context) error { order = append(order, "1.0.0"); return nil }},
				{TargetVersion: "2.0.0", Up: func(*migrate.Context) error { order = append(order, "2.0.0"); return nil }},
			}
			ctx := &migrate.Context{Root: t.TempDir()}

			got, err := migrate.Run(rec, registry, ctx)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != "2.0.0" {
				t.Errorf("returned version: got %q, want %q", got, "2.0.0")
			}
			if len(order) != 2 || order[0] != "1.0.0" || order[1] != "2.0.0" {
				t.Errorf("call order: got %v, want [1.0.0 2.0.0]", order)
			}
		})
	}
}

// TestRunStopsOnError: a failing step stops the chain; returned version is the
// last success, and subsequent steps do not run.
func TestRunStopsOnError(t *testing.T) {
	sentinel := errors.New("step 2 failed")
	var order []string
	registry := []migrate.Migration{
		{TargetVersion: "1.0.0", Up: func(*migrate.Context) error { order = append(order, "1.0.0"); return nil }},
		{TargetVersion: "2.0.0", Up: func(*migrate.Context) error { return sentinel }},
		{TargetVersion: "3.0.0", Up: func(*migrate.Context) error { order = append(order, "3.0.0"); return nil }},
	}
	ctx := &migrate.Context{Root: t.TempDir()}

	got, err := migrate.Run("0.0.0", registry, ctx)
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel error, got: %v", err)
	}
	// Version after last success.
	if got != "1.0.0" {
		t.Errorf("returned version after error: got %q, want %q", got, "1.0.0")
	}
	// Step 3.0.0 must not have run.
	for _, v := range order {
		if v == "3.0.0" {
			t.Errorf("step 3.0.0 ran despite error in step 2.0.0")
		}
	}
}

// TestRunEmptyRegistry: no steps — recorded is returned unchanged, no error.
func TestRunEmptyRegistry(t *testing.T) {
	ctx := &migrate.Context{Root: t.TempDir()}
	got, err := migrate.Run("1.2.3", nil, ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "1.2.3" {
		t.Errorf("empty registry: got %q, want %q unchanged", got, "1.2.3")
	}
}

// TestRunDoesNotMutateCallerSlice: Run sorts a copy — caller's slice unchanged.
func TestRunDoesNotMutateCallerSlice(t *testing.T) {
	registry := []migrate.Migration{
		{TargetVersion: "3.0.0", Up: func(*migrate.Context) error { return nil }},
		{TargetVersion: "1.0.0", Up: func(*migrate.Context) error { return nil }},
		{TargetVersion: "2.0.0", Up: func(*migrate.Context) error { return nil }},
	}
	original := []string{registry[0].TargetVersion, registry[1].TargetVersion, registry[2].TargetVersion}

	ctx := &migrate.Context{Root: t.TempDir()}
	_, _ = migrate.Run("", registry, ctx)

	for i, m := range registry {
		if m.TargetVersion != original[i] {
			t.Errorf("caller slice mutated at index %d: got %q, want %q", i, m.TargetVersion, original[i])
		}
	}
}

// TestRunPrereleaseSorting: prerelease < release per semver 2.0.
func TestRunPrereleaseSorting(t *testing.T) {
	var order []string
	registry := []migrate.Migration{
		{TargetVersion: "1.0.0", Up: func(*migrate.Context) error { order = append(order, "1.0.0"); return nil }},
		{TargetVersion: "1.0.0-rc.1", Up: func(*migrate.Context) error { order = append(order, "1.0.0-rc.1"); return nil }},
	}
	ctx := &migrate.Context{Root: t.TempDir()}

	_, err := migrate.Run("", registry, ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// rc.1 < release, so rc.1 runs first.
	if len(order) != 2 || order[0] != "1.0.0-rc.1" || order[1] != "1.0.0" {
		t.Errorf("prerelease order: got %v, want [1.0.0-rc.1 1.0.0]", order)
	}
}
