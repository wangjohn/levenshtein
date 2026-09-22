//go:build integration

package verify

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func repositoryRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	return root
}

// fixtureRequest points a native shared Go check at one of the runner's own
// fixture modules, with this repository as the pinned shared checkout.
func fixtureRequest(t *testing.T, shared, fixture string, kind CheckKind) Request {
	t.Helper()
	source, err := filepath.EvalSymlinks(filepath.Join(shared, "runner", "testdata", fixture))
	if err != nil {
		t.Fatal(err)
	}

	return Request{
		Source: source,
		Shared: shared,
		PlannedCheck: PlannedCheck{
			ID:          string(kind),
			Check:       Check{Kind: kind, Target: "app", Environment: "host"},
			Target:      Target{Dir: ".", Workspace: ".", Inputs: []string{"."}},
			Environment: Environment{Executor: ExecutorNative},
		},
	}
}

func fixtureFindings(t *testing.T, result Result) []finding {
	t.Helper()
	var details struct {
		Findings []finding `json:"findings"`
	}
	if err := json.Unmarshal(result.Details, &details); err != nil {
		t.Fatalf("findings are not the Dagger path's shape: %v (%s)", err, result.Details)
	}
	return details.Findings
}

// The native executor has to reach the same verdicts as the pinned container on
// the fixtures the Dagger self-test uses, including the exact rule codes.
func TestNativeGoChecksAgreeWithTheFixtures(t *testing.T) {
	shared := repositoryRoot(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	native := &Native{Cache: &Cache{Dir: t.TempDir()}}

	t.Run("go-lint passes good code", func(t *testing.T) {
		result := native.Execute(ctx, fixtureRequest(t, shared, "good", CheckGoLint))
		if result.Status != StatusPassed {
			t.Fatalf("good fixture must pass: %+v", result)
		}
	})

	t.Run("go-lint fails bad code with its rule codes", func(t *testing.T) {
		result := native.Execute(ctx, fixtureRequest(t, shared, "bad", CheckGoLint))
		if result.Status != StatusFailed {
			t.Fatalf("bad fixture must fail for diagnostics, not a tool error: %+v", result)
		}

		counts := map[string]int{}
		for _, item := range fixtureFindings(t, result) {
			counts[item.Code]++
			if filepath.IsAbs(item.Location.File) || item.Location.Line < 1 {
				t.Errorf("finding location is not repository-relative: %+v", item.Location)
			}
		}
		for _, code := range []string{"SA5001", "SA5003", "SA9001", "LV1001", "LV1002", "errcheck", "exhaustive"} {
			if counts[code] < 1 {
				t.Errorf("bad fixture must produce a %s diagnostic; got %v", code, counts)
			}
		}
	})

	t.Run("go-lint refuses an empty module", func(t *testing.T) {
		result := native.Execute(ctx, fixtureRequest(t, shared, "empty", CheckGoLint))
		if result.Status != StatusError {
			t.Fatalf("empty module must error rather than pass: %+v", result)
		}
	})

	t.Run("go-lint leaves opt-in gocognit off by default", func(t *testing.T) {
		result := native.Execute(ctx, fixtureRequest(t, shared, "complexity", CheckGoLint))
		if result.Status != StatusPassed {
			t.Fatalf("complexity fixture must pass the shipped selection: %+v", result)
		}
	})

	t.Run("go-lint reports gocognit when the check adds it", func(t *testing.T) {
		req := fixtureRequest(t, shared, "complexity", CheckGoLint)
		req.Check.Lint = &LintCheck{Checks: []string{"gocognit"}}
		result := native.Execute(ctx, req)
		if result.Status != StatusFailed {
			t.Fatalf("complexity fixture must fail once gocognit is added: %+v", result)
		}

		findings := fixtureFindings(t, result)
		if len(findings) != 1 || findings[0].Code != "gocognit" || findings[0].Location.File != "complexity.go" {
			t.Fatalf("want one gocognit finding in complexity.go, got %+v", findings)
		}
	})

	t.Run("go-lint drops a default rule the check turns off", func(t *testing.T) {
		req := fixtureRequest(t, shared, "bad", CheckGoLint)
		req.Check.Lint = &LintCheck{Checks: []string{"-errcheck"}}
		result := native.Execute(ctx, req)
		if result.Status != StatusFailed {
			t.Fatalf("bad fixture must still fail for its other rules: %+v", result)
		}

		counts := map[string]int{}
		for _, item := range fixtureFindings(t, result) {
			counts[item.Code]++
		}
		if counts["errcheck"] != 0 || counts["SA5001"] < 1 {
			t.Fatalf("want errcheck off and the rest on; got %v", counts)
		}
	})

	t.Run("go-lint refuses an added rule the linter does not register", func(t *testing.T) {
		req := fixtureRequest(t, shared, "complexity", CheckGoLint)
		req.Check.Lint = &LintCheck{Checks: []string{"gocogint"}}
		result := native.Execute(ctx, req)
		if result.Status != StatusError || !strings.Contains(result.Error, `"gocogint" matches no rule`) {
			t.Fatalf("a misspelled rule must be an error, not a silent pass: %+v", result)
		}
	})

	t.Run("go-vet fails vet-bad", func(t *testing.T) {
		result := native.Execute(ctx, fixtureRequest(t, shared, "vet-bad", CheckGoVet))
		if result.Status != StatusFailed {
			t.Fatalf("vet-bad fixture must fail: %+v", result)
		}

		findings := fixtureFindings(t, result)
		if len(findings) != 1 || findings[0].Code != string(CheckGoVet) || findings[0].Message == "" {
			t.Fatalf("lost the vet diagnostic: %+v", findings)
		}
	})

	t.Run("go-vet passes good code", func(t *testing.T) {
		result := native.Execute(ctx, fixtureRequest(t, shared, "good", CheckGoVet))
		if result.Status != StatusPassed {
			t.Fatalf("good fixture must pass vet: %+v", result)
		}
	})
}
