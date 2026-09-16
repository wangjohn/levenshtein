// Shared Go verification with pinned tools and explicit failure propagation.
package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"path/filepath"
	"strings"

	"dagger/levenshtein/internal/dagger"

	"github.com/vektah/gqlparser/v2/gqlerror"
)

type Levenshtein struct{}

//go:embed toolchain.json
var toolchainJSON []byte

type toolchain struct {
	Go                 string   `json:"go"`
	GoImage            string   `json:"goImage"`
	Staticcheck        string   `json:"staticcheck"`
	StaticcheckRelease string   `json:"staticcheckRelease"`
	Checks             []string `json:"checks"`
}

type diagnostic struct {
	Code     string `json:"code"`
	Message  string `json:"message"`
	Location struct {
		File   string `json:"file"`
		Line   int    `json:"line"`
		Column int    `json:"column"`
	} `json:"location"`
}

func lint(ctx context.Context, source *dagger.Directory, module string, tools toolchain, nonce string) ([]diagnostic, error) {
	if _, err := source.File(path.Join(module, "go.mod")).Contents(ctx); err != nil {
		return nil, fmt.Errorf("module %q needs a readable go.mod: %w", module, err)
	}

	ctr := dag.Container().From(tools.GoImage).
		WithEnvVariable("GOTOOLCHAIN", "local").
		WithMountedCache("/go/pkg/mod", dag.CacheVolume("levenshtein-go-mod-"+tools.Go)).
		WithMountedCache("/root/.cache/go-build", dag.CacheVolume("levenshtein-go-build-"+tools.Go)).
		WithDirectory("/policy", dag.CurrentModule().Source().Directory("lint")).
		WithWorkdir("/policy").
		WithExec([]string{"go", "build", "-trimpath", "-o", "/go/bin/levenshtein-lint", "./cmd/levenshtein-lint"}).
		WithDirectory("/src", source).
		WithWorkdir(path.Join("/src", module))

	// Go selects vendor mode for modules/workspaces that use it, and readonly otherwise.
	packages, err := ctr.WithExec([]string{"go", "list", "./..."}).Stdout(ctx)
	if err != nil {
		return nil, fmt.Errorf("discovering packages: %w", err)
	}
	if strings.TrimSpace(packages) == "" {
		return nil, fmt.Errorf("module %q contains no Go packages; refusing an empty pass", module)
	}

	if nonce == "" {
		ctr = ctr.WithMountedCache("/root/.cache/staticcheck", dag.CacheVolume("levenshtein-staticcheck-"+tools.Staticcheck+"-"+tools.Go)).
			WithEnvVariable("STATICCHECK_CACHE", "/root/.cache/staticcheck")
	} else {
		ctr = ctr.WithEnvVariable("LEVENSHTEIN_RUN_NONCE", nonce).
			WithEnvVariable("STATICCHECK_CACHE", "/tmp/staticcheck-fresh")
	}
	checked := ctr.WithExec([]string{"/go/bin/levenshtein-lint", "-f=json", "-checks=" + strings.Join(tools.Checks, ","), "./..."}, dagger.ContainerWithExecOpts{Expect: dagger.ReturnTypeAny})
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
	return parseFindings(exitCode, stdout, stderr, tools.Checks)
}

func parseFindings(exitCode int, stdout, stderr string, checks []string) ([]diagnostic, error) {
	if exitCode != 0 && exitCode != 1 {
		return nil, fmt.Errorf("Staticcheck exited %d: %s\n%s", exitCode, stderr, stdout)
	}

	allowed := map[string]bool{}
	for _, check := range checks {
		allowed[check] = true
	}

	var findings []diagnostic
	decoder := json.NewDecoder(strings.NewReader(stdout))
	for {
		var finding diagnostic
		err := decoder.Decode(&finding)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("invalid Staticcheck JSON: %w", err)
		}
		if !allowed[finding.Code] || finding.Message == "" || finding.Location.File == "" || finding.Location.Line < 1 {
			return nil, fmt.Errorf("unexpected diagnostic (possibly a compile error): %s", stdout)
		}
		finding.Location.File = strings.TrimPrefix(finding.Location.File, "/src/")
		findings = append(findings, finding)
	}

	if (exitCode == 0 && len(findings) != 0) || (exitCode == 1 && len(findings) == 0) {
		return nil, fmt.Errorf("Staticcheck exit %d does not match diagnostics: %s\n%s", exitCode, stdout, stderr)
	}
	if strings.TrimSpace(stderr) != "" {
		return nil, fmt.Errorf("Staticcheck could not produce a clean result: %s", stderr)
	}
	return findings, nil
}

func (m *Levenshtein) selfTest(ctx context.Context, tools toolchain, nonce string) error {
	fixtures := dag.CurrentModule().Source().Directory("testdata")
	for _, name := range []string{"good", "vendored", "embedded"} {
		findings, err := lint(ctx, fixtures.Directory(name), ".", tools, nonce)
		if err != nil || len(findings) != 0 {
			return fmt.Errorf("%s fixture must pass: findings=%v error=%v", name, findings, err)
		}
	}

	bad, err := lint(ctx, fixtures.Directory("bad"), ".", tools, nonce)
	if err != nil {
		return fmt.Errorf("bad fixture must fail for its lint diagnostics, not a tool error: %w", err)
	}

	counts := map[string]int{}
	for _, finding := range bad {
		counts[finding.Code]++
	}
	for _, check := range tools.Checks {
		if counts[check] != 1 {
			return fmt.Errorf("bad fixture must produce exactly one %s diagnostic; got %v", check, counts)
		}
	}

	for _, fixture := range []struct{ name, message string }{
		{"broken", "undefined: undefinedFunction"},
		{"empty", "contains no Go packages"},
	} {
		_, err := lint(ctx, fixtures.Directory(fixture.name), ".", tools, nonce)
		if err == nil || !strings.Contains(err.Error(), fixture.message) {
			return fmt.Errorf("%s fixture must fail for %q; got %v", fixture.name, fixture.message, err)
		}
	}
	return nil
}

// GoLint runs the shared Go policy and cleanup rules.
// +check
func (m *Levenshtein) GoLint(ctx context.Context,
	// +optional
	// +defaultPath="/"
	// +ignore=["**/.env", "**/.env.*", "!**/.env.example", "**/.git"]
	source *dagger.Directory,
	// +default="."
	module string,
	// +optional
	nonce string,
) error {
	if !filepath.IsLocal(module) || path.Clean(module) != module || strings.Contains(module, "\\") {
		return fmt.Errorf("invalid module path %q", module)
	}

	var tools toolchain
	if err := json.Unmarshal(toolchainJSON, &tools); err != nil {
		return err
	}

	findings, err := lint(ctx, source, module, tools, nonce)
	if err != nil {
		return err
	}
	if len(findings) != 0 {
		return &gqlerror.Error{Message: "Go policy lint failed", Extensions: map[string]any{"levenshteinFindings": findings}}
	}
	return nil
}

// SelfTest checks the shared lint rules against good and bad examples.
// +check
func (m *Levenshtein) SelfTest(ctx context.Context,
	// +optional
	nonce string,
) error {
	var tools toolchain
	if err := json.Unmarshal(toolchainJSON, &tools); err != nil {
		return err
	}
	return m.selfTest(ctx, tools, nonce)
}
