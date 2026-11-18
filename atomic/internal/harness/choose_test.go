package harness

import (
	"reflect"
	"testing"
)

func TestChooseWithFlagNeverDetects(t *testing.T) {
	called := false
	detect := func() Detection { called = true; return Detection{} }

	got, err := Choose("omp", detect)
	if err != nil {
		t.Fatal(err)
	}
	if called {
		t.Error("Choose consulted detection although --harness was given")
	}
	if got.Kind != KindOMP {
		t.Errorf("Kind = %q, want omp", got.Kind)
	}
	if len(got.Evidence) != 0 {
		t.Errorf("Evidence = %v, want none: it names environment variables only", got.Evidence)
	}

	if _, err := Choose("bogus", detect); err == nil {
		t.Error("Choose accepted an unknown --harness value")
	}
	if called {
		t.Error("Choose consulted detection on an invalid flag")
	}
}

func TestChooseWithoutFlagUsesDetection(t *testing.T) {
	want := Detection{Kind: KindCodex, Confidence: ConfidenceWeak}
	got, err := Choose("", func() Detection { return want })
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("Choose = %+v, %v; want %+v", got, err, want)
	}
}
