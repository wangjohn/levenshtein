package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"dagger/levenshtein/internal/checktool"
	"dagger/levenshtein/internal/dagger"

	"github.com/vektah/gqlparser/v2/gqlerror"
)

type checkName string

const (
	checkLint             checkName = "go-lint"
	checkVet              checkName = "go-vet"
	checkMod              checkName = "go-mod"
	checkTest             checkName = "go-test"
	checkHTTP             checkName = "go-http"
	checkSQL              checkName = "go-sql"
	checkVuln             checkName = "go-vuln"
	checkWorkflow         checkName = "workflow-lint"
	checkWorkflowSecurity checkName = "workflow-security"
	checkShellLint        checkName = "shell-lint"
	checkSecrets          checkName = "secrets"
	checkDepsVuln         checkName = "deps-vuln"
	checkSelfTest         checkName = "self-test"
)

func knownCheck(check checkName) bool {
	switch check {
	case checkLint, checkVet, checkMod, checkTest, checkHTTP, checkSQL, checkVuln, checkWorkflow, checkWorkflowSecurity, checkShellLint, checkSecrets, checkDepsVuln, checkSelfTest:
		return true
	case checkImports, checkGenerate, checkApidiff:
		return false // Each has its own function.
	}
	return false
}

// Code that can write a Go module cache can change what every later build in
// it compiles: Go checks the extracted module source against go.sum only when
// it downloads it. So the Go cache volumes are split by whose code has run
// with them mounted. Tools volumes build the linters and the other pinned
// tools, and back checks that only read the repository. Untrusted volumes back
// the steps that run a repository's own code as root: its tests, its
// generators, and mutation testing. Each repository gets untrusted volumes of
// its own, named by a cacheScope, so one repository's code cannot change what
// another's steps compile on a persistent engine.

// cacheScope names one set of untrusted cache volumes: a repository's, from
// the key the CLI derives from its clone, or one of the fixed scopes below.
type cacheScope string

const (
	// scopeUnkeyed backs a direct Dagger call that passes no cacheKey. Every
	// such call on an engine shares it, whatever repository it runs.
	scopeUnkeyed cacheScope = "unkeyed"
	// scopeSelfTest backs the self-test's own fixtures, apart from every
	// repository and every direct call.
	scopeSelfTest cacheScope = "self-test"
	// scopeTamperSelfTest backs the self-test that edits a module cache on
	// purpose, so that edit never reaches another scope's volumes.
	scopeTamperSelfTest cacheScope = "self-test-tamper"
)

// cacheKeyLength is the length of the SHA-256, in hex, a cacheKey carries.
const cacheKeyLength = 64

// repositoryScope is the untrusted cache scope a cacheKey argument names: the
// first 16 hex digits of the key, or scopeUnkeyed when there is none.
func repositoryScope(key string) (cacheScope, error) {
	if key == "" {
		return scopeUnkeyed, nil
	}
	_, err := hex.DecodeString(key)
	if err != nil || len(key) != cacheKeyLength || strings.ToLower(key) != key {
		return "", fmt.Errorf("invalid cacheKey %q: want the %d lowercase hex digits of a SHA-256", key, cacheKeyLength)
	}
	return cacheScope(key[:16]), nil
}

// goCache is one Go cache directory and the Dagger volume mounted there.
type goCache struct {
	Path   string
	Volume string
}

// goCaches are the module and build cache volumes whose names end in suffix.
func goCaches(suffix string) []goCache {
	return []goCache{
		{Path: "/go/pkg/mod", Volume: "levenshtein-go-mod-" + suffix},
		{Path: "/root/.cache/go-build", Volume: "levenshtein-go-build-" + suffix},
	}
}

// toolCaches are the volumes tools are built with, keyed by the pinned Go
// version and shared by every repository.
func toolCaches(tools toolchain) []goCache {
	return goCaches("tools-" + tools.Go)
}

// untrustedCaches are the volumes one scope's repository code runs with,
// keyed by the pinned Go version and by scope, so no tool is ever built from a
// cache repository code could have written, and no repository compiles from
// one another repository's code could have written.
func untrustedCaches(tools toolchain, scope cacheScope) []goCache {
	return goCaches("untrusted-" + tools.Go + "-" + string(scope))
}

func goContainerWith(tools toolchain, caches []goCache) *dagger.Container {
	ctr := dag.Container().From(tools.GoImage).WithEnvVariable("GOTOOLCHAIN", "local")
	for _, cache := range caches {
		ctr = ctr.WithMountedCache(cache.Path, dag.CacheVolume(cache.Volume))
	}
	return ctr
}

// goContainer is the pinned Go image with the caches tools are built from.
// Nothing that runs the repository's code may use it; see
// untrustedGoContainer.
func goContainer(tools toolchain) *dagger.Container {
	return goContainerWith(tools, toolCaches(tools))
}

