package main

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"dagger/levenshtein/internal/checktool"
	"dagger/levenshtein/internal/dagger"
)

// zizmorReleases is where upstream publishes zizmor's release archives.
// internal/verify downloads from the same place; change both together.
const zizmorReleases = "https://github.com/zizmorcore/zizmor/releases/download"

// zizmorPin is the zizmor release workflow-security runs: a version and, per
// OS/architecture platform, the upstream archive and its reviewed SHA-256.
type zizmorPin struct {
	Version  string                   `json:"version"`
	Archives map[string]zizmorArchive `json:"archives"`
}

type zizmorArchive struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
}

// workflowSecurity audits the repository's workflows, composite actions and
// Dependabot configuration with the pinned zizmor. The engine fetches the
// archive for the container's own platform and refuses it unless it matches the
// reviewed SHA-256, so nothing unverified is ever extracted or run.
func workflowSecurity(ctx context.Context, source *dagger.Directory, module string, tools toolchain, nonce string) ([]diagnostic, error) {
	inputs, config, err := zizmorInputs(ctx, source)
	if err != nil {
		return nil, err
	}

	ctr := dag.Container().From(tools.GoImage)
	platform, err := ctr.Platform(ctx)
	if err != nil {
		return nil, err
	}
	archive, err := tools.Zizmor.archive(string(platform))
	if err != nil {
		return nil, err
	}
	url := fmt.Sprintf("%s/v%s/%s", zizmorReleases, tools.Zizmor.Version, archive.Name)
	download := dag.HTTP(url, dagger.HTTPOpts{Checksum: "sha256:" + archive.SHA256})

	ctr = ctr.WithMountedFile("/tmp/zizmor.tar.gz", download).
		WithExec([]string{"tar", "-xzf", "/tmp/zizmor.tar.gz", "-C", "/usr/local/bin", "zizmor"}).
		WithDirectory("/src", source).
		WithWorkdir("/src")
	if nonce != "" {
		ctr = ctr.WithEnvVariable("LEVENSHTEIN_RUN_NONCE", nonce)
	}

	run, _, err := runTool(ctx, ctr, checktool.ZizmorArguments("/usr/local/bin/zizmor", config, inputs))
	if err != nil {
		return nil, err
	}
	return checktool.ZizmorFindings(module, run)
}

// archive is the pinned archive for one platform, ignoring any CPU variant the
// engine reports. A platform upstream does not publish, or one the pin has not
// reviewed, is an error rather than a fallback to an unverified build.
func (p zizmorPin) archive(platform string) (zizmorArchive, error) {
	if parts := strings.Split(platform, "/"); len(parts) > 2 {
		platform = parts[0] + "/" + parts[1]
	}
	archive, ok := p.Archives[platform]
	if !ok || archive.Name == "" || strings.Contains(archive.Name, "/") || len(archive.SHA256) != 64 {
		return zizmorArchive{}, fmt.Errorf("workflow-security has no reviewed zizmor %s archive for %s", p.Version, platform)
	}
	return archive, nil
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
