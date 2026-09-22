package verify

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

func nativeGoEnvironment() Environment {
	return Environment{
		Executor: ExecutorNative,
		Identity: "worker-v1",
		Env:      map[string]string{"CI": "true"},
		PassEnv:  []string{"GOFLAGS"},
		Tools:    []Tool{{Command: []string{"go", "version"}, Version: "go1.27.1"}},
	}
}

// A native environment runs the shared Go kinds with its own options, and the
// kinds themselves take no options. self-test stays Dagger-only.
func TestNativeEnvironmentAcceptsSharedGoChecks(t *testing.T) {
	env := nativeGoEnvironment()
	for kind := range sharedGoChecks {
		t.Run(string(kind), func(t *testing.T) {
			if err := validateCheck(Check{Kind: kind}, env); err != nil {
				t.Fatalf("rejected a native shared Go check: %v", err)
			}
			for _, check := range []Check{
				{Kind: kind, Command: &CommandCheck{Args: []string{"true"}}},
				{Kind: kind, Semantic: &SemanticCheck{}},
			} {
				if err := validateCheck(check, env); err == nil {
					t.Errorf("accepted options that belong to another kind: %+v", check)
				}
			}
		})
	}

	if err := validateCheck(Check{Kind: CheckSelfTest}, env); err == nil {
		t.Fatal("self-test is Dagger-only")
	}
	for _, kind := range []CheckKind{CheckGoHTTP, CheckGoSQL} {
		if err := validateCheck(Check{Kind: kind}, env); err == nil {
			t.Errorf("%s is Dagger-only", kind)
		}
	}
}

// A Dagger environment is unchanged: it still rejects command objects and the
// native environment options.
func TestDaggerEnvironmentIsUnchanged(t *testing.T) {
	if err := validateCheck(Check{Kind: CheckGoLint}, Environment{Executor: ExecutorDagger}); err != nil {
		t.Fatal(err)
	}
	for _, env := range []Environment{
		{Executor: ExecutorDagger, Identity: "worker-v1"},
		{Executor: ExecutorDagger, Env: map[string]string{"CI": "true"}},
		{Executor: ExecutorDagger, PassEnv: []string{"GOFLAGS"}},
		{Executor: ExecutorDagger, Tools: []Tool{{Command: []string{"go"}, Version: "go1"}}},
	} {
		if err := validateCheck(Check{Kind: CheckGoLint}, env); err == nil {
			t.Errorf("Dagger accepted native environment options: %+v", env)
		}
	}
	if err := validateCheck(Check{Kind: CheckGoLint, Command: &CommandCheck{Args: []string{"true"}}}, Environment{Executor: ExecutorDagger}); err == nil {
		t.Fatal("Dagger accepted a command object")
	}
}

// A fresh run needs nothing declared for these kinds: they bypass their own
// analysis caches themselves.
func TestSharedGoChecksAreReadyForFreshRuns(t *testing.T) {
	for kind := range sharedGoChecks {
		if err := nativeKinds[kind].rerunReady(Check{Kind: kind}); err != nil {
			t.Errorf("%s: %v", kind, err)
		}
	}
}

// Result caching follows the kind, not a command object these checks do not
// have; go-vuln stays uncacheable whichever executor runs it.
func TestNativeGoChecksAreCacheableWithoutCommandOptions(t *testing.T) {
	req := nativeRequest(t)
	req.Check = Check{Kind: CheckGoLint, Target: "app", Environment: "host"}
	req.Target.Inputs = []string{"input"}
	if err := os.WriteFile(filepath.Join(req.Source, "input"), []byte("one"), 0600); err != nil {
		t.Fatal(err)
	}
	executor := &countingExecutor{status: StatusPassed}
	runner := CachedExecutor{Cache: &Cache{Dir: t.TempDir()}, Executor: executor}

	if first := runner.Execute(context.Background(), req); first.Cache.Status != CacheMiss || executor.calls != 1 {
		t.Fatalf("cold: %+v", first)
	}
	if second := runner.Execute(context.Background(), req); second.Cache.Status != CacheHit || executor.calls != 1 {
		t.Fatalf("warm: %+v", second)
	}

	req.Check.Kind = CheckGoVuln
	if result := runner.Execute(context.Background(), req); result.Cache.Status != CacheDisabled {
		t.Fatalf("advisory data was cached: %+v", result)
	}
}

