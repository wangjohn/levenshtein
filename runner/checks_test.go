package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"slices"
	"strings"
	"testing"
)

// A direct Dagger call without a nonce could be answered with a stale verdict,
// so the checks whose state no source input covers refuse it before running.
func TestFreshChecksRequireANonce(t *testing.T) {
	for _, check := range []checkName{checkVuln, checkMod} {
		err := (&Levenshtein{}).SharedCheck(t.Context(), nil, string(check), ".", "")
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
// repository in must come from untrustedGoContainer.
func TestRepositoryCodeRunsWithUntrustedCaches(t *testing.T) {
	trusted := []string{"goContainer", "linter", "gochecker", "apidiffer"}
	for file, function := range map[string]string{"checks.go": "goTest", "gocheck.go": "goGenerate", "mutation.go": "runGremlins"} {
		calls := calledFunctions(t, file, function)
		if !slices.Contains(calls, "untrustedGoContainer") {
			t.Errorf("%s in %s must run in untrustedGoContainer; it calls %v", function, file, calls)
		}
		for _, name := range trusted {
			if slices.Contains(calls, name) {
				t.Errorf("%s in %s runs repository code but calls %s, whose caches build the tools", function, file, name)
			}
		}
	}
}

// Tool builds and the steps that run repository code mount the same Go cache
// directories, but never one volume in common.
func TestToolAndUntrustedCachesAreDisjoint(t *testing.T) {
	tools := toolchain{Go: "1.27.1"}
	trusted := goCaches(tools, cacheTools)
	untrusted := goCaches(tools, cacheUntrusted)
	if len(trusted) == 0 || len(trusted) != len(untrusted) {
		t.Fatalf("tools %v, untrusted %v", trusted, untrusted)
	}

	for i, cache := range trusted {
		if cache.Path != untrusted[i].Path {
			t.Errorf("both kinds of step must cache the same directories: %v and %v", cache, untrusted[i])
		}
		for _, other := range untrusted {
			if cache.Volume == other.Volume {
				t.Errorf("%s is mounted both where tools are built and where repository code runs", cache.Volume)
			}
		}
		if !strings.Contains(cache.Volume, tools.Go) || !strings.Contains(untrusted[i].Volume, tools.Go) {
			t.Errorf("cache volumes must be keyed by the Go version: %v, %v", cache, untrusted[i])
		}
	}
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
