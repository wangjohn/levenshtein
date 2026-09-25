package verify

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/wangjohn/levenshtein/internal/checktool"
)

var helperGitleaks = helper{Name: "gitleaks", Module: "runner/tools", Pkg: "github.com/zricethezav/gitleaks/v8"}

func (n *Native) secrets(ctx context.Context, req Request, work goRun) ([]finding, toolRun, error) {
	files, err := visibleFiles(req.Source, req.Target.Inputs, req.Target.Exclude, nil)
	if err != nil {
		return nil, toolRun{}, err
	}
	if len(files) == 0 {
		return nil, toolRun{}, fmt.Errorf("secrets found no files to scan")
	}
	binary, err := build(ctx, req, work, helperGitleaks)
	if err != nil {
		return nil, toolRun{}, err
	}

	// gitleaks scans a directory and reads its configuration from there, so it
	// runs over a copy holding only what the Dagger path would import.
	staged, cleanup, err := stageFiles(req.Source, files)
	if err != nil {
		return nil, toolRun{}, err
	}
	defer cleanup()
	reports, err := os.MkdirTemp("", "levenshtein-secrets-")
	if err != nil {
		return nil, toolRun{}, err
	}
	defer func() { _ = os.RemoveAll(reports) }()
	report := filepath.Join(reports, "report.json")

	args := checktool.SecretsArguments(binary, slices.Contains(files, checktool.SecretsConfig), report)
	run, err := runTool(ctx, staged, args, secretsEnv(work.Env), goCheckTimeout)
	if err != nil {
		return nil, run, err
	}
	data, err := os.ReadFile(report)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, run, err
	}
	findings, err := toolFindings(checktool.SecretsFindings(checktool.Run(run), data))
	return findings, run, err
}

// secretsEnv drops what gitleaks would otherwise read from the host: a
// configuration named by GITLEAKS_CONFIG or given whole in
// GITLEAKS_CONFIG_TOML, either of which would replace the repository's. The
// container has neither.
func secretsEnv(env []string) []string {
	return slices.DeleteFunc(slices.Clone(env), func(entry string) bool {
		return strings.HasPrefix(entry, "GITLEAKS_")
	})
}
