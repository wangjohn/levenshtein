// Shared Go verification with pinned tools and explicit failure propagation.
package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"strings"
	"time"

	"dagger/levenshtein/internal/dagger"
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

type Status string

const (
	StatusPlanned Status = "planned"
	StatusPassed  Status = "passed"
	StatusFailed  Status = "failed"
	StatusError   Status = "error"
)

//levenshtein:record
type result struct {
	Check      string       `json:"check"`
	Module     string       `json:"module,omitempty"`
	Status     Status       `json:"status"`
	DurationMS int64        `json:"duration_ms"`
	Findings   []diagnostic `json:"findings,omitempty"`
}

//levenshtein:record
type report struct {
	Run      string    `json:"run"`
	Status   Status    `json:"status"`
	Modules  []string  `json:"modules"`
	Selected []string  `json:"selected"`
	Tools    toolchain `json:"tools"`
	Results  []result  `json:"results"`
}

// Verify executes a configured run against an explicitly supplied repository.
func (m *Levenshtein) Verify(
	ctx context.Context,
	// Repository containing the Go modules to check.
	// Public .env.example templates can be embedded by Go packages.
	// +ignore=["**/.env", "**/.env.*", "!**/.env.example", "**/.git"]
	source *dagger.Directory,
	// +default="branch"
	run string,
	// Preview selected checks without executing them.
	// +optional
	dryRun bool,
	// Unique execution input for a fresh main audit; set by the launcher.
	// +optional
	nonce string,
) (string, error) {
	var tools toolchain
	if err := json.Unmarshal(toolchainJSON, &tools); err != nil {
		return "", err
	}
	cfg := defaultConfig()
	entries, err := source.Entries(ctx)
	if err != nil {
		return "", err
	}
	for _, entry := range entries {
		if entry == "levenshtein.json" {
			content, err := source.File(entry).Contents(ctx)
			if err != nil {
				return "", err
			}
			cfg, err = parseConfig(content)
			if err != nil {
				return "", err
			}
		}
	}
	checks, err := cfg.selectChecks(run)
	if err != nil {
		return "", err
	}

	results := []result{}
	renderReport := func(status Status) string {
		return render(report{
			Run:      run,
			Status:   status,
			Modules:  cfg.Modules,
			Selected: checks,
			Tools:    tools,
			Results:  results,
		})
	}

	if dryRun {
		return renderReport(StatusPlanned), nil
	}
	if run == "main" && nonce == "" {
		return "", fmt.Errorf("main requires a unique nonce for fresh execution; use ./verify main")
	}
	for _, check := range checks {
		if check == "self-test" {
			start := time.Now()
			err := m.selfTest(ctx, tools, nonce)
			status := StatusPassed
			if err != nil {
				status = StatusError
			}
			results = append(results, result{Check: check, Status: status, DurationMS: time.Since(start).Milliseconds()})
			if err != nil {
				return "", fmt.Errorf("%s\nself-test: %w", renderReport(StatusFailed), err)
			}
			continue
		}
		for _, module := range cfg.Modules {
			start := time.Now()
			findings, err := lint(ctx, source, module, tools, nonce)
			status := StatusPassed
			if err != nil {
				status = StatusError
			} else if len(findings) != 0 {
				status = StatusFailed
			}
			results = append(results, result{Check: check, Module: module, Status: status, DurationMS: time.Since(start).Milliseconds(), Findings: findings})
			if status != StatusPassed {
				if err != nil {
					return "", fmt.Errorf("%s\ngo-lint could not complete: %w", renderReport(StatusFailed), err)
				}
				return "", fmt.Errorf("%s\ngo-lint found %d issue(s); fix the reported locations and rerun ./verify %q with the same --source", renderReport(StatusFailed), len(findings), run)
			}
		}
	}
	return renderReport(StatusPassed), nil
}

func render(r report) string {
	data, _ := json.MarshalIndent(r, "", "  ")
	return string(data)
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
