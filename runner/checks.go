package main

import (
	"context"
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

// cacheTrust says whose code has run with a set of Go cache volumes mounted.
// Go does not re-verify extracted module source when it builds, so code that
// can write the module cache can change what every later build in it compiles.
type cacheTrust string

const (
	// cacheTools volumes build the linters and the other pinned tools, and
	// back checks that only read the repository.
	cacheTools cacheTrust = "tools"
	// cacheUntrusted volumes back the steps that run the repository's own
	// code as root: its tests, its generators, and mutation testing.
	cacheUntrusted cacheTrust = "untrusted"
)

// goCache is one Go cache directory and the Dagger volume mounted there.
type goCache struct {
	Path   string
	Volume string
}

// goCaches are the module and build cache volumes for one kind of step, keyed
// by the pinned Go version and by trust, so no tool is ever built from a cache
// the repository's code could have written.
func goCaches(tools toolchain, trust cacheTrust) []goCache {
	suffix := string(trust) + "-" + tools.Go
	return []goCache{
		{Path: "/go/pkg/mod", Volume: "levenshtein-go-mod-" + suffix},
		{Path: "/root/.cache/go-build", Volume: "levenshtein-go-build-" + suffix},
	}
}

func goContainerWith(tools toolchain, trust cacheTrust) *dagger.Container {
	ctr := dag.Container().From(tools.GoImage).WithEnvVariable("GOTOOLCHAIN", "local")
	for _, cache := range goCaches(tools, trust) {
		ctr = ctr.WithMountedCache(cache.Path, dag.CacheVolume(cache.Volume))
	}
	return ctr
}

// goContainer is the pinned Go image with the caches tools are built from.
// Nothing that runs the repository's code may use it; see
// untrustedGoContainer.
func goContainer(tools toolchain) *dagger.Container {
	return goContainerWith(tools, cacheTools)
}

// untrustedGoContainer is the pinned Go image with caches of its own, for the
// steps that run the repository's code. A tool such a step needs is built in
// goContainer and copied in, or, where it is built here, is only ever trusted
// by that step.
func untrustedGoContainer(tools toolchain) *dagger.Container {
	return goContainerWith(tools, cacheUntrusted)
}

func executeCheck(ctx context.Context, source *dagger.Directory, module string, tools toolchain, check checkName, nonce string) ([]diagnostic, error) {
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
		return goTest(ctx, source, module, tools, nonce)
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
		packages := map[checkName]string{
			checkWorkflow: "github.com/rhysd/actionlint/cmd/actionlint",
			checkVuln:     "golang.org/x/vuln/cmd/govulncheck",
		}
		pkg, ok := packages[check]
		if !ok {
			return nil, fmt.Errorf("unsupported check %q", check)
		}
		ctr = ctr.WithDirectory("/tools", dag.CurrentModule().Source().Directory("tools")).
			WithWorkdir("/tools").
			WithExec([]string{"go", "build", "-trimpath", "-o", "/usr/local/bin/check", pkg})
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
// runGremlins and runCommunityLinter still spell this sequence out; moving
// them onto runTool is left to a follow-up.
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
// code, so they run with untrusted caches. A fresh run bypasses go test's own
// result cache, which lives in the build cache volume.
func goTest(ctx context.Context, source *dagger.Directory, module string, tools toolchain, nonce string) ([]diagnostic, error) {
	if _, err := source.File(path.Join(module, "go.mod")).Contents(ctx); err != nil {
		return nil, fmt.Errorf("module %q needs a readable go.mod: %w", module, err)
	}
	ctr := untrustedGoContainer(tools).
		WithEnvVariable("CGO_ENABLED", "1").
		WithDirectory("/src", source).
		WithWorkdir(path.Join("/src", module))
	if nonce != "" {
		ctr = ctr.WithEnvVariable("LEVENSHTEIN_RUN_NONCE", nonce)
	}
	packages, err := ctr.WithExec([]string{"go", "list", "./..."}).Stdout(ctx)
	if err != nil || strings.TrimSpace(packages) == "" {
		return nil, fmt.Errorf("module %q package discovery failed or found no packages: %v", module, err)
	}

	run, _, err := runTool(ctx, ctr, checktool.TestArgs(nonce != ""))
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

	var tools toolchain
	if err := json.Unmarshal(toolchainJSON, &tools); err != nil {
		return err
	}
	findings, err := executeCheck(ctx, source, module, tools, kind, nonce)
	if err != nil {
		return err
	}
	if len(findings) != 0 {
		return &gqlerror.Error{Message: "shared check failed", Extensions: map[string]any{"levenshteinFindings": findings}}
	}
	return nil
}
