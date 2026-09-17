package main

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"dagger/levenshtein/internal/dagger"

	"github.com/vektah/gqlparser/v2/gqlerror"
)

type checkName string

const (
	checkLint     checkName = "go-lint"
	checkVet      checkName = "go-vet"
	checkHTTP     checkName = "go-http"
	checkSQL      checkName = "go-sql"
	checkVuln     checkName = "go-vuln"
	checkWorkflow checkName = "workflow-lint"
	checkSelfTest checkName = "self-test"
)

func knownCheck(check checkName) bool {
	switch check {
	case checkLint, checkVet, checkHTTP, checkSQL, checkVuln, checkWorkflow, checkSelfTest:
		return true
	}
	return false
}

func goContainer(tools toolchain) *dagger.Container {
	return dag.Container().From(tools.GoImage).
		WithEnvVariable("GOTOOLCHAIN", "local").
		WithMountedCache("/go/pkg/mod", dag.CacheVolume("levenshtein-go-mod-"+tools.Go)).
		WithMountedCache("/root/.cache/go-build", dag.CacheVolume("levenshtein-go-build-"+tools.Go))
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

	checked := ctr.WithExec(command, dagger.ContainerWithExecOpts{Expect: dagger.ReturnTypeAny})
	exitCode, err := checked.ExitCode(ctx)
	if err != nil {
		return nil, err
	}
	stdout, err := checked.Stdout(ctx)
	if err != nil {
		return nil, err
	}
	stderr, err := checked.Stderr(ctx)
	if err != nil {
		return nil, err
	}
	return commandFindings(check, module, exitCode, stdout, stderr)
}

// Preserve native tool output, including its precise locations, in the report.
func commandFindings(check checkName, module string, exitCode int, stdout, stderr string) ([]diagnostic, error) {
	if exitCode == 0 {
		return nil, nil
	}
	message := strings.TrimSpace(stdout + "\n" + stderr)
	failureCode := 1
	if check == checkVuln {
		failureCode = 3 // govulncheck distinguishes vulnerabilities from tool errors.
	}
	if exitCode != failureCode || message == "" {
		return nil, fmt.Errorf("%s exited %d: %s", check, exitCode, message)
	}
	return []diagnostic{{
		Code:     string(check),
		Message:  message,
		Location: location{File: module, Line: 1},
	}}, nil
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
	if kind == checkWorkflow && module != "." {
		return fmt.Errorf("workflow-lint requires a repository-root target")
	}
	if kind == checkVuln && nonce == "" {
		return fmt.Errorf("go-vuln requires a unique nonce; use the Levenshtein CLI")
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
