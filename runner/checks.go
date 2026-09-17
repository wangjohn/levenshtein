package main

import (
	"context"
	"fmt"
	"path"
	"sort"
	"strings"

	"dagger/levenshtein/internal/dagger"
)

func knownCheck(check string) bool {
	switch check {
	case "go-lint", "go-vet", "go-http", "go-sql", "go-vuln", "workflow-lint", "self-test":
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

func executeCheck(ctx context.Context, source *dagger.Directory, module string, tools toolchain, check, nonce string) ([]diagnostic, error) {
	switch check {
	case "go-lint":
		return lint(ctx, source, module, tools, nonce)
	case "go-http", "go-sql":
		checks := map[string]string{"go-http": "bodyclose", "go-sql": "sqlclosecheck"}
		tools.Checks = []string{checks[check]}
		return lint(ctx, source, module, tools, nonce)
	}

	ctr := goContainer(tools)
	var command []string
	if check == "go-vet" {
		command = []string{"go", "vet", "./..."}
	} else {
		packages := map[string]string{
			"workflow-lint": "github.com/rhysd/actionlint/cmd/actionlint",
			"go-vuln":       "golang.org/x/vuln/cmd/govulncheck",
		}
		pkg, ok := packages[check]
		if !ok {
			return nil, fmt.Errorf("unsupported check %q", check)
		}
		ctr = ctr.WithDirectory("/tools", dag.CurrentModule().Source().Directory("tools")).
			WithWorkdir("/tools").
			WithExec([]string{"go", "build", "-trimpath", "-o", "/usr/local/bin/check", pkg})
		command = []string{"/usr/local/bin/check", "./..."}
		if check == "workflow-lint" {
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
	if check != "workflow-lint" {
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
func commandFindings(check, module string, exitCode int, stdout, stderr string) ([]diagnostic, error) {
	if exitCode == 0 {
		return nil, nil
	}
	message := strings.TrimSpace(stdout + "\n" + stderr)
	failureCode := 1
	if check == "go-vuln" {
		failureCode = 3 // govulncheck distinguishes vulnerabilities from tool errors.
	}
	if exitCode != failureCode || message == "" {
		return nil, fmt.Errorf("%s exited %d: %s", check, exitCode, message)
	}
	finding := diagnostic{Code: check, Message: message}
	finding.Location.File = module
	finding.Location.Line = 1
	return []diagnostic{finding}, nil
}
