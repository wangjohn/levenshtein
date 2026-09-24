package verify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

var helperGitleaks = helper{Name: "gitleaks", Module: "runner/tools", Pkg: "github.com/zricethezav/gitleaks/v8"}

// secretsLeakExit is the exit code the secrets check asks gitleaks to use for
// leaks. gitleaks' own default, 1, is also what it exits with on a fatal
// error, and Go exits 2 on a panic, so neither could tell leaks from a failed
// scan. runner/secrets.go keeps a copy; change both together.
const secretsLeakExit = 3

// Configuration files gitleaks reads at the root of the directory it scans.
const (
	secretsConfig = ".gitleaks.toml"
	secretsIgnore = ".gitleaksignore"
)

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

	args := secretsArguments(binary, slices.Contains(files, secretsConfig), report)
	run, err := runTool(ctx, staged, args, secretsEnv(work.Env), goCheckTimeout)
	if err != nil {
		return nil, run, err
	}
	data, err := os.ReadFile(report)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, run, err
	}
	findings, err := secretsFindings(run.ExitCode, data, run.Stderr)
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

// secretsArguments scans the working directory as plain files: an exported
// source has no .git, so history is out of scope on both executors. Every
// secret is fully redacted before gitleaks writes its report or a log line,
// so no secret value can reach the check's report, output, or cache. The
// report goes to a file outside the scanned directory. gitleaks reads
// .gitleaksignore from the directory it scans; a root .gitleaks.toml is named
// explicitly, and without one gitleaks uses its default rules.
// runner/secrets.go keeps a copy; change both together.
func secretsArguments(binary string, configured bool, report string) []string {
	args := []string{binary, "dir", ".", "--no-banner", "--no-color", "--redact=100", "--log-level=warn", fmt.Sprintf("--exit-code=%d", secretsLeakExit), "--report-format=json", "--report-path=" + report}
	if configured {
		args = append(args, "--config="+secretsConfig)
	}
	return args
}

// secretsLeak is the part of one gitleaks JSON finding the check keeps. The
// Secret, Match and Line fields are deliberately absent: they are redacted,
// but the check never reads them. Fingerprint is file:rule:line, the entry
// .gitleaksignore takes. The tags are gitleaks' field names.
type secretsLeak struct {
	RuleID      string `json:"RuleID"`
	Description string `json:"Description"`
	File        string `json:"File"`
	StartLine   int    `json:"StartLine"`
	StartColumn int    `json:"StartColumn"`
	Fingerprint string `json:"Fingerprint"`
}

// secretsFindings turns one gitleaks run into findings, one per leak, coded by
// the rule that matched. Only secretsLeakExit means leaks; 0 means none, and
// every other exit, a missing or unreadable report, or a report that
// contradicts the exit code is an error, never a pass.
// runner/secrets.go keeps a copy; change both together.
func secretsFindings(exitCode int, report []byte, stderr string) ([]finding, error) {
	if exitCode != 0 && exitCode != secretsLeakExit {
		return nil, fmt.Errorf("gitleaks exited %d: %s", exitCode, strings.TrimSpace(stderr))
	}

	var leaks []secretsLeak
	if err := json.Unmarshal(report, &leaks); err != nil {
		return nil, fmt.Errorf("gitleaks exited %d without a readable report: %w: %s", exitCode, err, strings.TrimSpace(stderr))
	}
	if (exitCode == 0) != (len(leaks) == 0) {
		return nil, fmt.Errorf("gitleaks exit %d does not match its %d findings: %s", exitCode, len(leaks), strings.TrimSpace(stderr))
	}

	findings := make([]finding, 0, len(leaks))
	for _, leak := range leaks {
		if leak.RuleID == "" || leak.File == "" || leak.StartLine < 1 {
			return nil, fmt.Errorf("unexpected gitleaks finding for rule %q in %q", leak.RuleID, leak.File)
		}
		findings = append(findings, finding{
			Code:     leak.RuleID,
			Message:  fmt.Sprintf("%s The value is redacted; if it is not a secret, add %s to %s.", strings.TrimSpace(leak.Description), leak.Fingerprint, secretsIgnore),
			Location: location{File: filepath.ToSlash(leak.File), Line: leak.StartLine, Column: leak.StartColumn},
		})
	}
	return findings, nil
}