// untrustedGoContainer is the pinned Go image with scope's caches, for the
// steps that run the repository's code. A tool such a step needs is built in
// goContainer and copied in (see pinnedToolBinary), never built here, and
// each step calls verifyModuleCache before it runs anything.
func untrustedGoContainer(tools toolchain, scope cacheScope) *dagger.Container {
	return goContainerWith(tools, untrustedCaches(tools, scope))
}

// verifyModuleCache runs go mod verify in ctr, from module's directory,
// before a step runs the repository's code there, and fails when the module
// cache's copy of a dependency no longer matches what was downloaded.
//
// go mod verify compares each downloaded module's extracted directory and zip
// with the hash recorded beside them in the cache when it was downloaded. Go
// itself compares that recorded hash with go.sum whenever a build loads the
// module, so the two together tie the source a step compiles to the
// repository's go.sum. Modules the build list names that were never downloaded
// are skipped, so this needs no network beyond the go.mod files the step
// itself would fetch. A vendored module builds from vendor/, and a module
// without go.sum has no dependencies to verify, so both skip it.
//
// step is the command the verification guards. It rides along as arguments
// the shell ignores, so Dagger answers this verification from its cache
// exactly when it would answer the step, and never lets a step run on a cached
// verification from an earlier state of the volume.
func verifyModuleCache(ctx context.Context, ctr *dagger.Container, source *dagger.Directory, module string, step []string) error {
	dir := source.Directory(module)
	regular := dagger.DirectoryExistsOpts{ExpectedType: dagger.ExistsTypeRegularType}
	vendored, err := dir.Exists(ctx, "vendor/modules.txt", regular)
	if err != nil {
		return err
	}
	sums, err := dir.Exists(ctx, "go.sum", regular)
	if err != nil {
		return err
	}
	if vendored || !sums {
		return nil
	}

	command := append([]string{"sh", "-c", "exec go mod verify", "go-mod-verify"}, step...)
	run, _, err := runTool(ctx, ctr.WithWorkdir(path.Join("/src", module)), command)
	if err != nil {
		return err
	}
	return moduleCacheError(module, run)
}

// moduleCacheError is the check error a failed go mod verify run means, or nil
// when it passed. Either way nothing was compiled from the cache.
func moduleCacheError(module string, run checktool.Run) error {
	if run.ExitCode == 0 {
		return nil
	}
	output := strings.TrimSpace(strings.TrimSpace(run.Stdout) + "\n" + strings.TrimSpace(run.Stderr))
	if strings.Contains(output, "has been modified") || strings.Contains(output, "missing ziphash") {
		return fmt.Errorf("the Go module cache was modified since download; code a go-test, go-generate or go-mutation step of this repository ran on this engine changed it, so module %q was not built from it. Prune the engine's cache (dagger core engine local-cache prune) or use a fresh engine, and find the code that wrote to the module cache:\n%s", module, output)
	}
	return fmt.Errorf("go mod verify failed for module %q before running its code:\n%s", module, output)
}

// pinnedTool is a Go tool built from a module of its own, runner/tools/<Dir>,
// so updating one tool never moves a version another is built with.
type pinnedTool struct {
	Dir string
	Pkg string
}

var (
	toolActionlint  = pinnedTool{Dir: "actionlint", Pkg: "github.com/rhysd/actionlint/cmd/actionlint"}
	toolApidiff     = pinnedTool{Dir: "apidiff", Pkg: "golang.org/x/exp/cmd/apidiff"}
	toolGitleaks    = pinnedTool{Dir: "gitleaks", Pkg: "github.com/zricethezav/gitleaks/v8"}
	toolGovulncheck = pinnedTool{Dir: "govulncheck", Pkg: "golang.org/x/vuln/cmd/govulncheck"}
	toolGremlins    = pinnedTool{Dir: "gremlins", Pkg: "github.com/go-gremlins/gremlins/cmd/gremlins"}
)

// pinnedTools is every module under runner/tools.
var pinnedTools = []pinnedTool{toolActionlint, toolApidiff, toolGitleaks, toolGovulncheck, toolGremlins}

// withTool builds tool from its own module into output, using ctr's Go caches.
func withTool(ctr *dagger.Container, tool pinnedTool, output string) *dagger.Container {
	dir := path.Join("/tools", tool.Dir)
	return ctr.WithDirectory(dir, dag.CurrentModule().Source().Directory(path.Join("tools", tool.Dir))).
		WithWorkdir(dir).
		WithExec([]string{"go", "build", "-trimpath", "-o", output, tool.Pkg})
}

