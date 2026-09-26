package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"dagger/levenshtein/internal/checktool"
)

// A direct Dagger call without a nonce could be answered with a stale verdict,
// so the checks whose state no source input covers refuse it before running.
func TestFreshChecksRequireANonce(t *testing.T) {
	for _, check := range []checkName{checkVuln, checkMod} {
		err := (&Levenshtein{}).SharedCheck(t.Context(), nil, string(check), ".", "", "")
		if err == nil || !strings.Contains(err.Error(), string(check)+" requires a unique nonce") {
			t.Fatalf("%s ran without a nonce: %v", check, err)
		}
	}
}

// The steps that run the repository's own code, as root, must do it in a
// container whose Go caches no tool is ever built from: otherwise a test or a
// generator could rewrite, say, Staticcheck's source in the module cache and
// neuter every later linter build on a persistent engine. Each of these
// functions may take a binary built elsewhere, but the container it runs the
// repository in must come from untrustedGoContainer, and it must verify the
// module cache there before it runs anything.
func TestRepositoryCodeRunsWithUntrustedCaches(t *testing.T) {
	trusted := []string{"goContainer", "linter", "gochecker", "apidiffer"}
	for file, function := range repositorySteps {
		calls := calledFunctions(t, file, function)
		for _, name := range []string{"untrustedGoContainer", "verifyModuleCache"} {
			if !slices.Contains(calls, name) {
				t.Errorf("%s in %s must call %s; it calls %v", function, file, name, calls)
			}
		}
		for _, name := range trusted {
			if slices.Contains(calls, name) {
				t.Errorf("%s in %s runs repository code but calls %s, whose caches build the tools", function, file, name)
			}
		}
	}
}

// repositorySteps are the functions that run a repository's own code, by file.
var repositorySteps = map[string]string{"checks.go": "goTest", "gocheck.go": "goGenerate", "mutation.go": "runGremlins"}

// Only the steps that run repository code, and the self-test that tampers a
// module cache on purpose, may mount untrusted caches: every other container,
// and so every tool build, mounts the tool caches alone.
func TestOnlyRepositoryStepsMountUntrustedCaches(t *testing.T) {
	allowed := map[string][]string{
		"untrustedCaches":      {"untrustedGoContainer"},
		"untrustedGoContainer": {"goTest", "goGenerate", "runGremlins", "moduleCacheSelfTest"},
	}

	callers := callersIn(t, ".")

	for callee, want := range allowed {
		for _, caller := range callers[callee] {
			if !slices.Contains(want, caller) {
				t.Errorf("%s calls %s; only %v may", caller, callee, want)
			}
		}
	}
	for _, function := range repositorySteps {
		if !slices.Contains(callers["untrustedGoContainer"], function) {
			t.Errorf("%s no longer calls untrustedGoContainer, so this test checks nothing about it", function)
		}
	}
}

// Tool builds and the steps that run repository code mount the same Go cache
// directories, but never one volume in common, and each repository's steps
// mount volumes of their own.
func TestToolAndUntrustedCachesAreDisjoint(t *testing.T) {
	tools := toolchain{Go: "1.27.1"}
	one, err := repositoryScope(strings.Repeat("a1", 32))
	if err != nil {
		t.Fatal(err)
	}
	other, err := repositoryScope(strings.Repeat("b2", 32))
	if err != nil {
		t.Fatal(err)
	}

	trusted := toolCaches(tools)
	scopes := [][]goCache{
		untrustedCaches(tools, one),
		untrustedCaches(tools, other),
		untrustedCaches(tools, scopeUnkeyed),
		untrustedCaches(tools, scopeSelfTest),
		untrustedCaches(tools, scopeTamperSelfTest),
	}

	if len(trusted) == 0 {
		t.Fatal("no tool caches")
	}
	seen := map[string]bool{}
	for _, cache := range trusted {
		seen[cache.Volume] = true
		if !strings.Contains(cache.Volume, tools.Go) {
			t.Errorf("cache volumes must be keyed by the Go version: %v", cache)
		}
	}
	for _, untrusted := range scopes {
		if len(untrusted) != len(trusted) {
			t.Fatalf("tools %v, untrusted %v", trusted, untrusted)
		}
		for i, cache := range untrusted {
			if cache.Path != trusted[i].Path {
				t.Errorf("both kinds of step must cache the same directories: %v and %v", trusted[i], cache)
			}
			if seen[cache.Volume] {
				t.Errorf("%s is mounted by two scopes, or where tools are built", cache.Volume)
			}
			seen[cache.Volume] = true
			if !strings.Contains(cache.Volume, tools.Go) {
				t.Errorf("cache volumes must be keyed by the Go version: %v", cache)
			}
		}
	}
	for _, cache := range untrustedCaches(tools, one) {
		if !strings.HasSuffix(cache.Volume, "-"+strings.Repeat("a1", 8)) {
			t.Errorf("a repository's volume must end in its key's first 16 hex digits: %s", cache.Volume)
		}
	}
}

