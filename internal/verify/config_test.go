package verify

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestVersionedPlanningNeedsNoTools(t *testing.T) {
	t.Setenv("PATH", "")
	cfg, err := Parse([]byte(`{"version":1,"targets":{"app":{"dir":".","inputs":["."]}},"environments":{"go":{"executor":"dagger"}},"checks":{"lint":{"kind":"go-lint","target":"app","environment":"go"}},"runs":{"audit":{"checks":["lint"],"rerun_checks":true},"main":{"checks":["lint"]}}}`))
	if err != nil {
		t.Fatal(err)
	}
	plan, err := cfg.Plan(t.TempDir(), "audit")
	if err != nil || !plan.RerunChecks {
		t.Fatalf("plan: %+v %v", plan, err)
	}
	plan, err = cfg.Plan(t.TempDir(), "main")
	if err != nil || plan.RerunChecks {
		t.Fatalf("freshness still depends on name: %+v %v", plan, err)
	}
}

// Every rejected shape names a configuration mistake: no version, the retired
// module-list format, an unsupported version, a typo, an unknown or repeated
// check, an escaping target, an empty run, and trailing JSON.
func TestRejectInvalidConfiguration(t *testing.T) {
	const versioned = `{"version":1,"targets":{"app":{"dir":%q,"inputs":["."]}},"environments":{"go":{"executor":"dagger"}},"checks":{"lint":{"kind":"go-lint","target":"app","environment":"go"}},"runs":{"branch":{"checks":%s}}}`
	for _, input := range []string{
		`null`, `{}`, `{"version":2}`, `{"version":1,"typo":true}`,
		`{"modules":["."],"runs":{"branch":["go-lint"]}}`,
		fmt.Sprintf(versioned, ".", `["typo"]`),
		fmt.Sprintf(versioned, "..", `["lint"]`),
		fmt.Sprintf(versioned, ".", `["lint","lint"]`),
		fmt.Sprintf(versioned, ".", `[]`),
		fmt.Sprintf(versioned, ".", `["lint"]`) + ` {}`,
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
	cfg, err := Parse([]byte(`{"version":1,"targets":{"app":{"dir":"outside","inputs":["."]}},"environments":{"go":{"executor":"dagger"}},"checks":{"lint":{"kind":"go-lint","target":"app","environment":"go"}},"runs":{"branch":{"checks":["lint"]}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = cfg.Plan(source, "branch"); err == nil || !strings.Contains(err.Error(), "escapes") {
		t.Fatalf("accepted escaping symlink: %v", err)
	}
}

const (
	executorFake    ExecutorKind = "fake"
	executorMissing ExecutorKind = "missing"
)

type fakeExecutor struct {
	status Status
	calls  int
}

func (f *fakeExecutor) Execute(context.Context, Request) Result {
	f.calls++
	return Result{Status: f.status}
}

func TestAccountForEverySelectedCheck(t *testing.T) {
	plan := Plan{Checks: []PlannedCheck{{ID: "one", Environment: Environment{Executor: executorFake}}, {ID: "two", Environment: Environment{Executor: executorMissing}}}}
	executor := &fakeExecutor{status: StatusFailed}
	report := Execute(context.Background(), plan, "", map[ExecutorKind]Executor{executorFake: executor}, 0)
	if report.Status != StatusFailed || len(report.Results) != 2 || report.Results[1].Status != StatusIncomplete {
		t.Fatalf("lost required work: %+v", report)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	report = Execute(ctx, plan, "", map[ExecutorKind]Executor{executorFake: executor}, 0)
	if executor.calls != 1 || report.Results[0].Status != StatusCancelled {
		t.Fatalf("executed after cancellation: %+v", report)
	}
}

type orderedExecutor struct {
	mu    sync.Mutex
	delay map[string]time.Duration
	seen  []string
}

func (o *orderedExecutor) Execute(_ context.Context, req Request) Result {
	if delay := o.delay[req.ID]; delay > 0 {
		time.Sleep(delay)
	}

	o.mu.Lock()
	o.seen = append(o.seen, req.ID)
	o.mu.Unlock()
	return Result{Status: StatusPassed, Stdout: req.ID}
}

func TestParallelExecutePreservesPlanOrder(t *testing.T) {
	plan := Plan{
		Run: "branch",
		Checks: []PlannedCheck{
			{ID: "slow", Environment: Environment{Executor: executorFake}},
			{ID: "fast", Environment: Environment{Executor: executorFake}},
			{ID: "mid", Environment: Environment{Executor: executorFake}},
		},
	}
	executor := &orderedExecutor{delay: map[string]time.Duration{
		"slow": 80 * time.Millisecond,
		"fast": 5 * time.Millisecond,
		"mid":  20 * time.Millisecond,
	}}

	start := time.Now()
	report := Execute(context.Background(), plan, "", map[ExecutorKind]Executor{executorFake: executor}, 0)
	elapsed := time.Since(start)
	if report.Status != StatusPassed {
		t.Fatalf("status: %+v", report)
	}
	if elapsed >= 80*time.Millisecond+20*time.Millisecond {
		t.Fatalf("checks appear sequential: elapsed %s", elapsed)
	}
	for i, id := range []string{"slow", "fast", "mid"} {
		if report.Results[i].ID != id || report.Results[i].Stdout != id {
			t.Fatalf("results not in plan order: %+v", report.Results)
		}
	}
	if got := checkParallelism(1, 0); got != 1 {
		t.Fatalf("checkParallelism(1) = %d", got)
	}
	if got := checkParallelism(100, 0); got < 1 || got > maxCheckParallelism {
		t.Fatalf("checkParallelism(100) = %d", got)
	}
	if got := checkParallelism(100, maxCheckParallelism+3); got != maxCheckParallelism+3 {
		t.Fatalf("explicit jobs = %d", got)
	}
	if got := checkParallelism(2, 1); got != 1 {
		t.Fatalf("jobs cannot be exceeded: %d", got)
	}
}

type recordingNative struct {
	Native
	mu    sync.Mutex
	order []string
}

func (r *recordingNative) Execute(ctx context.Context, req Request) Result {
	result := r.Native.Execute(ctx, req)
	r.mu.Lock()
	r.order = append(r.order, req.ID)
	r.mu.Unlock()
	return result
}

func TestParallelExecuteSharedPreparationPreservesPlanOrder(t *testing.T) {
	source, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "lock"), []byte("1"), 0644); err != nil {
		t.Fatal(err)
	}
	prep := &Preparation{
		Command: []string{"/bin/sh", "-c", "mkdir -p .venv && printf ready > .venv/ok && sleep 0.05"},
		Inputs:  []string{"lock"},
		Outputs: []string{".venv"},
	}
	env := Environment{Executor: ExecutorNative, Identity: "shared-prep-parallel-fixture"}
	plan := Plan{
		Source: source,
		Checks: []PlannedCheck{
			{
				ID:          "lint",
				Check:       Check{Kind: CheckCommand, Command: &CommandCheck{Args: []string{"/bin/sh", "-c", "test -f .venv/ok"}}},
				Target:      Target{Dir: ".", Workspace: ".", Inputs: []string{"."}},
				Environment: env,
				Preparation: prep,
			},
			{
				ID:          "tests",
				Check:       Check{Kind: CheckCommand, Command: &CommandCheck{Args: []string{"/bin/sh", "-c", "test -f .venv/ok"}}},
				Target:      Target{Dir: ".", Workspace: ".", Inputs: []string{"."}},
				Environment: env,
				Preparation: prep,
			},
			{
				ID:          "other",
				Check:       Check{Kind: CheckCommand, Command: &CommandCheck{Args: []string{"true"}}},
				Target:      Target{Dir: ".", Workspace: ".", Inputs: []string{"."}},
				Environment: Environment{Executor: ExecutorNative},
			},
		},
	}
	native := &recordingNative{Native: Native{Cache: &Cache{Dir: t.TempDir()}}}
	report := Execute(context.Background(), plan, t.TempDir(), map[ExecutorKind]Executor{
		ExecutorNative: native,
	}, 0)
	if report.Status != StatusPassed {
		t.Fatalf("status: %+v", report)
	}
	if len(report.Results[0].Stages) == 0 || report.Results[0].Stages[0].Reused {
		t.Fatalf("lint should run preparation first: %+v", report.Results[0])
	}
	if len(report.Results[1].Stages) == 0 || !report.Results[1].Stages[0].Reused {
		t.Fatalf("tests should reuse preparation: %+v", report.Results[1])
	}

	native.mu.Lock()
	order := append([]string{}, native.order...)
	native.mu.Unlock()
	lintAt, testsAt := -1, -1
	for i, id := range order {
		if id == "lint" {
			lintAt = i
		}
	}
	for i, id := range order {
		if id == "tests" {
			testsAt = i
		}
	}
	if lintAt < 0 || testsAt < 0 || lintAt > testsAt {
		t.Fatalf("shared-prep checks ran out of plan order: %v", order)
	}
}

// A file written for the earlier version 1 layout gets a migration pointer,
// not just the decoder's field error.
func TestRetiredCheckFieldsGetAMigrationHint(t *testing.T) {
	for name, tc := range map[string]struct {
		check  string
		object string
	}{
		"command array":    {`{"kind":"command","target":"app","environment":"host","command":["go","test"]}`, `"command"`},
		"rerun_command":    {`{"kind":"command","target":"app","environment":"host","command":{"args":["go","test"]},"rerun_command":["go","test","-count=1"]}`, `"command"`},
		"semantic model":   {`{"kind":"semantic-lint","target":"app","environment":"host","model":"jev-1.13.0"}`, `"semantic"`},
		"semantic timeout": {`{"kind":"semantic-lint","target":"app","environment":"host","timeout":"2m"}`, `"semantic"`},
		"command timeout":  {`{"kind":"command","target":"app","environment":"host","command":{"args":["go","test"]},"timeout":"2m"}`, `"command"`},
		"dagger cache":     {`{"kind":"go-vuln","target":"app","environment":"host","cache":true}`, `always cached`},
		"semantic env":     {`{"kind":"semantic-lint","target":"app","environment":"host","env":{"FOO":"bar"}}`, `environment's "env"`},
	} {
		data := `{"version":1,"targets":{"app":{"dir":".","inputs":["."]}},"environments":{"host":{"executor":"native"}},"checks":{"c":` + tc.check + `},"runs":{"branch":{"checks":["c"]}}}`
		_, err := Parse([]byte(data))
		if err == nil || !strings.Contains(err.Error(), "docs/configuration.md") || !strings.Contains(err.Error(), tc.object) {
			t.Fatalf("%s: %v", name, err)
		}
	}
}
