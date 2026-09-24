package verify

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Analysis is slow and its tools are expensive to build, so give them room; a
// check that hangs is still bounded.
const goCheckTimeout = 30 * time.Minute

// goRun is what one shared Go check execution works in: the target directory,
// the environment with toolchain and workspace selection pinned, and the
// directory holding its built tools and analysis cache.
type goRun struct {
	Dir  string
	Env  []string
	Root string
}

// goRunner is the work one shared Go kind does. It reports diagnostics, the raw
// invocation for the report, and a tool error. goCheckExecutor owns everything
// the shared kinds have in common.
type goRunner func(*Native, context.Context, Request, goRun) ([]finding, toolRun, error)

// goCheckExecutor adapts one kind's work to the native executor's signature,
// keeping setup, cancellation and result shape identical across the shared
// kinds and identical to what the Dagger path reports.
func goCheckExecutor(run goRunner, message string) func(*Native, context.Context, Request, string, []string) Result {
	return func(n *Native, ctx context.Context, req Request, dir string, env []string) Result {
		root, release, err := n.cacheRoot()
		if err != nil {
			return Result{Status: StatusError, Error: "native tool directory: " + err.Error()}
		}
		defer release()

		work := goRun{Dir: dir, Env: analysisEnv(env, workspace(req, dir)), Root: root}
		findings, invocation, err := run(n, ctx, req, work)
		result := Result{Stdout: invocation.Stdout, Stderr: invocation.Stderr, Warnings: nativeWarnings(req)}

		if ctx.Err() != nil {
			return result.withOutcome(StatusCancelled, ctx.Err().Error())
		}
		if err != nil {
			return result.withOutcome(StatusError, err.Error())
		}
		if len(findings) == 0 {
			result.Status = StatusPassed
			return result
		}

		result.Status = StatusFailed
		result.Error = message
		result.Details = findingsDetails(findings)
		return result
	}
}

// nativeWarnings says what a native check left out. Community rules run
// only on the Dagger executor, where the lint step is isolated from the host;
// a native go-lint check runs its core rules and says it skipped the rest, so
// one configuration can still mix executors.
func nativeWarnings(req Request) []Warning {
	if len(req.RuleModules) == 0 {
		return nil
	}
	return []Warning{{
		Kind:    WarningRuleModulesSkipped,
		Message: fmt.Sprintf("community rules from %d rule module(s) run only on the Dagger executor; this native check ran the core rules", len(req.RuleModules)),
	}}
}

// validateSharedGoCheck accepts a shared Go kind on a native environment. Only
// go-lint takes options of its own, which validateCheck checked; the
// environment may still pin an identity, values, passed names and tool
// versions, which validateEnvironment checked.
func validateSharedGoCheck(check Check, _ Environment) error {
	if check.Command != nil || check.Semantic != nil {
		return fmt.Errorf("command and semantic options cannot be used for shared Go checks")
	}
	return nil
}

// workspace is the GOWORK value that makes the host resolve modules the way
// the container does. The Dagger path imports only declared inputs less the
// target's excludes, so Go inside it finds a go.work only between the target
// directory and the source root, and only when an input covers it and no
// exclude drops it; it can never see one above the source. Natively, Go would otherwise keep searching past the source root.
func workspace(req Request, dir string) string {
	for current := dir; ; current = filepath.Dir(current) {
		rel, err := filepath.Rel(req.Source, current)
		if err != nil || (rel != "." && !filepath.IsLocal(rel)) {
			return "off"
		}

		candidate := filepath.Join(current, "go.work")
		info, err := os.Stat(candidate)
		work := filepath.Join(rel, "go.work")
		if err == nil && info.Mode().IsRegular() && declared(req.Target.Inputs, work) && !excluded(work, req.Target.Exclude) {
			return candidate
		}
		if rel == "." {
			return "off"
		}
	}
}

