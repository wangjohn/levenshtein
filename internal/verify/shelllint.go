package verify

import (
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/wangjohn/levenshtein/internal/checktool"
)

// sourceSkipDir reports whether shell-lint and deps-vuln leave out a directory.
func sourceSkipDir(name string) bool {
	return slices.Contains(checktool.SourceSkipDirs, name)
}

// shellInputs names the scripts shell-lint checks and the root configuration it
// honors, from the files the target makes visible (see visibleFiles).
func shellInputs(source string, files []string) ([]string, string, error) {
	var scripts, configs []string
	for _, file := range files {
		if slices.Contains(checktool.ShellRCNames, file) {
			configs = append(configs, file)
		}

		var head []byte
		if path.Ext(path.Base(file)) == "" {
			read, err := readHead(filepath.Join(source, filepath.FromSlash(file)), checktool.ShellHeadLimit)
			if err != nil {
				return nil, "", err
			}
			head = read
		}
		if checktool.ShellScript(file, head) {
			scripts = append(scripts, file)
		}
	}

	if len(scripts) == 0 {
		return nil, "", fmt.Errorf("shell-lint found no shell scripts (*.sh, *.bash, or an extensionless file with a sh, bash, dash or ksh shebang) to check")
	}
	if len(configs) > 1 {
		return nil, "", fmt.Errorf("configure only one ShellCheck file; found %s", strings.Join(configs, ", "))
	}
	config := ""
	if len(configs) == 1 {
		config = configs[0]
	}
	return scripts, config, nil
}

func readHead(file string, limit int) ([]byte, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }() // Read-only.
	return io.ReadAll(io.LimitReader(f, int64(limit)))
}

func (n *Native) shellLint(ctx context.Context, req Request, work goRun) ([]finding, toolRun, error) {
	files, err := visibleFiles(ctx, req, sourceSkipDir)
	if err != nil {
		return nil, toolRun{}, err
	}
	scripts, config, err := shellInputs(req.Source, files)
	if err != nil {
		return nil, toolRun{}, err
	}
	binary, err := installRelease(ctx, req.Shared, work.Root, releaseShellCheck, "")
	if err != nil {
		return nil, toolRun{}, err
	}

	run, err := runTool(ctx, req.Source, checktool.ShellcheckArguments(binary, config, scripts), shellEnv(work.Env), goCheckTimeout)
	if err != nil {
		return nil, run, err
	}
	findings, err := toolFindings(checktool.ShellFindings(run.diagnostics()))
	return findings, run, err
}

// shellEnv drops SHELLCHECK_OPTS, which would otherwise add the host's own
// flags to the check's. The container has none.
func shellEnv(env []string) []string {
	return slices.DeleteFunc(slices.Clone(env), func(entry string) bool {
		return strings.HasPrefix(entry, "SHELLCHECK_OPTS=")
	})
}
