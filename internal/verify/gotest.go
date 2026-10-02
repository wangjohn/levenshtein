package verify

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/wangjohn/levenshtein/internal/checktool"
)

// goTest runs the target module's tests with the race detector, in the
// workspace the container would see. The race detector needs cgo, so the check
// turns it on and names a missing C compiler up front rather than letting
// every package fail to build runtime/cgo. The report keeps go test's text
// output rather than its event stream.
func (n *Native) goTest(ctx context.Context, req Request, work goRun) ([]finding, toolRun, error) {
	if err := goModule(ctx, req.Target.Dir, work); err != nil {
		return nil, toolRun{}, err
	}
	env := goEnv(work.Env, []string{"CGO_ENABLED=1"})
	if err := raceCompiler(ctx, work.Dir, env); err != nil {
		return nil, toolRun{}, err
	}

	run, err := runTool(ctx, work.Dir, checktool.TestArgs(req.RerunChecks), env, goCheckTimeout)
	if err != nil {
		return nil, run, err
	}
	findings, err := toolFindings(checktool.TestFindings(req.Target.Dir, run.diagnostics()))
	if text, parseErr := checktool.TestTranscript(run.Stdout); parseErr == nil {
		run.Stdout = text
	}
	return findings, run, err
}

// raceCompiler makes sure the C compiler go would use for cgo is on the
// check's PATH. go test -race cannot build without one, and the pinned
// container always has gcc.
func raceCompiler(ctx context.Context, dir string, env []string) error {
	run, err := runTool(ctx, dir, []string{"go", "env", "CC"}, env, time.Minute)
	if err != nil {
		return err
	}
	if run.ExitCode != 0 {
		return fmt.Errorf("go env CC failed: %s", strings.TrimSpace(run.Stderr))
	}

	compiler := strings.Fields(run.Stdout)
	if len(compiler) == 0 {
		return fmt.Errorf("go-test runs go test -race, which needs cgo, but go env CC names no C compiler")
	}
	if _, err := executable(dir, env, compiler[0]); err != nil {
		return fmt.Errorf("go-test runs go test -race, which needs cgo and a C compiler such as gcc or clang: %w", err)
	}
	return nil
}
