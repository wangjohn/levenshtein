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

	t.Run("go-mod fails mod-untidy with tidy's diff", func(t *testing.T) {
		result := native.Execute(ctx, fixtureRequest(t, shared, "mod-untidy", CheckGoMod))
		if result.Status != StatusFailed {
			t.Fatalf("mod-untidy fixture must fail for its diff, not a tool error: %+v", result)
		}

		findings := fixtureFindings(t, result)
		if len(findings) != 1 || findings[0].Code != string(CheckGoMod) || !strings.Contains(findings[0].Message, "-require example.com/mod-untidy/unused v0.0.0") {
			t.Fatalf("lost the tidy diff: %+v", findings)
		}
	})

	// A module that declares no packages, like one that only pins tools, still
	// has manifests to check.
	for _, fixture := range []string{"mod-tidy", "empty"} {
		t.Run("go-mod passes "+fixture, func(t *testing.T) {
			result := native.Execute(ctx, fixtureRequest(t, shared, fixture, CheckGoMod))
			if result.Status != StatusPassed {
				t.Fatalf("%s fixture must pass go-mod: %+v", fixture, result)
			}
		})
	}

	t.Run("go-mod refuses a directory without go.mod", func(t *testing.T) {
		result := native.Execute(ctx, fixtureRequest(t, shared, "mutation", CheckGoMod))
		if result.Status != StatusError || !strings.Contains(result.Error, "needs a readable go.mod") {
			t.Fatalf("a missing go.mod must error rather than pass: %+v", result)
		}
	})

	// workflow-security downloads the pinned zizmor release, so these need
	// github.com, as the Dagger self-test does.
	t.Run("workflow-security passes workflow-secure", func(t *testing.T) {
		result := native.Execute(ctx, fixtureRequest(t, shared, "workflow-secure", CheckWorkflowSecurity))
		if result.Status != StatusPassed {
			t.Fatalf("workflow-secure fixture must pass workflow-security: %+v", result)
		}
	})

	t.Run("workflow-security fails workflow-insecure with zizmor's report", func(t *testing.T) {
		result := native.Execute(ctx, fixtureRequest(t, shared, "workflow-insecure", CheckWorkflowSecurity))
		if result.Status != StatusFailed {
			t.Fatalf("workflow-insecure fixture must fail for its finding, not a tool error: %+v", result)
		}

		findings := fixtureFindings(t, result)
		if len(findings) != 1 || findings[0].Code != string(CheckWorkflowSecurity) || !strings.Contains(findings[0].Message, "template-injection") || !strings.Contains(findings[0].Message, ".github/workflows/triage.yml:17") {
			t.Fatalf("lost zizmor's template-injection finding: %+v", findings)
		}
	})

	t.Run("workflow-security refuses a repository with nothing to audit", func(t *testing.T) {
		result := native.Execute(ctx, fixtureRequest(t, shared, "good", CheckWorkflowSecurity))
		if result.Status != StatusError || !strings.Contains(result.Error, "found no workflows") {
			t.Fatalf("nothing to audit must error rather than pass: %+v", result)
		}
	})
}
