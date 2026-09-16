package verify

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLegacyTranslation(t *testing.T) {
	cfg, err := Parse([]byte(`{"modules":["."],"runs":{"custom":["go-lint","self-test"],"main":["go-lint"]}}`))
	if err != nil {
		t.Fatal(err)
	}
	plan, err := cfg.Plan(t.TempDir(), "custom")
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Checks) != 2 || plan.Fresh {
		t.Fatalf("incorrect plan: %+v", plan)
	}
	plan, err = cfg.Plan(t.TempDir(), "main")
	if err != nil || !plan.Fresh {
		t.Fatalf("lost main freshness: %+v %v", plan, err)
	}
}
func TestVersionedPlanningNeedsNoTools(t *testing.T) {
	t.Setenv("PATH", "")
	cfg, err := Parse([]byte(`{"version":1,"targets":{"app":{"dir":".","inputs":["."]}},"environments":{"go":{"executor":"dagger"}},"checks":{"lint":{"kind":"go-lint","target":"app","environment":"go"}},"runs":{"audit":{"checks":["lint"],"fresh":true},"main":{"checks":["lint"]}}}`))
	if err != nil {
		t.Fatal(err)
	}
	plan, err := cfg.Plan(t.TempDir(), "audit")
	if err != nil || !plan.Fresh {
		t.Fatalf("plan: %+v %v", plan, err)
	}
	plan, err = cfg.Plan(t.TempDir(), "main")
	if err != nil || plan.Fresh {
		t.Fatalf("freshness still depends on name: %+v %v", plan, err)
	}
}
func TestRejectInvalidConfiguration(t *testing.T) {
	for _, input := range []string{
		`null`, `{}`, `{"version":2}`, `{"version":1,"typo":true}`, `{"modules":["."],"runs":{"branch":["typo"]}}`,
		`{"modules":[".."],"runs":{"branch":["go-lint"]}}`, `{"modules":[".","."],"runs":{"branch":["go-lint"]}}`,
		`{"modules":["."],"runs":{"branch":["go-lint","go-lint"]}}`, `{"modules":["."],"runs":{"branch":[]}}`,
		`{"modules":["."],"runs":{"branch":["go-lint"]}} {}`,
	} {
		t.Run(input, func(t *testing.T) {
			cfg, err := Parse([]byte(input))
			if err == nil {
				_, err = cfg.Plan(t.TempDir(), "branch")
			}
			if err == nil {
				t.Fatal("accepted invalid configuration")
			}
		})
	}
}
func TestTargetCannotEscapeRepository(t *testing.T) {
	source := t.TempDir()
	if err := os.Symlink(t.TempDir(), filepath.Join(source, "outside")); err != nil {
		t.Fatal(err)
	}
	cfg, err := Parse([]byte(`{"modules":["outside"],"runs":{"branch":["go-lint"]}}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = cfg.Plan(source, "branch"); err == nil || !strings.Contains(err.Error(), "escapes") {
		t.Fatalf("accepted escaping symlink: %v", err)
	}
}

type fakeExecutor struct {
	status string
	calls  int
}

func (f *fakeExecutor) Execute(context.Context, Request) Result {
	f.calls++
	return Result{Status: f.status}
}
func TestAccountForEverySelectedCheck(t *testing.T) {
	plan := Plan{Checks: []PlannedCheck{{ID: "one", Environment: Environment{Executor: "fake"}}, {ID: "two", Environment: Environment{Executor: "missing"}}}}
	executor := &fakeExecutor{status: "failed"}
	report := Execute(context.Background(), plan, "", map[string]Executor{"fake": executor})
	if report.Status != "failed" || len(report.Results) != 2 || report.Results[1].Status != "incomplete" {
		t.Fatalf("lost required work: %+v", report)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	report = Execute(ctx, plan, "", map[string]Executor{"fake": executor})
	if executor.calls != 1 || report.Results[0].Status != "cancelled" {
		t.Fatalf("executed after cancellation: %+v", report)
	}
}