// pinnedToolBinary is tool built alone in goContainer, for a container that
// runs the repository's code and so must not build it: that code could have
// changed the tool's sources in the untrusted caches on an earlier run.
func pinnedToolBinary(tools toolchain, tool pinnedTool) *dagger.File {
	output := path.Join("/usr/local/bin", tool.Dir)
	return withTool(goContainer(tools), tool, output).File(output)
}

func executeCheck(ctx context.Context, source *dagger.Directory, module string, tools toolchain, check checkName, scope cacheScope, nonce string) ([]diagnostic, error) {
	//exhaustive:ignore Other checks use standalone tools below.
	switch check {
	case checkLint:
		return lint(ctx, source, module, tools, nonce)
	case checkHTTP, checkSQL:
		checks := map[checkName]string{checkHTTP: "bodyclose", checkSQL: "sqlclosecheck"}
		tools.Checks = []string{checks[check]}
		return lint(ctx, source, module, tools, nonce)
	case checkMod:
		return goMod(ctx, source, module, tools, nonce)
	case checkTest:
		return goTest(ctx, source, module, tools, scope, nonce)
	case checkWorkflowSecurity:
		return workflowSecurity(ctx, source, module, tools, nonce)
	case checkShellLint:
		return shellLint(ctx, source, tools, nonce)
	case checkSecrets:
		return secrets(ctx, source, tools, nonce)
	case checkDepsVuln:
		return depsVuln(ctx, source, tools, nonce)
	}

	ctr := goContainer(tools)
	var command []string
	if check == checkVet {
		command = []string{"go", "vet", "./..."}
	} else {
		builds := map[checkName]pinnedTool{
			checkWorkflow: toolActionlint,
			checkVuln:     toolGovulncheck,
		}
		tool, ok := builds[check]
		if !ok {
			return nil, fmt.Errorf("unsupported check %q", check)
		}
		ctr = withTool(ctr, tool, "/usr/local/bin/check")
		command = []string{"/usr/local/bin/check", "./..."}
		if check == checkWorkflow {
			// Shell/Python tools are separate checks, not ambient optional dependencies.
			command = []string{"/usr/local/bin/check", "-shellcheck=", "-pyflakes="}
			var workflows []string
			for _, pattern := range []string{".github/workflows/*.yml", ".github/workflows/*.yaml"} {
				files, err := source.Glob(ctx, pattern)
				if err != nil {
					return nil, err
				}
				workflows = append(workflows, files...)
			}
			if len(workflows) == 0 {
				return nil, fmt.Errorf("workflow-lint requires .github/workflows/*.yml or *.yaml")
			}
			configs, err := source.Glob(ctx, ".github/actionlint.y*ml")
			if err != nil {
				return nil, err
			}
			if len(configs) > 1 {
				return nil, fmt.Errorf("configure only one .github/actionlint YAML file")
			}
			if len(configs) == 1 {
				command = append(command, "-config-file", configs[0])
			}
			sort.Strings(workflows)
			command = append(command, workflows...) // Exported source has no .git for discovery.
		}
	}

	ctr = ctr.WithDirectory("/src", source).WithWorkdir(path.Join("/src", module))
	if nonce != "" {
		ctr = ctr.WithEnvVariable("LEVENSHTEIN_RUN_NONCE", nonce)
	}
	if check != checkWorkflow {
		if _, err := source.File(path.Join(module, "go.mod")).Contents(ctx); err != nil {
			return nil, fmt.Errorf("module %q needs a readable go.mod: %w", module, err)
		}
		packages, err := ctr.WithExec([]string{"go", "list", "./..."}).Stdout(ctx)
		if err != nil || strings.TrimSpace(packages) == "" {
			return nil, fmt.Errorf("module %q package discovery failed or found no packages: %v", module, err)
		}
	}

	run, _, err := runTool(ctx, ctr, command)
	if err != nil {
		return nil, err
	}
	return checktool.ToolExit(checktool.Kind(check), module, run)
}

// runTool runs args in ctr and returns how the process finished, whatever its
// exit code, with the container it ran in, which holds any report it wrote.
func runTool(ctx context.Context, ctr *dagger.Container, args []string) (checktool.Run, *dagger.Container, error) {
	checked := ctr.WithExec(args, dagger.ContainerWithExecOpts{Expect: dagger.ReturnTypeAny})
	exitCode, err := checked.ExitCode(ctx)
	if err != nil {
		return checktool.Run{}, nil, err
	}
	stdout, err := checked.Stdout(ctx)
	if err != nil {
		return checktool.Run{}, nil, err
	}
	stderr, err := checked.Stderr(ctx)
	if err != nil {
		return checktool.Run{}, nil, err
	}
	return checktool.Run{ExitCode: exitCode, Stdout: stdout, Stderr: stderr}, checked, nil
}

