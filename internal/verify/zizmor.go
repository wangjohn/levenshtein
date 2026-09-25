package verify

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/wangjohn/levenshtein/internal/checktool"
)

func (n *Native) workflowSecurity(ctx context.Context, req Request, work goRun) ([]finding, toolRun, error) {
	inputs, config, err := zizmorInputs(req.Source, req.Target.Inputs, req.Target.Exclude)
	if err != nil {
		return nil, toolRun{}, err
	}
	binary, err := installRelease(ctx, req.Shared, work.Root, releaseZizmor, "")
	if err != nil {
		return nil, toolRun{}, err
	}

	run, err := runTool(ctx, work.Dir, checktool.ZizmorArguments(binary, config, inputs), zizmorEnv(work.Env), goCheckTimeout)
	if err != nil {
		return nil, run, err
	}
	findings, err := toolFindings(checktool.ZizmorFindings(req.Target.Dir, checktool.Run(run)))
	return findings, run, err
}

// zizmorEnv drops what zizmor would otherwise read from the host: its own
// ZIZMOR_* settings, which could name another configuration or clash with the
// check's flags, and GitHub credentials, which an offline audit never needs.
// The container has none of them either.
func zizmorEnv(env []string) []string {
	out := make([]string, 0, len(env))
	for _, entry := range env {
		name, _, _ := strings.Cut(entry, "=")
		if !strings.HasPrefix(name, "ZIZMOR_") && name != "GH_TOKEN" && name != "GITHUB_TOKEN" && name != "GH_HOST" {
			out = append(out, entry)
		}
	}
	return out
}

// zizmorInputs names every file workflow-security audits, the way the Dagger
// path does: the workflows, the composite actions at the root and under
// .github/actions, and the Dependabot configuration. zizmor's own discovery
// would read .gitignore and look for a configuration above the repository only
// when there is no .git, which an exported source never has, so the inputs and
// the configuration are both named explicitly and the executors agree. Only
// files under the target's declared inputs and outside its excludes count:
// they are all the Dagger path imports and all the fingerprint covers.
func zizmorInputs(source string, declaredInputs, excludes []string) ([]string, string, error) {
	visible := func(path string) bool {
		return declared(declaredInputs, path) && !excluded(path, excludes)
	}

	var inputs []string
	for _, pattern := range []string{".github/workflows/*.yml", ".github/workflows/*.yaml", "action.yml", "action.yaml", ".github/dependabot.yml", ".github/dependabot.yaml"} {
		matches, err := filepath.Glob(filepath.Join(source, filepath.FromSlash(pattern)))
		if err != nil {
			return nil, "", err
		}
		for _, match := range matches {
			if path := repositoryPath(source, match); visible(path) {
				inputs = append(inputs, filepath.ToSlash(path))
			}
		}
	}

	actions := filepath.Join(source, ".github", "actions")
	err := filepath.WalkDir(actions, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel := repositoryPath(source, path)
		if !entry.IsDir() && (entry.Name() == "action.yml" || entry.Name() == "action.yaml") && visible(rel) {
			inputs = append(inputs, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, "", err
	}
	if len(inputs) == 0 {
		return nil, "", fmt.Errorf("workflow-security found no workflows, composite actions or Dependabot configuration to audit")
	}

	var configs []string
	for _, config := range checktool.ZizmorConfigs {
		info, err := os.Stat(filepath.Join(source, filepath.FromSlash(config)))
		if err == nil && info.Mode().IsRegular() && visible(filepath.FromSlash(config)) {
			configs = append(configs, config)
		}
	}
	if len(configs) > 1 {
		return nil, "", fmt.Errorf("configure only one zizmor file; found %s", strings.Join(configs, ", "))
	}

	sort.Strings(inputs)
	config := ""
	if len(configs) == 1 {
		config = configs[0]
	}
	return inputs, config, nil
}
