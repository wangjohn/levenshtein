package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"dagger/levenshtein/internal/checktool"
	"dagger/levenshtein/internal/dagger"
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
	configs, err := source.Glob(ctx, checktool.SecretsConfig)
	if err != nil {
		return nil, err
	}

	ctr := withTool(goContainer(tools), toolGitleaks, "/usr/local/bin/gitleaks").
		WithDirectory("/src", source).
		WithWorkdir("/src")
	if nonce != "" {
		ctr = ctr.WithEnvVariable("LEVENSHTEIN_RUN_NONCE", nonce)
	}

	run, checked, err := runTool(ctx, ctr, checktool.SecretsArguments("/usr/local/bin/gitleaks", len(configs) == 1, secretsReport))
	if err != nil {
		return nil, err
	}
	var report []byte
	if run.ExitCode == 0 || run.ExitCode == checktool.SecretsLeakExit {
		contents, err := checked.File(secretsReport).Contents(ctx)
		if err != nil {
			return nil, fmt.Errorf("gitleaks exited %d without a report: %w: %s", run.ExitCode, err, strings.TrimSpace(run.Stderr))
		}
		report = []byte(contents)
	}
	return checktool.SecretsFindings(run, report)
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