// declared reports whether a repository-relative path falls under one of the
// target's inputs, which is what decides whether the Dagger path imports it.
func declared(inputs []string, path string) bool {
	for _, input := range inputs {
		if input == "." || path == input || strings.HasPrefix(path, input+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// goModule refuses a module the shared checks cannot analyze, so an empty or
// unreadable module is an error rather than a silent pass. This mirrors the
// preflight the Dagger path runs inside the container.
func goModule(ctx context.Context, module string, work goRun) error {
	if _, err := os.Stat(filepath.Join(work.Dir, "go.mod")); err != nil {
		return fmt.Errorf("module %q needs a readable go.mod: %w", module, err)
	}

	run, err := runTool(ctx, work.Dir, []string{"go", "list", "./..."}, work.Env, goCheckTimeout)
	if err != nil {
		return err
	}
	if run.ExitCode != 0 {
		return fmt.Errorf("module %q package discovery failed: %s", module, strings.TrimSpace(run.Stderr))
	}
	if strings.TrimSpace(run.Stdout) == "" {
		return fmt.Errorf("module %q contains no Go packages; refusing an empty pass", module)
	}
	return nil
}

// staticcheckCache is where Staticcheck keeps its analysis facts. A fresh run
// points it at a throwaway directory, which is what the Dagger path's nonce
// achieves by swapping the mounted cache volume for a temporary path.
func staticcheckCache(req Request, root string) (string, func(), error) {
	if req.RerunChecks {
		dir, err := os.MkdirTemp("", "levenshtein-staticcheck-")
		return dir, func() { _ = os.RemoveAll(dir) }, err
	}

	dir := filepath.Join(root, "staticcheck")
	return dir, func() {}, os.MkdirAll(dir, 0700)
}

func (n *Native) goLint(ctx context.Context, req Request, work goRun) ([]finding, toolRun, error) {
	shipped, err := sharedChecks(req.Shared)
	if err != nil {
		return nil, toolRun{}, err
	}
	if err := goModule(ctx, req.Target.Dir, work); err != nil {
		return nil, toolRun{}, err
	}
	binary, err := build(ctx, req, work, helperLint)
	if err != nil {
		return nil, toolRun{}, err
	}
	checks, err := lintSelection(ctx, work, binary, shipped, req.Check.coreLintChecks())
	if err != nil {
		return nil, toolRun{}, err
	}
	cache, release, err := staticcheckCache(req, work.Root)
	if err != nil {
		return nil, toolRun{}, err
	}
	defer release()

	args := []string{binary, "-f=json", "-checks=" + strings.Join(checks, ","), "./..."}
	env := append(append([]string{}, work.Env...), "STATICCHECK_CACHE="+cache)
	run, err := runTool(ctx, work.Dir, args, env, goCheckTimeout)
	if err != nil {
		return nil, run, err
	}
	findings, err := parseFindings(run.ExitCode, run.Stdout, run.Stderr, checks, req.Source)
	return findings, run, err
}

func (n *Native) goVet(ctx context.Context, req Request, work goRun) ([]finding, toolRun, error) {
	if err := goModule(ctx, req.Target.Dir, work); err != nil {
		return nil, toolRun{}, err
	}

	run, err := runTool(ctx, work.Dir, []string{"go", "vet", "./..."}, work.Env, goCheckTimeout)
	if err != nil {
		return nil, run, err
	}
	findings, err := commandFindings(CheckGoVet, req.Target.Dir, run.ExitCode, run.Stdout, run.Stderr)
	return findings, run, err
}

// goMod checks the target module's manifests on their own. Tidy ignores a
// workspace anyway, and with GOWORK=off verify covers this module's
// requirements rather than every workspace member's. Neither command loads
// packages, so a module without any, such as one that only declares tools,
// is still checked; a missing go.mod is an error. Tidy reads go.mod and go.sum
// and never vendor/, so a vendored module still needs its module proxy.
func (n *Native) goMod(ctx context.Context, req Request, work goRun) ([]finding, toolRun, error) {
	if _, err := os.Stat(filepath.Join(work.Dir, "go.mod")); err != nil {
		return nil, toolRun{}, fmt.Errorf("module %q needs a readable go.mod: %w", req.Target.Dir, err)
	}

	env := goEnv(work.Env, []string{"GOWORK=off"})
	var findings []finding
	var output toolRun
	for _, step := range modSteps {
		run, err := runTool(ctx, work.Dir, step.args(), env, goCheckTimeout)
		output = toolRun{ExitCode: run.ExitCode, Stdout: output.Stdout + run.Stdout, Stderr: output.Stderr + run.Stderr}
		if err != nil {
			return nil, output, err
		}
		found, err := modFindings(step, req.Target.Dir, run.ExitCode, run.Stdout, run.Stderr)
		if err != nil {
			return nil, output, err
		}
		findings = append(findings, found...)
	}
	return findings, output, nil
}

func (n *Native) goVuln(ctx context.Context, req Request, work goRun) ([]finding, toolRun, error) {
	if err := goModule(ctx, req.Target.Dir, work); err != nil {
		return nil, toolRun{}, err
	}
	binary, err := build(ctx, req, work, helperVulncheck)
	if err != nil {
		return nil, toolRun{}, err
	}

	run, err := runTool(ctx, work.Dir, []string{binary, "./..."}, work.Env, goCheckTimeout)
	if err != nil {
		return nil, run, err
	}
	findings, err := commandFindings(CheckGoVuln, req.Target.Dir, run.ExitCode, run.Stdout, run.Stderr)
	return findings, run, err
}

func (n *Native) workflowLint(ctx context.Context, req Request, work goRun) ([]finding, toolRun, error) {
	binary, err := build(ctx, req, work, helperActionlint)
	if err != nil {
		return nil, toolRun{}, err
	}
	args, err := workflowArguments(req.Source, binary)
	if err != nil {
		return nil, toolRun{}, err
	}

	run, err := runTool(ctx, work.Dir, args, work.Env, goCheckTimeout)
	if err != nil {
		return nil, run, err
	}
	findings, err := commandFindings(CheckWorkflowLint, req.Target.Dir, run.ExitCode, run.Stdout, run.Stderr)
	return findings, run, err
}

// workflowArguments names every workflow file explicitly, the way the Dagger
// path does, because the tool's own discovery needs a repository it can read
// and the check must not depend on one. Shell and Python tools are separate
// checks, not ambient optional dependencies.
func workflowArguments(source, binary string) ([]string, error) {
	var workflows []string
	for _, pattern := range []string{"*.yml", "*.yaml"} {
		matches, err := filepath.Glob(filepath.Join(source, ".github", "workflows", pattern))
		if err != nil {
			return nil, err
		}
		for _, match := range matches {
			workflows = append(workflows, repositoryPath(source, match))
		}
	}
	if len(workflows) == 0 {
		return nil, fmt.Errorf("workflow-lint requires .github/workflows/*.yml or *.yaml")
	}

	configs, err := filepath.Glob(filepath.Join(source, ".github", "actionlint.y*ml"))
	if err != nil {
		return nil, err
	}
	if len(configs) > 1 {
		return nil, fmt.Errorf("configure only one .github/actionlint YAML file")
	}

	args := []string{binary, "-shellcheck=", "-pyflakes="}
	if len(configs) == 1 {
		args = append(args, "-config-file", repositoryPath(source, configs[0]))
	}
	sort.Strings(workflows)
	return append(args, workflows...), nil
}
