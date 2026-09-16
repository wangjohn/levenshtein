package verify

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func nativeRequest(t *testing.T) Request {
	t.Helper()
	source, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return Request{Source: source, Shared: t.TempDir(), PlannedCheck: PlannedCheck{ID: "test", Check: Check{Kind: "command", Command: []string{"/bin/sh", "-c", "printf hello; printf warning >&2"}}, Target: Target{Dir: ".", Workspace: ".", Inputs: []string{"."}}, Environment: Environment{Executor: "native"}}}
}
func TestNativeCommandOutcomes(t *testing.T) {
	for _, tc := range []struct{ name, script, status string }{{"pass", "printf hello; printf warning >&2", "passed"}, {"assertion", "printf failure; exit 3", "failed"}} {
		t.Run(tc.name, func(t *testing.T) {
			req := nativeRequest(t)
			req.Check.Command = []string{"/bin/sh", "-c", tc.script}
			result := (&Native{}).Execute(context.Background(), req)
			if result.Status != tc.status || result.Stdout == "" {
				t.Fatalf("lost outcome: %+v", result)
			}
		})
	}
	req := nativeRequest(t)
	req.Check.Command = []string{"nonexistent-levenshtein-tool"}
	result := (&Native{}).Execute(context.Background(), req)
	if result.Status != "error" {
		t.Fatalf("missing tool: %+v", result)
	}
	req = nativeRequest(t)
	req.Environment.Tools = []Tool{{Command: []string{"/bin/sh", "-c", "printf actual"}, Version: "expected"}}
	result = (&Native{}).Execute(context.Background(), req)
	if result.Status != "error" || !strings.Contains(result.Error, "version mismatch") {
		t.Fatalf("tool pin: %+v", result)
	}
}
func TestNativeTimeoutKillsProcessGroup(t *testing.T) {
	req := nativeRequest(t)
	req.Check.Command = []string{"/bin/sh", "-c", "(sleep 1; touch escaped) & wait"}
	req.Check.Timeout = "30ms"
	result := (&Native{}).Execute(context.Background(), req)
	if result.Status != "error" || result.Error != "command timed out" {
		t.Fatalf("timeout: %+v", result)
	}
	time.Sleep(1200 * time.Millisecond)
	if _, err := os.Stat(filepath.Join(req.Source, "escaped")); !os.IsNotExist(err) {
		t.Fatal("descendant survived timeout")
	}
}
func TestNativeEnvironmentAndArtifacts(t *testing.T) {
	t.Setenv("UNDECLARED_VARIABLE", "must-not-leak")
	req := nativeRequest(t)
	req.Environment.Env = map[string]string{"VALUE": "configured"}
	req.Check.Env = map[string]string{"VALUE": "check"}
	req.Check.Command = []string{"/bin/sh", "-c", `test -z "$UNDECLARED_VARIABLE" && test "$VALUE" = check && test "$LEVENSHTEIN_FRESH" = true && printf report > artifact.txt`}
	req.Fresh = true
	req.Check.Artifacts = []string{"artifact.txt"}
	result := (&Native{}).Execute(context.Background(), req)
	if result.Status != "passed" {
		t.Fatalf("env/artifact: %+v", result)
	}
	req.Check.Artifacts = []string{"missing"}
	result = (&Native{}).Execute(context.Background(), req)
	if result.Status != "error" {
		t.Fatalf("missing artifact passed: %+v", result)
	}
}
func TestShareCompatiblePreparation(t *testing.T) {
	req := nativeRequest(t)
	req.Environment.Identity = "shared-preparation-fixture"
	req.Preparation = &Preparation{Command: []string{"/bin/sh", "-c", "echo prepare >> count; touch ready"}, Inputs: []string{"lock"}, Outputs: []string{"ready"}}
	req.Check.Command = []string{"/bin/sh", "-c", "test -f ready"}
	native := &Native{}
	for i := 0; i < 2; i++ {
		result := native.Execute(context.Background(), req)
		if result.Status != "passed" {
			t.Fatalf("preparation: %+v", result)
		}
	}
	data, err := os.ReadFile(filepath.Join(req.Source, "count"))
	if err != nil || string(data) != "prepare\n" {
		t.Fatalf("preparation repeated: %q %v", data, err)
	}
	if err := os.Remove(filepath.Join(req.Source, "ready")); err != nil {
		t.Fatal(err)
	}
	result := native.Execute(context.Background(), req)
	if result.Status != "passed" {
		t.Fatalf("missing preparation not restored: %+v", result)
	}
	data, _ = os.ReadFile(filepath.Join(req.Source, "count"))
	if string(data) != "prepare\nprepare\n" {
		t.Fatalf("missing output wrongly reused: %s", data)
	}
}
func TestNativeConfiguration(t *testing.T) {
	cfg, err := Parse([]byte(`{"version":1,"targets":{"app":{"dir":".","inputs":["."]}},"environments":{"host":{"executor":"native"}},"preparations":{"deps":{"command":["true"],"inputs":["lock"],"outputs":["env"]}},"checks":{"test":{"kind":"command","target":"app","environment":"host","command":["true"],"preparation":"deps"}},"runs":{"branch":{"checks":["test"]}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cfg.Plan(t.TempDir(), "branch"); err != nil {
		t.Fatal(err)
	}
	check := cfg.Checks["test"]
	check.Timeout = "0s"
	cfg.Checks["test"] = check
	if _, err := cfg.Plan(t.TempDir(), "branch"); err == nil {
		t.Fatal("invalid timeout accepted")
	}
}

func TestArtifactErrorRetainsOutput(t *testing.T) {
	req := nativeRequest(t)
	req.Check.Artifacts = []string{"missing"}
	result := (&Native{}).Execute(context.Background(), req)
	if result.Status != "error" || result.Stdout != "hello" || result.Stderr != "warning" {
		t.Fatalf("lost native diagnostics: %+v", result)
	}
}

func TestRejectPreparationOutputAliases(t *testing.T) {
	req := nativeRequest(t)
	if err := os.Mkdir(filepath.Join(req.Source, "real"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("real", filepath.Join(req.Source, "alias")); err != nil {
		t.Fatal(err)
	}
	req.Preparation = &Preparation{Command: []string{"/bin/sh", "-c", "touch alias/ready"}, Inputs: []string{"."}, Outputs: []string{"alias/ready"}}
	result := (&Native{}).Execute(context.Background(), req)
	if result.Status != "error" || !strings.Contains(result.Error, "symlink") {
		t.Fatalf("accepted aliased output: %+v", result)
	}
}

func TestFreshRunRequiresNativeFreshCommand(t *testing.T) {
	cfg := Config{Version: 1, Targets: map[string]Target{"app": {Dir: ".", Inputs: []string{"."}}}, Environments: map[string]Environment{"host": {Executor: "native"}}, Checks: map[string]Check{"test": {Kind: "command", Target: "app", Environment: "host", Command: []string{"true"}}}, Runs: map[string]Run{"audit": {Checks: []string{"test"}, Fresh: true}}}
	if _, err := cfg.Plan(t.TempDir(), "audit"); err == nil {
		t.Fatal("freshness was silently assumed for a generic command")
	}
	check := cfg.Checks["test"]
	check.FreshCommand = []string{"true"}
	cfg.Checks["test"] = check
	if _, err := cfg.Plan(t.TempDir(), "audit"); err != nil {
		t.Fatal(err)
	}
}
