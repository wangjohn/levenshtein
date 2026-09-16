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

type result struct {
	Check      string       `json:"check"`
	Module     string       `json:"module,omitempty"`
	Status     string       `json:"status"`
	DurationMS int64        `json:"duration_ms"`
	Findings   []diagnostic `json:"findings,omitempty"`
}

type report struct {
	Run      string    `json:"run"`
	Status   string    `json:"status"`
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
	r := report{Run: run, Status: "planned", Modules: cfg.Modules, Selected: checks, Tools: tools, Results: []result{}}
	if dryRun {
		return render(r), nil
	}
	if run == "main" && nonce == "" {
		return "", fmt.Errorf("main requires a unique nonce for fresh execution; use ./verify main")
	}
	for _, check := range checks {
		if check == "self-test" {
			start := time.Now()
			err := m.selfTest(ctx, tools, nonce)
			status := "passed"
			if err != nil {
				status = "error"
			}
			r.Results = append(r.Results, result{Check: check, Status: status, DurationMS: time.Since(start).Milliseconds()})
			if err != nil {
				r.Status = "failed"
				return "", fmt.Errorf("%s\nself-test: %w", render(r), err)
			}
			continue
		}
		for _, module := range cfg.Modules {
			start := time.Now()
			findings, err := lint(ctx, source, module, tools, nonce)
			status := "passed"
			if err != nil {
				status = "error"
			} else if len(findings) != 0 {
				status = "failed"
			}
			r.Results = append(r.Results, result{Check: check, Module: module, Status: status, DurationMS: time.Since(start).Milliseconds(), Findings: findings})
			if status != "passed" {
				r.Status = "failed"
				if err != nil {
					return "", fmt.Errorf("%s\ngo-lint could not complete: %w", render(r), err)
				}
				return "", fmt.Errorf("%s\ngo-lint found %d issue(s); fix the reported locations and rerun ./verify %q with the same --source", render(r), len(findings), run)
			}
		}
	}
	r.Status = "passed"
	return render(r), nil
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
		WithExec([]string{"go", "install", "honnef.co/go/tools/cmd/staticcheck@" + tools.Staticcheck}).
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
	checked := ctr.WithExec([]string{"/go/bin/staticcheck", "-f=json", "-checks=" + strings.Join(tools.Checks, ","), "./..."}, dagger.ContainerWithExecOpts{Expect: dagger.ReturnTypeAny})
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

// Check executes one selected check without loading consumer configuration.
// Planning and run policy belong to the standalone runner.
func (m *Levenshtein) Check(
	ctx context.Context,
	// +ignore=["**/.env", "**/.env.*", "!**/.env.example", "**/.git"]
	source *dagger.Directory,
	module string,
	check string,
	// +optional
	nonce string,
) (string, error) {
	var tools toolchain
	if err := json.Unmarshal(toolchainJSON, &tools); err != nil {
		return "", err
	}
	r := report{Run: check, Status: "passed", Modules: []string{module}, Selected: []string{check}, Tools: tools, Results: []result{}}
	item := result{Check: check, Module: module, Status: "passed"}
	var err error
	switch check {
	case "go-lint":
		cfg := config{Modules: []string{module}, Runs: map[string][]string{check: {check}}}
		if _, err := cfg.selectChecks(check); err != nil {
			return "", err
		}
		item.Findings, err = lint(ctx, source, module, tools, nonce)
		if len(item.Findings) > 0 {
			item.Status = "failed"
		}
	case "self-test":
		err = m.selfTest(ctx, tools, nonce)
	default:
		return "", fmt.Errorf("unknown check %q", check)
	}
	if err != nil {
		return "", err
	}
	r.Status = item.Status
	r.Results = append(r.Results, item)
	return render(r), nil
}
