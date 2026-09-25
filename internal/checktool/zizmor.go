package checktool

import "fmt"

// ZizmorConfigs are the files zizmor would discover at a repository root, in
// its own order of precedence.
var ZizmorConfigs = []string{".github/zizmor.yml", ".github/zizmor.yaml", "zizmor.yml", "zizmor.yaml"}

// ZizmorArguments audits offline, so the verdict depends only on the inputs,
// the configuration and the pinned binary, which is what makes it cacheable.
// Online audits stay with zizmor's own GitHub Action. Without a configuration
// file, --no-config keeps zizmor from finding one outside the repository.
// --strict-collection makes an unparsable input an error instead of a warning
// and a silent pass.
func ZizmorArguments(binary, config string, inputs []string) []string {
	args := []string{binary, "--offline", "--strict-collection", "--min-severity=medium", "--format=plain", "--color=never", "--no-progress", "--quiet"}
	if config == "" {
		args = append(args, "--no-config")
	} else {
		args = append(args, "--config="+config)
	}
	return append(args, inputs...)
}

// ZizmorFindings keeps zizmor's own report as the finding. zizmor exits 13 or
// 14 when its highest finding is medium or high; with --min-severity=medium
// nothing lower is reported, so 11 and 12 cannot mean findings here. 1 is an
// audit error, 2 a usage error and 3 no inputs; every other code is reserved.
func ZizmorFindings(module string, run Run) ([]Finding, error) {
	if run.ExitCode == 0 {
		return nil, nil
	}

	message := output(run)
	if (run.ExitCode != 13 && run.ExitCode != 14) || message == "" {
		return nil, fmt.Errorf("zizmor exited %d: %s", run.ExitCode, message)
	}
	return whole(KindWorkflowSecurity, module, message), nil
}