// A cacheKey names volumes, so only the SHA-256 the CLI derives is accepted:
// anything else could reach another scope's volumes or a fixed one's.
func TestRepositoryScopeAcceptsOnlyAKey(t *testing.T) {
	key := strings.Repeat("0f", 32)

	scope, err := repositoryScope(key)
	if err != nil || scope != cacheScope(key[:16]) {
		t.Errorf("repositoryScope(%q) = %q, %v", key, scope, err)
	}
	if scope, err := repositoryScope(""); err != nil || scope != scopeUnkeyed {
		t.Errorf("no key must mean %q, got %q, %v", scopeUnkeyed, scope, err)
	}
	for _, bad := range []string{"self-test", "unkeyed", key[:16], strings.ToUpper(key), key + "0", strings.Repeat("0g", 32), "../" + key[3:]} {
		if scope, err := repositoryScope(bad); err == nil {
			t.Errorf("repositoryScope(%q) = %q, want an error", bad, scope)
		}
	}
}

// A module cache go mod verify finds modified is an error that says so, a
// failure for another reason is an error that keeps go's output, and only a
// clean run lets the step go on.
func TestModuleCacheErrorNeverPassesAFailedVerification(t *testing.T) {
	modified := checktool.Run{ExitCode: 1, Stderr: "example.com/cached v1.0.0: dir has been modified (/go/pkg/mod/example.com/cached@v1.0.0)\n"}
	missing := checktool.Run{ExitCode: 1, Stderr: "example.com/cached v1.0.0: missing ziphash: open /go/pkg/mod/cache/download/example.com/cached/@v/v1.0.0.ziphash: no such file or directory\n"}
	other := checktool.Run{ExitCode: 1, Stderr: "go: example.com/cached@v1.0.0: missing go.sum entry for go.mod file\n"}

	if err := moduleCacheError(".", checktool.Run{Stdout: "all modules verified\n"}); err != nil {
		t.Errorf("a clean verification must pass: %v", err)
	}
	for _, run := range []checktool.Run{modified, missing} {
		err := moduleCacheError(".", run)
		if err == nil || !strings.HasPrefix(err.Error(), "the Go module cache was modified since download; ") || !strings.Contains(err.Error(), strings.TrimSpace(run.Stderr)) {
			t.Errorf("%q must be a modified-cache error with go's output, got %v", run.Stderr, err)
		}
	}
	err := moduleCacheError("sub", other)
	if err == nil || strings.Contains(err.Error(), "modified since download") || !strings.Contains(err.Error(), "missing go.sum entry") {
		t.Errorf("another failure must be an error with go's output, got %v", err)
	}
}

// callersIn maps each plain function name called in dir's non-test files to
// the top-level functions that call it.
func callersIn(t *testing.T, dir string) map[string][]string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		t.Fatal(err)
	}

	callers := map[string][]string{}
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), file, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range parsed.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			ast.Inspect(fn.Body, func(node ast.Node) bool {
				if call, ok := node.(*ast.CallExpr); ok {
					if name, ok := call.Fun.(*ast.Ident); ok {
						callers[name.Name] = append(callers[name.Name], fn.Name.Name)
					}
				}
				return true
			})
		}
	}
	return callers
}

// calledFunctions lists the plain function names one top-level function in
// file calls.
func calledFunctions(t *testing.T, file, function string) []string {
	t.Helper()
	parsed, err := parser.ParseFile(token.NewFileSet(), file, nil, 0)
	if err != nil {
		t.Fatal(err)
	}

	var calls []string
	for _, decl := range parsed.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv != nil || fn.Name.Name != function {
			continue
		}
		ast.Inspect(fn.Body, func(node ast.Node) bool {
			if call, ok := node.(*ast.CallExpr); ok {
				if name, ok := call.Fun.(*ast.Ident); ok {
					calls = append(calls, name.Name)
				}
			}
			return true
		})
		return calls
	}
	t.Fatalf("%s declares no function %s", file, function)
	return nil
}
