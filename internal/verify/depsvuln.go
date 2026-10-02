package verify

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/wangjohn/levenshtein/internal/checktool"
)

func (n *Native) depsVuln(ctx context.Context, req Request, work goRun) ([]finding, toolRun, error) {
	files, err := visibleFiles(ctx, req, sourceSkipDir)
	if err != nil {
		return nil, toolRun{}, err
	}
	binary, err := installRelease(ctx, req.Shared, work.Root, releaseOSVScanner, "")
	if err != nil {
		return nil, toolRun{}, err
	}

	// osv-scanner walks a directory and finds its own configuration there, so
	// it runs over a copy holding only what the Dagger path would import.
	staged, cleanup, err := stageFiles(req.Source, files)
	if err != nil {
		return nil, toolRun{}, err
	}
	defer cleanup()
	staged, err = filepath.EvalSymlinks(staged)
	if err != nil {
		return nil, toolRun{}, err
	}
	reports, err := os.MkdirTemp("", "levenshtein-deps-")
	if err != nil {
		return nil, toolRun{}, err
	}
	defer func() { _ = os.RemoveAll(reports) }()
	report := filepath.Join(reports, "report.json")

	args := checktool.DepsArguments(binary, slices.Contains(files, checktool.DepsConfig), report, checktool.DepsStaged)
	run, err := runTool(ctx, staged, args, depsEnv(work.Env), goCheckTimeout)
	if err != nil {
		return nil, run, err
	}
	data, err := os.ReadFile(report)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, run, err
	}
	findings, err := toolFindings(checktool.DepsFindings(staged, run.diagnostics(), data))
	return findings, run, err
}

// depsEnv drops osv-scanner's own OSV_SCANNER_* settings, which the container
// does not have either.
func depsEnv(env []string) []string {
	return slices.DeleteFunc(slices.Clone(env), func(entry string) bool {
		return strings.HasPrefix(entry, "OSV_SCANNER_")
	})
}