// goMod checks the module's manifests on their own. Tidy ignores a workspace
// anyway, and with GOWORK=off verify covers this module's requirements rather
// than every workspace member's. Neither command loads packages, so a module
// that only declares tools is still checked. Tidy reads go.mod and go.sum and
// never vendor/, so a vendored module still needs its module proxy.
func goMod(ctx context.Context, source *dagger.Directory, module string, tools toolchain, nonce string) ([]diagnostic, error) {
	if _, err := source.File(path.Join(module, "go.mod")).Contents(ctx); err != nil {
		return nil, fmt.Errorf("module %q needs a readable go.mod: %w", module, err)
	}
	ctr := goContainer(tools).
		WithEnvVariable("GOWORK", "off").
		WithDirectory("/src", source).
		WithWorkdir(path.Join("/src", module))
	if nonce != "" {
		ctr = ctr.WithEnvVariable("LEVENSHTEIN_RUN_NONCE", nonce)
	}

	var findings []diagnostic
	for _, step := range checktool.ModSteps {
		run, _, err := runTool(ctx, ctr, step.Args())
		if err != nil {
			return nil, err
		}
		found, err := checktool.ModFindings(step, module, run)
		if err != nil {
			return nil, err
		}
		findings = append(findings, found...)
	}
	return findings, nil
}

// goTest runs the module's tests with the race detector, which needs cgo; the
// pinned golang image carries gcc for it. The tests are the repository's own
// code, so they run with scope's untrusted caches, after verifyModuleCache. A
// fresh run bypasses go test's own result cache, which lives in the build
// cache volume.
func goTest(ctx context.Context, source *dagger.Directory, module string, tools toolchain, scope cacheScope, nonce string) ([]diagnostic, error) {
	if _, err := source.File(path.Join(module, "go.mod")).Contents(ctx); err != nil {
		return nil, fmt.Errorf("module %q needs a readable go.mod: %w", module, err)
	}
	ctr := untrustedGoContainer(tools, scope).
		WithEnvVariable("CGO_ENABLED", "1").
		WithDirectory("/src", source).
		WithWorkdir(path.Join("/src", module))
	if nonce != "" {
		ctr = ctr.WithEnvVariable("LEVENSHTEIN_RUN_NONCE", nonce)
	}
	command := checktool.TestArgs(nonce != "")
	if err := verifyModuleCache(ctx, ctr, source, module, command); err != nil {
		return nil, err
	}
	packages, err := ctr.WithExec([]string{"go", "list", "./..."}).Stdout(ctx)
	if err != nil || strings.TrimSpace(packages) == "" {
		return nil, fmt.Errorf("module %q package discovery failed or found no packages: %v", module, err)
	}

	run, _, err := runTool(ctx, ctr, command)
	if err != nil {
		return nil, err
	}
	return checktool.TestFindings(module, run)
}

// SharedCheck runs one pinned upstream check for a target.
func (m *Levenshtein) SharedCheck(ctx context.Context,
	// +defaultPath="/"
	// +ignore=["**/.env", "**/.env.*", "!**/.env.example", "**/.git"]
	source *dagger.Directory,
	check string,
	// +default="."
	module string,
	// +optional
	nonce string,
	// Names the untrusted Go cache volumes go-test runs with: the CLI passes
	// a SHA-256 of the repository's clone, so each clone gets its own. A call
	// without one shares "unkeyed" volumes with every other such call on the
	// engine. Other checks ignore it.
	// +optional
	cacheKey string,
) error {
	kind := checkName(check)
	if !knownCheck(kind) || kind == checkSelfTest {
		return fmt.Errorf("unsupported shared check %q", check)
	}
	if !filepath.IsLocal(module) || path.Clean(module) != module || strings.Contains(module, "\\") {
		return fmt.Errorf("invalid module path %q", module)
	}
	if (kind == checkWorkflow || kind == checkWorkflowSecurity || kind == checkShellLint || kind == checkSecrets || kind == checkDepsVuln) && module != "." {
		return fmt.Errorf("%s requires a repository-root target", kind)
	}
	// Their verdicts depend on state no source input covers, so Dagger must
	// never answer them from its own cache.
	if (kind == checkVuln || kind == checkMod || kind == checkDepsVuln) && nonce == "" {
		return fmt.Errorf("%s requires a unique nonce; use the Levenshtein CLI", kind)
	}

	scope, err := repositoryScope(cacheKey)
	if err != nil {
		return err
	}

	var tools toolchain
	if err := json.Unmarshal(toolchainJSON, &tools); err != nil {
		return err
	}
	findings, err := executeCheck(ctx, source, module, tools, kind, scope, nonce)
	if err != nil {
		return err
	}
	if len(findings) != 0 {
		return &gqlerror.Error{Message: "shared check failed", Extensions: map[string]any{"levenshteinFindings": findings}}
	}
	return nil
}
