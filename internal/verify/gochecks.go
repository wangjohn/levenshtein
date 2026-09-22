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

// goRunner is the work one shared Go kind does. It reports diagnostics, the raw
// invocation for the report, and a tool error. runGoCheck owns everything the
// four kinds share.
type goRunner func(*Native, context.Context, Request, string, []string) ([]finding, toolRun, error)

// goCheckExecutor adapts one kind's work to the native executor's signature,
// keeping cancellation and result shape identical across the four kinds and
// identical to what the Dagger path reports.
func goCheckExecutor(run goRunner, message string) func(*Native, context.Context, Request, string, []string) Result {
	return func(n *Native, ctx context.Context, req Request, dir string, env []string) Result {
		findings, invocation, err := run(n, ctx, req, dir, env)
		result := Result{Stdout: invocation.Stdout, Stderr: invocation.Stderr}

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

// validateSharedGoCheck accepts a shared Go kind on a native environment. The
// kind has no options of its own; the environment may still pin an identity,
// values, passed names and tool versions, which validateEnvironment checked.
func validateSharedGoCheck(check Check, _ Environment) error {
	if check.Command != nil || check.Semantic != nil {
		return fmt.Errorf("command and semantic options cannot be used for shared Go checks")
	}
	return nil
}

// goModule refuses a module the shared checks cannot analyze, so an empty or
// unreadable module is an error rather than a silent pass. This mirrors the
// preflight the Dagger path runs inside the container.
func goModule(ctx context.Context, module, dir string, env []string) error {
	if _, err := os.Stat(filepath.Join(dir, "go.mod")); err != nil {
		return fmt.Errorf("module %q needs a readable go.mod: %w", module, err)
	}

	run, err := runTool(ctx, dir, []string{"go", "list", "./..."}, analysisEnv(env), goCheckTimeout)
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
func (n *Native) staticcheckCache(req Request) (string, func(), error) {
	if req.RerunChecks {
		dir, err := os.MkdirTemp("", "levenshtein-staticcheck-")
		return dir, func() { _ = os.RemoveAll(dir) }, err
	}

	root, err := n.cacheRoot()
	if err != nil {
		return "", func() {}, err
	}
	dir := filepath.Join(root, "staticcheck")
	return dir, func() {}, os.MkdirAll(dir, 0700)
}

func (n *Native) goLint(ctx context.Context, req Request, dir string, env []string) ([]finding, toolRun, error) {
	checks, err := sharedChecks(req.Shared)
	if err != nil {
		return nil, toolRun{}, err
	}
	if err := goModule(ctx, req.Target.Dir, dir, env); err != nil {
		return nil, toolRun{}, err
	}
	binary, err := n.build(ctx, req, helperLint, env)
	if err != nil {
		return nil, toolRun{}, err
	}
	cache, release, err := n.staticcheckCache(req)
	if err != nil {
		return nil, toolRun{}, err
	}
	defer release()

	args := []string{binary, "-f=json", "-checks=" + strings.Join(checks, ","), "./..."}
	run, err := runTool(ctx, dir, args, append(analysisEnv(env), "STATICCHECK_CACHE="+cache), goCheckTimeout)
	if err != nil {
		return nil, run, err
	}
	findings, err := parseFindings(run.ExitCode, run.Stdout, run.Stderr, checks, req.Source)
	return findings, run, err
}

func (n *Native) goVet(ctx context.Context, req Request, dir string, env []string) ([]finding, toolRun, error) {
	if err := goModule(ctx, req.Target.Dir, dir, env); err != nil {
		return nil, toolRun{}, err
	}

	run, err := runTool(ctx, dir, []string{"go", "vet", "./..."}, analysisEnv(env), goCheckTimeout)
	if err != nil {
		return nil, run, err
	}
	findings, err := commandFindings(CheckGoVet, req.Target.Dir, run.ExitCode, run.Stdout, run.Stderr)
	return findings, run, err
}

func (n *Native) goVuln(ctx context.Context, req Request, dir string, env []string) ([]finding, toolRun, error) {
	if err := goModule(ctx, req.Target.Dir, dir, env); err != nil {
		return nil, toolRun{}, err
	}
	binary, err := n.build(ctx, req, helperVulncheck, env)
	if err != nil {
		return nil, toolRun{}, err
	}

	run, err := runTool(ctx, dir, []string{binary, "./..."}, analysisEnv(env), goCheckTimeout)
	if err != nil {
		return nil, run, err
	}
	findings, err := commandFindings(CheckGoVuln, req.Target.Dir, run.ExitCode, run.Stdout, run.Stderr)
	return findings, run, err
}

func (n *Native) workflowLint(ctx context.Context, req Request, dir string, env []string) ([]finding, toolRun, error) {
	binary, err := n.build(ctx, req, helperActionlint, env)
	if err != nil {
		return nil, toolRun{}, err
	}
	args, err := workflowArguments(req.Source, binary)
	if err != nil {
		return nil, toolRun{}, err
	}

	run, err := runTool(ctx, dir, args, analysisEnv(env), goCheckTimeout)
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
