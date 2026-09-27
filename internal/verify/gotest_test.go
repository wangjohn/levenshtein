package verify

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The native check turns a failing package into a finding and reports go
// test's text, not the event stream it parsed.
func TestNativeGoTestReportsFailuresAsText(t *testing.T) {
	t.Parallel()
	dir, err := filepath.Abs(filepath.Join("..", "..", "runner", "testdata", "test-fail"))
	if err != nil {
		t.Fatal(err)
	}
	req := Request{PlannedCheck: PlannedCheck{Target: Target{Dir: "test-fail"}}, RerunChecks: true}

	findings, run, err := (&Native{}).goTest(context.Background(), req, goRun{Dir: dir, Env: os.Environ()})
	if err != nil {
		t.Fatal(err)
	}

	if len(findings) != 1 || findings[0].Code != string(CheckGoTest) {
		t.Fatalf("a failing package must be one go-test finding: %+v", findings)
	}
	if !strings.Contains(run.Stdout, "--- FAIL") || strings.HasPrefix(strings.TrimSpace(run.Stdout), "{") {
		t.Fatalf("the report must keep go test's text, not its JSON events:\n%s", run.Stdout)
	}
}
