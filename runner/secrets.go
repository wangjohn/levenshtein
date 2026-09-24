package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"dagger/levenshtein/internal/dagger"
)

// secretsLeakExit is the exit code the secrets check asks gitleaks to use for
// leaks. gitleaks' own default, 1, is also what it exits with on a fatal
// error, and Go exits 2 on a panic, so neither could tell leaks from a failed
// scan. internal/verify/secrets.go keeps a copy; change both together.
const secretsLeakExit = 3

// Configuration files gitleaks reads at the root of the directory it scans.
const (
	secretsConfig = ".gitleaks.toml"
	secretsIgnore = ".gitleaksignore"
)

// secretsReport is where gitleaks writes its report, outside the scanned
// source.
const secretsReport = "/tmp/levenshtein-secrets.json"

// secrets scans the source's files, not its history, which an exported source
// does not have, with gitleaks built from the pinned tools module.
func secrets(ctx context.Context, source *dagger.Directory, tools toolchain, nonce string) ([]diagnostic, error) {
	entries, err := source.Entries(ctx)
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("secrets found no files to scan")
	}
	configs, err := source.Glob(ctx, secretsConfig)
	if err != nil {
		return nil, err
	}

	ctr := goContainer(tools).
		WithDirectory("/tools", dag.CurrentModule().Source().Directory("tools")).
		WithWorkdir("/tools").
		WithExec([]string{"go", "build", "-trimpath", "-o", "/usr/local/bin/gitleaks", "github.com/zricethezav/gitleaks/v8"}).
		WithDirectory("/src", source).
		WithWorkdir("/src")
	if nonce != "" {
		ctr = ctr.WithEnvVariable("LEVENSHTEIN_RUN_NONCE", nonce)
	}

	checked := ctr.WithExec(secretsArguments("/usr/local/bin/gitleaks", len(configs) == 1, secretsReport), dagger.ContainerWithExecOpts{Expect: dagger.ReturnTypeAny})
	exitCode, err := checked.ExitCode(ctx)
	if err != nil {
		return nil, err
	}
	stderr, err := checked.Stderr(ctx)
	if err != nil {
		return nil, err
	}
	var report []byte
	if exitCode == 0 || exitCode == secretsLeakExit {
		contents, err := checked.File(secretsReport).Contents(ctx)
		if err != nil {
			return nil, fmt.Errorf("gitleaks exited %d without a report: %w: %s", exitCode, err, strings.TrimSpace(stderr))
		}
		report = []byte(contents)
	}
	return secretsFindings(exitCode, report, stderr)
}

// secretsArguments scans the working directory as plain files. Every secret is
// fully redacted before gitleaks writes its report or a log line, so no secret
// value can reach the check's report, output, or cache. gitleaks reads
// .gitleaksignore from the directory it scans; a root .gitleaks.toml is named
// explicitly, and without one gitleaks uses its default rules.
// internal/verify/secrets.go keeps a copy; change both together.
func secretsArguments(binary string, configured bool, report string) []string {
	args := []string{binary, "dir", ".", "--no-banner", "--no-color", "--redact=100", "--log-level=warn", fmt.Sprintf("--exit-code=%d", secretsLeakExit), "--report-format=json", "--report-path=" + report}
	if configured {
		args = append(args, "--config="+secretsConfig)
	}
	return args
}

// secretsLeak is the part of one gitleaks JSON finding the check keeps. The
// Secret, Match and Line fields are deliberately absent. The tags are gitleaks'
// field names.
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
// internal/verify/secrets.go keeps a copy; change both together.
func secretsFindings(exitCode int, report []byte, stderr string) ([]diagnostic, error) {
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

	findings := make([]diagnostic, 0, len(leaks))
	for _, leak := range leaks {
		if leak.RuleID == "" || leak.File == "" || leak.StartLine < 1 {
			return nil, fmt.Errorf("unexpected gitleaks finding for rule %q in %q", leak.RuleID, leak.File)
		}
		findings = append(findings, diagnostic{
			Code:     leak.RuleID,
			Message:  fmt.Sprintf("%s The value is redacted; if it is not a secret, add %s to %s.", strings.TrimSpace(leak.Description), leak.Fingerprint, secretsIgnore),
			Location: location{File: leak.File, Line: leak.StartLine, Column: leak.StartColumn},
		})
	}
	return findings, nil
}

// secretsSelfTest proves gitleaks builds from the pinned tools module, passes a
// source without secrets, and reports secrets-leaky's made-up key at its
// location under its rule, without the value itself.
func secretsSelfTest(ctx context.Context, fixtures *dagger.Directory, tools toolchain, nonce string) error {
	clean, err := secrets(ctx, fixtures.Directory("secrets-clean"), tools, nonce)
	if err != nil || len(clean) != 0 {
		return fmt.Errorf("secrets-clean fixture must pass secrets: findings=%v error=%v", clean, err)
	}

	leaky, err := secrets(ctx, fixtures.Directory("secrets-leaky"), tools, nonce)
	if err != nil {
		return fmt.Errorf("secrets-leaky fixture must fail for its finding, not a tool error: %w", err)
	}
	if len(leaky) != 1 || leaky[0].Code != "generic-api-key" || leaky[0].Location.File != "settings.py" || leaky[0].Location.Line != 9 {
		return fmt.Errorf("secrets-leaky fixture must report its key at settings.py:9: %v", leaky)
	}
	encoded, err := json.Marshal(leaky)
	if err != nil || strings.Contains(string(encoded), secretsLeakyValue) {
		return fmt.Errorf("a secrets finding must never carry the secret: %v", err)
	}
	return nil
}

// secretsLeakyValue is secrets-leaky's made-up key, which no finding may carry.
const secretsLeakyValue = "9f8a7Qm2Lx0Zc4Vb6Nn1Ty8Ru3Ew5Qd" // gitleaks:allow
