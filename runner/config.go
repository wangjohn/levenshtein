package main

import (
	"encoding/json"
	"fmt"
	"io"
	"path"
	"strings"
)

// config is deliberately small: runs compose known checks without a new DSL.
type config struct {
	Modules []string            `json:"modules"`
	Runs    map[string][]string `json:"runs"`
}

func defaultConfig() config {
	return config{Modules: []string{"."}, Runs: map[string][]string{
		"branch": {"go-lint", "go-vet"}, "pre-merge": {"go-lint", "go-vet"},
		"main": {"go-lint", "go-vet", "go-vuln"}, "go-lint": {"go-lint"},
		"go-vet": {"go-vet"}, "go-http": {"go-http"}, "go-sql": {"go-sql"},
		"go-vuln": {"go-vuln"}, "workflow-lint": {"workflow-lint"},
	}}
}

func parseConfig(content string) (config, error) {
	var cfg config
	decoder := json.NewDecoder(strings.NewReader(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cfg); err != nil {
		return cfg, fmt.Errorf("invalid levenshtein.json: %w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return cfg, fmt.Errorf("levenshtein.json must contain exactly one JSON object")
	}
	return cfg, nil
}

func (cfg config) selectChecks(run string) ([]string, error) {
	if len(cfg.Modules) == 0 {
		return nil, fmt.Errorf("configure at least one Go module directory")
	}
	seen := map[string]bool{}
	for _, module := range cfg.Modules {
		if module == "" || path.IsAbs(module) || path.Clean(module) != module || module == ".." || strings.HasPrefix(module, "../") || strings.Contains(module, "\\") {
			return nil, fmt.Errorf("module %q must be a clean relative directory inside the source repo", module)
		}
		if seen[module] {
			return nil, fmt.Errorf("duplicate module %q", module)
		}
		seen[module] = true
	}
	checks, ok := cfg.Runs[run]
	if !ok || len(checks) == 0 {
		return nil, fmt.Errorf("run %q is missing or empty in levenshtein.json", run)
	}
	seen = map[string]bool{}
	for _, check := range checks {
		if !knownCheck(check) {
			return nil, fmt.Errorf("unknown check %q in run %q", check, run)
		}
		if seen[check] {
			return nil, fmt.Errorf("duplicate check %q in run %q", check, run)
		}
		seen[check] = true
	}
	return checks, nil
}
