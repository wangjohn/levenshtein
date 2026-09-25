package main

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"dagger/levenshtein/internal/checktool"
	"dagger/levenshtein/internal/dagger"
)

// workflowSecurity audits the repository's workflows, composite actions and
// Dependabot configuration with the pinned zizmor, installed like every
// pinned release (see withRelease), so nothing unverified is ever extracted
// or run.
func workflowSecurity(ctx context.Context, source *dagger.Directory, module string, tools toolchain, nonce string) ([]diagnostic, error) {
	inputs, config, err := zizmorInputs(ctx, source)
	if err != nil {
		return nil, err
	}

	ctr, err := withRelease(ctx, dag.Container().From(tools.GoImage), tools.Zizmor, "zizmor", "/usr/local/bin/zizmor")
	if err != nil {
		return nil, err
	}
	ctr = ctr.WithDirectory("/src", source).WithWorkdir("/src")
	if nonce != "" {
		ctr = ctr.WithEnvVariable("LEVENSHTEIN_RUN_NONCE", nonce)
	}

	run, _, err := runTool(ctx, ctr, checktool.ZizmorArguments("/usr/local/bin/zizmor", config, inputs))
	if err != nil {
		return nil, err
	}
	return checktool.ZizmorFindings(module, run)
}

// zizmorInputs names every file workflow-security audits: the workflows, the
// composite actions at the root and under .github/actions, and the Dependabot
// configuration. The exported source has no .git, so zizmor's own discovery
// would ignore .gitignore and could look for a configuration outside it; the
// inputs and the configuration are named explicitly instead.
func zizmorInputs(ctx context.Context, source *dagger.Directory) ([]string, string, error) {
	var inputs []string
	for _, pattern := range []string{".github/workflows/*.yml", ".github/workflows/*.yaml", "action.yml", "action.yaml", ".github/dependabot.yml", ".github/dependabot.yaml", ".github/actions/**/action.yml", ".github/actions/**/action.yaml"} {
		files, err := source.Glob(ctx, pattern)
		if err != nil {
			return nil, "", err
		}
		inputs = append(inputs, files...)
	}
	if len(inputs) == 0 {
		return nil, "", fmt.Errorf("workflow-security found no workflows, composite actions or Dependabot configuration to audit")
	}

	var configs []string
	for _, config := range checktool.ZizmorConfigs {
		files, err := source.Glob(ctx, config)
		if err != nil {
			return nil, "", err
		}
		configs = append(configs, files...)
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
