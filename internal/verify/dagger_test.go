package verify

import (
	"context"
	"errors"
	"testing"

	"github.com/vektah/gqlparser/v2/gqlerror"
)

func TestDaggerResultSeparatesLintFromInfrastructureFailures(t *testing.T) {
	for _, tc := range []struct {
		name, status string
		err          error
	}{
		{"pass", "passed", nil},
		{"transport", "error", errors.New("engine unavailable")},
		{"compile", "error", &gqlerror.Error{Message: "undefined: missing"}},
		{"empty diagnostics", "error", &gqlerror.Error{Extensions: map[string]any{"levenshteinFindings": []any{}}}},
		{"malformed diagnostics", "error", &gqlerror.Error{Extensions: map[string]any{"levenshteinFindings": "bad"}}},
		{"lint", "failed", gqlerror.List{&gqlerror.Error{Message: "lint failed", Extensions: map[string]any{"levenshteinFindings": []any{map[string]any{"code": "SA5001"}}}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := daggerResult(tc.err)
			if got.Status != tc.status {
				t.Fatalf("%+v", got)
			}
			if tc.err != nil && got.Error == "" {
				t.Fatal("lost failure details")
			}
			if tc.status == "failed" && len(got.Details) == 0 {
				t.Fatal("lost diagnostics")
			}
		})
	}
}

func TestDaggerRejectsUnknownCheckBeforeStartingEngine(t *testing.T) {
	runner := &Dagger{}
	result := runner.Execute(context.Background(), Request{PlannedCheck: PlannedCheck{Check: Check{Kind: "typo"}}})
	if result.Status != "error" || runner.client != nil {
		t.Fatalf("%+v", result)
	}
}

func TestDaggerToolFailureKeepsOutput(t *testing.T) {
	got := daggerResult(gqlerror.List{&gqlerror.Error{
		Message:    "process exited 1",
		Extensions: map[string]any{"stdout": "loading packages", "stderr": "no required module provides package example.invalid/missing"},
	}})
	if got.Status != "error" || got.Stdout != "loading packages" || got.Stderr != "no required module provides package example.invalid/missing" {
		t.Fatalf("lost tool output: %+v", got)
	}
}
