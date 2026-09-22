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

	analysis := analysisEnv(base, "/src/go.work")
	if slices.Contains(analysis, "GOTOOLCHAIN=go1.9") || !slices.Contains(analysis, "GOTOOLCHAIN=local") {
		t.Fatalf("analysis environment does not pin the toolchain: %v", analysis)
	}
	if slices.Contains(analysis, "GOWORK=/elsewhere/go.work") || !slices.Contains(analysis, "GOWORK=/src/go.work") {
		t.Fatalf("analysis environment does not pin the container's workspace: %v", analysis)
	}

	build := buildEnv(analysis)
	if !slices.Contains(build, "GOWORK=off") || slices.Contains(build, "GOWORK=/src/go.work") {
		t.Fatalf("helper builds must ignore a workspace: %v", build)
	}
}

// Natively, Go would search for a go.work past the source root and use one the
// target never declared. The container only sees declared inputs under the
// source, so the host must see exactly those too.
func TestWorkspaceMatchesWhatTheContainerImports(t *testing.T) {
	parent, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(parent, "repo")
	module := filepath.Join(source, "services", "api")
	writeTestFile(t, filepath.Join(parent, "go.work"), "go 1.27\n")
	writeTestFile(t, filepath.Join(module, "go.mod"), "module example.com/api\n")

	for _, tt := range []struct {
		name   string
		inputs []string
		file   string
		want   string
	}{
		{name: "no workspace under the source ignores one above it", inputs: []string{"."}, want: "off"},
		{name: "an undeclared workspace is not imported", inputs: []string{"services/api"}, file: "go.work", want: "off"},
		{name: "a declared root workspace is used", inputs: []string{"services/api", "go.work"}, file: "go.work", want: "go.work"},
		{name: "a whole-tree input declares the workspace", inputs: []string{"."}, file: "go.work", want: "go.work"},
		{name: "the nearest declared workspace wins", inputs: []string{"services"}, file: "services/go.work", want: "services/go.work"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_ = os.Remove(filepath.Join(source, "go.work"))
			_ = os.Remove(filepath.Join(source, "services", "go.work"))
			if tt.file != "" {
				writeTestFile(t, filepath.Join(source, filepath.FromSlash(tt.file)), "go 1.27\n")
			}
			req := Request{Source: source, PlannedCheck: PlannedCheck{Target: Target{Dir: "services/api", Inputs: tt.inputs}}}

			want := tt.want
			if want != "off" {
				want = filepath.Join(source, filepath.FromSlash(want))
			}
			if got := workspace(req, module); got != want {
				t.Fatalf("workspace = %q, want %q", got, want)
			}
		})
	}
}

// A shared Go check runs the shared checkout's linter, rule list and pinned
// tools, so two checkouts that differ only there must not share a result.
// Other kinds do not run them, and their keys must not move.
func TestSharedGoCheckKeyCoversTheRunner(t *testing.T) {
	checkouts := make([]string, 2)
	for i, checks := range []string{`{"checks":["all"]}`, `{"checks":["all","-SA5001"]}`} {
		checkouts[i] = t.TempDir()
		writeTestFile(t, filepath.Join(checkouts[i], "runner", "toolchain.json"), checks)
	}

	source := nativeRequest(t)
	keys := func(kind CheckKind, executor ExecutorKind) []string {
		t.Helper()
		out := make([]string, 0, len(checkouts))
		for _, shared := range checkouts {
			req := source
			req.Shared = shared
			req.Check = Check{Kind: kind, Target: "app", Environment: "host"}
			if kind == CheckCommand {
				req.Check.Command = &CommandCheck{Args: []string{"true"}}
			}
			req.Environment.Executor = executor
			key, err := fingerprint(req)
			if err != nil {
				t.Fatal(err)
			}
			out = append(out, key)
		}
		return out
	}

	for kind := range sharedGoChecks {
		for _, executor := range []ExecutorKind{ExecutorNative, ExecutorDagger} {
			if got := keys(kind, executor); got[0] == got[1] {
				t.Errorf("%s on %s: a different runner/toolchain.json kept the same key", kind, executor)
			}
		}
	}
	if got := keys(CheckCommand, ExecutorNative); got[0] != got[1] {
		t.Error("a command check's key depends on the shared runner it never runs")
	}
}

// Without a result cache, a check builds its tools into a directory of its
// own, and must remove it afterwards.
func TestNativeGoCheckWithoutACacheLeavesNoToolDirectory(t *testing.T) {
	temporary := t.TempDir()
	t.Setenv("TMPDIR", temporary)
	req := nativeRequest(t)
	req.Check = Check{Kind: CheckGoVet, Target: "app", Environment: "host"}
	writeTestFile(t, filepath.Join(req.Source, "go.mod"), "module example.com/tiny\n\ngo 1.27\n")
	writeTestFile(t, filepath.Join(req.Source, "tiny.go"), "package tiny\n")

	result := (&Native{}).Execute(t.Context(), req)
	if result.Status != StatusPassed {
		t.Fatalf("go vet on a clean module: %+v", result)
	}

	leftovers, err := filepath.Glob(filepath.Join(temporary, "levenshtein-tools-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(leftovers) != 0 {
		t.Fatalf("tool directories were left behind: %v", leftovers)
	}
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}