// legacyFingerprint is the key shape before the host toolchain joined it. Every
// kind that does not depend on the host's Go must still produce this exact key,
// so existing cache entries survive.
func legacyFingerprint(t *testing.T, req Request) string {
	t.Helper()
	paths := append([]string{}, req.Target.Inputs...)
	slices.Sort(paths)
	source, err := snapshot(req.Source, paths, outputPaths(req), false)
	if err != nil {
		t.Fatal(err)
	}
	impl, err := implementation(req)
	if err != nil {
		t.Fatal(err)
	}

	req.RerunChecks = false
	var env []string
	if req.Environment.Executor == ExecutorNative {
		env = nativeEnv(req, req.Check.env())
	}
	return digest(struct {
		Check          PlannedCheck
		Source         string
		Implementation string
		OS             string
		Arch           string
		Env            []string
	}{req.PlannedCheck, source, impl, runtime.GOOS, runtime.GOARCH, env})
}

func TestNativeGoFingerprintIncludesTheHostToolchain(t *testing.T) {
	command := nativeRequest(t)

	// A command check's key is byte-identical to the one it had before.
	key, err := fingerprint(command)
	if err != nil {
		t.Fatal(err)
	}
	if want := legacyFingerprint(t, command); key != want {
		t.Fatalf("command check key changed: %q want %q", key, want)
	}

	// A shared Go check on the same environment must not share that key: the
	// host's Go decides the verdict and nothing else in the key names it.
	lint := command
	lint.Check = Check{Kind: CheckGoLint, Target: "app", Environment: "host"}
	lintKey, err := fingerprint(lint)
	if err != nil {
		t.Fatal(err)
	}
	if lintKey == legacyFingerprint(t, lint) {
		t.Fatal("native go-lint key does not depend on the host toolchain")
	}

	identity, err := toolchainIdentity(context.Background(), lint.Source, nativeEnv(lint, nil))
	if err != nil {
		t.Fatal(err)
	}
	if identity.Version == "" || identity.OS == "" || identity.Arch == "" {
		t.Fatalf("incomplete toolchain identity: %+v", identity)
	}
	if digest(identity) == digest(goToolchain{Version: identity.Version + "x", OS: identity.OS, Arch: identity.Arch}) {
		t.Fatal("toolchain identity ignores the version")
	}
}

func TestWorkflowArgumentsNameEveryFileAndOneConfig(t *testing.T) {
	source := t.TempDir()
	workflows := filepath.Join(source, ".github", "workflows")
	if err := os.MkdirAll(workflows, 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := workflowArguments(source, "actionlint"); err == nil {
		t.Fatal("accepted a repository with no workflows")
	}

	for _, name := range []string{"verify.yml", "release.yaml"} {
		if err := os.WriteFile(filepath.Join(workflows, name), []byte("on: push\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	args, err := workflowArguments(source, "actionlint")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"actionlint", "-shellcheck=", "-pyflakes=", ".github/workflows/release.yaml", ".github/workflows/verify.yml"}
	if strings.Join(args, " ") != strings.Join(want, " ") {
		t.Fatalf("arguments = %v, want %v", args, want)
	}

	if err := os.WriteFile(filepath.Join(source, ".github", "actionlint.yml"), []byte("self-hosted-runner:\n"), 0600); err != nil {
		t.Fatal(err)
	}
	args, err = workflowArguments(source, "actionlint")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(args, "-config-file") || !slices.Contains(args, ".github/actionlint.yml") {
		t.Fatalf("configuration was not passed: %v", args)
	}

	if err := os.WriteFile(filepath.Join(source, ".github", "actionlint.yaml"), []byte("self-hosted-runner:\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := workflowArguments(source, "actionlint"); err == nil {
		t.Fatal("accepted two configuration files")
	}
}

func TestGoEnvironmentPinsToolchainSelection(t *testing.T) {
	base := []string{"PATH=/bin", "GOTOOLCHAIN=go1.9", "GOWORK=/elsewhere/go.work"}

	analysis := analysisEnv(base)
	if slices.Contains(analysis, "GOTOOLCHAIN=go1.9") || !slices.Contains(analysis, "GOTOOLCHAIN=local") {
		t.Fatalf("analysis environment does not pin the toolchain: %v", analysis)
	}
	if !slices.Contains(analysis, "GOWORK=/elsewhere/go.work") {
		t.Fatalf("analysis environment dropped the consumer's workspace: %v", analysis)
	}

	build := buildEnv(base)
	if !slices.Contains(build, "GOWORK=off") || slices.Contains(build, "GOWORK=/elsewhere/go.work") {
		t.Fatalf("helper builds must ignore a workspace: %v", build)
	}
}
