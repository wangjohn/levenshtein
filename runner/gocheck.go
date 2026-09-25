package main

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"dagger/levenshtein/internal/dagger"

	"github.com/vektah/gqlparser/v2/gqlerror"
)

// The checks that judge a whole module run levenshtein-gocheck, built from
// lint/cmd/levenshtein-gocheck, so the Dagger path and the CLI's native path
// decide each verdict with the same code.
const checkImports checkName = "go-imports"

// gochecker is the pinned Go image with levenshtein-gocheck built from this
// module.
func gochecker(tools toolchain) *dagger.Container {
	return goContainer(tools).
		WithDirectory("/policy", dag.CurrentModule().Source().Directory("lint")).
		WithWorkdir("/policy").
		WithExec([]string{"go", "build", "-trimpath", "-o", "/go/bin/levenshtein-gocheck", "./cmd/levenshtein-gocheck"})
}

// gocheckOutput is levenshtein-gocheck's report.
type gocheckOutput struct {
	Findings []diagnostic `json:"findings"`
	Notes    []string     `json:"notes"`
}

// gocheckReport reads one levenshtein-gocheck run, refusing any result that
// does not agree with itself: exit 0 must carry no findings and exit 1 some,
// every one of the check's own code at a real location, with nothing on
// stderr. Exit 2, the command's own error, and any other exit are tool errors,
// never a pass. internal/verify/gocheck.go keeps a copy for the native
// executor; both tests load testdata/gocheck-reports.json.
func gocheckReport(check checkName, exitCode int, stdout, stderr string) ([]diagnostic, []string, error) {
	if exitCode != 0 && exitCode != 1 {
		return nil, nil, fmt.Errorf("%s could not run: %s", check, strings.TrimSpace(stderr+"\n"+stdout))
	}
	var output gocheckOutput
	decoder := json.NewDecoder(strings.NewReader(stdout))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&output); err != nil {
		return nil, nil, fmt.Errorf("%s printed an unreadable report: %w: %s", check, err, strings.TrimSpace(stdout+"\n"+stderr))
	}
	if (exitCode == 0) != (len(output.Findings) == 0) {
		return nil, nil, fmt.Errorf("%s exit %d does not match its %d findings: %s", check, exitCode, len(output.Findings), stdout)
	}
	for _, found := range output.Findings {
		if found.Code != string(check) || found.Message == "" || found.Location.File == "" || found.Location.Line < 1 {
			return nil, nil, fmt.Errorf("%s reported an unexpected finding: %+v", check, found)
		}
	}
	if strings.TrimSpace(stderr) != "" {
		return nil, nil, fmt.Errorf("%s could not produce a clean result: %s", check, stderr)
	}
	return output.Findings, output.Notes, nil
}

// runGocheck runs one levenshtein-gocheck invocation in ctr and reads its
// report. A nonce makes Dagger execute it again rather than answer from its
// call cache.
func runGocheck(ctx context.Context, ctr *dagger.Container, check checkName, args []string, nonce string) ([]diagnostic, []string, error) {
	if nonce != "" {
		ctr = ctr.WithEnvVariable("LEVENSHTEIN_RUN_NONCE", nonce)
	}
	checked := ctr.WithExec(append([]string{"/go/bin/levenshtein-gocheck"}, args...), dagger.ContainerWithExecOpts{Expect: dagger.ReturnTypeAny})
	exitCode, err := checked.ExitCode(ctx)
	if err != nil {
		return nil, nil, err
	}
	stdout, err := checked.Stdout(ctx)
	if err != nil {
		return nil, nil, err
	}
	stderr, err := checked.Stderr(ctx)
	if err != nil {
		return nil, nil, err
	}
	return gocheckReport(check, exitCode, stdout, stderr)
}

// validModule accepts a module directory that stays inside the source.
func validModule(module string) error {
	if !filepath.IsLocal(module) || path.Clean(module) != module || strings.Contains(module, "\\") {
		return fmt.Errorf("invalid module path %q", module)
	}
	return nil
}

// failed turns findings into the error the CLI reads them from.
func failed(findings []diagnostic) error {
	if len(findings) == 0 {
		return nil
	}
	return &gqlerror.Error{Message: "shared check failed", Extensions: map[string]any{"levenshteinFindings": findings}}
}

// goImports checks the module's packages against the rules. It reads what go
// list and the files' import declarations say, so it needs neither module
// downloads nor type checking.
func goImports(ctx context.Context, source *dagger.Directory, module, rules string, tools toolchain, nonce string) ([]diagnostic, []string, error) {
	if _, err := source.File(path.Join(module, "go.mod")).Contents(ctx); err != nil {
		return nil, nil, fmt.Errorf("module %q needs a readable go.mod: %w", module, err)
	}
	ctr := gochecker(tools).
		WithDirectory("/src", source).
		WithWorkdir(path.Join("/src", module))
	return runGocheck(ctx, ctr, checkImports, []string{"imports", "-rules=" + rules, "-prefix=" + module}, nonce)
}

// gocheckSelfTest proves each whole-module check passes its good fixture and
// fails its bad one for the findings the fixtures were written to produce.
func gocheckSelfTest(ctx context.Context, fixtures *dagger.Directory, tools toolchain, nonce string) error {
	table, err := fixtures.File("import-rules.json").Contents(ctx)
	if err != nil {
		return err
	}
	var rules struct {
		Fixture  json.RawMessage `json:"fixture"`
		Findings []string        `json:"fixture_findings"`
	}
	if err := json.Unmarshal([]byte(table), &rules); err != nil {
		return fmt.Errorf("import-rules.json: %w", err)
	}

	good, notes, err := goImports(ctx, fixtures.Directory("imports-good"), ".", string(rules.Fixture), tools, nonce)
	if err != nil || len(good) != 0 || !slices.ContainsFunc(notes, func(note string) bool { return strings.HasPrefix(note, "2 rules checked 2 packages") }) {
		return fmt.Errorf("imports-good fixture must pass go-imports after checking both governed packages: findings=%v notes=%v error=%v", good, notes, err)
	}
	bad, _, err := goImports(ctx, fixtures.Directory("imports-bad"), ".", string(rules.Fixture), tools, nonce)
	if err != nil {
		return fmt.Errorf("imports-bad fixture must fail for its imports, not a tool error: %w", err)
	}
	if len(bad) != len(rules.Findings) {
		return fmt.Errorf("imports-bad fixture must report %v: %v", rules.Findings, bad)
	}
	for i, finding := range bad {
		file, _, _ := strings.Cut(rules.Findings[i], ":")
		if finding.Code != string(checkImports) || finding.Location.File != file {
			return fmt.Errorf("imports-bad fixture must report %v: %v", rules.Findings, bad)
		}
	}
	return nil
}

// GoImports checks a module's packages against layering rules and reports
// every import a rule forbids, at the import.
func (m *Levenshtein) GoImports(ctx context.Context,
	// +defaultPath="/"
	// +ignore=["**/.env", "**/.env.*", "!**/.env.example", "**/.git"]
	source *dagger.Directory,
	// The rules as JSON, spelled like a go-imports check's "imports" object in
	// levenshtein.json.
	rules string,
	// +default="."
	module string,
	// +optional
	nonce string,
) error {
	if err := validModule(module); err != nil {
		return err
	}
	var tools toolchain
	if err := json.Unmarshal(toolchainJSON, &tools); err != nil {
		return err
	}

	findings, _, err := goImports(ctx, source, module, rules, tools, nonce)
	if err != nil {
		return err
	}
	return failed(findings)
}
